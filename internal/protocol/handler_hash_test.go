package protocol

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/engine/memory"
)

func TestHashCommands_RESP(t *testing.T) {
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

	resp := writeAndRead("*6\r\n$4\r\nHSET\r\n$6\r\nmyhash\r\n$2\r\nf1\r\n$2\r\nv1\r\n$2\r\nf2\r\n$2\r\nv2\r\n")
	if resp != ":2\r\n" {
		t.Fatalf("HSET response = %q, want %q", resp, ":2\\r\\n")
	}

	resp = writeAndRead("*3\r\n$4\r\nHGET\r\n$6\r\nmyhash\r\n$2\r\nf1\r\n")
	if resp != "$2\r\nv1\r\n" {
		t.Fatalf("HGET response = %q, want %q", resp, "$2\\r\\nv1\\r\\n")
	}

	resp = writeAndRead("*2\r\n$7\r\nHGETALL\r\n$6\r\nmyhash\r\n")
	if !strings.HasPrefix(resp, "*4\r\n") {
		t.Fatalf("HGETALL response = %q, want array response", resp)
	}
	if !strings.Contains(resp, "$2\r\nf1\r\n$2\r\nv1\r\n") {
		t.Fatalf("HGETALL missing first pair: %q", resp)
	}
	if !strings.Contains(resp, "$2\r\nf2\r\n$2\r\nv2\r\n") {
		t.Fatalf("HGETALL missing second pair: %q", resp)
	}

	resp = writeAndRead("*2\r\n$4\r\nHLEN\r\n$6\r\nmyhash\r\n")
	if resp != ":2\r\n" {
		t.Fatalf("HLEN response = %q, want %q", resp, ":2\\r\\n")
	}

	resp = writeAndRead("*3\r\n$4\r\nHDEL\r\n$6\r\nmyhash\r\n$2\r\nf2\r\n")
	if resp != ":1\r\n" {
		t.Fatalf("HDEL response = %q, want %q", resp, ":1\\r\\n")
	}

	resp = writeAndRead("*3\r\n$7\r\nHEXISTS\r\n$6\r\nmyhash\r\n$2\r\nf2\r\n")
	if resp != ":0\r\n" {
		t.Fatalf("HEXISTS response = %q, want %q", resp, ":0\\r\\n")
	}

	resp = writeAndRead("*2\r\n$3\r\nGET\r\n$6\r\nmyhash\r\n")
	if !strings.HasPrefix(resp, "-WRONGTYPE ") {
		t.Fatalf("GET on hash response = %q, want WRONGTYPE", resp)
	}

	resp = writeAndRead("*2\r\n$6\r\nSTRLEN\r\n$6\r\nmyhash\r\n")
	if !strings.HasPrefix(resp, "-WRONGTYPE ") {
		t.Fatalf("STRLEN on hash response = %q, want WRONGTYPE", resp)
	}

	resp = writeAndRead("*3\r\n$6\r\nGETSET\r\n$6\r\nmyhash\r\n$1\r\nz\r\n")
	if !strings.HasPrefix(resp, "-WRONGTYPE ") {
		t.Fatalf("GETSET on hash response = %q, want WRONGTYPE", resp)
	}
}
