# Phase 1: 单节点 KV 引擎

## 目标

- 实现完整的单节点 KV 存储引擎
- 完全兼容 Redis RESP 协议，支持 redis-cli 和 redis-benchmark
- 实现核心数据结构：String、Hash、List、Set、ZSet
- 实现 TTL 过期机制和 LRU/LFU 淘汰策略
- 实现 RDB 和 AOF 持久化
- **性能目标：超越 Redis 基准性能**
  - GET QPS: > 200,000 ops/sec
  - SET QPS: > 150,000 ops/sec
  - P99 延迟: < 0.5ms
  - GC 暂停: < 10ms (10M keys)

## 时间：2026.01.06 - 2026.02.09（5 周）

---

## 架构设计

```
┌─────────────────────────────────────────────────────────────┐
│                      Protocol Layer                         │
│  ┌─────────────────────────────────────────────────────┐   │
│  │              RESP Server (redcon)                    │   │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐             │   │
│  │  │  PING   │  │   GET   │  │   SET   │  ...        │   │
│  │  └─────────┘  └─────────┘  └─────────┘             │   │
│  └─────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                      Engine Layer                            │
│  ┌─────────────────┐  ┌─────────────────┐                  │
│  │   Dict (KV)     │  │   Expiry Mgr    │                  │
│  │  ┌───────────┐  │  │  ┌───────────┐  │                  │
│  │  │ Shard 0   │  │  │  │ Lazy Del  │  │                  │
│  │  │ Shard 1   │  │  │  │ Active Del│  │                  │
│  │  │ ...       │  │  │  └───────────┘  │                  │
│  │  │ Shard N   │  │  │                 │                  │
│  │  └───────────┘  │  └─────────────────┘                  │
│  └─────────────────┘                                        │
│  ┌─────────────────┐  ┌─────────────────┐                  │
│  │  Eviction       │  │  Persistence    │                  │
│  │  ┌───────────┐  │  │  ┌───────────┐  │                  │
│  │  │   LRU     │  │  │  │    RDB    │  │                  │
│  │  │   LFU     │  │  │  │    AOF    │  │                  │
│  │  └───────────┘  │  │  └───────────┘  │                  │
│  └─────────────────┘  └─────────────────┘                  │
└─────────────────────────────────────────────────────────────┘
```

---

## Week 1: 核心存储结构

### 1.1 Engine 接口定义

#### internal/engine/engine.go
```go
package engine

import (
	"context"
	"time"
)

// ValueType 表示值的类型
type ValueType int

const (
	TypeString ValueType = iota
	TypeList
	TypeSet
	TypeZSet
	TypeHash
)

// Entry 表示一个键值对
type Entry struct {
	Key       string
	Value     interface{}
	Type      ValueType
	ExpireAt  time.Time // 零值表示永不过期
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Engine 是存储引擎的核心接口
type Engine interface {
	// 基础操作
	Get(ctx context.Context, key string) (*Entry, error)
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) (int64, error)
	Exists(ctx context.Context, keys ...string) (int64, error)
	
	// Key 操作
	Keys(ctx context.Context, pattern string) ([]string, error)
	Type(ctx context.Context, key string) (string, error)
	Rename(ctx context.Context, key, newkey string) error
	
	// TTL 操作
	Expire(ctx context.Context, key string, ttl time.Duration) (bool, error)
	ExpireAt(ctx context.Context, key string, t time.Time) (bool, error)
	TTL(ctx context.Context, key string) (time.Duration, error)
	Persist(ctx context.Context, key string) (bool, error)
	
	// 数据库操作
	DBSize(ctx context.Context) (int64, error)
	FlushDB(ctx context.Context) error
	
	// 生命周期
	Close() error
}

// StringEngine String 类型操作接口
type StringEngine interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)
	SetXX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)
	GetSet(ctx context.Context, key string, value string) (string, error)
	Incr(ctx context.Context, key string) (int64, error)
	IncrBy(ctx context.Context, key string, delta int64) (int64, error)
	IncrByFloat(ctx context.Context, key string, delta float64) (float64, error)
	Decr(ctx context.Context, key string) (int64, error)
	DecrBy(ctx context.Context, key string, delta int64) (int64, error)
	Append(ctx context.Context, key string, value string) (int64, error)
	Strlen(ctx context.Context, key string) (int64, error)
	MGet(ctx context.Context, keys ...string) ([]interface{}, error)
	MSet(ctx context.Context, pairs ...interface{}) error
}

// HashEngine Hash 类型操作接口
type HashEngine interface {
	HGet(ctx context.Context, key, field string) (string, error)
	HSet(ctx context.Context, key string, pairs ...interface{}) (int64, error)
	HDel(ctx context.Context, key string, fields ...string) (int64, error)
	HExists(ctx context.Context, key, field string) (bool, error)
	HGetAll(ctx context.Context, key string) (map[string]string, error)
	HKeys(ctx context.Context, key string) ([]string, error)
	HVals(ctx context.Context, key string) ([]string, error)
	HLen(ctx context.Context, key string) (int64, error)
	HIncrBy(ctx context.Context, key, field string, delta int64) (int64, error)
	HIncrByFloat(ctx context.Context, key, field string, delta float64) (float64, error)
}

// ListEngine List 类型操作接口
type ListEngine interface {
	LPush(ctx context.Context, key string, values ...interface{}) (int64, error)
	RPush(ctx context.Context, key string, values ...interface{}) (int64, error)
	LPop(ctx context.Context, key string) (string, error)
	RPop(ctx context.Context, key string) (string, error)
	LRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	LLen(ctx context.Context, key string) (int64, error)
	LIndex(ctx context.Context, key string, index int64) (string, error)
	LSet(ctx context.Context, key string, index int64, value string) error
	LTrim(ctx context.Context, key string, start, stop int64) error
}

// SetEngine Set 类型操作接口
type SetEngine interface {
	SAdd(ctx context.Context, key string, members ...interface{}) (int64, error)
	SRem(ctx context.Context, key string, members ...interface{}) (int64, error)
	SMembers(ctx context.Context, key string) ([]string, error)
	SIsMember(ctx context.Context, key, member string) (bool, error)
	SCard(ctx context.Context, key string) (int64, error)
	SPop(ctx context.Context, key string) (string, error)
	SRandMember(ctx context.Context, key string, count int64) ([]string, error)
	SUnion(ctx context.Context, keys ...string) ([]string, error)
	SInter(ctx context.Context, keys ...string) ([]string, error)
	SDiff(ctx context.Context, keys ...string) ([]string, error)
}

// ZSetEngine ZSet 类型操作接口
type ZSetEngine interface {
	ZAdd(ctx context.Context, key string, members ...ZMember) (int64, error)
	ZRem(ctx context.Context, key string, members ...string) (int64, error)
	ZRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	ZRangeWithScores(ctx context.Context, key string, start, stop int64) ([]ZMember, error)
	ZRevRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	ZScore(ctx context.Context, key, member string) (float64, error)
	ZRank(ctx context.Context, key, member string) (int64, error)
	ZCard(ctx context.Context, key string) (int64, error)
	ZCount(ctx context.Context, key string, min, max float64) (int64, error)
	ZIncrBy(ctx context.Context, key, member string, delta float64) (float64, error)
}

// ZMember ZSet 成员
type ZMember struct {
	Score  float64
	Member string
}
```

### 1.2 分片哈希表实现（高性能优化版）

