# Phase 76 deferred items

## 76-09: a fresh chart install can starve api or minio on a small node

- Found by: the cad loadproof, attempts 3 and 4 (evidence/76-09-keda-loadproof-attempt3-4-cause.txt).
- What happens:
  - Every KEDA-owned worker Deployment is rendered without `replicas`, so Kubernetes creates it at 1.
  - Ten workers at 2 CPU each plus api at 2 CPU ask for about 22 CPU at once. The OrbStack node has 11.
  - When api or minio loses the scheduling race, the queue metric never appears. KEDA's fallback then holds every worker at 1, and the install never settles.
  - A worker pod blocked in its `wait-for-bucket` init container ignores SIGTERM. It holds a deleted namespace for its full grace (783 s for av-worker).
- Why not fixed in 76-09:
  - The fix is a chart change, for example a PriorityClass for the always-on services, or a first install at 0 that a later `helm upgrade` does not reset. Either is a design decision (Rule 4) outside this plan.
  - The loadproof works around it by scaling the nine non-cad ScaledObject targets to 0 while `helm install` runs, and records an INSTALL_ASSIST line. cad-worker is untouched.
- Production relevance: a node pool with fewer free CPUs than the sum of the worker limits can hit the same race on a fresh install.
