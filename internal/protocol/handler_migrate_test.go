package protocol

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/engine/memory"
)

func TestMigrate_BasicKey(t *testing.T) {
	sourceStore := memory.NewStore(memory.DefaultConfig())
	sourceAdapter := NewMemoryStoreAdapter(sourceStore)
	sourceServer := NewServer(":0", sourceAdapter)

	go func() {
		sourceServer.Start()
	}()
	defer sourceServer.Stop()

	sourceAddr := waitForServer(t, sourceServer, 2*time.Second)

	targetStore := memory.NewStore(memory.DefaultConfig())
	targetAdapter := NewMemoryStoreAdapter(targetStore)
	targetServer := NewServer(":0", targetAdapter)

	go func() {
		targetServer.Start()
	}()
	defer targetServer.Stop()

	targetAddr := waitForServer(t, targetServer, 2*time.Second)

	sourceConn, err := net.Dial("tcp", sourceAddr)
	if err != nil {
		t.Fatalf("failed to connect to source: %v", err)
	}
	defer sourceConn.Close()

	setCmd := "*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n"
	_, err = sourceConn.Write([]byte(setCmd))
	if err != nil {
		t.Fatalf("failed to write SET: %v", err)
	}

	buf := make([]byte, 1024)
	sourceConn.SetReadDeadline(time.Now().Add(time.Second))
	n, err := sourceConn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read SET response: %v", err)
	}
	if string(buf[:n]) != "+OK\r\n" {
		t.Errorf("SET expected +OK\\r\\n, got %q", string(buf[:n]))
	}

	targetHost, targetPort, _ := net.SplitHostPort(targetAddr)
	migrateCmd := fmt.Sprintf("*6\r\n$7\r\nMIGRATE\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n$3\r\nfoo\r\n$1\r\n0\r\n$4\r\n5000\r\n",
		len(targetHost), targetHost, len(targetPort), targetPort)

	_, err = sourceConn.Write([]byte(migrateCmd))
	if err != nil {
		t.Fatalf("failed to write MIGRATE: %v", err)
	}

	sourceConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err = sourceConn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read MIGRATE response: %v", err)
	}
	if string(buf[:n]) != "+OK\r\n" {
		t.Errorf("MIGRATE expected +OK\\r\\n, got %q", string(buf[:n]))
	}

	getCmd := "*2\r\n$3\r\nGET\r\n$3\r\nfoo\r\n"
	_, err = sourceConn.Write([]byte(getCmd))
	if err != nil {
		t.Fatalf("failed to write GET on source: %v", err)
	}

	sourceConn.SetReadDeadline(time.Now().Add(time.Second))
	n, err = sourceConn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read GET response from source: %v", err)
	}
	if string(buf[:n]) != "$-1\r\n" {
		t.Errorf("GET on source after MIGRATE expected $-1\\r\\n (nil), got %q", string(buf[:n]))
	}

	targetConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		t.Fatalf("failed to connect to target: %v", err)
	}
	defer targetConn.Close()

	_, err = targetConn.Write([]byte(getCmd))
	if err != nil {
		t.Fatalf("failed to write GET on target: %v", err)
	}

	targetConn.SetReadDeadline(time.Now().Add(time.Second))
	n, err = targetConn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read GET response from target: %v", err)
	}
	if string(buf[:n]) != "$3\r\nbar\r\n" {
		t.Errorf("GET on target expected $3\\r\\nbar\\r\\n, got %q", string(buf[:n]))
	}
}

