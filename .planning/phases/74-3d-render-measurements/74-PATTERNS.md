# Phase 74: 3D Render & Measurements - Pattern Map

**Mapped:** 2026-10-05
**Files analyzed:** 17 new/modified
**Analogs found:** 17 / 17 (3 with partial match)

Read the tree as it stands with the uncommitted PR #34 P1 fix (R-12): `cad-mesh` has the `incomplete` verdict and `face_survey`; `cad.go` has `requireSTLMatchesVerdict` and `classifyCADStage` maps `incomplete` to `ErrCADUnreadable`. All excerpts below include that state. All analog paths are git-tracked (checked with `git ls-files`). Tracked `.planning/research/SANDBOX-PATHS.md` is the sandbox record (there is no `docs/SANDBOX-PATHS.md`).

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match |
|---|---|---|---|---|
| `cmd/cad-worker/helper/cad-render` (new) | helper (confined python) | transform (STL file -> PNG file) | `scripts/cad-measure/cad_render.py` (verbatim port) + `cmd/cad-worker/helper/cad-mesh` (verdict/fd wrapper) | exact |
| `cmd/cad-worker/helper/cad-mesh` (mod: `--quality`, `--measure`, `stlstats`) | helper | transform | itself + `scripts/cad-measure/cad_mesh.py:245-348,429-476` (71 measurements/stlstats) | exact |
| `cmd/cad-worker/requirements-vtk.txt`, `vtk_keeplist.py`, `vtk-keeplist.txt` (new, copies) | config | batch | `scripts/cad-measure/requirements-vtk.txt`, `vtk_keeplist.py`, `vtk-keeplist.txt`; `cmd/cad-worker/requirements-mesh.txt` | exact |
| `Dockerfile.cad-worker` (mod) | config | batch | itself (venv stage + engine stage); `Dockerfile.cad-measure` for VTK/osmesa lines | exact |
| `internal/convert/cad.go` (mod: quality, render timeout, triangle cap, `stl` ceiling, sentinels) | service | request-response | itself | exact |
| `internal/convert/cadopts.go` (new) | utility (opts parser) | request-response | `internal/convert/audioopts.go` (+ `opts.go` `checkStrictObject`) | exact |
| `internal/convert/cadrender.go` (new: `CADRenderConverter`, render stage, `judgePNG`) | service | transform | `internal/convert/cad.go` (`CADConverter`, `meshStage`, `judgeBinarySTL`) | exact |
| `internal/convert/cadmeasure.go` (new: draft -> `measurements.Document`) | utility | transform | `internal/measurements/measurements.go` (types/Validate) + `internal/worker/descriptors.go` `probeSideOutputs` (side-output body) | role-match |
| `internal/convert/cadsandbox.go` (mod: `cadRenderReadOnly`, `cadRenderEngines`, preflight, `ValidateCADTimeouts` render arg, `ConfinedCAD`) | config/service | request-response | itself | exact |
| `internal/convert/*_test.go` (cad_test, cadsandbox_test, new cadopts/cadrender/cadmeasure tests) | test | - | `internal/convert/cad_test.go`, `cadsandbox_test.go` | exact |
| `cmd/cad-worker/main.go` (mod: render timeout env, one `Replace` per converter) | config | request-response | itself | exact |
| `internal/worker/descriptors.go` (mod: `CADDescriptor` `SideOutputs`, sentinels, failure codes) | config | request-response | `CADDescriptor` (L668-715) + `probeSideOutputs` (L410) | exact |
| `internal/worker/worker.go` `process()` (mod: validate both, upload both, `AddOutputs`, `MarkDone`) | service | CRUD/event | `process()` L1470-1511 | exact |
| `internal/jobs/repo.go` (new `AddOutputs`) | model/repo | CRUD (transaction) | `AddOutputWithRole` L646 + `pgx.BeginFunc` in `transition` | role-match |
| `internal/api/handlers.go` (mod: CAD opts parse/applicability at create) | controller | request-response | audio branch at `handlers.go:868-874` | exact |
| `internal/api/api.go` (`maxFormatBytes` `stl`, `engineTimeouts`) | config | request-response | `api.go:179, 457, 521` | exact |
| `.planning/research/SANDBOX-PATHS.md` `## cad`, `scripts/conf05-crosscheck.sh`, `docs/MEASUREMENTS.md`, `scripts/cad-liveproof.sh` | docs/script | batch | existing `cad` entries | exact |

## Pattern Assignments

### `cmd/cad-worker/helper/cad-render` (helper, transform)