> **性能优化说明**：
> - 使用 `hash/maphash` 替代 `hash/fnv`（4.4x 更快，零分配）
> - 使用 `sync/atomic` 进行无锁统计更新
> - 添加缓存行 padding 防止伪共享
> - 参考: bigcache, ristretto, freecache

#### internal/engine/memory/dict.go
```go
package memory

import (
	"hash/maphash"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultShardCount = 256
	cacheLineSize     = 64 // CPU 缓存行大小
)

// Entry 存储条目（使用 atomic 避免数据竞争）
type Entry struct {
	Value     interface{}
	expireAt  atomic.Int64 // Unix 纳秒时间戳，0 表示永不过期
	CreatedAt int64        // 创建后不变，无需 atomic
	UpdatedAt int64

	// LRU/LFU 相关（atomic 实现无锁更新）
	lastAccess atomic.Int64  // 最后访问时间
	accessFreq atomic.Uint32 // 访问频率（LFU）
}

// IsExpired 检查是否过期
func (e *Entry) IsExpired() bool {
	expireAt := e.expireAt.Load()
	if expireAt == 0 {
		return false
	}
	return time.Now().UnixNano() > expireAt
}

// SetExpireAt 设置过期时间
func (e *Entry) SetExpireAt(t int64) {
	e.expireAt.Store(t)
}

// GetExpireAt 获取过期时间
func (e *Entry) GetExpireAt() int64 {
	return e.expireAt.Load()
}

// UpdateAccessStats 无锁更新访问统计
func (e *Entry) UpdateAccessStats() {
	e.lastAccess.Store(time.Now().UnixNano())
	e.accessFreq.Add(1)
}

// GetLastAccess 获取最后访问时间
func (e *Entry) GetLastAccess() int64 {
	return e.lastAccess.Load()
}

// GetAccessFreq 获取访问频率
func (e *Entry) GetAccessFreq() uint32 {
	return e.accessFreq.Load()
}

// Shard 单个分片（带缓存行 padding 防止伪共享）
type Shard struct {
	mu    sync.RWMutex
	items map[string]*Entry
	_     [cacheLineSize - 32]byte // Padding: RWMutex(24) + map pointer(8) = 32 bytes
}

// Dict 分片字典（高性能版本）
type Dict struct {
	shards     []*Shard
	shardCount uint32       // 改为 uint32 避免类型转换
	seed       maphash.Seed // maphash 种子（零分配哈希）
}

// NewDict 创建分片字典
func NewDict(shardCount int) *Dict {
	if shardCount <= 0 {
		shardCount = defaultShardCount
	}

	d := &Dict{
		shards:     make([]*Shard, shardCount),
		shardCount: uint32(shardCount),
		seed:       maphash.MakeSeed(), // 初始化哈希种子
	}

	for i := 0; i < shardCount; i++ {
		d.shards[i] = &Shard{
			items: make(map[string]*Entry),
		}
	}

	return d
}

// getShard 根据 key 获取对应的分片（零分配，4.4x 更快）
func (d *Dict) getShard(key string) *Shard {
	// maphash.String: 零分配，使用 Go runtime 内置哈希
	hash := maphash.String(d.seed, key)
	return d.shards[hash%uint64(d.shardCount)]
}

// Get 获取值（使用 atomic 避免数据竞争）
func (d *Dict) Get(key string) (*Entry, bool) {
	shard := d.getShard(key)
	shard.mu.RLock()
	entry, ok := shard.items[key]
	shard.mu.RUnlock()

	if !ok {
		return nil, false
	}

	// 惰性删除：检查是否过期
	if entry.IsExpired() {
		d.Del(key)
		return nil, false
	}

	// 无锁更新访问统计（atomic 操作）
	entry.UpdateAccessStats()

	return entry, true
}

// Set 设置值
func (d *Dict) Set(key string, value interface{}, ttl time.Duration) {
	shard := d.getShard(key)
	now := time.Now().UnixNano()
	
	var expireAt int64
	if ttl > 0 {
		expireAt = now + int64(ttl)
	}
	
	entry := &Entry{
		Value:      value,
		ExpireAt:   expireAt,
		CreatedAt:  now,
		UpdatedAt:  now,
		LastAccess: now,
		AccessFreq: 1,
	}
	
	shard.mu.Lock()
	shard.items[key] = entry
	shard.mu.Unlock()
}

// SetEntry 设置完整的 Entry
func (d *Dict) SetEntry(key string, entry *Entry) {
	shard := d.getShard(key)
	shard.mu.Lock()
	shard.items[key] = entry
	shard.mu.Unlock()
}

// Del 删除 key
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

// Exists 检查 key 是否存在
func (d *Dict) Exists(keys ...string) int64 {
	var count int64
	for _, key := range keys {
		if _, ok := d.Get(key); ok {
			count++
		}
	}
	return count
}

// Keys 获取匹配 pattern 的所有 key
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

// Len 获取总 key 数量
func (d *Dict) Len() int64 {
	var count int64
	for _, shard := range d.shards {
		shard.mu.RLock()
		count += int64(len(shard.items))
		shard.mu.RUnlock()
	}
	return count
}

// Expire 设置过期时间
func (d *Dict) Expire(key string, ttl time.Duration) bool {
	shard := d.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	
	entry, ok := shard.items[key]
	if !ok || entry.IsExpired() {
		return false
	}
	
	if ttl > 0 {
		entry.ExpireAt = time.Now().UnixNano() + int64(ttl)
	} else {
		entry.ExpireAt = 0
	}
	
	return true
}

// TTL 获取剩余过期时间
func (d *Dict) TTL(key string) (time.Duration, bool) {
	entry, ok := d.Get(key)
	if !ok {
		return -2, false // key 不存在
	}
	
	if entry.ExpireAt == 0 {
		return -1, true // 永不过期
	}
	
	remaining := entry.ExpireAt - time.Now().UnixNano()
	if remaining < 0 {
		return -2, false
	}
	
	return time.Duration(remaining), true
}

// ForEach 遍历所有 key
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

// RandomKeys 随机获取 n 个 key（用于过期扫描）
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

// Clear 清空所有数据
func (d *Dict) Clear() {
	for _, shard := range d.shards {
		shard.mu.Lock()
		shard.items = make(map[string]*Entry)
		shard.mu.Unlock()
	}
}

// matchPattern 简单的通配符匹配（支持 * 和 ?）
func matchPattern(pattern, key string) bool {
	if pattern == "*" {
		return true
	}
	// TODO: 实现完整的 glob 匹配
	return pattern == key
}
```

### 1.3 Store 核心实现

