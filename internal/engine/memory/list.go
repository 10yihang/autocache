package memory

import (
	"context"

	"github.com/10yihang/autocache/internal/engine"
	"github.com/10yihang/autocache/pkg/errors"
)

func (s *Store) LPush(_ context.Context, key string, values ...interface{}) (int64, error) {
	return s.pushList(key, true, values...)
}

func (s *Store) RPush(_ context.Context, key string, values ...interface{}) (int64, error) {
	return s.pushList(key, false, values...)
}

func (s *Store) LPop(_ context.Context, key string) (string, error) {
	return s.popList(key, true)
}

func (s *Store) RPop(_ context.Context, key string) (string, error) {
	return s.popList(key, false)
}

func (s *Store) LRange(_ context.Context, key string, start, stop int64) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list, err := s.getListValue(key)
	if err != nil {
		if err == errors.ErrKeyNotFound {
			return []string{}, nil
		}
		return nil, err
	}

	startIdx, stopIdx, ok := normalizeRange(len(list), start, stop)
	if !ok {
		return []string{}, nil
	}

	result := make([]string, stopIdx-startIdx+1)
	copy(result, list[startIdx:stopIdx+1])
	return result, nil
}

func (s *Store) LLen(_ context.Context, key string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list, err := s.getListValue(key)
	if err != nil {
		if err == errors.ErrKeyNotFound {
			return 0, nil
		}
		return 0, err
	}
	return int64(len(list)), nil
}

func (s *Store) LIndex(_ context.Context, key string, index int64) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list, err := s.getListValue(key)
	if err != nil {
		return "", err
	}

	idx, ok := normalizeIndex(len(list), index)
	if !ok {
		return "", errors.ErrKeyNotFound
	}
	return list[idx], nil
}

func (s *Store) LSet(_ context.Context, key string, index int64, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	list, tv, err := s.getMutableListValue(key)
	if err != nil {
		return err
	}

	idx, ok := normalizeIndex(len(list), index)
	if !ok {
		return errors.ErrKeyNotFound
	}
	list[idx] = value
	tv.Value = list
	return nil
}

func (s *Store) LTrim(_ context.Context, key string, start, stop int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	list, tv, err := s.getMutableListValue(key)
	if err != nil {
		if err == errors.ErrKeyNotFound {
			return nil
		}
		return err
	}

	startIdx, stopIdx, ok := normalizeRange(len(list), start, stop)
	if !ok {
		s.deleteObjectKey(key)
		return nil
	}

	trimmed := append([]string(nil), list[startIdx:stopIdx+1]...)
	tv.Value = trimmed
	return nil
}

func (s *Store) pushList(key string, left bool, values ...interface{}) (int64, error) {
	if len(values) == 0 {
		return 0, errors.ErrInvalidArgs
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	list, tv, err := s.getMutableListValue(key)
	if err != nil && err != errors.ErrKeyNotFound {
		return 0, err
	}
	if err == errors.ErrKeyNotFound {
		list = []string{}
		tv = &typedValue{Type: engine.TypeList, Value: list}
		s.objects.Set(key, tv, 0)
		s.trackKey(key)
	}

	items := make([]string, 0, len(values))
	for _, value := range values {
		str, ok := value.(string)
		if !ok {
			return 0, errors.ErrInvalidArgs
		}
		items = append(items, str)
	}

	if left {
		for _, item := range items {
			list = append([]string{item}, list...)
		}
	} else {
		list = append(list, items...)
	}

	tv.Value = list
	return int64(len(list)), nil
}

func (s *Store) popList(key string, left bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	list, tv, err := s.getMutableListValue(key)
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "", errors.ErrKeyNotFound
	}

	var value string
	if left {
		value = list[0]
		list = append([]string(nil), list[1:]...)
	} else {
		value = list[len(list)-1]
		list = append([]string(nil), list[:len(list)-1]...)
	}

	if len(list) == 0 {
		s.deleteObjectKey(key)
	} else {
		tv.Value = list
	}

	return value, nil
}

func (s *Store) getListValue(key string) ([]string, error) {
	if s.cache.Exists(key) {
		return nil, errors.ErrWrongType
	}

	value, _, ok := s.getObjectValue(key)
	if !ok {
		return nil, errors.ErrKeyNotFound
	}
	if value.Type != engine.TypeList {
		return nil, errors.ErrWrongType
	}

	list, ok := value.Value.([]string)
	if !ok {
		return nil, errors.ErrWrongType
	}
	return list, nil
}

func (s *Store) getMutableListValue(key string) ([]string, *typedValue, error) {
	if s.cache.Exists(key) {
		return nil, nil, errors.ErrWrongType
	}

	value, _, ok := s.getObjectValue(key)
	if !ok {
		return nil, nil, errors.ErrKeyNotFound
	}
	if value.Type != engine.TypeList {
		return nil, nil, errors.ErrWrongType
	}

	list, ok := value.Value.([]string)
	if !ok {
		return nil, nil, errors.ErrWrongType
	}
	return list, value, nil
}

func normalizeIndex(length int, index int64) (int, bool) {
	if length == 0 {
		return 0, false
	}
	if index < 0 {
		index = int64(length) + index
	}
	if index < 0 || index >= int64(length) {
		return 0, false
	}
	return int(index), true
}

func normalizeRange(length int, start, stop int64) (int, int, bool) {
	if length == 0 {
		return 0, 0, false
	}

	if start < 0 {
		start = int64(length) + start
	}
	if stop < 0 {
		stop = int64(length) + stop
	}

	if start < 0 {
		start = 0
	}
	if stop >= int64(length) {
		stop = int64(length) - 1
	}
	if start > stop || start >= int64(length) {
		return 0, 0, false
	}

	return int(start), int(stop), true
}
