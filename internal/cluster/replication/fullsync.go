package replication

import (
	"context"
	"fmt"
	"time"

	"github.com/10yihang/autocache/internal/cluster/hash"
	"github.com/10yihang/autocache/internal/engine"
	"github.com/10yihang/autocache/internal/engine/memory"
)

type FullSyncStore interface {
	KeysInSlot(slot uint16, count int) []string
	CountKeysInSlot(slot uint16) int
	Keys(ctx context.Context, pattern string) ([]string, error)
	GetEntry(ctx context.Context, key string) (*engine.Entry, error)
	RestoreEntry(ctx context.Context, key string, valueType engine.ValueType, payload []byte, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) (int64, error)
}

func (m *Manager) NeedsFullSync(slot uint16, appliedLSN uint64) bool {
	if m == nil || m.logs == nil {
		return true
	}
	return !m.logs.CanReplayFromLSN(slot, appliedLSN)
}

func (m *Manager) SnapshotLSN(slot uint16) (uint64, bool) {
	if m == nil || m.logs == nil {
		return 0, false
	}
	_, last, ok := m.logs.Range(slot)
	if !ok {
		return 0, true
	}
	return last, true
}

func (m *Manager) PostSnapshotOps(slot uint16, snapshotLSN uint64) []Op {
	if m == nil || m.logs == nil {
		return nil
	}
	return m.logs.ListAfter(slot, snapshotLSN)
}

func (m *Manager) FullSyncSlot(ctx context.Context, slot uint16, snapshotLSN uint64, source FullSyncStore, target FullSyncStore, applyOp func(op Op) error) error {
	if m == nil || source == nil || target == nil {
		return fmt.Errorf("full sync requires manager/source/target")
	}

	existing, err := target.Keys(ctx, "*")
	if err != nil {
		return fmt.Errorf("list target keys: %w", err)
	}
	filtered := make([]string, 0, len(existing))
	for _, key := range existing {
		if hash.KeySlot(key) == slot {
			filtered = append(filtered, key)
		}
	}
	existing = filtered
	if len(existing) > 0 {
		if _, err := target.Del(ctx, existing...); err != nil {
			return fmt.Errorf("clear target slot: %w", err)
		}
	}

	sourceCount := source.CountKeysInSlot(slot)
	if sourceCount < 1024 {
		sourceCount = 1024
	}
	keys := source.KeysInSlot(slot, sourceCount)
	for _, key := range keys {
		entry, err := source.GetEntry(ctx, key)
		if err != nil || entry == nil {
			continue
		}
		serialized, err := memory.SerializeEntryValue(entry)
		if err != nil {
			return fmt.Errorf("serialize entry %s: %w", key, err)
		}
		valueType := engine.ValueType(serialized[0])
		payload := serialized[1:]
		var ttl time.Duration
		if !entry.ExpireAt.IsZero() {
			ttl = time.Until(entry.ExpireAt)
			if ttl < 0 {
				ttl = 0
			}
		}
		if err := target.RestoreEntry(ctx, key, valueType, payload, ttl); err != nil {
			return fmt.Errorf("restore entry %s: %w", key, err)
		}
	}

	if applyOp == nil {
		return nil
	}
	for _, op := range m.PostSnapshotOps(slot, snapshotLSN) {
		if err := applyOp(op); err != nil {
			return fmt.Errorf("replay post-snapshot op lsn=%d: %w", op.LSN, err)
		}
	}
	return nil
}
