package testutil

import (
	"fmt"
	"raft_tutorial/kvstore"
	"raft_tutorial/raft"
	"time"
)

// TestCluster manages a cluster of in-process Raft nodes for testing.
type TestCluster struct {
	Nodes   []*raft.RaftNode
	KVs     []*kvstore.KVStore
	Network *NetworkSimulator

	ids     []string
	stopChs []chan struct{}
	storages []raft.Storage
	config  raft.Config
}

// NewTestCluster creates a cluster of n nodes but does not start them.
func NewTestCluster(n int) *TestCluster {
	network := NewNetworkSimulator()

	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("node-%d", i)
	}

	nodes := make([]*raft.RaftNode, n)
	kvs := make([]*kvstore.KVStore, n)
	stopChs := make([]chan struct{}, n)
	storages := make([]raft.Storage, n)
	config := raft.TestConfig()

	for i, id := range ids {
		peers := make([]string, 0, n-1)
		for j, pid := range ids {
			if i != j {
				peers = append(peers, pid)
			}
		}
		storage := raft.NewMemoryStorage()
		storages[i] = storage
		transport := network.RegisterNode(id)
		nodes[i] = raft.NewRaftNode(id, peers, storage, transport, config)
		kvs[i] = kvstore.NewKVStore()
		stopChs[i] = make(chan struct{})
	}

	return &TestCluster{
		Nodes:    nodes,
		KVs:      kvs,
		Network:  network,
		ids:      ids,
		stopChs:  stopChs,
		storages: storages,
		config:   config,
	}
}

// Start starts all nodes in the cluster.
func (tc *TestCluster) Start() error {
	for i, node := range tc.Nodes {
		if node == nil {
			continue
		}
		if err := node.Start(); err != nil {
			return fmt.Errorf("start node %d: %w", i, err)
		}
		go tc.KVs[i].Run(node.ApplyCh(), tc.stopChs[i])
	}
	return nil
}

// Stop stops all nodes in the cluster.
func (tc *TestCluster) Stop() {
	for i, node := range tc.Nodes {
		if node != nil {
			select {
			case <-tc.stopChs[i]:
			default:
				close(tc.stopChs[i])
			}
			node.Stop()
		}
	}
}

// WaitForLeader waits until exactly one leader is elected.
func (tc *TestCluster) WaitForLeader(timeout time.Duration) (*raft.RaftNode, error) {
	var leader *raft.RaftNode
	err := WaitFor(timeout, func() bool {
		leader = nil
		count := 0
		for _, n := range tc.Nodes {
			if n != nil && n.IsLeader() {
				leader = n
				count++
			}
		}
		return count == 1
	})
	return leader, err
}

// Leader returns the current leader, or nil.
func (tc *TestCluster) Leader() *raft.RaftNode {
	for _, n := range tc.Nodes {
		if n != nil && n.IsLeader() {
			return n
		}
	}
	return nil
}

// Followers returns nodes that are not the leader.
func (tc *TestCluster) Followers() []*raft.RaftNode {
	var result []*raft.RaftNode
	for _, n := range tc.Nodes {
		if n != nil && !n.IsLeader() {
			result = append(result, n)
		}
	}
	return result
}

// StopNode stops a node by index (simulates crash).
func (tc *TestCluster) StopNode(idx int) {
	if tc.Nodes[idx] != nil {
		select {
		case <-tc.stopChs[idx]:
		default:
			close(tc.stopChs[idx])
		}
		tc.Nodes[idx].Stop()
		tc.Nodes[idx] = nil
	}
}

// RestartNode restarts a previously stopped node.
func (tc *TestCluster) RestartNode(idx int) error {
	id := tc.ids[idx]
	peers := make([]string, 0, len(tc.ids)-1)
	for j, pid := range tc.ids {
		if j != idx {
			peers = append(peers, pid)
		}
	}

	transport := tc.Network.RegisterNode(id)
	tc.Nodes[idx] = raft.NewRaftNode(id, peers, tc.storages[idx], transport, tc.config)
	tc.KVs[idx] = kvstore.NewKVStore()
	tc.stopChs[idx] = make(chan struct{})

	if err := tc.Nodes[idx].Start(); err != nil {
		return err
	}
	go tc.KVs[idx].Run(tc.Nodes[idx].ApplyCh(), tc.stopChs[idx])
	return nil
}

// NodeIndex returns the index of a node by pointer.
func (tc *TestCluster) NodeIndex(node *raft.RaftNode) int {
	for i, n := range tc.Nodes {
		if n == node {
			return i
		}
	}
	return -1
}

// NodeID returns the ID of a node by index.
func (tc *TestCluster) NodeID(idx int) string {
	return tc.ids[idx]
}
