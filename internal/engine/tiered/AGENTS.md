# internal/engine/tiered Knowledge Base

Apply the root `AGENTS.md` first, then these tiered-storage rules.

## OVERVIEW
This package orchestrates promotion, demotion, and lifecycle management across hot memory, warm Badger, and optional cold storage tiers.

## WHERE TO LOOK
- `internal/engine/tiered/manager.go` - cross-tier read/write orchestration and lifecycle
- `internal/engine/tiered/policy.go` - promotion/demotion policy logic
- `internal/engine/tiered/migrator.go` - background migration loop and transfer flow
- `internal/engine/tiered/stats.go` - access recording and tiered statistics
- `internal/engine/tiered/*_test.go` - manager internals, policy, and tiered behavior coverage

## CONVENTIONS
- Background work must be owned by `context`, `cancel`, and `WaitGroup`; `Start()`/`Stop()` semantics are part of the API contract.
- Reads and writes should keep policy signals, stats collection, and migration counters in sync with actual tier movement.
- Warm-tier and cold-tier initialization failures must fail fast with wrapped context (`%w`) so startup errors stay actionable.
- Treat S3/cold-tier support as optional codepath; the commonly validated path is memory plus Badger.

## ANTI-PATTERNS
- Do not launch goroutines without lifecycle tracking in `Manager` or `Migrator`.
- Do not promote/demote entries without updating the policy/stats hooks that explain why the movement happened.
- Do not couple tiered logic directly to protocol behavior; stay behind engine-facing interfaces.
- Do not assume cold tier is always enabled in tests or production flows.

## VERIFY
```bash
go test -v ./internal/engine/tiered/...
```
