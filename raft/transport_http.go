package raft

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// HTTPTransport implements Transport using HTTP/JSON.
type HTTPTransport struct {
	mu       sync.Mutex
	server   *http.Server
	listener net.Listener
	client   *http.Client
}

// NewHTTPTransport creates a new HTTP-based transport.
func NewHTTPTransport() *HTTPTransport {
	return &HTTPTransport{
		client: &http.Client{Timeout: 2 * time.Second},
	}
}

func (t *HTTPTransport) SendRequestVote(target string, args RequestVoteArgs) (RequestVoteReply, error) {
	var reply RequestVoteReply
	err := t.sendRPC(target, "/raft/request-vote", args, &reply)
	return reply, err
}

func (t *HTTPTransport) SendAppendEntries(target string, args AppendEntriesArgs) (AppendEntriesReply, error) {
	var reply AppendEntriesReply
	err := t.sendRPC(target, "/raft/append-entries", args, &reply)
	return reply, err
}

func (t *HTTPTransport) sendRPC(target, path string, args interface{}, reply interface{}) error {
	body, err := json.Marshal(args)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s%s", target, path)
	resp, err := t.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, reply)
}

func (t *HTTPTransport) Serve(addr string, handler RPCHandler) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/raft/request-vote", func(w http.ResponseWriter, r *http.Request) {
		var args RequestVoteArgs
		if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		reply := handler.HandleRequestVote(args)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(reply)
	})

	mux.HandleFunc("/raft/append-entries", func(w http.ResponseWriter, r *http.Request) {
		var args AppendEntriesArgs
		if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		reply := handler.HandleAppendEntries(args)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(reply)
	})

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	t.mu.Lock()
	t.listener = ln
	t.server = &http.Server{Handler: mux}
	t.mu.Unlock()

	go t.server.Serve(ln)
	return nil
}

func (t *HTTPTransport) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		t.server.Shutdown(ctx)
	}
}

// Addr returns the listener address, useful for tests.
func (t *HTTPTransport) Addr() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.listener != nil {
		return t.listener.Addr().String()
	}
	return ""
}
