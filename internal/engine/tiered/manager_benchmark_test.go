package tiered

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/10yihang/autocache/internal/engine/memory"
)

type noPromotePolicy struct{}

func (noPromotePolicy) ShouldDemote(stats *AccessStats, currentTier TierType) bool {
	return false
}

func (noPromotePolicy) ShouldPromote(stats *AccessStats, currentTier TierType) bool {
	return false
}

func (noPromotePolicy) GetTargetTier(stats *AccessStats) TierType {
	return TierWarm
}

func BenchmarkTieredWarmPathGet(b *testing.B) {
	for _, engineName := range []string{"badger", "nokv"} {
		b.Run(engineName, func(b *testing.B) {
			manager, cleanup := createWarmPathBenchmarkManager(b, engineName)
			defer cleanup()

			ctx := context.Background()
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := manager.Get(ctx, "warm-key"); err != nil {
					b.Fatalf("warm path get: %v", err)
				}
			}
		})
	}
}

func BenchmarkTieredHotPathGet(b *testing.B) {
	for _, engineName := range []string{"badger", "nokv"} {
		b.Run(engineName, func(b *testing.B) {
			manager, cleanup := createHotPathBenchmarkManager(b, engineName)
			defer cleanup()

			ctx := context.Background()
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := manager.Get(ctx, "hot-key"); err != nil {
					b.Fatalf("hot path get: %v", err)
				}
			}
		})
	}
}

func createHotPathBenchmarkManager(b *testing.B, engineName string) (*Manager, func()) {
	b.Helper()

	dir, err := os.MkdirTemp("", "tiered-hot-bench")
	if err != nil {
		b.Fatalf("temp dir: %v", err)
	}

	hotTier := memory.NewStore(memory.DefaultConfig())
	cfg := DefaultConfig()
	cfg.WarmTierEnabled = true
	cfg.WarmTierEngine = engineName
	cfg.WarmTierPath = dir
	cfg.ColdTierEnabled = false

	manager, err := NewManager(cfg, hotTier)
	if err != nil {
		hotTier.Close()
		os.RemoveAll(dir)
		b.Fatalf("new manager: %v", err)
	}

	ctx := context.Background()
	if err := manager.Set(ctx, "hot-key", strings.Repeat("v", 1024), 0); err != nil {
		manager.Stop()
		hotTier.Close()
		os.RemoveAll(dir)
		b.Fatalf("seed hot key: %v", err)
	}

	return manager, func() {
		manager.Stop()
		if err := hotTier.Close(); err != nil {
			b.Fatalf("close hot tier: %v", err)
		}
		if err := os.RemoveAll(dir); err != nil {
			b.Fatalf("remove temp dir: %v", err)
		}
	}
}

func createWarmPathBenchmarkManager(b *testing.B, engineName string) (*Manager, func()) {
	b.Helper()

	dir, err := os.MkdirTemp("", "tiered-warm-bench")
	if err != nil {
		b.Fatalf("temp dir: %v", err)
	}

	hotTier := memory.NewStore(memory.DefaultConfig())
	cfg := DefaultConfig()
	cfg.WarmTierEnabled = true
	cfg.WarmTierEngine = engineName
	cfg.WarmTierPath = dir
	cfg.ColdTierEnabled = false

	manager, err := NewManager(cfg, hotTier)
	if err != nil {
		hotTier.Close()
		os.RemoveAll(dir)
		b.Fatalf("new manager: %v", err)
	}
	manager.policy = noPromotePolicy{}

	ctx := context.Background()
	if err := manager.Set(ctx, "warm-key", strings.Repeat("v", 1024), 0); err != nil {
		manager.Stop()
		hotTier.Close()
		os.RemoveAll(dir)
		b.Fatalf("seed warm key: %v", err)
	}
	if err := manager.migrator.demoteKey(ctx, "warm-key", TierHot, TierWarm); err != nil {
		manager.Stop()
		hotTier.Close()
		os.RemoveAll(dir)
		b.Fatalf("demote warm key: %v", err)
	}

	return manager, func() {
		manager.Stop()
		if err := hotTier.Close(); err != nil {
			b.Fatalf("close hot tier: %v", err)
		}
		if err := os.RemoveAll(dir); err != nil {
			b.Fatalf("remove temp dir: %v", err)
		}
	}
}
