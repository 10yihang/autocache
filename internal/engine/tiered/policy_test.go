package tiered

import (
	"testing"
	"time"
)

func TestDefaultPolicy(t *testing.T) {
	p := NewDefaultPolicy()

	// Mock thresholds for testing
	p.HotAccessThreshold = 5
	p.HotIdleThreshold = 100 * time.Millisecond

	now := time.Now().UnixNano()

	tests := []struct {
		name        string
		stats       *AccessStats
		currentTier TierType
		method      string // "promote" or "demote"
		want        bool
	}{
		{
			name: "Hot -> Warm (Demote): Access low, Idle high",
			stats: &AccessStats{
				AccessCount:    2,
				LastAccessTime: now - int64(200*time.Millisecond),
			},
			currentTier: TierHot,
			method:      "demote",
			want:        true,
		},
		{
			name: "Hot -> Warm (Keep): Access high",
			stats: &AccessStats{
				AccessCount:    10,
				LastAccessTime: now - int64(200*time.Millisecond),
			},
			currentTier: TierHot,
			method:      "demote",
			want:        false,
		},
		{
			name: "Warm -> Hot (Promote): Access high",
			stats: &AccessStats{
				AccessCount: 10,
			},
			currentTier: TierWarm,
			method:      "promote",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bool
			if tt.method == "demote" {
				got = p.ShouldDemote("k", tt.stats, tt.currentTier)
			} else {
				got = p.ShouldPromote("k", tt.stats, tt.currentTier)
			}

			if got != tt.want {
				t.Errorf("%s() = %v, want %v", tt.method, got, tt.want)
			}
		})
	}
}

func TestWTinyLFUCostAwarePolicy_HotWarmPromotionAndDemotion(t *testing.T) {
	p := NewWTinyLFUCostAwarePolicy(WTinyLFUCostAwareConfig{
		HotCapacity:               100,
		HotIdleThreshold:          50 * time.Millisecond,
		WindowPercentage:          1,
		ProtectedPercentage:       80,
		WarmPromotionMinFrequency: 2,
	})

	p.RecordWrite("k1", TierHot)
	p.RecordWrite("k2", TierHot)
	p.RecordAccess("k1", TierHot)
	p.RecordAccess("k1", TierHot)
	p.RecordAccess("k2", TierHot)

	coldStats := &AccessStats{AccessCount: 1, LastAccessTime: time.Now().Add(-200 * time.Millisecond).UnixNano()}
	if !p.ShouldDemote("k2", coldStats, TierHot) {
		t.Fatal("expected W-TinyLFU to demote low-frequency idle hot key")
	}

	hotStats := &AccessStats{AccessCount: 3, LastAccessTime: time.Now().Add(-10 * time.Millisecond).UnixNano()}
	if p.ShouldDemote("k1", hotStats, TierHot) {
		t.Fatal("expected W-TinyLFU to keep high-frequency recent hot key")
	}

	warmStats := &AccessStats{AccessCount: 3, LastAccessTime: time.Now().UnixNano()}
	if !p.ShouldPromote("k1", warmStats, TierWarm) {
		t.Fatal("expected W-TinyLFU to promote frequent warm key")
	}
}

func TestWTinyLFUCostAwarePolicy_WarmColdCostAwareDemotion(t *testing.T) {
	p := NewWTinyLFUCostAwarePolicy(WTinyLFUCostAwareConfig{
		HotCapacity:               100,
		WindowPercentage:          1,
		ProtectedPercentage:       80,
		WarmPromotionMinFrequency: 2,
		WarmToColdDemoteThreshold: -0.2,
		CostAccessWeight:          1.0,
		CostRecencyWeight:         0.5,
		CostSizePenaltyWeight:     0.8,
		CostWritePenaltyWeight:    0.2,
	})

	coldCandidate := &AccessStats{
		AccessCount:    1,
		LastAccessTime: time.Now().Add(-5 * time.Hour).UnixNano(),
		WriteTime:      time.Now().Add(-5 * time.Hour).UnixNano(),
		Size:           4 * 1024,
	}
	hotCandidate := &AccessStats{
		AccessCount:    40,
		LastAccessTime: time.Now().Add(-20 * time.Second).UnixNano(),
		WriteTime:      time.Now().Add(-30 * time.Second).UnixNano(),
		Size:           128,
	}

	if !p.ShouldDemote("cold", coldCandidate, TierWarm) {
		t.Fatal("expected cost-aware policy to demote low-value warm key")
	}
	if p.ShouldDemote("hot", hotCandidate, TierWarm) {
		t.Fatal("expected cost-aware policy to keep high-value warm key")
	}
}
