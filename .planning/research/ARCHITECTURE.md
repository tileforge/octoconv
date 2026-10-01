# Architecture Research — v1.16 CAD Engine

**Domain:** Internal Go async file-conversion service — tenth engine class (`cad`), subsequent-milestone integration research
**Researched:** 2026-10-02
**Confidence:** HIGH on integration points and on what already exists (every cited `file:line` was read in this pass, tree at `0fc352f`). MEDIUM on FreeCAD/renderer behaviour (not executable here; settled by the evaluation phase). Claims are tagged: **[V]** verified in code, **[P]** established precedent in this repo, **[U]** unverified, must be settled by measurement.

Scope: only what the NEW features need — formats step/stp/iges/stl/dxf; `render` → png with closed view/size opts plus a second JSON measurements output; STEP/IGES → STL and DXF → svg/pdf; FreeCAD headless as the geometry engine; renderer chosen by an evaluation phase.

---

## 1. Answers up front

| Question | Answer |
|---|---|
| Is a two-output job new? | **No.** `sanitize` (probe, Phase 60) already emits primary (ordinal 0) + `role=report` (ordinal 1) through `EngineDescriptor.SideOutputs` (`internal/worker/runner.go:88-102`, loop `internal/worker/worker.go:1481-1509`, writer `jobs.Repo.AddOutputWithRole` `internal/jobs/repo.go:646`). The hook, the exactly-one-primary guard (`repo.go:277-284`), the `UNIQUE (job_id, ordinal)` retry idempotency (migration 0012) and role-keyed `download_url` selection (`internal/api/handlers.go:1285`, `internal/worker/worker.go:1024-1036`) all exist. **New here:** a role value that is not `report`, an output that exists for ONE operation of the class but not the other, and JSON that originates in a Python script. |
| Does the new role need a migration? | **Yes, one small one.** `job_outputs.role` is `CHECK (role IN ('primary','report','preview','thumbnail'))` (`internal/db/migrations/0015_operation_and_output_roles.sql`, last statement). New file `0018_cad_output_role.sql` (0017 is the latest) drops/re-adds `job_outputs_role_check` with the new value. **No migration is needed for the engine or the operation:** `'cad'` is in `jobs_engine_check` (0001:48, carried through 0016) and `'render'` is in `jobs_operation_check` (0015). |
| Usage accounting for two outputs | **No change.** `recordUsage` sums `size_bytes` over ALL `job_outputs` rows with no ordinal/role predicate (`internal/jobs/usage.go:60`), by design (v1.12). The measurements JSON adds a few hundred bytes to `bytes_out`. Admission quotas read `job_inputs` only (`admission.go:379`). |
| First engine driven by a Python script | **Yes.** Closest precedents: tectonic (input is a program, `cmd/tectonic-worker`), probe (two-pass ghostscript with a PostScript intermediate). Neither runs an interpreter on a **worker-authored** script. Recommendation: `go:embed` the script (precedent: `internal/convert/texresolvable.go:52`), write it into the job directory per job, run it under the same Landlock ruleset as any engine, and make the script↔Go contract two files (`params.json` in, `result.json` out) judged **by content**, never by exit status. See §4.3. |
| Helper process (Xvfb) | Expressible, with two costs that the evaluation phase must price. (a) Xvfb's socket lives in `/tmp/.X11-unix`, which drags the class into the CONF-06 shared-`/tmp` regime (concurrency exactly 1 + sweep + `sock:` grant), the same regime `html` and `document` live in. (b) There is no long-lived-helper primitive in `engineExec`; the cheap route is ONE `engineExec.run` of `xvfb-run -a <renderer>` so the process-group kill and the single ruleset cover both. A renderer that needs no X (EGL surfaceless / OSMesa / CPU rasteriser) keeps CAD out of both costs. See §4.4. |
| How do inspect and magic bytes learn CAD? | Two parallel detector chains must be edited in lockstep: `handleCreateJob` (`handlers.go:285-499`) and `detectInspectFormat` (`inspect_handlers.go:606-811`). CAD formats have weak or no magic bytes, so they follow the declared-source-gated structural-matcher pattern (html/latex/md, `handlers.go:347-408`, `inspect_handlers.go:764-772`), not the `Sniff` signature table. `RejectBindings` (`internal/inspect/bindings.go`) must gain every new `reason=` token. `/v1/inspect` offers come from the registry automatically (`estimate.go:321`), so offers appear when triples register. See §5. |
| Build order | Evaluation phase first, then pure-Go/SQL foundations that can be built and tested without any engine, then ONE atomic engine-class change (the `AllEngines` contract tests make a partial wiring impossible), then render, then ops. See §8. |

---

## 2. System Overview

```
 client ──multipart POST /v1/jobs (operation=convert|render, target, opts)──▶ API (chi)
                                                                              │
   handleCreateJob: sniff (declared-gated CAD matchers) ─▶ declared==detected ─▶ Gate1 IsKnownOperation
        ─▶ Gate2 EngineForOp(op, detected, target) ─▶ engine=cad ─▶ size ceiling ─▶ PreAdmit
        ─▶ opts: ParseCADOpts + ValidateCADApplicability ─▶ S3 upload ─▶ INSERT jobs (queued) ─▶ EnqueueCADConvert
                                                                              │ asynq queue "cad"
                                                                              ▼
 cad-worker (cmd/cad-worker)  Runner.Handle(CADDescriptor) ─▶ MarkActive ─▶ process():
     LookupOp(job.Operation, src, tgt)  ── convert ─▶ CADConvertConverter ──┐
                                        └─ render  ─▶ CADRenderConverter  ──┤
                                                                            ▼
              engineExec#1 (FreeCAD ruleset)  freecadcmd octoconv_cad.py  ── params.json ─▶ result.json + mesh/stl
              engineExec#2 (render ruleset)   [xvfb-run -a] f3d | other     ── mesh ─▶ out.png      (render only, 3D)
                                                                            │
     out.<target>  ─▶ S3 results/{job}/0-out.<target>  ─▶ AddOutput  (role=primary, DB default)
     out.<target>.measurements.json ─▶ SideOutputs ─▶ results/{job}/1-… ─▶ AddOutputWithRole(role=<new>)
     MarkDone (exactly one primary) ─▶ recordUsage in same tx ─▶ finish(): metrics + webhook enqueue
                                                                            │
 GET /v1/jobs/{id}: download_url = role==primary; outputs[] (present because len>1) lists every role
```

The `cad` class reuses, unchanged: the asynq `Runner` skeleton, `jobs.Repo.transition`, `recordUsage`, cancel pub/sub, progress infra (CAD does not implement `ProgressConverter`, so the plain `Convert` fallback in `runConvertWithOptionalProgress` `worker.go:1320` is used), webhook worker, reconciler, quotas, metrics.

---

## 3. Component Responsibilities — NEW vs MODIFIED

### 3.1 NEW