func TestMigrate_WithCopy(t *testing.T) {
	sourceStore := memory.NewStore(memory.DefaultConfig())
	sourceAdapter := NewMemoryStoreAdapter(sourceStore)
	sourceServer := NewServer(":0", sourceAdapter)

	go func() {
		sourceServer.Start()
	}()
	defer sourceServer.Stop()

	sourceAddr := waitForServer(t, sourceServer, 2*time.Second)

	targetStore := memory.NewStore(memory.DefaultConfig())
	targetAdapter := NewMemoryStoreAdapter(targetStore)
	targetServer := NewServer(":0", targetAdapter)

	go func() {
		targetServer.Start()
	}()
	defer targetServer.Stop()

	targetAddr := waitForServer(t, targetServer, 2*time.Second)

	sourceConn, err := net.Dial("tcp", sourceAddr)
	if err != nil {
		t.Fatalf("failed to connect to source: %v", err)
	}
	defer sourceConn.Close()

	setCmd := "*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n"
	_, err = sourceConn.Write([]byte(setCmd))
	if err != nil {
		t.Fatalf("failed to write SET: %v", err)
	}

	buf := make([]byte, 1024)
	sourceConn.SetReadDeadline(time.Now().Add(time.Second))
	n, err := sourceConn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read SET response: %v", err)
	}
	if string(buf[:n]) != "+OK\r\n" {
		t.Errorf("SET expected +OK\\r\\n, got %q", string(buf[:n]))
	}

	targetHost, targetPort, _ := net.SplitHostPort(targetAddr)
	migrateCmd := fmt.Sprintf("*7\r\n$7\r\nMIGRATE\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n$3\r\nfoo\r\n$1\r\n0\r\n$4\r\n5000\r\n$4\r\nCOPY\r\n",
		len(targetHost), targetHost, len(targetPort), targetPort)

	_, err = sourceConn.Write([]byte(migrateCmd))
	if err != nil {
		t.Fatalf("failed to write MIGRATE: %v", err)
	}

	sourceConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err = sourceConn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read MIGRATE response: %v", err)
	}
	if string(buf[:n]) != "+OK\r\n" {
		t.Errorf("MIGRATE expected +OK\\r\\n, got %q", string(buf[:n]))
	}

	getCmd := "*2\r\n$3\r\nGET\r\n$3\r\nfoo\r\n"
	_, err = sourceConn.Write([]byte(getCmd))
	if err != nil {
		t.Fatalf("failed to write GET on source: %v", err)
	}

	sourceConn.SetReadDeadline(time.Now().Add(time.Second))
	n, err = sourceConn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read GET response from source: %v", err)
	}
	if string(buf[:n]) != "$3\r\nbar\r\n" {
		t.Errorf("GET on source with COPY expected $3\\r\\nbar\\r\\n, got %q", string(buf[:n]))
	}

	targetConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		t.Fatalf("failed to connect to target: %v", err)
	}
	defer targetConn.Close()

	_, err = targetConn.Write([]byte(getCmd))
	if err != nil {
		t.Fatalf("failed to write GET on target: %v", err)
	}

	targetConn.SetReadDeadline(time.Now().Add(time.Second))
	n, err = targetConn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read GET response from target: %v", err)
	}
	if string(buf[:n]) != "$3\r\nbar\r\n" {
		t.Errorf("GET on target expected $3\\r\\nbar\\r\\n, got %q", string(buf[:n]))
	}
}

func TestMigrate_NoSuchKey(t *testing.T) {
	sourceStore := memory.NewStore(memory.DefaultConfig())
	sourceAdapter := NewMemoryStoreAdapter(sourceStore)
	sourceServer := NewServer(":0", sourceAdapter)

	go func() {
		sourceServer.Start()
	}()
	defer sourceServer.Stop()

	sourceAddr := waitForServer(t, sourceServer, 2*time.Second)

	targetStore := memory.NewStore(memory.DefaultConfig())
	targetAdapter := NewMemoryStoreAdapter(targetStore)
	targetServer := NewServer(":0", targetAdapter)

	go func() {
		targetServer.Start()
	}()
	defer targetServer.Stop()

	targetAddr := waitForServer(t, targetServer, 2*time.Second)

	sourceConn, err := net.Dial("tcp", sourceAddr)
	if err != nil {
		t.Fatalf("failed to connect to source: %v", err)
	}
	defer sourceConn.Close()

	targetHost, targetPort, _ := net.SplitHostPort(targetAddr)
	migrateCmd := fmt.Sprintf("*6\r\n$7\r\nMIGRATE\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n$11\r\nnonexistent\r\n$1\r\n0\r\n$4\r\n5000\r\n",
		len(targetHost), targetHost, len(targetPort), targetPort)

	_, err = sourceConn.Write([]byte(migrateCmd))
	if err != nil {
		t.Fatalf("failed to write MIGRATE: %v", err)
	}

	buf := make([]byte, 1024)
	sourceConn.SetReadDeadline(time.Now().Add(time.Second))
	n, err := sourceConn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read MIGRATE response: %v", err)
	}

	response := string(buf[:n])
	if response[0] != '-' {
		t.Errorf("MIGRATE with nonexistent key expected error, got %q", response)
	}
}

