package memory

import (
	"context"
	"reflect"
	"testing"

	pkgerrors "github.com/10yihang/autocache/pkg/errors"
)

func TestList_LPushRangePop(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()

	ctx := context.Background()
	length, err := store.LPush(ctx, "mylist", "b", "a")
	if err != nil {
		t.Fatalf("LPush failed: %v", err)
	}
	if length != 2 {
		t.Fatalf("LPush returned %d, want 2", length)
	}

	length, err = store.RPush(ctx, "mylist", "c")
	if err != nil {
		t.Fatalf("RPush failed: %v", err)
	}
	if length != 3 {
		t.Fatalf("RPush returned %d, want 3", length)
	}

	values, err := store.LRange(ctx, "mylist", 0, -1)
	if err != nil {
		t.Fatalf("LRange failed: %v", err)
	}
	if !reflect.DeepEqual(values, []string{"a", "b", "c"}) {
		t.Fatalf("LRange returned %v, want [a b c]", values)
	}

	left, err := store.LPop(ctx, "mylist")
	if err != nil {
		t.Fatalf("LPop failed: %v", err)
	}
	if left != "a" {
		t.Fatalf("LPop returned %q, want %q", left, "a")
	}

	right, err := store.RPop(ctx, "mylist")
	if err != nil {
		t.Fatalf("RPop failed: %v", err)
	}
	if right != "c" {
		t.Fatalf("RPop returned %q, want %q", right, "c")
	}
}

func TestList_WrongType(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()

	ctx := context.Background()
	if err := store.Set(ctx, "plain", "value", 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	if _, err := store.LPush(ctx, "plain", "x"); err == nil || err != pkgerrors.ErrWrongType {
		t.Fatalf("LPush on string key error = %v, want ErrWrongType", err)
	}
}
