package protocol

import (
	"bufio"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/cluster"
	"github.com/10yihang/autocache/internal/cluster/hash"
	"github.com/10yihang/autocache/internal/engine/memory"
)

func TestExtractMSetKeys(t *testing.T) {
	tests := []struct {
		name     string
		args     [][]byte
		expected int
	}{
		{"empty", [][]byte{}, 0},
		{"single_pair", [][]byte{[]byte("k1"), []byte("v1")}, 1},
		{"two_pairs", [][]byte{[]byte("k1"), []byte("v1"), []byte("k2"), []byte("v2")}, 2},
		{"three_pairs", [][]byte{[]byte("k1"), []byte("v1"), []byte("k2"), []byte("v2"), []byte("k3"), []byte("v3")}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys := extractMSetKeys(tt.args)
			if len(keys) != tt.expected {
				t.Errorf("extractMSetKeys() got %d keys, want %d", len(keys), tt.expected)
			}
		})
	}
}

func TestIsMultiKeyCommand(t *testing.T) {
	multiKey := []string{"MGET", "MSET", "DEL", "EXISTS", "RENAME"}
	singleKey := []string{"GET", "SET", "INCR", "TTL", "EXPIRE"}

	for _, cmd := range multiKey {
		if !isMultiKeyCommand(cmd) {
			t.Errorf("isMultiKeyCommand(%q) = false, want true", cmd)
		}
	}

	for _, cmd := range singleKey {
		if isMultiKeyCommand(cmd) {
			t.Errorf("isMultiKeyCommand(%q) = true, want false", cmd)
		}
	}
}

func setupClusterServer(t *testing.T) (*Server, string, func()) {
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
		c.GetSlotManager().AssignSlot(i, "node1")
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

	return server, addr, func() { server.Stop() }
}

func sendCommand(conn net.Conn, args ...string) (string, error) {
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

	if line[0] == '*' || line[0] == '$' {
		return readFullResponse(reader, line)
	}

	return line, nil
}

func readFullResponse(reader *bufio.Reader, firstLine string) (string, error) {
	result := firstLine

	if firstLine[0] == '*' {
		var count int
		fmt.Sscanf(firstLine, "*%d", &count)
		for i := 0; i < count; i++ {
			line, err := reader.ReadString('\n')
			if err != nil {
				return result, err
			}
			result += line
			if line[0] == '$' {
				var len int
				fmt.Sscanf(line, "$%d", &len)
				if len > 0 {
					data := make([]byte, len+2)
					_, err := reader.Read(data)
					if err != nil {
						return result, err
					}
					result += string(data)
				}
			}
		}
	} else if firstLine[0] == '$' {
		var len int
		fmt.Sscanf(firstLine, "$%d", &len)
		if len > 0 {
			data := make([]byte, len+2)
			_, err := reader.Read(data)
			if err != nil {
				return result, err
			}
			result += string(data)
		}
	}

	return result, nil
}

func TestMGET_SameSlot_Success(t *testing.T) {
	_, addr, cleanup := setupClusterServer(t)
	defer cleanup()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := sendCommand(conn, "MSET", "{user}:1", "val1", "{user}:2", "val2")
	if err != nil {
		t.Fatalf("MSET failed: %v", err)
	}
	if resp != "+OK\r\n" {
		t.Errorf("MSET expected +OK, got %q", resp)
	}

	resp, err = sendCommand(conn, "MGET", "{user}:1", "{user}:2")
	if err != nil {
		t.Fatalf("MGET failed: %v", err)
	}
	if resp[0] != '*' {
		t.Errorf("MGET expected array response, got %q", resp)
	}
}

func TestMGET_CrossSlot_Error(t *testing.T) {
	_, addr, cleanup := setupClusterServer(t)
	defer cleanup()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	key1 := "foo"
	key2 := "bar"

	slot1 := hash.KeySlot(key1)
	slot2 := hash.KeySlot(key2)
	if slot1 == slot2 {
		t.Skip("test keys hash to same slot, need different keys")
	}

	resp, err := sendCommand(conn, "MGET", key1, key2)
	if err != nil {
		t.Fatalf("MGET failed: %v", err)
	}
	if resp != "-CROSSSLOT Keys in request don't hash to the same slot\r\n" {
		t.Errorf("MGET expected CROSSSLOT error, got %q", resp)
	}
}

