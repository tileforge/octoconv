---
phase: 41-progress-core
verified: 2026-07-25T07:49:02Z
status: passed
score: 12/12 must-haves verified
overrides_applied: 0
---

# Phase 41: Progress Core Verification Report

**Phase Goal:** A client polling `GET /v1/jobs/{id}` for an in-flight av/audio job sees an honest, real-time `progress_pct` and `eta_seconds`, driven by ffmpeg's `-progress` stream — genuinely `null` (never fabricated) for engines with no machine-readable signal — without regressing the hardened process-group-kill contract.
**Verified:** 2026-07-25T07:49:02Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | PROG-01: `progress_pct` 0→100 for in-flight av transcode, driven by `-progress` (out_time÷1e6, N/A handled); null for image/document/html/whisper/cheap av paths | VERIFIED | `internal/convert/progress.go:119-140` (parser), `internal/convert/av.go:628-648` (D-02 dispatch: only mp4/webm re-encode ramps; audio-extract/thumbnail/stream-copy call plain `Convert`), `internal/convert/converters.go:9` (AudioConverter/whisper registered separately, never wrapped), real fixture `internal/convert/testdata/ffmpeg_progress_transcode.txt` captured from pinned n8.1.2 build, parsed correctly by `TestParseProgress_*` (all PASS) |
| 2 | PROG-02: `source_duration_seconds` persisted from existing `avProbeSource` (no new ffprobe); null for non-ffmpeg engines | VERIFIED | `internal/convert/av.go:634` (`sink.SourceDuration(src.duration.Seconds())` called once from the already-probed `src`), `internal/jobs/repo.go:404-414` (`SetSourceDuration`, write-once via `IS NULL` guard); non-av engines never call the sink (D-01 type-assert) |
| 3 | PROG-03: `UpdateProgress` non-locking, no `FOR UPDATE`, no `job_events`; monotonic+active-only; dual throttle (≥2s AND ≥1%) env-tunable; terminal 100 bypasses throttle | VERIFIED | `internal/jobs/repo.go:387-397` (plain `UPDATE ... WHERE status='active' AND (progress_pct IS NULL OR progress_pct <= $2)`, no lock/no events); `internal/jobs/repo_test.go:998-1090` (`TestUpdateProgress` asserts `job_events` count unchanged); `internal/worker/worker.go:1168-1182` (`progressSink.Update` dual gate) + `:1201-1215` (`runConvertWithOptionalProgress` issues unconditional terminal-100 write bypassing the sink); `cmd/av-worker/main.go:102-103,125-126` + `.env.example:111-112` (env knobs) |
| 4 | PROG-04: `eta_seconds` via EMA of `speed=` + warm-up; null before derivable | VERIFIED | `internal/convert/progress.go:213-263` (`etaEstimator`, `progressEMAAlpha=0.3`, `progressWarmupSamples=3`, nil until warm-up); `TestETA_NilBeforeWarmup`/`TestETA_PositiveAfterWarmup`/`TestETA_NonPositiveSamplesDoNotPollute` all PASS |
| 5 | PROG-05: `exec.go` byte-for-byte unchanged; `runCommandStreaming` leak-safe (incl. never-opened-FIFO); named-FIFO/`file:` transport (no `pipe:`); live gate PASSED 40/40 | VERIFIED | `git diff --exit-code internal/convert/exec.go` exit 0 (file last touched by phase 23, commit `4e36a73`, predates phase 41 entirely); `internal/convert/progressexec.go` (Mkfifo, O_NONBLOCK read-open, post-Wait poke, `scanDone` joined both arms, `syscall.Kill(-pid,...)` mirrored); `grep -c "pipe:" progressexec.go` == 0; `TestRunCommandStreaming_NeverOpenedFIFO`/`_SIGKILLMidStream_NoLeak` (5 iterations) PASS with goroutine-delta==0; `scripts/av-progress-liveproof.sh` exists, executable, `bash -n` clean, hard-gates on `go_goroutines` (no "not a gate failure" wording, `grep -c` == 0); `41-04-SUMMARY.md` + `b0db519` commit record the operator live-run: 40/40 assertions PASS, ffmpeg PIDs gone from process table across 5 cycles, `go_goroutines` 22→25 (delta 3 ≤ 5 tolerance), progress_pct frozen at 1 on cancel, completed transcode landed 100 — REQUIREMENTS.md PROG-05 marked Complete with this same evidence (not re-run per task instructions; stack currently up and consistent with this record) |
| 6 | D-01 seam: shared `Converter` interface + 4 non-progress engines unchanged; `ProgressConverter` implemented only by `AVConverter`; worker type-asserts with plain-Convert fallback | VERIFIED | `grep -rn "ProgressConverter\|ConvertWithProgress"` across `internal/convert/*.go` shows only `progress.go` (contract) and `av.go` (implementation + `var _ ProgressConverter = AVConverter{}` at :615); `whisper.go`/`libvips.go`/`document.go`/`html.go`/`convert.go` untouched since before phase 41 (git log); `internal/worker/worker.go:1202-1204` (`pconv, ok := conv.(convert.ProgressConverter); if !ok ... return conv.Convert(...)`) |
| 7 | D-06: freeze-on-abort (no write after error, explicit spy test) + terminal 100 on success | VERIFIED | `internal/worker/worker.go:1201-1215` (terminal-100 line only reached on success path, structurally); `internal/worker/worker_test.go:756-770` (`TestProgressFreezeOnAbort`: spy repo shows exactly 1 write — the throttled 30% tick — after a simulated mid-stream error, zero further calls) — PASS |
| 8 | go build/vet clean; `internal/...` `-race` green; exec.go invariant holds | VERIFIED | `go build ./...` exit 0; `go vet ./...` exit 0; `DATABASE_URL=... go test ./internal/... -race -count=1` — all packages `ok` (convert 18s incl. real-ffmpeg SIGKILL/success-path tests, jobs, worker, api, etc.); `git diff --exit-code internal/convert/exec.go` exit 0 |

