# internal/protocol Knowledge Base

Apply the root `AGENTS.md` first, then these protocol-specific rules.

## OVERVIEW
This package is the RESP edge: command lookup, cluster-aware routing, replication apply hooks, reply formatting, adapters, and protocol benchmarks all converge here.

## WHERE TO LOOK
- `internal/protocol/handler.go` - command registration, routing gates, write acceptance, command implementations
- `internal/protocol/cmdmap.go` - fast command lookup path for `[]byte` dispatch
- `internal/protocol/commands/cluster.go` - cluster subcommands and cluster-only behavior
- `internal/protocol/adapter.go` - bridge from RESP handlers to storage engines
- `internal/protocol/handler_*_test.go` - command-family coverage including primary/migrate cases
- `internal/protocol/crossslot_test.go` - slot-routing and multi-key safety expectations

## CONVENTIONS
- Register every new command in `registerCommands()` and keep `cmdMap` lookup behavior aligned with it.
- Preserve exact Redis-style behavior where already implemented: argument validation, error text, reply shape, and cross-slot rejection semantics matter.
- Use `pkg/bytes`, pooled protocol helpers, and existing byte-oriented utilities on hot paths instead of casual string conversion churn.
- When cluster mode is involved, verify both direct execution and routing behavior (`MOVED`, `ASK`, asking flag, multi-key slot checks, readonly write rejection).
- If a write path interacts with replication, keep `REPLAPPLY`/`WAIT` semantics and write-state bookkeeping consistent with command execution.

## ANTI-PATTERNS
- Do not add a handler without matching tests for normal execution and routing/slot edge cases.
- Do not bypass metrics recording or connection-state cleanup when returning early from `ExecuteBytes` or `Execute`.
- Do not mix protocol concerns with storage-engine internals when an adapter or engine interface already exists.
- Do not change command names, reply shapes, or error strings casually; client compatibility depends on them.

## VERIFY
```bash
go test -v ./internal/protocol/...
```
