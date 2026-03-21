package replication

import (
	"fmt"
	"sort"
	"sync"
)

type Op struct {
	Slot       uint16
	Epoch      uint64
	LSN        uint64
	OpType     string
	Key        string
	Payload    []byte
	ExpireAtNs int64
}

type ReplicaAck struct {
	Slot       uint16
	Epoch      uint64
	NodeID     string
	AppliedLSN uint64
}

type LogStore struct {
	limit int
	mu    sync.RWMutex
	logs  map[uint16][]Op
	first map[uint16]uint64
	last  map[uint16]uint64
}

func NewLogStore(limit int) *LogStore {
	if limit <= 0 {
		limit = 1024
	}
	return &LogStore{
		limit: limit,
		logs:  make(map[uint16][]Op),
		first: make(map[uint16]uint64),
		last:  make(map[uint16]uint64),
	}
}

func (s *LogStore) Append(op Op) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	lastLSN := s.last[op.Slot]
	if op.LSN != lastLSN+1 {
		return fmt.Errorf("slot %d append out of order: got %d want %d", op.Slot, op.LSN, lastLSN+1)
	}
	s.logs[op.Slot] = append(s.logs[op.Slot], op)
	if _, ok := s.first[op.Slot]; !ok {
		s.first[op.Slot] = op.LSN
	}
	s.last[op.Slot] = op.LSN
	if len(s.logs[op.Slot]) > s.limit {
		s.logs[op.Slot] = append([]Op(nil), s.logs[op.Slot][len(s.logs[op.Slot])-s.limit:]...)
	}
	if len(s.logs[op.Slot]) > 0 {
		s.first[op.Slot] = s.logs[op.Slot][0].LSN
	}
	return nil
}

func (s *LogStore) ListAfter(slot uint16, lsn uint64) []Op {
	s.mu.RLock()
	defer s.mu.RUnlock()

	log := s.logs[slot]
	result := make([]Op, 0, len(log))
	for _, op := range log {
		if op.LSN > lsn {
			result = append(result, op)
		}
	}
	return result
}

func (s *LogStore) Range(slot uint16) (first uint64, last uint64, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	last, ok = s.last[slot]
	if !ok {
		return 0, 0, false
	}
	return s.first[slot], last, true
}

func (s *LogStore) CanReplayFromLSN(slot uint16, appliedLSN uint64) bool {
	first, last, ok := s.Range(slot)
	if !ok {
		return appliedLSN == 0
	}
	if appliedLSN > last {
		return false
	}
	if appliedLSN == last {
		return true
	}
	return appliedLSN+1 >= first
}

type AckTracker struct {
	mu   sync.RWMutex
	acks map[uint16]map[string]ReplicaAck
}

func NewAckTracker() *AckTracker {
	return &AckTracker{acks: make(map[uint16]map[string]ReplicaAck)}
}

func (t *AckTracker) Track(ack ReplicaAck) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.acks[ack.Slot] == nil {
		t.acks[ack.Slot] = make(map[string]ReplicaAck)
	}
	current, ok := t.acks[ack.Slot][ack.NodeID]
	if ok && current.AppliedLSN >= ack.AppliedLSN {
		return
	}
	t.acks[ack.Slot][ack.NodeID] = ack
}

func (t *AckTracker) Get(slot uint16, nodeID string) uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.acks[slot][nodeID].AppliedLSN
}

func (t *AckTracker) Snapshot(slot uint16) []ReplicaAck {
	t.mu.RLock()
	defer t.mu.RUnlock()

	items := make([]ReplicaAck, 0, len(t.acks[slot]))
	for _, ack := range t.acks[slot] {
		items = append(items, ack)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].AppliedLSN == items[j].AppliedLSN {
			return items[i].NodeID < items[j].NodeID
		}
		return items[i].AppliedLSN > items[j].AppliedLSN
	})
	return items
}
