# Deferred Items — Phase 58

## Plan 58-03

- **`helm lint deploy/chart/octoconv` / `helm template ... ` with NO `-f` fails
  regardless of Phase 58**, pre-existing and out of scope for this plan:
  `templates/secret.yaml` dereferences `.Values.secrets.postgresPassword`
  (and siblings), and `values.yaml` deliberately carries no `secrets:` block
  by design ("Real dev-credential values live in values-local.yaml, never
  here" — values.yaml's own header comment). This has been true since
  whichever earlier phase introduced `secret.yaml`; Task 3 of this plan
  touches `deployment-api.yaml` / `service-api.yaml` /
  `networkpolicy-metrics.yaml` / `configmap.yaml` / `values.yaml`, none of
  which are the cause. All Task 3 acceptance-criteria `helm lint`/
  `helm template` runs in 58-03-SUMMARY.md were therefore run with
  `-f deploy/chart/octoconv/values-local.yaml` (the chart's own supplied
  dev-secrets overlay, already used by the existing `values-local.yaml`
  file) to get past this pre-existing gap and actually exercise the
  templates this plan changed. Not fixed here — out of scope (Rule 3
  scope boundary: pre-existing failure in a file this plan does not touch).
