package commands

import (
	"net"
	"strings"
	"testing"

	"github.com/tidwall/redcon"

	"github.com/10yihang/autocache/internal/cluster"
)

type mockConn struct {
	response    string
	isBulk      bool
	bulkContent []byte
}

func (m *mockConn) WriteString(s string) {
	m.response = s
}

func (m *mockConn) WriteError(s string) {
	m.response = "-" + s
}

func (m *mockConn) WriteBulk(b []byte) {
	m.isBulk = true
	m.bulkContent = b
}

func (m *mockConn) WriteBulkString(s string) {
	m.isBulk = true
	m.bulkContent = []byte(s)
}

func (m *mockConn) WriteInt(n int)                 {}
func (m *mockConn) WriteInt64(n int64)             {}
func (m *mockConn) WriteUint64(n uint64)           {}
func (m *mockConn) WriteArray(n int)               {}
func (m *mockConn) WriteNull()                     {}
func (m *mockConn) WriteRaw(b []byte)              {}
func (m *mockConn) WriteAny(v interface{})         {}
func (m *mockConn) Context() interface{}           { return nil }
func (m *mockConn) SetContext(v interface{})       {}
func (m *mockConn) SetReadBuffer(n int)            {}
func (m *mockConn) Detach() redcon.DetachedConn    { return nil }
func (m *mockConn) ReadPipeline() []redcon.Command { return nil }
func (m *mockConn) PeekPipeline() []redcon.Command { return nil }
func (m *mockConn) NetConn() net.Conn              { return nil }
func (m *mockConn) RemoteAddr() string             { return "127.0.0.1:12345" }
func (m *mockConn) Close() error                   { return nil }

func newTestClusterHandler(t *testing.T) *ClusterHandler {
	cfg := &cluster.Config{
		NodeID:      "node1-test-id",
		BindAddr:    "127.0.0.1",
		Port:        6379,
		ClusterPort: 16379,
	}
	c, err := cluster.NewCluster(cfg, nil)
	if err != nil {
		t.Fatalf("failed to create cluster: %v", err)
	}
	return NewClusterHandler(c)
}

func TestClusterSetSlot_Migrating(t *testing.T) {
	handler := newTestClusterHandler(t)
	conn := &mockConn{}

	handler.cluster.GetSlotManager().AssignSlot(100, "node1-test-id")

	handler.HandleCluster(conn, [][]byte{
		[]byte("SETSLOT"), []byte("100"), []byte("MIGRATING"), []byte("target-node-id"),
	})

	if conn.response != "OK" {
		t.Errorf("expected OK, got %q", conn.response)
	}

	info := handler.cluster.GetSlotManager().GetSlotInfo(100)
	if info.State != cluster.SlotStateExporting {
		t.Errorf("expected SlotStateExporting, got %v", info.State)
	}
	if info.Exporting != "target-node-id" {
		t.Errorf("expected Exporting=target-node-id, got %q", info.Exporting)
	}
}

func TestClusterSetSlot_Importing(t *testing.T) {
	handler := newTestClusterHandler(t)
	conn := &mockConn{}

	handler.HandleCluster(conn, [][]byte{
		[]byte("SETSLOT"), []byte("200"), []byte("IMPORTING"), []byte("source-node-id"),
	})

	if conn.response != "OK" {
		t.Errorf("expected OK, got %q", conn.response)
	}

	info := handler.cluster.GetSlotManager().GetSlotInfo(200)
	if info.State != cluster.SlotStateImporting {
		t.Errorf("expected SlotStateImporting, got %v", info.State)
	}
	if info.Importing != "source-node-id" {
		t.Errorf("expected Importing=source-node-id, got %q", info.Importing)
	}
}

func TestClusterSetSlot_Node(t *testing.T) {
	handler := newTestClusterHandler(t)
	conn := &mockConn{}

	handler.cluster.GetSlotManager().AssignSlot(300, "old-owner")
	handler.cluster.GetSlotManager().SetExporting(300, "new-owner")

	handler.HandleCluster(conn, [][]byte{
		[]byte("SETSLOT"), []byte("300"), []byte("NODE"), []byte("new-owner"),
	})

	if conn.response != "OK" {
		t.Errorf("expected OK, got %q", conn.response)
	}

	info := handler.cluster.GetSlotManager().GetSlotInfo(300)
	if info.NodeID != "new-owner" {
		t.Errorf("expected NodeID=new-owner, got %q", info.NodeID)
	}
	if info.State != cluster.SlotStateNormal {
		t.Errorf("expected SlotStateNormal, got %v", info.State)
	}
	if info.Exporting != "" {
		t.Errorf("expected Exporting to be cleared, got %q", info.Exporting)
	}
}

func TestClusterSetSlot_Stable(t *testing.T) {
	handler := newTestClusterHandler(t)
	conn := &mockConn{}

	handler.cluster.GetSlotManager().AssignSlot(400, "node1")
	handler.cluster.GetSlotManager().SetExporting(400, "target")

	handler.HandleCluster(conn, [][]byte{
		[]byte("SETSLOT"), []byte("400"), []byte("STABLE"),
	})

	if conn.response != "OK" {
		t.Errorf("expected OK, got %q", conn.response)
	}

	info := handler.cluster.GetSlotManager().GetSlotInfo(400)
	if info.State != cluster.SlotStateNormal {
		t.Errorf("expected SlotStateNormal, got %v", info.State)
	}
	if info.Exporting != "" {
		t.Errorf("expected Exporting to be cleared, got %q", info.Exporting)
	}
}

