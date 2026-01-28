package memory

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/10yihang/autocache/pkg/errors"
)

type Store struct {
	cache   *ShardedCache
	expires *ExpiryManager
	evictor *Evictor

	maxMemory   int64
	evictPolicy string

	stats *Stats

	mu sync.RWMutex
}

// Stats uses atomic counters for lock-free updates
// Performance: Eliminates lock cont	tion on hot path
type Stats struct {
	Hits        atomic.Int64
	Misses      atomic.Int64
	SetOps      atomic.Int64
	GetOps      atomic.Int64
	DelOps      atomic.Int64
	ExpiredKeys atomic.Int64
	EvictedKeys atomic.Int64
}

type Config struct {
	ShardCount  int
	MaxMemory   int64
	EvictPolicy string
}

func DefaultConfig() *Config {
	return &Config{
		ShardCount:  256,
		MaxMemory:   0,
		EvictPolicy: "noeviction",
	}
}

func NewStore(cfg *Config) *Store {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	s := &Store{
		cache:       NewShardedCache(ShardedCacheConfig{ShardCount: cfg.ShardCount}),
		maxMemory:   cfg.MaxMemory,
		evictPolicy: cfg.EvictPolicy,
		stats:       &Stats{},
	}

	s.expires = NewExpiryManager(s.cache)
	s.expires.Start()

	if cfg.MaxMemory > 0 && cfg.EvictPolicy != "noeviction" {
		s.evictor = NewEvictor(s.cache, cfg.MaxMemory, cfg.EvictPolicy)
	}

	return s
}

func (s *Store) Get(_ context.Context, key string) (string, error) {
	s.stats.GetOps.Add(1)

	value, ok := s.cache.GetCopy(key)
	if !ok {
		s.stats.Misses.Add(1)
		return "", errors.ErrKeyNotFound
	}

	s.stats.Hits.Add(1)
	return BytesToString(value), nil
}

func (s *Store) GetBytes(_ context.Context, key string) ([]byte, error) {
	s.stats.GetOps.Add(1)

	value, ok := s.cache.Get(key)
	if !ok {
		s.stats.Misses.Add(1)
		return nil, errors.ErrKeyNotFound
	}

	s.stats.Hits.Add(1)
	return value, nil
}

func (s *Store) Set(_ context.Context, key string, value string, ttl time.Duration) error {
	s.stats.SetOps.Add(1)

	if s.evictor != nil {
		s.evictor.TryEvict()
	}

	return s.cache.Set(key, StringToBytes(value), ttl)
}

func (s *Store) SetNX(_ context.Context, key string, value string, ttl time.Duration) (bool, error) {
	return s.cache.SetNX(key, StringToBytes(value), ttl)
}

func (s *Store) SetXX(_ context.Context, key string, value string, ttl time.Duration) (bool, error) {
	return s.cache.SetXX(key, StringToBytes(value), ttl)
}

func (s *Store) GetSet(_ context.Context, key string, value string) (string, error) {
	old, _ := s.Get(context.Background(), key)
	s.Set(context.Background(), key, value, 0)
	return old, nil
}

func (s *Store) Incr(_ context.Context, key string) (int64, error) {
	return s.IncrBy(context.Background(), key, 1)
}

func (s *Store) IncrBy(_ context.Context, key string, delta int64) (int64, error) {
	value, ok := s.cache.GetCopy(key)
	var val int64
	if ok {
		str := BytesToString(value)
		parsed, err := strconv.ParseInt(str, 10, 64)
		if err != nil {
			return 0, errors.ErrNotInteger
		}
		val = parsed
	}

	val += delta
	if err := s.cache.Set(key, StringToBytes(strconv.FormatInt(val, 10)), 0); err != nil {
		return 0, err
	}
	return val, nil
}

func (s *Store) Decr(_ context.Context, key string) (int64, error) {
	return s.IncrBy(context.Background(), key, -1)
}

func (s *Store) DecrBy(_ context.Context, key string, delta int64) (int64, error) {
	return s.IncrBy(context.Background(), key, -delta)
}

