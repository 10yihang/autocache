package replication

import (
	"context"
	"fmt"
	"sync"
)

type SlotLSNAllocator func(slot uint16) (epoch uint64, lsn uint64, err error)

type WriteRequest struct {
	Slot       uint16
	OpType     string
	Key        string
	Payload    []byte
	ExpireAtNs int64
	Apply      func() error
}

type Manager struct {
	logs     *LogStore
	allocate SlotLSNAllocator
	stream   *Stream
	acks     *AckTracker
	registry *MasterConnRegistry // persistent replica connections (Redis-style)

	mu               sync.RWMutex
	resolve          func(slot uint16) []PeerTarget
	inbound          map[uint16][]Op
	updateReplicaLSN func(slot uint16, nodeID string, lsn uint64) error
}

func NewManager(logs *LogStore, allocate SlotLSNAllocator) *Manager {
	if logs == nil {
		logs = NewLogStore(1024)
	}
	mgr := &Manager{
		logs:     logs,
		allocate: allocate,
		stream:   NewStream(1024),
		acks:     NewAckTracker(),
		registry: NewMasterConnRegistry(),
		inbound:  make(map[uint16][]Op),
	}
	mgr.initCallbacks()
	return mgr
}

// ConnRegistry returns the master connection registry for replica registration.
func (m *Manager) ConnRegistry() *MasterConnRegistry {
	if m == nil {
		return nil
	}
	return m.registry
}

func (m *Manager) initCallbacks() {
	if m == nil || m.stream == nil {
		return
	}
	m.stream.SetOnSent(func(nodeID string, op Op) {
		ack := ReplicaAck{Slot: op.Slot, Epoch: op.Epoch, NodeID: nodeID, AppliedLSN: op.LSN}
		m.acks.Track(ack)
		m.mu.RLock()
		update := m.updateReplicaLSN
		m.mu.RUnlock()
		if update != nil {
			_ = update(op.Slot, nodeID, op.LSN)
		}
	})
}

func (m *Manager) ApplyWrite(_ context.Context, req WriteRequest) (Op, error) {
	if m == nil {
		return Op{}, fmt.Errorf("replication manager is nil")
	}
	if req.Apply == nil {
		return Op{}, fmt.Errorf("apply callback is required")
	}
	if err := req.Apply(); err != nil {
		return Op{}, err
	}
	if m.allocate == nil {
		return Op{}, fmt.Errorf("lsn allocator is not configured")
	}

	epoch, lsn, err := m.allocate(req.Slot)
	if err != nil {
		return Op{}, fmt.Errorf("allocate lsn for slot %d: %w", req.Slot, err)
	}

	op := Op{
		Slot:       req.Slot,
		Epoch:      epoch,
		LSN:        lsn,
		OpType:     req.OpType,
		Key:        req.Key,
		Payload:    append([]byte(nil), req.Payload...),
		ExpireAtNs: req.ExpireAtNs,
	}
	if err := m.logs.Append(op); err != nil {
		return Op{}, fmt.Errorf("append replication op: %w", err)
	}
	m.dispatch(op)
	return op, nil
}

func (m *Manager) SetReplicaResolver(resolve func(slot uint16) []PeerTarget) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resolve = resolve
}

func (m *Manager) SetReplicaLSNUpdater(update func(slot uint16, nodeID string, lsn uint64) error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateReplicaLSN = update
}

func (m *Manager) SetStreamSender(send peerSender) {
	if m == nil || m.stream == nil {
		return
	}
	m.stream.SetSender(send)
}

func (m *Manager) Close() {
	if m == nil || m.stream == nil {
		return
	}
	m.stream.Close()
}

func (m *Manager) ReceiveTransportOp(op Op) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inbound[op.Slot] = append(m.inbound[op.Slot], op)
}

func (m *Manager) InboundAfter(slot uint16, lsn uint64) []Op {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	ops := m.inbound[slot]
	out := make([]Op, 0, len(ops))
	for _, op := range ops {
		if op.LSN > lsn {
			out = append(out, op)
		}
	}
	return out
}

func (m *Manager) dispatch(op Op) {
	// Push through persistent replica connections first (Redis-style)
	sent := m.registry.Broadcast(op)

	// Fallback: resolve via slot replicas for peers not yet connected
	m.mu.RLock()
	resolve := m.resolve
	m.mu.RUnlock()
	if resolve != nil && m.stream != nil {
		for _, target := range resolve(op.Slot) {
			if sent > 0 {
				_ = sent
			}
			m.stream.Enqueue(target, op)
		}
	}
}

func (m *Manager) LogStore() *LogStore {
	if m == nil {
		return nil
	}
	return m.logs
}

func (m *Manager) AckTracker() *AckTracker {
	if m == nil {
		return nil
	}
	return m.acks
}