func TestMigrate_WrongArgs(t *testing.T) {
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

	migrateCmd := "*3\r\n$7\r\nMIGRATE\r\n$9\r\nlocalhost\r\n$4\r\n6379\r\n"
	_, err = conn.Write([]byte(migrateCmd))
	if err != nil {
		t.Fatalf("failed to write MIGRATE: %v", err)
	}

	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read MIGRATE response: %v", err)
	}

	response := string(buf[:n])
	if response[0] != '-' {
		t.Errorf("MIGRATE with wrong args expected error, got %q", response)
	}
}

func TestMigrate_WithKeys_MultipleKeys_SomeMissing(t *testing.T) {
	sourceStore := memory.NewStore(memory.DefaultConfig())
	sourceAdapter := NewMemoryStoreAdapter(sourceStore)
	sourceServer := NewServer(":0", sourceAdapter)

	go func() {
		sourceServer.Start()
	}()
	defer sourceServer.Stop()

	sourceAddr := waitForServer(t, sourceServer, 2*time.Second)

	targetStore := memory.NewStore(memory.DefaultConfig())
	targetAdapter := NewMemoryStoreAdapter(targetStore)
	targetServer := NewServer(":0", targetAdapter)

	go func() {
		targetServer.Start()
	}()
	defer targetServer.Stop()

	targetAddr := waitForServer(t, targetServer, 2*time.Second)

	sourceConn, err := net.Dial("tcp", sourceAddr)
	if err != nil {
		t.Fatalf("failed to connect to source: %v", err)
	}
	defer sourceConn.Close()

	response := sendCommandWithTimeout(t, sourceConn, respCommand("SET", "k1", "v1"), time.Second)
	if response != "+OK\r\n" {
		t.Errorf("SET k1 expected +OK\\r\\n, got %q", response)
	}

	response = sendCommandWithTimeout(t, sourceConn, respCommand("SET", "k2", "v2"), time.Second)
	if response != "+OK\r\n" {
		t.Errorf("SET k2 expected +OK\\r\\n, got %q", response)
	}

	targetHost, targetPort, _ := net.SplitHostPort(targetAddr)
	migrateCmd := respCommand("MIGRATE", targetHost, targetPort, "", "0", "5000", "KEYS", "k1", "k2", "k3")
	response = sendCommandWithTimeout(t, sourceConn, migrateCmd, 5*time.Second)
	if response != "+OK\r\n" {
		t.Errorf("MIGRATE expected +OK\\r\\n, got %q", response)
	}

	response = sendCommandWithTimeout(t, sourceConn, respCommand("GET", "k1"), time.Second)
	if response != "$-1\r\n" {
		t.Errorf("GET on source for k1 expected $-1\\r\\n, got %q", response)
	}

	response = sendCommandWithTimeout(t, sourceConn, respCommand("GET", "k2"), time.Second)
	if response != "$-1\r\n" {
		t.Errorf("GET on source for k2 expected $-1\\r\\n, got %q", response)
	}

	targetConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		t.Fatalf("failed to connect to target: %v", err)
	}
	defer targetConn.Close()

	response = sendCommandWithTimeout(t, targetConn, respCommand("GET", "k1"), time.Second)
	if response != "$2\r\nv1\r\n" {
		t.Errorf("GET on target for k1 expected $2\\r\\nv1\\r\\n, got %q", response)
	}

	response = sendCommandWithTimeout(t, targetConn, respCommand("GET", "k2"), time.Second)
	if response != "$2\r\nv2\r\n" {
		t.Errorf("GET on target for k2 expected $2\\r\\nv2\\r\\n, got %q", response)
	}
}

