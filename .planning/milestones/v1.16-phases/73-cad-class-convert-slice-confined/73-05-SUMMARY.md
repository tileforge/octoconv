---
phase: 73-cad-class-convert-slice-confined
plan: 05
subsystem: cad-exposure
tags: [cad, per-format-ceiling, engine-registry, reconciler, compose, helm, keda]
requires:
  - phase: 73-02
    provides: CADConverter, the byte ceilings, ValidateCADTimeouts
  - phase: 73-04
    provides: cad:job, CADDescriptor, cmd/cad-worker, the final Dockerfile.cad-worker
provides:
  - Config.MaxFormatBytes, defaultMaxFormatBytes (cad: step 63 MiB, iges 135 MiB) and Server.sizeLimit, read by the create-time check and by inspect offers
  - the cad class registered, routed, recovered, bounded, documented and deployable in one commit (D-14)
  - Queue client EnqueueCADJob, env CAD_ENGINE_TIMEOUT and CAD_MAX_RETRY in every queue-client service
  - compose service cad-worker, chart Deployment and ScaledObject for cad (minReplicaCount 0)
affects: [73-06]
actuals:
  tokens: 19200
  tasks: 2
  commits: 3
plan_head_before: fb9e25074eba1925c46daa5f86eb7bc9c863eb21
plan_head_after: 49729fde8c85d3664f7d2ff7c4ef516f8c08b739
tech-stack:
  added: []
  patterns:
    - one size rule (sizeLimit) with two readers, create and inspect
    - cad filtered out of the pinned goldens with withoutEngine and its presence asserted, never a golden edit
key-files:
  created:
    - internal/api/cad_ceiling_test.go
    - deploy/chart/octoconv/templates/deployment-cad-worker.yaml
    - deploy/chart/octoconv/templates/scaledobject-cad.yaml
  modified:
    - internal/api/api.go
    - internal/api/handlers.go
    - internal/api/estimate.go
    - internal/convert/convert.go
    - internal/convert/converters.go
    - internal/convert/sniff.go
    - internal/queue/queue.go
    - internal/queue/client.go
    - cmd/api/main.go
    - internal/reconciler/reconciler.go
    - docker-compose.yml
    - .env.example
    - deploy/chart/octoconv/templates/configmap.yaml
    - deploy/chart/octoconv/values.yaml
    - docs/ERROR-CODES.md
    - README.md
key-decisions:
  - "IGES ceiling is 135 MiB (141557760), not 155; STEP 63 MiB (66060288); engine timeout 406 s, pod grace 426 s"
  - "The engine-wide cad ceiling in defaultMaxEngineBytes is CADMaxIGESBytes, the larger of the two, so an engine-only reader is never stricter than the per-format rule"
  - "keda.cad: threshold 1, maxReplicaCount 2, scaleDownStabilizationSeconds 900 (the 406 s engine timeout is past the 300 s HPA default window, the probe argument), minReplicaCount 0"
  - "The chart's global MAX_UPLOAD_BYTES and INSPECT_MAX_UPLOAD_BYTES (100 MiB) are not raised (R-04); the min() effect is documented"
requirements-completed: [CADW-01, CCONV-01, CADIN-04]
duration: 100min
completed: 2026-10-05
status: complete
---

# Phase 73 Plan 05: Per-format ceilings and the atomic exposure of cad Summary

**STEP and IGES now convert to STL through a class that exists everywhere at once: registry, queue, enqueue route, reconciler route, ceilings, published timeout, estimate source, compose service and chart Deployment and ScaledObject, introduced by one commit while the other nine classes, every golden and the metric label pins stay byte-identical.**

## Accomplishments