#### internal/engine/memory/store.go
```go
package memory

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/10yihang/autocache/internal/engine"
)

// Store 内存存储引擎
type Store struct {
	dict    *Dict
	expires *ExpiryManager
	evictor *Evictor
	
	// 配置
	maxMemory     int64
	evictPolicy   string
	
	// 统计
	stats *Stats
	
	mu sync.RWMutex
}

// Stats 统计信息
type Stats struct {
	Hits       int64
	Misses     int64
	SetOps     int64
	GetOps     int64
	DelOps     int64
	ExpiredKeys int64
	EvictedKeys int64
}

// Config 存储配置
type Config struct {
	ShardCount  int
	MaxMemory   int64  // 最大内存（字节）
	EvictPolicy string // noeviction, allkeys-lru, allkeys-lfu, volatile-lru, volatile-lfu
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	return &Config{
		ShardCount:  256,
		MaxMemory:   0, // 不限制
		EvictPolicy: "noeviction",
	}
}

// NewStore 创建存储引擎
func NewStore(cfg *Config) *Store {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	
	s := &Store{
		dict:        NewDict(cfg.ShardCount),
		maxMemory:   cfg.MaxMemory,
		evictPolicy: cfg.EvictPolicy,
		stats:       &Stats{},
	}
	
	// 初始化过期管理器
	s.expires = NewExpiryManager(s.dict)
	s.expires.Start()
	
	// 初始化淘汰器
	if cfg.MaxMemory > 0 && cfg.EvictPolicy != "noeviction" {
		s.evictor = NewEvictor(s.dict, cfg.MaxMemory, cfg.EvictPolicy)
	}
	
	return s
}

// =============== String 操作 ===============

// Get 获取 string 值
func (s *Store) Get(ctx context.Context, key string) (string, error) {
	s.stats.GetOps++
	
	entry, ok := s.dict.Get(key)
	if !ok {
		s.stats.Misses++
		return "", engine.ErrKeyNotFound
	}
	
	// 类型检查
	str, ok := entry.Value.(string)
	if !ok {
		return "", engine.ErrWrongType
	}
	
	s.stats.Hits++
	return str, nil
}

// Set 设置 string 值
func (s *Store) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	s.stats.SetOps++
	
	// 检查是否需要淘汰
	if s.evictor != nil {
		s.evictor.TryEvict()
	}
	
	s.dict.Set(key, value, ttl)
	return nil
}

// SetNX 仅当 key 不存在时设置
func (s *Store) SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	if _, ok := s.dict.Get(key); ok {
		return false, nil
	}
	
	s.dict.Set(key, value, ttl)
	return true, nil
}

// SetXX 仅当 key 存在时设置
func (s *Store) SetXX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	if _, ok := s.dict.Get(key); !ok {
		return false, nil
	}
	
	s.dict.Set(key, value, ttl)
	return true, nil
}

// GetSet 设置新值并返回旧值
func (s *Store) GetSet(ctx context.Context, key string, value string) (string, error) {
	old, _ := s.Get(ctx, key)
	s.Set(ctx, key, value, 0)
	return old, nil
}

// Incr 自增 1
func (s *Store) Incr(ctx context.Context, key string) (int64, error) {
	return s.IncrBy(ctx, key, 1)
}

// IncrBy 自增指定值
func (s *Store) IncrBy(ctx context.Context, key string, delta int64) (int64, error) {
	entry, ok := s.dict.Get(key)
	
	var val int64
	if ok {
		str, ok := entry.Value.(string)
		if !ok {
			return 0, engine.ErrWrongType
		}
		var err error
		val, err = strconv.ParseInt(str, 10, 64)
		if err != nil {
			return 0, engine.ErrNotInteger
		}
	}
	
	val += delta
	s.dict.Set(key, strconv.FormatInt(val, 10), 0)
	return val, nil
}

// Decr 自减 1
func (s *Store) Decr(ctx context.Context, key string) (int64, error) {
	return s.IncrBy(ctx, key, -1)
}

// DecrBy 自减指定值
func (s *Store) DecrBy(ctx context.Context, key string, delta int64) (int64, error) {
	return s.IncrBy(ctx, key, -delta)
}

// Append 追加字符串
func (s *Store) Append(ctx context.Context, key string, value string) (int64, error) {
	entry, ok := s.dict.Get(key)
	
	var str string
	if ok {
		str, ok = entry.Value.(string)
		if !ok {
			return 0, engine.ErrWrongType
		}
	}
	
	str += value
	s.dict.Set(key, str, 0)
	return int64(len(str)), nil
}

// Strlen 获取字符串长度
func (s *Store) Strlen(ctx context.Context, key string) (int64, error) {
	entry, ok := s.dict.Get(key)
	if !ok {
		return 0, nil
	}
	
	str, ok := entry.Value.(string)
	if !ok {
		return 0, engine.ErrWrongType
	}
	
	return int64(len(str)), nil
}

// MGet 批量获取
func (s *Store) MGet(ctx context.Context, keys ...string) ([]interface{}, error) {
	result := make([]interface{}, len(keys))
	for i, key := range keys {
		val, err := s.Get(ctx, key)
		if err == nil {
			result[i] = val
		} else {
			result[i] = nil
		}
	}
	return result, nil
}

// MSet 批量设置
func (s *Store) MSet(ctx context.Context, pairs ...interface{}) error {
	if len(pairs)%2 != 0 {
		return fmt.Errorf("wrong number of arguments")
	}
	
	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			return fmt.Errorf("invalid key type")
		}
		value := fmt.Sprint(pairs[i+1])
		s.dict.Set(key, value, 0)
	}
	return nil
}

// =============== Key 操作 ===============

// Del 删除 key
func (s *Store) Del(ctx context.Context, keys ...string) (int64, error) {
	s.stats.DelOps++
	return s.dict.Del(keys...), nil
}

// Exists 检查 key 是否存在
func (s *Store) Exists(ctx context.Context, keys ...string) (int64, error) {
	return s.dict.Exists(keys...), nil
}

// Keys 获取匹配的 key
func (s *Store) Keys(ctx context.Context, pattern string) ([]string, error) {
	return s.dict.Keys(pattern), nil
}

// Type 获取 key 类型
func (s *Store) Type(ctx context.Context, key string) (string, error) {
	entry, ok := s.dict.Get(key)
	if !ok {
		return "none", nil
	}
	
	switch entry.Value.(type) {
	case string:
		return "string", nil
	case map[string]string:
		return "hash", nil
	case []string:
		return "list", nil
	case map[string]struct{}:
		return "set", nil
	default:
		return "unknown", nil
	}
}

// Rename 重命名 key
func (s *Store) Rename(ctx context.Context, key, newkey string) error {
	entry, ok := s.dict.Get(key)
	if !ok {
		return engine.ErrKeyNotFound
	}
	
	s.dict.Del(key)
	s.dict.SetEntry(newkey, entry)
	return nil
}

// =============== TTL 操作 ===============

// Expire 设置过期时间（秒）
func (s *Store) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return s.dict.Expire(key, ttl), nil
}

// ExpireAt 设置过期时间点
func (s *Store) ExpireAt(ctx context.Context, key string, t time.Time) (bool, error) {
	ttl := time.Until(t)
	if ttl < 0 {
		s.dict.Del(key)
		return true, nil
	}
	return s.dict.Expire(key, ttl), nil
}

// TTL 获取剩余过期时间
func (s *Store) TTL(ctx context.Context, key string) (time.Duration, error) {
	ttl, ok := s.dict.TTL(key)
	if !ok {
		return -2 * time.Second, nil
	}
	return ttl, nil
}

// Persist 移除过期时间
func (s *Store) Persist(ctx context.Context, key string) (bool, error) {
	return s.dict.Expire(key, 0), nil
}

// =============== 数据库操作 ===============

// DBSize 获取 key 数量
func (s *Store) DBSize(ctx context.Context) (int64, error) {
	return s.dict.Len(), nil
}

// FlushDB 清空数据库
func (s *Store) FlushDB(ctx context.Context) error {
	s.dict.Clear()
	return nil
}

// =============== 统计 ===============

// GetStats 获取统计信息
func (s *Store) GetStats() *Stats {
	return s.stats
}

// =============== 生命周期 ===============

// Close 关闭存储引擎
func (s *Store) Close() error {
	s.expires.Stop()
	return nil
}
```

---

## Week 2: 过期机制与淘汰策略

