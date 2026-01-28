package protocol

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/engine/memory"
)

func TestRestore_BasicString(t *testing.T) {
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

	serializedValue := []byte{0x00, 'b', 'a', 'r'}
	respCmd := fmt.Sprintf("*4\r\n$7\r\nRESTORE\r\n$3\r\nfoo\r\n$1\r\n0\r\n$%d\r\n%s\r\n",
		len(serializedValue), serializedValue)

	_, err = conn.Write([]byte(respCmd))
	if err != nil {
		t.Fatalf("failed to write RESTORE: %v", err)
	}

	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read RESTORE response: %v", err)
	}
	if string(buf[:n]) != "+OK\r\n" {
		t.Errorf("RESTORE expected +OK\\r\\n, got %q", string(buf[:n]))
	}

	getCmd := "*2\r\n$3\r\nGET\r\n$3\r\nfoo\r\n"
	_, err = conn.Write([]byte(getCmd))
	if err != nil {
		t.Fatalf("failed to write GET: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read GET response: %v", err)
	}
	if string(buf[:n]) != "$3\r\nbar\r\n" {
		t.Errorf("GET expected $3\\r\\nbar\\r\\n, got %q", string(buf[:n]))
	}
}

func TestRestore_WithTTL(t *testing.T) {
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

	serializedValue := []byte{0x00, 'b', 'a', 'r'}
	respCmd := fmt.Sprintf("*4\r\n$7\r\nRESTORE\r\n$3\r\nfoo\r\n$5\r\n10000\r\n$%d\r\n%s\r\n",
		len(serializedValue), serializedValue)

	_, err = conn.Write([]byte(respCmd))
	if err != nil {
		t.Fatalf("failed to write RESTORE: %v", err)
	}

	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read RESTORE response: %v", err)
	}
	if string(buf[:n]) != "+OK\r\n" {
		t.Errorf("RESTORE expected +OK\\r\\n, got %q", string(buf[:n]))
	}

	pttlCmd := "*2\r\n$4\r\nPTTL\r\n$3\r\nfoo\r\n"
	_, err = conn.Write([]byte(pttlCmd))
	if err != nil {
		t.Fatalf("failed to write PTTL: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read PTTL response: %v", err)
	}

	response := string(buf[:n])
	if response[0] != ':' {
		t.Errorf("PTTL expected integer response, got %q", response)
	}
}

func TestRestore_Replace(t *testing.T) {
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

	setCmd := "*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$8\r\nexisting\r\n"
	_, err = conn.Write([]byte(setCmd))
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
		t.Errorf("SET expected +OK\\r\\n, got %q", string(buf[:n]))
	}

	serializedValue := []byte{0x00, 'n', 'e', 'w'}
	respCmd := fmt.Sprintf("*5\r\n$7\r\nRESTORE\r\n$3\r\nfoo\r\n$1\r\n0\r\n$%d\r\n%s\r\n$7\r\nREPLACE\r\n",
		len(serializedValue), serializedValue)

	_, err = conn.Write([]byte(respCmd))
	if err != nil {
		t.Fatalf("failed to write RESTORE REPLACE: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read RESTORE response: %v", err)
	}
	if string(buf[:n]) != "+OK\r\n" {
		t.Errorf("RESTORE REPLACE expected +OK\\r\\n, got %q", string(buf[:n]))
	}

	getCmd := "*2\r\n$3\r\nGET\r\n$3\r\nfoo\r\n"
	_, err = conn.Write([]byte(getCmd))
	if err != nil {
		t.Fatalf("failed to write GET: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read GET response: %v", err)
	}
	if string(buf[:n]) != "$3\r\nnew\r\n" {
		t.Errorf("GET expected $3\\r\\nnew\\r\\n, got %q", string(buf[:n]))
	}
}

func TestRestore_BusyKey(t *testing.T) {
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

	setCmd := "*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$8\r\nexisting\r\n"
	_, err = conn.Write([]byte(setCmd))
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
		t.Errorf("SET expected +OK\\r\\n, got %q", string(buf[:n]))
	}

	serializedValue := []byte{0x00, 'n', 'e', 'w'}
	respCmd := fmt.Sprintf("*4\r\n$7\r\nRESTORE\r\n$3\r\nfoo\r\n$1\r\n0\r\n$%d\r\n%s\r\n",
		len(serializedValue), serializedValue)

	_, err = conn.Write([]byte(respCmd))
	if err != nil {
		t.Fatalf("failed to write RESTORE: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read RESTORE response: %v", err)
	}

	response := string(buf[:n])
	if response != "-BUSYKEY Target key name already exists.\r\n" {
		t.Errorf("RESTORE expected -BUSYKEY error, got %q", response)
	}
}

func TestRestore_WrongArgs(t *testing.T) {
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

	respCmd := "*2\r\n$7\r\nRESTORE\r\n$3\r\nfoo\r\n"
	_, err = conn.Write([]byte(respCmd))
	if err != nil {
		t.Fatalf("failed to write RESTORE: %v", err)
	}

	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read RESTORE response: %v", err)
	}

	response := string(buf[:n])
	if response[0] != '-' {
		t.Errorf("RESTORE with wrong args expected error, got %q", response)
	}
}
