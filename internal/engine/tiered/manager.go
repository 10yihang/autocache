package tiered

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/10yihang/autocache/internal/engine"
	"github.com/10yihang/autocache/internal/engine/badger"
	"github.com/10yihang/autocache/internal/engine/memory"
	"github.com/10yihang/autocache/internal/engine/s3"
	metrics2 "github.com/10yihang/autocache/internal/metrics"
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

	migrationsUp   atomic.Int64
	migrationsDown atomic.Int64

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
		policy: NewWTinyLFUCostAwarePolicy(WTinyLFUCostAwareConfig{
			HotCapacity:               1024,
			HotIdleThreshold:          cfg.HotIdleThreshold,
			WindowPercentage:          1,
			ProtectedPercentage:       80,
			WarmPromotionMinFrequency: cfg.HotAccessThreshold,
			WarmToColdDemoteThreshold: -0.5,
			CostAccessWeight:          1.0,
			CostRecencyWeight:         0.4,
			CostSizePenaltyWeight:     0.2,
			CostWritePenaltyWeight:    0.2,
		}),
		ctx:    ctx,
		cancel: cancel,
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
		m.recordPolicyAccess(key, TierHot)
		return val, nil
	}

	// 2. Warm Tier
	if m.warmTier != nil {
		entry, err := m.warmTier.Get(ctx, key)
		if err == nil {
			val, err := stringEntryValue(entry)
			if err != nil {
				return "", err
			}
			m.stats.RecordAccess(key, int64(len(val)))
			m.recordPolicyAccess(key, TierWarm)

			// Consider promotion
			m.maybePromote(ctx, key, val, entry)

			return val, nil
		}
	}

	// 3. Cold Tier
	if m.coldTier != nil {
		entry, err := m.coldTier.Get(ctx, key)
		if err == nil {
			val, err := stringEntryValue(entry)
			if err != nil {
				return "", err
			}
			m.stats.RecordAccess(key, int64(len(val)))
			m.recordPolicyAccess(key, TierCold)

			// Promote from cold
			m.promoteFromCold(ctx, key, val, entry)

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
	m.recordPolicyWrite(key, TierHot)
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

func (m *Manager) Exists(ctx context.Context, keys ...string) (int64, error) {
	var count int64
	for _, key := range keys {
		if _, err := m.GetEntry(ctx, key); err == nil {
			count++
		}
	}
	return count, nil
}

func (m *Manager) Keys(ctx context.Context, pattern string) ([]string, error) {
	seen := make(map[string]struct{})
	keys := make([]string, 0)
	appendKeys := func(list []string) {
		for _, key := range list {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	if hotKeys, err := m.hotTier.Keys(ctx, pattern); err == nil {
		appendKeys(hotKeys)
	}
	if m.warmTier != nil {
		if warmKeys, err := m.warmTier.Keys(ctx, pattern); err == nil {
			appendKeys(warmKeys)
		}
	}
	if m.coldTier != nil {
		if coldKeys, err := m.coldTier.Keys(ctx, pattern); err == nil {
			appendKeys(coldKeys)
		}
	}
	return keys, nil
}

func (m *Manager) Type(ctx context.Context, key string) (string, error) {
	entry, err := m.GetEntry(ctx, key)
	if err != nil {
		return "none", nil
	}
	switch entry.Type {
	case engine.TypeString:
		return "string", nil
	case engine.TypeHash:
		return "hash", nil
	case engine.TypeList:
		return "list", nil
	case engine.TypeSet:
		return "set", nil
	case engine.TypeZSet:
		return "zset", nil
	default:
		return "none", nil
	}
}

func (m *Manager) GetEntry(ctx context.Context, key string) (*engine.Entry, error) {
	if entry, err := m.hotTier.GetEntry(ctx, key); err == nil {
		return entry, nil
	}
	if m.warmTier != nil {
		if entry, err := m.warmTier.Get(ctx, key); err == nil {
			return entry, nil
		}
	}
	if m.coldTier != nil {
		if entry, err := m.coldTier.Get(ctx, key); err == nil {
			return entry, nil
		}
	}
	return nil, engine.ErrKeyNotFound
}

func (m *Manager) TTL(ctx context.Context, key string) (time.Duration, error) {
	if ttl, err := m.hotTier.TTL(ctx, key); err == nil && ttl != -2*time.Second {
		return ttl, nil
	}
	if m.warmTier != nil {
		if ttl, err := m.warmTier.TTL(ctx, key); err == nil && ttl != -2*time.Second {
			return ttl, nil
		}
	}
	if m.coldTier != nil {
		if ttl, err := m.coldTier.TTL(ctx, key); err == nil && ttl != -2*time.Second {
			return ttl, nil
		}
	}
	return -2 * time.Second, nil
}

func (m *Manager) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if ok, _ := m.hotTier.Expire(ctx, key, ttl); ok {
		return true, nil
	}
	if m.warmTier != nil {
		if ok, _ := m.warmTier.Expire(ctx, key, ttl); ok {
			return true, nil
		}
	}
	if m.coldTier != nil {
		if ok, _ := m.coldTier.Expire(ctx, key, ttl); ok {
			return true, nil
		}
	}
	return false, nil
}

func (m *Manager) ExpireAt(ctx context.Context, key string, t time.Time) (bool, error) {
	return m.Expire(ctx, key, time.Until(t))
}

func (m *Manager) Persist(ctx context.Context, key string) (bool, error) {
	if ok, _ := m.hotTier.Persist(ctx, key); ok {
		return true, nil
	}
	if m.warmTier != nil {
		if ok, _ := m.warmTier.Persist(ctx, key); ok {
			return true, nil
		}
	}
	if m.coldTier != nil {
		if ok, _ := m.coldTier.Persist(ctx, key); ok {
			return true, nil
		}
	}
	return false, nil
}

func (m *Manager) DBSize(ctx context.Context) (int64, error) {
	var total int64
	if n, err := m.hotTier.DBSize(ctx); err == nil {
		total += n
	}
	if m.warmTier != nil {
		if n, err := m.warmTier.DBSize(ctx); err == nil {
			total += n
		}
	}
	if m.coldTier != nil {
		if n, err := m.coldTier.DBSize(ctx); err == nil {
			total += n
		}
	}
	return total, nil
}

func (m *Manager) FlushDB(ctx context.Context) error {
	if err := m.hotTier.FlushDB(ctx); err != nil {
		return err
	}
	if m.warmTier != nil {
		if err := m.warmTier.FlushDB(ctx); err != nil {
			return err
		}
	}
	if m.coldTier != nil {
		if err := m.coldTier.FlushDB(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	exists, err := m.Exists(ctx, key)
	if err != nil {
		return false, err
	}
	if exists > 0 {
		return false, nil
	}
	return true, m.Set(ctx, key, value, ttl)
}

func (m *Manager) SetXX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	exists, err := m.Exists(ctx, key)
	if err != nil {
		return false, err
	}
	if exists == 0 {
		return false, nil
	}
	return true, m.Set(ctx, key, value, ttl)
}

func (m *Manager) GetSet(ctx context.Context, key string, value string) (string, error) {
	current, err := m.Get(ctx, key)
	if err != nil && err != engine.ErrKeyNotFound {
		return "", err
	}
	_, _ = m.Del(ctx, key)
	if err := m.Set(ctx, key, value, 0); err != nil {
		return "", err
	}
	if err == engine.ErrKeyNotFound {
		return "", nil
	}
	return current, nil
}

func (m *Manager) IncrBy(ctx context.Context, key string, delta int64) (int64, error) {
	current, err := m.Get(ctx, key)
	if err != nil && err != engine.ErrKeyNotFound {
		return 0, err
	}
	var value int64
	if err == nil {
		value, err = strconv.ParseInt(current, 10, 64)
		if err != nil {
			return 0, err
		}
	}
	value += delta
	_, _ = m.Del(ctx, key)
	if err := m.Set(ctx, key, strconv.FormatInt(value, 10), 0); err != nil {
		return 0, err
	}
	return value, nil
}

func (m *Manager) Append(ctx context.Context, key string, suffix string) (int64, error) {
	current, err := m.Get(ctx, key)
	if err != nil && err != engine.ErrKeyNotFound {
		return 0, err
	}
	updated := current + suffix
	_, _ = m.Del(ctx, key)
	if err := m.Set(ctx, key, updated, 0); err != nil {
		return 0, err
	}
	return int64(len(updated)), nil
}

func (m *Manager) Strlen(ctx context.Context, key string) (int64, error) {
	value, err := m.Get(ctx, key)
	if err != nil {
		if err == engine.ErrKeyNotFound {
			return 0, nil
		}
		return 0, err
	}
	return int64(len(value)), nil
}

func (m *Manager) MGetBytes(ctx context.Context, keys ...string) ([][]byte, error) {
	result := make([][]byte, len(keys))
	for i, key := range keys {
		value, err := m.Get(ctx, key)
		if err != nil {
			if err == engine.ErrKeyNotFound {
				continue
			}
			return nil, err
		}
		result[i] = []byte(value)
	}
	return result, nil
}

func (m *Manager) maybePromote(ctx context.Context, key, value string, entry *engine.Entry) {
	stats := m.stats.GetStats(key)
	if m.policy.ShouldPromote(key, stats, TierWarm) {
		ttl := entryTTL(entry)
		go func() {
			m.hotTier.Set(ctx, key, value, ttl)
			if m.warmTier != nil {
				m.warmTier.Del(ctx, key)
			}
			m.stats.UpdateTier(key, TierHot)
			m.recordPolicyMove(key, TierWarm, TierHot)
			m.migrationsUp.Add(1)
			metrics2.RecordTieredMigration("up")
			// log.Printf("Promoted key %s from warm to hot tier", key)
		}()
	}
}

func (m *Manager) promoteFromCold(ctx context.Context, key, value string, entry *engine.Entry) {
	ttl := entryTTL(entry)
	go func() {
		if m.warmTier != nil {
			m.warmTier.Set(ctx, key, value, ttl)
		}
		if m.coldTier != nil {
			m.coldTier.Del(ctx, key)
		}
		m.stats.UpdateTier(key, TierWarm)
		m.recordPolicyMove(key, TierCold, TierWarm)
		m.migrationsUp.Add(1)
		metrics2.RecordTieredMigration("up")
		// log.Printf("Promoted key %s from cold to warm tier", key)
	}()
}

func (m *Manager) recordPolicyAccess(key string, tier TierType) {
	if observer, ok := m.policy.(PolicyObserver); ok {
		observer.RecordAccess(key, tier)
	}
}

func (m *Manager) recordPolicyWrite(key string, tier TierType) {
	if observer, ok := m.policy.(PolicyObserver); ok {
		observer.RecordWrite(key, tier)
	}
}

func (m *Manager) recordPolicyMove(key string, from, to TierType) {
	if observer, ok := m.policy.(PolicyObserver); ok {
		observer.RecordMove(key, from, to)
	}
}

func entryTTL(entry *engine.Entry) time.Duration {
	if entry == nil || entry.ExpireAt.IsZero() {
		return 0
	}
	ttl := time.Until(entry.ExpireAt)
	if ttl < 0 {
		return 0
	}
	return ttl
}

func stringEntryValue(entry *engine.Entry) (string, error) {
	if entry == nil {
		return "", engine.ErrKeyNotFound
	}
	switch value := entry.Value.(type) {
	case string:
		return value, nil
	case []byte:
		return string(value), nil
	default:
		return "", engine.ErrNotSupported
	}
}

// GetStats returns tier stats
func (m *Manager) GetStats() *TieredStats {
	hotKeys := len(m.stats.GetKeysByTier(TierHot))
	warmKeys := len(m.stats.GetKeysByTier(TierWarm))
	coldKeys := len(m.stats.GetKeysByTier(TierCold))
	hotSize := m.stats.GetSizeByTier(TierHot)
	warmSize := m.stats.GetSizeByTier(TierWarm)
	coldSize := m.stats.GetSizeByTier(TierCold)

	stats := &TieredStats{
		HotTierKeys:    int64(hotKeys),
		HotTierSize:    hotSize,
		WarmTierKeys:   int64(warmKeys),
		WarmTierSize:   warmSize,
		ColdTierKeys:   int64(coldKeys),
		ColdTierSize:   coldSize,
		TotalKeys:      int64(hotKeys + warmKeys + coldKeys),
		TotalSize:      hotSize + warmSize + coldSize,
		MigrationsUp:   m.migrationsUp.Load(),
		MigrationsDown: m.migrationsDown.Load(),
	}
	metrics2.TieredKeys.WithLabelValues("hot").Set(float64(stats.HotTierKeys))
	metrics2.TieredKeys.WithLabelValues("warm").Set(float64(stats.WarmTierKeys))
	metrics2.TieredKeys.WithLabelValues("cold").Set(float64(stats.ColdTierKeys))
	return stats
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
