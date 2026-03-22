package replication

import (
	"context"
	"strings"
	"testing"

	"github.com/10yihang/autocache/internal/cluster/hash"
	"github.com/10yihang/autocache/internal/engine/memory"
)

func TestManagerFullSyncSlotRestoresAndReplaysPostSnapshotOps(t *testing.T) {
	slot := hash.KeySlot("{fullsync}:k")
	source := memory.NewStore(nil)
	target := memory.NewStore(nil)

	if err := source.Set(context.Background(), "{fullsync}:k", "v1", 0); err != nil {
		t.Fatalf("source set failed: %v", err)
	}
	if err := target.Set(context.Background(), "{fullsync}:stale", "old", 0); err != nil {
		t.Fatalf("target set failed: %v", err)
	}

	mgr := NewManager(NewLogStore(8), nil)
	defer mgr.Close()
	if err := mgr.LogStore().Append(Op{Slot: slot, Epoch: 1, LSN: 1, OpType: "SET", Key: "{fullsync}:k", Payload: []byte("{fullsync}:k\x00v1")}); err != nil {
		t.Fatalf("append lsn1 failed: %v", err)
	}
	if err := mgr.LogStore().Append(Op{Slot: slot, Epoch: 1, LSN: 2, OpType: "SET", Key: "{fullsync}:k", Payload: []byte("{fullsync}:k\x00v2")}); err != nil {
		t.Fatalf("append lsn2 failed: %v", err)
	}

	apply := func(op Op) error {
		if op.OpType != "SET" {
			return nil
		}
		parts := strings.Split(string(op.Payload), "\x00")
		if len(parts) < 2 {
			return nil
		}
		return target.Set(context.Background(), op.Key, parts[1], 0)
	}

	if err := mgr.FullSyncSlot(context.Background(), slot, 1, source, target, apply); err != nil {
		t.Fatalf("FullSyncSlot failed: %v", err)
	}

	v, err := target.GetBytes(context.Background(), "{fullsync}:k")
	if err != nil {
		t.Fatalf("target get failed: %v", err)
	}
	if string(v) != "v2" {
		t.Fatalf("target value = %q, want v2", string(v))
	}
	if _, err := target.GetBytes(context.Background(), "{fullsync}:stale"); err == nil {
		t.Fatal("expected stale key to be cleared during full sync")
	}
}