### 2.1 过期管理器

#### internal/engine/memory/expiry.go
```go
package memory

import (
	"sync"
	"time"
)

const (
	// 定期删除配置
	expireScanInterval = 100 * time.Millisecond // 扫描间隔
	expireScanCount    = 20                     // 每次扫描的 key 数量
	expireThreshold    = 0.25                   // 如果过期比例超过 25%，继续扫描
)

// ExpiryManager 过期管理器
type ExpiryManager struct {
	dict   *Dict
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewExpiryManager 创建过期管理器
func NewExpiryManager(dict *Dict) *ExpiryManager {
	return &ExpiryManager{
		dict:   dict,
		stopCh: make(chan struct{}),
	}
}

// Start 启动定期删除
func (m *ExpiryManager) Start() {
	m.wg.Add(1)
	go m.activeExpireLoop()
}

// Stop 停止
func (m *ExpiryManager) Stop() {
	close(m.stopCh)
	m.wg.Wait()
}

// activeExpireLoop 定期删除循环
func (m *ExpiryManager) activeExpireLoop() {
	defer m.wg.Done()
	
	ticker := time.NewTicker(expireScanInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.activeExpireCycle()
		}
	}
}

// activeExpireCycle 单次过期扫描
func (m *ExpiryManager) activeExpireCycle() {
	for {
		// 随机采样
		keys := m.dict.RandomKeys(expireScanCount)
		if len(keys) == 0 {
			return
		}
		
		expired := 0
		for _, key := range keys {
			entry, ok := m.dict.Get(key)
			if !ok {
				continue
			}
			if entry.IsExpired() {
				m.dict.Del(key)
				expired++
			}
		}
		
		// 如果过期比例低于阈值，停止扫描
		if float64(expired)/float64(len(keys)) < expireThreshold {
			return
		}
	}
}
```

### 2.2 LRU/LFU 淘汰策略

#### internal/engine/memory/eviction.go
```go
package memory

import (
	"container/heap"
	"runtime"
	"sync"
	"time"
)

// EvictPolicy 淘汰策略
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

const (
	evictSampleCount = 5 // 采样数量（类似 Redis）
)

// Evictor 淘汰器
type Evictor struct {
	dict      *Dict
	maxMemory int64
	policy    EvictPolicy
	mu        sync.Mutex
}

// NewEvictor 创建淘汰器
func NewEvictor(dict *Dict, maxMemory int64, policy string) *Evictor {
	return &Evictor{
		dict:      dict,
		maxMemory: maxMemory,
		policy:    EvictPolicy(policy),
	}
}

// TryEvict 尝试淘汰
func (e *Evictor) TryEvict() bool {
	if e.maxMemory <= 0 {
		return false
	}
	
	e.mu.Lock()
	defer e.mu.Unlock()
	
	// 获取当前内存使用
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	if int64(m.Alloc) < e.maxMemory {
		return false
	}
	
	// 执行淘汰
	return e.evict()
}

// evict 执行淘汰
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

// evictLRU LRU 淘汰
func (e *Evictor) evictLRU(volatileOnly bool) bool {
	keys := e.dict.RandomKeys(evictSampleCount)
	if len(keys) == 0 {
		return false
	}
	
	var bestKey string
	var bestTime int64 = time.Now().UnixNano()
	
	for _, key := range keys {
		entry, ok := e.dict.Get(key)
		if !ok {
			continue
		}
		
		// volatile-lru 只淘汰有过期时间的 key
		if volatileOnly && entry.ExpireAt == 0 {
			continue
		}
		
		if entry.LastAccess < bestTime {
			bestTime = entry.LastAccess
			bestKey = key
		}
	}
	
	if bestKey != "" {
		e.dict.Del(bestKey)
		return true
	}
	
	return false
}

// evictLFU LFU 淘汰
func (e *Evictor) evictLFU(volatileOnly bool) bool {
	keys := e.dict.RandomKeys(evictSampleCount)
	if len(keys) == 0 {
		return false
	}
	
	var bestKey string
	var bestFreq uint32 = ^uint32(0) // 最大值
	
	for _, key := range keys {
		entry, ok := e.dict.Get(key)
		if !ok {
			continue
		}
		
		if volatileOnly && entry.ExpireAt == 0 {
			continue
		}
		
		if entry.AccessFreq < bestFreq {
			bestFreq = entry.AccessFreq
			bestKey = key
		}
	}
	
	if bestKey != "" {
		e.dict.Del(bestKey)
		return true
	}
	
	return false
}

// evictRandom 随机淘汰
func (e *Evictor) evictRandom(volatileOnly bool) bool {
	keys := e.dict.RandomKeys(1)
	if len(keys) == 0 {
		return false
	}
	
	if volatileOnly {
		entry, ok := e.dict.Get(keys[0])
		if !ok || entry.ExpireAt == 0 {
			return false
		}
	}
	
	e.dict.Del(keys[0])
	return true
}

// evictTTL 淘汰最快过期的 key
func (e *Evictor) evictTTL() bool {
	keys := e.dict.RandomKeys(evictSampleCount)
	if len(keys) == 0 {
		return false
	}
	
	var bestKey string
	var bestExpire int64 = ^int64(0) >> 1 // 最大值
	
	for _, key := range keys {
		entry, ok := e.dict.Get(key)
		if !ok || entry.ExpireAt == 0 {
			continue
		}
		
		if entry.ExpireAt < bestExpire {
			bestExpire = entry.ExpireAt
			bestKey = key
		}
	}
	
	if bestKey != "" {
		e.dict.Del(bestKey)
		return true
	}
	
	return false
}

// =============== W-TinyLFU 实现（可选高级功能） ===============

// CountMinSketch Count-Min Sketch 用于频率估计
type CountMinSketch struct {
	matrix [][]uint8
	width  int
	depth  int
}

// NewCountMinSketch 创建 CMS
func NewCountMinSketch(width, depth int) *CountMinSketch {
	matrix := make([][]uint8, depth)
	for i := range matrix {
		matrix[i] = make([]uint8, width)
	}
	return &CountMinSketch{
		matrix: matrix,
		width:  width,
		depth:  depth,
	}
}

// Increment 增加计数
func (c *CountMinSketch) Increment(key string) {
	for i := 0; i < c.depth; i++ {
		idx := c.hash(key, i) % c.width
		if c.matrix[i][idx] < 255 {
			c.matrix[i][idx]++
		}
	}
}

// Estimate 估计频率
func (c *CountMinSketch) Estimate(key string) uint8 {
	min := uint8(255)
	for i := 0; i < c.depth; i++ {
		idx := c.hash(key, i) % c.width
		if c.matrix[i][idx] < min {
			min = c.matrix[i][idx]
		}
	}
	return min
}

// Reset 重置（衰减）
func (c *CountMinSketch) Reset() {
	for i := range c.matrix {
		for j := range c.matrix[i] {
			c.matrix[i][j] /= 2
		}
	}
}

func (c *CountMinSketch) hash(key string, seed int) int {
	h := 0
	for _, ch := range key {
		h = h*31 + int(ch) + seed*17
	}
	if h < 0 {
		h = -h
	}
	return h
}
```

---

## Week 3: RESP 协议与命令处理

### 3.1 协议服务器

