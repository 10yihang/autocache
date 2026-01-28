package memory

import (
	"sync"
	"sync/atomic"
)

const (
	cmSketchDepth        = 4
	cmSketchDefaultWidth = 1 << 16
	counterMax           = 15
	resetThreshold       = 1 << 20

	stripeLockCount = 16

	doorkeeperDefaultWidth = 1 << 16
	doorkeeperResetEvery   = 1 << 20
)

// CountMinSketch is a probabilistic frequency estimator using 4-bit counters.
// Uses striped locking for thread-safety with minimal contention.
type CountMinSketch struct {
	rows   [cmSketchDepth][]byte
	seeds  [cmSketchDepth]uint64
	mask   uint64
	stripe uint64

	locks     [stripeLockCount]sync.Mutex
	additions atomic.Int64
}

type Doorkeeper struct {
	bits       []uint64
	mask       uint64
	stripe     uint64
	locks      [stripeLockCount]sync.Mutex
	additions  atomic.Int64
	resetEvery int64
}

// NewCountMinSketch creates a new Count-Min Sketch with the given width.
func NewCountMinSketch(width int) *CountMinSketch {
	if width <= 0 {
		width = cmSketchDefaultWidth
	}

	width = nextPowerOf2(width)
	byteSize := width / 2

	cms := &CountMinSketch{
		mask:   uint64(width - 1),
		stripe: uint64(stripeLockCount - 1),
	}

	for i := 0; i < cmSketchDepth; i++ {
		cms.rows[i] = make([]byte, byteSize)
		cms.seeds[i] = uint64(i+1) * 0xc4ceb9fe1a85ec53
	}

	return cms
}

func NewDoorkeeper(width int) *Doorkeeper {
	if width <= 0 {
		width = doorkeeperDefaultWidth
	}

	width = nextPowerOf2(width)
	wordCount := width >> 6
	if wordCount == 0 {
		wordCount = 1
	}

	return &Doorkeeper{
		bits:       make([]uint64, wordCount),
		mask:       uint64(width - 1),
		stripe:     uint64(stripeLockCount - 1),
		resetEvery: doorkeeperResetEvery,
	}
}

// Increment increases the frequency counter for the given hash.
func (c *CountMinSketch) Increment(hash uint64) {
	lockIdx := hash & c.stripe
	c.locks[lockIdx].Lock()

	for i := 0; i < cmSketchDepth; i++ {
		pos := (hash ^ c.seeds[i]) & c.mask
		idx := pos >> 1
		shift := (pos & 1) << 2

		val := (c.rows[i][idx] >> shift) & 0x0F
		if val < counterMax {
			c.rows[i][idx] += 1 << shift
		}
	}

	c.locks[lockIdx].Unlock()

	if c.additions.Add(1) >= resetThreshold {
		c.reset()
	}
}

// Estimate returns the minimum count across all rows.
func (c *CountMinSketch) Estimate(hash uint64) int {
	lockIdx := hash & c.stripe
	c.locks[lockIdx].Lock()

	min := counterMax
	for i := 0; i < cmSketchDepth; i++ {
		pos := (hash ^ c.seeds[i]) & c.mask
		val := int((c.rows[i][pos>>1] >> ((pos & 1) << 2)) & 0x0F)
		if val < min {
			min = val
		}
	}

	c.locks[lockIdx].Unlock()
	return min
}

// reset halves all counters to prevent saturation.
func (c *CountMinSketch) reset() {
	for i := 0; i < stripeLockCount; i++ {
		c.locks[i].Lock()
	}

	for i := range c.rows {
		for j := range c.rows[i] {
			c.rows[i][j] = (c.rows[i][j] >> 1) & 0x77
		}
	}
	c.additions.Store(0)

	for i := stripeLockCount - 1; i >= 0; i-- {
		c.locks[i].Unlock()
	}
}

