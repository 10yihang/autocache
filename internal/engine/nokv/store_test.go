package nokv

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/engine"
)

func createTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	return store
}

func TestStore_SetGetRoundTrip(t *testing.T) {
	store := createTestStore(t)
	ctx := context.Background()

	if err := store.Set(ctx, "key", "value", 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	entry, err := store.Get(ctx, "key")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if entry.Key != "key" {
		t.Fatalf("entry.Key = %q, want %q", entry.Key, "key")
	}
	if entry.Value != "value" {
		t.Fatalf("entry.Value = %#v, want %q", entry.Value, "value")
	}
	if entry.Type != engine.TypeString {
		t.Fatalf("entry.Type = %v, want %v", entry.Type, engine.TypeString)
	}
}

func TestStore_GetMissing(t *testing.T) {
	store := createTestStore(t)

	_, err := store.Get(context.Background(), "missing")
	if !errors.Is(err, engine.ErrKeyNotFound) {
		t.Fatalf("Get missing error = %v, want %v", err, engine.ErrKeyNotFound)
	}
}

func TestStore_SetPreservesMetadata(t *testing.T) {
	store := createTestStore(t)
	ctx := context.Background()

	tests := []struct {
		name  string
		key   string
		value interface{}
		want  engine.ValueType
	}{
		{name: "string", key: "string", value: "value", want: engine.TypeString},
		{name: "hash", key: "hash", value: map[string]string{"field": "value"}, want: engine.TypeHash},
		{name: "list", key: "list", value: []string{"a", "b"}, want: engine.TypeList},
		{name: "set", key: "set", value: map[string]struct{}{"x": {}}, want: engine.TypeSet},
		{name: "zset", key: "zset", value: map[string]float64{"member": 1.5}, want: engine.TypeZSet},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.Set(ctx, tt.key, tt.value, 2*time.Second); err != nil {
				t.Fatalf("Set failed: %v", err)
			}

			entry, err := store.Get(ctx, tt.key)
			if err != nil {
				t.Fatalf("Get failed: %v", err)
			}
			if entry.Type != tt.want {
				t.Fatalf("entry.Type = %v, want %v", entry.Type, tt.want)
			}
			if !reflect.DeepEqual(entry.Value, tt.value) {
				t.Fatalf("entry.Value = %#v, want %#v", entry.Value, tt.value)
			}
			if entry.ExpireAt.IsZero() {
				t.Fatal("expected ExpireAt to be preserved")
			}
		})
	}
}

func TestStore_TTLSemantics(t *testing.T) {
	store := createTestStore(t)
	ctx := context.Background()

	if err := store.Set(ctx, "persistent", "value", 0); err != nil {
		t.Fatalf("Set persistent failed: %v", err)
	}

	ttl, err := store.TTL(ctx, "persistent")
	if err != nil {
		t.Fatalf("TTL persistent failed: %v", err)
	}
	if ttl != -1 {
		t.Fatalf("TTL persistent = %v, want -1", ttl)
	}

	if err := store.Set(ctx, "ephemeral", "value", time.Second); err != nil {
		t.Fatalf("Set ephemeral failed: %v", err)
	}

	ttl, err = store.TTL(ctx, "ephemeral")
	if err != nil {
		t.Fatalf("TTL ephemeral failed: %v", err)
	}
	if ttl <= 0 {
		t.Fatalf("TTL ephemeral = %v, want > 0", ttl)
	}

	time.Sleep(2 * time.Second)

	ttl, err = store.TTL(ctx, "ephemeral")
	if !errors.Is(err, engine.ErrKeyNotFound) {
		t.Fatalf("TTL expired error = %v, want %v", err, engine.ErrKeyNotFound)
	}
	if ttl != -2 {
		t.Fatalf("TTL expired = %v, want -2", ttl)
	}

	ttl, err = store.TTL(ctx, "missing")
	if !errors.Is(err, engine.ErrKeyNotFound) {
		t.Fatalf("TTL missing error = %v, want %v", err, engine.ErrKeyNotFound)
	}
	if ttl != -2 {
		t.Fatalf("TTL missing = %v, want -2", ttl)
	}
}

