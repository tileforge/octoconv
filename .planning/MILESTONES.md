# Milestones

## v1.14 Typesetting & Debt (Shipped: 2026-09-05)

**Phases:** 3 (62-64) | **Plans:** 7
**Range:** 2026-09-02 → 2026-09-05 (4 days, 13 commits on main; 52 in phase 64's branch alone)
**Code:** +3.9k Go (non-test), +5.9k Go test
**Requirements:** TEX-01..05, SANI-09, PROBE-01/02, DEBT-14 — all closed, two
reworded on measurement rather than ticked as written.

### Delivered

A ninth engine class that typesets LaTeX **offline**. A client posts a `.tex`,
gets back a PDF with real typography — formulas, resolved cross-references
including forward ones, and a bibliography — from an engine that has no network
at all.

**Key accomplishments:**

- **tectonic as the ninth engine class**, by the established template: its own
  queue, `cmd/tectonic-worker`, container, transient/terminal classification and
  ScaledObject, with the other eight rendering byte-identical.

- **A closed TeX distribution, baked in.** A 122 MiB reduced bundle (from 2.69
  GiB unreduced) built by a script that derives its keep-lists from a measured
  dependency closure rather than from names somebody thought of, digest-pinned
  in the Dockerfile and verified again inside the image build. The worker has no
  egress: a client's `\usepackage` cannot decide what gets downloaded.

- **Shell escape is structurally impossible**, proven by running a hostile
  document and by inverting the defence, not by citing `--untrusted`.

- **A MEASURED timeout, paired with the input ceiling that makes it a bound.**
  `ceil(25.76 s/pass × 7 passes × 1.5) = 271s`, where 7 is the engine's own
  rerun maximum and the per-pass cost comes from a 2 143 444-byte document — the
  largest the API accepts. The measurement's real output was a finding: cost is
  linear in source size, so the 100 MiB ceiling this engine had inherited made
  the timeout bound nothing. The operator chose the (2 MiB, 271s) pair from
  three measured options before any of it was written.

- **An admission gate that refuses what cannot succeed**, reworded on evidence:
  gating on the declared eighteen packages was measured to refuse twelve that
  compile — five of them in pandoc's own default template — so admission is
  keyed on the bundle's 608 resolvable `.sty` files, with a small deny list for
  what the bundle carries and this deployment refuses on purpose.

- **Bounds on what comes BACK, not only on what goes in.** For the one engine
  whose input is a program, the input ceiling implies nothing about the output:
  199 bytes of `.tex` measured 19 545 890 bytes of PDF in 93 seconds, and a
  235-byte `\message` loop produced 323 MB of engine log in 30. Both are now
  bounded, and an OOM-killed or panicking engine lands the job in a terminal
  state instead of a retry loop.

- **Phase 63's sanitize hardening and probe page ceiling**, and phase 62's
  operational debt tail: an initContainer for all ten pods, a test that had
  stopped asserting about the database, and one requirement that was already met.

---

## v1.13 Inspect & Operations (Shipped: 2026-09-02)

**Phases:** 6 (56-61) | **Plans:** 38 | **Tasks:** 58
**Range:** 2026-08-07 → 2026-09-02 (26 days, 45 commits)
**Code:** +15.5k Go (non-test), +22.8k Go test — the test-to-code ratio is 1.5:1 and is not an accident; see below.
**Requirements:** 28/28 v1 complete.

### Delivered

A client can now ask the service what a file *is* before paying for a job, and
can ask for a disarmed copy of it. Two new synchronous surfaces and one new
engine class, on top of a job pipeline that gained a second dimension.

**Key accomplishments:**

- **`POST /v1/inspect`** — one synchronous request returns the magic-byte
  format, a closed 14-code findings vocabulary with severity, and the
  (operation, target) triples available for that file with a live quota check
  and an upper-bound duration estimate carrying its own provenance. It writes
  nothing and pins nothing a later job could use to skip its own validation.
  Bounded by a byte+time work ceiling, a non-blocking concurrency semaphore
  returning `inspect_busy`, and an independent per-client rate budget.

- **`operation` became a real dimension.** The converter registry re-keyed from
  (from, to) pairs to (operation, from, to) triples; `Register` now panics on
  collision instead of silently overwriting. Two ordered fail-closed 422 gates
  reject garbage before any S3 or DB write. Output rows gained a closed-enum
  `role`, and `handleGetJob` selects the download URL by role rather than by
  ordinal — closing a live defect where a job's first-uploaded output was
  blindly presigned as "the result".

- **`operation=sanitize` on an eighth engine class (`probe`)** — the first class
  that converts nothing. PDF is rebuilt through PostScript; OOXML macro and
  object parts are dropped **by content and by relationship type**, never by
  name or folder; image metadata is stripped structurally with the compressed
  data copied byte for byte. Every output is then re-checked by an
  implementation that did not do the removing — pdfcpu for PDF, unzip and
  libmagic for OOXML, a full decode for images — and an output that cannot be
  verified is deleted rather than delivered.

- **The sanitize report says what was checked and refuses to say more.** Built
  from two closed vocabularies with no verdict field and no `clean` boolean,
  held equal to `docs/SANITIZE.md` by a bidirectional test, with a matcher that
  fails the build on an affirmative safety claim. CDR removes carriers; it does
  not determine whether code is malicious, and nothing in the contract now says
  otherwise.

- **Born-digital PDFs stopped being refused.** A PDF with a text layer routed
  to `txt` now receives that layer instead of an OCR rejection, and `hocr`/
  `alto` still render — because `pdftotext` produces no word or line boxes.

- **The error-code contract closed.** 28 codes (14 sync, 14 async) frozen in
  `docs/ERROR-CODES.md`, tied to Go by a bidirectional set-equality test, with
  a separate historical section so a retired code stays looked-up-able for jobs
  that already carry it.

- **Operators can cancel a stuck job** with `manage-clients cancel-job` instead
  of raw SQL — which had been repairing the quota and silently breaking its
  accounting in one stroke.

### What this milestone actually cost, and why it is worth recording

Phase 60 went through **eight rounds of external review producing 26 findings**,
23 fixed before merge. Twelve of the first fourteen were the same shape: a
component that worked with nothing connecting it, or two components sharing one
wrong assumption. Three named forms of it recurred —

1. a test that calls a function directly cannot see that nothing else calls it;
2. a test that exercises two components together cannot see an assumption they
   share;
3. a class fix that stops at its first instance leaves the second under the very
   comment explaining why that shape is wrong.

The last one repeated four times in one phase. The countermeasure that worked
was not more tests of the same kind: it was making the independent checker
*broader* than the thing it checks — which immediately caught two remover bugs
that eight review rounds had not.

### Known gaps at close

- **probe has no page ceiling.** Measuring the timeout established that cost is
  per page (~2.0 s/page for scans), not per byte, so a document beyond ~300
  scanned pages fails on timeout instead of being refused at admission the way
  OCR refuses with `too_many_pages`. Tracked in `todos/pending`.
- **Three P2 review findings merged unfixed by decision** (round 8): multi-chunk
  ICC profiles, `endstream` as a substring inside binary stream data, and a
  crafted relationship that removes a package's own main part. None is the
  threat-survives-and-is-reported-verified class. Tracked in `todos/pending`.
- **Known deferred items at close: 46** (26 quick tasks, 15 dormant seeds,
  5 todos, accumulated across thirteen milestones) — see STATE.md § Deferred
  Items.


## v1.12 Consumer Accounting & Fairness (Shipped: 2026-08-07)

Known deferred items at close: 13 entries across tech_debt / accepted_risk / infra / tooling_noise / seeds (see STATE.md Deferred Items). Milestone audit: tech_debt — 15/15 requirements, 5/5 phases, 9/9 cross-phase seams, 1/1 E2E flow, 0 blockers (.planning/milestones/v1.12-MILESTONE-AUDIT.md).

**Phases completed:** 5 phases, 30 plans, 79 tasks

**Key accomplishments:**

- Every terminal job transition writes its `client_usage_daily` consumption row inside the SAME `pgx.Tx` as the guarded status change — one `INSERT...SELECT...ON CONFLICT` with correlated `sum(size_bytes)` subqueries and zero ordinal predicates, so bytes sum over ALL inputs/outputs (SEED-017-proof). Exactly-once proven `-race` against live Postgres, including a deliberate inversion run showing the test fails when the write moves out of the transaction, and a soak test that forces the reconciler's stale-recovery race for real and asserts `job_count = 1`, not merely that recovery happened.
- A cardinality-bounded `octoconv_client_job_outcomes_total` counter with an explicit numeric ceiling and a first-seen-first-served `overflow` collapse — added as a SEPARATE counter so `octoconv_job_duration_seconds` stays engine/status-only and `octoconv_queue_depth`'s label set stays exactly `{queue, state}`, pinned by tests that fail loudly on any future diff.
- A transaction-scoped Postgres admission gate that serializes the per-`(client, engine)` occupancy count with `INSERT INTO jobs` under a `pg_advisory_xact_lock`, enforced fail-closed on every job-creation path (REST + both MCP entry points, proven by integration test rather than assumed from proxying), rejecting with `429` + `Retry-After` + a machine-distinguishable `quota_exceeded` code, behind an observe-only default mode and a service-wide kill-switch.
- Daily byte/job quotas charge in-flight (queued+active) admitted work under the same lock — closing the one operator-reachable configuration (`max_concurrent = nil`) where the cap previously admitted every request until a job finished — and a canceled job now charges the ledger exactly once, closing the `create → cancel → create` evasion path.
- Executable retention, not documentary: a batched, advisory-lock-gated purge on the reconciler's existing tick deletes aged `jobs` rows (cascading to `job_events`/`job_inputs`/`job_outputs`/`webhook_deliveries`) and aged `client_usage_daily` rows on its own longer 400-day window, proven by tests that age data and assert the rows are actually gone.
- `GET /v1/system/usage` — one operator-gated response merging per-`(client, engine)` usage from the rollup with configured limits and a `remaining` figure that mirrors `evaluate()`'s admission arithmetic field-by-field, including idle unlimited clients as synthetic rows, with a byte-identical no-leak 404 proven against a live `/v1/system/presets` request and honest truth-claims that honor both the kill-switch and per-client observe mode.
- `minReplicaCount` templatized out of all 7 `scaledobject-*.yaml` into per-class Helm values through a validating helper that aborts the render on negative, over-max or non-integer input (the coercion bypass a code review caught after both SUMMARYs were written), defaulting to today's `0` for zero behavior change — closing SEED-011, and giving the chart its first CI coverage of any kind.
- QUOT-05 closed by a live gate, not by assertion: 10 rounds of 10 concurrent requests at `api.replicas: 2` admitted exactly K=2 and rejected exactly 8 with `quota_exceeded` every round, 0 `rate_limited`, both pods serving both outcomes — 167 assertions, evidence TSVs committed. The gate has a real history of catching genuine failures (a disproven host→ClusterIP reachability assumption, a legitimate-slot-reuse ambiguity at round 4/10) and closing them structurally: a heavy fixture, a self-checking spread bound, and a per-round Postgres precondition proving no admitted job went terminal inside the burst — never by relaxing `==` to `<=`.
- `internal/jobs/admission.go` now records the inverse of `internal/ratelimit`'s own caveat register: the quota mechanism carries NO `api.replicas: 1`-only caveat, structural argument first, live confirmation second, with the run's bounds (K=2, image engine, exactly 2 replicas) stated as bounds rather than implied as generality.

---

## v1.11 Pandoc Engine (Shipped: 2026-08-02)

Known deferred items at close: 5 categories (see STATE.md Deferred Items). Milestone audit: passed — 9/9 requirements, 4/4 phases, 6/6 cross-phase seams, 1/1 E2E flow (.planning/milestones/v1.11-MILESTONE-AUDIT.md).

**Phases completed:** 4 phases, 14 plans, 28 tasks

**Key accomplishments:**

- PandocOpts closed 5-field typed allowlist with strict-parse (checkStrictObject + DisallowUnknownFields), wrap enum-gated via map lookup, and a discrete-argv `pandocArgs` builder proven by an injection test that hostile crafted wrap strings are rejected at parse time, before ever reaching pandoc's argv.
- Fail-closed `LooksLikeLaTeX`/`LooksLikeText` content probes (UTF-8/NUL/marker discipline cloned from `LooksLikeHTML`) plus `enforcePandocInputSize`'s stat-then-reject DoS guard, and md/rst/org/latex/epub registered in the MIME/container registry — with the md/rst/org content-interchangeability limitation documented in-code as an accepted, recorded gap rather than silent behavior.
- Standalone `PandocConverter` (unregistered) converting across a curated closed markup allowlist via hardened `pandoc` 3.10.1 invocations (`--sandbox` + per-source raw-extension table, no network), with output validated by the Phase-47 sniffers and a live SC3 security canary proving shell-escape/`\input`/network-fetch cannot fire.
- Dedicated `pandoc` asynq queue + task type with a fresh-derived (never-copied) `PandocUniqueTTL`, a 2-entry own-schedule retry, and `EnqueuePandocConvert` -- a mechanical clone of the OCR Phase-44 queue/client machinery, standalone-compiling with zero api/worker/migration changes.
- Registered PandocConverter into convert.Default (zero new routing code), added migration 0010 admitting engine='pandoc', and wired EnginePandoc through the API's opts-dispatch/enqueue switches plus the latex/md/rst/org content-detection chain.
- HandlePandocConvert + isPandocTerminal (timeout+engine-error terminal, mirroring isDocumentTerminal) + cmd/pandoc-worker consuming the pandoc queue + reconciler recovery for stranded pandoc jobs — closes Phase 48 (PDCP-01).
- A buildable, SHA-256-pinned, TeX-free `pandoc-worker` container image running pandoc 3.10.1 as USER nobody, verified live for both amd64 and arm64.
- Authored scripts/pandoc-timing-measure.sh (p95 wall-sec/conversion gate over a 24-cell md/latex/html/docx x size x target corpus), added the pandoc-worker compose service with a placeholder 90s timeout, and propagated IN-02 PANDOC_ENGINE_TIMEOUT/PANDOC_MAX_RETRY parity across all 10 queue.NewClient()-constructing services.
- PANDOC_ENGINE_TIMEOUT finalized at 65s from a supervised p95 wall-sec/conversion measurement (worst cell html_large->md, p95=32.318s), propagated byte-identically across all 10 queue-client services, with a Path-B PANDOC_MAX_INPUT_BYTES lever fired for a memory-OOM finding distinct from the CAP.
- TestPandocConversionE2E proves md->html (text-out) + md->docx (zip-out) end-to-end against the real pandoc-worker container (presigned download + HMAC-signed webhook) with the measured PANDOC_ENGINE_TIMEOUT=65s — closing Phase 49's SC1.
- pandoc-worker Deployment + KEDA ScaledObject shipped into the Helm chart with FAST-engine tuning (null stabilization) derived from the measured 65s PANDOC_ENGINE_TIMEOUT, plus IN-02 ConfigMap env parity — helm template/lint green, SC2 confirmed unmodified.
- values-loadproof.yaml pandoc overlay exercising the falsy-0 guard (scaleDownStabilizationSeconds: 0) plus a values-driven Prometheus scrape interval, and a self-contained scripts/keda-pandoc-loadproof.sh SC3 scale-from-zero gate with run-time in-container markdown fixture synthesis (no committed testdata).
- scripts/keda-pandoc-downscale-survival.sh (a depth-arithmetic drive shape + /proc-based attribution SC4 harness, structurally distinct from the ocr clone source by design) plus committed offline calibration evidence (fixture_bytes=4094606, wall_seconds=18.276, safely inside [15,50]) proving the SC4 job window before any cluster time is spent.

---

## v1.10 OCR Engine (Shipped: 2026-07-27)

**Phases completed:** 4 phases, 15 plans, 35 tasks

**Key accomplishments:**

- OCROpts with a closed `eng`/`rus`/`eng+rus` language allowlist cloned verbatim from AudioOpts, strictly parsed via the shared `checkStrictObject` helper, with an injection test proving client bytes never reach tesseract's `-l` argv unvalidated — zero OSD/orientation or PSM/OEM references anywhere in the file per the phase's deferral decisions.
- PDF page-count cap + per-page page-box physical-size guard + image pixel cap + born-digital pdftotext detection, all fail-closed pre-render, in `internal/convert/ocrguards.go`
- OCRConverter (Pairs/Engine/Convert) extracts text from images via tesseract at pinned PSM 3/OEM 1, emits hOCR/ALTO with word-level bounding boxes, and defines the named `ocrPageMerger` encoding/xml multi-page merge helper Plan 04 will reuse -- all validated against real tesseract 5.5.2 + poppler binaries, standalone and unregistered.
- A scanned/image-only PDF is OCR'd exactly one page at a time (pdfinfo page-count cap + born-digital check before any render, then per-page page-box bound -> pdftoppm -> tesseract -> delete), multi-page hOCR/ALTO merges into one valid-XML document via the Plan 03 encoding/xml merger, and OCRConverter becomes the second `ProgressConverter` implementor with a page-granular percent+ETA ramp -- closing Phase 43's OCR-02 and OCR-04.
- Migration 0009 admits engine='ocr'; OCRConverter registered into convert.Default with matchPDF sniffing and a 100 MiB EngineOCR upload ceiling — zero new routing code, target-based EngineFor disambiguation alone routes jpg/pdf->txt to ocr vs jpg->png to image.
- Dedicated `ocr` asynq queue/task type with a fresh-derived OCRUniqueTTL (never copied from AV/Audio) and a client-level EnqueueOCRConvert, mechanically cloning the av engine-class wiring.
- HandleOCRConvert + isOCRTerminal/ocrFailureCode + cmd/ocr-worker + reconciler ocr routing — OCR jobs now flow queued -> active -> done through a dedicated asynq worker with stage-aware retry classification.
- Integration test proves OCR drives the unmodified v1.9 progressSink: single image jobs record null->100 (zero intermediate ticks), a 3-page scanned PDF records a genuine 33/66/100 page-by-page ramp, and `git diff --exit-code` confirms progress.go/sse.go carry zero edits across the whole phase.
- Source-built tag-pinned Tesseract 5.5.3 + Leptonica 1.87.0 Docker image (eng+rus tessdata_fast only, SHA-256 verified) plus cmd/ocr-worker re-registering a configured OCRConverter with cgroup-derived OMP_THREAD_LIMIT and concurrency=1
- Supervised p95-seconds-per-page measurement script cloning av-rtf-measure.sh, an ocr-worker compose service with a clearly-flagged PLACEHOLDER timeout, IN-02 OCR_ENGINE_TIMEOUT/OCR_MAX_RETRY parity across all 9 queue.NewClient() services, and an ocr-worker CI bake target -- no measured timeout number is committed.
- OCR_ENGINE_TIMEOUT derived from a real seconds-per-page measurement (worst cell dense_eng p95=5.81 s/page) at 744s, with the Path-B NO-GO lever lowering OCR_MAX_PAGES from 200 to 64 to clear the 900s reconciler CAP — propagated byte-identically across all 9 queue-client services.
- TestOCRConversionE2E proves image->txt + 20-page PDF->hocr end-to-end against the real ocr-worker container (presigned download + HMAC-signed webhook + live 0<progress_pct<100 ramp), closing the deferred Phase-44 SC4 — and surfaced three production bugs fixed inline.
- Authored the SC3 scale-from-zero and SC4 downscale-survival live-proof gate scripts for the OCR engine class as structural clones of the av Phase-37 harness, plus the keda.ocr load-proof overlay knob -- statically verified, ready for Plan 03's live OrbStack run.
- OCR class autoscaling live-proven on OrbStack k8s — scale-from-zero 0→1→0 (SC3, 11 assertions) and a 60-page OCR job surviving a genuine 2→1 downscale with graceful SIGTERM-before-completion / exit-0 / grace=774 (SC4, 22 assertions).

---

## v1.9 Job Progress & Control (Shipped: 2026-07-25)

**Phases completed:** 5 phases, 15 plans, 29 tasks

**Key accomplishments:**

- Additive `0007_job_progress.sql` migration adds four nullable jobs columns (progress_pct, eta_seconds, source_duration_seconds, progress_updated_at) with fail-closed CHECK constraints — verified on both fresh and populated Postgres with zero application behavior change
- DB-authoritative `POST /v1/jobs/{id}/cancel` backed by a guarded `{queued,active}->canceled` Postgres transition, with 404-not-403 ownership and always-200 idempotent/race-safe responses.
- New `internal/cancel` package: a mutex-guarded in-memory cancel-func registry plus a Redis pub/sub `job:cancel` Publisher/Subscriber, reusing the existing go-redis client with zero new dependencies.
- Wires the internal/cancel registry and Redis pub/sub (built in isolation by Plan 02) into the live worker/API processes: process() registers a per-attempt CancelFunc before every Convert call, all five engine handlers SkipRetry (never MarkFailed) on a context.Canceled kill, cmd/api publishes job:cancel, and every engine worker subscribes -- activating the low-latency in-flight kill mechanism end to end.
- Non-resurrection pinned by four explicit regression tests (FindStale-excludes-canceled, allow-list backstop, FindWebhookGaps-ignores-canceled, reconciler no-enqueue), plus an authored (not-yet-run) scripts/cancel-liveproof.sh that will prove CNCL-03's real ffmpeg-child death via the process table — Task 3 is an operator checkpoint, still PENDING.
- Client-scoped keyset-paginated `Repo.List` for job listing: one bound-parameter SQL query per page, an opaque base64 cursor codec, and a `JobSummary` row type — with a dedicated two-client leak test proving cross-client isolation at the SQL layer.
- `GET /v1/jobs` — an auth-scoped, fail-closed-validated, keyset-paginated listing endpoint that exposes Plan 01's `Repo.List` read layer with the exact `{jobs, next_cursor}` envelope (no `download_url`/`opts`, `error_code` only on failed).
- 1. [Rule 3 - Blocking issue] Docker image's ffmpeg has no `pipe` protocol; fixture captured via `-progress <file>` instead
- `runCommandStreaming` streams ffmpeg's `-progress` feed over a named FIFO on the allowed `file:` protocol (not the unavailable `pipe:` scheme), with a leak-safe reader proven at zero goroutines on both SIGKILL and a never-opened FIFO; `AVConverter.ConvertWithProgress` wires it into the transcode path only, ramping 0->100 monotonically while audio-extract/thumbnail/stream-copy stay null.
- A non-locking, monotonic `Repo.UpdateProgress`/`SetSourceDuration` write path that never contends the guarded status-transition lock or `job_events`, driven by a worker-side dual-gate throttle (>=2s AND >=1%) with a terminal-100 bypass and freeze-on-abort, surfaced null-aware on `GET /v1/jobs/{id}` — closing PROG-01 through PROG-04 end-to-end.
- Authored scripts/av-progress-liveproof.sh — a docker-compose-live operator gate that forces a real hevc@1080 ffmpeg re-encode ramp, kills it mid-stream, and hard-gates on zero orphaned processes, zero goroutine growth across 5 repeated cycles, and a frozen (non-null, non-100) progress_pct. Task 2 (the live run) was executed on the live docker-compose stack on 2026-07-25: 40/40 assertions PASS — ffmpeg PIDs (533/673/812/951/1090) confirmed gone from the av-worker process table across all 5 cycles, go_goroutines 22→25 (delta 3 ≤ 5 tolerance) proving zero reader-goroutine leak, a mid-flight cancel froze progress_pct at 1 (not null, not 100), and a completed transcode independently landed progress_pct=100. PROG-05 CLOSED.
- Added the `progress_webhook` per-job opt-in flag end-to-end: additive migration `0008_progress_webhook.sql` (boolean NOT NULL DEFAULT false), threaded through `jobs.CreateParams`/`jobs.Job`/`Repo.Create`/`Repo.Get`, and parsed at job create as a top-level form flag (NOT in opts) with an opt-in-without-callback = silent no-op stance. PUSH-02's delivery (42-03) builds on this; foundation only, no webhook fired yet.
- Added `GET /v1/jobs/{id}/events`: a DB-poll-sourced, single-goroutine SSE handler that mirrors `handleGetJob`'s auth + 404-ownership guard exactly, emits a named `progress` event only on change, heartbeats every 15s, and closes cleanly on terminal status, client disconnect, or a 30m safety cap -- zero goroutine leak by construction (no `go func()` anywhere in the handler).
- Adds an opt-in, HMAC-signed `progress` webhook event fired from a detached, single-attempt, own-timeout goroutine inside the Phase 41 progressSink — reusing the existing PROG-03 throttle decision and structurally incapable of touching the terminal `done`/`failed` bounded-retry/dead-letter queue — plus the additive `event` discriminator on both webhook bodies and cmd/av-worker's real `webhook.Deliverer` wiring.
- Authored scripts/sse-liveproof.sh -- a docker-compose-live operator gate that submits a forced hevc@1080 ffmpeg re-encode, opens GET /v1/jobs/{id}/events over a real socket, and hard-gates on Content-Type: text/event-stream, >=2 event: progress frames at distinct wall-clock timestamps before the stream's one terminal frame, >=1 `: keepalive` heartbeat, a clean close shortly after the terminal frame, and a cross-client 404. Task 2 (the live operator run) has NOT been executed -- PUSH-01 stays Pending until an operator runs the script against a live docker-compose stack and confirms the PASS summary.

---

## v1.8 AV Engine (video/ffmpeg) (Shipped: 2026-07-23)

**Phases completed:** 4 phases, 18 plans, 42 tasks

**Key accomplishments:**

- Fail-closed magic-bytes detection for mp4/mov/avi (fixed-offset ftyp/RIFF matchers) and mkv/webm (bounded-peek EBML/DocType walker) added to `internal/convert`, unit-tested against live-verified byte layouts, with a permanent brand-disjointness invariant against the existing image/audio sniffers.
- Closed AVOpts allowlist (timecode/resolution-height enum/HEVC codec) with distinct x264/x265 CRF constants, a new protocol-whitelisted ffprobe resolution guard, and hardening of the reused duration probe with `-protocol_whitelist file,crypto`.
- Standalone AVConverter shelling out to ffmpeg/ffprobe: mov/avi/mkv/webm->mp4 (H.264/AAC/+faststart) and mp4->webm (VP9/Opus) transcode with a codec-allowlist-gated stream-copy fast path, audio-extract to mp3/wav/m4a with AAC->m4a stream-copy, and input-side-seek thumbnail extraction to jpg/png/webp -- every ffmpeg/ffprobe invocation hardened with `-protocol_whitelist file,crypto`, live-verified against a real ffmpeg 8.1.2 binary, deliberately unregistered.
- Split AV's shared ffmpeg-stage error into three errors.Is-distinguishable sentinels, gave whisper.go a video-aware source set with a -map 0:a:0 stream pin and a raised pre-stage budget floor, and pinned AV/Audio pair disjointness with a regression test.
- Wires the `av` engine class into the queue layer (task type, queue name, task builder, D-03 retry schedule, unique-lock TTL, producer method) and replaces the API's hand-written queue-depth collector list with a derived `AllConvertQueues()` helper guarded by a completeness test (D-06).
- A stage-aware isAVTerminal classifier (transcode-timeout transient, extract/thumbnail-timeout terminal, D-02) and HandleAVConvert with a distinguishable four-way terminal error_code mapping (timecode_out_of_range/duration_exceeded/resolution_exceeded/no_video_stream), pinned by a test proving isAVTerminal genuinely disagrees with isAudioTerminal.
- Adds `EnqueueAVConvert` to the API's Enqueuer seam and its routing switch case, rewires `cmd/api/main.go`'s queue-depth collector to the derived `queue.AllConvertQueues()` helper (closing the phase's highest-risk silent seam), and implements D-07's two-tier upload ceiling — a 2 GiB global pre-parse bound plus a per-engine post-detection check that holds image/document/html/audio at their prior 100 MiB effective ceiling.
- 1. [Rule 1 - Bug] `TestSweepSkipsUnknownEngine` fixture used `Engine: "av"`, which stopped being unroutable after Task 1
- Registers AVConverter into convert.Default and wires SniffVideo into the upload detection chain in the same change (D-08) -- making video jobs reachable from outside for the first time -- plus the EngineAV opts-dispatch case and the third and final D-06 routing-completeness test.
- Task 1 — `cmd/av-worker/main.go` (commit `7055b7c`).
- Fail-closed `EnforceMinFreeDisk` disk-space guard (D-06) via `golang.org/x/sys/unix.Statfs`, plus an `AVConverter` struct-field refactor (D-09/Pitfall 4) that lets `cmd/av-worker` re-register a configured instance from `AV_MAX_DURATION_SECONDS`/`AV_DISK_SAFETY_FACTOR` at startup — no Docker, no measurement, pure-Go and fully `go test`-verified.
- From-source ffmpeg n8.1.2 av-worker image (CVE-2026-8461-clean, fail-loud pin guard) plus a VP9/HEVC/H264 x 480/720/1080/passthrough RTF measurement script, both live-verified against the full production argv suite
- 1. [Rule 3 - Blocking issue] audio-worker service missing from Task 2's IN-02 parity edit
- AV_ENGINE_TIMEOUT finalized at 753s (Path B lever, AV_MAX_DURATION_SECONDS 14400s->90s) from a measured hevc@1080 worst case (4.179133s p95 RTF, contradicting the plan's VP9-dominance assumption), plus a new fail-closed guard bounding the unbounded no-scale passthrough path to <=1080p to close a live hevc@2160p OOM-DoS vector the measurement discovered.
- Generalized the Phase 36 headline OOM-DoS fix from a no-scale-only, height-only guard to a fail-closed bound on every re-encode path (explicit resize included) checking both source Height (>1080) and Width (>1920), closing code-review findings CR-01 (CRITICAL) and HI-01 (HIGH) and correcting the docs' false "OOM DoS closed" claim.
- 1. [Rule 1 - Bug] Comment wording in configmap.yaml initially double-counted the grep-gated env-key tokens
- Two statically-verified live-proof gate scripts (scripts/keda-av-loadproof.sh for scale-from-zero, scripts/keda-av-downscale-survival.sh for N->N-1 downscale survival) plus a values-loadproof.yaml keda.av.scaleDownStabilizationSeconds:15 override, ready for Plan 03's live cluster execution.
- Rebuilding octoconv-api:dev from current HEAD (which already has QueueAV wired into AllConvertQueues()) resolved the prior BLOCKED run's STEP 6 failure outright, and Task 1 (SC3 scale-from-zero) passed clean on the first attempt; Task 2 (SC4 downscale-survival) then surfaced and fixed a second, independent live bug (an inherited WR-05-class jsonpath defect in the non-frozen keda-av-downscale-survival.sh) before passing all 22 assertions on retry, live-proving that a long hevc@1080 av transcode survives a genuine KEDA/HPA 2->1 downscale under production terminationGracePeriodSeconds=783 with a graceful exit 0.

---

## v1.7 Audio Engine & Hardening (Shipped: 2026-07-18)

**Phases completed:** 5 phases, 18 plans, 44 tasks

**Key accomplishments:**

- Flipped all three KEDA ScaledObjects to fail-safe null handling and retry-inclusive PromQL, replaced a would-be recursive Prometheus checksum with a shared named-template, and closed the falsy-0 stabilization bug with a paired hasKey/not-nil guard — all verified offline via helm lint/template, no cluster involved.
- Wired the missing `OPERATOR_CLIENT_IDS` compose passthrough and proved the operator system-presets path end-to-end against the live compose stack: operator CRUD on `/v1/system/presets`, a byte-identical non-operator no-leak 404, and cross-client system-preset job usability (61/61 acceptance assertions passing).
- Closed keda-load-proof.sh's five remaining gate-tooling warnings (stale-pod race, false-PASS download check, orphaned watcher, unpinned interpreter, CWD-relative fixture), added keda-gate.sh's presigned direct-dial step (HARD-04/D-07), and live-verified the whole gate end-to-end (21/21 PASS) after fixing a Rule-1 bug the WR-01 fallback change exposed in a fresh-install run.
- Local whisper-cli v1.9.1 toolchain provisioned and live-verified, plus a bespoke ID3v2-aware MP3 magic-bytes detector and an ffprobe-based declared-duration guard, both fail-closed and unit-tested in `internal/convert`.
- EngineAudio const + AudioOpts{Language, Translate} validated-opts layer with a closed 6-entry language allowlist and an injection test proving client bytes never reach whisper-cli argv
- AudioConverter's two-stage ffmpeg-normalize -> whisper-cli-transcribe pipeline, live-verified against the pinned whisper-cli v1.9.1 binary to emit segment- and token-level JSON timestamps, deliberately left unregistered pending Phase 31's API/queue wiring
- Postgres CHECK-constraint migration, AudioConverter registration, and full asynq queue/task-type/UniqueTTL/client layer that unblocks every downstream audio-engine plan in Wave 2
- Stage-aware terminal/transient classifier (Key Decision 1) wired into a new HandleAudioConvert handler, with the previously-dormant duration guard actually spliced into the pipeline and ffprobe/ffmpeg path args hardened with the `file:` protocol prefix.
- Audio content-detection, opts validation, and enqueue routing wired live into both the API request path and the reconciler's stranded-job recovery path, closing two confirmed integration bugs (12-byte upload truncation and opts mis-routing) before they could ship.
- cmd/audio-worker built, verified, and live-proven end-to-end: an uploaded jfk.wav job flowed queued → active → done in ~2.8s against real compose infra (Postgres/Redis/MinIO) with local ffmpeg + whisper-cli, producing the correct transcript ("...ask not what your country can do for you..."); .env.example documents all 5 audio env levers plus both Phase-31-deferred tradeoffs.
- Three-stage `Dockerfile.audio-worker` (Go build, whisper.cpp v1.9.1 source-built with `-DGGML_NATIVE=OFF`, slim ffmpeg runtime) built and verified locally on arm64 (682MB), with the mandatory live cgroup v2 `cpu.max` spot-check confirming `200000 100000` under `--cpus=2.0` (Assumption A2 resolved).
- whisper-cli's `--threads` is now sized to the container's real cgroup v2 CPU quota (floor of quota/period, never host core count), resolved once at `cmd/audio-worker` startup via an `AUDIO_THREADS` env override → `CgroupCPULimit()` → `runtime.NumCPU()` precedence chain, and always injected as an explicit `-t <n>` argv pair on every whisper-cli invocation.
- `scripts/audio-rtf-measure.sh` measured p95 RTF=0.2059 (N=10, arm64, base model, 2-cpu container) and the NO-GO lever fired: AUDIO_MAX_DURATION_SECONDS is lowered from the placeholder 14400s (4h) to 1800s (30min), yielding a derived AUDIO_ENGINE_TIMEOUT=742s (12.4min, 17.6% margin under the asserted 900s/15m CAP) and a measured AUDIO_WORKER_CONCURRENCY=1.
- Added the audio-worker compose service with RTF-measured values (742s timeout, concurrency=1, cpus=2.0/memory=1g), propagated AUDIO_ENGINE_TIMEOUT/AUDIO_MAX_RETRY identically across all 7 queue.NewClient()-constructing services (IN-02), corrected the stale Phase-16 5m reconciler-CAP override on both webhook-workers to 15m (IN-16), added audio-worker to the CI bake matrix, and replaced .env.example's [ASSUMED] placeholder with the measured values.
- TestAudioConversionE2E passes live against the containerized audio-worker (docker compose), closing AUD-06's live-acceptance gate with an 8.10s observed job wall-clock — far inside the 5-minute cold-start bound and negligible against the CI e2e suite's 30-minute timeout.
- 1. [Rule 1 - Bug] Reworded two inline comments to avoid literal grep-gate string collisions
- New self-contained scripts/keda-audio-loadproof.sh: structural clone of scripts/keda-load-proof.sh, narrower (no trial-run mode) and with new scope (timestamped kubectl pod-event-timeline capture for the audio class's scale-from-zero proof), statically verified — the two frozen sibling scripts remain byte-unchanged.
- Live-ran both KEDA load-proof gates on OrbStack: keda-audio-loadproof.sh cleanly proved audio 0->1 scale-from-zero with the whisper model baked in (SC3, AUD-08 complete); the unmodified keda-load-proof.sh re-run newly confirmed SC1/SC2 pass under the WR-01-hardened chart but also empirically surfaced a real, previously-only-theoretical WR-05 jsonpath defect in SC3's BUSY_POD selection (fails loud, as 29-REVIEW.md predicted) -- Phase 29's deferred item is re-verified, not cleanly closed.

---

## v1.6 Kubernetes & KEDA (Shipped: 2026-07-17)

**Known deferred items at close:** 3 (stale v1.3-era quick-task record + 2 dormant seeds, SEED-004 substantively delivered — see STATE.md Deferred Items)

**Phases completed:** 5 phases, 14 plans, 33 tasks

**Key accomplishments:**

- Single flat Helm chart (`deploy/chart/octoconv`) with a complete interface-first `values.yaml`, dev-cred `values-local.yaml`, shared config/secret templates carrying the FQDN `S3_ENDPOINT`/`0.0.0.0` `METRICS_ADDR` landmine fixes, and hand-rolled Postgres/Redis/MinIO Deployments — lint-clean and accepted by a live OrbStack `kubectl apply --dry-run=server`.
- Five app Deployments (api + 4 engine-class workers) with dependency-aware probes and per-class grace periods, the atomic metrics-bind NetworkPolicy closure, chromium's /dev/shm memory mount, fixed 2-replica webhook-worker, and an idempotent createbucket post-install hook Job — zero source-tree changes, no migrate hook (api self-migrates per D-05 refinement).
- 1. [Rule 3 - Blocking] `--wait` + post-install hook chicken-egg
- Streamable-HTTP MCP endpoint with per-request caller-key pass-through, presigned-only remote results, and an unconditional session-key-binding hijack guard — after a live spike proved go-sdk v1.6.1 stateless mode delivers in-flight progress notifications.
- Dockerfile.mcp-http + gated single-replica Deployment/ClusterIP Service/NetworkPolicy for cmd/mcp-http, wired with key-free env (OCTOCONV_BASE_URL + MCP_HTTP_ADDR only) and verified entirely offline (helm lint, template gating, server dry-run).
- D-08 live hard gate PASSED: a real streamable-HTTP MCP session against a chart-deployed mcp-http pod completed initialize, tools/list=5, a real png→jpg conversion with a per-request minted caller key returning a presigned-only result (no local_path), a 401-without-key rejection, and a bonus session-hijack 403 — plus Phase 24's deferred SC3 presigned-from-host recheck, which again required the `--connect-to`-equivalent fallback path (OrbStack proxy wedge, same as 24-03).
- Operator-only REST CRUD for system-scope presets under `/v1/system/presets`, gated by an `OPERATOR_CLIENT_IDS` env allowlist that fails closed when empty and fails loud when malformed, reusing the existing scope-agnostic `PresetAdmin` interface unchanged.
- Fixed `presets.Repo.Create` to compute the next version via `COALESCE(MAX(version),0)+1` across active AND inactive rows, closing the "deactivate a preset then it's permanently unusable" 500 bug for both system and user scope, with a `pgconn` 23505 backstop mapping any residual race to `ErrAlreadyExists` (409).
- Moved `octoconv_queue_depth` from the four worker binaries to the always-on api process (single `prometheus.MustRegister` call for all four queues) and set per-class `asynq.Config.ShutdownTimeout` on all four workers, both live-verified against the full compose stack.
- Three per-class KEDA ScaledObjects (image/document/html) scaling from zero on an in-chart Prometheus's `octoconv_queue_depth` signal, with both landmine gaps from RESEARCH.md (NetworkPolicy monitoring-namespace mismatch, missing api :9090 Service port) closed in the same plan.
- Authored and executed `scripts/keda-gate.sh` on OrbStack k8s: KEDA v2.20.1 installed live, the full D-12 proof passed 18/18 assertions (metric resolves at genuinely 0 replicas, all three classes scale 0→1 from one real job each, image cycles back to 0, webhook-worker fixed at 2 throughout), and teardown left the cluster completely clean — human-verify checkpoint approved.
- Field-level `spec.replicas` omission on the three KEDA-scaled Deployments plus a values-gated document-class HPA scaleDown stabilization override, both production-inert by default and both machine-verified via `helm template` — the chart substrate the live load-proof gate (28-02/28-03) depends on.
- Three new tools -- a calibratable python-docx heavy-fixture generator, a headless matplotlib CSV-to-PNG dual-axis renderer, and a self-contained 854-line `keda-load-proof.sh` gate implementing the SC1/SC2 image-burst 0->N->0 scenario and the SC3 document-class downscale-soak with deterministic pod-deletion-cost victim selection and the D-09 triple-check -- all statically verified (bash -n, uv smoke runs) and ready for the live in-cluster run in plan 28-03.
- A single ALL-27-ASSERTIONS-PASSED live gate run against the real OrbStack cluster proving the full 0→4→0 image-class burst cycle (first replica +6s, peak 4 = maxReplicaCount at +11s, drain +76s, scale-to-zero +136s) and a 178s document conversion gracefully surviving a genuine KEDA/HPA 2→1 downscale (SIGTERM 142.8s before job completion, Completed/exit-0 with 188s of the 330s grace window unused) — all captured as committed, credential-free, timestamped evidence.

---

## v1.5 MCP Access & Document Fidelity (Shipped: 2026-07-13)

**Phases completed:** 4 phases, 10 plans, 22 tasks

**Key accomplishments:**

- Authenticated REST self-service for client-scope presets (create/list/show/update/deactivate) plus a registry-derived GET /v1/formats capability endpoint, both mounted inside the existing /v1 auth+rate-limit chain.
- curl-driven live gate (scripts/presets-rest-acceptance.sh) proving all five /v1/presets verbs, mass-assignment resistance, byte-identical no-leak 404s, and registry-derived GET /v1/formats against the real compose stack — 42/42 assertions passed on first run
- Hand-rolled, zero-internal-import HTTP client of the OctoConv public API implementing the blocking convert workflow (multipart upload -> poll -> presigned download), with API-key redaction, output-path containment, and an optional Host-preserving dial-redirect knob for presigned downloads -- all proven by offline httptest unit tests.
- Five agent-facing MCP tools (convert_file/get_job_status/download_result/list_supported_formats/list_presets) registered on the pinned go-sdk v1.6.1, with per-tick NotifyProgress during blocking conversion and upstream API failures surfaced as isError tool results — all proven against a real in-memory MCP session, not just faked handler calls.
- cmd/mcp-server ships as a thin, fail-fast, stderr-only-logging stdio binary; README documents client wiring; and the D-13 live stdio JSON-RPC hard gate PASSED against the real compose stack -- five tools, a real png→jpg conversion (presigned_url + local JPEG-magic file via the child binary's OCTOCONV_S3_DIAL_ADDR dial-redirect), list round-trips, bad-input isError, and full stdout purity.
- Hand-rolled, bounded, fuzz-hardened CFB directory-entry-name parser (`ClassifyCFB`) distinguishing encrypted vs. legacy-binary Office uploads, zero new dependencies, proven crash-free over a 3.5M-execution 30s native fuzz run.
- handleCreateJob now returns a distinct "remove the password" 422 for encrypted OOXML and a distinct "legacy binary ... convert to docx/xlsx/pptx" 422 for legacy .doc/.xls/.ppt, via `convert.ClassifyCFB`, proven live end-to-end against the real fixtures through the rebuilt compose stack.
- veraPDF CLI bundled into Dockerfile.document-worker via a Debian-JRE fallback path (jlink JRE fails the glibc boundary live); measured JVM cold-start p95 = 4650ms over 10 real PDF/A-2b validations -- GO, 2.15x margin under the 10s D-01 budget.
- ValidatePDFA wired into validateDocumentOutput's wantPDFA branch (after the /GTS_PDFA pre-filter) via the existing hardened runCommand, extended to capture stdout since veraPDF's `--format xml` report is stdout-only; fail-closed terminal classification (terminalVeraPDFSignatures) lands in the same commit, and VERAPDF_TIMEOUT is injected from cmd/document-worker/main.go via SetVeraPDFTimeout.
- Live, unconditional proof against the rebuilt document-worker image (real veraPDF JVM in the path): a genuine PDF/A-2b export still reaches done (no regression), and a marker-bearing-but-corrupted PDF/A export injected via an e2e-only soffice shim fails terminally with the exact "pdf/a non-compliant" veraPDF reason recorded in job_events -- all three live tests (TestPDFAExportE2E, TestPDFANonCompliantE2E, TestDocumentConversionE2E) passed in a single 43.16s run; document-worker image build-time delta measured at +12.13s (+8%) via two controlled --no-cache local builds.

---

## v1.4 CI, Presets & Debt Cleanup (Shipped: 2026-07-12)

**Phases completed:** 3 phases, 8 plans, 15 tasks

**Key accomplishments:**

- Removed dead webhook.NewRepo/NewDeliverer/WEBHOOK_SIGNING_SECRET wiring from cmd/document-worker and cmd/chromium-worker (mirroring cmd/worker's nil-safe pattern), and made fakeEnqueuer's call counters mutex-guarded so `go test ./internal/reconciler/... -race` runs the soak test clean against a live Postgres.
- Added TestImageConversionE2E (png->jpg via libvips) to internal/e2e, closing the last gap in the E2E format matrix — live-verified PASS against a real docker-compose stack with HMAC webhook confirmation.
- New `internal/presets` package with SQL-only scope-precedence preset resolution (shadowing + no-leak), bump-on-update versioning, and jobs.preset_name/preset_version provenance wiring — all backed by 11 DB-gated tests against live Postgres.
- Operator CLI (`cmd/manage-presets`) for create/update/list/show/deactivate of system- and client-scoped presets, backed by a new `internal/presets.ValidateOptsJSON` helper that fail-early-rejects opts matching neither the document nor HTML print allowlist schema (D-11).
- POST /v1/jobs now accepts `preset=<name>` which resolves through a narrow PresetRepo interface to target_format+opts, enforcing mutual exclusivity with explicit target/opts, a single non-leaking 422 for any resolution miss, full re-validation of stored opts through the existing engine-keyed parsers, and a pre-insert TOCTOU re-check that rejects a preset deactivated in the resolve-to-create window.
- scripts/presets-acceptance.sh — a 33-assertion live hard gate proving all five manage-presets CLI verbs plus preset resolution/provenance/shadowing/no-leak/re-validation against the real compose stack, passing twice in a row (fresh run + idempotent re-run) with a rebuilt api image.
- Authored `.github/workflows/ci.yml` — OctoConv's first-ever CI workflow, a 4-tier needs-chained pipeline (gate → race → docker-build → e2e) covering CI-01..CI-04, with every tier's exact commands locally hard-gated green before any push.
- Executed inline by the orchestrator

---

## v1.3 Document Class v2 (Shipped: 2026-07-12)

**Phases completed:** 5 phases, 17 plans, 44 tasks

**Key accomplishments:**

- Closed all 5 inherited advisory tech-debt items (v1.0 docker-compose audit + v1.2 WR-02/WR-03/WR-04 + gofmt nit) with zero new features, before v1.3 engine work begins.
- LibreOfficeConverter now handles docx<->odt, xlsx<->ods, pptx<->odp via an explicit (source,target) filter table, with non-PDF output validated by the same convert.SniffContainer that guards upload input, coupled same-commit with the worker's terminal-error classifier.
- Uploads beginning with the OLE-CFB magic byte header (legacy binary .doc/.xls/.ppt or password-protected OOXML) now get an immediate 422 before touching S3/Postgres, via a standalone `convert.IsOLECFB` detector wired as a third fail-closed branch in `handleCreateJob`.
- Extended `internal/e2e` with a 6-pair cross-format table (SniffContainer-verified) and a 2-fixture OLE-CFB-rejection table, then ran the full suite live against a freshly built docker-compose stack -- everything passed on the first run, including the existing 6 `->pdf` pairs and the webhook path, confirming CONV-01, CONV-02, and SAFE-01 end to end.
- Wired the already-existing, previously-inert `jobs.options jsonb` column into a live round-trip via `Job.Opts` / `CreateParams.Opts` (`map[string]any`), with no schema migration.
- Closed DocOpts contract with strict allow-list parsing, a PDF/A-2b filter-options builder assembled entirely from server constants, and a worker-side OutputIntent check that fails a mis-tagged PDF/A export terminally — the injection unit test proves adversarial client bytes never reach the soffice argv.
- API now parses/validates/persists/echoes the opts form field end to end (fail-closed 422 before storage), and a live docker-compose run confirmed a real PDF/A-2b export carrying `/Type/OutputIntent/S/GTS_PDFA1` on LibreOffice 7.4 -- no code corrections needed. Checkpoint APPROVED by the user (2026-07-11); plan complete.
- Third (html) engine class fully declared end-to-end — CHECK migration, EngineHTML const, htm-alias, dedicated asynq queue/retry/TTL, producer method, and reconciler routing — mirroring the document engine 1:1, with no converter or worker wired yet.
- Registered `{html→pdf}` ChromiumConverter with server-constant CSS `@page` print-option injection (never CLI flags) and layered network-block chromium argv, backed by a fail-closed HTML content sniff and a closed-strict print-opts struct mirroring Phase 14's PDFAFilterOptions invariant.
- HandleHTMLConvert + isHTMLTerminal wired into the worker (terminal-classified HTML_ENGINE_TIMEOUT, mirroring HandleDocumentConvert exactly), and handleCreateJob now fail-closed-detects HTML content, validates+persists print opts via an engine-keyed dispatch, and routes html jobs to their own asynq queue -- the entire HTML-01/02/03 Go code path is now present, pending only the chromium-worker container (Plan 04).
- Third engine-class container (chromium-worker) built and live-tested against the real chromium-headless-shell 150.0.7871.100 binary; two load-bearing RESEARCH.md assumptions (JS-disable via launch flag, print_background via CSS hint) were found broken and corrected in place with live-verified working mechanisms — checkpoint approved by user 2026-07-11.
- All four Phase 15 success criteria (html→pdf end-to-end, network-block canary with zero hits across external/loopback/compose-host/file:// vectors, page_size/print_background opts round-trip, and HTML_ENGINE_TIMEOUT terminal classification) live-verified against a freshly built docker-compose stack — awaiting human checkpoint sign-off, not self-approved.
- Postgres session-level advisory-lock (`pg_try_advisory_lock` on a dedicated `pgxpool.Conn`) added to `internal/reconciler` so exactly one webhook-worker replica sweeps at a time, fail-safe-closed on any lock-check error.
- New `cmd/webhook-worker` binary (trimmed from `cmd/worker`, wired with storage for PresignGet) consumes `TypeWebhookDeliver` and runs the reconciler sweeper under the Plan 16-01 Postgres advisory lock; `cmd/worker` is demoted to image-only with the webhook role and sweeper fully removed.
- Two named webhook-worker services (webhook-worker-1/-2, Dockerfile.webhook-worker) wired into docker-compose.yml with full storage+webhook+reconciler env, image/document/chromium workers stripped of webhook env, E2E host-gateway reachability moved to the new services, and .env.example documenting the webhook-worker-only config surface.
- All three Phase 16 success criteria live-verified against a freshly built two-webhook-worker stack and human-approved: webhook delivery survives image-worker absence (SC1), survives a mid-delivery consumer kill with zero lost/zero duplicated delivered webhooks (SC2), and exactly one fleet-wide advisory-lock sweeper with ~11s auto-failover (SC3). WEBH-01 satisfied.
- Verified CR-01/WR-01 pool-slot-leak and shutdown-blocking fixes already landed by a parallel quick-task, and closed the one remaining test gap with a new -race regression test for the TryAcquire/Close mutex interleaving.

---

## v1.2 Document Engine Class (Shipped: 2026-07-10)

**Phases completed:** 4 phases (8–11), 12 plans (incl. gap-closure 11-04), 26 tasks
**Audit:** passed — 10/10 requirements, 10/10 integration links, 1/1 E2E flow (`.planning/milestones/v1.2-MILESTONE-AUDIT.md`)
**Known deferred items at close:** 12 (6 newly acknowledged at v1.2 close: WR-02/03/04 advisory review findings, gofmt nit, 2 dormant seeds — see STATE.md Deferred Items)

**Key accomplishments:**

- Stdlib-only `SniffContainer` disambiguates all 6 ZIP-based office formats via root-part-name/mimetype structural checks, sums declared uncompressed size for a zip-bomb guard, and flags macro-carrying/duplicate-named parts in one `archive/zip` central-directory pass — plus a `HasDimensionLimit` predicate fixing the confirmed image-only dimension-check regression.
- `handleCreateJob` now structurally detects the six ZIP-based office formats via `convert.SniffContainer`, rejects zip-bomb-shaped and macro-carrying uploads with 422 before any storage write, and the confirmed image-only dimension-check regression is fixed with a `HasDimensionLimit` guard — all threaded through a new configurable `MAX_DOCUMENT_UNCOMPRESSED_BYTES` limit (500 MiB default).
- LibreOfficeConverter shells out to soffice headless for docx/xlsx/pptx/odt/ods/odp to PDF, self-isolates its LibreOffice profile per job, and validates output (size + %PDF- magic bytes) before returning success; registered in convert.Default and covered by unit tests plus a soffice-gated live process-kill proof test.
- Provisioned LibreOffice + fonts into Dockerfile.worker, built a repeatable Dockerfile.worker-test harness, and proved DOC-06's zero-survivors process-group-kill guarantee via a live-executed (not skipped) integration test — discovering and fixing a real zombie-process reaping gap (tini added as PID 1) along the way.
- Document conversion tasks now route to a dedicated `document` asynq queue with a genuinely-derived (not hardcoded) per-job unique-lock TTL and a no-jitter 5s/15s/30s retry schedule, mirroring the existing image-queue pattern exactly.
- Reconciler sweep now dispatches stranded-job recovery by `jobs.engine` (image -> image queue, document -> document queue) with a fail-closed, metric-visible skip for any other engine value, closing the launch-blocking image-only hardcode (DOC-09).
- HandleDocumentConvert task handler plus a standalone cmd/document-worker binary that consumes only the document asynq queue, classifying a DOCUMENT_ENGINE_TIMEOUT expiry as terminal via an engine-scoped isDocumentTerminal while leaving the image engine's timeout-as-transient isTerminal untouched.
- Reverted Dockerfile.worker to libvips-only and created Dockerfile.document-worker carrying LibreOffice + tini-as-PID-1, correcting Phase 9's pragmatic same-image compromise; added a document-worker compose service with its own concurrency/timeout and identical D-02 resource limits, and documented all three new env vars in .env.example.
- handleCreateJob now derives the job's engine class from content-detected format via a new Converter.Engine()/Registry.EngineFor contract, routing document uploads to the document queue (Engine="document") and image uploads to the image queue, replacing the last hardcoded image-only assumption in the request path.
- Env-gated `internal/e2e` suite drives all 6 document pairs (docx/xlsx/pptx/odt/ods/odp -> pdf) over real HTTP against a live compose stack — upload, poll, presigned `%PDF-` download check, plus a fully HMAC-verified webhook assertion on the docx pair — backed by soffice-verified fixtures and an E2E-only compose override.
- All 6 document format pairs (docx/xlsx/pptx/odt/ods/odp to pdf) converted successfully through a freshly built, live docker-compose stack (api + worker + document-worker + postgres/redis/minio), including a fully HMAC-verified signed webhook delivery for the docx pair — `go test ./internal/e2e/ -run E2E -v` ran (not skipped) and passed in 15.3s.
- Extended `convert.MIMEType` with the six document formats and `pdf`, closing the last gap in DOC-10 so document job uploads/downloads are served with the correct MIME type instead of `application/octet-stream`, exactly like image jobs.

---

## v1.1 Tech Debt Cleanup (Shipped: 2026-07-08)

**Phases completed:** 3 phases, 7 plans, 13 tasks

**Key accomplishments:**

- Added `WEBHOOK_ALLOW_PRIVATE_IPS` operator opt-in that narrowly relaxes only the RFC1918 check inside `isBlockedIP`, with a startup warning and both-sides test coverage, while loopback/link-local/unspecified stay hard-blocked.
- Derived, jitter-corrected `WebhookUniqueTTL` (2477.5s for MaxRetry=6/10s) wired into `asynq.Unique` on the webhook delivery task, closing the duplicate-enqueue race RECON-04's gap sweep depends on
- `FindWebhookGaps` NOT EXISTS anti-join detects done/failed jobs with a silently-dropped webhook enqueue, with `RecordWebhookGapRecovered` logging recovery without a fake status transition
- `Sweeper.sweep()` gains a second enqueue-first scan over `FindWebhookGaps`, combining Plan 01's `asynq.Unique` lock and Plan 02's gap-finder into the working RECON-04 behavior
- Two real-wall-clock integration tests (`TestSoakRecoversStrandedQueuedJob`, `TestSoakExhaustsAtCap`) prove Phase 3's staleness recovery and cap-exhaustion behavior under genuine elapsed time, using a live Postgres `jobs.Repo` paired with the existing in-memory `fakeEnqueuer`, completing in under 4 seconds combined
- `convert.Dimensions()` — hand-written binary parsers for PNG/JPEG/WebP/TIFF/HEIC extracting declared pixel width/height from a bounded 64 KiB non-seekable stream prefix, with zero new dependencies and full byte-fixture test coverage.
- MAX_IMAGE_PIXELS config/env wiring plus a handleCreateJob gate that calls convert.Dimensions between the format pair-check and callback_url validation, rejecting 422 before any storage write when declared pixel dimensions are unknown or exceed the configurable 100-megapixel default.

---

## v1.0 Hardening MVP (Shipped: 2026-07-08)

**Phases completed:** 4 phases, 15 plans, 36 tasks

**Key accomplishments:**

- Salted-SHA-256 client API key issuance: `0002` migration with dual-slot key columns, `internal/auth` hash helpers, `internal/clients` repository, and a `manage-clients` operator CLI supporting create/add-key/revoke.
- chi middleware turning issued API keys into hard-cutover 401 enforcement on `/v1/*`, with `client_id` threaded through job creation and a 404-only (never 403) cross-client ownership guard on job reads.
- In-process `go-chi/httprate` middleware (`internal/ratelimit`) with a coarse pre-auth IP flood guard and a per-client fair-use limiter keyed on the authenticated `client_id`, wired into `/v1` as `ByIP -> auth -> PerClient` with env-configurable 60/120 rpm defaults.
- Fixed two verified-but-unfixed gaps from `01-VERIFICATION.md`: jobs integration tests violating the new `jobs_client_id_fkey` when run against a live Postgres, and the pre-auth `ratelimit.ByIP` guard being fully bypassable via spoofed `X-Forwarded-For` because of chi's deprecated `middleware.RealIP`.
- POST /v1/jobs now accepts a per-job `callback_url`, rejecting SSRF targets (loopback/RFC1918/link-local/metadata) and non-https schemes with a fixed 400 before any storage write, and persists/reads it through Postgres via the existing nullable-column idiom.
- HMAC-SHA256 payload signing, a Postgres delivery-attempt repository with dead-lettering, and a single-attempt HTTPS deliverer (2xx-only, 10s timeout), each independently unit/integration tested.
- Completing jobs with a callback_url now trigger a signed, retried, tracked webhook end-to-end: `webhook:deliver` enqueued after MarkDone/MarkFailed, delivered with a freshly-presigned URL per attempt, retried by asynq with bounded exponential backoff + jitter, and dead-lettered after 6 exhausted retries.
- Image conversion tasks now retry on their own fast 2s/5s/15s schedule with a bounded MaxRetry (default 4) via a queue-aware RetryDelayFunc dispatcher, and carry a per-job asynq.Unique lock whose TTL is derived from IMAGE_MAX_RETRY + ENGINE_TIMEOUT so duplicate enqueues collide safely instead of double-processing.
- Worker now distinguishes transient from terminal image-conversion failures via a pure `isTerminal(err)` classifier, `MarkActive` is idempotent for asynq's same-task retries, raw vips stderr no longer reaches `error_message`, and a single whole-attempt timeout bounds download+convert+upload+record so no attempt can outlive the asynq unique-lock TTL.
- A ticker-driven reconciler now sweeps Postgres every minute for jobs stranded in `queued`/`active` past a staleness threshold, requeues genuinely-stranded ones through an enqueue-first, `asynq.ErrDuplicateTask`-guarded recovery path (never duplicating a still-live task or falsely inflating a backlogged job's recovery count), and terminally fails jobs that exceed a bounded recovery cap with a webhook fired on exhaustion.
- Magic-byte content sniffing (hardcoded 5-format signature table) gates `handleCreateJob` before any pair-check or S3 write, rejecting declared/detected mismatches and unrecognized content with a detailed 422 and a client-scoped log line.
- MinIO ILM lifecycle rule (7-day default TTL on uploads/ and results/) applied declaratively via minio-go's SetBucketLifecycle at API startup, plus a read-only storage.Ping probe for the future health endpoint.
- Defined four Prometheus metric families (job outcomes, job duration, webhook deliveries, reconciler actions) plus a pull-based queue-depth collector in a new `internal/metrics` package, and instrumented the existing worker/reconciler terminal exit points to call them — closing the instrumentation half of OBS-01.
- GET /healthz now pings Postgres, Redis, and S3/MinIO under a shared 3s timeout, returning 200/ok when all reachable and 503/degraded with per-dependency detail otherwise.
- Second localhost-only `/metrics` HTTP listener mounted in both `cmd/api/main.go` and `cmd/worker/main.go` (promhttp.Handler(), METRICS_ADDR default 127.0.0.1:9090), queue-depth collector registered in the worker, and a pinned `hibiken/asynqmon:0.7.2` dashboard service bound to 127.0.0.1:8980 — all three verified live end-to-end (real conversion job, real metrics scrape, real dashboard query).

---
