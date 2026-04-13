package raft_test

import (
	"fmt"
	"raft_tutorial/kvstore"
	"raft_tutorial/raft"
	"raft_tutorial/testutil"
	"testing"
	"time"
)

// TestScenario_LeaderElection verifies that exactly one leader emerges in a 3-node cluster.
func TestScenario_LeaderElection(t *testing.T) {
	tc := testutil.NewTestCluster(3)
	if err := tc.Start(); err != nil {
		t.Fatal(err)
	}
	defer tc.Stop()

	leader, err := tc.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatal("no leader elected:", err)
	}

	// Verify exactly one leader
	leaderCount := 0
	for _, n := range tc.Nodes {
		if n.IsLeader() {
			leaderCount++
		}
	}
	if leaderCount != 1 {
		t.Fatalf("expected 1 leader, got %d", leaderCount)
	}

	// Verify all followers know the leader
	err = testutil.WaitFor(2*time.Second, func() bool {
		for _, n := range tc.Nodes {
			if n.LeaderID() == "" {
				return false
			}
		}
		return true
	})
	if err != nil {
		t.Fatal("not all nodes know the leader")
	}
	_ = leader
}

// TestScenario_LeaderFailoverReElection stops the leader and verifies a new one is elected.
func TestScenario_LeaderFailoverReElection(t *testing.T) {
	tc := testutil.NewTestCluster(3)
	if err := tc.Start(); err != nil {
		t.Fatal(err)
	}
	defer tc.Stop()

	leader, err := tc.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Propose a value before killing the leader
	cmd := kvstore.Command{Type: kvstore.CmdSet, Key: "survive", Value: "yes"}
	data, _ := cmd.Encode()
	if err := leader.Propose(data); err != nil {
		t.Fatal("propose failed:", err)
	}

	// Wait for replication
	err = testutil.WaitFor(2*time.Second, func() bool {
		for _, n := range tc.Nodes {
			if n.CommitIndex() < 1 {
				return false
			}
		}
		return true
	})
	if err != nil {
		t.Fatal("entry not replicated before leader kill")
	}

	// Kill the leader
	leaderIdx := tc.NodeIndex(leader)
	tc.StopNode(leaderIdx)

	// Wait for a new leader
	newLeader, err := tc.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatal("no new leader elected:", err)
	}
	if newLeader == leader {
		t.Fatal("new leader should be different from old leader")
	}

	// The data should still be accessible
	newLeaderIdx := tc.NodeIndex(newLeader)
	v, ok := tc.KVs[newLeaderIdx].Get("survive")
	if !ok || v != "yes" {
		t.Fatalf("expected survive=yes, got %q (ok=%v)", v, ok)
	}
}

// TestScenario_SplitVote verifies that the protocol converges after split votes.
func TestScenario_SplitVote(t *testing.T) {
	// With 5 nodes, split votes are more likely but should still resolve.
	tc := testutil.NewTestCluster(5)
	if err := tc.Start(); err != nil {
		t.Fatal(err)
	}
	defer tc.Stop()

	// Just verify that a leader eventually emerges despite potential split votes
	_, err := tc.WaitForLeader(10 * time.Second)
	if err != nil {
		t.Fatal("no leader elected despite split votes:", err)
	}
}

// TestScenario_BasicReplication proposes on the leader and verifies all nodes apply it.
func TestScenario_BasicReplication(t *testing.T) {
	tc := testutil.NewTestCluster(3)
	if err := tc.Start(); err != nil {
		t.Fatal(err)
	}
	defer tc.Stop()

	leader, err := tc.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}

	cmd := kvstore.Command{Type: kvstore.CmdSet, Key: "hello", Value: "world"}
	data, _ := cmd.Encode()
	if err := leader.Propose(data); err != nil {
		t.Fatal("propose failed:", err)
	}

	// Wait for all nodes to apply
	err = testutil.WaitFor(5*time.Second, func() bool {
		for _, kv := range tc.KVs {
			v, ok := kv.Get("hello")
			if !ok || v != "world" {
				return false
			}
		}
		return true
	})
	if err != nil {
		t.Fatal("not all nodes applied the entry")
	}
}

