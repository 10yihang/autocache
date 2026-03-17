package integration

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/cluster"
	"github.com/10yihang/autocache/internal/cluster/hash"
	"github.com/10yihang/autocache/internal/engine/memory"
	"github.com/10yihang/autocache/internal/protocol"
)

func freePort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("freePort close: %v", err)
	}

	return port
}

type testNode struct {
	store   *memory.Store
	adapter *protocol.MemoryStoreAdapter
	server  *protocol.Server
	cluster *cluster.Cluster
}

func startTestNode(t *testing.T, cfg *cluster.Config) *testNode {
	t.Helper()

	store := memory.NewStore(memory.DefaultConfig())
	store.SetSlotFunc(hash.KeySlot)

	adapter := protocol.NewMemoryStoreAdapter(store)
	server := protocol.NewServer(fmt.Sprintf(":%d", cfg.Port), adapter)

	c, err := cluster.NewCluster(cfg, nil)
	if err != nil {
		t.Fatalf("create cluster %s: %v", cfg.NodeID, err)
	}

	server.SetCluster(c)

	if err := c.Start(cfg.Seeds); err != nil {
		t.Fatalf("start cluster %s: %v", cfg.NodeID, err)
	}

	go func() {
		if err := server.Start(); err != nil {
			t.Logf("server %s stopped: %v", cfg.NodeID, err)
		}
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.Port))
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server %s not ready on %d: %v", cfg.NodeID, cfg.Port, err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	return &testNode{
		store:   store,
		adapter: adapter,
		server:  server,
		cluster: c,
	}
}

func (n *testNode) stop(t *testing.T) {
	t.Helper()

	if err := n.cluster.Stop(); err != nil {
		t.Errorf("stop cluster: %v", err)
	}
	if err := n.server.Stop(); err != nil {
		t.Errorf("stop server: %v", err)
	}
	if err := n.store.Close(); err != nil {
		t.Errorf("close store: %v", err)
	}
}

func TestSlotMigration_KeysMove(t *testing.T) {
	portA := freePort(t)
	clusterPortA := freePort(t)

	nodeA := startTestNode(t, &cluster.Config{
		NodeID:      "node-aaaaa-aaaaa",
		BindAddr:    "127.0.0.1",
		Port:        portA,
		ClusterPort: clusterPortA,
		Seeds:       nil,
	})
	defer nodeA.stop(t)

	portB := freePort(t)
	clusterPortB := freePort(t)
	seedA := fmt.Sprintf("127.0.0.1:%d", clusterPortA)

	nodeB := startTestNode(t, &cluster.Config{
		NodeID:      "node-bbbbb-bbbbb",
		BindAddr:    "127.0.0.1",
		Port:        portB,
		ClusterPort: clusterPortB,
		Seeds:       []string{seedA},
	})
	defer nodeB.stop(t)

	t.Log("waiting for cluster convergence")
	time.Sleep(3 * time.Second)

	keys := make([]string, 0, 5)
	for i := 0; len(keys) < 5 && i < 1_000_000; i++ {
		key := fmt.Sprintf("key-%d", i)
		if hash.KeySlot(key) == 0 {
			keys = append(keys, key)
		}
	}
	if len(keys) != 5 {
		t.Fatalf("expected 5 keys for slot 0, got %d", len(keys))
	}

	ctx := context.Background()
	for i, key := range keys {
		if err := nodeA.store.Set(ctx, key, fmt.Sprintf("value-%d", i), 0); err != nil {
			t.Fatalf("set key %s: %v", key, err)
		}
	}

	if err := nodeA.cluster.AssignSlots([]uint16{0}); err != nil {
		t.Fatalf("assign slot 0 to node A: %v", err)
	}

	keysInSlot := nodeA.adapter.KeysInSlot(0, 100)
	if len(keysInSlot) != len(keys) {
		t.Fatalf("KeysInSlot returned %d keys, want %d", len(keysInSlot), len(keys))
	}

	countInSlot := nodeA.adapter.CountKeysInSlot(0)
	if countInSlot != len(keys) {
		t.Fatalf("CountKeysInSlot returned %d, want %d", countInSlot, len(keys))
	}

	t.Logf("slot=0 keys=%v keys_in_slot=%v count=%d", keys, keysInSlot, countInSlot)
}
