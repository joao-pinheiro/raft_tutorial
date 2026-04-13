package raft

import "sync"

// LogEntry represents a single entry in the Raft log.
type LogEntry struct {
	Index   uint64 `json:"index"`
	Term    uint64 `json:"term"`
	Command []byte `json:"command"`
}

// RaftLog is a thread-safe in-memory log of entries.
// Entries are 1-indexed: the first real entry has Index=1.
type RaftLog struct {
	mu      sync.RWMutex
	entries []LogEntry
}

// NewRaftLog creates an empty log.
func NewRaftLog() *RaftLog {
	return &RaftLog{}
}

// NewRaftLogFromEntries creates a log pre-populated with entries.
func NewRaftLogFromEntries(entries []LogEntry) *RaftLog {
	cp := make([]LogEntry, len(entries))
	copy(cp, entries)
	return &RaftLog{entries: cp}
}

// Append adds entries to the end of the log.
func (l *RaftLog) Append(entries ...LogEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, entries...)
}

// GetEntry returns the entry at the given 1-based index, or nil if out of range.
func (l *RaftLog) GetEntry(index uint64) *LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if index == 0 || index > uint64(len(l.entries)) {
		return nil
	}
	e := l.entries[index-1]
	return &e
}

// GetRange returns entries from startIndex to endIndex inclusive (1-based).
// Returns nil if the range is invalid.
func (l *RaftLog) GetRange(startIndex, endIndex uint64) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if startIndex == 0 || startIndex > endIndex || startIndex > uint64(len(l.entries)) {
		return nil
	}
	if endIndex > uint64(len(l.entries)) {
		endIndex = uint64(len(l.entries))
	}
	result := make([]LogEntry, endIndex-startIndex+1)
	copy(result, l.entries[startIndex-1:endIndex])
	return result
}

// GetFrom returns all entries from startIndex onwards (1-based).
func (l *RaftLog) GetFrom(startIndex uint64) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if startIndex == 0 || startIndex > uint64(len(l.entries)) {
		return nil
	}
	result := make([]LogEntry, uint64(len(l.entries))-startIndex+1)
	copy(result, l.entries[startIndex-1:])
	return result
}

// LastIndex returns the index of the last log entry, or 0 if empty.
func (l *RaftLog) LastIndex() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if len(l.entries) == 0 {
		return 0
	}
	return l.entries[len(l.entries)-1].Index
}

// LastTerm returns the term of the last log entry, or 0 if empty.
func (l *RaftLog) LastTerm() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if len(l.entries) == 0 {
		return 0
	}
	return l.entries[len(l.entries)-1].Term
}

// Len returns the number of entries in the log.
func (l *RaftLog) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.entries)
}

// TruncateFrom removes all entries from the given index onwards (1-based, inclusive).
func (l *RaftLog) TruncateFrom(index uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if index == 0 || index > uint64(len(l.entries)) {
		return
	}
	l.entries = l.entries[:index-1]
}

// Entries returns a copy of all log entries.
func (l *RaftLog) Entries() []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	result := make([]LogEntry, len(l.entries))
	copy(result, l.entries)
	return result
}
