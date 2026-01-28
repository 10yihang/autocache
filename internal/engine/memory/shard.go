package memory

import (
	"hash/maphash"
	"sync"
	"time"
)

const (
	// defaultShardedDictShards is the default number of shards
	defaultShardedDictShards = 256

	// defaultRingSize is the default ring buffer size per shard (16MB)
	defaultRingSize = 16 << 20

	// defaultLFUWidth is the default TinyLFU counter width
	defaultLFUWidth = 1 << 16
)

// ZeroGCShard is a single shard with zero-GC index.
// The index uses map[uint64]uint64 which only contains integers,
// eliminating GC scanning of string keys entirely.
type ZeroGCShard struct {
	mu    sync.RWMutex
	index map[uint64]uint64
	ring  *RingBuffer
	lfu   *TinyLFU
	_     [cacheLineSize - 56]byte
}

// ShardedCache is a high-performance cache with zero-GC overhead.
//
// Key design decisions:
// - Index uses map[uint64]uint64: hash → ring buffer offset
// - Keys stored in ring buffer for collision detection
// - TinyLFU for intelligent admission control
// - Per-shard ring buffers prevent global lock contention
type ShardedCache struct {
	shards     []*ZeroGCShard
	shardCount uint64
	seed       maphash.Seed
	sampleSize int
}

// ShardedCacheConfig configures the sharded cache.
type ShardedCacheConfig struct {
	ShardCount   int
	RingSize     int
	LFUWidth     int
	SampledEvict int
}

// DefaultShardedCacheConfig returns sensible defaults.
func DefaultShardedCacheConfig() ShardedCacheConfig {
	return ShardedCacheConfig{
		ShardCount:   defaultShardedDictShards,
		RingSize:     defaultRingSize,
		LFUWidth:     defaultLFUWidth,
		SampledEvict: 5,
	}
}

// NewShardedCache creates a new zero-GC sharded cache.
func NewShardedCache(cfg ShardedCacheConfig) *ShardedCache {
	if cfg.ShardCount <= 0 {
		cfg.ShardCount = defaultShardedDictShards
	}
	if cfg.RingSize <= 0 {
		cfg.RingSize = defaultRingSize
	}
	if cfg.LFUWidth <= 0 {
		cfg.LFUWidth = defaultLFUWidth
	}
	if cfg.SampledEvict <= 0 {
		cfg.SampledEvict = 5
	}

	sc := &ShardedCache{
		shards:     make([]*ZeroGCShard, cfg.ShardCount),
		shardCount: uint64(cfg.ShardCount),
		seed:       maphash.MakeSeed(),
		sampleSize: cfg.SampledEvict,
	}

	for i := 0; i < cfg.ShardCount; i++ {
		sc.shards[i] = &ZeroGCShard{
			index: make(map[uint64]uint64),
			ring:  NewRingBuffer(cfg.RingSize),
			lfu:   NewTinyLFU(cfg.LFUWidth / cfg.ShardCount),
		}
	}

	return sc
}

func (sc *ShardedCache) hash(key string) uint64 {
	return maphash.String(sc.seed, key)
}

func (sc *ShardedCache) getShard(hash uint64) *ZeroGCShard {
	return sc.shards[hash%sc.shardCount]
}

// Get retrieves a value by key. Returns nil, false if not found or expired.
// The returned []byte is a zero-copy view and should not be modified.
func (sc *ShardedCache) Get(key string) ([]byte, bool) {
	hash := sc.hash(key)
	shard := sc.getShard(hash)

	shard.mu.RLock()
	offset, ok := shard.index[hash]
	shard.mu.RUnlock()

	if !ok {
		return nil, false
	}

	storedKey, value, header, err := shard.ring.ReadAt(int64(offset))
	if err != nil {
		return nil, false
	}

	if BytesToString(storedKey) != key {
		return nil, false
	}

	if header.ExpireAt > 0 && time.Now().UnixNano() > header.ExpireAt {
		shard.mu.Lock()
		delete(shard.index, hash)
		shard.mu.Unlock()
		return nil, false
	}

	shard.lfu.RecordAccess(hash)

	return value, true
}

