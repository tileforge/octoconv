# Project Research Summary

**Project:** OctoConv v1.9 "Job Progress & Control"
**Domain:** Backend integration milestone — job progress %, ETA, live push (webhook/SSE), job listing, and in-flight cancellation added to an already-production-hardened async Go file-conversion service (asynq + Redis, Postgres system-of-record, S3/MinIO storage)
**Researched:** 2026-07-24
**Confidence:** HIGH

## Executive Summary

This is not greenfield feature research — it's an integration design exercise on top of a mature, previously-hardened pipeline (Postgres-first double write, guarded status transitions via `Repo.transition()`, engine-class asynq queues, hardened `runCommand` process-group kill, HMAC-signed webhook delivery). All four researchers (stack, features, architecture, pitfalls) independently converged on the same core design and reached it by reading the same code (`internal/convert/exec.go`, `internal/jobs/repo.go`, `internal/worker/worker.go`, `internal/reconciler/reconciler.go`), which is a strong signal the design is correct rather than coincidental. **Zero new Go dependencies are required** — stdlib (`net/http`'s `http.Flusher`, `os/exec`+`os.Pipe`+`bufio.Scanner`) plus the already-present `asynq`/`pgx` cover all five features.

The recommended approach: (1) progress/ETA/`source_duration` live in new, nullable `jobs` columns written through a **non-locking** `UpdateProgress` (`WHERE status='active'`, dual time+delta throttle) that deliberately bypasses the guarded-transition lock — progress is high-frequency telemetry, not a state change, and routing it through `transition()` would serialize it against real status changes and bloat the append-only `job_events` audit log for zero benefit; (2) progress reporting is an **optional** `ProgressReporter` capability only the ffmpeg-backed av/audio stage implements, added via an additive `runCommandWithProgress` sibling to `exec.go` that must not regress the load-bearing Setpgid+SIGKILL contract; (3) cancellation reuses the *already-existing* `StatusCanceled` DB value and Go constant (Phase 1 leftover, never wired up) through a guarded `{queued,active}→canceled` transition, with asynq's `Inspector.CancelProcessing` as a best-effort speed optimization and the Postgres transition allow-list as the **authoritative correctness backstop** (asynq has a confirmed, unfixed upstream retry-on-cancel race, issue #987, present in the pinned v0.26.0); (4) `GET /v1/jobs` is architecturally independent and can ship in parallel with everything else, using keyset pagination on `(created_at, id)` to avoid drift under this table's continuous-insert load.

The key risks all four researchers flagged in near-identical language: (a) unthrottled progress writes causing a Postgres write-amplification storm once multiple video/audio jobs run concurrently (already-proven KEDA scale-out makes this not hypothetical); (b) a naive ffmpeg-progress-pipe bolt-on silently breaking the Setpgid+SIGKILL contract via a goroutine/fd leak on cancel — this needs its own live-gated verification, mirroring the project's Phase 9 zombie-reaping precedent; (c) the ffmpeg `out_time_ms`-is-actually-microseconds naming trap, verifiable only against the actual pinned n8.1.2 binary, not assumed from docs; (d) cancellation races across three existing subsystems simultaneously (guarded transitions, reconciler stale-sweep, asynq retry) — mitigated entirely by mechanisms that already exist (the transition allow-list, the "already active/done/canceled" `SkipRetry` idiom already present at every `MarkActive` call site) rather than new code.

## Key Findings

### Recommended Stack

