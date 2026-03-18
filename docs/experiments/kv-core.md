# KV Core Experiment Record

## Goal

Verify Redis-compatible core data structures and command correctness for `string`, `hash`, `list`, `set`, and `zset`.

## Environment

- Date:
- Commit/branch:
- Host:
- Go version:

## Commands

```bash
go test -v -race ./internal/engine/memory/... ./internal/protocol/...
go build ./...
```

## Functional Checklist

- `SET/GET/TTL`
- `HSET/HGET/HGETALL/HLEN`
- `LPUSH/RPUSH/LRANGE/LPOP/RPOP`
- `SADD/SMEMBERS/SCARD/SREM`
- `ZADD/ZRANGE/ZSCORE/ZRANK/ZCARD/ZREM`
- WRONGTYPE behavior for string commands on non-string keys

## Results

- Test summary:
- Functional notes:
- Regressions found:

## Evidence

- Terminal output:
- Screenshot path:
