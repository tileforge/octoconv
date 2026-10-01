---
phase: quick-261001-r2s
plan: 01
subsystem: deploy/chart
tags: [helm, minio, dataOwnership, reuse-values, codex-pr31-round3]
requires:
  - quick-261001-q68 (minio.dataOwnership block and its validation)
provides:
  - "octoconv.minioDataOwnership: one helper holding the defaults (65532/65532/true/empty image), the absent-or-null handling and the checks of set values"
  - "helm upgrade --reuse-values from a release made before minio.dataOwnership renders and gives the same pod as the defaults"
affects:
  - deploy/chart/octoconv
  - scripts/ci-helm-checks.sh
  - internal/e2e/minio_image_pin_test.go
key-files:
  created:
    - .planning/quick/261001-r2s-chart-template-side-defaults-for-minio-d/evidence/ (probe, stub, raw outputs)
  modified:
    - deploy/chart/octoconv/templates/_helpers.tpl
    - deploy/chart/octoconv/templates/minio.yaml
    - deploy/chart/octoconv/values.yaml
    - scripts/ci-helm-checks.sh
    - internal/e2e/minio_image_pin_test.go
decisions:
  - "Defaults live on the template side, in one dict in the helper; absence and null are told apart from a wrong value by kind, never by truthiness, so uid 0 and migrate false stay as set"
  - "null equals absence in every path: Helm drops a null key that exists in the chart's values.yaml and keeps it as nil when it does not, which is exactly the --reuse-values case; strict hasKey would give one command two meanings"
  - "A dataOwnership block that is neither a map nor null aborts with 'minio.dataOwnership must be a map'"
  - "minio.dataOwnership.image is still not validated (same as q68 and mcImage); the repo's values files are held by TestMinioImagePinnedByDigest"
  - "The CI gate simulates reuse-values with helm template on a copy of the chart whose values.yaml lost .minio.dataOwnership and .minio.mcImage; the real helm upgrade lives in the evidence probe, and its manifest was measured equal to the simulation"
status: complete
plan_head_before: 93ed97192ef318d294bb5f3715d6727a60fd90c2
plan_head_after: f91b24f066b4ff36764546775a866a1e5d45e905
commits: 2
actuals:
  tokens: 40000
  tasks: 3
  commits: 2
metrics:
  duration: "about 1 h"
  completed: 2026-10-01
---

# Phase quick-261001-r2s Plan 01: template-side defaults for minio.dataOwnership under --reuse-values Summary

One helper (`octoconv.minioDataOwnership`) returns JSON with the MinIO pod ownership defaults for a missing block, a missing key or a null, so `helm upgrade --reuse-values` from a pre-q68 release renders the same pod as the defaults, while a set but wrong value still aborts the render.

## What was done

Task 1 (tracer, commit 1be952e)
- Probe of the real Helm (`evidence/reuse-values-probe.sh`, `fake-kube-discovery.py`): `HELM_DRIVER=memory`, a release seeded from main's chart (caf0718), a stub that answers only the four discovery URLs, explicit temporary KUBECONFIG, stub stopped by `trap`. On 93ed971 (`reuse-values-probe-pre.txt`) `--reuse-values` fails with `minio.dataOwnership.uid must be a whole number, got <nil> (kind invalid)` and the `--reset-then-reuse-values` control passes: the Codex error is reproduced by Helm itself.
- CI gate section "helm upgrade --reuse-values" in `scripts/ci-helm-checks.sh`, written before the fix and run red (`ci-helm-red.txt`): the first FAIL is case (a), with the Codex message.
- The fix: `octoconv.minioDataOwnershipId` and the inline `$migrate := dig ...` in `minio.yaml` are replaced by `octoconv.minioDataOwnership`; `minio.yaml` reads it once into `$ownership` and runAsUser, runAsGroup, fsGroup, the init condition, the init image and the chown owner all read only that. The 65532 literal appears on one code line (the `dict` in the helper).
- `minioOwnershipTemplateLine` in the guard test changed to the new image line (red then green, `guard-test.txt`).
- After the fix (`reuse-values-probe-post.txt`): rc 0, `.chart.values.minio` keys are `image,mcImage,rootPassword,rootUser,storage` (no dataOwnership, so the reuse path really dropped the new defaults), the manifest of the real upgrade is byte-identical to the simulation (2651 lines), hook images equal, init `migrate-data-ownership` and `wait-for-bucket` present.
- Inversion INV-B (default uid 65533 in the helper, values.yaml untouched): first FAIL in case (a) with a diff showing 65533 (`gate-inversions.txt`).

Task 2 (commit f91b24f)
- Gates added to the same section: (b) each of uid, gid, migrate, image removed in turn is byte-identical to the defaults; (c) only `uid: 1000` gives owner 1000:65532, gid and fsGroup 65532, exactly 4 changed lines; (d) `uid: 0` and `migrate: false` stay as set; (e) four null forms and `dataOwnership: {}` on a chart without the block; (f) 1.5, -1, `no`, and a non-map block on the block-less chart plus a non-map block on the real chart.
- The three q68 "null must fail" checks became byte-equality checks (the changed meaning is D-03); nothing was deleted without a replacement.
- values.yaml: only comments changed (four added or changed lines).
- Inversions C to H each tripped a gate (`gate-inversions.txt`).

