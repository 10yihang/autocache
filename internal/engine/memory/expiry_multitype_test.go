package memory

import (
	"context"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/engine"
)

func TestExpiry_MultiTypeExpirePersistRenameDel(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()

	ctx := context.Background()
	if _, err := store.HSet(ctx, "hash", "field", "value"); err != nil {
		t.Fatalf("HSet failed: %v", err)
	}
	if _, err := store.LPush(ctx, "list", "a", "b"); err != nil {
		t.Fatalf("LPush failed: %v", err)
	}
	if _, err := store.SAdd(ctx, "set", "x", "y"); err != nil {
		t.Fatalf("SAdd failed: %v", err)
	}
	if _, err := store.ZAdd(ctx, "zset", engine.ZMember{Score: 1, Member: "m1"}); err != nil {
		t.Fatalf("ZAdd failed: %v", err)
	}

	if ok, err := store.Expire(ctx, "hash", 250*time.Millisecond); err != nil || !ok {
		t.Fatalf("Expire hash = (%v,%v), want (true,nil)", ok, err)
	}
	if ok, err := store.Expire(ctx, "list", 250*time.Millisecond); err != nil || !ok {
		t.Fatalf("Expire list = (%v,%v), want (true,nil)", ok, err)
	}
	if ok, err := store.Expire(ctx, "set", 250*time.Millisecond); err != nil || !ok {
		t.Fatalf("Expire set = (%v,%v), want (true,nil)", ok, err)
	}
	if ok, err := store.Expire(ctx, "zset", 250*time.Millisecond); err != nil || !ok {
		t.Fatalf("Expire zset = (%v,%v), want (true,nil)", ok, err)
	}

	if ok, err := store.Persist(ctx, "set"); err != nil || !ok {
		t.Fatalf("Persist set = (%v,%v), want (true,nil)", ok, err)
	}
	if ttl, err := store.TTL(ctx, "set"); err != nil || ttl != -1 {
		t.Fatalf("TTL(set) = (%v,%v), want (-1,nil)", ttl, err)
	}

	if err := store.Rename(ctx, "zset", "zset-renamed"); err != nil {
		t.Fatalf("Rename failed: %v", err)
	}
	if ttl, err := store.TTL(ctx, "zset-renamed"); err != nil || ttl <= 0 {
		t.Fatalf("TTL(zset-renamed) = (%v,%v), want positive TTL", ttl, err)
	}

	deleted, err := store.Del(ctx, "list")
	if err != nil || deleted != 1 {
		t.Fatalf("Del(list) = (%d,%v), want (1,nil)", deleted, err)
	}
	if length, err := store.LLen(ctx, "list"); err != nil || length != 0 {
		t.Fatalf("LLen(list after DEL) = (%d,%v), want (0,nil)", length, err)
	}

	time.Sleep(450 * time.Millisecond)
	if count, err := store.Exists(ctx, "hash", "zset-renamed", "set"); err != nil {
		t.Fatalf("Exists failed: %v", err)
	} else if count != 1 {
		t.Fatalf("Exists after expiry returned %d, want 1 surviving key", count)
	}

	size, err := store.DBSize(ctx)
	if err != nil {
		t.Fatalf("DBSize failed: %v", err)
	}
	if size != 1 {
		t.Fatalf("DBSize after expiry = %d, want 1", size)
	}
}
