# PROJECT KNOWLEDGE BASE

**Generated:** 2026-03-22 Asia/Shanghai
**Commit:** `66f61de`
**Branch:** `main`

## OVERVIEW
AutoCache is a Go 1.24 Redis-compatible distributed cache with two executable entry points: the RESP server in `cmd/server` and the Kubernetes operator in `cmd/operator`.
The active implementation centers on the protocol layer, the in-memory engine, tiered storage orchestration, and cluster gossip; use `Makefile` and current source files as truth when docs or plans drift.

## STRUCTURE
```text
autocache/
|- cmd/                  # server and operator entry points
|- internal/protocol/    # RESP command dispatch, routing, adapters
|- internal/engine/      # hot-tier store plus tiered orchestration
|- internal/cluster/     # slot ownership, gossip, migration
|- api/v1alpha1/         # CRD types; generated deepcopy lives here
|- controllers/          # operator reconcile and migration logic
|- scripts/              # benchmark and local cluster workflows
|- test/integration/     # tagged integration coverage
`- tools/findkey/        # standalone utility main outside cmd/
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Run the cache server | `cmd/server/main.go` | Flags wire cluster, tiered, metrics, and CLI modes |
| Run the operator | `cmd/operator/main.go` | Kubebuilder/controller-runtime manager bootstrap |
| Add or change Redis behavior | `internal/protocol/` | Command registration, routing, RESP replies, adapters |
| Change hot-tier data behavior | `internal/engine/memory/` | Shards, expiry, eviction, Redis-like types |
| Change warm/cold movement | `internal/engine/tiered/` | Policy, migrator, stats, promotion/demotion |
| Change membership or failure detection | `internal/cluster/gossip/` | Node state machine and gossip transport |
| Change CRD surface | `api/v1alpha1/autocache_types.go` | Run `make generate` and `make manifests` after edits |
| Change operator rollout logic | `controllers/` | Reconcile loop, services, StatefulSet, migration |
| Run integration coverage | `test/integration/` | Uses `-tags=integration` |
| Benchmark or local cluster automation | `scripts/` | Many scripts assume `kubectl`, `redis-benchmark`, or kind |

## CODE MAP
| Symbol | Type | Location | Role |
|--------|------|----------|------|
| `main` | function | `cmd/server/main.go` | Starts RESP server, optional tiered storage, cluster, metrics |
| `main` | function | `cmd/operator/main.go` | Starts controller manager and reconciler |
| `Handler` | struct | `internal/protocol/handler.go` | Central RESP command dispatcher and cluster-aware router |
| `Store` | struct | `internal/engine/memory/store.go` | Hot-tier storage facade over shards, expiry, objects, eviction |
| `Manager` | struct | `internal/engine/tiered/manager.go` | Cross-tier read/write orchestration and migration lifecycle |
| `Gossip` | struct | `internal/cluster/gossip/gossip.go` | Membership, ping/pong, failure detection, node exchange |
| `AutoCacheReconciler` | struct | `controllers/autocache_controller.go` | Operator reconciliation, service/statefulset/status flow |
| `AutoCacheSpec` | struct | `api/v1alpha1/autocache_types.go` | CRD contract for operator-managed clusters |

## CONVENTIONS
- Use `goimports` ordering with local prefix `github.com/10yihang/autocache`; `.golangci.yml` enables `errcheck`, `staticcheck`, `revive`, and shadow checks.
- Prefer `make test`, `make lint`, `make generate`, and `make manifests` when touching operator or API surfaces; integration coverage lives behind explicit build tags.
- Package tests stay close to source as `*_test.go`; integration tests live in `test/integration`, and protocol/tiered packages also carry benchmark coverage.
- `scripts/bench-*.sh` assume Redis cluster slot rules; same-slot hash tags are deliberate, not incidental.

## ANTI-PATTERNS (THIS PROJECT)
- Do not edit generated files such as `api/v1alpha1/zz_generated.deepcopy.go`.
- Do not ignore errors, skip `%w` wrapping, or bypass tests before moving phases.
- Do not treat `plan/` or thesis-style `docs/` content as authoritative when current source, `Makefile`, and tests disagree.
- Do not add new executable mains outside `cmd/` unless the utility is intentionally standalone like `tools/findkey`.

## UNIQUE STYLES
- Cluster behavior follows Redis-compatible slot semantics: 16384 slots, `MOVED`/`ASK` redirects, and same-slot handling for multi-key operations.
- The validated storage path is memory hot tier plus Badger warm tier; S3/cold-tier code exists but is not the primary benchmark/demo path.
- Operator logic is controller-runtime based, but server-side runtime and benchmark tooling are equally first-class in this repo.

## COMMANDS
```bash
make build
make build-operator
make test
make test-integration
make lint
make fmt
make generate
make manifests
make kind-create
make kind-load
make redis-benchmark
go test -v ./internal/protocol/...
go test -v ./internal/engine/memory/...
go test -v ./internal/engine/tiered/...
go test -v ./controllers/...
```

## NOTES
- Apply child `AGENTS.md` files when working in `internal/protocol/`, `internal/engine/memory/`, `internal/engine/tiered/`, or `controllers/`.
- Ignore `.worktrees/` when mapping the active repository; they are sibling worktrees, not part of the current hierarchy.
- `test/integration/` and `scripts/` are workflow-heavy, but they are better referenced from root than given duplicated child instructions right now.
