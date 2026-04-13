package raft

// NodeRole represents the current role of a Raft node.
type NodeRole int

const (
	Follower NodeRole = iota
	Candidate
	Leader
)

func (r NodeRole) String() string {
	switch r {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	default:
		return "Unknown"
	}
}

// PersistentState is persisted to stable storage before responding to RPCs.
type PersistentState struct {
	CurrentTerm uint64 `json:"currentTerm"`
	VotedFor    string `json:"votedFor"` // node ID, empty means no vote
}

// VolatileState is rebuilt on restart.
type VolatileState struct {
	CommitIndex uint64
	LastApplied uint64
}

// LeaderState is maintained only on the leader, reset after each election.
type LeaderState struct {
	NextIndex  map[string]uint64
	MatchIndex map[string]uint64
}

// NewLeaderState initializes leader state for the given peers.
// NextIndex is set to lastLogIndex+1; MatchIndex is set to 0.
func NewLeaderState(peers []string, lastLogIndex uint64) *LeaderState {
	ls := &LeaderState{
		NextIndex:  make(map[string]uint64, len(peers)),
		MatchIndex: make(map[string]uint64, len(peers)),
	}
	for _, p := range peers {
		ls.NextIndex[p] = lastLogIndex + 1
		ls.MatchIndex[p] = 0
	}
	return ls
}
