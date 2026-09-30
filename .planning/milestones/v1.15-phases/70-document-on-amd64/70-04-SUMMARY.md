---
phase: 70-document-on-amd64
plan: 04
subsystem: infra
tags: [landlock, sandbox, document-worker, liveproof, emulation, compose, helm, conf02]

requires:
  - phase: 70-document-on-amd64
    provides: "document class confined in code, four generic gate checks passing, /tmp route read-write, DIVERGENCES: 0"
provides:
  - "scripts/sandbox-liveproof.sh document: emulated-amd64-refuses, concurrency-guard, sweeps-shared-tmp; NOT RUN accounting"
  - "live proof that the committed Dockerfile built for linux/amd64 and run emulated on arm64 refuses before Postgres naming DOCUMENT_SANDBOX_DISABLED"
  - "DOCUMENT_SANDBOX_DISABLED forwarded empty by compose, documented unset in .env.example, reachable via documentWorker.extraEnv"
  - "DOCUMENT_WORKER_CONCURRENCY 1 in compose, .env.example and the ConfigMap"
affects: [70-05, docker-compose.yml, .env.example, deploy/chart/octoconv]

actuals:
  tokens: 14000
  tasks: 2
  commits: 2
plan_head_before: 31c230fef1ecd6a659435613a735f4ba7063539b
plan_head_after: 9f639c66638d31cf7aae57d4cd9c01899d091267

tech-stack:
  added: []
  patterns:
    - "A check that ran nothing prints NOT RUN, bumps a separate counter, and changes the final line to (<n> checks, <m> not run) instead of counting as a pass"
    - "The emulation refusal is proven by building the same Dockerfile for the other architecture, not by code in the worker"

key-files:
  created:
    - .planning/phases/70-document-on-amd64/evidence/liveproof-document-70-04.txt
  modified:
    - scripts/sandbox-liveproof.sh
    - docker-compose.yml
    - .env.example
    - deploy/chart/octoconv/templates/deployment-document-worker.yaml
    - deploy/chart/octoconv/templates/configmap.yaml

key-decisions:
  - "Route applied: read-write /tmp (70-02). The document gate has 7 checks: four generic, emulated-amd64-refuses, concurrency-guard, sweeps-shared-tmp; the socket-only tmp-left-clean branch was not written"
  - "concurrency-guard matches the guard's own wording 'DOCUMENT_WORKER_CONCURRENCY=2 with the sandbox enabled', because the worker's earlier log line already names the variable and would satisfy a bare match"
  - "startupProbe left at 24 x 5 = 120 s: measured start-to-postgres of the confined worker is at most 6.4 s wall, plus 30 s is far below 120 s (and the 60 s preflight timeout plus 30 s is 90 s)"

requirements-completed: []
requirements-advanced: [CONF-01, CONF-02, CONF-07]

coverage:
  - id: D1
    description: "An image built for linux/amd64 from the committed Dockerfile and run emulated on this arm64 host refuses to start before Postgres, naming DOCUMENT_SANDBOX_DISABLED, with no emulation detection code in the worker"
    requirement: CONF-02
    verification:
      - kind: other
        ref: "scripts/sandbox-liveproof.sh document -> PASS: document emulated-amd64-refuses (evidence/liveproof-document-70-04.txt)"
        status: pass
    human_judgment: false
  - id: D2
    description: "The check is never counted as a pass when nothing is emulated"
    requirement: CONF-07
    verification:
      - kind: other
        ref: "same script with a docker shim reporting an amd64 server -> NOT RUN line and 'ALL PASS (6 checks, 1 not run)'"
        status: pass
    human_judgment: false
  - id: D3
    description: "The measured read-write /tmp route on the running image: concurrency 2 refused naming the variable and the shared /tmp; planted /tmp entries gone after the preflight"
    requirement: CONF-01
    verification:
      - kind: other
        ref: "scripts/sandbox-liveproof.sh document -> PASS: document concurrency-guard, PASS: document sweeps-shared-tmp"
        status: pass
    human_judgment: false
  - id: D4
    description: "The escape hatch is reachable from compose and the chart and off by default; concurrency equals the route default in every configured place"
    requirement: CONF-02
    verification:
      - kind: other
        ref: "plan Task 2 verify: helm template with extraEnv renders true, compose config renders true when set and empty when unset, scripts/ci-helm-checks.sh exit 0"
        status: pass
    human_judgment: false

duration: n/a
completed: 2026-09-30
status: complete
---

# Phase 70 Plan 04: Emulated-amd64 refusal proven live, document remedy and concurrency deployed Summary

**The committed document Dockerfile built for linux/amd64 and run emulated on this arm64 host refuses before Postgres with sandbox.ResolveHelper's own message naming DOCUMENT_SANDBOX_DISABLED, the live gate gained the measured read-write /tmp route checks plus NOT RUN accounting, and compose and the chart now forward the remedy (unset) and run document at concurrency 1.**

## Route applied

- `/tmp` route: **read-write** (70-02, applied in 70-03 code). Gate checks `document concurrency-guard` and `document sweeps-shared-tmp`; `document tmp-left-clean` (socket-only or none) was not written.
- Document gate total on this route: **7 checks** (four generic + emulated-amd64-refuses + concurrency-guard + sweeps-shared-tmp).

## Gate results

- `scripts/sandbox-liveproof.sh document` -> `ALL PASS (7 checks)` on a freshly built native image `sha256:8bfc7b4ba416d57a46f1528134bacc14aa4560de3cfdaf41803f8d77acae3650`; the emulated image (`--platform linux/amd64`, Architecture amd64) was `sha256:83e3ba54e1b9f995ab617fb43dcd984cf93b2fa55ac323b1f6afe2349e67f83f`. Gate started at commit `31c230f` (HEAD before this plan's commits); the tree differed from it only by the script under test (image inputs identical to `31c230f`). Transcript: `evidence/liveproof-document-70-04.txt`.
- Emulated refusal text: `document sandbox: landlock is unavailable on this kernel; ... Set DOCUMENT_SANDBOX_DISABLED=true to accept that deliberately`; no `🔒`, no `postgres:`, non-zero exit.
- Concurrency guard text: `document sandbox: DOCUMENT_WORKER_CONCURRENCY=2 with the sandbox enabled: this class's ruleset grants the container's shared /tmp read-write ... (CONF-06)`.
- NOT RUN branch exercised with a docker shim that reports an amd64 server: `NOT RUN: document emulated-amd64-refuses (the docker server is amd64: nothing is emulated here)` and `ALL PASS (6 checks, 1 not run)`. Recorded at the end of the evidence file (shim run output reproduced, not a second genuine daemon).
- `scripts/conf05-crosscheck.sh document` -> `MATCH: soffice ...`, `MATCH: verapdf ...`.
- Task 2 verify (helm extraEnv renders `true`, default renders nothing, compose renders `true` when set and empty when unset) passes; `scripts/ci-helm-checks.sh` exits 0; `go test ./internal/queue/... ./cmd/...` pass; `git diff --exit-code internal/convert/exec.go` exits 0; `values.yaml` untouched.

## Start-to-postgres time and startupProbe

Confined document-worker, fresh image, `--cpus 2 --memory 1g`, unreachable DATABASE_URL, five runs: log time from the landlock line to the postgres attempt 4-6 s; wall clock per whole `docker run` 6.32, 4.89, 6.40, 5.29, 5.64 s. Worst 6.4 s + 30 s = 36.4 s, below `failureThreshold x periodSeconds` = 24 x 5 = 120 s. **startupProbe unchanged.** (The preflight's own 60 s timeout plus 30 s is 90 s, also below 120 s.)

## Accomplishments

- `scripts/sandbox-liveproof.sh`: `not_run` helper and `NOT_RUN_COUNT`; `check_document_emulated_amd64_refuses` (reads `docker version --format '{{.Server.Arch}}'`, builds the same Dockerfile with `--platform linux/amd64` to a second unique tag, runs it through `run_gated` with no seccomp profile, removes the tag); `check_document_concurrency_guard`; `check_document_sweeps_shared_tmp` (plants a plain file and an `OSL_PIPE_`-shaped name); final line `ALL PASS (<n> checks[, <m> not run])`; header updated.
- `docker-compose.yml`: `DOCUMENT_SANDBOX_DISABLED: "${DOCUMENT_SANDBOX_DISABLED:-}"` with a three-line comment; `DOCUMENT_WORKER_CONCURRENCY: "1"` with reason.
- `.env.example`: `DOCUMENT_SANDBOX_DISABLED` block in the PROBE register, ending with the commented `# DOCUMENT_SANDBOX_DISABLED=true`; `DOCUMENT_WORKER_CONCURRENCY=1` with reason.
- `deployment-document-worker.yaml`: extraEnv comment names the remedy and the refused concurrency above 1; `configmap.yaml`: `DOCUMENT_WORKER_CONCURRENCY: "1"`.

## Task Commits

1. **Task 1: emulated-amd64 refusal and /tmp route checks in the document gate (tracer)** - `86bf658` (test)
2. **Task 2: remedy forwarded, concurrency 1** - `9f639c6` (chore)

The tracer gate passed end to end before Task 2 was started.

## Deviations from Plan

**1. [Rule 1 - Bug avoided] concurrency-guard matches the guard's wording, not the bare variable name**
- **Found during:** Task 1 design
- **Issue:** `cmd/document-worker` logs `📄 DOCUMENT_WORKER_CONCURRENCY=2` before the guard runs, so a check for the variable name alone would pass on any worker that logged it, even one that never refused.
- **Fix:** the check requires `DOCUMENT_WORKER_CONCURRENCY=2 with the sandbox enabled` (the guard's own text), the shared `/tmp`, no `🔒`, no `postgres:`, non-zero exit.
- **Files modified:** scripts/sandbox-liveproof.sh. **Commit:** `86bf658`.

**2. [Process] NOT RUN branch shown with a docker shim**
- No amd64 docker daemon is available here; the branch was exercised by a shim that answers `docker version` with amd64 and forwards everything else. It proves the accounting, not an amd64 daemon.

**Total deviations:** 1 hardening, 1 process. **Impact:** none on scope.

## Inversions performed

- Emulated check non-vacuous: the same image run natively (the confined-startup check in the same run) logs `🔒` and reaches `postgres:`, which the emulated check treats as failure; the emulated run shows neither.

## Auth gates

None.

## Known Stubs

None.

## Threat Flags

None beyond the plan's threat model (T-70-17, T-70-23, T-70-24): `"${VAR:-}"` forwarding verified set and unset, values.yaml untouched, nothing checked in defaults the variable on (no `DOCUMENT_SANDBOX_DISABLED` in docker-compose.e2e.yml or any values file).

## Self-Check: PASSED

- Files exist: `scripts/sandbox-liveproof.sh`, `.planning/phases/70-document-on-amd64/evidence/liveproof-document-70-04.txt`.
- Commits `86bf658` and `9f639c6` exist; `git rev-list --count 31c230f..HEAD` measured 2 before this SUMMARY commit.
- Acceptance: `grep -c 'NOT RUN:'` in the script is 2 (>= 1); `grep -c emulated-amd64-refuses` is 4 (>= 1); compose concurrency renders 1 and the ConfigMap line count is 1; `.env.example` has 0 `^DOCUMENT_SANDBOX_DISABLED=` lines and 2 mentions; `^DOCUMENT_WORKER_CONCURRENCY=1 ` count 1; chart file mentions the variable once.
- Scratch image `octoconv-document:t704-time` and the gate's uniquely tagged images removed; no compose stack was started; STATE.md and ROADMAP.md untouched.

---
*Phase: 70-document-on-amd64*
*Completed: 2026-09-30*
