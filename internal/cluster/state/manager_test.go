package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type mockProvider struct {
	mu             sync.RWMutex
	nodeID         string
	nodes          []NodeInfo
	slotMap        [16384]string
	migratingSlots map[uint16]MigrationState
	currentEpoch   uint64
	myEpoch        uint64
	restoredState  *PersistentState
}

func (m *mockProvider) GetNodeID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.nodeID
}

func (m *mockProvider) GetNodeInfos() []NodeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]NodeInfo, len(m.nodes))
	copy(result, m.nodes)
	return result
}

func (m *mockProvider) GetSlotMap() [16384]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.slotMap
}

func (m *mockProvider) GetMigratingSlots() map[uint16]MigrationState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[uint16]MigrationState)
	for k, v := range m.migratingSlots {
		result[k] = v
	}
	return result
}

func (m *mockProvider) GetCurrentEpoch() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentEpoch
}

func (m *mockProvider) GetMyEpoch() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.myEpoch
}

func (m *mockProvider) RestoreState(state *PersistentState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.restoredState = state
	m.nodeID = state.NodeID
	m.nodes = state.Nodes
	m.slotMap = state.SlotMap
	m.migratingSlots = state.MigratingSlots
	m.currentEpoch = state.CurrentEpoch
	m.myEpoch = state.MyEpoch
	return nil
}

func TestStateManager_NewStateManager(t *testing.T) {
	dir := t.TempDir()

	mgr, err := NewStateManager(dir)
	if err != nil {
		t.Fatalf("NewStateManager failed: %v", err)
	}
	defer mgr.Close()

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("data directory not created")
	}
}

func TestStateManager_SaveLoad(t *testing.T) {
	dir := t.TempDir()

	mgr, err := NewStateManager(dir)
	if err != nil {
		t.Fatalf("NewStateManager failed: %v", err)
	}

	provider := &mockProvider{
		nodeID: "node-abc123",
		nodes: []NodeInfo{
			{ID: "node-abc123", Addr: "127.0.0.1:6379", ClusterPort: 16379, Role: "master"},
			{ID: "node-def456", Addr: "127.0.0.1:6380", ClusterPort: 16380, Role: "replica", MasterID: "node-abc123"},
		},
		migratingSlots: map[uint16]MigrationState{
			100: {SourceNodeID: "node-abc123", TargetNodeID: "node-def456", State: "exporting"},
		},
		currentEpoch: 42,
		myEpoch:      10,
	}
	for i := 0; i < 8192; i++ {
		provider.slotMap[i] = "node-abc123"
	}
	for i := 8192; i < 16384; i++ {
		provider.slotMap[i] = "node-def456"
	}

	mgr.SetProvider(provider)

	if err := mgr.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	mgr.Close()

	if _, err := os.Stat(filepath.Join(dir, stateFileName)); os.IsNotExist(err) {
		t.Fatal("state file not created")
	}

	mgr2, err := NewStateManager(dir)
	if err != nil {
		t.Fatalf("NewStateManager2 failed: %v", err)
	}
	defer mgr2.Close()

	provider2 := &mockProvider{}
	mgr2.SetProvider(provider2)

	if err := mgr2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if provider2.nodeID != "node-abc123" {
		t.Errorf("nodeID mismatch: got %q, want %q", provider2.nodeID, "node-abc123")
	}
	if len(provider2.nodes) != 2 {
		t.Errorf("nodes count mismatch: got %d, want 2", len(provider2.nodes))
	}
	if provider2.currentEpoch != 42 {
		t.Errorf("currentEpoch mismatch: got %d, want 42", provider2.currentEpoch)
	}
	if provider2.myEpoch != 10 {
		t.Errorf("myEpoch mismatch: got %d, want 10", provider2.myEpoch)
	}
	if provider2.slotMap[0] != "node-abc123" {
		t.Errorf("slotMap[0] mismatch: got %q, want %q", provider2.slotMap[0], "node-abc123")
	}
	if provider2.slotMap[8192] != "node-def456" {
		t.Errorf("slotMap[8192] mismatch: got %q, want %q", provider2.slotMap[8192], "node-def456")
	}
	if migration, ok := provider2.migratingSlots[100]; !ok || migration.State != "exporting" {
		t.Errorf("migratingSlots mismatch")
	}
}