**Score:** 8/8 truths verified (12/12 counting the granular must-haves from the 4 plan frontmatters, all corroborated above)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/convert/progress.go` | ProgressUpdate/ProgressSink/ProgressConverter + parser + math | VERIFIED | All three contracts exported (:13,28,45); parser divides by 1e6 (:157); N/A guarded before strconv (:150,166); computeProgressPct clamps [0,100] (:191-203); etaEstimator warm-up-gated (:229-263) |
| `internal/convert/progress_test.go` | Fixture-driven RED→GREEN tests | VERIFIED | 12 test functions, all PASS against real captured fixture |
| `internal/convert/testdata/ffmpeg_progress_transcode.txt` | Real n8.1.2 fixture | VERIFIED | Header documents exact capture command against `octoconv-av-worker:dev`; contains `N/A` warm-up block and `progress=end` terminator |
| `internal/convert/progressexec.go` | runCommandStreaming (named-FIFO transport, leak-safe) | VERIFIED | Mkfifo (:79), O_NONBLOCK read-open (:87), Setpgid+SIGKILL mirrored (:94,152), post-Wait poke (:156,166), scanDone joined both arms (:160,170) |
| `internal/convert/progressexec_test.go` | Success/never-opened-FIFO/SIGKILL-leak/argv-hardening tests | VERIFIED | 4 tests, all PASS incl. 5x real-ffmpeg SIGKILL loop with goroutine-delta==0 |
| `internal/convert/av.go` | ConvertWithProgress + D-02 dispatch | VERIFIED | `var _ ProgressConverter = AVConverter{}` (:615); ConvertWithProgress (:628); convertTranscodeWithProgress (:758); null paths delegate to plain runCommand paths |
| `internal/jobs/repo.go` | UpdateProgress + SetSourceDuration (non-locking) + Get scan | VERIFIED | :387-414 (both methods, no FOR UPDATE/job_events); Get's SELECT/Scan extended (:344-351) |
| `internal/jobs/jobs.go` | ProgressPct/EtaSeconds/SourceDurationSeconds fields | VERIFIED | Fields present (:48-50) |
| `internal/worker/worker.go` | Type-assert seam + throttle + terminal-100 + freeze | VERIFIED | progressRepo/progressSink/runConvertWithOptionalProgress (:1127-1215); SetProgressThrottle (:442-445) |
| `internal/api/handlers.go` | Null-aware progress_pct/eta_seconds on handleGetJob | VERIFIED | :707-708, unconditional pointer assignment, relies on encoding/json nil→null |
| `cmd/av-worker/main.go` | PROGRESS_* env knobs wired before srv.Start | VERIFIED | :102-103,125-126 |
| `.env.example` | PROGRESS_* + liveproof knobs documented | VERIFIED | :105-112 |
| `scripts/av-progress-liveproof.sh` | Live SIGKILL-mid-stream proof, hard-gated | VERIFIED | Executable, `bash -n` clean, hard-gates on go_goroutines (no skip path), operator-run PASS 40/40 recorded in SUMMARY + REQUIREMENTS.md |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `internal/convert/av.go` (convertTranscodeWithProgress) | `internal/convert/progressexec.go` (runCommandStreaming) | onLine closure feeding progress.go's parser/composer | WIRED | :768-769 |
| `internal/worker/worker.go` (progressSink) | `internal/jobs/repo.go` (UpdateProgress/SetSourceDuration) | best-effort calls | WIRED | :1163,1178 |
| `internal/worker/worker.go` (process/runConvertWithOptionalProgress) | `internal/convert` (ProgressConverter) | type-assert | WIRED | :1202, called from `process()` at :1276 |
| `internal/api/handlers.go` (handleGetJob) | `internal/jobs/repo.go` (Get) | Job.ProgressPct/EtaSeconds read | WIRED | :707-708 reads fields populated by Get's extended scan |
| `cmd/av-worker/main.go` | `internal/worker.Handler` | SetProgressThrottle before srv.Start | WIRED | :125, only av-worker calls it (`grep -rl` confirms) |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|---------------------|--------|
| `handleGetJob` response `progress_pct`/`eta_seconds` | `job.ProgressPct`/`job.EtaSeconds` | `repo.Get` SELECT of `jobs.progress_pct`/`jobs.eta_seconds`, written by real `UpdateProgress` calls from a real ffmpeg-driven sink | Yes | FLOWING — DB columns confirmed live (`\d jobs` shows `progress_pct smallint`, `eta_seconds integer` with CHECK constraints); write path traced end-to-end from ffmpeg subprocess → parser → sink → repo → column |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Real fixture parses correctly (microsecond division, N/A guard) | `go test ./internal/convert/ -run TestParseProgress -v` | 6/6 PASS | PASS |
| Percent/ETA math clamps + warms up correctly | `go test ./internal/convert/ -run 'TestComputeProgressPct|TestETA' -v` | 6/6 PASS | PASS |
| runCommandStreaming leak-safe on real ffmpeg (success + 5x SIGKILL) | `go test ./internal/convert/ -run TestRunCommandStreaming -race -v` | 4/4 PASS | PASS |
| AVConverter.ConvertWithProgress ramp/null split on real binaries | `go test ./internal/convert/ -run 'TestAV.*Progress' -v` | 3/3 PASS | PASS |
| Worker throttle/terminal-bypass/freeze contract | `go test ./internal/worker/ -run 'TestProgressThrottle|TestProgressFreezeOnAbort' -v` | PASS (verified via full suite) | PASS |
| exec.go invariant | `git diff --exit-code internal/convert/exec.go` | exit 0 | PASS |
| Full build/vet/race | `go build ./... && go vet ./... && DATABASE_URL=... go test ./internal/... -race -count=1` | all green | PASS |

### Probe Execution

| Probe | Command | Result | Status |
|-------|---------|--------|--------|
| `scripts/av-progress-liveproof.sh` | `bash "$probe"` (docker-compose live stack) | 40/40 assertions PASS, recorded 2026-07-25 in `41-04-SUMMARY.md` and `REQUIREMENTS.md` PROG-05 entry | PASS (not re-run per task instructions — the full docker-compose stack is currently up and consistent with the recorded result; static checks (`bash -n`, executable bit, hard-gate wording) independently re-verified in this session) |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|--------------|-------------|--------------|--------|----------|
| PROG-01 | 41-01, 41-02, 41-03 | progress_pct 0-100, null when indeterminate | SATISFIED | See Truth #1, #6 |
| PROG-02 | 41-02, 41-03 | source_duration_seconds persisted once | SATISFIED | See Truth #2 |
| PROG-03 | 41-03 | non-locking, throttled, monotonic, terminal bypass | SATISFIED | See Truth #3 |
| PROG-04 | 41-01, 41-03 | eta_seconds EMA, null before warm-up | SATISFIED | See Truth #4 |
| PROG-05 | 41-02, 41-04 | exec.go unchanged, leak-safe, live-verified | SATISFIED | See Truth #5, #8 |

No orphaned requirements found — REQUIREMENTS.md's Phase 41 mapping (PROG-01..05) matches exactly what the four plans' frontmatter claims.

### Anti-Patterns Found

None. Scanned every file modified by this phase (`progress.go`, `progressexec.go`, `av.go`, `repo.go`, `jobs.go`, `worker.go`, `handlers.go`, `cmd/av-worker/main.go`, `scripts/av-progress-liveproof.sh`) for `TBD|FIXME|XXX|TODO|HACK|PLACEHOLDER` — zero matches. `gofmt -l` clean on all modified Go files.

### Human Verification Required

None. The one item that would normally require human testing (PROG-05's live SIGKILL-mid-stream proof) was already executed by the operator on 2026-07-25 against the live docker-compose stack (checkpoint:human-verify gate in 41-04-PLAN.md Task 2), with a recorded 40/40 PASS documented in both `41-04-SUMMARY.md` and `REQUIREMENTS.md`. Per this verification's scope instructions, that live gate was not re-run, but its static artifact (`scripts/av-progress-liveproof.sh`) was re-inspected and its hard-gate structure confirmed to match the claimed behavior, and the recorded evidence (specific PIDs, specific goroutine counts, specific frozen pct value) is concrete and falsifiable rather than a vague claim — accepted as sufficient evidence.

### Gaps Summary

No gaps. All 5 requirements (PROG-01..05) are genuinely implemented end-to-end: a real ffmpeg `-progress` stream is parsed correctly against a captured n8.1.2 fixture, composed into a clamped/monotonic percent and an EMA-smoothed ETA, streamed through a leak-safe named-FIFO transport that preserves the hardened `exec.go` process-group-kill invariant byte-for-byte, persisted through a non-locking/throttled/monotonic write path that provably never touches the guarded transition lock or `job_events`, and surfaced null-aware on `GET /v1/jobs/{id}`. The D-01 optional-capability seam correctly isolates this to `AVConverter` alone — the other four engines (image/document/html/audio-transcribe) are provably untouched. The PROG-05 live proof was operator-run and passed with concrete, falsifiable evidence (specific PIDs, specific goroutine deltas).

One cosmetic (non-blocking) bookkeeping note: `.planning/ROADMAP.md` line 140's top-level Phase 41 checkbox (`- [ ] **Phase 41: Progress Core**`) is not yet ticked even though all four sub-plan checkboxes (41-01..41-04) are `[x]` and `REQUIREMENTS.md` marks all five PROG requirements Complete. This is a roadmap-bookkeeping gap, not a functional gap — flagged for the orchestrator to close out during phase-closure bookkeeping, not a blocker to this verification's PASS status.

---

_Verified: 2026-07-25T07:49:02Z_
_Verifier: Claude (gsd-verifier)_