| Component | Responsibility | Location (follow precedent) |
|---|---|---|
| `EngineCAD = "cad"` | Single compile-time engine literal | `internal/convert/convert.go` const block (:20-46) |
| `OperationRender = "render"` | Constant for an operation the DB already allows | `internal/convert/convert.go` beside `OperationSanitize` (:74) |
| `CADConvertConverter` | `(convert, step→stl)`, `(convert, iges→stl)`, `(convert, dxf→svg|pdf)`; `Engine()=cad` | `internal/convert/cad.go` |
| `CADRenderConverter` | `(render, {step,iges,stl,dxf}→png)`; implements `OperationalConverter` (`Operation()=render`); writes the measurements sidecar | `internal/convert/cadrender.go` |
| CAD opts | `ParseCADOpts` / `CADOptsFromMap` / `ValidateCADApplicability(engine, source, target, o)`; closed `view` vocabulary, closed `size` | `internal/convert/cadopts.go` (shape: `htmlopts.go:60-130`) |
| CAD sniffers | `LooksLikeSTEPFromPrefix`, `…IGES…`, `…STL…` (size-aware, binary), `…DXF…` (ASCII + binary sentinel) | `internal/convert/cadsniff.go` |
| Measurements schema + validator | Strict decode of the script's `result.json`, finite-number check, closed `unit` vocabulary, canonical re-encode | `internal/convert/cadmeasure.go` |
| Embedded script(s) | `go:embed` of `octoconv_cad.py` (and a 2D DXF script if the evaluation picks one) | `internal/convert/cadscripts/*.py` |
| `cadsandbox.go` | Measured grants per stage, `forJob` builders, `CADPreflight`, shared-`/tmp` guard if needed | `internal/convert/cadsandbox.go` (shape: `documentsandbox.go`, `probesandbox.go`) |
| `CADDescriptor`, `isCADTerminal`, `cadFailureCode`, `cadSideOutputs` | Retry classification (stage-aware), failure codes, measurements read-back | `internal/worker/descriptors.go` (after `TectonicDescriptor`, :468) |
| `cmd/cad-worker/main.go` | Worker binary: sandbox helper first, `Replace` configured converters, preflight before Postgres, `NewRunner(h, CADDescriptor)` | clone of `cmd/pandoc-worker/main.go` (:43-81 sandbox+preflight, :205 mux) |
| Queue plumbing | `TypeCAD…`, `QueueCAD`, `CADRetryDelay`, `CADUniqueTTL`, `NewCADConvertTask`, `Client.EnqueueCADConvert` | `internal/queue/queue.go`, `client.go` |
| Migration `0018` | Widen `job_outputs_role_check` | `internal/db/migrations/0018_cad_output_role.sql` |
| `Dockerfile.cad-worker`, compose service, chart Deployment + ScaledObject + values + ConfigMap | Infra | repo root, `deploy/chart/octoconv/templates/` |
| Measurement scripts | `scripts/cad-timing-measure.sh`, KEDA load proof, extend `sandbox-paths-measure.sh` / `sandbox-liveproof.sh` | `scripts/` |

### 3.2 MODIFIED (small, mostly additive)

See the full checklist in §6. The ones that carry judgement rather than mechanical additions: `NormalizeFormat` (new aliases), `handleCreateJob` opts switch (new `case`), the two detector chains, `MIMEType`, `jobs.go` role constant, `mcpserver/client.go:112` and `docs/openapi.yaml:190-220` role descriptions, and three tests that currently assert `render` is rejected (§7).

---

## 4. What is genuinely new, and how to handle it

### 4.1 Two operations in one engine class (`convert` and `render`)

**[V]** The registry key is `(operation, from, to)` and `OperationalConverter.Operation()` returns ONE value per converter value (`convert.go:147-163`, `Register` `:246`). `SanitizeConverter` is the only precedent and serves one operation. CAD serves two, so it needs **two converter types** (or one struct with an `op` field registered twice): `Register` panics only on a triple collision, and `(render, step, png)` vs `(convert, step, stl)` cannot collide.

Consequences:

- `cmd/cad-worker` must `Replace` **both** configured converters (the pattern at `cmd/pandoc-worker/main.go:61`). Forgetting one leaves the zero-value placeholder (no sandbox, no ceilings) registered for that operation.
- `GET /v1/formats` `engines` key lists only `OperationConvert` pairs (`Classes()` `convert.go:385-405`), so `engines.cad` shows `step→stl, iges→stl, dxf→svg, dxf→pdf`; render pairs appear only under `operations.render.cad` (`OperationClasses()` `:413`). That is what the golden pin expects structurally. The `jobs_list_handler.go:46` engine filter derives from `Classes()`, so `cad` is filterable because it has convert pairs.
- **One asynq task type serves both operations**, because `process()` dispatches through `LookupOp(job.Operation, …)` (`worker.go:1389`) and `enqueueForEngine(ctx, engine, id)` (`handlers.go:1168`) and the reconciler switch (`reconciler.go:388`) key on engine only — they cannot choose a type per operation without a signature change. The repo's own argument (`queue.go:28-32`: "probe:convert for symmetry would put a false operation name into every asynq dashboard") says do not name it `cad:convert`. Recommend a neutral type name (e.g. `cad:job`). Low stakes, but decide it once: `RetryDelayFunc` (`queue.go:528`) dispatches on `t.Type()`.
- **The descriptor's opts re-parse is operation-blind.** `EngineDescriptor.OptsFromMap func(map[string]any) (any, error)` (`runner.go:48`) receives no job. The render schema (`view`, `size`) and the convert schema (empty) differ. Either (a) `CADOptsFromMap` parses the **union** schema strictly (acceptable: applicability already ran at the API, `ValidateCADApplicability`, and the re-parse is "strictness only", `runner.go:220-231`), or (b) widen the descriptor field to take the job. Recommend (a); it keeps the `Runner` untouched and its pinning tests green.
- **Opts dispatch in the API is a closed `switch engine`** (`handlers.go:832`) whose `default:` runs `ParseDocOpts`. Probe and tectonic fall into `default` and work only because they take no opts. `cad` **must** get its own `case` or every `view`/`size` request fails as an unknown key. `ValidateCADApplicability` can key on `target == "png"` (only `render` registers png for CAD sources), so the existing `(engine, source, target, opts)` signature suffices and operation need not be threaded.
- The two engine-keyed maps in `api.go` hold ONE timeout and ONE size ceiling per engine, not per operation (`api.go:429`, `:473`). The single `CAD_ENGINE_TIMEOUT` must cover the worst cell across both operations. See §4.7.

### 4.2 The second output: role, ordinals, accounting, failure ordering

