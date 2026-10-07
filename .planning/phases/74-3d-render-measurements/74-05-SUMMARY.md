---
phase: 74-3d-render-measurements
plan: 05
subsystem: cad-engine
tags: [atomic-outputs, render, measurements, exposure]
status: complete
requires:
  - phase: 74-04
    provides: CADRenderConverter, CADMeasurementsPath, render sentinels (unregistered)
provides:
  - Repo.AddOutputs, one transaction for every output row of a job
  - process() order for descriptors with side outputs (hook, uploads, one AddOutputs, MarkDone)
  - cadSideOutputs and CADDescriptor.SideOutputs
  - render exposed in one commit (registry, cad-worker, stl ceiling, allowlists)
affects: [74-06, 74-07]
key-files:
  created:
    - internal/jobs/add_outputs_test.go
    - internal/worker/cad_outputs_test.go
  modified:
    - internal/jobs/repo.go
    - internal/worker/worker.go
    - internal/worker/runner.go
    - internal/worker/descriptors.go
    - internal/worker/probe_sideoutput_test.go
    - internal/worker/pinning_test.go
    - internal/worker/worker_test.go
    - internal/worker/webhook_role_test.go
    - internal/convert/converters.go
    - internal/convert/convert_test.go
    - internal/convert/operations_test.go
    - cmd/cad-worker/main.go
    - internal/api/api.go
    - internal/api/cad_ceiling_test.go
    - internal/api/cad_opts_test.go
    - internal/api/formats_handlers_test.go
    - internal/api/pre58_pinning_test.go
    - internal/api/handlers_test.go
key-decisions:
  - "A side-output hook runs before any upload so an invalid measurements body or a rejected PNG leaves S3 and Postgres untouched; the nil-hook path is statement-for-statement the old one"
  - "cad becomes the one engine class serving two operations (convert, render); TestOneOperationPerEngineClass pins it by name instead of being relaxed, because every subsystem it guards is keyed by job.Engine"
commits: 3
plan_head_before: 8a2b4db7f15e237bc3b8d564169584ed88513c8c
plan_head_after: 6aa69702372c0b7fecdc8730b906a71e468ba72c
actuals:
  tokens: 14200
  tasks: 3
  commits: 3
requirements-completed: [MEAS-01, RNDR-01]
---

# Phase 74 Plan 05: Atomic outputs and the render exposure Summary

A render job can only become done with its PNG and measurements rows written in one transaction, proven by an inversion test that fails against the naive sequence, and render is now visible to clients in a single commit.

## What was built

- **Repo.AddOutputs (D-07, R-07).** The AddOutputWithRole upsert is one package constant used by both writers. `AddOutputs` refuses an empty slice or an empty role before opening the transaction, then runs every row inside one `pgx.BeginFunc`, wrapping errors with the ordinal. `MarkDone` and `transition` are untouched.
- **Inversion proof.** `TestAddOutputsRollsBackWhenTheSecondRowFails` forces the second row to fail the role CHECK: zero visible rows, `MarkDone` refuses naming "0 primary outputs", the job stays active. In the same function a control job runs `AddOutputWithRole(primary)` then `AddOutputWithRole(bad role)` and must leave exactly one visible row. Run live against the project's compose postgres: the real `AddOutputs` passes; with `AddOutputs` temporarily replaced by the naive sequence the test FAILED ("AddOutputs left [primary] visible after a failed second row"); with DATABASE_URL unset it SKIPs, and the verify gate rejects any SKIP. Original restored before the commit.
- **process().** When `h.sideOutputs != nil`: hook (validates in memory), upload primary, upload each extra at ordinal i+1, one `AddOutputs` (primary role `primary` at ordinal 0), `MarkDone`. With no hook it still calls `AddOutput` then `MarkDone`. `convertRepo` gained `AddOutputs`; `pinRepo` and `opGateRepo` implement it.
- **cadSideOutputs.** Only for operation render: reads `CADMeasurementsPath(outPath)`, `measurements.Validate`, returns one `measurements.json` SideOutput. Missing or invalid wraps `ErrCADMeasurementsInvalid` (terminal, engine_error). Convert jobs return no extras and still write their single row through `AddOutputs` with role primary. The probe report now goes through `AddOutputs` too (ordinal 1, role report).
- **Hermetic proof.** `cad_outputs_test.go` runs the real `process()` against an in-process fake S3 (httptest, minio client) and a recording repo: both rows in one call then MarkDone; failed second upload, failed AddOutputs, missing/invalid sidecar (missing, volume 0, not JSON) and a rejected PNG each leave no row and no MarkDone, and the validation failures upload nothing.
- **Exposure (commit 6aa6970).** `converters.go` registers `CADRenderConverter{}`; `cmd/cad-worker/main.go` replaces both converters; `defaultMaxFormatBytes[cad]` gains `stl: CADMaxSTLBytes`. `/v1/formats` and `/v1/inspect` offer step, iges, stl to png under render.

## Mandatory additions

