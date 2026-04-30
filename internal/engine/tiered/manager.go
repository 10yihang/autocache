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
	"github.com/10yihang/autocache/internal/persistence"
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
	PolicyConfig       *WTinyLFUCostAwareConfig
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

	// Write-behind persistence
	writeBehind *persistence.WriteBehind

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu sync.RWMutex

	// Semaphore to limit concurrent promotions
	promoteSem chan struct{}
}

// NewManager creates a tiered manager
func NewManager(cfg *Config, hotTier *memory.Store) (*Manager, error) {
	ctx, cancel := context.WithCancel(context.Background())

	policyCfg := cfg.PolicyConfig
	if policyCfg == nil {
		defaultCfg := DefaultWTinyLFUCostAwareConfig()
		policyCfg = &defaultCfg
	}
	policyCfg.HotIdleThreshold = cfg.HotIdleThreshold
	policyCfg.WarmPromotionMinFrequency = cfg.HotAccessThreshold

	m := &Manager{
		config:     cfg,
		hotTier:    hotTier,
		stats:      NewStatsCollector(1.0),
		policy:     NewWTinyLFUCostAwarePolicy(*policyCfg),
		ctx:        ctx,
		cancel:     cancel,
		promoteSem: make(chan struct{}, 16),
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
	metrics2.SetTierCapacity(TierHot.String(), cfg.HotTierCapacity)
	metrics2.SetTierCapacity(TierWarm.String(), cfg.WarmTierCapacity)
	metrics2.SetTierCapacity(TierCold.String(), cfg.ColdTierCapacity)
	m.updateTierMetrics()

	return m, nil
}

func (m *Manager) initWarmTier(path string) (engine.Engine, error) {
	return badger.NewStore(path)
}

func (m *Manager) initColdTier(endpoint, bucket string) (engine.Engine, error) {
	return s3.NewStore(endpoint, bucket)
}

// SetWriteBehind attaches a write-behind persistence layer backed by the warm tier.
func (m *Manager) SetWriteBehind(wb *persistence.WriteBehind) {
	m.writeBehind = wb
}

// GetWriteBehind returns the attached write-behind, if any.
func (m *Manager) GetWriteBehind() *persistence.WriteBehind {
	return m.writeBehind
}

// warmStore returns the warm tier as a badger.Store, if available.
func (m *Manager) warmStore() *badger.Store {
	if m.warmTier == nil {
		return nil
	}
	store, ok := m.warmTier.(*badger.Store)
	if !ok {
		return nil
	}
	return store
}

// LoadFromDisk restores all keys from the warm tier into the hot tier memory store.
func (m *Manager) LoadFromDisk(ctx context.Context) (int64, error) {
	store := m.warmStore()
	if store == nil {
		return 0, fmt.Errorf("warm tier not available for loading")
	}
	wb := persistence.NewWriteBehind(store.DB(), m.hotTier, persistence.DefaultConfig())
	return wb.LoadIntoMemory(ctx)
}

// InitWriteBehind creates and starts a write-behind persistence layer backed by the warm tier.
func (m *Manager) InitWriteBehind(cfg persistence.Config) error {
	store := m.warmStore()
	if store == nil {
		return fmt.Errorf("warm tier not available for write-behind")
	}
	wb := persistence.NewWriteBehind(store.DB(), m.hotTier, cfg)
	m.SetWriteBehind(wb)
	wb.Start()
	return nil
}

// Start starts background tasks
func (m *Manager) Start() {
	m.wg.Add(1)
	go m.migrator.MigrationLoop()
	log.Println("Tiered storage manager started")
}

// Stop stops background tasks and flushes pending writes.
func (m *Manager) Stop() {
	m.cancel()
	m.wg.Wait()
	m.stats.Stop()

	if m.writeBehind != nil {
		m.writeBehind.Stop()
	}

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

// Set sets value to hot tier and pushes to write-behind if enabled.
func (m *Manager) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	err := m.hotTier.Set(ctx, key, value, ttl)
	if err != nil {
		return err
	}

	if m.writeBehind != nil {
		var expireAt time.Time
		if ttl > 0 {
			expireAt = time.Now().Add(ttl)
		}
		m.writeBehind.AppendSet(key, &engine.Entry{
			Key:      key,
			Value:    value,
			Type:     engine.TypeString,
			ExpireAt: expireAt,
		})
	}

	m.stats.RecordWrite(key, int64(len(value)))
	m.recordPolicyWrite(key, TierHot)
	m.updateTierMetrics()
	return nil
}

// Del deletes from all tiers and pushes tombstone to write-behind.
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

	if m.writeBehind != nil {
		for _, key := range keys {
			m.writeBehind.AppendDel(key)
		}
	}

	for _, key := range keys {
		m.stats.RecordDelete(key)
	}
	m.updateTierMetrics()

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
		select {
		case m.promoteSem <- struct{}{}:
		case <-ctx.Done():
			return
		default:
			return
		}
		go func() {
			defer func() { <-m.promoteSem }()
			start := time.Now()
			metrics2.AddTieredMigrationInFlight(1)
			defer metrics2.AddTieredMigrationInFlight(-1)
			promoCtx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
			defer cancel()
			ttl := entryTTL(entry)
			if err := m.hotTier.Set(promoCtx, key, value, ttl); err != nil {
				metrics2.RecordTieredMigrationError("up", "storage_error")
				metrics2.ObserveTieredMigrationDuration("up", time.Since(start), false)
				return
			}
			if m.warmTier != nil {
				if _, err := m.warmTier.Del(promoCtx, key); err != nil {
					metrics2.RecordTieredMigrationError("up", classifyMigrationError(err))
					metrics2.ObserveTieredMigrationDuration("up", time.Since(start), false)
					return
				}
			}
			m.stats.UpdateTier(key, TierHot)
			m.recordPolicyMove(key, TierWarm, TierHot)
			m.migrationsUp.Add(1)
			metrics2.RecordTieredMigration("up")
			m.updateTierMetrics()
			metrics2.ObserveTieredMigrationDuration("up", time.Since(start), true)
			// log.Printf("Promoted key %s from warm to hot tier", key)
		}()
	}
}

