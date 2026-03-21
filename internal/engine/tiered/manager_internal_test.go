package tiered

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/engine"
	"github.com/10yihang/autocache/internal/engine/memory"
)

type alwaysPromotePolicy struct{}

func (alwaysPromotePolicy) ShouldDemote(string, *AccessStats, TierType) bool  { return false }
func (alwaysPromotePolicy) ShouldPromote(string, *AccessStats, TierType) bool { return true }
func (alwaysPromotePolicy) GetTargetTier(string, *AccessStats) TierType       { return TierHot }

func TestManager_PromotionPreservesTTL(t *testing.T) {
	dir, err := os.MkdirTemp("", "tiered-manager-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	hotTier := memory.NewStore(memory.DefaultConfig())
	defer hotTier.Close()

	cfg := DefaultConfig()
	cfg.WarmTierEnabled = true
	cfg.ColdTierEnabled = false
	cfg.WarmTierPath = dir

	manager, err := NewManager(cfg, hotTier)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer manager.Stop()
	manager.policy = alwaysPromotePolicy{}

	ctx := context.Background()
	if err := manager.Set(ctx, "promo-key", "value", 2*time.Second); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	if err := manager.migrator.demoteKey(ctx, "promo-key", TierHot, TierWarm); err != nil {
		t.Fatalf("demoteKey failed: %v", err)
	}

	if _, err := manager.Get(ctx, "promo-key"); err != nil {
		t.Fatalf("Get after demotion failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	time.Sleep(2200 * time.Millisecond)

	if _, err := manager.Get(ctx, "promo-key"); err == nil {
		t.Fatal("promoted key should expire after original TTL")
	}
}

func TestManager_TracksMigrationCounters(t *testing.T) {
	dir, err := os.MkdirTemp("", "tiered-manager-stats")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	hotTier := memory.NewStore(memory.DefaultConfig())
	defer hotTier.Close()

	cfg := DefaultConfig()
	cfg.WarmTierEnabled = true
	cfg.ColdTierEnabled = false
	cfg.WarmTierPath = dir

	manager, err := NewManager(cfg, hotTier)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer manager.Stop()
	manager.policy = alwaysPromotePolicy{}

	ctx := context.Background()
	if err := manager.Set(ctx, "count-key", "value", 2*time.Second); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if err := manager.migrator.demoteKey(ctx, "count-key", TierHot, TierWarm); err != nil {
		t.Fatalf("demoteKey failed: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, _ = manager.Get(ctx, "count-key")
		stats := manager.GetStats()
		if stats.MigrationsDown == 1 && stats.MigrationsUp == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	stats := manager.GetStats()
	t.Fatalf("migration counters = up:%d down:%d, want 1/1", stats.MigrationsUp, stats.MigrationsDown)
}

func TestManager_GetStatsAggregatesTierSizes(t *testing.T) {
	hotTier := memory.NewStore(memory.DefaultConfig())
	defer hotTier.Close()

	manager := &Manager{stats: NewStatsCollector(1.0)}
	defer manager.stats.Stop()

	manager.stats.RecordWrite("hot-key", 5)
	manager.stats.RecordWrite("warm-key", 7)
	manager.stats.RecordWrite("cold-key", 11)
	manager.stats.UpdateTier("warm-key", TierWarm)
	manager.stats.UpdateTier("cold-key", TierCold)

	stats := manager.GetStats()
	if stats.HotTierSize != 5 || stats.WarmTierSize != 7 || stats.ColdTierSize != 11 {
		t.Fatalf("tier sizes = hot:%d warm:%d cold:%d, want 5/7/11", stats.HotTierSize, stats.WarmTierSize, stats.ColdTierSize)
	}
	if stats.TotalSize != 23 {
		t.Fatalf("TotalSize = %d, want 23", stats.TotalSize)
	}
}

func TestMigrator_DemoteHashPreservesType(t *testing.T) {
	dir, err := os.MkdirTemp("", "tiered-demote-hash")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	hotTier := memory.NewStore(memory.DefaultConfig())
	defer hotTier.Close()

	cfg := DefaultConfig()
	cfg.WarmTierEnabled = true
	cfg.ColdTierEnabled = false
	cfg.WarmTierPath = dir

	manager, err := NewManager(cfg, hotTier)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer manager.Stop()

	ctx := context.Background()
	if _, err := hotTier.HSet(ctx, "hash-key", "field", "value"); err != nil {
		t.Fatalf("HSet failed: %v", err)
	}
	if ok, err := hotTier.Expire(ctx, "hash-key", 2*time.Second); err != nil || !ok {
		t.Fatalf("Expire failed: ok=%v err=%v", ok, err)
	}

	if err := manager.migrator.demoteKey(ctx, "hash-key", TierHot, TierWarm); err != nil {
		t.Fatalf("demoteKey failed: %v", err)
	}

	entry, err := manager.warmTier.Get(ctx, "hash-key")
	if err != nil {
		t.Fatalf("warmTier.Get failed: %v", err)
	}
	if entry.Type != engine.TypeHash {
		t.Fatalf("entry.Type = %v, want %v", entry.Type, engine.TypeHash)
	}
	if !reflect.DeepEqual(entry.Value, map[string]string{"field": "value"}) {
		t.Fatalf("entry.Value = %#v, want hash payload", entry.Value)
	}
	if entry.ExpireAt.IsZero() {
		t.Fatal("expected demoted hash to preserve TTL metadata")
	}
}
