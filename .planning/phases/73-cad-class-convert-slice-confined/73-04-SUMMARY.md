---
phase: 73-cad-class-convert-slice-confined
plan: 04
subsystem: cad-worker
tags: [error-codes, cad-descriptor, cad-job, cad-worker, landlock, liveproof]
decision: r08-four-codes
requires:
  - phase: 73-02
    provides: CADConverter, the six CAD sentinels, ConfinedCAD, CADPreflight
  - phase: 73-03
    provides: the measured Landlock sets and the production engine stage
provides:
  - the four cad_* failure codes on the wire, each in allFailureCodes and the async table of docs/ERROR-CODES.md
  - CADDescriptor, isCADTerminal, cadFailureCode
  - TypeCADJob (cad:job) on queue cad with its own retry delay and unique-lock TTL
  - cmd/cad-worker and the final Dockerfile.cad-worker stages
  - the cad row of scripts/sandbox-liveproof.sh and a passing run on the real image
affects: [73-05, 73-06]
actuals:
  tokens: 7000
  tasks: 3
  commits: 2
plan_head_before: f802b58f02cd240b48e7145109514d59392d6e63
plan_head_after: 4110eb41c9b3906ab7488c43da10ac11b3607938
tech-stack:
  added: []
  patterns:
    - sandbox fault checked first with a literal false, then one errors.Is per sentinel, then the shared classifier
    - confined startup in the document-worker order, the real-engine preflight before Postgres
key-files:
  created:
    - cmd/cad-worker/main.go
  modified:
    - Dockerfile.cad-worker
    - docs/ERROR-CODES.md
    - internal/queue/queue.go
    - internal/queue/queue_test.go
    - internal/worker/descriptors.go
    - internal/worker/pinning_test.go
    - internal/worker/engine_sandbox_retryable_test.go
    - scripts/sandbox-liveproof.sh
key-decisions:
  - "R-08 code set: input_too_large reused, cad_too_complex, cad_unreadable, cad_empty_result and cad_mesh_timeout added (auto-selected under user delegation, 2026-10-04)"
  - "ErrCADEngineKilled is reported as cad_too_complex, not as a separate code"
  - "The task type is cad:job, not cad:convert, per the v1.16 roadmap decision recorded in the plan"
requirements-completed: [CADW-01, CADW-02, CADW-03]
duration: 40min
completed: 2026-10-05
status: complete
---

# Phase 73 Plan 04: Failure codes, CADDescriptor, cad:job and the confined cad-worker Summary

**Decision (Task 1): `r08-four-codes`, auto-selected under user delegation (2026-10-04).** The cad class reuses `input_too_large` and adds `cad_too_complex`, `cad_unreadable`, `cad_empty_result` and `cad_mesh_timeout`. The `engine_error` row of docs/ERROR-CODES.md was edited so the document stays true.

**The cad worker now exists as a confined, preflighted process in its final image: a bad file fails once with a code that names the client's action, a node fault retries, and the real image proves both rulesets with the real engines before it touches Postgres. The class is still invisible to every client.**

## Accomplishments

