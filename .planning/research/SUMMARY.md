# Project Research Summary

**Project:** OctoConv — milestone v1.16 CAD Engine (tenth engine class `cad`)
**Domain:** Confined headless CAD preview + measurement + conversion inside an existing async Go conversion platform
**Researched:** 2026-10-02
**Confidence:** MEDIUM-HIGH (stack measured hands-on; integration points read from code; real-file behaviour, memory and timing still unmeasured)

## Executive Summary

v1.16 adds a tenth engine class, `cad`, that does five things for step/stp/iges/stl/dxf: `render` to PNG (closed view vocabulary, default 1280x1280), a second JSON output with bbox/volume/units, STEP/IGES to STL, and DXF to svg/pdf (plus a DXF `render` to PNG). It sits on the existing engine template (queue, `Runner`/descriptor, `engineExec` with a measured Landlock ruleset, preflight, KEDA). Two-output jobs are not new (sanitize/probe already use `SideOutputs` and `AddOutputWithRole`); what is new is a Python-script-driven engine, a role value that is not `report`, and the first class serving two operations (`convert` and `render`).

**The central reconciliation: FreeCAD is out.** STACK.md measured that the `cadquery-ocp-novtk` wheel (same OCCT kernel) gives identical volume and byte-identical STL at ~250 MB versus 1.6-1.9 GiB for FreeCAD, and that a VTK 9.7.1 wheel plus Debian `libosmesa6` renders headless with no X, no `/dev`, no `/tmp`, under `nobody` and `env -i`. ARCHITECTURE.md and PITFALLS.md were written assuming FreeCAD and possibly Xvfb; those parts (FreeCADCmd argv/exit-status/console behaviour, Xvfb socket and stale locks, shared-`/tmp` guard with concurrency forced to 1, `sock:` grant, tini-for-Xvfb, FreeCAD apt image weight, Qt/PySide import surface) are superseded. Everything else stands and is the real risk list: fail-closed sniffers with a non-UTF-8 envelope, hostile-parser bombs, honest units, volume only from solids, exit code and stdout lie (judge by content), `WaitDelay`/helper-kill, env allowlist, determinism, SVG as untrusted output, and measured (not copied) timeout/caps.

Recommended approach: `debian:bookworm-slim` image with pinned pip wheels (OCP, VTK pruned, ezdxf, trimesh, pillow) plus a few bookworm apt packages (`libosmesa6`, `libgl1`, `librsvg2-bin`, `fonts-dejavu-core`), three embedded Python helpers run through `engineExec`, each stage with its own Landlock ruleset (mesh/OCP, render/VTK, dxf/ezdxf, rsvg), no new Go dependencies. Main risks: image size (~1.1 GB pruned, largest in the fleet, cold-start dominated), unmeasured STEP memory (one synthetic sample: ~18x file size, 1.2 GB RSS), llvmpipe thread count and non-bit-reproducible rendering, and parser divergence in the new sniffers. Mitigate with an evaluation phase first, engine-free contract foundations before any engine is registered, and measured bounds in the last phase.

## Key Findings

### Recommended Stack

Details: `STACK.md`. Measured on arm64 native and amd64 emulated; Landlock traces from a trixie prototype are hypotheses for the real bookworm image.

