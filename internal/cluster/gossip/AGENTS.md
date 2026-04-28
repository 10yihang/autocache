# internal/cluster/gossip Knowledge Base

Apply the root `AGENTS.md` first, then `internal/cluster/AGENTS.md`, then these gossip-specific rules.

## OVERVIEW
Membership transport over TCP: ping/pong exchange, node-state machine (`Handshake → Connected → PFail → Fail`), slot epoch propagation, and failure detection for the cluster control plane.

## WHERE TO LOOK
- `gossip.go` — `Gossip`, `GossipNode`, lifecycle (`Start` / `Stop` / `Meet`), accept loop, ping loop (~1 s tick), pong / fail handlers, slot ownership propagation, `SlotAssigner` callback
- `message.go` — wire encoding for `Message` (ping / pong / fail) and `NodeInfo` payload
- `gossip_test.go` — slot-ownership and stale-epoch regression coverage
- See parent `internal/cluster/AGENTS.md` for how gossip fits with `SlotManager`, `Cluster`, and failover

## CONVENTIONS
- Mutate node maps under the existing locks; clone snapshots before returning them to other packages — callers must not mutate shared state.
- Keep state transitions explicit (`Handshake → Connected → PFail → Fail`) and centralize timeout thresholds (~15 s for PFail, multiple fail reports for Fail) in named constants.
- Slot ownership updates are epoch-sensitive; preserve stale-vs-newer ownership checks and do not let an older `ConfigEpoch` overwrite a newer one.
- Network and decode errors return wrapped context (`%w`); never silently downgrade a node's state on a single transient error.
- Long-running loops respect `ctx.Done()` and terminate cleanly through `Stop()` and the package `WaitGroup`.

## ANTI-PATTERNS
- Do not expose internal maps or `*GossipNode` without copying; downstream mutation breaks invariants.
- Do not introduce new timers or background loops that bypass the package `WaitGroup`.
- Do not change failure thresholds, gossip fanout, or slot-epoch rules casually; they alter cluster behavior system-wide and break replication ACK assumptions.
- Do not mix slot-assignment side effects into transport code unless the `SlotAssigner` callback contract requires it.

## VERIFY
```bash
go test -v ./internal/cluster/gossip/...
go test -v -tags=integration ./test/integration/cluster_test.go
```
