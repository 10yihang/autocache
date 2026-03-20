package nokv

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	NoKV "github.com/feichai0017/NoKV"
	nokvutils "github.com/feichai0017/NoKV/utils"

	"github.com/10yihang/autocache/internal/engine"
)

type Store struct {
	db     *NoKV.DB
	path   string
	mu     sync.Mutex
	closed bool
}

func NewStore(path string) (*Store, error) {
	db, err := openDB(path)
	if err != nil {
		return nil, err
	}

	return &Store{db: db, path: path}, nil
}

func openDB(path string) (*NoKV.DB, error) {
	opt := NoKV.NewDefaultOptions()
	opt.WorkDir = path

	db := NoKV.Open(opt)
	if db == nil {
		return nil, fmt.Errorf("open nokv at %q: returned nil db", path)
	}

	return db, nil
}

func (s *Store) Get(ctx context.Context, key string) (*engine.Entry, error) {
	return s.getEntry(key)
}

func (s *Store) getEntry(key string) (*engine.Entry, error) {
	rawEntry, err := s.db.Get([]byte(key))
	if err != nil {
		return nil, mapError(err)
	}

	entry, err := decodeEntry(rawEntry.Value)
	if err != nil {
		return nil, err
	}
	if entry.ExpireAt.IsZero() && rawEntry.ExpiresAt > 0 {
		entry.ExpireAt = time.Unix(int64(rawEntry.ExpiresAt), 0)
	}

	return entry, nil
}

func (s *Store) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	now := time.Now()
	entry := engine.Entry{
		Key:       key,
		Value:     value,
		Type:      inferValueType(value),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if ttl > 0 {
		entry.ExpireAt = now.Add(ttl)
	}

	return s.writeEntry(entry)
}

func (s *Store) writeEntry(entry engine.Entry) error {
	encoded, err := encodeEntry(entry)
	if err != nil {
		return err
	}

	if !entry.ExpireAt.IsZero() {
		return mapError(s.db.SetWithTTL([]byte(entry.Key), encoded, uint64(entry.ExpireAt.Unix())))
	}

	return mapError(s.db.Set([]byte(entry.Key), encoded))
}

func (s *Store) Del(ctx context.Context, keys ...string) (int64, error) {
	var deleted int64
	for _, key := range keys {
		if _, err := s.Get(ctx, key); err != nil {
			if errors.Is(err, engine.ErrKeyNotFound) {
				continue
			}
			return deleted, err
		}
		if err := mapError(s.db.Del([]byte(key))); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func (s *Store) Exists(ctx context.Context, keys ...string) (int64, error) {
	var count int64
	for _, key := range keys {
		if _, err := s.Get(ctx, key); err != nil {
			if errors.Is(err, engine.ErrKeyNotFound) {
				continue
			}
			return count, err
		}
		count++
	}
	return count, nil
}

func (s *Store) Keys(ctx context.Context, pattern string) ([]string, error) {
	it := s.db.NewIterator(&nokvutils.Options{IsAsc: true, OnlyUseKey: true})
	defer it.Close()

	keys := make([]string, 0)
	for it.Rewind(); it.Valid(); it.Next() {
		item := it.Item()
		if item == nil || item.Entry() == nil {
			continue
		}
		key := string(item.Entry().Key)
		matched, err := filepath.Match(pattern, key)
		if err != nil {
			return nil, fmt.Errorf("match key pattern %q: %w", pattern, err)
		}
		if matched {
			keys = append(keys, key)
		}
	}

	return keys, nil
}

func (s *Store) Type(ctx context.Context, key string) (string, error) {
	entry, err := s.Get(ctx, key)
	if err != nil {
		return "", err
	}

	switch entry.Type {
	case engine.TypeString:
		return "string", nil
	case engine.TypeList:
		return "list", nil
	case engine.TypeSet:
		return "set", nil
	case engine.TypeZSet:
		return "zset", nil
	case engine.TypeHash:
		return "hash", nil
	default:
		return "unknown", nil
	}
}

func (s *Store) Rename(ctx context.Context, key, newkey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, err := s.getEntry(key)
	if err != nil {
		return err
	}
	entry.Key = newkey
	if err := s.writeEntry(*entry); err != nil {
		return err
	}
	return mapError(s.db.Del([]byte(key)))
}

func (s *Store) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, err := s.getEntry(key)
	if err != nil {
		if errors.Is(err, engine.ErrKeyNotFound) {
			return false, nil
		}
		return false, err
	}
	entry.ExpireAt = time.Now().Add(ttl)
	entry.UpdatedAt = time.Now()
	if err := s.writeEntry(*entry); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) ExpireAt(ctx context.Context, key string, t time.Time) (bool, error) {
	return s.Expire(ctx, key, time.Until(t))
}

func (s *Store) TTL(ctx context.Context, key string) (time.Duration, error) {
	entry, err := s.Get(ctx, key)
	if err != nil {
		if errors.Is(err, engine.ErrKeyNotFound) {
			return -2, engine.ErrKeyNotFound
		}
		return 0, err
	}
	if entry.ExpireAt.IsZero() {
		return -1, nil
	}

	ttl := time.Until(entry.ExpireAt)
	if ttl <= 0 {
		return -2, engine.ErrKeyNotFound
	}

	return ttl, nil
}

func (s *Store) Persist(ctx context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, err := s.getEntry(key)
	if err != nil {
		if errors.Is(err, engine.ErrKeyNotFound) {
			return false, nil
		}
		return false, err
	}
	if entry.ExpireAt.IsZero() {
		return false, nil
	}
	entry.ExpireAt = time.Time{}
	entry.UpdatedAt = time.Now()
	if err := s.writeEntry(*entry); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) DBSize(ctx context.Context) (int64, error) {
	it := s.db.NewIterator(&nokvutils.Options{IsAsc: true, OnlyUseKey: true})
	defer it.Close()

	var count int64
	for it.Rewind(); it.Valid(); it.Next() {
		item := it.Item()
		if item == nil || item.Entry() == nil {
			continue
		}
		count++
	}

	return count, nil
}

func (s *Store) FlushDB(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		if err := s.db.Close(); err != nil {
			return fmt.Errorf("close nokv for flush: %w", err)
		}
	}
	if err := os.RemoveAll(s.path); err != nil {
		return fmt.Errorf("remove nokv data dir: %w", err)
	}
	if err := os.MkdirAll(s.path, 0o755); err != nil {
		return fmt.Errorf("recreate nokv data dir: %w", err)
	}

	db, err := openDB(s.path)
	if err != nil {
		return err
	}
	s.db = db
	s.closed = false
	return nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	if s.db == nil {
		s.closed = true
		return nil
	}

	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close nokv: %w", err)
	}

	s.closed = true
	return nil
}

func encodeEntry(entry engine.Entry) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(entry); err != nil {
		return nil, fmt.Errorf("encode entry: %w", err)
	}
	return buf.Bytes(), nil
}

func decodeEntry(data []byte) (*engine.Entry, error) {
	var entry engine.Entry
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&entry); err != nil {
		return nil, fmt.Errorf("decode entry: %w", err)
	}
	return &entry, nil
}

func inferValueType(value interface{}) engine.ValueType {
	switch value.(type) {
	case string, []byte:
		return engine.TypeString
	case map[string]string:
		return engine.TypeHash
	case []string:
		return engine.TypeList
	case map[string]struct{}:
		return engine.TypeSet
	case map[string]float64:
		return engine.TypeZSet
	default:
		return engine.TypeString
	}
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, nokvutils.ErrKeyNotFound) {
		return engine.ErrKeyNotFound
	}
	return err
}