**Analog A (render body):** `scripts/cad-measure/cad_render.py`. Port verbatim; do not add or remove a `vtkmodules` import (pruned venv fails at runtime under `nobody`, R-11).

Imports that define the keep-list (cad_render.py:70-77):
```python
from vtkmodules.vtkCommonCore import vtkObject
from vtkmodules.vtkFiltersCore import vtkCleanPolyData, vtkFeatureEdges, vtkPolyDataNormals
from vtkmodules.vtkIOGeometry import vtkSTLReader
from vtkmodules.vtkIOImage import vtkPNGWriter
from vtkmodules.vtkRenderingCore import (
    vtkActor, vtkLight, vtkPolyDataMapper, vtkRenderer, vtkWindowToImageFilter,
)
from vtkmodules.vtkRenderingOpenGL2 import vtkOSOpenGLRenderWindow
```
Constants (cad_render.py:19-36): `VIEWS`, `FILL=(0.80,0.80,0.80)`, `EDGE=(0.08,0.08,0.10)`, `FEATURE_ANGLE=30.0`, `CAMERA_DISTANCE=2.0`, `LIGHTS`. Camera fit (L98-107, 171-177): corner projection on `right`/`up`, `scale = long_half / (1.0 - 2.0 * margin)` with margin fixed 0.06, `ParallelProjectionOn`, never `ResetCamera`, only `ResetCameraClippingRange`. Transparent: `SetBackgroundAlpha(0.0)`, `SetAlphaBitPlanes(1)`, `SetInputBufferTypeToRGBA()` (L146-188).

Drop: `dump_maps`, `--margin`, `--lp-threads`, `os.environ["LP_NUM_THREADS"]` (Go sets `LP_NUM_THREADS=2`, `cadsandbox.go:52`), the `.json` sidecar write (L215-216), `time` timing fields. Tighten arg check (L231): `256 <= size <= 2048`, `background in ("opaque","transparent")` (rename from `white`), exit 64.

**Analog B (verdict + fd wrapper)**, `cad-mesh` L1-35 and L174-187:
```python
def verdict(fd, verdict_name, reason, triangles=0, load_fails=NOT_EVALUATED):
    obj = {"stage": "mesh", "verdict": verdict_name, "reason": reason, "triangles": triangles, "load_fails": load_fails}
    os.write(fd, ("CADVERDICT " + json.dumps(obj, separators=(",", ":"), sort_keys=True) + "\n").encode("ascii"))
...
class Parser(argparse.ArgumentParser):
    def error(self, message):
        sys.stderr.write("cad-mesh: %s\n" % message)
        sys.exit(EXIT_USAGE)
...
    fd = os.dup(1)
    os.dup2(2, 1)
    return run(args.format, args.src, args.out, fd)
```
Shebang `#!/opt/venv/bin/python -IB`. Emit `{"stage":"render","verdict":"ok",...,"view","size","background","proj_half_w","proj_half_h","proj_aspect","bbox"}` (RESEARCH Pattern 1); add `triangles`. Exit codes: 2 empty, 3 zero extent, 64 usage. No `OCP` import anywhere (R-09; a Go test greps the helper).

---

### `cmd/cad-worker/helper/cad-mesh` (modify)

Current structure to extend: `finite_box` (L35-55), `run` (L100-172) with `Interface_Static.SetCVal_s("xstep.cascade.unit","MM")` pin, `face_survey`, verdicts `unreadable|empty|incomplete|too_complex|ok`. Constants `LINEAR_DEFLECTION = 0.001`, `ANGULAR_DEFLECTION = 0.35` become `--quality` table (draft 0.004/0.5, standard 0.001/0.35, fine 0.0003/0.2):
```python
mesher = BRepMesh_IncrementalMesh(shape, LINEAR_DEFLECTION * diag, False, ANGULAR_DEFLECTION, False)
```
Measurements to add (analog `scripts/cad-measure/cad_mesh.py:245-348`): `TopExp_Explorer(shape, TopAbs_SOLID)`, `BRep_Tool.IsClosed_s(shell)`, `BRepCheck_Analyzer(solid).IsValid()`, `BRepGProp.VolumeProperties_s` / `SurfaceProperties_s`, reason logic at cad_mesh.py:307-321. Two deviations from the 71 helper (RESEARCH "Measurements"): bbox via `BRepBndLib.AddOptimal_s(shape, box, False, False)` with the existing `finite_box` fallback; a zero total volume demotes solids to `invalid_solid`, never 0. Write the draft to `--measure PATH` (JSON), keep the stdout `CADVERDICT` line unchanged in shape. STL stats path: copy `stlstats` from cad_mesh.py:429-476 and add area (chunked); needs no OCP import (guard `--format` choices to include `stl` for this subcommand only; `cad-guard` stays step/iges only, Go skips guard for STL, R-08).