func TestStateManager_AtomicWrite(t *testing.T) {
	dir := t.TempDir()

	mgr, err := NewStateManager(dir)
	if err != nil {
		t.Fatalf("NewStateManager failed: %v", err)
	}
	defer mgr.Close()

	provider := &mockProvider{
		nodeID:       "node-test",
		currentEpoch: 1,
	}
	mgr.SetProvider(provider)

	if err := mgr.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	tempPath := filepath.Join(dir, stateFileName+".tmp")
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Error("temp file should not exist after successful save")
	}

	statePath := filepath.Join(dir, stateFileName)
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state file failed: %v", err)
	}

	var state PersistentState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if state.Version != CurrentStateVersion {
		t.Errorf("version mismatch: got %d, want %d", state.Version, CurrentStateVersion)
	}
}

func TestStateManager_MarkDirtyBatching(t *testing.T) {
	dir := t.TempDir()

	mgr, err := NewStateManager(dir)
	if err != nil {
		t.Fatalf("NewStateManager failed: %v", err)
	}

	var saveCount atomic.Int32
	provider := &mockProvider{
		nodeID:       "node-batch",
		currentEpoch: 1,
	}

	wrappedProvider := &countingProvider{mockProvider: provider, saveCount: &saveCount}
	mgr.SetProvider(wrappedProvider)

	for i := 0; i < 10; i++ {
		mgr.MarkDirty()
	}

	time.Sleep(200 * time.Millisecond)

	mgr.Close()

	if _, err := os.Stat(filepath.Join(dir, stateFileName)); os.IsNotExist(err) {
		t.Fatal("state file not created despite MarkDirty calls")
	}
}

type countingProvider struct {
	*mockProvider
	saveCount *atomic.Int32
}

func (c *countingProvider) GetNodeID() string {
	c.saveCount.Add(1)
	return c.mockProvider.GetNodeID()
}

func TestStateManager_LoadNoFile(t *testing.T) {
	dir := t.TempDir()

	mgr, err := NewStateManager(dir)
	if err != nil {
		t.Fatalf("NewStateManager failed: %v", err)
	}
	defer mgr.Close()

	provider := &mockProvider{nodeID: "initial-id"}
	mgr.SetProvider(provider)

	if err := mgr.Load(); err != nil {
		t.Fatalf("Load should succeed with no file: %v", err)
	}

	if provider.nodeID != "initial-id" {
		t.Errorf("nodeID should remain unchanged: got %q", provider.nodeID)
	}
}

func TestStateManager_CorruptedFile(t *testing.T) {
	dir := t.TempDir()

	statePath := filepath.Join(dir, stateFileName)
	if err := os.WriteFile(statePath, []byte("invalid json{"), 0644); err != nil {
		t.Fatalf("write corrupted file failed: %v", err)
	}

	mgr, err := NewStateManager(dir)
	if err != nil {
		t.Fatalf("NewStateManager failed: %v", err)
	}
	defer mgr.Close()

	provider := &mockProvider{}
	mgr.SetProvider(provider)

	err = mgr.Load()
	if err == nil {
		t.Fatal("Load should fail with corrupted file")
	}
}

func TestStateManager_CloseWithDirtyState(t *testing.T) {
	dir := t.TempDir()

	mgr, err := NewStateManager(dir)
	if err != nil {
		t.Fatalf("NewStateManager failed: %v", err)
	}

	provider := &mockProvider{
		nodeID:       "node-dirty",
		currentEpoch: 99,
	}
	mgr.SetProvider(provider)

	mgr.MarkDirty()

	if err := mgr.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, stateFileName)); os.IsNotExist(err) {
		t.Fatal("state file should be saved on close when dirty")
	}

	data, _ := os.ReadFile(filepath.Join(dir, stateFileName))
	var state PersistentState
	json.Unmarshal(data, &state)
	if state.CurrentEpoch != 99 {
		t.Errorf("epoch mismatch: got %d, want 99", state.CurrentEpoch)
	}
}

func TestStateManager_UnsupportedVersion(t *testing.T) {
	dir := t.TempDir()

	state := PersistentState{
		Version: 999,
		NodeID:  "old-node",
	}
	data, _ := json.Marshal(state)
	os.WriteFile(filepath.Join(dir, stateFileName), data, 0644)

	mgr, err := NewStateManager(dir)
	if err != nil {
		t.Fatalf("NewStateManager failed: %v", err)
	}
	defer mgr.Close()

	provider := &mockProvider{}
	mgr.SetProvider(provider)

	err = mgr.Load()
	if err == nil {
		t.Fatal("Load should fail with unsupported version")
	}
}
