# AutoCache Scheduler

AutoCache does not currently ship a custom Kubernetes scheduler plugin,
scheduler binary, or scheduler image.

Do not deploy the old custom scheduler manifest directly. It referenced
nonexistent `AutoCacheTopologyAware` and `AutoCacheLoadAware` scheduler plugins
and an `autocache-scheduler:latest` image that is not built by this repository.

Use the native Kubernetes scheduling fields exposed by the AutoCache CRD for
production deployments:

- `schedulerName`
- `nodeSelector`
- `affinity`
- `topologySpreadConstraints`
- `tolerations`
- `priorityClassName`

If AutoCache adds a custom scheduler in the future, it must include a real
scheduler binary or plugin implementation, container image build, RBAC,
deployment manifests, and integration tests.
