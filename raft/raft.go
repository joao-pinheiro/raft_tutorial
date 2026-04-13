package raft

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// ApplyMsg is sent on the applyCh when an entry is committed.
type ApplyMsg struct {
	Index   uint64
	Term    uint64
	Command []byte
}

// proposalFuture tracks a pending proposal waiting for commitment.
type proposalFuture struct {
	index    uint64
	errCh    chan error
}

// RaftNode implements the Raft consensus protocol.
type RaftNode struct {
	mu sync.Mutex

	id    string
	role  NodeRole
	peers []string

	persistent PersistentState
	volatile   VolatileState
	leader     *LeaderState

	log       *RaftLog
	storage   Storage
	transport Transport
	config    Config

	applyCh  chan ApplyMsg
	commitCh chan struct{} // signals new commits to apply

	// Proposal tracking: log index -> future
	pendingMu  sync.Mutex
	pending    map[uint64]*proposalFuture

	electionTimer  *time.Timer
	heartbeatTimer *time.Timer

	stopCh chan struct{}
	wg     sync.WaitGroup
	logger *log.Logger

	leaderID string // ID of the current known leader
}

// NewRaftNode creates a new Raft node. Call Start() to begin.
func NewRaftNode(id string, peers []string, storage Storage, transport Transport, config Config) *RaftNode {
	return &RaftNode{
		id:        id,
		role:      Follower,
		peers:     peers,
		log:       NewRaftLog(),
		storage:   storage,
		transport: transport,
		config:    config,
		applyCh:   make(chan ApplyMsg, 100),
		commitCh:  make(chan struct{}, 1),
		pending:   make(map[uint64]*proposalFuture),
		stopCh:    make(chan struct{}),
		logger:    log.New(os.Stderr, "", log.LstdFlags|log.Lmicroseconds),
	}
}

// Start initializes the node: loads persisted state, starts the transport, and
// begins the main event loops.
func (rn *RaftNode) Start() error {
	// Load persisted state
	state, err := rn.storage.LoadState()
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	rn.persistent = state

	entries, err := rn.storage.LoadLog()
	if err != nil {
		return fmt.Errorf("load log: %w", err)
	}
	if len(entries) > 0 {
		rn.log = NewRaftLogFromEntries(entries)
	}

	// Start transport
	if err := rn.transport.Serve(rn.id, rn); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	// Start election timer
	rn.mu.Lock()
	rn.resetElectionTimer()
	rn.mu.Unlock()

	// Start apply loop
	rn.wg.Add(1)
	go rn.applyLoop()

	rn.logger.Printf("[%s] started (term=%d, log=%d entries)", rn.id, rn.persistent.CurrentTerm, rn.log.Len())
	return nil
}

// Stop gracefully shuts down the node.
func (rn *RaftNode) Stop() {
	rn.mu.Lock()
	if rn.electionTimer != nil {
		rn.electionTimer.Stop()
	}
	if rn.heartbeatTimer != nil {
		rn.heartbeatTimer.Stop()
	}
	rn.mu.Unlock()

	close(rn.stopCh)
	rn.transport.Stop()
	rn.wg.Wait()
}

// Propose submits a command to the Raft cluster.
// Returns an error if this node is not the leader.
// Blocks until the entry is committed or the node loses leadership.
func (rn *RaftNode) Propose(command []byte) error {
	rn.mu.Lock()
	if rn.role != Leader {
		leaderID := rn.leaderID
		rn.mu.Unlock()
		if leaderID != "" {
			return fmt.Errorf("not leader, leader is %s", leaderID)
		}
		return fmt.Errorf("not leader, no known leader")
	}

	entry := LogEntry{
		Index:   rn.log.LastIndex() + 1,
		Term:    rn.persistent.CurrentTerm,
		Command: command,
	}
	rn.log.Append(entry)
	rn.persist()

	// Create a future for this proposal
	future := &proposalFuture{
		index: entry.Index,
		errCh: make(chan error, 1),
	}
	rn.pendingMu.Lock()
	rn.pending[entry.Index] = future
	rn.pendingMu.Unlock()

	rn.logger.Printf("[%s] proposed entry index=%d term=%d", rn.id, entry.Index, entry.Term)

	// Trigger replication to all peers
	rn.replicateToAll()

	// Advance commit index (important for single-node cluster, also helps multi-node)
	rn.advanceCommitIndex()

	rn.mu.Unlock()

	// Wait for commit
	select {
	case err := <-future.errCh:
		return err
	case <-rn.stopCh:
		return fmt.Errorf("node stopped")
	}
}

// ApplyCh returns the channel on which committed entries are delivered.
func (rn *RaftNode) ApplyCh() <-chan ApplyMsg {
	return rn.applyCh
}

// IsLeader returns true if this node is the current leader.
func (rn *RaftNode) IsLeader() bool {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	return rn.role == Leader
}

// LeaderID returns the ID of the current known leader.
func (rn *RaftNode) LeaderID() string {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	if rn.role == Leader {
		return rn.id
	}
	return rn.leaderID
}

