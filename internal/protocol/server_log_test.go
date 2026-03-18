package protocol

import (
	"bytes"
	"log"
	"net"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/engine/memory"
)

func TestServer_QuietConnectionsSuppressesConnectLogs(t *testing.T) {
	store := memory.NewStore(memory.DefaultConfig())
	defer store.Close()
	server := NewServer(":0", NewMemoryStoreAdapter(store))
	server.SetQuietConnections(true)

	var buf bytes.Buffer
	originalWriter := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(originalWriter)

	go func() { _ = server.Start() }()
	defer server.Stop()

	addr := waitForServer(t, server, 2*time.Second)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	_ = conn.Close()
	time.Sleep(50 * time.Millisecond)

	if bytes.Contains(buf.Bytes(), []byte("Client connected")) || bytes.Contains(buf.Bytes(), []byte("Client disconnected")) {
		t.Fatalf("expected quiet connection mode to suppress connect/disconnect logs, got %q", buf.String())
	}
}
