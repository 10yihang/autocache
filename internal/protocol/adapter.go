package protocol

import (
	"context"
	"fmt"
	"time"

	"github.com/10yihang/autocache/internal/engine"
	"github.com/10yihang/autocache/internal/engine/memory"
	"github.com/10yihang/autocache/internal/engine/tiered"
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

func (a *MemoryStoreAdapter) HGet(ctx context.Context, key, field string) (string, error) {
	return a.store.HGet(ctx, key, field)
}

func (a *MemoryStoreAdapter) HSet(ctx context.Context, key string, pairs ...interface{}) (int64, error) {
	return a.store.HSet(ctx, key, pairs...)
}

func (a *MemoryStoreAdapter) HDel(ctx context.Context, key string, fields ...string) (int64, error) {
	return a.store.HDel(ctx, key, fields...)
}

func (a *MemoryStoreAdapter) HExists(ctx context.Context, key, field string) (bool, error) {
	return a.store.HExists(ctx, key, field)
}

func (a *MemoryStoreAdapter) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return a.store.HGetAll(ctx, key)
}

func (a *MemoryStoreAdapter) HKeys(ctx context.Context, key string) ([]string, error) {
	return a.store.HKeys(ctx, key)
}

func (a *MemoryStoreAdapter) HVals(ctx context.Context, key string) ([]string, error) {
	return a.store.HVals(ctx, key)
}

func (a *MemoryStoreAdapter) HLen(ctx context.Context, key string) (int64, error) {
	return a.store.HLen(ctx, key)
}

func (a *MemoryStoreAdapter) LPush(ctx context.Context, key string, values ...interface{}) (int64, error) {
	return a.store.LPush(ctx, key, values...)
}

func (a *MemoryStoreAdapter) RPush(ctx context.Context, key string, values ...interface{}) (int64, error) {
	return a.store.RPush(ctx, key, values...)
}

func (a *MemoryStoreAdapter) LPop(ctx context.Context, key string) (string, error) {
	return a.store.LPop(ctx, key)
}

func (a *MemoryStoreAdapter) RPop(ctx context.Context, key string) (string, error) {
	return a.store.RPop(ctx, key)
}

func (a *MemoryStoreAdapter) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return a.store.LRange(ctx, key, start, stop)
}

func (a *MemoryStoreAdapter) LLen(ctx context.Context, key string) (int64, error) {
	return a.store.LLen(ctx, key)
}

func (a *MemoryStoreAdapter) LIndex(ctx context.Context, key string, index int64) (string, error) {
	return a.store.LIndex(ctx, key, index)
}

func (a *MemoryStoreAdapter) LSet(ctx context.Context, key string, index int64, value string) error {
	return a.store.LSet(ctx, key, index, value)
}

func (a *MemoryStoreAdapter) LTrim(ctx context.Context, key string, start, stop int64) error {
	return a.store.LTrim(ctx, key, start, stop)
}

func (a *MemoryStoreAdapter) SAdd(ctx context.Context, key string, members ...interface{}) (int64, error) {
	return a.store.SAdd(ctx, key, members...)
}

func (a *MemoryStoreAdapter) SRem(ctx context.Context, key string, members ...interface{}) (int64, error) {
	return a.store.SRem(ctx, key, members...)
}

func (a *MemoryStoreAdapter) SMembers(ctx context.Context, key string) ([]string, error) {
	return a.store.SMembers(ctx, key)
}

func (a *MemoryStoreAdapter) SIsMember(ctx context.Context, key, member string) (bool, error) {
	return a.store.SIsMember(ctx, key, member)
}

func (a *MemoryStoreAdapter) SCard(ctx context.Context, key string) (int64, error) {
	return a.store.SCard(ctx, key)
}

func (a *MemoryStoreAdapter) ZAdd(ctx context.Context, key string, members ...engine.ZMember) (int64, error) {
	return a.store.ZAdd(ctx, key, members...)
}

func (a *MemoryStoreAdapter) ZRem(ctx context.Context, key string, members ...string) (int64, error) {
	return a.store.ZRem(ctx, key, members...)
}

func (a *MemoryStoreAdapter) ZRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return a.store.ZRange(ctx, key, start, stop)
}

func (a *MemoryStoreAdapter) ZScore(ctx context.Context, key, member string) (float64, error) {
	return a.store.ZScore(ctx, key, member)
}