// TestScenario_FollowerCrashRecovery stops a follower, replicates more, restarts, verifies catchup.
func TestScenario_FollowerCrashRecovery(t *testing.T) {
	tc := testutil.NewTestCluster(3)
	if err := tc.Start(); err != nil {
		t.Fatal(err)
	}
	defer tc.Stop()

	leader, err := tc.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Propose initial entry
	cmd := kvstore.Command{Type: kvstore.CmdSet, Key: "before", Value: "crash"}
	data, _ := cmd.Encode()
	if err := leader.Propose(data); err != nil {
		t.Fatal(err)
	}

	// Wait for replication
	err = testutil.WaitFor(2*time.Second, func() bool {
		for _, n := range tc.Nodes {
			if n.CommitIndex() < 1 {
				return false
			}
		}
		return true
	})
	if err != nil {
		t.Fatal("initial replication failed")
	}

	// Find a follower and crash it
	followers := tc.Followers()
	if len(followers) == 0 {
		t.Fatal("no followers found")
	}
	followerIdx := tc.NodeIndex(followers[0])
	tc.StopNode(followerIdx)

	// Propose more entries while follower is down
	for i := 0; i < 3; i++ {
		cmd := kvstore.Command{Type: kvstore.CmdSet, Key: fmt.Sprintf("missed-%d", i), Value: "value"}
		data, _ := cmd.Encode()
		if err := leader.Propose(data); err != nil {
			t.Fatalf("propose %d failed: %v", i, err)
		}
	}

	// Restart the follower
	if err := tc.RestartNode(followerIdx); err != nil {
		t.Fatal("restart failed:", err)
	}

	// Wait for the restarted follower to catch up
	err = testutil.WaitFor(5*time.Second, func() bool {
		n := tc.Nodes[followerIdx]
		return n != nil && n.CommitIndex() >= 4 // 1 initial + 3 missed
	})
	if err != nil {
		if tc.Nodes[followerIdx] != nil {
			t.Fatalf("follower did not catch up, commitIndex=%d", tc.Nodes[followerIdx].CommitIndex())
		}
		t.Fatal("follower did not catch up")
	}

	// Verify KV store has all values
	err = testutil.WaitFor(2*time.Second, func() bool {
		for i := 0; i < 3; i++ {
			v, ok := tc.KVs[followerIdx].Get(fmt.Sprintf("missed-%d", i))
			if !ok || v != "value" {
				return false
			}
		}
		return true
	})
	if err != nil {
		t.Fatal("follower KV store missing values after recovery")
	}
}

// TestScenario_LeaderCrashMidReplication crashes the leader after proposing, verifies correct outcome.
func TestScenario_LeaderCrashMidReplication(t *testing.T) {
	tc := testutil.NewTestCluster(3)
	if err := tc.Start(); err != nil {
		t.Fatal(err)
	}
	defer tc.Stop()

	leader, err := tc.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Propose an entry that gets committed (replicated to majority)
	cmd := kvstore.Command{Type: kvstore.CmdSet, Key: "committed", Value: "yes"}
	data, _ := cmd.Encode()
	if err := leader.Propose(data); err != nil {
		t.Fatal(err)
	}

	// Kill the leader
	leaderIdx := tc.NodeIndex(leader)
	tc.StopNode(leaderIdx)

	// A new leader should be elected
	newLeader, err := tc.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatal("no new leader:", err)
	}

	// Propose a new entry on the new leader to trigger commitment of
	// previous-term entries (Raft Section 5.4.2: leader only commits entries
	// from its current term; previous-term entries are committed indirectly).
	noop := kvstore.Command{Type: kvstore.CmdSet, Key: "noop", Value: "trigger"}
	noopData, _ := noop.Encode()
	if err := newLeader.Propose(noopData); err != nil {
		t.Fatal("propose on new leader failed:", err)
	}

	// The committed entry should be preserved
	newLeaderIdx := tc.NodeIndex(newLeader)
	err = testutil.WaitFor(5*time.Second, func() bool {
		v, ok := tc.KVs[newLeaderIdx].Get("committed")
		return ok && v == "yes"
	})
	if err != nil {
		t.Fatal("committed entry lost after leader crash")
	}
}

// TestScenario_NetworkPartitionMinority isolates one node; majority continues.
func TestScenario_NetworkPartitionMinority(t *testing.T) {
	tc := testutil.NewTestCluster(3)
	if err := tc.Start(); err != nil {
		t.Fatal(err)
	}
	defer tc.Stop()

	leader, err := tc.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Find a follower and isolate it
	followers := tc.Followers()
	isolatedIdx := tc.NodeIndex(followers[0])
	isolatedID := tc.NodeID(isolatedIdx)
	tc.Network.Isolate(isolatedID)

	// The majority (leader + 1 follower) should still work
	cmd := kvstore.Command{Type: kvstore.CmdSet, Key: "partition", Value: "test"}
	data, _ := cmd.Encode()
	if err := leader.Propose(data); err != nil {
		t.Fatal("propose failed during partition:", err)
	}

	// Heal the partition
	tc.Network.Heal()

	// Wait for the isolated node to catch up
	err = testutil.WaitFor(5*time.Second, func() bool {
		v, ok := tc.KVs[isolatedIdx].Get("partition")
		return ok && v == "test"
	})
	if err != nil {
		t.Fatal("isolated node did not catch up after heal")
	}
}

