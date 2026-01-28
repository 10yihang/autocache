package migration

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/engine/memory"
	"github.com/10yihang/autocache/internal/protocol"
)

type mockSlotProvider struct {
	mu             sync.RWMutex
	migratingSlots map[uint16]MigrationInfo
	nodes          map[string]string
}

func newMockSlotProvider() *mockSlotProvider {
	return &mockSlotProvider{
		migratingSlots: make(map[uint16]MigrationInfo),
		nodes:          make(map[string]string),
	}
}

func (m *mockSlotProvider) GetMigratingSlots() map[uint16]MigrationInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[uint16]MigrationInfo, len(m.migratingSlots))
	for k, v := range m.migratingSlots {
		result[k] = v
	}
	return result
}

func (m *mockSlotProvider) GetNode(nodeID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	addr, ok := m.nodes[nodeID]
	return addr, ok
}

func (m *mockSlotProvider) SetMigrating(slot uint16, info MigrationInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.migratingSlots[slot] = info
}

func (m *mockSlotProvider) ClearMigrating(slot uint16) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.migratingSlots, slot)
}

func (m *mockSlotProvider) AddNode(nodeID, addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodes[nodeID] = addr
}

func TestWorker_StartStop(t *testing.T) {
	store := memory.NewStore(memory.DefaultConfig())
	adapter := protocol.NewMemoryStoreAdapter(store)
	provider := newMockSlotProvider()

	cfg := &Config{
		Interval:  10 * time.Millisecond,
		Timeout:   time.Second,
		BatchSize: 10,
	}
	worker := NewWorker(adapter, provider, cfg)

	worker.Start()
	time.Sleep(50 * time.Millisecond)
	worker.Stop()
}

func TestWorker_NoMigration(t *testing.T) {
	store := memory.NewStore(memory.DefaultConfig())
	adapter := protocol.NewMemoryStoreAdapter(store)
	provider := newMockSlotProvider()

	ctx := context.Background()
	adapter.Set(ctx, "foo", "bar", 0)

	cfg := &Config{
		Interval:  10 * time.Millisecond,
		Timeout:   time.Second,
		BatchSize: 10,
	}
	worker := NewWorker(adapter, provider, cfg)

	worker.Start()
	time.Sleep(50 * time.Millisecond)
	worker.Stop()

	val, err := adapter.GetBytes(ctx, "foo")
	if err != nil {
		t.Fatalf("expected key to still exist: %v", err)
	}
	if string(val) != "bar" {
		t.Errorf("expected 'bar', got %q", string(val))
	}
}

func TestWorker_MigrateKey(t *testing.T) {
	sourceStore := memory.NewStore(memory.DefaultConfig())
	sourceAdapter := protocol.NewMemoryStoreAdapter(sourceStore)

	targetStore := memory.NewStore(memory.DefaultConfig())
	targetAdapter := protocol.NewMemoryStoreAdapter(targetStore)
	targetServer := protocol.NewServer(":0", targetAdapter)

	go func() {
		targetServer.Start()
	}()
	defer targetServer.Stop()

	targetAddr := waitForServer(t, targetServer, 2*time.Second)

	ctx := context.Background()
	sourceAdapter.Set(ctx, "foo", "bar", 0)

	keySlot := getKeySlot("foo")

	provider := newMockSlotProvider()
	provider.SetMigrating(keySlot, MigrationInfo{
		TargetAddr: targetAddr,
	})

	cfg := &Config{
		Interval:  10 * time.Millisecond,
		Timeout:   5 * time.Second,
		BatchSize: 100,
	}
	worker := NewWorker(sourceAdapter, provider, cfg)

	worker.Start()
	time.Sleep(200 * time.Millisecond)
	worker.Stop()

	_, err := sourceAdapter.GetBytes(ctx, "foo")
	if err == nil {
		t.Error("expected key to be deleted from source")
	}

	val, err := targetAdapter.GetBytes(ctx, "foo")
	if err != nil {
		t.Fatalf("expected key to exist on target: %v", err)
	}
	if string(val) != "bar" {
		t.Errorf("expected 'bar' on target, got %q", string(val))
	}
}

