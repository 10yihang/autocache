# controllers Knowledge Base

Apply the root `AGENTS.md` first, then these operator-specific rules.

## OVERVIEW
controller-runtime reconcile flow for the `AutoCache` CRD: ConfigMap, three Services (headless / client / metrics), StatefulSet, cluster initialization (CLUSTER MEET + ADDSLOTS), scaling, slot migration, status updates, and finalizer-driven cleanup all converge here.

## WHERE TO LOOK
- `autocache_controller.go` — `AutoCacheReconciler`, finalizer (`cache.autocache.io/finalizer`), `Reconcile()` phases, kubebuilder RBAC markers, cluster init (`forget` stale → `MEET` → batched `ADDSLOTS` → health verify)
- `statefulset.go` — pod template, init container, volumes, replica scaling detection
- `migration.go` — slot-plan parsing, migration task calculation, progress tracking into `status.migration`
- `scaling.go` — `ScaleManager`: scale-up (wait Ready → MEET → rebalance) and scale-down (drain slots → forget)
- `resp.go` — minimal RESP parser (arrays, integers, bulk strings, simple strings, errors)
- `resp_helpers.go` — RESP command sender, `CLUSTER NODES` / slot / key helpers used by reconcile
- `*_test.go` — fake-client reconcile regression coverage; covers degraded, stale-node, and partial-progress branches

## CONVENTIONS
- Reconcile must stay level-based and idempotent; every branch tolerates retries and partial progress. Use `requeueAfterSuccess` (30 s) and `requeueAfterError` (5 s) constants instead of ad-hoc values.
- Spec is desired state, status is observed state — never derive status from assumptions when live cluster (`CLUSTER INFO`, pod readiness) is the source of truth.
- Finalizer add/remove flow is explicit and ordered; safe deletion depends on it. Always requeue after adding the finalizer so the next pass sees a stable object.
- Resource names, labels, selectors, and owner references must stay consistent across ConfigMap, three Services, StatefulSet, and Pods so kustomize and listers resolve correctly.
- Set `controllerutil.SetControllerReference` on every child resource for cascading deletion.
- When changing cluster initialization or migration behavior, cover degraded-health, stale-node cleanup, and partial-progress cases in tests.
- Modifying CRD types (in `api/v1alpha1/`) or kubebuilder RBAC markers requires `make generate` AND `make manifests` before commit.

## ANTI-PATTERNS
- Do not mutate `status` based on assumptions when live cluster health or pod state is the real source of truth.
- Do not make reconcile steps depend on one-shot execution; every branch should tolerate retries.
- Do not skip tests for scaling, migration, stale-node cleanup, or degraded-cluster status transitions when changing behavior.
- Do not edit CRD-generated artifacts (`api/v1alpha1/zz_generated.deepcopy.go`, `config/crd/bases/*.yaml`, `config/rbac/{role,role_binding,service_account}.yaml`); change source types or markers and regenerate.
- Do not shell out for cluster operations when the in-package RESP helpers in `resp_helpers.go` already cover the command.

## VERIFY
```bash
go test -v ./controllers/...
make generate
make manifests
```
