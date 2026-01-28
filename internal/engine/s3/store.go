package s3

import (
	"context"
	"time"

	"github.com/10yihang/autocache/internal/engine"
)

// Store implements engine.Engine using S3/MinIO
type Store struct {
	endpoint string
	bucket   string
}

// NewStore creates a new S3 store
func NewStore(endpoint, bucket string) (*Store, error) {
	return &Store{
		endpoint: endpoint,
		bucket:   bucket,
	}, nil
}

// Get gets a key
func (s *Store) Get(ctx context.Context, key string) (*engine.Entry, error) {
	return nil, engine.ErrKeyNotFound
}

// Set sets a key
func (s *Store) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return nil
}

// Del deletes keys
func (s *Store) Del(ctx context.Context, keys ...string) (int64, error) {
	return 0, nil
}

// Exists checks if keys exist
func (s *Store) Exists(ctx context.Context, keys ...string) (int64, error) {
	return 0, nil
}

// Keys returns all keys matching pattern
func (s *Store) Keys(ctx context.Context, pattern string) ([]string, error) {
	return []string{}, nil
}

// Type returns key type
func (s *Store) Type(ctx context.Context, key string) (string, error) {
	return "none", nil
}

// Rename renames a key
func (s *Store) Rename(ctx context.Context, key, newkey string) error {
	return engine.ErrKeyNotFound
}

// Expire sets expiration
func (s *Store) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return false, nil
}

// ExpireAt sets expiration at time
func (s *Store) ExpireAt(ctx context.Context, key string, t time.Time) (bool, error) {
	return false, nil
}

// TTL gets ttl
func (s *Store) TTL(ctx context.Context, key string) (time.Duration, error) {
	return -2, engine.ErrKeyNotFound
}

// Persist removes expiration
func (s *Store) Persist(ctx context.Context, key string) (bool, error) {
	return false, nil
}

// DBSize returns size
func (s *Store) DBSize(ctx context.Context) (int64, error) {
	return 0, nil
}

// FlushDB clears db
func (s *Store) FlushDB(ctx context.Context) error {
	return nil
}

// Close closes db
func (s *Store) Close() error {
	return nil
}
