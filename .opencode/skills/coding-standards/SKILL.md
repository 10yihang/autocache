---
name: coding-standards
description: Go coding standards for the AutoCache project (github.com/10yihang/autocache). Use when writing or modifying Go code to stay consistent on project structure, formatting (gofmt/goimports), naming, error handling, concurrency lifecycle, API design, testing, logging, configuration validation, linting, performance, and security.
compatibility: Designed for Claude Code / OpenCode-style agents working in a Go repository.
metadata:
  author: 10yihang
  version: "1.0"
---

# AutoCache Coding Standards (Go)

Use this skill whenever you write or change Go code in `github.com/10yihang/autocache`.

## Prime directive
- Prefer **small, focused changes**.
- **Never ignore errors**.
- **No wild goroutines**: every goroutine must have a clear lifecycle.
- Keep code **gofmt/goimports-clean**.

## 1) Project structure
- `cmd/`: binaries entrypoints (wiring only; no business logic).
- `internal/`: core business logic (private).
- `pkg/`: reusable utilities (public).
- `api/`: K8s CRD types.
- `controllers/`: operator controllers.
- `config/`, `deploy/`, `test/` as usual.

### Naming
- Files: `lowercase.go`, tests: `*_test.go`, benchmarks: `*_bench_test.go`.
- Packages: **short, specific, lowercase**, no underscores/camelCase.
  - Good: `memory`, `cluster`, `tiered`
  - Bad: `util`, `common`

## 2) Formatting & imports (mandatory)
Run these before committing / sending a patch:

```bash
gofmt -w .
goimports -w .
```

Import grouping (prefer goimports). `local-prefixes` should be:

```yaml
goimports:
  local-prefixes: github.com/10yihang/autocache
```

## 3) Naming conventions (Go)
- Variables: short but meaningful (`buf`, `ctx`, `mu`, `wg`, `conn`).
- Constants: `MaxRetries`, `DefaultTimeout`; iota blocks for enums.
- Functions: Verb-first, `CamelCase`. Bool returns: `Is/Has/Can`.
- Interfaces: usually `-er` suffix; keep interfaces **small**.

## 4) Error handling (non-negotiable)
### Define sentinel errors

```go
var ErrNotFound = errors.New("key not found")
```

### Check immediately and wrap with context

```go
res, err := doSomething()
if err != nil {
    return fmt.Errorf("do something: %w", err)
}
```

### Use `errors.Is/As`

```go
if errors.Is(err, ErrNotFound) { /* ... */ }

var ce *ClusterError
if errors.As(err, &ce) { /* ... */ }
```

### Forbidden
- `result, _ := ...`
- swallowing errors
- returning unwrapped low-context errors when you can add context

## 5) Concurrency & lifecycle
### Goroutines must be owned and stoppable

```go
func (s *Server) Start() {
    s.wg.Add(1)
    go func() {
        defer s.wg.Done()
        s.acceptLoop()
    }()
}

func (s *Server) Stop() {
    close(s.stopCh)
    s.wg.Wait()
}
```

### Channels
- Specify direction in signatures:

```go
func worker(jobs <-chan Job, results chan<- Result) {}
```

- Use `select` for timeout/cancel:

```go
select {
case v := <-ch:
    _ = v
case <-time.After(5 * time.Second):
    return ErrTimeout
case <-ctx.Done():
    return ctx.Err()
}
```

### Locks
- Always `defer Unlock()` / `defer RUnlock()`.
- Keep lock scope tight; do slow work outside locks.

## 6) API design
### `context.Context`
- `ctx` is the **first parameter**.
- Use timeouts where appropriate.

```go
func (s *Store) Get(ctx context.Context, key string) (string, error) {}

ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()
```

### Options pattern (for complex constructors)

```go
type Option func(*options)

func WithMaxMemory(size int64) Option {
    return func(o *options) { o.maxMemory = size }
}

func NewStore(opts ...Option) *Store {
    o := defaultOptions()
    for _, opt := range opts { opt(&o) }
    return &Store{/* ... */}
}
```

## 7) Testing
### Unit tests
- Prefer **table-driven tests**.
- Structure: Arrange / Act / Assert.

```go
func TestKeySlot(t *testing.T) {
    tests := []struct {
        name string
        key  string
        want uint16
    }{
        {"simple", "foo", 12182},
        {"hashtag", "{user:123}.name", 11535},
        {"empty", "", 0},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := KeySlot(tt.key); got != tt.want {
                t.Errorf("KeySlot(%q)=%v, want %v", tt.key, got, tt.want)
            }
        })
    }
}
```

### Benchmarks
- Use `b.RunParallel` for concurrent hot paths.

```go
func BenchmarkStore_Get(b *testing.B) {
    store := NewStore()
    store.Set("key", "value")
    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            store.Get("key")
        }
    })
}
```

## 8) Comments
- Package comment for exported packages.
- Exported types/functions: comment starts with the symbol name.
- TODO format:
  - `// TODO: ...`
  - `// TODO(username): ...`
  - `// FIXME: ...`

## 9) Logging
- Use **structured logging** (zap).
- Log levels: `Debug/Info/Warn/Error/Fatal`.
- **Never log secrets**; redact sensitive fields.

```go
logger.Info("client connected",
    zap.String("remote_addr", conn.RemoteAddr().String()),
)

logger.Info("auth attempt",
    zap.String("user", username),
    zap.String("password", "***"),
)
```

## 10) Configuration
- Use structured config types.
- Provide `Validate() error` and enforce invariants early.

```go
func (c *Config) Validate() error {
    if c.Server.Addr == "" {
        return errors.New("server.addr is required")
    }
    if c.Server.MaxConnections <= 0 {
        return errors.New("server.max_connections must be positive")
    }
    return nil
}
```

## 11) Linting baseline (.golangci.yml)
Recommended linters to enable:
- `gofmt`, `goimports`, `govet`, `errcheck`, `staticcheck`, `gosimple`, `ineffassign`, `unused`, `misspell`, `gocritic`, `revive`

Notes:
- `errcheck.check-type-assertions: true`
- `govet.check-shadowing: true`
- Exclude some linters for `_test.go` when appropriate (e.g., `errcheck`).

## 12) Performance
- Preallocate slices when size known.
- Use `sync.Pool` for reusable buffers.
- Avoid copying large structs; prefer pointers.

## 13) Security
- Validate all external input (key/value sizes, etc.).
- Apply resource limits (max connections, semaphores).

```go
select {
case s.connSem <- struct{}{}:
default:
    conn.Close()
    return ErrTooManyConnections
}
```

## Git commit message format

```
<type>(<scope>): <subject>

<body>

<footer>
```

Types: `feat|fix|docs|style|refactor|perf|test|chore`.