---

### `internal/convert/cadopts.go` (utility, request-response)

**Analog:** `internal/convert/audioopts.go` (L55-110) with `checkStrictObject` from `opts.go`. Copy this shape: `ParseCADOpts(raw []byte)`, `CADOptsFromMap(m)`, `ValidateCADApplicability(engine, source, target string, o)`.
```go
func ParseAudioOpts(raw []byte) (AudioOpts, error) {
	if err := checkStrictObject(raw); err != nil {
		return AudioOpts{}, err
	}
	var o AudioOpts
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&o); err != nil {
		return AudioOpts{}, fmt.Errorf("parse opts: %w", err)
	}
	...allowlist checks...
}
func AudioOptsFromMap(m map[string]any) (AudioOpts, error) {
	if len(m) == 0 { return AudioOpts{}, nil }
	raw, err := json.Marshal(m)
	...
	return ParseAudioOpts(raw)
}
```
Replaces the stub `CADOptsFromMap` in `cad.go:49-54` (which today errors on any key; its test `TestCADOptsFromMapTakesNothing` in `cad_test.go:170` must be rewritten). Fields: `view`, `size` (int 256..2048), `quality`, `background`, `assume_unit` (R-03). Applicability: convert accepts only `quality`; `quality` on STL source gives `opts_not_applicable` (R-03). Wire into the API at `internal/api/handlers.go:868-874` (copy the audio branch):
```go
audioOpts, err := convert.ParseAudioOpts([]byte(rawOpts))
...
if err := convert.ValidateAudioApplicability(engine, detected, target, audioOpts); err != nil {
```
And the worker re-parse in `descriptors.go:220` (`OptsFromMap: func(m map[string]any) (any, error) { return convert.AudioOptsFromMap(m) }`), now `CADDescriptor.OptsFromMap`.

---

### `internal/convert/cadrender.go` (service, transform) and `cad.go` mods

**Analog:** `internal/convert/cad.go`. Register two converters because `Operation()` returns one string per converter (R: one embedded stage struct, `CADConverter` convert + `CADRenderConverter` render).

Stage pattern to copy (cad.go:143-153), swapping set/engine and argv:
```go
func (c CADConverter) meshStage(ctx context.Context, jobDir, format, inPath, outPath string) (cadVerdict, error) {
	x, err := c.cadStage(jobDir, cadMeshReadOnly, cadMeshEngines)
	if err != nil { return cadVerdict{}, err }
	timeout := c.meshTimeout()
	stageCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, cpu, err := x.runBounded(stageCtx, cadOutputLimit, "cad-mesh", "--format", format, inPath, outPath)
	return classifyCADStage(cadStageMesh, ctx, stageCtx, timeout, out, cpu, err)
}
```
Convert pipeline order to copy (cad.go:89-118): `OptsFromMap`, extension/pair check, `os.Stat` + `cadMaxBytes` ceiling (add `stl`, 191 MiB, R-02), `guardStage` (skip for STL), `meshStage`, `judgeBinarySTL`, `requireSTLMatchesVerdict`. Render adds: triangle cap check (3,806,000 pending Wave 0, terminal `ErrCADTooComplex`), zero-extent check (`ErrCADEmptyResult`), `renderStage`, `judgePNG` (Go `image/png`, rules J0-J4 in RESEARCH Pattern 2; differential test vs `scripts/cad-measure/pixelcheck.py`).

Verdict struct to extend with optional render fields (cad.go:155-161; unknown keys are ignored by `json.Unmarshal`). New sentinels in the `var (...)` block at cad.go:40-47, same style: `ErrCADRenderTimeout` (terminal, `cad_too_complex`, R-01), `ErrCADRenderRejected` (terminal, `cad_empty_result`, R-06). Classification: in `classifyCADStage` (cad.go:193-235) the `stage == cadStageMesh` branch at L212 is where a render-timeout sentinel branch goes; non-timeout render failures fall through to the existing transient returns.

Constants (cad.go:21-38) to add: `CADDefaultRenderTimeout = 480 * time.Second`, `cadStageRender`, `CADMaxSTLBytes`, triangle cap; `CADDefaultEngineTimeout` becomes guard+mesh+render+IO = 886 s (R-01).

