package raft

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Storage is the interface for persisting Raft state.
type Storage interface {
	SaveState(state PersistentState) error
	LoadState() (PersistentState, error)
	SaveLog(entries []LogEntry) error
	LoadLog() ([]LogEntry, error)
}

// FileStorage persists state and log to JSON files in a directory.
type FileStorage struct {
	dir string
}

// NewFileStorage creates a FileStorage backed by the given directory.
func NewFileStorage(dir string) (*FileStorage, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return &FileStorage{dir: dir}, nil
}

func (fs *FileStorage) statePath() string { return filepath.Join(fs.dir, "state.json") }
func (fs *FileStorage) logPath() string   { return filepath.Join(fs.dir, "log.json") }

func (fs *FileStorage) SaveState(state PersistentState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return writeFileSync(fs.statePath(), data)
}

func (fs *FileStorage) LoadState() (PersistentState, error) {
	var state PersistentState
	data, err := os.ReadFile(fs.statePath())
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, err
	}
	err = json.Unmarshal(data, &state)
	return state, err
}

func (fs *FileStorage) SaveLog(entries []LogEntry) error {
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return writeFileSync(fs.logPath(), data)
}

// writeFileSync atomically writes data to path with fsync durability guarantees:
// write to a temp file in the same directory, fsync the file, rename over the
// target, then fsync the directory so the rename itself is durable. A crash at
// any point leaves either the old file or the fully-written new file.
func writeFileSync(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// On any error path, remove the temp file.
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	// fsync the directory so the rename is durable.
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (fs *FileStorage) LoadLog() ([]LogEntry, error) {
	data, err := os.ReadFile(fs.logPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var entries []LogEntry
	err = json.Unmarshal(data, &entries)
	return entries, err
}

// MemoryStorage is an in-memory Storage implementation for tests.
type MemoryStorage struct {
	mu      sync.Mutex
	state   PersistentState
	entries []LogEntry
}

func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{}
}

func (ms *MemoryStorage) SaveState(state PersistentState) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.state = state
	return nil
}

func (ms *MemoryStorage) LoadState() (PersistentState, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.state, nil
}

func (ms *MemoryStorage) SaveLog(entries []LogEntry) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.entries = make([]LogEntry, len(entries))
	copy(ms.entries, entries)
	return nil
}

func (ms *MemoryStorage) LoadLog() ([]LogEntry, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	result := make([]LogEntry, len(ms.entries))
	copy(result, ms.entries)
	return result, nil
}