#### internal/protocol/server.go
```go
package protocol

import (
	"context"
	"log"
	"strings"
	"sync"

	"github.com/tidwall/redcon"
	"github.com/10yihang/autocache/internal/engine/memory"
)

// Server Redis 协议服务器
type Server struct {
	addr    string
	store   *memory.Store
	handler *Handler
	server  *redcon.Server
	
	mu      sync.RWMutex
	clients map[redcon.Conn]*Client
}

// Client 客户端连接
type Client struct {
	conn     redcon.Conn
	db       int
	name     string
	authenticated bool
}

// NewServer 创建服务器
func NewServer(addr string, store *memory.Store) *Server {
	s := &Server{
		addr:    addr,
		store:   store,
		clients: make(map[redcon.Conn]*Client),
	}
	s.handler = NewHandler(store)
	return s
}

// Start 启动服务器
func (s *Server) Start() error {
	log.Printf("AutoCache server starting on %s", s.addr)
	
	var err error
	s.server = redcon.NewServer(s.addr,
		s.handleCommand,
		s.handleAccept,
		s.handleClose,
	)
	
	return s.server.ListenAndServe()
}

// Stop 停止服务器
func (s *Server) Stop() error {
	return s.server.Close()
}

// handleAccept 处理新连接
func (s *Server) handleAccept(conn redcon.Conn) bool {
	s.mu.Lock()
	s.clients[conn] = &Client{
		conn: conn,
		db:   0,
	}
	s.mu.Unlock()
	
	log.Printf("Client connected: %s", conn.RemoteAddr())
	return true
}

// handleClose 处理连接关闭
func (s *Server) handleClose(conn redcon.Conn, err error) {
	s.mu.Lock()
	delete(s.clients, conn)
	s.mu.Unlock()
	
	log.Printf("Client disconnected: %s", conn.RemoteAddr())
}

// handleCommand 处理命令
func (s *Server) handleCommand(conn redcon.Conn, cmd redcon.Command) {
	if len(cmd.Args) == 0 {
		conn.WriteError("ERR empty command")
		return
	}
	
	// 转换为大写以忽略大小写
	cmdName := strings.ToUpper(string(cmd.Args[0]))
	
	// 执行命令
	ctx := context.Background()
	s.handler.Execute(ctx, conn, cmdName, cmd.Args[1:])
}
```

### 3.2 命令处理器

#### internal/protocol/handler.go
```go
package protocol

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/redcon"
	"github.com/10yihang/autocache/internal/engine/memory"
)

// Handler 命令处理器
type Handler struct {
	store    *memory.Store
	commands map[string]CommandFunc
}

// CommandFunc 命令处理函数
type CommandFunc func(ctx context.Context, conn redcon.Conn, args [][]byte)

// NewHandler 创建处理器
func NewHandler(store *memory.Store) *Handler {
	h := &Handler{
		store:    store,
		commands: make(map[string]CommandFunc),
	}
	h.registerCommands()
	return h
}

// registerCommands 注册所有命令
func (h *Handler) registerCommands() {
	// 连接命令
	h.commands["PING"] = h.cmdPing
	h.commands["ECHO"] = h.cmdEcho
	h.commands["QUIT"] = h.cmdQuit
	h.commands["COMMAND"] = h.cmdCommand
	h.commands["INFO"] = h.cmdInfo
	
	// String 命令
	h.commands["GET"] = h.cmdGet
	h.commands["SET"] = h.cmdSet
	h.commands["SETNX"] = h.cmdSetNX
	h.commands["SETEX"] = h.cmdSetEX
	h.commands["PSETEX"] = h.cmdPSetEX
	h.commands["MGET"] = h.cmdMGet
	h.commands["MSET"] = h.cmdMSet
	h.commands["INCR"] = h.cmdIncr
	h.commands["INCRBY"] = h.cmdIncrBy
	h.commands["DECR"] = h.cmdDecr
	h.commands["DECRBY"] = h.cmdDecrBy
	h.commands["APPEND"] = h.cmdAppend
	h.commands["STRLEN"] = h.cmdStrlen
	h.commands["GETSET"] = h.cmdGetSet
	
	// Key 命令
	h.commands["DEL"] = h.cmdDel
	h.commands["EXISTS"] = h.cmdExists
	h.commands["KEYS"] = h.cmdKeys
	h.commands["TYPE"] = h.cmdType
	h.commands["RENAME"] = h.cmdRename
	h.commands["DBSIZE"] = h.cmdDBSize
	h.commands["FLUSHDB"] = h.cmdFlushDB
	h.commands["FLUSHALL"] = h.cmdFlushDB
	
	// TTL 命令
	h.commands["EXPIRE"] = h.cmdExpire
	h.commands["EXPIREAT"] = h.cmdExpireAt
	h.commands["PEXPIRE"] = h.cmdPExpire
	h.commands["TTL"] = h.cmdTTL
	h.commands["PTTL"] = h.cmdPTTL
	h.commands["PERSIST"] = h.cmdPersist
	
	// 调试命令
	h.commands["DEBUG"] = h.cmdDebug
	h.commands["CONFIG"] = h.cmdConfig
	h.commands["CLIENT"] = h.cmdClient
}

// Execute 执行命令
func (h *Handler) Execute(ctx context.Context, conn redcon.Conn, cmd string, args [][]byte) {
	fn, ok := h.commands[cmd]
	if !ok {
		conn.WriteError("ERR unknown command '" + cmd + "'")
		return
	}
	fn(ctx, conn, args)
}

// =============== 连接命令 ===============

func (h *Handler) cmdPing(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) == 0 {
		conn.WriteString("PONG")
	} else {
		conn.WriteBulkString(string(args[0]))
	}
}

func (h *Handler) cmdEcho(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'echo' command")
		return
	}
	conn.WriteBulkString(string(args[0]))
}

func (h *Handler) cmdQuit(ctx context.Context, conn redcon.Conn, args [][]byte) {
	conn.WriteString("OK")
	conn.Close()
}

func (h *Handler) cmdCommand(ctx context.Context, conn redcon.Conn, args [][]byte) {
	// redis-benchmark 需要这个命令
	// 返回空数组或者命令列表
	if len(args) > 0 && strings.ToUpper(string(args[0])) == "DOCS" {
		conn.WriteArray(0)
		return
	}
	conn.WriteArray(0)
}

func (h *Handler) cmdInfo(ctx context.Context, conn redcon.Conn, args [][]byte) {
	stats := h.store.GetStats()
	size, _ := h.store.DBSize(ctx)
	
	info := "# Server\r\n" +
		"autocache_version:0.1.0\r\n" +
		"\r\n# Stats\r\n" +
		"total_connections_received:0\r\n" +
		"total_commands_processed:" + strconv.FormatInt(stats.GetOps+stats.SetOps+stats.DelOps, 10) + "\r\n" +
		"\r\n# Keyspace\r\n" +
		"db0:keys=" + strconv.FormatInt(size, 10) + ",expires=0\r\n"
	
	conn.WriteBulkString(info)
}

// =============== String 命令 ===============

func (h *Handler) cmdGet(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'get' command")
		return
	}
	
	val, err := h.store.Get(ctx, string(args[0]))
	if err != nil {
		conn.WriteNull()
		return
	}
	conn.WriteBulkString(val)
}

func (h *Handler) cmdSet(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) < 2 {
		conn.WriteError("ERR wrong number of arguments for 'set' command")
		return
	}
	
	key := string(args[0])
	value := string(args[1])
	var ttl time.Duration
	var nx, xx bool
	
	// 解析选项
	for i := 2; i < len(args); i++ {
		opt := strings.ToUpper(string(args[i]))
		switch opt {
		case "EX":
			if i+1 >= len(args) {
				conn.WriteError("ERR syntax error")
				return
			}
			secs, err := strconv.ParseInt(string(args[i+1]), 10, 64)
			if err != nil {
				conn.WriteError("ERR value is not an integer or out of range")
				return
			}
			ttl = time.Duration(secs) * time.Second
			i++
		case "PX":
			if i+1 >= len(args) {
				conn.WriteError("ERR syntax error")
				return
			}
			ms, err := strconv.ParseInt(string(args[i+1]), 10, 64)
			if err != nil {
				conn.WriteError("ERR value is not an integer or out of range")
				return
			}
			ttl = time.Duration(ms) * time.Millisecond
			i++
		case "NX":
			nx = true
		case "XX":
			xx = true
		}
	}
	
	if nx && xx {
		conn.WriteError("ERR XX and NX options at the same time are not compatible")
		return
	}
	
	if nx {
		ok, _ := h.store.SetNX(ctx, key, value, ttl)
		if !ok {
			conn.WriteNull()
			return
		}
	} else if xx {
		ok, _ := h.store.SetXX(ctx, key, value, ttl)
		if !ok {
			conn.WriteNull()
			return
		}
	} else {
		h.store.Set(ctx, key, value, ttl)
	}
	
	conn.WriteString("OK")
}

func (h *Handler) cmdSetNX(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'setnx' command")
		return
	}
	
	ok, _ := h.store.SetNX(ctx, string(args[0]), string(args[1]), 0)
	if ok {
		conn.WriteInt(1)
	} else {
		conn.WriteInt(0)
	}
}

func (h *Handler) cmdSetEX(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 3 {
		conn.WriteError("ERR wrong number of arguments for 'setex' command")
		return
	}
	
	secs, err := strconv.ParseInt(string(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}
	
	h.store.Set(ctx, string(args[0]), string(args[2]), time.Duration(secs)*time.Second)
	conn.WriteString("OK")
}

func (h *Handler) cmdPSetEX(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 3 {
		conn.WriteError("ERR wrong number of arguments for 'psetex' command")
		return
	}
	
	ms, err := strconv.ParseInt(string(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}
	
	h.store.Set(ctx, string(args[0]), string(args[2]), time.Duration(ms)*time.Millisecond)
	conn.WriteString("OK")
}

func (h *Handler) cmdMGet(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) == 0 {
		conn.WriteError("ERR wrong number of arguments for 'mget' command")
		return
	}
	
	keys := make([]string, len(args))
	for i, arg := range args {
		keys[i] = string(arg)
	}
	
	results, _ := h.store.MGet(ctx, keys...)
	conn.WriteArray(len(results))
	for _, v := range results {
		if v == nil {
			conn.WriteNull()
		} else {
			conn.WriteBulkString(v.(string))
		}
	}
}

func (h *Handler) cmdMSet(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) < 2 || len(args)%2 != 0 {
		conn.WriteError("ERR wrong number of arguments for 'mset' command")
		return
	}
	
	pairs := make([]interface{}, len(args))
	for i, arg := range args {
		pairs[i] = string(arg)
	}
	
	h.store.MSet(ctx, pairs...)
	conn.WriteString("OK")
}

func (h *Handler) cmdIncr(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'incr' command")
		return
	}
	
	val, err := h.store.Incr(ctx, string(args[0]))
	if err != nil {
		conn.WriteError("ERR " + err.Error())
		return
	}
	conn.WriteInt64(val)
}

func (h *Handler) cmdIncrBy(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'incrby' command")
		return
	}
	
	delta, err := strconv.ParseInt(string(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}
	
	val, err := h.store.IncrBy(ctx, string(args[0]), delta)
	if err != nil {
		conn.WriteError("ERR " + err.Error())
		return
	}
	conn.WriteInt64(val)
}

func (h *Handler) cmdDecr(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'decr' command")
		return
	}
	
	val, err := h.store.Decr(ctx, string(args[0]))
	if err != nil {
		conn.WriteError("ERR " + err.Error())
		return
	}
	conn.WriteInt64(val)
}

func (h *Handler) cmdDecrBy(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'decrby' command")
		return
	}
	
	delta, err := strconv.ParseInt(string(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}
	
	val, err := h.store.DecrBy(ctx, string(args[0]), delta)
	if err != nil {
		conn.WriteError("ERR " + err.Error())
		return
	}
	conn.WriteInt64(val)
}

func (h *Handler) cmdAppend(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'append' command")
		return
	}
	
	length, err := h.store.Append(ctx, string(args[0]), string(args[1]))
	if err != nil {
		conn.WriteError("ERR " + err.Error())
		return
	}
	conn.WriteInt64(length)
}

func (h *Handler) cmdStrlen(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'strlen' command")
		return
	}
	
	length, _ := h.store.Strlen(ctx, string(args[0]))
	conn.WriteInt64(length)
}

func (h *Handler) cmdGetSet(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'getset' command")
		return
	}
	
	old, err := h.store.GetSet(ctx, string(args[0]), string(args[1]))
	if err != nil || old == "" {
		conn.WriteNull()
		return
	}
	conn.WriteBulkString(old)
}

// =============== Key 命令 ===============

func (h *Handler) cmdDel(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) == 0 {
		conn.WriteError("ERR wrong number of arguments for 'del' command")
		return
	}
	
	keys := make([]string, len(args))
	for i, arg := range args {
		keys[i] = string(arg)
	}
	
	count, _ := h.store.Del(ctx, keys...)
	conn.WriteInt64(count)
}

func (h *Handler) cmdExists(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) == 0 {
		conn.WriteError("ERR wrong number of arguments for 'exists' command")
		return
	}
	
	keys := make([]string, len(args))
	for i, arg := range args {
		keys[i] = string(arg)
	}
	
	count, _ := h.store.Exists(ctx, keys...)
	conn.WriteInt64(count)
}

func (h *Handler) cmdKeys(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'keys' command")
		return
	}
	
	keys, _ := h.store.Keys(ctx, string(args[0]))
	conn.WriteArray(len(keys))
	for _, key := range keys {
		conn.WriteBulkString(key)
	}
}

func (h *Handler) cmdType(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'type' command")
		return
	}
	
	t, _ := h.store.Type(ctx, string(args[0]))
	conn.WriteString(t)
}

func (h *Handler) cmdRename(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'rename' command")
		return
	}
	
	err := h.store.Rename(ctx, string(args[0]), string(args[1]))
	if err != nil {
		conn.WriteError("ERR " + err.Error())
		return
	}
	conn.WriteString("OK")
}

func (h *Handler) cmdDBSize(ctx context.Context, conn redcon.Conn, args [][]byte) {
	size, _ := h.store.DBSize(ctx)
	conn.WriteInt64(size)
}

func (h *Handler) cmdFlushDB(ctx context.Context, conn redcon.Conn, args [][]byte) {
	h.store.FlushDB(ctx)
	conn.WriteString("OK")
}

// =============== TTL 命令 ===============

func (h *Handler) cmdExpire(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'expire' command")
		return
	}
	
	secs, err := strconv.ParseInt(string(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}
	
	ok, _ := h.store.Expire(ctx, string(args[0]), time.Duration(secs)*time.Second)
	if ok {
		conn.WriteInt(1)
	} else {
		conn.WriteInt(0)
	}
}

func (h *Handler) cmdExpireAt(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'expireat' command")
		return
	}
	
	ts, err := strconv.ParseInt(string(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}
	
	ok, _ := h.store.ExpireAt(ctx, string(args[0]), time.Unix(ts, 0))
	if ok {
		conn.WriteInt(1)
	} else {
		conn.WriteInt(0)
	}
}

func (h *Handler) cmdPExpire(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'pexpire' command")
		return
	}
	
	ms, err := strconv.ParseInt(string(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}
	
	ok, _ := h.store.Expire(ctx, string(args[0]), time.Duration(ms)*time.Millisecond)
	if ok {
		conn.WriteInt(1)
	} else {
		conn.WriteInt(0)
	}
}

func (h *Handler) cmdTTL(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'ttl' command")
		return
	}
	
	ttl, _ := h.store.TTL(ctx, string(args[0]))
	conn.WriteInt64(int64(ttl / time.Second))
}

func (h *Handler) cmdPTTL(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'pttl' command")
		return
	}
	
	ttl, _ := h.store.TTL(ctx, string(args[0]))
	conn.WriteInt64(int64(ttl / time.Millisecond))
}

func (h *Handler) cmdPersist(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'persist' command")
		return
	}
	
	ok, _ := h.store.Persist(ctx, string(args[0]))
	if ok {
		conn.WriteInt(1)
	} else {
		conn.WriteInt(0)
	}
}

// =============== 调试命令 ===============

func (h *Handler) cmdDebug(ctx context.Context, conn redcon.Conn, args [][]byte) {
	conn.WriteString("OK")
}

func (h *Handler) cmdConfig(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) == 0 {
		conn.WriteError("ERR wrong number of arguments for 'config' command")
		return
	}
	
	subcmd := strings.ToUpper(string(args[0]))
	switch subcmd {
	case "GET":
		conn.WriteArray(0)
	case "SET":
		conn.WriteString("OK")
	default:
		conn.WriteError("ERR unknown subcommand")
	}
}

func (h *Handler) cmdClient(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) == 0 {
		conn.WriteError("ERR wrong number of arguments for 'client' command")
		return
	}
	
	subcmd := strings.ToUpper(string(args[0]))
	switch subcmd {
	case "SETNAME":
		conn.WriteString("OK")
	case "GETNAME":
		conn.WriteNull()
	case "LIST":
		conn.WriteBulkString("")
	default:
		conn.WriteString("OK")
	}
}
```

