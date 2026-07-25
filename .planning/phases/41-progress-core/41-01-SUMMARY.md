---
phase: 41-progress-core
plan: 01
subsystem: convert
tags: [ffmpeg, progress-parsing, ema, tdd]

# Dependency graph
requires:
  - phase: 34-av-engine
    provides: avProbeSource / src.duration (the future percent denominator), runCommand's hardened Setpgid+SIGKILL exec contract this plan does not touch
provides:
  - "ProgressUpdate / ProgressSink / ProgressConverter seam contracts, added alongside (not inside) Converter"
  - "progressBlockParser: a bounded, panic-free ffmpeg -progress key=value block accumulator (out_time_us/out_time_ms /1e6, N/A guard, progress=continue/end terminator, trailing-x speed strip)"
  - "computeProgressPct: [0,100]-clamped percent with a <=0-denominator null policy"
  - "etaEstimator: EMA-smoothed speed with a warm-up-gated nil-until-derivable ETA"
  - "buildProgressUpdate: the single composition point Plans 02/03 will call"
  - "A real captured n8.1.2 -progress golden fixture (internal/convert/testdata/ffmpeg_progress_transcode.txt)"
affects: [41-02-streaming-exec, 41-03-worker-throttle-write]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Optional-capability seam added alongside an existing interface (ProgressConverter beside Converter), never widening the shared interface"
    - "Bounded per-line parser: every numeric field is N/A-guarded strictly before any strconv call"
    - "Package-level [ASSUMED] compile-time constants for un-measured tuning values (progressEMAAlpha/progressWarmupSamples), mirroring avDiskSafetyFactorDefault's precedent"

key-files:
  created:
    - internal/convert/progress.go
    - internal/convert/progress_test.go
    - internal/convert/testdata/ffmpeg_progress_transcode.txt
  modified: []

key-decisions:
  - "Fixture captured via -progress <file> (the 'file' protocol) against the real octoconv-av-worker:dev docker image, not -progress pipe:N -- the pinned build's ffmpeg is compiled with --enable-protocol='file,crypto' only, and the pipe protocol is NOT registered in that binary. Block content/format is identical regardless of transport; only the URL scheme differs. Flagged in the fixture's header comment as a MUST-READ item for Plan 02 (streaming exec), since -progress pipe:3 as sketched in 41-RESEARCH.md will not work against the real pinned binary."
  - "ConvertWithProgress/ProgressSink/ProgressConverter naming kept per 41-RESEARCH.md Open Question 3 (no drift) with a sink interface (SourceDuration + Update) per the plan text, rather than a bare callback func -- gives Plan 03's worker a clearer two-call contract (duration once, then throttled ticks)"
  - "progressEMAAlpha=0.3 / progressWarmupSamples=3 shipped as plain package constants, not env vars, per Open Question 2 -- only Plan 03's throttle gates (PROGRESS_MIN_INTERVAL_SECONDS/PROGRESS_MIN_DELTA_PCT) get env-tunable ceremony"

patterns-established:
  - "Pattern: N/A-before-strconv guard -- every ffmpeg -progress numeric field is checked against the literal string \"N/A\" before any ParseFloat call, so a warm-up/mid-stream gap block never panics or produces a garbage numeric value"

requirements-completed: []  # Deliberately NOT marking PROG-01/PROG-04 complete per this plan's explicit instruction -- Plans 02/03 finish the requirement (persistence + streaming + worker wiring)

# Metrics
duration: ~20min
completed: 2026-07-25
---

# Phase 41 Plan 01: Progress Parser + Seam Contracts Summary

Pure, subprocess-free ffmpeg `-progress` block parser (microsecond-correct, N/A-guarded, terminator-aware) plus clamped-percent/EMA-warm-up-ETA math and the `ProgressUpdate`/`ProgressSink`/`ProgressConverter` seam contracts, proven against a real captured n8.1.2 fixture -- ready for Plan 02 (streaming exec) and Plan 03 (worker throttle) to compose against.

## Performance

- **Duration:** ~20 min
- **Completed:** 2026-07-25
- **Tasks:** 2/2 completed
- **Files modified:** 3 (all new)

## Accomplishments
- Captured ONE real `-progress` golden fixture from the pinned `octoconv-av-worker:dev` docker image (not a hand-written mock), including a genuine mid-stream N/A block in addition to the expected leading warm-up block
- Discovered and documented that the pinned av-worker ffmpeg build does not register the `pipe` protocol at all (`--enable-protocol='file,crypto'` only) -- a finding that directly affects Plan 02's streaming-exec transport choice, flagged in the fixture's header comment
- Implemented a bounded, panic-free block parser: `out_time_us`/`out_time_ms` divided by 1e6, N/A guarded strictly before any `strconv.ParseFloat` call, `progress=end`/`progress=continue` as the sole terminator signal, trailing/leading-whitespace `speed=` stripped (`"   3x"` → `3.0`)
- Implemented `computeProgressPct` (clamped [0,100], `ok=false` for a `<=0` denominator) and `etaEstimator` (EMA over `speed=`, nil ETA before a 3-sample warm-up), both proven at multi-minute microsecond scale to pin the classic ÷1000-vs-÷1e6 regression
- Added `ProgressUpdate`/`ProgressSink`/`ProgressConverter` alongside (never inside) `Converter` -- `git diff --stat internal/convert/convert.go` stays empty

## Task Commits

Each task followed RED → GREEN:

1. **Task 1: Seam contracts + progress-block parser**
   - `1b07b34` test(41-01): add failing test for ffmpeg -progress block parser
   - `d521f3b` feat(41-01): implement ffmpeg -progress block parser + seam contracts
