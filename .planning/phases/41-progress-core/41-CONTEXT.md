# Phase 41: Progress Core - Context

**Gathered:** 2026-07-25
**Status:** Ready for planning

<domain>
## Phase Boundary

A client polling `GET /v1/jobs/{id}` for an in-flight av/audio job sees an honest, real-time `progress_pct` and `eta_seconds`, driven by ffmpeg's `-progress` stream — genuinely `null` (never fabricated) for engines with no machine-readable signal (image/document/html and the whisper transcribe stage). The ffmpeg progress pipe MUST NOT regress the hardened process-group-kill contract (`internal/convert/exec.go`) — a timeout/cancel mid-stream leaves zero orphaned engine processes AND zero leaked reader goroutines. This is PROG-01..05.

**In scope:** a streaming exec variant that reads ffmpeg `-progress`; the converter→worker→DB progress seam (optional interface, non-invasive to the 4 non-progress engines); persisting `source_duration_seconds` from the existing ffprobe probe; a non-locking throttled progress write path (never contends on the guarded status-transition lock, never appends `job_events`); ETA derivation from `speed=`; and live SIGKILL-mid-stream verification.

**Out of scope:** SSE / webhook push delivery (Phase 42 — pure consumer of the progress/ETA data written here); progress for any engine other than av transcode; a new OCR/text-extraction engine class (deferred — see Deferred Ideas).
</domain>

<decisions>
## Implementation Decisions

### Progress sink seam (converter → worker → DB)
- **D-01:** Progress crosses the `internal/convert` → worker boundary via a NEW **optional interface** (e.g. a `ProgressConverter`/sink shape) that ONLY `AVConverter` implements. The worker holds the concrete `*convert.Registry`, looks up the `Converter`, and **type-asserts** for the optional interface — if present it passes a progress sink; if absent (image/document/html/whisper) it calls the existing `Convert(ctx, in, out, opts) error` unchanged. The shared `Converter` interface and the 4 non-progress engines are NOT modified. Rejected: ctx-value sink (context-value anti-pattern for a non-request-scoped dependency, harder to test) and a new `Convert` signature for all engines (forces a progress param onto engines that have no progress).
- **D-01a (exec invariant — PROG-05):** A NEW streaming exec function (e.g. `runCommandStreaming`) wires ffmpeg's `-progress` to a pipe and spawns a reader goroutine that parses the `key=value` progress blocks and invokes the sink. The reader goroutine MUST be guaranteed to drain and exit on process-group SIGKILL (no leak). The existing `runCommand` (exec.go:28) stays **byte-for-byte unchanged** — its Setpgid + `syscall.Kill(-pid, SIGKILL)` on `ctx.Done()` contract is the reused invariant, not a thing to edit. The new function mirrors that same kill discipline for its own pipe/goroutine.

### Which stages report progress + null policy
- **D-02:** Only the **long transcode path** (mp4/webm full re-encode, `convertTranscode`) emits a real monotonic `0→100` ramp. The cheap paths — audio-extract (`convertAudioExtract`), thumbnail (`convertThumbnail`), and the stream-copy remux fast path (`streamCopyArgs`, no decode/encode) — plus the whisper transcribe stage report `null` until the terminal `100%` write. Rationale: a seconds-long job gains nothing from a ramp; `null→100` is honest and simpler, and avoids needless DB writes on sub-second operations.
- **D-02a:** `progress_pct` is monotonic within a job. If the transcode path has a probe phase before the encode, the ramp reflects the encode only (probe is fast); it never goes backward.
- **D-02b (locked by requirement, PROG-01):** image/document/html engines and the whisper **transcribe** stage are always `null` — progress is never fabricated for an engine with no machine-readable signal.

