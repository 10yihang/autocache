package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const (
	stateFileName        = "cluster-state.json"
	saveDebounceDuration = 100 * time.Millisecond
)

type ClusterStateProvider interface {
	GetNodeID() string
	GetNodeInfos() []NodeInfo
	GetSlotMap() [16384]string
	GetMigratingSlots() map[uint16]MigrationState
	GetCurrentEpoch() uint64
	GetMyEpoch() uint64
	RestoreState(state *PersistentState) error
}

type StateManager struct {
	dataDir  string
	provider ClusterStateProvider

	dirty atomic.Bool
	mu    sync.Mutex

	saveCh chan struct{}
	doneCh chan struct{}
	wg     sync.WaitGroup
}

func NewStateManager(dataDir string) (*StateManager, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	m := &StateManager{
		dataDir: dataDir,
		saveCh:  make(chan struct{}, 1),
		doneCh:  make(chan struct{}),
	}

	m.wg.Add(1)
	go m.saveLoop()

	return m, nil
}

func (m *StateManager) SetProvider(provider ClusterStateProvider) {
	m.provider = provider
}

func (m *StateManager) saveLoop() {
	defer m.wg.Done()

	var timer *time.Timer
	var timerC <-chan time.Time

	for {
		select {
		case <-m.saveCh:
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(saveDebounceDuration)
			timerC = timer.C

		case <-timerC:
			timerC = nil
			timer = nil
			if m.dirty.Load() && m.provider != nil {
				if err := m.save(); err != nil {
					fmt.Fprintf(os.Stderr, "state save error: %v\n", err)
				}
			}

		case <-m.doneCh:
			if timer != nil {
				timer.Stop()
			}
			return
		}
	}
}

func (m *StateManager) MarkDirty() {
	if m.dirty.CompareAndSwap(false, true) {
		select {
		case m.saveCh <- struct{}{}:
		default:
		}
	}
}

func (m *StateManager) Load() error {
	if m.provider == nil {
		return fmt.Errorf("provider not set")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	path := filepath.Join(m.dataDir, stateFileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read state file: %w", err)
	}

	var state PersistentState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("unmarshal state: %w", err)
	}

	if state.Version != CurrentStateVersion {
		return fmt.Errorf("unsupported state version: %d", state.Version)
	}

	return m.provider.RestoreState(&state)
}

func (m *StateManager) save() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state := PersistentState{
		Version:        CurrentStateVersion,
		NodeID:         m.provider.GetNodeID(),
		Nodes:          m.provider.GetNodeInfos(),
		SlotMap:        m.provider.GetSlotMap(),
		MigratingSlots: m.provider.GetMigratingSlots(),
		CurrentEpoch:   m.provider.GetCurrentEpoch(),
		MyEpoch:        m.provider.GetMyEpoch(),
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	path := filepath.Join(m.dataDir, stateFileName)
	tempPath := path + ".tmp"

	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	f, err := os.OpenFile(tempPath, os.O_RDONLY, 0)
	if err == nil {
		_ = f.Sync()
		_ = f.Close()
	}

	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("rename state file: %w", err)
	}

	m.dirty.Store(false)
	return nil
}

func (m *StateManager) Save() error {
	if m.provider == nil {
		return fmt.Errorf("provider not set")
	}
	return m.save()
}

func (m *StateManager) Close() error {
	close(m.doneCh)
	m.wg.Wait()

	if m.dirty.Load() && m.provider != nil {
		return m.save()
	}
	return nil
}

func (m *StateManager) FilePath() string {
	return filepath.Join(m.dataDir, stateFileName)
}