func (a *MemoryStoreAdapter) ZRank(ctx context.Context, key, member string) (int64, error) {
	return a.store.ZRank(ctx, key, member)
}

func (a *MemoryStoreAdapter) ZCard(ctx context.Context, key string) (int64, error) {
	return a.store.ZCard(ctx, key)
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
	return a.store.GetEntry(ctx, key)
}

func (a *MemoryStoreAdapter) RestoreEntry(ctx context.Context, key string, valueType engine.ValueType, payload []byte, ttl time.Duration) error {
	return a.store.RestoreEntry(ctx, key, valueType, payload, ttl)
}

func (a *MemoryStoreAdapter) GetStats() interface{} {
	return a.store.GetStats()
}

func (a *MemoryStoreAdapter) Close() error {
	return a.store.Close()
}

func (a *MemoryStoreAdapter) KeysInSlot(slot uint16, count int) []string {
	return a.store.KeysInSlot(slot, count)
}

func (a *MemoryStoreAdapter) CountKeysInSlot(slot uint16) int {
	return a.store.CountKeysInSlot(slot)
}

type TieredStoreAdapter struct {
	manager *tiered.Manager
	store   *memory.Store
}

func NewTieredStoreAdapter(manager *tiered.Manager, store *memory.Store) *TieredStoreAdapter {
	return &TieredStoreAdapter{manager: manager, store: store}
}

func (a *TieredStoreAdapter) GetBytes(ctx context.Context, key string) ([]byte, error) {
	value, err := a.manager.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return []byte(value), nil
}

func (a *TieredStoreAdapter) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return a.manager.Set(ctx, key, value, ttl)
}

func (a *TieredStoreAdapter) SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	return a.manager.SetNX(ctx, key, value, ttl)
}

func (a *TieredStoreAdapter) SetXX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	return a.manager.SetXX(ctx, key, value, ttl)
}

func (a *TieredStoreAdapter) GetSet(ctx context.Context, key string, value string) (string, error) {
	return a.manager.GetSet(ctx, key, value)
}

func (a *TieredStoreAdapter) Incr(ctx context.Context, key string) (int64, error) {
	return a.store.Incr(ctx, key)
}

func (a *TieredStoreAdapter) IncrBy(ctx context.Context, key string, delta int64) (int64, error) {
	return a.manager.IncrBy(ctx, key, delta)
}

func (a *TieredStoreAdapter) Decr(ctx context.Context, key string) (int64, error) {
	return a.store.Decr(ctx, key)
}

func (a *TieredStoreAdapter) DecrBy(ctx context.Context, key string, delta int64) (int64, error) {
	return a.manager.IncrBy(ctx, key, -delta)
}

func (a *TieredStoreAdapter) Append(ctx context.Context, key string, value string) (int64, error) {
	return a.manager.Append(ctx, key, value)
}

func (a *TieredStoreAdapter) Strlen(ctx context.Context, key string) (int64, error) {
	return a.manager.Strlen(ctx, key)
}

func (a *TieredStoreAdapter) MGetBytes(ctx context.Context, keys ...string) ([][]byte, error) {
	return a.manager.MGetBytes(ctx, keys...)
}

func (a *TieredStoreAdapter) MSet(ctx context.Context, pairs ...interface{}) error {
	return a.store.MSet(ctx, pairs...)
}

func (a *TieredStoreAdapter) HGet(ctx context.Context, key, field string) (string, error) {
	return a.store.HGet(ctx, key, field)
}

func (a *TieredStoreAdapter) HSet(ctx context.Context, key string, pairs ...interface{}) (int64, error) {
	n, err := a.store.HSet(ctx, key, pairs...)
	if n > 0 {
		a.pushEntry(key)
	}
	return n, err
}

func (a *TieredStoreAdapter) HDel(ctx context.Context, key string, fields ...string) (int64, error) {
	n, err := a.store.HDel(ctx, key, fields...)
	if n > 0 {
		a.pushEntry(key)
	}
	return n, err
}

func (a *TieredStoreAdapter) HExists(ctx context.Context, key, field string) (bool, error) {
	return a.store.HExists(ctx, key, field)
}

func (a *TieredStoreAdapter) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return a.store.HGetAll(ctx, key)
}

