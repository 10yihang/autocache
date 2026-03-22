package replication

import (
	"fmt"
	"sync"
)

type ApplyProgress struct {
	Epoch uint64
	LSN   uint64
}

type ReplicaApplier struct {
	mu       sync.Mutex
	progress map[uint16]ApplyProgress
}

func NewReplicaApplier() *ReplicaApplier {
	return &ReplicaApplier{progress: make(map[uint16]ApplyProgress)}
}

func (a *ReplicaApplier) Begin(op Op) (skip bool, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	state := a.progress[op.Slot]
	if op.Epoch < state.Epoch {
		return false, fmt.Errorf("stale replication epoch: got %d want >= %d", op.Epoch, state.Epoch)
	}
	if op.Epoch > state.Epoch {
		state = ApplyProgress{Epoch: op.Epoch, LSN: 0}
	}
	if op.LSN <= state.LSN {
		return true, nil
	}
	if op.LSN != state.LSN+1 {
		return false, fmt.Errorf("replication gap for slot %d: got %d want %d", op.Slot, op.LSN, state.LSN+1)
	}
	return false, nil
}

func (a *ReplicaApplier) Commit(op Op) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.progress[op.Slot] = ApplyProgress{Epoch: op.Epoch, LSN: op.LSN}
}