---

### `internal/convert/cadsandbox.go` (modify)

Existing sets (L26-39):
```go
var cadMeshReadOnly = []string{"/opt/venv", "/usr/bin/python3.11", "/usr/lib/python3.11"}
var cadGuardReadOnly = []string{"/usr/bin/python3.11", "/usr/lib/python3.11"}
var cadMeshEngines = []string{"cad-mesh"}
var cadGuardEngines = []string{"cad-guard"}
```
Add `cadRenderReadOnly` (may equal mesh paths; measured on the production image, R-09) and `cadRenderEngines = []string{"cad-render"}`. `cadStage` (L43-54) stays unchanged (appends `/usr/lib/<triple>` and `LP_NUM_THREADS=2`). `ValidateCADTimeouts(guard, mesh, engine)` (L61-75) gains a render argument following the same `{name, value}` loop and `guard+mesh+render+CADIOAllowance > engine` check; `ConfinedCAD` (L77-87) passes it through; `cmd/cad-worker/main.go:51-58` gets `CAD_RENDER_TIMEOUT` via `envDuration`.

Preflight (L104-179) is the pattern for the render proof: positive run inside the job dir, judged by content, then negative run on a file outside via `requireCADDenied` + `requireDenied`. Extend: render the 12-triangle mesh fixture and judge the PNG by pixels (`judgePNG`), and add a render-stage negative control. `cadPreflightSTEP` embeds `testdata/cad/synthetic/units_inch.step` (L16-17); `requireOutsideGrants` (L122) gets `cadRenderReadOnly`.

Tests copy: `TestCADSetsAreTheMeasuredOnes` (cadsandbox_test.go:180), `TestCADGuardSetHasNoPathToTheReader` (:203), `TestCADSetsGrantNoSharedWritableOrProcessPath` (:212), `TestCADStagesRunThroughTheHelperUnderTheirOwnRulesets` (cad_test.go:549). Fixture helpers: `newCADFixture`, `cadSay`, `cadSayTriangles`, `cadBoxSTL` (cad_test.go:35-122); the hermetic render test uses a stub `cad-render` shell script like the stub guard/mesh.

---

### `internal/convert/cadmeasure.go` (utility, transform)

**Analog:** `internal/measurements/measurements.go` types (L66-83):
```go
type Document struct {
	Schema, Format, Unit, UnitSource string
	BBox BBox
	Solids, ClosedSolids *int  // omitempty; forbidden for stl
	Volume *float64; VolumeNullReason string
	Area *float64;  AreaNullReason string
}
```
Use `measurements.Validate(raw)` / `Document.Validate()` before upload; vocab via `AllUnits()`, `AllNullReasons()`. Unit rules (R-04/R-05): divide by factor table (mm 1, cm 10, m 1000, inch 25.4, foot 304.8, micrometre 0.001) `length/f`, `area/f^2`, `volume/f^3`; `assume_unit` only fills a missing unit (`client_assumed`); STL is `unspecified`/`none` unless assumed; out-of-vocabulary text unit becomes `unspecified`/`none`; `invalid_solid` additive for STL zero volume (doc + `doc_sync_test.go` in the same commit).

---

### `internal/worker/descriptors.go` (modify `CADDescriptor`)

Copy the existing descriptor (L668-715) and extend; copy `probeSideOutputs` (L410-423) shape for the second output, but the body is the measurements JSON built from the converter's draft:
```go
var CADDescriptor = EngineDescriptor{
	Engine: convert.EngineCAD, Type: queue.TypeCADJob,
	OptsFromMap: func(m map[string]any) (any, error) { return convert.CADOptsFromMap(m) },
	IsTerminal: isCADTerminal, FailureCode: cadFailureCode,
}
```
`SideOutput` struct is at `runner.go:109` (`Role, Filename, Format, ContentType, Body`); `jobs.OutputRoleMeasurements` already exists (`jobs.go:98`). Add the two new sentinels to the `isCADTerminal` list (L683-690) and `cadFailureCode` cases (L698-710); render timeout maps to `CodeCADTooComplex` with its own message, rejection to `CodeCADEmptyResult` with its own message (not the engine_error text).

### `internal/worker/worker.go` `process()` and `internal/jobs/repo.go` `AddOutputs` (CRUD, atomic)