- **Per-format ceilings (Task 1, `831c04b`).** `Config.MaxFormatBytes` follows the nil-triggers-default discipline of `MaxEngineBytes` (a caller map is never mutated); `defaultMaxFormatBytes` holds only `cad: {step, iges}` taken from the convert constants. `Server.sizeLimit(engine, format)` returns the per-format limit when one exists, else the engine limit. The post-detection size check and `availableOperations` both read it. A per-format hit logs `reason=format_size_limit` and answers 413 `payload_too_large` with `<format> file exceeds the <N> MiB limit for this conversion` (a limit that is not whole MiB is stated in bytes); an engine-level hit keeps today's exact line and message. Inspect reports the same number as `max_bytes`.
- **Tests for the ceilings.** The behaviours are proven on an injected map over pandoc (`md` limited to 1 MiB, `rst` at the same size accepted, file exactly at the limit accepted, no upload, job or enqueue on refusal, inspect `max_bytes` 1048576 and `payload_too_large`). A zero Config is pinned to the convert constants with no other engine. The lying binary STL (`0xFFFFFFFF` count, one record) is answered 422 `truncated_cad_file` with no upload and no enqueue. In Task 2 the defaults are exercised for real: a STEP of 63 MiB + 1 under `.step` and `.stp` and an IGES of 135 MiB + 1 under `.iges` and `.igs` are each refused with 413 naming the format; inspect of a small STEP reports the cad offer with `max_bytes` 66060288 and estimate source `unmeasured`.
- **The atomic exposure (Task 2, `49729fd`).** One commit, 32 files:
  - `AllEngines()` gains `cad` (doc comment made true); `Default.Register(CADConverter{})`; MIME types `model/step`, `model/iges`, `model/stl`.
  - `AllConvertQueues()` gains `cad`; the client reads `CAD_MAX_RETRY` (2) and `CAD_ENGINE_TIMEOUT` (406 s) and has `EnqueueCADJob`.
  - `Enqueuer.EnqueueCADJob`, the `enqueueForEngine` case, the reconciler interface and sweep case (the default comment now names only archive), `defaultEngineTimeouts`, `defaultMaxEngineBytes`, `engineEstimateSource` (`unmeasured`, absent from `engineMeasuredDefaultSeconds`), `engineTimeoutsFromEnv`.
  - Test doubles (`fakeQueue`, `fakeEnqueuer` with `cadCalls`/`cadCallIDs`, `quotaCountingEnqueuer`), `TestEveryEngineHasAnEnqueueRoute` and `TestCreateJobRoutesEveryEngineToItsQueue` fixtures (a valid STEP to `stl`), and a test that non-empty `opts` on a cad job is 422 with no upload and no enqueue.
  - Compose: the 406 s / 2 pair in all twelve services that carried the TECTONIC pair, and a new `cad-worker` (426 s grace, 2 CPU, 4g, concurrency 1, guard 30 s, mesh 316 s, `CAD_SANDBOX_DISABLED` passthrough).
  - `.env.example` cad block (derivation, the CAD_ENGINE_TIMEOUT line says "not measured in the production worker"); chart ConfigMap (no `CAD_SANDBOX_DISABLED`, `MAX_UPLOAD_BYTES` still 104857600), `cadWorker` (grace 426, 2 CPU / 4Gi) and `keda.cad` values, `deployment-cad-worker.yaml`, `scaledobject-cad.yaml` (`queue="cad"`, `minReplicaCount: 0`); `scripts/ci-helm-checks.sh` counts ten ScaledObjects and isolates `cad`; chart-grace table; `ci.yml` cache scopes in both bake steps.
- **Guards turned into allowlists.** `TestDefaultRegistersNoCADPair` became `TestDefaultRegistersOnlyAllowlistedCADPairs` (any pair touching step, stp, iges, igs, stl, dxf, dwg or whose class is cad must be exactly `(convert, cad, step, stl)` or `(convert, cad, iges, stl)`, both present, vacuity guard kept) and `TestListFormats_AdvertisesNoCADFormat` became `TestListFormats_AdvertisesOnlyAllowlistedCADPairs` on the `/v1/formats` body (both under `engines` and `operations.convert`). `TestListFormats_EnginesKeyUnchanged`, `TestPre57GoldenListFormats` and `assertFormatsGolden58ModuloSanitize` strip `cad` with `withoutEngine` and assert the key is present in the live body. No golden file changed.
- **Leftovers from 73-04.** `docs/ERROR-CODES.md`: the `invalid_options` row no longer says "All six" (it names every class that re-parses `opts`, tectonic and cad included, and says cad accepts none); the "While no CAD engine is registered" sentence is replaced by the truth, including the min() effect; the `payload_too_large` row mentions the per-format ceiling. The Redis-gated `TestEnqueueCADJob` follows `TestEnqueueOCRConvert` (it also asserts the task type and `MaxRetry`); it skips here because there is no Redis of mine on this host.

