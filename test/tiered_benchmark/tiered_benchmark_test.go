package tieredbenchmark

import (
	"context"
	"os"
	"testing"

	"github.com/10yihang/autocache/internal/engine/memory"
	"github.com/10yihang/autocache/internal/engine/tiered"
)

func BenchmarkHotOnlyGet(b *testing.B) {
	store := memory.NewStore(memory.DefaultConfig())
	defer store.Close()
	ctx := context.Background()
	if err := store.Set(ctx, "bench-key", "value", 0); err != nil {
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
}

func BenchmarkTieredHotPathGet(b *testing.B) {
	dir, err := os.MkdirTemp("", "tiered-bench")
	if err != nil {
		b.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	hotTier := memory.NewStore(memory.DefaultConfig())
	defer hotTier.Close()

	cfg := tiered.DefaultConfig()
	cfg.WarmTierEnabled = true
	cfg.ColdTierEnabled = false
	cfg.WarmTierPath = dir

	manager, err := tiered.NewManager(cfg, hotTier)
	if err != nil {
		b.Fatalf("new manager: %v", err)
	}
	defer manager.Stop()

	ctx := context.Background()
	if err := manager.Set(ctx, "bench-key", "value", 0); err != nil {
		b.Fatalf("seed key: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := manager.Get(ctx, "bench-key"); err != nil {
				b.Fatalf("get key: %v", err)
			}
		}
	})
}
