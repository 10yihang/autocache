# clipboard Knowledge Base

Apply the root `AGENTS.md` first, then these clipboard-app rules. This subsystem is a first-class demo app, separate from the cache server / operator.

## OVERVIEW
Standalone HTTP paste-bin app that uses an AutoCache (or Redis) backend over the RESP wire. Go backend in `clipboard/backend/` exposes a JSON + raw-text + admin REST API and serves an embedded React/Vite SPA built from `clipboard/frontend/`.

## STRUCTURE
```text
clipboard/
|- API.md              # HTTP endpoint contract (read this when changing routes)
|- MIGRATION.md        # legacy → current layout map
|- backend/            # Go HTTP backend; `package clipboard`
|  |- service.go       # PasteService: shortcode allocation, TTL, burn-after-read, atomic stats
|  |- handler.go       # router + route table (registered in handler.go)
|  |- middleware.go    # rate limit + admin bearer auth
|  |- shortcode.go     # paste shortcode generation
|  |- validate.go      # input validation
|  |- types.go, metrics_types.go  # request/response and metrics types
|  |- prometheus.go    # Prometheus metrics exposition
|  |- static.go        # SPA shell and embedded asset serving
|  |- embed/           # frontend build output (overwritten by `make build-clipboard-frontend`)
|  `- *_test.go        # service / handler / middleware / shortcode coverage
`- frontend/           # React + TypeScript + Vite SPA; built into backend/embed/
```

## WHERE TO LOOK
- `clipboard/API.md` — authoritative HTTP contract (paths, auth, error formats); update it when routes change
- `backend/service.go` — `PasteService`, `PasteServiceOptions`, `ErrPasteNotFound`, atomic `serviceStats`
- `backend/handler.go` — route registration; mirrors `API.md`
- `backend/middleware.go` — rate limiting (configurable per-IP create / read windows) and admin bearer auth
- `backend/static.go` + `backend/embed/` — SPA shell delivery; `embed/` is overwritten by the build
- `cmd/clipboard/main.go` — wires `redis.UniversalClient` → `PasteService` → middleware → `http.Server` + metrics server
- `clipboard/MIGRATION.md` — legacy `internal/clipboard/*` → `clipboard/backend/*` move map; useful when reading old commits

## CONVENTIONS
- Backend Go files use `package clipboard` (the directory is `clipboard/backend` but the import path is `github.com/10yihang/autocache/clipboard/backend`).
- Persistence MUST go through the injected `redis.UniversalClient`; never reach into the cache engine packages directly. The app must run unchanged against AutoCache or vanilla Redis.
- All public state on `PasteService` is `atomic.Int64` counters; keep them lock-free.
- Request validation lives in `validate.go`; handlers should not duplicate it.
- Admin endpoints (`/admin/*`) require `Authorization: Bearer <admin-token>`; rate limits use a sliding window keyed by client IP.
- Frontend builds are committed to `backend/embed/` via `make build-clipboard-frontend`; do not hand-edit files inside `embed/`.
- When changing a route, update both `handler.go` AND `API.md`; tests assert on the documented shape.
- Error format duality is intentional (JSON for `/api/*`, plain text from middleware) — preserve it.

## ANTI-PATTERNS
- Do not import `internal/engine/...` or `internal/cluster/...`; the app is a Redis client, not a cache server.
- Do not commit changes to files inside `clipboard/backend/embed/` by hand; rebuild the frontend instead.
- Do not add a long-lived background goroutine without lifecycle ownership and a `Shutdown` path mirroring `cmd/clipboard/main.go`.
- Do not skip `make build-clipboard-frontend` before `make build-clipboard`; the binary will ship a stale SPA.
- Do not add new endpoints without updating `API.md` and adding handler tests.

## VERIFY
```bash
make build-clipboard-frontend         # rebuild SPA into backend/embed/
make test-clipboard                   # ./clipboard/backend/... ./cmd/clipboard/...
make build-clipboard                  # binary -> bin/clipboard
make run-clipboard                    # frontend rebuild + go run ./cmd/clipboard
```
