package protocol

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/cluster"
	"github.com/10yihang/autocache/internal/cluster/hash"
	"github.com/10yihang/autocache/internal/cluster/replication"
	"github.com/10yihang/autocache/internal/engine/memory"
)

func TestClusterWriteRejectedOnReplicaNode(t *testing.T) {
	store := memory.NewStore(nil)
	adapter := NewMemoryStoreAdapter(store)
	server := NewServer(":0", adapter)

	c, err := cluster.NewCluster(&cluster.Config{
		NodeID:   "node1",
		BindAddr: "127.0.0.1",
		Port:     7000,
	}, nil)
	if err != nil {
		t.Fatalf("failed to create cluster: %v", err)
	}
	for i := uint16(0); i < 16384; i++ {
		if err := c.GetSlotManager().AssignSlot(i, "node1"); err != nil {
			t.Fatalf("AssignSlot(%d) failed: %v", i, err)
		}
	}
	server.SetCluster(c)

	go server.Start()
	addr := waitForServer(t, server, 2*time.Second)
	cleanup := func() { _ = server.Stop() }
	defer cleanup()

	slot := hash.KeySlot("{user}:write")
	if err := c.GetSlotManager().ConfigureReplication(slot, "node2", []string{"node1"}, 1); err != nil {
		t.Fatalf("ConfigureReplication failed: %v", err)
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, 1024)
	cmd := "*3\r\n$3\r\nSET\r\n$12\r\n{user}:write\r\n$1\r\n1\r\n"
	if _, err := conn.Write([]byte(cmd)); err != nil {
		t.Fatalf("failed to write SET: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("failed to set deadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	response := string(buf[:n])
	if response != "-READONLY slot is not writable on this node\r\n" {
		t.Fatalf("response = %q, want READONLY", response)
	}
}

func TestClusterPrimaryWriteCreatesReplicationEvent(t *testing.T) {
	store := memory.NewStore(nil)
	adapter := NewMemoryStoreAdapter(store)
	server := NewServer(":0", adapter)

	c, err := cluster.NewCluster(&cluster.Config{
		NodeID:   "node1",
		BindAddr: "127.0.0.1",
		Port:     7001,
	}, nil)
	if err != nil {
		t.Fatalf("failed to create cluster: %v", err)
	}
	for i := uint16(0); i < 16384; i++ {
		if err := c.GetSlotManager().AssignSlot(i, "node1"); err != nil {
			t.Fatalf("AssignSlot(%d) failed: %v", i, err)
		}
	}
	server.SetCluster(c)

	go server.Start()
	addr := waitForServer(t, server, 2*time.Second)
	defer func() { _ = server.Stop() }()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, 1024)
	cmd := "*3\r\n$3\r\nSET\r\n$11\r\n{user}:name\r\n$5\r\nalice\r\n"
	if _, err := conn.Write([]byte(cmd)); err != nil {
		t.Fatalf("failed to write SET: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("failed to set deadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	if got := string(buf[:n]); got != "+OK\r\n" {
		t.Fatalf("response = %q, want +OK", got)
	}

	slot := hash.KeySlot("{user}:name")
	logs := c.GetReplicationManager().LogStore().ListAfter(slot, 0)
	if len(logs) != 1 {
		t.Fatalf("replication log length = %d, want 1", len(logs))
	}
	if logs[0].OpType != "SET" {
		t.Fatalf("op type = %s, want SET", logs[0].OpType)
	}
	if logs[0].Key != "{user}:name" {
		t.Fatalf("op key = %s, want {user}:name", logs[0].Key)
	}
}

func TestPrimaryReplicationTransportDeliversToReplicaNode(t *testing.T) {
	replicaStore := memory.NewStore(nil)
	replicaServer := NewServer(":0", NewMemoryStoreAdapter(replicaStore))
	replicaCluster, err := cluster.NewCluster(&cluster.Config{NodeID: "node2", BindAddr: "127.0.0.1", Port: 7002}, nil)
	if err != nil {
		t.Fatalf("create replica cluster: %v", err)
	}
	replicaServer.SetCluster(replicaCluster)
	go replicaServer.Start()
	replicaAddr := waitForServer(t, replicaServer, 2*time.Second)
	defer func() { _ = replicaServer.Stop() }()

	primaryStore := memory.NewStore(nil)
	primaryServer := NewServer(":0", NewMemoryStoreAdapter(primaryStore))
	primaryCluster, err := cluster.NewCluster(&cluster.Config{NodeID: "node1", BindAddr: "127.0.0.1", Port: 7003}, nil)
	if err != nil {
		t.Fatalf("create primary cluster: %v", err)
	}
	for i := uint16(0); i < 16384; i++ {
		if err := primaryCluster.GetSlotManager().AssignSlot(i, "node1"); err != nil {
			t.Fatalf("AssignSlot(%d) failed: %v", i, err)
		}
	}
	slot := hash.KeySlot("{u}:k")
	if err := primaryCluster.GetSlotManager().ConfigureReplication(slot, "node1", []string{"node2"}, 1); err != nil {
		t.Fatalf("ConfigureReplication failed: %v", err)
	}
	primaryCluster.GetReplicationManager().SetReplicaResolver(func(s uint16) []replication.PeerTarget {
		if s != slot {
			return nil
		}
		return []replication.PeerTarget{{NodeID: "node2", Addr: replicaAddr}}
	})
	primaryCluster.GetReplicationManager().SetStreamSender(func(_ context.Context, _ string, op replication.Op) error {
		replicaCluster.GetReplicationManager().ReceiveTransportOp(op)
		return nil
	})

	primaryServer.SetCluster(primaryCluster)
	go primaryServer.Start()
	primaryAddr := waitForServer(t, primaryServer, 2*time.Second)
	defer func() { _ = primaryServer.Stop() }()

	conn, err := net.Dial("tcp", primaryAddr)
	if err != nil {
		t.Fatalf("connect primary: %v", err)
	}
	defer conn.Close()

	cmd := "*3\r\n$3\r\nSET\r\n$5\r\n{u}:k\r\n$1\r\n1\r\n"
	if _, err := conn.Write([]byte(cmd)); err != nil {
		t.Fatalf("write SET: %v", err)
	}
	buf := make([]byte, 1024)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if got := string(buf[:n]); got != "+OK\r\n" {
		t.Fatalf("SET response = %q, want +OK", got)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		received := replicaCluster.GetReplicationManager().InboundAfter(slot, 0)
		if len(received) > 0 {
			if received[0].OpType != "SET" || received[0].Key != "{u}:k" {
				t.Fatalf("unexpected transported op: %+v", received[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting replication transport delivery")
		}
		time.Sleep(10 * time.Millisecond)
	}

}

func TestReplApplyCommandStoresInboundOp(t *testing.T) {
	store := memory.NewStore(nil)
	server := NewServer(":0", NewMemoryStoreAdapter(store))
	c, err := cluster.NewCluster(&cluster.Config{NodeID: "node-r", BindAddr: "127.0.0.1", Port: 7004}, nil)
	if err != nil {
		t.Fatalf("create cluster: %v", err)
	}
	server.SetCluster(c)
	go server.Start()
	addr := waitForServer(t, server, 2*time.Second)
	defer func() { _ = server.Stop() }()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	cmd := "*8\r\n$9\r\nREPLAPPLY\r\n$1\r\n1\r\n$1\r\n2\r\n$1\r\n1\r\n$3\r\nSET\r\n$3\r\nkey\r\n$1\r\n0\r\n$5\r\nvalue\r\n"
	if _, err := conn.Write([]byte(cmd)); err != nil {
		t.Fatalf("write replapply: %v", err)
	}
	buf := make([]byte, 1024)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if got := string(buf[:n]); got != "+OK\r\n" {
		t.Fatalf("response = %q, want +OK", got)
	}

	received := c.GetReplicationManager().InboundAfter(1, 0)
	if len(received) != 1 {
		t.Fatalf("inbound len = %d, want 1", len(received))
	}
	if received[0].LSN != 1 || received[0].Epoch != 2 || received[0].Key != "key" {
		t.Fatalf("unexpected inbound op: %+v", received[0])
	}
}

func TestReplicaApplyMatchesPrimaryAfterOrderedWrite(t *testing.T) {
	replicaStore := memory.NewStore(nil)
	replicaServer := NewServer(":0", NewMemoryStoreAdapter(replicaStore))
	replicaCluster, err := cluster.NewCluster(&cluster.Config{NodeID: "node2", BindAddr: "127.0.0.1", Port: 7005}, nil)
	if err != nil {
		t.Fatalf("create replica cluster: %v", err)
	}
	replicaServer.SetCluster(replicaCluster)
	go replicaServer.Start()
	replicaAddr := waitForServer(t, replicaServer, 2*time.Second)
	defer func() { _ = replicaServer.Stop() }()

	primaryStore := memory.NewStore(nil)
	primaryServer := NewServer(":0", NewMemoryStoreAdapter(primaryStore))
	primaryCluster, err := cluster.NewCluster(&cluster.Config{NodeID: "node1", BindAddr: "127.0.0.1", Port: 7006}, nil)
	if err != nil {
		t.Fatalf("create primary cluster: %v", err)
	}
	for i := uint16(0); i < 16384; i++ {
		if err := primaryCluster.GetSlotManager().AssignSlot(i, "node1"); err != nil {
			t.Fatalf("AssignSlot(%d) failed: %v", i, err)
		}
	}
	slot := hash.KeySlot("{p4}:k")
	if err := primaryCluster.GetSlotManager().ConfigureReplication(slot, "node1", []string{"node2"}, 1); err != nil {
		t.Fatalf("ConfigureReplication failed: %v", err)
	}
	primaryCluster.GetReplicationManager().SetReplicaResolver(func(s uint16) []replication.PeerTarget {
		if s != slot {
			return nil
		}
		return []replication.PeerTarget{{NodeID: "node2", Addr: replicaAddr}}
	})

	primaryServer.SetCluster(primaryCluster)
	go primaryServer.Start()
	primaryAddr := waitForServer(t, primaryServer, 2*time.Second)
	defer func() { _ = primaryServer.Stop() }()

	conn, err := net.Dial("tcp", primaryAddr)
	if err != nil {
		t.Fatalf("connect primary: %v", err)
	}
	defer conn.Close()

	cmd := "*3\r\n$3\r\nSET\r\n$6\r\n{p4}:k\r\n$5\r\nvalue\r\n"
	if _, err := conn.Write([]byte(cmd)); err != nil {
		t.Fatalf("write SET: %v", err)
	}
	buf := make([]byte, 1024)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if got := string(buf[:n]); got != "+OK\r\n" {
		t.Fatalf("SET response = %q, want +OK", got)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		val, getErr := replicaStore.GetBytes(context.Background(), "{p4}:k")
		if getErr == nil && string(val) == "value" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting replica apply, got value=%q err=%v", string(val), getErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestReplApplyRejectsLSNGap(t *testing.T) {
	store := memory.NewStore(nil)
	server := NewServer(":0", NewMemoryStoreAdapter(store))
	c, err := cluster.NewCluster(&cluster.Config{NodeID: "node-gap", BindAddr: "127.0.0.1", Port: 7007}, nil)
	if err != nil {
		t.Fatalf("create cluster: %v", err)
	}
	server.SetCluster(c)
	go server.Start()
	addr := waitForServer(t, server, 2*time.Second)
	defer func() { _ = server.Stop() }()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	gapCmd := "*8\r\n$9\r\nREPLAPPLY\r\n$1\r\n1\r\n$1\r\n1\r\n$1\r\n2\r\n$3\r\nSET\r\n$3\r\nkey\r\n$1\r\n0\r\n$5\r\nvalue\r\n"
	if _, err := conn.Write([]byte(gapCmd)); err != nil {
		t.Fatalf("write replapply: %v", err)
	}
	buf := make([]byte, 1024)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	resp := string(buf[:n])
	if !strings.Contains(resp, "replication gap") {
		t.Fatalf("response = %q, want replication gap error", resp)
	}
}

func TestClusterPSyncReturnsMissingOps(t *testing.T) {
	store := memory.NewStore(nil)
	server := NewServer(":0", NewMemoryStoreAdapter(store))
	c, err := cluster.NewCluster(&cluster.Config{NodeID: "node-psync", BindAddr: "127.0.0.1", Port: 7008}, nil)
	if err != nil {
		t.Fatalf("create cluster: %v", err)
	}
	for i := uint16(0); i < 16384; i++ {
		if err := c.GetSlotManager().AssignSlot(i, "node-psync"); err != nil {
			t.Fatalf("AssignSlot(%d) failed: %v", i, err)
		}
	}
	slot := hash.KeySlot("{psync}:k")
	if err := c.GetSlotManager().ConfigureReplication(slot, "node-psync", []string{"replica-a"}, 1); err != nil {
		t.Fatalf("ConfigureReplication failed: %v", err)
	}
	server.SetCluster(c)
	go server.Start()
	addr := waitForServer(t, server, 2*time.Second)
	defer func() { _ = server.Stop() }()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	send := func(cmd string) string {
		t.Helper()
		if _, err := conn.Write([]byte(cmd)); err != nil {
			t.Fatalf("write command: %v", err)
		}
		if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatalf("deadline: %v", err)
		}
		buf := make([]byte, 2048)
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		return string(buf[:n])
	}

	if resp := send("*3\r\n$3\r\nSET\r\n$9\r\n{psync}:k\r\n$1\r\n1\r\n"); resp != "+OK\r\n" {
		t.Fatalf("first SET response = %q", resp)
	}
	if resp := send("*3\r\n$3\r\nSET\r\n$9\r\n{psync}:k\r\n$1\r\n2\r\n"); resp != "+OK\r\n" {
		t.Fatalf("second SET response = %q", resp)
	}

	slotStr := strconv.FormatUint(uint64(slot), 10)
	psyncCmd := "*5\r\n$7\r\nCLUSTER\r\n$5\r\nPSYNC\r\n$" + strconv.Itoa(len(slotStr)) + "\r\n" + slotStr + "\r\n$1\r\n1\r\n$1\r\n1\r\n"
	resp := send(psyncCmd)
	if !strings.HasPrefix(resp, "*1\r\n") {
		t.Fatalf("psync response = %q, want one op array", resp)
	}
	if !strings.Contains(resp, "$3\r\nSET\r\n") {
		t.Fatalf("psync response missing SET op: %q", resp)
	}
}

func TestWaitReturnsReplicatedAckCount(t *testing.T) {
	store := memory.NewStore(nil)
	server := NewServer(":0", NewMemoryStoreAdapter(store))
	c, err := cluster.NewCluster(&cluster.Config{NodeID: "node-wait", BindAddr: "127.0.0.1", Port: 7009}, nil)
	if err != nil {
		t.Fatalf("create cluster: %v", err)
	}
	for i := uint16(0); i < 16384; i++ {
		if err := c.GetSlotManager().AssignSlot(i, "node-wait"); err != nil {
			t.Fatalf("AssignSlot(%d) failed: %v", i, err)
		}
	}
	slot := hash.KeySlot("{wait}:k")
	if err := c.GetSlotManager().ConfigureReplication(slot, "node-wait", []string{"replica-1"}, 1); err != nil {
		t.Fatalf("ConfigureReplication failed: %v", err)
	}
	c.GetReplicationManager().SetReplicaResolver(func(s uint16) []replication.PeerTarget {
		if s != slot {
			return nil
		}
		return []replication.PeerTarget{{NodeID: "replica-1", Addr: "noop:0"}}
	})
	c.GetReplicationManager().SetStreamSender(func(_ context.Context, _ string, _ replication.Op) error {
		return nil
	})

	server.SetCluster(c)
	go server.Start()
	addr := waitForServer(t, server, 2*time.Second)
	defer func() { _ = server.Stop() }()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	send := func(cmd string) string {
		t.Helper()
		if _, err := conn.Write([]byte(cmd)); err != nil {
			t.Fatalf("write command: %v", err)
		}
		if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatalf("deadline: %v", err)
		}
		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		return string(buf[:n])
	}

	if resp := send("*3\r\n$3\r\nSET\r\n$8\r\n{wait}:k\r\n$1\r\n1\r\n"); resp != "+OK\r\n" {
		t.Fatalf("SET response = %q", resp)
	}
	if resp := send("*3\r\n$4\r\nWAIT\r\n$1\r\n1\r\n$3\r\n200\r\n"); resp != ":1\r\n" {
		t.Fatalf("WAIT response = %q, want :1", resp)
	}
}

func TestClusterPSyncBacklogMissReturnsFullResync(t *testing.T) {
	store := memory.NewStore(nil)
	server := NewServer(":0", NewMemoryStoreAdapter(store))
	c, err := cluster.NewCluster(&cluster.Config{NodeID: "node-full", BindAddr: "127.0.0.1", Port: 7010}, nil)
	if err != nil {
		t.Fatalf("create cluster: %v", err)
	}
	for i := uint16(0); i < 16384; i++ {
		if err := c.GetSlotManager().AssignSlot(i, "node-full"); err != nil {
			t.Fatalf("AssignSlot(%d) failed: %v", i, err)
		}
	}
	slot := hash.KeySlot("{full}:k")
	if err := c.GetSlotManager().ConfigureReplication(slot, "node-full", []string{"replica-f"}, 1); err != nil {
		t.Fatalf("ConfigureReplication failed: %v", err)
	}
	server.SetCluster(c)
	go server.Start()
	addr := waitForServer(t, server, 2*time.Second)
	defer func() { _ = server.Stop() }()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("*3\r\n$3\r\nSET\r\n$8\r\n{full}:k\r\n$1\r\n1\r\n")); err != nil {
		t.Fatalf("write SET: %v", err)
	}
	buf := make([]byte, 2048)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	if _, err := conn.Read(buf); err != nil {
		t.Fatalf("read SET response: %v", err)
	}

	slotStr := strconv.FormatUint(uint64(slot), 10)
	cmd := "*5\r\n$7\r\nCLUSTER\r\n$5\r\nPSYNC\r\n$" + strconv.Itoa(len(slotStr)) + "\r\n" + slotStr + "\r\n$1\r\n1\r\n$3\r\n999\r\n"
	if _, err := conn.Write([]byte(cmd)); err != nil {
		t.Fatalf("write PSYNC: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read PSYNC response: %v", err)
	}
	resp := string(buf[:n])
	if !strings.Contains(resp, "FULLRESYNC") {
		t.Fatalf("PSYNC fallback response = %q, want FULLRESYNC", resp)
	}
}

func TestReplicaTTLRemainsConsistentAfterReplicatedSetEX(t *testing.T) {
	replicaStore := memory.NewStore(nil)
	replicaServer := NewServer(":0", NewMemoryStoreAdapter(replicaStore))
	replicaCluster, err := cluster.NewCluster(&cluster.Config{NodeID: "node2", BindAddr: "127.0.0.1", Port: 7011}, nil)
	if err != nil {
		t.Fatalf("create replica cluster: %v", err)
	}
	replicaServer.SetCluster(replicaCluster)
	go replicaServer.Start()
	replicaAddr := waitForServer(t, replicaServer, 2*time.Second)
	defer func() { _ = replicaServer.Stop() }()

	primaryStore := memory.NewStore(nil)
	primaryServer := NewServer(":0", NewMemoryStoreAdapter(primaryStore))
	primaryCluster, err := cluster.NewCluster(&cluster.Config{NodeID: "node1", BindAddr: "127.0.0.1", Port: 7012}, nil)
	if err != nil {
		t.Fatalf("create primary cluster: %v", err)
	}
	for i := uint16(0); i < 16384; i++ {
		if err := primaryCluster.GetSlotManager().AssignSlot(i, "node1"); err != nil {
			t.Fatalf("AssignSlot(%d) failed: %v", i, err)
		}
	}
	slot := hash.KeySlot("{ttl}:k")
	if err := primaryCluster.GetSlotManager().ConfigureReplication(slot, "node1", []string{"node2"}, 1); err != nil {
		t.Fatalf("ConfigureReplication failed: %v", err)
	}
	primaryCluster.GetReplicationManager().SetReplicaResolver(func(s uint16) []replication.PeerTarget {
		if s != slot {
			return nil
		}
		return []replication.PeerTarget{{NodeID: "node2", Addr: replicaAddr}}
	})

	primaryServer.SetCluster(primaryCluster)
	go primaryServer.Start()
	primaryAddr := waitForServer(t, primaryServer, 2*time.Second)
	defer func() { _ = primaryServer.Stop() }()

	conn, err := net.Dial("tcp", primaryAddr)
	if err != nil {
		t.Fatalf("connect primary: %v", err)
	}
	defer conn.Close()

	cmd := "*5\r\n$3\r\nSET\r\n$7\r\n{ttl}:k\r\n$1\r\n1\r\n$2\r\nEX\r\n$1\r\n5\r\n"
	if _, err := conn.Write([]byte(cmd)); err != nil {
		t.Fatalf("write SET EX: %v", err)
	}
	buf := make([]byte, 1024)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	if _, err := conn.Read(buf); err != nil {
		t.Fatalf("read response: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		ttl, ttlErr := replicaStore.TTL(context.Background(), "{ttl}:k")
		if ttlErr == nil && ttl > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting ttl replication, ttl=%v err=%v", ttl, ttlErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
