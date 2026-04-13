.PHONY: build test test-verbose test-cover vet clean run-cluster

build:
	go build -o bin/raftkvd ./cmd/raftkvd/

test:
	go test ./... -timeout 120s

test-verbose:
	go test ./... -v -timeout 120s

test-cover:
	go test ./... -coverprofile=coverage.out -timeout 120s
	go tool cover -html=coverage.out -o coverage.html

vet:
	go vet ./...

clean:
	rm -rf bin/ coverage.out coverage.html

# Starts a 3-node cluster on localhost
run-cluster: build
	@echo "Starting 3-node Raft cluster..."
	@echo "Node 1: HTTP=localhost:8001 Raft=localhost:9001"
	@echo "Node 2: HTTP=localhost:8002 Raft=localhost:9002"
	@echo "Node 3: HTTP=localhost:8003 Raft=localhost:9003"
	@echo ""
	@echo "Kill any node with Ctrl+C in its terminal to test failover."
	@echo "---"
	@trap 'kill 0' EXIT; \
	./bin/raftkvd --id=node1 --raft-addr=localhost:9001 --http-addr=localhost:8001 \
		--peers=node2=localhost:9002=localhost:8002,node3=localhost:9003=localhost:8003 \
		--data-dir=/tmp/raft-node1 & \
	./bin/raftkvd --id=node2 --raft-addr=localhost:9002 --http-addr=localhost:8002 \
		--peers=node1=localhost:9001=localhost:8001,node3=localhost:9003=localhost:8003 \
		--data-dir=/tmp/raft-node2 & \
	./bin/raftkvd --id=node3 --raft-addr=localhost:9003 --http-addr=localhost:8003 \
		--peers=node1=localhost:9001=localhost:8001,node2=localhost:9002=localhost:8002 \
		--data-dir=/tmp/raft-node3 & \
	wait
