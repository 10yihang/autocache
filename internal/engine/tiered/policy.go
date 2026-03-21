package tiered

import (
	"math"
	"sync"
	"time"
)

// TierPolicy defines tiering policy
type TierPolicy interface {
	// ShouldDemote checks if key should be demoted
	ShouldDemote(key string, stats *AccessStats, currentTier TierType) bool

	// ShouldPromote checks if key should be promoted
	ShouldPromote(key string, stats *AccessStats, currentTier TierType) bool

	// GetTargetTier gets target tier for a key
	GetTargetTier(key string, stats *AccessStats) TierType
}

type PolicyObserver interface {
	RecordAccess(key string, tier TierType)
	RecordWrite(key string, tier TierType)
	RecordMove(key string, from, to TierType)
}

// DefaultPolicy implementation
type DefaultPolicy struct {
	// Hot tier thresholds
	HotAccessThreshold uint64        // Access count threshold
	HotIdleThreshold   time.Duration // Idle time threshold

	// Warm tier thresholds
	WarmAccessThreshold uint64
	WarmIdleThreshold   time.Duration

	// Cold tier thresholds
	ColdIdleThreshold time.Duration
}

// NewDefaultPolicy creates default policy
func NewDefaultPolicy() *DefaultPolicy {
	return &DefaultPolicy{
		HotAccessThreshold:  100,
		HotIdleThreshold:    5 * time.Minute,
		WarmAccessThreshold: 10,
		WarmIdleThreshold:   30 * time.Minute,
		ColdIdleThreshold:   2 * time.Hour,
	}
}

// ShouldDemote checks if key should be demoted
func (p *DefaultPolicy) ShouldDemote(_ string, stats *AccessStats, currentTier TierType) bool {
	if stats == nil {
		return false
	}

	now := time.Now().UnixNano()
	idleTime := time.Duration(now - stats.LastAccessTime)

	switch currentTier {
	case TierHot:
		// Hot -> Warm: Idle too long AND access count low
		return idleTime > p.HotIdleThreshold && stats.AccessCount < p.HotAccessThreshold
	case TierWarm:
		// Warm -> Cold: Idle too long
		return idleTime > p.ColdIdleThreshold
	default:
		return false
	}
}

// ShouldPromote checks if key should be promoted
func (p *DefaultPolicy) ShouldPromote(_ string, stats *AccessStats, currentTier TierType) bool {
	if stats == nil {
		return false
	}

	switch currentTier {
	case TierWarm:
		// Warm -> Hot: Access count high
		return stats.AccessCount > p.HotAccessThreshold
	case TierCold:
		// Cold -> Warm: Access count medium
		return stats.AccessCount > p.WarmAccessThreshold
	default:
		return false
	}
}

// GetTargetTier gets target tier
func (p *DefaultPolicy) GetTargetTier(_ string, stats *AccessStats) TierType {
	if stats == nil {
		return TierHot
	}

	now := time.Now().UnixNano()
	idleTime := time.Duration(now - stats.LastAccessTime)

	// High frequency -> Hot
	if stats.AccessCount >= p.HotAccessThreshold {
		return TierHot
	}

	// Medium frequency OR recently accessed -> Warm
	if stats.AccessCount >= p.WarmAccessThreshold || idleTime < p.WarmIdleThreshold {
		return TierWarm
	}

	// Low frequency AND idle -> Cold
	return TierCold
}

type WTinyLFUCostAwareConfig struct {
	HotCapacity               int
	HotIdleThreshold          time.Duration
	WindowPercentage          int
	ProtectedPercentage       int
	WarmPromotionMinFrequency uint64
	WarmToColdDemoteThreshold float64
	CostAccessWeight          float64
	CostRecencyWeight         float64
	CostSizePenaltyWeight     float64
	CostWritePenaltyWeight    float64
}

type WTinyLFUCostAwarePolicy struct {
	cfg WTinyLFUCostAwareConfig

	mu sync.Mutex

	windowAdmissionFreq uint8
	protectedMinFreq    uint8
	sampleLimit         int
	sampleCount         int

	sketch     *CountMinSketch
	doorkeeper *BloomFilter
	warmGhost  map[string]uint8
}

