# Phase 74: 3D Render & Measurements - Research

**Researched:** 2026-10-05
**Domain:** confined VTK/OSMesa render stage, OCP/numpy measurements, two-output atomic job completion on the existing `cad` class
**Confidence:** MEDIUM. The stack, code seams and 71/73 numbers are read from files this session. Three numbers are unmeasured and need a live Wave 0 probe (2048 px frame cost at `LP_NUM_THREADS=2`, `nist_ctc_05` unit scaling, production image size delta). Docker was not run, per the brief.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** Stages run as guard → mesh → render. Each stage is its own confined helper with its own measured Landlock ruleset.
  - **guard:** unchanged from phase 73. For STL input it gives `ok` with no CAD caps; the phase 72 sniffer has already validated the STL.
  - **mesh:** OCP. It writes the tessellated STL plus a measurements draft (bbox, solids, closed_solids, volume, area, unit, unit source) computed with OCP. For STL input there is a numpy STL stats path with no OCP: exact bbox, and volume and area only when the mesh is watertight.
  - **render:** VTK + OSMesa, with no OCP. It reads only the STL from mesh and writes the PNG. This is the measured ruleset gap from 71-07: render does not need `/opt/venv` OCP libs, only VTK.
- **D-02:** The engine image gains the VTK wheel from the approved 71 lock (`requirements-vtk.txt`, vtk 9.7.1). The image is pruned by the 71 architecture-neutral keep-list (`scripts/cad-measure/vtk-keeplist.txt` and the `vtk_keeplist.py` logic, copied into the production build stage, not shared). No new packages are added. Any apt additions must be a subset of the 71 measurement image list, for example `libosmesa6`; anything else is a supply-chain checkpoint.
- **D-03:** Failure classes:
  - A render-stage failure is transient (retryable); the mesh stage already produced a valid STL.
  - A mesh-stage failure stays terminal, as in phase 73.
  - The triangle cap 4014975 (from 71-DECISION) is checked after mesh and before render. Exceeding it gives terminal `cad_too_complex`.
  - The render stage timeout is derived from the 71 render evidence, using the same method as phase 73. The stacking hazard (61.6 s vs 1.2 s) is deferred to phase 76, and the derived timeout must cover the measured worst case below the caps.