func (a *TieredStoreAdapter) HKeys(ctx context.Context, key string) ([]string, error) {
	return a.store.HKeys(ctx, key)
}

func (a *TieredStoreAdapter) HVals(ctx context.Context, key string) ([]string, error) {
	return a.store.HVals(ctx, key)
}

func (a *TieredStoreAdapter) HLen(ctx context.Context, key string) (int64, error) {
	return a.store.HLen(ctx, key)
}

func (a *TieredStoreAdapter) LPush(ctx context.Context, key string, values ...interface{}) (int64, error) {
	n, err := a.store.LPush(ctx, key, values...)
	if n > 0 {
		a.pushEntry(key)
	}
	return n, err
}

func (a *TieredStoreAdapter) RPush(ctx context.Context, key string, values ...interface{}) (int64, error) {
	n, err := a.store.RPush(ctx, key, values...)
	if n > 0 {
		a.pushEntry(key)
	}
	return n, err
}

func (a *TieredStoreAdapter) LPop(ctx context.Context, key string) (string, error) {
	val, err := a.store.LPop(ctx, key)
	if val != "" {
		a.pushEntry(key)
	}
	return val, err
}

func (a *TieredStoreAdapter) RPop(ctx context.Context, key string) (string, error) {
	val, err := a.store.RPop(ctx, key)
	if val != "" {
		a.pushEntry(key)
	}
	return val, err
}

func (a *TieredStoreAdapter) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return a.store.LRange(ctx, key, start, stop)
}

func (a *TieredStoreAdapter) LLen(ctx context.Context, key string) (int64, error) {
	return a.store.LLen(ctx, key)
}

func (a *TieredStoreAdapter) LIndex(ctx context.Context, key string, index int64) (string, error) {
	return a.store.LIndex(ctx, key, index)
}

func (a *TieredStoreAdapter) LSet(ctx context.Context, key string, index int64, value string) error {
	err := a.store.LSet(ctx, key, index, value)
	if err == nil {
		a.pushEntry(key)
	}
	return err
}

func (a *TieredStoreAdapter) LTrim(ctx context.Context, key string, start, stop int64) error {
	err := a.store.LTrim(ctx, key, start, stop)
	if err == nil {
		a.pushEntry(key)
	}
	return err
}

func (a *TieredStoreAdapter) SAdd(ctx context.Context, key string, members ...interface{}) (int64, error) {
	n, err := a.store.SAdd(ctx, key, members...)
	if n > 0 {
		a.pushEntry(key)
	}
	return n, err
}

func (a *TieredStoreAdapter) SRem(ctx context.Context, key string, members ...interface{}) (int64, error) {
	n, err := a.store.SRem(ctx, key, members...)
	if n > 0 {
		a.pushEntry(key)
	}
	return n, err
}

func (a *TieredStoreAdapter) SMembers(ctx context.Context, key string) ([]string, error) {
	return a.store.SMembers(ctx, key)
}

func (a *TieredStoreAdapter) SIsMember(ctx context.Context, key, member string) (bool, error) {
	return a.store.SIsMember(ctx, key, member)
}

func (a *TieredStoreAdapter) SCard(ctx context.Context, key string) (int64, error) {
	return a.store.SCard(ctx, key)
}

func (a *TieredStoreAdapter) ZAdd(ctx context.Context, key string, members ...engine.ZMember) (int64, error) {
	n, err := a.store.ZAdd(ctx, key, members...)
	if n > 0 {
		a.pushEntry(key)
	}
	return n, err
}

func (a *TieredStoreAdapter) ZRem(ctx context.Context, key string, members ...string) (int64, error) {
	n, err := a.store.ZRem(ctx, key, members...)
	if n > 0 {
		a.pushEntry(key)
	}
	return n, err
}

func (a *TieredStoreAdapter) ZRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return a.store.ZRange(ctx, key, start, stop)
}

func (a *TieredStoreAdapter) ZScore(ctx context.Context, key, member string) (float64, error) {
	return a.store.ZScore(ctx, key, member)
}

func (a *TieredStoreAdapter) ZRank(ctx context.Context, key, member string) (int64, error) {
	return a.store.ZRank(ctx, key, member)
}

func (a *TieredStoreAdapter) ZCard(ctx context.Context, key string) (int64, error) {
	return a.store.ZCard(ctx, key)
}

