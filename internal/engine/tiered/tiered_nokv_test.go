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

func TestTieredStorageNoKV(t *testing.T) {
	dir, err := os.MkdirTemp("", "nokv-tiered-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	hotTier := memory.NewStore(memory.DefaultConfig())
	defer hotTier.Close()

	cfg := tiered.DefaultConfig()
	cfg.HotIdleThreshold = 100 * time.Millisecond
	cfg.MigrationInterval = 200 * time.Millisecond
	cfg.WarmTierEnabled = true
	cfg.WarmTierEngine = "nokv"
	cfg.WarmTierPath = dir
	cfg.ColdTierEnabled = false

	manager, err := tiered.NewManager(cfg, hotTier)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	manager.Start()
	defer manager.Stop()

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("hot-key-%d", i)
		if err := manager.Set(ctx, key, "value", 0); err != nil {
			t.Fatalf("Set hot key failed: %v", err)
		}
	}
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("cold-key-%d", i)
		if err := manager.Set(ctx, key, "value", 0); err != nil {
			t.Fatalf("Set cold key failed: %v", err)
		}
	}

	stopAccess := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopAccess:
				return
			default:
				for i := 0; i < 10; i++ {
					_, _ = manager.Get(ctx, fmt.Sprintf("hot-key-%d", i))
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	time.Sleep(time.Second)
	close(stopAccess)

	stats := manager.GetStats()
	if stats.TotalKeys != 20 {
		t.Fatalf("TotalKeys = %d, want 20", stats.TotalKeys)
	}
	if stats.WarmTierKeys == 0 {
		t.Fatal("expected some keys to demote into NoKV warm tier")
	}

	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("cold-key-%d", i)
		val, err := manager.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get migrated key %s failed: %v", key, err)
		}
		if val != "value" {
			t.Fatalf("value for %s = %q, want %q", key, val, "value")
		}
	}
}