func (m *Manager) promoteFromCold(ctx context.Context, key, value string, entry *engine.Entry) {
	select {
	case m.promoteSem <- struct{}{}:
	case <-ctx.Done():
		return
	default:
		return
	}
	go func() {
		defer func() { <-m.promoteSem }()
		start := time.Now()
		metrics2.AddTieredMigrationInFlight(1)
		defer metrics2.AddTieredMigrationInFlight(-1)
		promoCtx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
		defer cancel()
		ttl := entryTTL(entry)
		if m.warmTier != nil {
			if err := m.warmTier.Set(promoCtx, key, value, ttl); err != nil {
				metrics2.RecordTieredMigrationError("up", "storage_error")
				metrics2.ObserveTieredMigrationDuration("up", time.Since(start), false)
				return
			}
		}
		if m.coldTier != nil {
			if _, err := m.coldTier.Del(promoCtx, key); err != nil {
				metrics2.RecordTieredMigrationError("up", classifyMigrationError(err))
				metrics2.ObserveTieredMigrationDuration("up", time.Since(start), false)
				return
			}
		}
		m.stats.UpdateTier(key, TierWarm)
		m.recordPolicyMove(key, TierCold, TierWarm)
		m.migrationsUp.Add(1)
		metrics2.RecordTieredMigration("up")
		m.updateTierMetrics()
		metrics2.ObserveTieredMigrationDuration("up", time.Since(start), true)
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
	m.updateTierMetricsWithStats(stats)
	return stats
}

func (m *Manager) updateTierMetrics() {
	m.updateTierMetricsWithStats(&TieredStats{
		HotTierKeys:  int64(len(m.stats.GetKeysByTier(TierHot))),
		HotTierSize:  m.stats.GetSizeByTier(TierHot),
		WarmTierKeys: int64(len(m.stats.GetKeysByTier(TierWarm))),
		WarmTierSize: m.stats.GetSizeByTier(TierWarm),
		ColdTierKeys: int64(len(m.stats.GetKeysByTier(TierCold))),
		ColdTierSize: m.stats.GetSizeByTier(TierCold),
	})
}

func (m *Manager) updateTierMetricsWithStats(stats *TieredStats) {
	if stats == nil {
		return
	}
	metrics2.TieredKeys.WithLabelValues(TierHot.String()).Set(float64(stats.HotTierKeys))
	metrics2.TieredKeys.WithLabelValues(TierWarm.String()).Set(float64(stats.WarmTierKeys))
	metrics2.TieredKeys.WithLabelValues(TierCold.String()).Set(float64(stats.ColdTierKeys))
	metrics2.SetTieredBytes(TierHot.String(), stats.HotTierSize)
	metrics2.SetTieredBytes(TierWarm.String(), stats.WarmTierSize)
	metrics2.SetTieredBytes(TierCold.String(), stats.ColdTierSize)
	metrics2.KeysTotal.Set(float64(stats.HotTierKeys + stats.WarmTierKeys + stats.ColdTierKeys))
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
