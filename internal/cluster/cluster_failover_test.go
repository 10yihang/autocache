package cluster

import "testing"

func TestBroadcastSlotOwnershipChangeReturnsErrorForUnknownOwner(t *testing.T) {
	c, err := NewCluster(&Config{NodeID: "node-1", BindAddr: "127.0.0.1", Port: 6379, ClusterPort: 16379}, nil)
	if err != nil {
		t.Fatalf("NewCluster failed: %v", err)
	}

	if err := c.BroadcastSlotOwnershipChange(5, "node-unknown"); err == nil {
		t.Fatal("expected error for unknown new primary")
	}
}

func TestBroadcastSlotOwnershipChangeSelfOwner(t *testing.T) {
	c, err := NewCluster(&Config{NodeID: "node-1", BindAddr: "127.0.0.1", Port: 6379, ClusterPort: 16379}, nil)
	if err != nil {
		t.Fatalf("NewCluster failed: %v", err)
	}
	if err := c.GetSlotManager().AssignSlot(7, "node-1"); err != nil {
		t.Fatalf("AssignSlot failed: %v", err)
	}

	if err := c.BroadcastSlotOwnershipChange(7, "node-1"); err != nil {
		t.Fatalf("BroadcastSlotOwnershipChange failed: %v", err)
	}
}
