package protocol

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/engine/memory"
)

func TestSetCommands_RESP(t *testing.T) {
	store := memory.NewStore(memory.DefaultConfig())
	adapter := NewMemoryStoreAdapter(store)
	server := NewServer(":0", adapter)

	go func() {
		_ = server.Start()
	}()
	defer server.Stop()

	addr := waitForServer(t, server, 2*time.Second)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, 1024)
	writeAndRead := func(cmd string) string {
		t.Helper()
		if _, err := conn.Write([]byte(cmd)); err != nil {
			t.Fatalf("failed to write command: %v", err)
		}
		if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatalf("failed to set deadline: %v", err)
		}
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("failed to read response: %v", err)
		}
		return string(buf[:n])
	}

	resp := writeAndRead("*5\r\n$4\r\nSADD\r\n$5\r\nmyset\r\n$1\r\nb\r\n$1\r\na\r\n$1\r\nb\r\n")
	if resp != ":2\r\n" {
		t.Fatalf("SADD response = %q, want %q", resp, ":2\\r\\n")
	}

	resp = writeAndRead("*3\r\n$9\r\nSISMEMBER\r\n$5\r\nmyset\r\n$1\r\na\r\n")
	if resp != ":1\r\n" {
		t.Fatalf("SISMEMBER response = %q, want %q", resp, ":1\\r\\n")
	}

	resp = writeAndRead("*2\r\n$8\r\nSMEMBERS\r\n$5\r\nmyset\r\n")
	if !strings.HasPrefix(resp, "*2\r\n") {
		t.Fatalf("SMEMBERS response = %q, want array response", resp)
	}
	if !strings.Contains(resp, "$1\r\na\r\n") || !strings.Contains(resp, "$1\r\nb\r\n") {
		t.Fatalf("SMEMBERS missing values: %q", resp)
	}

	resp = writeAndRead("*2\r\n$5\r\nSCARD\r\n$5\r\nmyset\r\n")
	if resp != ":2\r\n" {
		t.Fatalf("SCARD response = %q, want %q", resp, ":2\\r\\n")
	}

	resp = writeAndRead("*3\r\n$4\r\nSREM\r\n$5\r\nmyset\r\n$1\r\na\r\n")
	if resp != ":1\r\n" {
		t.Fatalf("SREM response = %q, want %q", resp, ":1\\r\\n")
	}
}
