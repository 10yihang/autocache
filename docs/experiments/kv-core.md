# KV Core Experiment Record

## Goal

Verify Redis-compatible core data structures and command correctness for `string`, `hash`, `list`, `set`, and `zset`.

## Environment

- Date: 2026-03-18
- Commit/branch: `origin/thesis-plan`
- Host: macOS arm64 local workstation
- Go version: `go1.26.1`

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

- Test summary: `go test -v -race ./internal/engine/memory/... ./internal/protocol/...` passed during final verification
- Functional notes:
  - `SET/GET`, `HSET/HGETALL`, `LPUSH/LRANGE`, `SADD/SMEMBERS`, `ZADD/ZRANGE/ZSCORE` verified via `redis-cli` against local AutoCache on port `6389`
  - TTL path verified with `SET temp v EX 2`, `TTL temp`, and `EXISTS temp` after expiry
  - `ZRANGE ... WITHSCORES` is not yet implemented; validation used `ZRANGE` plus `ZSCORE`
- Regressions found: none in the final single-node verification path

## Benchmark Snapshot

Single-node `redis-benchmark -n 100000 -c 50 -t set,get -q`

| System | SET rps | SET p50 | GET rps | GET p50 |
|---|---:|---:|---:|---:|
| AutoCache (`6389`) | 84,817.64 | 0.431 ms | 88,888.89 | 0.391 ms |
| Redis 8.4 (`6390`) | 85,689.80 | 0.287 ms | 90,744.10 | 0.279 ms |

Relative to Redis in this local run:

- AutoCache SET throughput is about `99.0%` of Redis
- AutoCache GET throughput is about `97.9%` of Redis
- Redis still has lower p50 latency on both operations

## Aggressive Benchmark Snapshot

Higher-pressure single-node run with `redis-benchmark -n 500000 -c 100 -t set,get -q`

| System | SET rps | SET p50 | GET rps | GET p50 |
|---|---:|---:|---:|---:|
| AutoCache (`6389`) | 40,883.07 | 2.287 ms | 39,698.29 | 2.279 ms |
| Redis 8.4 (`6390`) | 40,449.80 | 2.295 ms | 39,796.24 | 2.287 ms |

Relative to Redis in this higher-pressure run:

- AutoCache SET throughput is about `101.1%` of Redis
- AutoCache GET throughput is about `99.8%` of Redis
- p50 latency is effectively in the same range for both systems in this workload

## Cluster Benchmark Snapshot

Local 3-node cluster run with `redis-benchmark --cluster -p 7001 -n 100000 -c 50 -t set,get -q`

| System | SET rps | SET p50 | GET rps | GET p50 |
|---|---:|---:|---:|---:|
| AutoCache cluster (`7001/7002/7003`) | 66,622.25 | 0.583 ms | 66,666.66 | 0.583 ms |

Notes:

- This run includes cluster routing and multi-node coordination overhead
- Throughput is lower than single-node mode, which matches the extra distributed path cost

## Mode Comparison Summary

| Mode | SET rps | GET rps | SET p50 | GET p50 |
|---|---:|---:|---:|---:|
| AutoCache single node | 84,817.64 | 88,888.89 | 0.431 ms | 0.391 ms |
| Redis 8.4 single node | 85,689.80 | 90,744.10 | 0.287 ms | 0.279 ms |
| AutoCache aggressive single node | 40,883.07 | 39,698.29 | 2.287 ms | 2.279 ms |
| Redis 8.4 aggressive single node | 40,449.80 | 39,796.24 | 2.295 ms | 2.287 ms |
| AutoCache 3-node cluster | 66,622.25 | 66,666.66 | 0.583 ms | 0.583 ms |

Interpretation:

- AutoCache single-node performance is close to Redis for the tested string `SET/GET` path
- Under higher concurrency, AutoCache and Redis converge to nearly identical throughput
- Cluster mode introduces the largest visible overhead in this test set

## Evidence

- Terminal output: local benchmark and `redis-cli` verification run on 2026-03-18
- Screenshot path: not captured in CLI session