func (s *Store) Append(_ context.Context, key string, value string) (int64, error) {
	current, ok := s.cache.GetCopy(key)
	var str string
	if ok {
		str = BytesToString(current)
	}

	str += value
	if err := s.cache.Set(key, StringToBytes(str), 0); err != nil {
		return 0, err
	}
	return int64(len(str)), nil
}

func (s *Store) Strlen(_ context.Context, key string) (int64, error) {
	value, ok := s.cache.GetCopy(key)
	if !ok {
		return 0, nil
	}

	return int64(len(value)), nil
}

func (s *Store) MGet(_ context.Context, keys ...string) ([]interface{}, error) {
	result := make([]interface{}, len(keys))
	for i, key := range keys {
		val, err := s.Get(context.Background(), key)
		if err == nil {
			result[i] = val
		} else {
			result[i] = nil
		}
	}
	return result, nil
}

func (s *Store) MGetBytes(_ context.Context, keys ...string) ([][]byte, error) {
	result := make([][]byte, len(keys))
	for i, key := range keys {
		val, err := s.GetBytes(context.Background(), key)
		if err == nil {
			result[i] = val
		} else {
			result[i] = nil
		}
	}
	return result, nil
}

func (s *Store) MSet(_ context.Context, pairs ...interface{}) error {
	if len(pairs)%2 != 0 {
		return fmt.Errorf("wrong number of arguments")
	}

	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			return fmt.Errorf("invalid key type")
		}
		value := fmt.Sprint(pairs[i+1])
		if err := s.cache.Set(key, StringToBytes(value), 0); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Del(_ context.Context, keys ...string) (int64, error) {
	s.stats.DelOps.Add(1)
	var count int64
	for _, key := range keys {
		if s.cache.Delete(key) {
			count++
		}
	}
	return count, nil
}

func (s *Store) Exists(_ context.Context, keys ...string) (int64, error) {
	var count int64
	for _, key := range keys {
		if s.cache.Exists(key) {
			count++
		}
	}
	return count, nil
}

func (s *Store) Keys(_ context.Context, pattern string) ([]string, error) {
	return s.cache.Keys(pattern), nil
}

func (s *Store) Type(_ context.Context, key string) (string, error) {
	_, ok := s.cache.Get(key)
	if !ok {
		return "none", nil
	}

	return "string", nil
}

func (s *Store) Rename(_ context.Context, key, newkey string) error {
	value, ok := s.cache.GetCopy(key)
	if !ok {
		return errors.ErrKeyNotFound
	}

	s.cache.Delete(key)
	return s.cache.Set(newkey, value, 0)
}

func (s *Store) Expire(_ context.Context, key string, ttl time.Duration) (bool, error) {
	return s.cache.Expire(key, ttl), nil
}

func (s *Store) ExpireAt(_ context.Context, key string, t time.Time) (bool, error) {
	ttl := time.Until(t)
	if ttl < 0 {
		s.cache.Delete(key)
		return true, nil
	}
	return s.cache.Expire(key, ttl), nil
}

func (s *Store) TTL(_ context.Context, key string) (time.Duration, error) {
	ttl, ok := s.cache.TTL(key)
	if !ok {
		return -2 * time.Second, nil
	}
	return ttl, nil
}

func (s *Store) Persist(_ context.Context, key string) (bool, error) {
	return s.cache.Expire(key, 0), nil
}

func (s *Store) DBSize(_ context.Context) (int64, error) {
	return s.cache.Len(), nil
}

func (s *Store) FlushDB(_ context.Context) error {
	s.cache.Clear()
	return nil
}

func (s *Store) GetStats() *Stats {
	return s.stats
}

func (s *Store) Close() error {
	s.expires.Stop()
	return nil
}

func (s *Store) Scan(_ context.Context, cursor uint64, pattern string, count int) ([]string, uint64, error) {
	return s.cache.Scan(cursor, pattern, count)
}
