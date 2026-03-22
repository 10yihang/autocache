# internal/cluster/gossip Knowledge Base

Apply the root `AGENTS.md` first, then these gossip-specific rules.

## OVERVIEW
This package implements membership transport, ping/pong exchange, node-state transitions, and failure detection for the cluster control plane.

## WHERE TO LOOK
- `internal/cluster/gossip/gossip.go` - listener lifecycle, ping loop, failure detection, node registry
- `internal/cluster/gossip/message.go` - wire message encoding and decoding
- `internal/cluster/gossip/failover.go` - failure-oriented cluster transitions
- `internal/cluster/gossip/ping.go` - ping/pong message helpers
- `internal/cluster/...` tests - broader cluster tests cover gossip behavior transitively

## CONVENTIONS
- Protect node mutation with the existing locks; clone state before returning snapshots to other packages.
- Keep state transitions explicit (`Handshake -> Connected -> PFail -> Fail`) and time-based thresholds centralized in constants.
- Network and decode errors should return wrapped context, not silently downgrade cluster state.
- Long-running loops must respect `ctx.Done()` and terminate cleanly through `Stop()`.

## ANTI-PATTERNS
- Do not expose internal maps or node structs without copying; callers must not mutate shared state.
- Do not introduce new timers or background loops that bypass the package `WaitGroup`.
- Do not change failure thresholds or gossip fanout casually; they alter cluster behavior system-wide.
- Do not mix slot-assignment side effects into unrelated transport code unless the slot callback contract requires it.

## VERIFY
```bash
go test -v ./internal/cluster/...
go test -v -tags=integration ./test/integration/...
```
