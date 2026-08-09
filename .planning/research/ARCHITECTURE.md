# Architecture Research — v1.13 Inspect & Operations

**Domain:** Internal Go async file-conversion service — subsequent-milestone integration research
**Researched:** 2026-08-07
**Confidence:** HIGH (every claim below is grounded in a file read during this research pass; SEED claims were independently re-verified against current code, not assumed)

This is not a generic architecture survey. It answers, file by file, how v1.13's five
workstreams (JobRunner consolidation, the `operation` dimension, output roles, synchronous
inspect, born-digital routing) land on the actual OctoConv codebase as it exists today
(post-v1.12, pre-Phase-56).

---

## A. JobRunner consolidation (SEED-020)

### What SEED-020 claims vs. what the code shows

| Claim | Verified? | Detail |
|---|---|---|
| `internal/worker/worker.go` is 1989 lines | Confirmed (`wc -l` → 1989; file has no trailing newline, so line-numbered reads show content through line 1990 — same file, off-by-one artifact of the counting method, not a discrepancy) | |
| Seven handlers, "94 to 163 lines each" | **Partially inaccurate — corrected.** Measured function-body spans (signature line to closing `}`), body only: `HandleImageConvert` 704–782 (79), `HandleDocumentConvert` 798–903 (106), `HandleHTMLConvert` 917–1014 (98), `HandleAudioConvert` 1033–1142 (110), `HandleAVConvert` 1196–1294 (99), `HandleOCRConvert` 1314–1412 (99), `HandlePandocConvert` 1436–1534 (99). Range is **79–110**, not 94–163. Including each function's leading doc comment (12–22 lines) brings the range to **86–127** — still short of 163. This doesn't change SEED-020's conclusion, but the roadmap should cite the corrected 79–110 (body) / 86–127 (with doc comment) figures, not the seed's. |
| "Phase 51 had to touch 14 symmetric call sites to add a single Prometheus counter" | **Confirmed exactly.** `grep -n "metrics.RecordClientJobOutcome" internal/worker/worker.go` returns exactly 14 lines (2 per handler × 7 handlers: one in the terminal-failure branch, one in the terminal-success tail). |
| Post-commit hooks are the ×7 tax; transactional concerns already have a home (`Repo.transition`'s `apply` closure) | Confirmed. `recordUsage` (internal/jobs/usage.go) is called from inside `MarkDone`/`MarkFailed`/`Cancel`'s `apply` closures in `internal/jobs/repo.go:204-269`, never adjacent. |
| `HandleWebhookDeliver` is out of scope | Confirmed — genuinely different shape: no `MarkActive`/terminal-classification skeleton, its own retry/dead-letter bookkeeping via `webhook.Repo.RecordAttempt`/`MarkDeadLetter` (worker.go:1563-1637). |

**One thing SEED-020 does not mention, found during this pass:** the exact same defect
class PR #4 caught in worker.go is **still live** in `internal/reconciler/reconciler.go:311-312`:

```go
ferr := s.store.MarkFailed(ctx, j.ID, "reconciler_exhausted", ...)
metrics.RecordReconcilerAction("exhausted")   // <-- unconditional, NOT guarded on ferr
if job != nil && ferr == nil {
    metrics.RecordClientJobOutcome(...)        // <-- this one IS correctly guarded
}
```

`RecordReconcilerAction("exhausted")` fires even when `MarkFailed` did not commit — a real,
currently-uncaught instance of the "counter before commit" bug, in a different package than
the seven handlers SEED-020 names. It should be fixed in the same phase as A (see §G),
because it's the same bug shape and touches the same "post-commit hook must be exactly one
call in exactly one place" invariant the refactor exists to establish.

### What is genuinely identical across all seven handlers

The skeleton SEED-020 describes is real and, more importantly, **`process()` (worker.go:1847-1942) is already the single shared orchestration function** — every handler calls the same `h.process(ctx, job)`. The ×7 tax is not in `process()`; it is in the ~80–110-line wrapper repeated around it in each `Handle*Convert`:

1. `queue.ParseConvertPayload` → unparseable payload → `SkipRetry` (identical, 7×)
2. `h.repo.Get(ctx, jobID)` (identical, 7×)
3. **Conditional** strict opts re-parse (`*OptsFromMap`) → `MarkFailed("invalid_options", ...)` → `SkipRetry` (6 of 7 handlers — image has none; see table below)
4. `h.repo.MarkActive(ctx, jobID)` → already active/done/canceled → `SkipRetry` (identical, 7×)
5. `start := time.Now()`; `h.process(ctx, job)` (identical call, 7×)
6. On error: `isCanceled` check → `SkipRetry` without `MarkFailed` (identical, 7×)
7. On terminal error: engine-scoped classifier → `MarkFailed(code, msg, ...)` → `RecordJobOutcome` → guarded `RecordClientJobOutcome` → guarded webhook enqueue → `SkipRetry` (same shape, 7×; only the classifier fn and code/message mapping differ)
8. On transient error: return unwrapped, no metric (identical, 7×)
9. On success: `RecordJobOutcome` → `RecordClientJobOutcome` → webhook enqueue (identical, 7×)

### Per-engine differences, as a data shape

| Field | Image | Document | HTML | Audio | AV | OCR | Pandoc |
|---|---|---|---|---|---|---|---|
| `queue.Type*Convert` / `queue.Queue*` | `TypeImageConvert`/`QueueImage` | `TypeDocumentConvert`/`QueueDocument` | `TypeHTMLConvert`/`QueueHTML` | `TypeAudioConvert`/`QueueAudio` | `TypeAVConvert`/`QueueAV` | `TypeOCRConvert`/`QueueOCR` | `TypePandocConvert`/`QueuePandoc` |
| Opts strict re-parse (`D-10` guard) | **none** | `DocOptsFromMap` | `HTMLOptsFromMap` | `AudioOptsFromMap` | `AVOptsFromMap` | `OCROptsFromMap` | `PandocOptsFromMap` |
| Terminal classifier | `isTerminal` | `isDocumentTerminal` (timeout=terminal) | `isHTMLTerminal` (timeout=terminal) | `isAudioTerminal` (stage-aware, timeout=transient at whisper stage) | `isAVTerminal` (stage-aware, transcode-timeout=transient) | `isOCRTerminal` (timeout=transient, deliberate divergence) | `isPandocTerminal` (timeout=terminal) |
| Failure-code mapper | inline `"engine_error"` only | inline `"engine_error"` only | inline `"engine_error"` only | inline 2-way (`duration_exceeded` / `engine_error`) | `avFailureCode` (4-way) | `ocrFailureCode` (6-way) | `pandocFailureCode` (3-way) |
| Pre-`Convert` guard splice inside `process()` | none | none | none | `enforceAudioGuardBeforeConvert`, gated on `job.Engine == convert.EngineAudio` (worker.go:1915) | none (AVConverter self-contains its own duration/resolution guard) | none (OCRConverter self-contains page/box/born-digital guards) | none |
| Progress reporting wiring | none | none | none | none | `SetProgressThrottle`/`SetProgressWebhook` called from `cmd/av-worker/main.go` | `SetProgressThrottle`/`SetProgressWebhook` called from `cmd/ocr-worker/main.go` | `SetProgressThrottle`/`SetProgressWebhook` called from `cmd/pandoc-worker/main.go` |
| `metrics.RecordJobOutcome`/`RecordClientJobOutcome` label | `queue.QueueImage` | `queue.QueueDocument` | `queue.QueueHTML` | `queue.QueueAudio` | `queue.QueueAV` | `queue.QueueOCR` | `queue.QueuePandoc` |

**Correction to the milestone context's framing:** "disk guards for av" and "page caps for
ocr" are **not** handler-level differences — they live entirely inside `AVConverter.Convert`
and `OCRConverter.Convert` (`internal/convert/av.go`, `internal/convert/ocrguards.go` +
`ocrpdf.go`), which the shared `process()` invokes uniformly via `registry.Lookup`. The
handler never branches on them. Likewise "unique-lock TTL" is not a `Handler`/runner concern
at all — it lives in `internal/queue/queue.go`'s `*UniqueTTL` functions, consumed only by
`EnqueueXConvert` (producer side) and the reconciler's re-enqueue, never read inside
`worker.go`. The milestone context's list of "per-engine differences" conflates converter-
internal guards with handler-level differences; the JobRunner descriptor only needs the four
rows above (opts parser, terminal classifier, failure-code mapper, progress config), plus the
one `process()`-level guard splice (today audio-only).

**Also notable for the runner design:** each `cmd/*-worker/main.go` already instantiates
exactly **one** `Handler` per binary and registers exactly **one** `Handle*Convert` on its
own `asynq.ServeMux` (verified: `grep mux.HandleFunc cmd/*/main.go` — one match per binary,
except `cmd/webhook-worker` which registers `HandleWebhookDeliver`). So the runner does not
need to dispatch across engines within one process — one engine class per binary is already
the deployment model (Docker/K8s isolation per engine, per CLAUDE.md's Architectural
Constraints). This means `NewHandler`'s current ~10-parameter constructor, where every
`cmd/*-worker/main.go` passes several `nil`/`0` values it will never use (visible directly in
the grep output above — `nil, // webhookRepo — webhook-only; HandleXConvert never reads it`,
repeated in all six non-webhook worker mains), is itself evidence of the same problem at the
constructor level, not just inside the handler bodies.

### Proposed runner signature

```go
// EngineDescriptor is the per-engine-class data SEED-020 asks for — everything that
// varies across the seven Handle*Convert bodies, as values/functions instead of
// hand-copied branches.
type EngineDescriptor struct {
    Engine string // convert.EngineImage, .EngineDocument, ... — same string as queue.QueueX
    Type   string // queue.TypeXConvert, for mux registration

    // OptsFromMap is nil for image (no strict re-parse guard exists there today).
    OptsFromMap func(map[string]any) (any, error)

    // IsTerminal classifies a process() error. Every engine has one; image's is the
    // shared isTerminal itself.
    IsTerminal func(error) bool

    // FailureCode maps a terminal error to (code, message). Defaults to
    // ("engine_error", "unsupported or corrupted input format") when nil.
    FailureCode func(error) (code, message string)

    // PreConvertGuard splices a guard in front of conv.Convert inside process().
    // nil for six of seven engines; audio sets enforceAudioGuardBeforeConvert.
    PreConvertGuard func(ctx context.Context, job *jobs.Job, inPath string) error
}

// Runner replaces the seven Handle*Convert methods with one generic mux.HandlerFunc,
// closed over one EngineDescriptor per instance (one Runner per worker binary, mirroring
// today's one-Handler-per-binary deployment model).
type Runner struct {
    h *Handler // unchanged: repo/store/registry/engineTimout/webhookRepo/deliverer/enqueuer/...
    d EngineDescriptor
}

func (r *Runner) Handle(ctx context.Context, t *asynq.Task) error { ... }
```

`cmd/worker/main.go` (etc.) changes from `mux.HandleFunc(queue.TypeImageConvert,
h.HandleImageConvert)` to `mux.HandleFunc(queue.TypeImageConvert, worker.NewRunner(h,
worker.ImageDescriptor).Handle)`. `queue.QueueX` labels can be **eliminated entirely** in
the runner body — `job.Engine` (already loaded by `h.repo.Get`) is byte-identical to
`queue.QueueX` by construction (`internal/queue/queue.go:38-45`: `QueueImage =
convert.EngineImage`, etc.) — one fewer thing to keep in sync per engine.

### Post-commit hook: making it structurally one call in one place

The runner's `Handle` method should have **exactly one** call to
`metrics.RecordJobOutcome`/`RecordClientJobOutcome` and **exactly one** call to
`EnqueueWebhookDeliver`, both reached only from a single terminal-outcome branch that is
computed AFTER (never before) the `MarkDone`/`MarkFailed` call's error is checked:

```go
outcome, ferr := r.finish(ctx, job, processErr) // MarkDone or MarkFailed, returns (status, err)
if ferr == nil {
    metrics.RecordJobOutcome(job.Engine, outcome, elapsed)
    metrics.RecordClientJobOutcome(job.ClientID.String(), job.Engine, outcome)
    if job.CallbackURL != "" {
        _ = r.h.enqueuer.EnqueueWebhookDeliver(ctx, job.ID)
    }
}
```

This is what makes "structurally impossible to fire before commit" true: there is no code
path that reaches the metrics call without first passing through `ferr == nil`, because there
is only one call site, guarded by one `if`, and no handler-specific copy of that `if` to get
wrong. The reconciler's own `RecordReconcilerAction("exhausted")` bug found above is a live
demonstration of what happens when this guard is duplicated by hand instead of centralized —
the fix belongs in the same phase.

### Tests that pin behavior and must not change

- `internal/metrics/metrics_test.go`: `TestJobDurationLabelsPinned`,
  `TestQueueDepthLabelsPinned` — guard the KEDA PromQL contract (`{engine,status}` /
  `{queue,state}` label sets). The runner must keep calling `RecordJobOutcome`/
  `RecordClientJobOutcome` with the same two-label shape.
- Every `isXTerminal`/`isCanceled`/`*FailureCode` function has direct unit test coverage in
  `internal/worker/worker_test.go` (not read line-by-line here, but referenced throughout
  the doc comments above — e.g. `T-31-08`, `WR-01`/`WR-03` audio comments, `44-CONTEXT.md`
  OCR references) — these are pure functions and should be extracted **unchanged**, only
  relocated from package-level funcs into `EngineDescriptor` field values; a refactor that
  changes their *logic* while moving them is out of scope and risks re-litigating already-
  settled classification decisions (D-01 through D-15 scattered through worker.go's comments).
- `internal/reconciler/reconciler_test.go` / `reconciler_soak_test.go` — the engine-routing
  `switch` (reconciler.go:342-369) is untouched by A; only the metrics-guard fix above
  touches this file.
- `internal/jobs/repo_test.go` — `Repo.transition` and its guarantees are explicitly called
  out in PROJECT.md's milestone framing as an invariant this milestone does not touch; the
  runner only reorders/consolidates code that calls into it, never the transition machinery
  itself.

---

## B. The `operation` dimension

### Confirmed CHECK values (0001_init.sql)

```sql
-- jobs.operation (line 45-46) and presets.operation (line 22-23), identical:
CHECK (operation IN ('convert', 'extract', 'archive', 'inspect', 'render'))

-- jobs.engine, as originally shipped (line 47-48):
CHECK (engine IN ('image', 'document', 'av', 'cad', 'archive', 'probe'))
```

`jobs.engine`'s CHECK has been widened four times since, each an `ALTER TABLE ... DROP
CONSTRAINT jobs_engine_check; ADD CONSTRAINT ...` pair mirroring the same pattern:
`0005_html_engine.sql` (+html), `0006_audio_engine.sql` (+audio), `0009_ocr_engine.sql`
(+ocr), `0010_pandoc_engine.sql` (+pandoc). Current live constraint:

```sql
CHECK (engine IN ('image', 'document', 'av', 'cad', 'archive', 'probe', 'html', 'audio', 'ocr', 'pandoc'))
```

`cad`, `archive`, and `probe` were reserved from day one and remain **unused** — no
`Converter.Engine()` implementation returns any of them today (verified: `grep -rn
"func.*Engine() string"` across `internal/convert/*.go` returns exactly seven implementations,
one per registered converter, matching `AllEngines()`). `probe` is the value SEED-013 and the
v1.13 milestone scope both name as `sanitize`'s engine class.

`jobs.operation` has **never been altered** — it is still exactly the five 0001 values, and
critically, **`sanitize` is not one of them**. Adding it requires a new migration mirroring
the `jobs_engine_check` widening pattern (a `jobs_operation_check`-style
`DROP`/`ADD CONSTRAINT`, presumably also widening `presets.operation`'s identical CHECK for
symmetry, though presets are out of this milestone's scope unless a sanitize preset is
wanted).

### `operation` is already fully plumbed at the persistence layer — only the API ignores it

- `jobs.CreateParams.Operation string` (internal/jobs/repo.go:74) is a real field.
- `Repo.Create`'s `INSERT INTO jobs (..., operation, ...)` (repo.go:151-154) writes it
  verbatim.
- `Repo.Get`'s `SELECT ... operation, ...` (repo.go:435-441) scans it back into `j.Operation`.
- The **only** place `operation` is ever set is `internal/api/handlers.go:32,730`:
  `const operationConv = "convert"` ... `Operation: operationConv`. There is exactly one call
  site, and it is a hardcoded constant — the API never reads a client-supplied operation
  value at all today.

This means B's "minimal change set" is smaller than it looks: the column, the insert, and the
read path are done. What's missing is (1) the CHECK constraint widening for `sanitize`
(needed only once sanitize ships, not for `inspect`, which is already in the enum), (2) API
acceptance of an `operation` form field defaulting to `operationConv`, and (3) making
`convert.Registry` operation-aware so `(operation, from, to)` can resolve to a *different*
converter than `(convert, from, to)` for the same `(from, to)` pair (this is what makes
`sanitize` on `pdf→pdf` distinguishable from a hypothetical future `convert` on `pdf→pdf`).

### Making `Registry` operation-aware without breaking existing call sites

`convert.Pair` (convert.go:66-71) is `{From, To string}`, and `Registry.m` is `map[Pair]Converter`
(convert.go:112). `Lookup`/`Supports`/`EngineFor` (convert.go:128-148) all take `(from, to)`
only. There are dozens of existing callers across `internal/api/handlers.go:397`,
`internal/worker/worker.go:1888`, `internal/api/formats_handlers.go:21`, and every
`*_test.go` in `internal/convert`.

The codebase already has a precedent for adding an **optional capability** to `Converter`
without touching the required three-method interface or any existing implementation:
`ProgressConverter` (`internal/convert/progress.go:39-45`) is a second, optional interface
that `AVConverter`/`OCRConverter` implement and everything else doesn't; `process()`
type-asserts for it (`runConvertWithOptionalProgress`, worker.go:1827-1829) and falls back to
plain `Convert` when absent. This is the right template for operation-awareness too:

```go
// OperationalConverter is an optional capability (mirrors ProgressConverter's precedent).
// A Converter that does not implement it is assumed operation="convert" — every existing
// converter (image/document/html/audio/av/ocr/pandoc) needs ZERO changes.
type OperationalConverter interface {
    Operation() string
}
```

`Registry.Register` type-asserts each `Converter` for `OperationalConverter`, defaulting to
`"convert"` when absent, and keys `r.m` on a widened `Pair{Operation, From, To}` (Operation
normalized the same defensive way `NormalizeFormat` normalizes formats — lowercase, no
aliasing needed since the CHECK constraint is the closed set).

To keep **every existing caller byte-unchanged**, `Lookup`/`Supports`/`EngineFor` keep their
current two-argument signatures and internally delegate to `operationConvert` (mirrors the
codebase's established convention of adding a new method rather than widening an existing
signature — see `SetProgressThrottle`/`SetProgressWebhook`'s post-construction-setter
precedent, worker.go:647-671, explicitly chosen over widening `NewHandler`'s signature for the
same "existing callers must not change" reason):

```go
func (r *Registry) Lookup(from, to string) (Converter, bool) { return r.LookupOp("convert", from, to) }
func (r *Registry) LookupOp(operation, from, to string) (Converter, bool) { ... }
// Supports/EngineFor mirror the same pattern.
```

Only two call sites need to move to the `*Op` variant once `operation` is API-accepted:
`internal/worker/worker.go:1888` (`process()`'s registry lookup, reading `job.Operation`
instead of assuming convert) and `internal/api/handlers.go:397` (`handleCreateJob`'s
`EngineFor` call, once it accepts a client-supplied operation). Every other of the ~30+
existing `Lookup`/`Supports`/`EngineFor` call sites (tests, `formats_handlers.go`) needs zero
changes.

### `GET /v1/formats` backward compatibility

`Registry.Classes()` (convert.go:150-172) walks `r.m` keyed by `Pair` and groups by
`c.Engine()` only — it has no operation concept today. `handleListFormats`
(formats_handlers.go:20-33) renders `{"engines": {engine: {"pairs": [[from,to],...]}}}`. To
keep this response **byte-identical** for the seven existing (implicitly `convert`) engines,
`Classes()` should keep grouping the top-level `engines` key by pairs whose `Operation ==
"convert"` only, and a new, additive top-level key (e.g. `"operations"`) can expose the full
`(operation → engine → pairs)` tree once a second operation (`sanitize`, and already-reserved
`inspect`) has registered converters. This mirrors the existing "additive key, old key
unchanged" idiom already used at `handlers.go:802-804` (`resp["opts"]` only added `if
len(normalizedOpts) > 0`).

### asynq queue for `sanitize`

`sanitize`'s engine is `probe` (SEED-013's breadcrumb, reserved in the schema since 0001).
There is **no** existing `QueueProbe`/`TypeProbeConvert` and **no** `cmd/probe-worker` — this
is a genuinely new, eighth engine class, not a reuse of an existing queue. The correct answer
per the established 1:1 `Type<Engine>Convert`/`Queue<Engine>` convention
(`internal/queue/queue.go:19-46`) is a **new** queue, following the exact pattern
`0009_ocr_engine.sql`/`0010_pandoc_engine.sql` already proved out twice (6th and 7th engine
classes, both added with zero incident per PROJECT.md's v1.10/v1.11 history).

**On the "KEDA blast radius" framing in the question:** adding an eighth queue is **not**
high blast radius. Each `deploy/chart/octoconv/templates/scaledobject-*.yaml` file's PromQL
is self-contained to its own queue name literal (verified:
`scaledobject-image.yaml:38`: `query: sum(octoconv_queue_depth{queue="image",
state=~"pending|active|retry"})` — the string `"image"` appears nowhere else in that file or
any other ScaledObject file). Adding `scaledobject-probe.yaml` touches **zero** existing
files at the ScaledObject layer. The one place a new queue must be registered centrally is
`internal/queue/queue.go:813` `AllConvertQueues()` — and even that function's own doc comment
(queue.go:801-810) explains it exists **precisely so** `cmd/api/main.go`'s queue-depth
collector never needs a second edit: "`cmd/api/main.go`'s queue-depth collector arg list...
is VARIADIC... this is the single source of truth." The actual blast radius for adding
`sanitize`/`probe` is: one migration (widen `jobs_operation_check` and note `probe` is already
in `jobs_engine_check`), new `queue.go` constants + `AllConvertQueues()` entry, a new
`cmd/probe-worker`, a new `Enqueuer.EnqueueProbeConvert` interface method + implementation, a
new `case` in `reconciler.go`'s engine switch (line ~342-369) and `handlers.go`'s engine
switch (line ~770-792), and one new ScaledObject file. **Renaming or removing** an existing
queue is the actually-expensive operation (7 PromQL strings + `AllConvertQueues()` +
reconciler routing) — nothing in v1.13's scope requires that.

### Admission gate and `client_usage_daily` treatment of non-convert operations

Both are **entirely operation-agnostic already, by construction, requiring zero code
change**:

- `internal/jobs/admission.go`'s `loadQuotaState` (line 293-306) joins `clients` +
  `client_limits` on `engine` only (`LEFT JOIN client_limits l ON l.client_id = c.id AND
  l.engine = $2`) — no `operation` predicate anywhere in the file.
- `evaluate`'s concurrency count (line 320-339) and daily-usage query (line 373-449) both
  filter `WHERE ... engine = $2` only.
- `internal/jobs/usage.go`'s `recordUsage` INSERT (line 46-81) has no `operation` reference
  at all; `client_usage_daily`'s primary key is `(client_id, engine, day, status)` (migration
  0011, confirmed via the `ON CONFLICT (client_id, engine, day, status)` clause at usage.go:75).

A `sanitize` job with `engine='probe'` automatically gets its own independent
`client_limits` row keyed `(client_id, 'probe')`, fully isolated from every other engine's
quota bucket, for free. The one **disclosed limitation**: because the ledger's grain is
`(client_id, engine, day, status)` with no operation column, if a *second* `probe`-engine
operation is ever added later, the two operations' consumption would be indistinguishable in
`client_usage_daily` and in `GET /v1/system/usage` — not a v1.13 problem (only one probe
operation ships), but worth a one-line note in the roadmap so it isn't silently assumed away
when SEED-013's "read" half (an `inspect`-operation, `probe`-engine job) is considered later.

---

## C. Output roles

### Current schema

`job_outputs` (0001_init.sql:89-101): `id, job_id, ordinal, object_key, filename, format,
size_bytes, content_type, metadata jsonb, expires_at, created_at`. No `role` column.
Migration `0012_output_identity_and_retention_index.sql` added `UNIQUE (job_id, ordinal)` and
`jobs_retention_idx` — it does not touch `role` (didn't exist to touch).

### Minimal additive migration

```sql
ALTER TABLE job_outputs
    ADD COLUMN role text NOT NULL DEFAULT 'primary'
        CHECK (role IN ('primary', 'report', 'preview', 'thumbnail'));
```

`DEFAULT 'primary'` is what makes this zero-behavior-change: `Repo.AddOutput`'s current
`INSERT INTO job_outputs (job_id, ordinal, object_key, filename, format, size_bytes,
content_type) VALUES (...)` (repo.go:410-419) does not list `role` at all — every existing
call takes the column default automatically, with **no Go code change required** for the
column to exist safely under every current caller. `jobs.Output` (the Go struct) and
`AddOutput`'s `ON CONFLICT ... DO UPDATE SET` list only need `Role` added once a caller
(sanitize's handler) actually wants to set it to `'report'`.

### `GET /v1/jobs/{id}` backward compatibility

`handleGetJob` (handlers.go:856-878) currently does `outs, _ := s.repo.Outputs(ctx, id)` then
unconditionally presigns `outs[0].ObjectKey` into `resp["download_url"]`. Since ordinal 0 is
always the primary output today, `outs[0]` and "the primary-role output" are currently
identical. The additive, backward-compatible change:

1. `download_url` continues to be the presigned URL of the `role == 'primary'` output (in
   practice still `outs[0]` for every job that has exactly one output).
2. A new `outputs` array key is added to the response **only when `len(outs) > 1`**, listing
   `{role, ordinal, format, size_bytes}` per output (presigned lazily, or all eagerly — a
   phase-56/60-era planning decision, not an architecture question). This mirrors the
   established idiom at `handlers.go:802-804` (`if len(normalizedOpts) > 0 { resp["opts"] =
   ... }`) and `handlers.go:872-877` (`error_code`/`error_message` only when non-empty) —
   this codebase's convention is additive keys guarded on non-emptiness, never a shape change
   to an existing key. A single-output client's response is **byte-identical**.

### Verifying the byte-accounting claim

The milestone context claims byte accounting already sums over ALL ordinals with no
predicates. Confirmed by direct read of `internal/jobs/usage.go:59-60`:

```sql
COALESCE((SELECT sum(size_bytes) FROM job_inputs WHERE job_id = j.id), 0),
COALESCE((SELECT sum(size_bytes) FROM job_outputs WHERE job_id = j.id), 0),
```

Neither subquery filters on `ordinal` (and, once added, neither would need to filter on
`role` either — there is no `WHERE` clause beyond `job_id = j.id`). A `sanitize` job that
calls `AddOutput` twice (`role='primary'` cleaned file, `role='report'` findings JSON) is
**already** correctly summed into `client_usage_daily.bytes_out` with zero changes to
`usage.go` — the claim is verified true, exactly as PROJECT.md's v1.12 retrospective states.

---

## D. Synchronous `POST /v1/inspect`

### Where it lives

Following this codebase's established package-per-domain convention (`internal/presets`,
`internal/webhook`, `internal/jobs`, `internal/convert` are each their own top-level package
rather than code crammed into `internal/api`), the inspection *logic* — composing sniffers,
computing risk signals, building the report — belongs in a new **`internal/inspect/`**
package, not inside `internal/convert` (whose `Converter` interface is about transformation,
not reporting) and not inside `internal/api` (whose `handlers.go` files are already thin HTTP
adapters over other packages, e.g. `handleCreateJob` calling into `internal/presets.Resolve`).
The HTTP handler itself is a new `internal/api/inspect_handlers.go` (mirroring the
one-file-per-responsibility convention: `formats_handlers.go`, `jobs_list_handler.go`-style
naming), a thin adapter calling `internal/inspect.Report(...)`.

This also matters for the worker-delegation question below: a package under `internal/`
(not `internal/api`) can be imported by a future `cmd/*-worker` binary without an import-cycle
or layering violation, if deep inspection ever needs to run inside a worker container.

### What it reuses from `internal/convert`

| Sniffer | File | Runtime dependency | Usable from the API process today? |
|---|---|---|---|
| Magic-byte format detection | `convert.Sniff` (sniff.go) | pure Go | Yes |
| ZIP/OOXML structure + macro detection (`cr.HasMacro`, already used at handlers.go:240) | `convert.SniffContainer` | pure Go | Yes |
| Declared image dimensions (bomb defense) | `convert.Dimensions`/`HasDimensionLimit` | pure Go, zero-dependency (CLAUDE.md Key Decision) | Yes |
| OLE-CFB legacy/encrypted detection | `convert.IsOLECFB`/`ClassifyCFB` | pure Go, custom bounded parser | Yes |
| HTML/LaTeX/markup family sniff | `convert.LooksLikeHTML`/`LooksLikeLaTeX`/`LooksLikeText` | pure Go | Yes |
| PDF page count / page box | `ocrPDFPageCount`/`ocrPDFPageSize` (ocrguards.go) | shells out to `pdfinfo` (poppler-utils) | **No** |
| Born-digital text-layer detection | `detectBornDigitalPDF` (ocrguards.go:230) | shells out to `pdftotext` (poppler-utils) | **No** |
| Audio/video duration + codecs | (av/audio converters' ffprobe calls) | shells out to `ffprobe` | **No** |
| EXIF/GPS/IPTC/XMP metadata | none exists yet (SEED-013's own scope) | would need `exiftool` or a pure-Go parser | **No** |

### Guarantees that do NOT apply to `/v1/inspect`, enumerated

1. **v1.12 admission gate** (`internal/jobs/admission.go`): inspect never creates a `jobs`
   row, so `admitLocked`/`PreAdmit` never run. There is no per-`(client, engine)`
   concurrency/daily-byte/daily-job accounting for inspect calls, ever.
2. **`client_usage_daily` accounting**: `recordUsage` only fires inside
   `Repo.transition`'s terminal `apply` closures (`MarkDone`/`MarkFailed`/`Cancel`). No job
   row, no transition, no usage row — an inspect call is invisible to `GET
   /v1/system/usage` and to daily quota totals.
3. **The reconciler**: only scans `jobs` rows in `queued`/`active`
   (`internal/jobs/repo.go:306-331`). A dead API process mid-inspect leaves nothing to
   recover; the client's only recourse is to retry the HTTP request. No crash-recovery
   guarantee exists, unlike every async operation.
4. **Webhooks**: no `callback_url` concept applies; the report is the HTTP response body.
5. **SSE progress** (`GET /v1/jobs/{id}/events`): no job id is ever minted.
6. **Guarded status transitions / `job_events` audit trail**: by default, an inspect call
   leaves zero rows anywhere in Postgres — no audit trail of who inspected what, unless
   deliberately added (a design choice for the implementing phase, not an architecture
   given).
7. **S3 persistence / TTL cleanup**: `handleCreateJob` always uploads to S3 before creating a
   job row; inspect, being synchronous, should almost certainly never call `storage.Upload`
   at all — the request body lives only for the HTTP request's lifetime. MinIO's ILM rule
   never needs to know inspect exists.
8. **Multi-replica-safe rate limiting**: the existing per-client/IP limiter
   (`internal/ratelimit`) is explicitly documented (and referenced by `admission.go:24-27`'s
   own comment) as **only correct under `api.replicas: 1`** — unlike the Postgres-anchored
   admission gate. Since inspect deliberately bypasses that Postgres-anchored gate, *any*
   rate control placed on inspect inherits this weakness. This should be a disclosed, explicit
   limitation in whichever phase ships it, not silently assumed away.

### What the API process needs instead

- **Work ceiling**: a per-request `context.WithTimeout` at the top of the handler — inspect
  is "cheap" almost by definition, so a budget in the single-digit-to-low-tens-of-seconds
  range, not `ENGINE_TIMEOUT`'s 120s+.
- **Bounded page sampling**: the milestone text is explicit — "ограниченная выборка страниц с
  честной пометкой выборочности в отчёте." Mirrors `OCR_MAX_PAGES`'s pattern but scoped to
  inspect's own (smaller) cap, and the report JSON must carry a `sampled: bool` +
  `pages_inspected/pages_total` pair so a client can never mistake a partial scan for a
  complete one.
- **Request body size cap**: mirrors `http.MaxBytesReader(w, r.Body, s.maxUploadByte)`
  (handlers.go:118) but with its own, likely smaller, ceiling — inspect never needs to accept
  a multi-GiB video just to read its header.
- **Its own rate budget**: a dedicated per-client RPM counter scoped to `/v1/inspect` only,
  separate from the existing per-client RPM shared across `/v1/*` — so an inspect flood from
  one client cannot starve that same client's job-creation budget, or vice versa. Same
  multi-replica caveat as point 8 above applies to this new budget too.
- **No S3 write, no Postgres write** by default (see points 6-7 above).

### Does inspection need worker delegation?

`Dockerfile.api` (per CLAUDE.md's documented tech stack) is `debian:bookworm-slim` with only
`ca-certificates` — no `poppler-utils`, no `exiftool`, no `ffmpeg`/`ffprobe`. This is a
deliberate architectural choice, not an oversight: every engine's binary toolchain lives in
its *own* worker container (`Dockerfile.worker` = libvips only, `Dockerfile.document-worker`
= LibreOffice, `Dockerfile.ocr-worker` = tesseract+poppler, `Dockerfile.av-worker` = ffmpeg,
...), and the API process is the one always-on, publicly-reachable binary that today never
shells out to untrusted-input tooling at all (`os/exec` + `runCommand`'s hardened
process-group-kill discipline, `internal/convert/exec.go`, is exclusively a worker-side
concern).

Given the table above, a meaningful slice of a genuinely useful inspect report — PDF page
count, born-digital detection, audio/video duration and codecs, EXIF/GPS risk — **cannot**
run inside the API process as currently built. Two structural options, stated plainly:

- **(a) Add the missing binaries to `Dockerfile.api`.** Simplest, but directly contradicts
  the established "one engine's toolchain per worker container" isolation pattern, and adds
  `os/exec` against client-supplied bytes to the one process in this architecture that has no
  resource ceiling and runs unprivileged only nominally (the worker containers are
  `USER nobody` with explicit `cpus`/`memory` limits in `docker-compose.yml`; the API
  container's isolation posture has never had to consider executing untrusted input before).
- **(b) A genuine synchronous call into a worker.** OctoConv has no existing seam for this —
  workers today only ever consume from asynq queues, asynchronously. Building one is real new
  architecture, not a small addition.

Given the milestone's own framing (an "explicit work ceiling," not a promise of deep
inspection for every format), the pragmatic v1.13 answer is a **two-tier report**: Tier 1
fields (format, size, dimensions, encryption/OLE-CFB/macro/zip-bomb risk signals) computed
entirely from the pure-Go sniffers already available to the API process, with zero new
binaries; Tier 2 fields (page count, duration, codecs, deep PDF risk) either omitted with an
honest `"available": false`-style flag, or enabled only if a later phase adds the specific
missing binary to `Dockerfile.api` as a disclosed, deliberate exception (mirroring how
`Dockerfile.worker` already carries `libvips-tools` as its own one exception to an otherwise
binary-free API-adjacent image). Building seam (b) is a bigger, SEED-worthy undertaking for a
later milestone, not v1.13 scope.

---

## E. Born-digital PDF routing (SEED-021)

### Verifying SEED-021's specific code claims

| Claim | Verified? | Evidence |
|---|---|---|
| `pdf` is a source format in exactly one place: `ocrSourceFormats` | **Confirmed.** `internal/convert/ocr.go:34`: `ocrSourceFormats = []string{"jpg", "png", "tiff", "webp", "pdf"}`. Cross-checked via `grep -rn '"pdf"' internal/convert/*.go`: every other hit is `pdf` as a **target** (`chromium.go:19` `{html→pdf}`; `libreoffice.go:35-40,143-148` `{format→pdf}`) or a target-side opts gate (`opts.go:134`, `avopts.go:31`, `htmlopts.go:110` all check `target == "pdf"`), never a source. |
| `LibreOfficeConverter` only produces `documentFormats → pdf`, never accepts pdf as source | **Confirmed.** `internal/convert/libreoffice.go:14`: `documentFormats = []string{"docx", "odt", "xlsx", "ods", "pptx", "odp"}` — no `pdf`. |
| `detectBornDigitalPDF` rejects text-based PDFs with `ErrOCRBornDigital` | **Confirmed.** `internal/convert/ocrguards.go:224-239`, threshold `ocrBornDigitalCharThreshold = 16` non-whitespace chars via `pdftotext`. |
| The comment at `ocrguards.go:227-228` says the rejection is "steering the client to the document engine instead" — but the document engine does not accept pdf as input | **Confirmed, still present, still misleading.** `ocrguards.go:227-228` verbatim: "conservative per D-03: ANY substantial extractable text rejects, steering the client to the document engine instead." Per the two rows above, this advice points nowhere reachable — worth fixing the comment regardless of what else v1.13 does with SEED-021. |
| So born-digital `PDF → txt` has no path through this service at all | **Confirmed** — OCR refuses it, and no other engine accepts pdf as input, period. |

### Where the routing decision belongs, and what actually changes in v1.13's scope

Given `POST /v1/inspect` (§D) is the milestone's new pre-flight surface, the correct place for
the born-digital fact to first reach the client is **inspect's report**, not a change to
`EngineFor`/`Registry`. Concretely: inspect's report should include a `text_layer_present:
bool` field, computed by reusing (exporting) the existing `detectBornDigitalPDF` check, and
its `(operation, target)` recommendation list should simply **not** offer `(convert, txt)` via
OCR for a PDF where that field is true (since that job would deterministically fail
`born_digital` today) — the client learns this *before* submitting a job, rather than after
burning admission + a queue slot + a worker attempt. This satisfies the milestone's
"routed by inspect's result, not rejected blindly" framing without requiring any Registry or
`EngineFor` change, because **the set of `(from, to)` pairs the registry can resolve for pdf
does not change** — pdf still only resolves through OCR.

Whether `handleCreateJob` should *also* duplicate this check API-side (rejecting a
born-digital-PDF-to-OCR job at creation time with a structured 422, rather than relying on the
worker's existing `MarkFailed("born_digital", ...)` after admission) is constrained by the
same poppler-availability problem as §D: `pdftotext` is not in the API container. The
lower-risk, dependency-free v1.13 reading is: **inspect is what stops the routing from being
"blind"** (the client can check first), and `handleCreateJob`'s admission logic is unchanged
— the worker's existing, already-correct `ErrOCRBornDigital` → `"born_digital"` classification
(`ocrFailureCode`, worker.go:484-485) remains the enforcement backstop for a client that
submits without inspecting first. Duplicating the check API-side is a possible future
enhancement, gated on the same "does poppler belong in the API image" decision as §D, not a
v1.13 requirement as scoped.

**Adopting `pdf2md` (SEED-021 Opportunity 1) is explicitly out of v1.13's scope** as written
in PROJECT.md ("Отложено, не выбрано в v1.13" does not list SEED-021's adoption; the milestone
bullet only promises the routing/reporting improvement above) and SEED-021 itself says to
"harvest as a spike first, not a phase." If a future milestone does adopt it, `EngineFor`
would then gain a genuinely new `(pdf, md)` pair via a new converter — but that is not this
milestone's concern, and no Registry change should be built speculatively for it now.

---

## F. Structured error codes

### Where fixed strings live today

- **Synchronous API responses**: `internal/api/handlers.go:1050-1052`'s `writeError(w,
  status, msg string)` writes `{"error": msg}` with **no** machine-readable code, for roughly
  twenty distinct hardcoded string literals scattered across `handlers.go` (`"missing target
  format"`, `"unsupported conversion: X -> Y"`, `"unrecognized file content for X"`, etc.).
  The **one existing exception** is `writeQuotaError` (handlers.go:1007-1010), which does
  include `"code": "quota_exceeded"` — and its own doc comment says, verbatim: *"D-10 scopes
  it to the two 429s in this codebase... the general sweep is SEED-020"* (handlers.go:1006).
  This is a direct, pre-existing pointer in the code itself confirming where the general
  error-code work is expected to land.
- **Worker/async path**: `jobs.error_code`/`jobs.error_message` columns
  (0001_init.sql:59-60) already exist and are **already** populated with a real taxonomy —
  `"engine_error"`, `"invalid_options"`, `"duration_exceeded"`, `"timecode_out_of_range"`,
  `"resolution_exceeded"`, `"no_video_stream"`, `"born_digital"`, `"too_many_pages"`,
  `"page_too_large"`, `"image_too_large"`, `"unsupported_language"`, `"unsupported_target"`,
  `"input_too_large"`, `"reconciler_exhausted"` — every one of these already flows through
  `GET /v1/jobs/{id}`'s `error_code` field (handlers.go:872-874) and the terminal webhook
  body's `error_code` field (worker.go:1599-1601).
- **MCP servers**: `internal/mcpserver/client.go:100-101` already carries `ErrorCode
  string \`json:"error_code,omitempty"\`` / `ErrorMessage string` as typed DTO fields — MCP
  already consumes the async path's codes **structurally**, not by text-matching.

**Correction to the milestone framing:** "фронт и оба MCP матчат текстом" is accurate for the
**synchronous** `writeError` surface, but the **async** job-failure surface is already
structured and already consumed structurally by MCP. The real gap F needs to close is
narrower and more precise than the milestone bullet implies: extend the same `error_code`
treatment the async path already has to the ~20 synchronous `writeError` call sites (e.g.
`writeError(w, status, code, msg string)`, or a sibling function, with a fixed code constant
assigned per call site).

### Verify/refute SEED-020's claim that error codes belong inside JobRunner

**Partially true, partially misleading — both halves stated precisely:**

- **True** for the async-path half: `avFailureCode`, `ocrFailureCode`, `pandocFailureCode`,
  and audio's inline duration-exceeded branch are the async side's structured-error-code
  implementation, and they already live directly inside (and are called from) each
  `Handle*Convert`'s terminal branch. Consolidating the seven handlers naturally
  consolidates these five slightly-divergent mapper functions into one
  `EngineDescriptor.FailureCode func(error) (code, message string)` field (§A) — doing this
  as a separate earlier phase really would mean editing the same seven sites twice, exactly
  as SEED-020 argues.
- **Not sufficient on its own** for the synchronous-path half: the ~20 `writeError` call
  sites live in a different package (`internal/api`, not `internal/worker`), on a completely
  different kind of code path (request validation, not terminal-transition classification).
  JobRunner consolidation, scoped to `internal/worker/worker.go`, structurally cannot touch
  them. The roadmap should not read "error codes belong inside JobRunner" as "one phase closes
  the whole error-code gap" — it closes the async half; the synchronous half needs its own
  (much cheaper — one package, no ×7 duplication) sweep, which can reasonably be a second
  workstream in the *same phase* as JobRunner (satisfying SEED-020's "not before it, not as a
  separate earlier phase" intent) without pretending it's the same code change.

### Verifying the `invalid_options` metrics gap

Confirmed by direct reading: every `MarkFailed(ctx, jobID, "invalid_options", ...)` call site
(document worker.go:833, html:944, audio:1060, av:1223, ocr:1341, pandoc:1463 — six sites;
image has none, since it has no opts re-parse guard) is followed immediately by the
`SkipRetry` return with **no** `metrics.RecordJobOutcome`/`RecordClientJobOutcome` call
anywhere between the `MarkFailed` and the `return`. `recordUsage` still fires (it's inside
`MarkFailed`'s own `transition`/`apply` closure, engine-agnostic), so the Postgres rollup is
intact — only the two Prometheus counters miss these outcomes, exactly as SEED-020 states.
This is a natural side-effect of the runner design in §A: once there is exactly one
post-commit metrics call site per engine (reached for every `MarkFailed`, not just the
"engine ran and then failed" branch), this gap closes as a consequence of the refactor rather
than as a separate fix.

---

## G. Suggested build order

```
Phase 56  A: JobRunner consolidation (worker-side)
              + F (async half): FailureCode folded into EngineDescriptor
              + fix reconciler.go:311-312's unguarded RecordReconcilerAction("exhausted")
          |
          | (no hard dependency FROM 56, but everything downstream benefits from
          |  landing on the descriptor-driven runner before adding an 8th handler)
          v
Phase 57  B: operation dimension
              - migration: widen jobs_operation_check (+ presets.operation, if symmetric)
              - convert.Registry: OperationalConverter optional interface, LookupOp/SupportsOp/EngineForOp
              - handleCreateJob: accept `operation` form field, default "convert"
              - process(): read job.Operation instead of assuming convert
            C: output roles (small, bundled — same "additive schema + registry plumbing,
               no client-visible behavior yet" shape as B)
              - migration: job_outputs.role, DEFAULT 'primary'
              - jobs.Output/AddOutput: thread Role through (unused until sanitize)
          |
          v
Phase 58  D: synchronous POST /v1/inspect
              - internal/inspect/ package (Tier 1: pure-Go sniffers only)
              - internal/api/inspect_handlers.go
              - work ceiling, body cap, dedicated rate budget (disclosed multi-replica caveat)
              - (depends on B for "available (operation,target) pairs" reporting to be
                 meaningful; does not depend on C)
          |
          v
Phase 59  E: born-digital routing via inspect
              - export/reuse detectBornDigitalPDF from internal/inspect
              - inspect report: text_layer_present field + pair-recommendation suppression
              - fix the misleading ocrguards.go:227-228 comment regardless
              - (hard dependency on D existing; no Registry/EngineFor change)
          |
          v
Phase 60  sanitize: first async non-convert operation
              - migration: widen jobs_engine_check is UNNEEDED (probe already reserved);
                only jobs_operation_check needed (done in 57) plus a value for "sanitize"
                if not already covered — VERIFY at execution time whether "sanitize" needs
                its own enum value distinct from "extract" (0001's five values do not
                include "sanitize" literally; this needs a decision, not just a migration)
              - new SanitizeConverter (or similarly named), engine=probe, implementing
                OperationalConverter.Operation()=="sanitize"
              - new queue.TypeProbeConvert/QueueProbe + AllConvertQueues() entry
              - new cmd/probe-worker (own toolchain: macro/JS/attachment strip, EXIF/GPS/IPTC/XMP)
              - Enqueuer.EnqueueProbeConvert + reconciler.go/handlers.go engine-switch cases
              - new deploy/chart/octoconv/templates/scaledobject-probe.yaml (additive, zero
                diff to the existing 7)
              - uses the Phase-56 EngineDescriptor pattern directly — this becomes the FIRST
                new engine class added the "right way," not an 8th hand-copied handler
              - second AddOutput call with role='report' (uses C)
```

**Independent hygiene tail** (per the milestone's own framing — "не шумящий в диффе основной
работы"): the three v1.12 debt items touch files with **zero overlap** against A–F's file
set, and can run in any phase slot, or as their own tiny phase, without blocking or being
blocked by anything above:

- **52 WR-01** (stuck in-flight job burns daily budget forever, no operator relief besides raw
  SQL) — touches `internal/jobs/admission.go` and possibly a new `cmd/manage-clients` verb;
  does not touch `internal/worker`, `internal/convert`, or `internal/api/handlers.go`.
- **52 WR-09** (self-contradictory quota section in README) — touches only `README.md`.
- **`PurgeUsage`'s Go-clock cutoff** — confirmed live at `internal/jobs/retention.go:126`:
  `cutoff := time.Now().UTC().Add(-olderThan)`, while every other v1.12-era time-sensitive
  read (`admission.go`'s `evaluate`, `usage.go`'s `recordUsage`) derives "today" from
  Postgres's own `now() AT TIME ZONE 'utc'` specifically to avoid host/DB clock skew — this
  is a one-line fix (`SELECT now() AT TIME ZONE 'utc'` bound as the cutoff basis, or push the
  `< $1` comparison into a `(now() AT TIME ZONE 'utc') - $1::interval` SQL expression) confined
  to `retention.go`.

Recommended slot: fold these three into whichever phase has the lightest diff otherwise (56
or 61 as a standalone tail), per the milestone's own Phase-54 precedent of shipping hygiene
debt as an explicitly separate, non-noisy phase.

---

## Integration Points (file paths)

### New components

| Path | Purpose |
|---|---|
| `internal/inspect/` (new package) | Synchronous inspection logic: composes `internal/convert` sniffers, computes risk signals, builds the report, exposes `text_layer_present` for born-digital routing |
| `internal/api/inspect_handlers.go` | `POST /v1/inspect` HTTP adapter over `internal/inspect` |
| `internal/worker/runner.go` (or similar) | `EngineDescriptor` type + `Runner` replacing the seven `Handle*Convert` methods |
| `internal/db/migrations/0015_*.sql` (or next number) | Widen `jobs_operation_check`; add `job_outputs.role` |
| `internal/db/migrations/0016_*.sql` (or next) | Reserve/confirm `sanitize` operation value once decided (§G Phase 60 open question) |
| `cmd/probe-worker/` (new binary) | Sanitize engine worker (macro/JS/attachment strip, EXIF/GPS/IPTC/XMP scrub) |
| `deploy/chart/octoconv/templates/scaledobject-probe.yaml` | 8th ScaledObject, additive |

### Modified components

| Path | Change |
|---|---|
| `internal/worker/worker.go` | Seven `Handle*Convert` collapse into `Runner.Handle` + `EngineDescriptor` table; `isXTerminal`/`*FailureCode` functions relocate into descriptors, logic unchanged |
| `internal/reconciler/reconciler.go` | Fix unguarded `RecordReconcilerAction("exhausted")` at line 311-312; add `probe` case to the engine-routing switch (line ~342-369) once sanitize ships |
| `internal/convert/convert.go` | `OperationalConverter` optional interface; `Pair` gains `Operation`; `LookupOp`/`SupportsOp`/`EngineForOp` added, existing signatures kept as convert-defaulting wrappers |
| `internal/api/handlers.go` | `handleCreateJob` accepts `operation` form field (default `operationConv`); `writeError` gains a code parameter across ~20 call sites; engine-routing switch (line ~770-792) gains `probe` case |
| `internal/api/formats_handlers.go` | `Classes()`/`handleListFormats` gain an additive `operations` key, `engines` key stays convert-only for compatibility |
| `internal/jobs/repo.go` | `jobs.Output`/`AddOutput` thread `Role` through; `Repo.Get`/`Create` already handle `Operation` — no change needed there |
| `internal/jobs/retention.go` | `PurgeUsage` cutoff moves from Go clock to Postgres clock (hygiene tail) |
| `internal/jobs/admission.go` | Operator-relief mechanism for stuck in-flight jobs (52 WR-01, hygiene tail) — no change needed for B/C/D/E, since admission is already engine-only/operation-agnostic |
| `internal/queue/queue.go` | New `TypeProbeConvert`/`QueueProbe` consts, `NewProbeConvertTask`, `ProbeUniqueTTL`; `AllConvertQueues()` gains one entry |
| `README.md` | Fix self-contradictory quota section (52 WR-09, hygiene tail) |
| `internal/convert/ocrguards.go` | Fix misleading "steering the client to the document engine" comment at line 227-228 (independent of whether pdf2md is ever adopted) |

---

## Sources

Every claim above is grounded in a file read during this research session:
`.planning/PROJECT.md`, `.planning/seeds/SEED-020.md`, `.planning/seeds/SEED-021.md`,
`.planning/seeds/SEED-013.md`, `internal/worker/worker.go` (full, 1990 lines),
`internal/jobs/repo.go`, `internal/jobs/admission.go`, `internal/jobs/usage.go`,
`internal/jobs/retention.go`, `internal/convert/convert.go`, `internal/convert/converters.go`,
`internal/convert/ocrguards.go`, `internal/convert/ocr.go` (grep), `internal/convert/libreoffice.go`
(grep), `internal/api/handlers.go` (full), `internal/api/api.go`, `internal/api/formats_handlers.go`,
`internal/queue/queue.go` (full), `internal/metrics/metrics.go` (full),
`internal/reconciler/reconciler.go` (full), `internal/db/migrations/0001_init.sql` through
`0014_client_usage_canceled.sql`, `deploy/chart/octoconv/templates/scaledobject-image.yaml`,
`cmd/*/main.go` (grep for handler registration).

No web research was performed for this pass — the question is entirely about this specific
codebase's current state, which only direct file reading can answer.

---
*Architecture research for: OctoConv v1.13 "Inspect & Operations" (subsequent milestone, phases continuing from 56)*
*Researched: 2026-08-07*
