# Tiered Storage Experiment Record

## Goal

Measure hot/warm tier migration behavior, TTL preservation, and key distribution across tiers.

Cold tier remains experimental and is excluded from the primary thesis benchmark path for now.

## Environment

- Date:
- Commit/branch:
- Warm tier path:
- Dataset size:

## Commands

```bash
go test -v ./internal/engine/tiered/... ./internal/engine/badger/...
go test -bench=. -benchmem ./internal/engine/...
```

## Scenario Checklist

- Idle key demotion hot -> warm
- Warm key promotion warm -> hot
- TTL preserved across promotion/demotion
- Typed key demotion preserved in warm tier
- Migration counters and size stats updated
- Cold tier explicitly excluded from benchmark conclusions

## Metrics to Capture

- `MigrationsUp`
- `MigrationsDown`
- `HotTierKeys/WarmTierKeys`
- `HotTierSize/WarmTierSize`
- Latency before/after demotion

## Benchmark Snapshot

Local hot+warm run with `redis-benchmark -p 6391 -n 100000 -c 50 -t set,get -q`

| System | SET rps | SET p50 | GET rps | GET p50 |
|---|---:|---:|---:|---:|
| AutoCache hot+warm (`6391`) | 83,333.33 | 0.479 ms | 86,281.27 | 0.471 ms |

Comparison against single-node non-tiered AutoCache:

- Single-node baseline: `SET 84,817.64 rps`, `GET 88,888.89 rps`
- Hot+warm tiered mode keeps similar throughput with a small latency increase on the hot path

Interpretation:

- Enabling warm tier support adds only modest overhead for hot-path string reads and writes
- This supports using the hot+warm path as the primary thesis demo/storage experiment path

## Cloud-Native Tiered Benchmark Snapshot

Executed from the thesis worktree with:

```bash
scripts/bench-cloud-native.sh record tiered 50000 50 100
```

Log artifact:

- `scripts/output/bench-tiered-20260318-165745.log`

Observed results:

- `SET`: `61425.06 req/s`, `p50=0.479 ms`
- `GET`: `76804.91 req/s`, `p50=0.463 ms`

Interpretation:

- The hot+warm Kubernetes path remains faster than the current cloud-native compare result for clustered AutoCache
- Warm-tier support is active without preventing the Pod from serving a normal benchmark workload

## Evidence

- Benchmark output:
- Metrics snapshot:
- Screenshot path:
