# Phase 73: CAD Class & Convert Slice, Confined - Pattern Map

**Mapped:** 2026-10-05
**Files analyzed:** 44 new/modified (grouped below)
**Analogs found:** 43 / 44 (the only no-analog item is the Python guard/mesh helper pair, which has the 71 measurement helper as a source rather than an in-repo production analog)
**Authority:** 73-CONTEXT.md `<research_resolutions>` R-01..R-10 supersede CONTEXT D-xx where they conflict. All analog paths below were checked with `git ls-files` and are tracked source (no `.gsd/capabilities` mirrors involved).

Skeleton analog: `cmd/document-worker/main.go` (preflight BEFORE Postgres). Most recent class addition: tectonic (phase 64, `e443c23`). Hardened-exec invariant: `internal/convert/exec.go` is NOT edited.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match |
|-------------------|------|-----------|----------------|-------|
| `cmd/cad-worker/main.go` (new) | worker entrypoint | event-driven (asynq consume) | `cmd/document-worker/main.go` | exact |
| `cmd/cad-worker/helper/cad_guard.py` (new) | engine helper (guard) | transform / batch | `scripts/cad-measure/counters.py` (counting only) | partial |
| `cmd/cad-worker/helper/cad_mesh.py` (new) | engine helper (mesh) | file-I/O transform | `scripts/cad-measure/cad_mesh.py` | exact (strip) |
| `internal/convert/cad.go` (new) | service (converter) | file-I/O | `internal/convert/pandoc.go` + `tectonic.go` (sentinels) | role-match |
| `internal/convert/cadsandbox.go` (new) | config + preflight | file-I/O | `internal/convert/documentsandbox.go` (two stage sets) + `pandocsandbox.go` (preflight) | exact |
| `internal/convert/cadjudge.go` (new) | utility (STL content judge) | transform | `internal/convert/sandboxpreflight.go` (`requireOutput`) | partial |
| `internal/convert/sandboxwrap.go` (modify: bounded run variant) | utility | request-response | itself (`engineExec.run`) | exact |
| `internal/convert/convert.go` (modify) | registry | CRUD | tectonic additions | exact |
| `internal/convert/converters.go` (modify) | registry wiring | CRUD | `Default.Register(TectonicConverter{})` | exact |
| `internal/convert/sniff.go` (modify `MIMEType`) | utility | transform | existing MIMEType switch | exact |
| `internal/queue/queue.go`, `client.go` (modify) | queue producer | pub-sub | tectonic block | exact |
| `internal/api/api.go` (modify) | interface + maps | CRUD | tectonic entries | exact |
| `internal/api/handlers.go` (modify: enqueue + per-format ceiling) | controller | request-response | `enqueueForEngine`, `maxEngineBytes` check | exact / role-match |
| `internal/api/estimate.go` (modify) | controller | request-response | `engineEstimateSource` | exact |
| `cmd/api/main.go` (modify `engineTimeoutsFromEnv`) | config | request-response | tectonic line | exact |
| `internal/reconciler/reconciler.go` (modify) | service | event-driven | tectonic case | exact |
| `internal/worker/descriptors.go` (modify) | service (classification) | request-response | `TectonicDescriptor`, `tectonicFailureCode` | exact |
| `docs/ERROR-CODES.md`, `README.md` (modify) | docs (test-held) | n/a | tectonic rows | exact |
| `Dockerfile.cad-worker` (new) | config (image) | n/a | `Dockerfile.cad-measure` (venv stage) + `Dockerfile.tectonic-worker` (tail) | role-match |
| `docker-compose.yml` (modify, 13 services + new) | config | n/a | `tectonic-worker` block | exact |
| `.env.example`, `.gitignore` (modify) | config | n/a | tectonic entries | exact |
| `deploy/chart/octoconv/templates/deployment-cad-worker.yaml` (new) | config | n/a | `deployment-tectonic-worker.yaml` | exact |
| `deploy/chart/octoconv/templates/scaledobject-cad.yaml` (new) | config | n/a | `scaledobject-tectonic.yaml` | exact |
| `deploy/chart/octoconv/templates/configmap.yaml`, `values.yaml` (modify) | config | n/a | tectonic blocks | exact |
| `scripts/ci-helm-checks.sh`, `scripts/sandbox-liveproof.sh`, `scripts/conf05-crosscheck.sh`, `.github/workflows/ci.yml` (modify) | script/config | n/a | tectonic/pandoc entries | exact |
| `.planning/research/SANDBOX-PATHS.md` (modify, `## cad`) | doc (held by test) | n/a | pandoc section `conf05:pandoc:*` | exact |
| `internal/e2e/chart_grace_test.go` (modify) | test | n/a | `workers` map | exact |
| Test doubles: `internal/api/handlers_test.go` fakeQueue, `internal/reconciler/reconciler_test.go` fakeEnqueuer, `internal/mcpserver/quota_admission_test.go` | test | n/a | tectonic methods | exact |
| Flipped tests (convert_test.go, formats_handlers_test.go, golden tests, estimate_test.go, sandboxcoverage_test.go, engine_sandbox_retryable_test.go) | test | n/a | see "Test flips" | exact |
| New tests: `cad_test.go`, `cadsandbox_test.go`, cad guard tests, `pinning_test.go` CAD verdicts, `cmd/cad-worker/main_test.go` | test | n/a | `pandocsandbox_test.go`, `pinning_test.go:1096`, `tectonic_sandbox_test.go` | role-match |

