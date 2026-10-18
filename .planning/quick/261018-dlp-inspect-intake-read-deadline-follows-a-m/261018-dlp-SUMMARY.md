---
phase: quick-261018-dlp
plan: 01
subsystem: api-inspect-intake
tags: [codex-p2, pr-40, inspect, read-deadline, slowloris, minimum-rate, real-connection-tests]
status: complete
requires: [inspectIntakeSem and inspectIntakeMaxConcurrent, http.ResponseController.SetReadDeadline through chi's wrapped ResponseWriter, the 261018-bmr tracer helpers (chartLiteral, writeTransportCapRequest, largestAdmissibleSTL, transportCapZeros)]
provides: [inspectIntakeReader, inspectIntakeMinBytesPerSecond, Server.inspectIntakeWindow and Server.inspectIntakeMinRate, real-connection intake tests T1 to T5, TestChartTransportCapsAdmitTheLargestCeilingFileOverAPacedConnection, TestInspectIntakeRuleIsStatedInTheOpenAPI, the intake rule in the POST /v1/inspect 400 description, two pending todos]
affects: [internal/api/inspect_handlers.go, internal/api/api.go, docs/openapi.yaml, .planning/quick/261018-bmr-helm-upload-cap-at-least-the-measured-ca/261018-bmr-PLAN.md, .planning/STATE.md]
tech-stack:
  added: []
  patterns: [a read deadline that follows a minimum rate, credited per byte a Read returned and bounded by the size cap, a behaviour-preserving seam committed before the RED tests, real-connection tests with raw HTTP/1.1 over net.Conn and a pacing reader, time scaled down by 10 for the full-size variant]
key-files:
  created:
    - internal/api/inspectintake.go
    - internal/api/inspect_intake_test.go
    - .planning/todos/pending/2026-10-18-create-intake-no-body-deadline.md
    - .planning/todos/pending/2026-10-18-inspect-post-intake-discard-holds-analysis-slot.md
    - .planning/quick/261018-dlp-inspect-intake-read-deadline-follows-a-m/evidence/
  modified:
    - internal/api/inspect_handlers.go
    - internal/api/api.go
    - internal/api/transport_cap_test.go
    - docs/openapi.yaml
    - .planning/quick/261018-bmr-helm-upload-cap-at-least-the-measured-ca/261018-bmr-PLAN.md
    - .planning/STATE.md
decisions:
  - "D1 (user): minimum rate 1 MiB/s, a Go constant (inspectIntakeMinBytesPerSecond = 1 << 20), not env-configurable; the intake slot's heap bound (8 x 32 MiB) does not depend on time, so the deadline exists for slot availability; worst-case hold 213 s at the chart cap, 130 s at the 100 MiB fallback"
  - "D2: the counting reader wraps http.MaxBytesReader, so it credits exactly the bytes the multipart parser receives and *http.MaxBytesError still maps to 413; the credit is also clamped at s.inspectMaxUploadBytes"
  - "D3: test injection through two unexported Server fields set by NewServer from the constants; no Config field, no package variable"
  - "D4: the name inspectIntakeReadDeadline stays as the base window (30 s); the two comment fragments the change made false were deleted, not rewritten"
  - "D5 (orchestrator confirmed): the rule is stated once, in the existing POST /v1/inspect 400 description of docs/openapi.yaml, held by a test that builds the sentences from the two constants"
  - "D6 (orchestrator confirmed): a pointer appended in place on 261018-bmr-PLAN.md lines 116 and 258, no new lines; the bmr SUMMARY untouched"
  - "D7 (orchestrator confirmed): the create path gets a pending todo only; 260810-slf declined a body timeout for POST /v1/jobs"
  - "D8 (orchestrator confirmed): the post-intake discard hold of the analysis slot is filed as a pending todo, analysed and not reproduced"
  - "D9 (orchestrator confirmed): the read deadline is kept after a successful parse; clearing it would make the D8 discard read unbounded"
metrics:
  completed: 2026-10-18
  duration: "07:13 to 07:36 UTC"
actuals:
  tokens: 9200
  tasks: 2
  commits: 4
plan_head_before: 7489c7bfd255fba26453ef1d06eec9d48afb8932
plan_head_after: f2a6e11a92fbaa6ff3d505969adbbb881844bb92
requirements-completed: [quick-261018-dlp]
---

# Quick 261018-dlp: the inspect intake read deadline follows a minimum rate, Summary

POST /v1/inspect no longer reads its body under one absolute 30 s deadline. The deadline is now
intake start + 30 s + min(bytes received, INSPECT_MAX_UPLOAD_BYTES) / 1 MiB/s. It is set before
the first Read, credited only for bytes a Read returned, moves only forward, and at the chart's
191889408-byte cap never passes start + 213 s (130 s at the 100 MiB Go fallback). The 182 MiB STL
that Codex P2 showed being cut at 20 to 50 Mbit/s is now admitted over any uplink of at least
1 MiB/s. A client that sends below that rate, or stalls, still gets today's answer, 400
`invalid_request` "invalid multipart form". A stalled client is cut at 30 s, as before.

Every existing test went through `httptest.ResponseRecorder`, which does not support the deadline.
The new tests run over a real connection through the full `srv.Routes()` chain with
`ReadHeaderTimeout` 10 s, as `cmd/api/main.go` configures it. The stalled-client test proves that
`ResponseController.SetReadDeadline` reaches the `net.Conn` through chi's wrapped ResponseWriter.

`commits: 4` counts the commits up to `plan_head_after`. The docs commit that carries this SUMMARY
is the fifth.

## Commits

| Task | Commit | What |
|---|---|---|
| 1 refactor | 1e386e5a | `refactor`: `inspectIntakeReader` seam, still the fixed rule (start + window); constant and Server fields |
| 1 RED | 1498dcb4 | `test`: T1 to T5 in `internal/api/inspect_intake_test.go` |
| 1 GREEN | df3c3b30 | `fix`: the minimum-rate formula with the clamp; the two D4 comment fragments deleted |
| 2 tests | f2a6e11a | `test`: the paced-connection variant of the bmr tracer; the openapi rule test |
| 2 docs | (this commit) | `docs`: openapi 400 description, bmr PLAN pointers, two todos, STATE, SUMMARY, evidence |

All commits are SSH-signed (`%G?` = G). None has a Co-Authored-By trailer. Nothing was pushed.

## RED and GREEN

- **Refactor** (`evidence/t1-refactor.txt`): gofmt clean, `go vet ./internal/api/` clean, and the whole internal/api package passes at the seam with the fixed rule.
- **Task 1 RED** (`evidence/t1-red.txt`, at 1e386e5a): exit 1, matching the plan exactly.
  - T1 `TestInspectIntakeAdmitsASteadyBodyThatOutlastsTheWindow` FAIL: 400 invalid multipart form after 401.7 ms.
  - T4 `TestInspectIntakeDeadlineIsBoundedByTheUploadCap` FAIL in both halves: the last deadline is start + 1 s (want + 10 ms), and the chart-cap hold is 30 s (want 213 s).
  - T5 `TestInspectIntakeCreditsBytesReturnedNotRequested` FAIL: all five deadlines are start + 1 s.
  - T2 `TestInspectIntakeCutsABodyBelowTheMinimumRate` PASS (cut at 401.5 ms) and T3 `TestInspectIntakeCutsAStalledClientAtTheWindow` PASS (cut at 402.1 ms). Both guard behaviour that holds today too.
- **Task 1 GREEN** (`evidence/t1-green.txt`, -race): T1 to T5 pass, and so do `TestInspectIntakeConcurrencyIsBounded`, `TestInspectSlowBodyDoesNotHoldConcurrencySlot`, `TestInspectPreAdmitGetsFreshContextAfterSlowBody`, `TestMultipartSpillFilesAreRemovedWhenTheHandlerReturns` (6 subtests) and `TestChartTransportCapsAdmitTheLargestCeilingFile`. The whole package passes without -race.
  - T1: 98542 bytes at 64 KiB/s, 200 after 1.503 s (window 400 ms, so the transfer outlasted it 3.75x).
  - T2: 8 KiB/s, cut with 400 after 542 ms (the plan predicted about 0.55 s).
  - T3: no body byte, cut with 400 after 402 ms.
  - T4 logs the worst-case intake slot hold at the chart cap of 191889408 bytes: **3m33s (213 s)**.
- **Task 2 RED** (`evidence/t2-red.txt`, -race): `TestInspectIntakeRuleIsStatedInTheOpenAPI` FAIL and names both missing sentences; the description read "Invalid multipart form / missing file.". The paced variant PASS: the 190840784-byte STL at 25000000 B/s took 7.649 s against the 3 s scaled window.
- **Task 2 GREEN**: after the openapi edit the rule test passes, and so do `TestOpenAPIPathsMatchRegisteredRoutes` and `TestOpenAPIDocumentsEveryEmittedStatus`.

## Timing stability (-race -count=3)

- T1 to T5 (`evidence/t1-green.txt`): 9 of 9 timed runs pass. T1 took 1.503 to 1.504 s, T2 541.6 to 542.6 ms, and T3 401.6 to 402.0 ms.
- Paced variant (`evidence/t2-variant.txt`): 3 of 3 pass at 7.646 to 7.658 s.
- The final-gates race run repeats all of them once more, with the same figures.

## Mutations (`evidence/t1-mutations.txt`, `evidence/t2-variant.txt`; each applied to a saved copy, reverted, and cmp-identical to it afterwards)

| # | Mutation | Caught by | Observed |
|---|---|---|---|
| M1 | fixed deadline restored: `deadline()` returns start + window | T1, T4 (both halves), T5 | T1 400 after 402 ms; T4 max start + 1 s and 30 s; T5 all start + 1 s |
| M2 | credit `int64(len(p))` instead of `n` | T2, T5 | T2 never cut, 200 after 12.0 s; T5 deadlines + 4.096 s per byte |
| M3 | no clamp: `credited := r.received` | T4 (both halves) | (a) max start + 1.025 s, six values past the bound; (b) 3m34s instead of 3m33s |
| M4 | the initial `SetReadDeadline` before `ParseMultipartForm` removed | T3 | no response, the client's 15 s safety deadline fired |
| M5 | sliding idle deadline: `deadline()` measured from `time.Now()` | T2 (plus T4, T5) | T2 never cut, 200 after 12.0 s |
| variant M1 | fixed deadline restored | `TestChartTransportCapsAdmitTheLargestCeilingFileOverAPacedConnection` | 400 `invalid_request` "invalid multipart form" after 3.007 s |

No mutation survived.

## Final gates (`evidence/final-gates.txt`, go1.26.4, python 3.14.8 first on PATH)

- `gofmt -l .`: empty. `go vet ./...`: clean.
- The -race run of the new tests and the regression set passes (14 top-level tests, exit 0). The regression set is the Task 1 regex plus `TestInspectIntakeRuleIsStatedInTheOpenAPI`, `TestChartTransportCaps` and `TestOpenAPI`.
- Full `go test ./... -count=1` exits 0: 32 packages ok, 17 without test files, 0 FAIL.
- `git diff --quiet main -- internal/convert/exec.go` exits 0. `git diff --quiet 7489c7b -- internal/api/handlers.go cmd/api/main.go` exits 0.
- Comment-line counts: inspect_handlers.go went from 1083 to 1079 (the D4 deletions), api.go stays at 358, transport_cap_test.go at 0, the two new Go files are 0, and docs/openapi.yaml has 1 `#` line. The only `+` comment lines in the diff since BASE are the two shortened remainders of the D4 lines; no comment line was added.
- Doc sweep (`evidence/doc-sweep.txt`) over docs/, .env.example, README.md, deploy/ and docker-compose.yml: no hit states the fixed 30 s inspect body rule or the 6.1 MiB/s arithmetic as current. The 30s hits are worker grace and retry margins. The only intake-rule text is the new openapi sentence.

## Residuals, recorded and not fixed

- **Post-intake discard hold (D8).** When an inspect response above 2048 bytes flushes inside the handler, net/http reads up to 256 KiB + 1 of any unread body under the current deadline while `s.inspectSem` is held. It was analysed and not reproduced. Before this task the hold was at most 30 s from intake start. Now it is at most start + window + received / minRate. The cheapest variant, a tiny form, stays at about 30 s. Filed as `.planning/todos/pending/2026-10-18-inspect-post-intake-discard-holds-analysis-slot.md`.
- **Background-read context cancel (D9).** The deadline is kept after a successful parse. net/http's background read starts at body EOF under that deadline. If the deadline expires while the handler still runs, it cancels the request context, and PreAdmit verdicts become "unknown". The new rule narrows this edge. A client that sent at or above the minimum rate throughout reaches EOF at least 30 s before its deadline. Before, a 183 MiB upload at 6.5 MiB/s had about 2 s. Only a client that stalled through most of the window and then burst keeps the edge.
- **Create path.** POST /v1/jobs still reads its body with no deadline and no intake bound, by the 260810-slf decision. Filed as `.planning/todos/pending/2026-10-18-create-intake-no-body-deadline.md`.
- **A published timing rule.** The openapi sentence is the first timing rule this API publishes. A future change to `inspectIntakeReadDeadline` or `inspectIntakeMinBytesPerSecond` must update it, and `TestInspectIntakeRuleIsStatedInTheOpenAPI` fails until it does.

## Deviations from Plan

1. **Evidence capture redone, results unchanged.** The first `t1-red.txt` capture read `PIPESTATUS`, which the session shell (zsh) does not set, so it recorded an empty exit. I re-ran it with the exit captured directly and got exit=1 and the same pass/fail set. The first `t1-mutations.txt` run printed each mutation's diff against HEAD, which left M1's diff empty. I re-ran all five with the diff taken against the saved copy. Every mutation was caught both times.
2. **Pin guard before three writes.** The guard ran (PIN-OK) before every other write and before every commit. It did not run immediately before the two todo `Write` calls or the first `Write` of this SUMMARY. In each case it had passed just before (bmr PLAN edit, final gates) and passed again right after (STATE edit, self-check), and the root did not change in between.
3. **Mutation runs without -race.** The plan does not ask for -race on mutations. GREEN, the stability runs and the final gates ran with -race.

## Known Stubs

None.

## Threat Flags

None. No new endpoint, auth path or schema. The change narrows T-dlp-01 to T-dlp-04 as the threat register planned. T-dlp-06 (create) and T-dlp-07 (discard hold) are transferred to the two todos.

## Self-Check: PASSED

- Commits 1e386e5a, 1498dcb4, df3c3b30 and f2a6e11a are ancestors of HEAD, all with `%G?` = G.
- `internal/api/inspectintake.go`, `internal/api/inspect_intake_test.go`, both new pending todos and the evidence files (base.sha, t1-refactor, t1-red, t1-green, t1-mutations, t2-red, t2-variant, doc-sweep, final-gates) exist.
- The plan's Task 1 automated check printed T1-OK. The Task 2 check runs after the docs commit (see the return message).
