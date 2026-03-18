package memory

import (
	"context"
	"reflect"
	"sort"
	"testing"

	pkgerrors "github.com/10yihang/autocache/pkg/errors"
)

func TestSet_AddMembersAndQuery(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()

	ctx := context.Background()
	added, err := store.SAdd(ctx, "myset", "b", "a", "b")
	if err != nil {
		t.Fatalf("SAdd failed: %v", err)
	}
	if added != 2 {
		t.Fatalf("SAdd returned %d, want 2", added)
	}

	exists, err := store.SIsMember(ctx, "myset", "a")
	if err != nil {
		t.Fatalf("SIsMember failed: %v", err)
	}
	if !exists {
		t.Fatal("SIsMember returned false, want true")
	}

	members, err := store.SMembers(ctx, "myset")
	if err != nil {
		t.Fatalf("SMembers failed: %v", err)
	}
	sort.Strings(members)
	if !reflect.DeepEqual(members, []string{"a", "b"}) {
		t.Fatalf("SMembers returned %v, want [a b]", members)
	}

	count, err := store.SCard(ctx, "myset")
	if err != nil {
		t.Fatalf("SCard failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("SCard returned %d, want 2", count)
	}

	removed, err := store.SRem(ctx, "myset", "a")
	if err != nil {
		t.Fatalf("SRem failed: %v", err)
	}
	if removed != 1 {
		t.Fatalf("SRem returned %d, want 1", removed)
	}
}

func TestSet_WrongType(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()

	ctx := context.Background()
	if err := store.Set(ctx, "plain", "value", 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	if _, err := store.SAdd(ctx, "plain", "x"); err == nil || err != pkgerrors.ErrWrongType {
		t.Fatalf("SAdd on string key error = %v, want ErrWrongType", err)
	}
}
