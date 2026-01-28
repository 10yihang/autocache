# AutoCache - Agent Guidelines

Cloud-native distributed cache system in Go, Redis RESP compatible.

## Build & Test Commands

```bash
# Build
go build -o bin/autocache ./cmd/server
go build -o bin/autocache-operator ./cmd/operator

# Run
go run ./cmd/server

# Test all
go test -v -race ./...

# Test single package
go test -v ./internal/engine/memory/...

# Test single function
go test -v -run TestStore_Get ./internal/engine/memory/

# Test with coverage
go test -v -race -cover ./...

# Benchmark
go test -bench=. -benchmem ./internal/engine/memory/

# Lint
golangci-lint run ./...

# Format
gofmt -w .
goimports -w .

# Generate (CRD, DeepCopy)
make generate
make manifests

# Docker
docker build -t autocache:latest -f deploy/docker/Dockerfile .

# Redis benchmark (after server running)
redis-benchmark -p 6379 -n 100000 -c 50 -t set,get -q
```

## Project Structure

```
cmd/           # Entry points (server, operator)
internal/      # Private packages (engine, protocol, cluster)
pkg/           # Public utilities (log, errors)
api/           # K8s CRD types (v1alpha1)
controllers/   # Operator controllers
config/        # K8s manifests (crd, rbac)
deploy/        # Docker, Helm
test/          # Integration, e2e, benchmark
```

## Code Style

### Imports (use goimports)
```go
import (
    // stdlib
    "context"
    "fmt"

    // external
    "github.com/tidwall/redcon"

    // internal
    "github.com/10yihang/autocache/internal/engine"
)
```

### Naming
```go
// Variables: short, meaningful
var buf bytes.Buffer
var mu sync.Mutex
var wg sync.WaitGroup

// Constants: CamelCase or UPPER_SNAKE for iota
const MaxRetries = 3
const (
    StatusPending = iota
    StatusRunning
)

// Functions: verb prefix, CamelCase
func GetUser(id string) *User {}
func IsValid() bool {}           // bool -> Is/Has/Can prefix

// Interfaces: -er suffix typically
type Engine interface {}
type Reader interface {}

// Files: lowercase, underscore for tests
store.go
store_test.go
```

### Error Handling
```go
// Define errors
var ErrNotFound = errors.New("key not found")

// Always check errors immediately
result, err := doSomething()
if err != nil {
    return fmt.Errorf("do something: %w", err)  // wrap with %w
}

// Use errors.Is/As for checking
if errors.Is(err, ErrNotFound) { ... }

// NEVER ignore errors
result, _ := doSomething()  // BAD
```

### Concurrency
```go
// Goroutines: track lifecycle with WaitGroup
func (s *Server) Start() {
    s.wg.Add(1)
    go func() {
        defer s.wg.Done()
        s.loop()
    }()
}

// Mutex: use defer for unlock
func (s *Store) Get(key string) interface{} {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.data[key]
}

// Context: always first parameter
func Get(ctx context.Context, key string) (string, error)
```

### Testing
```go
// Table-driven tests
func TestKeySlot(t *testing.T) {
    tests := []struct {
        name string
        key  string
        want uint16
    }{
        {"simple", "foo", 12182},
        {"hashtag", "{user}.name", 5474},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := KeySlot(tt.key); got != tt.want {
                t.Errorf("KeySlot(%q) = %v, want %v", tt.key, got, tt.want)
            }
        })
    }
}
```

## Key Dependencies

| Package | Purpose |
|---------|---------|
| `tidwall/redcon` | RESP protocol server |
| `dgraph-io/badger/v4` | SSD storage (warm tier) |
| `prometheus/client_golang` | Metrics |
| `sigs.k8s.io/controller-runtime` | K8s operator |
| `go.uber.org/zap` | Structured logging |

## Architecture Notes

- **16384 slots** for sharding (Redis Cluster compatible)
- **Gossip protocol** for cluster membership
- **Tiered storage**: Memory (hot) → SSD (warm) → S3 (cold)
- **MOVED/ASK** redirects for cluster routing

## Phase Completion Requirements

**MANDATORY**: Each phase MUST pass all tests before proceeding to the next phase.

1. **Unit Tests**: Run `go test -v -race ./...` - all tests must pass
2. **Build Verification**: Run `go build ./...` - must compile without errors
3. **Integration Test**: Start server and verify with `redis-cli`
4. **Functional Test**: Test new features manually or with scripts
5. **Regression Test**: Ensure existing features still work (`redis-benchmark`)

**DO NOT proceed to next phase until ALL tests pass.**

## Don'ts

- No `as any`, `@ts-ignore` equivalents (no type bypassing)
- No empty catch blocks / ignored errors
- No wild goroutines without lifecycle management
- No commits unless explicitly requested
- No proceeding to next phase without passing tests
