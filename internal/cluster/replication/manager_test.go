package replication

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestManagerApplyWriteAppendsReplicationOp(t *testing.T) {
	store := NewLogStore(8)
	allocatorCalls := 0
	coordinator := NewManager(store, func(slot uint16) (uint64, uint64, error) {
		allocatorCalls++
		if slot != 101 {
			t.Fatalf("slot = %d, want 101", slot)
		}
		return 3, 1, nil
	})

	applyCalled := false
	op, err := coordinator.ApplyWrite(context.Background(), WriteRequest{
		Slot:       101,
		OpType:     "SET",
		Key:        "{user:1}:name",
		Payload:    []byte("alice"),
		ExpireAtNs: 42,
		Apply: func() error {
			applyCalled = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("ApplyWrite failed: %v", err)
	}
	if !applyCalled {
		t.Fatal("expected apply callback to be called")
	}
	if allocatorCalls != 1 {
		t.Fatalf("allocator calls = %d, want 1", allocatorCalls)
	}
	if op.Epoch != 3 || op.LSN != 1 {
		t.Fatalf("op epoch/lsn = (%d,%d), want (3,1)", op.Epoch, op.LSN)
	}
	if op.OpType != "SET" || op.Key != "{user:1}:name" || string(op.Payload) != "alice" {
		t.Fatalf("unexpected op contents: %+v", op)
	}

	ops := store.ListAfter(101, 0)
	if len(ops) != 1 {
		t.Fatalf("backlog length = %d, want 1", len(ops))
	}
	if ops[0].LSN != 1 {
		t.Fatalf("backlog lsn = %d, want 1", ops[0].LSN)
	}
}

func TestManagerApplyWriteSkipsLogOnApplyFailure(t *testing.T) {
	store := NewLogStore(8)
	allocatorCalls := 0
	coordinator := NewManager(store, func(_ uint16) (uint64, uint64, error) {
		allocatorCalls++
		return 1, 1, nil
	})

	errBoom := errors.New("boom")
	_, err := coordinator.ApplyWrite(context.Background(), WriteRequest{
		Slot:   7,
		OpType: "DEL",
		Key:    "k",
		Apply: func() error {
			return errBoom
		},
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("ApplyWrite error = %v, want %v", err, errBoom)
	}
	if allocatorCalls != 0 {
		t.Fatalf("allocator calls = %d, want 0", allocatorCalls)
	}
	if got := store.ListAfter(7, 0); len(got) != 0 {
		t.Fatalf("backlog length = %d, want 0", len(got))
	}
}

func TestManagerDispatchesOpsToReplicaQueuesInOrder(t *testing.T) {
	store := NewLogStore(16)
	var nextLSN uint64
	mgr := NewManager(store, func(_ uint16) (uint64, uint64, error) {
		nextLSN++
		return 1, nextLSN, nil
	})
	defer mgr.Close()

	mgr.SetReplicaResolver(func(_ uint16) []PeerTarget {
		return []PeerTarget{{NodeID: "r1", Addr: "addr-1"}, {NodeID: "r2", Addr: "addr-2"}}
	})

	var mu sync.Mutex
	received := map[string][]uint64{}
	mgr.SetStreamSender(func(_ context.Context, addr string, op Op) error {
		mu.Lock()
		received[addr] = append(received[addr], op.LSN)
		mu.Unlock()
		return nil
	})

	for i := 0; i < 2; i++ {
		_, err := mgr.ApplyWrite(context.Background(), WriteRequest{
			Slot:   12,
			OpType: "SET",
			Key:    "k",
			Apply:  func() error { return nil },
		})
		if err != nil {
			t.Fatalf("ApplyWrite failed: %v", err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		got1 := len(received["addr-1"])
		got2 := len(received["addr-2"])
		mu.Unlock()
		if got1 == 2 && got2 == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for replica dispatch, got addr-1=%d addr-2=%d", got1, got2)
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if received["addr-1"][0] != 1 || received["addr-1"][1] != 2 {
		t.Fatalf("addr-1 lsn order = %v, want [1 2]", received["addr-1"])
	}
	if received["addr-2"][0] != 1 || received["addr-2"][1] != 2 {
		t.Fatalf("addr-2 lsn order = %v, want [1 2]", received["addr-2"])
	}
	if got := mgr.ReplicaProgress(12, "r1"); got != 2 {
		t.Fatalf("replica progress r1 = %d, want 2", got)
	}
	if got := mgr.ReplicaProgress(12, "r2"); got != 2 {
		t.Fatalf("replica progress r2 = %d, want 2", got)
	}
}

func TestManagerIncrementalOpsFromAppliedLSN(t *testing.T) {
	store := NewLogStore(8)
	var nextLSN uint64
	mgr := NewManager(store, func(_ uint16) (uint64, uint64, error) {
		nextLSN++
		return 1, nextLSN, nil
	})
	defer mgr.Close()

	for _, key := range []string{"k1", "k2", "k3"} {
		_, err := mgr.ApplyWrite(context.Background(), WriteRequest{
			Slot:   4,
			OpType: "SET",
			Key:    key,
			Apply:  func() error { return nil },
		})
		if err != nil {
			t.Fatalf("ApplyWrite failed: %v", err)
		}
	}

	ops, ok := mgr.IncrementalOps(4, 1)
	if !ok {
		t.Fatal("expected incremental ops to be available")
	}
	if len(ops) != 2 {
		t.Fatalf("ops len = %d, want 2", len(ops))
	}
	if ops[0].LSN != 2 || ops[1].LSN != 3 {
		t.Fatalf("unexpected lsn sequence: [%d,%d]", ops[0].LSN, ops[1].LSN)
	}
}

func TestManagerNeedsFullSyncWhenBacklogMissed(t *testing.T) {
	store := NewLogStore(2)
	var nextLSN uint64
	mgr := NewManager(store, func(_ uint16) (uint64, uint64, error) {
		nextLSN++
		return 1, nextLSN, nil
	})
	defer mgr.Close()

	for _, key := range []string{"k1", "k2", "k3"} {
		_, err := mgr.ApplyWrite(context.Background(), WriteRequest{
			Slot:   6,
			OpType: "SET",
			Key:    key,
			Apply:  func() error { return nil },
		})
		if err != nil {
			t.Fatalf("ApplyWrite failed: %v", err)
		}
	}

	if !mgr.NeedsFullSync(6, 0) {
		t.Fatal("expected full sync requirement for missing backlog start")
	}
	snapshotLSN, ok := mgr.SnapshotLSN(6)
	if !ok || snapshotLSN != 3 {
		t.Fatalf("snapshot lsn = %d, ok=%v, want 3,true", snapshotLSN, ok)
	}
	post := mgr.PostSnapshotOps(6, 2)
	if len(post) != 1 || post[0].LSN != 3 {
		t.Fatalf("post snapshot ops = %+v, want lsn3", post)
	}
}
