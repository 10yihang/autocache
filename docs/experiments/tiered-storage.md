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

## Evidence

- Benchmark output:
- Metrics snapshot:
- Screenshot path:
