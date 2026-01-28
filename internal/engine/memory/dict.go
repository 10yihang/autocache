package memory

import (
	"hash/maphash"
	"path"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultShardCount = 256
	cacheLineSize     = 64 // CPU cache line size for padding
)

// Entry 存储条目（使用 atomic 避免数据竞争）
// Performance: atomic 字段可在 RLock 后安全更新，避免写锁竞争
type Entry struct {
	Value     interface{}
	expireAt  atomic.Int64 // Unix nanoseconds, 0 means never expires
	CreatedAt int64        // Immutable after creation
	UpdatedAt int64

	// LRU/LFU stats (atomic for lock-free updates)
	lastAccess atomic.Int64  // Last access time
	accessFreq atomic.Uint32 // Access frequency (LFU)
}

// IsExpired checks if entry has expired
func (e *Entry) IsExpired() bool {
	expireAt := e.expireAt.Load()
	if expireAt == 0 {
		return false
	}
	return time.Now().UnixNano() > expireAt
}

// SetExpireAt sets expiration time atomically
func (e *Entry) SetExpireAt(t int64) {
	e.expireAt.Store(t)
}

// GetExpireAt returns expiration time
func (e *Entry) GetExpireAt() int64 {
	return e.expireAt.Load()
}

// UpdateAccessStats updates access statistics atomically (lock-free)
func (e *Entry) UpdateAccessStats() {
	e.lastAccess.Store(time.Now().UnixNano())
	e.accessFreq.Add(1)
}

// GetLastAccess returns last access time
func (e *Entry) GetLastAccess() int64 {
	return e.lastAccess.Load()
}

// GetAccessFreq returns access frequency
func (e *Entry) GetAccessFreq() uint32 {
	return e.accessFreq.Load()
}

// Shard represents a cache partition with cache line padding
// Performance: Padding prevents false sharing between shards on different CPU cores
type Shard struct {
	mu    sync.RWMutex
	items map[string]*Entry
	_     [cacheLineSize - 32]byte // Padding: RWMutex(24) + map pointer(8) = 32 bytes
}

// Dict is a high-performance sharded dictionary
// Performance optimizations:
// - maphash: 4.4x faster than FNV, zero allocations
// - Cache line padding: prevents false sharing
// - Atomic stats: lock-free access pattern updates
type Dict struct {
	shards     []*Shard
	shardCount uint32       // uint32 for faster modulo
	seed       maphash.Seed // maphash seed (zero-allocation hash)
}

// NewDict creates a new sharded dictionary
func NewDict(shardCount int) *Dict {
	if shardCount <= 0 {
		shardCount = defaultShardCount
	}

	d := &Dict{
		shards:     make([]*Shard, shardCount),
		shardCount: uint32(shardCount),
		seed:       maphash.MakeSeed(), // Initialize hash seed
	}

	for i := 0; i < shardCount; i++ {
		d.shards[i] = &Shard{
			items: make(map[string]*Entry),
		}
	}

	return d
}

// getShard returns the shard for a given key
// Performance: maphash.String is zero-allocation, ~4.4x faster than FNV
func (d *Dict) getShard(key string) *Shard {
	hash := maphash.String(d.seed, key)
	return d.shards[hash%uint64(d.shardCount)]
}

// Get retrieves an entry by key
// Performance: Uses atomic operations to update stats without write lock
func (d *Dict) Get(key string) (*Entry, bool) {
	shard := d.getShard(key)
	shard.mu.RLock()
	entry, ok := shard.items[key]
	shard.mu.RUnlock()

	if !ok {
		return nil, false
	}

	// Lazy deletion: check if expired
	if entry.IsExpired() {
		d.Del(key)
		return nil, false
	}

	// Lock-free stats update (atomic operations)
	entry.UpdateAccessStats()

	return entry, true
}

// Set stores a key-value pair with optional TTL
func (d *Dict) Set(key string, value interface{}, ttl time.Duration) {
	shard := d.getShard(key)
	now := time.Now().UnixNano()

	var expireAtVal int64
	if ttl > 0 {
		expireAtVal = now + int64(ttl)
	}

	entry := &Entry{
		Value:     value,
		CreatedAt: now,
		UpdatedAt: now,
	}
	entry.expireAt.Store(expireAtVal)
	entry.lastAccess.Store(now)
	entry.accessFreq.Store(1)

	shard.mu.Lock()
	shard.items[key] = entry
	shard.mu.Unlock()
}

