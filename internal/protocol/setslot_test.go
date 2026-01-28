package protocol

import (
	"bufio"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/cluster"
	"github.com/10yihang/autocache/internal/engine/memory"
)

func setupSetSlotTestServer(t *testing.T) (*Server, *cluster.Cluster, string, func()) {
	store := memory.NewStore(nil)
	adapter := NewMemoryStoreAdapter(store)
	server := NewServer(":0", adapter)

	c, err := cluster.NewCluster(&cluster.Config{
		NodeID:   "test-node-1",
		BindAddr: "127.0.0.1",
		Port:     7000,
	}, nil)
	if err != nil {
		t.Fatalf("failed to create cluster: %v", err)
	}

	for i := uint16(0); i < 16384; i++ {
		c.GetSlotManager().AssignSlot(i, "test-node-1")
	}

	server.SetCluster(c)

	go server.Start()

	var addr string
	for i := 0; i < 50; i++ {
		addr = server.Addr()
		if addr != "" && addr != ":0" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if addr == "" || addr == ":0" {
		t.Fatal("server failed to start")
	}

	return server, c, addr, func() { server.Stop() }
}

func sendSetSlotCommand(conn net.Conn, args ...string) (string, error) {
	cmd := fmt.Sprintf("*%d\r\n", len(args))
	for _, arg := range args {
		cmd += fmt.Sprintf("$%d\r\n%s\r\n", len(arg), arg)
	}
	_, err := conn.Write([]byte(cmd))
	if err != nil {
		return "", err
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return line, nil
}

func TestClusterSetSlot_Migrating(t *testing.T) {
	_, c, addr, cleanup := setupSetSlotTestServer(t)
	defer cleanup()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := sendSetSlotCommand(conn, "CLUSTER", "SETSLOT", "100", "MIGRATING", "target-node-id")
	if err != nil {
		t.Fatalf("SETSLOT MIGRATING failed: %v", err)
	}
	if resp != "+OK\r\n" {
		t.Errorf("expected +OK, got %q", resp)
	}

	slotInfo := c.GetSlotManager().GetSlotInfo(100)
	if slotInfo.State != cluster.SlotStateExporting {
		t.Errorf("expected SlotStateExporting, got %v", slotInfo.State)
	}
	if slotInfo.Exporting != "target-node-id" {
		t.Errorf("expected Exporting='target-node-id', got %q", slotInfo.Exporting)
	}
}

func TestClusterSetSlot_Importing(t *testing.T) {
	_, c, addr, cleanup := setupSetSlotTestServer(t)
	defer cleanup()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := sendSetSlotCommand(conn, "CLUSTER", "SETSLOT", "200", "IMPORTING", "source-node-id")
	if err != nil {
		t.Fatalf("SETSLOT IMPORTING failed: %v", err)
	}
	if resp != "+OK\r\n" {
		t.Errorf("expected +OK, got %q", resp)
	}

	slotInfo := c.GetSlotManager().GetSlotInfo(200)
	if slotInfo.State != cluster.SlotStateImporting {
		t.Errorf("expected SlotStateImporting, got %v", slotInfo.State)
	}
	if slotInfo.Importing != "source-node-id" {
		t.Errorf("expected Importing='source-node-id', got %q", slotInfo.Importing)
	}
}

func TestClusterSetSlot_Node(t *testing.T) {
	_, c, addr, cleanup := setupSetSlotTestServer(t)
	defer cleanup()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	sendSetSlotCommand(conn, "CLUSTER", "SETSLOT", "300", "MIGRATING", "new-owner-id")

	conn2, _ := net.Dial("tcp", addr)
	defer conn2.Close()

	resp, err := sendSetSlotCommand(conn2, "CLUSTER", "SETSLOT", "300", "NODE", "new-owner-id")
	if err != nil {
		t.Fatalf("SETSLOT NODE failed: %v", err)
	}
	if resp != "+OK\r\n" {
		t.Errorf("expected +OK, got %q", resp)
	}

	slotInfo := c.GetSlotManager().GetSlotInfo(300)
	if slotInfo.State != cluster.SlotStateNormal {
		t.Errorf("expected SlotStateNormal, got %v", slotInfo.State)
	}
	if slotInfo.NodeID != "new-owner-id" {
		t.Errorf("expected NodeID='new-owner-id', got %q", slotInfo.NodeID)
	}
}

func TestClusterSetSlot_Stable(t *testing.T) {
	_, c, addr, cleanup := setupSetSlotTestServer(t)
	defer cleanup()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	sendSetSlotCommand(conn, "CLUSTER", "SETSLOT", "400", "MIGRATING", "target-node")

	slotInfo := c.GetSlotManager().GetSlotInfo(400)
	if slotInfo.State != cluster.SlotStateExporting {
		t.Fatalf("precondition failed: slot not in MIGRATING state")
	}

	conn2, _ := net.Dial("tcp", addr)
	defer conn2.Close()

	resp, err := sendSetSlotCommand(conn2, "CLUSTER", "SETSLOT", "400", "STABLE")
	if err != nil {
		t.Fatalf("SETSLOT STABLE failed: %v", err)
	}
	if resp != "+OK\r\n" {
		t.Errorf("expected +OK, got %q", resp)
	}

	slotInfo = c.GetSlotManager().GetSlotInfo(400)
	if slotInfo.State != cluster.SlotStateNormal {
		t.Errorf("expected SlotStateNormal after STABLE, got %v", slotInfo.State)
	}
	if slotInfo.Exporting != "" {
		t.Errorf("expected Exporting='', got %q", slotInfo.Exporting)
	}
}

func TestClusterSetSlot_InvalidSlot(t *testing.T) {
	_, _, addr, cleanup := setupSetSlotTestServer(t)
	defer cleanup()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := sendSetSlotCommand(conn, "CLUSTER", "SETSLOT", "99999", "MIGRATING", "node-id")
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}
	if resp != "-ERR Invalid slot\r\n" {
		t.Errorf("expected ERR Invalid slot, got %q", resp)
	}
}

func TestClusterSetSlot_MissingArgs(t *testing.T) {
	_, _, addr, cleanup := setupSetSlotTestServer(t)
	defer cleanup()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := sendSetSlotCommand(conn, "CLUSTER", "SETSLOT", "100", "MIGRATING")
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}
	if resp != "-ERR wrong number of arguments for 'cluster setslot migrating' command\r\n" {
		t.Errorf("expected ERR wrong number of arguments, got %q", resp)
	}
}

func TestClusterSetSlot_EpochIncremented(t *testing.T) {
	_, c, addr, cleanup := setupSetSlotTestServer(t)
	defer cleanup()

	initialEpoch := c.GetCurrentEpoch()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	sendSetSlotCommand(conn, "CLUSTER", "SETSLOT", "100", "MIGRATING", "target-node")

	newEpoch := c.GetCurrentEpoch()
	if newEpoch <= initialEpoch {
		t.Errorf("expected epoch to increment, got initial=%d new=%d", initialEpoch, newEpoch)
	}
}
