package migrate

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/cluster/hash"
	"github.com/10yihang/autocache/internal/engine/memory"
	"github.com/10yihang/autocache/internal/protocol"
)

func waitForServer(t *testing.T, s *protocol.Server, timeout time.Duration) string {
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

func TestWorker_MigrateSlot(t *testing.T) {
	sourceStore := memory.NewStore(memory.DefaultConfig())
	sourceAdapter := protocol.NewMemoryStoreAdapter(sourceStore)
	sourceServer := protocol.NewServer(":0", sourceAdapter)

	go func() {
		sourceServer.Start()
	}()
	defer sourceServer.Stop()

	sourceAddr := waitForServer(t, sourceServer, 2*time.Second)
	_ = sourceAddr

	targetStore := memory.NewStore(memory.DefaultConfig())
	targetAdapter := protocol.NewMemoryStoreAdapter(targetStore)
	targetServer := protocol.NewServer(":0", targetAdapter)

	go func() {
		targetServer.Start()
	}()
	defer targetServer.Stop()

	targetAddr := waitForServer(t, targetServer, 2*time.Second)

	ctx := context.Background()
	sourceAdapter.Set(ctx, "{user}key1", "value1", 0)
	sourceAdapter.Set(ctx, "{user}key2", "value2", 0)
	sourceAdapter.Set(ctx, "{user}key3", "value3", 0)

	expectedSlot := hash.KeySlot("{user}key1")

	worker := NewWorker(sourceAdapter)
	worker.SetTimeout(5 * time.Second)

	err := worker.MigrateSlot(ctx, expectedSlot, targetAddr, false)
	if err != nil {
		t.Fatalf("MigrateSlot failed: %v", err)
	}

	progress := worker.GetProgress(expectedSlot)
	if progress == nil {
		t.Fatal("no progress found")
	}
	if progress.TotalKeys != 3 {
		t.Errorf("expected 3 total keys, got %d", progress.TotalKeys)
	}
	if progress.MigratedKeys != 3 {
		t.Errorf("expected 3 migrated keys, got %d", progress.MigratedKeys)
	}
	if progress.Status != MigrationStatusCompleted {
		t.Errorf("expected status completed, got %v", progress.Status)
	}

	for _, key := range []string{"{user}key1", "{user}key2", "{user}key3"} {
		_, err := sourceAdapter.GetBytes(ctx, key)
		if err == nil {
			t.Errorf("key %s should be deleted from source", key)
		}
	}

	targetConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		t.Fatalf("failed to connect to target: %v", err)
	}
	defer targetConn.Close()

	for _, key := range []string{"{user}key1", "{user}key2", "{user}key3"} {
		getCmd := fmt.Sprintf("*2\r\n$3\r\nGET\r\n$%d\r\n%s\r\n", len(key), key)
		_, err = targetConn.Write([]byte(getCmd))
		if err != nil {
			t.Fatalf("failed to write GET: %v", err)
		}

		buf := make([]byte, 1024)
		targetConn.SetReadDeadline(time.Now().Add(time.Second))
		n, err := targetConn.Read(buf)
		if err != nil {
			t.Fatalf("failed to read GET response: %v", err)
		}
		if string(buf[:n])[0] == '-' || string(buf[:n]) == "$-1\r\n" {
			t.Errorf("key %s not found on target, got: %s", key, string(buf[:n]))
		}
	}
}

func TestWorker_EmptySlot(t *testing.T) {
	store := memory.NewStore(memory.DefaultConfig())
	adapter := protocol.NewMemoryStoreAdapter(store)
	server := protocol.NewServer(":0", adapter)

	go func() {
		server.Start()
	}()
	defer server.Stop()

	targetAddr := waitForServer(t, server, 2*time.Second)

	worker := NewWorker(adapter)
	ctx := context.Background()

	err := worker.MigrateSlot(ctx, 1000, targetAddr, false)
	if err != nil {
		t.Fatalf("MigrateSlot failed for empty slot: %v", err)
	}

	progress := worker.GetProgress(1000)
	if progress == nil {
		t.Fatal("no progress found")
	}
	if progress.TotalKeys != 0 {
		t.Errorf("expected 0 total keys, got %d", progress.TotalKeys)
	}
	if progress.Status != MigrationStatusCompleted {
		t.Errorf("expected status completed, got %v", progress.Status)
	}
}

func TestWorker_GetProgress(t *testing.T) {
	store := memory.NewStore(memory.DefaultConfig())
	adapter := protocol.NewMemoryStoreAdapter(store)

	worker := NewWorker(adapter)

	progress := worker.GetProgress(999)
	if progress != nil {
		t.Errorf("expected nil progress for non-existent slot, got %v", progress)
	}
}
