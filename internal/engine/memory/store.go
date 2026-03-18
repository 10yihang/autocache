package memory

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/10yihang/autocache/internal/engine"
	"github.com/10yihang/autocache/pkg/errors"
)

type Store struct {
	cache   *ShardedCache
	objects *Dict
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
		objects:     NewDict(cfg.ShardCount),
		maxMemory:   cfg.MaxMemory,
		evictPolicy: cfg.EvictPolicy,
		stats:       &Stats{},
	}

	s.expires = NewExpiryManager(s.cache, s.objects, s.untrackKey)
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
		if s.hasObjectKey(key) {
			return "", errors.ErrWrongType
		}
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
		if s.hasObjectKey(key) {
			return nil, errors.ErrWrongType
		}
		s.stats.Misses.Add(1)
		return nil, errors.ErrKeyNotFound
	}

	s.stats.Hits.Add(1)
	return value, nil
}

func (s *Store) Set(_ context.Context, key string, value string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stats.SetOps.Add(1)
	return s.setStringLocked(key, value, ttl)
}

func (s *Store) setStringLocked(key string, value string, ttl time.Duration) error {
	s.removeObjectKey(key)

	if s.evictor != nil {
		s.evictor.TryEvict()
	}

	return s.cache.Set(key, StringToBytes(value), ttl)
}

func (s *Store) SetNX(_ context.Context, key string, value string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasObjectKey(key) {
		return false, nil
	}
	return s.cache.SetNX(key, StringToBytes(value), ttl)
}

func (s *Store) SetXX(_ context.Context, key string, value string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasObjectKey(key) {
		return true, s.setStringLocked(key, value, ttl)
	}
	return s.cache.SetXX(key, StringToBytes(value), ttl)
}

func (s *Store) GetSet(_ context.Context, key string, value string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var old string
	if current, ok := s.cache.GetCopy(key); ok {
		old = BytesToString(current)
	} else if s.hasObjectKey(key) {
		return "", errors.ErrWrongType
	}

	if err := s.setStringLocked(key, value, 0); err != nil {
		return "", err
	}
	return old, nil
}

func (s *Store) Incr(_ context.Context, key string) (int64, error) {
	return s.IncrBy(context.Background(), key, 1)
}

