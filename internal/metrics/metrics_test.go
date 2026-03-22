package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestV1MetricsRegistered(t *testing.T) {
	t.Parallel()

	RequestsTotal.WithLabelValues("GET", "success").Add(0)
	RequestDuration.WithLabelValues("GET").Observe(0)
	TieredBytes.WithLabelValues("hot").Set(0)
	TierCapacityBytes.WithLabelValues("hot").Set(0)
	TieredMigrationDuration.WithLabelValues("down", "success").Observe(0)
	TieredMigrationErrors.WithLabelValues("down", "storage_error").Add(0)

	wantMetrics := []string{
		"autocache_requests_total",
		"autocache_request_duration_seconds",
		"autocache_tiered_bytes",
		"autocache_tier_capacity_bytes",
		"autocache_tiered_migration_pending",
		"autocache_tiered_migration_in_flight",
		"autocache_tiered_migration_duration_seconds",
		"autocache_tiered_migration_errors_total",
	}

	got := gatherMetricNames(t)
	for _, want := range wantMetrics {
		if _, ok := got[want]; !ok {
			t.Fatalf("missing metric %q", want)
		}
	}
}

func gatherMetricNames(t *testing.T) map[string]struct{} {
	t.Helper()

	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	names := make(map[string]struct{}, len(mfs))
	for _, mf := range mfs {
		names[mf.GetName()] = struct{}{}
	}

	return names
}
