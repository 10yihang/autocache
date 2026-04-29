package gossip

import "testing"

type testSlotAssigner struct {
	nodeSlots map[string][]uint16
	owners    map[uint16]string
	epochs    map[uint16]uint64
}

func (a *testSlotAssigner) GetNodeSlots(nodeID string) []uint16 {
	if slots, ok := a.nodeSlots[nodeID]; ok {
		cp := make([]uint16, len(slots))
		copy(cp, slots)
		return cp
	}
	return nil
}

func (a *testSlotAssigner) GetSlotNode(slot uint16) string {
	return a.owners[slot]
}

func (a *testSlotAssigner) GetSlotConfigEpoch(slot uint16) uint64 {
	if a.epochs == nil {
		return 0
	}
	return a.epochs[slot]
}

func (a *testSlotAssigner) AssignSlot(slot uint16, nodeID string) error {
	a.owners[slot] = nodeID
	return nil
}

func (a *testSlotAssigner) AddSlotReplica(slot uint16, replicaID string) {}

func TestBroadcastSlotOwnershipUnknownOwner(t *testing.T) {
	slots := &testSlotAssigner{
		nodeSlots: map[string][]uint16{},
		owners:    map[uint16]string{},
		epochs:    map[uint16]uint64{},
	}
	self := &GossipNode{ID: "node-1", IP: "127.0.0.1", Port: 6379, ClusterPort: 16379, FailReports: map[string]int64{}}
	g := NewGossip(self, slots)

	if err := g.BroadcastSlotOwnership(10, "node-2"); err == nil {
		t.Fatal("expected error for unknown owner node")
	}
}

func TestBroadcastSlotOwnershipSelfNodeWithoutPeers(t *testing.T) {
	slots := &testSlotAssigner{
		nodeSlots: map[string][]uint16{"node-1": {11}},
		owners:    map[uint16]string{11: "node-1"},
		epochs:    map[uint16]uint64{11: 3},
	}
	self := &GossipNode{ID: "node-1", IP: "127.0.0.1", Port: 6379, ClusterPort: 16379, FailReports: map[string]int64{}}
	g := NewGossip(self, slots)

	if err := g.BroadcastSlotOwnership(11, "node-1"); err != nil {
		t.Fatalf("BroadcastSlotOwnership failed: %v", err)
	}
}

func TestProcessNodeInfoRejectsStaleSlotOwnership(t *testing.T) {
	slots := &testSlotAssigner{
		nodeSlots: map[string][]uint16{"node-new": {12}},
		owners:    map[uint16]string{12: "node-new"},
		epochs:    map[uint16]uint64{12: 5},
	}
	self := &GossipNode{ID: "node-1", IP: "127.0.0.1", Port: 6379, ClusterPort: 16379, FailReports: map[string]int64{}}
	g := NewGossip(self, slots)

	g.processNodeInfo(&NodeInfo{
		ID:          "node-old",
		IP:          "127.0.0.1",
		Port:        6380,
		ClusterPort: 16380,
		Flags:       NodeFlagMaster,
		ConfigEpoch: 4,
		Slots:       SlotsToBytes([]uint16{12}),
	}, true)

	if got := slots.GetSlotNode(12); got != "node-new" {
		t.Fatalf("slot owner = %s, want node-new", got)
	}
}

func TestProcessNodeInfoAcceptsNewerSlotOwnership(t *testing.T) {
	slots := &testSlotAssigner{
		nodeSlots: map[string][]uint16{"node-old": {13}},
		owners:    map[uint16]string{13: "node-old"},
		epochs:    map[uint16]uint64{13: 2},
	}
	self := &GossipNode{ID: "node-1", IP: "127.0.0.1", Port: 6379, ClusterPort: 16379, FailReports: map[string]int64{}}
	g := NewGossip(self, slots)

	g.processNodeInfo(&NodeInfo{
		ID:          "node-new",
		IP:          "127.0.0.1",
		Port:        6381,
		ClusterPort: 16381,
		Flags:       NodeFlagMaster,
		ConfigEpoch: 3,
		Slots:       SlotsToBytes([]uint16{13}),
	}, true)

	if got := slots.GetSlotNode(13); got != "node-new" {
		t.Fatalf("slot owner = %s, want node-new", got)
	}
}
