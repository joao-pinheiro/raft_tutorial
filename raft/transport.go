package raft

// Transport is the interface for sending and receiving Raft RPCs.
type Transport interface {
	SendRequestVote(target string, args RequestVoteArgs) (RequestVoteReply, error)
	SendAppendEntries(target string, args AppendEntriesArgs) (AppendEntriesReply, error)
	Serve(addr string, handler RPCHandler) error
	Stop()
}

// RPCHandler processes incoming Raft RPCs.
type RPCHandler interface {
	HandleRequestVote(args RequestVoteArgs) RequestVoteReply
	HandleAppendEntries(args AppendEntriesArgs) AppendEntriesReply
}