func (a *TieredStoreAdapter) Del(ctx context.Context, keys ...string) (int64, error) {
	return a.manager.Del(ctx, keys...)
}

func (a *TieredStoreAdapter) Exists(ctx context.Context, keys ...string) (int64, error) {
	return a.manager.Exists(ctx, keys...)
}

func (a *TieredStoreAdapter) Keys(ctx context.Context, pattern string) ([]string, error) {
	return a.manager.Keys(ctx, pattern)
}

func (a *TieredStoreAdapter) Type(ctx context.Context, key string) (string, error) {
	return a.manager.Type(ctx, key)
}

func (a *TieredStoreAdapter) Rename(ctx context.Context, key, newkey string) error {
	entry, err := a.manager.GetEntry(ctx, key)
	if err != nil {
		return err
	}
	ttl, _ := a.manager.TTL(ctx, key)
	_, _ = a.manager.Del(ctx, newkey)
	_, _ = a.manager.Del(ctx, key)
	if entry.Type != engine.TypeString {
		return a.store.RestoreEntry(ctx, newkey, entry.Type, mustSerializeEntry(entry), ttl)
	}
	value, err := tieredStringValue(entry)
	if err != nil {
		return err
	}
	return a.manager.Set(ctx, newkey, value, ttl)
}

func (a *TieredStoreAdapter) Scan(ctx context.Context, cursor uint64, pattern string, count int) ([]string, uint64, error) {
	return a.store.Scan(ctx, cursor, pattern, count)
}

func (a *TieredStoreAdapter) TTL(ctx context.Context, key string) (time.Duration, error) {
	return a.manager.TTL(ctx, key)
}

func (a *TieredStoreAdapter) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return a.manager.Expire(ctx, key, ttl)
}

func (a *TieredStoreAdapter) ExpireAt(ctx context.Context, key string, t time.Time) (bool, error) {
	return a.manager.ExpireAt(ctx, key, t)
}

func (a *TieredStoreAdapter) Persist(ctx context.Context, key string) (bool, error) {
	return a.manager.Persist(ctx, key)
}

func (a *TieredStoreAdapter) DBSize(ctx context.Context) (int64, error) {
	return a.manager.DBSize(ctx)
}

func (a *TieredStoreAdapter) FlushDB(ctx context.Context) error {
	return a.manager.FlushDB(ctx)
}

func (a *TieredStoreAdapter) GetEntry(ctx context.Context, key string) (*engine.Entry, error) {
	return a.manager.GetEntry(ctx, key)
}

func (a *TieredStoreAdapter) RestoreEntry(ctx context.Context, key string, valueType engine.ValueType, payload []byte, ttl time.Duration) error {
	return a.store.RestoreEntry(ctx, key, valueType, payload, ttl)
}

func (a *TieredStoreAdapter) GetStats() interface{} {
	return a.manager.GetStats()
}

func (a *TieredStoreAdapter) Close() error {
	return a.store.Close()
}

func (a *TieredStoreAdapter) KeysInSlot(slot uint16, count int) []string {
	return a.store.KeysInSlot(slot, count)
}

func (a *TieredStoreAdapter) CountKeysInSlot(slot uint16) int {
	return a.store.CountKeysInSlot(slot)
}

var _ ProtocolEngine = (*MemoryStoreAdapter)(nil)
var _ ProtocolEngine = (*TieredStoreAdapter)(nil)

var _ = pkgerrors.ErrKeyNotFound

func mustSerializeEntry(entry *engine.Entry) []byte {
	serialized, err := memory.SerializeEntryValue(entry)
	if err != nil {
		panic(err)
	}
	return serialized[1:]
}

func (a *TieredStoreAdapter) pushEntry(key string) {
	wb := a.manager.GetWriteBehind()
	if wb == nil {
		return
	}
	entry, err := a.store.GetEntry(context.Background(), key)
	if err != nil {
		wb.AppendDel(key)
		return
	}
	wb.AppendSet(key, entry)
}

func tieredStringValue(entry *engine.Entry) (string, error) {
	if entry == nil {
		return "", pkgerrors.ErrKeyNotFound
	}
	switch value := entry.Value.(type) {
	case string:
		return value, nil
	case []byte:
		return string(value), nil
	default:
		return "", fmt.Errorf("entry value type %T is not string-like", entry.Value)
	}
}
