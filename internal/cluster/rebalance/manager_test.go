package rebalance

import (
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/cluster/hotspot"
)

type fakeHotSlotSource struct {
	hot      []hotspot.HotSlotInfo
	totalQPS float64
}

func (s *fakeHotSlotSource) HotSlots() []hotspot.HotSlotInfo {
	out := make([]hotspot.HotSlotInfo, len(s.hot))
	copy(out, s.hot)
	return out
}

func (s *fakeHotSlotSource) TotalQPS() float64 {
	return s.totalQPS
}

type fakeClusterOps struct {
	local map[uint16]bool
	peers []PeerLoad
	moves []migrationMove
}

type migrationMove struct {
	slot   uint16
	target string
}

func (c *fakeClusterOps) GetNodeID() string { return "local" }

func (c *fakeClusterOps) GetSlotOwner(slot uint16) string {
	if c.local[slot] {
		return "local"
	}
	return "remote"
}

func (c *fakeClusterOps) IsSlotLocal(slot uint16) bool { return c.local[slot] }

func (c *fakeClusterOps) GetPeerLoads() []PeerLoad {
	out := make([]PeerLoad, len(c.peers))
	copy(out, c.peers)
	return out
}

func (c *fakeClusterOps) MigrateSlot(slot uint16, targetNodeID string) error {
	c.moves = append(c.moves, migrationMove{slot: slot, target: targetNodeID})
	return nil
}

func TestManagerSkipsMigrationWhenTargetWouldBeMoreLoaded(t *testing.T) {
	cluster := &fakeClusterOps{
		local: map[uint16]bool{42: true},
		peers: []PeerLoad{
			{NodeID: "peer-busy", TotalQPS: 1900},
		},
	}
	source := &fakeHotSlotSource{
		totalQPS: 2000,
		hot: []hotspot.HotSlotInfo{
			{Slot: 42, QPS: 900, Score: 9},
		},
	}
	cfg := Config{
		CheckInterval:                time.Hour,
		MinStickyWindows:             1,
		MinScoreRatio:                5,
		MaxMigrationsPerRun:          1,
		CoolDownSlots:                1,
		MaxTargetProjectedLoadRatio:  1.0,
		MinProjectedLoadReductionQPS: 1,
	}

	manager := New(cluster, source, cfg)
	manager.Stop()
	manager.tick()

	if len(cluster.moves) != 0 {
		t.Fatalf("moves = %#v, want none because target projected load is worse than local", cluster.moves)
	}
}

func TestManagerContinuesBalancingMoreHotSlotsThanPeers(t *testing.T) {
	cluster := &fakeClusterOps{
		local: map[uint16]bool{10: true, 11: true, 12: true},
		peers: []PeerLoad{
			{NodeID: "peer-a", TotalQPS: 100},
			{NodeID: "peer-b", TotalQPS: 100},
		},
	}
	source := &fakeHotSlotSource{
		totalQPS: 2000,
		hot: []hotspot.HotSlotInfo{
			{Slot: 10, QPS: 200, Score: 10},
			{Slot: 11, QPS: 200, Score: 9},
			{Slot: 12, QPS: 200, Score: 8},
		},
	}
	cfg := Config{
		CheckInterval:                time.Hour,
		MinStickyWindows:             1,
		MinScoreRatio:                5,
		MaxMigrationsPerRun:          3,
		CoolDownSlots:                1,
		MaxTargetProjectedLoadRatio:  1.0,
		MinProjectedLoadReductionQPS: 1,
	}

	manager := New(cluster, source, cfg)
	manager.Stop()
	manager.tick()

	if len(cluster.moves) != 3 {
		t.Fatalf("move count = %d, want 3 moves distributed with projected load", len(cluster.moves))
	}
	targets := map[string]int{}
	for _, move := range cluster.moves {
		targets[move.target]++
	}
	if targets["peer-a"] == 0 || targets["peer-b"] == 0 {
		t.Fatalf("targets = %#v, want both peers used", targets)
	}
}