// Role returns the current role of this node.
func (rn *RaftNode) Role() NodeRole {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	return rn.role
}

// CurrentTerm returns the current term.
func (rn *RaftNode) CurrentTerm() uint64 {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	return rn.persistent.CurrentTerm
}

// CommitIndex returns the current commit index.
func (rn *RaftNode) CommitIndex() uint64 {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	return rn.volatile.CommitIndex
}

// LogLength returns the number of entries in the log.
func (rn *RaftNode) LogLength() int {
	return rn.log.Len()
}

// stepDown transitions to follower for the given term.
// Must be called with rn.mu held.
func (rn *RaftNode) stepDown(term uint64) {
	wasLeader := rn.role == Leader
	rn.logger.Printf("[%s] stepping down to follower (term %d -> %d)", rn.id, rn.persistent.CurrentTerm, term)
	rn.role = Follower
	rn.persistent.CurrentTerm = term
	rn.persistent.VotedFor = ""
	rn.leader = nil
	if rn.heartbeatTimer != nil {
		rn.heartbeatTimer.Stop()
		rn.heartbeatTimer = nil
	}
	rn.persist()
	rn.resetElectionTimer()

	// Fail any pending proposals — this node can no longer commit them.
	// Committed entries will still be applied on whichever node holds them.
	if wasLeader {
		rn.failPendingProposals(fmt.Errorf("leadership lost before commit"))
	}
}

// failPendingProposals resolves all pending proposal futures with the given error.
func (rn *RaftNode) failPendingProposals(err error) {
	rn.pendingMu.Lock()
	defer rn.pendingMu.Unlock()
	for idx, f := range rn.pending {
		f.errCh <- err
		delete(rn.pending, idx)
	}
}

// becomeLeader transitions to leader.
// Must be called with rn.mu held.
func (rn *RaftNode) becomeLeader() {
	rn.logger.Printf("[%s] became leader for term %d", rn.id, rn.persistent.CurrentTerm)
	rn.role = Leader
	rn.leaderID = rn.id
	rn.leader = NewLeaderState(rn.peers, rn.log.LastIndex())

	// Stop election timer, start heartbeat timer
	if rn.electionTimer != nil {
		rn.electionTimer.Stop()
	}

	// Send immediate heartbeat
	rn.replicateToAll()
	rn.startHeartbeatTimer()
}

// startHeartbeatTimer starts the periodic heartbeat timer.
// Must be called with rn.mu held.
func (rn *RaftNode) startHeartbeatTimer() {
	if rn.heartbeatTimer != nil {
		rn.heartbeatTimer.Stop()
	}
	rn.heartbeatTimer = time.AfterFunc(rn.config.HeartbeatInterval, func() {
		rn.mu.Lock()
		if rn.role != Leader {
			rn.mu.Unlock()
			return
		}
		rn.replicateToAll()
		rn.startHeartbeatTimer()
		rn.mu.Unlock()
	})
}

// persist saves the current persistent state and log to storage.
// Must be called with rn.mu held (or during single-threaded init).
func (rn *RaftNode) persist() {
	if err := rn.storage.SaveState(rn.persistent); err != nil {
		rn.logger.Printf("[%s] ERROR saving state: %v", rn.id, err)
	}
	if err := rn.storage.SaveLog(rn.log.Entries()); err != nil {
		rn.logger.Printf("[%s] ERROR saving log: %v", rn.id, err)
	}
}

// applyLoop applies committed entries to the state machine.
func (rn *RaftNode) applyLoop() {
	defer rn.wg.Done()
	for {
		select {
		case <-rn.stopCh:
			return
		case <-rn.commitCh:
			rn.applyCommitted()
		}
	}
}

// applyCommitted sends all newly committed entries to the applyCh.
func (rn *RaftNode) applyCommitted() {
	rn.mu.Lock()
	commitIndex := rn.volatile.CommitIndex
	lastApplied := rn.volatile.LastApplied
	rn.mu.Unlock()

	for lastApplied < commitIndex {
		lastApplied++
		entry := rn.log.GetEntry(lastApplied)
		if entry == nil {
			break
		}

		msg := ApplyMsg{
			Index:   entry.Index,
			Term:    entry.Term,
			Command: entry.Command,
		}

		select {
		case rn.applyCh <- msg:
		case <-rn.stopCh:
			return
		}

		// Resolve pending proposal future if any
		rn.pendingMu.Lock()
		if f, ok := rn.pending[entry.Index]; ok {
			f.errCh <- nil
			delete(rn.pending, entry.Index)
		}
		rn.pendingMu.Unlock()
	}

	rn.mu.Lock()
	if lastApplied > rn.volatile.LastApplied {
		rn.volatile.LastApplied = lastApplied
	}
	rn.mu.Unlock()
}

// signalCommit non-blocking signal to the apply loop.
func (rn *RaftNode) signalCommit() {
	select {
	case rn.commitCh <- struct{}{}:
	default:
	}
}
