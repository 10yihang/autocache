package tiered

import (
	"context"
	"time"

	"github.com/10yihang/autocache/internal/engine"
	metrics2 "github.com/10yihang/autocache/internal/metrics"
)

// Migrator handles data migration between tiers
type Migrator struct {
	manager *Manager
}

// NewMigrator creates a new migrator
func NewMigrator(manager *Manager) *Migrator {
	return &Migrator{manager: manager}
}

// MigrationLoop runs the migration loop
func (m *Migrator) MigrationLoop() {
	defer m.manager.wg.Done()

	ticker := time.NewTicker(m.manager.config.MigrationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.manager.ctx.Done():
			return
		case <-ticker.C:
			m.RunMigration()
		}
	}
}

// RunMigration executes a single migration run
func (m *Migrator) RunMigration() {
	ctx := context.Background()

	// Hot -> Warm
	coldKeys := m.manager.stats.GetColdKeys(m.manager.config.HotIdleThreshold, m.manager.config.HotAccessThreshold)
	demoted := 0
	for _, key := range coldKeys {
		if demoted >= m.manager.config.MigrationBatchSize {
			break
		}

		if err := m.demoteKey(ctx, key, TierHot, TierWarm); err != nil {
			continue
		}
		demoted++
	}

	// Warm -> Cold
	if m.manager.coldTier != nil {
		warmKeys := m.manager.stats.GetKeysByTier(TierWarm)
		demoted = 0
		for _, key := range warmKeys {
			stats := m.manager.stats.GetStats(key)
			if stats == nil || !m.manager.policy.ShouldDemote(stats, TierWarm) {
				continue
			}
			if demoted >= m.manager.config.MigrationBatchSize {
				break
			}

			if err := m.demoteKey(ctx, key, TierWarm, TierCold); err != nil {
				continue
			}
			demoted++
		}
	}
}

func (m *Migrator) demoteKey(ctx context.Context, key string, from, to TierType) error {
	var value interface{}
	var err error
	var ttl time.Duration
	var entry *engine.Entry

	// Get from source
	switch from {
	case TierHot:
		entry, err = m.manager.hotTier.GetEntry(ctx, key)
		if err == nil {
			value = entry.Value
			ttl = entryTTL(entry)
		}
	case TierWarm:
		if m.manager.warmTier != nil {
			entry, err = m.manager.warmTier.Get(ctx, key)
			if err == nil {
				value = entry.Value
				ttl = entryTTL(entry)
			}
		}
	}

	if err != nil {
		return err
	}

	if ttl < 0 {
		ttl = 0
	}

	// Write to dest
	switch to {
	case TierWarm:
		if m.manager.warmTier != nil {
			err = m.manager.warmTier.Set(ctx, key, value, ttl)
		}
	case TierCold:
		if m.manager.coldTier != nil {
			err = m.manager.coldTier.Set(ctx, key, value, ttl)
		}
	}

	if err != nil {
		return err
	}

	// Delete from source
	switch from {
	case TierHot:
		m.manager.hotTier.Del(ctx, key)
	case TierWarm:
		if m.manager.warmTier != nil {
			m.manager.warmTier.Del(ctx, key)
		}
	}

	if from == TierHot {
		m.manager.migrationsDown.Add(1)
		metrics2.RecordTieredMigration("down")
	}
	m.manager.stats.UpdateTier(key, to)
	return nil
}
