package tiered

import (
	"context"
	"errors"
	"time"

	"github.com/10yihang/autocache/internal/engine"
	metrics2 "github.com/10yihang/autocache/internal/metrics"
	pkgerrors "github.com/10yihang/autocache/pkg/errors"
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
	metrics2.SetTieredMigrationPending(m.pendingMigrations())

	// Hot -> Warm
	hotKeys := m.manager.stats.GetKeysByTier(TierHot)
	demoted := 0
	for _, key := range hotKeys {
		stats := m.manager.stats.GetStats(key)
		if stats == nil || !m.manager.policy.ShouldDemote(key, stats, TierHot) {
			continue
		}
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
			if stats == nil || !m.manager.policy.ShouldDemote(key, stats, TierWarm) {
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

func (m *Migrator) pendingMigrations() int {
	pending := 0
	for _, key := range m.manager.stats.GetKeysByTier(TierHot) {
		stats := m.manager.stats.GetStats(key)
		if stats != nil && m.manager.policy.ShouldDemote(key, stats, TierHot) {
			pending++
		}
	}
	if m.manager.coldTier != nil {
		for _, key := range m.manager.stats.GetKeysByTier(TierWarm) {
			stats := m.manager.stats.GetStats(key)
			if stats != nil && m.manager.policy.ShouldDemote(key, stats, TierWarm) {
				pending++
			}
		}
	}
	return pending
}

func (m *Migrator) demoteKey(ctx context.Context, key string, from, to TierType) error {
	direction := "down"
	start := time.Now()
	metrics2.AddTieredMigrationInFlight(1)
	defer metrics2.AddTieredMigrationInFlight(-1)
	defer func() {
		if r := recover(); r != nil {
			metrics2.RecordTieredMigrationError(direction, "panic")
			metrics2.ObserveTieredMigrationDuration(direction, time.Since(start), false)
			panic(r)
		}
	}()

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
		metrics2.RecordTieredMigrationError(direction, classifyMigrationError(err))
		metrics2.ObserveTieredMigrationDuration(direction, time.Since(start), false)
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
		metrics2.RecordTieredMigrationError(direction, classifyMigrationError(err))
		metrics2.ObserveTieredMigrationDuration(direction, time.Since(start), false)
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
	m.manager.recordPolicyMove(key, from, to)
	m.manager.updateTierMetrics()
	metrics2.ObserveTieredMigrationDuration(direction, time.Since(start), true)
	return nil
}

func classifyMigrationError(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, engine.ErrKeyNotFound) || errors.Is(err, pkgerrors.ErrKeyNotFound) {
		return "not_found"
	}
	return "storage_error"
}
