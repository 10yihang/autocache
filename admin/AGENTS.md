# admin Knowledge Base

Apply the root `AGENTS.md` first, then these admin-app rules. This subsystem is a first-class built-in admin UI, running in the same process as the RESP server.

## OVERVIEW
Built-in node admin UI that provides a web dashboard for a single AutoCache node. Cluster information comes from the local `cluster.Cluster` instance; storage metrics come from the injected `memory.Store` and `tiered.Manager`. The admin server is OFF by default and must be explicitly enabled via `-admin-enabled`.

## STRUCTURE
```text
admin/
|- AGENTS.md           # this file
|- backend/            # Go HTTP backend; `package admin`
|  |- server.go        # Server lifecycle (Start / Shutdown)
|  |- handler.go       # route table, SPA fallback, stub handlers
|  |- middleware.go    # basic auth, panic recovery, request logging, body limit
|  |- config.go        # Config struct and defaults
|  |- deps.go          # Deps struct (injected Store, Cluster, Tiered, Replication, Handler)
|  |- audit.go         # thread-safe ring buffer audit log
|  |- static.go        # embed.FS accessor for SPA assets
|  |- embed/           # frontend build output (overwritten by `make build-admin-frontend`)
|  `- *_test.go        # unit test coverage
`- frontend/           # (Wave 3) React + TypeScript + Vite SPA; built into backend/embed/
```

## WHERE TO LOOK
- `backend/server.go` — `Server`, `New()`, `Start()`, `Shutdown()`
- `backend/handler.go` — route registration, SPA fallback, stub API endpoints
- `backend/middleware.go` — auth, panic recovery, logging, body limits
- `backend/deps.go` — `Deps` struct injected from `cmd/server/main.go`
- `backend/audit.go` — `AuditLog` ring buffer for write operation tracking
- `backend/config.go` — `Config` with `applyDefaults()`
- `backend/static.go` + `backend/embed/` — embedded SPA assets

## CONVENTIONS
- Backend Go files use `package admin` (the directory is `admin/backend` but the import path is `github.com/10yihang/autocache/admin/backend`).
- The admin server runs in the SAME process as the RESP server; it shares live `Store`, `Cluster`, `Tiered`, and `Replication` instances via dependency injection through the `Deps` struct.
- Read-only by default. Write/mutate endpoints require the `-admin-allow-dangerous` flag.
- Default bind address is `127.0.0.1` (localhost only). Production users must explicitly set `-admin-addr` to expose externally.
- All write operations MUST be recorded in the `AuditLog` before execution.
- Dangerous commands (FLUSHALL, FLUSHDB, CONFIG SET, CLUSTER RESET, etc.) are rejected unless `-admin-allow-dangerous` is set.
- Frontend builds are committed to `backend/embed/` via `make build-admin-frontend`; do not hand-edit files inside `embed/`.
- Error responses use JSON format: `{"error":{"code":"...","message":"..."}}`.
- Authentication uses HTTP Basic Auth. Password may be plaintext or bcrypt-hashed (detected by `$2a$`/`$2b$`/`$2y$` prefix).

## SECURITY CAVEATS
- Default bind `127.0.0.1:8080` is the security boundary; external exposure requires the operator to explicitly set `-admin-addr`.
- Basic Auth username and plaintext password are SHA-256 hashed before constant-time comparison; this prevents length leakage via timing.
- Bcrypt-hashed passwords are detected by prefix and verified via `bcrypt.CompareHashAndPassword`. The configured storage mode (bcrypt vs plaintext) is distinguishable from the outside via response timing differences; this is acceptable for a built-in admin UI but worth knowing.
- WriteTimeout is 30s by default; SSE handlers added in later waves MUST disable per-connection write deadlines via `http.ResponseController.SetWriteDeadline(time.Time{})`.

## ANTI-PATTERNS
- Do not bypass `protocol.Handler` to call internal cluster mutators directly; all command execution should flow through the protocol layer.
- Do not expose write endpoints without recording to the audit log first.
- Do not add a separate `cmd/admin` entrypoint; the admin server is same-process with the RESP server.
- Do not commit changes to files inside `admin/backend/embed/` by hand; rebuild the frontend instead.
- Do not import `internal/engine/...` types beyond the injected `Deps` pointers; the admin backend should not reach into engine internals.
- Do not add new API endpoints without adding corresponding handler tests.

## VERIFY
```bash
go test -race -v ./admin/backend/...
go vet ./admin/...
go build ./...
```