// GetCopy retrieves a value by key and returns a copy (safe for long-term storage).
func (sc *ShardedCache) GetCopy(key string) ([]byte, bool) {
	hash := sc.hash(key)
	shard := sc.getShard(hash)

	shard.mu.RLock()
	offset, ok := shard.index[hash]
	shard.mu.RUnlock()

	if !ok {
		return nil, false
	}

	storedKey, value, header, err := shard.ring.ReadAtCopy(int64(offset))
	if err != nil {
		return nil, false
	}

	if BytesToString(storedKey) != key {
		return nil, false
	}

	if header.ExpireAt > 0 && time.Now().UnixNano() > header.ExpireAt {
		shard.mu.Lock()
		delete(shard.index, hash)
		shard.mu.Unlock()
		return nil, false
	}

	shard.lfu.RecordAccess(hash)

	return value, true
}

// Set stores a key-value pair with optional TTL.
func (sc *ShardedCache) Set(key string, value []byte, ttl time.Duration) error {
	hash := sc.hash(key)
	shard := sc.getShard(hash)

	var expireAt int64
	if ttl > 0 {
		expireAt = time.Now().UnixNano() + int64(ttl)
	}

	keyBytes := StringToBytes(key)

	header := &EntryHeader{
		ExpireAt: expireAt,
		Hash64:   hash,
		KeyLen:   uint16(len(keyBytes)),
		ValLen:   uint16(len(value)),
		Flags:    EntryTypeString,
	}

	shard.mu.Lock()
	offset, evicted, err := shard.ring.WriteWithEvict(header, keyBytes, value)
	if err != nil {
		shard.mu.Unlock()
		return err
	}

	for _, ev := range evicted {
		if existing, ok := shard.index[ev.Hash]; ok && existing == uint64(ev.Offset) {
			delete(shard.index, ev.Hash)
		}
	}
	shard.index[hash] = uint64(offset)

	shard.mu.Unlock()

	shard.lfu.RecordAccess(hash)

	return nil
}

// SetNX stores a key-value pair only if the key does not exist.
// Returns true if the key was set.
func (sc *ShardedCache) SetNX(key string, value []byte, ttl time.Duration) (bool, error) {
	hash := sc.hash(key)
	shard := sc.getShard(hash)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if offset, ok := shard.index[hash]; ok {
		storedKey, _, header, err := shard.ring.ReadAt(int64(offset))
		if err == nil && BytesToString(storedKey) == key {
			if header.ExpireAt == 0 || time.Now().UnixNano() <= header.ExpireAt {
				return false, nil
			}
		}
	}

	var expireAt int64
	if ttl > 0 {
		expireAt = time.Now().UnixNano() + int64(ttl)
	}

	keyBytes := StringToBytes(key)
	header := &EntryHeader{
		ExpireAt: expireAt,
		Hash64:   hash,
		KeyLen:   uint16(len(keyBytes)),
		ValLen:   uint16(len(value)),
		Flags:    EntryTypeString,
	}

	offset, evicted, err := shard.ring.WriteWithEvict(header, keyBytes, value)
	if err != nil {
		return false, err
	}

	for _, ev := range evicted {
		if existing, ok := shard.index[ev.Hash]; ok && existing == uint64(ev.Offset) {
			delete(shard.index, ev.Hash)
		}
	}

	shard.index[hash] = uint64(offset)
	shard.lfu.RecordAccess(hash)
	return true, nil
}

// Delete removes a key from the cache.
// Returns true if the key existed.
func (sc *ShardedCache) Delete(key string) bool {
	hash := sc.hash(key)
	shard := sc.getShard(hash)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if _, ok := shard.index[hash]; ok {
		delete(shard.index, hash)
		return true
	}
	return false
}

func (sc *ShardedCache) SetXX(key string, value []byte, ttl time.Duration) (bool, error) {
	hash := sc.hash(key)
	shard := sc.getShard(hash)

	shard.mu.Lock()
	if offset, ok := shard.index[hash]; ok {
		storedKey, _, header, err := shard.ring.ReadAt(int64(offset))
		if err != nil || BytesToString(storedKey) != key {
			shard.mu.Unlock()
			return false, nil
		}
		if header.ExpireAt > 0 && time.Now().UnixNano() > header.ExpireAt {
			shard.mu.Unlock()
			return false, nil
		}
	} else {
		shard.mu.Unlock()
		return false, nil
	}

	var expireAt int64
	if ttl > 0 {
		expireAt = time.Now().UnixNano() + int64(ttl)
	}

	keyBytes := StringToBytes(key)
	header := &EntryHeader{
		ExpireAt: expireAt,
		Hash64:   hash,
		KeyLen:   uint16(len(keyBytes)),
		ValLen:   uint16(len(value)),
		Flags:    EntryTypeString,
	}

	offset, evicted, err := shard.ring.WriteWithEvict(header, keyBytes, value)
	if err != nil {
		shard.mu.Unlock()
		return false, err
	}

	for _, ev := range evicted {
		if existing, ok := shard.index[ev.Hash]; ok && existing == uint64(ev.Offset) {
			delete(shard.index, ev.Hash)
		}
	}

	shard.index[hash] = uint64(offset)
	shard.lfu.RecordAccess(hash)

	shard.mu.Unlock()
	return true, nil
}

