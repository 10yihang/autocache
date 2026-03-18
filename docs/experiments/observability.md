# Observability Experiment Record

## Goal

Verify that AutoCache exposes actionable runtime metrics and deployable monitoring assets.

## Environment

- Date:
- Commit/branch:
- Metrics address:
- Helm release:

## Commands

```bash
go test -v ./internal/protocol/...
helm template autocache ./deploy/helm/autocache --set monitoring.serviceMonitor.enabled=true
curl http://127.0.0.1:9090/metrics
```

## Metrics Checklist

- `autocache_commands_total`
- `autocache_command_duration_seconds`
- `autocache_connections_total`
- `autocache_tiered_keys_total`
- `autocache_tiered_migrations_total`

## Asset Checklist

- Grafana dashboard ConfigMap
- Prometheus rules ConfigMap
- Metrics Service
- ServiceMonitor

## Evidence

- `/metrics` excerpt:
- Helm render excerpt:
- Dashboard screenshot path:
