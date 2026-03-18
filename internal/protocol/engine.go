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

	HGet(ctx context.Context, key, field string) (string, error)
	HSet(ctx context.Context, key string, pairs ...interface{}) (int64, error)
	HDel(ctx context.Context, key string, fields ...string) (int64, error)
	HExists(ctx context.Context, key, field string) (bool, error)
	HGetAll(ctx context.Context, key string) (map[string]string, error)
	HKeys(ctx context.Context, key string) ([]string, error)
	HVals(ctx context.Context, key string) ([]string, error)
	HLen(ctx context.Context, key string) (int64, error)
	LPush(ctx context.Context, key string, values ...interface{}) (int64, error)
	RPush(ctx context.Context, key string, values ...interface{}) (int64, error)
	LPop(ctx context.Context, key string) (string, error)
	RPop(ctx context.Context, key string) (string, error)
	LRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	LLen(ctx context.Context, key string) (int64, error)
	LIndex(ctx context.Context, key string, index int64) (string, error)
	LSet(ctx context.Context, key string, index int64, value string) error
	LTrim(ctx context.Context, key string, start, stop int64) error
	SAdd(ctx context.Context, key string, members ...interface{}) (int64, error)
	SRem(ctx context.Context, key string, members ...interface{}) (int64, error)
	SMembers(ctx context.Context, key string) ([]string, error)
	SIsMember(ctx context.Context, key, member string) (bool, error)
	SCard(ctx context.Context, key string) (int64, error)
	ZAdd(ctx context.Context, key string, members ...engine.ZMember) (int64, error)
	ZRem(ctx context.Context, key string, members ...string) (int64, error)
	ZRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	ZScore(ctx context.Context, key, member string) (float64, error)
	ZRank(ctx context.Context, key, member string) (int64, error)
	ZCard(ctx context.Context, key string) (int64, error)

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
	RestoreEntry(ctx context.Context, key string, valueType engine.ValueType, payload []byte, ttl time.Duration) error

	// Stats
	GetStats() interface{}

	// Lifecycle
	Close() error

	// Slot operations (for CLUSTER GETKEYSINSLOT / COUNTKEYSINSLOT)
	KeysInSlot(slot uint16, count int) []string
	CountKeysInSlot(slot uint16) int
}
