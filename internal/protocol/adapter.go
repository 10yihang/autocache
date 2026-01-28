package protocol

import (
	"context"
	"time"

	"github.com/10yihang/autocache/internal/engine"
	"github.com/10yihang/autocache/internal/engine/memory"
	pkgerrors "github.com/10yihang/autocache/pkg/errors"
)

type MemoryStoreAdapter struct {
	store *memory.Store
}

func NewMemoryStoreAdapter(store *memory.Store) *MemoryStoreAdapter {
	return &MemoryStoreAdapter{store: store}
}

func (a *MemoryStoreAdapter) GetBytes(ctx context.Context, key string) ([]byte, error) {
	return a.store.GetBytes(ctx, key)
}

func (a *MemoryStoreAdapter) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return a.store.Set(ctx, key, value, ttl)
}

func (a *MemoryStoreAdapter) SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	return a.store.SetNX(ctx, key, value, ttl)
}

func (a *MemoryStoreAdapter) SetXX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	return a.store.SetXX(ctx, key, value, ttl)
}

func (a *MemoryStoreAdapter) GetSet(ctx context.Context, key string, value string) (string, error) {
	return a.store.GetSet(ctx, key, value)
}

func (a *MemoryStoreAdapter) Incr(ctx context.Context, key string) (int64, error) {
	return a.store.Incr(ctx, key)
}

func (a *MemoryStoreAdapter) IncrBy(ctx context.Context, key string, delta int64) (int64, error) {
	return a.store.IncrBy(ctx, key, delta)
}

func (a *MemoryStoreAdapter) Decr(ctx context.Context, key string) (int64, error) {
	return a.store.Decr(ctx, key)
}

func (a *MemoryStoreAdapter) DecrBy(ctx context.Context, key string, delta int64) (int64, error) {
	return a.store.DecrBy(ctx, key, delta)
}

func (a *MemoryStoreAdapter) Append(ctx context.Context, key string, value string) (int64, error) {
	return a.store.Append(ctx, key, value)
}

func (a *MemoryStoreAdapter) Strlen(ctx context.Context, key string) (int64, error) {
	return a.store.Strlen(ctx, key)
}

func (a *MemoryStoreAdapter) MGetBytes(ctx context.Context, keys ...string) ([][]byte, error) {
	return a.store.MGetBytes(ctx, keys...)
}

func (a *MemoryStoreAdapter) MSet(ctx context.Context, pairs ...interface{}) error {
	return a.store.MSet(ctx, pairs...)
}

func (a *MemoryStoreAdapter) Del(ctx context.Context, keys ...string) (int64, error) {
	return a.store.Del(ctx, keys...)
}

func (a *MemoryStoreAdapter) Exists(ctx context.Context, keys ...string) (int64, error) {
	return a.store.Exists(ctx, keys...)
}

func (a *MemoryStoreAdapter) Keys(ctx context.Context, pattern string) ([]string, error) {
	return a.store.Keys(ctx, pattern)
}

func (a *MemoryStoreAdapter) Type(ctx context.Context, key string) (string, error) {
	return a.store.Type(ctx, key)
}

func (a *MemoryStoreAdapter) Rename(ctx context.Context, key, newkey string) error {
	return a.store.Rename(ctx, key, newkey)
}

func (a *MemoryStoreAdapter) Scan(ctx context.Context, cursor uint64, pattern string, count int) ([]string, uint64, error) {
	return a.store.Scan(ctx, cursor, pattern, count)
}

func (a *MemoryStoreAdapter) TTL(ctx context.Context, key string) (time.Duration, error) {
	return a.store.TTL(ctx, key)
}

func (a *MemoryStoreAdapter) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return a.store.Expire(ctx, key, ttl)
}

func (a *MemoryStoreAdapter) ExpireAt(ctx context.Context, key string, t time.Time) (bool, error) {
	return a.store.ExpireAt(ctx, key, t)
}

func (a *MemoryStoreAdapter) Persist(ctx context.Context, key string) (bool, error) {
	return a.store.Persist(ctx, key)
}

func (a *MemoryStoreAdapter) DBSize(ctx context.Context) (int64, error) {
	return a.store.DBSize(ctx)
}

func (a *MemoryStoreAdapter) FlushDB(ctx context.Context) error {
	return a.store.FlushDB(ctx)
}

func (a *MemoryStoreAdapter) GetEntry(ctx context.Context, key string) (*engine.Entry, error) {
	value, err := a.store.GetBytes(ctx, key)
	if err != nil {
		return nil, err
	}

	ttl, _ := a.store.TTL(ctx, key)

	entry := &engine.Entry{
		Key:   key,
		Value: value,
		Type:  engine.TypeString,
	}

	if ttl > 0 {
		entry.ExpireAt = time.Now().Add(ttl)
	}

	return entry, nil
}

func (a *MemoryStoreAdapter) GetStats() interface{} {
	return a.store.GetStats()
}

func (a *MemoryStoreAdapter) Close() error {
	return a.store.Close()
}

var _ ProtocolEngine = (*MemoryStoreAdapter)(nil)

var _ = pkgerrors.ErrKeyNotFound
