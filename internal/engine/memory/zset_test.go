package memory

import (
	"context"
	"reflect"
	"testing"

	"github.com/10yihang/autocache/internal/engine"
	pkgerrors "github.com/10yihang/autocache/pkg/errors"
)

func TestZSet_AddRangeScoreRankRemove(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()

	ctx := context.Background()
	added, err := store.ZAdd(ctx, "myzset", engine.ZMember{Score: 2, Member: "b"}, engine.ZMember{Score: 1, Member: "a"})
	if err != nil {
		t.Fatalf("ZAdd failed: %v", err)
	}
	if added != 2 {
		t.Fatalf("ZAdd returned %d, want 2", added)
	}

	values, err := store.ZRange(ctx, "myzset", 0, -1)
	if err != nil {
		t.Fatalf("ZRange failed: %v", err)
	}
	if !reflect.DeepEqual(values, []string{"a", "b"}) {
		t.Fatalf("ZRange returned %v, want [a b]", values)
	}

	score, err := store.ZScore(ctx, "myzset", "b")
	if err != nil {
		t.Fatalf("ZScore failed: %v", err)
	}
	if score != 2 {
		t.Fatalf("ZScore returned %v, want 2", score)
	}

	rank, err := store.ZRank(ctx, "myzset", "b")
	if err != nil {
		t.Fatalf("ZRank failed: %v", err)
	}
	if rank != 1 {
		t.Fatalf("ZRank returned %d, want 1", rank)
	}

	count, err := store.ZCard(ctx, "myzset")
	if err != nil {
		t.Fatalf("ZCard failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("ZCard returned %d, want 2", count)
	}

	removed, err := store.ZRem(ctx, "myzset", "a")
	if err != nil {
		t.Fatalf("ZRem failed: %v", err)
	}
	if removed != 1 {
		t.Fatalf("ZRem returned %d, want 1", removed)
	}
}

func TestZSet_WrongType(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()

	ctx := context.Background()
	if err := store.Set(ctx, "plain", "value", 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	if _, err := store.ZAdd(ctx, "plain", engine.ZMember{Score: 1, Member: "a"}); err == nil || err != pkgerrors.ErrWrongType {
		t.Fatalf("ZAdd on string key error = %v, want ErrWrongType", err)
	}
}
