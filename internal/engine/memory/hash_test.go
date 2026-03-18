package memory

import (
	"context"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/10yihang/autocache/internal/engine"
	pkgerrors "github.com/10yihang/autocache/pkg/errors"
)

func TestHash_HSetHGet(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()

	ctx := context.Background()

	added, err := store.HSet(ctx, "myhash", "field1", "val1", "field2", "val2")
	if err != nil {
		t.Fatalf("HSet failed: %v", err)
	}
	if added != 2 {
		t.Fatalf("HSet added %d fields, want 2", added)
	}

	got, err := store.HGet(ctx, "myhash", "field1")
	if err != nil {
		t.Fatalf("HGet failed: %v", err)
	}
	if got != "val1" {
		t.Fatalf("HGet returned %q, want %q", got, "val1")
	}
}

func TestHash_WrongType(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()

	ctx := context.Background()
	if err := store.Set(ctx, "plain", "value", 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	if _, err := store.HSet(ctx, "plain", "field", "value"); err == nil {
		t.Fatal("HSet on string key should fail with wrong type")
	}
}

func TestHash_HDelExistsGetAllLen(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()

	ctx := context.Background()
	if _, err := store.HSet(ctx, "myhash", "field1", "val1", "field2", "val2"); err != nil {
		t.Fatalf("HSet failed: %v", err)
	}

	length, err := store.HLen(ctx, "myhash")
	if err != nil {
		t.Fatalf("HLen failed: %v", err)
	}
	if length != 2 {
		t.Fatalf("HLen returned %d, want 2", length)
	}

	exists, err := store.HExists(ctx, "myhash", "field2")
	if err != nil {
		t.Fatalf("HExists failed: %v", err)
	}
	if !exists {
		t.Fatal("HExists returned false, want true")
	}

	all, err := store.HGetAll(ctx, "myhash")
	if err != nil {
		t.Fatalf("HGetAll failed: %v", err)
	}
	if len(all) != 2 || all["field1"] != "val1" || all["field2"] != "val2" {
		t.Fatalf("HGetAll returned %#v, want both fields", all)
	}

	deleted, err := store.HDel(ctx, "myhash", "field2")
	if err != nil {
		t.Fatalf("HDel failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("HDel deleted %d fields, want 1", deleted)
	}

	exists, err = store.HExists(ctx, "myhash", "field2")
	if err != nil {
		t.Fatalf("HExists after delete failed: %v", err)
	}
	if exists {
		t.Fatal("HExists returned true after delete")
	}
}

func TestStore_GetSet_OnHashReturnsWrongType(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()

	ctx := context.Background()
	if _, err := store.HSet(ctx, "myhash", "field", "value"); err != nil {
		t.Fatalf("HSet failed: %v", err)
	}

	if _, err := store.GetSet(ctx, "myhash", "new"); err != pkgerrors.ErrWrongType {
		t.Fatalf("GetSet error = %v, want ErrWrongType", err)
	}
}

func TestStore_Scan_MixedKeyPagination(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		key := string(rune('a' + i))
		if err := store.Set(ctx, "s:"+key, key, 0); err != nil {
			t.Fatalf("Set failed: %v", err)
		}
	}
	if _, err := store.HSet(ctx, "h:1", "field", "value"); err != nil {
		t.Fatalf("HSet failed: %v", err)
	}
	if _, err := store.LPush(ctx, "l:1", "x", "y"); err != nil {
		t.Fatalf("LPush failed: %v", err)
	}

	var (
		cursor uint64
		keys   []string
	)
	for {
		batch, next, err := store.Scan(ctx, cursor, "*", 3)
		if err != nil {
			t.Fatalf("Scan failed: %v", err)
		}
		keys = append(keys, batch...)
		cursor = next
		if cursor == 0 {
			break
		}
	}

	sort.Strings(keys)
	if len(keys) != 12 {
		t.Fatalf("Scan returned %d keys, want 12 (%v)", len(keys), keys)
	}
	if !reflect.DeepEqual(keys[:2], []string{"h:1", "l:1"}) {
		t.Fatalf("Scan missing object keys, got %v", keys[:2])
	}
}

func TestStore_KeysInSlot_IncludesObjectKeys(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()

	store.SetSlotFunc(func(string) uint16 { return 7 })
	ctx := context.Background()
	if _, err := store.HSet(ctx, "hash-key", "field", "value"); err != nil {
		t.Fatalf("HSet failed: %v", err)
	}
	if _, err := store.LPush(ctx, "list-key", "a"); err != nil {
		t.Fatalf("LPush failed: %v", err)
	}

	keys := store.KeysInSlot(7, 10)
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, []string{"hash-key", "list-key"}) {
		t.Fatalf("KeysInSlot returned %v, want object keys", keys)
	}
	if count := store.CountKeysInSlot(7); count != 2 {
		t.Fatalf("CountKeysInSlot returned %d, want 2", count)
	}
}

func TestStore_ConcurrentTypeMutations_KeepSingleRepresentation(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()

	ctx := context.Background()
	tests := []struct {
		name   string
		object func() error
	}{
		{name: "hash", object: func() error { _, err := store.HSet(ctx, "shared", "field", "value"); return err }},
		{name: "list", object: func() error { _, err := store.LPush(ctx, "shared", "value"); return err }},
		{name: "set", object: func() error { _, err := store.SAdd(ctx, "shared", "value"); return err }},
		{name: "zset", object: func() error {
			_, err := store.ZAdd(ctx, "shared", engine.ZMember{Score: 1, Member: "value"})
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < 50; i++ {
				if _, err := store.Del(ctx, "shared"); err != nil {
					t.Fatalf("Del failed: %v", err)
				}

				var wg sync.WaitGroup
				wg.Add(2)
				go func() {
					defer wg.Done()
					_ = store.Set(ctx, "shared", "string", 0)
				}()
				go func() {
					defer wg.Done()
					_ = tt.object()
				}()
				wg.Wait()

				hasString := store.cache.Exists("shared")
				hasObject := store.hasObjectKey("shared")
				if hasString && hasObject {
					t.Fatalf("key has both string and object representations after concurrent mutation")
				}
			}
		})
	}
}