**Core technologies:**
- `cadquery-ocp-novtk` 7.9.3.1.1 (8.0.1.0.0 as eval candidate): STEP/IGES read, exact volume (`BRepGProp`), tight bbox (`BRepBndLib.AddOptimal`), `BRepMesh_IncrementalMesh`, STL write. Same kernel as FreeCAD. Use `CornerMin()/CornerMax()` so the helper works on both versions. Plain `cadquery-ocp` is wrong (bundles VTK).
- `vtk` 9.7.1 wheel (`--no-deps`) + `libosmesa6`: offscreen render via `vtkOSOpenGLRenderWindow`, 0.8-1.7 s. Prune unused `.so` (566 to 290 MB, identical PNG). `libgl1`/`libx11-6` still required because `import OCP` fails without `libGL.so.1`.
- `ezdxf` 1.4.4 (+ `pillow`, numpy, fonttools) with native SVG backend, then `rsvg-convert` (`librsvg2-bin`) for pdf and png (0.4 s + 0.16 s). Avoid PyMuPDF (AGPL), LibreCAD, Inkscape. `fonts-dejavu-core` is mandatory or DXF text is dropped.
- `trimesh` 5.1.0 (or numpy signed-tetra) for STL-source stats only; tessellated volume is approximate, so STEP/IGES volume always comes from the B-rep.
- Python 3.11.2 from bookworm, venv built in a builder stage, run as `python -I -B`, no pip in the final image; wheels pinned by hash.
- Not installed on purpose: FreeCAD, f3d, Xvfb, LibreCAD, Inkscape, occt-draw, python3-vtk9, matplotlib, PyMuPDF. Go side: zero new dependencies, CGO stays off.
- Render candidates (eval phase): 1) VTK+OSMesa (default), 2) f3d 3.5.0 amd64-only (yardstick only, no arm64 build, needs unversioned `libOSMesa.so` symlink), 3) own numpy/Go rasteriser (only if 1 fails size/determinism), rejected: Debian f3d+Xvfb, FreeCAD GUI offscreen.
- Keep OCP and VTK in separate processes (mesh helper, then render helper), each with its own ruleset.

### Expected Features

Details: `FEATURES.md`.

**Must have (table stakes):**
- `render` step/stp/iges/stl to PNG: closed 7-view enum (`iso` default, front/back/left/right/top/bottom), Z-up contract, per-view fit-to-view with margin, closed `size` (default 1280x1280, hard ceiling), opaque light background, neutral shading, tessellation relative to bbox diagonal (server constant).
- Dimensions JSON as second output of render, new role: tight bbox, `solid_count`/triangle counts, volume only from closed solids else `null` with a closed reason code, honest `units` block with provenance, schema version, closed reason vocabulary doc-synced.
- STEP/IGES to STL (one merged binary STL, mm, documented); `stp`/`igs` aliases.
- DXF to svg and pdf (model space, visible layers, content-fit page, `$INSUNITS` provenance, extents from entities not `$EXTMIN/$EXTMAX`).
- Fail-closed sniffers for all five formats with distinct codes (binary DXF, DWG, truncated STEP/IGES/DXF, ambiguous STL), per-format input caps, stable error codes, explicit DWG rejection.
- Worker class infra: queue, Dockerfile, compose, chart, KEDA, measured Landlock ruleset, preflight, measured timeout/caps, live gate; SEED-007 preview contract fixed on CAD.
- **Recommendation carried from FEATURES:** support DXF `render` to PNG (fixed top-down, `view` rejected for DXF) through the 2D path (ezdxf SVG then `rsvg-convert` PNG). Without it a DXF client cannot obtain its 2D bbox at all, because measurements are the second output of a `render` job. Flag for requirements.

**Should have (post-validation, v1.x):** `assume_unit` (fills gaps only, never overrides declared), surface area, `background: transparent`, `quality` enum (draft/standard/fine, maps to server constants), STL mesh-health findings, DXF `layout`/`page` opts.

**Defer (v2+):** per-body breakdown, feature-edge outlines, IGES sewing to solid, DWG (ODA licence decision), CAD codes in the inspect vocabulary, SEED-007 on other classes.

**Anti-features:** size text overlay, client camera vectors, projection-angle/multi-view sheets, the consumer's 500 B floor and silent skips, `volume: 0` or invented `mm`, trusting header extents, ACIS/PMI/colours.

### Architecture Approach

Details: `ARCHITECTURE.md` (its FreeCAD/Xvfb sections superseded as above; integration inventory in section 6 and pin list in section 7 remain the authoritative checklist). `cad` plugs into the existing `Runner`/`EngineDescriptor` skeleton; one asynq task type (recommend neutral `cad:job`) serves both operations because `process()` dispatches via `LookupOp`.

