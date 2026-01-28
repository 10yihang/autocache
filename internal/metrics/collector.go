package metrics

import (
	"runtime"
	"sync"
	"time"
)

// Collector collects custom metrics
type Collector struct {
	startTime time.Time
	mu        sync.RWMutex
}

// NewCollector creates a collector
func NewCollector() *Collector {
	return &Collector{
		startTime: time.Now(),
	}
}

// Collect collects periodic metrics
func (c *Collector) Collect() {
	c.collectMemory()
	c.collectUptime()
}

func (c *Collector) collectMemory() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	MemoryUsage.WithLabelValues("alloc").Set(float64(m.Alloc))
	MemoryUsage.WithLabelValues("sys").Set(float64(m.Sys))
	MemoryUsage.WithLabelValues("heap_alloc").Set(float64(m.HeapAlloc))
	MemoryUsage.WithLabelValues("heap_sys").Set(float64(m.HeapSys))
	MemoryUsage.WithLabelValues("heap_inuse").Set(float64(m.HeapInuse))
}

func (c *Collector) collectUptime() {
	Uptime.Set(time.Since(c.startTime).Seconds())
}

// RecordCommand records command execution
func RecordCommand(cmd string, duration time.Duration, success bool) {
	status := "success"
	if !success {
		status = "error"
	}

	CommandsTotal.WithLabelValues(cmd, status).Inc()
	CommandDuration.WithLabelValues(cmd).Observe(duration.Seconds())
}

// RecordCacheHit records cache hit
func RecordCacheHit() {
	CacheHits.Inc()
}

// RecordCacheMiss records cache miss
func RecordCacheMiss() {
	CacheMisses.Inc()
}

// RecordConnection records connection count change
func RecordConnection(delta int) {
	ConnectionsTotal.Add(float64(delta))
}

// RecordTieredMigration records migration
func RecordTieredMigration(direction string) {
	TieredMigrations.WithLabelValues(direction).Inc()
}
