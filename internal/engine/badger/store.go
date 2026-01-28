package badger

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"time"

	"github.com/10yihang/autocache/internal/engine"
	"github.com/dgraph-io/badger/v4"
)

// Store implements engine.Engine using BadgerDB
type Store struct {
	db *badger.DB
}

// NewStore creates a new BadgerDB store
func NewStore(path string) (*Store, error) {
	opts := badger.DefaultOptions(path)
	// Optimize for SSD
	opts.BlockCacheSize = 256 << 20 // 256MB
	opts.IndexCacheSize = 256 << 20 // 256MB

	// Open DB
	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open badger: %w", err)
	}

	return &Store{db: db}, nil
}

// Get gets a key
func (s *Store) Get(ctx context.Context, key string) (*engine.Entry, error) {
	var entry engine.Entry

	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}

		// Check expiry
		if item.ExpiresAt() > 0 && item.ExpiresAt() < uint64(time.Now().Unix()) {
			return badger.ErrKeyNotFound
		}

		return item.Value(func(val []byte) error {
			return gob.NewDecoder(bytes.NewReader(val)).Decode(&entry)
		})
	})

	if err != nil {
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil, engine.ErrKeyNotFound
		}
		return nil, err
	}

	return &entry, nil
}

// Set sets a key
func (s *Store) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	var buf bytes.Buffer

	// Create entry
	now := time.Now()
	entry := engine.Entry{
		Key:       key,
		Value:     value,
		Type:      engine.TypeString, // Default to string for now
		CreatedAt: now,
		UpdatedAt: now,
	}

	if ttl > 0 {
		entry.ExpireAt = now.Add(ttl)
	}

	// Serialize
	if err := gob.NewEncoder(&buf).Encode(entry); err != nil {
		return fmt.Errorf("encode entry: %w", err)
	}

	return s.db.Update(func(txn *badger.Txn) error {
		e := badger.NewEntry([]byte(key), buf.Bytes())
		if ttl > 0 {
			e.WithTTL(ttl)
		}
		return txn.SetEntry(e)
	})
}

// Del deletes keys
func (s *Store) Del(ctx context.Context, keys ...string) (int64, error) {
	var count int64

	err := s.db.Update(func(txn *badger.Txn) error {
		for _, key := range keys {
			// Check if exists first to return correct count
			_, err := txn.Get([]byte(key))
			if err != nil {
				if errors.Is(err, badger.ErrKeyNotFound) {
					continue
				}
				return err
			}

			if err := txn.Delete([]byte(key)); err == nil {
				count++
			}
		}
		return nil
	})

	return count, err
}

// Exists checks if keys exist
func (s *Store) Exists(ctx context.Context, keys ...string) (int64, error) {
	var count int64

	err := s.db.View(func(txn *badger.Txn) error {
		for _, key := range keys {
			_, err := txn.Get([]byte(key))
			if err == nil {
				count++
			}
		}
		return nil
	})

	return count, err
}

// Keys returns all keys matching pattern
func (s *Store) Keys(ctx context.Context, pattern string) ([]string, error) {
	var keys []string

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			k := string(item.Key())
			keys = append(keys, k)
		}
		return nil
	})

	return keys, err
}

// Type returns key type
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

// Rename renames a key
func (s *Store) Rename(ctx context.Context, key, newkey string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}

		valCopy, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}

		// Set new key
		e := badger.NewEntry([]byte(newkey), valCopy)
		if item.ExpiresAt() > 0 {
			ttl := time.Until(time.Unix(int64(item.ExpiresAt()), 0))
			if ttl > 0 {
				e.WithTTL(ttl)
			}
		}
		if err := txn.SetEntry(e); err != nil {
			return err
		}

		// Delete old key
		return txn.Delete([]byte(key))
	})
}

// Expire sets expiration
func (s *Store) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	var found bool
	err := s.db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return nil
			}
			return err
		}
		found = true

		valCopy, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}

		e := badger.NewEntry([]byte(key), valCopy).WithTTL(ttl)
		return txn.SetEntry(e)
	})
	return found, err
}

// ExpireAt sets expiration at time
func (s *Store) ExpireAt(ctx context.Context, key string, t time.Time) (bool, error) {
	return s.Expire(ctx, key, time.Until(t))
}

// TTL gets ttl
func (s *Store) TTL(ctx context.Context, key string) (time.Duration, error) {
	var ttl time.Duration
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}

		if item.ExpiresAt() == 0 {
			ttl = -1
			return nil
		}

		ttl = time.Until(time.Unix(int64(item.ExpiresAt()), 0))
		if ttl < 0 {
			return badger.ErrKeyNotFound
		}
		return nil
	})

	if errors.Is(err, badger.ErrKeyNotFound) {
		return -2, engine.ErrKeyNotFound
	}
	return ttl, err
}

// Persist removes expiration
func (s *Store) Persist(ctx context.Context, key string) (bool, error) {
	var found bool
	err := s.db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return nil
			}
			return err
		}

		if item.ExpiresAt() == 0 {
			return nil
		}

		found = true
		valCopy, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}

		// Re-set without TTL
		return txn.SetEntry(badger.NewEntry([]byte(key), valCopy))
	})
	return found, err
}

// DBSize returns size
func (s *Store) DBSize(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			count++
		}
		return nil
	})
	return count, err
}

// FlushDB clears db
func (s *Store) FlushDB(ctx context.Context) error {
	return s.db.DropAll()
}

// Close closes db
func (s *Store) Close() error {
	return s.db.Close()
}