---

## Week 4-5: 持久化与测试

### 4.1 RDB 持久化

#### internal/persistence/rdb/rdb.go
```go
package rdb

import (
	"bufio"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/10yihang/autocache/internal/engine/memory"
)

const (
	RDBMagic   = "AUTOCACHE"
	RDBVersion = 1
)

// Snapshot RDB 快照
type Snapshot struct {
	path string
}

// NewSnapshot 创建快照管理器
func NewSnapshot(path string) *Snapshot {
	return &Snapshot{path: path}
}

// Save 保存快照
func (s *Snapshot) Save(dict *memory.Dict) error {
	// 创建临时文件
	tmpPath := s.path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer f.Close()
	
	w := bufio.NewWriter(f)
	
	// 写入 header
	if err := s.writeHeader(w); err != nil {
		return err
	}
	
	// 写入数据
	if err := s.writeData(w, dict); err != nil {
		return err
	}
	
	// 写入 footer
	if err := s.writeFooter(w); err != nil {
		return err
	}
	
	if err := w.Flush(); err != nil {
		return err
	}
	
	// 原子替换
	return os.Rename(tmpPath, s.path)
}

// Load 加载快照
func (s *Snapshot) Load(dict *memory.Dict) error {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 没有快照文件
		}
		return err
	}
	defer f.Close()
	
	r := bufio.NewReader(f)
	
	// 读取并验证 header
	if err := s.readHeader(r); err != nil {
		return err
	}
	
	// 读取数据
	return s.readData(r, dict)
}

func (s *Snapshot) writeHeader(w *bufio.Writer) error {
	// Magic
	if _, err := w.WriteString(RDBMagic); err != nil {
		return err
	}
	// Version
	return binary.Write(w, binary.LittleEndian, uint32(RDBVersion))
}

func (s *Snapshot) readHeader(r *bufio.Reader) error {
	magic := make([]byte, len(RDBMagic))
	if _, err := io.ReadFull(r, magic); err != nil {
		return err
	}
	if string(magic) != RDBMagic {
		return fmt.Errorf("invalid RDB magic")
	}
	
	var version uint32
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return err
	}
	if version > RDBVersion {
		return fmt.Errorf("unsupported RDB version: %d", version)
	}
	return nil
}

func (s *Snapshot) writeData(w *bufio.Writer, dict *memory.Dict) error {
	enc := gob.NewEncoder(w)
	
	dict.ForEach(func(key string, entry *memory.Entry) bool {
		// 跳过已过期的 key
		if entry.IsExpired() {
			return true
		}
		
		item := &RDBItem{
			Key:      key,
			Value:    entry.Value,
			ExpireAt: entry.ExpireAt,
		}
		enc.Encode(item)
		return true
	})
	
	// 写入结束标记
	return enc.Encode(&RDBItem{Key: ""})
}

func (s *Snapshot) readData(r *bufio.Reader, dict *memory.Dict) error {
	dec := gob.NewDecoder(r)
	
	for {
		var item RDBItem
		if err := dec.Decode(&item); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		
		// 结束标记
		if item.Key == "" {
			break
		}
		
		// 跳过已过期
		if item.ExpireAt > 0 && time.Now().UnixNano() > item.ExpireAt {
			continue
		}
		
		entry := &memory.Entry{
			Value:      item.Value,
			ExpireAt:   item.ExpireAt,
			CreatedAt:  time.Now().UnixNano(),
			UpdatedAt:  time.Now().UnixNano(),
			LastAccess: time.Now().UnixNano(),
		}
		dict.SetEntry(item.Key, entry)
	}
	
	return nil
}

func (s *Snapshot) writeFooter(w *bufio.Writer) error {
	// 可以添加 CRC 校验等
	return nil
}

// RDBItem RDB 存储项
type RDBItem struct {
	Key      string
	Value    interface{}
	ExpireAt int64
}
```

### 4.2 AOF 持久化

