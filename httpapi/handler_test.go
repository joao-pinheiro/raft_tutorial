package httpapi

import (
	"net/http"
	"net/http/httptest"
	"raft_tutorial/kvstore"
	"raft_tutorial/raft"
	"raft_tutorial/testutil"
	"strings"
	"testing"
	"time"
)

func setupTestNode(t *testing.T) (*raft.RaftNode, *kvstore.KVStore, func()) {
	t.Helper()
	network := testutil.NewNetworkSimulator()
	storage := raft.NewMemoryStorage()
	transport := network.RegisterNode("node-0")

	// Single-node cluster (always becomes leader)
	node := raft.NewRaftNode("node-0", nil, storage, transport, raft.TestConfig())
	if err := node.Start(); err != nil {
		t.Fatal(err)
	}

	kv := kvstore.NewKVStore()
	stopCh := make(chan struct{})
	go kv.Run(node.ApplyCh(), stopCh)

	// Wait for leader
	err := testutil.WaitFor(5*time.Second, func() bool {
		return node.IsLeader()
	})
	if err != nil {
		node.Stop()
		t.Fatal("node never became leader")
	}

	cleanup := func() {
		close(stopCh)
		node.Stop()
	}
	return node, kv, cleanup
}

func TestHTTPSetAndGet(t *testing.T) {
	node, kv, cleanup := setupTestNode(t)
	defer cleanup()

	h := NewHandler(node, kv, func() string { return "" })

	// Set
	req := httptest.NewRequest(http.MethodPut, "/kv/foo", strings.NewReader("bar"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Small delay for apply
	time.Sleep(50 * time.Millisecond)

	// Get
	req = httptest.NewRequest(http.MethodGet, "/kv/foo", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "bar" {
		t.Fatalf("expected 'bar', got '%s'", w.Body.String())
	}
}

func TestHTTPGetNotFound(t *testing.T) {
	node, kv, cleanup := setupTestNode(t)
	defer cleanup()

	h := NewHandler(node, kv, func() string { return "" })

	req := httptest.NewRequest(http.MethodGet, "/kv/missing", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHTTPStatus(t *testing.T) {
	node, kv, cleanup := setupTestNode(t)
	defer cleanup()

	h := NewHandler(node, kv, func() string { return "" })

	req := httptest.NewRequest(http.MethodGet, "/raft/status", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"role":"Leader"`) {
		t.Fatalf("expected leader role in status: %s", w.Body.String())
	}
}
