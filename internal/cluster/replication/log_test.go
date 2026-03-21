package replication

import "testing"

func TestLogStoreAppendAndListAfter(t *testing.T) {
	store := NewLogStore(16)

	op1 := Op{Slot: 1, Epoch: 2, LSN: 1, OpType: "SET", Key: "k1"}
	op2 := Op{Slot: 1, Epoch: 2, LSN: 2, OpType: "DEL", Key: "k1"}

	if err := store.Append(op1); err != nil {
		t.Fatalf("Append op1 failed: %v", err)
	}
	if err := store.Append(op2); err != nil {
		t.Fatalf("Append op2 failed: %v", err)
	}

	ops := store.ListAfter(1, 1)
	if len(ops) != 1 {
		t.Fatalf("ListAfter returned %d ops, want 1", len(ops))
	}
	if ops[0].LSN != 2 {
		t.Fatalf("returned lsn = %d, want 2", ops[0].LSN)
	}
}

func TestLogStoreRejectsOutOfOrderAppend(t *testing.T) {
	store := NewLogStore(16)

	if err := store.Append(Op{Slot: 1, Epoch: 1, LSN: 2, OpType: "SET", Key: "k"}); err == nil {
		t.Fatal("expected out-of-order append to fail")
	}
}

func TestAckTrackerTracksReplicaProgress(t *testing.T) {
	tracker := NewAckTracker()

	tracker.Track(ReplicaAck{Slot: 5, Epoch: 3, NodeID: "node2", AppliedLSN: 9})
	tracker.Track(ReplicaAck{Slot: 5, Epoch: 3, NodeID: "node2", AppliedLSN: 7})
	tracker.Track(ReplicaAck{Slot: 5, Epoch: 3, NodeID: "node3", AppliedLSN: 8})

	if got := tracker.Get(5, "node2"); got != 9 {
		t.Fatalf("Get(node2) = %d, want 9", got)
	}

	acks := tracker.Snapshot(5)
	if len(acks) != 2 {
		t.Fatalf("Snapshot length = %d, want 2", len(acks))
	}
}

func TestLogStoreCanReplayFromLSN(t *testing.T) {
	store := NewLogStore(2)

	if err := store.Append(Op{Slot: 9, Epoch: 1, LSN: 1, OpType: "SET", Key: "k1"}); err != nil {
		t.Fatalf("append #1 failed: %v", err)
	}
	if err := store.Append(Op{Slot: 9, Epoch: 1, LSN: 2, OpType: "SET", Key: "k2"}); err != nil {
		t.Fatalf("append #2 failed: %v", err)
	}
	if err := store.Append(Op{Slot: 9, Epoch: 1, LSN: 3, OpType: "SET", Key: "k3"}); err != nil {
		t.Fatalf("append #3 failed: %v", err)
	}

	if !store.CanReplayFromLSN(9, 1) {
		t.Fatal("expected replay from lsn=1 to be possible")
	}
	if store.CanReplayFromLSN(9, 0) {
		t.Fatal("expected replay from lsn=0 to be impossible after truncation")
	}
	if !store.CanReplayFromLSN(9, 3) {
		t.Fatal("expected replay from lsn=3 to be possible")
	}
	if store.CanReplayFromLSN(9, 4) {
		t.Fatal("expected replay from future lsn to be impossible")
	}
}
