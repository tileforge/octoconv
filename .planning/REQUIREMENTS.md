# Requirements: OctoConv v1.9 — Job Progress & Control

**Defined:** 2026-07-23
**Core Value:** Внутренние сервисы компании могут безопасно и надёжно поставить задачу конвертации файла и получить результат — а для долгих задач видеть прогресс и управлять ими, без риска для стабильности/безопасности продакшена.

## v1 Requirements

Requirements for milestone v1.9. Each maps to a roadmap phase.

### Progress & ETA (PROG)

- [ ] **PROG-01**: `GET /v1/jobs/{id}` returns a `progress_pct` (0–100 int) for an in-flight av/audio job, driven by ffmpeg `-progress`; it is `null` (indeterminate) for engines with no machine-readable signal (image/document/html and the whisper transcribe stage) — progress is never fabricated.
- [ ] **PROG-02**: The source media duration (already probed transiently by ffprobe for av/audio) is persisted on the job row as the percent denominator; non-ffmpeg jobs leave it null.
- [ ] **PROG-03**: Progress is written through a non-locking update path that never contends on the guarded status-transition lock and never appends to the `job_events` audit log; writes are throttled by BOTH a time interval AND a ≥1% delta, and the terminal 100% write always lands.
- [ ] **PROG-04**: `GET /v1/jobs/{id}` returns an `eta_seconds` (time remaining) derived from ffmpeg's `speed=` field with a warm-up/smoothing window; `null` when not derivable.
- [ ] **PROG-05**: The ffmpeg progress pipe preserves the hardened process-group-kill contract — a job that times out or is canceled mid-stream leaves zero orphaned engine processes AND zero leaked reader goroutines (live-verified, not just `go test -race`).

### Cancellation (CNCL)

- [ ] **CNCL-01**: `POST /v1/jobs/{id}/cancel` transitions a `queued` or `active` job to `canceled` via the guarded transition; a job owned by another client or not found returns `404` (never `403`), mirroring the existing ownership model.
- [ ] **CNCL-02**: Cancel is idempotent — an already-terminal job (`done`/`failed`/`canceled`) returns `200` with its current status and never overwrites a terminal outcome.
- [ ] **CNCL-03**: Cancelling an `active` job kills the real in-flight engine process group (ffmpeg/whisper/soffice/…) via the existing hardened kill path, verified the child process is actually dead — not just that the DB row reads `canceled`.
- [ ] **CNCL-04**: A `canceled` job is terminal and non-retryable — the reconciler stale-sweep and asynq retry never resurrect it, backstopped by the Postgres guarded-transition allow-list (not the handler's own `context.Canceled` check, which the asynq#987 race can bypass).

### Job Listing (JLIST)

- [ ] **JLIST-01**: `GET /v1/jobs` returns only the caller's own jobs (SQL-layer `WHERE client_id = $1`, never Go-side filtering), with optional `status` and `engine` filters.
- [ ] **JLIST-02**: Listing uses keyset pagination (stable under this table's continuous inserts) with a server-enforced max page size; each returned job includes its `status` and `progress_pct` from the denormalized columns (no N+1).

### Live Push (PUSH)

- [ ] **PUSH-01**: `GET /v1/jobs/{id}/events` streams live progress via SSE with the same ApiKey auth + 404-ownership check as every other route; it is DB-poll sourced so any api replica can serve it (stateless), sends heartbeat comments to survive proxy idle-timeouts, and closes cleanly on terminal state or client disconnect without leaking goroutines.
- [ ] **PUSH-02**: An opt-in `progress` webhook event (new `event` discriminator field; enabled by a per-job flag alongside `callback_url`) delivers throttled progress on a best-effort single-attempt path — explicitly NOT the terminal event's bounded-retry/dead-letter contract, so stale progress never competes for the `done`/`failed` delivery budget.

## v2 Requirements

Deferred to a future release. Tracked, not in this roadmap.

### Progress UX / breadth

- **PROG-V2-01**: Stage-based sub-progress surfaced in the reference frontend UI (progress bars).
- **PROG-V2-02**: Coarse elapsed-vs-timeout progress heuristic for non-ffmpeg engines (rejected for v1.9 — `null` is the honest answer).
- **CNCL-V2-01**: Batch cancel (cancel many jobs in one call).
- **PUSH-V2-01**: `event`-discriminated payloads for ALL webhook types (broader webhook contract-versioning conversation; v1.9 adds the discriminator only for the new `progress` event).

## Out of Scope

Explicitly excluded for v1.9, documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Fabricated/simulated progress for engines without a real signal | Violates the project's no-fabrication principle; `null` is correct |
| Progress webhook on the durable retry/dead-letter contract | Retrying stale progress is harmful and would starve the terminal-event retry budget |
| Unauthenticated / query-string-token SSE | Must reuse the existing ApiKey auth + 404-ownership verbatim |
| Auto-resume of a canceled job | Cancel is terminal by design |
| Offset pagination for `GET /v1/jobs` | Drifts under continuous inserts; keyset is required |
| whisper transcribe-stage fine-grained progress | No machine-readable progress stream in the pinned build; stays indeterminate |

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| PROG-01 | Phase 41 | Pending |
| PROG-02 | Phase 41 | Pending |
| PROG-03 | Phase 41 | Pending |
| PROG-04 | Phase 41 | Pending |
| PROG-05 | Phase 41 | Pending |
| CNCL-01 | Phase 39 | Pending |
| CNCL-02 | Phase 39 | Pending |
| CNCL-03 | Phase 39 | Pending |
| CNCL-04 | Phase 39 | Pending |
| JLIST-01 | Phase 40 | Pending |
| JLIST-02 | Phase 40 | Pending |
| PUSH-01 | Phase 42 | Pending |
| PUSH-02 | Phase 42 | Pending |

**Coverage:**
- v1 requirements: 13 total
- Mapped to phases: 13 (roadmap created 2026-07-24, Phases 38-42)
- Unmapped: 0 ✓

**Note:** Phase 38 (Schema Foundation) carries no direct requirement mapping — it is the additive, zero-risk migration (`0007_job_progress.sql`) that Phase 40's denormalized read and Phase 41's progress/ETA writes depend on.

## Notes

- **Tunable constants (env vars, measure-don't-assume):** the progress-write time-gate interval, the ≥1% delta gate, the SSE poll cadence, and the max page size ship as env vars (matching the project's `os.Getenv`-only config discipline), with defaults measured against a live multi-job load test — not hardcoded. Mirrors the RTF/timeout-gate precedent.
- **No schema migration for `canceled`:** the value already exists in the `jobs.status` CHECK constraint and the Go `StatusCanceled` constant since `0001_init.sql` — Phase 38's migration adds only the nullable progress columns.
- **Zero new Go dependencies** (stdlib + existing asynq/pgx), per research SUMMARY.md.

---
*Requirements defined: 2026-07-23*
*Last updated: 2026-07-24 after v1.9 roadmap creation (Phases 38-42, 13/13 requirements mapped)*