No new dependencies. SSE uses stdlib `http.Flusher`/`http.ResponseController`, verified pass-through-compatible with the existing chi v5.3.0 middleware chain (`Logger`/`RequestID`/`Recoverer`) by reading the vendored `wrap_writer.go` directly. ffmpeg progress parsing uses stdlib `os.Pipe`+`bufio.Scanner` as an additive sibling to the existing `runCommand`, never a replacement. Cancellation uses `asynq.Inspector.CancelProcessing` + `asynq.TaskID`, both already present in the pinned `v0.26.0` — no upgrade needed, and none exists that would fix the confirmed retry-on-cancel race (asynq#987), so the design must work around it regardless of version. Pagination uses plain parameterized SQL through the existing `pgxpool`; no pagination library exists or is warranted for a two-column keyset query.

**Core technologies (all already dependencies, no version changes):**
- stdlib `net/http` (`http.Flusher`) — SSE progress endpoint; verified compatible with existing middleware
- stdlib `os/exec` + `os.Pipe` + `bufio.Scanner` — ffmpeg `-progress` parsing via a new `runCommandWithProgress`, additive to `exec.go`
- `github.com/hibiken/asynq` v0.26.0 (existing) — `Inspector.CancelProcessing`/`asynq.TaskID` for in-flight cancellation signal (best-effort only; correctness depends on Postgres)
- `github.com/jackc/pgx/v5` v5.10.0 (existing) — keyset-pagination SQL for `GET /v1/jobs`, `UpdateProgress`, `Cancel`

### Expected Features

**Must have (table stakes, P1):**
- `progress_pct` (nullable int 0-100) on `GET /v1/jobs/{id}` — `null` is a valid, expected value for engines that can't report it (honesty over fabrication)
- `eta_seconds` (nullable), coarse-rounded, `null` when not derivable
- `GET /v1/jobs` scoped to caller's own jobs (hard `WHERE client_id=$1`), status/engine filters, server-enforced max page size, keyset pagination
- `POST /v1/jobs/{id}/cancel` — idempotent (200 + current status on already-terminal job, never overwrite `done`/`failed`), guarded `{queued,active}→canceled` transition, real in-flight process kill via existing `runCommand` SIGKILL path
- Persisted `source_duration` (av/audio only) as the percent denominator — non-ffmpeg engines stay indeterminate/null

**Should have (P2, deferred until P1 validated):**
- Webhook `progress` event — new `event` discriminator field, throttled, **best-effort single-attempt delivery** (deliberately NOT the existing bounded-retry/dead-letter contract — a stale progress delivery isn't worth chasing and would compete for retry budget with the terminal event)
- SSE stream `GET /v1/jobs/{id}/events` — same auth/ownership as every other route, DB-poll sourced (keeps API stateless), 1-2s latency ceiling accepted as a deliberate trade-off

**Defer (v2+):** stage-based sub-progress in the reference frontend UI; batch cancel; `event`-discriminated payloads for all webhook types (broader contract-versioning conversation).

**Anti-features to explicitly reject:** per-tick/per-frame webhook delivery, progress webhook events on the durable retry/dead-letter contract, fabricated/simulated progress for engines without a real signal, unauthenticated SSE (no query-string token workaround), unbounded job listing, auto-resume of a canceled job, silently overwriting a completed job's terminal state on a stale cancel race.

### Architecture Approach

Progress and cancellation each get a dedicated, non-locking write path deliberately separated from the existing guarded-transition machinery, while cancellation's *correctness* still flows entirely through that same machinery (reused, not duplicated). The `internal/convert.ProgressReporter` interface is additive/optional — only `AVConverter`/`AudioConverter`'s ffmpeg-normalize stage implement it — so the stable, audited `Converter` interface (`Pairs`/`Convert`/`Engine`) is never widened, and libvips/LibreOffice/Chromium/whisper-cli's transcribe stage are completely untouched. A small new `internal/cancel` package provides an in-memory `map[uuid.UUID]context.CancelFunc` registry bridged to a Redis pub/sub channel (`job:cancel`) for low-latency signal delivery to whichever worker replica happens to be running a job — deliberately a *speed* optimization only, never the source of truth. The SSE endpoint stays in the stateless `api` process and polls the same Postgres row every 1-2s rather than requiring worker-to-api affinity or a new push channel, preserving the project's "any api replica can serve any request" horizontal-scaling constraint.

**Major components (new/modified):**
1. `internal/jobs.Repo.UpdateProgress` — non-locking, self-conditioning `UPDATE ... WHERE status='active'` (no `job_events` row, no `FOR UPDATE`)
2. `internal/jobs.Repo.Cancel` — thin wrapper around the existing `transition()` helper, `{queued,active}→canceled`
3. `internal/jobs.Repo.List` — client-scoped keyset-paginated read, reuses/extends the existing `client_id, created_at` index
4. `internal/convert.ProgressReporter` (new optional interface) + `exec.runCommandWithProgress` (additive sibling to `runCommand`, fd-3 via `cmd.ExtraFiles`, parent write-end closed immediately after `Start()`)
5. `internal/cancel` package — in-memory cancel-func registry + Redis pub/sub subscribe/publish, imported by both `api` and every worker binary
6. Migration `0007_job_progress.sql` — nullable `progress_pct`, `eta_seconds`, `progress_updated_at`, `source_duration_seconds`; `canceled` needs no schema change (already in the CHECK constraint since Phase 1)

**Suggested build order:** schema migration → cancellation end-to-end → job listing (parallelizable) → progress core (riskiest, sequenced after cancellation to validate the new Redis pub/sub pattern first) → live push (SSE + webhook progress event, built last since both are pure consumers of progress data).

### Critical Pitfalls

1. **Progress-write storm** — a naive per-tick `UPDATE` at ffmpeg's ~1-30Hz `-progress` cadence causes Postgres write-amplification under concurrent video/audio jobs (already-proven KEDA scale-out, not hypothetical). Avoid: dual-gate throttle — time interval AND ≥1% delta, both must pass; the final 100%/terminal write always bypasses the throttle.
2. **ffmpeg `-progress` parsing traps** — `out_time_ms` is actually **microseconds** (÷1,000,000, not ÷1,000), a well-known unfixed ffmpeg naming wart; must be verified against a real fixture captured from the pinned n8.1.2 binary, not assumed from docs or a short test clip. Also: clamp to [0,100], reject non-monotonic ticks, treat `speed=` as noisy/needs a warm-up window before trusting it for ETA.
3. **Progress pipe breaking the Setpgid+SIGKILL contract** — the single highest-risk item; a careless pipe-reader goroutine can leak forever if the parent's write-end fd copy isn't explicitly closed after `cmd.Start()`. Requires its own live-gated phase with a real SIGKILL-mid-stream test asserting zero orphaned processes AND zero leaked goroutines (`go test -race` is insufficient — it can't catch leaks/deadlocks). Mirrors the Phase 9 zombie-reaping precedent.
4. **Cancellation races across three existing subsystems** — guarded transitions, reconciler stale-sweep, and asynq retry classification must all be threaded consistently. Fully solved by mechanisms that already exist: the transition allow-list rejects any late `MarkDone`/`MarkFailed` on an already-canceled row, and every `Handle*Convert` already comments "already active/done/canceled — let asynq drop it." Must NOT rely on the handler's own `errors.Is(err, context.Canceled)` check to stop retries (asynq#987 race can bypass it) — the DB-side allow-list is the only guaranteed backstop.
5. **`GET /v1/jobs` correctness/security traps** — offset pagination drift under this continuously-inserting table, N+1 queries if progress isn't denormalized onto the `jobs` row, and cross-client leakage if `WHERE client_id=$1` isn't enforced at the SQL layer (not filtered in Go). Solved by keyset pagination + denormalized columns + a dedicated cross-client leak test (not inferred from the existing single-job 404 test).

## Implications for Roadmap

Suggested phase structure (continues numbering from v1.8 which ended at Phase 37 → v1.9 starts at Phase 38):

### Phase 38: Schema Foundation
**Rationale:** Every other phase reads or writes these columns; purely additive/nullable so zero risk, unblocks parallel work.
**Delivers:** Migration `0007_job_progress.sql` adding `jobs.progress_pct`, `jobs.eta_seconds`, `jobs.progress_updated_at`, `jobs.source_duration_seconds` (all nullable). `canceled` requires no schema change (already in the CHECK constraint + Go constants since Phase 1).
**Avoids:** N/A (foundational, zero-risk).

### Phase 39: Cancellation (end-to-end)
**Rationale:** Dedicated phase, built before progress; reuses the hardened `runCommand` SIGKILL path unchanged; clearest client-visible value; correctness backstop already exists. The one genuinely new piece is the Redis pub/sub plumbing — worth validating in isolation before building progress on the same worker loop.
**Delivers:** `Repo.Cancel` (guarded `{queued,active}→canceled`), `POST /v1/jobs/{id}/cancel` (404-not-403 ownership, idempotent terminal handling), `internal/cancel` package (in-memory registry + Redis pub/sub `job:cancel`), worker `context.WithCancel` wiring, `context.Canceled` branch in the shared terminal classifier (skip `MarkFailed`, `SkipRetry`), reconciler/webhook `canceled`-awareness with an explicit `FindStale`-excludes-canceled regression test.
**Avoids:** cancellation races (cancel-after-done, cancel-wrong-worker, reconciler resurrection, asynq retry-on-cancel).
**Live verification:** an actual process-table check that the real ffmpeg/whisper-cli/soffice child is dead, not just that the DB row says `canceled`.

### Phase 40: GET /v1/jobs Listing
**Rationale:** Independent of cancellation and progress; can run in parallel with Phase 39.
**Delivers:** `Repo.List` (keyset pagination on `(created_at, id)`, hard `WHERE client_id=$1`, status/engine filters, server-max page size), `handleListJobs` route.
**Avoids:** offset-drift, N+1, cross-client leakage — keyset + denormalized `progress_pct` + SQL-layer `client_id` filter + a dedicated two-API-key leak test.

### Phase 41: Progress Core (ffmpeg-driven % + ETA)
**Rationale:** Riskiest change (new exec-contract variant, new Converter capability, hot path of two hardened engine classes); sequenced after cancellation so the `internal/cancel` Redis pattern is already validated.
**Delivers:** `convert.ProgressReporter` optional interface; `exec.runCommandWithProgress` (fd-3 via `cmd.ExtraFiles`, parent write-end closed after `Start()`); `AVConverter`/`AudioConverter` ffmpeg-stage implementations parsing `out_time_ms`(µs)/`speed`; `jobs.Repo.UpdateProgress` (non-locking, dual throttle); `GET /v1/jobs/{id}` gains `progress_pct`/`eta_seconds`.
**Split into two verification tracks:** (a) parsing logic vs a REAL captured n8.1.2 fixture (normal / unknown-duration / non-monotonic / >100% cases); (b) the progress-pipe-to-exec wiring as its OWN live-gated plan with a SIGKILL-mid-stream test asserting zero orphaned processes AND zero leaked goroutines.

### Phase 42: Live Push (SSE + webhook `progress` event)
**Rationale:** Built last; both are pure consumers of Phase 41's data.
**Delivers:** `GET /v1/jobs/{id}/events` SSE (DB-poll ticker 1-2s, same auth/ownership, heartbeat comments for proxy idle-timeout); webhook `progress` event (new `event` discriminator, opt-in, throttled, best-effort single-attempt — NOT the retry/dead-letter contract).
**Avoids:** SSE goroutine/connection leaks, proxy buffering, unauth/cross-client SSE, unbounded fan-out. Live-verify through the real deployed path (compose AND k8s).

### Research Flags
- **Phase 41:** verify ffmpeg `-progress` fields against the actual pinned n8.1.2 binary (capture a real fixture); confirm whisper.cpp has no machine-readable progress stream; treat the pipe-wiring as its own live-gated plan.
- **Phase 39:** re-check asynq `CancelProcessing` vs asynq#987 at the version pinned at implementation time.
- **Phase 42:** live-verify reverse-proxy/ingress buffering on the real topology.
- **Standard patterns (skip research):** Phase 38 (additive nullable migration), Phase 40 (keyset pagination).

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Verified against vendored source of the exact pinned versions (asynq@v0.26.0, chi/v5@v5.3.0) + a live open issue |
| Features | MEDIUM | Synthesized from MediaConvert/asynq/ffmpeg/REST precedent; no single identical vendor |
| Architecture | HIGH | Derived directly from the current codebase — integration design on known code |
| Pitfalls | HIGH (project) / MEDIUM (general) | Codebase-grounded pitfalls HIGH; general ffmpeg/SSE ecosystem MEDIUM |

**Overall confidence:** HIGH

### Gaps to Address (product decisions for requirements)
- Webhook `progress` event **opt-in vs default-on** (recommend opt-in via a `progress_webhook` flag to avoid surprising existing integrations).
- Indeterminate-progress fallback for non-ffmpeg engines: `progress_pct: null` agreed; whether a coarse elapsed-vs-timeout heuristic is ever worth it is punted to product.
- whisper.cpp transcribe-stage progress precision: likely indeterminate, unverified against the pinned build.
- Throttle / SSE-interval **constants as tunable env vars**, measured against a live multi-job load test (measure-don't-assume, per the RTF-gate precedent).
- ETA formula: ffmpeg `speed=`-derived (with warm-up/smoothing) vs progress-ratio-derived — pin in requirements.

## Sources

### Primary (HIGH)
- Vendored `github.com/hibiken/asynq@v0.26.0` (`inspector.go`, `processor.go`, `subscriber.go`) — `CancelProcessing`/`TaskID` + the retry-on-cancel race
- Vendored `github.com/go-chi/chi/v5@v5.3.0/middleware/wrap_writer.go` — `http.Flusher` pass-through
- [hibiken/asynq#987 "Task always retry when canceled"](https://github.com/hibiken/asynq/issues/987) — live open issue
- This repository, read directly: `internal/api/*`, `internal/jobs/*`, `internal/worker/worker.go`, `internal/convert/{convert.go,exec.go,av.go,avduration.go,audioduration.go}`, `internal/reconciler/reconciler.go`, `internal/db/migrations/*.sql`

### Secondary (MEDIUM)
- AWS MediaConvert job-progress docs (progress/phase/percent precedent)
- ffmpeg `-progress` docs (`out_time_ms` units quirk)
- ffmpeg-kit Wiki: Pipe Support (streaming-pipe pattern)
- Speakeasy REST pagination guide (cursor vs offset drift)

### Tertiary (LOW, needs validation)
- whisper.cpp progress-stream capability — unverified against the pinned build
- ffmpeg n8.1.2-specific field behavior — capture a real fixture during implementation

---
*Research completed: 2026-07-24*
*Ready for roadmap: yes*
