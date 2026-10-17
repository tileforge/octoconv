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

## 76-10: the deletion-cost annotation races the HPA decision

- Found by: the cad downscale-survival run, official attempt 2 (evidence/76-10-keda-downscale.txt).
- What happened:
  - SHORT ran on the second pod before that pod reported Ready, and finished at 16:22:24.796Z.
  - The script saw 2 Ready replicas at 16:22:27.457Z, ran the census once and annotated the busy pod at 16:22:28.740Z.
  - The busy pod's Killing event is stamped 16:22:29Z, so the HPA acted between 0.26 s and 1.26 s after the annotation.
- Why it still counts: the annotation landed while spec.replicas was 2, it preceded the Killing second, and the victim was the busy pod. Without the annotation, the ReplicaSet controller would have deleted the more recently Ready pod, which was the idle one.
- Why it is fragile: if the HPA had acted one or two seconds earlier, the idle pod would have been the victim and the run would have failed. The ocr and pandoc scripts use the same order (annotate after the second pod is up).
- Possible hardening, not done here because the passing run is the evidence and a changed script would need a new run: annotate the only pod as soon as LONG is active, before SHORT is submitted, and keep the engine census after 1->2 as the cross-check.