#### internal/persistence/aof/aof.go
```go
package aof

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SyncPolicy AOF 同步策略
type SyncPolicy int

const (
	SyncAlways   SyncPolicy = iota // 每次写入都同步
	SyncEverySec                   // 每秒同步
	SyncNo                         // 不主动同步
)

// AOF Append-Only File
type AOF struct {
	path   string
	file   *os.File
	writer *bufio.Writer
	policy SyncPolicy
	
	mu       sync.Mutex
	lastSync time.Time
	
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewAOF 创建 AOF
func NewAOF(path string, policy SyncPolicy) (*AOF, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	
	a := &AOF{
		path:   path,
		file:   f,
		writer: bufio.NewWriter(f),
		policy: policy,
		stopCh: make(chan struct{}),
	}
	
	// 启动定时同步
	if policy == SyncEverySec {
		a.wg.Add(1)
		go a.syncLoop()
	}
	
	return a, nil
}

// Write 写入命令
func (a *AOF) Write(args ...string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	
	// RESP 格式
	// *<count>\r\n
	// $<len>\r\n<arg>\r\n
	// ...
	
	a.writer.WriteString("*")
	a.writer.WriteString(strconv.Itoa(len(args)))
	a.writer.WriteString("\r\n")
	
	for _, arg := range args {
		a.writer.WriteString("$")
		a.writer.WriteString(strconv.Itoa(len(arg)))
		a.writer.WriteString("\r\n")
		a.writer.WriteString(arg)
		a.writer.WriteString("\r\n")
	}
	
	// 根据策略同步
	if a.policy == SyncAlways {
		a.writer.Flush()
		a.file.Sync()
	}
	
	return nil
}

// Replay 重放 AOF
func (a *AOF) Replay(handler func(args []string) error) error {
	f, err := os.Open(a.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	
	reader := bufio.NewReader(f)
	
	for {
		args, err := a.readCommand(reader)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		
		if len(args) > 0 {
			if err := handler(args); err != nil {
				return err
			}
		}
	}
	
	return nil
}

func (a *AOF) readCommand(reader *bufio.Reader) ([]string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	
	line = strings.TrimSpace(line)
	if len(line) == 0 || line[0] != '*' {
		return nil, fmt.Errorf("invalid AOF format")
	}
	
	count, err := strconv.Atoi(line[1:])
	if err != nil {
		return nil, err
	}
	
	args := make([]string, count)
	for i := 0; i < count; i++ {
		// 读取 $<len>
		line, err = reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if len(line) == 0 || line[0] != '$' {
			return nil, fmt.Errorf("invalid AOF format")
		}
		
		length, err := strconv.Atoi(line[1:])
		if err != nil {
			return nil, err
		}
		
		// 读取参数
		data := make([]byte, length+2) // +2 for \r\n
		if _, err := io.ReadFull(reader, data); err != nil {
			return nil, err
		}
		
		args[i] = string(data[:length])
	}
	
	return args, nil
}

// syncLoop 定时同步循环
func (a *AOF) syncLoop() {
	defer a.wg.Done()
	
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-a.stopCh:
			return
		case <-ticker.C:
			a.sync()
		}
	}
}

func (a *AOF) sync() {
	a.mu.Lock()
	defer a.mu.Unlock()
	
	a.writer.Flush()
	a.file.Sync()
	a.lastSync = time.Now()
}

// Close 关闭 AOF
func (a *AOF) Close() error {
	close(a.stopCh)
	a.wg.Wait()
	
	a.mu.Lock()
	defer a.mu.Unlock()
	
	a.writer.Flush()
	return a.file.Close()
}
```

---

## 错误定义

#### internal/engine/errors.go
```go
package engine

import "errors"

var (
	ErrKeyNotFound = errors.New("key not found")
	ErrWrongType   = errors.New("WRONGTYPE Operation against a key holding the wrong kind of value")
	ErrNotInteger  = errors.New("value is not an integer or out of range")
	ErrOutOfRange  = errors.New("index out of range")
	ErrSyntax      = errors.New("syntax error")
)
```

---

## 测试验证

### redis-benchmark 测试脚本

#### test/benchmark/redis_benchmark.sh
```bash
#!/bin/bash

# AutoCache Benchmark Script
# 使用 redis-benchmark 进行性能测试

HOST=${1:-127.0.0.1}
PORT=${2:-6379}
REQUESTS=${3:-100000}
CLIENTS=${4:-50}

echo "========================================"
echo "AutoCache Performance Benchmark"
echo "========================================"
echo "Host: $HOST"
echo "Port: $PORT"
echo "Requests: $REQUESTS"
echo "Clients: $CLIENTS"
echo "========================================"

# 基础测试
echo ""
echo "=== Basic Commands ==="
redis-benchmark -h $HOST -p $PORT -n $REQUESTS -c $CLIENTS -t ping,set,get,incr,del -q

# SET 测试（不同数据大小）
echo ""
echo "=== SET with different data sizes ==="
for size in 10 100 1000 10000; do
    echo "Data size: $size bytes"
    redis-benchmark -h $HOST -p $PORT -n $REQUESTS -c $CLIENTS -t set -d $size -q
done

# Pipeline 测试
echo ""
echo "=== Pipeline Test ==="
for pipeline in 1 10 50 100; do
    echo "Pipeline: $pipeline"
    redis-benchmark -h $HOST -p $PORT -n $REQUESTS -c $CLIENTS -t set,get -P $pipeline -q
done

# 延迟测试
echo ""
echo "=== Latency Test ==="
redis-benchmark -h $HOST -p $PORT -n 10000 -c 1 -t get --latency

echo ""
echo "========================================"
echo "Benchmark Complete"
echo "========================================"
```

### 单元测试示例

#### internal/engine/memory/store_test.go
```go
package memory

import (
	"context"
	"testing"
	"time"
)

func TestStore_SetGet(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()
	
	ctx := context.Background()
	
	// Test SET
	err := store.Set(ctx, "key1", "value1", 0)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	
	// Test GET
	val, err := store.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if val != "value1" {
		t.Fatalf("Expected 'value1', got '%s'", val)
	}
}

func TestStore_TTL(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()
	
	ctx := context.Background()
	
	// Set with TTL
	store.Set(ctx, "key1", "value1", 100*time.Millisecond)
	
	// Should exist
	_, err := store.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Key should exist")
	}
	
	// Wait for expiry
	time.Sleep(150 * time.Millisecond)
	
	// Should not exist
	_, err = store.Get(ctx, "key1")
	if err == nil {
		t.Fatalf("Key should be expired")
	}
}

func TestStore_Incr(t *testing.T) {
	store := NewStore(DefaultConfig())
	defer store.Close()
	
	ctx := context.Background()
	
	// INCR on non-existing key
	val, err := store.Incr(ctx, "counter")
	if err != nil {
		t.Fatalf("Incr failed: %v", err)
	}
	if val != 1 {
		t.Fatalf("Expected 1, got %d", val)
	}
	
	// INCR again
	val, err = store.Incr(ctx, "counter")
	if err != nil {
		t.Fatalf("Incr failed: %v", err)
	}
	if val != 2 {
		t.Fatalf("Expected 2, got %d", val)
	}
}

func BenchmarkStore_Set(b *testing.B) {
	store := NewStore(DefaultConfig())
	defer store.Close()
	
	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Set(ctx, "key", "value", 0)
	}
}

func BenchmarkStore_Get(b *testing.B) {
	store := NewStore(DefaultConfig())
	defer store.Close()
	
	ctx := context.Background()
	store.Set(ctx, "key", "value", 0)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Get(ctx, "key")
	}
}

func BenchmarkStore_SetGet_Parallel(b *testing.B) {
	store := NewStore(DefaultConfig())
	defer store.Close()
	
	ctx := context.Background()
	
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := "key" + string(rune(i%1000))
			store.Set(ctx, key, "value", 0)
			store.Get(ctx, key)
			i++
		}
	})
}
```

---

## 交付物

- [x] 内存存储引擎（分片哈希表）
- [x] 完整的 String 命令实现
- [x] TTL 过期机制（惰性删除 + 定期删除）
- [x] LRU/LFU 淘汰策略
- [x] RESP 协议服务器
- [x] RDB 持久化
- [x] AOF 持久化
- [x] redis-benchmark 测试通过

## 验收标准

```bash
# 启动服务器
go run cmd/server/main.go

# redis-cli 测试
redis-cli -p 6379 PING        # PONG
redis-cli -p 6379 SET a b     # OK
redis-cli -p 6379 GET a       # "b"

# redis-benchmark 测试
redis-benchmark -p 6379 -n 100000 -c 50 -t set,get -q
# 期望 QPS > 50000
```

## 下一步

完成 Phase 1 后，进入 [Phase 2: Gossip 分布式集群](./03-phase2-cluster.md)
