package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"raft_tutorial/httpapi"
	"raft_tutorial/kvstore"
	"raft_tutorial/raft"
	"strings"
	"syscall"
)

func main() {
	id := flag.String("id", "", "Node ID")
	raftAddr := flag.String("raft-addr", "", "Raft RPC listen address (host:port)")
	httpAddr := flag.String("http-addr", "", "HTTP API listen address (host:port)")
	peersFlag := flag.String("peers", "", "Comma-separated peers: id=raftAddr=httpAddr,...")
	dataDir := flag.String("data-dir", "", "Data directory for persistent state")
	flag.Parse()

	if *id == "" || *raftAddr == "" || *httpAddr == "" {
		fmt.Fprintln(os.Stderr, "Usage: raftkvd --id=ID --raft-addr=HOST:PORT --http-addr=HOST:PORT [--peers=...] [--data-dir=DIR]")
		os.Exit(1)
	}

	// Parse peers
	var peerIDs []string
	peerHTTPAddrs := make(map[string]string) // nodeID -> httpAddr
	if *peersFlag != "" {
		for _, p := range strings.Split(*peersFlag, ",") {
			parts := strings.SplitN(p, "=", 3)
			if len(parts) != 3 {
				log.Fatalf("invalid peer format: %s (expected id=raftAddr=httpAddr)", p)
			}
			peerIDs = append(peerIDs, parts[1]) // use raftAddr as peer identifier for transport
			peerHTTPAddrs[parts[0]] = parts[2]
		}
	}

	// Storage
	var storage raft.Storage
	if *dataDir != "" {
		var err error
		storage, err = raft.NewFileStorage(*dataDir)
		if err != nil {
			log.Fatalf("failed to create storage: %v", err)
		}
	} else {
		storage = raft.NewMemoryStorage()
	}

	// Transport
	transport := raft.NewHTTPTransport()

	// Create Raft node (use raftAddr as the node ID for transport routing)
	node := raft.NewRaftNode(*raftAddr, peerIDs, storage, transport, raft.DefaultConfig())
	if err := node.Start(); err != nil {
		log.Fatalf("failed to start raft node: %v", err)
	}

	// KV store
	kv := kvstore.NewKVStore()
	stopCh := make(chan struct{})
	go kv.Run(node.ApplyCh(), stopCh)

	// HTTP API
	leaderAddr := func() string {
		leaderRaftAddr := node.LeaderID()
		// Map raft addr back to HTTP addr
		for pid, hAddr := range peerHTTPAddrs {
			_ = pid
			// The leader ID is the raft address
			if leaderRaftAddr != "" {
				// We stored raft addresses as peer IDs
				// Check all peers for a match
				for _, p := range strings.Split(*peersFlag, ",") {
					parts := strings.SplitN(p, "=", 3)
					if len(parts) == 3 && parts[1] == leaderRaftAddr {
						return parts[2]
					}
				}
			}
			_ = hAddr
		}
		if leaderRaftAddr == *raftAddr {
			return *httpAddr
		}
		return ""
	}

	handler := httpapi.NewHandler(node, kv, leaderAddr)

	srv := &http.Server{
		Addr:    *httpAddr,
		Handler: handler,
	}

	go func() {
		log.Printf("HTTP API listening on %s", *httpAddr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	log.Printf("Node %s started (raft=%s, http=%s)", *id, *raftAddr, *httpAddr)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")
	close(stopCh)
	srv.Close()
	node.Stop()
	log.Println("Stopped.")
}