Task 3
- `render-invariance.txt`: all five renders with set values (`-f values-local.yaml`, `$SECRETS_SET`, uid 1000/gid 2000, migrate=false, own dataOwnership.image) are byte-identical between 93ed971 and HEAD.
- `reuse-values-probe-final.txt`: PASS on the final HEAD; port 18471 is free and the probe's work directory is gone.
- `scripts/ci-local.sh helm gate` on f91b24f: `### tier helm: PASS`, `### tier gate: PASS`, `commit: f91b24f`, no `TRACKED CHANGES NOT IN THIS COMMIT`. Transcript: `.planning/ci-local/ci-local-20261001T165830Z.log`. Local helm is v4.3.0, CI pins v4.2.3.

## Deviations from Plan

### Auto-fixed Issues

None to the code. Two differences in how evidence was produced:

1. INV-C: the full gate run stopped earlier than the plan expected, on the real-chart null case (the same edit also breaks `--set minio.dataOwnership.uid=null` on the real chart, where Helm drops the key). Per the plan's fallback, gate (b) was then run by hand under the same edit (render of a chart without `.minio.dataOwnership.uid`) and it fails with the Codex message. Recorded in `gate-inversions.txt`.
2. INV-G: removing only the `fail` line makes `index` on a string abort with a template error, so the render still fails (rc 1) but without the "must be a map" text; the gate caught exactly that.

Two commits, as planned.

## Not proven

- A live cluster: nothing was applied; OrbStack Kubernetes stayed off.
- The reuse path on helm v4.2.3 (the version pinned in ci.yml): the probe ran on v4.3.0.
- The old official image `minio/minio:RELEASE.2025-09-07T16-13-09Z` running as uid 65532 and its `chown --from -h` in the init container: the image is not present locally and pulling was not allowed. It is only rendered.

## Comment lines

`evidence/comment-lines.txt`: one added comment line in templates, scripts and tests, the `{{/* */}}` line about `--reuse-values` above the helper. Four values.yaml comment lines are operator documentation. No `#` lines were added to `scripts/ci-helm-checks.sh`.

## PR body addendum (round 3)

Codex P1 on `minio.yaml:57`: with `helm upgrade --reuse-values`, Helm replaces the new chart's `values.yaml` with the values of the previous release (`reuseValues` in `pkg/action/upgrade.go`: `chart.Values = oldVals`). A release made before `minio.dataOwnership` existed has no such block, so the helper received nil and the render aborted with `minio.dataOwnership.uid must be a whole number, got <nil>`.

The defaults now live on the template side, in one helper (`octoconv.minioDataOwnership`) that returns uid 65532, gid 65532, migrate true and an empty image (falling back to `minio.image`) for a missing block, a missing key or a null. Values that are set are checked exactly as before (whole non-negative uid/gid, boolean migrate, same messages), and `uid: 0` or `migrate: false` are honoured, not replaced by a default, because absence is detected by kind and not by truthiness. A `dataOwnership` that is neither a map nor null now aborts with `minio.dataOwnership must be a map`. `values.yaml` documents the same defaults, and the CI check requires that a chart without the block, or without any one key, renders byte-identically to the defaults, so the two cannot drift.

Why null means "absent": measured on Helm v4.3.0, when the key exists in the chart's `values.yaml`, `--set k=null` and `-f` with `k: null` delete it; when the key does not exist there (exactly the `--reuse-values` case) the same input leaves it as nil. Treating nil as absent everywhere gives one command one meaning on a fresh install and on `--reuse-values`.

How it was checked: (1) a real `helm upgrade --reuse-values --dry-run=client` (memory driver, a stub answering only API discovery, a release seeded from main's chart) reproduces the Codex error before the fix and passes after it; the manifest it produces is byte-identical to the `helm template` simulation the CI gate uses, and `.chart.values.minio` has no `dataOwnership`, confirming that the reuse path really dropped the new defaults. (2) The gates in `scripts/ci-helm-checks.sh` cover the missing block, each missing key, a partial block, `uid: 0`, `migrate: false`, every null form and the invalid values; the new gates failed first on the previous commit, and each was inverted once. (3) Renders with explicitly set values are byte-identical to the previous commit.

Operator note: `--reuse-values` keeps all old values, including `minio.image` and `minio.mcImage` pointing at the Docker Hub image. The pod then runs the old image as 65532:65532 and the init container chowns with that same image; this is rendered but was not run. `--reset-then-reuse-values` takes the new chart's values (the pinned Chainguard image verified in round 2) and still keeps the operator's own overrides.

Not verified: a live cluster, the reuse path on helm v4.2.3 (the CI pin; the probe used v4.3.0), and the old official MinIO image under uid 65532 with its `chown --from -h`.

## Self-Check: PASSED

- Commits 1be952e and f91b24f exist on quick-minio-chainguard; `git rev-list --count 93ed971..HEAD` is 2; no `Co-Authored-By` in either message.
- Evidence files exist: the probe, the stub, pre/post/final outputs, `ci-helm-red.txt`, `guard-test.txt`, `gate-inversions.txt`, `render-invariance.txt`, `comment-lines.txt`.
- `scripts/ci-local.sh helm gate` passed on f91b24f; tracked tree clean; the stub's port is free.
