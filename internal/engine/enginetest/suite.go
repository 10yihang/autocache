package enginetest

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/engine"
)

type Factory func(t *testing.T) engine.Engine

func RunConformance(t *testing.T, factory Factory) {
	t.Helper()

	t.Run("GetMissing", func(t *testing.T) {
		store := newStore(t, factory)

		_, err := store.Get(context.Background(), "missing")
		if !errors.Is(err, engine.ErrKeyNotFound) {
			t.Fatalf("Get missing error = %v, want %v", err, engine.ErrKeyNotFound)
		}
	})

	t.Run("SetGetString", func(t *testing.T) {
		store := newStore(t, factory)
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
	})

	t.Run("SetOverwrite", func(t *testing.T) {
		store := newStore(t, factory)
		ctx := context.Background()

		if err := store.Set(ctx, "key", "first", 0); err != nil {
			t.Fatalf("Set first failed: %v", err)
		}
		if err := store.Set(ctx, "key", "second", 0); err != nil {
			t.Fatalf("Set second failed: %v", err)
		}

		entry, err := store.Get(ctx, "key")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if entry.Value != "second" {
			t.Fatalf("entry.Value = %#v, want %q", entry.Value, "second")
		}
	})

	t.Run("MetadataRoundTrip", func(t *testing.T) {
		tests := []struct {
			name  string
			key   string
			value interface{}
			want  engine.ValueType
		}{
			{name: "String", key: "string", value: "value", want: engine.TypeString},
			{name: "Hash", key: "hash", value: map[string]string{"field": "value"}, want: engine.TypeHash},
			{name: "List", key: "list", value: []string{"a", "b"}, want: engine.TypeList},
			{name: "Set", key: "set", value: map[string]struct{}{"x": {}}, want: engine.TypeSet},
			{name: "ZSet", key: "zset", value: map[string]float64{"member": 1.5}, want: engine.TypeZSet},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				store := newStore(t, factory)
				ctx := context.Background()

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
	})

	t.Run("Del", func(t *testing.T) {
		store := newStore(t, factory)
		ctx := context.Background()

		for _, key := range []string{"key1", "key2"} {
			if err := store.Set(ctx, key, "value", 0); err != nil {
				t.Fatalf("Set %s failed: %v", key, err)
			}
		}

		count, err := store.Del(ctx, "key1", "key2", "missing")
		if err != nil {
			t.Fatalf("Del failed: %v", err)
		}
		if count != 2 {
			t.Fatalf("Del count = %d, want %d", count, 2)
		}

		_, err = store.Get(ctx, "key1")
		if !errors.Is(err, engine.ErrKeyNotFound) {
			t.Fatalf("Get deleted key error = %v, want %v", err, engine.ErrKeyNotFound)
		}
	})

	t.Run("Exists", func(t *testing.T) {
		store := newStore(t, factory)
		ctx := context.Background()

		if err := store.Set(ctx, "key1", "value", 0); err != nil {
			t.Fatalf("Set failed: %v", err)
		}

		count, err := store.Exists(ctx, "key1", "missing")
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}
		if count != 1 {
			t.Fatalf("Exists count = %d, want %d", count, 1)
		}
	})

	t.Run("Keys", func(t *testing.T) {
		store := newStore(t, factory)
		ctx := context.Background()

		want := []string{"alpha", "beta", "gamma"}
		for _, key := range want {
			if err := store.Set(ctx, key, "value", 0); err != nil {
				t.Fatalf("Set %s failed: %v", key, err)
			}
		}

		keys, err := store.Keys(ctx, "*")
		if err != nil {
			t.Fatalf("Keys failed: %v", err)
		}

		sort.Strings(keys)
		sort.Strings(want)
		if !reflect.DeepEqual(keys, want) {
			t.Fatalf("Keys = %#v, want %#v", keys, want)
		}
	})

	t.Run("Type", func(t *testing.T) {
		tests := []struct {
			name string
			key  string
			val  interface{}
			want string
		}{
			{name: "String", key: "string", val: "value", want: "string"},
			{name: "Hash", key: "hash", val: map[string]string{"field": "value"}, want: "hash"},
			{name: "List", key: "list", val: []string{"a"}, want: "list"},
			{name: "Set", key: "set", val: map[string]struct{}{"a": {}}, want: "set"},
			{name: "ZSet", key: "zset", val: map[string]float64{"a": 1}, want: "zset"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				store := newStore(t, factory)
				ctx := context.Background()

				if err := store.Set(ctx, tt.key, tt.val, 0); err != nil {
					t.Fatalf("Set failed: %v", err)
				}
				got, err := store.Type(ctx, tt.key)
				if err != nil {
					t.Fatalf("Type failed: %v", err)
				}
				if got != tt.want {
					t.Fatalf("Type = %q, want %q", got, tt.want)
				}
			})
		}
	})

	t.Run("Rename", func(t *testing.T) {
		store := newStore(t, factory)
		ctx := context.Background()

		if err := store.Set(ctx, "old", "value", 3*time.Second); err != nil {
			t.Fatalf("Set failed: %v", err)
		}
		if err := store.Rename(ctx, "old", "new"); err != nil {
			t.Fatalf("Rename failed: %v", err)
		}

		entry, err := store.Get(ctx, "new")
		if err != nil {
			t.Fatalf("Get renamed key failed: %v", err)
		}
		if entry.Value != "value" {
			t.Fatalf("renamed entry.Value = %#v, want %q", entry.Value, "value")
		}

		_, err = store.Get(ctx, "old")
		if !errors.Is(err, engine.ErrKeyNotFound) {
			t.Fatalf("Get old key error = %v, want %v", err, engine.ErrKeyNotFound)
		}
	})

	t.Run("RenameMissing", func(t *testing.T) {
		store := newStore(t, factory)

		if err := store.Rename(context.Background(), "missing", "new"); err == nil {
			t.Fatal("expected Rename missing key to fail")
		}
	})

	t.Run("TTLAndExpire", func(t *testing.T) {
		store := newStore(t, factory)
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

		ok, err := store.Expire(ctx, "persistent", 2*time.Second)
		if err != nil {
			t.Fatalf("Expire failed: %v", err)
		}
		if !ok {
			t.Fatal("Expire returned false, want true")
		}

		ttl, err = store.TTL(ctx, "persistent")
		if err != nil {
			t.Fatalf("TTL expiring failed: %v", err)
		}
		if ttl <= 0 {
			t.Fatalf("TTL expiring = %v, want > 0", ttl)
		}

		ok, err = store.ExpireAt(ctx, "persistent", time.Now().Add(2*time.Second))
		if err != nil {
			t.Fatalf("ExpireAt failed: %v", err)
		}
		if !ok {
			t.Fatal("ExpireAt returned false, want true")
		}

		ok, err = store.Persist(ctx, "persistent")
		if err != nil {
			t.Fatalf("Persist failed: %v", err)
		}
		if !ok {
			t.Fatal("Persist returned false, want true")
		}

		ttl, err = store.TTL(ctx, "persistent")
		if err != nil {
			t.Fatalf("TTL after Persist failed: %v", err)
		}
		if ttl != -1 {
			t.Fatalf("TTL after Persist = %v, want -1", ttl)
		}
	})

	t.Run("ExpireMissing", func(t *testing.T) {
		store := newStore(t, factory)

		ok, err := store.Expire(context.Background(), "missing", time.Second)
		if err != nil {
			t.Fatalf("Expire missing failed: %v", err)
		}
		if ok {
			t.Fatal("Expire missing returned true, want false")
		}
	})

	t.Run("PersistMissing", func(t *testing.T) {
		store := newStore(t, factory)

		ok, err := store.Persist(context.Background(), "missing")
		if err != nil {
			t.Fatalf("Persist missing failed: %v", err)
		}
		if ok {
			t.Fatal("Persist missing returned true, want false")
		}
	})

	t.Run("TTLExpiredAndMissing", func(t *testing.T) {
		store := newStore(t, factory)
		ctx := context.Background()

		if err := store.Set(ctx, "ephemeral", "value", time.Second); err != nil {
			t.Fatalf("Set failed: %v", err)
		}
		time.Sleep(2 * time.Second)

		ttl, err := store.TTL(ctx, "ephemeral")
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
	})

	t.Run("DBSize", func(t *testing.T) {
		store := newStore(t, factory)
		ctx := context.Background()

		for i := 0; i < 3; i++ {
			key := "key" + strconv.Itoa(i)
			if err := store.Set(ctx, key, "value", 0); err != nil {
				t.Fatalf("Set %s failed: %v", key, err)
			}
		}

		size, err := store.DBSize(ctx)
		if err != nil {
			t.Fatalf("DBSize failed: %v", err)
		}
		if size != 3 {
			t.Fatalf("DBSize = %d, want %d", size, 3)
		}
	})

	t.Run("FlushDB", func(t *testing.T) {
		store := newStore(t, factory)
		ctx := context.Background()

		if err := store.Set(ctx, "key", "value", 0); err != nil {
			t.Fatalf("Set failed: %v", err)
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

		if err := store.Set(ctx, "key", "value", 0); err != nil {
			t.Fatalf("Set after FlushDB failed: %v", err)
		}
	})

	t.Run("Close", func(t *testing.T) {
		store := factory(t)
		if err := store.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		store := newStore(t, factory)
		ctx := context.Background()

		const workers = 8
		const iterations = 50

		var wg sync.WaitGroup
		for worker := 0; worker < workers; worker++ {
			wg.Add(1)
			go func(worker int) {
				defer wg.Done()
				for i := 0; i < iterations; i++ {
					key := "key-" + strconv.Itoa(worker) + "-" + strconv.Itoa(i)
					if err := store.Set(ctx, key, "value", 0); err != nil {
						t.Errorf("Set failed: %v", err)
						return
					}
					if _, err := store.Get(ctx, key); err != nil {
						t.Errorf("Get failed: %v", err)
						return
					}
					if _, err := store.Del(ctx, key); err != nil {
						t.Errorf("Del failed: %v", err)
						return
					}
				}
			}(worker)
		}
		wg.Wait()
	})
}

func newStore(t *testing.T, factory Factory) engine.Engine {
	t.Helper()

	store := factory(t)
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	return store
}
