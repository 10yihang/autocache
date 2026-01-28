package memory

import (
	"context"
	"testing"
)

func TestStore_Scan(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()
	ctx := context.Background()

	store.Set(ctx, "user:1", "alice", 0)
	store.Set(ctx, "user:2", "bob", 0)
	store.Set(ctx, "other:1", "data", 0)

	keys, nextCursor, err := store.Scan(ctx, 0, "user:*", 10)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(keys) != 2 {
		t.Errorf("Scan returned %d keys, want 2", len(keys))
	}

	userKeys := make(map[string]bool)
	for _, k := range keys {
		userKeys[k] = true
	}
	if !userKeys["user:1"] || !userKeys["user:2"] {
		t.Errorf("Scan missing expected keys: got %v", keys)
	}

	if nextCursor != 0 {
		t.Errorf("nextCursor = %d, want 0 (scan complete)", nextCursor)
	}
}

func TestStore_Scan_AllKeys(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()
	ctx := context.Background()

	store.Set(ctx, "key1", "val1", 0)
	store.Set(ctx, "key2", "val2", 0)
	store.Set(ctx, "key3", "val3", 0)

	keys, _, err := store.Scan(ctx, 0, "*", 10)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(keys) != 3 {
		t.Errorf("Scan returned %d keys, want 3", len(keys))
	}
}

func TestStore_Scan_Pagination(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()
	ctx := context.Background()

	for i := 0; i < 20; i++ {
		store.Set(ctx, "key"+string(rune('a'+i)), "val", 0)
	}

	allKeys := make(map[string]bool)
	cursor := uint64(0)
	iterations := 0

	for {
		keys, nextCursor, err := store.Scan(ctx, cursor, "*", 5)
		if err != nil {
			t.Fatalf("Scan failed: %v", err)
		}

		for _, k := range keys {
			allKeys[k] = true
		}

		cursor = nextCursor
		iterations++

		if cursor == 0 || iterations > 100 {
			break
		}
	}

	if len(allKeys) != 20 {
		t.Errorf("Scan collected %d keys, want 20", len(allKeys))
	}
}

func TestStore_Scan_EmptyDB(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()
	ctx := context.Background()

	keys, nextCursor, err := store.Scan(ctx, 0, "*", 10)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(keys) != 0 {
		t.Errorf("Scan returned %d keys, want 0", len(keys))
	}

	if nextCursor != 0 {
		t.Errorf("nextCursor = %d, want 0", nextCursor)
	}
}
