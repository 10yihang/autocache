package memory

import (
	"runtime"
	"sync"
	"time"
)

type EvictPolicy string

const (
	PolicyNoEviction    EvictPolicy = "noeviction"
	PolicyAllKeysLRU    EvictPolicy = "allkeys-lru"
	PolicyAllKeysLFU    EvictPolicy = "allkeys-lfu"
	PolicyVolatileLRU   EvictPolicy = "volatile-lru"
	PolicyVolatileLFU   EvictPolicy = "volatile-lfu"
	PolicyAllKeysRandom EvictPolicy = "allkeys-random"
	PolicyVolatileTTL   EvictPolicy = "volatile-ttl"
)

const evictSampleCount = 5

type Evictor struct {
	cache     *ShardedCache
	maxMemory int64
	policy    EvictPolicy
	mu        sync.Mutex
}

func NewEvictor(cache *ShardedCache, maxMemory int64, policy string) *Evictor {
	return &Evictor{
		cache:     cache,
		maxMemory: maxMemory,
		policy:    EvictPolicy(policy),
	}
}

func (e *Evictor) TryEvict() bool {
	if e.maxMemory <= 0 {
		return false
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	if int64(m.Alloc) < e.maxMemory {
		return false
	}

	return e.evict()
}

func (e *Evictor) evict() bool {
	switch e.policy {
	case PolicyAllKeysLRU:
		return e.evictLRU(false)
	case PolicyVolatileLRU:
		return e.evictLRU(true)
	case PolicyAllKeysLFU:
		return e.evictLFU(false)
	case PolicyVolatileLFU:
		return e.evictLFU(true)
	case PolicyAllKeysRandom:
		return e.evictRandom(false)
	case PolicyVolatileTTL:
		return e.evictTTL()
	default:
		return false
	}
}

func (e *Evictor) evictLRU(volatileOnly bool) bool {
	if volatileOnly {
		return e.cache.SampledEvictVolatile(uint64(time.Now().UnixNano()))
	}
	return e.cache.SampledEvict(uint64(time.Now().UnixNano()))
}

func (e *Evictor) evictLFU(volatileOnly bool) bool {
	if volatileOnly {
		return e.cache.SampledEvictVolatile(uint64(time.Now().UnixNano()))
	}
	return e.cache.SampledEvict(uint64(time.Now().UnixNano()))
}

func (e *Evictor) evictRandom(volatileOnly bool) bool {
	keys := e.cache.RandomKeys(1)
	if len(keys) == 0 {
		return false
	}

	if volatileOnly {
		ttl, ok := e.cache.TTL(keys[0])
		if !ok || ttl == -1 {
			return false
		}
	}

	e.cache.Delete(keys[0])
	return true
}

func (e *Evictor) evictTTL() bool {
	keys := e.cache.RandomKeys(evictSampleCount)
	if len(keys) == 0 {
		return false
	}

	var bestKey string
	var bestExpire int64 = ^int64(0) >> 1

	for _, key := range keys {
		ttl, ok := e.cache.TTL(key)
		if !ok || ttl == -1 {
			continue
		}

		expireAt := time.Now().UnixNano() + int64(ttl)
		if expireAt < bestExpire {
			bestExpire = expireAt
			bestKey = key
		}
	}

	if bestKey != "" {
		e.cache.Delete(bestKey)
		return true
	}

	return false
}