- **Codes (one-way).** The four constants are appended to the const block and to `allFailureCodes`. The async table has one row per new code (meaning, client action, TERMINAL). The `input_too_large` row now lists `pandoc, cad` with the per-format ceiling (63 MiB STEP, 135 MiB IGES) re-checked in the worker. The count sentence says nineteen and names phase 73. The `engine_error` row no longer claims to be the catch-all for every class: it says the cad engine has finer codes and why. `TestErrorCodesDocMatchesGoConstants` and `TestActiveAndHistoricalFailureCodesAreDisjoint` pass.
- **Classification.** `isCADTerminal`: `ErrEngineSandboxUnavailable` first with a literal `false`, then each of the six CAD sentinels true, then `isTerminal`. A bare `context.DeadlineExceeded` (an S3 stall) retries. `cadFailureCode` maps the byte re-check to `input_too_large`, too-complex and engine-killed to `cad_too_complex`, unreadable, empty result and mesh timeout to their own codes, everything else to `engine_error`; every message is a fixed sentence and engine output never reaches the client. `TestPinnedCADFailureVerdicts` (11 subtests) drives each sentinel through `NewRunner(h, CADDescriptor).Handle`: SkipRetry, one `MarkFailed` on the first attempt, the exact code and message. It also pins the sandbox fault wrapping a terminal-looking error (retries, marks nothing), the attempt deadline, cancellation, a non-empty persisted opts map (terminal, `invalid_options`) and the plain-failure fallback. The two sandbox tests in `engine_sandbox_retryable_test.go` gained a cad case each, with the control case so the retryable one is not vacuous.
- **Task type.** `TypeCADJob = "cad:job"`, `QueueCAD = convert.EngineCAD`, `CADRetryDelay` (the shared probe schedule), `cadBackoffSum`, `CADUniqueTTL` (3 x engine timeout + backoff sum + margin) and `NewCADJobTask`, with the `RetryDelayFunc` case. `AllConvertQueues()` is untouched and a test holds that it still has nine queues without `cad`.
- **cmd/cad-worker.** Order: `RunExecModeIfRequested`, signal context, `ResolveHelper("CAD_SANDBOX_DISABLED")`, `CAD_WORKER_CONCURRENCY` (default 1), `CAD_ENGINE_TIMEOUT`/`CAD_GUARD_TIMEOUT`/`CAD_MESH_TIMEOUT` (the convert defaults), `ConfinedCAD`, `convert.Default.Replace`, the landlock or escape-hatch line, `CADPreflight` under 60 s (line 71) before `db.Connect` (line 80), then storage, redis, the cancel subscriber, the handler, the mux on `TypeCADJob`, the asynq server on queue `cad` with concurrency from the variable and `ShutdownTimeout` = engine timeout + 10 s, and the metrics listener. No comments.
- **Image.** `Dockerfile.cad-worker` gained a `build` stage and a last stage `FROM engine` ending in `USER nobody` and the ENTRYPOINT. The engine stage is unchanged.
- **Live proof on the real image** (`scripts/sandbox-liveproof.sh cad`, built from the final Dockerfile, native arm64, one `docker run` at a time, image removed afterwards; log at `.../scratchpad/cad-liveproof-04.log`):

```
PASS: cad confined-startup
  🔒 cad sandbox: landlock via /usr/local/bin/cad-worker __sandbox-exec
  🔒 cad sandbox proven: cad-guard and cad-mesh ran inside the job directory and were denied a file outside it
  postgres: ping postgres: failed to connect to `user=gate database=gate`:
PASS: cad refuses-without-landlock
  cad sandbox: landlock is unavailable on this kernel; the engine would run with read access to every file in this container. Set CAD_SANDBOX_DISABLED=true to accept that deliberately
PASS: cad escape-hatch
  ⚠️  CAD_SANDBOX_DISABLED=true — the engines can read every file in this container
PASS: cad preflight-catches-restrict-self
  cad sandbox preflight: the cad guard stage cannot count a file inside the job directory under its sandbox: cad guard: engine sandbox could not be established: cad-guard failed: exit status 78: __sandbox-exec: landlock: restrict self: operation not permitted
ALL PASS (4 checks)
```

Free disk stayed at 16-17 GiB throughout (stop threshold 6).

## Task Commits

1. **Task 1: R-08 decision gate** - no commit (`r08-four-codes`, auto-selected under user delegation, 2026-10-04)
2. **Task 2: codes, CADDescriptor, cad:job** - `56a4f94` (feat)
3. **Task 3: cmd/cad-worker, final image, liveproof row** - `4110eb4` (feat)

Both are SSH-signed (`Good "git" signature`).

## Verification

`GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./... && go test ./... -count=1` is green. `git diff --exit-code e219f8c -- docker-compose.yml deploy internal/api internal/queue/client.go internal/convert/converters.go internal/convert/exec.go go.mod go.sum internal/metrics` prints nothing, and `git diff --numstat e219f8c -- internal/convert/convert.go` is `1 0` (plan 73-02's `EngineCAD`). No added `//` line in `queue.go`, `descriptors.go`, `cmd/cad-worker/main.go` or the Go test files. The two TestJobDurationLabelsPinned / TestQueueDepthLabelsPinned label tests in `internal/metrics` pass.

## Deviations from Plan

**1. [Rule 3 - Blocking] Invariance base.** Every `git diff --exit-code` that the plan writes against `520ecd4` was run against `e219f8c` (the 73-01 deviation 1 base problem, as the dispatch said).

**2. [Rule 1 - Operand] `.gitignore` needed no edit.** `/cad-worker` was already in the cmd-binaries block, so Task 3 step 3 was a no-op; `internal/repohygiene` passes.

**3. [Rule 1 - Operand] `isCADTerminal` has no separate `context.Canceled` branch.** The plan's order ends "context.Canceled through isTerminal, then isTerminal"; both branches call the same function, so the first is dead code and was left out. Behaviour is identical and cancellation is pinned by a subtest.

**4. [Rule 3 - Blocking] The task's queue, MaxRetry and Unique options are not asserted.** asynq keeps a task's options unexported and there is no Redis on this host that is not another project's. `TestCADJobTaskRoundTrip` checks the type, the queue constant and the payload; the TTL is checked through `CADUniqueTTL` and `cadBackoffSum`. The options are set by the same three lines as the tectonic constructor. Plan 73-05 adds the client method and can add the Redis-gated enqueue test next to `TestEnqueueOCRConvert`.

**5. Liveproof ran before the Task 3 commit.** The plan's verify order has the run first, so the image was built from the working tree (the script printed `commit: 56a4f94`, `tracked dirt: 6 file(s)`). Those six files are exactly the Task 3 files that were committed unchanged afterwards as `4110eb4`.

**6. Header text of `scripts/sandbox-liveproof.sh`.** The existing class-list sentence in the file header was updated to name `cad` (an edit of existing text, no comment line added).

**Total deviations:** 6, none outside the plan's files.

## Issues Encountered

None. `go test ./internal/convert/` takes about 100 s (the known fake-engine cost).

## Observation for later (not changed here)

The `invalid_options` row of docs/ERROR-CODES.md lists "All six opts-guarded engines" and omits tectonic and now cad, both of which emit it through their `OptsFromMap` guard. It was already stale for tectonic; the "While no CAD engine is registered" sentence is left for plan 73-05 as planned.

## Known Stubs

None.

## Known Gaps

- The cad queue is not in `AllConvertQueues()`, `queue.NewClient` does not read `CAD_ENGINE_TIMEOUT`, there is no client enqueue method, and compose and the chart do not know the class. That is plan 73-05 by design (D-14).
- The first three liveproof checks use the unreachable-Postgres pattern, so the worker's behaviour after Postgres connects (the asynq server and the metrics listener) is not exercised by this plan; it is the same code path as the other classes' workers.

## Threat Flags

None beyond the register. T-73-10: the refusal names `CAD_SANDBOX_DISABLED`, the preflight with the real engines runs before Postgres, and the liveproof proves refusal under a seccomp profile and the preflight catching a failed `restrict_self`. T-73-11: pinned by `TestPinnedCADFailureVerdicts`. T-73-12: `cadFailureCode` returns fixed sentences.

## Self-Check: PASSED

- FOUND: cmd/cad-worker/main.go, Dockerfile.cad-worker, internal/worker/descriptors.go, internal/queue/queue.go, docs/ERROR-CODES.md
- FOUND commits: 56a4f94, 4110eb4 (both ancestors of HEAD)
