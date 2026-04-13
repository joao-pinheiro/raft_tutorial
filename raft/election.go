package raft

import (
	"math/rand"
	"sync"
	"time"
)

// resetElectionTimer resets the election timer with a randomized timeout.
// Must be called with rn.mu held.
func (rn *RaftNode) resetElectionTimer() {
	if rn.electionTimer != nil {
		rn.electionTimer.Stop()
	}
	timeout := rn.config.ElectionTimeoutBase + time.Duration(rand.Int63n(int64(rn.config.ElectionTimeoutJitter)))
	rn.electionTimer = time.AfterFunc(timeout, func() {
		rn.startElection()
	})
}

// startElection transitions to Candidate and begins a new election.
func (rn *RaftNode) startElection() {
	rn.mu.Lock()

	// Only followers and candidates start elections
	if rn.role == Leader {
		rn.mu.Unlock()
		return
	}

	rn.role = Candidate
	rn.persistent.CurrentTerm++
	rn.persistent.VotedFor = rn.id
	currentTerm := rn.persistent.CurrentTerm
	lastLogIndex := rn.log.LastIndex()
	lastLogTerm := rn.log.LastTerm()
	peers := rn.peers

	rn.persist()
	rn.resetElectionTimer()

	rn.logger.Printf("[%s] starting election for term %d", rn.id, currentTerm)

	// Single-node cluster: immediately become leader
	if len(peers) == 0 {
		rn.becomeLeader()
		rn.mu.Unlock()
		return
	}

	rn.mu.Unlock()

	args := RequestVoteArgs{
		Term:         currentTerm,
		CandidateID:  rn.id,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}

	var (
		mu     sync.Mutex
		votes  = 1 // vote for self
		needed = (len(peers)+1)/2 + 1
	)

	for _, peer := range peers {
		go func(peer string) {
			reply, err := rn.transport.SendRequestVote(peer, args)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				rn.logger.Printf("[%s] RequestVote to %s failed: %v", rn.id, peer, err)
				return
			}

			// If we see a higher term, step down immediately
			if reply.Term > currentTerm {
				rn.mu.Lock()
				rn.stepDown(reply.Term)
				rn.mu.Unlock()
				return
			}

			if reply.VoteGranted {
				votes++
				rn.logger.Printf("[%s] received vote from %s (votes=%d, needed=%d)", rn.id, peer, votes, needed)
			}

			// Check if we won the election
			if votes >= needed {
				rn.mu.Lock()
				// Verify we're still a candidate for the same term
				if rn.role == Candidate && rn.persistent.CurrentTerm == currentTerm {
					rn.becomeLeader()
				}
				rn.mu.Unlock()
			}
		}(peer)
	}
}

// HandleRequestVote processes an incoming RequestVote RPC.
func (rn *RaftNode) HandleRequestVote(args RequestVoteArgs) RequestVoteReply {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	reply := RequestVoteReply{Term: rn.persistent.CurrentTerm}

	// Reply false if term < currentTerm
	if args.Term < rn.persistent.CurrentTerm {
		return reply
	}

	// If RPC has higher term, step down
	if args.Term > rn.persistent.CurrentTerm {
		rn.stepDown(args.Term)
		reply.Term = rn.persistent.CurrentTerm
	}

	// Grant vote if we haven't voted or already voted for this candidate,
	// and candidate's log is at least as up-to-date as ours (Section 5.4.1)
	canVote := rn.persistent.VotedFor == "" || rn.persistent.VotedFor == args.CandidateID
	logOK := rn.isLogUpToDate(args.LastLogTerm, args.LastLogIndex)

	if canVote && logOK {
		rn.persistent.VotedFor = args.CandidateID
		rn.persist()
		rn.resetElectionTimer()
		reply.VoteGranted = true
		rn.logger.Printf("[%s] voted for %s in term %d", rn.id, args.CandidateID, args.Term)
	}

	return reply
}

// isLogUpToDate returns true if the candidate's log is at least as up-to-date as ours.
// Raft determines which of two logs is more up-to-date by comparing the index and term
// of the last entries. If the logs end with different terms, the one with the higher term
// is more up-to-date. If the logs end with the same term, the longer log is more up-to-date.
// Must be called with rn.mu held.
func (rn *RaftNode) isLogUpToDate(candidateLastTerm, candidateLastIndex uint64) bool {
	myLastTerm := rn.log.LastTerm()
	myLastIndex := rn.log.LastIndex()

	if candidateLastTerm != myLastTerm {
		return candidateLastTerm > myLastTerm
	}
	return candidateLastIndex >= myLastIndex
}