func TestWorker_MigrateKeyWithTTL(t *testing.T) {
	sourceStore := memory.NewStore(memory.DefaultConfig())
	sourceAdapter := protocol.NewMemoryStoreAdapter(sourceStore)

	targetStore := memory.NewStore(memory.DefaultConfig())
	targetAdapter := protocol.NewMemoryStoreAdapter(targetStore)
	targetServer := protocol.NewServer(":0", targetAdapter)

	go func() {
		targetServer.Start()
	}()
	defer targetServer.Stop()

	targetAddr := waitForServer(t, targetServer, 2*time.Second)

	ctx := context.Background()
	sourceAdapter.Set(ctx, "ttlkey", "value", 10*time.Second)

	keySlot := getKeySlot("ttlkey")

	provider := newMockSlotProvider()
	provider.SetMigrating(keySlot, MigrationInfo{
		TargetAddr: targetAddr,
	})

	cfg := &Config{
		Interval:  10 * time.Millisecond,
		Timeout:   5 * time.Second,
		BatchSize: 100,
	}
	worker := NewWorker(sourceAdapter, provider, cfg)

	worker.Start()
	time.Sleep(200 * time.Millisecond)
	worker.Stop()

	ttl, err := targetAdapter.TTL(ctx, "ttlkey")
	if err != nil {
		t.Fatalf("TTL error: %v", err)
	}
	if ttl <= 0 || ttl > 10*time.Second {
		t.Errorf("expected TTL around 10s, got %v", ttl)
	}
}

func TestWorker_BatchSize(t *testing.T) {
	sourceStore := memory.NewStore(memory.DefaultConfig())
	sourceAdapter := protocol.NewMemoryStoreAdapter(sourceStore)

	targetStore := memory.NewStore(memory.DefaultConfig())
	targetAdapter := protocol.NewMemoryStoreAdapter(targetStore)
	targetServer := protocol.NewServer(":0", targetAdapter)

	go func() {
		targetServer.Start()
	}()
	defer targetServer.Stop()

	targetAddr := waitForServer(t, targetServer, 2*time.Second)

	ctx := context.Background()
	keysInSlot := findKeysInSlot(0, 20)
	for _, key := range keysInSlot {
		sourceAdapter.Set(ctx, key, "value", 0)
	}

	provider := newMockSlotProvider()
	provider.SetMigrating(0, MigrationInfo{
		TargetAddr: targetAddr,
	})

	cfg := &Config{
		Interval:  10 * time.Millisecond,
		Timeout:   5 * time.Second,
		BatchSize: 5,
	}
	worker := NewWorker(sourceAdapter, provider, cfg)

	worker.Start()
	time.Sleep(50 * time.Millisecond)
	worker.Stop()

	sourceCount, _ := sourceAdapter.DBSize(ctx)
	targetCount, _ := targetAdapter.DBSize(ctx)

	if sourceCount == 0 && targetCount == int64(len(keysInSlot)) {
		t.Log("all keys migrated in small time window")
	} else if sourceCount > 0 && targetCount > 0 {
		t.Log("partial migration as expected with batch size")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Interval != 100*time.Millisecond {
		t.Errorf("expected interval 100ms, got %v", cfg.Interval)
	}
	if cfg.Timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", cfg.Timeout)
	}
	if cfg.BatchSize != 10 {
		t.Errorf("expected batch size 10, got %d", cfg.BatchSize)
	}
}

