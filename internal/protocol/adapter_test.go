package protocol

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/engine"
	"github.com/10yihang/autocache/internal/engine/memory"
	"github.com/10yihang/autocache/internal/engine/tiered"
)

func TestMemoryStoreAdapter_GetSet(t *testing.T) {
	store := memory.NewStore(memory.DefaultConfig())
	defer store.Close()

	adapter := NewMemoryStoreAdapter(store)
	ctx := context.Background()

	err := adapter.Set(ctx, "key1", "value1", 0)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, err := adapter.GetBytes(ctx, "key1")
	if err != nil {
		t.Fatalf("GetBytes failed: %v", err)
	}
	if string(val) != "value1" {
		t.Errorf("GetBytes = %q, want %q", val, "value1")
	}
}

func TestMemoryStoreAdapter_SetNX(t *testing.T) {
	store := memory.NewStore(memory.DefaultConfig())
	defer store.Close()

	adapter := NewMemoryStoreAdapter(store)
	ctx := context.Background()

	ok, err := adapter.SetNX(ctx, "key1", "value1", 0)
	if err != nil {
		t.Fatalf("SetNX failed: %v", err)
	}
	if !ok {
		t.Error("SetNX should succeed for new key")
	}

	ok, err = adapter.SetNX(ctx, "key1", "value2", 0)
	if err != nil {
		t.Fatalf("SetNX failed: %v", err)
	}
	if ok {
		t.Error("SetNX should fail for existing key")
	}
}

func TestMemoryStoreAdapter_IncrDecr(t *testing.T) {
	store := memory.NewStore(memory.DefaultConfig())
	defer store.Close()

	adapter := NewMemoryStoreAdapter(store)
	ctx := context.Background()

	val, err := adapter.Incr(ctx, "counter")
	if err != nil {
		t.Fatalf("Incr failed: %v", err)
	}
	if val != 1 {
		t.Errorf("Incr = %d, want 1", val)
	}

	val, err = adapter.IncrBy(ctx, "counter", 5)
	if err != nil {
		t.Fatalf("IncrBy failed: %v", err)
	}
	if val != 6 {
		t.Errorf("IncrBy = %d, want 6", val)
	}

	val, err = adapter.Decr(ctx, "counter")
	if err != nil {
		t.Fatalf("Decr failed: %v", err)
	}
	if val != 5 {
		t.Errorf("Decr = %d, want 5", val)
	}
}

func TestMemoryStoreAdapter_Del(t *testing.T) {
	store := memory.NewStore(memory.DefaultConfig())
	defer store.Close()

	adapter := NewMemoryStoreAdapter(store)
	ctx := context.Background()

	adapter.Set(ctx, "key1", "value1", 0)
	adapter.Set(ctx, "key2", "value2", 0)

	count, err := adapter.Del(ctx, "key1", "key2", "nonexistent")
	if err != nil {
		t.Fatalf("Del failed: %v", err)
	}
	if count != 2 {
		t.Errorf("Del = %d, want 2", count)
	}
}

func TestMemoryStoreAdapter_TTL(t *testing.T) {
	store := memory.NewStore(memory.DefaultConfig())
	defer store.Close()

	adapter := NewMemoryStoreAdapter(store)
	ctx := context.Background()

	adapter.Set(ctx, "key1", "value1", 10*time.Second)

	ttl, err := adapter.TTL(ctx, "key1")
	if err != nil {
		t.Fatalf("TTL failed: %v", err)
	}
	if ttl < 9*time.Second || ttl > 10*time.Second {
		t.Errorf("TTL = %v, want ~10s", ttl)
	}
}

func TestMemoryStoreAdapter_GetEntry(t *testing.T) {
	store := memory.NewStore(memory.DefaultConfig())
	defer store.Close()

	adapter := NewMemoryStoreAdapter(store)
	ctx := context.Background()

	adapter.Set(ctx, "key1", "value1", 0)

	entry, err := adapter.GetEntry(ctx, "key1")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if entry.Key != "key1" {
		t.Errorf("entry.Key = %q, want %q", entry.Key, "key1")
	}
	if string(entry.Value.([]byte)) != "value1" {
		t.Errorf("entry.Value = %v, want %q", entry.Value, "value1")
	}
}

func TestMemoryStoreAdapter_ImplementsInterface(t *testing.T) {
	store := memory.NewStore(memory.DefaultConfig())
	defer store.Close()

	var _ ProtocolEngine = NewMemoryStoreAdapter(store)
}

func TestTieredStoreAdapter_WithoutHotTierRejectsMemoryOnlyCommands(t *testing.T) {
	cfg := tiered.DefaultConfig()
	cfg.HotTierEnabled = false
	cfg.WarmTierEnabled = true
	cfg.WarmTierEngine = "nokv"
	cfg.WarmTierPath = t.TempDir()
	cfg.ColdTierEnabled = false

	manager, err := tiered.NewManager(cfg, nil)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer manager.Stop()

	adapter := NewTieredStoreAdapter(manager, nil)
	ctx := context.Background()

	if err := adapter.Set(ctx, "key", "value", 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	value, err := adapter.GetBytes(ctx, "key")
	if err != nil {
		t.Fatalf("GetBytes failed: %v", err)
	}
	if string(value) != "value" {
		t.Fatalf("GetBytes = %q, want %q", value, "value")
	}

	if _, err := adapter.HSet(ctx, "hash", "field", "value"); !errors.Is(err, engine.ErrNotSupported) {
		t.Fatalf("HSet error = %v, want %v", err, engine.ErrNotSupported)
	}
	if _, err := adapter.Incr(ctx, "counter"); !errors.Is(err, engine.ErrNotSupported) {
		t.Fatalf("Incr error = %v, want %v", err, engine.ErrNotSupported)
	}
	if err := adapter.RestoreEntry(ctx, "hash", engine.TypeHash, []byte("payload"), 0); !errors.Is(err, engine.ErrNotSupported) {
		t.Fatalf("RestoreEntry error = %v, want %v", err, engine.ErrNotSupported)
	}

	if err := adapter.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if got := adapter.CountKeysInSlot(0); got != 0 {
		t.Fatalf("CountKeysInSlot = %d, want 0", got)
	}
}
