package tieredbenchmark

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/engine"
	"github.com/10yihang/autocache/internal/engine/badger"
	"github.com/10yihang/autocache/internal/engine/nokv"
)

type backendFactory func(path string) (engine.Engine, error)

var benchmarkBackendFactories = map[string]backendFactory{
	"badger": func(path string) (engine.Engine, error) {
		return badger.NewStore(path)
	},
	"nokv": func(path string) (engine.Engine, error) {
		return nokv.NewStore(path)
	},
}

func BenchmarkWarmBackendGetParallel(b *testing.B) {
	ctx := context.Background()
	value := strings.Repeat("v", 1024)

	for name, factory := range benchmarkBackendFactories {
		b.Run(name, func(b *testing.B) {
			store, cleanup := createBenchmarkStore(b, factory)
			defer cleanup()

			if err := store.Set(ctx, "bench-key", value, 0); err != nil {
				b.Fatalf("seed key: %v", err)
			}

			b.ResetTimer()
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if _, err := store.Get(ctx, "bench-key"); err != nil {
						b.Fatalf("get key: %v", err)
					}
				}
			})
		})
	}
}

func BenchmarkWarmBackendSetParallel(b *testing.B) {
	ctx := context.Background()
	value := strings.Repeat("v", 1024)

	for name, factory := range benchmarkBackendFactories {
		b.Run(name, func(b *testing.B) {
			store, cleanup := createBenchmarkStore(b, factory)
			defer cleanup()

			var counter uint64

			b.ResetTimer()
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					id := atomic.AddUint64(&counter, 1)
					key := fmt.Sprintf("bench-key-%d", id)
					if err := store.Set(ctx, key, value, 0); err != nil {
						b.Fatalf("set key: %v", err)
					}
				}
			})
		})
	}
}

func BenchmarkWarmBackendMixedParallel(b *testing.B) {
	ctx := context.Background()
	value := strings.Repeat("v", 1024)

	for name, factory := range benchmarkBackendFactories {
		b.Run(name, func(b *testing.B) {
			store, cleanup := createBenchmarkStore(b, factory)
			defer cleanup()

			for i := 0; i < 1024; i++ {
				key := fmt.Sprintf("seed-key-%d", i)
				if err := store.Set(ctx, key, value, 0); err != nil {
					b.Fatalf("seed key: %v", err)
				}
			}

			var counter uint64

			b.ResetTimer()
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					id := atomic.AddUint64(&counter, 1)
					if id%20 == 0 {
						key := fmt.Sprintf("mixed-write-%d", id)
						if err := store.Set(ctx, key, value, 0); err != nil {
							b.Fatalf("mixed set: %v", err)
						}
						continue
					}
					key := fmt.Sprintf("seed-key-%d", id%1024)
					if _, err := store.Get(ctx, key); err != nil {
						b.Fatalf("mixed get: %v", err)
					}
				}
			})
		})
	}
}

func BenchmarkWarmBackendSetWithTTLParallel(b *testing.B) {
	ctx := context.Background()
	value := strings.Repeat("v", 1024)

	for name, factory := range benchmarkBackendFactories {
		b.Run(name, func(b *testing.B) {
			store, cleanup := createBenchmarkStore(b, factory)
			defer cleanup()

			var counter uint64

			b.ResetTimer()
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					id := atomic.AddUint64(&counter, 1)
					key := fmt.Sprintf("ttl-key-%d", id)
					if err := store.Set(ctx, key, value, time.Minute); err != nil {
						b.Fatalf("set ttl key: %v", err)
					}
				}
			})
		})
	}
}

func createBenchmarkStore(b *testing.B, factory backendFactory) (engine.Engine, func()) {
	b.Helper()

	dir, err := os.MkdirTemp("", "backend-bench")
	if err != nil {
		b.Fatalf("temp dir: %v", err)
	}

	store, err := factory(dir)
	if err != nil {
		os.RemoveAll(dir)
		b.Fatalf("new store: %v", err)
	}

	return store, func() {
		if err := store.Close(); err != nil {
			b.Fatalf("close store: %v", err)
		}
		if err := os.RemoveAll(dir); err != nil {
			b.Fatalf("remove temp dir: %v", err)
		}
	}
}
