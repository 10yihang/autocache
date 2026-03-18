package memory

import (
	"context"
	"sort"
	"strconv"

	"github.com/10yihang/autocache/internal/engine"
	"github.com/10yihang/autocache/pkg/errors"
)

func (s *Store) ZAdd(_ context.Context, key string, members ...engine.ZMember) (int64, error) {
	if len(members) == 0 {
		return 0, errors.ErrInvalidArgs
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	zset, tv, err := s.getMutableZSetValue(key)
	if err != nil && err != errors.ErrKeyNotFound {
		return 0, err
	}
	if err == errors.ErrKeyNotFound {
		zset = make(map[string]float64)
		tv = &typedValue{Type: engine.TypeZSet, Value: zset}
		s.objects.Set(key, tv, 0)
		s.trackKey(key)
	}

	var added int64
	for _, member := range members {
		if _, exists := zset[member.Member]; !exists {
			added++
		}
		zset[member.Member] = member.Score
	}
	tv.Value = zset
	return added, nil
}

func (s *Store) ZRem(_ context.Context, key string, members ...string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	zset, tv, err := s.getMutableZSetValue(key)
	if err != nil {
		if err == errors.ErrKeyNotFound {
			return 0, nil
		}
		return 0, err
	}

	var removed int64
	for _, member := range members {
		if _, exists := zset[member]; exists {
			delete(zset, member)
			removed++
		}
	}
	if len(zset) == 0 {
		s.deleteObjectKey(key)
	} else {
		tv.Value = zset
	}
	return removed, nil
}

func (s *Store) ZRange(_ context.Context, key string, start, stop int64) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := s.sortedZMembers(key)
	if err != nil {
		if err == errors.ErrKeyNotFound {
			return []string{}, nil
		}
		return nil, err
	}

	startIdx, stopIdx, ok := normalizeRange(len(entries), start, stop)
	if !ok {
		return []string{}, nil
	}

	result := make([]string, 0, stopIdx-startIdx+1)
	for _, entry := range entries[startIdx : stopIdx+1] {
		result = append(result, entry.Member)
	}
	return result, nil
}

func (s *Store) ZRangeWithScores(_ context.Context, key string, start, stop int64) ([]engine.ZMember, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := s.sortedZMembers(key)
	if err != nil {
		if err == errors.ErrKeyNotFound {
			return []engine.ZMember{}, nil
		}
		return nil, err
	}

	startIdx, stopIdx, ok := normalizeRange(len(entries), start, stop)
	if !ok {
		return []engine.ZMember{}, nil
	}

	result := make([]engine.ZMember, stopIdx-startIdx+1)
	copy(result, entries[startIdx:stopIdx+1])
	return result, nil
}

func (s *Store) ZRevRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	entries, err := s.ZRangeWithScores(ctx, key, 0, -1)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	startIdx, stopIdx, ok := normalizeRange(len(entries), start, stop)
	if !ok {
		return []string{}, nil
	}
	result := make([]string, 0, stopIdx-startIdx+1)
	for _, entry := range entries[startIdx : stopIdx+1] {
		result = append(result, entry.Member)
	}
	return result, nil
}

func (s *Store) ZScore(_ context.Context, key, member string) (float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	zset, err := s.getZSetValue(key)
	if err != nil {
		return 0, err
	}
	score, exists := zset[member]
	if !exists {
		return 0, errors.ErrKeyNotFound
	}
	return score, nil
}

func (s *Store) ZRank(_ context.Context, key, member string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := s.sortedZMembers(key)
	if err != nil {
		return 0, err
	}
	for idx, entry := range entries {
		if entry.Member == member {
			return int64(idx), nil
		}
	}
	return 0, errors.ErrKeyNotFound
}

func (s *Store) ZCard(_ context.Context, key string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	zset, err := s.getZSetValue(key)
	if err != nil {
		if err == errors.ErrKeyNotFound {
			return 0, nil
		}
		return 0, err
	}
	return int64(len(zset)), nil
}

func (s *Store) ZCount(_ context.Context, key string, min, max float64) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	zset, err := s.getZSetValue(key)
	if err != nil {
		if err == errors.ErrKeyNotFound {
			return 0, nil
		}
		return 0, err
	}
	var count int64
	for _, score := range zset {
		if score >= min && score <= max {
			count++
		}
	}
	return count, nil
}

func (s *Store) ZIncrBy(ctx context.Context, key, member string, delta float64) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	zset, tv, err := s.getMutableZSetValue(key)
	if err != nil && err != errors.ErrKeyNotFound {
		return 0, err
	}
	if err == errors.ErrKeyNotFound {
		zset = make(map[string]float64)
		tv = &typedValue{Type: engine.TypeZSet, Value: zset}
		s.objects.Set(key, tv, 0)
		s.trackKey(key)
	}
	zset[member] += delta
	tv.Value = zset
	return zset[member], nil
}

func (s *Store) getZSetValue(key string) (map[string]float64, error) {
	if s.cache.Exists(key) {
		return nil, errors.ErrWrongType
	}
	value, _, ok := s.getObjectValue(key)
	if !ok {
		return nil, errors.ErrKeyNotFound
	}
	if value.Type != engine.TypeZSet {
		return nil, errors.ErrWrongType
	}
	zset, ok := value.Value.(map[string]float64)
	if !ok {
		return nil, errors.ErrWrongType
	}
	return zset, nil
}

func (s *Store) getMutableZSetValue(key string) (map[string]float64, *typedValue, error) {
	if s.cache.Exists(key) {
		return nil, nil, errors.ErrWrongType
	}
	value, _, ok := s.getObjectValue(key)
	if !ok {
		return nil, nil, errors.ErrKeyNotFound
	}
	if value.Type != engine.TypeZSet {
		return nil, nil, errors.ErrWrongType
	}
	zset, ok := value.Value.(map[string]float64)
	if !ok {
		return nil, nil, errors.ErrWrongType
	}
	return zset, value, nil
}

func (s *Store) sortedZMembers(key string) ([]engine.ZMember, error) {
	zset, err := s.getZSetValue(key)
	if err != nil {
		return nil, err
	}
	entries := make([]engine.ZMember, 0, len(zset))
	for member, score := range zset {
		entries = append(entries, engine.ZMember{Score: score, Member: member})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Score == entries[j].Score {
			return entries[i].Member < entries[j].Member
		}
		return entries[i].Score < entries[j].Score
	})
	return entries, nil
}

func formatZScore(score float64) string {
	return strconv.FormatFloat(score, 'f', -1, 64)
}