- **Ordinals:** primary is ordinal 0 (`worker.go:1456`, `storage.OutputKey(job.ID, 0, outName)`); side outputs get `i+1` (`worker.go:1487`). `UNIQUE (job_id, ordinal)` (0012) turns an asynq re-attempt into an upsert, so a retry cannot double-count `bytes_out`.
- **Role:** add `jobs.OutputRoleX` beside `OutputRoleReport` (`internal/jobs/jobs.go:96`). **Name it `measurements`, not `dimensions`:** `internal/convert/dimensions.go` already means "declared pixel dimensions" (the decompression-bomb guard), and a role called `dimensions` will be misread in grep and in review. Do **not** reuse `report`: `docs/openapi.yaml:617` binds `role=report` to the `SanitizeReport` schema, so a client switching on role would parse the wrong shape.
- **Do not use `preview`/`thumbnail` for the PNG.** `MarkDone` requires exactly one `role='primary'` row (`repo.go:277-284`) and a render job's deliverable IS the PNG, so the PNG is primary. The reserved `preview`/`thumbnail` roles remain for a future "convert + attach preview" capability (this is the SEED-007 contract: fix `operation=render` + closed `view`/`size` + 1280×1280 default on CAD; do not spend the `preview` role on it).
- **Write path:** `AddOutputWithRole` only; `AddOutput` deliberately never writes `role` (`repo.go:625-662`, and the PR #12 defect described there — a role-bearing struct handed to `AddOutput` silently became a second primary and failed `MarkDone` on every attempt). Reusing the existing hook means this is already correct; a unit test must still go through the repository write, not only the hook.
- **Hook must be operation-conditional.** `SideOutputs` receives the `*jobs.Job` (`runner.go:102`), so `cadSideOutputs` returns `nil, nil` for `operation=convert` and the measurements for `render`. This also forces a change to `TestOnlyProbeDeclaresSideOutputs` (`internal/worker/probe_sideoutput_test.go:105-128`), which currently fails the build if any descriptor other than probe sets the hook — an intentional pin to update, not to bypass.
- **Validate before upload, not in the hook.** The hook runs after the primary is uploaded and recorded (`runner.go:99-102`). For probe that ordering is the point. For CAD, malformed JSON from the script should fail the job before any S3 byte moves, so `CADRenderConverter.Convert` should parse + canonicalise `result.json` and write the sidecar `outPath + ".measurements.json"` itself (exact pattern: `SanitizeReportPath`, `internal/convert/sanitizeconverter.go:89-107`); the hook only reads the blob back and treats a missing sidecar as an error (`descriptors.go:402-414`). The `out.*` stem is already worker-owned (`jobs.WorkerOwnedName`, `inputnames.go:117`), so the sidecar cannot collide with a client file (and CAD takes no attachments: `EngineTakesMultipleInputs` is tectonic-only, `convert.go:176`).
- **Webhook:** `setWebhookDownloadURL` selects the primary only (`worker.go:1024`), so a completed-webhook carries the PNG URL and never the measurements; they are reachable only via `GET /v1/jobs/{id}` `outputs[]`. Keep it that way for this milestone and say so in `openapi.yaml`; widening the webhook body is a contract change.
- **`GET /v1/jobs/{id}`** needs no code change: it presigns every output and emits `outputs[]` when `len(outs) > 1` (`handlers.go:1246-1310`) with `role` as a raw string. Only the descriptive text in `internal/mcpserver/client.go:112` ("report, preview and thumbnail are secondary artifacts") and `docs/openapi.yaml:190-220` must name the new role.
- **Measurements contract (the part that needs design, not wiring):** closed JSON, validated in Go, never forwarded raw. Fields to fix up front: `bbox` (min/max per axis), `volume` (null with a reason when not a closed solid or not applicable — DXF is 2D, STL volume is only valid for a watertight mesh), `unit`. **Units honest by source:** STEP/IGES declare units and FreeCAD normalises to mm on import ([U], confirm in evaluation), STL carries none, DXF carries `$INSUNITS` which is often 0/unitless. So `unit` needs a closed vocabulary that includes an explicit `unspecified`, and the converter must not label an STL's numbers as mm. The script must emit with `allow_nan=False` so a NaN/Inf fails the Go strict decode rather than producing a JSON literal that is not JSON.

### 4.3 First Python-script-driven engine: where the script lives and how it is confined

| Decision | Recommendation | Why |
|---|---|---|
| Where the source of truth lives | `go:embed` in `internal/convert/cadscripts/`, written to the job directory (0o400, `O_EXCL`) at the start of `Convert` | One artefact versions the script with the Go code that parses its output; no image/Go skew; `go:embed` already used (`texresolvable.go:52`). Baking it into the image at `/opt/...` would need an `ro:` grant plus a parity check between two artefacts. The job directory is already the only writable path and CAD takes no attachments, so no client file can occupy the name. |
| How parameters reach it | `params.json` (written by Go from already-validated opts) read by the script from a fixed relative name; **not** argv | FreeCAD documents a `--pass` option so a script can read later arguments from `sys.argv`, but the page that defines its exact semantics could not be fetched in this pass [U]; a params file avoids depending on FreeCAD's argv parsing at all and keeps "discrete argv, no client text" (the `pandocopts` rule). |
| How the result comes back | `result.json` + the produced file(s), **judged by content** | Project rule (`sandboxpreflight.go:2-12`): exit status lies (ghostscript exits 0 and writes nothing when denied). FreeCADCmd exit behaviour on a script exception is unverified [U]; success = `result.json` present, schema-valid, output non-empty. |
| Script termination | Script ends with an explicit exit; stdin is `/dev/null` (`runCommandCore` leaves `cmd.Stdin` nil) | FreeCADCmd can fall into its interactive console after a script file [U]; the process-group SIGKILL (`exec.go` `Setpgid`) is the backstop, not the plan. |
| Environment | `engineExec.withEnv(...)` with the `engineEnv(jobDir)` allowlist (`sandboxwrap.go:219`) **plus** class additions: `HOME` and `TMPDIR` pointing into a job-scoped subdirectory, `PYTHONDONTWRITEBYTECODE=1`, `PYTHONNOUSERSITE=1` | FreeCAD writes user config/cache under `$HOME` at start and exit and temp files under `TMPDIR`. The job directory is the only writable path, so leaving `HOME` as the worker's would either fail or push the class into a wider write grant. The documented precedent is `documentEngineTmpDir` (`documentsandbox.go:121-137`). `engineEnv` copies `HOME` verbatim, so the CAD builder overrides it rather than using the helper as is. |
| Landlock grants | Measured, not reasoned: `scripts/sandbox-paths-measure.sh` trace → verify, content-judged, then recorded in `.planning/research/SANDBOX-PATHS.md` | Mandatory method for every class (SANDBOX-PATHS.md "Метод", traps 1-8). Expect a large read-only set (Python stdlib, FreeCAD/OpenCASCADE libs, Qt if linked) and an interpreter/wrapper chain whose binaries are `execve`d and so invisible to an `openat` trace (trap 1/2). |
| What the script can and cannot do | It is OUR code, not client code: nothing in STEP/IGES/STL/DXF reaches `exec`/`eval` unless the script does so | This is why the tectonic "input is a program" treatment (CPU rlimit, starvation detection, terminal-on-timeout) is not automatically needed. The threat is memory/CPU blow-up inside OpenCASCADE parsers and, for DXF, a pure-Python importer; confinement + cgroup + ceilings answer that. |

Test strategy that fits the repo: pure-Go tests for params writing, result parsing, argv, classification, using a fake engine binary on `PATH` (the `engineExec`/`LookPath` seam resolves by name, `sandboxwrap.go:40-56`); real FreeCAD only in image-level live tests and the preflight. Add a `python3 -m py_compile` (or equivalent) `RUN` in `Dockerfile.cad-worker` so a syntax error in the embedded script fails the image build rather than the first job.

### 4.4 Multi-stage pipeline and a renderer helper (Xvfb)

**Multi-stage is established.** OCR (tesseract + pdftoppm + pdfinfo + pdftotext), audio (ffmpeg + whisper), probe (gs ×2, pdfinfo, unzip, file) all run several binaries from one job through `engineExec`. The sandbox-coverage test (`internal/convert/sandboxcoverage_test.go:77-153`) requires every literal binary name a class exec's to be in that class's `…Engines` list and fails on a stale entry.

**Two rulesets per job is established.** `document` builds one per engine, each its own measured set (`documentsandbox.go`: soffice vs veraPDF, D-10). Recommend the same for CAD, because the two stages have opposite risk profiles:

| Stage | Input trust | Needs | Ruleset |
|---|---|---|---|
| 1 FreeCAD/OpenCASCADE (+ Python) | **Hostile** STEP/IGES/DXF parsed by large C++ and Python code | Python + OCC libs read-only; job dir read-write | Tight: no socket, no shared `/tmp`, no `/dev/dri`, no `/proc` beyond measured entries |
| 2 Renderer (f3d / FreeCAD offscreen / other) | A mesh our stage 1 produced | GL stack (Mesa llvmpipe or EGL), fonts maybe; possibly Xvfb | Wider, but never sees the original upload |

**Where Xvfb bites, concretely:**

1. **The socket.** Xvfb's pathname socket is `/tmp/.X11-unix/X<n>`. The repo has the right grant: `sock:` token / `Paths.Socket` / `forJobRWSock` (`sandboxwrap.go:175`, `internal/sandbox/sandbox.go`), measured for soffice. But soffice's measured result was that a socket-only grant on `/tmp` is **not enough** — it unlinks its socket at exit, which needs a remove right, so `/tmp` was granted read-write (`documentsandbox.go:23-41`). Expect the same for Xvfb ([U]). That makes CAD a **CONF-06 shared-/tmp class**: `sharedTmpGuard` (`sandboxwrap.go:247`) refuses to start above concurrency 1, and a pre-task sweep of `/tmp` is mandatory (`htmlsandbox.go:105-135`). Stale `/tmp/.X*-lock` files left by a SIGKILL are exactly what the sweep would have to cover.
2. **Connecting is not governed today.** The deployment measured Landlock ABI 8; `handledAccess` (`sandbox_linux.go:44-48`) does not name `LANDLOCK_ACCESS_FS_RESOLVE_UNIX`, which controls `connect()` to a pathname socket and arrives only in ABI 9 (kernel-lists material, MEDIUM confidence). So under the current helper the client can reach Xvfb without a grant, and an ABI bump that starts handling the right would suddenly need a new `ro`/path grant for the X socket. Record it as a known coupling.
3. **Lifecycle.** `engineExec.run` is synchronous and `runCommandCore` kills the whole process group on timeout. `xvfb-run` is a shell script that starts Xvfb, runs the client, and kills Xvfb; as ONE `run` it dies with the group, and one ruleset (inherited across `execve`/`fork`) confines Xvfb, `xauth`, the shell helpers and the renderer. The binaries `xvfb-run` execs internally are not visible to the AST scanner and must be granted by measurement (SANDBOX-PATHS trap 2: wrapper/interpreter chain). The alternative — a Go-managed long-lived Xvfb — needs a new `engineExec.start`-style primitive that does not exist; avoid it. Keep `tini` as PID 1 for reaping (`Dockerfile.document-worker` entrypoint, same reason).
4. **`/dev/shm`, `/proc`, GPU libs.** Chromium (`htmlReadOnly`) needed all of `/proc` and `/sys/devices`; that class is already tracked as a standing weakness (`.planning/todos/pending/2026-09-30-proc-grant-other-process-entries.md`). A Mesa/Xvfb grant must be narrowed by removal, not copied from html.

**Decision rule for the evaluation phase (add to its criteria next to image size):** *does the winning renderer require a shared `/tmp` and a socket?* A renderer that does not (EGL surfaceless, OSMesa, or a CPU rasteriser with no GL at all) avoids the concurrency-1 constraint, the sweep, the `sock:` grant and the Xvfb stale-lock class, and should win a close contest on that ground alone. The measured fact on file is "mesh → PNG through f3d works only under xvfb-run" (PROJECT.md v1.16 context); a software-rasterised path (e.g. a Python mesh rasteriser run inside stage 1's ruleset) is worth including as a fourth candidate precisely because it removes stage 2 entirely [U].

**DXF is a different pipeline, not a fifth format on the 3D one.** It is 2D, has no volume, and its outputs are vector (svg/pdf) plus a raster (png). Decide in the evaluation phase whether DXF → svg is produced by FreeCAD (Draft import/export) or by a lighter 2D library, and where svg → png/pdf rasterisation happens **inside the cad class** (no cross-class dependency: libvips lives in the `image` worker). That choice fixes how many binaries are in `cadEngines` and whether DXF needs its own ruleset.

### 4.5 Preflight (CONF-04) for a two-stage, content-judged class

The preflight toolkit (`internal/convert/sandboxpreflight.go`) is `place` → sufficiency by content → effectiveness by identical bytes outside the job dir. For CAD:

- **Sufficiency (stage 1):** convert a tiny embedded STEP cube of known size through the production `Convert` path and require the validated measurements to equal the known bbox/volume (content, not exit code).
- **Sufficiency (stage 2):** render it; require a decodable PNG of the requested size that is not blank (a denied GL stack can produce an empty image with exit 0 — the same shape as chromium's blank 652-byte PDF, `htmlsandbox.go:135-145`).
- **Effectiveness:** run each stage's engine directly against the identical file placed outside the job dir (the html pattern: the negative run cannot go through `Convert`, which copies its input into the job dir, `htmlsandbox.go:185-200`); require no result.
- `requireOutsideGrants` must pass with the class's real grants; if `/tmp` is granted, use `newPreflightWorkspaceOutside("cad", "/var/tmp")` (`htmlsandbox.go:156`).
- Extend: `sandboxClassFileEngines` / `sandboxClassFileGroups` / `sandboxGroupEngines` (`sandboxcoverage_test.go:77,107,132`) with the new files and `cadEngines`; `scripts/conf05-crosscheck.sh` (class table at :53-95 pins path sets against the doc); `scripts/sandbox-liveproof.sh` class table (:53). Tectonic is **not** in those two scripts (legacy-confined), so cad would be the ninth entry in a list that today names eight.

### 4.6 Retry classification must be stage-aware

Polarity is per-engine and disagrees between neighbours (`descriptors.go:200-216` audio vs av vs ocr vs pandoc; the rule at `runner.go:57-66`: **`IsTerminal` is `func(error) bool`, never a boolean**). Derive CAD from its own failure modes, with distinguishable sentinels from `convert`:

| Failure | Verdict | Reason |
|---|---|---|
| Sandbox cannot be established (`ErrEngineSandboxUnavailable`) | **Transient**, checked first | A property of the node (`descriptors.go:322`, `sandboxwrap.go:297`) |
| FreeCAD/OCC cannot read the file, empty/degenerate geometry, unsupported entity | Terminal | Deterministic on the bytes |
| STEP/IGES exceeds complexity or output ceiling (triangle count, output bytes) | Terminal | Property of the file (`CodeOutputTooLarge`, `CodeTooManyPages` precedents) |
| Stage-1 deadline | Terminal (pandoc/document/tectonic shape): CPU-bound and content-deterministic | Same reasoning as `isPandocTerminal`; revisit if a starved-node signal is added (tectonic `ErrTeXEngineStarved`) |
| Renderer-stage failure or timeout | **Transient** (OCR/probe shape: an external renderer whose worst case is load) | Decide in the evaluation phase with measured variance |
| OOM-kill with time remaining | Terminal at concurrency 1 | Tectonic `ErrTeXEngineKilled` argument (`descriptors.go:563-571`) |
| Non-JSON / schema-invalid `result.json` | Terminal | Script/engine contract breach; deterministic |

New async failure codes (if any) go in BOTH `allFailureCodes` (`descriptors.go:72`) and `docs/ERROR-CODES.md`; the doc-sync test (`internal/errcode/doc_sync_test.go`) holds them equal. Prefer reusing `CodeOutputTooLarge`, `CodeEngineError`, `CodeInvalidOptions` over inventing vocabulary.

### 4.7 Timeout, ceilings, estimate provenance

- `CAD_ENGINE_TIMEOUT` and `CAD_MAX_INPUT_BYTES` are ONE decision (`api.go:448-452` and `:482-500` state it for tectonic). For STEP/IGES the cost driver is **geometry complexity (NURBS/faces/meshing deflection), not file bytes**, so a byte ceiling is a weak proxy, exactly as page count was for probe. Measure the driver (face count / triangle count) and bound it with a cheap pre-check where one exists: **binary STL declares its triangle count in an 84-byte header, so size must equal `84 + 50·n`** (a free fail-closed bomb check, the analogue of `MAX_IMAGE_PIXELS`); STEP/IGES/DXF have no declared count, so they get the byte ceiling + timeout + cgroup + an output-size cap.
- Expect the pandoc lesson: a CPU-light-looking engine can be **memory-bound** (4 MiB html OOMKilled a 1 Gi pod, PROJECT.md v1.11). OpenCASCADE meshing is a likely repeat. Plan concurrency 1 and measure peak RSS before fixing the cgroup and the ceiling (Path-B discipline: ceiling derived from the live OOM, not copied).
- Contract tests force the entries: `engineEstimateSource` (`estimate.go:102`), `engineMeasuredDefaultSeconds` (`:152`; a value there with an unmeasured number is the mislabel `estimateSourceFor` exists to prevent), `defaultEngineTimeouts` (`api.go:429`), `defaultMaxEngineBytes` (`api.go:473`), `engineTimeoutsFromEnv` (`cmd/api/main.go:431-447`). Start `estimate_source` as `unmeasured` and flip it in the same commit as the measured number (the rule stated at `estimate.go:116-118`).
- A single per-engine timeout must be the **max over both operations**; render (two stages, GL start-up) and a large STEP→STL are different cells. Measure both and take the worst, as pandoc took `html_large→md`.

---

## 5. `/v1/inspect` and magic-byte detection for CAD

### 5.1 What each format actually gives us (all [U] until the sniffers are fuzzed against real exporter output)

| Format | Identifier | Notes |
|---|---|---|
| STEP / STP | ASCII `ISO-10303-21;` at the start (optional leading whitespace), then `HEADER;` | 13 bytes, so it does not fit the 12-byte `Sniff` window (`sniff.go` `sniffLen`); use the window matcher. `stp` must alias to `step`. |
| IGES / IGS | Fixed 80-column ASCII records; column 73 is a section letter, first record is section `S`; columns 74-80 a sequence number | `igs` must alias to `iges`. Hollerith strings can carry 8-bit bytes. |
| STL | ASCII: `solid` … `facet normal`. Binary: 80-byte header + `uint32` count, total size `84 + 50·count` | **Ambiguity:** many binary STLs begin with the text `solid` in the 80-byte header. Test the size equation FIRST; only then fall back to the ASCII shape. Needs the total `size`, which the matchers already receive (`ValidateSniffPrefix(raw, size)`). |
| DXF | ASCII: group-code/value line pairs beginning `0` / `SECTION` (optionally preceded by `999` comment pairs); Binary: `AutoCAD Binary DXF\r\n\x1a\x00` | Binary sentinel is 22 bytes. |

### 5.2 Mechanism: declared-source-gated structural matchers, in both chains

`Sniff` (`sniff.go:44-54`, 12-byte window, closed table) is for formats with real magic. Everything above is weak, so use the html/latex/md pattern: the format is only claimed when the **declared** extension already names it AND a fail-closed structural check passes on a bounded window, otherwise `detected` stays empty and the request falls through to the generic 422 (`handlers.go:491-499`). Because declared must equal detected afterwards (`handlers.go:500-507`), a `.step` file whose bytes are a PNG is still rejected by the earlier `Sniff` (detected=`png`, mismatch). Insert the CAD matchers after the text branches and before OLE-CFB (`handlers.go:394-409`), reading the shared window once (`readSniffWindow`, `inspect_handlers.go:570`; the Phase 59 D-12 "one read, two answers" rule).

Two traps to design around now:

1. **`ValidateSniffPrefix` is strict** (`markupsniff.go:59-80`: no NUL, valid UTF-8). Real STEP and IGES exporters emit raw 8-bit (Latin-1) in header and string fields; a UTF-8 gate would 422 legitimate files. CAD needs its own envelope: no NUL for the text formats, high bytes tolerated, and the binary formats (binary STL, binary DXF) must bypass the text envelope entirely. [U] — confirm against a corpus of real exporter output (SolidWorks, Fusion, FreeCAD, AutoCAD, LibreCAD).
2. **Two detectors, one truth.** `detectInspectFormat` mirrors the create chain; an edit to one without the other makes `/v1/inspect` offer `render` for a file `POST /v1/jobs` then 422s, or the reverse. `internal/inspect/bindings.go` `RejectBindings` is checked by `TestRejectBindingsCoverEveryContentValidationReason`, which **scans `handlers.go` for `reason=` tokens**, so any new rejection token (e.g. an STL size/count inconsistency) must be added there or the test fails by name. Both chains also sit under `inspectgate` budgets; the CAD window read must be charged (`tr.Consume`) like the existing four matchers (`inspect_handlers.go:744-782`).

### 5.3 What `/v1/inspect` does for a CAD file, with no new analyzer

- `detectInspectFormat` returns `step`/`iges`/`stl`/`dxf`; `dispatchInspectFamilies` hits its `default:` (`inspect_handlers.go:1365-1470`) and runs no family analyzer; `buildChecks` then reports all 14 closed-vocabulary codes as `not_applicable / format_not_applicable` for a recognised format (`internal/inspect/report.go:142-175`). That is honest: the vocabulary has no CAD codes.
- `availableOperations` (`estimate.go:321`) walks `OperationClasses()`, so offers for `(convert, stl)` and `(render, png)` appear **as soon as the triples register**, each with the engine's `estimated_max_seconds`, provenance and `available`/`max_bytes` from `maxEngineBytes`. No inspect code change beyond detection.
- **Not in scope, and should stay out:** CAD findings (non-manifold mesh, unit unknown). Adding a code means extending the closed 14-code vocabulary, `docs/FINDINGS.md`, severity tables and their sync tests. Defer; note that `unit: unspecified` in the measurements output is the honest place to surface the unit gap for now.

### 5.4 Other format-keyed places

- `NormalizeFormat` (`convert.go:182-201`): add `stp→step`, `igs→iges` (same discipline as `jpeg→jpg`, `tex→latex`). **The frontend duplicates this table** (`frontend/src/formats.ts:6-25`, with a test in `formats.test.ts`); update both or `model.stp` is rejected client-side while the server accepts it. `GET /v1/formats` lists normalised names only, so `stp`/`igs` never appear as sources; document that.
- `MIMEType` (`sniff.go:157`) has no entry for step/iges/stl/dxf/svg; the default is `application/octet-stream`, which is the stored Content-Type for the **input** (`handlers.go:991`) and the **output**. Add entries (`model/stl`, `application/step`, `model/iges`, `image/vnd.dxf`, `image/svg+xml`); `json` and `png`/`pdf` exist. Note SVG delivered from a presigned URL is client-visible text derived from DXF entities (labels, block names); the converter, not the worker, must guarantee it is well-formed and script-free.
- Registry `Register` panics on collision; none exist: `(convert, dxf, pdf)` vs `document`'s `(convert, docx, pdf)` differ in `from`; `(render, step, png)` is its own operation.

---

## 6. Integration point inventory (the checklist every engine class touches)

Legend — **T** = a contract test fails by name until done (derived from `AllEngines()` or from compose/chart); **S** = silent, no test catches a miss (the failure mode of PR #12/#16); **E** = environment/process.

### 6.1 Go: engine identity and registry

| # | File:line | Change | |
|---|---|---|---|
| 1 | `internal/convert/convert.go:20-46` | `EngineCAD`; `:84-96` `AllEngines()` add — **only in the same change as everything marked T below** (the const's own doc, `:38-44`, records that adding it earlier broke nine contracts and advertised pairs with no consumer) | T |
| 2 | `internal/convert/convert.go:74` | `OperationRender` | |
| 3 | `internal/convert/convert.go:182-201` | aliases `stp`, `igs` | S (frontend copy) |
| 4 | `internal/convert/converters.go:96` | `Default.Register(CAD…{})` ×2 with **empty/zero config** (refuses to run unconfigured, the tectonic pattern `:84-96`); register LAST in the milestone, after the worker exists | T |
| 5 | `internal/convert/sniff.go:157` | `MIMEType` entries | S |
| 6 | `internal/convert/` new files | converters, opts, sniffers, measure, sandbox, scripts (§3.1) | |
| 7 | `internal/convert/sandboxcoverage_test.go:77,107,132` | classify new files + `cadEngines` | T |
| 8 | `internal/convert/readme_sync_test.go` + `README.md` | engine-class list | T |

### 6.2 Go: API

| # | File:line | Change | |
|---|---|---|---|
| 9 | `internal/api/handlers.go:285-499` | CAD matchers in the detection chain | S |
| 10 | `internal/api/handlers.go:832` | `case convert.EngineCAD:` ParseCADOpts + ValidateCADApplicability + normalised `json.Marshal` (exact shape of the HTML case at :865-909) | S (default silently rejects render opts) |
| 11 | `internal/api/handlers.go:1168-1190` | `enqueueForEngine` case | T (`TestEveryEngineHasAnEnqueueRoute`, `handlers_test.go:3911`) |
| 12 | `internal/api/api.go:116-133` | `Enqueuer` interface method; fakes in tests | T |
| 13 | `internal/api/api.go:429` / `:473` | `defaultEngineTimeouts`, `defaultMaxEngineBytes` | T (`estimate_test.go:613-630`) |
| 14 | `internal/api/estimate.go:102` / `:152` | provenance + measured-default maps | T (`estimate_test.go:341`) |
| 15 | `cmd/api/main.go:431-447` | `engineTimeoutsFromEnv` entry | T (`cmd/api/enginetimeouts_test.go`) |
| 16 | `internal/api/inspect_handlers.go:764-772` | CAD matchers in `detectInspectFormat` | S |
| 17 | `internal/inspect/bindings.go` | new `reason=` tokens | T |
| 18 | `internal/api/testdata/pre57_formats.golden.json`, `pre58_formats.golden.json`, `frontend/src/formats.test.ts` | add `cad` pairs (tectonic's change did the same, commit `e443c23`) | T |
| 19 | `docs/openapi.yaml` | formats, render operation, new role, opts schema | T (`openapi_*_sync_test.go`) |

### 6.3 Go: queue, worker, reconciler

| # | File:line | Change | |
|---|---|---|---|
| 20 | `internal/queue/queue.go:20-36,49-56` | task type, `QueueCAD = convert.EngineCAD` | |
| 21 | `internal/queue/queue.go:528` | `RetryDelayFunc` case + `CADRetryDelay` | S (default falls back to asynq's schedule silently) |
| 22 | `internal/queue/queue.go:854` | `AllConvertQueues()` — the KEDA queue-depth collector arg list | T (`queue_test.go:793`) |
| 23 | `internal/queue/queue.go:999-1021` | `CADUniqueTTL`, `NewCADConvertTask` | |
| 24 | `internal/queue/client.go:105-108,167-203,310` | fields, `CAD_MAX_RETRY` / `CAD_ENGINE_TIMEOUT` read in `NewClient`, `EnqueueCADConvert` | **S for the env**: IN-02 — every `queue.NewClient()`-constructing process must carry identical values or the unique-lock TTL silently diverges |
| 25 | `internal/reconciler/reconciler.go:93,388-406` | `enqueuer` interface + switch case (else `unroutable_engine`, job stranded) | T (`reconciler_test.go:593-616`, needs a fake accessor) |
| 26 | `internal/worker/descriptors.go` (after :482) | `CADDescriptor`, `isCADTerminal`, `cadFailureCode`, `cadSideOutputs`; add codes to `allFailureCodes` :72 | |
| 27 | `internal/worker/probe_sideoutput_test.go:105-128` | allow cad to declare `SideOutputs` | T (intentional pin edit) |
| 28 | `internal/worker/pinning_test.go` | descriptor enumeration if it lists engines | check |
| 29 | `cmd/cad-worker/main.go` | new; clone `cmd/pandoc-worker/main.go`: `RunExecModeIfRequested` first (:43), `ResolveHelper("CAD_SANDBOX_DISABLED")` (:55), `Replace` both converters (:61), `CADPreflight` before `db.Connect` (:74), `Concurrency` from env, `Queues: {QueueCAD: 1}` | |
| 30 | `cmd/manage-clients` | engine validation derives from `AllEngines()` | T (automatic) |

### 6.4 Database

| # | File | Change |
|---|---|---|
| 31 | `internal/db/migrations/0018_cad_output_role.sql` | `ALTER TABLE job_outputs DROP CONSTRAINT job_outputs_role_check; ADD CONSTRAINT … CHECK (role IN ('primary','report','preview','thumbnail','measurements'))`. Constraint name is Postgres's auto-name for the inline column CHECK (the same assumption 0015/0016 made and confirmed live). Additive, no backfill, no index. Embedded by `internal/db/db.go:32`. |
| — | `jobs_engine_check`, `jobs_operation_check` | **No change** (`cad`, `render` already present). `client_limits.engine` / `client_usage_daily.engine` carry no allow-list (0016 says so). `presets.operation` is intentionally not widened (0015 divergence #2); keep that. |
| 32 | `internal/jobs/jobs.go:96` | `OutputRole…` constant; do not let `internal/api` or `internal/worker` declare their own string |

### 6.5 Containers, compose, chart, CI

| # | File | Change | |
|---|---|---|---|
| 33 | `Dockerfile.cad-worker` | FreeCAD + chosen renderer, `USER nobody`, `tini` entrypoint if a forking helper exists, pinned versions/checksums (pandoc/tectonic precedent) | |
| 34 | `docker-compose.yml` | service; **every** `queue.NewClient()`-constructing service gets `CAD_MAX_RETRY` + `CAD_ENGINE_TIMEOUT` | T (`queue/compose_in02_test.go`, infers the service set) |
| 35 | `.github/workflows/ci.yml`, `.gitignore`, `.env.example` | bake-cache scope for the new service (compose-derived test), worker binary ignore, documented env | T (`internal/e2e/bake_wiring_test.go`) |
| 36 | `deploy/chart/octoconv/templates/deployment-cad-worker.yaml`, `scaledobject-cad.yaml`, `configmap.yaml`, `values.yaml` (`cadWorker`, `keda.cad`), `_helpers.tpl` (`kedaMinReplicaCount` is class-keyed, `:126`) | per-class literal `queue="cad"` in the ScaledObject (the "additive by construction" claim) | T |
| 37 | `terminationGracePeriodSeconds` | derive `timeout + 10s asynq ShutdownTimeout margin + 10s SIGKILL margin` | T (`internal/e2e/chart_grace_test.go:41` class→env map) |
| 38 | `scripts/ci-helm-checks.sh:56-72` | anti-vacuity count 9 → 10, class loop | T |
| 39 | `scripts/` | `cad-timing-measure.sh`, KEDA scale-from-zero + downscale-survival proofs, sandbox measure/liveproof/crosscheck extension, `.planning/research/SANDBOX-PATHS.md` cad section | E |

### 6.6 Docs

`README.md` (engine list), `docs/ERROR-CODES.md`, `docs/openapi.yaml`, `internal/mcpserver/client.go:112` jsonschema text, `frontend/src/api.ts:33-38` comment. A dedicated `docs/CAD.md` (units, views, measurements schema) is cheap and mirrors `docs/TYPESETTING.md`/`docs/SANITIZE.md`.

---

## 7. Existing pins that must change on purpose (not "fix the test")

| Pin | Why it breaks | Treatment |
|---|---|---|
| `internal/api/handlers_test.go:929` (`"extract","archive","render","bogus"` → 422 unsupported_operation) | `render` becomes a known operation | Drop `render` from the unregistered list; add positive coverage |
| `internal/api/formats_handlers_test.go:250` (same list) | same | same |
| `internal/convert/operations_test.go:197` (`IsKnownOperation` false for render) | same | same |
| `internal/worker/probe_sideoutput_test.go:105` (`TestOnlyProbeDeclaresSideOutputs`) | cad declares the hook | change to an explicit allow-set `{probe, cad}` and keep the "does not spread by accident" intent |
| `pre57/pre58 formats golden` + frontend `formats.test.ts` | new engine pairs | regenerate/extend with the engine PR, as tectonic did |
| `scripts/ci-helm-checks.sh` "exactly 9 ScaledObjects" | tenth | update count and loop |
| `pre60_pinning_test.go` ("other unregistered operations are still rejected") | check whether it lists `render` | verify during the foundations phase |

Per the project's own lesson (PITFALLS / memory: *fix the class, not the named instance*): the three `render` assertions are one class; sweep with `grep -rn '"render"' --include='*_test.go'` after the change, do not stop at the three found here.

---

## 8. Suggested build order

Dependencies drive it: the `AllEngines()` contract tests make "add the constant now, wire later" impossible (`convert.go:38-44`), the worker is not shippable without its measured ruleset and preflight (v1.15: confine from the first line, as pandoc did), and the renderer decision gates the image, the second ruleset and the timeout.

| Phase (numbering continues from 70) | Content | Depends on | Why here |
|---|---|---|---|
| **71 — Evaluation** | Renderer × FreeCAD invocation contract × DXF 2D path × image size × needs-X-or-not. Deliverables: decision record, `Dockerfile.cad-measure` (precedent `Dockerfile.probe-measure`), candidate path sets from the **trace** phase of `sandbox-paths-measure.sh`, FreeCADCmd facts (`--pass`/argv, exit status on exception, end-of-script behaviour, `$HOME`/`TMPDIR` writes, units on import), measured image size and cold-pull cost, measured STEP→mesh peak RSS. **No production wiring.** | — | Everything downstream (stack, rulesets, shared-/tmp class, timeout, chart resources) is a function of this. SEED-005 already says the first step must be an evaluation phase. |
| **72 — Contract foundations (engine-free)** | Migration `0018` + role constant; `OperationRender`; aliases; `MIMEType`; `cadopts.go`; `cadsniff.go` + both detector chains + `RejectBindings`; `cadmeasure.go` (schema, validator, canonical encoder, fuzz/negative tests); doc/openapi/mcp/frontend text for the role and formats. **Nothing is registered, `AllEngines()` unchanged**, so nothing is advertised. | 71 (opts vocabulary, units, schema) | Pure Go/SQL, fully testable with no FreeCAD; highest-risk review surface (fail-closed validation, the PR #9-style parser-divergence class) gets its own review round before any engine exists. `/v1/inspect` already recognises CAD formats here and offers nothing, which is the correct state. |
| **73 — CAD class + `convert` slice, confined** | The atomic engine-class change: `EngineCAD` + `AllEngines()` + queue + enqueue route + reconciler + timeouts/estimate/size maps + compose IN-02 env + README, `cmd/cad-worker`, `Dockerfile.cad-worker`, `CADConvertConverter` (STEP/IGES→STL, FreeCAD stage only), stage-1 ruleset **measured and verified**, `CADPreflight` (stage 1), `isCADTerminal`/failure codes, coverage-test classification, conf05/liveproof extension. Estimate source `unmeasured`. | 72 | Smallest vertical slice that exercises every integration point and the confinement, with no renderer risk. Ruleset + preflight cannot be deferred (the worker must refuse to start unconfined). |
| **74 — `render` (3D) + measurements** | `CADRenderConverter`, renderer stage and its second ruleset, `view`/`size` opts end to end (API case → persisted opts → `CADOptsFromMap`), measurements sidecar + `cadSideOutputs` + role path through `AddOutputWithRole`, render preflight, the three `render` pins, `TestOnlyProbeDeclaresSideOutputs`. If the renderer needs Xvfb: shared-`/tmp` guard + sweep + `sock:` lands here. | 73 | The two-output behaviour and the renderer risk are isolated in one phase. Needs a test that goes through the **repository write**, not only the hook (the PR #12 defect). |
| **75 — DXF (2D) pipeline** | `dxf→svg`, `dxf→pdf`, `(render, dxf, png)` on the path chosen in 71; its ruleset if it adds binaries; units from `$INSUNITS`; SVG well-formedness guarantee. | 73 (74 if it shares the rasteriser) | Different toolchain and risk profile from 3D; keeps phase 74 reviewable. Fold into 74 only if 71 picks FreeCAD for 2D as well. |
| **76 — Measured timeout, chart, KEDA, live gate** | `cad-timing-measure.sh` over both operations, `CAD_ENGINE_TIMEOUT` (max over operations) + size ceiling as one pair, estimate flipped to `measured`, chart Deployment/ScaledObject/values/ConfigMap/grace, `ci-helm-checks` count, KEDA scale-from-zero + downscale-survival on OrbStack, the all-classes live gate extended, docs. Decide `keda.cad.minReplicaCount` explicitly: a multi-GB image makes scale-from-zero cold start the dominant latency (backlog item AVX-02, registry cold-pull). | 74, 75 | Same position as the proven 4-phase template (container + measured timeout, then KEDA last). |

**Phase ordering rationale:** 71 → 72 is research → pure contracts; 72 before 73 because the sniffers and role migration are independent of any engine and are the riskiest code to review; 73 before 74/75 because the confined worker and all contract entries are a prerequisite for any registered triple; 74/75 split by toolchain; 76 last because the timeout is only meaningful once both operations exist.

**Research flags**

| Phase | Flag |
|---|---|
| 71 | Entirely research. Must answer the open [U] items listed in §9. |
| 72 | Needs a real-exporter corpus for the sniffers (Latin-1, binary-STL-starts-with-`solid`, DXF with leading `999`); otherwise standard patterns. |
| 73 | Standard template once 71 settles the invocation contract; the measured Landlock set is the long pole (expect several trace→verify iterations; SANDBOX-PATHS traps 1-8 all apply to a Python+C++ stack). |
| 74 | Needs deeper research if Xvfb is chosen (socket removal right, `/proc` narrowing, stale-lock sweep). Standard if the renderer is X-free. |
| 75 | Needs research (2D toolchain, SVG sanitisation, unit semantics). |
| 76 | Standard (KEDA/chart template), but resource limits are measured, not copied. |

---

## 9. Open questions the evaluation phase must close

1. FreeCADCmd: exact `sys.argv` handling with `--pass`; exit status when the script raises; whether it falls into an interactive console after the script; what it writes under `$HOME`/`TMPDIR` at start and exit. (Primary docs page was unreachable in this pass.)
2. Does the winning renderer need a socket and a shared `/tmp`? (Decides concurrency 1 + sweep + `sock:` for the whole class.)
3. Units: confirm FreeCAD normalises STEP/IGES to mm on import and what it reports for STL and for DXF with `$INSUNITS=0`. Fixes the closed `unit` vocabulary.
4. Is `volume` computable for non-solid STEP assemblies and for non-watertight STL, and what is the honest representation (null + reason)?
5. Peak RSS vs input size/complexity for STEP→STL at the chosen deflection; whether a triangle-count or output-size cap is enforceable inside the script before the write.
6. DXF 2D path: FreeCAD Draft vs a lighter library; where svg→png/pdf happens without leaving the `cad` class.
7. Image size and cold-pull time for FreeCAD + renderer on `debian:bookworm-slim` (native arm64 and amd64 — Landlock is unavailable to emulated amd64, SANDBOX-PATHS "Где замер возможен").
8. Task-type name (`cad:job` vs `cad:convert`) and measurements-role name (`measurements` recommended) — trivial but must be fixed once, because `RetryDelayFunc` and every enum consumer key on them.

---

## 10. Anti-Patterns

- **Adding `cad` to `AllEngines()` before the queue/route/reconciler/timeout entries exist.** It advertises pairs on `GET /v1/formats` with nothing consuming them (the phase-60 sanitize defect); the contract tests catch it, but only if the change is atomic.
- **Reusing `report` for the measurements.** Breaks the `role=report ⇒ SanitizeReport` binding clients may switch on.
- **Passing a role to `AddOutput`.** Silently becomes a second primary; `MarkDone` then fails every attempt (PR #12).
- **Trusting exit status** from FreeCADCmd, the renderer or Xvfb. Judge by content (PNG decodes and is non-blank; `result.json` validates).
- **Forwarding the script's JSON raw.** Python's default `json.dumps` emits `NaN`/`Infinity`; validate, bound, and re-encode in Go.
- **One ruleset for both stages "for simplicity".** Hands the hostile-input parser the renderer's wider grants (socket, `/tmp`, GPU/`/proc`), the exact asymmetry `document` avoided with D-10.
- **Copying html's `/proc` and `/sys/devices` grants** for the renderer. That grant is a tracked weakness; narrow by removal from measurement.
- **Copying a neighbour's timeout, concurrency or `scaleDownStabilizationSeconds`.** Every prior engine's number came from its own cost driver; CAD's is geometry complexity, not bytes.
- **Fixing the three `render` test assertions and stopping.** Sweep the class (§7).
- **Extending the closed inspect vocabulary for CAD in this milestone.** Out of scope; each new code ripples through severities, docs and sync tests.

---

## 11. Scaling Considerations

| Concern | Now (internal clients) | Notes |
|---|---|---|
| Throughput | KEDA scale-out per queue; `CAD_WORKER_CONCURRENCY=1` | Memory-bound engines (pandoc precedent) get throughput from replicas, not in-pod parallelism. If Xvfb/shared `/tmp` is in play, concurrency 1 is **enforced** by `sharedTmpGuard`, not a choice. |
| Cold start | Image is multi-GB (FreeCAD + GL stack) | Scale-from-zero latency is dominated by image pull; set `keda.cad.minReplicaCount` deliberately and measure (AVX-02). |
| Output size | PNG ≤ size² × channels; STL can be hundreds of MB | Output-size cap before upload (tectonic `ErrTeXOutputTooLarge` precedent); `SideOutput.Body` is in memory, which is fine for a small JSON and the reason a mesh must never be a side output. |
| Reconciler window | `RECONCILER_ACTIVE_STALE_AFTER` 900s cap | `CAD_ENGINE_TIMEOUT` must sit well under it (all measured timeouts are 65-753s). |

---

## Sources

All **[V]** claims: files read in this pass at `/Users/apaderin/dev/octoconv` (tree `0fc352f`): `internal/worker/{runner,descriptors,worker}.go`, `internal/jobs/{jobs,repo,usage}.go`, `internal/convert/{convert,converters,sandboxwrap,sandboxpreflight,sandboxcoverage_test,htmlsandbox,documentsandbox,probesandbox,pandocsandbox,sanitizeconverter,sniff,markupsniff,htmlopts}.go`, `internal/sandbox/{sandbox,sandbox_linux,execmode,helper}.go`, `internal/api/{handlers,api,estimate,inspect_handlers}.go`, `internal/inspect/{bindings,report}.go`, `internal/queue/{queue,client}.go`, `internal/reconciler/reconciler.go`, `internal/db/migrations/{0012,0015,0016}*.sql`, `cmd/pandoc-worker/main.go`, `cmd/api/main.go`, `deploy/chart/octoconv/**`, `scripts/ci-helm-checks.sh`, `frontend/src/formats.ts`, `.planning/research/SANDBOX-PATHS.md`, `.planning/seeds/SEED-005.md`, `SEED-007.md`, `SEED-017.md`.

External (MEDIUM, secondary sources, cached via `research-store`):
- Landlock `LANDLOCK_ACCESS_FS_RESOLVE_UNIX` (controls `connect()`/`sendmsg()` to pathname UNIX sockets) is ABI 9: Linux security-module list patch series, "landlock: Control pathname UNIX domain socket resolution by path" (kernsec.org linux-security-module-archive, 2026-01…04).
- FreeCAD headless executable `FreeCADCmd`, script files as CLI arguments, `--pass` passes later arguments through to a script: FreeCAD wiki "Headless FreeCAD" / "Start up and Configuration" via search results (primary page blocked; exact `sys.argv` semantics NOT confirmed — treated as [U]).
- Headless GL options (EGL vs OSMesa) in VTK/ParaView/Open3D docs (general background only; f3d-specific flags NOT verified — [U], evaluation phase).

---
*Architecture research for: OctoConv v1.16 CAD Engine (tenth engine class)*
*Researched: 2026-10-02*
