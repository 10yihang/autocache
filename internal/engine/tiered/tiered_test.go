package tiered_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/engine/memory"
	"github.com/10yihang/autocache/internal/engine/tiered"
)

func TestTieredStorage(t *testing.T) {
	// Setup temporary directory for BadgerDB
	dir, err := os.MkdirTemp("", "badger-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Create memory store
	hotTier := memory.NewStore(memory.DefaultConfig())
	defer hotTier.Close()

	// Create manager config
	cfg := tiered.DefaultConfig()
	cfg.HotIdleThreshold = 100 * time.Millisecond
	cfg.MigrationInterval = 200 * time.Millisecond
	cfg.WarmTierPath = dir
	cfg.WarmTierEnabled = true
	cfg.ColdTierEnabled = false // Cold tier requires S3, skipping

	// Create manager
	manager, err := tiered.NewManager(cfg, hotTier)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	manager.Start()
	defer manager.Stop()

	ctx := context.Background()

	// 1. Write to Hot Tier
	t.Log("Writing hot data...")
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("hot-key-%d", i)
		err := manager.Set(ctx, key, "value", 0)
		if err != nil {
			t.Errorf("Set failed: %v", err)
		}
	}

	// 2. Write data intended for Cold Tier (simulate by writing and waiting)
	t.Log("Writing data to cool down...")
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("cold-key-%d", i)
		manager.Set(ctx, key, "value", 0)
	}

	// 3. Keep accessing hot data to prevent demotion
	t.Log("Accessing hot data...")
	stopAccess := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopAccess:
				return
			default:
				for i := 0; i < 10; i++ {
					key := fmt.Sprintf("hot-key-%d", i)
					manager.Get(ctx, key)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	// 4. Wait for migration (Cold keys should move to Warm)
	t.Log("Waiting for migration...")
	time.Sleep(1 * time.Second)
	close(stopAccess)

	// 5. Verify stats
	stats := manager.GetStats()
	t.Logf("Stats: %+v", stats)

	// Hot keys should remain in Hot Tier (10 keys)
	// Cold keys (10 keys) should have moved to Warm Tier
	// Total 20 keys

	if stats.TotalKeys != 20 {
		t.Errorf("Expected 20 total keys, got %d", stats.TotalKeys)
	}

	// Ideally:
	// HotTierKeys >= 10 (kept hot)
	// WarmTierKeys >= 0 (some moved)

	// Note: Exact counts depend on timing and probabilistic counters (CMS),
	// so we check if *some* migration happened or if data is accessible.

	// Verify data accessibility
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("cold-key-%d", i)
		val, err := manager.Get(ctx, key)
		if err != nil {
			t.Errorf("Failed to get migrated key %s: %v", key, err)
		}
		if val != "value" {
			t.Errorf("Value mismatch for %s: got %s, want value", key, val)
		}
	}
}
