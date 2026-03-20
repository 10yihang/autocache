package tiered

import (
	"strings"
	"testing"

	"github.com/10yihang/autocache/internal/engine/memory"
)

func TestDefaultConfigSetsWarmTierEngine(t *testing.T) {
	if got := DefaultConfig().WarmTierEngine; got != "badger" {
		t.Fatalf("DefaultConfig().WarmTierEngine = %q, want %q", got, "badger")
	}
}

func TestNewManagerRejectsUnknownWarmTierEngine(t *testing.T) {
	hotTier := memory.NewStore(memory.DefaultConfig())
	defer hotTier.Close()

	cfg := DefaultConfig()
	cfg.WarmTierEnabled = true
	cfg.WarmTierEngine = "unknown"
	cfg.WarmTierPath = t.TempDir()
	cfg.ColdTierEnabled = false

	_, err := NewManager(cfg, hotTier)
	if err == nil {
		t.Fatal("expected unknown warm tier engine to fail")
	}
	if !strings.Contains(err.Error(), "unknown warm tier engine") {
		t.Fatalf("error = %v, want substring %q", err, "unknown warm tier engine")
	}
}

func TestNewManagerAcceptsNoKVWarmTierEngine(t *testing.T) {
	hotTier := memory.NewStore(memory.DefaultConfig())
	defer hotTier.Close()

	cfg := DefaultConfig()
	cfg.WarmTierEnabled = true
	cfg.WarmTierEngine = "nokv"
	cfg.WarmTierPath = t.TempDir()
	cfg.ColdTierEnabled = false

	manager, err := NewManager(cfg, hotTier)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	manager.Stop()
}

func TestNewManagerRejectsAllTiersDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HotTierEnabled = false
	cfg.WarmTierEnabled = false
	cfg.ColdTierEnabled = false

	if _, err := NewManager(cfg, nil); err == nil {
		t.Fatal("expected all tiers disabled to fail")
	}
}

func TestNewManagerAllowsWarmOnlyWithoutHotTier(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HotTierEnabled = false
	cfg.WarmTierEnabled = true
	cfg.WarmTierEngine = "nokv"
	cfg.WarmTierPath = t.TempDir()
	cfg.ColdTierEnabled = false

	manager, err := NewManager(cfg, nil)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	manager.Stop()
}

func TestNewManagerAllowsHotOnly(t *testing.T) {
	hotTier := memory.NewStore(memory.DefaultConfig())
	defer hotTier.Close()

	cfg := DefaultConfig()
	cfg.HotTierEnabled = true
	cfg.WarmTierEnabled = false
	cfg.ColdTierEnabled = false

	manager, err := NewManager(cfg, hotTier)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	manager.Stop()
}
