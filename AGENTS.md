# PROJECT KNOWLEDGE BASE

**Generated:** 2026-03-22 Asia/Shanghai
**Commit:** `2565c76`
**Branch:** `main`

## OVERVIEW
AutoCache is a Go 1.24 Redis-compatible distributed cache with two main executables: the RESP server in `cmd/server` and the Kubernetes operator in `cmd/operator`.
The active implementation center is the Redis protocol layer, in-memory engine, tiered orchestration, cluster control plane, and operator reconcile flow; when docs or plans drift, prefer current source plus `Makefile` targets.

## STRUCTURE
```text
autocache/
|- cmd/                  # server and operator entry points
|- internal/protocol/    # RESP command dispatch, routing, adapters
|- internal/engine/      # hot-tier store plus tiered orchestration
|- internal/cluster/     # slots, gossip, failover, replication, migration
|- api/v1alpha1/         # CRD types; generated deepcopy lives here
|- controllers/          # operator reconcile and migration logic
|- scripts/              # benchmark and local cluster workflows
|- test/integration/     # tagged integration coverage
|- pkg/                  # shared utility packages used across subsystems
`- tools/findkey/        # standalone utility main outside cmd/
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Run the cache server | `cmd/server/main.go` | Wires memory, cluster, metrics, tiered mode, and CLI mode |
| Run the operator | `cmd/operator/main.go` | Kubebuilder/controller-runtime manager bootstrap |
| Change Redis command behavior | `internal/protocol/` | Handler registration, routing, RESP replies, cluster redirects |
| Change hot-tier data structures | `internal/engine/memory/` | Shards, expiry, eviction, slot indexing, Redis-like types |
| Change warm/cold tier movement | `internal/engine/tiered/` | Migration policy, promotion/demotion, stats, lifecycle |
| Change cluster membership or slot gossip | `internal/cluster/gossip/` | Node exchange, failure detection, slot ownership propagation |
| Change operator rollout logic | `controllers/` | Reconcile loop, services, StatefulSet, migration, status |
| Change CRD surface | `api/v1alpha1/autocache_types.go` | Regenerate with `make generate` and `make manifests` |
| Run integration tests | `test/integration/` | Uses `-tags=integration` |
| Run benchmarks or local cluster workflows | `scripts/` | Assumes `kubectl`, `redis-benchmark`, and cluster slot semantics |

## CODE MAP
| Symbol | Type | Location | Role |
|--------|------|----------|------|
| `main` | function | `cmd/server/main.go` | Starts RESP server, cluster services, metrics, and optional tiered manager |
| `main` | function | `cmd/operator/main.go` | Starts controller manager and health probes |
| `Handler` | struct | `internal/protocol/handler.go` | Central RESP command dispatcher with cluster routing and replication apply hooks |
| `Store` | struct | `internal/engine/memory/store.go` | In-memory store facade over shards, expiry, objects, eviction, and slot tracking |
| `Manager` | struct | `internal/engine/tiered/manager.go` | Cross-tier read/write orchestration and migration lifecycle |
| `Gossip` | struct | `internal/cluster/gossip/gossip.go` | Cluster membership, ping/pong, slot epoch exchange, and failure detection |
| `AutoCacheReconciler` | struct | `controllers/autocache_controller.go` | Operator reconciliation, resource orchestration, cluster init, and migration trigger |
| `AutoCacheSpec` | struct | `api/v1alpha1/autocache_types.go` | CRD contract for operator-managed clusters |

## CONVENTIONS
- Use `goimports` with local prefix `github.com/10yihang/autocache`; `.golangci.yml` enables `errcheck`, `staticcheck`, `revive`, shadow checks, and stricter vet/type-assertion rules.
- Prefer `make test`, `make lint`, `make generate`, and `make manifests` for normal verification; integration coverage lives behind explicit build tags.
- Package tests live next to source as `*_test.go`; full-system tests stay in `test/integration`, while benchmark-oriented shell workflows stay in `scripts/`.
- Benchmark scripts intentionally use same-slot hash tags to avoid `MOVED` noise during multi-key and cluster benchmarks.
- Operator/deployment paths assume `controller-gen`, `kubectl`, and kustomize-style config layout under `config/` and `deploy/`.

## ANTI-PATTERNS (THIS PROJECT)
- Do not edit generated files such as `api/v1alpha1/zz_generated.deepcopy.go`; change source types and regenerate.
- Do not ignore errors, skip `%w` wrapping, or bypass verification before moving phases.
- Do not treat `plan/` or thesis/documentation material as authoritative when current source and `Makefile` disagree.
- Do not add new executable mains outside `cmd/` unless the tool is intentionally standalone like `tools/findkey`.
- Do not create child `AGENTS.md` files for shared utility or workflow directories unless they gain truly local rules; root coverage is intentional for `pkg/`, `scripts/`, `test/`, `tools/`, `deploy/`, and `docs/`.

## UNIQUE STYLES
- Cluster behavior follows Redis-compatible slot semantics: 16384 slots, `MOVED`/`ASK` redirects, `ASKING`, and same-slot handling for multi-key operations.
- The validated storage path is memory hot tier plus Badger warm tier; S3/cold-tier support exists but is not the primary benchmark/demo path.
- Operator code and server code are both first-class: the repo is not operator-only, so root guidance must serve runtime, storage, cluster, and Kubernetes paths together.

## COMMANDS
```bash
make build
make build-operator
make run
make test
make test-unit
make test-integration
make test-benchmark
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
go test -v ./internal/cluster/...
go test -v ./controllers/...
```

## NOTES
- Apply child `AGENTS.md` files when working in `internal/protocol/`, `internal/engine/memory/`, `internal/engine/tiered/`, `internal/cluster/gossip/`, or `controllers/`.
- Ignore `.worktrees/` when mapping the active repository; they are sibling worktrees, not part of the current hierarchy.
- `scripts/`, `test/`, `pkg/`, `tools/`, `deploy/`, and `docs/` are intentionally root-scoped because they are shared workflows or utilities, not isolated correctness domains.
