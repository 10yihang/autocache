# internal/protocol Knowledge Base

Apply the root `AGENTS.md` first, then these protocol-specific rules.

## OVERVIEW
RESP edge: command lookup, cluster-aware routing, replication apply hooks, reply formatting, adapters, and protocol benchmarks all converge here. The package owns both the wire surface (`Server` + redcon) and the dispatch surface (`Handler` + `cmdMap`).

## WHERE TO LOOK
- `handler.go` — `Handler`, `registerCommands()`, `ExecuteBytes()` / `Execute()`, write-acceptance gate, replication apply hook
- `server.go` — `Server` lifecycle, `Client` per-connection state, redcon glue
- `cmdmap.go` — fast `[]byte` dispatch table for hot-path command lookup
- `connstate.go` — per-connection ASKING flag, DB selection, write tracking
- `crossslot.go` — multi-key slot validation and `CROSSSLOT` rejection
- `responses.go` — pooled RESP reply formatting, integer caching
- `adapter.go` — `MemoryStoreAdapter` and `TieredStoreAdapter` bridge to engines
- `commands/cluster.go` — `CLUSTER` subcommands and cluster-only behavior
- `handler_*_test.go`, `crossslot_test.go` — coverage for command families, primary/migrate cases, slot-routing edge cases

## CONVENTIONS
- Register every new command in `registerCommands()` and keep `cmdMap` aligned; lookup is `[]byte`-keyed and case-folded.
- Preserve exact Redis-style behavior: argument validation, error text (`WRONGTYPE`, `MOVED`, `ASK`, `CROSSSLOT`, `READONLY`), reply shape, and cross-slot rejection semantics matter for client compatibility.
- Use `pkg/bytes` and `pkg/protocolbuf` (sync.Pool, slab pool, args pool) on hot paths instead of `string()` / `[]byte()` churn.
- For cluster mode, verify both direct execution and routing behavior: `MOVED`, `ASK`, `ASKING` flag handling, multi-key slot checks, readonly write rejection on replicas.
- Write paths must keep `REPLAPPLY` / `WAIT` semantics consistent with `Handler` write tracking and `replication.Manager.ApplyWrite()`.
- Connection cleanup belongs in the redcon close hook; do not leak `ConnState` entries.

## ANTI-PATTERNS
- Do not add a handler without tests for normal execution AND routing/slot edge cases.
- Do not bypass metrics recording or `ConnState` cleanup when returning early from `ExecuteBytes` / `Execute`.
- Do not mix protocol concerns with engine internals; go through the adapter or the engine interface.
- Do not change command names, reply shapes, or error strings casually; client compatibility depends on them.
- Do not allocate fresh `[]byte` for every reply when a pooled or static response in `responses.go` already exists.

## VERIFY
```bash
go test -v ./internal/protocol/...
```
