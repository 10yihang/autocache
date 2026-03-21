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

func TestSlotManager_AssignSlotSetsPrimary(t *testing.T) {
	sm := NewSlotManager()

	if err := sm.AssignSlot(7, "node1"); err != nil {
		t.Fatalf("AssignSlot failed: %v", err)
	}

	info := sm.GetSlotInfo(7)
	if info == nil {
		t.Fatal("GetSlotInfo returned nil")
	}
	if info.PrimaryID != "node1" {
		t.Fatalf("PrimaryID = %q, want %q", info.PrimaryID, "node1")
	}
	if info.ConfigEpoch != 0 {
		t.Fatalf("ConfigEpoch = %d, want 0", info.ConfigEpoch)
	}
	if info.NextLSN != 0 {
		t.Fatalf("NextLSN = %d, want 0", info.NextLSN)
	}
}

func TestSlotManager_ConfigureReplication(t *testing.T) {
	sm := NewSlotManager()

	if err := sm.AssignSlot(9, "node1"); err != nil {
		t.Fatalf("AssignSlot failed: %v", err)
	}
	if err := sm.ConfigureReplication(9, "node1", []string{"node2", "node3"}, 3); err != nil {
		t.Fatalf("ConfigureReplication failed: %v", err)
	}

	info := sm.GetSlotInfo(9)
	if info.PrimaryID != "node1" {
		t.Fatalf("PrimaryID = %q, want %q", info.PrimaryID, "node1")
	}
	if info.ConfigEpoch != 3 {
		t.Fatalf("ConfigEpoch = %d, want 3", info.ConfigEpoch)
	}
	if len(info.Replicas) != 2 {
		t.Fatalf("replica count = %d, want 2", len(info.Replicas))
	}
	if info.Replicas[0].NodeID != "node2" || info.Replicas[1].NodeID != "node3" {
		t.Fatalf("replicas = %#v, want node2,node3", info.Replicas)
	}
}

func TestSlotManager_AllocateLSNAndTrackReplicaAck(t *testing.T) {
	sm := NewSlotManager()

	if err := sm.AssignSlot(11, "node1"); err != nil {
		t.Fatalf("AssignSlot failed: %v", err)
	}
	if err := sm.ConfigureReplication(11, "node1", []string{"node2"}, 2); err != nil {
		t.Fatalf("ConfigureReplication failed: %v", err)
	}

	epoch, lsn, err := sm.AllocateLSN(11)
	if err != nil {
		t.Fatalf("AllocateLSN failed: %v", err)
	}
	if epoch != 2 || lsn != 1 {
		t.Fatalf("first allocation = (%d,%d), want (2,1)", epoch, lsn)
	}

	_, lsn, err = sm.AllocateLSN(11)
	if err != nil {
		t.Fatalf("AllocateLSN second call failed: %v", err)
	}
	if lsn != 2 {
		t.Fatalf("second lsn = %d, want 2", lsn)
	}

	if err := sm.UpdateReplicaLSN(11, "node2", 2); err != nil {
		t.Fatalf("UpdateReplicaLSN failed: %v", err)
	}

	info := sm.GetSlotInfo(11)
	if info.Replicas[0].MatchLSN != 2 {
		t.Fatalf("replica MatchLSN = %d, want 2", info.Replicas[0].MatchLSN)
	}
}

func TestSlotManager_PromoteReplica(t *testing.T) {
	sm := NewSlotManager()

	if err := sm.AssignSlot(13, "node1"); err != nil {
		t.Fatalf("AssignSlot failed: %v", err)
	}
	if err := sm.ConfigureReplication(13, "node1", []string{"node2", "node3"}, 4); err != nil {
		t.Fatalf("ConfigureReplication failed: %v", err)
	}
	if err := sm.UpdateReplicaLSN(13, "node3", 8); err != nil {
		t.Fatalf("UpdateReplicaLSN failed: %v", err)
	}

	if err := sm.PromoteReplica(13, "node3"); err != nil {
		t.Fatalf("PromoteReplica failed: %v", err)
	}

	info := sm.GetSlotInfo(13)
	if info.PrimaryID != "node3" {
		t.Fatalf("PrimaryID = %q, want %q", info.PrimaryID, "node3")
	}
	if info.NodeID != "node3" {
		t.Fatalf("NodeID = %q, want %q", info.NodeID, "node3")
	}
	if info.ConfigEpoch != 5 {
		t.Fatalf("ConfigEpoch = %d, want 5", info.ConfigEpoch)
	}
	foundOldPrimary := false
	for _, replica := range info.Replicas {
		if replica.NodeID == "node1" {
			foundOldPrimary = true
		}
	}
	if !foundOldPrimary {
		t.Fatal("expected old primary to remain in replica set after promotion")
	}
}