// Clear resets all counters to zero.
func (c *CountMinSketch) Clear() {
	for i := 0; i < stripeLockCount; i++ {
		c.locks[i].Lock()
	}

	for i := range c.rows {
		for j := range c.rows[i] {
			c.rows[i][j] = 0
		}
	}
	c.additions.Store(0)

	for i := stripeLockCount - 1; i >= 0; i-- {
		c.locks[i].Unlock()
	}
}

func (d *Doorkeeper) Allow(hash uint64) bool {
	pos1 := hash & d.mask
	pos2 := ((hash >> 17) ^ (hash * 0x9e3779b97f4a7c15)) & d.mask

	word1 := pos1 >> 6
	word2 := pos2 >> 6
	bit1 := uint64(1) << (pos1 & 63)
	bit2 := uint64(1) << (pos2 & 63)

	lock1 := word1 & d.stripe
	lock2 := word2 & d.stripe

	if lock1 == lock2 {
		d.locks[lock1].Lock()
		seen := (d.bits[word1]&bit1 != 0) && (d.bits[word2]&bit2 != 0)
		d.bits[word1] |= bit1
		d.bits[word2] |= bit2
		d.locks[lock1].Unlock()
		if d.additions.Add(1) >= d.resetEvery {
			d.reset()
		}
		return seen
	}

	if lock1 < lock2 {
		d.locks[lock1].Lock()
		d.locks[lock2].Lock()
	} else {
		d.locks[lock2].Lock()
		d.locks[lock1].Lock()
	}

	seen := (d.bits[word1]&bit1 != 0) && (d.bits[word2]&bit2 != 0)
	d.bits[word1] |= bit1
	d.bits[word2] |= bit2

	if lock1 < lock2 {
		d.locks[lock2].Unlock()
		d.locks[lock1].Unlock()
	} else {
		d.locks[lock1].Unlock()
		d.locks[lock2].Unlock()
	}

	if d.additions.Add(1) >= d.resetEvery {
		d.reset()
	}
	return seen
}

func (d *Doorkeeper) reset() {
	for i := 0; i < stripeLockCount; i++ {
		d.locks[i].Lock()
	}

	for i := range d.bits {
		d.bits[i] = 0
	}
	d.additions.Store(0)

	for i := stripeLockCount - 1; i >= 0; i-- {
		d.locks[i].Unlock()
	}
}

func (d *Doorkeeper) Clear() {
	d.reset()
}

// TinyLFU implements the TinyLFU admission policy.
type TinyLFU struct {
	sketch *CountMinSketch
	door   *Doorkeeper
}

// NewTinyLFU creates a new TinyLFU admission policy.
func NewTinyLFU(counterWidth int) *TinyLFU {
	return &TinyLFU{
		sketch: NewCountMinSketch(counterWidth),
		door:   NewDoorkeeper(counterWidth),
	}
}

// RecordAccess records an access to the given hash.
func (t *TinyLFU) RecordAccess(hash uint64) {
	if t.door.Allow(hash) {
		t.sketch.Increment(hash)
	}
}

func (t *TinyLFU) AllowAdmission(hash uint64) bool {
	return t.door.Allow(hash)
}

// Admit returns true if the candidate should be admitted over the victim.
func (t *TinyLFU) Admit(candidateHash, victimHash uint64) bool {
	candidateFreq := t.sketch.Estimate(candidateHash)
	victimFreq := t.sketch.Estimate(victimHash)
	return candidateFreq >= victimFreq
}

// Frequency returns the estimated frequency for a hash.
func (t *TinyLFU) Frequency(hash uint64) int {
	return t.sketch.Estimate(hash)
}

// Clear resets all frequency counters.
func (t *TinyLFU) Clear() {
	t.sketch.Clear()
	t.door.Clear()
}

// nextPowerOf2 returns the smallest power of 2 >= n.
func nextPowerOf2(n int) int {
	if n <= 1 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n |= n >> 32
	return n + 1
}
