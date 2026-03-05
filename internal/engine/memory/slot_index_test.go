package memory

import (
	"testing"
)

func testSlotFunc(key string) uint16 {
	// Simple hash for testing: first byte mod 16384
	if len(key) == 0 {
		return 0
	}
	return uint16(key[0]) % 16384
}

func TestShardedCache_KeysInSlot(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	sc := NewShardedCache(cfg)
	sc.SetSlotFunc(testSlotFunc)

	// Set some keys
	sc.Set("aaa", []byte("v1"), 0)
	sc.Set("aab", []byte("v2"), 0)
	sc.Set("bbb", []byte("v3"), 0)

	// 'a' = 97, slot = 97 % 16384 = 97
	// 'b' = 98, slot = 98 % 16384 = 98
	keys := sc.KeysInSlot(97, 10)
	if len(keys) != 2 {
		t.Errorf("KeysInSlot(97) = %d keys, want 2", len(keys))
	}

	keys = sc.KeysInSlot(98, 10)
	if len(keys) != 1 {
		t.Errorf("KeysInSlot(98) = %d keys, want 1", len(keys))
	}

	// Count
	count := sc.CountKeysInSlot(97)
	if count != 2 {
		t.Errorf("CountKeysInSlot(97) = %d, want 2", count)
	}

	// Delete and verify
	sc.Delete("aaa")
	keys = sc.KeysInSlot(97, 10)
	if len(keys) != 1 {
		t.Errorf("After delete, KeysInSlot(97) = %d keys, want 1", len(keys))
	}

	// Empty slot
	keys = sc.KeysInSlot(999, 10)
	if len(keys) != 0 {
		t.Errorf("KeysInSlot(999) = %d keys, want 0", len(keys))
	}
}

func TestShardedCache_KeysInSlot_Count(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	sc := NewShardedCache(cfg)
	sc.SetSlotFunc(testSlotFunc)

	// All keys map to slot 97
	sc.Set("a1", []byte("v1"), 0)
	sc.Set("a2", []byte("v2"), 0)
	sc.Set("a3", []byte("v3"), 0)

	keys := sc.KeysInSlot(97, 2)
	if len(keys) != 2 {
		t.Errorf("KeysInSlot(97, count=2) = %d keys, want 2", len(keys))
	}
}

func TestShardedCache_KeysInSlot_AllKeys(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	sc := NewShardedCache(cfg)
	sc.SetSlotFunc(testSlotFunc)

	sc.Set("a1", []byte("v1"), 0)
	sc.Set("a2", []byte("v2"), 0)
	sc.Set("a3", []byte("v3"), 0)

	// count=0 returns all keys
	keys := sc.KeysInSlot(97, 0)
	if len(keys) != 3 {
		t.Errorf("KeysInSlot(97, count=0) = %d keys, want 3", len(keys))
	}
}

func TestShardedCache_KeysInSlot_NoSlotFunc(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	sc := NewShardedCache(cfg)
	// No slot func set

	sc.Set("abc", []byte("v1"), 0)

	// Should return empty since slotFunc is nil (slot index not populated)
	keys := sc.KeysInSlot(0, 10)
	if len(keys) != 0 {
		t.Errorf("Without slotFunc, KeysInSlot should return empty, got %d", len(keys))
	}
}

func TestShardedCache_Clear_ResetsSlotIndex(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	sc := NewShardedCache(cfg)
	sc.SetSlotFunc(testSlotFunc)

	sc.Set("aaa", []byte("v1"), 0)
	sc.Clear()

	keys := sc.KeysInSlot(97, 10)
	if len(keys) != 0 {
		t.Errorf("After Clear, KeysInSlot should return empty, got %d", len(keys))
	}
}

func TestShardedCache_SetNX_UpdatesSlotIndex(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	sc := NewShardedCache(cfg)
	sc.SetSlotFunc(testSlotFunc)

	// SetNX should update slot index
	ok, err := sc.SetNX("aaa", []byte("v1"), 0)
	if err != nil {
		t.Fatalf("SetNX: %v", err)
	}
	if !ok {
		t.Fatal("SetNX should return true for new key")
	}

	keys := sc.KeysInSlot(97, 10)
	if len(keys) != 1 {
		t.Errorf("After SetNX, KeysInSlot(97) = %d keys, want 1", len(keys))
	}

	// SetNX again should not add duplicate
	ok, err = sc.SetNX("aaa", []byte("v2"), 0)
	if err != nil {
		t.Fatalf("SetNX: %v", err)
	}
	if ok {
		t.Fatal("SetNX should return false for existing key")
	}

	count := sc.CountKeysInSlot(97)
	if count != 1 {
		t.Errorf("After duplicate SetNX, CountKeysInSlot(97) = %d, want 1", count)
	}
}
