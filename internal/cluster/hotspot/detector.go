// Package hotspot provides per-slot hotspot detection for the cluster.
// Uses EWMA + 3-sigma threshold to identify slots with abnormally high load.
package hotspot

import (
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	slotCount      = 16384
	sampleInterval = time.Second

	// EWMA decay factor — responds to changes within ~10 seconds.
	avgAlpha = 0.1
	// EWMA decay factor for variance — slower to stabilize.
	varBeta = 0.05
	// 3-sigma threshold multiplier.
	sigmaThreshold = 3.0
	// Absolute minimum QPS to flag a slot as hot (avoids noise in idle clusters).
	minHotQPS = 100
	// Maximum number of hot slots to report.
	maxHotSlots = 20
)

// HotSlotInfo describes a detected hot slot.
type HotSlotInfo struct {
	Slot       uint16  `json:"slot"`
	QPS        float64 `json:"qps"`
	AvgQPS     float64 `json:"avg_qps"`
	StdDev     float64 `json:"stddev"`
	Score      float64 `json:"score"` // sigmas above mean
	Bandwidth  float64 `json:"bandwidth_bps"`
}

// Detector tracks per-slot QPS and identifies hot spots using EWMA + sigma analysis.
type Detector struct {
	slots [slotCount]slotStat

	ticker  *time.Ticker
	done    chan struct{}
	wg      sync.WaitGroup

	mu       sync.RWMutex
	hotSlots []HotSlotInfo
}

type slotStat struct {
	requests atomic.Uint64
	bytes    atomic.Uint64

	avgReq  float64
	varReq  float64
	avgByte float64
	varByte float64
}

// Config holds detector configuration.
type Config struct {
	SampleInterval time.Duration
	AvgAlpha       float64
	VarBeta        float64
	SigmaThreshold float64
	MinHotQPS      float64
	MaxHotSlots    int
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		SampleInterval: sampleInterval,
		AvgAlpha:       avgAlpha,
		VarBeta:        varBeta,
		SigmaThreshold: sigmaThreshold,
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
// Hot-path: two atomic adds, lock-free, ~10 ns overhead.
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
			d.sampleAndDetect(cfg)
		}
	}
}

func (d *Detector) sampleAndDetect(cfg Config) {
	var candidates []HotSlotInfo

	for i := range slotCount {
		req := d.slots[i].requests.Swap(0)
		b := d.slots[i].bytes.Swap(0)
		if req == 0 {
			// Idle slot: apply decay without new input.
			d.slots[i].avgReq *= (1 - cfg.AvgAlpha)
			d.slots[i].varReq *= (1 - cfg.VarBeta)
			d.slots[i].avgByte *= (1 - cfg.AvgAlpha)
			d.slots[i].varByte *= (1 - cfg.VarBeta)
			continue
		}

		qps := float64(req)
		bps := float64(b)

		// Update QPS EWMA.
		oldAvg := d.slots[i].avgReq
		newAvg := cfg.AvgAlpha*qps + (1-cfg.AvgAlpha)*oldAvg
		d.slots[i].avgReq = newAvg

		// Update QPS variance EWMA.
		dev := qps - newAvg
		newVar := cfg.VarBeta*dev*dev + (1-cfg.VarBeta)*d.slots[i].varReq
		d.slots[i].varReq = newVar

		// Update byte rate EWMA.
		oldByteAvg := d.slots[i].avgByte
		newByteAvg := cfg.AvgAlpha*bps + (1-cfg.AvgAlpha)*oldByteAvg
		d.slots[i].avgByte = newByteAvg

		// Update byte variance EWMA.
		byteDev := bps - newByteAvg
		newByteVar := cfg.VarBeta*byteDev*byteDev + (1-cfg.VarBeta)*d.slots[i].varByte
		d.slots[i].varByte = newByteVar

		// Detect hotspot.
		stdDev := math.Sqrt(newVar)
		if newAvg < cfg.MinHotQPS {
			continue
		}
		if qps <= newAvg+cfg.SigmaThreshold*stdDev {
			continue
		}

		score := (qps - newAvg) / stdDev
		candidates = append(candidates, HotSlotInfo{
			Slot:      uint16(i),
			QPS:       qps,
			AvgQPS:    newAvg,
			StdDev:    stdDev,
			Score:     score,
			Bandwidth: bps,
		})
	}

	// Sort by score descending, keep top K.
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
