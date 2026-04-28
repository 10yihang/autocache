# internal/cluster/replication Knowledge Base

Apply the root `AGENTS.md` first, then `internal/cluster/AGENTS.md`, then these replication-specific rules.

## OVERVIEW
LSN-based, per-slot write replication. Primary allocates `(epoch, lsn)` per slot, appends to an in-memory log, streams ops to replicas, and tracks ACKs by `MatchLSN`. Full-sync handles cold-start replicas.

## WHERE TO LOOK
- `manager.go` — `Manager`, `WriteRequest` (Slot / OpType / Key / Payload / ExpireAtNs / `Apply` callback), `SlotLSNAllocator`, primary-side `ApplyWrite()` orchestration; wires `LogStore`, `Stream`, `AckTracker`
- `log.go` — `LogStore` (append-only, capacity-bounded), `Op` (Slot/Epoch/LSN/OpType/Key/Payload), `ReplicaAck`, `AckTracker`
- `stream.go` — `Stream` dispatches ops to replica peers; bounded channel backpressure
- `applier.go` — `ReplicaApplier` consumes ops on the replica side and invokes the local apply callback
- `peer.go` — `PeerTarget` resolution; one peer per replica `NodeID`
- `fullsync.go` — cold-start full-state transfer to a fresh replica; `fullsync_test.go` covers correctness
- `ack.go`, `sync.go` — small helpers wiring ACK propagation back into `SlotManager`
- `manager_test.go`, `applier_test.go`, `log_test.go`, `fullsync_test.go` — coverage

## CONVENTIONS
- All writes funnel through `Manager.ApplyWrite()`; the `Apply` callback runs the local mutation, then the manager allocates LSN, appends to log, and dispatches.
- LSN MUST be monotonically increasing per `(slot, epoch)`; never reuse, never skip backwards. The `SlotLSNAllocator` is the single source of truth.
- ACK propagation MUST end at `SlotManager.UpdateReplicaLSN` (via the `updateReplicaLSN` callback set on `Manager`); failover correctness depends on this.
- Replicas apply ops in LSN order; `ReplicaApplier` MUST refuse to skip gaps. Use full-sync (`fullsync.go`) when a replica falls too far behind the log capacity.
- The log is in-memory and capacity-bounded (default 1024 entries via `NewLogStore`); evicted entries force any lagging replica to recover via full-sync.
- Stream channel sizing matters: a too-small buffer stalls writes; a too-large buffer hides slow peers. Default is 1024 — change with benchmarks.

## ANTI-PATTERNS
- Do not call the local apply mutation outside `Manager.ApplyWrite()`; bypassing the manager skips LSN allocation and the log append, breaking replicas.
- Do not allocate or compare LSNs by hand; always go through the `SlotLSNAllocator`.
- Do not propagate ACKs without updating `SlotManager.UpdateReplicaLSN`; the slot's `MatchLSN` is what failover reads.
- Do not assume the replica is in sync; check log capacity and trigger `fullsync` when needed.
- Do not block the apply callback on network I/O; the manager invokes it under its own locks and on the hot write path.

## VERIFY
```bash
go test -v ./internal/cluster/replication/...
go test -v -tags=integration ./test/integration/...
```
