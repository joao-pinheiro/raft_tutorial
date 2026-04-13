package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"raft_tutorial/kvstore"
	"raft_tutorial/raft"
	"strings"
)

// Handler serves the client-facing HTTP API.
type Handler struct {
	node       *raft.RaftNode
	kv         *kvstore.KVStore
	leaderAddr func() string // returns the HTTP address of the current leader
}

// NewHandler creates a new HTTP handler.
// leaderAddr should return the HTTP address of the current leader (for redirects).
func NewHandler(node *raft.RaftNode, kv *kvstore.KVStore, leaderAddr func() string) *Handler {
	return &Handler{
		node:       node,
		kv:         kv,
		leaderAddr: leaderAddr,
	}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/kv/") {
		h.handleKV(w, r)
		return
	}
	if r.URL.Path == "/raft/status" {
		h.handleStatus(w, r)
		return
	}
	http.NotFound(w, r)
}

func (h *Handler) handleKV(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/kv/")
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		v, ok := h.kv.Get(key)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(v))

	case http.MethodPut:
		if !h.node.IsLeader() {
			h.redirectToLeader(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cmd := kvstore.Command{Type: kvstore.CmdSet, Key: key, Value: string(body)}
		data, err := cmd.Encode()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := h.node.Propose(data); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)

	case http.MethodDelete:
		if !h.node.IsLeader() {
			h.redirectToLeader(w, r)
			return
		}
		cmd := kvstore.Command{Type: kvstore.CmdDelete, Key: key}
		data, err := cmd.Encode()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := h.node.Propose(data); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) redirectToLeader(w http.ResponseWriter, r *http.Request) {
	addr := h.leaderAddr()
	if addr == "" {
		http.Error(w, "no known leader", http.StatusServiceUnavailable)
		return
	}
	http.Redirect(w, r, "http://"+addr+r.URL.Path, http.StatusTemporaryRedirect)
}

type statusResponse struct {
	Role        string `json:"role"`
	Term        uint64 `json:"term"`
	LeaderID    string `json:"leaderId"`
	CommitIndex uint64 `json:"commitIndex"`
	LogLength   int    `json:"logLength"`
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := statusResponse{
		Role:        h.node.Role().String(),
		Term:        h.node.CurrentTerm(),
		LeaderID:    h.node.LeaderID(),
		CommitIndex: h.node.CommitIndex(),
		LogLength:   h.node.LogLength(),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}