- **D-04:**
  - `view`: one of iso (default), front, back, left, right, top, bottom. Camera definitions come from 71-DECISION: orthographic, Z up, fit by bbox, never `ResetCamera`.
  - `size`: a square integer from 256 to 2048, default 1280. The 2048 ceiling is a recommendation; research validates its render RSS and time against the 71 evidence. If those exceed the pod budget, lower it and record why.
  - `quality`: draft, standard or fine. The tessellation constants are relative to the bbox diagonal, using the 71 STACK.md values. For convert as well as render, a finer level gives at least as many triangles, tested on the same part.
  - `background`: `opaque` (default, white #FFFFFF) or `transparent` (an RGBA PNG whose background alpha is 0).
  - Any unknown key or value is a 422.
- **D-05:** `quality` on convert (step/iges → stl) is the same option with the same constants. Phase 73's default (`standard`) stays the default.
- **D-06:** The worker judges the PNG by content before upload: the frame is not empty, the part fits with a margin, and the view reported by the helper matches. The 71 frac-only rule falsely rejected a thin real part (71 W15), so the judge uses projected-extent/aspect-aware criteria. At minimum it applies the 71 "sliver" mode, extended to aspect below about 0.1. Research tunes this on the corpus so that every real corpus part and the thin STL prefix pass, and an empty frame fails. A failed judgment is a terminal output failure, not silently uploaded. Preflight renders a fixture and judges it by pixels.
- **D-07:** Both outputs are produced, validated and uploaded before the job is marked done.
  - The PNG passes the pixel judge.
  - The JSON passes `measurements.Validate`.
  - If either fails, the job fails and no output row is visible. This is proven by an inversion test at the repository level: force the second write to fail and assert the client sees zero outputs.
  - `handleGetJob` `download_url` points to the PNG; the role selection from phase 72 already guarantees this.
  - A webhook payload gets the same treatment.
- **D-08:** The rules of the phase 72 validator are binding.
  - Volume and area come only from closed solids. Otherwise they are null, with a reason code.
  - STL: area is computed from triangles when the mesh is watertight, and both volume and area are null with `stl_not_watertight` otherwise. This follows the phase 72 pairing rule; 72-VERIFICATION flagged it for this phase.
  - Units follow the source:
    - STEP and IGES: the declared unit, with source `declared`. OCP reports values in mm after scaling (phase 73 R-01), but the document reports the declared unit and the values in that unit. Research decides whether to convert back or to report mm with the declared unit noted; the honest choice must keep "STEP in inches with assume_unit=mm stays inches".
    - STL: `unspecified`, with source `none`.
  - `assume_unit` fills only a missing unit, with source `client_assumed`. It never overrides a declared unit.
  - IGES inch scaling is unproven (73 GAP). Report the declared unit and record the gap; do not assert a scale.
- **D-09:** The render stage's Landlock set is measured on the production image by the 71/73 method: a removal table, a negative control, and judgment by pixels. The result is recorded in SANDBOX-PATHS `## cad` and held by the Go test and conf05-crosscheck. The mesh and guard sets are re-measured only if their argv or imports change; the measurements code adds OCP calls, so re-check the mesh set.

### Claude's Discretion

- Helper names and the JSON handoff format between stages.
- Whether measurements are emitted by mesh or by a separate small stage, as long as D-01's "render has no OCP" holds.

### Deferred Ideas (OUT OF SCOPE)

- DXF render: phase 75.
- Render time guard for stacked duplicates, minReplicaCount, measured timeouts, OpenAPI/MCP docs: phase 76.
- amd64 runtime Landlock measurement. IGES inch scaling proof (no corpus file).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| RNDR-01 | `render` step/stp/iges/stl → png, non-empty image | New `render` operation on the `cad` engine (DB CHECK already allows `'render'`); helper = verbatim port of `cad_render.py`; Go pixel judge (section "Pixel judge") |
| RNDR-02 | closed view vocabulary, Z up, fit with margin, unknown → 422 | 7 views and camera constants quoted below; `ParseCADOpts` closed enum |
| RNDR-03 | size option, default 1280, hard ceiling, out of range → 422 | 256..2048 range; ceiling arithmetic in "Size ceiling" |
| RNDR-04 | `quality` selects bbox-relative tessellation for render and convert | 71 constants (draft 0.004/0.5, standard 0.001/0.35, fine 0.0003/0.2); `cad-mesh --quality` |
| RNDR-05 | opaque default, `background: transparent` option | `SetBackgroundAlpha`, `SetAlphaBitPlanes(1)`, RGBA grab; alpha judge |
| MEAS-01 | two outputs, both visible or neither | `AddOutputs` single-transaction repo method + inversion tests |
| MEAS-02 | exact bbox, solids; volume/area only from closed solids else null with code | OCP calls in "Measurements"; ROW walk-through |
| MEAS-03 | units honest by source, with provenance | convert-back-to-declared-unit rule; STL `unspecified`/`none` |
| MEAS-04 | `assume_unit` fills only a missing unit, `client_assumed` | `assume_unit` is NOT in D-04's option list but is required here: add it to the closed render options |
</phase_requirements>

## Summary

The phase is mostly assembly of measured parts. The render helper is a near-verbatim port of `scripts/cad-measure/cad_render.py`; it must stay verbatim in its VTK imports and code paths because the pruned VTK keep-list (77 `vtkmodules` entries) was collected from exactly those branches, and a missing module fails at runtime under `nobody`, not at build. Measurements move OCP calls from `cad_mesh.py` into the production `cmd/cad-worker/helper/cad-mesh`, which today has none. A numpy STL stats path is needed because the 71 `stlstats` computes volume but no area, and the validator requires volume and area to be null together.

Three findings change the plan relative to CONTEXT. (1) The `/opt/venv` Landlock grant cannot be dropped for render: VTK lives in the same venv, and 71-07 lists `/opt/venv` as load-bearing for `cad_render`. The render set equals the mesh set in paths; the stages differ by the exec grant and argv. (2) Timeout arithmetic collides with the 900 s reconciler cap: the 73 method (1.5 x worst in-cap render row 437.36 s = 656 s) gives an engine timeout of 1062 s. (3) `GET /v1/jobs` already hides outputs unless status is `done`, but today's `process()` commits the primary row before the second output, and `MarkDone` only requires one primary. A real both-or-neither guarantee at repository level needs one transaction for both rows.

**Primary recommendation:** Add a `render` operation (registered by a second converter type sharing one confined stage set), keep one venv with VTK pruned by the copied keep-list, write both output rows in one DB transaction after both objects are uploaded, convert measurements back to the declared unit in Go, and judge the PNG in Go with an extent-first, aspect-aware rule that keeps every 71-passing frame passing.

## Working-tree note for the planner

When this research finished, the working tree held uncommitted edits by someone else to `cmd/cad-worker/helper/cad-mesh`, `internal/convert/cad.go`, `cadsandbox.go` and `cad_test.go` (observed with `git diff`; not part of this research and not committed by it). They add a mesh-completeness check to `cad-mesh` (`IMeshData` status flags, a per-face triangulation survey, an `incomplete` verdict mapped to `ErrCADUnreadable` in `classifyCADStage`) and a `requireSTLMatchesVerdict` cross-check in `Convert` and `CADPreflight`. Consequences for phase 74: the mesh helper now imports `OCP.IMeshData`, `OCP.TopLoc` and `OCP.TopoDS` and calls `BRep_Tool.Triangulation_s`, which strengthens the case for re-running the mesh Landlock table (D-09); the render converter should reuse `requireSTLMatchesVerdict`; plan against the tree after those edits land, not against `HEAD`.

## Project Constraints (from CLAUDE.md)

- No code comments by default; allowed only for an external constraint the code cannot express (user global CLAUDE.md). Code examples below carry none. Design rationale goes to the PR body.
- Go 1.26; verify with the pinned toolchain `GOTOOLCHAIN=go1.26.4`, not the local one (memory: pinned-toolchain).
- Library code (`internal/*`) never logs; only `cmd/*/main.go` logs.
- Wrap errors `fmt.Errorf("<action>: %w", err)`; sentinels `Err<Reason>`; HTTP never leaks internal error text.
- Stage failures classify through `EngineDescriptor` (`IsTerminal`, `FailureCode`), `asynq.SkipRetry` for terminal.
- Local gates, not GitHub CI (`ci.yml` is `workflow_dispatch` only). Live gates run sequentially.
- Verify against the goal, not the requirement text; "a scan of nothing is never clean": the pixel judge must fail an empty or unexamined frame.
- Test against the parser, do not predict it: the Go judge needs a differential test against `pixelcheck.py` on the same PNGs.
- GSD workflow: edits go through a GSD command; this research writes only `74-RESEARCH.md`.
- Phase 74 PR: assignee `@me`, request `@codex review`, no agent footer.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Option parsing and applicability (422 `invalid_options` / `opts_not_applicable`) | API / Backend (`internal/convert` parser, called by `internal/api`) | Worker (strict re-parse only) | Single validation authority; worker re-parse is strictness-only (runner.go:220-241) |
| Format-pair gate, per-format byte ceiling | API / Backend | Worker (re-check) | `maxFormatBytes[EngineCAD]` keyed by format |
| Entity/NAUO bomb guard | Confined guard helper (Python stdlib) | Go classifier | Unchanged from 73; STEP/IGES only |
| Tessellation, measurements (OCP) | Confined mesh helper | Go (unit conversion, validation) | OCP only in this stage |
| STL stats (bbox, watertight, volume, area) | Confined mesh helper (numpy path) | Go | No OCP for STL input |
| Rasterisation | Confined render helper (VTK+OSMesa, no OCP) | — | Reads one STL, writes one PNG |
| Pixel judgment | Go worker (`image/png`) | Helper-reported facts | No PIL in image; judge by content, not exit code |
| Measurements document assembly and `Validate` | Go worker | — | Declared-unit conversion and `assume_unit` live in testable Go |
| Two-output persistence | Database (`jobs` repo transaction) | Object storage | Rows appear atomically; S3 objects uploaded first |
| `download_url` selection, webhook payload | API / Worker (role-driven, unchanged) | — | Already selects role `primary`; test exists |

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| vtk (PyPI wheel) | 9.7.1, cp311, hashes in `scripts/cad-measure/requirements-vtk.txt` | Render stage | Approved 71 lock; user-approved renderer [CITED: 71-DECISION.md, `renderer_user_approval`] |
| cadquery-ocp-novtk | 7.9.3.1.1 | Mesh stage, measurements | Already in `cmd/cad-worker/requirements-mesh.txt` (73) |
| numpy | 2.4.6 | STL stats path | Already pinned in `requirements-mesh.txt` |
| libosmesa6 (apt, bookworm) | 22.3.6-1+deb12u2 per 71 image header | OSMesa software GL | In the 71 measurement apt list: `python3 libosmesa6 libgl1 libx11-6 librsvg2-bin fonts-dejavu-core` [VERIFIED: Dockerfile.cad-measure] |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| Go stdlib `image/png` | go1.26.4 | Pixel judge | Decode PNG in the worker; no new Go dependency |
| `internal/measurements` | in repo | Document validation | Every measurements body, before upload |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| One venv holding OCP, numpy and VTK | Separate `/opt/venv-vtk` for render | Would give render a Landlock set without OCP, but the 71 pruning and Landlock evidence were measured with one venv; a second venv is unmeasured work. Not recommended |
| Python PIL pixel judge | Go `image/png` judge | PIL is not in the production venv and adding it is a new package. Use Go |
| Per-stage timeouts only | One `CAD_ENGINE_TIMEOUT` | Keep 73's structure: stage deadlines inside one engine deadline |

**Installation (production image, `Dockerfile.cad-worker`):**

```bash
cp scripts/cad-measure/requirements-vtk.txt cmd/cad-worker/requirements-vtk.txt
cp scripts/cad-measure/vtk_keeplist.py scripts/cad-measure/vtk-keeplist.txt cmd/cad-worker/
```

Dockerfile changes, all inside the existing structure:

```dockerfile
COPY cmd/cad-worker/requirements-vtk.txt /req/requirements-vtk.txt
RUN /opt/venv/bin/pip install --no-cache-dir --only-binary=:all: --no-deps --require-hashes \
      -r /req/requirements-vtk.txt
COPY cmd/cad-worker/vtk_keeplist.py cmd/cad-worker/vtk-keeplist.txt /tmp/keeplist/
RUN /opt/venv/bin/python -I -B /tmp/keeplist/vtk_keeplist.py prune \
      --keeplist /tmp/keeplist/vtk-keeplist.txt --site /opt/venv/lib/python3.11/site-packages \
 && rm -rf /tmp/keeplist
```

In the `engine` stage add `libosmesa6` to the existing `apt-get install` (`python3 libgl1 libx11-6 ca-certificates` become `python3 libgl1 libx11-6 libosmesa6 ca-certificates`), copy `cmd/cad-worker/helper/cad-render`, and extend the import smoke test with the `vtkmodules.*` modules listed in `Dockerfile.cad-measure`.

**Version verification:** `vtk==9.7.1` and its two hashes are copied from the approved lock; no version is chosen in this phase. The registry was queried through the legitimacy seam (below).

### Exact apt additions and image size delta (ask 3)

- apt additions: `libosmesa6` only. `libgl1` and `libx11-6` are already in `Dockerfile.cad-worker`. `librsvg2-bin` and `fonts-dejavu-core` are DXF (phase 75) and must NOT be added now. `libosmesa6` is a member of the 71 list, so D-02's subset rule holds. [VERIFIED: Dockerfile.cad-measure, Dockerfile.cad-worker, both read this session]
- Size delta: the VTK part is measured, the OSMesa part is not. `PRUNE_SIZE,vtk_full_mib=564.1,vtk_pruned_mib=288.7,...` [CITED: 71-07-prune.txt line 19], so the pruned VTK adds about +288.7 MiB uncompressed to the venv. `libosmesa6` plus its dependency closure (llvm) is unmeasured; its upper bound is the whole 71 apt layer, 105,889,001 compressed bytes, which also holds python3, libgl1, libx11-6, librsvg2-bin and fonts [CITED: 71-08-image-size.txt `LAYER` lines]. The plan must record `docker image inspect` before and after in the live gate; do not publish a number before that.
- Multi-arch: the wheel lock has 2 hashes [VERIFIED: grep count of `hash=` in requirements-vtk.txt = 2]; the 71 lock note states 2 hashes, which I read as aarch64 plus x86_64 [ASSUMED: platform of each hash not printed]. The keep-list is arch-neutral and an emulated amd64 build passed prune (`kept=77,removed=299`) and the import check [CITED: 71-DECISION.md "Образ", `71-07-prune-amd64.txt#AMD64_PRUNE`]. amd64 render output and amd64 Landlock are not measured; the render sufficiency run is arm64 only (consistent with 71 "Не замерено").

## Package Legitimacy Audit

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| vtk | PyPI | latest release 2026-09-28 (registry `publishedAt`) | not reported | vtk.org | SUS (`too-new`, `unknown-downloads`) | Flagged. Approved by the user in phase 71 as a hash-locked wheel (71-DECISION: renderer approved 2026-10-04; lock files admitted, `pip --require-hashes` only). Planner must add a `checkpoint:human-verify` that records that approval before the first install in `Dockerfile.cad-worker` |
| numpy | PyPI | latest release 2026-09-06 | not reported | none listed | SUS (same two signals) | Already installed in production by phase 73 from the same lock; no new install. Mention only |
| cadquery-ocp-novtk | PyPI | — | — | — | not re-run | Already shipped in phase 73 (hash-locked); no change |

**Packages removed due to SLOP verdict:** none.
**Packages flagged SUS:** vtk (new install), numpy (existing). The seam's `SUS` here is recency only: the signal is the latest upload date, not the pinned artifact. The hash lock is the real control.

*Command run: `gsd-tools query package-legitimacy check --ecosystem pypi vtk numpy`.*

## Architecture Patterns

### System Architecture Diagram

```
client POST /v1/jobs  operation=render  file=part.{step,stp,iges,igs,stl}  opts={view,size,quality,background,assume_unit}
   |
   v
API: detect format (phase 72 sniffers) -> registry gate (render, step|iges|stl, png) -> per-format byte ceiling
     -> ParseCADOpts (closed, strict) -> ValidateCADApplicability(op, source) -> Postgres row + asynq cad:job
   |
   v
cad-worker (Runner, CADDescriptor): OptsFromMap strict re-parse -> MarkActive -> download in.<fmt>
   |
   +-- step/iges --> [guard] cad-guard (unchanged argv) --verdict--> ok
   |                    |
   |                    v
   |               [mesh] cad-mesh --format F --quality Q --measure /job/measure.json in out.stl
   |                    OCP: read, units pinned MM, GProp volume/area on closed solids, bbox, tessellate, STL write
   |                    |
   |                    +--> Go: judgeBinarySTL(out.stl) -> triangles, bounds ; cap check (triangles <= cap) else cad_too_complex (terminal)
   |
   +-- stl --------> [stats] cad-mesh stlstats (numpy): bbox, watertight, volume, area  (no guard stage, no OCP)
   |                    |
   |                    +--> Go: triangle cap check, zero-extent check (terminal)
   |
   v
[render] cad-render in.stl out.png --view V --size S --background B     (VTK+OSMesa, no OCP; CADVERDICT line on fd)
   |        failure here = transient
   v
Go: judgePNG(out.png, verdict facts, opts)  -> fail = terminal ErrCADRenderRejected
Go: build Document (unit rules, assume_unit) -> measurements.Validate -> body bytes
   |
   v
process(): upload out.png, upload measurements.json  (objects first)
           -> repo.AddOutputs(ctx, jobID, [png primary ord 0, json measurements ord 1])   ONE transaction
           -> repo.MarkDone (exactly-one-primary guard)
   |
   v
GET /v1/jobs/{id}: outputs listed only when status==done; download_url = role primary (PNG); webhook body download_url = role primary
```

### Recommended Project Structure

```
cmd/cad-worker/
├── helper/cad-guard            (unchanged)
├── helper/cad-mesh             (+ --quality, --measure, stlstats subcommand)
├── helper/cad-render           (new, port of scripts/cad-measure/cad_render.py)
├── requirements-mesh.txt       (unchanged)
├── requirements-vtk.txt        (copy of the 71 lock)
├── vtk_keeplist.py             (copy)
└── vtk-keeplist.txt            (copy)
internal/convert/
├── cad.go                      (+ quality, render timeout, triangle cap, verdict facts)
├── cadopts.go                  (new: CADOpts, ParseCADOpts, CADOptsFromMap, ValidateCADApplicability)
├── cadrender.go                (new: CADRenderConverter, render stage, judgePNG)
├── cadmeasure.go               (new: draft -> measurements.Document)
└── cadsandbox.go               (+ cadRenderReadOnly, cadRenderEngines, preflight extension)
internal/jobs/repo.go           (+ AddOutputs transaction)
internal/worker/                (process() ordering, CADDescriptor SideOutputs, sentinels)
```

### Pattern 1: Camera and view definitions (ask 1)

Values read from `scripts/cad-measure/cad_render.py` this session [VERIFIED: cad_render.py:19-36, 98-107, 161-177]:

```python
VIEWS = {
    "iso": ((1.0, -1.0, 1.0), (0.0, 0.0, 1.0)),
    "front": ((0.0, -1.0, 0.0), (0.0, 0.0, 1.0)),
    "back": ((0.0, 1.0, 0.0), (0.0, 0.0, 1.0)),
    "left": ((-1.0, 0.0, 0.0), (0.0, 0.0, 1.0)),
    "right": ((1.0, 0.0, 0.0), (0.0, 0.0, 1.0)),
    "top": ((0.0, 0.0, 1.0), (0.0, 1.0, 0.0)),
    "bottom": ((0.0, 0.0, -1.0), (0.0, -1.0, 0.0)),
}
FILL = (0.80, 0.80, 0.80)
EDGE = (0.08, 0.08, 0.10)
FEATURE_ANGLE = 30.0
CAMERA_DISTANCE = 2.0
LIGHTS = (
    ((-0.5, -1.0, 1.5), 0.80),
    ((1.0, 0.5, 0.7), 0.40),
    ((0.0, 1.0, -1.0), 0.25),
)
```

The same values are recorded in `71-04-render.txt` `VIEWDEF` lines and 71-DECISION ("Ракурсы - семь"), which agree with the code.

How 71 fitted the camera by bbox (no `ResetCamera`):

1. Bounds come from `poly.GetBounds()` of the STL; `center` = bbox midpoint, `diag` = bbox diagonal. `diag` not positive or NaN returns exit 3.
2. `direction = unit(view direction)`, `up = unit(up_hint - direction * dot(up_hint, direction))`, `right = unit(cross(-direction, up))`.
3. The 8 bbox corners are projected on `right` and `up`: `half_w`, `half_h` = max absolute projection from `center`. `long_half = max(half_w, half_h)`, `short_half = min(...)`.
4. `scale = long_half / (1.0 - 2.0 * margin)` with default `--margin 0.06`; `cam.SetParallelScale(scale)` with `ParallelProjectionOn`. For a square window the long axis spans `(1 - 2 x 0.06) = 88%` of the frame, so margin is 6% = 76.8 px at 1280. Observed margins on 35 real PNGs: 76 to 78 px [CITED: 71-04-render.txt PIX lines].
5. `cam.SetFocalPoint(center)`, `cam.SetPosition(center + direction * 2.0 * diag)`, `cam.SetViewUp(up)`, `renderer.ResetCameraClippingRange()`.
6. Pipeline: `vtkSTLReader` -> `vtkCleanPolyData` -> `vtkPolyDataNormals` (feature angle 30, splitting on) for the surface actor (ambient 0.45, diffuse 0.55, specular 0.0, `SetResolveCoincidentTopologyToPolygonOffset`); `vtkFeatureEdges` (boundary, feature and non-manifold edges on, manifold edges off, feature angle 30, `ColoringOff`) for the edge actor with colour EDGE, `LightingOff`, line width `max(1.0, size / 640.0)`.
7. FXAA: `renderer.UseFXAAOn()` with `win.SetMultiSamples(0)`. Transparent: `renderer.SetBackgroundAlpha(0.0)`, `win.SetAlphaBitPlanes(1)`, `grab.SetInputBufferTypeToRGBA()`. Opaque: background `(1,1,1)`, alpha 1.0. Window: `vtkOSOpenGLRenderWindow`, offscreen on, `SetSize(size, size)`.

The bbox-corner projection over-estimates the real silhouette in oblique views, which is why the W15 band spans 1030 px rather than 1126 px (see judge).

Port rules for the production `cad-render`:

- Keep every `vtkmodules` import and VTK call path identical to the 71 helper. The keep-list was collected from `/proc/self/maps` over all render branches; one extra VTK class loads a `.so` that `prune` removed. Evidence of the failure mode: after a stale list the render fails under `nobody` with `No module named 'vtkmodules.vtkFiltersCore'` [CITED: 71-DECISION.md "Обрезка VTK"]. The render test must run on the pruned image and cover opaque, transparent and 2048 px.
- Drop `--dump-maps`, `--margin` and the `.json` sidecar. Emit one stdout verdict line with the existing `CADVERDICT ` prefix, redirecting fd 1 to fd 2 as `cad-mesh` does, so VTK chatter cannot forge a verdict.
- Enforce `256 <= size <= 2048` and `background in (opaque, transparent)` in the helper (exit 64) as well as in Go. The 71 helper only checked `size >= 16`.
- Keep `os.environ["LP_NUM_THREADS"]` out of the helper; the Go side already sets `LP_NUM_THREADS=2` in `cadStage` [VERIFIED: cadsandbox.go `env := append(engineEnv(jobDir), "LP_NUM_THREADS=2")`].

Verdict line shape (new optional `Render` object is ignored by the existing `parseCADVerdict` unmarshal, since `json.Unmarshal` skips unknown keys):

```json
{"stage":"render","verdict":"ok","reason":"","triangles":5944,"view":"iso","size":1280,"background":"opaque","proj_half_w":12.3,"proj_half_h":8.1,"proj_aspect":0.66,"bbox":{"min":[0,0,0],"max":[1,2,3]}}
```

### Pattern 2: the pixel judge (ask 1, D-06)

Evidence table. `frac` is non-background pixels over the whole frame, `dark` is pixels with all channels below 80 over the whole frame, margin is the minimum distance from the pixel bounding box to the frame edge.

| Frame | N | frac | dark | margin px | pixel bbox | Source |
|---|---|---|---|---|---|---|
| real parts, 35 PNGs | 1280 | 0.1159 to 0.6537 | 0.0046 min | 76 min | long side 1067 to 1127 | `CALOBS,real_pngs=35,frac_min=0.1159,frac_max=0.6537,dark_min=0.0046,margin_px_min=76` [CITED: 71-04-render.txt] |
| robust fixtures (plate_iso, rod, lbracket) | 1280 | 0.2262 to 0.7766 | 0.0016 min (plate iso) | 76 | up to 1128 | 71-04-render.txt PIX |
| `face` (2 triangles, edge-on sliver) | 1280 | 0.0014 | 0.0014 | 77 | `77:639:1202:640`, 1126 x 2 | 71-04-render.txt line 176, `mode=sliver,ok=1` |
| W15 thin STL prefix | 1280 | 0.0022 | 0.0005 | 124 | `594:124:686:1153`, 93 x 1030 | `PIX,frac=0.0022,margin_px=124,dark=0.0005,bbox=594:124:686:1153,w=1280,h=1280,mode=part,ok=0` [CITED: 71-06 ladder par-off-lp2.txt line 1007] |
| empty frame | any | 0 | 0 | none | none | `pixelcheck.py` prints `ok=0` when `box is None` [VERIFIED: pixelcheck.py:66-68] |

W15 fails the 71 rule on two counts, not one: `frac` 0.0022 < `FRAC_MIN` 0.02 and `dark` 0.0005 < `DARK_MIN` 0.001. The 71 sliver mode did not fire because `proj_aspect` was not recorded for that row and its aspect (93/1030 = 0.090) is above `SLIVER_ASPECT` 0.02 anyway [VERIFIED: pixelcheck.py:26,77].

Proposed Go judge. `mask` = pixels that are not background: for opaque, any channel below 250 (the 71 rule); for transparent, alpha not 0. `c` = mask count, `bw`,`bh` = pixel bbox sides, `L = max(bw,bh)`, `S = min(bw,bh)`, `aspect_px = S/L`, `dark_count` = pixels with alpha not 0 and all channels below 80.

| Rule | Condition | Why / evidence |
|---|---|---|
| J0 frame | decoded size equals `size x size` and equals the verdict's `size` and `view` | D-06 "the view reported by the helper matches" |
| J1 non-empty | `c >= L` and `c > 0` | W15: c = 0.0022 x 1280^2 = 3605 >= 1030. `face`: 0.0014 x 1280^2 = 2294 >= 1126. Empty: 0 fails. The 71 sliver rule already used `mask.sum() >= extent` |
| J2 extent | `L >= 0.50 x N` | Observed smallest long side is 1030 (0.80 N, W15); 0.50 leaves 1.6x slack and rejects a dot or stray artifact |
| J3 margin | `min distance to edge >= int(0.03 x N)` | The 71 `MARGIN_MIN` rule; observed 76 against 38 required. Also fails an all-foreground frame |
| J4 blob or thin | blob: `0.02 <= frac <= 0.90` and `dark >= 0.001`; thin: `aspect_px < 0.15` and `dark_count >= 0.25 x L` | Blob is the unchanged 71 rule, so every frame that passed 71 still passes. Thin extends the sliver mode: W15 aspect 0.0903 < 0.15, dark_count about 0.0005 x 1.6384e6 = 819 >= 257.5; `face` aspect 0.0018, dark_count about 2294 |

Verdict: ok = J0 and J1 and J2 and J3 and (blob or thin). All 35 real PNGs and all robust fixtures pass J4 by blob (unchanged 71 thresholds) and J1 to J3 by large margins. W15 and `face` pass by thin. The empty frame fails J1. A frame filled edge to edge fails J3. A single dot fails J2.

Caveats the plan must close with a live calibration task on the production image (run `cad-render` for W15 and for the committed corpus, and compare Go judge output to `pixelcheck.py` on the same PNGs, a differential test):

- The W15 `dark` value is recorded to 4 decimals (0.0005), so `dark_count` is 819 +- 82; the threshold 257.5 has 3x slack.
- `aspect_px < 0.15` is wider than the CONTEXT hint "about 0.1" to leave 1.7x slack over W15's 0.0903; blob frames with aspect between 0.1 and 0.15 pass through blob or thin, so the extension cannot reject anything 71 accepted.
- Small sizes are unmeasured: at 256 px a 1 px edge line blurred by FXAA may leave `dark_count` below threshold for thin parts. Only 512, 1280 and 2048 were observed (slide: dark 0.0023, 0.0057, 0.0069) [CITED: 71-04-render.txt lines 213, 43, 215]. Calibrate at 256.
- A part whose silhouette is a dot (a thin rod along the view axis) fails J2. This is a deliberate fail-closed choice; document it.
- Hermetic unit tests build synthetic PNGs with `image/png` for each row of the table (empty, band 93 x 1030 at x=594,y=124 with about 3605 filled and about 819 dark pixels, sliver 1126 x 2, hexagon blob, all-dark frame, dot, transparent variants) so the judge is tested without VTK.

### Pattern 3: stage orchestration and stage deadlines

`CADConverter` today runs guard then mesh (`cad.go` `Convert`). The new render converter runs: size ceiling (existing `cadMaxBytes`, add `stl`), guard (STEP/IGES only), mesh or stats, triangle cap, render, judge, measurements document. Two registry entries are required because `OperationalConverter.Operation()` returns one string per converter [VERIFIED: convert.go:149-165]. Use one embedded stage struct and two thin types: `CADConverter` (operation `convert`, pairs `step`,`iges` to `stl`) and `CADRenderConverter` (operation `render`, pairs `step`,`iges`,`stl` to `png`). `cmd/cad-worker/main.go` currently calls `convert.Default.Replace(conv)` once; it needs one `Replace` per converter.

`cad-guard` rejects `--format stl`: `ap.add_argument("--format", required=True, choices=("step", "iges"))` [VERIFIED: cmd/cad-worker/helper/cad-guard:287]. CONTEXT D-01 says guard "gives ok" for STL; the helper cannot. Skip the guard stage for STL in Go. This leaves the guard argv and set unchanged, so it needs no re-measurement. The STL byte and triangle bounds are enforced by the API ceiling and the post-stats triangle cap.

### Anti-Patterns to Avoid

- **Importing anything new in `cad-render`:** breaks the pruned venv at runtime, after the Docker build passed.
- **Judging a render by exit code:** 71 learned rc=0 with `pix=fail` (the `stl-ascii-nasa-sls-block1-1MiB.stl` row).
- **Reusing `engine_error`'s message for render rejection:** it says "unsupported or corrupted input format".
- **Writing the primary row, then the second:** leaves a visible primary row if the second write fails (see Two-output atomicity).
- **Dividing OCP output by a unit factor chosen from text alone:** see unit rule; the factor must follow what OCP scaled by.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Closed-solid volume and area | Own tessellation integration | `BRepGProp.VolumeProperties_s`, `BRepGProp.SurfaceProperties_s` | Exact over B-rep; already proven in the 71 helper and corpus rows |
| Tight bbox with infinite-face fallback | New bbox code | `finite_box()` already in `cmd/cad-worker/helper/cad-mesh` | Handles the +-1e100 box 71 observed on `V-Slot-20x20-100mm.step` |
| Document validation | Ad hoc checks | `measurements.Validate` | Closed vocab, pairing rules, doc-synced |
| Strict JSON options | Custom decoder | `checkStrictObject` + `DisallowUnknownFields` pattern (`opts.go`) | Rejects trailing bytes, duplicate keys, unknown keys |
| Process confinement and kill | New exec wrapper | `engineSandbox.forJob(...).runBounded` and unchanged `exec.go` | Process-group kill proven in 73 |
| VTK pruning | Hand-edited file lists | `vtk_keeplist.py prune` with the copied list | Rejects a stale list; 77 entries |
| PNG decode | PIL | `image/png` | No new package |

**Key insight:** every hazard in this phase was found by a measured counterexample in 71 or 73 (W15, `V-Slot` infinite box, missing keep-list module, stacked copies). Reuse the measured code paths; the unmeasured edit is where the next defect is.

## Measurements (ask 4)

### OCP calls the 71 helper used [VERIFIED: scripts/cad-measure/cad_mesh.py:245-348]

| Quantity | Call | Notes |
|---|---|---|
| read | `STEPControl_Reader` / `IGESControl_Reader`, `ReadFile`, `TransferRoots`, `OneShape` | production `cad-mesh` already does this and pins `xstep.cascade.unit` to `MM` |
| solids | `TopExp_Explorer(shape, TopAbs_SOLID)` | count |
| closed | `BRep_Tool.IsClosed_s(shell)` over `TopAbs_SHELL` | a solid with any open shell is `open_solids` |
| valid | `BRepCheck_Analyzer(solid).IsValid()` | invalid closed solid is not counted closed |
| volume | `GProp_GProps()` + `BRepGProp.VolumeProperties_s(solid, props)`, `props.Mass()` | summed over closed valid solids only |
| area | `GProp_GProps()` + `BRepGProp.SurfaceProperties_s(solid, sprops)`, `sprops.Mass()` | same solids |
| bbox | `BRepBndLib.Add_s(shape, box)`; if `box.IsVoid()` or diagonal over `FINITE_LIMIT = 1e12`: per-face `BRepBndLib.AddOptimal_s(face, fbox, False, False)`, finite faces added | production `finite_box()` implements this |

Reason logic [VERIFIED: cad_mesh.py:307-321]: no closed solids: `no_solids` when `solids == 0`, else `open_shell` when `open_solids`, else `invalid_solid`; closed solids with `volume < 0` gives `negative_volume`; otherwise volume and area are the sums. Matches `cadCountRules` in `measurements.go` (`ReasonNegativeVolume: closed > 0`, `ReasonNoSolids: solids == 0`, and so on).

Two edits are needed that the 71 helper did not make:

1. **Exactness (MEAS-02 "точный bbox").** The 71 helper reports `BRepBndLib.Add_s` on the whole shape, which is the loose box (control points plus tolerance). Use `AddOptimal_s(shape, box, False, False)` for the document bbox and keep the finite-face fallback. Cost on large assemblies is unmeasured [ASSUMED]; re-measure the mesh wall in Wave 0 on the 61.6 MiB copies file.
2. **Zero volume.** The validator rejects `volume <= 0`. A closed valid solid whose `Mass()` is exactly 0 would produce an invalid document. Treat `Mass() <= 0` per solid as: negative maps to `negative_volume` only if the sum is negative; an exactly zero total demotes those solids out of `closed` and into `invalid`. The reason mapping must still satisfy `cadCountRules` (`invalid_solid` needs `closed_solids == 0`).

Time and memory: the 71 ladder row used for the 316 s mesh timeout (210.63 s at 63 MiB+) ran with this measurement code [CITED: 73 `DERIVED,key=cad_mesh_timeout_s ... floor=316_is_ceil_210.63x1.5_from_71_ladder_line_917`]. The 73 production helper measured 84.62 s at 61.6 MiB without it. Keep 316 s; re-run the 61.6 MiB row with the measurement code in Wave 0 and keep 316 s only if the wall is at most 210 s.

### Units: convert back to the declared unit (ask 4)

Recommendation: convert back to the declared unit in Go, and report that unit with `unit_source: declared`. Reporting mm would be false in two places. `unit_source: declared` means the unit comes from the file [CITED: docs/MEASUREMENTS.md "Unit sources"], and a document with `unit: mm` for an inch STEP would say the file declared mm. Using one `unit` field for all numbers and keeping `assume_unit=mm` from overriding a declared inch (D-08) requires the numbers to be in that unit.

Rule (Go, `cadmeasure.go`):

| Input | `unit` | `unit_source` | Numbers |
|---|---|---|---|
| STEP/IGES, declared unit in vocabulary | declared unit | `declared` | `length / f`, `area / f^2`, `volume / f^3`, with `f` = mm per unit |
| STEP/IGES, no unit stated or unit not in vocabulary | `assume_unit` if given | `client_assumed` | values as OCP produced them divided by the factor OCP applied (see below) |
| same, no `assume_unit` | `unspecified` | `none` | same |
| STL | `assume_unit` if given else `unspecified` | `client_assumed` / `none` | exact file values (no scaling) |

Factors `f` (mm per unit): `mm` 1, `cm` 10, `m` 1000, `inch` 25.4, `foot` 304.8, `micrometre` 0.001. These are exact definitions, not measured; mark them `[ASSUMED]` only as to implementation, the constants are standard.

Worked check on the 71 rows [CITED: internal/measurements/testdata/helpers-71-04-rows.txt]: `units_inch.step` OCP volume 98322.38 mm^3, `/ 25.4^3 = 6.000`; area 14193.51 mm^2 `/ 25.4^2 = 22.000`; bbox diagonal 95.038098 mm gives 3.7417 in, which is `sqrt(14)` for a 1 x 2 x 3 in box. The result equals `legacy_cp1251.step` (volume 6.0, area 22.0, diag 3.741658), a mm file of the same shape. The preflight fixture is this file.

Which factor did OCP apply? The 71 evidence is split:

- Proven: `units_inch.step` scaled inch to mm (`98322.384 mm^3 = 6 in^3`) [CITED: 71-DECISION "Расхождение с `FileUnits`"], and `CADPreflight` re-proves it on every start (25.4 / 50.8 / 76.2 mm extents) [VERIFIED: cadsandbox.go `cadPreflightExtents = [3]float64{25.4, 50.8, 76.2}`].
- Unproven: `nist_ctc_05_asme1_ap242-e1.stp` declares `CONVERSION_BASED_UNIT('inch')` in lowercase; OCP `FileUnits` returned an empty name, `unit_agree=0`, so scaling for that file "не доказано" [CITED: 71-DECISION, same section]. Circumstantial evidence only: its OCP bbox diagonal 907.17 is comparable to `nist_ctc_01` (mm) 930.05, which suggests OCP did scale it; this is an inference, not a proof [ASSUMED].
- IGES inch: unproven, no inch IGES in the corpus (73 `GAP,key=iges_inch_scaling,value=unproven`) [VERIFIED: 73-01-spikes.txt:165]. D-08: report the declared unit, record the gap, do not assert a scale.

Principled formula for the unsure case: divide by the factor of the unit OCP recognised (`FileUnits`), not by the text unit: `value_in_declared = ocp_value / f(FileUnits_or_mm_if_empty)`, labelled with the text unit. When OCP recognised the same unit as the text this is the table above. When `FileUnits` is empty OCP applied no scaling and the numbers are already in the file's own unit, so the factor is 1 [ASSUMED]. The plan needs a Wave 0 live probe on `nist_ctc_05` comparing its OCP bbox against the raw `CARTESIAN_POINT` extent in the file text. If they agree to float precision, scaling is 1 and the formula holds; if the OCP bbox is 25.4x larger, the factor must be 25.4. Until then record the case as an open gap in `docs/MEASUREMENTS.md` the same way 73 recorded IGES.

Do not use `unspecified` for "the reader failed to understand a unit": `docs/MEASUREMENTS.md` says it "means the file states no unit, not that the reader failed to understand one". A text unit outside the vocabulary (for example `CONVERSION_BASED_UNIT('furlong')`) is the one honest-versus-vocabulary conflict left; recommend `unspecified`/`none` plus a recorded gap, and flag it in Open Questions.

`assume_unit` (MEAS-04): vocabulary `mm cm m inch foot micrometre` (the document vocabulary minus `unspecified`); it applies only when the file states no unit; it never changes a declared unit, source `client_assumed`. It is NOT in CONTEXT D-04's option list; add it to the closed render options. It is meaningless for `quality`-only convert jobs: `opts_not_applicable` there.

### STL stats path (ask 4)

71 `stlstats` [VERIFIED: cad_mesh.py:429-476]: reads binary (size equals `84 + 50 n`) or ASCII (`re.findall` over `vertex` lines), exact bbox over float64-promoted vertices, watertight = every directed edge appears exactly once and its reverse exists and no degenerate edge (`a != b`), signed volume `sum(v0 . (v1 x v2)) / 6`, `negative_volume` when negative, `stl_not_watertight` otherwise.

Gaps against D-08 and the validator:

- **No area.** The helper emits `volume` but not `area`; `stl_ascii_box.stl` row has `volume 6000.0` and no area [CITED: helpers-71-04-rows.txt]. The validator requires volume and area null together or both numbers [VERIFIED: measurements.go `(d.Volume == nil) != (d.Area == nil)`]. Production must add `area = 0.5 * sum(norm(cross(v1 - v0, v2 - v0)))` over triangles when watertight.
- **Zero volume.** A watertight but flat (zero enclosed volume) mesh is not in the vocabulary. Recommend emitting `stl_not_watertight` for "no positive enclosed volume" and documenting that meaning in `MEASUREMENTS.md`; flag in Open Questions.
- **Memory.** The 71 `stlstats` row for the 205 MiB binary tile peaked at 2148 MiB RSS in 32.80 s [CITED: 71-06 ladder par-off-lp2.txt, `op=stlstats`, `stl-tile-nasa-sls-block1-200MiB.stl`, `rss=2148`]. Area adds temporaries; compute volume and area in 1 M-triangle chunks and free the vertex array before the unique-sort, then re-measure.
- **STL byte ceiling.** The 71 binary-STL ceiling 277 MiB is an extrapolation above the top rung; the triangle cap binds first: `4014975 x 50 + 84 = 200,748,834 B`, 191.45 MiB, so a 191 MiB per-format ceiling (`191 << 20 = 200,278,016`) is the tighter and measured-range number (the 196 MiB tile passed at 1692 MiB render peak [CITED: 71-06 ladder par-off-lp2.txt, `stl-tile-nasa-sls-block1-200MiB.stl`, render `cg=1692`]). ASCII 200 MiB passed the stats path at 858 MiB. Add `"stl": 191 << 20` to `maxFormatBytes[EngineCAD]`.
- Stats stage timeout: worst measured stats wall 32.80 s, times 1.5 = 49.2 s, far under the 316 s mesh timeout. Reuse `CAD_MESH_TIMEOUT` for the stats stage.
- A degenerate STL with bbox diagonal 0 makes `cad-render` exit 3 (zero extent). Check this in Go after stats and fail terminally with `ErrCADEmptyResult` before render, since a render-stage failure is classified transient.

### What the production emitter must output for each 71 ROW combination (ask 4)

Run through `Document.Validate` mentally; every row below satisfies the rules cited. Rows from `helpers-71-04-rows.txt` [CITED].

| 71 ROW | format | solids/closed | unit, source | volume, area | reason | Rules exercised |
|---|---|---|---|---|---|---|
| `LM8UU.step`, `SC8UU.step`, `LinearSlide...`, `V-Slot...`, `nist_ctc_01`, `nist_stc_06`, `assembly_nauo4`, `asymmetric_l_bracket`, `legacy_*.step`, `reversed_solid.step` | step | n/n closed | `mm`, `declared` | numbers (mm) | none | closed>=1 permits positive; bbox axes 3 |
| `units_inch.step`, `nist-d2mi-827-9999-906.stp`, `nist_ctc_05` | step | 1/1 | `inch`, `declared` | numbers divided by 25.4^3 and 25.4^2 | none | unit/`assume_unit` interplay; ctc_05 scale caveat |
| `degenerate_plate.step` | step | 1/1 | `mm`, `declared` | 1000.0, 20040.0 | none | volume>0 needs extent on all 3 axes: 0.1 thick plate passes |
| `degenerate_rod.step` | step | 1/1 | `mm`, `declared` | 157.0796, 629.8893 | none | same |
| `open_shell.step`, `single_face.step` | step | 0/0 | `mm`, `declared` | null, null | `no_solids` both | `solids == 0`; bbox for a flat face: min equals max on one axis but not all, so not a point |
| `surfaces_only.iges`, `iges_no_newlines.iges` | iges | 0/0 | `mm`, `declared` (IGES flag 2) | null, null | `no_solids` both | same |
| `stl_ascii_box.stl`, `stl_binary_solid_header.stl` | stl | absent | `unspecified`, `none` | 6000.0 and area from triangles | none | `solids`/`closed_solids` forbidden for stl; area computed (71 gave none) |
| `nasa-hubble-all-pieces.stl`, `nasa-jwst...stl` | stl | absent | `unspecified`, `none` | null, null | `stl_not_watertight` both | equal reasons on both |
| DXF rows | dxf | — | — | — | — | Phase 75; not emitted here |

With `assume_unit` set: STL rows become `client_assumed` with that unit; STEP/IGES rows with a declared unit stay declared (never overridden); a STEP/IGES file with no recognisable unit takes `client_assumed`.

## Size ceiling and render stage timeout (ask 2)

### Frame cost and RSS (arithmetic)

Evidence at `lp=default`, 2 CPUs, 5944-triangle part [CITED: 71-04-render.txt lines 212-215]:

| size | child_rss_mib | cg_peak_mib | wall_s | t_render_s |
|---|---|---|---|---|
| 512 | 207 | 89 | 0.74 | 0.356 |
| 1280 | 273 | about 154 | 0.92 | — |
| 2048 | 393 | 275 | 1.21 | 0.524 |

2048 adds 393 - 273 = 120 MiB of child RSS and 275 - 154 = 121 MiB of cgroup peak over 1280, about 48 bytes per added pixel. 2048 px is therefore bounded in time (+0.3 s) and costs a fixed ~121 MiB.

The 71 triangle cap derivation: `(0.70 x 4096 - 517.0 - 24.9) / 579.16 = 4.015 M` at 1280 [CITED: 71-DECISION.md, "Капы-кандидаты"]. `0.70 x 4096 = 2867.2`. At the cap and 1280 px the formula line is therefore exactly zero slack; any larger frame crosses the 70% line for the extreme file. At 2048:

- Slack-honest cap: `(2867.2 - 517.0 - 24.9 - 121) / 579.16 = 2204.3 / 579.16 = 3.806 M` triangles.
- Keeping 4,014,975: peak = `517.0 + 24.9 + 579.16 x 4.014975 + 121 = 2988 MiB`, 72.9% of 4096. That is 121 MiB over the 2867 line and still 1108 MiB under the pod limit.
- Every measured in-cap render row stays under the line at +121 MiB: the heaviest are `step-nauo-...-n1000` 3,816,000 triangles `cg=2035` (2156 with the frame) and `step-copies-freecad-...-80MiB` 3,931,578 triangles `cg=1926` (2047) [CITED: 71-06 ladder par-off-lp2.txt `op=render` rows].

Answer to ask 2: 2048 is safe on every measured row but not by the strict 70% formula at the exact cap. Recommendation: ceiling 2048, and lower the triangle cap to 3,806,000, recorded as a derived deviation from 4,014,975 (D-04 explicitly allows lowering to fit the pod budget). Cost: 3,806,000 sits 0.26% below the derived stress file `step-nauo-n1000` (3,816,000), which would be refused although it rendered at 2156 MiB; no real corpus part is near it (largest real render 436,842 triangles, peak 757 MiB). If the user prefers CONTEXT's literal 4,014,975, lower the size ceiling instead; the arithmetic gives no size above 1280 that keeps zero slack at that cap.

Caveat that blocks treating the 121 MiB as final: it was measured at `lp=default`; 71 found `lp=2` raises render RSS (STL 100 MiB 873 to 1113 MiB) [CITED: 71-DECISION "Потоки и тесселяция"]. Wave 0 must re-measure one 2048 px render at `LP_NUM_THREADS=2` on the heaviest in-cap file before the number is committed.

### Render stage timeout (73 method)

73 method for mesh [CITED: 73-01-spikes.txt:159]: `max_316_ceil_1.5x_largest_S5_mesh_wall`, i.e. the larger of the 71-ladder floor and 1.5 x the worst measured wall at or below the caps.

Render rows at `par=off,lp=2`, filtered to inputs within the byte ceilings (STEP 63 MiB, IGES 135 MiB) and the triangle cap, `rc=0`, `pix=ok` [CITED: 71-06 ladder par-off-lp2.txt; parsed this session, 146 rows]:

| Row | Triangles | Render wall | Note |
|---|---|---|---|
| `step-copies-LinearSlide-MGN15x-Block-40MiB.step` | 427,968 | **437.36 s** | stacked copies; the worst in-cap row |
| `step-copies-freecad-...-80MiB.step` | 3,931,578 | 95.96 s | 83 MB, above the 63 MiB STEP ceiling, excluded |
| `step-copies-LinearSlide-...-20MiB.step` | 213,984 | 61.26 s | stack |
| `step-copies-freecad-...-40MiB.step` | 2,184,210 | 22.59 s | |
| `step-nauo-...-n1000.step` | 3,816,000 | 11.63 s | |
| `nasa-pillars-of-creation-full.stl` (real) | 1,040,838 | 3.60 to 3.73 s | |

`step-copies-LinearSlide-...-80MiB.step` timed out at the 900 s cell budget (`rc=137`), but it is above the ceiling. The stacking cause: the time goes before VTK's render, `t_total_s 61.238` against `t_render_s 1.048` for 36 coincident copies, versus 1.18 s when the copies are spread (`71-06-render-stacking.txt`).

Arithmetic:

- 73 method on this evidence: `1.5 x 437.36 = 656.04`, so render 657 s. Engine = `30 + 316 + 657 + 60 = 1063 s`. That exceeds the 900 s `RECONCILER_ACTIVE_STALE_AFTER` cap that 73 relied on ("406s is under the 900s ... cap, so the reconciler cannot treat a running cad job as abandoned") [VERIFIED: .env.example:584]. A cap breach makes the reconciler re-enqueue jobs that are still running.
- Cap-limited alternative: engine must stay below 900 s, so render <= `900 - 406 = 494 s`; an engine of exactly 900 s equals the cap, so keep a margin: **render 480 s, engine 886 s**. It covers the worst measured in-cap wall 437.36 s by 1.10x, not 1.5x.
- Retry multiplier: render failure is transient by D-03 and `CAD_MAX_RETRY=2`, so a deterministic render timeout burns up to 3 x 480 s of pod time before failing. Flag for phase 76 (where the stacking guard lands).
- Pod grace becomes `886 + 20 = 906 s`; `ShutdownTimeout = engine + 10 s`.

Recommendation: render 480 s (cap-bound), engine `CAD_ENGINE_TIMEOUT=886s`, and record that stacked inputs above about 40 MiB are unbounded until phase 76; this is a user decision point (Open Question 1). Do not describe 480 s as "1.5x derived".

Constants and wiring that move with it: `CADDefaultEngineTimeout` in `cad.go`, `ValidateCADTimeouts` gains a render argument and a `CAD_RENDER_TIMEOUT` env, `docker-compose.yml` (4 places `CAD_ENGINE_TIMEOUT: "406s"`), `deploy/chart/octoconv/values.yaml:224` (`terminationGracePeriodSeconds: 426`), `.env.example` derivation block, `scripts/cad-liveproof.sh` bounds, `engineTimeouts` map (`api.go:457`) and the inspect estimate.

## Two-output atomicity (ask 5)

### What the code does today [VERIFIED: internal/worker/worker.go:1432-1511, internal/jobs/repo.go:254-297, 608-666]

`process()` order: upload primary object, `repo.AddOutput` (immediate, auto-committed row, role defaults to `primary`), then `h.sideOutputs` if the descriptor declares it: per extra output `uploadBytes` then `AddOutputWithRole`, then `repo.MarkDone`. `MarkDone` runs in the `transition` transaction, counts `job_outputs` rows with `role = 'primary'` and requires exactly one. It does not require a measurements row. `AddOutput` and `AddOutputWithRole` upsert on `(job_id, ordinal)`, so retries overwrite.

Visibility: `handleGetJob` calls `s.repo.Outputs` only inside `case jobs.StatusDone` [VERIFIED: handlers.go:1283-1286]; a failed or active job lists no outputs. `HandleWebhookDeliver` does the same for `StatusDone`. So a client never sees a primary-only done job unless the code writes `done` with one row; the existing guard allows exactly that.

Hazards with a naive reuse of `SideOutputs`:

1. The primary row is committed before the measurements output is produced or validated. If measurements fail afterwards, a primary row exists in a job that ends `failed`; `Outputs()` returns it, and any new endpoint or admin path that lists by job id would show one output.
2. `MarkDone` would succeed with only the PNG if the measurements hook returned an empty slice.
3. `TestOnlyProbeDeclaresSideOutputs` pins that only probe declares the hook [VERIFIED: probe_sideoutput_test.go function list]; it flips.

### Recommended design

Add `Repo.AddOutputs(ctx, jobID, outs []Output) error` that inserts or upserts all rows (with explicit roles) inside one `pgx.BeginFunc` transaction, so rows appear together or not at all. Then reorder `process()` for descriptors that declare extra outputs:

1. Produce and validate the extra outputs in memory first (hook returns bytes after `measurements.Validate`).
2. Upload the primary object and every extra object. Object keys are deterministic (`storage.OutputKey(jobID, ordinal, name)`), so a retry overwrites; an orphan object from a failed attempt has no row.
3. One `AddOutputs` call with the primary (ordinal 0, role `primary`) and the extras (ordinal 1.., role from the hook).
4. `MarkDone`.

The probe sanitize path keeps its behaviour (its sidecar already exists after `Convert`); only the DB write is batched. Worker tests that pin the old call order must be updated; the planner should run `go test ./internal/worker/...` to find them [ASSUMED: not enumerated here].

Also add the engine-agnostic belt: `MarkDone` is untouched (the repo says `transition` is an untouchable invariant), so the second-output requirement is enforced by the worker calling `AddOutputs` as the only write path for CAD render.

### Inversion tests (prove, not assert)

1. **Repository (DB-gated, `DATABASE_URL`, same harness as `output_role_test.go`):** `TestAddOutputsRollsBackWhenTheSecondRowFails`. Create a job, `MarkActive`, call `AddOutputs` with a valid primary at ordinal 0 and a second row whose role is outside the CHECK allowlist (the technique of `TestOutputRoleRejectsValueOutsideAllowlist`, which exists at output_role_test.go:121). Assert the call errors, `Outputs()` returns zero rows, `MarkDone` errors with "0 primary outputs", and the job is still `active`. Add a control in the same test: the old two-statement sequence (`AddOutputWithRole` primary, then the bad row) leaves one visible row. The control is what makes this an inversion: it shows the assertion can fail against the naive implementation.
2. **Worker (hermetic, fake repo and storage):** force the second upload to fail; assert zero `AddOutputs` calls, zero `MarkDone`, and the fake repo's `Outputs()` is empty. Force `AddOutputs` itself to fail and assert `MarkDone` is never called.
3. **Hook level:** feed a measurements body that `measurements.Validate` rejects (for example `volume: 0`); assert no upload and no row. Feed a PNG rejected by `judgePNG`; assert the same.
4. **Webhook:** `TestSetWebhookDownloadURL_NeverSelectsMeasurements` already exists [VERIFIED: webhook_role_test.go function list]; add a case with PNG at ordinal 0 and measurements at ordinal 1 in both orders. The webhook body carries only `download_url`; it contains no `outputs` array [VERIFIED: worker.go:960-1000 body builder reads].
5. **Live:** extend `scripts/cad-liveproof.sh` for one render job: both outputs present, roles `primary` and `measurements`, `download_url` fetches PNG magic bytes, measurements body validates; and a forced-failure job shows `status=failed`, no `outputs`, no `download_url`.

## Options validation (ask 6)

Existing pattern [VERIFIED: handlers.go:858-1010, opts.go, avopts.go, runner.go:220-241]:

- Create path: `rawOpts` empty or `{}` skips validation; over `maxOptsBytes` gives 422 `payload_too_large`; otherwise an engine-keyed `switch engine` with cases for audio, av, html, ocr, pandoc and a `default` that calls `convert.ParseDocOpts`. Parse failure gives 422 `invalid_options` ("invalid opts"); applicability failure gives 422 `opts_not_applicable` ("opts not applicable to this conversion"). The normalized struct is re-marshalled and stored, never the raw client bytes.
- CAD today has no `case`: a CAD job with any opts falls to `default`, where `ParseDocOpts` has `DisallowUnknownFields` and rejects `quality`, so it already returns `invalid_options`. Phase 74 adds `case convert.EngineCAD`.
- Worker: `EngineDescriptor.OptsFromMap(job.Opts)` re-parses strictly before `MarkActive`; failure writes `invalid_options` "stored conversion options are invalid" and `SkipRetry`. It receives only the map (no operation), so the persisted-opts parser must be one closed union struct; operation-specific applicability stays at the API only, as the code comment states ("the applicability/enum business rules already ran once, at the API layer").
- `CADOptsFromMap` today rejects any key: `cad takes no options, got %d` [VERIFIED: cad.go]. `CADConverter.Convert` also calls it. Both change, and `TestPinned...` and `cad_test.go` cases that pin "takes no options" flip.

Design:

```go
type CADOpts struct {
	View       string `json:"view,omitempty"`
	Size       *int   `json:"size,omitempty"`
	Quality    string `json:"quality,omitempty"`
	Background string `json:"background,omitempty"`
	AssumeUnit string `json:"assume_unit,omitempty"`
}

func ParseCADOpts(raw []byte) (CADOpts, error)
func CADOptsFromMap(m map[string]any) (CADOpts, error)
func ValidateCADApplicability(engine, operation, source, target string, o CADOpts) error
```

- `Size` is a pointer so explicit `0` is rejected as out of range rather than read as unset (the CR-04 lesson in `avopts.go`). Decoding `1280.5` or `"1280"` into `*int` fails in `json.Decoder`, giving 422.
- Closed enums: `view` iso front back left right top bottom; `quality` draft standard fine; `background` opaque transparent; `size` 256..2048; `assume_unit` mm cm m inch foot micrometre.
- Applicability: `operation=convert`: only `quality`; any other key gives `opts_not_applicable`. `operation=render`: all keys; `quality` on an `stl` source gives `opts_not_applicable` (no tessellation) [ASSUMED: CONTEXT says quality applies to step/iges tessellation; the STL-source case is not stated].
- Defaults are resolved in the converter (view iso, size 1280, quality standard, background opaque), not persisted, so stored opts stay what the client asked for.
- New create-path branch dispatches on `engine`, and inside it the applicability call receives `operation`. The `operation` variable is in scope at that point [VERIFIED: handlers.go:178-180, 1107].
- Quality constants (71 DECISION `quality_*`, linear coefficient and angular rad relative to bbox diagonal): draft 0.004/0.5, standard 0.001/0.35, fine 0.0003/0.2. Production `cad-mesh` hardcodes `LINEAR_DEFLECTION = 0.001`, `ANGULAR_DEFLECTION = 0.35`; add `--quality` with the table. 71 measured fine = 820,472 and draft = 273,208 triangles on the heaviest real STEP (9 MB) [CITED: 71-DECISION "Потоки и тесселяция"]; degenerate fixtures give 2/100/12 (non-strict monotonic). The test for "finer gives at least as many triangles" must use `>=`, not `>`, and run on one real part plus the 12-triangle fixture.

## Landlock (ask 7)

Render candidate set from 71-07 [CITED: 71-DECISION "Кандидаты Landlock", `71-07-landlock.txt#LANDLOCK_MINIMAL` line 218]: `rw=/job`, `ro=/opt/cad-measure:/opt/venv:/usr/bin/python3.11:/usr/lib/aarch64-linux-gnu:/usr/lib/python3.11`. Removable on render: `/etc/ld.so.cache`, `/etc/nsswitch.conf`, `/etc/passwd`, `/proc/cpuinfo`, `/sys/devices/system/cpu`, `/usr/lib/locale`, `/usr/share/zoneinfo`. Load-bearing: `/opt/cad-measure` (the 71 helper dir), `/opt/venv`, `/usr/bin/python3.11`, `/usr/lib/aarch64-linux-gnu`, `/usr/lib/python3.11` [CITED: 71-07-landlock.txt lines 201-217, `cad_render` REMOVAL rows]. `/proc`, `/sys` and `/tmp` are not needed (`PROC_SYS entries=none`, `TMP_NEED none`).

Production mapping [VERIFIED: cadsandbox.go]: the mesh set is `cadMeshReadOnly = {/opt/venv, /usr/bin/python3.11, /usr/lib/python3.11}` plus `/usr/lib/<triple>` (appended in `cadStage` when the sandbox confines) and engines `{"cad-mesh"}`; the guard set drops `/opt/venv`. `/opt/cad-measure` does not exist in production; the helper file itself is covered by the engines (exec) list.

Therefore:

- Candidate render set = `cadMeshReadOnly` in paths, with `cadRenderEngines = {"cad-render"}`. D-01's "render does not need `/opt/venv` OCP libs, only VTK" does not narrow the filesystem set: VTK is in `/opt/venv`, and 71 measured `/opt/venv` load-bearing for `cad_render`. The no-OCP property is enforced by the helper's imports and by a separate exec grant, not by paths. A narrower set would need a second venv, which is unmeasured work (Alternatives table).
- Define `cadRenderReadOnly` as its own slice (not an alias of `cadMeshReadOnly`) so a future divergence is a one-line change and `conf05-crosscheck.sh` can pin it separately.
- `scripts/cad-worker-sandbox-measure.sh` (596 lines, `STAGES="guard mesh"` at line 165, `stage_argv`, `signature`, `judge`, `make_refs` functions): add stage `render` with argv `/usr/local/bin/cad-render --view V --size S --background B /job/in.stl /job/out.png`. Its `signature` must be content, not exit code: sha256 of the PNG plus the Go-judge verdict (or `pixelcheck.py` for the measurement script, run on the host side), matching 71's `check=sig_equal+pixelcheck_ok`. Matrix: the 71 render inputs (slide and nasa-hubble, iso/front/top at 1280 opaque, iso 1280 transparent, iso 2048) plus the W15 prefix. Keep the per-entry removal table and add the negative control for render (drop one load-bearing entry, require the sufficiency judge to fail). Effect row: the same job outside all grants must be denied (`stl`/`png` absent, confined rc 2 with the `/etc/hostname` canary read before the ruleset, as in 73-03).
- The mesh set when measurements are added: the new OCP calls (`BRepGProp`, `GProp`, `BRepCheck`, `TopExp`, `TopAbs`, `BRep`) and numpy load from the same `/opt/venv`. No new path is expected [ASSUMED], but D-09 requires re-measurement because argv (`--quality`, `--measure`) and imports change: rerun the mesh removal table with STEP/IGES inputs plus the STL stats inputs (71 `cad_mesh` stlstats rows were sufficient under the same minimal set: `nasa-hubble`, `stl_ascii_box`, `stl_binary_solid_header` [CITED: 71-07-landlock.txt lines 136-138, 196-198]). The guard set and argv are unchanged: no re-measurement.
- Update three holders: `SANDBOX-PATHS.md` `## cad` (new render subsection, measured on the production engine image id), the Go constants with a pinning test like `TestCADGuardSetHasNoPathToTheReader`, and `scripts/conf05-crosscheck.sh cad` (currently prints `MATCH: cadguard readonly (2) engines (1)`, `MATCH: cadmesh readonly (3) engines (1)`, `MATCH: cad multiarch (2)` [CITED: 73-VERIFICATION.md]; add `cadrender`).
- Measured only on arm64. amd64 Landlock for render is deferred (CONTEXT) and must be stated, not implied.

## Preflight (ask 8)

Extend `CADPreflight` [VERIFIED: cadsandbox.go]. Current flow: workspace, `requireOutsideGrants(cadGuardReadOnly, cadMeshReadOnly, triple)`, guard entity count, mesh to `out.stl`, `judgeBinarySTL` (12 triangles, sorted extents 25.4/50.8/76.2 within 1e-3), then a negative control per stage.

Add, after the mesh checks and reusing `out.stl` from the embedded `units_inch.step` (1 x 2 x 3 in):

1. Render `out.stl` to `out.png` iso, size 256 (cheap), opaque; `judgePNG` must pass by pixels (blob rule; a 12-triangle box in iso is a hexagon), and the verdict's `view`, `size` and `background` must equal the request.
2. Render the same with `transparent`; judge alpha: background alpha 0, part alpha non-zero, `frac_transparent` between 0.05 and 0.95 (the 71 `TRANSPARENT_MIN`/`MAX`).
3. Build the measurements document from the fixture and require declared inches: `unit` `inch`, `unit_source` `declared`, sorted bbox extents 1, 2, 3 (tolerance 1e-4), `volume` 6.0, `area` 22.0, `closed_solids` 1. This is what proves the convert-back rule on every start, and it is the same file 71 proved.
4. Negative control: render a file outside the job directory and require denial through `requireCADDenied("cad render", err)` and `requireDenied(renderErr, outPNG, nil)`, as the mesh stage does. Add `cadRenderReadOnly` to `requireOutsideGrants`.
5. A judge that cannot fail proves nothing: also feed `judgePNG` a blank PNG built in memory and require rejection.

Budget: `cadPreflightTimeout = 60 * time.Second` in `cmd/cad-worker/main.go`; guard, mesh, two renders and four negative controls each start a Python process and, for render, VTK and OSMesa. Measure the preflight wall on the production image; raise the constant if it exceeds half of 60 s [ASSUMED: not measured]. The startup log line "cad-guard and cad-mesh ran inside the job directory" gains `cad-render`.

## Runtime State Inventory

Not applicable: this is a feature phase, not a rename or migration. No renamed string. Operation `render` is already allowed by `jobs_operation_check` (`CHECK (operation IN ('convert', 'extract', 'archive', 'inspect', 'render', 'sanitize'))` in `0015_operation_and_output_roles.sql:66-67`) and `role` `measurements` is already allowed (`0018_output_role_measurements.sql`). No new migration is needed [VERIFIED: grep of migrations].

## Common Pitfalls

### Pitfall 1: the pruned keep-list is silently stale
**What goes wrong:** the production `cad-render` imports one more VTK class (or uses a branch 71 never ran); the build passes and runtime fails with `No module named 'vtkmodules...'` under `nobody`.
**Why:** `prune` accepts a list missing a needed module; only a render on the pruned image notices.
**Avoid:** port `cad_render.py` verbatim; the image build smoke test imports the same modules as `Dockerfile.cad-measure`; the render test runs on the pruned image for opaque, transparent and 2048 px.
**Warning signs:** `ModuleNotFoundError` on the first transparent render only.

### Pitfall 2: a frame-fraction judge rejects a real thin part
**What goes wrong:** 71 W15: frac 0.0022 and dark 0.0005 fail the blob thresholds on a real part.
**Avoid:** the extent-first judge above; J1 to J3 plus blob or thin.

### Pitfall 3: the unit table applied to the wrong scale
**What goes wrong:** dividing OCP's mm output by 25.4 for an inch file that OCP did not scale yields numbers 25.4x too small.
**Avoid:** divide by the factor OCP applied; Wave 0 probe on `nist_ctc_05`; preflight keeps proving `units_inch.step`.

### Pitfall 4: engine timeout beyond the reconciler window
**What goes wrong:** engine 1063 s versus a 900 s stale window: the reconciler re-enqueues a running job and the unique TTL (`CADUniqueTTL`) no longer matches reality.
**Avoid:** keep `guard + mesh + render + I/O` below `RECONCILER_ACTIVE_STALE_AFTER`; the cap-bound render value above.

### Pitfall 5: render failure retried to exhaustion
**What goes wrong:** D-03 makes render failures transient; a deterministic stack timeout burns 3 attempts.
**Avoid:** keep as locked; document for phase 76; pixel-judge rejection is terminal, not transient (D-06).

### Pitfall 6: STL stats memory
**What goes wrong:** adding area to a path that already peaks at 2148 MiB on a 205 MiB tile.
**Avoid:** chunked accumulation; re-measure the tile row.

### Pitfall 7: a missing frame cost at 2048 under `lp=2`
**What goes wrong:** 121 MiB was measured at `lp=default`.
**Avoid:** Wave 0 measurement before committing the cap.

### Pitfall 8: SUS flag for `vtk` becoming a silent skip
**What goes wrong:** the registry flag is recency only, but an unreviewed `Dockerfile` edit installs a new binary wheel.
**Avoid:** checkpoint recording the 71 approval; `--require-hashes` with the copied 2 hashes.

## Code Examples

### Judge core (Go sketch; no comments per project rule)

```go
type pixelFacts struct {
	Count, DarkCount           int
	BoxW, BoxH, Margin         int
	Frac, Dark                 float64
}

func judgeFrame(f pixelFacts, n int) error {
	long := max(f.BoxW, f.BoxH)
	short := min(f.BoxW, f.BoxH)
	if f.Count == 0 || f.Count < long {
		return fmt.Errorf("%w: frame is empty", ErrCADRenderRejected)
	}
	if long*2 < n {
		return fmt.Errorf("%w: part spans %d of %d px", ErrCADRenderRejected, long, n)
	}
	if f.Margin < int(0.03*float64(n)) {
		return fmt.Errorf("%w: margin %d px", ErrCADRenderRejected, f.Margin)
	}
	blob := f.Frac >= 0.02 && f.Frac <= 0.90 && f.Dark >= 0.001
	thin := float64(short)/float64(long) < 0.15 && float64(f.DarkCount) >= 0.25*float64(long)
	if !blob && !thin {
		return fmt.Errorf("%w: neither a part-shaped nor a thin-part frame", ErrCADRenderRejected)
	}
	return nil
}
```

### Unit conversion (Go sketch)

```go
var mmPerUnit = map[string]float64{
	measurements.UnitMM: 1, measurements.UnitCM: 10, measurements.UnitM: 1000,
	measurements.UnitInch: 25.4, measurements.UnitFoot: 304.8, measurements.UnitMicrometre: 0.001,
}

func inUnit(mm, f float64, power int) float64 { return mm / math.Pow(f, float64(power)) }
```

### Atomic output write (SQL shape)

```go
func (r *Repo) AddOutputs(ctx context.Context, jobID uuid.UUID, outs []Output) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		for _, o := range outs {
			if o.Role == "" {
				return fmt.Errorf("AddOutputs: empty role for job %s ordinal %d", jobID, o.Ordinal)
			}
			if _, err := tx.Exec(ctx, upsertOutputWithRole,
				jobID, o.Ordinal, o.ObjectKey, o.Filename, o.Format, o.SizeBytes, o.ContentType, o.Role); err != nil {
				return fmt.Errorf("insert job_output ordinal %d: %w", o.Ordinal, err)
			}
		}
		return nil
	})
}
```

`upsertOutputWithRole` is the existing `AddOutputWithRole` statement [VERIFIED: repo.go:652-660]; extract it to a package constant so both paths share it.

## State of the Art

| Old approach | Current approach | When changed | Impact |
|---|---|---|---|
| 71 frac-only pixel rule | extent-first, aspect-aware judge | this phase | W15 thin part passes, empty frame still fails |
| convert exposes only STL | `render` operation plus `measurements` role | this phase | `/v1/formats` gains `operations.render.cad` |
| `process()` primary row first | rows batched in one transaction | this phase | zero visible outputs on a failed second write |

**Deprecated:** CONTEXT's assumption that render's Landlock set drops `/opt/venv` (see Landlock).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The 2 hashes in `requirements-vtk.txt` are the aarch64 and x86_64 cp311 wheels | Standard Stack | amd64 build fails `--require-hashes`; check with `pip download --platform` in the plan |
| A2 | `libosmesa6` size and closure add under the 71 apt layer upper bound; production size delta unmeasured | image size | Image larger than expected; affects 76 minReplicaCount only |
| A3 | `AddOptimal_s` on the whole shape is not materially slower than `Add_s` on large assemblies | Measurements | Mesh timeout 316 s may be too low; Wave 0 re-measure |
| A4 | When OCP `FileUnits` is empty it applies no scaling (factor 1) | Units | Wrong inch numbers for `CBU:inch` lowercase files; Wave 0 probe on `nist_ctc_05` |
| A5 | The 121 MiB 2048 px frame cost holds at `LP_NUM_THREADS=2` | Size ceiling | Cap 3,806,000 too high; re-measure |
| A6 | Mesh Landlock set unchanged after measurements code; only the re-run confirms | Landlock | Missing path breaks the mesh stage in production; preflight catches it at start |
| A7 | `quality` on an STL source is `opts_not_applicable` | Options | A client sending quality with STL gets 422 where it expected a no-op |
| A8 | Preflight wall fits in 60 s with two renders and four denials | Preflight | Worker refuses to start; raise the constant |
| A9 | `thin` thresholds (aspect 0.15, dark count 0.25 L) hold at 256 px for thin parts | Pixel judge | False rejects at small sizes; calibrate live |
| A10 | Worker tests pinning the primary-then-side order will need updates | Atomicity | Planner finds them by running `go test ./internal/worker/...` |
| A11 | Zero-volume watertight STL is best mapped to `stl_not_watertight` | STL stats | Doc semantics shift; needs user confirmation |

## Open Questions

1. **Render timeout versus the reconciler window**
   - Known: 73 method gives 657 s and an engine of 1063 s; the 900 s cap allows 480 s render with a 886 s engine.
   - Unclear: whether to raise `RECONCILER_ACTIVE_STALE_AFTER` (fleet-wide) or accept 480 s with a thin 1.10x margin on one stacked row.
   - Recommendation: take 480 s (user decision); document the unbounded stack class for phase 76.
2. **Triangle cap at 2048:** 3,806,000 (formula-honest, deviates from CONTEXT's 4,014,975) versus lowering the size ceiling. Recommendation: lower the cap and record it.
3. **`assume_unit` is missing from D-04's option list** but required by MEAS-04; confirm it joins the closed render options.
4. **Units for an unrecognised text unit** (outside the vocabulary): `unspecified`/`none` plus a recorded gap versus a terminal failure. Recommendation: `unspecified`/`none` and document.
5. **Zero-volume watertight STL reason** (A11).
6. **Render rejection code:** reuse `cad_empty_result` with a new message via a new sentinel (`ErrCADRenderRejected`, terminal) versus a new code (needs `docs/ERROR-CODES.md` and `errcode` doc-sync). Recommendation: reuse the code, new sentinel and message.
7. **Real-flow coverage:** `PRIVATE_CORPUS files=0`; the judge and ceilings are tuned on the public corpus only.

## Environment Availability

Docker was not run, per the brief; no probes beyond reading files. The plan's live tasks require:

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Docker / OrbStack | image build, Landlock measurement, liveproof | not probed here | — | none; blocking for live gates |
| Linux kernel with Landlock | sandbox measurement | OrbStack kernel 7.0.14 per 73 evidence | 7.0.14-orbstack | none |
| Postgres (`DATABASE_URL`) | `AddOutputs` inversion test | not probed here | 18 per CLAUDE.md | tests skip without it; must run in the gate |
| Corpus caches | W15 prefix, large rows | `OCTOCONV_CAD_CACHE` | — | `scripts/cad-corpus-fetch.sh` |

**Missing dependencies with no fallback:** a Postgres for the DB-gated inversion test, to be provided at execution.

## Validation Architecture

Skipped: `workflow.nyquist_validation` is `false` in `.planning/config.json` [VERIFIED: file read this session]. Test impact is listed instead, because the brief asks what flips (ask 9).

### Tests and goldens that flip (ask 9)

Verified by reading the cited lines this session unless marked:

| Test or artifact | Change |
|---|---|
| `internal/convert/operations_test.go:176` | `want := []string{OperationConvert, OperationSanitize}` becomes convert, render, sanitize |
| `internal/convert/convert_test.go` `TestDefaultRegistersOnlyAllowlistedCADPairs` (`allowed := map[string]bool{"step>stl": true, "iges>stl": true}`; condition `op != OperationConvert || class != EngineCAD`) | allow `step>stl`, `iges>stl` under convert and `step>png`, `iges>png`, `stl>png` under render |
| `internal/api/formats_handlers_test.go` ~296-330 (same allowlist, `want only step -> stl and iges -> stl under cad`) | same allowlist; `stp`/`igs` come through `NormalizeFormat`, so pairs register as `step`/`iges`; the wire shows `step>png`, `iges>png`, `stl>png` |
| `internal/api/pre58_pinning_test.go` ~213-215 (`if key != "sanitize"` for new operation keys) | permit `render`, and assert its presence like the engine strips |
| `internal/api/golden_pinning_test.go`, `formats_handlers_test.go` engine strips (`withoutEngine(... EngineCAD)`) | unchanged; the `render` operation is a new top-level key under `operations` |
| `internal/api/cad_ceiling_test.go` `maxFormatBytes[EngineCAD]` length 2 and `TestCADInspectReportsPerFormatCeilingAsMaxBytes` (every CAD offer must be `stl`/`convert`) | length 3 (add `stl`); inspect gains a `render`/`png` offer for step, iges, stl with the same per-format `MaxBytes` |
| inspect offer spec sync (`inspect_offer_spec_sync_test.go`) | [ASSUMED] check offers for the new operation |
| `internal/api/handlers_test.go:4070` engine fixture table (`{convert.EngineCAD, fixture{"in.step", "stl", ...}}`) | add a render/png row |
| `internal/api/estimate.go:135` (`EngineCAD: EstimateSourceUnmeasured`), `api.go:457` timeout map, `estimate_test.go:368` env mapping | value changes with `CADDefaultEngineTimeout` (406 to 886 s); source stays `unmeasured` |
| `internal/worker/probe_sideoutput_test.go` `TestOnlyProbeDeclaresSideOutputs` | CAD render also declares extra outputs |
| `internal/worker/pinning_test.go` CAD verdicts (`TestPinnedCADFailureVerdicts`) and `cad_test.go` "takes no options" cases | new sentinels, `CADOptsFromMap` now accepts the union |
| `internal/convert/cad_test.go`, `cadsandbox_test.go`, `cadguard_test.go` | `ConfinedCAD` and `ValidateCADTimeouts` gain a render argument; render set and preflight tests |
| `internal/measurements/consistency_test.go` STL rows | STL rows now carry area; keep the rule that rc 0 helper rows validate |
| `docs/MEASUREMENTS.md` "No engine emits this document yet..." paragraph | replace; add the sentence that numbers are expressed in `unit`; `doc_sync_test.go` parses tables, not this paragraph [ASSUMED]; run it |
| `docs/ERROR-CODES.md` | only if a new code is added (Open Question 6) |
| `docker-compose.yml` (4 `CAD_ENGINE_TIMEOUT` sites), `deploy/chart/octoconv/values.yaml:224`, `.env.example`, `scripts/cad-liveproof.sh`, `scripts/ci-helm-checks.sh`, `scripts/conf05-crosscheck.sh` | timeout and grace values, FORMATS row (`iges>stl;step>stl` gains the render pairs), `cadrender` MATCH |
| `internal/queue/queue_test.go` (unique TTL from engine timeout) | [ASSUMED] values pinned from 406 s |

No `*.golden.json` file is regenerated; the pre-57/pre-58 pins are compared after stripping the permitted additions, so the `render` key is added to the permitted list instead.

## Security Domain

`security_enforcement` is absent in config (treated as enabled).

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no change | existing API-key auth on `/v1/jobs` |
| V3 Session Management | no | — |
| V4 Access Control | yes | existing `client_id` scoping; measurements output role selected by role, never position |
| V5 Input Validation | yes | `ParseCADOpts` closed enums and range; `measurements.Validate` on the output; strict JSON |
| V6 Cryptography | no | no new crypto |
| V12 Files and Resources | yes | Landlock per stage, `LP_NUM_THREADS=2`, process-group kill, byte and triangle ceilings |

### Known Threat Patterns

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Hostile STL/STEP exploiting VTK or OCP readers | Elevation | per-stage Landlock, no network, `nobody`, 4 GiB cgroup |
| Render DoS via coincident copies (61.6 s vs 1.2 s) | DoS | stage timeout now; guard in phase 76 |
| PNG bomb or oversized frame | DoS | helper enforces size 256..2048; judge decodes after a size check from the header |
| Output forgery through a stage stdout | Spoofing | fd redirect so only the helper writes the `CADVERDICT` line; last-line parse |
| Extra supply-chain surface | Tampering | hash-locked wheel, no new package beyond `vtk`, checkpoint |
| Client reads a half-written result | Information | single-transaction `AddOutputs` |

## Sources

### Primary (HIGH confidence, read this session)

- `scripts/cad-measure/cad_render.py`, `pixelcheck.py`, `cad_mesh.py`, `vtk_keeplist.py`, `requirements-vtk.txt`, `Dockerfile.cad-measure`
- `cmd/cad-worker/main.go`, `helper/cad-mesh`, `helper/cad-guard`, `Dockerfile.cad-worker`
- `internal/convert/cad.go`, `cadsandbox.go`, `convert.go`, `opts.go`, `avopts.go`, `sanitizeconverter.go`
- `internal/measurements/measurements.go`, `docs/MEASUREMENTS.md`, `testdata/helpers-71-04-rows.txt`
- `internal/jobs/repo.go` (`MarkDone`, `AddOutput`, `AddOutputWithRole`, `Outputs`), `internal/worker/worker.go`, `runner.go`, `descriptors.go`
- `internal/api/handlers.go`, `formats_handlers.go`, test files listed in ask 9
- `.planning/phases/71-evaluation-gate/71-DECISION.md`, `evidence/71-04-render.txt`, `71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt`, `71-07-landlock.txt`, `71-08-image-size.txt`, `71-06-render-stacking.txt`
- `.planning/phases/73-cad-class-convert-slice-confined/73-VERIFICATION.md`, `evidence/73-01-spikes.txt`
- `.planning/research/SANDBOX-PATHS.md` `## cad`, `.env.example`

### Secondary (MEDIUM)

- `gsd-tools query package-legitimacy check --ecosystem pypi vtk numpy` (registry signals)

### Tertiary (LOW)

- none; every unverified item is in the Assumptions Log.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH. All versions come from the approved locks and the existing image.
- Architecture: HIGH for seams (code read); MEDIUM for the batched-output change, which touches the shared `process()` path.
- Pitfalls and numbers: MEDIUM. Frame cost, unit scale for lowercase `inch`, and image size delta are unmeasured.

**Research date:** 2026-10-05
**Valid until:** 2026-11-04 (stable code; the 71 evidence is fixed to one image build)