func NewWTinyLFUCostAwarePolicy(cfg WTinyLFUCostAwareConfig) *WTinyLFUCostAwarePolicy {
	if cfg.HotCapacity <= 0 {
		cfg.HotCapacity = 1024
	}
	if cfg.HotIdleThreshold <= 0 {
		cfg.HotIdleThreshold = 5 * time.Minute
	}
	if cfg.WindowPercentage <= 0 {
		cfg.WindowPercentage = 1
	}
	if cfg.ProtectedPercentage <= 0 || cfg.ProtectedPercentage >= 100 {
		cfg.ProtectedPercentage = 80
	}
	if cfg.WarmPromotionMinFrequency == 0 {
		cfg.WarmPromotionMinFrequency = 2
	}
	if cfg.CostAccessWeight == 0 {
		cfg.CostAccessWeight = 1.0
	}
	if cfg.CostRecencyWeight == 0 {
		cfg.CostRecencyWeight = 0.4
	}
	if cfg.CostSizePenaltyWeight == 0 {
		cfg.CostSizePenaltyWeight = 0.2
	}
	if cfg.CostWritePenaltyWeight == 0 {
		cfg.CostWritePenaltyWeight = 0.2
	}
	if cfg.WarmToColdDemoteThreshold == 0 {
		cfg.WarmToColdDemoteThreshold = -0.5
	}

	windowAdmissionFreq := uint8(1)
	if cfg.WarmPromotionMinFrequency > 1 {
		windowAdmissionFreq = uint8((cfg.WarmPromotionMinFrequency*uint64(cfg.WindowPercentage) + 99) / 100)
		if windowAdmissionFreq < 1 {
			windowAdmissionFreq = 1
		}
	}
	protectedMinFreq := uint8(cfg.WarmPromotionMinFrequency)
	if protectedMinFreq < 1 {
		protectedMinFreq = 1
	}
	sampleLimit := cfg.HotCapacity * 8
	if sampleLimit < 1024 {
		sampleLimit = 1024
	}

	return &WTinyLFUCostAwarePolicy{
		cfg:                 cfg,
		windowAdmissionFreq: windowAdmissionFreq,
		protectedMinFreq:    protectedMinFreq,
		sampleLimit:         sampleLimit,
		sketch:              NewCountMinSketch(65536, 4),
		doorkeeper:          NewBloomFilter(65536),
		warmGhost:           make(map[string]uint8),
	}
}

func (p *WTinyLFUCostAwarePolicy) ShouldDemote(key string, stats *AccessStats, currentTier TierType) bool {
	if stats == nil {
		return false
	}

	switch currentTier {
	case TierHot:
		idle := time.Since(time.Unix(0, stats.LastAccessTime))
		if idle <= p.cfg.HotIdleThreshold {
			return false
		}

		frequency := p.estimateFrequency(key, stats)
		if frequency <= p.windowAdmissionFreq {
			return true
		}
		if frequency < p.protectedMinFreq {
			return true
		}
		return false
	case TierWarm:
		return p.utilityScore(stats) <= p.cfg.WarmToColdDemoteThreshold
	default:
		return false
	}
}

func (p *WTinyLFUCostAwarePolicy) ShouldPromote(key string, stats *AccessStats, currentTier TierType) bool {
	if stats == nil {
		return false
	}

	switch currentTier {
	case TierWarm:
		return p.estimateFrequency(key, stats) >= p.protectedMinFreq
	case TierCold:
		return p.estimateFrequency(key, stats) >= p.protectedMinFreq
	default:
		return false
	}
}

func (p *WTinyLFUCostAwarePolicy) GetTargetTier(key string, stats *AccessStats) TierType {
	if stats == nil {
		return TierHot
	}
	if p.ShouldPromote(key, stats, TierWarm) {
		return TierHot
	}
	if p.ShouldDemote(key, stats, TierWarm) {
		return TierCold
	}
	return TierWarm
}

func (p *WTinyLFUCostAwarePolicy) RecordAccess(key string, tier TierType) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if key == "" {
		return
	}

	if p.doorkeeper.Contains(key) {
		p.sketch.Increment(key)
	} else {
		p.doorkeeper.Add(key)
	}

	if tier == TierWarm || tier == TierCold {
		if _, ok := p.warmGhost[key]; ok {
			p.warmGhost[key] = p.sketch.Estimate(key)
		}
	}

	p.sampleCount++
	if p.sampleCount >= p.sampleLimit {
		p.sampleCount = 0
		p.sketch.Reset()
		p.doorkeeper.Reset()
	}
}