func TestMigrate_WithKeys_AllMissing_ReturnsNOKEY(t *testing.T) {
	sourceStore := memory.NewStore(memory.DefaultConfig())
	sourceAdapter := NewMemoryStoreAdapter(sourceStore)
	sourceServer := NewServer(":0", sourceAdapter)

	go func() {
		sourceServer.Start()
	}()
	defer sourceServer.Stop()

	sourceAddr := waitForServer(t, sourceServer, 2*time.Second)

	targetStore := memory.NewStore(memory.DefaultConfig())
	targetAdapter := NewMemoryStoreAdapter(targetStore)
	targetServer := NewServer(":0", targetAdapter)

	go func() {
		targetServer.Start()
	}()
	defer targetServer.Stop()

	targetAddr := waitForServer(t, targetServer, 2*time.Second)

	sourceConn, err := net.Dial("tcp", sourceAddr)
	if err != nil {
		t.Fatalf("failed to connect to source: %v", err)
	}
	defer sourceConn.Close()

	targetHost, targetPort, _ := net.SplitHostPort(targetAddr)
	migrateCmd := respCommand("MIGRATE", targetHost, targetPort, "", "0", "5000", "KEYS", "k1", "k2")
	response := sendCommandWithTimeout(t, sourceConn, migrateCmd, 5*time.Second)
	if response != "+NOKEY\r\n" {
		t.Errorf("MIGRATE expected +NOKEY\\r\\n, got %q", response)
	}
}

func TestMigrate_WithKeys_RejectsNonEmptyKeyArg(t *testing.T) {
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

	migrateCmd := respCommand("MIGRATE", "127.0.0.1", "6379", "foo", "0", "5000", "KEYS", "k1")
	response := sendCommandWithTimeout(t, conn, migrateCmd, time.Second)
	if !strings.HasPrefix(response, "-ERR") {
		t.Errorf("MIGRATE expected -ERR, got %q", response)
	}
}

func TestMigrate_RetriesOnDialFailureThenSucceeds(t *testing.T) {
	sourceStore := memory.NewStore(memory.DefaultConfig())
	sourceAdapter := NewMemoryStoreAdapter(sourceStore)
	sourceServer := NewServer(":0", sourceAdapter)

	go func() {
		sourceServer.Start()
	}()
	defer sourceServer.Stop()

	sourceAddr := waitForServer(t, sourceServer, 2*time.Second)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve target port: %v", err)
	}
	targetAddr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("failed to release target port: %v", err)
	}

	targetStore := memory.NewStore(memory.DefaultConfig())
	targetAdapter := NewMemoryStoreAdapter(targetStore)
	targetServer := NewServer(targetAddr, targetAdapter)

	go func() {
		time.Sleep(150 * time.Millisecond)
		targetServer.Start()
	}()
	defer targetServer.Stop()

	sourceConn, err := net.Dial("tcp", sourceAddr)
	if err != nil {
		t.Fatalf("failed to connect to source: %v", err)
	}
	defer sourceConn.Close()

	response := sendCommandWithTimeout(t, sourceConn, respCommand("SET", "retry", "value"), time.Second)
	if response != "+OK\r\n" {
		t.Errorf("SET expected +OK\\r\\n, got %q", response)
	}

	targetHost, targetPort, _ := net.SplitHostPort(targetAddr)
	migrateCmd := respCommand("MIGRATE", targetHost, targetPort, "retry", "0", "5000")
	response = sendCommandWithTimeout(t, sourceConn, migrateCmd, 5*time.Second)
	if response != "+OK\r\n" {
		t.Errorf("MIGRATE expected +OK\\r\\n, got %q", response)
	}

	response = sendCommandWithTimeout(t, sourceConn, respCommand("GET", "retry"), time.Second)
	if response != "$-1\r\n" {
		t.Errorf("GET on source expected $-1\\r\\n, got %q", response)
	}

	targetConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		t.Fatalf("failed to connect to target: %v", err)
	}
	defer targetConn.Close()

	response = sendCommandWithTimeout(t, targetConn, respCommand("GET", "retry"), time.Second)
	if response != "$5\r\nvalue\r\n" {
		t.Errorf("GET on target expected $5\\r\\nvalue\\r\\n, got %q", response)
	}
}

func respCommand(args ...string) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&builder, "$%d\r\n%s\r\n", len(arg), arg)
	}
	return builder.String()
}

func sendCommandWithTimeout(t *testing.T, conn net.Conn, cmd string, timeout time.Duration) string {
	t.Helper()
	_, err := conn.Write([]byte(cmd))
	if err != nil {
		t.Fatalf("failed to write command: %v", err)
	}

	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(timeout))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	return string(buf[:n])
}
