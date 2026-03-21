package failover

import (
	"fmt"
	"log"

	"github.com/10yihang/autocache/internal/cluster"
)

type OwnershipBroadcaster interface {
	BroadcastSlotOwnershipChange(slot uint16, newPrimary string) error
}

type Manager struct {
	slots      *cluster.SlotManager
	broadcast  func(slot uint16, newPrimary string)
	failClosed func(slot uint16, stalePrimary string)
}

func NewManager(slots *cluster.SlotManager) *Manager {
	return &Manager{slots: slots}
}

func NewManagerForCluster(c *cluster.Cluster) *Manager {
	if c == nil {
		return &Manager{}
	}
	mgr := NewManager(c.GetSlotManager())
	mgr.BindOwnershipBroadcaster(c)
	return mgr
}

func (m *Manager) SetBroadcastHook(hook func(slot uint16, newPrimary string)) {
	m.broadcast = hook
}

func (m *Manager) SetFailClosedHook(hook func(slot uint16, stalePrimary string)) {
	m.failClosed = hook
}

func (m *Manager) BindOwnershipBroadcaster(b OwnershipBroadcaster) {
	if b == nil {
		m.broadcast = nil
		return
	}
	m.broadcast = func(slot uint16, newPrimary string) {
		if err := b.BroadcastSlotOwnershipChange(slot, newPrimary); err != nil {
			log.Printf("failover broadcast slot ownership slot=%d primary=%s failed: %v", slot, newPrimary, err)
		}
	}
}

func (m *Manager) SelectBestReplica(slot uint16) (string, error) {
	if m == nil || m.slots == nil {
		return "", fmt.Errorf("failover manager not configured")
	}
	info := m.slots.GetSlotInfo(slot)
	if info == nil {
		return "", fmt.Errorf("slot %d not found", slot)
	}
	bestNode := ""
	bestLSN := uint64(0)
	for _, replica := range info.Replicas {
		if !replica.Healthy {
			continue
		}
		if bestNode == "" || replica.MatchLSN > bestLSN {
			bestNode = replica.NodeID
			bestLSN = replica.MatchLSN
		}
	}
	if bestNode == "" {
		return "", fmt.Errorf("no healthy replica for slot %d", slot)
	}
	return bestNode, nil
}

func (m *Manager) PromoteBestReplica(slot uint16) (string, error) {
	best, err := m.SelectBestReplica(slot)
	if err != nil {
		return "", err
	}
	info := m.slots.GetSlotInfo(slot)
	stalePrimary := ""
	if info != nil {
		stalePrimary = info.PrimaryID
		if stalePrimary == "" {
			stalePrimary = info.NodeID
		}
	}
	if err := m.slots.PromoteReplica(slot, best); err != nil {
		return "", err
	}
	if m.broadcast != nil {
		m.broadcast(slot, best)
	}
	if stalePrimary != "" && stalePrimary != best && m.failClosed != nil {
		m.failClosed(slot, stalePrimary)
	}
	return best, nil
}

func (m *Manager) CanNodeServeWrites(slot uint16, nodeID string) bool {
	if m == nil || m.slots == nil {
		return false
	}
	info := m.slots.GetSlotInfo(slot)
	if info == nil {
		return false
	}
	primary := info.PrimaryID
	if primary == "" {
		primary = info.NodeID
	}
	return primary != "" && primary == nodeID
}