func TestClusterSetSlot_InvalidSlot(t *testing.T) {
	handler := newTestClusterHandler(t)
	conn := &mockConn{}

	handler.HandleCluster(conn, [][]byte{
		[]byte("SETSLOT"), []byte("99999"), []byte("MIGRATING"), []byte("target"),
	})

	if !strings.HasPrefix(conn.response, "-") {
		t.Errorf("expected error response, got %q", conn.response)
	}
}

func TestClusterSetSlot_MissingArguments(t *testing.T) {
	handler := newTestClusterHandler(t)

	tests := []struct {
		name string
		args [][]byte
	}{
		{"no args", [][]byte{[]byte("SETSLOT")}},
		{"slot only", [][]byte{[]byte("SETSLOT"), []byte("100")}},
		{"migrating no target", [][]byte{[]byte("SETSLOT"), []byte("100"), []byte("MIGRATING")}},
		{"importing no source", [][]byte{[]byte("SETSLOT"), []byte("100"), []byte("IMPORTING")}},
		{"node no id", [][]byte{[]byte("SETSLOT"), []byte("100"), []byte("NODE")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := &mockConn{}
			handler.HandleCluster(conn, tt.args)
			if !strings.HasPrefix(conn.response, "-") {
				t.Errorf("expected error, got %q", conn.response)
			}
		})
	}
}

func TestClusterSetSlot_EpochIncrement(t *testing.T) {
	handler := newTestClusterHandler(t)

	initialEpoch := handler.cluster.GetCurrentEpoch()

	conn := &mockConn{}
	handler.HandleCluster(conn, [][]byte{
		[]byte("SETSLOT"), []byte("100"), []byte("MIGRATING"), []byte("target"),
	})

	if handler.cluster.GetCurrentEpoch() != initialEpoch+1 {
		t.Errorf("epoch should increment after MIGRATING")
	}

	conn = &mockConn{}
	handler.HandleCluster(conn, [][]byte{
		[]byte("SETSLOT"), []byte("100"), []byte("STABLE"),
	})

	if handler.cluster.GetCurrentEpoch() != initialEpoch+1 {
		t.Errorf("epoch should NOT increment after STABLE")
	}
}

func TestClusterNodes_MigrationMarkers(t *testing.T) {
	handler := newTestClusterHandler(t)
	slotMgr := handler.cluster.GetSlotManager()

	slotMgr.AssignSlot(100, "node1-test-id")
	slotMgr.AssignSlot(200, "node1-test-id")

	slotMgr.SetExporting(100, "target-node")

	slotMgr.SetImporting(200, "source-node")

	conn := &mockConn{}
	handler.HandleCluster(conn, [][]byte{[]byte("NODES")})

	if !conn.isBulk {
		t.Fatal("expected bulk response")
	}

	output := string(conn.bulkContent)

	if !strings.Contains(output, "[100->-target-node]") {
		t.Errorf("expected migrating marker [100->-target-node] in output:\n%s", output)
	}

	if !strings.Contains(output, "[200-<-source-node]") {
		t.Errorf("expected importing marker [200-<-source-node] in output:\n%s", output)
	}
}

func TestClusterNodes_SlotRanges(t *testing.T) {
	handler := newTestClusterHandler(t)
	slotMgr := handler.cluster.GetSlotManager()

	for i := uint16(0); i <= 10; i++ {
		slotMgr.AssignSlot(i, "node1-test-id")
	}
	slotMgr.AssignSlot(15, "node1-test-id")

	conn := &mockConn{}
	handler.HandleCluster(conn, [][]byte{[]byte("NODES")})

	if !conn.isBulk {
		t.Fatal("expected bulk response")
	}

	output := string(conn.bulkContent)

	if !strings.Contains(output, "0-10") {
		t.Errorf("expected slot range 0-10 in output:\n%s", output)
	}
	if !strings.Contains(output, "15") {
		t.Errorf("expected single slot 15 in output:\n%s", output)
	}
}

func TestBuildNodeSlotRanges(t *testing.T) {
	handler := newTestClusterHandler(t)
	slotMgr := handler.cluster.GetSlotManager()

	slotMgr.AssignSlot(0, "node1")
	slotMgr.AssignSlot(1, "node1")
	slotMgr.AssignSlot(2, "node1")
	slotMgr.AssignSlot(5, "node1")
	slotMgr.AssignSlot(6, "node1")
	slotMgr.AssignSlot(10, "node1")

	result := handler.buildNodeSlotRanges("node1", slotMgr)

	if !strings.Contains(result, "0-2") {
		t.Errorf("expected range 0-2, got %s", result)
	}
	if !strings.Contains(result, "5-6") {
		t.Errorf("expected range 5-6, got %s", result)
	}
	if !strings.Contains(result, "10") {
		t.Errorf("expected single slot 10, got %s", result)
	}
}

func TestFormatSlotRange(t *testing.T) {
	tests := []struct {
		start uint16
		end   uint16
		want  string
	}{
		{0, 0, "0"},
		{100, 100, "100"},
		{0, 100, "0-100"},
		{5461, 10922, "5461-10922"},
	}

	for _, tt := range tests {
		result := formatSlotRange(tt.start, tt.end)
		if result != tt.want {
			t.Errorf("formatSlotRange(%d, %d) = %q, want %q", tt.start, tt.end, result, tt.want)
		}
	}
}

func TestSortSlots(t *testing.T) {
	slots := []uint16{100, 5, 50, 1, 200}
	sortSlots(slots)

	expected := []uint16{1, 5, 50, 100, 200}
	for i, v := range slots {
		if v != expected[i] {
			t.Errorf("sortSlots: position %d = %d, want %d", i, v, expected[i])
		}
	}
}
