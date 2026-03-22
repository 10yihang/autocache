# controllers Knowledge Base

Apply the root `AGENTS.md` first, then these operator-specific rules.

## OVERVIEW
This package owns the controller-runtime reconcile flow for AutoCache clusters: config, services, StatefulSets, cluster initialization, scaling, slot migration, and status updates all converge here.

## WHERE TO LOOK
- `controllers/autocache_controller.go` - top-level reconcile loop, finalizers, status flow, cluster init trigger
- `controllers/statefulset.go` - pod template and rollout behavior
- `controllers/migration.go` - slot-plan and migration orchestration
- `controllers/scaling.go` - scale-manager behavior and cluster resize flow
- `controllers/*_test.go` - fake-client and reconcile regression coverage

## CONVENTIONS
- Reconcile logic must stay level-based and idempotent; repeated runs should converge, not duplicate work.
- Keep spec ownership, status ownership, and side effects distinct: desired resources come from spec, observed health feeds status.
- Finalizer add/remove flows must be explicit and ordered so deletion remains safe and retryable.
- Resource names, labels, selectors, and owner references must stay consistent across ConfigMaps, Services, StatefulSets, and Pods.
- Prefer controller-runtime client patterns already used here over ad hoc Kubernetes calls.
- When changing cluster initialization or migration behavior, cover degraded-health, stale-node, and partial-progress cases in tests.

## ANTI-PATTERNS
- Do not mutate status fields based on assumptions when live cluster health or pod state is the real source of truth.
- Do not make reconcile steps depend on one-shot execution; every branch should tolerate retries and partial progress.
- Do not skip tests for scaling, migration, stale-node cleanup, or degraded-cluster status transitions when changing behavior.
- Do not edit CRD-generated artifacts here; change API types in `api/v1alpha1/` and regenerate.

## VERIFY
```bash
go test -v ./controllers/...
make generate
make manifests
```
