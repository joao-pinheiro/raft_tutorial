package kvstore

import (
	"raft_tutorial/raft"
	"sync"
)

// KVStore is an in-memory key/value store.
// Committed Raft log entries are applied here via the Apply method.
type KVStore struct {
	mu   sync.RWMutex
	data map[string]string
}

// NewKVStore creates an empty KV store.
func NewKVStore() *KVStore {
	return &KVStore{
		data: make(map[string]string),
	}
}

// Apply processes a committed log entry by executing the encoded command.
func (kv *KVStore) Apply(entry raft.ApplyMsg) {
	cmd, err := DecodeCommand(entry.Command)
	if err != nil {
		return
	}

	kv.mu.Lock()
	defer kv.mu.Unlock()

	switch cmd.Type {
	case CmdSet:
		kv.data[cmd.Key] = cmd.Value
	case CmdDelete:
		delete(kv.data, cmd.Key)
	}
}

// Get returns the value for a key, and whether it exists.
func (kv *KVStore) Get(key string) (string, bool) {
	kv.mu.RLock()
	defer kv.mu.RUnlock()
	v, ok := kv.data[key]
	return v, ok
}

// Run consumes committed entries from the apply channel and applies them.
// Blocks until the channel is closed or the stop channel is signaled.
func (kv *KVStore) Run(applyCh <-chan raft.ApplyMsg, stopCh <-chan struct{}) {
	for {
		select {
		case msg, ok := <-applyCh:
			if !ok {
				return
			}
			kv.Apply(msg)
		case <-stopCh:
			return
		}
	}
}
