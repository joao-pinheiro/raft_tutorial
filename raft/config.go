package raft

import "time"

// Config holds tunable parameters for a Raft node.
type Config struct {
	ElectionTimeoutBase   time.Duration
	ElectionTimeoutJitter time.Duration
	HeartbeatInterval     time.Duration
}

// DefaultConfig returns production-suitable default configuration.
func DefaultConfig() Config {
	return Config{
		ElectionTimeoutBase:   150 * time.Millisecond,
		ElectionTimeoutJitter: 150 * time.Millisecond,
		HeartbeatInterval:     50 * time.Millisecond,
	}
}

// TestConfig returns fast timeouts suitable for tests.
func TestConfig() Config {
	return Config{
		ElectionTimeoutBase:   50 * time.Millisecond,
		ElectionTimeoutJitter: 50 * time.Millisecond,
		HeartbeatInterval:     20 * time.Millisecond,
	}
}

// Peer represents a node in the cluster.
type Peer struct {
	ID       string
	RaftAddr string
	HTTPAddr string
}