## Pattern Assignments

### `cmd/cad-worker/main.go` (worker entrypoint, event-driven)

**Analog:** `cmd/document-worker/main.go` (lines 49-231). Do NOT copy `cmd/tectonic-worker/main.go`'s order: it calls `db.Connect` first (`:47`) and never calls `preflightSandbox` from `main`.

**Order to copy exactly** (document-worker lines 54-120):
```go
if sandbox.RunExecModeIfRequested(os.Args) {
	return
}
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()

sandboxHelper, err := sandbox.ResolveHelper(documentSandboxDisabledEnv)
if err != nil {
	log.Fatalf("document sandbox: %v", err)
}
concurrency := envInt(documentConcurrencyEnv, 1)
...
convert.Default.Replace(conv)
if len(sandboxHelper) == 0 {
	log.Printf("⚠️  %s=true — the engines can read every file in this container", documentSandboxDisabledEnv)
} else {
	log.Printf("🔒 document sandbox: landlock via %s", strings.Join(sandboxHelper, " "))
}
preflightCtx, cancelPreflight := context.WithTimeout(ctx, documentPreflightTimeout)
if err := convert.DocumentPreflight(preflightCtx, conv); err != nil {
	cancelPreflight()
	log.Fatalf("document sandbox preflight: %v", err)
}
cancelPreflight()
pool, err := db.Connect(ctx)
```
Adapt: const `cadSandboxDisabledEnv = "CAD_SANDBOX_DISABLED"`, `cadConcurrencyEnv = "CAD_WORKER_CONCURRENCY"`, `cadPreflightTimeout = 60 * time.Second` (the floor every class uses). `sandbox.ResolveHelper` already produces the "Set %s=true to accept that deliberately" refusal; add no custom refusal text. Per research 2.2, refuse a concurrency other than 1 at startup naming `CAD_WORKER_CONCURRENCY` (document worker's refusal lives in `convert.ConfinedDocument`; `sharedTmpGuard` does not apply because the CAD sets grant no `/tmp`, so write a small explicit check in `cad-worker/main.go` or `ConfinedCAD`).

**Worker wiring** (document-worker lines 120-187), swap constants:
```go
mux.HandleFunc(queue.TypeDocumentConvert, worker.NewRunner(h, worker.DocumentDescriptor).Handle)
srv := asynq.NewServer(redisOpt, asynq.Config{
	Concurrency:     concurrency,
	Queues:          map[string]int{queue.QueueDocument: 1},
	RetryDelayFunc:  queue.RetryDelayFunc,
	ShutdownTimeout: envDuration("DOCUMENT_ENGINE_TIMEOUT", 300*time.Second) + 10*time.Second,
})
```
-> `queue.TypeCADConvert`, `worker.CADDescriptor`, `queue.QueueCAD`, `CAD_ENGINE_TIMEOUT` default 406 s (R-07). `worker.NewHandler(...)` takes the same eleven positional args with `nil`/`0` for webhook/audio parameters (document-worker 154-166). Keep the cancel `Subscriber`, metrics listener (`METRICS_ADDR` default `127.0.0.1:9090`), `srv.Shutdown()` on `ctx.Done()`, and helpers `envInt` / `envDuration` / `firstField` (lines 233-258) verbatim. `CAD_MESH_TIMEOUT` and `CAD_GUARD_TIMEOUT` are read ONLY here and injected into `internal/convert` through a setter or constructor argument (document-worker precedent: `convert.SetVeraPDFTimeout(envDuration("VERAPDF_TIMEOUT", 60*time.Second))`, line 93; `internal/convert` never calls `os.Getenv`).

No sweeper is constructed here (document-worker comment lines 168-171): the reconciler runs only in `cmd/webhook-worker`.

---

### `internal/convert/cadsandbox.go` (config + preflight, file-I/O)

**Analogs:** `internal/convert/documentsandbox.go` (two measured sets for two stages, lines 33-120) for the vars; `internal/convert/pandocsandbox.go` (lines 1-92) for `WithSandbox` + preflight.

**Per-stage sets** (document precedent, `documentsandbox.go:48-120`):
```go
var sofficeReadOnly = []string{ "/usr/lib", "/usr/bin/dash", ... }
var sofficeEngines = []string{ "soffice" }
var verapdfReadOnly = []string{ ... }
var verapdfEngines = []string{ "verapdf" }
```
For CAD: `cadGuardReadOnly` (no `/opt/venv`; helper dir, `/usr/bin/python3.11`, `/usr/lib/python3.11`, plus whatever the re-measurement proves) and `cadMeshReadOnly` (71 measured candidate, `evidence/71-07-landlock.txt:188`: `/opt/<helper-dir>`, `/opt/venv`, `/usr/bin/python3.11`, `/usr/lib/python3.11`, plus the multiarch triplet). Each is a flat gofmt-shaped `[]string{...}` literal (the crosscheck script parses only that shape). The triplet is NOT in the flat var (R-05): a separate func keyed on `runtime.GOARCH` (`arm64` -> `aarch64-linux-gnu`, `amd64` -> `x86_64-linux-gnu`) appended at spec-build time, with its own conf05 block kind. Engines vars: `cadGuardEngines`, `cadMeshEngines` (the python binary name resolved via PATH; `forJob` skips names that do not resolve, so `wrapEngine` reports "locate engine").

**Per-stage exec construction** (`sandboxwrap.go:155-190`, `219-231`):
```go
func (s *engineSandbox) forJob(jobDir string, readOnly, engines []string) engineExec
func engineEnv(tmpDir string) []string   // PATH HOME LANG LC_ALL (+ TMPDIR)
func (e engineExec) withEnv(env []string) engineExec
```
Research Pattern 1: `c.Sandbox.forJob(jobDir, cadMeshReadOnly(), cadEngines).withEnv(engineEnv(jobDir))`, called once per stage with that stage's own set. CAD's writable set is the job directory only (no `forJobRW`).

**WithSandbox** (`pandocsandbox.go:35-38`):
```go
func (c PandocConverter) WithSandbox(helper []string) PandocConverter {
	c.Sandbox = newEngineSandbox(helper)
	return c
}
```
`CADConverter` must stay comparable (Sandbox is a `*engineSandbox`; pandocsandbox_test.go `TestPandocConverterStaysComparable` is the test shape).

**Preflight skeleton** (`pandocsandbox.go:49-92`, copy structure):
```go
if !c.Sandbox.confines() { return nil }
w, cleanup, err := newPreflightWorkspace("pandoc")
...
marker, err := preflightMarker()
inside, outside, err := w.place("in.md", markdown)
// sufficiency
if err := c.Convert(ctx, inside, outInside, nil); err != nil { ... }
if err := requireOutput(outInside, []byte(marker)); err != nil { ... }
// effectiveness: SAME bytes, SAME ruleset, from OUTSIDE the job dir
convErr := c.Convert(ctx, outside, outOutside, nil)
if err := requireDenied(convErr, outOutside, []byte(marker)); err != nil { ... }
```
CAD differences (research 2.3): the input is an embedded minimal inch STEP (`units_inch.step` shape: 12 triangles, extents 1:2:3 x 25.4 mm); sufficiency is judged with the new STL judge (84+50n bytes, n==12, extents within 1e-3 of (25.4, 50.8, 76.2)), not a text marker; the negative run is judged by absence of a valid STL (helper exits rc=2 with no text under denial, `71-07-landlock.txt:140`). Call `w.requireOutsideGrants(guardSet, meshSet)` (`sandboxpreflight.go:82`) with both stages' sets. The preflight ctx is the signal ctx (`73e7b79`).

**Held by tests** (copy `pandocsandbox_test.go:15-37`): `TestCADGuardReadOnlySetIsTheMeasuredOne`, `TestCADMeshReadOnlySetIsTheMeasuredOne`, `...EnginesAreTheMeasuredOnes`, plus `TestNeitherEngineIsGrantedUsrBinWholesale` (document precedent), each failing with "re-measure with scripts/... rather than editing the expectation".

---

### `internal/convert/cad.go` (converter + sentinels, file-I/O)

**Analogs:** `internal/convert/pandoc.go` for the Pairs()/Engine()/guard-before-subprocess shape and `ErrTeX*` sentinels in `tectonic.go:1333-1460` for classification keyed on sentinels.

**Sentinel pattern** (`tectonic.go:1451-1460`):
```go
var ErrTeXEngineTimeout = errors.New("tex: the engine did not finish within the time limit")
var ErrTeXEngineKilled  = errors.New("tex: the engine was killed before it finished")
```
Define `ErrCADInputTooLarge`, `ErrCADTooComplex`, `ErrCADUnreadable`, `ErrCADEmptyResult`, `ErrCADMeshTimeout`, `ErrCADEngineKilled` (R-08).

**Killed-with-ctx-alive** (`tectonic.go:1353-1359`):
```go
var exitErr *exec.ExitError
if ctx.Err() == nil && errors.As(err, &exitErr) {
	if st, ok := exitErr.Sys().(syscall.WaitStatus); ok && st.Signaled() && st.Signal() == syscall.SIGKILL {
		return fmt.Errorf("tectonic: %w: %s", ErrTeXEngineKilled, tectonicTail(out))
	}
}
```
CAD: classify as terminal (R-02), mapped to `cad_too_complex`.

**Mesh stage timeout** keys on a STAGE context (`context.WithTimeout(ctx, meshTimeout)`) and sets `ErrCADMeshTimeout` only when the stage ctx expired while the engine ran and the parent ctx did not (research 4.3; PR #16 round-2 lesson: never key on `context.DeadlineExceeded`).

**Bounded output** (research Pitfall 7): add to `sandboxwrap.go` (NOT `exec.go`) a variant of `engineExec.run` that calls `runCommandCore(ctx, limit, e.env, name, cmdName, cmdArgs...)`. Signature to call (`exec.go:194`): `runCommandCore(ctx context.Context, limit int, env []string, label, name string, args ...string) ([]byte, engineCPU, error)`. Existing `run` passes `0` (unbounded) at `sandboxwrap.go:287`. Preserve `e.sandboxFault(err)` on the return. The status JSON line is printed last so the retained tail contains it.

**Judge by content, never exit code:** the Go side parses the JSON verdict from the output tail, then runs `judgeBinarySTL` on `out.stl` (size == 84+50n, n>0, finite bbox). Emptiness = triangles == 0 (NOT solids == 0: every IGES in the 71 corpus reads `solids:0` and still meshes).

**Single source of numbers** (research Pattern 3): `CADMaxSTEPBytes = 63<<20`, `CADMaxIGESBytes = 155<<20`, `CADMaxSTEPEntities = 754762`, `CADMaxNAUO = 1518` defined once in `internal/convert`; API, worker re-check and helper argv all read them. A pinned test names `71-DECISION.md` in its failure message, copying `TestTectonicTimeoutAndInputCeilingAreOnePair` (`internal/api/estimate_test.go:649`).

---

### `cmd/cad-worker/helper/cad_mesh.py` (file-I/O transform)

**Analog:** `scripts/cad-measure/cad_mesh.py` (separate production copy, D-03). Reuse from lines 245-348: the `OCP.*` import list inside `mesh()` (drop `numpy`, `BRepCheck`, `GProp`, `BRepGProp` and everything measurement-only), reader selection by extension, `reader.ReadFile(src) != IFSelect_RetDone` -> rc 2, `TransferRoots`/`OneShape`, the finite-bbox fallback (`FINITE_LIMIT`, `BRepBndLib.AddOptimal_s` per face; 71 FINDING for V-Slot returning +-1e100), the `QUALITY` table (`standard` = 0.001/0.35), and the writer:
```python
BRepMesh_IncrementalMesh(shape, lin, False, ang, in_parallel)
writer = StlAPI_Writer()
writer.ASCIIMode = False
if not writer.Write(shape, stl): ...
triangles = (os.path.getsize(stl) - 84) // 50
```
R-01 overrides D-06: do NOT scale in the helper (OCP already scaled `nist_ctc_05`, 841.6 mm vertex box vs OCP diag 907.17; a second scale would be 25.4x). Set `xstep.cascade.unit` to MM explicitly before `ReadFile` (binding spelling is ASSUMED, verify in the image); report `unit_text` as a diagnostic only. Parallel off. Print the JSON verdict LAST on stdout. Keep it Python 3.11-compatible and `-I -B` safe.

### `cmd/cad-worker/helper/cad_guard.py` (transform)

**Analog:** `scripts/cad-measure/counters.py:20-24` for what 71 counted (`STEP_ENTITY` line-anchored regex, `STEP_NAUO`, mmap after `DATA;`). R-03 overrides it: entity upper bound = count of `;` after `DATA;` read in 16 MiB chunks with early exit (cannot undercount; compare raw count to the cap); NAUO via `mmap.find(b"NEXT_ASSEMBLY_USAGE_OCCURRENCE", pos)` early exit at cap+1. Pure stdlib so Go tests run it with host `python3`. "Could not count" is a failure, never a pass. NAUO expansion/cycle logic is decided AFTER the Wave-0 spike on the real reader (R-03).

---

### `internal/worker/descriptors.go` (classification, request-response)

**Analog:** `TectonicDescriptor` (`:416-482`), `isTectonicTerminal` (`:486-583`), `tectonicFailureCode` (`:590+`).

**Descriptor shape** (`:468-482`):
```go
var TectonicDescriptor = EngineDescriptor{
	Engine: convert.EngineTectonic,
	Type:   queue.TypeTectonicConvert,
	OptsFromMap: func(m map[string]any) (any, error) {
		return convert.TeXOptsFromMap(m)
	},
	IsTerminal:  isTectonicTerminal,
	FailureCode: tectonicFailureCode,
}
```
CAD takes no options: use the strict probe-style `OptsFromMap` (a populated map is a corrupt column, D-10 terminal case; comment at `:471-476`).

**Classifier ordering to copy:** sandbox sentinel FIRST with a literal `return false` (`:500-502`), then each terminal sentinel, then `if errors.Is(err, context.Canceled) { return isTerminal(err) }`, then fall through to `isTerminal(err)`. Mesh timeout terminal via sentinel only; a plain `context.DeadlineExceeded` (S3 stall) stays transient.

**Codes** (`:34-88`): add to the `const` block and `allFailureCodes`: `CodeCADTooComplex = "cad_too_complex"`, `CodeCADUnreadable = "cad_unreadable"`, `CodeCADEmptyResult = "cad_empty_result"`, `CodeCADMeshTimeout = "cad_mesh_timeout"`; reuse `CodeInputTooLarge` (`:54`). There is no generic engine-timeout code today (R-08). Held by `internal/errcode/doc_sync_test.go:TestErrorCodesDocMatchesGoConstants`: update the `docs/ERROR-CODES.md` async table (four rows, the "Fifteen codes" sentence at `:124`, the `input_too_large` emitter list).

**Tests:** copy `TestPinnedTectonicFailureVerdicts` (`pinning_test.go:1096`): `NewRunner(h, TectonicDescriptor).Handle(...)` with `h.processFn` returning the exact wrapped shape the converter emits, asserting `asynq.SkipRetry`, one `MarkFailed` on the FIRST attempt, code and message. Add CAD cases to `TestEngineSandboxUnavailableIsRetryableForEveryConfinedClass` and its control.

---

### Atomic class registration (Go registries)

All analogs are the tectonic lines; each is a one-line addition and each fails a derived test by name until done.

| File | Anchor to copy | Change |
|------|----------------|--------|
| `internal/convert/convert.go:45`, `:84-96` | `EngineTectonic = "tectonic"`; `AllEngines()` list | `EngineCAD = "cad"`, append; update doc comment ("nine" -> "ten") |
| `internal/convert/converters.go:96` | `Default.Register(TectonicConverter{})` | `Default.Register(CADConverter{})` (zero value; `cmd/cad-worker` uses `convert.Default.Replace`) |
| `internal/convert/sniff.go:157` `MIMEType` | octet-stream fallback | add `step`, `iges`, `stl` content types (`worker.go:1457` uses `MIMEType(job.TargetFormat)`) |
| `internal/queue/queue.go:35,55,548,855,980-1023` | `TypeTectonicConvert = "tectonic:convert"`, `QueueTectonic = convert.EngineTectonic`, retry switch case, `AllConvertQueues`, `TectonicRetryDelay/backoffSum/UniqueTTL/NewTectonicConvertTask` | `TypeCADConvert = "cad:convert"`, `QueueCAD = convert.EngineCAD`, `CADRetryDelay` (delegates to `ProbeRetryDelay`), `cadBackoffSum`, `CADUniqueTTL`, `NewCADConvertTask` |
| `internal/queue/client.go:105-108,175-182,202-203,309-319` | `tectonicMaxRetry`/`tectonicUniqueTTL`, `envInt("TECTONIC_MAX_RETRY", 2)`, `envDuration("TECTONIC_ENGINE_TIMEOUT", 271*time.Second)`, `EnqueueTectonicConvert` | CAD equivalents; `CAD_ENGINE_TIMEOUT` default 406 s (R-07, PLACEHOLDER in `.env.example`) |
| `internal/api/api.go:130` | `EnqueueTectonicConvert(...)` in `Enqueuer` | add `EnqueueCADConvert` |
| `internal/api/handlers.go:1214` | `case convert.EngineTectonic: return true, s.queue.EnqueueTectonicConvert(ctx, id)` | `case convert.EngineCAD:` |
| `internal/api/api.go:429-453, 473-505` | `defaultEngineTimeouts`, `defaultMaxEngineBytes` | `EngineCAD: 406 * time.Second`; engine-wide `155 << 20` |
| `internal/api/estimate.go:102-135` | `engineEstimateSource` | `convert.EngineCAD: EstimateSourceUnmeasured`; NOT in `engineMeasuredDefaultSeconds` |
| `cmd/api/main.go:447` | `convert.EngineTectonic: envDuration("TECTONIC_ENGINE_TIMEOUT", 271*time.Second)` | `convert.EngineCAD: envDuration("CAD_ENGINE_TIMEOUT", 406*time.Second)` |
| `internal/reconciler/reconciler.go:93,388-395` | `enqueuer` interface + `case convert.EngineTectonic:` | add method + case; reword the `default:` comment at `:397` ("cad/archive are out of scope") |

**Per-format ceiling (R-04, no analog for the per-format map, role-match the engine map):** existing check to extend (`handlers.go:703-708`):
```go
if limit, ok := s.maxEngineBytes[engine]; ok && totalBytes > limit {
	log.Printf("content validation rejected: ... reason=engine_size_limit ...")
	writeError(w, http.StatusRequestEntityTooLarge, errcode.PayloadTooLarge,
		"file exceeds size limit for this conversion type")
	return
}
```
Add a second map keyed by (engine, format) consulted FIRST here and at `estimate.go:353` (`limit, gated := s.maxEngineBytes[p.engine]`), with a message naming format and ceiling only for classes that have a per-format entry (e.g. "step file exceeds the 63 MiB limit for this conversion"), leaving every other 413 message byte-identical (`pre58_createjob_rejections.golden.json`). Reuses sync `payload_too_large`, so no sync-code or openapi enum change. Do NOT raise chart `MAX_UPLOAD_BYTES` (R-04).

---

### Deploy and ops files

**`docker-compose.yml`:** copy the `tectonic-worker` service (`:1579-1683`): `build.dockerfile`, `container_name`, `restart: always`, `stop_grace_period` = timeout + 10 s asynq + 10 s margin (426s), `depends_on` postgres/redis/minio healthy, env block, then:
```yaml
CAD_WORKER_CONCURRENCY: "1"
CAD_ENGINE_TIMEOUT: "406s" # placeholder, see .env.example
CAD_MAX_RETRY: "2"
CAD_SANDBOX_DISABLED: "${CAD_SANDBOX_DISABLED:-}"
deploy:
  resources:
    limits:
      cpus: "2.0"
      memory: 4g
```
Also the OTHER class retry/timeout vars the block repeats "unconditionally (DEBT-05)" (`IMAGE_MAX_RETRY` ... `PROBE_MAX_RETRY`), and `CAD_ENGINE_TIMEOUT`/`CAD_MAX_RETRY` into all 12 existing queue-client services (anchors in research 1b #18). Held by `internal/queue/compose_in02_test.go`.

**Chart deployment:** copy `deployment-tectonic-worker.yaml` (header comment block aside): `.Values.keda.enabled and .Values.prometheus.enabled` replicas guard, `octoconv.waitForDeps`, `octoconv.io/tier: app`, `terminationGracePeriodSeconds: {{ .Values.cadWorker.terminationGracePeriodSeconds }}` (426), `octoconv.commonEnv`, optional `extraEnv`, metrics ports and the three probes (startup `periodSeconds: 5 failureThreshold: 24`, readiness, liveness), `resources.limits` from `.Values.cadWorker.resources.limits` (cpu `"2"`, memory `"4Gi"`). Per project rule, no design-rationale comments in code: the header blocks in the tectonic template are NOT to be copied (rationale goes to the PR description).

**Chart ScaledObject:** copy `scaledobject-tectonic.yaml` structure verbatim:
```yaml
{{- if and .Values.keda.enabled .Values.prometheus.enabled }}
...
  scaleTargetRef:
    name: tectonic-worker
  minReplicaCount: {{ include "octoconv.kedaMinReplicaCount" (dict "class" "tectonic" "root" $) }}
  ...
        query: sum(octoconv_queue_depth{queue="tectonic", state=~"pending|active|retry"})
        ignoreNullValues: "false"
```
with its own `queue="cad"` literal and `class: cad`. `values.yaml` needs both `cadWorker:` (`:182` pattern) and `keda.cad:` (`:484` pattern), `minReplicaCount: 0`, or `octoconv.kedaMinReplicaCount` aborts the render (`index .root.Values.keda .class`). `configmap.yaml:164-217`: add `CAD_ENGINE_TIMEOUT`, `CAD_MAX_RETRY`, `CAD_WORKER_CONCURRENCY: "1"`; `CAD_SANDBOX_DISABLED` deliberately absent (tectonic comment at `:205`).

**Dockerfile.cad-worker:** three stages. Stage 1 as every worker (`Dockerfile.pandoc-worker:1-7`):
```dockerfile
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/cad-worker ./cmd/cad-worker
```
Stage 2: the `venv` stage of `Dockerfile.cad-measure:3-16` WITHOUT the `requirements-vtk.txt` line and WITHOUT `venv-pruned`/`venv-selected`:
```dockerfile
FROM debian:bookworm-slim@sha256:3783cc01769c7b2b1b83a5c5ad96c815348e28ed7da68e2e3687004faa906251 AS venv
RUN apt-get update && apt-get install -y --no-install-recommends python3 python3-venv && rm -rf /var/lib/apt/lists/*
RUN python3 -m venv /opt/venv
COPY scripts/cad-measure/requirements-*.txt /req/
RUN /opt/venv/bin/pip install --no-cache-dir --only-binary=:all: --require-hashes -r /req/requirements-common.txt \
 && /opt/venv/bin/pip install --no-cache-dir --only-binary=:all: --no-deps --require-hashes -r /req/requirements-ocp-7.9.3.1.1.txt
```
R-06: the pip set is a hash-identical SUBSET of the approved lock (numpy + OCP pair; no ezdxf/pillow/fonttools yet; plan a task asserting each hash line equals the approved lock line, lock-file sha256s in research "Version verification"). Stage 3: same digest base, apt `python3 libgl1 libx11-6 ca-certificates` (R-06), `COPY --from=venv /opt/venv /opt/venv`, copy binary and helper dir, `python -m compileall -q`, import smoke `RUN /opt/venv/bin/python -I -B -c "import OCP"` (from `Dockerfile.cad-measure:36`), `USER nobody`, and `ENTRYPOINT ["/usr/local/bin/cad-worker"]` (`Dockerfile.tectonic-worker:155,159`).

---

### Landlock re-measurement and crosscheck (config/doc, held by test and script)

**Doc:** `.planning/research/SANDBOX-PATHS.md` new `## cad` with marker blocks (pandoc precedent `:488-492`):
```
<!-- conf05:pandoc:readonly -->
<!-- /conf05:pandoc:readonly -->
<!-- conf05:pandoc:engines -->
pandoc
<!-- /conf05:pandoc:engines -->
```
as `conf05:cadguard:*` and `conf05:cadmesh:*` (one entry per line; CAD writes only the job dir, so no `:readwrite`).

**Script:** `scripts/conf05-crosscheck.sh`: add `cadguard` and `cadmesh` to `class_go_file` (`:64`), `class_go_readonly_var` (`:76`), `class_go_engines_var` (`:88`), the usage/header text, and a `cad` alias expanding to both (the `document` -> `soffice verapdf` expansion at lines 52-61 is the precedent). It parses only `var <name> = []string{` literals; exit 2 on a missing block, 3 on a missing Go var.

**Coverage test:** `internal/convert/sandboxcoverage_test.go` fails the build for an unclassified file: add `cad*.go` to `sandboxClassFileEngines` (`:77`), `sandboxClassFileGroups` (`:107`) and `sandboxGroupEngines` (`:132`):
```go
case "libreoffice.go", "verapdf.go", "documentsandbox.go":
	return []string{"document"}
```
**Re-measurement:** `scripts/cad-sandbox-measure.sh` parametrised for the production image tag and helper path, native arch only; produces removal table and negative control (research 7.4). Also `scripts/sandbox-liveproof.sh` `class_row()` gets `cad) echo "Dockerfile.cad-worker cad-worker CAD_SANDBOX_DISABLED yes -"`.

---

## Test flips (phase 72 "no CAD" guards become allowlists, same commit, R-09)

| Test | Anchor | Change |
|------|--------|--------|
| `TestDefaultRegistersNoCADPair` | `internal/convert/convert_test.go:303-` | blanket ban (`cad` map of step/stp/iges/igs/stl/dxf/dwg, `class == "cad"` error) becomes an exact allowlist: only `step->stl`, `iges->stl`, engine `cad`, operation `convert`; keep the `walked == 0` vacuity guard |
| `TestAllEnginesCoversEveryEngineClass` | `convert_test.go:183-` | `len(got) != 9` -> 10, add `EngineCAD: true` |
| `TestListFormats_AdvertisesNoCADFormat` | `internal/api/formats_handlers_test.go:257` | same allowlist on `/v1/formats` |
| `TestListFormats_EnginesKeyUnchanged`, `TestPre57GoldenListFormats`, `assertFormatsGolden58ModuloSanitize` | `formats_handlers_test.go:~86`, `golden_pinning_test.go:~102`, `pre58_pinning_test.go:~150-210` | leave `pre57_formats.golden.json` bytes untouched; compare after stripping `cad` via `withoutEngine` (`pre58_pinning_test.go:399`), asserting the key is present (the tectonic comparator's non-vacuity clause) |
| `TestEveryEngineHasAnEnqueueRoute` | `handlers_test.go:3994` | fixture row `{"in.step","stl",...}` and `enqueuedCAD` on `fakeQueue` (`:281`) |
| `TestSweepRoutesEveryEngineConstant` | `reconciler_test.go:590` | `cadCalls`/`cadCallIDs` accessor (`:317`, `:606`) |
| estimate/env tests | `estimate_test.go:338,522,610,580` | `envExampleEngineVar[convert.EngineCAD] = "CAD_ENGINE_TIMEOUT"`; the `.env.example` line must say "placeholder" or "not measured" |
| `TestCADTracerSTEPBothEndpoints`, `TestCADCreateRejections` | `internal/api/cad_sniff_test.go` | UNCHANGED (post target `png`, still unsupported) |
| `docs/ERROR-CODES.md:120` | untested sentence "While no CAD engine is registered..." | edit by hand (not test-held) |

Byte-for-byte untouched goldens: `pre58_createjob_rejections.golden.json`, `pre59_inspect_pdf.golden.json`, `pre60_sanitize_rejection.golden.json`, `internal/convert/testdata/cad-sniff-golden.tsv`.

Test doubles that must gain `EnqueueCADConvert` (compile error otherwise): `internal/api/handlers_test.go:281`, `internal/reconciler/reconciler_test.go:236`, `internal/mcpserver/quota_admission_test.go:554`.

## Shared Patterns

### Confinement and env scrub
**Source:** `internal/convert/sandboxwrap.go` (`forJob` :155, `engineEnv` :219, `run` :278, `sandboxFault` :297).
**Apply to:** `cad.go`, `cadsandbox.go`, bounded-run variant.
```go
func (e engineExec) sandboxFault(err error) error {
	if err == nil || e.sandbox == nil || len(e.sandbox.Helper) == 0 { return err }
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == sandbox.SetupExit {
		return fmt.Errorf("%w: %w", ErrEngineSandboxUnavailable, err)
	}
	return err
}
```
Every stage's error passes through `sandboxFault`; helper exit 78 is the only transient CAD classification.

### Judge by content in both directions (preflight)
**Source:** `internal/convert/sandboxpreflight.go` (`newPreflightWorkspace` :43, `requireOutsideGrants` :82, `place` :111, `requireOutput` :136, `requireDenied` :155, `preflightMarker` :226).
**Apply to:** `CADPreflight`, and unit tests with a fake helper.

### Kill and no-orphan proofs
**Source:** `internal/convert/sandboxexec_test.go:312 TestEngineExecKilledOnContextDeadline`, `internal/convert/libreoffice_test.go:525 TestLibreOfficeConverter_TimeoutKillsRealProcess` (polls `ps`, asserts zero survivors), `fakeEngine` precedent in `sandboxexec_test.go`.
**Apply to:** the CAD kill test (R-10): fake helper that forks a grandchild `sleep 300`; cancel the parent ctx at ~200 ms; assert `Convert` returns within ~2 s and the grandchild PID is gone. Live docker proof of worker SIGKILL recovery (asynq lease 60-120 s) is a separate task marked `NOT RUN` when docker is unavailable; do not run docker in this mapping.

### Error wrapping and no library logging
**Source:** CLAUDE.md conventions; `fmt.Errorf("<action>: %w", err)`; only `cmd/*/main.go` logs; no comments by default (user global rule is stricter than project wording).
**Apply to:** all new Go files, including the converter. HTTP layer never echoes engine text (`writeError(w, status, code, "fixed message")`).

### Single-source constants pinned by a test naming 71-DECISION
**Source:** `internal/api/estimate_test.go:649 TestTectonicTimeoutAndInputCeilingAreOnePair`.
**Apply to:** `CAD*` ceilings/caps/timeouts (63/155 MiB, 754762, 1518, 316 s mesh, 30 s guard, 60 s I/O, 406 s attempt). Tests pin numbers as Go constants, never read `.planning/`.

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `cmd/cad-worker/helper/*.py` (production helpers) | engine helper | transform | No Python helper ships in any existing runtime image; source is `scripts/cad-measure/cad_mesh.py` / `counters.py` (measurement-only, copy and strip) |
| per-format byte-ceiling map | policy | request-response | No per-format size map exists; only `defaultMaxEngineBytes[engine]` (extend the existing check, keep other classes' 413 text byte-identical) |
| `cadjudge.go` (binary STL judge) | utility | transform | No STL reader in Go; the phase 72 sniffer (`internal/convert/cadsniff.go`) checks only `84+50n` size consistency, reuse its constants rather than a new parser |
| bounded engine-output variant | utility | request-response | `engineExec.run` passes limit `0`; `runCommandCore(ctx, limit, ...)` already accepts a limit, so the variant is a small addition in `sandboxwrap.go` (exec.go stays unchanged) |
| Wave-0 OCP spikes (self-ref STEP, non-numeric coords, doubling-hierarchy bomb, IGES inch) | measurement | batch | Never measured anywhere (research 6.3); model on the 71 ladder script, record rc/wall/RSS/`oom_kill`, decide guard rules after |

## Metadata

**Analog search scope:** `cmd/`, `internal/convert`, `internal/worker`, `internal/queue`, `internal/api`, `internal/reconciler`, `scripts/` (cad-measure, conf05), `deploy/chart/octoconv/templates`, `Dockerfile.*`, `docker-compose.yml`, `.planning/research/SANDBOX-PATHS.md`.
**Files read this session:** about 20 (document-worker main, pandocsandbox, sandboxwrap, descriptors regions, queue/client/api/reconciler/handlers regions, Dockerfile.cad-measure, cad_mesh.py 235-360, scaledobject/deployment tectonic, compose tectonic block, sandboxcoverage_test region).
**Pattern extraction date:** 2026-10-05
