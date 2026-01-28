package protocol

import (
	"context"
	"time"

	"github.com/10yihang/autocache/internal/engine"
)

// ProtocolEngine defines the interface required by the protocol layer.
// It is a superset of operations needed for RESP command handling.
type ProtocolEngine interface {
	// String operations
	GetBytes(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)
	SetXX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)
	GetSet(ctx context.Context, key string, value string) (string, error)
	Incr(ctx context.Context, key string) (int64, error)
	IncrBy(ctx context.Context, key string, delta int64) (int64, error)
	Decr(ctx context.Context, key string) (int64, error)
	DecrBy(ctx context.Context, key string, delta int64) (int64, error)
	Append(ctx context.Context, key string, value string) (int64, error)
	Strlen(ctx context.Context, key string) (int64, error)
	MGetBytes(ctx context.Context, keys ...string) ([][]byte, error)
	MSet(ctx context.Context, pairs ...interface{}) error

	// Key operations
	Del(ctx context.Context, keys ...string) (int64, error)
	Exists(ctx context.Context, keys ...string) (int64, error)
	Keys(ctx context.Context, pattern string) ([]string, error)
	Type(ctx context.Context, key string) (string, error)
	Rename(ctx context.Context, key, newkey string) error
	Scan(ctx context.Context, cursor uint64, pattern string, count int) ([]string, uint64, error)

	// TTL operations
	TTL(ctx context.Context, key string) (time.Duration, error)
	Expire(ctx context.Context, key string, ttl time.Duration) (bool, error)
	ExpireAt(ctx context.Context, key string, t time.Time) (bool, error)
	Persist(ctx context.Context, key string) (bool, error)

	// Database operations
	DBSize(ctx context.Context) (int64, error)
	FlushDB(ctx context.Context) error

	// Entry access for advanced operations
	GetEntry(ctx context.Context, key string) (*engine.Entry, error)

	// Stats
	GetStats() interface{}

	// Lifecycle
	Close() error
}
