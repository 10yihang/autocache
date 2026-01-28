package badger

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/engine"
)

func createTestStore(t *testing.T) (*Store, string) {
	dir, err := os.MkdirTemp("", "badger-test")
	if err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(dir)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatal(err)
	}

	return store, dir
}

func closeTestStore(t *testing.T, store *Store, dir string) {
	store.Close()
	os.RemoveAll(dir)
}

func TestStore_SetGet(t *testing.T) {
	store, dir := createTestStore(t)
	defer closeTestStore(t, store, dir)

	ctx := context.Background()

	// Set
	err := store.Set(ctx, "key1", "value1", 0)
	if err != nil {
		t.Errorf("Set failed: %v", err)
	}

	// Get
	entry, err := store.Get(ctx, "key1")
	if err != nil {
		t.Errorf("Get failed: %v", err)
	}

	if entry.Key != "key1" {
		t.Errorf("Key mismatch: got %s, want key1", entry.Key)
	}
	if entry.Value != "value1" {
		t.Errorf("Value mismatch: got %v, want value1", entry.Value)
	}
}

func TestStore_Expiry(t *testing.T) {
	store, dir := createTestStore(t)
	defer closeTestStore(t, store, dir)

	ctx := context.Background()

	// Set with TTL (Badger uses second precision for Expiry check via public API, though internal storage might be finer)
	// We use 1 second to be safe
	err := store.Set(ctx, "key1", "value1", 1*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Get immediately
	_, err = store.Get(ctx, "key1")
	if err != nil {
		t.Errorf("Get failed before expiry: %v", err)
	}

	// Wait for expiry
	time.Sleep(2 * time.Second)

	// Get after expiry
	_, err = store.Get(ctx, "key1")
	if err != engine.ErrKeyNotFound {
		t.Errorf("Get after expiry mismatch: got %v, want ErrKeyNotFound", err)
	}
}

func TestStore_Del(t *testing.T) {
	store, dir := createTestStore(t)
	defer closeTestStore(t, store, dir)

	ctx := context.Background()

	store.Set(ctx, "key1", "val", 0)
	store.Set(ctx, "key2", "val", 0)

	count, err := store.Del(ctx, "key1", "key2", "key3")
	if err != nil {
		t.Fatal(err)
	}

	if count != 2 {
		t.Errorf("Del count mismatch: got %d, want 2", count)
	}

	_, err = store.Get(ctx, "key1")
	if err != engine.ErrKeyNotFound {
		t.Error("key1 should be deleted")
	}
}

func TestStore_Keys(t *testing.T) {
	store, dir := createTestStore(t)
	defer closeTestStore(t, store, dir)

	ctx := context.Background()

	for i := 0; i < 5; i++ {
		store.Set(ctx, fmt.Sprintf("key%d", i), "val", 0)
	}

	keys, err := store.Keys(ctx, "*")
	if err != nil {
		t.Fatal(err)
	}

	if len(keys) != 5 {
		t.Errorf("Keys len mismatch: got %d, want 5", len(keys))
	}
}
