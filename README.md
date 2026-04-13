# Raft Tutorial: Distributed KV Store

A from-scratch Go implementation of the [Raft consensus protocol](https://raft.github.io/raft.pdf) backing an in-memory key/value store. No external Raft libraries -- every line of the consensus algorithm is implemented here for educational purposes.

## Architecture

```
                    +-------------------+
                    |   HTTP Client API |  PUT/GET/DELETE /kv/{key}
                    +--------+----------+  GET /raft/status
                             |
                    +--------v----------+
                    |    KV Store        |  State machine: applies committed commands
                    |  (map[string]str)  |
                    +--------+----------+
                             |  applyCh
                    +--------v----------+
                    |     Raft Node      |  Leader election + log replication
                    |                    |
                    | state.go   - roles, persistent/volatile state
                    | election.go - RequestVote, election timer
                    | replication.go - AppendEntries, commit logic
                    | log.go     - log entries, append/truncate
                    +--------+----------+
                             |  Transport interface
              +--------------+--------------+
              |                             |
     +--------v--------+          +--------v--------+
     | HTTP Transport   |          | InProc Transport |
     | (production)     |          | (tests)          |
     +------------------+          +---------+--------+
                                             |
                                   +---------v--------+
                                   | NetworkSimulator  |
                                   | (fault injection) |
                                   +------------------+
```

## Building

```bash
make build
```

## Running a 3-Node Cluster

```bash
make run-cluster
```

This starts 3 nodes on localhost. Interact via curl:

```bash
# Set a key (on the leader)
curl -X PUT localhost:8001/kv/greeting -d "hello world"

# Get a key
curl localhost:8001/kv/greeting

# Delete a key
curl -X DELETE localhost:8001/kv/greeting

# Check node status
curl localhost:8001/raft/status
```

Writes to a follower return a `307 Redirect` to the leader.

## Testing

```bash
# Run all tests
make test

# Verbose output
make test-verbose

# Coverage report
make test-cover
```

## Test Scenarios

The integration tests in `raft/scenario_test.go` cover all major Raft failure modes:

| Test | Scenario |
|------|----------|
| `TestScenario_LeaderElection` | Exactly one leader emerges |
| `TestScenario_LeaderFailoverReElection` | Leader crash triggers re-election; data preserved |
| `TestScenario_SplitVote` | Split votes resolve via randomized timeouts |
| `TestScenario_BasicReplication` | Entries replicated to all nodes |
| `TestScenario_FollowerCrashRecovery` | Crashed follower catches up on restart |
| `TestScenario_LeaderCrashMidReplication` | Committed entries survive leader crash |
| `TestScenario_NetworkPartitionMinority` | Isolated minority cannot elect; majority continues |
| `TestScenario_NetworkPartitionOldLeader` | Old leader steps down after partition heals |
| `TestScenario_LogInconsistencyResolution` | Leader overwrites divergent follower logs |
| `TestScenario_CommitSafety` | Entries only committed after majority replication |
| `TestScenario_StaleLeaderDetection` | Leader steps down on higher term |
| `TestScenario_ClientRedirect` | Followers redirect writes to leader |

## Manual Failure Testing

With `make run-cluster` running:

1. **Leader failure**: Find the leader via `curl localhost:800{1,2,3}/raft/status`, kill it with Ctrl+C, observe re-election.
2. **Network partition**: Use `iptables` or simply kill/restart nodes.
3. **Data persistence**: Kill all nodes, restart, verify data from `--data-dir`.

## What's Not Implemented

- **Log compaction / snapshots**: Logs grow unbounded. In production, periodic snapshots would truncate the log.
- **Dynamic membership changes**: Cluster membership is static (set at startup).
- **Read consistency**: `GET` reads are served from the local KV store without a read quorum. In production, you'd use read indexes or lease-based reads.

## References

- [In Search of an Understandable Consensus Algorithm (Raft paper)](https://raft.github.io/raft.pdf)
- [Raft visualization](https://raft.github.io/)
