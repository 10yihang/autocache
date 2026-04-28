# PROJECT KNOWLEDGE BASE

**Generated:** 2026-04-23 Asia/Shanghai
**Commit:** `8369728`
**Branch:** `main`
··
## OVERVIEW
AutoCache is a Go 1.24 Redis-compatible distributed cache with three executables: the RESP server (`cmd/server`), the Kubernetes operator (`cmd/operator`), and a clipboard demo app (`cmd/clipboard`).
Active implementation centers are the Redis protocol layer, in-memory engine, tiered orchestration, cluster control plane (gossip + replication + failover + migration), operator reconcile flow, and the standalone clipboard subsystem; when docs or `plan/` drift, prefer current source plus `Makefile` targets.

## STRUCTURE
```text
autocache/
|- cmd/                  # server, operator, clipboard entry points
|- internal/protocol/    # RESP command dispatch, routing, adapters
|- internal/engine/      # memory hot tier + tiered orchestration (memory, tiered, badger, s3)
|- internal/cluster/     # cluster (top-level), gossip, replication, failover, migration, router, slots, state, hash
|- internal/metrics/     # Prometheus exporter
|- api/v1alpha1/         # CRD types; generated deepcopy lives here
|- controllers/          # operator reconcile, scaling, migration, RESP helpers
|- clipboard/            # standalone demo app: Go backend (HTTP) + React/Vite frontend (embedded SPA)
|- pkg/                  # shared utilities: bytes, errors, protocolbuf
|- scripts/              # benchmark and local cluster workflows (uses same-slot hash tags)
|- test/integration/     # tagged integration coverage (-tags=integration)
|- tools/findkey/        # standalone slot-finder utility (intentional non-cmd main)
|- config/               # kubebuilder-generated CRD + RBAC manifests
|- deploy/               # docker, helm, monitoring, systemd
|- docs/, plan/          # academic/aspirational; not authoritative
`- .worktrees/           # sibling worktrees; ignore when mapping repo
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Run the cache server | `cmd/server/main.go` | Wires memory/tiered, cluster, metrics, replication, CLI mode |
| Run the operator | `cmd/operator/main.go` | controller-runtime manager, leader election, health probes |
| Run the clipboard demo | `cmd/clipboard/main.go` | HTTP server + Redis client (talks to AutoCache) + metrics |
| Change Redis command behavior | `internal/protocol/` | Handler dispatch, cmdmap, cluster routing, conn state, replication apply |
| Change hot-tier data structures | `internal/engine/memory/` | Sharded zero-GC cache, ring buffers, TinyLFU admission, Redis types, slot tracking |
| Change warm/cold tier movement | `internal/engine/tiered/` | Manager, WTinyLFUCostAware policy, migrator (rate-limited), stats |
| Change cluster membership/gossip | `internal/cluster/gossip/` | Ping/pong, node-state machine, slot epoch propagation |
| Change replication semantics | `internal/cluster/replication/` | LSN allocation, log, stream, applier, ACK tracker |
| Change failover/scaling/migration | `internal/cluster/{failover,migration,migrate}/`, `controllers/scaling.go` | Replica promotion, slot moves |
| Change operator rollout logic | `controllers/` | Reconcile loop, services, StatefulSet, scaling, migration, status |
| Change CRD surface | `api/v1alpha1/autocache_types.go` | Regenerate with `make generate` and `make manifests` |
| Change clipboard backend | `clipboard/backend/` | Paste service, handlers, middleware, embedded SPA assets |
| Run integration tests | `test/integration/` | Uses `-tags=integration`; covers cluster, migration, tiered |
| Run benchmarks or local cluster workflows | `scripts/` | Uses same-slot `{bench-*}` hash tags to avoid `MOVED` noise |

## CODE MAP
| Symbol | Type | Location | Role |
|--------|------|----------|------|
| `main` | function | `cmd/server/main.go` | Starts RESP server, cluster, metrics, optional tiered manager, CLI mode |
| `main` | function | `cmd/operator/main.go` | Starts controller-runtime manager and health probes |
| `main` | function | `cmd/clipboard/main.go` | Starts clipboard HTTP + metrics; connects to AutoCache backend |
| `Handler` | struct | `internal/protocol/handler.go` | Central RESP command dispatcher with cluster routing and replication apply hooks |
| `Server` | struct | `internal/protocol/server.go` | RESP server lifecycle and connection management |
| `Store` | struct | `internal/engine/memory/store.go` | In-memory store facade over shards, expiry, objects, eviction, slot tracking |
| `ShardedCache` | struct | `internal/engine/memory/shard.go` | 256-shard zero-GC cache with ring buffers and TinyLFU admission |
| `Manager` | struct | `internal/engine/tiered/manager.go` | Cross-tier read/write orchestration and migration lifecycle |
| `Cluster` | struct | `internal/cluster/cluster.go` | Cluster state machine; coordinates gossip, slots, replication |
| `SlotManager` | struct | `internal/cluster/slots.go` | 16384-slot ownership, state, replicas, LSN bookkeeping |
| `Gossip` | struct | `internal/cluster/gossip/gossip.go` | Membership, ping/pong, slot epoch exchange, failure detection |
| `replication.Manager` | struct | `internal/cluster/replication/manager.go` | Write log, LSN allocator, stream, ACK tracker |
| `AutoCacheReconciler` | struct | `controllers/autocache_controller.go` | Operator reconciliation, finalizer, cluster init, migration trigger |
| `ScaleManager` | struct | `controllers/scaling.go` | Scale-up/scale-down with CLUSTER MEET and slot rebalancing |
| `AutoCacheSpec` | struct | `api/v1alpha1/autocache_types.go` | CRD contract for operator-managed clusters |
| `PasteService` | struct | `clipboard/backend/service.go` | Clipboard paste lifecycle backed by Redis-compatible client |

