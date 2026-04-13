package testutil

import (
	"fmt"
	"raft_tutorial/raft"
	"sync"
)

// NetworkSimulator provides fault injection for in-process transports.
// It can partition nodes, isolate individual nodes, and drop messages.
type NetworkSimulator struct {
	mu         sync.RWMutex
	blocked    map[string]map[string]bool // blocked[from][to] = true
	transports map[string]*InProcTransport
	handlers   map[string]raft.RPCHandler
}

// NewNetworkSimulator creates a new simulator.
func NewNetworkSimulator() *NetworkSimulator {
	return &NetworkSimulator{
		blocked:    make(map[string]map[string]bool),
		transports: make(map[string]*InProcTransport),
		handlers:   make(map[string]raft.RPCHandler),
	}
}

// RegisterNode registers a node with the simulator and returns its transport.
func (ns *NetworkSimulator) RegisterNode(id string) *InProcTransport {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	t := &InProcTransport{
		id:      id,
		network: ns,
	}
	ns.transports[id] = t
	return t
}

// SetHandler registers the RPC handler for a node.
func (ns *NetworkSimulator) SetHandler(id string, handler raft.RPCHandler) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	ns.handlers[id] = handler
}

// RemoveHandler removes the RPC handler for a node (simulates crash).
func (ns *NetworkSimulator) RemoveHandler(id string) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	delete(ns.handlers, id)
}

// getHandler returns the handler for a node, or nil if not registered / crashed.
func (ns *NetworkSimulator) getHandler(id string) raft.RPCHandler {
	ns.mu.RLock()
	defer ns.mu.RUnlock()
	return ns.handlers[id]
}

// Partition creates a bidirectional network partition between two groups.
func (ns *NetworkSimulator) Partition(group1, group2 []string) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	for _, a := range group1 {
		for _, b := range group2 {
			ns.block(a, b)
			ns.block(b, a)
		}
	}
}

// Isolate completely isolates a node from all others.
func (ns *NetworkSimulator) Isolate(nodeID string) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	for id := range ns.transports {
		if id != nodeID {
			ns.block(nodeID, id)
			ns.block(id, nodeID)
		}
	}
}

// Heal removes all network partitions.
func (ns *NetworkSimulator) Heal() {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	ns.blocked = make(map[string]map[string]bool)
}

// IsBlocked returns whether messages from 'from' to 'to' are blocked.
func (ns *NetworkSimulator) IsBlocked(from, to string) bool {
	ns.mu.RLock()
	defer ns.mu.RUnlock()
	if m, ok := ns.blocked[from]; ok {
		return m[to]
	}
	return false
}

func (ns *NetworkSimulator) block(from, to string) {
	if ns.blocked[from] == nil {
		ns.blocked[from] = make(map[string]bool)
	}
	ns.blocked[from][to] = true
}

// InProcTransport is an in-process Transport that routes through NetworkSimulator.
type InProcTransport struct {
	id      string
	network *NetworkSimulator
}

func (t *InProcTransport) SendRequestVote(target string, args raft.RequestVoteArgs) (raft.RequestVoteReply, error) {
	if t.network.IsBlocked(t.id, target) {
		return raft.RequestVoteReply{}, fmt.Errorf("network: connection refused %s -> %s", t.id, target)
	}
	handler := t.network.getHandler(target)
	if handler == nil {
		return raft.RequestVoteReply{}, fmt.Errorf("network: node %s is down", target)
	}
	reply := handler.HandleRequestVote(args)
	return reply, nil
}

func (t *InProcTransport) SendAppendEntries(target string, args raft.AppendEntriesArgs) (raft.AppendEntriesReply, error) {
	if t.network.IsBlocked(t.id, target) {
		return raft.AppendEntriesReply{}, fmt.Errorf("network: connection refused %s -> %s", t.id, target)
	}
	handler := t.network.getHandler(target)
	if handler == nil {
		return raft.AppendEntriesReply{}, fmt.Errorf("network: node %s is down", target)
	}
	reply := handler.HandleAppendEntries(args)
	return reply, nil
}

func (t *InProcTransport) Serve(addr string, handler raft.RPCHandler) error {
	t.network.SetHandler(t.id, handler)
	return nil
}

func (t *InProcTransport) Stop() {
	t.network.RemoveHandler(t.id)
}
