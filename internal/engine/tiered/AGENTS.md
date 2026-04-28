# internal/engine/tiered Knowledge Base

Apply the root `AGENTS.md` first, then these tiered-storage rules.

## OVERVIEW
Cross-tier orchestration of hot memory, warm Badger (`internal/engine/badger`), and optional cold S3 (`internal/engine/s3`). Owns the migration loop, promotion/demotion policies, and per-key access stats.

## WHERE TO LOOK
- `manager.go` — `Manager`, `Config`, `Get/Set/Delete`, `Start/Stop` lifecycle, hot-miss → warm fallback path
- `policy.go` — `TierPolicy` interface; `WTinyLFUCostAwarePolicy` (primary, considers size + recency + frequency), `TinyLFUPolicy`, `DefaultPolicy` (no-op fallback)
- `migrator.go` — background `Migrator` ticking on `MigrationInterval`; rate-limited transfers (default ~10 MB/s)
- `stats.go` — `StatsCollector` and `AccessStats`: per-key access count, last-access time, size, write count
- `*_test.go` — manager internals, policy decisions, migration behavior

## CONVENTIONS
- Background work belongs to `context` + `cancel` + `WaitGroup`; `Start()` / `Stop()` are part of the API contract — callers MUST call `Stop()` before discarding a `Manager`.
- Reads and writes must keep policy signals (`StatsCollector.RecordAccess`) and migration counters in sync with actual tier movement; if you skip the hook, the policy goes blind.
- Warm and cold initialization failures must wrap context with `%w` so startup errors stay actionable in `cmd/server`.
- Validated path is memory + Badger; treat S3/cold-tier as an experimental optional codepath and gate it behind capability checks rather than assuming presence.
- Cross-tier helpers MUST preserve value bytes AND TTL when promoting or demoting; lossy demotion silently violates Redis semantics.
- Pick `WTinyLFUCostAwarePolicy` as the default when in doubt; the simpler `TinyLFUPolicy` and `DefaultPolicy` exist mainly for testing and degraded fallbacks.

## ANTI-PATTERNS
- Do not launch goroutines without lifecycle tracking on `Manager` or `Migrator`.
- Do not promote/demote entries without updating the policy/stats hooks that explain why the movement happened.
- Do not couple tiered logic to protocol behavior; stay behind engine-facing interfaces — `protocol/adapter.go` is the only seam.
- Do not assume the cold tier is enabled in tests or production flows.
- Do not bypass the migrator's rate limit in tests with bursty migration calls; integration tests rely on backpressure.

## VERIFY
```bash
go test -v ./internal/engine/tiered/...
go test -v -tags=integration ./test/integration/tiered_test.go
```
