// Package engine defines the core storage engine interfaces.
package engine

import (
	"context"
	"errors"
	"time"
)

// Common errors
var (
	ErrKeyNotFound  = errors.New("key not found")
	ErrNotSupported = errors.New("operation not supported")
)

// ValueType represents the type of value stored.
type ValueType int

const (
	TypeString ValueType = iota
	TypeList
	TypeSet
	TypeZSet
	TypeHash
)

// Entry represents a key-value entry with metadata.
type Entry struct {
	Key       string
	Value     interface{}
	Type      ValueType
	ExpireAt  time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Engine is the core storage engine interface.
type Engine interface {
	Get(ctx context.Context, key string) (*Entry, error)
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) (int64, error)
	Exists(ctx context.Context, keys ...string) (int64, error)

	Keys(ctx context.Context, pattern string) ([]string, error)
	Type(ctx context.Context, key string) (string, error)
	Rename(ctx context.Context, key, newkey string) error

	Expire(ctx context.Context, key string, ttl time.Duration) (bool, error)
	ExpireAt(ctx context.Context, key string, t time.Time) (bool, error)
	TTL(ctx context.Context, key string) (time.Duration, error)
	Persist(ctx context.Context, key string) (bool, error)

	DBSize(ctx context.Context) (int64, error)
	FlushDB(ctx context.Context) error

	Close() error
}

// StringEngine defines string type operations.
type StringEngine interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)
	SetXX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)
	GetSet(ctx context.Context, key string, value string) (string, error)
	Incr(ctx context.Context, key string) (int64, error)
	IncrBy(ctx context.Context, key string, delta int64) (int64, error)
	IncrByFloat(ctx context.Context, key string, delta float64) (float64, error)
	Decr(ctx context.Context, key string) (int64, error)
	DecrBy(ctx context.Context, key string, delta int64) (int64, error)
	Append(ctx context.Context, key string, value string) (int64, error)
	Strlen(ctx context.Context, key string) (int64, error)
	MGet(ctx context.Context, keys ...string) ([]interface{}, error)
	MSet(ctx context.Context, pairs ...interface{}) error
}

// HashEngine defines hash type operations.
type HashEngine interface {
	HGet(ctx context.Context, key, field string) (string, error)
	HSet(ctx context.Context, key string, pairs ...interface{}) (int64, error)
	HDel(ctx context.Context, key string, fields ...string) (int64, error)
	HExists(ctx context.Context, key, field string) (bool, error)
	HGetAll(ctx context.Context, key string) (map[string]string, error)
	HKeys(ctx context.Context, key string) ([]string, error)
	HVals(ctx context.Context, key string) ([]string, error)
	HLen(ctx context.Context, key string) (int64, error)
	HIncrBy(ctx context.Context, key, field string, delta int64) (int64, error)
	HIncrByFloat(ctx context.Context, key, field string, delta float64) (float64, error)
}

// ListEngine defines list type operations.
type ListEngine interface {
	LPush(ctx context.Context, key string, values ...interface{}) (int64, error)
	RPush(ctx context.Context, key string, values ...interface{}) (int64, error)
	LPop(ctx context.Context, key string) (string, error)
	RPop(ctx context.Context, key string) (string, error)
	LRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	LLen(ctx context.Context, key string) (int64, error)
	LIndex(ctx context.Context, key string, index int64) (string, error)
	LSet(ctx context.Context, key string, index int64, value string) error
	LTrim(ctx context.Context, key string, start, stop int64) error
}

// SetEngine defines set type operations.
type SetEngine interface {
	SAdd(ctx context.Context, key string, members ...interface{}) (int64, error)
	SRem(ctx context.Context, key string, members ...interface{}) (int64, error)
	SMembers(ctx context.Context, key string) ([]string, error)
	SIsMember(ctx context.Context, key, member string) (bool, error)
	SCard(ctx context.Context, key string) (int64, error)
	SPop(ctx context.Context, key string) (string, error)
	SRandMember(ctx context.Context, key string, count int64) ([]string, error)
	SUnion(ctx context.Context, keys ...string) ([]string, error)
	SInter(ctx context.Context, keys ...string) ([]string, error)
	SDiff(ctx context.Context, keys ...string) ([]string, error)
}

// ZSetEngine defines sorted set type operations.
type ZSetEngine interface {
	ZAdd(ctx context.Context, key string, members ...ZMember) (int64, error)
	ZRem(ctx context.Context, key string, members ...string) (int64, error)
	ZRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	ZRangeWithScores(ctx context.Context, key string, start, stop int64) ([]ZMember, error)
	ZRevRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	ZScore(ctx context.Context, key, member string) (float64, error)
	ZRank(ctx context.Context, key, member string) (int64, error)
	ZCard(ctx context.Context, key string) (int64, error)
	ZCount(ctx context.Context, key string, min, max float64) (int64, error)
	ZIncrBy(ctx context.Context, key, member string, delta float64) (float64, error)
}

// ZMember represents a sorted set member with score.
type ZMember struct {
	Score  float64
	Member string
}