func (s *Store) IncrBy(_ context.Context, key string, delta int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	value, ok := s.cache.GetCopy(key)
	var val int64
	if ok {
		str := BytesToString(value)
		parsed, err := strconv.ParseInt(str, 10, 64)
		if err != nil {
			return 0, errors.ErrNotInteger
		}
		val = parsed
	} else if s.hasObjectKey(key) {
		return 0, errors.ErrWrongType
	}

	val += delta
	if err := s.setStringLocked(key, strconv.FormatInt(val, 10), 0); err != nil {
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
	s.mu.Lock()
	defer s.mu.Unlock()

	current, ok := s.cache.GetCopy(key)
	var str string
	if ok {
		str = BytesToString(current)
	} else if s.hasObjectKey(key) {
		return 0, errors.ErrWrongType
	}

	str += value
	if err := s.setStringLocked(key, str, 0); err != nil {
		return 0, err
	}
	return int64(len(str)), nil
}

func (s *Store) Strlen(_ context.Context, key string) (int64, error) {
	value, ok := s.cache.GetCopy(key)
	if !ok {
		if s.hasObjectKey(key) {
			return 0, errors.ErrWrongType
		}
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
		if err := s.Set(context.Background(), key, value, 0); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Del(_ context.Context, keys ...string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stats.DelOps.Add(1)
	var count int64
	for _, key := range keys {
		deleted := false
		if s.cache.Delete(key) {
			deleted = true
		}
		if s.deleteObjectKey(key) {
			deleted = true
		}
		if deleted {
			count++
		}
	}
	return count, nil
}

func (s *Store) Exists(_ context.Context, keys ...string) (int64, error) {
	var count int64
	for _, key := range keys {
		if s.cache.Exists(key) || s.hasObjectKey(key) {
			count++
		}
	}
	return count, nil
}

func (s *Store) Keys(_ context.Context, pattern string) ([]string, error) {
	keys := s.cache.Keys(pattern)
	for _, key := range s.objects.Keys(pattern) {
		if !containsKey(keys, key) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (s *Store) GetEntry(_ context.Context, key string) (*engine.Entry, error) {
	if value, ok := s.cache.GetCopy(key); ok {
		entry := &engine.Entry{Key: key, Value: value, Type: engine.TypeString}
		if ttl, ok := s.cache.TTL(key); ok && ttl > 0 {
			entry.ExpireAt = time.Now().Add(ttl)
		}
		return entry, nil
	}

	value, entry, ok := s.getObjectValue(key)
	if !ok {
		return nil, errors.ErrKeyNotFound
	}

	result := &engine.Entry{Key: key, Value: value.Value, Type: value.Type}
	if expireAt := entry.GetExpireAt(); expireAt > 0 {
		result.ExpireAt = time.Unix(0, expireAt)
	}
	return result, nil
}

func (s *Store) RestoreEntry(_ context.Context, key string, valueType engine.ValueType, payload []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	decoded, err := DecodeSerializedValue(valueType, payload)
	if err != nil {
		return err
	}

	s.cache.Delete(key)
	s.deleteObjectKey(key)

	if valueType == engine.TypeString {
		value, ok := decoded.(string)
		if !ok {
			return fmt.Errorf("decoded string payload has type %T", decoded)
		}
		return s.setStringLocked(key, value, ttl)
	}

	s.objects.Set(key, &typedValue{Type: valueType, Value: decoded}, ttl)
	s.trackKey(key)
	return nil
}

func (s *Store) Type(_ context.Context, key string) (string, error) {
	_, ok := s.cache.Get(key)
	if !ok {
		objectValue, _, objectOK := s.getObjectValue(key)
		if !objectOK {
			return "none", nil
		}

		switch objectValue.Type {
		case engine.TypeHash:
			return "hash", nil
		case engine.TypeList:
			return "list", nil
		case engine.TypeSet:
			return "set", nil
		case engine.TypeZSet:
			return "zset", nil
		default:
			return "none", nil
		}
	}

	return "string", nil
}

func (s *Store) Rename(_ context.Context, key, newkey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	value, ok := s.cache.GetCopy(key)
	if ok {
		ttl := s.ttlOrZero(key)
		s.cache.Delete(key)
		s.deleteObjectKey(newkey)
		return s.cache.Set(newkey, value, ttl)
	}

	_, entry, objectOK := s.getObjectValue(key)
	if !objectOK {
		return errors.ErrKeyNotFound
	}

	s.deleteObjectKey(key)
	s.cache.Delete(newkey)
	s.objects.SetEntry(newkey, entry)
	s.trackKey(newkey)
	return nil
}

func (s *Store) Expire(_ context.Context, key string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cache.Expire(key, ttl) {
		return true, nil
	}
	return s.objects.Expire(key, ttl), nil
}

func (s *Store) ExpireAt(_ context.Context, key string, t time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ttl := time.Until(t)
	if ttl < 0 {
		deleted := s.cache.Delete(key)
		if s.deleteObjectKey(key) {
			deleted = true
		}
		return deleted, nil
	}
	if s.cache.Expire(key, ttl) {
		return true, nil
	}
	return s.objects.Expire(key, ttl), nil
}

func (s *Store) TTL(_ context.Context, key string) (time.Duration, error) {
	ttl, ok := s.cache.TTL(key)
	if ok {
		return ttl, nil
	}
	ttl, ok = s.objects.TTL(key)
	if ok {
		return ttl, nil
	}
	return -2 * time.Second, nil
}

func (s *Store) Persist(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cache.Expire(key, 0) {
		return true, nil
	}
	return s.objects.Expire(key, 0), nil
}

func (s *Store) DBSize(_ context.Context) (int64, error) {
	return s.cache.Len() + s.objects.Len(), nil
}

func (s *Store) FlushDB(_ context.Context) error {
	s.cache.Clear()
	s.objects.Clear()
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
	if count <= 0 {
		count = 10
	}

	keys := s.cache.Keys(pattern)
	for _, key := range s.objects.Keys(pattern) {
		if !containsKey(keys, key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	start := int(cursor)
	if start >= len(keys) {
		return []string{}, 0, nil
	}
	end := start + count
	if end > len(keys) {
		end = len(keys)
	}

	nextCursor := uint64(0)
	if end < len(keys) {
		nextCursor = uint64(end)
	}

	result := make([]string, end-start)
	copy(result, keys[start:end])
	return result, nextCursor, nil
}

// SetSlotFunc sets the slot computation function for the slot index.
func (s *Store) SetSlotFunc(fn func(string) uint16) {
	s.cache.SetSlotFunc(fn)
}

// KeysInSlot returns up to count keys in the given slot.
func (s *Store) KeysInSlot(slot uint16, count int) []string {
	s.cache.slotMu.RLock()
	keySet, ok := s.cache.slotIndex[slot]
	if !ok || len(keySet) == 0 {
		s.cache.slotMu.RUnlock()
		return nil
	}
	keys := make([]string, 0, len(keySet))
	for key := range keySet {
		keys = append(keys, key)
	}
	s.cache.slotMu.RUnlock()

	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		if count > 0 && len(result) >= count {
			break
		}
		if s.cache.Exists(key) || s.hasObjectKey(key) {
			result = append(result, key)
		}
	}
	return result
}

// CountKeysInSlot returns the number of keys in the given slot.
func (s *Store) CountKeysInSlot(slot uint16) int {
	return len(s.KeysInSlot(slot, 0))
}

func (s *Store) getObjectValue(key string) (*typedValue, *Entry, bool) {
	entry, ok := s.objects.Get(key)
	if !ok {
		return nil, nil, false
	}

	value, ok := entry.Value.(*typedValue)
	if !ok {
		return nil, nil, false
	}

	return value, entry, true
}

func (s *Store) hasObjectKey(key string) bool {
	_, _, ok := s.getObjectValue(key)
	return ok
}

func (s *Store) deleteObjectKey(key string) bool {
	if s.objects.Del(key) > 0 {
		s.untrackKey(key)
		return true
	}
	return false
}

func (s *Store) removeObjectKey(key string) {
	_ = s.deleteObjectKey(key)
}

func (s *Store) trackKey(key string) {
	if s.cache.slotFunc == nil {
		return
	}
	slot := s.cache.slotFunc(key)
	s.cache.slotMu.Lock()
	if s.cache.slotIndex[slot] == nil {
		s.cache.slotIndex[slot] = make(map[string]struct{})
	}
	s.cache.slotIndex[slot][key] = struct{}{}
	s.cache.slotMu.Unlock()
}

func (s *Store) untrackKey(key string) {
	if s.cache.slotFunc == nil {
		return
	}
	slot := s.cache.slotFunc(key)
	s.cache.slotMu.Lock()
	if keys, ok := s.cache.slotIndex[slot]; ok {
		delete(keys, key)
		if len(keys) == 0 {
			delete(s.cache.slotIndex, slot)
		}
	}
	s.cache.slotMu.Unlock()
}

func (s *Store) ttlOrZero(key string) time.Duration {
	ttl, err := s.TTL(context.Background(), key)
	if err != nil || ttl < 0 {
		return 0
	}
	return ttl
}

func containsKey(keys []string, target string) bool {
	for _, key := range keys {
		if key == target {
			return true
		}
	}
	return false
}
