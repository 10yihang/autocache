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

- Local 3-node verification (standalone, no Kubernetes):

```text
redis-cli -p 7001 cluster nodes
28316c818988d6251073056a39ee928fe07fa4b2 127.0.0.1:7001@17001 myself,master - 0 0 0 connected
6b40ee31e0fed32f8f27df8ca8299f866ec50783 127.0.0.1:7003@17003 master - ... connected
0ff17574df3b1770dd12e00944c14224bb94e95a 127.0.0.1:7002@17002 master - ... connected
```

- Result: all three local cluster-mode nodes discovered each other successfully via gossip/meet
- Note: this local check validates node discovery and cluster membership; slot assignment and Kubernetes-driven scaling remain covered primarily by controller tests

## Local Cluster Benchmark Snapshot

```bash
redis-benchmark --cluster -p 7001 -n 100000 -c 50 -t set,get -q
```

Results:

- `SET`: `66,622.25 req/s`, `p50=0.583 ms`
- `GET`: `66,666.66 req/s`, `p50=0.583 ms`

Interpretation:

- Compared with single-node AutoCache, cluster mode shows the expected throughput drop from routing and multi-node coordination overhead
- The cluster path remains functional and benchmarkable for thesis discussion, even though it is not yet optimized to single-node levels

## Cloud-Native Cluster Benchmark Snapshot

Executed from the thesis worktree with:

```bash
scripts/bench-cloud-native.sh record cluster 100000 50
```

Log artifact:

- `scripts/output/bench-cluster-20260318-165516.log`

Observed results:

- Individual node SET throughput:
  - node0: `13338.54 req/s`
  - node1: `11557.91 req/s`
  - node2: `13739.90 req/s`
- Individual node GET throughput:
  - node0: `43629.58 req/s`
  - node1: `52909.52 req/s`
  - node2: `52328.10 req/s`
- Parallel aggregate results:
  - `SET`: `~21963.92 req/s`
  - `GET`: `~56169.35 req/s`

Notes:

- The script successfully benchmarked all three Pods in parallel inside Kubernetes
- The cluster metadata still reported `cluster_slots_assigned:0`, so this run should be treated as a current-runtime measurement rather than a fully normalized Redis-cluster-equivalent benchmark
