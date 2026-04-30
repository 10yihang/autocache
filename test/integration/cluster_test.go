package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/cluster"
)

func startNode(t *testing.T, id string, port int, seeds []string) *cluster.Cluster {
	cfg := &cluster.Config{
		NodeID:      id,
		BindAddr:    "127.0.0.1",
		Port:        port,
		ClusterPort: port + 10000, // e.g. 6379 -> 16379
		Seeds:       seeds,
	}

	c, err := cluster.NewCluster(cfg, nil)
	if err != nil {
		t.Fatalf("Failed to create cluster node %s: %v", id, err)
	}

	if err := c.Start(seeds); err != nil {
		t.Fatalf("Failed to start cluster node %s: %v", id, err)
	}

	return c
}

func TestGossipCluster(t *testing.T) {
	// Node A (Seed)
	portA := 20001
	clusterPortA := portA + 10000
	seedA := fmt.Sprintf("127.0.0.1:%d", clusterPortA)

	nodeA := startNode(t, "node-aaaaa-aaaaa", portA, nil)
	defer nodeA.Stop()

	// Node B (Joins A)
	portB := 20002
	nodeB := startNode(t, "node-bbbbb-bbbbb", portB, []string{seedA})
	defer nodeB.Stop()

	// Node C (Joins A)
	portC := 20003
	nodeC := startNode(t, "node-ccccc-ccccc", portC, []string{seedA})
	defer nodeC.Stop()

	// Wait for convergence
	t.Log("Waiting for cluster convergence...")
	time.Sleep(5 * time.Second)

	// Verify Mesh
	verifyMesh(t, nodeA, 3)
	verifyMesh(t, nodeB, 3)
	verifyMesh(t, nodeC, 3)

	// Test Slot Assignment
	t.Log("Assigning slots...")
	// Assign slots 0-10 to A (Reduced from 5000 to avoid log flood)
	slotsA := make([]uint16, 11)
	for i := 0; i <= 10; i++ {
		slotsA[i] = uint16(i)
	}
	if err := nodeA.AssignSlots(slotsA); err != nil {
		t.Errorf("Failed to assign slots to A: %v", err)
	}

	// Propagate slot info (gossip takes time)
	time.Sleep(3 * time.Second)

	// Verify B knows A owns slot 5
	nodeForSlot5 := nodeB.GetSlotNode(5)
	if nodeForSlot5 == nil {
		t.Error("Node B does not know owner of slot 5")
	} else if nodeForSlot5.ID != nodeA.GetSelf().ID {
		t.Errorf("Node B thinks slot 5 owner is %s, want %s", nodeForSlot5.ID, nodeA.GetSelf().ID)
	}

	// Verify Redirection logic
	// Find a key that maps to slot 5
	keySlot5 := ""
	for i := 0; i < 1000000; i++ {
		k := fmt.Sprintf("key-%d", i)
		if nodeA.GetKeySlot(k) == 5 {
			keySlot5 = k
			break
		}
	}
	if keySlot5 == "" {
		t.Fatal("Failed to find key for slot 5 after 1M attempts")
	}

	// Ask A (owner)
	target, err := nodeA.RouteKey(keySlot5)
	if err != nil {
		t.Errorf("Node A returned error for owned key: %v", err)
	}
	if target != nil {
		t.Errorf("Node A returned redirection for owned key: %v", target)
	}

	// Ask B (non-owner)
	target, err = nodeB.RouteKey(keySlot5)
	if err == nil {
		t.Error("Node B did not return error for non-owned key")
	} else {
		// Expect MOVED error
		clusterErr, ok := err.(*cluster.ClusterError)
		if !ok || clusterErr.Type != "MOVED" {
			t.Errorf("Node B returned wrong error: %v", err)
		}
		if target == nil || target.ID != nodeA.GetSelf().ID {
			t.Errorf("Node B redirected to wrong node: %v, want %s", target, nodeA.GetSelf().ID)
		}
	}
}

func verifyMesh(t *testing.T, c *cluster.Cluster, expectedNodes int) {
	nodes := c.GetNodes()
	if len(nodes) != expectedNodes {
		t.Errorf("Node %s sees %d nodes, want %d", c.GetSelf().ID, len(nodes), expectedNodes)
		for _, n := range nodes {
			t.Logf(" - Seen: %s (%s)", n.ID, n.IP)
		}
	}
}

