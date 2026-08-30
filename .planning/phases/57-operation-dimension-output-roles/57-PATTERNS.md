# Phase 57: Operation Dimension & Output Roles - Pattern Map

**Mapped:** 2026-08-08
**Files analyzed:** 15
**Analogs found:** 15 / 15

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|--------------------|------|-----------|-----------------|----------------|
| `internal/db/migrations/00NN_operation_and_output_roles.sql` | migration | batch (DDL) | `internal/db/migrations/0009_ocr_engine.sql` + `0010_pandoc_engine.sql` (CHECK extension) + `internal/db/migrations/0012_output_identity_and_retention_index.sql` (additive column/index style) | exact |
| `internal/convert/convert.go` (`OperationalConverter`, triple-keyed `Registry`, `Register`/`Replace`, `LookupOp`/`SupportsOp`/`EngineForOp`) | service (registry) | CRUD (in-memory lookup table) | `internal/convert/progress.go:39-49` (`ProgressConverter` optional-interface precedent) + `internal/convert/convert.go:110-172` itself (extend in place) | exact |
| `internal/convert/converters.go` | config (registration wiring) | event-driven (`init()`) | itself, unchanged except doc comment (D-06: zero diff to the seven `Default.Register(...)` calls) | exact |
| `cmd/av-worker/main.go` (`Register` → `Replace`) | config (worker bootstrap) | event-driven | `cmd/av-worker/main.go:81` itself | exact |
| `cmd/ocr-worker/main.go` (`Register` → `Replace`) | config (worker bootstrap) | event-driven | `cmd/ocr-worker/main.go:90` itself | exact |
| `cmd/pandoc-worker/main.go` (`Register` → `Replace`) | config (worker bootstrap) | event-driven | `cmd/pandoc-worker/main.go:93` itself | exact |
| `internal/api/handlers.go` (`handleCreateJob`: `operation` field, `EngineForOp`, `unsupported_operation`) | controller (HTTP handler) | request-response | itself, `handlers.go:133-135` (form-field read) + `handlers.go:394-403` (`EngineFor` + 422 fail-closed pattern) | exact |
| `internal/api/handlers.go` (`handleGetJob`: role-driven `outputs[]`/`download_url`) | controller (HTTP handler) | request-response | itself, `handlers.go:857-871` (current `outs[0]` presign) + `handlers.go:802-805`/`872-878` (additive-key-guarded-on-nonempty idiom) | exact |
| `internal/api/formats_handlers.go` (`operations` tree key) | controller (HTTP handler) | request-response | itself (extend `Classes()`-derived rendering) | exact |
| `internal/convert/convert.go` (`Classes()` → operation-aware walk for `/v1/formats`) | service | transform | itself, `convert.go:150-172` | exact |
| `internal/jobs/repo.go` (`Output.Role`, `Outputs` SELECT, `AddOutput` upsert, `MarkDone` primary-output invariant) | model + repository | CRUD | itself, `repo.go:393-424` (`AddOutput`) + `repo.go:533-558` (`Outputs`) + `repo.go:204-212` (`MarkDone`/`transition`) | exact |
| `internal/jobs/jobs.go` (`Output.Role` field) | model | CRUD | itself, `jobs.go:69-77` | exact |
| `internal/mcpserver/client.go` (`CreateJob` operation param) | service (HTTP client wrapper) | request-response | itself, `client.go:160-206` (`CreateJob`, `target`/`preset`/`opts` optional-field pattern) | exact |
| `internal/mcpserver/tools.go` (`ConvertInput.Operation` field) | controller (MCP tool schema) | request-response | itself, `tools.go:66-70` (`ConvertInput` struct tags) | exact |
| `frontend/src/api.ts` (`createJob` operation param) | service (fetch wrapper) | request-response | itself, `api.ts:85-107` (`createJob`) | exact |
| `internal/convert/pairs_disjoint_test.go` (new operation-triple disjointness test, incl. `pdf→pdf` identity case) | test | batch (pure assertions) | itself, all four existing tests (`TestAVAudioPairDisjointness`, `TestOCRPairsDisjointFromOtherEngines`, `TestPandocPairsDisjointFromOtherEngines`) | exact |
| `internal/api/formats_handlers_test.go` / new golden test file (D-17 pinning) | test | batch (golden/pinning) | `internal/api/formats_handlers_test.go:13-47` (httptest harness) + `internal/worker/pinning_test.go:1-70` (pinning-suite doc-comment convention) + `internal/api/system_usage_handlers_test.go` (`byte-identical` assertion idiom) | exact |
| `docs/ERROR-CODES.md` (`unsupported_operation` row) | config (doc) | transform | itself, existing `unsupported_conversion`/`unknown_preset` rows | exact |

## Pattern Assignments

### `internal/db/migrations/00NN_operation_and_output_roles.sql` (migration, batch/DDL)

**Analogs:** `internal/db/migrations/0009_ocr_engine.sql`, `internal/db/migrations/0010_pandoc_engine.sql`, `internal/db/migrations/0012_output_identity_and_retention_index.sql`