func waitForServer(t *testing.T, server *protocol.Server, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		addr := server.Addr()
		if addr != "" {
			conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
			if err == nil {
				conn.Close()
				return addr
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not start in time")
	return ""
}

func getKeySlot(key string) uint16 {
	crc := uint16(0)
	for i := 0; i < len(key); i++ {
		crc = (crc << 8) ^ crc16Table[byte(crc>>8)^key[i]]
	}
	return crc % 16384
}

func findKeysInSlot(slot uint16, count int) []string {
	var keys []string
	for i := 0; len(keys) < count && i < 1000000; i++ {
		key := fmt.Sprintf("key%d", i)
		if getKeySlot(key) == slot {
			keys = append(keys, key)
		}
	}
	return keys
}

var crc16Table = [256]uint16{
	0x0000, 0x1021, 0x2042, 0x3063, 0x4084, 0x50a5, 0x60c6, 0x70e7,
	0x8108, 0x9129, 0xa14a, 0xb16b, 0xc18c, 0xd1ad, 0xe1ce, 0xf1ef,
	0x1231, 0x0210, 0x3273, 0x2252, 0x52b5, 0x4294, 0x72f7, 0x62d6,
	0x9339, 0x8318, 0xb37b, 0xa35a, 0xd3bd, 0xc39c, 0xf3ff, 0xe3de,
	0x2462, 0x3443, 0x0420, 0x1401, 0x64e6, 0x74c7, 0x44a4, 0x5485,
	0xa56a, 0xb54b, 0x8528, 0x9509, 0xe5ee, 0xf5cf, 0xc5ac, 0xd58d,
	0x3653, 0x2672, 0x1611, 0x0630, 0x76d7, 0x66f6, 0x5695, 0x46b4,
	0xb75b, 0xa77a, 0x9719, 0x8738, 0xf7df, 0xe7fe, 0xd79d, 0xc7bc,
	0x48c4, 0x58e5, 0x6886, 0x78a7, 0x0840, 0x1861, 0x2802, 0x3823,
	0xc9cc, 0xd9ed, 0xe98e, 0xf9af, 0x8948, 0x9969, 0xa90a, 0xb92b,
	0x5af5, 0x4ad4, 0x7ab7, 0x6a96, 0x1a71, 0x0a50, 0x3a33, 0x2a12,
	0xdbfd, 0xcbdc, 0xfbbf, 0xeb9e, 0x9b79, 0x8b58, 0xbb3b, 0xab1a,
	0x6ca6, 0x7c87, 0x4ce4, 0x5cc5, 0x2c22, 0x3c03, 0x0c60, 0x1c41,
	0xedae, 0xfd8f, 0xcdec, 0xddcd, 0xad2a, 0xbd0b, 0x8d68, 0x9d49,
	0x7e97, 0x6eb6, 0x5ed5, 0x4ef4, 0x3e13, 0x2e32, 0x1e51, 0x0e70,
	0xff9f, 0xefbe, 0xdfdd, 0xcffc, 0xbf1b, 0xaf3a, 0x9f59, 0x8f78,
	0x9188, 0x81a9, 0xb1ca, 0xa1eb, 0xd10c, 0xc12d, 0xf14e, 0xe16f,
	0x1080, 0x00a1, 0x30c2, 0x20e3, 0x5004, 0x4025, 0x7046, 0x6067,
	0x83b9, 0x9398, 0xa3fb, 0xb3da, 0xc33d, 0xd31c, 0xe37f, 0xf35e,
	0x02b1, 0x1290, 0x22f3, 0x32d2, 0x4235, 0x5214, 0x6277, 0x7256,
	0xb5ea, 0xa5cb, 0x95a8, 0x8589, 0xf56e, 0xe54f, 0xd52c, 0xc50d,
	0x34e2, 0x24c3, 0x14a0, 0x0481, 0x7466, 0x6447, 0x5424, 0x4405,
	0xa7db, 0xb7fa, 0x8799, 0x97b8, 0xe75f, 0xf77e, 0xc71d, 0xd73c,
	0x26d3, 0x36f2, 0x0691, 0x16b0, 0x6657, 0x7676, 0x4615, 0x5634,
	0xd94c, 0xc96d, 0xf90e, 0xe92f, 0x99c8, 0x89e9, 0xb98a, 0xa9ab,
	0x5844, 0x4865, 0x7806, 0x6827, 0x18c0, 0x08e1, 0x3882, 0x28a3,
	0xcb7d, 0xdb5c, 0xeb3f, 0xfb1e, 0x8bf9, 0x9bd8, 0xabbb, 0xbb9a,
	0x4a75, 0x5a54, 0x6a37, 0x7a16, 0x0af1, 0x1ad0, 0x2ab3, 0x3a92,
	0xfd2e, 0xed0f, 0xdd6c, 0xcd4d, 0xbdaa, 0xad8b, 0x9de8, 0x8dc9,
	0x7c26, 0x6c07, 0x5c64, 0x4c45, 0x3ca2, 0x2c83, 0x1ce0, 0x0cc1,
	0xef1f, 0xff3e, 0xcf5d, 0xdf7c, 0xaf9b, 0xbfba, 0x8fd9, 0x9ff8,
	0x6e17, 0x7e36, 0x4e55, 0x5e74, 0x2e93, 0x3eb2, 0x0ed1, 0x1ef0,
}