1. **Two HTTP render cases** (cad_opts_test.go): `TestCreateJobRenderInvalidViewIsInvalidOptions` (422 invalid_options) and `TestCreateJobRenderOfAnSTLWithQualityIsOptsNotApplicable` (422 opts_not_applicable), both asserting no upload, create or enqueue. Also a 202 case per source format and a routing subtest to the cad queue.
2. **Landlock set is provisional.** The render stage's read-only set (`cadRenderReadOnly`, seeded equal to the mesh paths in 74-04) is not final: plan 74-06 measures it, and nothing in this plan proves it sufficient or minimal.
3. **Comment guard extended to runner.go.** Against the phase-73 merge-base the only added `//` line in runner.go is the edited SideOutputs doc line ("BEFORE any output is uploaded or recorded, and returns"); the now-false "Ordering is deliberate ... AFTER" paragraph was deleted, not rewritten. worker.go, descriptors.go, repo.go and add_outputs_test.go add no `//` lines.

## Flipped pins (every one listed)

- `convert_test.go` TestDefaultRegistersOnlyAllowlistedCADPairs: five exact (operation, pair) keys; vacuity guard kept.
- `operations_test.go` TestOperationsDerivedFromRegistry (convert, render, sanitize; "Render" added to the case-mismatch list, "render" removed from the unknown list) and TestOneOperationPerEngineClass (cad pinned to exactly convert and render).
- `formats_handlers_test.go`: operations tree has three keys, render holds only cad; NoOperationOutsideRegistry drops render; the CAD allowlist is per `engines`/`operations.convert`/`operations.render`.
- `pre58_pinning_test.go`: operations may gain sanitize and render; render must be present.
- `cad_ceiling_test.go`: ceiling map has step, iges, stl; inspect test is a step/iges/stl table checking the convert and render offers and per-format max_bytes.
- `handlers_test.go`: unknown-operation list uses inspect instead of render; render routing subtest; GET job ordinal-1 measurements case.
- `cad_opts_test.go`: TestCreateJobRenderStaysUnregistered replaced by the cases above.
- `probe_sideoutput_test.go` TestOnlyProbeDeclaresSideOutputs now probe and cad; `webhook_role_test.go` gains measurements-after-primary.

## Verification

- `GOTOOLCHAIN=go1.26.4`: gofmt clean, `go build ./...`, `go vet ./...`, `go test ./... -count=1` pass; jobs tests pass with DATABASE_URL set and no SKIP in the AddOutputs/role set; label-pin tests pass; `scripts/ci-local.sh helm gate` ended ALL REQUESTED TIERS PASSED.
- No golden, `internal/api/testdata` or `internal/metrics` diff against the phase-73 merge-base; `git diff main -- internal/convert/exec.go` empty.
- 17 passing atomicity, probe and webhook tests matched the Task 2 pattern (plan: at least 6). All three commits are SSH-signed. Exposure commit: `6aa6970` contains converters.go, main.go and api.go together.
- The project's own compose postgres was started for the DB tests and stopped afterwards; nothing else was touched.

## Deviations from Plan

**1. [Rule 1 - Bug] D-14 one-operation-per-engine test**
- TestOneOperationPerEngineClass failed once render registered. Its own text says lifting the invariant needs an explicit decision; the plan's exposure is that decision, and every subsystem it lists keys on job.Engine. The test now allows exactly convert and render for cad and still fails for any other drift. Not on the plan's list for that reason; operations_test.go is. Commit 6aa6970.

**2. [Plan interpretation] `grep -c 'AddOutputs(' worker.go` is 1**
- The interface declaration also contains `AddOutputs(`, so the count is 2; there is exactly one call site (`h.repo.AddOutputs(` count 1) and one `h.repo.AddOutput(` in the nil-hook branch.

**3. [Plan interpretation] handlers_test.go in the worker commit**
- The ordinal-1 GET-job case belongs to Task 2's behaviour list, so it landed in commit 80c4700; the exposure-only edits to the same file are in 6aa6970.

**4. [Plan interpretation] fake S3, not a storage seam**
- `h.store` is a concrete `*storage.Client` with no fake; the hermetic tests use an httptest S3 endpoint through `storage.New` rather than introducing an interface.

## Known Stubs

None.

## Threat Flags

None beyond the register. T-74-10 is mitigated by `AddOutputs` and the inversion; T-74-11 by role-driven selection tested at ordinal 0 and 1 for GET and webhook. Residual: a failed second upload or a failed AddOutputs leaves orphan objects in S3 with no row (never visible to a client).

## Notes for later plans

- runner.go's SideOutputs doc still says "nil for seven of the eight engines ... only probe populates it", now stale (cad also does). Fixing it would add comment lines; left for a decision.
- 74-07 owns the `scripts/cad-liveproof.sh` FORMATS row (`operations_convert_cad=2`) that the render pairs now extend.

## Self-Check: PASSED

add_outputs_test.go and cad_outputs_test.go exist; commits 0cefdc5, 80c4700 and 6aa6970 are ancestors of HEAD.