### Progress-write throttling (PROG-03)
- **D-03:** A progress write happens only when BOTH conditions hold: at least the configured **time interval** has elapsed since the last write, AND the value moved by **≥1%**. Starting defaults: **≥2s interval AND ≥1% delta**. Both are **env-tunable** (`PROGRESS_*` knobs, measure-first discipline, mirrors the RTF/timeout env-knob precedent) — tuning without recompilation. Rejected: hardcoded constants (departs from the project's env-config discipline).
- **D-03a (terminal write always lands):** The terminal `100%` write bypasses the throttle entirely — it always lands even if the interval/delta gate would otherwise suppress it.
- **D-03b (non-locking path, PROG-03):** Progress writes go through a dedicated **non-locking UPDATE** path that NEVER acquires the guarded status-transition row lock (`SELECT … FOR UPDATE`) and NEVER appends to `job_events`. Progress is denormalized state on the `jobs` row (columns from Phase 38's `0007_job_progress.sql`), not an audited lifecycle transition.

### ETA derivation (PROG-04)
- **D-04:** `eta_seconds` is derived from ffmpeg's `speed=` field using an **EMA (exponential moving average)** with a **short warm-up window** (a few samples / ~5s). It returns `null` until the warm-up completes (before ETA is derivable). Rejected: raw `remaining/speed` each tick (jittery, `speed` swings per scene — poor UX) and a fixed-N sliding average (needs a buffer and reacts harshly to outliers).

### Source duration persistence (PROG-02)
- **D-05:** `source_duration_seconds` is persisted on the `jobs` row from the **existing** ffprobe probe (`avProbeSource` already computes `src.duration` — no new subprocess). It is the percent denominator for av/audio ffmpeg jobs and stays `null` for non-ffmpeg engines.

### Progress on abort / terminal states
- **D-06 (locked by recommendation, not objected):** On cancel / timeout / engine failure **mid-stream**, the last written `progress_pct` is **frozen** (an honest "reached 63%"), NOT reset to `null`. Resetting would destroy real information and contradict "progress is never fabricated." On success, the terminal `100%` always lands (D-03a).

### Claude's Discretion
- Exact `-progress` transport (`pipe:1` on stdout vs a dedicated fd/named pipe), the progress-block parser shape, the EMA alpha and exact warm-up length, the `PROGRESS_*` env var names, and the concrete optional-interface name/signature — all executor/planner's call, provided: the streaming reader cannot leak on SIGKILL (PROG-05), the throttle honors BOTH gates with a terminal-write bypass (PROG-03), and `runCommand` stays byte-for-byte unchanged (D-01a).
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The hardened exec invariant (PROG-05)
- `internal/convert/exec.go` — `runCommand` (:28): the Setpgid + `syscall.Kill(-pid, SIGKILL)` on `ctx.Done()` process-group-kill contract. MUST stay byte-for-byte unchanged; the new streaming variant mirrors this same discipline for its pipe + reader goroutine.

### The ffmpeg converter + existing probe (PROG-01/02)
- `internal/convert/av.go` — `AVConverter.Convert` (:522) dispatch; `convertTranscode` (:645, the only ramping path per D-02); `convertAudioExtract`/`convertThumbnail` (null paths); `avProbeSource` (:609) already yields `src.duration` for PROG-02; every `runCommand(ctx, "ffmpeg", …)` call site is a candidate to route through the streaming variant.
- `internal/convert/avduration.go` — ffprobe duration/stream probing; `ProbeDuration` is the source of `source_duration_seconds`.
- `internal/convert/whisper.go` — the audio engine (transcribe stage = `null`, D-02b); confirm whether any ffmpeg pre-decode step exists there (research question).
- `internal/convert/convert.go` — the `Converter` interface + `Registry`; the optional `ProgressConverter` interface (D-01) is added alongside, not into, `Converter`.

### Worker + DB write path (PROG-03)
- `internal/worker/worker.go` — the asynq handler that owns `*jobs.Repo` and calls `Convert`; where the type-assert (D-01) and the throttled progress-write loop live; mirror the Phase 39 `cancel.Default.Register`/deregister structure already here.
- `internal/jobs/repo.go` — `transition` (guarded lock path progress must NOT use, D-03b); add a new non-locking `UpdateProgress`-style method here.
- `internal/jobs/jobs.go` — `Job` struct; surfaces the new progress columns.
- `internal/db/migrations/0007_job_progress.sql` — the four columns (`progress_pct`, `eta_seconds`, `progress_updated_at`, `source_duration_seconds`) added by Phase 38, populated for the first time this phase.
- `internal/api/handlers.go` — `handleGetJob` (:664): surfaces `progress_pct`/`eta_seconds` on the per-job response (null-aware).

### The cancel-mid-progress reuse (validated Phase 39)
- `internal/cancel/cancel.go` + `internal/cancel/pubsub.go` — the `job:cancel` Redis pub/sub + cancel-func registry proven in Phase 39; the SIGKILL-mid-stream test (PROG-05) exercises this path against a job using the progress pipe.
- `scripts/cancel-liveproof.sh` — the Phase 39 live process-death proof; the PROG-05 live gate mirrors its process-table inspection, extended to also check for leaked reader goroutines.

### Requirements + prior context
- `.planning/REQUIREMENTS.md` — PROG-01..05.
- `.planning/phases/40-job-listing/40-CONTEXT.md` — the `progress_pct` column's read side (listing) already surfaces this phase's write side.
- `.planning/research/` (STACK/FEATURES/ARCHITECTURE/PITFALLS/SUMMARY) — v1.9 research; ffmpeg n8.1.2 pin, `out_time_ms` = MICROSECONDS (÷1e6) pitfall.
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `avProbeSource` (av.go:609) already probes `src.duration` under its own short timeout — PROG-02's denominator needs no new subprocess, just persistence.
- `runCommand` (exec.go:28) — the reused SIGKILL-on-ctx invariant; the streaming variant copies its kill discipline rather than modifying it.
- Phase 38's `0007_job_progress.sql` columns exist and are null today — this phase is their first writer.
- Phase 39's `internal/cancel` pub/sub + `cancel.Default.Register`/deregister structure in worker.go — the cancel-mid-progress path reuses it wholesale.

### Established Patterns
- Env-knob + measure-first config discipline (RTF/timeout precedent) → the `PROGRESS_*` throttle knobs (D-03).
- Guarded transition = the ONLY path for lifecycle status changes; progress is deliberately OUTSIDE it (D-03b) — a non-locking denormalized update, never a `job_events` append.
- `Set*`-at-startup process-global config (`SetAVDiskSafetyFactor`, `SetAudioThreads`) — precedent for wiring the throttle knobs once before the asynq server starts.
- Optional-capability-via-type-assert is a fresh seam for this codebase (the worker currently depends on concrete converter types) — D-01 introduces it narrowly.

### Integration Points
- `internal/convert` (streaming exec + optional `ProgressConverter`) ← type-asserted by `internal/worker` ← writes via a new non-locking `jobs.Repo` method ← surfaced by `internal/api` `handleGetJob`. Phase 42 (SSE/webhook) is a downstream consumer of exactly these columns.
</code_context>

<specifics>
## Specific Ideas

- `out_time_ms` in ffmpeg `-progress` output is **microseconds**, not milliseconds — divide by 1e6 to get seconds (flagged in v1.9 research; SC1 verifies against a real n8.1.2 captured fixture).
- The PROG-05 live gate must inspect BOTH the process table (zero orphaned ffmpeg) AND goroutine state (zero leaked readers) — `go test -race` alone is explicitly insufficient per the requirement.
- Prefer a captured real `-progress` fixture from the pinned ffmpeg n8.1.2 binary for deterministic parser unit tests, over a hand-written mock.
</specifics>

<deferred>
## Deferred Ideas

- **OCR / text-extraction engine class** (raised during discussion): a new worker that extracts text from documents/images (tesseract or PaddleOCR). Fits the proven engine-class pattern (own queue/worker/binary/container/KEDA, fail-closed content validation) but is a NEW engine class — its own phase/milestone, NOT Phase 41. To be captured as a seed alongside SEED-005/006/007 (CAD/archive/preview) via `/gsd:capture --seed` after v1.9 closes.
- SSE / webhook progress push — already scoped as Phase 42 (PUSH-01/02), not deferred, just downstream.
</deferred>

---

*Phase: 41-progress-core*
*Context gathered: 2026-07-25*
