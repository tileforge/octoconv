# Phase 41 Discussion Log

**Date:** 2026-07-25
**Mode:** discuss (default)

## Framing

Progress Core (PROG-01..05). Carried forward without re-asking (research + code scout): the `runCommand` SIGKILL-on-ctx process-group-kill contract (exec.go:28) is an INVARIANT — not to be regressed (PROG-05); `internal/cancel` pub/sub validated in Phase 39 is reused for the cancel-mid-progress path; env-knob + measure-first config discipline (RTF/timeout precedent); progress writes live OUTSIDE the guarded status-transition lock and never append `job_events` (PROG-03). Scouted: `runCommand` is batch (captures whole stdout, no streaming) → needs a streaming variant; `internal/convert` is DB-decoupled and `Convert(ctx,in,out,opts) error` has no progress channel → a seam is required; `avProbeSource` already yields `src.duration` (PROG-02 = persist only); only transcode is long, audio-extract/thumbnail/whisper are cheap/no-signal.

## Gray areas presented (all 4 selected)

**A. Progress sink seam** — how ffmpeg progress crosses converter→worker→DB without touching the 4 non-progress engines or regressing exec.go.
- **Decision (D-01/D-01a):** optional `ProgressConverter`/sink interface, worker type-asserts; new `runCommandStreaming` reads `-progress` via a leak-safe reader goroutine; existing `runCommand` byte-for-byte unchanged.

**B. Stages + null policy** — which paths ramp vs null→100.
- **Decision (D-02):** only the long transcode (mp4/webm re-encode) ramps 0→100; audio-extract, thumbnail, stream-copy, and whisper transcribe = null→100.

**C. Throttle defaults** — interval + delta + terminal write.
- **Decision (D-03):** ≥2s interval AND ≥1% delta, env-tunable (`PROGRESS_*`); terminal 100% bypasses throttle; non-locking write path.

**D. ETA smoothing + abort behavior** — how eta_seconds is derived; what happens to progress on abort.
- **Decision (D-04):** EMA + short warm-up (~5s), null before derivable.
- **Decision (D-06, locked by recommendation, unopposed):** on cancel/timeout/fail mid-stream, freeze last progress_pct (honest "reached 63%"), never reset to null; success always lands 100%.

## Out-of-scope raised

- **OCR / text-extraction engine class** (user asked "how hard would an OCR worker be?"). Answer: not hard — fits the engine-class pattern (tesseract/PaddleOCR, own queue/worker/container/KEDA). But it is a NEW engine class = its own phase/milestone, not Phase 41. Redirected → captured as a Deferred Idea; to become a seed via `/gsd:capture --seed` after v1.9 closes.

## Deferred

- OCR engine class → seed after v1.9 (see above).
- SSE/webhook progress push → already Phase 42 (PUSH-01/02), downstream consumer of this phase's columns.