## The min() effect with the chart's global ceiling (R-04)

In the Helm chart `MAX_UPLOAD_BYTES` and `INSPECT_MAX_UPLOAD_BYTES` are 100 MiB and were not raised. A STEP (63 MiB) is under both. An IGES between 100 MiB and 135 MiB is refused at the transport with the generic message, not the per-format one, so the effective IGES ceiling there is 100 MiB. Documented in `docs/ERROR-CODES.md` and in the `.env.example` cad block; tuning belongs to phase 76.

## Task Commits

1. **Task 1: per-format ceilings, inert until cad is registered** - `831c04b` (feat)
2. **Deviation fix: bind `format_size_limit` in the inspect vocabulary** - `968093f` (fix)
3. **Task 2: the atomic exposure of cad** - `49729fd` (feat), exactly one commit, 32 files

All three are SSH-signed (`G` in `%G?`). The Task 2 SHA is T = `49729fd`.

## Verification

- `GOTOOLCHAIN=go1.26.4 gofmt -l internal cmd` empty; `go build ./... && go vet ./... && go test ./... -count=1` green after the last edit.
- The plan's targeted `-run` list (TestAllEngines, TestDefaultRegistersOnlyAllowlistedCADPairs, TestListFormats, TestPre5, TestPre6, TestEveryEngine, TestEstimateSource, TestComposeReplicates, TestAllConvertQueues, TestSweepRoutesEveryEngineConstant, TestChartGrace, TestEveryBakingJob, TestJobDurationLabelsPinned, TestQueueDepthLabelsPinned, TestREADME, TestEngineTimeoutsFromEnv): 39 PASS, 0 FAIL.
- `scripts/ci-helm-checks.sh`: `OK: 10 ScaledObjects in both renders`, `OK: cad unchanged`, exit 0.
- `git diff --exit-code e219f8c -- internal/api/testdata internal/convert/testdata/cad-sniff-golden.tsv internal/convert/exec.go go.mod go.sum` prints nothing.
- Rendered chart: `scaledobject-cad.yaml` contains `queue="cad"` and `minReplicaCount: 0`; the ConfigMap has no `CAD_SANDBOX_DISABLED` and `MAX_UPLOAD_BYTES: "104857600"`; the Deployment renders grace 426 and 2 / 4Gi.
- `GOTOOLCHAIN=go1.26.4 scripts/ci-local.sh helm gate` ran after the commit, `ALL REQUESTED TIERS PASSED` at commit 49729fd. Transcript: `/Users/apaderin/dev/octoconv/.planning/ci-local/ci-local-20261005T102044Z.log` (the directory is gitignored).
- Added-comment audit (`^\+\s*(//|#)` over `*.go *.yaml *.yml *.sh`) lists only edits of existing comments that would otherwise be false: the AllEngines doc comment (three lines) and the "Ten as of Phase 73" note in `convert_test.go`; the reconciler default-case sentence (`cad/archive` to `archive`); the `TestSweepSkipsUnknownEngine` doc comment. The `.env.example` cad block is the paragraph the plan requires there. No comment was added to any Go file otherwise, and no `#` comment to the chart templates or compose.

## Deviations from Plan

**1. [Rule 1 - Bug] Task 1 left `internal/inspect` red.** The new log reason `format_size_limit` needs a binding in `inspect.RejectBindings` and a row in `docs/FINDINGS.md` (`TestRejectBindingsCoverEveryContentValidationReason`, `TestFindingsDocListsEveryRejectBinding`). Task 1's verify ran only `./internal/api`. Fixed in its own small commit `968093f` before Task 2, so the Task 2 commit stays the exposure alone.