**Major components:**
1. `CADConvertConverter` + `CADRenderConverter`: two converter types (registry is `(operation, from, to)`, `Operation()` is single-valued); `cmd/cad-worker` must `Replace` both configured converters.
2. Embedded Python helpers (`go:embed`, written 0o400 into the job dir, params in `params.json`, result in `result.json`, judged by content, `allow_nan=False`, validated and re-encoded in Go before upload).
3. `cadopts.go` (closed `view`/`size`, union-schema re-parse in `OptsFromMap`), `cadsniff.go` (own envelope: no NUL for text formats, high bytes tolerated, binary formats bypass), `cadmeasure.go` (schema/validator), `cadsandbox.go` (per-stage measured grants, preflight).
4. New output role **`measurements`** (not `dimensions`: `convert/dimensions.go` already means pixel dimensions; not `report`: bound to `SanitizeReport`). Migration `0018` widens `job_outputs_role_check`; PNG stays `primary`; `cadSideOutputs` returns nil for `convert`; the write goes through `AddOutputWithRole`, never `AddOutput` (PR #12 defect).
5. Detection lockstep: `handleCreateJob` and `detectInspectFormat` chains, `RejectBindings`, `NormalizeFormat` aliases (+ frontend copy), `MIMEType`, API opts `case convert.EngineCAD:` (default branch silently rejects render opts).
6. Atomic engine-class wiring: `EngineCAD` in `AllEngines()` only together with queue, enqueue route, reconciler, timeouts/estimate/size maps, IN-02 compose env, chart, README (contract tests fail by name otherwise).
7. Retry classification stage-aware: sandbox-unavailable transient first; unreadable/empty/over-ceiling/stage-1 timeout/bad `result.json` terminal; render stage failure transient.
- Because no renderer needs `/tmp` or a socket, the class avoids the shared-`/tmp` guard, sweep and `sock:` grant. Start `CAD_WORKER_CONCURRENCY=1` anyway until memory is measured (STEP ~18x RSS), scale by KEDA.

### Critical Pitfalls

Top items still valid after the FreeCAD/Xvfb supersession (full list in `PITFALLS.md`):

1. **Sniffer envelope and parser divergence** (PITFALLS 1-6) — do not reuse `ValidateSniffPrefix` (rejects cp1251/Latin-1 STEP, IGES, legacy DXF); STL: size equation `84+50n` first, ambiguity fail-closed, differential test against the real reader (OCP) with a divergence table; STEP head+tail tokens, IGES section walk + Terminate counts, DXF pair parsing with `999`, binary sentinel and `AC10xx` DWG getting distinct codes; one detection order through both endpoints.
2. **Hostile-parser bombs, memory and unmeasured bounds** (7, 13, 27) — byte ceiling is a weak proxy (self-referencing STEP, NAUO instancing, MINSERT, huge STL `n`). Worker guard stage plus post-import finite-bbox check, triangle cap before write, deflection relative to bbox diagonal; measure RSS-per-MiB on a real corpus under the pod limit, pair `CAD_ENGINE_TIMEOUT` with the input ceiling (max over both operations), concurrency 1. Pandoc OOM lesson applies to OCCT.
3. **Exit code and stdout lie** (11, 12) — OCCT prints to stdout; judge by `result.json` and output content (PNG pixel stats, STL arithmetic, JSON strict parse); no `WaitDelay` in shared `exec.go`, so supervise any helper inside the wrapper, redirect helper output to files, prove kill-mid-job leaves zero survivors and prompt `Wait`. Do not change `exec.go`. Retry polarity per failure mode, new error strings and terminal signatures in the same commit.
4. **Honest measurements** (14, 15, 16, 22) — volume only from `Solids`, `abs` after orientation check, `null` + reason otherwise, never sew silently; units closed vocabulary with provenance (OCCT converts STEP/IGES to mm so report declared unit; STL `unspecified`; DXF `$INSUNITS` 0 is unspecified); bbox from one defined tight source; `schema: 1`; PNG+JSON atomicity inversion-tested; role in enum, CHECK and `handleGetJob`.
5. **Render correctness and determinism** (17, 18) — normalise mesh not camera, Z-up fixed, non-collinear view-up, degenerate bbox handled, judge by pixel stats/three-view difference, never hash-compare PNGs (llvmpipe differs across arch); set `LP_NUM_THREADS` and any mesher thread cap inside the helper (engine env is an allowlist: `PATH/HOME/LANG/LC_ALL/TMPDIR`; extend per class, do not widen `engineEnv`); `LC_ALL=C.UTF-8`, `python -I -B`, sorted iteration.
6. **Grant-set measurement and image budget** (10, 20, 21) — trace as the image user across the full (format x operation) matrix, add `execve` targets by hand (`/usr/bin/python3.11`, venv symlink targets), prove sufficiency/necessity/effectiveness by content; no `/usr/lib` blanket grants; fonts grant for ezdxf; HOME into a job-dir subpath; native image on both arches (Landlock unavailable under emulation); ~1.1 GB image means cold-pull dominates scale-from-zero, set `keda.cad.minReplicaCount` deliberately.
7. **DXF to svg/pdf output is untrusted** (19) — SVG is derived from attacker DXF text: well-formed XML, no script/foreignObject/event attrs/external hrefs, round-trip test `&<>"'`; never resolve XREF/IMAGE; keep default libxml2 limits in `rsvg-convert`.

## Implications for Roadmap

Phase numbering continues from 71. Six phases, following the architecture's dependency analysis but narrowed by the measured stack.

### Phase 71: Evaluation gate (render quality, corpus, budgets)
**Rationale:** Everything downstream (rulesets, timeout, chart resources, view/quality constants) depends on a few facts the prototype did not settle. Stack is mostly decided; this phase confirms it on the real bookworm image and fixes the open numbers. No production wiring.
**Delivers:** decision record (VTK+OSMesa vs fallbacks), `Dockerfile.cad-measure`, a real CAD corpus (STEP assemblies, IGES with trimmed surfaces, large STL, DXF with hatches/blocks/dimensions; cp1251 and quirk files; public-domain models with licences recorded), candidate path sets from `sandbox-paths-measure.sh` trace for all four helpers, VTK prune keep-list, framing/quality settings (bbox-fit, FXAA, feature edges, light kit) judged visually on 3 real parts, `LP_NUM_THREADS`/OCP thread behaviour at `--cpus=2`, STEP peak RSS and time on real files, measured image size and cold-pull, closed vocabularies (units, reason codes, views), `Bnd_Box` 7.9 vs 8.0 decision, licence one-liner for OCP-bundled libs.
**Avoids:** pitfalls 9, 10, 13, 18, 20, 21, 29.

### Phase 72: Contract foundations (engine-free)
**Rationale:** Riskiest review surface (fail-closed validation, parser divergence) is pure Go/SQL and testable with no engine; nothing registered, `AllEngines()` unchanged, so nothing is advertised.
**Delivers:** migration `0018` + `OutputRoleMeasurements`, `OperationRender`, `stp`/`igs` aliases (+ frontend), `MIMEType`, `cadopts.go`, `cadsniff.go` + both detector chains + `RejectBindings`, `cadmeasure.go` (schema, validator, canonical encoder, negative/fuzz tests), distinct 422 codes (incl. dwg, binary DXF, ambiguous STL), doc/openapi/mcp text for role and formats. Differential test of sniffer verdict vs OCP/ezdxf readers.
**Avoids:** pitfalls 1-6, 14-16 (contract side), 22 (enum/role).

### Phase 73: CAD class and `convert` slice, confined
**Rationale:** Smallest vertical slice exercising every integration point and the confinement; the worker must refuse to start unconfined.
**Delivers:** atomic engine-class change (`EngineCAD`, `AllEngines()`, queue `cad:job`, enqueue route, reconciler, timeouts/estimate/size maps, IN-02 env, README), `cmd/cad-worker`, `Dockerfile.cad-worker` (pruned VTK, hashed wheels, py_compile at build), `CADConvertConverter` STEP/IGES to STL (mesh helper only), measured stage-1 ruleset, `CADPreflight` (content-judged, outside-file refusal), `isCADTerminal`/failure codes, guard stage (entity/occurrence/triangle caps), coverage tests, conf05/liveproof extension. Estimate source `unmeasured`.
**Avoids:** pitfalls 7, 10, 11, 12, 23, 28, 33.

### Phase 74: `render` (3D) + measurements output
**Rationale:** Isolates two-output behaviour and renderer risk in one reviewable phase.
**Delivers:** `CADRenderConverter`, render helper with its own ruleset (no OCP), view/size opts end to end, mesh helper writing `part.stl` and `measure.json` from one STEP read, STL-source path via trimesh/numpy, `cadSideOutputs` + role via `AddOutputWithRole` (test through the repository write), PNG/JSON atomicity inversion test, render preflight (non-blank pixel stats, three views differ), update the three `render` pins and `TestOnlyProbeDeclaresSideOutputs` (allow-set `{probe, cad}`), sweep `grep '"render"'` across tests.
**Avoids:** pitfalls 14-18, 22, 32.

### Phase 75: DXF (2D) pipeline
**Rationale:** Different toolchain and risk profile (ezdxf + rsvg), kept apart from 3D.
**Delivers:** `dxf->svg`, `dxf->pdf`, `(render, dxf, png)` with 2D bbox and `$INSUNITS` provenance, font cache pre-seeded or HOME in job dir, SVG well-formedness/sanitisation guarantee, DXF bombs (MINSERT, cyclic blocks), rulesets for `cad_dxf.py` and `rsvg-convert`, non-square page letterbox decision.
**Avoids:** pitfalls 5, 7, 19 and the SVG/external-reference security items.

### Phase 76: Measured timeout, chart, KEDA, live gate
**Rationale:** Timeout is only meaningful once both operations exist.
**Delivers:** `cad-timing-measure.sh` over both operations on the real corpus at the input ceiling, `CAD_ENGINE_TIMEOUT` (max over operations) paired with the size ceiling, estimate flipped to `measured`, deployment/ScaledObject/values/ConfigMap/grace, `ci-helm-checks` count 9 to 10, KEDA scale-from-zero and downscale-survival on OrbStack, `keda.cad.minReplicaCount` decided, all-classes live gate extended, kill-mid-render zero-survivor proof, two-run byte-identity and cross-arch tolerance table, `docs/CAD.md`, `docs/ERROR-CODES.md`.
**Avoids:** pitfalls 13, 18, 20, 27.

### Phase Ordering Rationale

- 71 first: the `AllEngines()` contract tests forbid "constant now, wire later", and the numbers (RSS, image size, grants) shape everything after.
- 72 before 73: sniffers and role migration are engine-independent and the riskiest to review; inspect already recognises CAD but offers nothing, which is the correct interim state.
- 73 before 74/75: any registered triple requires the confined worker and all contract entries.
- 74/75 split by toolchain (OCP+VTK vs ezdxf+rsvg); 75 may start in parallel with 74 once 73 lands.
- 76 last: timeout and chart resources need both operations.

### Research Flags

Needs `/gsd:plan-phase --research-phase`:
- **Phase 71:** entirely research (corpus sourcing, VTK framing/quality, llvmpipe and OCP threads at cgroup quota, real RSS, prune keep-list).
- **Phase 72:** needs a real-exporter corpus for sniffers (Latin-1/cp1251, binary STL with `solid` header, newline-less IGES, DXF with `999`); otherwise standard.
- **Phase 73:** measured Landlock set is the long pole (python+C++ stack, execve invisibility, several trace/verify iterations).
- **Phase 75:** SVG sanitisation and DXF entity coverage (hatch, dimension, SHX text, paper space, ACIS ignored), units semantics.

Standard patterns (skip research-phase):
- **Phase 74:** unless 71 changes the renderer, follows sanitize/probe two-output and document two-ruleset precedents.
- **Phase 76:** standard KEDA/chart template; resource limits measured, not copied.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH (measured), MEDIUM on versions | OCP vs FreeCAD identical results, VTK+OSMesa under `nobody` no X, ezdxf/rsvg all run; OCP 8.0.1 is <1 month old; amd64 timings emulated; traces from a trixie prototype, not the bookworm image |
| Features | MEDIUM | Format semantics verified against docs; tight-bbox, memory/time on 40 MB STEP, mesh-health APIs not verified; triangle caps and timing LOW until measured |
| Architecture | HIGH on integration points, MEDIUM on engine details | Every `file:line` read at `0fc352f`; FreeCAD/Xvfb sections superseded, remainder (roles, registry, pins, wiring checklist) stands |
| Pitfalls | MEDIUM | Repo-specific items HIGH; FreeCAD-specific items obsolete; format and OCCT behaviours (IGES shells, orientation, multi-context STEP units) MEDIUM/LOW |

**Overall confidence:** MEDIUM-HIGH.

### Gaps to Address

- **No real CAD corpus:** prototype used synthetic geometry; Phase 71 must source one; timeout, caps and RSS multiplier (18x, single sample) derive from it.
- **IGES/assembly semantics:** shells instead of solids, negative orientation, overlapping bodies, multi-unit-context STEP; helper must count `TopAbs_SOLID`, emit `volume: null` otherwise; untested beyond one round-trip.
- **STEP source-unit echo:** OCCT converts to mm; declared unit via header parse or `STEPControl_Reader::FileUnits` not measured.
- **Render quality and determinism:** framing cropped with `ResetCamera()`; need bbox-fit, AA, edges; llvmpipe output differs per arch, same-arch determinism not asserted; `LP_NUM_THREADS` vs cgroup.
- **VTK pruning stability** across bumps (keep-list regenerated per bump behind a build script and a render test); fallback is accepting 566 MB.
- **Image size and cold pull:** ~1.1 GB estimated, registry cold-pull never measured (AVX-02 backlog); decide `minReplicaCount`.
- **DXF:** paper-space layouts, 3DSOLID/ACIS and proxy entities, SHX fonts, non-square page vs 1280x1280 (product decision: letterbox on white); DXF `render` PNG recommendation needs requirements sign-off.
- **Licences:** OCP wheel bundles `libfreeimage` (GPL/FIPL), `libraw`, `libjasper`; internal-only, needs a one-line legal OK; DWG stays out.
- **Naming decisions to fix once:** task type `cad:job`, role `measurements`, OCP 7.9.3 vs 8.0.1 pin, `quality` enum in v1.16 or v1.x.
- **Landlock grants:** all path sets are hypotheses until measured on the real bookworm image on native arch (emulated amd64 has no Landlock).

## Sources

### Primary (HIGH confidence)
- Hands-on prototypes (Docker, arm64 native, amd64 Rosetta): `cadquery-ocp-novtk` 7.9.3.1.1 and 8.0.1.0.0 vs FreeCAD 1.0.0 on the same STEP, VTK 9.7.1 OSMesa, ezdxf 1.4.4 SVG, `rsvg-convert` 2.54.7, strace traces (STACK.md)
- Repo code at `0fc352f`: `internal/worker/{runner,descriptors,worker}.go`, `internal/jobs/*`, `internal/convert/*`, `internal/api/*`, `internal/queue/*`, `internal/reconciler`, migrations 0012/0015/0016, `cmd/pandoc-worker`, `.planning/research/SANDBOX-PATHS.md` (ARCHITECTURE.md, PITFALLS.md)
- ezdxf docs (units, drawing add-on) (FEATURES.md)
- Project notes `architecture/octoconv-cad-preview.md`, `gsh-cad-preview-integration.md`

### Secondary (MEDIUM confidence)
- PyPI/GitHub/conda registry metadata for OCP, vtk, ezdxf, trimesh, f3d, FreeCAD (versions, wheel tags, sizes)
- OCCT STEP/IGES unit handling, STL binary/ASCII `84+50N` rule, FreeCAD standard views, IGES unit flag, Landlock ABI 9 `RESOLVE_UNIX`

### Tertiary (LOW confidence, needs validation)
- OCCT docs pages summarised only (`xstep.cascade.unit`, mesh parameters; the "degrees" claim for angular deflection is wrong, API takes radians)
- VTK single-wheel EGL/OSMesa fallback (confirmed by measurement)
- Compressed image size (~40-45% guess), tight-bbox and mesh-health API availability, competitor behaviour rows

---
*Research completed: 2026-10-02*
*Ready for roadmap: yes*