func TestStore_DelExistsKeysTypeAndDBSize(t *testing.T) {
	store := createTestStore(t)
	ctx := context.Background()

	fixtures := map[string]interface{}{
		"alpha": "value",
		"beta":  []string{"a", "b"},
		"gamma": map[string]string{"field": "value"},
	}
	for key, value := range fixtures {
		if err := store.Set(ctx, key, value, 0); err != nil {
			t.Fatalf("Set %s failed: %v", key, err)
		}
	}

	count, err := store.Exists(ctx, "alpha", "missing", "gamma")
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("Exists count = %d, want %d", count, 2)
	}

	keys, err := store.Keys(ctx, "*")
	if err != nil {
		t.Fatalf("Keys failed: %v", err)
	}
	sort.Strings(keys)
	if want := []string{"alpha", "beta", "gamma"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("Keys = %#v, want %#v", keys, want)
	}

	typ, err := store.Type(ctx, "beta")
	if err != nil {
		t.Fatalf("Type failed: %v", err)
	}
	if typ != "list" {
		t.Fatalf("Type = %q, want %q", typ, "list")
	}

	size, err := store.DBSize(ctx)
	if err != nil {
		t.Fatalf("DBSize failed: %v", err)
	}
	if size != 3 {
		t.Fatalf("DBSize = %d, want %d", size, 3)
	}

	deleted, err := store.Del(ctx, "alpha", "missing")
	if err != nil {
		t.Fatalf("Del failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("Del count = %d, want %d", deleted, 1)
	}

	_, err = store.Get(ctx, "alpha")
	if !errors.Is(err, engine.ErrKeyNotFound) {
		t.Fatalf("Get deleted key error = %v, want %v", err, engine.ErrKeyNotFound)
	}
}

func TestStore_RenameExpirePersistAndFlushDB(t *testing.T) {
	store := createTestStore(t)
	ctx := context.Background()

	if err := store.Set(ctx, "old", "value", time.Second); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if err := store.Rename(ctx, "old", "new"); err != nil {
		t.Fatalf("Rename failed: %v", err)
	}
	if _, err := store.Get(ctx, "old"); !errors.Is(err, engine.ErrKeyNotFound) {
		t.Fatalf("Get old key error = %v, want %v", err, engine.ErrKeyNotFound)
	}
	if _, err := store.Get(ctx, "new"); err != nil {
		t.Fatalf("Get new key failed: %v", err)
	}

	ok, err := store.Expire(ctx, "new", 2*time.Second)
	if err != nil {
		t.Fatalf("Expire failed: %v", err)
	}
	if !ok {
		t.Fatal("Expire returned false, want true")
	}

	ttl, err := store.TTL(ctx, "new")
	if err != nil {
		t.Fatalf("TTL after Expire failed: %v", err)
	}
	if ttl <= 0 {
		t.Fatalf("TTL after Expire = %v, want > 0", ttl)
	}

	ok, err = store.Persist(ctx, "new")
	if err != nil {
		t.Fatalf("Persist failed: %v", err)
	}
	if !ok {
		t.Fatal("Persist returned false, want true")
	}

	ttl, err = store.TTL(ctx, "new")
	if err != nil {
		t.Fatalf("TTL after Persist failed: %v", err)
	}
	if ttl != -1 {
		t.Fatalf("TTL after Persist = %v, want -1", ttl)
	}

	if err := store.Set(ctx, "flush", "value", 0); err != nil {
		t.Fatalf("Set flush key failed: %v", err)
	}
	if err := store.FlushDB(ctx); err != nil {
		t.Fatalf("FlushDB failed: %v", err)
	}

	size, err := store.DBSize(ctx)
	if err != nil {
		t.Fatalf("DBSize after FlushDB failed: %v", err)
	}
	if size != 0 {
		t.Fatalf("DBSize after FlushDB = %d, want 0", size)
	}
	if err := store.Set(ctx, "after-flush", "value", 0); err != nil {
		t.Fatalf("Set after FlushDB failed: %v", err)
	}
}

func TestStore_ExpireAndPersistMissing(t *testing.T) {
	store := createTestStore(t)
	ctx := context.Background()

	ok, err := store.Expire(ctx, "missing", time.Second)
	if err != nil {
		t.Fatalf("Expire missing failed: %v", err)
	}
	if ok {
		t.Fatal("Expire missing returned true, want false")
	}

	ok, err = store.Persist(ctx, "missing")
	if err != nil {
		t.Fatalf("Persist missing failed: %v", err)
	}
	if ok {
		t.Fatal("Persist missing returned true, want false")
	}
}

func TestStore_ConcurrentCompositeAccess(t *testing.T) {
	store := createTestStore(t)
	ctx := context.Background()

	const workers = 4
	const iterations = 20

	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				key := "key-" + string(rune('a'+worker)) + "-" + string(rune('a'+i))
				if err := store.Set(ctx, key, "value", 0); err != nil {
					t.Errorf("Set failed: %v", err)
					return
				}
				if _, err := store.Expire(ctx, key, time.Second); err != nil {
					t.Errorf("Expire failed: %v", err)
					return
				}
				if _, err := store.Persist(ctx, key); err != nil {
					t.Errorf("Persist failed: %v", err)
					return
				}
			}
		}(worker)
	}
	wg.Wait()
}