func (sc *ShardedCache) Expire(key string, ttl time.Duration) bool {
	hash := sc.hash(key)
	shard := sc.getShard(hash)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	offset, ok := shard.index[hash]
	if !ok {
		return false
	}

	storedKey, _, header, err := shard.ring.ReadAt(int64(offset))
	if err != nil || BytesToString(storedKey) != key {
		return false
	}

	if header.ExpireAt > 0 && time.Now().UnixNano() > header.ExpireAt {
		return false
	}

	if ttl > 0 {
		header.ExpireAt = time.Now().UnixNano() + int64(ttl)
	} else {
		header.ExpireAt = 0
	}

	keyBytes := StringToBytes(key)
	value, ok := sc.valueForOffset(shard, int64(offset))
	if !ok {
		return false
	}
	header.KeyLen = uint16(len(keyBytes))
	header.ValLen = uint16(len(value))

	newOffset, evicted, err := shard.ring.WriteWithEvict(&header, keyBytes, value)
	if err != nil {
		return false
	}

	for _, ev := range evicted {
		if existing, ok := shard.index[ev.Hash]; ok && existing == uint64(ev.Offset) {
			delete(shard.index, ev.Hash)
		}
	}

	shard.index[hash] = uint64(newOffset)
	return true
}

func (sc *ShardedCache) TTL(key string) (time.Duration, bool) {
	hash := sc.hash(key)
	shard := sc.getShard(hash)

	shard.mu.RLock()
	offset, ok := shard.index[hash]
	shard.mu.RUnlock()

	if !ok {
		return -2 * time.Second, false
	}

	_, _, header, err := shard.ring.ReadAt(int64(offset))
	if err != nil {
		return -2 * time.Second, false
	}

	if header.ExpireAt == 0 {
		return -1, true
	}

	remaining := header.ExpireAt - time.Now().UnixNano()
	if remaining < 0 {
		return -2 * time.Second, false
	}

	return time.Duration(remaining), true
}

// Exists checks if a key exists and is not expired.
func (sc *ShardedCache) Exists(key string) bool {
	_, ok := sc.Get(key)
	return ok
}

// Len returns the total number of keys across all shards.
// This is an approximate count as it's not atomic across shards.
func (sc *ShardedCache) Len() int64 {
	var count int64
	for _, shard := range sc.shards {
		shard.mu.RLock()
		count += int64(len(shard.index))
		shard.mu.RUnlock()
	}
	return count
}

// Clear removes all entries from the cache.
func (sc *ShardedCache) Clear() {
	for _, shard := range sc.shards {
		shard.mu.Lock()
		shard.index = make(map[uint64]uint64)
		shard.ring.Reset()
		shard.lfu.Clear()
		shard.mu.Unlock()
	}
}

func (sc *ShardedCache) RandomKeys(n int) []string {
	if n <= 0 {
		return nil
	}

	result := make([]string, 0, n)
	for _, shard := range sc.shards {
		shard.mu.RLock()
		for _, offset := range shard.index {
			if len(result) >= n {
				shard.mu.RUnlock()
				return result
			}
			key, ok := sc.keyForOffset(shard, int64(offset))
			if !ok {
				continue
			}
			result = append(result, key)
		}
		shard.mu.RUnlock()
		if len(result) >= n {
			return result
		}
	}

	return result
}

func (sc *ShardedCache) Keys(pattern string) []string {
	if pattern == "*" {
		pattern = ""
	}

	result := make([]string, 0)
	for _, shard := range sc.shards {
		shard.mu.RLock()
		for _, offset := range shard.index {
			key, ok := sc.keyForOffset(shard, int64(offset))
			if !ok {
				continue
			}
			if pattern == "" || matchPattern(pattern, key) {
				result = append(result, key)
			}
		}
		shard.mu.RUnlock()
	}

	return result
}

