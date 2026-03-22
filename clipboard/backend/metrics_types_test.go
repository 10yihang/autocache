package clipboard

import (
	"encoding/json"
	"testing"
)

func TestAdminStatsDTO(t *testing.T) {
	t.Parallel()

	dto := AdminStatsDTO{
		Tier: TierStatsDTO{
			HotTierKeys:    12,
			WarmTierKeys:   5,
			ColdTierKeys:   1,
			MigrationsUp:   2,
			MigrationsDown: 3,
		},
		Usage: UsageStatsDTO{
			PastesCreatedTotal:     20,
			PastesReadTotal:        50,
			PastesExpiredTotal:     4,
			PastesBurnedTotal:      2,
			PastesViewLimitedTotal: 3,
		},
		RateLimits: RateLimitStatsDTO{
			CreateRateLimitedTotal: 7,
			ReadRateLimitedTotal:   9,
		},
	}

	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	got := map[string]any{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	tier := got["tier"].(map[string]any)
	usage := got["usage"].(map[string]any)
	rateLimits := got["rate_limits"].(map[string]any)

	if tier["hot_tier_keys"].(float64) != 12 {
		t.Fatalf("hot_tier_keys = %v, want 12", tier["hot_tier_keys"])
	}
	if tier["warm_tier_keys"].(float64) != 5 {
		t.Fatalf("warm_tier_keys = %v, want 5", tier["warm_tier_keys"])
	}
	if tier["cold_tier_keys"].(float64) != 1 {
		t.Fatalf("cold_tier_keys = %v, want 1", tier["cold_tier_keys"])
	}
	if usage["pastes_created_total"].(float64) != 20 {
		t.Fatalf("pastes_created_total = %v, want 20", usage["pastes_created_total"])
	}
	if rateLimits["read_rate_limited_total"].(float64) != 9 {
		t.Fatalf("read_rate_limited_total = %v, want 9", rateLimits["read_rate_limited_total"])
	}
}

func TestAdminStatsDTO_ZeroState(t *testing.T) {
	t.Parallel()

	dto := AdminStatsDTO{}
	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	got := map[string]any{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if _, ok := got["tier"]; !ok {
		t.Fatalf("zero-state payload missing tier field: %s", data)
	}
	if _, ok := got["usage"]; !ok {
		t.Fatalf("zero-state payload missing usage field: %s", data)
	}
	if _, ok := got["rate_limits"]; !ok {
		t.Fatalf("zero-state payload missing rate_limits field: %s", data)
	}
}
