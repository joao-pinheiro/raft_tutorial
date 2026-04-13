package raft

import "sort"

// replicateToAll sends AppendEntries RPCs to all peers.
// Must be called with rn.mu held.
func (rn *RaftNode) replicateToAll() {
	for _, peer := range rn.peers {
		go rn.replicateTo(peer)
	}
}

// replicateTo sends an AppendEntries RPC to a single peer.
func (rn *RaftNode) replicateTo(peer string) {
	rn.mu.Lock()
	if rn.role != Leader {
		rn.mu.Unlock()
		return
	}

	nextIndex := rn.leader.NextIndex[peer]
	prevLogIndex := nextIndex - 1
	var prevLogTerm uint64
	if prevLogIndex > 0 {
		entry := rn.log.GetEntry(prevLogIndex)
		if entry != nil {
			prevLogTerm = entry.Term
		}
	}

	var entries []LogEntry
	if nextIndex <= rn.log.LastIndex() {
		entries = rn.log.GetFrom(nextIndex)
	}

	args := AppendEntriesArgs{
		Term:         rn.persistent.CurrentTerm,
		LeaderID:     rn.id,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: rn.volatile.CommitIndex,
	}
	currentTerm := rn.persistent.CurrentTerm
	rn.mu.Unlock()

	reply, err := rn.transport.SendAppendEntries(peer, args)
	if err != nil {
		return
	}

	rn.mu.Lock()
	defer rn.mu.Unlock()

	// If we're no longer leader or the term changed, ignore response
	if rn.role != Leader || rn.persistent.CurrentTerm != currentTerm {
		return
	}

	if reply.Term > rn.persistent.CurrentTerm {
		rn.stepDown(reply.Term)
		return
	}

	if reply.Success {
		// Update NextIndex and MatchIndex
		if len(entries) > 0 {
			lastNewIndex := entries[len(entries)-1].Index
			rn.leader.NextIndex[peer] = lastNewIndex + 1
			rn.leader.MatchIndex[peer] = lastNewIndex
			rn.advanceCommitIndex()
		}
	} else {
		// Log inconsistency: use ConflictIndex/ConflictTerm for fast backup
		if reply.ConflictTerm > 0 {
			// Search for the last entry of ConflictTerm in our log
			lastConflict := uint64(0)
			for i := rn.log.LastIndex(); i >= 1; i-- {
				e := rn.log.GetEntry(i)
				if e != nil && e.Term == reply.ConflictTerm {
					lastConflict = i
					break
				}
			}
			if lastConflict > 0 {
				rn.leader.NextIndex[peer] = lastConflict + 1
			} else {
				rn.leader.NextIndex[peer] = reply.ConflictIndex
			}
		} else if reply.ConflictIndex > 0 {
			rn.leader.NextIndex[peer] = reply.ConflictIndex
		} else {
			// Simple decrement fallback
			if rn.leader.NextIndex[peer] > 1 {
				rn.leader.NextIndex[peer]--
			}
		}
		// Retry immediately
		go rn.replicateTo(peer)
	}
}

// advanceCommitIndex checks if the commit index can be advanced.
// Must be called with rn.mu held.
func (rn *RaftNode) advanceCommitIndex() {
	// Collect all matchIndex values (including leader's own last index)
	matchIndexes := make([]uint64, 0, len(rn.peers)+1)
	matchIndexes = append(matchIndexes, rn.log.LastIndex()) // leader
	for _, peer := range rn.peers {
		matchIndexes = append(matchIndexes, rn.leader.MatchIndex[peer])
	}

	sort.Slice(matchIndexes, func(i, j int) bool {
		return matchIndexes[i] > matchIndexes[j] // descending
	})

	// The majority-th element (0-indexed) is the highest index replicated to a majority
	majorityIdx := len(matchIndexes) / 2
	newCommitIndex := matchIndexes[majorityIdx]

	if newCommitIndex > rn.volatile.CommitIndex {
		// Only commit entries from the current term (Raft safety property, Section 5.4.2)
		entry := rn.log.GetEntry(newCommitIndex)
		if entry != nil && entry.Term == rn.persistent.CurrentTerm {
			rn.logger.Printf("[%s] advancing commitIndex %d -> %d", rn.id, rn.volatile.CommitIndex, newCommitIndex)
			rn.volatile.CommitIndex = newCommitIndex
			rn.signalCommit()
		}
	}
}

// HandleAppendEntries processes an incoming AppendEntries RPC.
func (rn *RaftNode) HandleAppendEntries(args AppendEntriesArgs) AppendEntriesReply {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	reply := AppendEntriesReply{Term: rn.persistent.CurrentTerm}

	// Reply false if term < currentTerm
	if args.Term < rn.persistent.CurrentTerm {
		return reply
	}

	// If RPC has higher or equal term, recognize the leader
	if args.Term > rn.persistent.CurrentTerm {
		rn.stepDown(args.Term)
		reply.Term = rn.persistent.CurrentTerm
	} else if rn.role == Candidate {
		// Same term but we're a candidate: step down since a leader exists
		rn.role = Follower
		if rn.heartbeatTimer != nil {
			rn.heartbeatTimer.Stop()
			rn.heartbeatTimer = nil
		}
	}

	rn.leaderID = args.LeaderID
	rn.resetElectionTimer()

	// Check if log contains an entry at PrevLogIndex with PrevLogTerm
	if args.PrevLogIndex > 0 {
		prevEntry := rn.log.GetEntry(args.PrevLogIndex)
		if prevEntry == nil {
			// We don't have an entry at PrevLogIndex
			reply.ConflictIndex = rn.log.LastIndex() + 1
			reply.ConflictTerm = 0
			return reply
		}
		if prevEntry.Term != args.PrevLogTerm {
			// Entry exists but term doesn't match: find the first index of the conflicting term
			reply.ConflictTerm = prevEntry.Term
			conflictIdx := args.PrevLogIndex
			for conflictIdx > 1 {
				e := rn.log.GetEntry(conflictIdx - 1)
				if e == nil || e.Term != reply.ConflictTerm {
					break
				}
				conflictIdx--
			}
			reply.ConflictIndex = conflictIdx
			return reply
		}
	}

	// Append new entries, deleting any conflicting existing entries
	for i, entry := range args.Entries {
		existing := rn.log.GetEntry(entry.Index)
		if existing == nil {
			// Append all remaining entries from this point
			rn.log.Append(args.Entries[i:]...)
			break
		}
		if existing.Term != entry.Term {
			// Conflict: delete this entry and all that follow, then append
			rn.log.TruncateFrom(entry.Index)
			rn.log.Append(args.Entries[i:]...)
			break
		}
		// Entry matches, continue
	}

	rn.persist()

	// Update commit index
	if args.LeaderCommit > rn.volatile.CommitIndex {
		lastNewIndex := rn.log.LastIndex()
		if args.LeaderCommit < lastNewIndex {
			rn.volatile.CommitIndex = args.LeaderCommit
		} else {
			rn.volatile.CommitIndex = lastNewIndex
		}
		rn.signalCommit()
	}

	reply.Success = true
	return reply
}