func TestMSET_CrossSlot_Error(t *testing.T) {
	_, addr, cleanup := setupClusterServer(t)
	defer cleanup()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	key1 := "foo"
	key2 := "bar"

	slot1 := hash.KeySlot(key1)
	slot2 := hash.KeySlot(key2)
	if slot1 == slot2 {
		t.Skip("test keys hash to same slot")
	}

	resp, err := sendCommand(conn, "MSET", key1, "v1", key2, "v2")
	if err != nil {
		t.Fatalf("MSET failed: %v", err)
	}
	if resp != "-CROSSSLOT Keys in request don't hash to the same slot\r\n" {
		t.Errorf("MSET expected CROSSSLOT error, got %q", resp)
	}
}

func TestDEL_CrossSlot_Error(t *testing.T) {
	_, addr, cleanup := setupClusterServer(t)
	defer cleanup()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	key1 := "foo"
	key2 := "bar"

	slot1 := hash.KeySlot(key1)
	slot2 := hash.KeySlot(key2)
	if slot1 == slot2 {
		t.Skip("test keys hash to same slot")
	}

	resp, err := sendCommand(conn, "DEL", key1, key2)
	if err != nil {
		t.Fatalf("DEL failed: %v", err)
	}
	if resp != "-CROSSSLOT Keys in request don't hash to the same slot\r\n" {
		t.Errorf("DEL expected CROSSSLOT error, got %q", resp)
	}
}

func TestEXISTS_CrossSlot_Error(t *testing.T) {
	_, addr, cleanup := setupClusterServer(t)
	defer cleanup()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	key1 := "foo"
	key2 := "bar"

	slot1 := hash.KeySlot(key1)
	slot2 := hash.KeySlot(key2)
	if slot1 == slot2 {
		t.Skip("test keys hash to same slot")
	}

	resp, err := sendCommand(conn, "EXISTS", key1, key2)
	if err != nil {
		t.Fatalf("EXISTS failed: %v", err)
	}
	if resp != "-CROSSSLOT Keys in request don't hash to the same slot\r\n" {
		t.Errorf("EXISTS expected CROSSSLOT error, got %q", resp)
	}
}

func TestRENAME_CrossSlot_Error(t *testing.T) {
	_, addr, cleanup := setupClusterServer(t)
	defer cleanup()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	sendCommand(conn, "SET", "foo", "value")

	key1 := "foo"
	key2 := "bar"

	slot1 := hash.KeySlot(key1)
	slot2 := hash.KeySlot(key2)
	if slot1 == slot2 {
		t.Skip("test keys hash to same slot")
	}

	conn2, _ := net.Dial("tcp", addr)
	defer conn2.Close()

	resp, err := sendCommand(conn2, "RENAME", key1, key2)
	if err != nil {
		t.Fatalf("RENAME failed: %v", err)
	}
	if resp != "-CROSSSLOT Keys in request don't hash to the same slot\r\n" {
		t.Errorf("RENAME expected CROSSSLOT error, got %q", resp)
	}
}

func TestRENAME_SameSlot_Success(t *testing.T) {
	_, addr, cleanup := setupClusterServer(t)
	defer cleanup()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	resp, _ := sendCommand(conn, "SET", "{tag}:old", "value")
	if resp != "+OK\r\n" {
		t.Fatalf("SET failed: %s", resp)
	}

	resp, err = sendCommand(conn, "RENAME", "{tag}:old", "{tag}:new")
	if err != nil {
		t.Fatalf("RENAME failed: %v", err)
	}
	if resp != "+OK\r\n" {
		t.Errorf("RENAME expected +OK, got %q", resp)
	}
}

func TestNonClusterMode_NoCrossSlotCheck(t *testing.T) {
	store := memory.NewStore(nil)
	adapter := NewMemoryStoreAdapter(store)
	server := NewServer(":0", adapter)

	go server.Start()
	defer server.Stop()

	var addr string
	for i := 0; i < 50; i++ {
		addr = server.Addr()
		if addr != "" && addr != ":0" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := sendCommand(conn, "MSET", "a", "1", "b", "2")
	if err != nil {
		t.Fatalf("MSET failed: %v", err)
	}
	if resp != "+OK\r\n" {
		t.Errorf("MSET in non-cluster mode expected +OK (no CROSSSLOT check), got %q", resp)
	}
}
