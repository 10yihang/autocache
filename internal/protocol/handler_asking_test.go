package protocol

import (
	"net"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/engine/memory"
)

func waitForServer(t *testing.T, s *Server, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		addr := s.Addr()
		if addr != ":0" && addr != "" {
			return addr
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("server did not start in time")
	return ""
}

func TestAskingCommand_ReturnsOK(t *testing.T) {
	store := memory.NewStore(memory.DefaultConfig())
	adapter := NewMemoryStoreAdapter(store)
	server := NewServer(":0", adapter)

	go func() {
		server.Start()
	}()
	defer server.Stop()

	addr := waitForServer(t, server, 2*time.Second)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte("*1\r\n$6\r\nASKING\r\n"))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	response := string(buf[:n])
	if response != "+OK\r\n" {
		t.Errorf("expected +OK\\r\\n, got %q", response)
	}
}

func TestAskingFlag_ClearsAfterCommand(t *testing.T) {
	store := memory.NewStore(memory.DefaultConfig())
	adapter := NewMemoryStoreAdapter(store)
	server := NewServer(":0", adapter)

	go func() {
		server.Start()
	}()
	defer server.Stop()

	addr := waitForServer(t, server, 2*time.Second)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte("*1\r\n$6\r\nASKING\r\n"))
	if err != nil {
		t.Fatalf("failed to write ASKING: %v", err)
	}

	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read ASKING response: %v", err)
	}
	if string(buf[:n]) != "+OK\r\n" {
		t.Errorf("ASKING: expected +OK\\r\\n, got %q", string(buf[:n]))
	}

	_, err = conn.Write([]byte("*2\r\n$3\r\nGET\r\n$7\r\ntestkey\r\n"))
	if err != nil {
		t.Fatalf("failed to write GET: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read GET response: %v", err)
	}
	if string(buf[:n]) != "$-1\r\n" {
		t.Errorf("GET: expected $-1\\r\\n (nil), got %q", string(buf[:n]))
	}

	_, err = conn.Write([]byte("*1\r\n$4\r\nPING\r\n"))
	if err != nil {
		t.Fatalf("failed to write PING: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read PING response: %v", err)
	}
	if string(buf[:n]) != "+PONG\r\n" {
		t.Errorf("PING: expected +PONG\\r\\n, got %q", string(buf[:n]))
	}
}

func TestCommands_WorkWithoutAsking(t *testing.T) {
	store := memory.NewStore(memory.DefaultConfig())
	adapter := NewMemoryStoreAdapter(store)
	server := NewServer(":0", adapter)

	go func() {
		server.Start()
	}()
	defer server.Stop()

	addr := waitForServer(t, server, 2*time.Second)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte("*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n"))
	if err != nil {
		t.Fatalf("failed to write SET: %v", err)
	}

	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read SET response: %v", err)
	}
	if string(buf[:n]) != "+OK\r\n" {
		t.Errorf("SET: expected +OK\\r\\n, got %q", string(buf[:n]))
	}

	_, err = conn.Write([]byte("*2\r\n$3\r\nGET\r\n$3\r\nfoo\r\n"))
	if err != nil {
		t.Fatalf("failed to write GET: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read GET response: %v", err)
	}
	if string(buf[:n]) != "$3\r\nbar\r\n" {
		t.Errorf("GET: expected $3\\r\\nbar\\r\\n, got %q", string(buf[:n]))
	}
}

func TestServerAddr_ReturnsActualAddress(t *testing.T) {
	store := memory.NewStore(memory.DefaultConfig())
	adapter := NewMemoryStoreAdapter(store)
	server := NewServer(":0", adapter)

	go func() {
		server.Start()
	}()
	defer server.Stop()

	addr := waitForServer(t, server, 2*time.Second)

	if addr == ":0" {
		t.Errorf("Addr() should return actual address, got %q", addr)
	}

	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("failed to parse address %q: %v", addr, err)
	}
	if port == "0" {
		t.Errorf("port should not be 0, got address %q", addr)
	}
}
