# Tiered Benchmark

This benchmark compares baseline in-memory access with tiered mode enabled.

## Run

```bash
go test -bench=. -benchmem ./test/tiered_benchmark/...
```

## Benchmarks

- `BenchmarkHotOnlyGet` measures steady-state reads from the memory store
- `BenchmarkTieredHotPathGet` measures steady-state reads with tiered manager enabled and warm tier configured

## Notes

- This is the first benchmark slice for thesis data collection
- It captures hot-path overhead before adding promotion/demotion-specific scenarios
- Extend this directory with workload traces and mixed read/write benchmarks as experiments mature