Today (worker.go:1481-1511): primary `AddOutput` committed first, then per side output `uploadBytes` + `AddOutputWithRole` with `ordinal := i + 1`, then `MarkDone`. New order (R-07): build and validate both artefacts in memory, upload both objects (`storage.OutputKey(job.ID, ordinal, filename)`, `h.uploadBytes`), one `repo.AddOutputs`, then `MarkDone`. Keep the `if h.sideOutputs != nil` hook seam; the hook now runs before any row write.

`AddOutputs` pattern: upsert SQL from `AddOutputWithRole` (repo.go:646-662, includes `role = EXCLUDED.role`, rejects empty role) executed per output inside `pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {...})` (same helper as `transition`, repo.go:207-233). The inversion test (DB-gated) forces the second insert to fail and asserts zero visible outputs, with a naive-sequence control (R-07). Copy the test style of `TestAddOutputWithRolePersistsAndUpserts`. `MarkDone` still requires exactly one `primary` row.

---

### `Dockerfile.cad-worker` (modify)

Current structure: `venv` stage (python venv + `pip install --require-hashes -r /req/requirements-mesh.txt`), `engine` stage (apt `python3 libgl1 libx11-6 ca-certificates`, copy venv, copy helpers, `chmod 0755`, import smoke test, `USER nobody`), `build`, final. Add per RESEARCH "Installation": second hash-locked `pip install` of `requirements-vtk.txt`, copy keep-list files, run `vtk_keeplist.py prune`, add `libosmesa6` only, copy `cad-render`, extend `chmod` and the import smoke test with `vtkmodules.*`. Smoke-import line to extend:
```dockerfile
RUN /opt/venv/bin/python -I -B -c "import OCP.STEPControl, OCP.IGESControl, OCP.BRepMesh, OCP.StlAPI, OCP.BRepBndLib, OCP.Interface"
```
Copy the VTK/prune and smoke-test lines from `Dockerfile.cad-measure`. Gate: `scripts/cad-prune-identity.sh` against the 71 renders (R-11).

---

## Shared Patterns

### Stage run, verdict, classification
**Source:** `internal/convert/cad.go:127-235` (`runBounded` + `parseCADVerdict` takes the LAST `CADVERDICT ` line of the running stage only + `classifyCADStage`).
**Apply to:** `renderStage`, the STL stats stage. Judge by content (STL/PNG decode), never by exit code alone.

### Helper verdict hygiene
**Source:** `cmd/cad-worker/helper/cad-mesh:174-187` (`os.dup(1)` / `os.dup2(2, 1)`).
**Apply to:** `cad-render`, stats path. VTK chatter must not forge a verdict.

### Error wrapping and sentinels
`fmt.Errorf("cad %s: %w: %s", stage, ErrCADxxx, reason)` (cad.go:204-208). Library code never logs; HTTP never leaks internal text; worker terminal via `IsTerminal` list (`asynq.SkipRetry` applied by `Runner`).

### Strict opts parse, API + worker
**Source:** `audioopts.go` + `handlers.go:868-874` + `descriptors.go:220`.
**Apply to:** CAD opts (create-time 422 `invalid_options` / `opts_not_applicable`; worker re-parse is strictness-only).

### Landlock + preflight proof
**Source:** `cadsandbox.go:104-179`, `scripts/conf05-crosscheck.sh` (`cadguard cadmesh` classes at L62-63; add `cadrender`), `.planning/research/SANDBOX-PATHS.md` `## cad`.
**Apply to:** the new render set; negative control mandatory.

### Code comments
Per user global CLAUDE.md: no narrative comments in new code; rationale goes to the PR body (existing Go files contain many; do not copy them).

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| `judgePNG` (in `cadrender.go`) | utility | transform | No Go PNG judge exists. Port rules from `scripts/cad-measure/pixelcheck.py:58-86` (`stats`, `single`), extended per RESEARCH J0-J4; test against `pixelcheck.py` differentially. Nearest Go shape: `judgeBinarySTL` (cad.go:246-301). |
| `Repo.AddOutputs` transactional two-row write | repo | CRUD | No multi-output transactional insert exists. Compose `AddOutputWithRole` SQL with the `pgx.BeginFunc` pattern from `transition`. |
| numpy STL stats with area | helper | transform | 71 `cad_mesh.py:429-476` has stlstats without area; area is new. |

## Metadata

**Analog search scope:** `cmd/cad-worker`, `internal/convert/cad*.go`, `internal/convert/audioopts.go`, `internal/worker/{descriptors,worker,runner}.go`, `internal/jobs/repo.go`, `internal/measurements`, `scripts/cad-measure`, `Dockerfile.cad-worker`.
**Pattern extraction date:** 2026-10-05