func (p *WTinyLFUCostAwarePolicy) RecordWrite(key string, tier TierType) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if key == "" {
		return
	}
	if !p.doorkeeper.Contains(key) {
		p.doorkeeper.Add(key)
	} else {
		p.sketch.Increment(key)
	}
	if tier == TierHot {
		delete(p.warmGhost, key)
	}
}

func (p *WTinyLFUCostAwarePolicy) RecordMove(key string, from, to TierType) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if key == "" {
		return
	}

	if from == TierHot && to == TierWarm {
		p.warmGhost[key] = p.sketch.Estimate(key)
		return
	}
	if from == TierWarm && to == TierHot {
		delete(p.warmGhost, key)
	}
}

func (p *WTinyLFUCostAwarePolicy) estimateFrequency(key string, stats *AccessStats) uint8 {
	if stats == nil {
		return 0
	}
	p.mu.Lock()
	estimate := p.sketch.Estimate(key)
	p.mu.Unlock()
	if estimate == 0 {
		if stats.AccessCount > 255 {
			return 255
		}
		return uint8(stats.AccessCount)
	}
	return estimate
}

func (p *WTinyLFUCostAwarePolicy) utilityScore(stats *AccessStats) float64 {
	access := math.Log1p(float64(stats.AccessCount))
	idleHours := time.Since(time.Unix(0, stats.LastAccessTime)).Hours()
	if idleHours < 0 {
		idleHours = 0
	}

	writeAgeHours := time.Since(time.Unix(0, stats.WriteTime)).Hours()
	if writeAgeHours < 0 {
		writeAgeHours = 0
	}
	writePenalty := 1.0 / (1.0 + writeAgeHours)

	sizeKB := float64(stats.Size) / 1024.0

	return p.cfg.CostAccessWeight*access -
		p.cfg.CostRecencyWeight*idleHours -
		p.cfg.CostSizePenaltyWeight*sizeKB -
		p.cfg.CostWritePenaltyWeight*writePenalty
}

// =============== W-TinyLFU Policy (Advanced) ===============

// TinyLFUPolicy implements W-TinyLFU
// Combines LRU recency and LFU frequency
type TinyLFUPolicy struct {
	// Window cache size (1% of total)
	windowSize int

	// Main cache segment size (99% of total)
	// Split into Protected (80%) and Probation (20%)
	protectedSize int
	probationSize int

	// Frequency stats
	sketch *CountMinSketch

	// Doorkeeper filter
	doorkeeper *BloomFilter
}

// NewTinyLFUPolicy creates W-TinyLFU policy
func NewTinyLFUPolicy(totalSize int) *TinyLFUPolicy {
	windowSize := totalSize / 100
	mainSize := totalSize - windowSize

	return &TinyLFUPolicy{
		windowSize:    windowSize,
		protectedSize: mainSize * 80 / 100,
		probationSize: mainSize * 20 / 100,
		sketch:        NewCountMinSketch(65536, 4),
		doorkeeper:    NewBloomFilter(65536),
	}
}

// ShouldAdmit checks if key should be admitted to cache
func (p *TinyLFUPolicy) ShouldAdmit(candidateKey, victimKey string) bool {
	candidateFreq := p.sketch.Estimate(candidateKey)
	victimFreq := p.sketch.Estimate(victimKey)

	// Admit if candidate has higher frequency
	return candidateFreq > victimFreq
}

// BloomFilter simple implementation
type BloomFilter struct {
	bits []bool
	size int
}

// NewBloomFilter creates bloom filter
func NewBloomFilter(size int) *BloomFilter {
	return &BloomFilter{
		bits: make([]bool, size),
		size: size,
	}
}

// Add adds key
func (bf *BloomFilter) Add(key string) {
	for i := 0; i < 3; i++ {
		idx := bf.hash(key, i)
		bf.bits[idx] = true
	}
}

// Contains checks key
func (bf *BloomFilter) Contains(key string) bool {
	for i := 0; i < 3; i++ {
		idx := bf.hash(key, i)
		if !bf.bits[idx] {
			return false
		}
	}
	return true
}

// Reset resets filter
func (bf *BloomFilter) Reset() {
	for i := range bf.bits {
		bf.bits[i] = false
	}
}

func (bf *BloomFilter) hash(key string, seed int) int {
	h := seed * 0x9e3779b9
	for _, ch := range key {
		h = h*31 + int(ch)
	}
	if h < 0 {
		h = -h
	}
	return h % bf.size
}
