# internal/cluster Knowledge Base

Apply the root `AGENTS.md` first, then these cluster-control-plane rules. This file is the umbrella for `failover/`, `migration/`, `migrate/`, `router/`, `state/`, `hash/`, and the top-level `cluster.go` / `slots.go` / `node.go`. Sibling subpackages with their own AGENTS.md (`gossip/`, `replication/`) take precedence in their own scope.

## OVERVIEW
Cluster control plane: 16384-slot ownership, gossip-driven membership, LSN-based replication, failover, slot migration, cluster-aware routing, and persistent cluster state. Designed to be Redis-Cluster-compatible at the wire level.

## WHERE TO LOOK
- `cluster.go` — `Cluster` state machine (`Down` / `OK` / `Fail`), wires `gossip`, `SlotManager`, `replication.Manager`, `state.StateManager`; owns `currentEpoch` / `myEpoch`
- `slots.go` — `SlotManager`, `SlotInfo`, `SlotState` (`Normal` / `Importing` / `Exporting`), `SlotReplica` (NodeID + `MatchLSN` + `Healthy`), LSN allocation per slot
- `node.go` — `Node` metadata: ID, IP, ports, role, state, fail-report map
- `gossip/` — membership transport (own AGENTS.md)
- `replication/` — write log + ACK protocol (own AGENTS.md)
- `failover/manager.go` — `Manager.SelectBestReplica()` (highest `MatchLSN`), `PromoteBestReplica()`, ownership broadcast
- `migration/worker.go` — background `Worker` enumerating keys in `MIGRATING` slots, batched `RESTORE` to target, then delete-source
- `migrate/worker.go` — alternate / older migration path; check usage before extending
- `router/router.go` — `ClusterRouter` implementing `Route` / `RouteMulti`; produces `MOVED` / `ASK` redirects with addresses; respects `ASKING` flag
- `state/manager.go` — `StateManager` persists cluster topology to `cluster-state.json` with debounced writes; `ClusterStateProvider` interface decouples persistence from the live cluster
- `hash/crc16.go` — Redis CRC16 + hash-tag-aware `KeySlot()` (used by `tools/findkey`)
- `cluster_failover_test.go`, `slots_test.go`, subpackage `*_test.go` — coverage

## CONVENTIONS
- All slot mutations go through `SlotManager`; do not maintain shadow ownership maps elsewhere.
- `(epoch, lsn)` is the durable ordering for writes per slot; never reuse an LSN within an epoch and never lower an epoch.
- Replicas advance `MatchLSN`; `failover.SelectBestReplica` picks the replica with the highest value, so any code path that updates ACKs MUST flow through `SlotManager.UpdateReplicaLSN`.
- Persistent state changes use `StateManager.MarkDirty()` and rely on the debounced writer (~100 ms); do not write `cluster-state.json` directly.
- Background work (gossip loop, migration worker, replication stream) MUST respect `ctx.Done()` and the package `WaitGroup`; lifecycle is owned by `Cluster.Start` / `Stop`.
- Cluster-mode error strings (`MOVED`, `ASK`, `CROSSSLOT`, `CLUSTERDOWN`) are part of the wire contract; preserve formatting exactly.

## ANTI-PATTERNS
- Do not bypass `SlotManager` to mutate slot state or replicas.
- Do not promote a replica without first picking the highest `MatchLSN` and broadcasting the new ownership; silent promotion splits the cluster's view.
- Do not change `ConfigEpoch` rules or epoch-comparison logic casually; gossip stale-vs-newer detection depends on it.
- Do not assume `cluster-state.json` exists; `StateManager` must restore-or-initialize cleanly on first boot.
- Do not extend `migrate/worker.go` for new behavior without first checking whether `migration/worker.go` is the active path for the call site.

## VERIFY
```bash
go test -v ./internal/cluster/...
go test -v -tags=integration ./test/integration/cluster_test.go ./test/integration/migration_test.go
```
