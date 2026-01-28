package tiered

import (
	"sync"
	"sync/atomic"
	"time"
)

// AccessStats contains access statistics for a key
type AccessStats struct {
	// Access count
	AccessCount uint64

	// Last access timestamp (UnixNano)
	LastAccessTime int64

	// Write timestamp (UnixNano)
	WriteTime int64

	// Data size in bytes
	Size int64

	// Current tier
	Tier TierType
}

// TierType represents storage tier
type TierType int

const (
	TierHot  TierType = iota // Hot data - Memory
	TierWarm                 // Warm data - SSD
	TierCold                 // Cold data - S3
)

func (t TierType) String() string {
	switch t {
	case TierHot:
		return "hot"
	case TierWarm:
		return "warm"
	case TierCold:
		return "cold"
	default:
		return "unknown"
	}
}

// StatsCollector collects access statistics
type StatsCollector struct {
	stats map[string]*AccessStats
	mu    sync.RWMutex

	// Count-Min Sketch for efficient frequency estimation
	sketch *CountMinSketch

	// Sample rate (0.0 - 1.0)
	sampleRate float64

	// Decay configuration
	decayInterval time.Duration
	decayFactor   float64

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewStatsCollector creates a new stats collector
func NewStatsCollector(sampleRate float64) *StatsCollector {
	sc := &StatsCollector{
		stats:         make(map[string]*AccessStats),
		sketch:        NewCountMinSketch(65536, 4), // 64K * 4 rows
		sampleRate:    sampleRate,
		decayInterval: time.Minute,
		decayFactor:   0.5,
		stopCh:        make(chan struct{}),
	}

	// Start decay loop
	sc.wg.Add(1)
	go sc.decayLoop()

	return sc
}

// RecordAccess records a read access
func (sc *StatsCollector) RecordAccess(key string, size int64) {
	now := time.Now().UnixNano()

	// Update Count-Min Sketch
	sc.sketch.Increment(key)

	sc.mu.Lock()
	stats, ok := sc.stats[key]
	if !ok {
		stats = &AccessStats{
			WriteTime: now,
			Size:      size,
			Tier:      TierHot,
		}
		sc.stats[key] = stats
	}
	stats.LastAccessTime = now
	atomic.AddUint64(&stats.AccessCount, 1)
	sc.mu.Unlock()
}

// RecordWrite records a write access
func (sc *StatsCollector) RecordWrite(key string, size int64) {
	now := time.Now().UnixNano()

	sc.mu.Lock()
	stats, ok := sc.stats[key]
	if !ok {
		stats = &AccessStats{
			Tier: TierHot,
		}
		sc.stats[key] = stats
	}
	stats.WriteTime = now
	stats.LastAccessTime = now
	stats.Size = size
	atomic.AddUint64(&stats.AccessCount, 1)
	sc.mu.Unlock()

	sc.sketch.Increment(key)
}

// RecordDelete records a delete
func (sc *StatsCollector) RecordDelete(key string) {
	sc.mu.Lock()
	delete(sc.stats, key)
	sc.mu.Unlock()
}

// GetStats gets stats for a key
func (sc *StatsCollector) GetStats(key string) *AccessStats {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	if stats, ok := sc.stats[key]; ok {
		// Return copy
		return &AccessStats{
			AccessCount:    atomic.LoadUint64(&stats.AccessCount),
			LastAccessTime: stats.LastAccessTime,
			WriteTime:      stats.WriteTime,
			Size:           stats.Size,
			Tier:           stats.Tier,
		}
	}
	return nil
}

// GetFrequency gets frequency estimate
func (sc *StatsCollector) GetFrequency(key string) uint8 {
	return sc.sketch.Estimate(key)
}

// UpdateTier updates key tier
func (sc *StatsCollector) UpdateTier(key string, tier TierType) {
	sc.mu.Lock()
	if stats, ok := sc.stats[key]; ok {
		stats.Tier = tier
	}
	sc.mu.Unlock()
}

// GetKeysByTier gets all keys in a tier
func (sc *StatsCollector) GetKeysByTier(tier TierType) []string {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	var keys []string
	for key, stats := range sc.stats {
		if stats.Tier == tier {
			keys = append(keys, key)
		}
	}
	return keys
}

// GetColdKeys gets cold keys for demotion
func (sc *StatsCollector) GetColdKeys(idleThreshold time.Duration, countThreshold uint64) []string {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	now := time.Now().UnixNano()
	var coldKeys []string

	for key, stats := range sc.stats {
		if stats.Tier != TierHot {
			continue
		}

		idleTime := time.Duration(now - stats.LastAccessTime)
		accessCount := atomic.LoadUint64(&stats.AccessCount)

		// Idle time exceeds threshold AND access count below threshold
		if idleTime > idleThreshold && accessCount < countThreshold {
			coldKeys = append(coldKeys, key)
		}
	}

	return coldKeys
}

// GetHotKeys gets hot keys for promotion
func (sc *StatsCollector) GetHotKeys(countThreshold uint64) []string {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	var hotKeys []string

	for key, stats := range sc.stats {
		if stats.Tier == TierHot {
			continue
		}

		accessCount := atomic.LoadUint64(&stats.AccessCount)

		// Access count exceeds threshold
		if accessCount > countThreshold {
			hotKeys = append(hotKeys, key)
		}
	}

	return hotKeys
}

// decayLoop regularly decays access counts
func (sc *StatsCollector) decayLoop() {
	defer sc.wg.Done()

	ticker := time.NewTicker(sc.decayInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sc.stopCh:
			return
		case <-ticker.C:
			sc.decay()
		}
	}
}

// decay decays all counts
func (sc *StatsCollector) decay() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	for _, stats := range sc.stats {
		old := atomic.LoadUint64(&stats.AccessCount)
		newCount := uint64(float64(old) * sc.decayFactor)
		atomic.StoreUint64(&stats.AccessCount, newCount)
	}

	// Reset sketch
	sc.sketch.Reset()
}

// Stop stops the collector
func (sc *StatsCollector) Stop() {
	close(sc.stopCh)
	sc.wg.Wait()
}

// =============== Count-Min Sketch ===============

// CountMinSketch implementation
type CountMinSketch struct {
	mu     sync.Mutex
	matrix [][]uint8
	width  int
	depth  int
}

// NewCountMinSketch creates CMS
func NewCountMinSketch(width, depth int) *CountMinSketch {
	matrix := make([][]uint8, depth)
	for i := range matrix {
		matrix[i] = make([]uint8, width)
	}
	return &CountMinSketch{
		matrix: matrix,
		width:  width,
		depth:  depth,
	}
}

// Increment increments count
func (c *CountMinSketch) Increment(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := 0; i < c.depth; i++ {
		idx := c.hash(key, i) % c.width
		if c.matrix[i][idx] < 255 {
			c.matrix[i][idx]++
		}
	}
}

// Estimate estimates frequency
func (c *CountMinSketch) Estimate(key string) uint8 {
	c.mu.Lock()
	defer c.mu.Unlock()
	min := uint8(255)
	for i := 0; i < c.depth; i++ {
		idx := c.hash(key, i) % c.width
		if c.matrix[i][idx] < min {
			min = c.matrix[i][idx]
		}
	}
	return min
}

// Reset resets (decays)
func (c *CountMinSketch) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.matrix {
		for j := range c.matrix[i] {
			c.matrix[i][j] /= 2
		}
	}
}

func (c *CountMinSketch) hash(key string, seed int) int {
	h := seed * 0x9e3779b9
	for _, ch := range key {
		h = h*31 + int(ch)
	}
	if h < 0 {
		h = -h
	}
	return h
}
