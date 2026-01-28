package tiered

import (
	"time"
)

// TierPolicy defines tiering policy
type TierPolicy interface {
	// ShouldDemote checks if key should be demoted
	ShouldDemote(stats *AccessStats, currentTier TierType) bool

	// ShouldPromote checks if key should be promoted
	ShouldPromote(stats *AccessStats, currentTier TierType) bool

	// GetTargetTier gets target tier for a key
	GetTargetTier(stats *AccessStats) TierType
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
func (p *DefaultPolicy) ShouldDemote(stats *AccessStats, currentTier TierType) bool {
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
func (p *DefaultPolicy) ShouldPromote(stats *AccessStats, currentTier TierType) bool {
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
func (p *DefaultPolicy) GetTargetTier(stats *AccessStats) TierType {
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