**CHECK-extension pattern** (`0010_pandoc_engine.sql:13-15`, identical shape in `0009`):
```sql
ALTER TABLE jobs DROP CONSTRAINT jobs_engine_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_engine_check
    CHECK (engine IN ('image', 'document', 'av', 'cad', 'archive', 'probe', 'html', 'audio', 'ocr', 'pandoc'));
```
Apply the identical `DROP CONSTRAINT` / `ADD CONSTRAINT` shape to `jobs_operation_check` (auto-generated name for the inline unnamed CHECK in `0001_init.sql:45-46`), extending `'convert', 'extract', 'archive', 'inspect', 'render'` to also include `'sanitize'` (D-15). Every prior migration in this family opens with a comment block explaining WHY the constraint name is safe to hardcode (Postgres's `<table>_<column>_check` auto-naming, already confirmed live by three prior migrations) — reuse that same comment, updated to reference this migration in the chain.

**Additive-column-with-DB-default pattern** — no exact prior migration adds a `NOT NULL ... DEFAULT` column to an existing populated table, but `0001_init.sql:89-101` (`job_outputs` table definition) is the base to extend, and `0012_output_identity_and_retention_index.sql`'s header comment (lines 1-45) is the closest analog for *documenting* an additive, backfill-free change with a rationale for why existing rows stay valid without a data migration. Use:
```sql
ALTER TABLE job_outputs
    ADD COLUMN role text NOT NULL DEFAULT 'primary'
    CHECK (role IN ('primary', 'report', 'preview', 'thumbnail'));
```
D-16 requires a comment (and a Go test — see `internal/jobs/repo_test.go` below) proving this default applies to a row inserted via `AddOutput` without a Go-side role value, verified on Postgres 18, not assumed from general Postgres knowledge.

D-15 also calls for an explicit comment noting the intentional non-backfill divergence: `sanitize` becomes a legal DB value with an empty registry behind it (D-02) until Phase 60 registers a converter for it — mirror the "additive only" framing used in `0009_ocr_engine.sql:9-12` ("no free-text engine value is ever admitted outside this closed allow-list").

`presets.operation` (`0001_init.sql:22-23`) is explicitly OUT of scope for this migration (D-15) — do not touch it; a one-line comment noting the deliberate non-symmetry belongs in the migration file per D-15's "planner should close it with a comment" note.

---

### `internal/convert/convert.go` — `OperationalConverter` + triple-keyed `Registry` (service, CRUD/lookup)

**Analog:** `internal/convert/progress.go:39-49` (`ProgressConverter`, optional-interface precedent)

**Optional-interface pattern to copy verbatim in shape** (`progress.go:39-49`):
```go
// ProgressConverter is an OPTIONAL capability a Converter may additionally
// implement (D-01) -- added ALONGSIDE Converter, never as a change to it.
// Converters with no native machine-readable progress signal (libvips,
// LibreOffice, Chromium, whisper-cli's transcribe stage) do not implement
// this; the worker type-asserts for it and falls back to the plain
// Convert(ctx, in, out, opts) path when absent.
type ProgressConverter interface {
	// ConvertWithProgress behaves like Convert but additionally reports
	// progress to sink as the conversion runs.
	ConvertWithProgress(ctx context.Context, inPath, outPath string, opts map[string]any, sink ProgressSink) error
}
```
`OperationalConverter` (D-06) follows the identical shape: `type OperationalConverter interface { Operation() string }`. `Register` type-asserts (`if oc, ok := c.(OperationalConverter); ok { op = oc.Operation() } else { op = "convert" }`) exactly as the worker type-asserts for `ProgressConverter` — locate that call site for the mirrored type-assert idiom (`internal/worker/worker.go`, `runConvertWithOptionalProgress`, grep `ProgressConverter` there for the exact `if pc, ok := conv.(ProgressConverter); ok` shape to mirror).

**Existing registry/lookup pattern to extend (not replace)** (`convert.go:110-148`):
```go
type Registry struct {
	m map[Pair]Converter
}

func (r *Registry) Register(c Converter) {
	for _, p := range c.Pairs() {
		r.m[Pair{From: NormalizeFormat(p.From), To: NormalizeFormat(p.To)}] = c
	}
}

func (r *Registry) Lookup(from, to string) (Converter, bool) {
	c, ok := r.m[Pair{From: NormalizeFormat(from), To: NormalizeFormat(to)}]
	return c, ok
}

func (r *Registry) Supports(from, to string) bool {
	_, ok := r.Lookup(from, to)
	return ok
}

func (r *Registry) EngineFor(from, to string) (string, bool) {
	c, ok := r.Lookup(from, to)
	if !ok {
		return "", false
	}
	return c.Engine(), true
}
```
D-07: `Registry.m` re-keys on a triple (operation, from, to); `Lookup`/`Supports`/`EngineFor` keep their existing two-arg signatures and become one-line delegations to new `*Op` variants passing `"convert"` — e.g. `func (r *Registry) Lookup(from, to string) (Converter, bool) { return r.LookupOp("convert", from, to) }`. This preserves the ~30+ existing call sites untouched (the doc-comment style already established above should carry over to explain why the two-arg form still exists).

**Panic-on-collision pattern (D-08) — no existing analog; net-new discipline.** The nearest structural precedent for "hard-fail at process start on a data invariant violation" in this codebase is `log.Fatalf` at `cmd/*/main.go` init time (per CLAUDE.md Logging conventions: `log.Fatalf` only at startup for unrecoverable init errors) — but `Register` runs from `init()` inside `internal/convert`, a library package that CLAUDE.md says never logs. Use a bare `panic(fmt.Sprintf(...))` inside `Register` on a triple-key collision (mirrors Go's own stdlib convention for `init()`-time invariant violations, e.g. `regexp.MustCompile`-style `Must*` naming already anticipated for `Replace`/`MustReplace` in Claude's Discretion). Reference the existing collision-avoidance comments in `converters.go:9-59` (each `Default.Register(...)` call already documents WHY collision cannot happen today, backed only by the four `pairs_disjoint_test.go` tests) — this doc-comment discipline should be preserved/adapted to describe the NEW panic backstop, not removed.

**`Classes()` extension for `/v1/formats`** (`convert.go:150-172`):
```go
func (r *Registry) Classes() map[string][]Pair {
	out := make(map[string][]Pair)
	for pair, c := range r.m {
		class := c.Engine()
		out[class] = append(out[class], pair)
	}
	...
	return out
}
```
D-10 requires the new `Registry.m` walk (grouped by operation) to be BYTE-IDENTICAL for the `engines` key (still grouped by `Operation == "convert"` pairs only) while adding a new `operations` (or Claude's-discretion-named) key with the full `{operation → engine → pairs}` tree. Both walks read the SAME underlying map, so this is the single (only) place `r.m` is iterated (per the existing comment at `convert.go:150-154`: "no engine/pair string literal exists anywhere else in the codebase (this is the only walk of r.m)") — that invariant comment must be preserved/updated, not silently dropped.

---

### `internal/convert/converters.go` (config, event-driven `init()`)

**Analog:** itself — zero diff expected (D-06)

No excerpt needed; D-06 explicitly promises the seven `Default.Register(...)` calls in `converters.go:5-60` need ZERO changes. The planner's only obligation here is to confirm no plan touches this file, and if `sanitize`/`probe` registration is deferred to Phase 60 (it is, per D-02), this file stays untouched in Phase 57.

---

### `cmd/av-worker/main.go`, `cmd/ocr-worker/main.go`, `cmd/pandoc-worker/main.go` (config, event-driven)

**Analog:** themselves, one-line change each

Current call sites to change from `Register` to `Replace` (D-09):
```go
// cmd/av-worker/main.go:81
convert.Default.Register(convert.AVConverter{
    ...
})

// cmd/ocr-worker/main.go:90
convert.Default.Register(buildOCRConverter(ocrMaxPages, ocrMaxPixels))

// cmd/pandoc-worker/main.go:93
convert.Default.Register(buildPandocConverter(pandocMaxInputBytes))
```
Each becomes `convert.Default.Replace(...)` (or whatever discretionary name is chosen) with the exact same argument — a pure rename at these three call sites, per D-09 ("one line per worker").

---

### `internal/api/handlers.go` — `operation` field intake (controller, request-response)

**Analog:** itself — `handlers.go:26-33` (const block), `handlers.go:130-155` (preset/target read-and-normalize-before-branching), `handlers.go:394-403` (`EngineFor` + fail-closed 422)

**Form-field constant pattern** (`handlers.go:26-33`):
```go
const (
	formFieldFile            = "file"
	formFieldTarget          = "target"
	formFieldCallbackURL     = "callback_url"
	formFieldProgressWebhook = "progress_webhook"
	formFieldOpts            = "opts"
	formFieldPreset          = "preset"
	operationConv            = "convert"
	...
)
```
Add `formFieldOperation = "operation"` alongside the existing field constants (D-01, working name confirmed by CONTEXT.md).

**Normalize-before-any-branching pattern** (`handlers.go:133-136`, mirrored for D-04):
```go
presetName := r.FormValue(formFieldPreset)
rawTarget := r.FormValue(formFieldTarget)
rawOpts := r.FormValue(formFieldOpts)
usingPreset := presetName != ""
```
D-04 requires: `operation := r.FormValue(formFieldOperation); if operation == "" { operation = operationConv }` — placed immediately after the `FormValue` call, BEFORE any other branch, so "field absent" and "field == convert" become physically indistinguishable from that line onward. `operationConv` already exists (`handlers.go:33`, currently used only at line 731) — reuse it as the default literal, do not introduce a second `"convert"` string constant.

**Fail-closed 422-before-S3-and-before-Create pattern** (`handlers.go:394-403`):
```go
// Validate the conversion pair BEFORE writing anything to storage, and
// derive the engine class in the same step (D-01/D-02). The DETECTED
// format (not the extension-derived one) is the source of truth fed into
// the pair-check (D-05).
engine, ok := convert.Default.EngineFor(detected, target)
if !ok {
	writeError(w, http.StatusUnprocessableEntity, errcode.UnsupportedConversion,
		"unsupported conversion: "+detected+" -> "+target)
	return
}
```
D-03/D-06/D-07: this call site is one of exactly two that move to the `*Op` variant (`EngineForOp(operation, detected, target)`). Two-code fail-closed split per D-03:
1. First check operation validity: `if !convert.Default.IsKnownOperation(operation)` (or equivalent registry-derived check — D-02, NOT a DB CHECK, NOT a Go allowlist) → 422 `errcode.UnsupportedOperation` ("no such operation" class).
2. Then, as today, `engine, ok := convert.Default.EngineForOp(operation, detected, target)` → 422 `errcode.UnsupportedConversion` if the triple is unsupported (operation known, pair not registered for it).

Both checks MUST run before `s.storage.Upload` (which happens later in the handler, after this point) and before `s.repo.Create` (`handlers.go:728`) — this call site already sits before both, so inserting the new check immediately before/alongside it preserves the existing ordering guarantee.

**Passing `operation` into `Create`** (`handlers.go:728-748`, specifically line 731):
```go
createdID, err := s.repo.Create(ctx, jobs.CreateParams{
	ID:              jobID,
	ClientID:        client.ID,
	Operation:       operationConv,
	Engine:          engine,
	...
})
```
Change `Operation: operationConv` to `Operation: operation` (the normalized local variable from D-04) — this is `handlers.go:731`, the single write site the research doc (§B) already identified as the only place `operation` is persisted.

---

### `internal/api/handlers.go` — `handleGetJob` role-driven outputs (controller, request-response)

**Analog:** itself — `handlers.go:857-871` (current unconditional `outs[0]`), `handlers.go:802-805` / `872-878` (additive-key-guarded-on-nonempty idiom already established for `opts`/`error_code`/`error_message`)

**Current defect to replace** (`handlers.go:857-871`):
```go
switch job.Status {
case jobs.StatusDone:
	outs, err := s.repo.Outputs(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to load outputs")
		return
	}
	if len(outs) > 0 {
		url, err := s.storage.PresignGet(ctx, outs[0].ObjectKey, s.presignTTL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to presign output")
			return
		}
		resp["download_url"] = url
	}
```
D-11/D-12/D-13: replace the blind `outs[0]` with a role-driven find-primary (`for _, o := range outs { if o.Role == "primary" { ... } }`), eager-presign EVERY output with the SAME `s.presignTTL` (D-12), and build `resp["outputs"]` ONLY when `len(outs) > 1` (D-11's stated consequence, matching the established additive-key idiom below).

**Additive-key-guarded-on-nonempty idiom to copy exactly** (`handlers.go:802-805`, `872-878`):
```go
if len(normalizedOpts) > 0 {
	resp["opts"] = normalizedOpts
}
```
```go
case jobs.StatusFailed:
	if job.ErrorCode != "" {
		resp["error_code"] = job.ErrorCode
	}
	if job.ErrorMessage != "" {
		resp["error_message"] = job.ErrorMessage
	}
```
D-11's `outputs[]` key follows this exact "aggregate populated, add key only if non-trivial" shape: `if len(outs) > 1 { resp["outputs"] = renderedOutputs }` — never present for a single-output convert job, so `SC4`'s byte-identical guarantee holds by construction (a missing map key, not an empty array).

**Note on D-13**: since `MarkDone` (see `internal/jobs/repo.go` section below) refuses to commit `done` without exactly one `primary` output, `handleGetJob`'s `StatusDone` branch can assume a primary always exists once reached — no new "primary missing" branch needs to be added to the handler itself (this is the point of D-13: "API тогда не ветвится вообще").

---

### `internal/api/formats_handlers.go` (controller, request-response)

**Analog:** itself, `formats_handlers.go:20-33`

```go
func (s *Server) handleListFormats(w http.ResponseWriter, r *http.Request) {
	classes := convert.Default.Classes()

	engines := make(map[string]engineFormats, len(classes))
	for class, pairs := range classes {
		rendered := make([][2]string, 0, len(pairs))
		for _, p := range pairs {
			rendered = append(rendered, [2]string{p.From, p.To})
		}
		engines[class] = engineFormats{Pairs: rendered}
	}

	writeJSON(w, http.StatusOK, map[string]any{"engines": engines})
}
```
D-10: add a second registry-derived walk producing the `operations` (working name) tree, and add it as a SECOND key in the same `map[string]any{...}` literal passed to `writeJSON` — `"engines"` stays computed by the exact same `Classes()`-derived loop above (byte-identical), `"operations"` is a new key built from the new operation-aware registry method. No existing line in this function should need to change; only new lines are added above the final `writeJSON` call.

---

### `internal/jobs/jobs.go` — `Output.Role` (model, CRUD)

**Analog:** itself, `jobs.go:69-77`

```go
// Output is a row of the job_outputs table.
type Output struct {
	Ordinal     int
	ObjectKey   string
	Filename    string
	Format      string
	SizeBytes   int64
	ContentType string
}
```
Add `Role string` as a new field, following the existing PascalCase/no-tag convention (this struct has no `json`/`db` tags anywhere in the file — scanning is done positionally in `repo.go`, not via a tag-driven mapper).

---

### `internal/jobs/repo.go` — `AddOutput`, `Outputs`, `MarkDone` (repository, CRUD)

**Analog:** itself — `repo.go:393-424` (`AddOutput`), `repo.go:533-558` (`Outputs`), `repo.go:196-212` (`MarkDone` + the `transition` helper it calls)

**`AddOutput` upsert pattern to extend** (`repo.go:409-424`):
```go
func (r *Repo) AddOutput(ctx context.Context, jobID uuid.UUID, o Output) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO job_outputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (job_id, ordinal) DO UPDATE SET
			object_key   = EXCLUDED.object_key,
			filename     = EXCLUDED.filename,
			format       = EXCLUDED.format,
			size_bytes   = EXCLUDED.size_bytes,
			content_type = EXCLUDED.content_type`,
		jobID, o.Ordinal, o.ObjectKey, o.Filename, o.Format, o.SizeBytes, o.ContentType)
	if err != nil {
		return fmt.Errorf("insert job_output: %w", err)
	}
	return nil
}
```
D-16: `role` should NOT be added to the INSERT column list or the `ON CONFLICT ... DO UPDATE SET` clause unless `o.Role` is actually populated by every one of the seven `AddOutput(...)` call sites in `internal/worker/worker.go` (currently only one call site at `worker.go:1317-1326`, since Phase 56's `Runner` consolidation collapsed all seven engines through the same `process()` — confirm this is truly the single call site before deciding whether `role` needs a Go-side default or can rely purely on the DB default per D-16 "DB defaults are the source of truth, not the Go layer"). The research/CONTEXT explicitly wants the DB DEFAULT to be what makes existing/omitted-role inserts read back as `'primary'` — prefer leaving `role` OUT of this INSERT'S column list entirely for callers that don't set it, letting the column default fire, per D-16's own required test.

**`Outputs` SELECT pattern to extend** (`repo.go:533-558`):
```go
func (r *Repo) Outputs(ctx context.Context, jobID uuid.UUID) ([]Output, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT ordinal, object_key, filename, format, size_bytes, content_type
		FROM job_outputs WHERE job_id = $1 ORDER BY ordinal`, jobID)
	if err != nil {
		return nil, fmt.Errorf("query outputs: %w", err)
	}
	defer rows.Close()

	var out []Output
	for rows.Next() {
		var o Output
		var key, name, format, ct *string
		var size *int64
		if err := rows.Scan(&o.Ordinal, &key, &name, &format, &size, &ct); err != nil {
			return nil, fmt.Errorf("scan output: %w", err)
		}
		o.ObjectKey, o.Filename, o.Format, o.ContentType = deref(key), deref(name), deref(format), deref(ct)
		if size != nil {
			o.SizeBytes = *size
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
```
Add `role` to the `SELECT` column list and `Scan` call (role is `NOT NULL`, so it scans directly into a plain `string`, matching the `optsJSON []byte` non-pointer convention at `repo.go:433` for NOT-NULL columns — do NOT use the `*string` + `deref()` pattern used for the genuinely-nullable columns in this same function). Per Claude's Discretion (open question), consider whether `ORDER BY ordinal` needs `role` as a tiebreaker — current `job_outputs_job_ordinal_key UNIQUE (job_id, ordinal)` constraint (`0012_output_identity_and_retention_index.sql:53-54`) already makes `(job_id, ordinal)` unique, so `ordinal` alone is already a stable sort key; the planner should treat this as genuinely open per D-18's Claude's Discretion note, not a forced change.

**`MarkDone` primary-output invariant (D-13)** (`repo.go:196-212`, and the `transition` helper it calls at `repo.go:632-679`):
```go
func (r *Repo) MarkDone(ctx context.Context, id uuid.UUID, attempt time.Duration) error {
	return r.transition(ctx, id, StatusDone, []string{StatusActive}, nil, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE jobs SET status = 'done', finished_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		return r.recordUsage(ctx, tx, id, StatusDone, attempt)
	})
}
```
D-13 requires the precondition to live INSIDE this `apply` closure (the `func(ctx context.Context, tx pgx.Tx) error { ... }` passed to `transition`), NOT inside `transition` itself (`repo.go:632-679`) — `transition` is explicitly documented in CONTEXT.md as untouchable machinery. Add a `SELECT count(*) FROM job_outputs WHERE job_id = $1 AND role = 'primary'` (or equivalent) query INSIDE the closure, using the SAME `tx` (so it observes the row lock already taken by `transition`'s `SELECT ... FOR UPDATE` at `repo.go:654-656`), and `return fmt.Errorf(...)` (non-wrapped, matching the existing "illegal transition" error convention at `repo.go:664`: `fmt.Errorf("illegal transition %s -> %s for job %s", from, to, id)`) if the count isn't exactly 1. This keeps `transition`'s signature and body completely unchanged — only `MarkDone`'s own closure grows a query before its two existing `tx.Exec` calls.

---

### `internal/mcpserver/client.go` — `CreateJob` operation param (service, request-response)

**Analog:** itself, `client.go:21-24` (form-field consts), `client.go:166-200` (`CreateJob` body)

```go
const (
	formFieldFile   = "file"
	formFieldTarget = "target"
	formFieldPreset = "preset"
	formFieldOpts   = "opts"
)
...
func (c *Client) CreateJob(ctx context.Context, filePath, target, preset, opts string) (string, error) {
	...
	if target != "" {
		if err := mw.WriteField(formFieldTarget, target); err != nil {
			return "", fmt.Errorf("build multipart request: %w", err)
		}
	}
	if preset != "" {
		if err := mw.WriteField(formFieldPreset, preset); err != nil {
			return "", fmt.Errorf("build multipart request: %w", err)
		}
	}
	if opts != "" {
		if err := mw.WriteField(formFieldOpts, opts); err != nil {
			return "", fmt.Errorf("build multipart request: %w", err)
		}
	}
```
D-05: add `formFieldOperation = "operation"` to the const block and a new `operation string` parameter to `CreateJob` (and its callers in `tools.go`), with the identical `if operation != "" { mw.WriteField(formFieldOperation, operation) }` guarded-write shape — an empty string is simply omitted, matching this file's existing "forward whatever the caller supplies without arbitrating" comment at `client.go:162-165`.

---

### `internal/mcpserver/tools.go` — `ConvertInput.Operation` (controller/MCP schema, request-response)

**Analog:** itself, `tools.go:66-70`

```go
type ConvertInput struct {
	...
	TargetFormat string         `json:"target_format,omitempty" jsonschema:"desired output format, e.g. jpg/png/webp -- provide exactly one of target_format or preset, never both, never neither"`
	Preset       string         `json:"preset,omitempty" jsonschema:"name of a saved conversion preset to apply -- provide exactly one of target_format or preset, never both, never neither"`
	Opts         map[string]any `json:"opts,omitempty" jsonschema:"engine-specific conversion options; only meaningful alongside target_format, not preset"`
}
```
Add `Operation string \`json:"operation,omitempty" jsonschema:"..."\`` following the same `omitempty` + descriptive `jsonschema` tag convention, then thread it through to the `CreateJob` call inside the tool handler (same file, the function that currently calls `c.CreateJob(ctx, ..., in.TargetFormat, in.Preset, in.Opts...)` — grep `CreateJob(` in this file for the exact call site to extend with the new argument).

---

### `frontend/src/api.ts` — `createJob` operation param (service, request-response)

**Analog:** itself, `api.ts:85-107`

```typescript
export async function createJob(params: {
  apiKey: string;
  file: File;
  target: string;
  opts?: string;
}): Promise<CreateJobResponse> {
  const form = new FormData();
  form.append('file', params.file);
  form.append('target', params.target);

  const trimmedOpts = params.opts?.trim();
  if (trimmedOpts) {
    form.append('opts', trimmedOpts);
  }

  const response = await fetch('/v1/jobs', {
    method: 'POST',
    headers: authHeaders(params.apiKey),
    body: form,
  });

  return parseResponse<CreateJobResponse>(response);
}
```
D-05: add an optional `operation?: string` field to the `params` object type and the identical guarded-append shape used for `opts` (`if (params.operation) { form.append('operation', params.operation); }`) — never appended when absent/empty, matching this function's existing optional-field convention.

---

### `internal/convert/pairs_disjoint_test.go` (test, batch/pure assertions)

**Analog:** itself — all four existing tests share one shape

```go
func TestPandocPairsDisjointFromOtherEngines(t *testing.T) {
	norm := func(p Pair) Pair {
		return Pair{From: NormalizeFormat(p.From), To: NormalizeFormat(p.To)}
	}
	normAll := func(pairs []Pair) []Pair { ... }

	pandocPairs := normAll((PandocConverter{}).Pairs())
	others := map[string][]Pair{
		"LibvipsConverter":     normAll((LibvipsConverter{}).Pairs()),
		...
	}

	for name, otherPairs := range others {
		for _, p := range pandocPairs {
			for _, q := range otherPairs {
				if p == q {
					t.Fatalf("normalized pair %+v registered by both PandocConverter and %s, want disjoint", p, name)
				}
			}
		}
		union := make(map[Pair]bool, len(pandocPairs)+len(otherPairs))
		...
		if want := len(pandocPairs) + len(otherPairs); len(union) != want {
			t.Fatalf(...)
		}
	}
}
```
D-02/D-08/SC2: the new test proves the TRIPLE-keyed dictionary can hold `pdf→pdf` registered under a hypothetical non-convert operation WITHOUT colliding with `convert`'s own `pdf→pdf`-shaped registrations (there are none today, so this is a synthetic/constructed test case — likely a small in-test fake `Converter` implementing `OperationalConverter` with `Operation() string { return "sanitize" }` and `Pairs() []Pair { return []Pair{{"pdf","pdf"}} }`, registered into a throwaway `*Registry` via `NewRegistry()` + `Register`, then asserting `LookupOp("sanitize", "pdf", "pdf")` succeeds while `Lookup("pdf", "pdf")` (the `"convert"`-defaulting two-arg form) fails). This is a NEW pattern (identity-pair, synthetic converter) not directly copyable line-for-line from the four existing tests, but the union-count double-check idiom above should be reused for whatever the final triple-based disjointness assertion looks like.

---

### `internal/api/formats_handlers_test.go` / new golden-pinning test (test, batch/golden)

**Analogs:** `internal/api/formats_handlers_test.go:13-47` (httptest harness shape), `internal/worker/pinning_test.go:1-31` (pinning-suite doc-comment convention), `internal/api/system_usage_handlers_test.go` (`byte-identical` assertion idiom, e.g. lines 99, 151, 1117)

**HTTP harness pattern to copy** (`formats_handlers_test.go:13-22`):
```go
func TestListFormats_RegistryDerived(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/formats", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	...
}
```
D-17's golden-file protocol: FIRST commit captures `rec.Body.Bytes()` from TODAY's code (pre-Phase-57) for `GET /v1/formats` and `GET /v1/jobs/{id}` (single-output convert job) into `internal/api/testdata/` (mirroring the existing `internal/api/testdata/encrypted.docx`/`legacy.doc` directory, though those are input fixtures, not golden output — this will be the first golden-*output* file in this directory, following the `os.ReadFile("testdata/" + name)` idiom already used at `handlers_test.go:1303`). The comparison test then does `os.ReadFile("testdata/formats_golden.json")` and asserts `bytes.Equal` (or `string` equality) against `rec.Body.Bytes()` — reuse the exact "byte-identical" phrasing already established in this codebase's assertion messages (`system_usage_handlers_test.go:99`: `t.Errorf("... = %q, want byte-identical to ... %q", ...)`).

**Pinning-suite doc-comment convention to copy** (`internal/worker/pinning_test.go:1-13`):
```go
package worker

// This suite pins the pre-refactor seven-handler network per
// 56-CONTEXT.md D-10 step 1: it freezes TODAY's exact terminal/transient/
// canceled/success behavior for every one of the seven Handle*Convert
// wrappers ... BEFORE plan 56-04's Runner/EngineDescriptor consolidation.
// Plan 56-04's refactor MUST make this suite pass UNCHANGED -- any
// modification to an assertion in this file during that refactor is a
// protocol violation requiring explicit justification (PITFALLS.md
// Pitfall 14: "the test suite proves the runner's shape, not the seven
// engines' pre-existing behavior").
```
The new golden test file's header comment should follow this exact rhetorical shape, referencing D-17/57-CONTEXT.md instead, and stating explicitly that this file's assertions must not change during Phase 57 development — only the SUT (handler code) changes, never the golden fixture, except at the one deliberate "step 3" inversion checkpoint D-18 describes (temporarily reverting to `outs[0]` and confirming the test FAILS, then reverting the revert).

---

## Shared Patterns

### Fail-closed 422 before S3 write and before job-row insert
**Source:** `internal/api/handlers.go:394-403` (existing `EngineFor` check), extended per D-03/D-06/D-07 to two sequential checks (`unsupported_operation` then `unsupported_conversion`)
**Apply to:** `internal/api/handlers.go` `handleCreateJob` only — this is the single admission gate; no other file duplicates it (D-01 explicitly rejects a second endpoint).
```go
engine, ok := convert.Default.EngineFor(detected, target)
if !ok {
	writeError(w, http.StatusUnprocessableEntity, errcode.UnsupportedConversion,
		"unsupported conversion: "+detected+" -> "+target)
	return
}
```
Both new checks (`unsupported_operation`, `unsupported_conversion`) must be positioned before `s.storage.Upload` (later in the same function) and before `s.repo.Create` (`handlers.go:728`), exactly as this existing check already is.

### Registry-derived, never hardcoded, capability enumeration
**Source:** `internal/convert/convert.go:150-172` (`Classes()`) + `internal/api/formats_handlers.go:20-33` (`handleListFormats`)
**Apply to:** operation validity check (`IsKnownOperation`/`IsOperationRegistered`-shaped function on `Registry`, D-02) and the new `operations` key in `GET /v1/formats` (D-10). Both must derive from `convert.Default`'s actual registered converters — never a DB CHECK, never a separate Go slice literal (mirrors the existing `AllEngines()`/`IsKnownEngine` compile-time-closed-allowlist discipline at `convert.go:29-64`, but D-02 explicitly wants operation validity to be REGISTRY-derived, not a parallel closed allowlist like `AllEngines()` — do not copy `AllEngines()`'s hardcoded-slice shape for operations).

### Additive-key-guarded-on-nonempty JSON response idiom
**Source:** `internal/api/handlers.go:802-805`, `872-878`
```go
if len(normalizedOpts) > 0 {
	resp["opts"] = normalizedOpts
}
```
**Apply to:** `resp["outputs"]` in `handleGetJob` (D-11) — populate the map key ONLY when `len(outs) > 1`, never an empty/present array, so a single-output convert response stays byte-identical (SC4).

### Guarded status transition — closure-scoped invariant, transition() itself untouched
**Source:** `internal/jobs/repo.go:632-679` (`transition`), consumed by `MarkDone`/`MarkFailed`/`Cancel`/`RequeueStale`
**Apply to:** `internal/jobs/repo.go` `MarkDone`'s `apply` closure only (D-13) — every other `transition`-consuming method (`MarkFailed`, `Cancel`, `RequeueStale`, `MarkActive`) stays untouched; the row lock (`SELECT status FROM jobs WHERE id = $1 FOR UPDATE`) taken inside `transition` is what makes the new primary-output count query inside `MarkDone`'s closure race-free, since it runs on the same `tx` after that lock is already held.

### Error wrapping convention (`fmt.Errorf("<action>: %w", err)`)
**Source:** repo-wide (`internal/jobs/repo.go`, `internal/storage/storage.go`, `internal/db/db.go`) per CLAUDE.md
**Apply to:** any new repo.go query added inside `MarkDone`'s closure or `Outputs`'/`AddOutput`'s modified SQL — wrap with `fmt.Errorf("count primary outputs for job %s: %w", id, err)`-shaped messages, matching `RecoveryCount`'s existing style at `repo.go:295` (`fmt.Errorf("count recoveries for job %s: %w", id, err)`).

### `errcode` package: adding a new sync code
**Source:** `internal/errcode/errcode.go:17-51`
```go
const (
	InvalidRequest        = "invalid_request"        // ...
	UnsupportedConversion = "unsupported_conversion" // the (source,target) pair is not served by any engine
	...
)

var all = []string{
	InvalidRequest,
	UnsupportedConversion,
	...
}
```
**Apply to:** adding `UnsupportedOperation = "unsupported_operation"` — a new const with an inline trailing comment describing its class (mirroring every existing entry's comment style), appended to both the `const` block and the `all` slice literal (the package's own doc comment states "The list is closed at fourteen codes" — this comment count must be bumped to fifteen). `internal/errcode/doc_sync_test.go` and `internal/errcode/errcode_test.go` already enforce Go↔doc parity (per D-06 Phase 56 / D-03 Phase 57) — `docs/ERROR-CODES.md` gets a new row following the existing `unsupported_conversion`/`unknown_preset` row shape (`docs/ERROR-CODES.md:87,91`).

## No Analog Found

None. Every file in scope for Phase 57 has a direct, in-repo analog — this phase is explicitly framed by CONTEXT.md as additive extension of well-established patterns (optional-interface registration, CHECK-extension migrations, guarded-transition closures, additive-key JSON idioms), not new architecture.

## Metadata

**Analog search scope:** `internal/convert/`, `internal/api/`, `internal/jobs/`, `internal/worker/`, `internal/mcpserver/`, `internal/db/migrations/`, `internal/errcode/`, `cmd/av-worker/`, `cmd/ocr-worker/`, `cmd/pandoc-worker/`, `frontend/src/`, `docs/ERROR-CODES.md`
**Files scanned (read in full or targeted ranges):** `internal/convert/convert.go`, `internal/convert/converters.go`, `internal/convert/progress.go`, `internal/convert/pairs_disjoint_test.go`, `internal/api/handlers.go` (imports, form-field consts, `handleCreateJob` body, `handleGetJob` body), `internal/api/formats_handlers.go`, `internal/api/formats_handlers_test.go`, `internal/jobs/repo.go` (`MarkActive`/`MarkDone`/`MarkFailed`/`Cancel`/`AddOutput`/`Outputs`/`transition`), `internal/jobs/jobs.go`, `internal/worker/worker.go` (`process`, `convertRepo` interface), `internal/worker/pinning_test.go` (header), `internal/mcpserver/client.go` (`CreateJob`), `internal/mcpserver/tools.go` (`ConvertInput`), `frontend/src/api.ts` (`createJob`), `internal/db/migrations/0001_init.sql`, `0009_ocr_engine.sql`, `0010_pandoc_engine.sql`, `0012_output_identity_and_retention_index.sql`, `internal/errcode/errcode.go`, `docs/ERROR-CODES.md` (grep), `cmd/av-worker/main.go`, `cmd/ocr-worker/main.go`, `cmd/pandoc-worker/main.go` (grep for `Register(` call sites)
**Pattern extraction date:** 2026-08-08
