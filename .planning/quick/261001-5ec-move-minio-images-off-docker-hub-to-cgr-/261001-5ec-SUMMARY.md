---
phase: quick-261001-5ec
plan: 01
subsystem: infra
tags: [minio, docker-compose, helm, chainguard, image-pinning]
requires: []
provides:
  - "compose and chart run MinIO and mc from cgr.dev/chainguard/minio pinned by multi-arch index digest"
  - "TestMinioImagePinnedByDigest / TestMinioImageRefRejectsUnpinnedForms guard"
affects: [docker-compose.yml, deploy/chart/octoconv, README.md, CLAUDE.md]
tech-stack:
  added: []
  patterns: ["single digest-pinned image string shared by compose and chart, equality held by a derived-set test"]
key-files:
  created: [internal/e2e/minio_image_pin_test.go]
  modified:
    - docker-compose.yml
    - deploy/chart/octoconv/values.yaml
    - deploy/chart/octoconv/templates/minio.yaml
    - deploy/chart/octoconv/templates/job-createbucket.yaml
    - deploy/chart/octoconv/templates/_helpers.tpl
    - README.md
    - CLAUDE.md
decisions:
  - "Pin form is repository@sha256:... without :latest (D-02; k8s reads :latest@digest as tag latest)"
  - "Pin stays at fa23f6a6... although :latest has moved on (orchestrator instruction; e2e passed on it)"
  - "mc comes from the MinIO image; the separate mc image key was removed from the chart"
metrics:
  duration: "~18 min wall (plus ~9 min for the ci-local run)"
  completed: 2026-10-01
status: complete
actuals:
  tokens: 4650
  tasks: 3
  commits: 3
plan_head_before: caf0718d464086321c4123ff221a9c2d53829f7c
plan_head_after: d93424a83e4acdbbeaf3cb4e8ffbea1329f1c0f1
---

# Phase quick-261001-5ec Plan 01: MinIO images off Docker Hub Summary

Compose and the Helm chart now run MinIO, the createbucket job and the chart's wait-for-bucket init containers from one `cgr.dev/chainguard/minio@sha256:fa23f6a6...2edc72` reference (multi-arch index digest, no `:latest`), with a test that keeps every reference pinned and equal.

## Commits

| Task | Commit | What |
|------|--------|------|
| 1 (tracer) | fd9784e | docker-compose.yml: minio + createbucket on the pinned image, rationale and bump procedure comment |
| 2 | c5618ec | chart: `minio.image` is the single source (minio Deployment, 2 Job containers, wait-for-bucket in `octoconv.waitForDeps`); fsGroup 65532; `internal/e2e/minio_image_pin_test.go` |
| 3 | d93424a | README note and CLAUDE.md deployment line |

Todo move, STATE.md edit, this SUMMARY and `evidence/` are left uncommitted for the orchestrator (the todo rename is staged via `git mv`).

## Measured (evidence/)

- Image (arm64 local): Entrypoint `/usr/bin/minio`, Cmd null, User 65532, HOME `/home/nonroot` writable, `/data` 0777; minio, mc, sh, bash present; no curl/wget/grep; minio RELEASE.2026-07-17T12-07-51Z. (`chainguard-minio-measure.txt`)
- Registry: pin is an OCI index with linux/amd64 `sha256:0284e595...` and linux/arm64 `sha256:f5ae133d...`; both manifests 200, config and all layers (12 amd64, 12 arm64) 307 via anonymous token. `:latest` is now `sha256:4692462f...`, not the pin.
- Root-written volume probe (`rootvol-probe.txt`): the non-root image does NOT start on a volume initialised by root. Server log: `unable to rename (/data/.minio.sys/tmp -> /data/.minio.sys/tmp-old/...) file access denied`, then `FATAL Unable to initialize backend: Unable to write to the backend`. A one-shot `chown -R 65532:65532 /data` from a root container fixes it (ready, write_rc=0). The README carries both the symptom and that command.
- Compose live, aliases removed, fresh `octoconv_minio_data` (`compose-tracer.txt`): minio healthy, Config.Image of both containers equals the pin, image id matches, uid 65532, createbucket exit 0 with "Bucket created successfully", `mc ls` shows `octoconv/`. wait-for-bucket commands passed at `--memory 64m --cpus 0.1` (`bucket-ok`, no OOM).
- Chart render with values-local.yaml: pin in minio Deployment, both Job containers and all 11 wait-for-bucket init containers; no empty `image:`; `fsGroup: 65532` on the MinIO pod; no `mcImage` anywhere; `helm lint` and `scripts/ci-helm-checks.sh` green.
- Guard (`guard-test.txt`): RED on the unchanged chart naming values.yaml:271, job-createbucket.yaml:58/69, _helpers.tpl:308; GREEN after; a one-hex-character corruption in docker-compose.yml:66 fails (equality), a tag-only `minio.image` fails; tree clean and test green after both probes.
- `GOTOOLCHAIN=go1.26.4 scripts/ci-local.sh helm gate e2e` on commit d93424a: helm PASS, gate PASS, e2e PASS (`.planning/ci-local/ci-local-20261001T012020Z.log`, no TRACKED CHANGES warning; only the pre-existing helm 4.3.0 vs 4.2.3 version WARNING). No octoconv containers afterwards, dockerdev set unchanged, both old aliases still absent.

## Not proven

- Chart on a live cluster (OrbStack Kubernetes off): only `helm template`/lint. The fsGroup/OnRootMismatch comment says so.
- That a clean Docker store can pull the pin: shown through the registry API, not `docker pull`, because the layers were already local (dockerdev).
- amd64 at runtime: the index lists it, the runs were arm64.

## Deviations from Plan

None to the plan's scope. Two execution notes:

1. **[Probe script] mc ready hang.** The first run of probe (b) hung because `mc ready` blocks after the server dies; I killed that container, added a per-call guard to the probe script and reran. The first run's server log ended with the same FATAL line (noted in `rootvol-probe.txt`).
2. **[Observation] The e2e tier's `down -v` also removed `octoconv_postgres_data`.** The plan only anticipated `octoconv_minio_data` being removed by that teardown. Before the gate both octoconv volumes existed; after it neither does. Dev Postgres data of this project is gone; the volume is recreated by the next `docker compose up`. dockerdev volumes were not touched.

## Known Stubs

None.

## Threat Flags

None. No new network surface; the process now runs as 65532 instead of root (T-261001-5ec-04 improved).

## Self-Check: PASSED

- fd9784e, c5618ec, d93424a present in `git log main..HEAD`.
- `internal/e2e/minio_image_pin_test.go`, all five evidence files, `.planning/todos/completed/2026-09-30-minio-images-off-docker-hub.md` exist; pending copy gone; STATE.md says "Ровно четыре".
- Task 1, 2 and 3 automated verify commands exit 0.
