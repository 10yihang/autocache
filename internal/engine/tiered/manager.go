package tiered

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/10yihang/autocache/internal/engine"
	"github.com/10yihang/autocache/internal/engine/badger"
	"github.com/10yihang/autocache/internal/engine/memory"
	"github.com/10yihang/autocache/internal/engine/s3"
)

// Config tiered storage config
type Config struct {
	// Enable tiering
	Enabled bool

	// Hot tier capacity (bytes)
	HotTierCapacity int64

	// Warm tier config
	WarmTierEnabled  bool
	WarmTierPath     string
	WarmTierCapacity int64

	// Cold tier config
	ColdTierEnabled  bool
	ColdTierEndpoint string
	ColdTierBucket   string
	ColdTierCapacity int64

	// Migration config
	MigrationInterval  time.Duration
	MigrationBatchSize int
	MigrationRateLimit int64

	// Policy config
	HotAccessThreshold uint64
	HotIdleThreshold   time.Duration
	ColdIdleThreshold  time.Duration
}

// DefaultConfig creates default config
func DefaultConfig() *Config {
	return &Config{
		Enabled:            true,
		HotTierCapacity:    512 * 1024 * 1024, // 512MB
		WarmTierEnabled:    true,
		WarmTierPath:       "/data/warm",
		WarmTierCapacity:   2 * 1024 * 1024 * 1024, // 2GB
		ColdTierEnabled:    false,
		MigrationInterval:  time.Minute,
		MigrationBatchSize: 100,
		MigrationRateLimit: 10 * 1024 * 1024, // 10MB/s
		HotAccessThreshold: 10,
		HotIdleThreshold:   5 * time.Minute,
		ColdIdleThreshold:  2 * time.Hour,
	}
}

// Manager orchestrates tiered storage
type Manager struct {
	config *Config

	// Tiers
	hotTier  *memory.Store
	warmTier engine.Engine
	coldTier engine.Engine

	// Stats collector
	stats *StatsCollector

	// Tier policy
	policy TierPolicy

	// Migrator
	migrator *Migrator

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu sync.RWMutex
}

// NewManager creates a tiered manager
func NewManager(cfg *Config, hotTier *memory.Store) (*Manager, error) {
	ctx, cancel := context.WithCancel(context.Background())

	m := &Manager{
		config:  cfg,
		hotTier: hotTier,
		stats:   NewStatsCollector(1.0),
		policy:  NewDefaultPolicy(),
		ctx:     ctx,
		cancel:  cancel,
	}

	if cfg.WarmTierEnabled {
		warmTier, err := m.initWarmTier(cfg.WarmTierPath)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("init warm tier: %w", err)
		}
		m.warmTier = warmTier
	}

	if cfg.ColdTierEnabled {
		coldTier, err := m.initColdTier(cfg.ColdTierEndpoint, cfg.ColdTierBucket)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("init cold tier: %w", err)
		}
		m.coldTier = coldTier
	}

	m.migrator = NewMigrator(m)

	return m, nil
}

func (m *Manager) initWarmTier(path string) (engine.Engine, error) {
	return badger.NewStore(path)
}

func (m *Manager) initColdTier(endpoint, bucket string) (engine.Engine, error) {
	return s3.NewStore(endpoint, bucket)
}

// Start starts background tasks
func (m *Manager) Start() {
	m.wg.Add(1)
	go m.migrator.MigrationLoop()
	log.Println("Tiered storage manager started")
}

// Stop stops background tasks
func (m *Manager) Stop() {
	m.cancel()
	m.wg.Wait()
	m.stats.Stop()

	if m.warmTier != nil {
		m.warmTier.Close()
	}
	if m.coldTier != nil {
		m.coldTier.Close()
	}
	log.Println("Tiered storage manager stopped")
}

// Get gets value from any tier
func (m *Manager) Get(ctx context.Context, key string) (string, error) {
	// 1. Hot Tier
	val, err := m.hotTier.Get(ctx, key)
	if err == nil {
		m.stats.RecordAccess(key, int64(len(val)))
		return val, nil
	}

	// 2. Warm Tier
	if m.warmTier != nil {
		entry, err := m.warmTier.Get(ctx, key)
		if err == nil {
			val := entry.Value.(string) // Assuming string value for now
			m.stats.RecordAccess(key, int64(len(val)))

			// Consider promotion
			m.maybePromote(ctx, key, val)

			return val, nil
		}
	}

	// 3. Cold Tier
	if m.coldTier != nil {
		entry, err := m.coldTier.Get(ctx, key)
		if err == nil {
			val := entry.Value.(string)
			m.stats.RecordAccess(key, int64(len(val)))

			// Promote from cold
			m.promoteFromCold(ctx, key, val)

			return val, nil
		}
	}

	return "", engine.ErrKeyNotFound
}

// Set sets value to hot tier
func (m *Manager) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	err := m.hotTier.Set(ctx, key, value, ttl)
	if err != nil {
		return err
	}

	m.stats.RecordWrite(key, int64(len(value)))
	return nil
}

// Del deletes from all tiers
func (m *Manager) Del(ctx context.Context, keys ...string) (int64, error) {
	var count int64

	c, _ := m.hotTier.Del(ctx, keys...)
	count += c

	if m.warmTier != nil {
		c, _ := m.warmTier.Del(ctx, keys...)
		count += c
	}

	if m.coldTier != nil {
		c, _ := m.coldTier.Del(ctx, keys...)
		count += c
	}

	for _, key := range keys {
		m.stats.RecordDelete(key)
	}

	return count, nil
}

func (m *Manager) maybePromote(ctx context.Context, key, value string) {
	stats := m.stats.GetStats(key)
	if m.policy.ShouldPromote(stats, TierWarm) {
		go func() {
			m.hotTier.Set(ctx, key, value, 0) // Keep TTL? Complex issue.
			if m.warmTier != nil {
				m.warmTier.Del(ctx, key)
			}
			m.stats.UpdateTier(key, TierHot)
			// log.Printf("Promoted key %s from warm to hot tier", key)
		}()
	}
}

func (m *Manager) promoteFromCold(ctx context.Context, key, value string) {
	go func() {
		if m.warmTier != nil {
			m.warmTier.Set(ctx, key, value, 0)
		}
		if m.coldTier != nil {
			m.coldTier.Del(ctx, key)
		}
		m.stats.UpdateTier(key, TierWarm)
		// log.Printf("Promoted key %s from cold to warm tier", key)
	}()
}

// GetStats returns tier stats
func (m *Manager) GetStats() *TieredStats {
	hotKeys := len(m.stats.GetKeysByTier(TierHot))
	warmKeys := len(m.stats.GetKeysByTier(TierWarm))
	coldKeys := len(m.stats.GetKeysByTier(TierCold))

	return &TieredStats{
		HotTierKeys:  int64(hotKeys),
		WarmTierKeys: int64(warmKeys),
		ColdTierKeys: int64(coldKeys),
		TotalKeys:    int64(hotKeys + warmKeys + coldKeys),
	}
}

// TieredStats holds stats
type TieredStats struct {
	HotTierKeys    int64
	HotTierSize    int64
	WarmTierKeys   int64
	WarmTierSize   int64
	ColdTierKeys   int64
	ColdTierSize   int64
	TotalKeys      int64
	TotalSize      int64
	MigrationsUp   int64
	MigrationsDown int64
}