// SetNX sets value only if key does not exist (atomic operation)
// Returns true if key was set, false if key already exists
func (d *Dict) SetNX(key string, value interface{}, ttl time.Duration) bool {
	shard := d.getShard(key)
	now := time.Now().UnixNano()

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if entry, ok := shard.items[key]; ok && !entry.IsExpired() {
		return false
	}

	var expireAtVal int64
	if ttl > 0 {
		expireAtVal = now + int64(ttl)
	}

	entry := &Entry{
		Value:     value,
		CreatedAt: now,
		UpdatedAt: now,
	}
	entry.expireAt.Store(expireAtVal)
	entry.lastAccess.Store(now)
	entry.accessFreq.Store(1)

	shard.items[key] = entry
	return true
}

// SetXX sets value only if key exists (atomic operation)
// Returns true if key was set, false if key does not exist
func (d *Dict) SetXX(key string, value interface{}, ttl time.Duration) bool {
	shard := d.getShard(key)
	now := time.Now().UnixNano()

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if entry, ok := shard.items[key]; !ok || entry.IsExpired() {
		return false
	}

	var expireAtVal int64
	if ttl > 0 {
		expireAtVal = now + int64(ttl)
	}

	entry := &Entry{
		Value:     value,
		CreatedAt: now,
		UpdatedAt: now,
	}
	entry.expireAt.Store(expireAtVal)
	entry.lastAccess.Store(now)
	entry.accessFreq.Store(1)

	shard.items[key] = entry
	return true
}

// SetEntry stores an entry directly
func (d *Dict) SetEntry(key string, entry *Entry) {
	shard := d.getShard(key)
	shard.mu.Lock()
	shard.items[key] = entry
	shard.mu.Unlock()
}

// Del deletes one or more keys
func (d *Dict) Del(keys ...string) int64 {
	var count int64
	for _, key := range keys {
		shard := d.getShard(key)
		shard.mu.Lock()
		if _, ok := shard.items[key]; ok {
			delete(shard.items, key)
			count++
		}
		shard.mu.Unlock()
	}
	return count
}

// Exists checks if keys exist
func (d *Dict) Exists(keys ...string) int64 {
	var count int64
	for _, key := range keys {
		if _, ok := d.Get(key); ok {
			count++
		}
	}
	return count
}

// Keys returns keys matching pattern
func (d *Dict) Keys(pattern string) []string {
	var result []string

	for _, shard := range d.shards {
		shard.mu.RLock()
		for key, entry := range shard.items {
			if !entry.IsExpired() && matchPattern(pattern, key) {
				result = append(result, key)
			}
		}
		shard.mu.RUnlock()
	}

	return result
}

// Len returns the total number of keys
func (d *Dict) Len() int64 {
	var count int64
	for _, shard := range d.shards {
		shard.mu.RLock()
		count += int64(len(shard.items))
		shard.mu.RUnlock()
	}
	return count
}

// Expire sets TTL on a key
func (d *Dict) Expire(key string, ttl time.Duration) bool {
	shard := d.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	entry, ok := shard.items[key]
	if !ok || entry.IsExpired() {
		return false
	}

	if ttl > 0 {
		entry.SetExpireAt(time.Now().UnixNano() + int64(ttl))
	} else {
		entry.SetExpireAt(0)
	}

	return true
}

// TTL returns remaining TTL for a key
func (d *Dict) TTL(key string) (time.Duration, bool) {
	entry, ok := d.Get(key)
	if !ok {
		return -2, false
	}

	expireAt := entry.GetExpireAt()
	if expireAt == 0 {
		return -1, true
	}

	remaining := expireAt - time.Now().UnixNano()
	if remaining < 0 {
		return -2, false
	}

	return time.Duration(remaining), true
}

// ForEach iterates over all entries
func (d *Dict) ForEach(fn func(key string, entry *Entry) bool) {
	for _, shard := range d.shards {
		shard.mu.RLock()
		for key, entry := range shard.items {
			if !fn(key, entry) {
				shard.mu.RUnlock()
				return
			}
		}
		shard.mu.RUnlock()
	}
}

// RandomKeys returns up to n random keys
func (d *Dict) RandomKeys(n int) []string {
	result := make([]string, 0, n)

	for _, shard := range d.shards {
		shard.mu.RLock()
		for key := range shard.items {
			result = append(result, key)
			if len(result) >= n {
				shard.mu.RUnlock()
				return result
			}
		}
		shard.mu.RUnlock()
	}

	return result
}

// Clear removes all entries
func (d *Dict) Clear() {
	for _, shard := range d.shards {
		shard.mu.Lock()
		shard.items = make(map[string]*Entry)
		shard.mu.Unlock()
	}
}

// matchPattern performs glob pattern matching using path.Match
func matchPattern(pattern, key string) bool {
	if pattern == "*" {
		return true
	}
	matched, _ := path.Match(pattern, key)
	return matched
}
