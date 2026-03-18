# Cluster Scaling Experiment Record

## Goal

Verify operator-driven scale up, rebalance, scale down, and slot migration safety.

## Environment

- Date:
- Commit/branch:
- Cluster environment:
- Node count:

## Commands

```bash
go test -v ./controllers/... ./internal/cluster/...
kubectl get autocache -A
kubectl get pods -n <namespace> -o wide
```

## Scenario Checklist

- Bootstrap cluster
- Scale from N to N+1
- Observe slot rebalance and migration status
- Scale from N+1 to N
- Confirm drained node removal and `CLUSTER FORGET`

## Metrics to Capture

- Migration phase transitions
- Slot distribution before/after
- Keys migrated per slot
- Failure/rollback behavior

## Evidence

- CR status snapshots:
- `kubectl` output:
- Grafana or metrics screenshot:
