---
phase: 72-engine-free-contract-foundations
plan: 03
subsystem: jobs
tags: [measurements, output-role, migration, postgres, check-constraint, go]
requires:
  - phase: 57-operation-and-output-roles
    provides: job_outputs.role, the primary-only MarkDone guard, role-based download_url selection
provides:
  - jobs.OutputRoleMeasurements
  - migration 0018 widening job_outputs_role_check to admit 'measurements'
  - live-DB, API and webhook tests pinning that measurements is never download_url
affects: [74]
status: complete
commits: 2
plan_head_before: d75573b6cb9d5e42029758bd92015befff499969
plan_head_after: e5db18533779353710feff050fad86cb41351544
actuals:
  tokens: 6000
  tasks: 3
  commits: 2
tech-stack:
  added: []
  patterns:
    - "CHECK widening by DROP and re-ADD of the named constraint in a new migration (0016 pattern)"
key-files:
  created:
    - internal/db/migrations/0018_output_role_measurements.sql
  modified:
    - internal/jobs/jobs.go
    - internal/jobs/output_role_test.go
    - internal/api/handlers_test.go
    - internal/worker/webhook_role_test.go
key-decisions:
  - "D-16 schema entry: widen-check-0018 (auto-selected under user delegation, 2026-10-04)"
requirements-completed: [MEAS-05]
---

# Phase 72 Plan 03: Measurements output role Summary

The `measurements` role is accepted by the Go constant, by `job_outputs_role_check` (migration 0018) and by the client-facing selection, and is proven on a live Postgres and in the API and webhook tests never to be served as `download_url`.

## Decision checkpoint

Task 1 (`widen-check-0018` vs `reuse-report-role`): `widen-check-0018`, auto-selected under user delegation (2026-10-04). Task 2 implements exactly that.

## Accomplishments

- `jobs.OutputRoleMeasurements = "measurements"` beside the primary and report roles.
- `0018_output_role_measurements.sql`: two statements, no comments, drops and re-adds `job_outputs_role_check` with `primary, report, preview, thumbnail, measurements`. 0015 is untouched.
- Live-DB tests (Postgres 18 on localhost:5434, zero SKIP): `legal` list extended with `measurements` while the `bogus` rejection assertion stays; `TestMarkDoneAcceptsPrimaryPlusMeasurements` (both roles read back, MarkDone succeeds); `TestMarkDoneRejectsJobWithOnlyMeasurementsOutput` (MarkDone errors, job stays active).
- `TestGetJob_DownloadURLNeverMeasurements`: measurements at ordinal 0 plus primary at ordinal 1 gives `download_url` equal to the primary URL, `outputs[0].role == "measurements"`; a done job with only a measurements row has no `download_url` key.
- `TestSetWebhookDownloadURL_NeverSelectsMeasurements`: same two shapes; presign called once with the primary key, and zero times when only measurements exists.

## Verification

- Task 2 gate printed `ROLE-LIVE-OK` (every test PASS, no SKIP, no FAIL) on go1.26.4.
- Task 3 gates green; `TestGetJob_DownloadURLNeverMeasurements` yields 3 PASS lines (parent plus two subtests).
- `go build ./...` and `go vet ./internal/jobs/ ./internal/api/ ./internal/worker/` green; `go test ./internal/jobs/ ./internal/api/ ./internal/worker/ ./internal/db/` green with the live DB.
- Acceptance greps: 0018 is the last migration, `'measurements'` count 1, `job_outputs_role_check` count 2, SQL comment count 0, 0015 byte-identical to d034437.
- `internal/api/handlers.go` and `internal/worker/worker.go` were not modified by this plan (the only `handlers.go` change since d034437 is 72-01's c7b1dc9).

## Deviations from Plan

None - plan executed exactly as written.

## Known Stubs

None.

## Threat Flags

None.

## Task Commits

- Task 2: `6578129` feat(72-03): admit the measurements output role in the Go constant and the role CHECK
- Task 3: `e5db185` test(72-03): pin that measurements is never served as download_url in the job response or the webhook

## Self-Check: PASSED