// Stats returns cache statistics.
type CacheStats struct {
	KeyCount    int64
	TotalWrites int64
	TotalEvicts int64
}

// Stats returns current cache statistics.
func (sc *ShardedCache) Stats() CacheStats {
	var stats CacheStats
	for _, shard := range sc.shards {
		shard.mu.RLock()
		stats.KeyCount += int64(len(shard.index))
		writes, evicts := shard.ring.Stats()
		stats.TotalWrites += writes
		stats.TotalEvicts += evicts
		shard.mu.RUnlock()
	}
	return stats
}

// Frequency returns the TinyLFU frequency estimate for a key.
func (sc *ShardedCache) Frequency(key string) int {
	hash := sc.hash(key)
	shard := sc.getShard(hash)
	return shard.lfu.Frequency(hash)
}

func (sc *ShardedCache) SampledEvict(hash uint64) bool {
	shard := sc.getShard(hash)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	return sc.sampledEvictLocked(shard)
}

func (sc *ShardedCache) SampledEvictVolatile(hash uint64) bool {
	shard := sc.getShard(hash)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	return sc.sampledEvictLocked(shard, true)
}

func (sc *ShardedCache) valueForOffset(shard *ZeroGCShard, offset int64) ([]byte, bool) {
	_, value, _, err := shard.ring.ReadAtCopy(offset)
	if err != nil {
		return nil, false
	}
	return value, true
}

func (sc *ShardedCache) keyForOffset(shard *ZeroGCShard, offset int64) (string, bool) {
	key, _, header, err := shard.ring.ReadAt(offset)
	if err != nil {
		return "", false
	}
	if header.ExpireAt > 0 && time.Now().UnixNano() > header.ExpireAt {
		return "", false
	}
	return BytesToString(key), true
}

func (sc *ShardedCache) sampledEvictLocked(shard *ZeroGCShard, volatileOnly ...bool) bool {
	if len(shard.index) == 0 {
		return false
	}

	needVolatile := false
	if len(volatileOnly) > 0 && volatileOnly[0] {
		needVolatile = true
	}

	var victimHash uint64
	victimFreq := int(^uint(0) >> 1)
	count := 0
	hasCandidate := false

	for h, offset := range shard.index {
		storedKey, _, header, err := shard.ring.ReadAt(int64(offset))
		if err != nil {
			continue
		}
		if BytesToString(storedKey) == "" {
			continue
		}
		if header.ExpireAt > 0 && time.Now().UnixNano() > header.ExpireAt {
			delete(shard.index, h)
			continue
		}
		if needVolatile && header.ExpireAt == 0 {
			continue
		}
		freq := shard.lfu.Frequency(h)
		if !hasCandidate || freq < victimFreq {
			victimFreq = freq
			victimHash = h
			hasCandidate = true
		}
		count++
		if count >= sc.sampleSize {
			break
		}
	}

	if !hasCandidate {
		return false
	}

	delete(shard.index, victimHash)
	return true
}

func (sc *ShardedCache) Scan(cursor uint64, pattern string, count int) ([]string, uint64, error) {
	if count <= 0 {
		count = 10
	}

	shardIdx := int(cursor >> 32)
	posInShard := int(cursor & 0xFFFFFFFF)

	if pattern == "*" {
		pattern = ""
	}

	result := make([]string, 0, count)
	totalShards := len(sc.shards)

	for shardIdx < totalShards && len(result) < count {
		shard := sc.shards[shardIdx]
		shard.mu.RLock()

		keys := make([]string, 0, len(shard.index))
		for _, offset := range shard.index {
			key, ok := sc.keyForOffset(shard, int64(offset))
			if ok {
				keys = append(keys, key)
			}
		}
		shard.mu.RUnlock()

		for i := posInShard; i < len(keys) && len(result) < count; i++ {
			key := keys[i]
			if pattern == "" || matchPattern(pattern, key) {
				result = append(result, key)
			}
			posInShard = i + 1
		}

		if posInShard >= len(keys) {
			shardIdx++
			posInShard = 0
		}
	}

	var nextCursor uint64
	if shardIdx >= totalShards {
		nextCursor = 0
	} else {
		nextCursor = (uint64(shardIdx) << 32) | uint64(posInShard)
	}

	return result, nextCursor, nil
}