2. **Task 2: Percent clamp + EMA/warm-up ETA math**
   - `04de082` test(41-01): add failing test for percent clamp + EMA/warm-up ETA math
   - `5214a43` feat(41-01): implement percent clamp + EMA/warm-up ETA math

_No REFACTOR commit needed -- both GREEN implementations landed clean on first pass._

**Plan metadata:** committed separately below (docs commit)

## Files Created/Modified
- `internal/convert/progress.go` - `ProgressUpdate`/`ProgressSink`/`ProgressConverter` contracts, `progressBlockParser`/`progressTick`, `computeProgressPct`, `etaEstimator`, `buildProgressUpdate` composition helper
- `internal/convert/progress_test.go` - table/behavior tests against the real fixture (N/A warm-up + mid-stream, microsecond division, terminator, no-clamp-at-parse-time) plus percent/ETA math tests (clamp both ends, multi-minute-scale pin, monotonic ramp, EMA warm-up/non-pollution)
- `internal/convert/testdata/ffmpeg_progress_transcode.txt` - real captured `-progress` block stream from `octoconv-av-worker:dev`, with a header comment recording the exact capture command and the pipe-protocol-unavailable finding

## Decisions Made
- Fixture transport: `-progress <file>` against the real pinned docker image, since that image's ffmpeg has no `pipe` protocol registered at all (verified live: `Failed to open progress URL "pipe:1": Protocol not found"`). Block content is identical either way (ffmpeg's progress-print logic is transport-agnostic) -- only the URL scheme differs, so the fixture is still the exact real byte content the parser will see in production.
- `ProgressSink` modeled as a two-method interface (`SourceDuration` + `Update`) rather than a bare `func(ProgressUpdate)` callback, per the plan text's explicit recommendation -- gives Plan 03 a distinct "report the denominator once" step separate from per-tick updates.
- EMA alpha (0.3) and warm-up window (3 samples) are plain package constants, not env vars, per 41-RESEARCH.md's resolved Open Question 2.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking issue] Docker image's ffmpeg has no `pipe` protocol; fixture captured via `-progress <file>` instead**
- **Found during:** Task 1 fixture capture
- **Issue:** The plan's preferred capture command (`docker run ... ffmpeg ... -progress pipe:3 ...`, mirroring 41-RESEARCH.md's live-verified Homebrew-ffmpeg pattern) failed against the actual pinned `octoconv-av-worker:dev` image: `Failed to open progress URL "pipe:1": Protocol not found` / `docker run --rm --entrypoint ffmpeg octoconv-av-worker:dev -protocols` shows no `pipe` entry at all (the image is built with `--enable-protocol='file,crypto'` only, a narrower allowlist than the Homebrew build 41-RESEARCH.md tested against).
- **Fix:** Captured the golden fixture using `-progress /cap/progress.txt` (the `file` protocol, which IS enabled in the pinned build) via a bind-mounted docker volume. The emitted block format/content is identical to a pipe-transported capture -- ffmpeg's progress-print logic in `fftools/ffmpeg.c` is transport-agnostic, only the destination URL scheme differs. Documented this finding explicitly in the fixture's header comment as a MUST-READ item for Plan 02, since that plan's `runCommandStreaming` design (per 41-RESEARCH.md Pattern 1) assumes `-progress pipe:3` works against the real binary -- it does not, and Plan 02 will need a different transport (e.g. a named FIFO via the `file` protocol, or rebuilding the image with the `pipe` protocol enabled).
- **Files modified:** `internal/convert/testdata/ffmpeg_progress_transcode.txt` (header comment)
- **Verification:** Fixture parses correctly in all Task 1/2 tests; content matches the exact real n8.1.2 block shape (N/A warm-up, microsecond out_time, trailing-x speed, progress=end terminator) documented in 41-RESEARCH.md.
- **Committed in:** `1b07b34`

**2. [Rule 1 - Bug] gofmt alignment fix in progress_test.go**
- **Found during:** Post-Task-2 verification (`gofmt -l .`)
- **Issue:** `fakeProgressSink.Update`'s method-alignment whitespace drifted out of gofmt canonical form after an edit inserted a second method above it.
- **Fix:** Ran `gofmt -w internal/convert/progress_test.go`.
- **Files modified:** `internal/convert/progress_test.go`
- **Verification:** `gofmt -l .` reports nothing project-wide.
- **Committed in:** `5214a43`

None of these deviations altered scope, behavior, or the seam contract shapes specified in the plan.

## Issues Encountered
None blocking. The pipe-protocol finding (deviation 1) is a forward-looking flag for Plan 02, not a blocker for this plan's pure-parsing scope.

## Requirements Status
`requirements-completed: []` -- deliberately left empty per this plan's explicit instruction ("Do NOT mark PROG requirements complete (later plans finish them)"), even though the plan frontmatter lists `[PROG-01, PROG-04]` as the requirements this work contributes to. PROG-01 (null policy + real ramp) and PROG-04 (ETA) will be marked complete once Plans 02/03 wire this parser into the actual worker/DB write path.

## Known Stubs
None. Every exported type/function in `progress.go` is fully implemented (no placeholder returns, no hardcoded empty values). `ProgressConverter` is an interface with zero implementations yet -- this is intentional per D-01 (Plan 02 implements it on `AVConverter`), not a stub.

## Threat Flags
None beyond what the plan's own `<threat_model>` already covered (T-41-01/02/03) -- no new network endpoints, auth paths, or schema changes were introduced by this plan.

## Self-Check: PASSED

- FOUND: internal/convert/progress.go
- FOUND: internal/convert/progress_test.go
- FOUND: internal/convert/testdata/ffmpeg_progress_transcode.txt
- FOUND: commit 1b07b34
- FOUND: commit d521f3b
- FOUND: commit 04de082
- FOUND: commit 5214a43
