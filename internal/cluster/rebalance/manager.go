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

type hotSlotSource interface {
	HotSlots() []hotspot.HotSlotInfo
	TotalQPS() float64
}

// PeerLoad represents a peer node's load snapshot.
type PeerLoad struct {
	NodeID   string
	Addr     string
	TotalQPS uint64
}

// Config holds rebalance configuration.
type Config struct {
	CheckInterval                time.Duration
	MinStickyWindows             int     // consecutive hot windows before triggering migration
	MinScoreRatio                float64 // hot slot score must be > MinScoreRatio to be considered
	MaxMigrationsPerRun          int     // limit migrations triggered in a single check
	CoolDownSlots                int     // windows before a previously-migrated slot can be re-migrated
	MaxTargetProjectedLoadRatio  float64 // projected target load must stay below local load times this ratio
	MinProjectedLoadReductionQPS float64 // projected local load reduction needed before migration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		CheckInterval:                10 * time.Second,
		MinStickyWindows:             5,
		MinScoreRatio:                5.0,
		MaxMigrationsPerRun:          2,
		CoolDownSlots:                30,
		MaxTargetProjectedLoadRatio:  1.0,
		MinProjectedLoadReductionQPS: 1,
	}
}

// Manager watches hot slots and triggers rebalancing.
type Manager struct {
	cluster  ClusterOps
	detector hotSlotSource
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
func New(cluster ClusterOps, detector hotSlotSource, cfg Config) *Manager {
	cfg = normalizeConfig(cfg)
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

func normalizeConfig(cfg Config) Config {
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 10 * time.Second
	}
	if cfg.MinStickyWindows <= 0 {
		cfg.MinStickyWindows = 1
	}
	if cfg.MinScoreRatio <= 0 {
		cfg.MinScoreRatio = 1
	}
	if cfg.MaxMigrationsPerRun <= 0 {
		cfg.MaxMigrationsPerRun = 1
	}
	if cfg.MaxTargetProjectedLoadRatio <= 0 {
		cfg.MaxTargetProjectedLoadRatio = 1
	}
	if cfg.MinProjectedLoadReductionQPS <= 0 {
		cfg.MinProjectedLoadReductionQPS = 1
	}
	return cfg
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
	if m.detector == nil {
		return
	}
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
	var candidates []migrationCandidate
	for _, h := range hot {
		if m.sticky[h.Slot] < m.cfg.MinStickyWindows {
			continue
		}
		if h.Score < m.cfg.MinScoreRatio {
			continue
		}
		candidates = append(candidates, migrationCandidate{slot: h.Slot, score: h.Score, qps: h.QPS})
	}
	m.mu.Unlock()

	if len(candidates) == 0 {
		return
	}

	plans := m.planMigrations(candidates)
	for _, plan := range plans {
		target := plan.target

		log.Printf("[rebalance] Migrating hot slot %d (score=%.1f qps=%.0f) to %s (qps=%d)",
			plan.slot, plan.score, plan.qps, target.NodeID, target.TotalQPS)

		if err := m.cluster.MigrateSlot(plan.slot, target.NodeID); err != nil {
			log.Printf("[rebalance] Slot %d migration failed: %v", plan.slot, err)
			continue
		}

		// Place in cooldown and remove from sticky.
		m.mu.Lock()
		delete(m.sticky, plan.slot)
		m.cooldown[plan.slot] = m.cfg.CoolDownSlots
		m.mu.Unlock()

		log.Printf("[rebalance] Slot %d migrated successfully", plan.slot)
	}
}

type migrationCandidate struct {
	slot  uint16
	score float64
	qps   float64
}

type migrationPlan struct {
	slot   uint16
	score  float64
	qps    float64
	target PeerLoad
}

func (m *Manager) planMigrations(candidates []migrationCandidate) []migrationPlan {
	peers := m.cluster.GetPeerLoads()
	if len(candidates) == 0 || len(peers) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) > m.cfg.MaxMigrationsPerRun {
		candidates = candidates[:m.cfg.MaxMigrationsPerRun]
	}

	localProjected := m.detector.TotalQPS()
	if localProjected <= 0 {
		return nil
	}

	plans := make([]migrationPlan, 0, len(candidates))
	for _, c := range candidates {
		sort.Slice(peers, func(i, j int) bool {
			if peers[i].TotalQPS == peers[j].TotalQPS {
				return peers[i].NodeID < peers[j].NodeID
			}
			return peers[i].TotalQPS < peers[j].TotalQPS
		})

		targetIndex := -1
		for i, peer := range peers {
			projectedTarget := float64(peer.TotalQPS) + c.qps
			projectedLocal := localProjected - c.qps
			if projectedLocal < 0 {
				projectedLocal = 0
			}
			if projectedTarget > localProjected*m.cfg.MaxTargetProjectedLoadRatio {
				continue
			}
			if localProjected-projectedLocal < m.cfg.MinProjectedLoadReductionQPS {
				continue
			}
			targetIndex = i
			break
		}
		if targetIndex == -1 {
			continue
		}

		target := peers[targetIndex]
		plans = append(plans, migrationPlan{
			slot:   c.slot,
			score:  c.score,
			qps:    c.qps,
			target: target,
		})

		localProjected -= c.qps
		if localProjected < 0 {
			localProjected = 0
		}
		peers[targetIndex].TotalQPS += uint64(c.qps)
	}
	return plans
}
