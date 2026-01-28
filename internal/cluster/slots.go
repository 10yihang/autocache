package cluster

import (
	"fmt"
	"sync"

	"github.com/10yihang/autocache/internal/cluster/hash"
	"github.com/10yihang/autocache/internal/cluster/state"
)

type SlotState int

const (
	SlotStateNormal SlotState = iota
	SlotStateImporting
	SlotStateExporting
)

type SlotInfo struct {
	State     SlotState
	NodeID    string
	Importing string
	Exporting string
}

type SlotManager struct {
	slots        [hash.SlotCount]*SlotInfo
	nodeSlots    map[string][]uint16
	stateManager *state.StateManager
	mu           sync.RWMutex
}

func NewSlotManager() *SlotManager {
	sm := &SlotManager{
		nodeSlots: make(map[string][]uint16),
	}
	for i := 0; i < hash.SlotCount; i++ {
		sm.slots[i] = &SlotInfo{State: SlotStateNormal}
	}
	return sm
}

func (sm *SlotManager) SetStateManager(mgr *state.StateManager) {
	sm.stateManager = mgr
}

func (sm *SlotManager) markDirty() {
	if sm.stateManager != nil {
		sm.stateManager.MarkDirty()
	}
}

func (sm *SlotManager) AssignSlot(slot uint16, nodeID string) error {
	if slot >= hash.SlotCount {
		return fmt.Errorf("invalid slot: %d", slot)
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	oldNodeID := sm.slots[slot].NodeID
	if oldNodeID != "" {
		sm.removeSlotFromNode(oldNodeID, slot)
	}

	sm.slots[slot].NodeID = nodeID
	sm.slots[slot].State = SlotStateNormal
	sm.nodeSlots[nodeID] = append(sm.nodeSlots[nodeID], slot)
	sm.markDirty()
	return nil
}

func (sm *SlotManager) AssignSlotRange(start, end uint16, nodeID string) error {
	for slot := start; slot <= end; slot++ {
		if err := sm.AssignSlot(slot, nodeID); err != nil {
			return err
		}
	}
	return nil
}

func (sm *SlotManager) GetSlotNode(slot uint16) string {
	if slot >= hash.SlotCount {
		return ""
	}
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.slots[slot].NodeID
}

func (sm *SlotManager) GetKeyNode(key string) string {
	slot := hash.KeySlot(key)
	return sm.GetSlotNode(slot)
}

func (sm *SlotManager) GetSlotInfo(slot uint16) *SlotInfo {
	if slot >= hash.SlotCount {
		return nil
	}
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	info := *sm.slots[slot]
	return &info
}

func (sm *SlotManager) GetNodeSlots(nodeID string) []uint16 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	slots := sm.nodeSlots[nodeID]
	result := make([]uint16, len(slots))
	copy(result, slots)
	return result
}

func (sm *SlotManager) SetImporting(slot uint16, fromNodeID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.slots[slot].State = SlotStateImporting
	sm.slots[slot].Importing = fromNodeID
	sm.markDirty()
}

func (sm *SlotManager) SetExporting(slot uint16, toNodeID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.slots[slot].State = SlotStateExporting
	sm.slots[slot].Exporting = toNodeID
	sm.markDirty()
}

func (sm *SlotManager) SetStable(slot uint16) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.slots[slot].State = SlotStateNormal
	sm.slots[slot].Importing = ""
	sm.slots[slot].Exporting = ""
	sm.markDirty()
}

func (sm *SlotManager) FinishMigration(slot uint16, newNodeID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	oldNodeID := sm.slots[slot].NodeID
	if oldNodeID != "" {
		sm.removeSlotFromNode(oldNodeID, slot)
	}

	sm.slots[slot].NodeID = newNodeID
	sm.slots[slot].State = SlotStateNormal
	sm.slots[slot].Importing = ""
	sm.slots[slot].Exporting = ""
	sm.nodeSlots[newNodeID] = append(sm.nodeSlots[newNodeID], slot)
	sm.markDirty()
}

func (sm *SlotManager) removeSlotFromNode(nodeID string, slot uint16) {
	slots := sm.nodeSlots[nodeID]
	for i, s := range slots {
		if s == slot {
			sm.nodeSlots[nodeID] = append(slots[:i], slots[i+1:]...)
			break
		}
	}
}

func (sm *SlotManager) GetClusterSlots() []SlotRange {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var ranges []SlotRange
	var current *SlotRange

	for i := uint16(0); i < hash.SlotCount; i++ {
		nodeID := sm.slots[i].NodeID
		if nodeID == "" {
			if current != nil {
				ranges = append(ranges, *current)
				current = nil
			}
			continue
		}

		if current == nil || current.NodeID != nodeID {
			if current != nil {
				ranges = append(ranges, *current)
			}
			current = &SlotRange{Start: i, End: i, NodeID: nodeID}
		} else {
			current.End = i
		}
	}

	if current != nil {
		ranges = append(ranges, *current)
	}
	return ranges
}

type SlotRange struct {
	Start  uint16
	End    uint16
	NodeID string
}

func (sm *SlotManager) CountAssigned() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	count := 0
	for _, slot := range sm.slots {
		if slot.NodeID != "" {
			count++
		}
	}
	return count
}

func (sm *SlotManager) GetSlotMapSnapshot() [hash.SlotCount]string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	var slotMap [hash.SlotCount]string
	for i, slot := range sm.slots {
		slotMap[i] = slot.NodeID
	}
	return slotMap
}

func (sm *SlotManager) GetMigratingSlots() map[uint16]state.MigrationState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	result := make(map[uint16]state.MigrationState)
	for i, slot := range sm.slots {
		if slot.State == SlotStateImporting {
			result[uint16(i)] = state.MigrationState{
				SourceNodeID: slot.Importing,
				TargetNodeID: slot.NodeID,
				State:        "importing",
			}
		} else if slot.State == SlotStateExporting {
			result[uint16(i)] = state.MigrationState{
				SourceNodeID: slot.NodeID,
				TargetNodeID: slot.Exporting,
				State:        "exporting",
			}
		}
	}
	return result
}

func (sm *SlotManager) RestoreFromState(slotMap [hash.SlotCount]string, migrations map[uint16]state.MigrationState) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.nodeSlots = make(map[string][]uint16)
	for i := 0; i < hash.SlotCount; i++ {
		sm.slots[i] = &SlotInfo{State: SlotStateNormal}
	}

	for i, nodeID := range slotMap {
		if nodeID != "" {
			sm.slots[i].NodeID = nodeID
			sm.nodeSlots[nodeID] = append(sm.nodeSlots[nodeID], uint16(i))
		}
	}

	for slot, migration := range migrations {
		switch migration.State {
		case "importing":
			sm.slots[slot].State = SlotStateImporting
			sm.slots[slot].Importing = migration.SourceNodeID
		case "exporting":
			sm.slots[slot].State = SlotStateExporting
			sm.slots[slot].Exporting = migration.TargetNodeID
		}
	}
}
