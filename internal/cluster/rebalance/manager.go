// Package rebalance monitors hot slots and automatically triggers slot migration
// to less-loaded peer nodes when hotspots persist across multiple sample windows.
package rebalance

import (
	"log"
	"sort"
	"sync"
	"time"

	"github.com/10yihang/autocache/internal/cluster/hotspot"
)

// ClusterOps is the subset of cluster operations needed for rebalancing.
type ClusterOps interface {
	GetNodeID() string
	GetSlotOwner(slot uint16) string
	IsSlotLocal(slot uint16) bool
	GetPeerLoads() []PeerLoad
	MigrateSlot(slot uint16, targetNodeID string) error
}

// PeerLoad represents a peer node's load snapshot.
type PeerLoad struct {
	NodeID  string
	Addr    string
	TotalQPS uint64
}

// Config holds rebalance configuration.
type Config struct {
	CheckInterval       time.Duration
	MinStickyWindows    int     // consecutive hot windows before triggering migration
	MinScoreRatio       float64 // hot slot score must be > MinScoreRatio to be considered
	MaxMigrationsPerRun int     // limit migrations triggered in a single check
	CoolDownSlots       int     // windows before a previously-migrated slot can be re-migrated
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		CheckInterval:       10 * time.Second,
		MinStickyWindows:    5,
		MinScoreRatio:       5.0,
		MaxMigrationsPerRun: 2,
		CoolDownSlots:       30,
	}
}

// Manager watches hot slots and triggers rebalancing.
type Manager struct {
	cluster  ClusterOps
	detector *hotspot.Detector
	cfg      Config

	// Sticky tracking: slot -> consecutive hot window count.
	sticky map[uint16]int
	// Cooldown: slot -> windows remaining before candidate again.
	cooldown map[uint16]int

	ticker *time.Ticker
	done   chan struct{}
	wg     sync.WaitGroup

	mu sync.Mutex
}

// New creates and starts a Manager.
func New(cluster ClusterOps, detector *hotspot.Detector, cfg Config) *Manager {
	m := &Manager{
		cluster:  cluster,
		detector: detector,
		cfg:      cfg,
		sticky:   make(map[uint16]int),
		cooldown: make(map[uint16]int),
		ticker:   time.NewTicker(cfg.CheckInterval),
		done:     make(chan struct{}),
	}
	m.wg.Add(1)
	go m.loop()
	return m
}

// Stop shuts down the rebalance manager.
func (m *Manager) Stop() {
	m.ticker.Stop()
	close(m.done)
	m.wg.Wait()
}

func (m *Manager) loop() {
	defer m.wg.Done()

	for {
		select {
		case <-m.done:
			return
		case <-m.ticker.C:
			m.tick()
		}
	}
}

func (m *Manager) tick() {
	hot := m.detector.HotSlots()
	found := make(map[uint16]bool)

	m.mu.Lock()
	// Update sticky counters.
	for _, h := range hot {
		if h.Slot == 0 {
			continue
		}
		if !m.cluster.IsSlotLocal(h.Slot) {
			continue
		}
		if m.cooldown[h.Slot] > 0 {
			m.cooldown[h.Slot]--
			continue
		}
		m.sticky[h.Slot]++
		found[h.Slot] = true
	}

	// Reset sticky for slots no longer hot.
	for slot := range m.sticky {
		if !found[slot] {
			delete(m.sticky, slot)
		}
	}

	// Find candidate slots: sticky count >= MinStickyWindows and score > threshold.
	type candidate struct {
		slot  uint16
		score float64
		qps   float64
	}
	var candidates []candidate
	for _, h := range hot {
		if m.sticky[h.Slot] < m.cfg.MinStickyWindows {
			continue
		}
		if h.Score < m.cfg.MinScoreRatio {
			continue
		}
		candidates = append(candidates, candidate{slot: h.Slot, score: h.Score, qps: h.QPS})
	}
	m.mu.Unlock()

	if len(candidates) == 0 {
		return
	}

	// Sort by score descending.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) > m.cfg.MaxMigrationsPerRun {
		candidates = candidates[:m.cfg.MaxMigrationsPerRun]
	}

	// Find target nodes.
	peers := m.cluster.GetPeerLoads()
	if len(peers) == 0 {
		return
	}

	// Sort peers by load ascending (lightest first).
	sort.Slice(peers, func(i, j int) bool {
		return peers[i].TotalQPS < peers[j].TotalQPS
	})

	for _, c := range candidates {
		if len(peers) == 0 {
			break
		}
		target := peers[0] // lightest node

		log.Printf("[rebalance] Migrating hot slot %d (score=%.1f qps=%.0f) to %s (qps=%d)",
			c.slot, c.score, c.qps, target.NodeID, target.TotalQPS)

		if err := m.cluster.MigrateSlot(c.slot, target.NodeID); err != nil {
			log.Printf("[rebalance] Slot %d migration failed: %v", c.slot, err)
			continue
		}

		// Place in cooldown and remove from sticky.
		m.mu.Lock()
		delete(m.sticky, c.slot)
		m.cooldown[c.slot] = m.cfg.CoolDownSlots
		m.mu.Unlock()

		log.Printf("[rebalance] Slot %d migrated successfully", c.slot)

		// Rotate target off the front so the next slot goes to a different node.
		peers = peers[1:]
	}
}