## CONVENTIONS
- Use `goimports` with local prefix `github.com/10yihang/autocache`; `.golangci.yml` enables `gofmt`, `goimports`, `errcheck` (with `check-type-assertions`), `staticcheck`, `gosimple`, `ineffassign`, `unused`, `misspell`, `gocritic`, `revive`, `govet` (with `check-shadowing`).
- Prefer `make test`, `make lint`, `make generate`, and `make manifests` for normal verification; integration coverage lives behind `-tags=integration`.
- Package tests live next to source as `*_test.go`; full-system tests stay in `test/integration`; benchmark-oriented shell workflows stay in `scripts/`.
- Benchmark scripts intentionally use same-slot `{bench-*}` hash tags (see `scripts/lib/bench-common.sh`) to avoid `MOVED` noise during multi-key and cluster benchmarks.
- Operator/deployment paths assume `controller-gen`, `kubectl`, and kustomize-style layout under `config/` and `deploy/`.
- Code generation is entirely `controller-gen` driven; no `//go:generate` directives in the tree.

## ANTI-PATTERNS (THIS PROJECT)
- Do not edit generated files: `api/v1alpha1/zz_generated.deepcopy.go`, `config/crd/bases/*.yaml`, `config/rbac/{role,role_binding,service_account}.yaml`. Change source types or kubebuilder markers and run `make generate` / `make manifests`.
- Do not ignore errors, skip `%w` wrapping, or bypass verification before moving phases; `errcheck` with type-assertion checking is enforced.
- Do not treat `plan/` or `docs/` (thesis material) as authoritative when current source and `Makefile` disagree.
- Do not add new executable mains outside `cmd/` unless they are intentionally standalone like `tools/findkey`.
- Do not modify byte slices returned by `pkg/bytes` / `internal/engine/memory` unsafe converters; the original string remains aliased and mutation is undefined behavior.
- Do not create child `AGENTS.md` files for shared utility or workflow directories (`pkg/`, `scripts/`, `test/`, `tools/`, `deploy/`, `docs/`, `config/`); root coverage is intentional. The `clipboard/` subsystem is an exception because it is a first-class app.

## UNIQUE STYLES
- Cluster behavior follows Redis-compatible slot semantics: 16384 slots, `MOVED`/`ASK` redirects, `ASKING` flag, same-slot multi-key handling.
- The validated storage path is memory hot tier plus Badger warm tier; S3 cold tier exists in `internal/engine/s3` but is treated as experimental.
- Memory hot tier uses zero-GC sharding (`map[uint64]uint64` index + ring buffers + TinyLFU admission) to keep GC pressure independent of key count.
- Replication is LSN-based and slot-scoped; primary allocates `(epoch, lsn)` per slot, log/stream propagate, replicas ACK by `MatchLSN`.
- Operator code, server code, and clipboard demo are all first-class subsystems; no single domain is the "main" focus of the repo.
- Hot-path code prefers `pkg/protocolbuf` (sync.Pool, slab pool, args pool) and `pkg/bytes` zero-alloc converters over casual string churn.

## COMMANDS
```bash
make build               # cmd/server -> bin/autocache
make build-operator      # cmd/operator -> bin/autocache-operator
make build-clipboard     # builds frontend + cmd/clipboard -> bin/clipboard
make run                 # go run ./cmd/server
make run-clipboard       # frontend build + go run ./cmd/clipboard
make test                # all packages, race + cover
make test-unit           # ./internal/... only
make test-integration    # -tags=integration ./test/integration/...
make test-clipboard      # ./clipboard/backend/... ./cmd/clipboard/...
make test-benchmark      # go test -bench=. -benchmem ./...
make lint                # golangci-lint run ./...
make fmt                 # gofmt + goimports
make generate            # controller-gen object  -> zz_generated.deepcopy.go
make manifests           # controller-gen crd+rbac+webhook -> config/crd/bases, config/rbac
make install-crd         # kubectl apply -f config/crd/bases
make deploy              # make manifests + kubectl apply -k config/default
make kind-create         # kind cluster from scripts/kind-config.yaml
make kind-load           # kind load docker-image autocache:latest
make redis-benchmark     # redis-benchmark against localhost:6379
go test -v ./internal/protocol/...
go test -v ./internal/engine/memory/...
go test -v ./internal/engine/tiered/...
go test -v ./internal/cluster/...
go test -v ./controllers/...
bash scripts/bench-native.sh        # localhost AutoCache vs Redis
bash scripts/bench-cluster.sh       # 3-node cluster benchmark
bash scripts/local-cluster-setup.sh # local multi-node with kubectl
```

## NOTES
- Apply child `AGENTS.md` files when working in `internal/protocol/`, `internal/engine/memory/`, `internal/engine/tiered/`, `internal/cluster/` (and its `gossip/`, `replication/` subpackages), `controllers/`, or `clipboard/`.
- `internal/cluster/AGENTS.md` is the umbrella for `failover/`, `migration/`, `migrate/`, `router/`, `state/`, `hash/`, and the top-level `cluster.go`/`slots.go`/`node.go`; smaller subpackages do not get their own AGENTS.md.
- Ignore `.worktrees/` when mapping the active repository; they are sibling worktrees, not part of the current hierarchy.
- `scripts/`, `test/`, `pkg/`, `tools/`, `deploy/`, `docs/`, and `config/` are intentionally root-scoped because they are shared workflows, generated artifacts, or utilities rather than isolated correctness domains.