**2. [Rule 1 - Operand] 73-04's tripwire flipped.** `TestCADQueueIsNotYetExposed` (queue_test.go) asserted nine queues without cad; it is now `TestCADQueueIsExposed` (ten, `cad` exactly once). `TestCADConverterIsInvisibleAndComparable` (cad_test.go) asserted nine engines and an unregistered pair; it is now `TestCADConverterIsRegisteredAndComparable`.

**3. [Rule 1 - Operand] The reconciler tests used `"cad"` as their unrecognized-engine fixture.** `TestSweepSkipsUnknownEngine` and `TestSweepUnroutableEngineActionStillUnguarded` now use `"archive"`, the class the default case's comment still names as out of scope.

**4. [Rule 3 - Blocking] Files outside Task 2's list in the commit.** `internal/convert/cad_test.go` and `internal/queue/queue_test.go` (items 2 and the dispatch's Redis-gated enqueue test) are in the commit. `internal/api/cad_ceiling_test.go` is in the plan's frontmatter, not Task 2's list. `git show --stat 49729fd` therefore lists the 29 files of the list plus these three and the two new chart templates, which the list also names.

**5. Invariance base.** Every `git diff --exit-code` that the plan writes against `520ecd4` was run against `e219f8c`, as the dispatch said.

**6. IGES ceiling 135 MiB, not 155.** The plan text and its truth line say 155; the code constant is 135 MiB (73-01 DERIVED), and the 413 message, the test and the docs say 135. This supersedes the 71 value as 73-01 and 73-02 recorded.

**7. Added `keda.cad` values are not in the plan.** The plan says "the tectonic keys and values, minReplicaCount 0". The tectonic profile (threshold 2, stabilization null) rests on a 271 s timeout under the HPA's 300 s window; the cad timeout is 406 s, so I set `scaleDownStabilizationSeconds: 900` (the probe argument) and `threshold: "1"` (one model per 4Gi pod, concurrency 1), max 2 replicas. This is a judgement call, flagged for review; phase 76 tunes it.

**8. Stale counts left as they were.** Existing compose comments say "twelve" and "eleven" queue-client services, and the compose test's header says eleven; they were already inconsistent and are not held by a test, so they were not touched.

**Total deviations:** 8 (one real bug found in my own Task 1, the rest operand corrections and scope notes), none outside the exposure's intent.

## Issues Encountered

- `go test ./internal/convert/` takes about 100 s (the known fake-engine cost).
- The first full test run after Task 2 found the three failures above in one pass; nothing flaky.

## Known Stubs

None. The estimate source `unmeasured` for cad is deliberate (ROADMAP SC1); phase 76 measures it (CADW-04).

## Known Gaps

- `TestEnqueueCADJob` is Redis-gated and skipped on this host; it follows the existing pattern and runs where `REDIS_ADDR` is set.
- The cad-worker has not been started from compose or the chart in this plan (no docker run, no cluster); the Deployment, ScaledObject and compose service are held by the derived contract tests and the helm checks, and the live proof is plan 73-06.
- The 135 MiB IGES and 406 s timeout are derived, not measured in the production worker (73-01 Known Gaps stand).

## Threat Flags

None beyond the register. T-73-13: per-format 413 before storage, proven at the real defaults for STEP and IGES and for the `.stp`/`.igs` aliases, inspect reports the same ceiling, chart transport ceiling documented. T-73-14: lying STL refused at creation with no upload or enqueue. T-73-15: queue, enqueue route, reconciler route, Deployment and ScaledObject landed in one commit under the derived tests. T-73-16: goldens byte-identical, label pins pass.

## Self-Check: PASSED

- FOUND: internal/api/cad_ceiling_test.go, deploy/chart/octoconv/templates/deployment-cad-worker.yaml, deploy/chart/octoconv/templates/scaledobject-cad.yaml
- FOUND commits 831c04b, 968093f, 49729fd (ancestors of HEAD)
- `git rev-list --count fb9e250..HEAD` was 3 before this summary's own commit
