package tiered

import (
	"context"
	"os"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"

	"github.com/10yihang/autocache/internal/engine/memory"
	metrics2 "github.com/10yihang/autocache/internal/metrics"
)

func TestManager_UpdatesTierSizeAndCapacityMetrics(t *testing.T) {
	dir := t.TempDir()
	hotTier := memory.NewStore(memory.DefaultConfig())
	defer hotTier.Close()

	cfg := DefaultConfig()
	cfg.WarmTierEnabled = true
	cfg.ColdTierEnabled = false
	cfg.WarmTierPath = dir
	cfg.HotTierCapacity = 64
	cfg.WarmTierCapacity = 128

	manager, err := NewManager(cfg, hotTier)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer manager.Stop()

	manager.stats.RecordWrite("hot-key", 5)
	manager.stats.RecordWrite("warm-key", 7)
	manager.stats.UpdateTier("warm-key", TierWarm)
	manager.GetStats()

	if got := testutil.ToFloat64(metrics2.TieredBytes.WithLabelValues("hot")); got != 5 {
		t.Fatalf("hot tier bytes = %v, want 5", got)
	}
	if got := testutil.ToFloat64(metrics2.TieredBytes.WithLabelValues("warm")); got != 7 {
		t.Fatalf("warm tier bytes = %v, want 7", got)
	}
	if got := testutil.ToFloat64(metrics2.TierCapacityBytes.WithLabelValues("hot")); got != 64 {
		t.Fatalf("hot tier capacity = %v, want 64", got)
	}
	if got := testutil.ToFloat64(metrics2.TierCapacityBytes.WithLabelValues("warm")); got != 128 {
		t.Fatalf("warm tier capacity = %v, want 128", got)
	}
	if got := testutil.ToFloat64(metrics2.TierCapacityBytes.WithLabelValues("cold")); got != 0 {
		t.Fatalf("cold tier capacity = %v, want 0", got)
	}
}

func TestMigrator_RecordsMigrationDurationAndErrors(t *testing.T) {
	dir, err := os.MkdirTemp("", "tiered-metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	hotTier := memory.NewStore(memory.DefaultConfig())
	defer hotTier.Close()

	cfg := DefaultConfig()
	cfg.WarmTierEnabled = true
	cfg.ColdTierEnabled = false
	cfg.WarmTierPath = dir

	manager, err := NewManager(cfg, hotTier)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer manager.Stop()

	ctx := context.Background()
	if err := manager.Set(ctx, "ok-key", "value", 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	beforeSuccess := histogramSampleCount(t, "autocache_tiered_migration_duration_seconds", map[string]string{"direction": "down", "result": "success"})
	beforeErrors := testutil.ToFloat64(metrics2.TieredMigrationErrors.WithLabelValues("down", "not_found"))
	beforeFailures := histogramSampleCount(t, "autocache_tiered_migration_duration_seconds", map[string]string{"direction": "down", "result": "error"})

	if err := manager.migrator.demoteKey(ctx, "ok-key", TierHot, TierWarm); err != nil {
		t.Fatalf("demoteKey(ok-key) failed: %v", err)
	}
	if err := manager.migrator.demoteKey(ctx, "missing-key", TierHot, TierWarm); err == nil {
		t.Fatal("demoteKey(missing-key) error = nil, want error")
	}

	afterSuccess := histogramSampleCount(t, "autocache_tiered_migration_duration_seconds", map[string]string{"direction": "down", "result": "success"})
	afterErrors := testutil.ToFloat64(metrics2.TieredMigrationErrors.WithLabelValues("down", "not_found"))
	afterFailures := histogramSampleCount(t, "autocache_tiered_migration_duration_seconds", map[string]string{"direction": "down", "result": "error"})
	inFlight := testutil.ToFloat64(metrics2.TieredMigrationInFlight)

	if afterSuccess <= beforeSuccess {
		t.Fatalf("success migration histogram count = %d, want > %d", afterSuccess, beforeSuccess)
	}
	if afterErrors != beforeErrors+1 {
		t.Fatalf("migration error counter = %v, want %v", afterErrors, beforeErrors+1)
	}
	if afterFailures <= beforeFailures {
		t.Fatalf("failure migration histogram count = %d, want > %d", afterFailures, beforeFailures)
	}
	if inFlight != 0 {
		t.Fatalf("in-flight migrations = %v, want 0", inFlight)
	}
}

func histogramSampleCount(t *testing.T, metricName string, labels map[string]string) uint64 {
	t.Helper()

	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, metric := range mf.GetMetric() {
			if hasLabels(metric, labels) {
				return metric.GetHistogram().GetSampleCount()
			}
		}
	}
	return 0
}

func hasLabels(metric *dto.Metric, labels map[string]string) bool {
	for name, value := range labels {
		matched := false
		for _, label := range metric.GetLabel() {
			if label.GetName() == name && label.GetValue() == value {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
