package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "autocache"
)

var (
	// CommandsTotal counts total commands
	CommandsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "commands_total",
			Help:      "Total number of commands processed",
		},
		[]string{"cmd", "status"}, // cmd: get/set/del, status: success/error
	)

	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "requests_total",
			Help:      "Total number of requests processed",
		},
		[]string{"command", "result"},
	)

	// CommandDuration measures command latency
	CommandDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "command_duration_seconds",
			Help:      "Command latency in seconds",
			Buckets:   []float64{.0001, .0005, .001, .005, .01, .05, .1, .5, 1},
		},
		[]string{"cmd"},
	)

	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "request_duration_seconds",
			Help:      "Request latency in seconds",
			Buckets:   []float64{.0001, .0005, .001, .005, .01, .05, .1, .5, 1},
		},
		[]string{"command"},
	)

	// CacheHits counts cache hits
	CacheHits = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_hits_total",
			Help:      "Total number of cache hits",
		},
	)

	// CacheMisses counts cache misses
	CacheMisses = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_misses_total",
			Help:      "Total number of cache misses",
		},
	)

	// MemoryUsage tracks memory usage
	MemoryUsage = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "memory_bytes",
			Help:      "Memory usage in bytes",
		},
		[]string{"type"}, // used/rss/peak
	)

	// KeysTotal tracks total keys
	KeysTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "keys_total",
			Help:      "Total number of keys",
		},
	)

	// ConnectionsTotal tracks active connections
	ConnectionsTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "connections_total",
			Help:      "Total number of client connections",
		},
	)

	// TieredKeys tracks keys per tier
	TieredKeys = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "tiered_keys_total",
			Help:      "Number of keys in each tier",
		},
		[]string{"tier"}, // hot/warm/cold
	)

	TieredBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "tiered_bytes",
			Help:      "Number of bytes stored in each tier",
		},
		[]string{"tier"},
	)

	TierCapacityBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "tier_capacity_bytes",
			Help:      "Configured capacity in bytes for each tier",
		},
		[]string{"tier"},
	)

	// TieredMigrations tracks migrations
	TieredMigrations = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "tiered_migrations_total",
			Help:      "Total number of tier migrations",
		},
		[]string{"direction"}, // promote/demote
	)

	TieredMigrationPending = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "tiered_migration_pending",
			Help:      "Number of keys currently pending migration",
		},
	)

	TieredMigrationInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "tiered_migration_in_flight",
			Help:      "Number of migrations currently in flight",
		},
	)

	TieredMigrationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "tiered_migration_duration_seconds",
			Help:      "Duration of tier migrations in seconds",
			Buckets:   []float64{.0005, .001, .005, .01, .05, .1, .25, .5, 1, 5},
		},
		[]string{"direction", "result"},
	)

	TieredMigrationErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "tiered_migration_errors_total",
			Help:      "Total number of tier migration errors",
		},
		[]string{"direction", "error_type"},
	)

	// Info exposes build info
	Info = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "info",
			Help:      "AutoCache server info",
		},
		[]string{"version", "go_version", "os", "arch"},
	)

	// Uptime tracks uptime
	Uptime = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "uptime_seconds",
			Help:      "Server uptime in seconds",
		},
	)
)

// InitInfo initializes info metric
func InitInfo(version, goVersion, os, arch string) {
	Info.WithLabelValues(version, goVersion, os, arch).Set(1)
}
