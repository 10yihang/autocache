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
				got = p.ShouldDemote(tt.stats, tt.currentTier)
			} else {
				got = p.ShouldPromote(tt.stats, tt.currentTier)
			}

			if got != tt.want {
				t.Errorf("%s() = %v, want %v", tt.method, got, tt.want)
			}
		})
	}
}