func TestCluster_GossipConcurrentJoin(t *testing.T) {
	// Start 5 nodes all joining a single seed simultaneously.
	portBase := 20101
	clusterPortBase := portBase + 10000
	seedAddr := fmt.Sprintf("127.0.0.1:%d", clusterPortBase)

	node0 := startNode(t, "node-00000-aaaaa", portBase, nil)
	defer node0.Stop()

	var nodes []*cluster.Cluster
	for i := 1; i <= 4; i++ {
		node := startNode(t, fmt.Sprintf("node-%05d-bbbbb", i), portBase+i, []string{seedAddr})
		nodes = append(nodes, node)
		defer node.Stop()
	}

	// Wait for gossip convergence.
	t.Log("Waiting for gossip convergence (5 concurrent joiners)...")
	time.Sleep(10 * time.Second)

	// All 5 nodes should know about each other.
	allNodes := append([]*cluster.Cluster{node0}, nodes...)
	for _, n := range allNodes {
		verifyMesh(t, n, 5)
	}
}

func TestCluster_NodeCrashMidSlotMigration(t *testing.T) {
	portBase := 20201
	clusterPortBase := portBase + 10000
	seedAddr := fmt.Sprintf("127.0.0.1:%d", clusterPortBase)

	nodeA := startNode(t, "node-a-crash-a", portBase, nil)
	defer nodeA.Stop()

	nodeB := startNode(t, "node-b-crash-b", portBase+1, []string{seedAddr})
	defer nodeB.Stop()

	nodeC := startNode(t, "node-c-crash-c", portBase+2, []string{seedAddr})
	defer nodeC.Stop()

	t.Log("Waiting for cluster convergence...")
	time.Sleep(5 * time.Second)

	verifyMesh(t, nodeA, 3)
	verifyMesh(t, nodeB, 3)
	verifyMesh(t, nodeC, 3)

	// Assign slots: A owns 0-5000, B owns 5001-10000, C owns the rest.
	if err := nodeA.AssignSlotRange(0, 5000); err != nil {
		t.Fatalf("Failed to assign slots to A: %v", err)
	}
	if err := nodeB.AssignSlotRange(5001, 10000); err != nil {
		t.Fatalf("Failed to assign slots to B: %v", err)
	}
	if err := nodeC.AssignSlotRange(10001, 16383); err != nil {
		t.Fatalf("Failed to assign slots to C: %v", err)
	}

	time.Sleep(3 * time.Second)

	// Verify slot assignment propagated.
	slotOwner := nodeB.GetSlotNode(0)
	if slotOwner == nil || slotOwner.ID != nodeA.GetSelf().ID {
		t.Errorf("Node B thinks slot 0 owner is %v, want node A", slotOwner)
	}

	// Start a migration from node A to node B (this will fail because there is no
	// target server on node B's data port — the important thing is it doesn't crash).
	t.Log("Attempting slot migration from A to B (connection expected to fail)...")
	if err := nodeA.MigrateSlot(0, nodeB.GetSelf().ID); err == nil {
		t.Log("Migration unexpectedly succeeded (this is OK if server ports are live)")
	} else {
		t.Logf("Migration failed as expected (no data server): %v", err)
	}

	// Kill node A (simulate crash).
	t.Log("Stopping node A (simulating crash)...")
	nodeA.Stop()

	time.Sleep(2 * time.Second)

	// Verify node B and node C are still responsive and not deadlocked.
	// Check node B's slot ownership.
	if nodeB.GetSlotNode(0) == nil {
		t.Error("Node B cannot determine owner of slot 0 after node A crash")
	}
	if nodeB.GetSlotNode(5001) == nil {
		t.Error("Node B lost its own slot 5001 after node A crash")
	}

	// Check node C's slot ownership.
	if nodeC.GetSlotNode(10001) == nil {
		t.Error("Node C lost its own slot 10001 after node A crash")
	}

	// Verify cluster info is still accessible.
	infoB := nodeB.GetClusterInfo()
	if infoB == nil {
		t.Error("Node B cluster info is nil after node A crash")
	}

	infoC := nodeC.GetClusterInfo()
	if infoC == nil {
		t.Error("Node C cluster info is nil after node A crash")
	}

	t.Log("Cluster survived node A crash mid-migration — no deadlock")
}

