# Tiered Benchmark

This benchmark compares baseline in-memory access with tiered mode enabled.

## Run

```bash
go test -bench=. -benchmem ./test/tiered_benchmark/...
```

## Benchmarks

- `BenchmarkHotOnlyGet` measures steady-state reads from the memory store
- `BenchmarkTieredHotPathGet` measures steady-state reads with tiered manager enabled and warm tier configured
- `BenchmarkWarmBackendGetParallel` measures direct warm-backend read throughput for `badger` vs `nokv`
- `BenchmarkWarmBackendSetParallel` measures direct warm-backend write throughput for `badger` vs `nokv`
- `BenchmarkWarmBackendMixedParallel` measures a 95/5 read-write mix on direct warm backends
- `BenchmarkWarmBackendSetWithTTLParallel` measures write throughput when each key carries a TTL
- `BenchmarkTieredWarmPathGet` measures reads after a key has been demoted into the warm tier and fetched through `tiered.Manager` (implemented in `internal/engine/tiered/manager_benchmark_test.go`)

## Notes

- This is the first benchmark slice for thesis data collection
- It captures hot-path overhead before adding promotion/demotion-specific scenarios
- Extend this directory with workload traces and mixed read/write benchmarks as experiments mature
