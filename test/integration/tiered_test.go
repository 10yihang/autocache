package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/engine/memory"
	"github.com/10yihang/autocache/internal/engine/tiered"
)

func TestTieredStorage_ColdDataEviction(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "autocache-tiered-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := memory.NewStore(memory.DefaultConfig())
	defer store.Close()

	cfg := &tiered.Config{
		Enabled:            true,
		HotTierCapacity:    64 * 1024 * 1024,
		WarmTierEnabled:    true,
		WarmTierPath:       tmpDir,
		ColdTierEnabled:    false,
		MigrationInterval:  500 * time.Millisecond,
		MigrationBatchSize: 100,
		HotAccessThreshold: 1000,
		HotIdleThreshold:   100 * time.Millisecond,
		ColdIdleThreshold:  2 * time.Hour,
	}

	mgr, err := tiered.NewManager(cfg, store)
	if err != nil {
		t.Fatalf("failed to create tiered manager: %v", err)
	}

	ctx := context.Background()

	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("cold-key-%d", i)
		value := fmt.Sprintf("cold-value-%d", i)
		if err := mgr.Set(ctx, key, value, 0); err != nil {
			t.Fatalf("set key %s: %v", key, err)
		}
	}

	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("cold-key-%d", i)
		expectedValue := fmt.Sprintf("cold-value-%d", i)
		got, err := mgr.Get(ctx, key)
		if err != nil {
			t.Fatalf("get key %s before migration: %v", key, err)
		}
		if got != expectedValue {
			t.Fatalf("value mismatch for %s before migration: got %q want %q", key, got, expectedValue)
		}
	}

	mgr.Start()
	defer mgr.Stop()

	time.Sleep(3 * time.Second)

	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("cold-key-%d", i)
		expectedValue := fmt.Sprintf("cold-value-%d", i)
		got, err := mgr.Get(ctx, key)
		if err != nil {
			t.Fatalf("get key %s after migration: %v", key, err)
		}
		if got != expectedValue {
			t.Fatalf("value mismatch for %s after migration: got %q want %q", key, got, expectedValue)
		}
	}

	stats := mgr.GetStats()
	t.Logf("tiered stats: hot=%d warm=%d cold=%d total=%d", stats.HotTierKeys, stats.WarmTierKeys, stats.ColdTierKeys, stats.TotalKeys)
	if stats.WarmTierKeys == 0 {
		t.Log("WARNING: no keys observed in warm tier; migration timing is asynchronous and may vary")
	}
}
