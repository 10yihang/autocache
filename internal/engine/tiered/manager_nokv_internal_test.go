package tiered

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/engine"
	"github.com/10yihang/autocache/internal/engine/memory"
)

func TestMigrator_DemoteHashPreservesTypeWithNoKV(t *testing.T) {
	dir, err := os.MkdirTemp("", "tiered-demote-hash-nokv")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	hotTier := memory.NewStore(memory.DefaultConfig())
	defer hotTier.Close()

	cfg := DefaultConfig()
	cfg.WarmTierEnabled = true
	cfg.WarmTierEngine = "nokv"
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

func TestManager_PromotesFromNoKVWarmTier(t *testing.T) {
	dir, err := os.MkdirTemp("", "tiered-promo-nokv")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	hotTier := memory.NewStore(memory.DefaultConfig())
	defer hotTier.Close()

	cfg := DefaultConfig()
	cfg.WarmTierEnabled = true
	cfg.WarmTierEngine = "nokv"
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

	val, err := manager.Get(ctx, "promo-key")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if val != "value" {
		t.Fatalf("Get returned %q, want %q", val, "value")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := hotTier.Get(context.Background(), "promo-key"); err == nil {
			if _, err := manager.warmTier.Get(context.Background(), "promo-key"); !errors.Is(err, engine.ErrKeyNotFound) {
				t.Fatalf("warm tier still has promoted key, err=%v", err)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("expected key to be promoted back into hot tier")
}
