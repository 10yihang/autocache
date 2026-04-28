# internal/engine/memory Knowledge Base

Apply the root `AGENTS.md` first, then these hot-tier engine rules.

## OVERVIEW
Performance-critical in-memory engine: 256-shard zero-GC cache, ring-buffer-backed value storage, TinyLFU admission control, Redis-type objects, unsafe helpers, and slot-aware key tracking.

## WHERE TO LOOK
- `store.go` — `Store` facade: TTL/object coordination, atomic `Stats`, slot indexing, eviction wiring
- `shard.go` — `ShardedCache` and `ZeroGCShard`: `map[uint64]uint64` index + ring buffer + per-shard RWMutex
- `ringbuf.go` — circular byte buffer for key/value pairs; collision detection by re-hashing
- `tinylfu.go` — `CountMinSketch` + `Doorkeeper` admission control on writes
- `dict.go` — object-type dictionary for Redis Hash/List/Set/ZSet
- `hash.go` — type-specific implementations
- `expiry.go` — background expiration sweeper (~100 ms tick)
- `eviction.go` — LRU/LFU/TTL eviction triggered when `maxMemory` exceeded
- `unsafe.go` — `StringToBytes` / `BytesToString` zero-copy converters
- `*_test.go` — behavior, slot, unsafe, and SCAN coverage

## CONVENTIONS
- Hot path is allocation-budgeted; this package already uses atomics, sharding, ring buffers, and unsafe helpers for a reason. Benchmark before adding allocations.
- A string-key write must keep `cache` (shard), `objects` (dict), `expires`, and `slotIndex` in sync; partial updates corrupt eviction and migration.
- Use `context.Context` in exported methods to match the engine interface even when not strictly needed today.
- Surface Redis semantics through `pkg/errors` sentinels (`ErrKeyNotFound`, `ErrWrongType`, `ErrNotInteger`, `ErrOOM`).
- Background work (`ExpiryManager`, `Evictor`) must own its lifecycle and clean up in `Close()`.
- Slot tracking is required for cluster migration; updating only the shard without `slotIndex` silently breaks `MIGRATE` enumeration.

## ANTI-PATTERNS
- Do not update one of `cache`/`objects`/`expires`/`slotIndex` without the others when key shape changes.
- Do not add background goroutines without explicit lifecycle ownership and `Close()` cleanup.
- Do not replace atomic counters in `Stats` with lock-protected updates on the hot path.
- Do not trade correctness for micro-optimizations without targeted tests or benchmarks.
- Do not mutate the `[]byte` returned by `unsafe.StringToBytes` or the `string` produced by `BytesToString`; the underlying memory is shared with an immutable string and mutation is undefined behavior.

## VERIFY
```bash
go test -v ./internal/engine/memory/...
go test -bench=. -benchmem ./internal/engine/memory/...
```
