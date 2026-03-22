package replication

import "testing"

func TestReplicaApplierOrdering(t *testing.T) {
	applier := NewReplicaApplier()

	skip, err := applier.Begin(Op{Slot: 1, Epoch: 2, LSN: 1})
	if err != nil || skip {
		t.Fatalf("first begin err=%v skip=%v", err, skip)
	}
	applier.Commit(Op{Slot: 1, Epoch: 2, LSN: 1})

	skip, err = applier.Begin(Op{Slot: 1, Epoch: 2, LSN: 1})
	if err != nil || !skip {
		t.Fatalf("duplicate begin err=%v skip=%v", err, skip)
	}

	if _, err := applier.Begin(Op{Slot: 1, Epoch: 2, LSN: 3}); err == nil {
		t.Fatal("expected gap error")
	}
}
