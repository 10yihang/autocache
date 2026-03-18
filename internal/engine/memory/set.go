package memory

import (
	"context"
	"sort"

	"github.com/10yihang/autocache/internal/engine"
	"github.com/10yihang/autocache/pkg/errors"
)

func (s *Store) SAdd(_ context.Context, key string, members ...interface{}) (int64, error) {
	if len(members) == 0 {
		return 0, errors.ErrInvalidArgs
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	set, tv, err := s.getMutableSetValue(key)
	if err != nil && err != errors.ErrKeyNotFound {
		return 0, err
	}
	if err == errors.ErrKeyNotFound {
		set = make(map[string]struct{})
		tv = &typedValue{Type: engine.TypeSet, Value: set}
		s.objects.Set(key, tv, 0)
		s.trackKey(key)
	}

	var added int64
	for _, member := range members {
		value, ok := member.(string)
		if !ok {
			return 0, errors.ErrInvalidArgs
		}
		if _, exists := set[value]; !exists {
			set[value] = struct{}{}
			added++
		}
	}
	tv.Value = set
	return added, nil
}

func (s *Store) SRem(_ context.Context, key string, members ...interface{}) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	set, tv, err := s.getMutableSetValue(key)
	if err != nil {
		if err == errors.ErrKeyNotFound {
			return 0, nil
		}
		return 0, err
	}

	var removed int64
	for _, member := range members {
		value, ok := member.(string)
		if !ok {
			return 0, errors.ErrInvalidArgs
		}
		if _, exists := set[value]; exists {
			delete(set, value)
			removed++
		}
	}
	if len(set) == 0 {
		s.deleteObjectKey(key)
	} else {
		tv.Value = set
	}
	return removed, nil
}

func (s *Store) SMembers(_ context.Context, key string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	set, err := s.getSetValue(key)
	if err != nil {
		if err == errors.ErrKeyNotFound {
			return []string{}, nil
		}
		return nil, err
	}

	result := make([]string, 0, len(set))
	for member := range set {
		result = append(result, member)
	}
	sort.Strings(result)
	return result, nil
}

func (s *Store) SIsMember(_ context.Context, key, member string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	set, err := s.getSetValue(key)
	if err != nil {
		if err == errors.ErrKeyNotFound {
			return false, nil
		}
		return false, err
	}
	_, exists := set[member]
	return exists, nil
}

func (s *Store) SCard(_ context.Context, key string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	set, err := s.getSetValue(key)
	if err != nil {
		if err == errors.ErrKeyNotFound {
			return 0, nil
		}
		return 0, err
	}
	return int64(len(set)), nil
}

func (s *Store) SPop(_ context.Context, key string) (string, error) {
	return "", errors.ErrKeyNotFound
}

func (s *Store) SRandMember(_ context.Context, key string, count int64) ([]string, error) {
	return nil, engine.ErrNotSupported
}

func (s *Store) SUnion(_ context.Context, keys ...string) ([]string, error) {
	return nil, engine.ErrNotSupported
}

func (s *Store) SInter(_ context.Context, keys ...string) ([]string, error) {
	return nil, engine.ErrNotSupported
}

func (s *Store) SDiff(_ context.Context, keys ...string) ([]string, error) {
	return nil, engine.ErrNotSupported
}

func (s *Store) getSetValue(key string) (map[string]struct{}, error) {
	if s.cache.Exists(key) {
		return nil, errors.ErrWrongType
	}
	value, _, ok := s.getObjectValue(key)
	if !ok {
		return nil, errors.ErrKeyNotFound
	}
	if value.Type != engine.TypeSet {
		return nil, errors.ErrWrongType
	}
	set, ok := value.Value.(map[string]struct{})
	if !ok {
		return nil, errors.ErrWrongType
	}
	return set, nil
}

func (s *Store) getMutableSetValue(key string) (map[string]struct{}, *typedValue, error) {
	if s.cache.Exists(key) {
		return nil, nil, errors.ErrWrongType
	}
	value, _, ok := s.getObjectValue(key)
	if !ok {
		return nil, nil, errors.ErrKeyNotFound
	}
	if value.Type != engine.TypeSet {
		return nil, nil, errors.ErrWrongType
	}
	set, ok := value.Value.(map[string]struct{})
	if !ok {
		return nil, nil, errors.ErrWrongType
	}
	return set, value, nil
}
