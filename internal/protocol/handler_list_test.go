package protocol

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/engine/memory"
)

func TestListCommands_RESP(t *testing.T) {
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

	resp := writeAndRead("*4\r\n$5\r\nLPUSH\r\n$6\r\nmylist\r\n$1\r\nb\r\n$1\r\na\r\n")
	if resp != ":2\r\n" {
		t.Fatalf("LPUSH response = %q, want %q", resp, ":2\\r\\n")
	}

	resp = writeAndRead("*3\r\n$5\r\nRPUSH\r\n$6\r\nmylist\r\n$1\r\nc\r\n")
	if resp != ":3\r\n" {
		t.Fatalf("RPUSH response = %q, want %q", resp, ":3\\r\\n")
	}

	resp = writeAndRead("*4\r\n$6\r\nLRANGE\r\n$6\r\nmylist\r\n$1\r\n0\r\n$2\r\n-1\r\n")
	if !strings.HasPrefix(resp, "*3\r\n") {
		t.Fatalf("LRANGE response = %q, want array response", resp)
	}
	if !strings.Contains(resp, "$1\r\na\r\n") || !strings.Contains(resp, "$1\r\nb\r\n") || !strings.Contains(resp, "$1\r\nc\r\n") {
		t.Fatalf("LRANGE response missing values: %q", resp)
	}

	resp = writeAndRead("*2\r\n$4\r\nLPOP\r\n$6\r\nmylist\r\n")
	if resp != "$1\r\na\r\n" {
		t.Fatalf("LPOP response = %q, want %q", resp, "$1\\r\\na\\r\\n")
	}

	resp = writeAndRead("*2\r\n$4\r\nRPOP\r\n$6\r\nmylist\r\n")
	if resp != "$1\r\nc\r\n" {
		t.Fatalf("RPOP response = %q, want %q", resp, "$1\\r\\nc\\r\\n")
	}
}
