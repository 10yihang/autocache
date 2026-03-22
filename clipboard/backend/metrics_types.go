package clipboard

type AdminStatsDTO struct {
	Tier       TierStatsDTO      `json:"tier"`
	Usage      UsageStatsDTO     `json:"usage"`
	RateLimits RateLimitStatsDTO `json:"rate_limits"`
}

type TierStatsDTO struct {
	HotTierKeys    int64 `json:"hot_tier_keys"`
	WarmTierKeys   int64 `json:"warm_tier_keys"`
	ColdTierKeys   int64 `json:"cold_tier_keys"`
	MigrationsUp   int64 `json:"migrations_up"`
	MigrationsDown int64 `json:"migrations_down"`
}

type UsageStatsDTO struct {
	PastesCreatedTotal     int64 `json:"pastes_created_total"`
	PastesReadTotal        int64 `json:"pastes_read_total"`
	PastesExpiredTotal     int64 `json:"pastes_expired_total"`
	PastesBurnedTotal      int64 `json:"pastes_burned_total"`
	PastesViewLimitedTotal int64 `json:"pastes_view_limited_total"`
}

type RateLimitStatsDTO struct {
	CreateRateLimitedTotal int64 `json:"create_rate_limited_total"`
	ReadRateLimitedTotal   int64 `json:"read_rate_limited_total"`
}
