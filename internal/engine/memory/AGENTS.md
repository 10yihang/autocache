# internal/engine/memory Knowledge Base

Apply the root `AGENTS.md` first, then these hot-tier engine rules.

## OVERVIEW
This package is the performance-critical in-memory engine: sharded storage, expiry, eviction, Redis-type objects, and slot-aware key tracking live here.

## WHERE TO LOOK
- `internal/engine/memory/store.go` - public store facade, TTL/object coordination, stats
- `internal/engine/memory/shard.go` - shard implementation and hot-path cache behavior
- `internal/engine/memory/expiry.go` - expiration lifecycle
- `internal/engine/memory/eviction.go` - eviction policy wiring
- `internal/engine/memory/*_test.go` - behavior, slot, and scan coverage

## CONVENTIONS
- Keep hot-path allocations low; this package already uses atomics, shards, and byte helpers for a reason.
- Any string-key write path must keep cache data, object-key bookkeeping, expiry state, and slot tracking consistent.
- Use `context.Context` in exported methods to match the engine interface even when the current implementation does not need request-scoped state.
- Preserve Redis wrong-type and not-found semantics through `pkg/errors` sentinels.

## ANTI-PATTERNS
- Do not update one backing structure (`cache`, `objects`, `expires`) without the others when key shape changes.
- Do not add background work without explicit lifecycle ownership and `Close()` cleanup.
- Do not replace atomic counters with lock-heavy stats updates on the hot path.
- Do not trade correctness for micro-optimizations without targeted tests or benchmarks.

## VERIFY
```bash
go test -v ./internal/engine/memory/...
```
