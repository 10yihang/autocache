package metrics

import (
	"runtime"
	"strings"
	"sync"
	"time"
)

var allowedCommands = map[string]struct{}{
	"append": {}, "asking": {}, "client": {}, "command": {}, "config": {}, "dbsize": {},
	"decr": {}, "decrby": {}, "debug": {}, "del": {}, "echo": {}, "exists": {},
	"expire": {}, "expireat": {}, "flushall": {}, "flushdb": {}, "get": {}, "getset": {},
	"hdel": {}, "hexists": {}, "hget": {}, "hgetall": {}, "hkeys": {}, "hlen": {},
	"hset": {}, "hvals": {}, "incr": {}, "incrby": {}, "info": {}, "keys": {},
	"lindex": {}, "llen": {}, "lpop": {}, "lpush": {}, "lrange": {}, "lset": {},
	"ltrim": {}, "mget": {}, "migrate": {}, "mset": {}, "persist": {}, "pexpire": {},
	"ping": {}, "psetex": {}, "pttl": {}, "quit": {}, "rename": {}, "replapply": {},
	"restore": {}, "rpop": {}, "rpush": {}, "sadd": {}, "scard": {}, "set": {},
	"setex": {}, "setnx": {}, "sismember": {}, "smembers": {}, "srem": {}, "strlen": {},
	"ttl": {}, "type": {}, "wait": {}, "zadd": {}, "zcard": {}, "zrange": {},
	"zrank": {}, "zrem": {}, "zscore": {},
}

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
	commandLabel := normalizeCommandLabel(cmd)

	CommandsTotal.WithLabelValues(cmd, status).Inc()
	CommandDuration.WithLabelValues(cmd).Observe(duration.Seconds())
	RequestsTotal.WithLabelValues(commandLabel, status).Inc()
	RequestDuration.WithLabelValues(commandLabel).Observe(duration.Seconds())
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

func SetTieredBytes(tier string, size int64) {
	TieredBytes.WithLabelValues(tier).Set(float64(size))
}

func SetTierCapacity(tier string, size int64) {
	TierCapacityBytes.WithLabelValues(tier).Set(float64(size))
}

func SetTieredMigrationPending(count int) {
	TieredMigrationPending.Set(float64(count))
}

func AddTieredMigrationInFlight(delta int) {
	TieredMigrationInFlight.Add(float64(delta))
}

func ObserveTieredMigrationDuration(direction string, duration time.Duration, success bool) {
	result := "success"
	if !success {
		result = "error"
	}
	TieredMigrationDuration.WithLabelValues(direction, result).Observe(duration.Seconds())
}

func RecordTieredMigrationError(direction, errorType string) {
	TieredMigrationErrors.WithLabelValues(direction, errorType).Inc()
}

func normalizeCommandLabel(cmd string) string {
	label := strings.ToLower(strings.TrimSpace(cmd))
	if _, ok := allowedCommands[label]; ok {
		return label
	}
	return "other"
}
