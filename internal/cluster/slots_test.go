package cluster

import (
	"testing"

	"github.com/10yihang/autocache/internal/cluster/hash"
)

func TestSlotManager_AssignSlot(t *testing.T) {
	sm := NewSlotManager()

	err := sm.AssignSlot(0, "node1")
	if err != nil {
		t.Fatalf("AssignSlot failed: %v", err)
	}

	nodeID := sm.GetSlotNode(0)
	if nodeID != "node1" {
		t.Errorf("GetSlotNode(0) = %q, want %q", nodeID, "node1")
	}
}

func TestSlotManager_AssignSlotRange(t *testing.T) {
	sm := NewSlotManager()

	err := sm.AssignSlotRange(0, 100, "node1")
	if err != nil {
		t.Fatalf("AssignSlotRange failed: %v", err)
	}

	for i := uint16(0); i <= 100; i++ {
		if sm.GetSlotNode(i) != "node1" {
			t.Errorf("slot %d should belong to node1", i)
		}
	}

	if sm.GetSlotNode(101) != "" {
		t.Errorf("slot 101 should be unassigned")
	}
}

func TestSlotManager_GetNodeSlots(t *testing.T) {
	sm := NewSlotManager()

	sm.AssignSlot(10, "node1")
	sm.AssignSlot(20, "node1")
	sm.AssignSlot(30, "node2")

	slots := sm.GetNodeSlots("node1")
	if len(slots) != 2 {
		t.Errorf("node1 should have 2 slots, got %d", len(slots))
	}

	slots2 := sm.GetNodeSlots("node2")
	if len(slots2) != 1 {
		t.Errorf("node2 should have 1 slot, got %d", len(slots2))
	}
}

func TestSlotManager_CountAssigned(t *testing.T) {
	sm := NewSlotManager()

	if sm.CountAssigned() != 0 {
		t.Errorf("initial count should be 0")
	}

	sm.AssignSlotRange(0, 99, "node1")

	if sm.CountAssigned() != 100 {
		t.Errorf("count should be 100, got %d", sm.CountAssigned())
	}
}

func TestSlotManager_InvalidSlot(t *testing.T) {
	sm := NewSlotManager()

	err := sm.AssignSlot(hash.SlotCount, "node1")
	if err == nil {
		t.Errorf("should fail for invalid slot")
	}

	nodeID := sm.GetSlotNode(hash.SlotCount)
	if nodeID != "" {
		t.Errorf("invalid slot should return empty string")
	}
}

func TestSlotManager_Reassign(t *testing.T) {
	sm := NewSlotManager()

	sm.AssignSlot(0, "node1")
	sm.AssignSlot(0, "node2")

	if sm.GetSlotNode(0) != "node2" {
		t.Errorf("slot 0 should now belong to node2")
	}

	slots1 := sm.GetNodeSlots("node1")
	if len(slots1) != 0 {
		t.Errorf("node1 should have 0 slots after reassign")
	}
}

func TestSlotManager_GetKeyNode(t *testing.T) {
	sm := NewSlotManager()

	for i := uint16(0); i < hash.SlotCount; i++ {
		sm.AssignSlot(i, "node1")
	}

	nodeID := sm.GetKeyNode("foo")
	if nodeID != "node1" {
		t.Errorf("GetKeyNode should return node1")
	}
}

func TestSlotManager_GetClusterSlots(t *testing.T) {
	sm := NewSlotManager()

	sm.AssignSlotRange(0, 5460, "node1")
	sm.AssignSlotRange(5461, 10922, "node2")
	sm.AssignSlotRange(10923, 16383, "node3")

	ranges := sm.GetClusterSlots()
	if len(ranges) != 3 {
		t.Errorf("should have 3 ranges, got %d", len(ranges))
	}
}
