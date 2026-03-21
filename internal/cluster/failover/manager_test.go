package failover

import (
	"testing"

	"github.com/10yihang/autocache/internal/cluster"
)

type testOwnershipBroadcaster struct {
	called     bool
	slot       uint16
	newPrimary string
}

func (b *testOwnershipBroadcaster) BroadcastSlotOwnershipChange(slot uint16, newPrimary string) error {
	b.called = true
	b.slot = slot
	b.newPrimary = newPrimary
	return nil
}

func TestManagerPromoteBestReplica(t *testing.T) {
	slots := cluster.NewSlotManager()
	if err := slots.AssignSlot(10, "node1"); err != nil {
		t.Fatalf("AssignSlot failed: %v", err)
	}
	if err := slots.ConfigureReplication(10, "node1", []string{"node2", "node3"}, 3); err != nil {
		t.Fatalf("ConfigureReplication failed: %v", err)
	}
	if err := slots.UpdateReplicaLSN(10, "node2", 8); err != nil {
		t.Fatalf("UpdateReplicaLSN node2 failed: %v", err)
	}
	if err := slots.UpdateReplicaLSN(10, "node3", 6); err != nil {
		t.Fatalf("UpdateReplicaLSN node3 failed: %v", err)
	}

	mgr := NewManager(slots)
	promoted, err := mgr.PromoteBestReplica(10)
	if err != nil {
		t.Fatalf("PromoteBestReplica failed: %v", err)
	}
	if promoted != "node2" {
		t.Fatalf("promoted = %s, want node2", promoted)
	}

	info := slots.GetSlotInfo(10)
	if info.PrimaryID != "node2" {
		t.Fatalf("primary = %s, want node2", info.PrimaryID)
	}
	if !mgr.CanNodeServeWrites(10, "node2") {
		t.Fatal("expected promoted node to serve writes")
	}
	if mgr.CanNodeServeWrites(10, "node1") {
		t.Fatal("expected stale primary to fail closed")
	}
}

func TestManagerInvokesBroadcastAndFailClosedHooks(t *testing.T) {
	slots := cluster.NewSlotManager()
	if err := slots.AssignSlot(8, "node1"); err != nil {
		t.Fatalf("AssignSlot failed: %v", err)
	}
	if err := slots.ConfigureReplication(8, "node1", []string{"node2"}, 5); err != nil {
		t.Fatalf("ConfigureReplication failed: %v", err)
	}
	if err := slots.UpdateReplicaLSN(8, "node2", 9); err != nil {
		t.Fatalf("UpdateReplicaLSN failed: %v", err)
	}

	mgr := NewManager(slots)
	broadcastCalled := false
	failClosedCalled := false
	mgr.SetBroadcastHook(func(slot uint16, newPrimary string) {
		if slot == 8 && newPrimary == "node2" {
			broadcastCalled = true
		}
	})
	mgr.SetFailClosedHook(func(slot uint16, stalePrimary string) {
		if slot == 8 && stalePrimary == "node1" {
			failClosedCalled = true
		}
	})

	if _, err := mgr.PromoteBestReplica(8); err != nil {
		t.Fatalf("PromoteBestReplica failed: %v", err)
	}
	if !broadcastCalled {
		t.Fatal("expected broadcast hook to be invoked")
	}
	if !failClosedCalled {
		t.Fatal("expected fail-closed hook to be invoked")
	}
}

func TestManagerBindOwnershipBroadcaster(t *testing.T) {
	slots := cluster.NewSlotManager()
	if err := slots.AssignSlot(3, "node1"); err != nil {
		t.Fatalf("AssignSlot failed: %v", err)
	}
	if err := slots.ConfigureReplication(3, "node1", []string{"node2"}, 1); err != nil {
		t.Fatalf("ConfigureReplication failed: %v", err)
	}
	if err := slots.UpdateReplicaLSN(3, "node2", 7); err != nil {
		t.Fatalf("UpdateReplicaLSN failed: %v", err)
	}

	mgr := NewManager(slots)
	b := &testOwnershipBroadcaster{}
	mgr.BindOwnershipBroadcaster(b)

	if _, err := mgr.PromoteBestReplica(3); err != nil {
		t.Fatalf("PromoteBestReplica failed: %v", err)
	}
	if !b.called {
		t.Fatal("expected ownership broadcaster to be called")
	}
	if b.slot != 3 || b.newPrimary != "node2" {
		t.Fatalf("unexpected broadcast payload: slot=%d primary=%s", b.slot, b.newPrimary)
	}
}

func TestNewManagerForClusterBindsBroadcaster(t *testing.T) {
	c, err := cluster.NewCluster(&cluster.Config{NodeID: "node-1", BindAddr: "127.0.0.1", Port: 6379, ClusterPort: 16379}, nil)
	if err != nil {
		t.Fatalf("NewCluster failed: %v", err)
	}

	slots := c.GetSlotManager()
	if err := slots.AssignSlot(12, "node-primary"); err != nil {
		t.Fatalf("AssignSlot failed: %v", err)
	}
	if err := slots.ConfigureReplication(12, "node-primary", []string{"node-1"}, 2); err != nil {
		t.Fatalf("ConfigureReplication failed: %v", err)
	}
	if err := slots.UpdateReplicaLSN(12, "node-1", 10); err != nil {
		t.Fatalf("UpdateReplicaLSN failed: %v", err)
	}

	mgr := NewManagerForCluster(c)
	promoted, err := mgr.PromoteBestReplica(12)
	if err != nil {
		t.Fatalf("PromoteBestReplica failed: %v", err)
	}
	if promoted != "node-1" {
		t.Fatalf("promoted = %s, want node-1", promoted)
	}
}
