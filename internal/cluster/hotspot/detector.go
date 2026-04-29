// Package hotspot provides per-slot hotspot detection for the cluster.
// Tracks per-slot QPS via atomic counters and flags slots whose current
// QPS significantly exceeds the cluster-wide average.
package hotspot

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	slotCount      = 16384
	sampleInterval = time.Second

	// EWMA decay factor for per-slot QPS baseline.
	avgAlpha = 0.1
	// Multiplier over cluster average to flag a slot as hot.
	// A slot needs QPS > avgQPS * hotMultiplier to be considered hot.
	hotMultiplier = 3.0
	// Absolute minimum QPS per slot to avoid noise in idle clusters.
	minHotQPS = 100
	// Number of sample windows to collect before emitting hotspots.
	samplesWarmup = 10
	// Maximum number of hot slots to report.
	maxHotSlots = 20
)

// HotSlotInfo describes a detected hot slot.
type HotSlotInfo struct {
	Slot      uint16  `json:"slot"`
	QPS       float64 `json:"qps"`
	AvgQPS    float64 `json:"avg_qps"`   // cluster-wide average QPS per slot
	Score     float64 `json:"score"`     // QPS / AvgQPS
	Bandwidth float64 `json:"bandwidth_bps"`
}

// Detector tracks per-slot QPS and identifies hot spots by comparing
// each slot against the cluster-wide average.
type Detector struct {
	slots [slotCount]slotStat

	ticker *time.Ticker
	done   chan struct{}
	wg     sync.WaitGroup

	mu           sync.RWMutex
	hotSlots     []HotSlotInfo
	lastTotalQPS float64

	ticks uint64
}

type slotStat struct {
	requests atomic.Uint64
	bytes    atomic.Uint64

	avgReq  float64
	avgByte float64
}

// Config holds detector configuration.
type Config struct {
	SampleInterval time.Duration
	AvgAlpha       float64
	HotMultiplier  float64
	MinHotQPS      float64
	MaxHotSlots    int
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		SampleInterval: sampleInterval,
		AvgAlpha:       avgAlpha,
		HotMultiplier:  hotMultiplier,
		MinHotQPS:      minHotQPS,
		MaxHotSlots:    maxHotSlots,
	}
}

// New creates and starts a Detector.
func New(cfg Config) *Detector {
	d := &Detector{
		ticker: time.NewTicker(cfg.SampleInterval),
		done:   make(chan struct{}),
	}
	d.wg.Add(1)
	go d.loop(cfg)
	return d
}

// Record registers a request against the given slot with its payload size.
// Hot-path: two atomic adds, lock-free.
func (d *Detector) Record(slot uint16, sizeBytes int) {
	if slot >= slotCount {
		return
	}
	d.slots[slot].requests.Add(1)
	if sizeBytes > 0 {
		d.slots[slot].bytes.Add(uint64(sizeBytes))
	}
}

// HotSlots returns the currently detected hot slots, sorted by score descending.
func (d *Detector) HotSlots() []HotSlotInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]HotSlotInfo, len(d.hotSlots))
	copy(out, d.hotSlots)
	return out
}

// TotalQPS returns the sum of all per-slot QPS in the last sample window.
func (d *Detector) TotalQPS() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastTotalQPS
}

// Stop shuts down the detector.
func (d *Detector) Stop() {
	d.ticker.Stop()
	close(d.done)
	d.wg.Wait()
}

func (d *Detector) loop(cfg Config) {
	defer d.wg.Done()

	for {
		select {
		case <-d.done:
			return
		case <-d.ticker.C:
			d.ticks++
			d.sampleAndDetect(cfg)
		}
	}
}

func (d *Detector) sampleAndDetect(cfg Config) {
	// First pass: collect per-slot QPS, update EWMAs, compute cluster average.
	var totalQPS float64
	activeSlots := 0
	slotQPS := make([]float64, slotCount)
	slotBPS := make([]float64, slotCount)

	for i := range slotCount {
		req := d.slots[i].requests.Swap(0)
		b := d.slots[i].bytes.Swap(0)
		if req == 0 {
			// Idle slot: decay EWMA and skip.
			d.slots[i].avgReq *= (1 - cfg.AvgAlpha)
			d.slots[i].avgByte *= (1 - cfg.AvgAlpha)
			continue
		}

		qps := float64(req)
		bps := float64(b)

		d.slots[i].avgReq = cfg.AvgAlpha*qps + (1-cfg.AvgAlpha)*d.slots[i].avgReq
		d.slots[i].avgByte = cfg.AvgAlpha*bps + (1-cfg.AvgAlpha)*d.slots[i].avgByte

		slotQPS[i] = qps
		slotBPS[i] = bps
		totalQPS += qps
		activeSlots++
	}

	// Skip emission during warmup.
	if d.ticks <= samplesWarmup {
		return
	}

	if activeSlots == 0 {
		d.mu.Lock()
		d.hotSlots = nil
		d.mu.Unlock()
		return
	}

	d.mu.Lock()
	d.lastTotalQPS = totalQPS
	d.mu.Unlock()

	avgQPS := totalQPS / float64(activeSlots)
	if avgQPS < cfg.MinHotQPS {
		d.mu.Lock()
		d.hotSlots = nil
		d.mu.Unlock()
		return
	}

	// Second pass: flag slots significantly above cluster average.
	var candidates []HotSlotInfo
	for i := range slotCount {
		if slotQPS[i] <= 0 {
			continue
		}
		if slotQPS[i] <= avgQPS*cfg.HotMultiplier {
			continue
		}
		candidates = append(candidates, HotSlotInfo{
			Slot:      uint16(i),
			QPS:       slotQPS[i],
			AvgQPS:    avgQPS,
			Score:     slotQPS[i] / avgQPS,
			Bandwidth: slotBPS[i],
		})
	}

	// Sort by score descending, cap.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})
	if len(candidates) > cfg.MaxHotSlots {
		candidates = candidates[:cfg.MaxHotSlots]
	}

	d.mu.Lock()
	d.hotSlots = candidates
	d.mu.Unlock()
}