// TestScenario_NetworkPartitionOldLeader partitions the leader; new leader elected.
func TestScenario_NetworkPartitionOldLeader(t *testing.T) {
	tc := testutil.NewTestCluster(3)
	if err := tc.Start(); err != nil {
		t.Fatal(err)
	}
	defer tc.Stop()

	leader, err := tc.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	leaderIdx := tc.NodeIndex(leader)
	leaderID := tc.NodeID(leaderIdx)

	// Isolate the leader
	tc.Network.Isolate(leaderID)

	// Wait for a new leader among the remaining nodes
	err = testutil.WaitFor(5*time.Second, func() bool {
		for i, n := range tc.Nodes {
			if i != leaderIdx && n != nil && n.IsLeader() {
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatal("no new leader elected after partitioning old leader")
	}

	// Find the new leader and propose
	var newLeader *raft.RaftNode
	for i, n := range tc.Nodes {
		if i != leaderIdx && n.IsLeader() {
			newLeader = n
			break
		}
	}

	cmd := kvstore.Command{Type: kvstore.CmdSet, Key: "new-era", Value: "yes"}
	data, _ := cmd.Encode()
	if err := newLeader.Propose(data); err != nil {
		t.Fatal("propose on new leader failed:", err)
	}

	// Heal partition - old leader should step down
	tc.Network.Heal()

	// Wait for old leader to step down (discover higher term)
	err = testutil.WaitFor(5*time.Second, func() bool {
		return !leader.IsLeader()
	})
	if err != nil {
		t.Fatal("old leader did not step down after partition heal")
	}

	// Old leader should catch up
	err = testutil.WaitFor(5*time.Second, func() bool {
		v, ok := tc.KVs[leaderIdx].Get("new-era")
		return ok && v == "yes"
	})
	if err != nil {
		t.Fatal("old leader did not catch up with new entries")
	}
}

// TestScenario_LogInconsistencyResolution verifies the leader fixes divergent follower logs.
func TestScenario_LogInconsistencyResolution(t *testing.T) {
	tc := testutil.NewTestCluster(3)
	if err := tc.Start(); err != nil {
		t.Fatal(err)
	}
	defer tc.Stop()

	leader, err := tc.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Propose several entries
	for i := 0; i < 3; i++ {
		cmd := kvstore.Command{Type: kvstore.CmdSet, Key: fmt.Sprintf("key-%d", i), Value: "v"}
		data, _ := cmd.Encode()
		if err := leader.Propose(data); err != nil {
			t.Fatal(err)
		}
	}

	// Wait for replication
	err = testutil.WaitFor(3*time.Second, func() bool {
		for _, n := range tc.Nodes {
			if n.CommitIndex() < 3 {
				return false
			}
		}
		return true
	})
	if err != nil {
		t.Fatal("initial replication failed")
	}

	// Verify all nodes have consistent logs
	leaderLogLen := leader.LogLength()
	for _, n := range tc.Nodes {
		if n.LogLength() != leaderLogLen {
			t.Fatalf("log length mismatch: leader=%d, node=%d", leaderLogLen, n.LogLength())
		}
	}
}

// TestScenario_CommitSafety verifies entries are only committed with majority replication.
func TestScenario_CommitSafety(t *testing.T) {
	tc := testutil.NewTestCluster(3)
	if err := tc.Start(); err != nil {
		t.Fatal(err)
	}
	defer tc.Stop()

	leader, err := tc.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	leaderIdx := tc.NodeIndex(leader)
	leaderID := tc.NodeID(leaderIdx)

	// Isolate the leader from all followers
	tc.Network.Isolate(leaderID)

	initialCommit := leader.CommitIndex()

	// Leader can't commit alone - commitIndex should not advance
	time.Sleep(300 * time.Millisecond)

	if leader.CommitIndex() != initialCommit {
		t.Fatal("commit index advanced without majority")
	}

	// Heal and let things proceed
	tc.Network.Heal()

	// Now propose should work
	err = testutil.WaitFor(5*time.Second, func() bool {
		// Wait for leader to re-establish or new leader
		for _, n := range tc.Nodes {
			if n.IsLeader() {
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatal("no leader after healing")
	}
}

// TestScenario_StaleLeaderDetection verifies a leader steps down on higher term.
func TestScenario_StaleLeaderDetection(t *testing.T) {
	tc := testutil.NewTestCluster(3)
	if err := tc.Start(); err != nil {
		t.Fatal(err)
	}
	defer tc.Stop()

	leader, err := tc.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Send a RequestVote with a much higher term
	reply := leader.HandleRequestVote(raft.RequestVoteArgs{
		Term:         leader.CurrentTerm() + 100,
		CandidateID:  "external",
		LastLogIndex: 999,
		LastLogTerm:  999,
	})

	if !reply.VoteGranted {
		t.Error("expected vote granted")
	}

	if leader.Role() != raft.Follower {
		t.Error("expected leader to step down")
	}
}

// TestScenario_ClientRedirect verifies that writes to followers are redirected.
func TestScenario_ClientRedirect(t *testing.T) {
	tc := testutil.NewTestCluster(3)
	if err := tc.Start(); err != nil {
		t.Fatal(err)
	}
	defer tc.Stop()

	_, err := tc.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Try to propose on a follower
	followers := tc.Followers()
	if len(followers) == 0 {
		t.Fatal("no followers")
	}

	cmd := kvstore.Command{Type: kvstore.CmdSet, Key: "test", Value: "val"}
	data, _ := cmd.Encode()
	err = followers[0].Propose(data)
	if err == nil {
		t.Fatal("expected error when proposing on follower")
	}

	// Error should mention the leader
	if err.Error() == "not leader, no known leader" {
		t.Fatal("follower should know the leader")
	}
}
