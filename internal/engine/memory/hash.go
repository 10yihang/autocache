package memory

import (
	"context"
	"sort"

	"github.com/10yihang/autocache/internal/engine"
	"github.com/10yihang/autocache/pkg/errors"
)

type typedValue struct {
	Type  engine.ValueType
	Value any
}

func (s *Store) HSet(_ context.Context, key string, pairs ...interface{}) (int64, error) {
	if len(pairs) == 0 || len(pairs)%2 != 0 {
		return 0, errors.ErrInvalidArgs
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cache.Exists(key) {
		return 0, errors.ErrWrongType
	}

	value, _, ok := s.getObjectValue(key)
	if ok && value.Type != engine.TypeHash {
		return 0, errors.ErrWrongType
	}

	var hash map[string]string
	if ok {
		hash = value.Value.(map[string]string)
	} else {
		hash = make(map[string]string)
		s.objects.Set(key, &typedValue{Type: engine.TypeHash, Value: hash}, 0)
		s.trackKey(key)
	}

	var added int64
	for i := 0; i < len(pairs); i += 2 {
		field, ok := pairs[i].(string)
		if !ok {
			return 0, errors.ErrInvalidArgs
		}
		val, ok := pairs[i+1].(string)
		if !ok {
			return 0, errors.ErrInvalidArgs
		}
		if _, exists := hash[field]; !exists {
			added++
		}
		hash[field] = val
	}

	return added, nil
}

func (s *Store) HGet(_ context.Context, key, field string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cache.Exists(key) {
		return "", errors.ErrWrongType
	}

	value, _, ok := s.getObjectValue(key)
	if !ok {
		return "", errors.ErrKeyNotFound
	}
	if value.Type != engine.TypeHash {
		return "", errors.ErrWrongType
	}

	hash := value.Value.(map[string]string)
	fieldValue, exists := hash[field]
	if !exists {
		return "", errors.ErrKeyNotFound
	}

	return fieldValue, nil
}

func (s *Store) HDel(_ context.Context, key string, fields ...string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cache.Exists(key) {
		return 0, errors.ErrWrongType
	}

	value, _, ok := s.getObjectValue(key)
	if !ok {
		return 0, nil
	}
	if value.Type != engine.TypeHash {
		return 0, errors.ErrWrongType
	}

	hash := value.Value.(map[string]string)
	var deleted int64
	for _, field := range fields {
		if _, exists := hash[field]; exists {
			delete(hash, field)
			deleted++
		}
	}
	if len(hash) == 0 {
		s.deleteObjectKey(key)
	}

	return deleted, nil
}

func (s *Store) HExists(_ context.Context, key, field string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cache.Exists(key) {
		return false, errors.ErrWrongType
	}

	value, _, ok := s.getObjectValue(key)
	if !ok {
		return false, nil
	}
	if value.Type != engine.TypeHash {
		return false, errors.ErrWrongType
	}

	_, exists := value.Value.(map[string]string)[field]
	return exists, nil
}

func (s *Store) HGetAll(_ context.Context, key string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cache.Exists(key) {
		return nil, errors.ErrWrongType
	}

	value, _, ok := s.getObjectValue(key)
	if !ok {
		return map[string]string{}, nil
	}
	if value.Type != engine.TypeHash {
		return nil, errors.ErrWrongType
	}

	hash := value.Value.(map[string]string)
	result := make(map[string]string, len(hash))
	for field, val := range hash {
		result[field] = val
	}
	return result, nil
}

func (s *Store) HKeys(ctx context.Context, key string) ([]string, error) {
	values, err := s.HGetAll(ctx, key)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(values))
	for field := range values {
		keys = append(keys, field)
	}
	sort.Strings(keys)
	return keys, nil
}

func (s *Store) HVals(ctx context.Context, key string) ([]string, error) {
	values, err := s.HGetAll(ctx, key)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(values))
	for field := range values {
		keys = append(keys, field)
	}
	sort.Strings(keys)

	result := make([]string, 0, len(keys))
	for _, field := range keys {
		result = append(result, values[field])
	}
	return result, nil
}

func (s *Store) HLen(_ context.Context, key string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cache.Exists(key) {
		return 0, errors.ErrWrongType
	}

	value, _, ok := s.getObjectValue(key)
	if !ok {
		return 0, nil
	}
	if value.Type != engine.TypeHash {
		return 0, errors.ErrWrongType
	}

	return int64(len(value.Value.(map[string]string))), nil
}
