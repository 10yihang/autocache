# internal/cluster/gossip Knowledge Base

Apply the root `AGENTS.md` first, then these gossip-specific rules.

## OVERVIEW
This package implements membership transport, ping/pong exchange, node-state transitions, slot epoch propagation, and failure detection for the cluster control plane.

## WHERE TO LOOK
- `internal/cluster/gossip/gossip.go` - listener lifecycle, ping loop, node registry, slot ownership propagation
- `internal/cluster/gossip/message.go` - wire message encoding and decoding
- `internal/cluster/gossip/gossip_test.go` - slot-ownership and stale-epoch regression tests
- `internal/cluster/...` tests - broader cluster tests cover routing, failover, and integration behavior transitively

## CONVENTIONS
- Protect node mutation with the existing locks; clone state before returning snapshots to other packages.
- Keep state transitions explicit (`Handshake -> Connected -> PFail -> Fail`) and time-based thresholds centralized in constants.
- Slot ownership updates are epoch-sensitive; preserve the stale-vs-newer ownership checks when touching gossip propagation.
- Network and decode errors should return wrapped context, not silently downgrade cluster state.
- Long-running loops must respect `ctx.Done()` and terminate cleanly through `Stop()`.

## ANTI-PATTERNS
- Do not expose internal maps or node structs without copying; callers must not mutate shared state.
- Do not introduce new timers or background loops that bypass the package `WaitGroup`.
- Do not change failure thresholds, gossip fanout, or slot-epoch rules casually; they alter cluster behavior system-wide.
- Do not mix slot-assignment side effects into unrelated transport code unless the slot callback contract requires it.

## VERIFY
```bash
go test -v ./internal/cluster/...
go test -v -tags=integration ./test/integration/...
```
