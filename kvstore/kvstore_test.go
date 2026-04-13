package kvstore

import (
	"raft_tutorial/raft"
	"testing"
)

func TestSetAndGet(t *testing.T) {
	kv := NewKVStore()

	cmd := Command{Type: CmdSet, Key: "hello", Value: "world"}
	data, err := cmd.Encode()
	if err != nil {
		t.Fatal(err)
	}

	kv.Apply(raft.ApplyMsg{Index: 1, Term: 1, Command: data})

	v, ok := kv.Get("hello")
	if !ok {
		t.Fatal("expected key to exist")
	}
	if v != "world" {
		t.Fatalf("expected 'world', got '%s'", v)
	}
}

func TestDelete(t *testing.T) {
	kv := NewKVStore()

	set := Command{Type: CmdSet, Key: "x", Value: "1"}
	data, _ := set.Encode()
	kv.Apply(raft.ApplyMsg{Index: 1, Term: 1, Command: data})

	del := Command{Type: CmdDelete, Key: "x"}
	data, _ = del.Encode()
	kv.Apply(raft.ApplyMsg{Index: 2, Term: 1, Command: data})

	_, ok := kv.Get("x")
	if ok {
		t.Fatal("expected key to be deleted")
	}
}

func TestGetMissing(t *testing.T) {
	kv := NewKVStore()
	_, ok := kv.Get("nonexistent")
	if ok {
		t.Fatal("expected key to not exist")
	}
}
