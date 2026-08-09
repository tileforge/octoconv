# Pitfalls Research

**Domain:** Adding inspect/sanitize/operation-dimension/large-handler-refactor to an
existing production-hardened Go async file-conversion service (OctoConv v1.13 "Inspect &
Operations")
**Researched:** 2026-08-07
**Confidence:** MEDIUM-HIGH — codebase claims verified directly against
`internal/worker/worker.go`, `internal/api/handlers.go`, `internal/convert/ocrguards.go`,
`internal/metrics/metrics.go`, `.planning/PROJECT.md`, `.planning/seeds/SEED-020.md`,
`SEED-013.md`, `SEED-021.md`, and `v1.12-MILESTONE-AUDIT.md`; sanitization security claims
verified against public CVEs, the PDF spec's incremental-update mechanism, OOXML/OPC package
semantics, and CDR vendor documentation (see Sources) — LOW confidence only where explicitly
flagged.

## Critical Pitfalls

### Pitfall 1: The synchronous `/v1/inspect` handler starves `/healthz` and cascades a fleet-wide outage

**What goes wrong:**
`handleHealth` and the future `handleInspect` both live in the same `net/http` server
process (`cmd/api/main.go`), sharing its goroutine pool and CPU allocation. Every other
safety property in this codebase — engine timeouts, `Setpgid`+SIGKILL, per-engine
concurrency caps — exists because analysis-on-untrusted-input is assumed to happen in a
worker with its own cgroup (`docker-compose.yml`: worker `cpus: "2.0"`, `memory: 1g`,
isolated per engine class). `/v1/inspect` is the first code path that puts real parsing of
adversarial bytes (magic-byte sniff is cheap; page/frame/stream analysis for "pages,
duration, resolution, codecs, risks" is not) directly on the same process that serves
`/healthz`, which k8s liveness probes and (transitively, via pod restarts) the whole fleet
depend on. A handful of concurrent pathological inspect requests (deeply nested PDF object
graphs, a crafted OOXML with a huge but not-yet-bomb-classified zip central directory, a
media file with a degenerate container) can consume enough CPU/goroutines to make
`/healthz` itself slow or unresponsive — which is not "that one client gets a slow
response," it is "the liveness probe fails, k8s restarts the API pod, and every client's
`POST /v1/jobs` and quota-gate traffic drops with it."

**Why it happens:**
Every existing synchronous handler in `internal/api/handlers.go` does bounded, cheap work
(multipart parse capped by `MaxBytesReader`, magic-byte sniff of a fixed prefix, declared
image-dimension parse without decode) specifically so it can stay synchronous safely. Inspect
is different in kind, not degree — it is being asked to do real content analysis, and the
milestone spec itself concedes this ("границы обязаны быть явными требованиями, а не
допущением: потолок работы + ограниченная выборка страниц"). The risk is that "explicit
bound" gets implemented as a per-analysis-step timeout (easy, familiar) rather than as a
process-wide isolation guarantee (hard, unfamiliar to this codebase, which has never needed
it before).

**How to avoid:**
- Treat `/v1/inspect` work as requiring the SAME `runCommand` hardening discipline
  (`internal/convert/exec.go`, Setpgid + process-group SIGKILL) as every worker engine
  invocation — never ad-hoc `os/exec` or in-process parsing libraries without an explicit
  wall-clock bound tied to `r.Context()`.
- Bound total inspect work with a dedicated, small, measured timeout — do not reuse or
  guess from any `*_ENGINE_TIMEOUT` (this project's own anti-pattern, restated seven times in
  Key Decisions: never copy a sibling's timeout).
- Consider a bounded worker-pool / semaphore specifically for inspect (distinct from the
  general `net/http` goroutine-per-request model) so a burst of inspect requests cannot
  starve the health-check goroutine or the quota-admission code path, both of which share
  the process.
- Live-gate this the same way KEDA scale-from-zero was live-gated: fire a burst of
  adversarial-shaped inspect requests concurrently and assert `/healthz` latency and the
  liveness-probe SLA hold, not just that inspect itself returns a bounded response.

**Warning signs:**
`/healthz` p99 latency degrading under inspect load in staging; k8s pod restarts correlated
with inspect traffic; `POST /v1/jobs` (which shares the process) showing latency spikes with
no corresponding change in Postgres/S3/Redis load.

**Phase to address:**
The synchronous `POST /v1/inspect` phase — this must be a named success criterion (with a
live gate, following this project's own precedent), not an assumption folded into "add the
endpoint."

---

### Pitfall 2: The JobRunner refactor collapses per-engine timeout classification onto a single axis, silently destroying stage-based asymmetries

**What goes wrong:**
`internal/worker/worker.go` currently has seven bespoke terminal-error classifiers, and at
least two of them are NOT expressible as "is a timeout terminal for this engine: yes/no."
`isAudioTerminal` (worker.go:293-338) treats a timeout during the ffmpeg-normalize stage as
terminal but a timeout during the whisper-cli-transcribe stage as transient — a stage
distinction WITHIN one engine. `isAVTerminal` (worker.go:355-389) goes further: a timeout
during the expensive transcode stage is transient, but ANY failure (timeout or not) during
the cheap audio-extract/thumbnail stages is terminal — the opposite polarity from audio's
own rule, deliberately, per the codebase's own comment: "porting audio's rule verbatim would
... silently destroy the transient-retry behavior this classifier exists to provide." If
SEED-020's "per-engine differences as data, not branches" is implemented as a per-engine
`TimeoutIsTerminal bool` (the natural first design), it has no way to represent
`isAVTerminal`'s per-STAGE split, and the refactor will either (a) force AV and Audio into a
shape they don't fit, silently changing retry behavior for the two most expensive engine
classes, or (b) leave them as hardcoded exceptions bolted onto the "data-driven" runner,
defeating the stated purpose of the refactor for exactly the two cases where the tax the
refactor is meant to eliminate is highest.

**Why it happens:**
The refactor's stated contract (SEED-020) is "per-engine differences ... as data." Stage-
level differences are a strictly finer axis than engine-level differences, and it is easy to
under-model the data schema on the first pass — especially because five of the seven engines
(image, document, html, ocr, pandoc) genuinely ARE representable as a single per-engine
timeout-classification flag, making the two outliers (audio, av) look like noise rather than
signal until a live/load test exercises them specifically.

**How to avoid:**
- Before writing the runner, enumerate every `isXTerminal` function's FULL decision tree
  (not just its timeout branch) as data: at minimum `(stage, isTimeout) -> terminal|transient`,
  not `(engine, isTimeout) -> terminal|transient`. Audio and AV are the forcing examples;
  design the schema to fit them first, then verify the other five degenerate correctly to a
  single implicit stage.
- Port every existing pinning test (`TestIsAudioTerminal`, and any analogous test for AV, OCR,
  Pandoc) unchanged in intent — same input errors, same expected terminal/transient verdict —
  against the NEW data-driven classifier, not just against a generic "runner produces the
  right Mark* call" test.
- Do not let "the data model is uglier for two engines" become pressure to special-case those
  two OUTSIDE the runner; that reintroduces exactly the branch-not-data shape SEED-020 exists
  to remove, just relocated.

**Warning signs:**
A refactored classifier that takes only `(engine, err)` and internally still has an `if
engine == "av" { ... }` branch — that is the smell of an under-fit data schema. A code review
or diff where `isAudioTerminal`'s or `isAVTerminal`'s stage-prefix string matching
(`"audio: ffmpeg:"`, `ErrAVTranscodeFailed` vs `ErrAVAudioExtractFailed`) gets flattened into
one boolean during the port.

**Phase to address:**
The JobRunner consolidation phase (SEED-020) — this is the single highest-value thing a
reviewer should check line-by-line against the pre-refactor `worker.go`, not sample-check.

---

### Pitfall 3: `outs[0]` is not role-aware — a `sanitize` job's report can be served as the primary result, or the primary result can be hidden

**What goes wrong:**
`handleGetJob` (`internal/api/handlers.go:857-870`) does this today, unconditionally:

```go
outs, err := s.repo.Outputs(ctx, id)
...
if len(outs) > 0 {
    url, err := s.storage.PresignGet(ctx, outs[0].ObjectKey, s.presignTTL)
    ...
    resp["download_url"] = url
}
```

It always presigns `outs[0]` with zero awareness of a `role` column. The moment `sanitize`
produces two outputs (`role=primary` cleaned file, `role=report` findings JSON) via
`AddOutput`, whichever one happens to be inserted or returned first by
`Repo.Outputs`'s ordering becomes `download_url`. If insertion order is not `primary` before
`report` in every code path (initial implementation, a retry, a reconciler recovery), a
client can silently receive the findings report where it expects the cleaned file — or the
reverse, silently never seeing the report at all. This is not a hypothetical: it is the
literal, present-tense shape of the handler this milestone is about to feed multi-output data
for the first time.

**Why it happens:**
Every engine class shipped so far is strictly 1-input/1-output (PROJECT.md's own documented
architectural constraint: "Single-input/single-output assumption ... the worker and API
currently only read/write ordinal 0"). `handleGetJob` was written and reviewed under that
assumption and has never been exercised with `len(outs) > 1`. Nothing in the existing test
suite proves `outs[0]` is safe to special-case for role, because nothing before `sanitize`
ever populates a second output row.

**How to avoid:**
- Make `handleGetJob`'s output selection explicitly role-aware (`role == "primary"`) before
  `sanitize` ships, not implicitly ordering-dependent. Treat `role=report` as a SEPARATE
  response field (e.g. `report_url`), never conflatable with `download_url`.
- Add a test that inserts outputs in `report, primary` order (the "wrong" order) and asserts
  `download_url` still resolves to the primary file — proving the selection is role-driven,
  not position-driven.
- Audit `Repo.Outputs`'s SQL ordering guarantee (or lack of one) directly — an `ORDER BY
  ordinal` without a role tiebreaker is exactly the kind of thing that "happens to work" until
  a retry inserts rows in a different order.

**Warning signs:**
Any code review comment that reads "outs[0] is fine, sanitize always writes primary first" —
that is an assumption, not an invariant, unless a test proves it under retry/reconciler-
recovery interleaving too.

**Phase to address:**
The operation-dimension / `role` phase, BEFORE `sanitize` ships (not concurrently with it) —
`handleGetJob`'s role-awareness is a precondition for `sanitize` being safe to expose, not a
follow-on cleanup.

---

### Pitfall 4: Metric-label drift breaks the KEDA contract during the exact milestone most likely to tempt it

**What goes wrong:**
`TestJobDurationLabelsPinned` and `TestQueueDepthLabelsPinned` guard
`octoconv_job_duration_seconds{engine,status}` and `octoconv_queue_depth{queue,state}` —
every one of the seven `ScaledObject`s' PromQL triggers depends on exactly these label sets
(PROJECT.md FAIR-02: "ноль `queue_depth` hits in the `metrics.go` diff" was itself a
verified milestone invariant in v1.12). This milestone adds an `operation` dimension to the
job model in the SAME window as the JobRunner refactor that touches every
`RecordJobOutcome`/`RecordClientJobOutcome` call site (14 sites today, per SEED-020's own
count). The natural, well-intentioned next thought during that refactor is "we're already
touching all 14 call sites — let's add `operation` as a label while we're here, so an
operator can distinguish `convert` failures from `sanitize` failures." That single line
change breaks scale-from-zero for all seven engine classes simultaneously, silently, because
neither pinning test's failure surfaces as a KEDA symptom until a real 0-replica engine class
gets a job and nothing scales it up.

**Why it happens:**
The refactor and the operation dimension are two of this milestone's five target features
landing in the same milestone, touching the same call sites, at the same time — the ideal
conditions for “while we’re here” scope creep on a metric contract that has no compile-time
enforcement, only two specific unit tests.

**How to avoid:**
- State explicitly, in the JobRunner phase's plan, that `octoconv_job_duration_seconds` and
  `octoconv_queue_depth` keep their EXACT current label sets — `operation`, if surfaced at
  all, goes on `octoconv_client_job_outcomes_total` (already unbounded-cardinality-managed via
  the `clientLabel` allocator) or a NEW counter, never on the two pinned metrics.
- Keep `TestJobDurationLabelsPinned`/`TestQueueDepthLabelsPinned` green through every commit
  of the refactor, not just at the end — this project's own CI runs `go test` on every push;
  make sure these two tests are not skipped or the metric that would fail is not accidentally
  removed and re-added under a different name (which changes the test's own label-set literal
  and would falsely "pass" a re-authored pinning test).
- Mirror v1.12 Phase 51's own precedent for `octoconv_client_job_outcomes_total`: a NEW,
  purpose-built counter for a new dimension, deliberately NOT added to the two histograms
  already near ~250 series.

**Warning signs:**
Any diff to `internal/metrics/metrics.go` during the JobRunner or operation-dimension phases
that touches the `[]string{"engine","status"}` or `[]string{"queue","state"}` label
declarations at all — that line should not appear in this milestone's diff, full stop
(mirrors FAIR-02's own "zero-diff" verification method from v1.12).

**Phase to address:**
The JobRunner consolidation phase — verify with a diff-based check (`git diff` against the
label declarations, exactly as FAIR-02 was verified in Phase 52), not just by running the
pinning tests once at the end.

---

### Pitfall 5: `sanitize` implemented as "delete known-bad" rather than "reconstruct from known-good" produces a false-clean guarantee

**What goes wrong:**
There are two fundamentally different implementation strategies for a sanitizer, and they
produce different security guarantees while looking identical in a demo:

1. **Blacklist removal** ("find macros/JS/embedded-files/external-refs and delete them"):
   fast, preserves fidelity, but is only as complete as the removal logic's enumeration of
   "bad." It cannot make a positive claim about the rest of the file.
2. **Positive-selection reconstruction** (CDR's "rebuild from a template using only known-good
   components," per vendor documentation — Glasswall, OPSWAT Deep CDR): rebuilds the document
   from a trusted schema, discarding anything not explicitly reconstructed. Stronger
   guarantee, but admittedly imperfect too — CDR vendors themselves document that "CDR is
   unable to distinguish between malicious and benign files [so] legitimate content can be
   destroyed" (Kiteworks, OPSWAT).

The milestone spec's own report language — "что найдено и вырезано" (what was found and cut)
— describes the FIRST strategy. If the `sanitize` engine is implemented as strategy 1 but the
findings report is worded (or consumed downstream) as if it were strategy 2's guarantee — "we
sanitized this file" rather than "we removed the specific things we know to look for" — every
consumer of that report inherits a false sense of completeness. This is the single most
consequential wording/design choice in the whole milestone: sanitizer output that is
confidently wrong is worse than no sanitizer, because it removes the downstream consumer's own
incentive to be cautious.

**Why it happens:**
Blacklist removal is dramatically cheaper to build (grep the OOXML relationships graph for
`vbaProject`, strip a PDF's `/JavaScript`/`/EmbeddedFile`/`/URI` name-tree entries) and this
codebase's whole philosophy elsewhere is "closed allowlist, fail-closed" — but that philosophy
has always been applied to ACCEPTING input (reject anything not on the allowlist), never
before to REMOVING content from an already-accepted file, which is the opposite operation and
does not inherit the same guarantee for free.

**How to avoid:**
- Decide and document, explicitly, in the phase's CONTEXT/design doc, which strategy
  `sanitize` implements, and word the `role=report` output honestly: "the following known
  risk categories were found and removed" (accurate for blacklist removal), never "this file
  is safe" or "this file has been sanitized" unqualified.
- If full reconstruct-from-trusted-content is out of scope for v1 (very plausible given the
  engine-CLI-invoked-via-hardened-`runCommand` convention this project uses everywhere else,
  and given no CDR-class tool fits that convention as a drop-in binary), say so as an explicit,
  disclosed residual-risk decision — mirroring this project's own precedent for disclosed
  residual risks (e.g. `file://` passive-load risk in chromium-worker, Key Decisions table:
  "accepted residual risk").
- Do not let the phrase "sanitize" in the operation name imply a guarantee the implementation
  does not make. If a future consumer (a legal/compliance stakeholder inside the company)
  reads `role=report` as an attestation, that gap is a real liability, not just an engineering
  nuance.

**Warning signs:**
Any documentation, API description, or `role=report` JSON schema that uses unqualified
language like "clean," "safe," or "sanitized" without stating what was checked FOR — the
absence of a stated scope is itself the warning sign.

**Phase to address:**
The `sanitize` operation phase — this should be a locked design decision (with rationale)
before implementation starts, the same way PDF/A validation strategy (full ISO 19005 vs
OutputIntent sanity-check) was an explicit, disclosed Key Decision in v1.3.

---

### Pitfall 6: The sanitizer is verified by the same parser that performed the removal, so it cannot catch its own blind spots

**What goes wrong:**
If the `sanitize` engine uses library/CLI X to both remove content and confirm the output is
clean (re-run the same scan, see zero hits), it is structurally incapable of detecting the
class of bug that matters most: content that X's parser does not see but a DIFFERENT parser
(the one a downstream consumer's Office/Adobe/browser actually uses) does. This is precisely
the polyglot-file threat model: CVE-2015-5092 demonstrated PDF/SVG polyglots that pass Adobe
Reader's content-smuggling countermeasure specifically because that countermeasure's own
parser missed content a DIFFERENT parser (an SVG interpreter) would render — the general
technique ("parasite polyglots" embedding a secondary file "within the structural or ignored
segments of the primary file type," per Glasswall's own polyglot research) is exactly the
shape a same-parser self-check will always be blind to, by construction.

**How to avoid:**
- Verify `sanitize` output with a SECOND, independently-implemented parser from the one that
  performed removal — this project already has a live precedent for exactly this discipline:
  veraPDF validates a LibreOffice-produced PDF/A export with a completely separate JVM-based
  ISO 19005 validator (Phase 23), fail-closed, because "an unverifiable archival claim is a
  failed archival claim, never silently retried into a false done" (D-06). Apply the same
  discipline here: the tool that scrubs is never the tool that certifies clean.
- Scope the verification to what the second parser can actually check (macro/JS/embedded-file
  presence, external-reference presence) — an unverifiable claim should mark the job's report
  as "could not verify with independent parser" rather than silently passing, mirroring
  veraPDF's fail-closed-on-unparseable-report precedent (`terminalVeraPDFSignatures`: "pdf/a
  validation error" for ANY invocation/parse failure, not just a real non-compliance).
- Budget for this: a second parser invocation is real added latency/CPU per sanitize job — an
  explicit tradeoff, not a free addition, and it belongs in the same measured-cost discipline
  this project applies to timeouts (measure, don't assume it's cheap).

**Warning signs:**
A `sanitize` implementation whose "verify clean" step calls the exact same binary/library
with the exact same invocation shape as the removal step — that is definitionally unable to
catch anything the removal step itself missed.

**Phase to address:**
The `sanitize` operation phase.

---

### Pitfall 7: PDF incremental updates and OOXML zip-entry deletion leave "removed" content physically recoverable in the output bytes

**What goes wrong:**
Two distinct, verified mechanisms make a naive "delete the bad part" sanitizer produce a file
that LOOKS clean to a report but still CONTAINS the removed bytes on disk:

- **PDF incremental updates.** The PDF spec explicitly allows appending changes after the
  original file's bytes rather than rewriting the document; a cross-reference section added
  by an update "contains entries only for objects that have been changed, replaced, or
  deleted, and deleted objects are left unchanged in the file but are marked as deleted by
  means of their cross-reference entries" — the bytes of a "deleted" object are still present
  in the file, recoverable by walking the earlier xref/trailer chain or simply scanning the
  raw byte stream. This is the well-documented reason PDF redaction failures are recoverable
  ("Phishing PDF With Incremental Updates," SANS ISC). A sanitizer that opens a PDF, deletes
  the `/JavaScript` name-tree entry, and SAVES via an incremental update (the cheap, fast,
  default save mode in many PDF libraries) has produced a file whose JavaScript object is
  merely unlinked, not gone.
- **OOXML zip-entry deletion.** OOXML is an Open Packaging Conventions zip archive where
  parts are graph-connected via `[Content_Types].xml` and `.rels` relationship files, "as
  it's a graph structure rather than a tree" — deleting only the `vbaProject.bin` zip entry
  without removing every relationship AND content-type declaration pointing at it leaves
  dangling references (confirmed pattern: `dotnet/Open-XML-SDK` issue #618, "ChangeDocumentType
  does not fully remove vbaProject reference"). Two failure directions exist here: (a) leaving
  the bytes IN the zip while only unregistering the relationship — the macro payload is still
  physically present and trivially recoverable by any zip tool, even though Office no longer
  auto-runs it; (b) deleting the bytes but leaving `[Content_Types].xml`/`.rels` pointing at a
  now-missing part — a corrupt-but-frequently-silently-repaired file (Office often just
  "fixes" it on open with no user-visible error, masking the defect from whoever built the
  sanitizer).

**Why it happens:**
Both are "it opened fine and the report says clean" traps — the sanitizer's own read-back
check (if it re-parses via the SAME library) will not flag either issue, because the
library's own writer produced them and its own reader tolerates them.

**How to avoid:**
- For PDF: force a FULL rewrite/rebuild on save (not an incremental update) — e.g. `qpdf`'s
  default linearize/rewrite behavior discards prior revisions entirely rather than appending;
  confirm whichever library/CLI is chosen does NOT default to incremental-save mode, and add
  an explicit regression test that a sanitized PDF's raw byte stream does not contain the
  known-bad substring/object that was "removed" (grep the OUTPUT bytes, not just re-parse
  them).
- For OOXML: when removing a part, remove it from all three places atomically — the zip
  entry's bytes, its `[Content_Types].xml` declaration, and every `.rels` relationship that
  points at it (walking the graph, not assuming a single reference) — and verify with a
  structural OPC-aware checker (this project already has `internal/convert/cfb.go`'s
  directory-entry-aware classification as a precedent for "don't trust the cheap magic-byte
  prefilter alone, actually parse the structure").
- Add exactly the kind of test this project already has a template for: `ocrguards_test.go`'s
  `TestDetectBornDigitalPDF_RealBinary` fixture-based real-binary assertion — build a fixture
  PDF/OOXML with a KNOWN embedded macro/script, sanitize it, and assert the byte pattern is
  ABSENT from the output file (not just absent from a re-parse), and separately assert the
  output still opens cleanly in a structural validator.

**Warning signs:**
A sanitize implementation whose test suite only asserts "re-parsing the output finds no
macro/JS" without a raw-byte-level absence check, or a chosen PDF library whose docs describe
"incremental update" as the default/fast save path.

**Phase to address:**
The `sanitize` operation phase — this is the section of the milestone where being wrong is
most costly, per the task's own quality gate, and it is the one place a live gate (real
fixture files, real byte-level assertions) is non-negotiable rather than optional.

---

### Pitfall 8: The synchronous inspect path bypasses the v1.12 quota gate, creating an unmetered abuse budget the milestone spec names but a rushed implementation can under-scope

**What goes wrong:**
Every admission control this service has — `client_limits`, the `pg_advisory_xact_lock`-
serialized admission gate, daily byte/job caps, concurrency caps — is wired through exactly
one insertion point: `jobs.Repo.Create`, called only from `handleCreateJob`
(`internal/api/handlers.go:727`) and proxied identically by both MCP entry points (v1.12
audit, seam #3: "No bypass path, no duplicated admission code to keep in sync"). `/v1/inspect`
does not call `Create` — by the milestone's own description it is "the first request path
that does real file analysis inline instead of queuing a job," meaning the entire quota
subsystem this project spent a whole milestone building has zero visibility into inspect
traffic. The milestone spec explicitly names this ("минует v1.12 admission-гейт... собственный
rate-бюджет") — the risk is not that this goes unnoticed, but that "собственный rate-бюджет"
gets implemented as a generic per-client rate limiter (the existing `internal/ratelimit`
package, already documented as "verified only at `api.replicas: 1`" per admission.go's own
comment) rather than something that accounts for the fact that inspect's cost profile (CPU-
bound parsing) is completely different from the byte/job-count profile quotas already model.

**Why it happens:**
Reaching for the existing rate-limit middleware is the path of least resistance — it is
already wired into the router. But `internal/ratelimit`'s own documented limitation (only
correct at `api.replicas: 1`) is exactly the flaw QUOT-05's live gate spent a whole phase
proving quotas do NOT have — reusing it for inspect reintroduces, for a NEW attack surface,
the multi-replica gap this project just spent Phase 55 closing for a different one.

**How to avoid:**
- Treat inspect's rate budget as a genuinely separate design decision, not a reuse of
  `internal/ratelimit` by default — if per-replica rate limiting is accepted for inspect, that
  acceptance should be an explicit, disclosed decision (mirroring this project's pattern of
  disclosed residual risks) stating it does not hold at `api.replicas > 1`, not a silent
  inheritance of a known limitation into a new surface.
  - If it needs multi-replica correctness, it needs the same Postgres-serialized-counter
  pattern QUOT-05 proved, sized for inspect's actual cost (CPU-seconds or request-count, not
  bytes).
- Measure inspect's actual CPU cost per request class (small image vs large multi-page PDF)
  before picking a budget number — this project's own convention (measure, don't guess,
  restated at every engine-timeout Key Decision) applies here just as much as it does to
  engine timeouts.

**Warning signs:**
An inspect rate-limit implementation that is a one-line reuse of the existing
per-IP/per-client middleware with no note about its multi-replica correctness, or a rate
budget picked as a round number without a measured cost basis.

**Phase to address:**
The synchronous `POST /v1/inspect` phase — should be an explicit success criterion, ideally
proven with the same kind of live multi-replica gate that validated QUOT-05, if inspect is
expected to run at `api.replicas > 1` (which it will, since it lives in the same process as
everything else).

---

### Pitfall 9: `sanitize`'s operation vocabulary conflicts with what SEED-013 actually specified, and the existing DB CHECK constraint does not contain it

**What goes wrong:**
Two sources in this project's own planning artifacts model the same feature two different
ways, and neither has been reconciled:

- `internal/db/migrations/0001_init.sql`'s existing `operation` CHECK constraint enumerates
  `convert/extract/archive/inspect/render` — it does **not** include `sanitize` or `probe`.
  Any implementation that inserts `operation = 'sanitize'` without a schema migration will
  fail at the database with a CHECK-constraint violation (fail-closed, which is good) — but if
  that migration is written casually as "just add sanitize to the list," it may not reconcile
  with the second source below.
- SEED-013 (the actual origin of this feature) models it differently: `probe` is the
  **engine** enum value reserved for metadata-probing, and the SEED explicitly says "this
  stretches the current convert-centric model... the `operation` might be `inspect` (read) and
  `convert`/`extract` (strip → cleaned copy)" — i.e. SEED-013's own author was unsure whether
  "strip metadata" should be `operation=convert` (a same-format transform) or a new distinct
  operation value at all, and reserved `probe` for the ENGINE, not the operation.

The milestone's target-features description, by contrast, treats `sanitize` as a literal new
`operation` value ("`sanitize` — первая асинхронная не-convert операция"). If phase design
proceeds directly from the milestone description without re-reading SEED-013's own open
question, the schema and the engine registry can be built around a vocabulary
(`operation='sanitize'`, presumably `engine=` something new or reused) that was never actually
decided against the seed's own reserved `probe` value — creating exactly the "compile-time
enum in Go vs CHECK constraint in SQL" drift the question explicitly warns about, except
happening at design time rather than as later code drift.

**How to avoid:**
- Resolve, explicitly and in writing (a CONTEXT.md D-decision, this project's own convention),
  whether `sanitize` is a new `operation` enum value, whether `probe` is used as the `engine`
  value for it, and how these map — before any migration or Go code is written.
- Whichever vocabulary is chosen, add BOTH the SQL CHECK-constraint update and the Go-side
  enum/allowlist in the SAME commit, with a test that inserts every valid `operation` value
  the Go code can produce and asserts none of them are rejected by the live CHECK constraint
  (a runtime cross-check, not just a code review comparison) — this is the concrete
  countermeasure to "operation vocabulary being compile-time in Go but CHECK in SQL."
- Grep `0001_init.sql`'s CHECK constraint list literally as part of phase planning, not from
  memory of what SEED-013/the milestone description say it contains — the two documents
  disagree with each other and with the actual migration file.

**Warning signs:**
A phase plan that writes `operation IN ('convert', 'sanitize', ...)` in prose without a
corresponding grep of the live migration file; two different phases independently deciding
what value `sanitize`'s engine registration uses.

**Phase to address:**
The operation-dimension phase, before the `sanitize` phase begins — this is a modeling
decision that both phases depend on and that neither should discover mid-implementation.

---

### Pitfall 10: Registry, reconciler routing, and unique-lock TTL derivation are keyed by `(engine)` or `(source,target)` today — none of them are operation-aware by default

**What goes wrong:**
Three separate subsystems currently assume "engine" (or "format pair") is sufficient to
route/recover/de-duplicate a job, and none of them currently know about `operation`:

1. **Registry lookup** — `convert.Default.EngineFor(detected, target)` returns an engine for a
   `(source, target)` pair. A `sanitize` job on `pdf→pdf` (source format equals target format —
   a case that has never occurred in this codebase, since every existing engine class converts
   BETWEEN formats) risks either (a) not matching any registered pair at all if the registry
   assumes source≠target implicitly, or (b) matching a coincidentally-registered
   identity/no-op pair meant for something else, if `Register`/`Lookup` are not made
   operation-aware in the SAME change that introduces `pdf→pdf` as a valid pair for the first
   time.
2. **Reconciler engine routing** — PROJECT.md documents the reconciler routing stuck jobs "по
   `jobs.engine` (image/document...)" with "fail-closed skip и метрикой для нераспознанного
   engine" (Phase 10, DOC-09). A stuck `sanitize` job recovered by an engine-only switch could
   silently re-enqueue onto the CONVERT handler for that engine — producing a converted file
   where a sanitized-file-plus-report was expected, a correctness bug that would not surface
   as an error, only as wrong output.
3. **Unique-lock TTL derivation** — each engine's `asynq.Unique` TTL (`ImageUniqueTTL`,
   `AudioUniqueTTL`, etc.) is derived from that engine's own worst-case CONVERT retry budget.
   A `sanitize` job on the same engine class may have a materially different duration profile
   (extra parsing pass + report generation); reusing the convert-derived TTL risks either
   permitting a duplicate enqueue mid-attempt (TTL too short for sanitize's real worst case) or
   blocking legitimate re-enqueues for too long (TTL too generous) — the exact kind of
   "verified live, not derived by analogy" gap this project's Key Decisions table repeatedly
   calls out for engine timeouts.

**Why it happens:**
`operation` defaulting to `convert` everywhere (the milestone's own stated backward-
compatibility guarantee) makes it very easy to thread that default through the ONE place it's
explicitly required (job creation) and miss the several other places engine-scoped logic
implicitly assumed "operation=convert" as an unstated invariant, because until now that
invariant was always true.

**How to avoid:**
- Audit every place `jobs.engine` alone currently drives a routing/dedup/TTL decision (the
  registry, the reconciler's engine switch, each `*UniqueTTL` constant, the worker's dispatch
  from queue to handler) and make each one explicitly `(operation, engine)`-keyed rather than
  assuming the default is safe to leave implicit.
- Add a registry disjointness test analogous to the existing pandoc/chromium pair-disjointness
  guard (v1.11 Key Decision: "пары disjoint by construction, guard-тест на disjointness") —
  but for `(operation, source, target)` triples, specifically covering the `pdf→pdf` identity
  case sanitize introduces for the first time.
- Derive `sanitize`'s unique-lock TTL from its OWN measured worst case, never inherited from
  the same-engine convert TTL — measure, don't copy, this project's standing rule.

**Warning signs:**
Any reconciler or dedup code that switches on `job.Engine` without also branching on
`job.Operation`; a `pdf→pdf` registry entry that "just works" without an explicit test proving
it does not collide with any existing convert pair.

**Phase to address:**
The operation-dimension phase for the registry/lookup piece; the `sanitize` phase for its own
TTL measurement; both phases should include the reconciler-routing audit as a named task, not
an implicit side effect of "operation is additive."

---

### Pitfall 11: Error-code vocabulary churns within the SAME milestone that asks two MCP servers and a frontend to migrate onto it

**What goes wrong:**
SEED-020 correctly identifies that structured error codes belong in the JobRunner
consolidation ("codes get centralised... doing them as a separate earlier phase would edit
the same seven sites twice"). But this creates a sequencing hazard: the JobRunner phase is
EARLY in the milestone (it's the stated foundation), while the frontend/MCP migration that
consumes the new codes presumably happens later, closer to when `inspect`/`sanitize` actually
need new codes of their own (`quota_exceeded`-style names, `unverifiable_sanitize_output`,
whatever `pdf→pdf` collisions produce, etc.). If the code vocabulary is not FROZEN and
versioned at the end of the JobRunner phase, later phases (operation dimension, sanitize) will
keep adding/renaming codes as they discover new failure modes — meaning the frontend/MCP
migration, whenever it happens, is targeting a moving contract, and any consumer that starts
migrating early (reasonable, since JobRunner ships first) will need to migrate AGAIN when
later phases rename things for consistency.

**Why it happens:**
The natural, well-intentioned instinct during a consolidation refactor is to also clean up
inconsistent naming while touching every site (`"engine_error"` used as a catch-all seven
times, per-engine specific codes like `"born_digital"`, `"timecode_out_of_range"`,
`"duration_exceeded"` — a reasonable person doing this refactor will want to rationalize these
into a consistent taxonomy). That rationalization is exactly what makes the codes unstable
across the SAME milestone a consumer migration is happening in.

**How to avoid:**
- Freeze the error-code vocabulary as a versioned, documented list at the END of the JobRunner
  phase (a literal `ERROR_CODES.md` or an OpenAPI/JSON-schema enum, not just "grep the Go
  source"), before any consumer migration work starts.
- Treat every code added by LATER phases (operation dimension, inspect, sanitize) as strictly
  additive to that frozen list — no renaming existing codes once the frontend/MCP migration
  has started, even if a later phase's taxonomy would be cleaner in hindsight.
- Coordinate with (or at minimum, notify) the frontend and both MCP server owners about the
  exact commit/version at which the code vocabulary stabilizes, so their migration work has a
  fixed target rather than chasing a moving one.

**Warning signs:**
A later phase's diff renaming an error code string that a consumer migration PR references;
no single canonical list of valid codes existing anywhere in the repo.

**Phase to address:**
The JobRunner phase — the freeze point should be an explicit deliverable/success criterion of
that phase, not an implicit byproduct.

---

### Pitfall 12: Adding new error codes is treated as "purely additive" without verifying the two MCP servers and frontend don't do exhaustive matching

**What goes wrong:**
"Additive, backward compatible" is true from the API's perspective (existing codes keep their
meaning, new codes are new). It is not automatically true from a CONSUMER's perspective. A
consumer that does `switch(code) { case "engine_error": ...; case "invalid_options": ...;
default: throw new Error("unexpected code") }` breaks the instant `inspect`/`sanitize`
introduce a genuinely new code, even though nothing about the existing contract changed. This
is a well-known API-versioning trap, and this milestone is exactly the first time this
service's error codes become a genuinely public, cross-repository (frontend + MCP servers,
which the milestone explicitly names as existing consumers matching on TEXT today) contract —
the risk was previously masked because text-matching consumers tend to already be tolerant
(they usually only match specific known substrings and ignore the rest), but a MIGRATION to
structured codes is exactly the moment someone is likely to write a stricter, code-generated,
exhaustive switch — the more "correct-looking" implementation is the one most likely to break.

**How to avoid:**
- State explicitly, as part of the contract documentation delivered alongside the frozen code
  list (Pitfall 11), that consumers MUST treat unrecognized codes as a generic/fallback case,
  never as a hard error — and verify this against the actual two MCP server implementations'
  migration PRs, not just document it and hope.
- If either MCP server is written in a language/style that encourages exhaustive enum
  matching (e.g. a generated TypeScript union type), prefer an open string type with a curated
  set of known values plus an explicit `"unknown"` fallback over a closed enum, specifically to
  make new-code-addition non-breaking by construction.

**Warning signs:**
An MCP server or frontend PR that generates a TypeScript/Go enum type from the current code
list — that is the moment "additive" silently becomes "breaking on next addition."

**Phase to address:**
The JobRunner phase (documentation) and whichever phase does the actual frontend/MCP migration
— should include the explicit "unknown codes are non-fatal" requirement.

---

### Pitfall 13: The new `role` and `operation` columns can disagree between what the API defaults and what the DB actually persists for edge-case insert paths

**What goes wrong:**
`handleCreateJob` already hardcodes `Operation: operationConv` (`"convert"`) for every job it
creates today (`internal/api/handlers.go:730`) — that's the existing default, and it's
explicit at the one call site that exists. The risk is not that path; it's every OTHER
path that touches `jobs`/`job_outputs` rows and may not go through that same explicit default:
the reconciler's job-recovery logic, any future admin/backfill tooling, and — concretely —
EXISTING rows created before this migration ships. If the new `operation`/`role` columns are
added with `DEFAULT NULL` (or no default) rather than `DEFAULT 'convert'`/`DEFAULT 'primary'`
at the SQL level, pre-migration rows either get backfilled explicitly (requires a migration
step, easy to forget) or are left `NULL`, and every downstream consumer that assumes
`operation` is always populated (metrics, the reconciler's future operation-aware routing from
Pitfall 10, any report-role logic in `handleGetJob`) needs to handle that `NULL` case
correctly rather than assuming "additive, defaults to convert" was applied uniformly.

**How to avoid:**
- Set the SQL-level `DEFAULT 'convert'` (and `NOT NULL`) on the `operation` column and
  `DEFAULT 'primary'` (`NOT NULL`) on `job_outputs.role` in the migration itself — do not rely
  on the API layer being the only place that sets these values correctly. This mirrors the
  general principle (and this project's own repeated practice, e.g. `client_limits`'s explicit
  `NULL`-means-no-limit modeling in v1.12) that the DATABASE should be the source of truth for
  what "unset" means, not application code.
  applied.
- Explicitly test that a row inserted with NO operation/role value in the Go struct still
  round-trips as `convert`/`primary` when read back — proving the DB default, not just the Go
  default, is what's load-bearing.
- If a backfill of pre-existing rows is needed (it may not be, if the column defaults
  correctly and existing rows are read with a coalesce), state that explicitly as a migration
  step and verify with a row count, not assume `DEFAULT` retroactively applies (Postgres
  `ADD COLUMN ... DEFAULT` does apply to existing rows since PG 11, but this must be VERIFIED
  against PostgreSQL 18's actual behavior for this specific migration, not assumed from
  general PostgreSQL knowledge).

**Warning signs:**
A migration that adds `operation`/`role` columns without an explicit `DEFAULT` clause, relying
on the Go layer's `operationConv` constant to be the only place that matters; any downstream
code (metrics, reports) that would panic or misbehave on a `NULL`/empty operation value rather
than treating it as `convert`.

**Phase to address:**
The operation-dimension phase — this is exactly the kind of "additive and backward compatible"
claim the question asks to be skeptical of; verify the DB-level default explicitly, don't take
API-level defaulting as sufficient.

---

### Pitfall 14: The JobRunner refactor's test suite proves the runner's shape, not the seven engines' pre-existing behavior — regressions surface only under a live gate, not in CI

**What goes wrong:**
A natural first test suite for a new `JobRunner` asserts things like "given a converter that
returns success, MarkDone is called and a webhook is enqueued" — testing the RUNNER's generic
orchestration logic, which is correct and necessary, but is a DIFFERENT thing from testing
that `HandleAVConvert`'s specific behavior (isAVTerminal's stage-aware split,
`avFailureCode`'s specific code mapping for `ErrAVTimecodeOutOfRange` vs
`ErrAVResolutionExceeded`, the exact metric queue label) is byte-for-byte preserved. If the
seven engines' pre-refactor behaviors are each pinned only by tests that got DELETED or
REWRITTEN as part of "the logic moved into the runner, so the old test doesn't apply anymore,"
the refactor can pass 100% green CI while silently changing retry/error-code/metric behavior
for any of the seven engines, and the first evidence would be a production symptom (a client
sees a different `error_code` for a failure mode that used to be distinguishable, or a live
KEDA gate under real load reveals a retry-storm on one engine class) — exactly the shape of
bug this project's own history shows CI alone does not catch (the PR #4 finding that motivated
this whole refactor was a REVIEW finding, not a test failure).

**How to avoid:**
- Before starting the refactor, inventory every existing engine-specific test
  (`TestIsAudioTerminal`, any AV/OCR/Pandoc classifier test, `ocrFailureCode`/`avFailureCode`/
  `pandocFailureCode` unit tests) as a checklist, and require each one to have a directly
  analogous test against the NEW runner's classification for that engine — same inputs, same
  expected outputs — not a generic runner-shape test standing in for it.
  - Treat any DELETED per-engine test during the refactor as a red flag requiring explicit
  justification in the PR description, not a routine cleanup.
- Since this project already treats live gates as the final word when a design claim can't be
  proven by unit tests alone (Phase 55's precedent: "the live gate has a real history of
  failures and structural, not threshold, fixes"), consider a targeted live smoke test post-
  refactor that exercises at least one failure mode per engine class end-to-end (not just AV
  and Audio's stage-split, but a representative case from each of the seven) and asserts the
  SAME error_code / terminal-vs-transient outcome as pre-refactor.

**Warning signs:**
A refactor PR whose diff shows net-negative test line count for `internal/worker`; any
per-engine pinning test renamed rather than kept alongside a new runner-level test.

**Phase to address:**
The JobRunner consolidation phase — the test-inventory-and-preserve step should be an explicit
task in the phase's plan, sequenced BEFORE the refactor's implementation task, not verified
only at review time.

---

### Pitfall 15: XMP/embedded-thumbnail metadata and OOXML custom-properties survive a "strip metadata" pass that only targets the well-known EXIF/IPTC tag sets

**What goes wrong:**
Metadata scrubbing tools have a well-documented incompleteness trap: EXIF images commonly
embed a SECOND, separate JPEG thumbnail inside the EXIF structure itself (the thumbnail IFD),
which carries its own — potentially uncropped, unedited, GPS-tagged — copy of the image and
its own metadata; a strip pass that walks only the primary EXIF/IPTC/XMP tag namespaces and
does not also clear the embedded thumbnail can leave the exact GPS/author leak the operation
exists to prevent, sitting one level deeper in the file structure. The general pattern
recurs for OOXML (custom document properties, revision/comment author metadata, and — this
project's own domain — tracked-changes/hidden-text content are metadata in the sense that
matters for a privacy operation, even though they are not in the EXIF/IPTC/XMP tag sense the
milestone spec's language emphasizes) and for PDF (document `/Info` dictionary vs XMP metadata
stream vs per-object custom metadata — these are three separate places PDF can carry the SAME
kind of information, and a tool scoped to only one of them "succeeds" while leaving the others
intact).

**How to avoid:**
- Scope the sanitize/metadata-strip operation's format coverage explicitly per format (image
  EXIF+IPTC+XMP+thumbnail-IFD; PDF `/Info`+XMP-metadata-stream+per-object custom properties;
  OOXML core-properties+custom-properties+revision-history), and test each sub-location with a
  fixture that specifically populates it — a single "strip metadata" fixture that only
  populates the primary/well-known location will not catch a tool that misses the others.
  Mirror this project's own precedent for per-branch fixture coverage (the OCR guard tests
  exercise each guard axis — page count, page box, born-digital — as a SEPARATE fixture, not
  one combined test).
- If a chosen CLI tool (exiftool is the domain-standard, per public documentation) is used,
  verify its invocation flags actually cover ALL these sub-locations (`-all=` is
  comprehensive and does clear the thumbnail per exiftool's own documented behavior, but a
  narrower flag set like a hand-picked GPS-only or EXIF-only removal will NOT) — and pin the
  exact flag set with a test, the same way this project pins exact argv construction for every
  other engine invocation (injection tests exist for every opts-to-argv path already).

**Warning signs:**
A sanitize/scrub implementation whose fixture only checks one metadata location (e.g. only
top-level EXIF GPS tags) and declares victory; a CLI invocation using a narrower flag than the
full "clear everything" mode without an explicit, documented reason.

**Phase to address:**
The `sanitize` operation phase.

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|-----------------|-----------------|
| Implement `sanitize` as blacklist-removal only (no reconstruct-from-trusted-content) | Fits the existing hardened-CLI-via-`runCommand` convention; ships in-milestone | False-clean guarantee if report language overclaims (Pitfall 5) | Only with explicit, disclosed scope-of-guarantee language in the report and docs — never silently |
| Verify sanitize output with the same tool/library that removed the content | Less engineering, one less dependency | Cannot catch parser-specific/polyglot blind spots (Pitfall 6) | Never for a security-sensitive operation named `sanitize`; acceptable only for a lower-stakes cosmetic scrub explicitly labeled as such |
| Reuse `internal/ratelimit` for inspect's rate budget | Already wired into the router, zero new code | Inherits the documented `api.replicas: 1`-only correctness gap for a NEW attack surface (Pitfall 8) | Acceptable short-term ONLY if explicitly disclosed as a known multi-replica gap, mirroring this project's disclosed-residual-risk convention |
| Add `operation` label to the two KEDA-pinned metrics "since we're already touching every call site" | Saves a second pass later if an operation-scoped metric is ever needed | Breaks scale-from-zero for all 7 engine classes (Pitfall 4) | Never — use a new or the existing client-scoped counter instead |
| Skip a per-stage terminal-classification data model for AV/Audio, hardcode them outside the runner | Faster refactor for 5/7 engines | Reintroduces the exact branch-not-data shape the refactor exists to remove, for the two costliest engines (Pitfall 2) | Never — design the schema to fit the outliers first |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|-----------------|-------------------|
| `handleGetJob` ↔ multi-output `sanitize` results | Presign `outs[0]` unconditionally, assuming ordering (Pitfall 3) | Select by `role == "primary"` explicitly; expose `role=report` as a separate field |
| Reconciler ↔ new `operation` dimension | Route stuck-job recovery by `jobs.engine` alone, silently re-enqueuing a `sanitize` job onto a `convert` handler (Pitfall 10) | Route by `(operation, engine)`; add a fail-closed default for any unrecognized operation, mirroring the existing unrecognized-engine skip |
| Registry `Lookup`/`EngineFor` ↔ `pdf→pdf` sanitize pair | Assume source≠target always holds, or let `pdf→pdf` collide with an unrelated future no-op convert registration | Make lookup `(operation, source, target)`-keyed; add a disjointness test covering the identity-pair case explicitly |
| Frontend / MCP servers ↔ structured error codes | Treat "additive" as automatically non-breaking; consumer writes an exhaustive switch (Pitfall 12) | Document and verify "unknown codes are non-fatal" against the actual consumer implementations before/during migration |
| Inspect endpoint ↔ `/healthz` (same process) | Assume request-level timeouts on inspect are sufficient isolation from the health-check path (Pitfall 1) | Treat process-level isolation (bounded worker pool / semaphore, hardened subprocess exec) as a first-class design requirement, live-gated |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|-----------------|
| Synchronous per-page/per-frame analysis inline in `/v1/inspect` for large PDFs/media | API pod CPU spikes, `/healthz` latency degrades, correlated pod restarts | Bound page/frame sampling explicitly (documented as partial), reuse the OCR engine's page-cap/page-box-cap guard discipline but enforce it IN the API process, not just the worker | Any multi-hundred-page PDF or long-duration media file submitted concurrently by even a few clients |
| Adding `operation` (or any new dimension) to `octoconv_job_duration_seconds`/`octoconv_queue_depth` | KEDA ScaledObjects stop matching PromQL, scale-from-zero silently fails | Never touch the two pinned label sets; use `octoconv_client_job_outcomes_total`-style bounded/separate counters for new dimensions | The instant a `ScaledObject` needs to scale an engine class whose queue depth is no longer reported under the expected label set |
| Reusing a convert-engine's `*UniqueTTL` for the same engine's `sanitize` jobs | Duplicate sanitize task enqueues under load, or overlong dedup blocking legitimate re-enqueues | Measure sanitize's own worst-case duration per engine, derive its own TTL constant | Any sanitize job whose real duration (extra pass + report generation) diverges materially from the convert TTL's basis |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Sanitizer saves PDF via incremental update rather than full rewrite | "Removed" JavaScript/embedded-file objects remain byte-recoverable in the output file (verified: PDF spec's own incremental-update xref semantics; SANS ISC "Phishing PDF With Incremental Updates") | Force full rewrite/rebuild on save; add a raw-byte-level absence test, not just a re-parse check |
| OOXML macro removal deletes only the zip entry OR only the relationship, not both plus `[Content_Types].xml` | Physically recoverable payload (entry-only deletion) or a corrupt-but-silently-repaired file that masks the defect (relationship-only deletion) — verified: OPC package graph semantics, `dotnet/Open-XML-SDK#618` | Remove the part's bytes, its content-type declaration, and every relationship pointing at it atomically; verify structurally, not just by re-opening in Office |
| Self-verification: sanitizer uses the same parser for removal and "confirm clean" | Cannot detect polyglot/parser-divergence payloads by construction — verified: CVE-2015-5092 PDF/SVG polyglot bypassing Adobe's own content-smuggling countermeasure; Glasswall's documented "parasite polyglot" pattern | Verify with an independently-implemented second parser, mirroring this project's own veraPDF-independent-of-LibreOffice precedent |
| Metadata strip targets only top-level EXIF/IPTC/XMP tags, not the embedded EXIF thumbnail sub-image, OOXML custom-properties, or PDF `/Info`+XMP+per-object metadata | GPS/author/edit-history leak survives in a less-obvious sub-location (documented pattern: EXIF thumbnail IFD carrying an independent, potentially uncropped/tagged copy) | Scope and test each format's FULL set of metadata-carrying sub-locations separately, per fixture |
| Sync inspect endpoint does adversarial-input parsing without the same hardened-subprocess discipline (`runCommand`, Setpgid+SIGKILL, context-bound timeout) applied everywhere else | A crafted input hangs or resource-exhausts the API process directly (not a worker), taking `/healthz` and quota admission down with it | Apply identical exec hardening to any subprocess/library the inspect path invokes; bound total inspect work independently of any engine's `*_ENGINE_TIMEOUT` |

## "Looks Done But Isn't" Checklist

- [ ] **`sanitize` reports "found and removed X"**: Often missing a stated scope of what was
  and wasn't checked for — verify the report language does not imply a stronger guarantee
  (full reconstruction) than the implementation (blacklist removal) actually provides.
- [ ] **Sanitized output "passes validation"**: Often means "passes the SAME tool's own
  re-check" — verify an independent second parser was actually used, not just re-invoked the
  removal tool.
- [ ] **`/v1/inspect` "has a timeout"**: Often means a per-step timeout inside the handler,
  not a process-level isolation guarantee — verify `/healthz` latency under concurrent
  adversarial inspect load, not just that a single inspect request eventually returns.
- [ ] **"Operation defaults to `convert`, fully backward compatible"**: Often verified only at
  the API layer's Go constant — verify the DB column itself has `DEFAULT 'convert' NOT NULL`,
  and that every non-API insert path (reconciler, any admin tooling) is covered too.
- [ ] **"Structured error codes are additive"**: Often verified only against the API's own
  OpenAPI/contract, not against the actual frontend and MCP-server consumer implementations —
  verify none of them do exhaustive switch/enum matching that a new code would break.
- [ ] **JobRunner "passes all tests"**: Often means the new runner-level tests pass — verify
  every PRE-EXISTING per-engine pinning test (terminal classification, failure-code mapping)
  still has a direct analogue exercised against the new code path, not just deleted/replaced
  by a generic test.
- [ ] **KEDA "unaffected by this milestone"**: Often verified by running the two pinning tests
  once at the end — verify with a `git diff` against the label declarations across the WHOLE
  milestone's commit range, mirroring FAIR-02's own verification method from v1.12.

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|----------------|-----------------|
| Metric-label drift breaks KEDA (Pitfall 4) | LOW if caught before merge (revert the label change); HIGH if it reaches production undetected (silent scale-from-zero failure across 7 classes, discovered only when an idle engine class gets traffic and never scales up) | Revert the label addition; re-run FAIR-02-style zero-diff verification; if already shipped, this needs an emergency patch + a live scale-from-zero re-proof per engine class before trusting it again |
| `outs[0]` serves the wrong role (Pitfall 3) | MEDIUM — requires an API patch plus auditing whether any client already consumed a mismatched report/primary output | Ship the role-aware fix; audit `job_events`/access logs for the window it was live to identify affected clients; consider a temporary response field deprecation note |
| Sanitize output is not actually clean (blacklist miss, incremental-update leftover, polyglot) | HIGH — a security-relevant false negative, potentially already delivered to a client who trusted the "sanitized" label | Re-scan all historically sanitized outputs still in retention with the corrected implementation; disclose the gap; this is the class of bug that justifies the pre-emptive cross-parser-verification design (Pitfall 6), because reactive recovery here is expensive and trust-damaging |
| Inspect starves `/healthz`, causing pod restarts | LOW-MEDIUM — mitigated by k8s self-healing (restart), but availability impact is real during the window | Add process-level isolation (bounded pool) as a hotfix; consider moving inspect to its own deployment/process if the coupling proves structurally unfixable in-process |
| Operation vocabulary mismatch between Go and SQL CHECK (Pitfall 9) | LOW — caught fail-closed at insert time (a CHECK violation is loud, not silent) | Add the missing migration; this pitfall's cost is mostly wasted design time, not data corruption, precisely because Postgres fails closed here |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|-------------------|----------------|
| 1. Sync inspect starves `/healthz` | Synchronous `POST /v1/inspect` phase | Live gate: concurrent adversarial-shaped inspect burst, assert `/healthz` latency/liveness-probe SLA holds |
| 2. Stage-based timeout classification collapsed | JobRunner consolidation phase | Per-engine pinning tests ported 1:1 against the new runner, not replaced by generic runner tests |
| 3. `outs[0]` role-blindness | Operation-dimension / `role` phase (before `sanitize` ships) | Test inserting outputs in non-primary-first order; assert `download_url` still resolves to `role=primary` |
| 4. KEDA label drift | JobRunner consolidation phase | `git diff` against the two pinned label-set declarations across the full milestone commit range (FAIR-02 method) |
| 5. False-clean sanitize guarantee | `sanitize` operation phase | Explicit, reviewed design decision on removal-vs-reconstruction strategy; report wording matches the actual guarantee |
| 6. Same-parser self-verification | `sanitize` operation phase | Independent second-parser verification step, mirroring veraPDF's independence from LibreOffice |
| 7. PDF/OOXML "removed" content physically recoverable | `sanitize` operation phase | Raw-byte-level absence test on sanitized fixture output, not just a re-parse check |
| 8. Inspect bypasses quota gate, unmetered abuse budget | Synchronous `POST /v1/inspect` phase | Measured rate-budget basis (not reused `internal/ratelimit` uncritically); multi-replica correctness explicitly stated |
| 9. Operation vocabulary / CHECK-constraint drift | Operation-dimension phase, before `sanitize` phase | Runtime test inserting every Go-side operation value against the live CHECK constraint |
| 10. Registry/reconciler/TTL not operation-aware | Operation-dimension phase (registry); `sanitize` phase (TTL) | Disjointness test covering `pdf→pdf`; reconciler routing audit as a named task |
| 11. Error-code vocabulary churns mid-milestone | JobRunner consolidation phase | Frozen, versioned code list published as an explicit phase deliverable |
| 12. New codes break exhaustive-match consumers | JobRunner phase (docs) + consumer migration phase | Explicit "unknown codes non-fatal" contract, checked against actual MCP/frontend implementations |
| 13. `operation`/`role` DB default disagreement | Operation-dimension phase | SQL-level `DEFAULT ... NOT NULL`; round-trip test with no Go-side value set |
| 14. Runner tests prove shape, not behavior | JobRunner consolidation phase | Test inventory-and-preserve step sequenced before refactor implementation |
| 15. Metadata strip misses thumbnail/sub-location metadata | `sanitize` operation phase | Per-sub-location fixture coverage (thumbnail IFD, OOXML custom-properties, PDF `/Info`+XMP+per-object) |

## Sources

- [PDF File Structure Object Streams Explained (Kestrel Tools, 2026)](https://blog.kestreltools.com/en/blog/pdf-object-streams-xref-mechanism-explainer-2026/) — PDF 1.5 object streams and cross-reference mechanism
- [Phishing PDF With Incremental Updates — SANS ISC](https://isc.sans.edu/diary/25904) — confirms incremental-update-based content recovery is an active real-world technique
- [PDF Writer and Incremental Updates — mozilla/pdf.js DeepWiki](https://deepwiki.com/mozilla/pdf.js/2.9-pdf-writer-and-incremental-updates) — incremental-update xref/deleted-object semantics
- [ChangeDocumentType does not fully remove vbaProject reference — dotnet/Open-XML-SDK#618](https://github.com/OfficeDev/Open-XML-SDK/issues/618) — confirms OOXML dangling-reference-after-macro-removal is a real, documented defect class
- [OOXML/Open Packaging Conventions (OPC) — Apache OpenOffice Wiki](https://wiki.openoffice.org/wiki/OOXML/Open_Packaging_Conventions_(OPC)) — OPC package relationship-graph semantics
- [What is Content Disarm and Reconstruction (CDR)? — Kiteworks](https://www.kiteworks.com/risk-compliance-glossary/cdr-content-disarm-and-reconstruction/) — CDR rebuild-from-trusted-content approach and its documented limitations
- [What is Content Disarm and Reconstruction? — OPSWAT](https://www.opswat.com/blog/what-is-content-disarm-and-reconstruction) — sanitization vs. full CDR distinction
- [PDF-based polyglots through SVG images (CVE-2015-5092) — Minded Security](https://blog.mindedsecurity.com/2015/08/pdf-based-polyglots-through-svg-images.html) — confirmed CVE of a sanitizer/countermeasure bypassed by a cross-format polyglot
- [Unmasking polyglot files — Glasswall](https://www.glasswall.com/us/blog/unmasking-polyglot-files-how-glasswalls-cdr-technology-tackles-this-stealthy-threat) — "parasite polyglot" pattern description
- [Exploitation and Sanitization of Hidden Data in PDF Files — arXiv](https://arxiv.org/pdf/2103.02707) — academic treatment of PDF sanitization gaps and cross-tool comparison
- [ExifTool: How to Remove All Metadata From Files (Safely + Completely)](https://renamer.io/blog/exiftool-how-to-remove-all-metadata-from-files-safely-completely/) — documents the embedded-thumbnail metadata survival trap and `-all=` as the comprehensive flag
- Direct codebase verification: `internal/worker/worker.go` (seven handlers and their terminal classifiers, read in full), `internal/api/handlers.go` (`handleCreateJob`, `handleGetJob`'s `outs[0]` behavior), `internal/convert/ocrguards.go` (fail-closed guard precedent), `internal/metrics/metrics.go` (pinned label sets, bounded-cardinality precedent), `.planning/PROJECT.md` Key Decisions table, `.planning/seeds/SEED-020.md`, `SEED-013.md`, `SEED-021.md`, `.planning/milestones/v1.12-MILESTONE-AUDIT.md`

---
*Pitfalls research for: OctoConv v1.13 Inspect & Operations*
*Researched: 2026-08-07*
