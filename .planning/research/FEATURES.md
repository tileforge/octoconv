# Feature Research

**Domain:** CAD preview + measurement service (headless render of STEP/IGES/STL/DXF to PNG, dimensions report, STEP/IGES to STL, DXF to SVG/PDF) inside an existing async conversion platform
**Researched:** 2026-10-02
**Milestone:** v1.16 CAD Engine (tenth engine class `cad`)
**Confidence:** MEDIUM. Format semantics (STL layout, DXF `$INSUNITS`, OCCT unit handling, f3d options, FreeCAD view axes) are verified against official docs. Kernel behaviour on open shells, bbox tightness, memory/time cost on 40 MB STEP, and ezdxf licensing are MEDIUM/LOW and flagged inline as measurement or decision gates.

## Context: what already exists (do not re-derive)

Facts from reading the repo and the two Obsidian notes that change what "table stakes" costs here:

- **`job_outputs.role` is a DB-level closed set** `('primary','report','preview','thumbnail')` (migration `0015`). `handleGetJob` already returns an `outputs[]` array with `role` per entry and selects `download_url` by role `primary` only. `Repo.MarkDone` counts exactly one `primary` row (D-13 guard). `AddOutputWithRole` is the only writer of a non-default role. So: the PNG is the `primary` output, the dimensions JSON needs ONE new role value (CHECK widening migration + Go constant next to `OutputRolePrimary`/`OutputRoleReport`). `report` is already claimed by the sanitize report schema and means "describes the job"; reusing it would force consumers to switch on `operation` to parse it, so a dedicated role (suggested `dimensions`) is cleaner.
- **`operation` enum already contains `render`** (`0015` CHECK); `engine` CHECK already reserves `cad`. No enum work needed for those two.
- **Closed validated opts precedent**: `AVOpts` (`internal/convert/avopts.go`) - strict-parse typed struct, every field validated against a closed allowlist, client bytes never reach argv, applicability gating per target (`avThumbnailTargets`). `view`/`size`/`background` follow exactly this shape.
- **Registry is `(operation, from, to)` triples**, `Register` panics on collision; `POST /v1/inspect` lists available triples per detected format, so new CAD triples appear in inspect for free once registered. New CAD formats need fail-closed content sniffers (precedent: `SniffAudio`, `SniffVideo`, `markupsniff`).
- **Measured-not-guessed timeouts and caps** (OCR/pandoc/audio precedent): `CAD_ENGINE_TIMEOUT`, input cap and any triangle cap must come from a timing/memory gate with a Path-B lever, not be copied from the consumer's 60 s / 40 MB.
- **Consumer being replaced (PreviewGenerator)**: Fusion 360 GUI, 1280x1280 PNG, `isFitView = True`, view orientation parameter, "Size: X x Y x Z mm" overlay (OUT of scope), `part_volume`; skips <500 B, >40 MB, DXF/DWG >5 MB; up to 60 s/file. At the consumer (GSH) the PNG is turned into `thumbnail`/`preview_small`/`preview_large` derivatives by ImageMagick, which matters for the background decision below.

## Feature Landscape

### Table Stakes (Users Expect These)

#### A. Render (`render` to PNG)

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Closed view vocabulary: `iso` (default), `front`, `back`, `top`, `bottom`, `left`, `right` | Every CAD viewer (FreeCAD, Fusion, SolidWorks, Onshape) exposes exactly these 6 orthographic faces + 1 axonometric; PreviewGenerator already has a view-orientation parameter | LOW | Closed enum, strict-parse, no raw camera vectors from the client. See "View conventions" below for the axis contract. HIGH confidence on vocabulary, MEDIUM on exact iso vector. |
| Fixed axis convention: right-handed, Z-up, view names relative to the FILE's own axes | CAD and STEP/IGES/DXF are Z-up; f3d defaults to `--up +Y`, so leaving the renderer default would turn every part on its side | LOW | Must pass explicit up (`--camera-view-up` / `--up +Z`). STL carries no orientation metadata: a Y-up STL from a game/three.js exporter will render "lying down"; do NOT auto-detect up, document it. |
| Fit-to-view with margin, centered on the bbox centre | Fusion `isFitView=True` is the baseline consumers saw; unfitted renders crop or shrink the part | LOW-MED | Fit by projected bbox (all 8 corners) per view, not one global radius, else `top` of a flat plate is tiny. Fixed margin (~5-10%) as server constant. f3d auto-fit exists but `--camera-position` OVERRIDES direction and zoom (man page), so choose one mechanism and test every view for cropping. |
| Size option, default 1280x1280, closed bounds | Consumer contract is exactly 1280x1280; opts closed by project rule | LOW | Either closed enum (e.g. 256/512/1024/1280/2048) or integer range with hard min/max. Prefer a short enum (matches `avResolutionHeights` precedent). Square only at first; non-square is a feature nobody asked for. |
| Opaque neutral background by default | GSH feeds the PNG through ImageMagick derivatives; a transparent PNG flattened to JPEG goes BLACK unless the consumer composites | LOW | Default opaque light background (white/light grey), not f3d's default `#333333`. Transparent is the differentiator below. |
| Shaded solid, neutral single colour, smooth normals, headlight-style lighting | Users expect "a clean grey part", not wireframe or flat triangles | LOW-MED | Tessellation quality is a fixed server constant relative to bbox diagonal (relative linear deflection; angular tolerance), NOT a client opt. Faceted-looking cylinders are the #1 visual complaint. |
| Tessellation of B-rep (STEP/IGES) before rendering | f3d/VTK render meshes, not B-rep; STL needs none | MED | Stage-1 geometry kernel (FreeCAD/OCCT) then stage-2 renderer. Same tessellation code path as STEP/IGES to STL conversion: build once, reuse. |
| Whole-assembly rendering as ONE image | A STEP file is usually an assembly; the preview must show everything | LOW | Merge all solids/shells/faces into one mesh. No per-part toggling. |
| Headless, offline, deterministic, no GPU, no display server visible to the confinement | Core platform constraints (Landlock, offline) | HIGH (stack phase) | Measured: f3d needs Xvfb on bookworm (note 2026-09-23). Renderer choice is the STACK/eval phase; for features the contract is just "PNG at the requested size". Deterministic output (same input + opts to same bytes or at least same pixels) enables test fixtures. |
| Fail-closed content validation for all five formats | Project invariant; formats are attacker-controlled | MED | STEP: `ISO-10303-21;` start + `HEADER;`/`DATA;`/`END-ISO-10303-21;` structure. IGES: fixed 80-column records, section letters at col 73 (`S`,`G`,`D`,`P`,`T`), terminate record. STL: see section C. DXF: ASCII `0\nSECTION` (optional leading BOM / `999` comment groups) or binary sentinel `AutoCAD Binary DXF\r\n\x1a\x00`. Detect **DWG** (`AC10xx` magic) explicitly and reject with a distinct "dwg not supported" error, not a generic unknown-format 422. |
| Input size caps per format, enforced before parsing | Parser memory/time is attacker-chosen | LOW | Start from consumer parity (40 MB STEP/IGES/STL, 5 MB DXF) as env-configurable defaults, then confirm by memory/timing gate (pandoc precedent: memory-bound, Path-B lowered the cap). Do NOT copy the 500 B floor (see anti-features). |
| Transient/terminal retry classification | Platform invariant | LOW-MED | Geometry-kernel timeout on a given file is deterministic: terminal (document/pandoc precedent). Renderer-stage timeout/crash is transient. Stage-aware like `isAVTerminal`. |
| Stable error codes for CAD failures | Platform invariant (`internal/errcode`) | LOW | New codes: unsupported format incl. dwg, no geometry found, input too large, geometry unreadable, render failed. Closed set, documented in `docs/ERROR-CODES.md`. |

#### B. Dimensions report (second output of `render`, new role)

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Axis-aligned bbox in file coordinates: `min[3]`, `max[3]`, `size[3]` | This is exactly what the consumer's "Size: X x Y x Z mm" overlay was built from | LOW-MED | **Must be a TIGHT bbox.** OCCT's default shape bbox (`BRepBndLib::Add`, FreeCAD `Shape.BoundBox`) can be loose for curved/NURBS faces (uses poles/tolerance). Use the optimal/tight variant (FreeCAD `Shape.optimalBoundingBox`, MEDIUM confidence the API exists in the pinned build) and add a fixture test with a rotated cylinder/sphere whose analytic bbox is known. Label it `bbox_kind: "axis_aligned"` (it overstates the true envelope of a rotated part). |
| Volume where, and only where, it is defined | `part_volume` is what the consumer prices on; a wrong number is worse than no number | MED | Volume is computed only for sources with closed solid geometry (see matrix below). Everything else returns `volume: null` with a closed reason code, never `0`. Note a measured baseline: box 20x10x5 gives exactly 1000.0. |
| `units` block with explicit provenance (see "Honest units semantics") | Project's stated honesty requirement; the consumer overlay silently printed "mm" for everything | MED | Per-format rules below. Every measured quantity names its unit or says it has none. |
| Solid / body count and an assembly-ish indicator | A volume is only interpretable if you know whether it sums 1 or 50 bodies | LOW | `solid_count` (B-rep), `shell_count`, `component_count`/`triangle_count` for meshes. `volume` for multi-solid input = SUM of solid volumes (no boolean union): overlapping solids double count, and instanced parts count once per instance. State this in the contract. |
| Per-quantity status, no single boolean verdict | Project lesson from sanitize/inspect: report only what was verified, closed vocabularies, no `ok`/`clean` flag | LOW-MED | e.g. `volume: {value, unit, status: "computed"}` or `{value: null, status: "unavailable", reason: "no_solid_geometry" | "mesh_not_closed" | "two_dimensional_source" | "invalid_geometry"}`. The report is produced even when volume is unavailable; the JOB fails only if the PNG cannot be produced. |
| Schema version field + closed reason vocabulary documented in `docs/` | Same discipline as `docs/FINDINGS.md`/`ERROR-CODES.md` | LOW | Doc-sync test (existing pattern) binds Go constants to the doc table. |
| JSON stored as a second `job_outputs` row with the new role, exposed in `outputs[]` | Reuses the role mechanism built in v1.13 | LOW | Needs CHECK widening migration + `OutputRoleDimensions` const + `AddOutputWithRole` call. Terminal webhook must keep selecting `primary` only (existing invariant). |
| 2D (DXF) report: x/y bbox only, `volume: null, reason: two_dimensional_source` | Scope statement | LOW | Compute extents from entities, NOT from `$EXTMIN/$EXTMAX` (header values are frequently stale or at their `1e20` defaults; MEDIUM confidence, from ezdxf practice). |

**Dimensions availability matrix (what each source can honestly report)**

| Source | bbox | volume | surface area | Unit provenance | Notes |
|--------|------|--------|--------------|-----------------|-------|
| STEP (solids) | 3D tight | yes (sum of solids) | yes | declared in file; OCCT converts to mm on read | Assemblies resolved to instances by OCCT; PMI/colours/layers ignored. |
| STEP (surfaces/wires only, no solids) | 3D | null (`no_solid_geometry`) | faces only | as above | Seen in practice from surface modellers. |
| IGES (typical: trimmed surfaces) | 3D | null (`no_solid_geometry`) unless file has real B-rep solid (entity 186) | yes | declared in Global Section, param 14 flag | Do not trust `Shape.Volume` on an open shell: OCCT will return a number. Gate on `Shape.Solids` non-empty AND valid. |
| IGES with closed shell but no solid entity | 3D | null by default | yes | as above | Sewing to a solid is a differentiator (HIGH risk), not default. |
| STL, closed and consistently oriented | 3D | yes (signed-tetra sum) | yes | NONE | Volume valid only if every edge is shared by exactly two triangles. |
| STL, open / non-manifold / self-intersecting | 3D | null (`mesh_not_closed`) | yes | NONE | Check manifoldness explicitly before trusting the sum. |
| DXF | 2D (x,y) | null (`two_dimensional_source`) | null | `$INSUNITS` | If entities have non-zero Z, still report x/y only and add a closed note that Z was ignored. |

#### C. Format semantics the engine must get right

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| STL ASCII vs binary detection by size equation, never by the `solid` prefix | Binary STL files starting with `solid` exist in the wild (header text is free); naive sniff parses a binary file as text | LOW-MED | Rule (verified): treat as binary iff `filesize == 84 + 50*N` where N = little-endian uint32 at offset 80; otherwise if it starts with `solid` and contains `facet normal`/`endsolid`, ASCII; otherwise reject. Check size equation BEFORE trusting N for any allocation (header can lie; matches the project's "an unexamined input never gets an optimistic answer" lesson). |
| STL sanity: N>0, finite coordinates, triangle cap | NaN/Inf make bbox/fit undefined; N=0 renders nothing | LOW | Reject with a stable code; never emit a blank PNG as "success". |
| STL has NO units; report `unspecified`, never invent mm | Explicit scope requirement | LOW | See honest-units section. |
| STEP/IGES unit handling via the kernel's unit-aware import | Files declare units (`SI_UNIT` with prefix, `CONVERSION_BASED_UNIT` e.g. inch for STEP; Global-Section flag/name for IGES); OCCT converts geometry to mm and can report the declared unit (`STEPControl_Reader::FileUnits`, verified) | LOW-MED | Report `unit: "mm"` (the unit of the numbers) plus `declared_unit` (what the file said). Mixed unit contexts in one STEP: kernel converts per shape; report the list. |
| DXF 2D vs 3D content detection | DXF is mostly 2D drafting but can carry 3D (3DFACE, MESH, polyface POLYLINE, SURFACE, 3DSOLID/BODY/REGION as proprietary ACIS) | MED | Closed finding: `acis_entities_ignored` (3DSOLID/BODY/REGION cannot be read by OSS tools; do not pretend to render them). 3D wireframe DXF renders top-down only (ezdxf projects 3D to XY; verified). |
| DXF model space is what gets rendered/converted | Paper-space layouts hold title blocks and viewports that point back at model space; model space holds the geometry | MED | Default: model space, fit to its entity extents. Fallback: if model space is empty and a paper layout has entities (some exporters), use the first non-empty layout and record `layout_used`. |
| DXF layers: honour on/off and frozen; render every visible layer | CAD users expect hidden layers to stay hidden | LOW-MED | ACI colour 7 means white-or-black depending on background: on a white page it must render BLACK (classic white-on-white failure). Library handles it if the background is told. |
| DXF units from `$INSUNITS`, not `$MEASUREMENT` | `$INSUNITS` defines modelspace units; `$MEASUREMENT` only selects imperial/metric linetypes/hatches (verified) | LOW | Code table (verified): 0 unitless, 1 in, 2 ft, 3 mi, 4 mm, 5 cm, 6 m, 7 km, 8 uin, 9 mil, 10 yd, 13 micron, 14 dm, ... Old R12 files have no `$INSUNITS` at all: same as 0. |
| `dxf to svg` and `dxf to pdf` as VECTOR outputs, content-fit page | Users expect lines/arcs/circles/splines/text/hatches to stay vector, correct Y orientation (DXF is Y-up, SVG is Y-down), linetypes and lineweights preserved | MED | Page = content extents + margin ("auto"). SVG `viewBox` from extents. Scale fidelity (1:1 print) is NOT promised. Text uses substitute fonts (SHX fonts are not available), say so in docs. Library choice (ezdxf drawing add-on has SVG and PDF backends, but its PDF backend needs PyMuPDF which is AGPL-licensed; verify with the owner before adopting, or rasterless alternatives) is a STACK decision. |
| `step/stp/iges to stl` as one merged binary STL, in mm | Standard CAD to mesh conversion | MED | Same tessellation as render stage. STL cannot carry units, so the contract is "millimetres, always" (documented), even for an inch STEP: numbers differ by 25.4x from the source's native values, which downstream consumers must know. ASCII STL output is not offered. |
| `stp` treated as alias of `step` | Same format, two extensions; `NormalizeFormat` already does this for `jpeg`/`tif` | LOW | One registry entry, alias in normalization, sniff by content only. |

### View conventions (the axis contract to write into the docs)

Camera positions relative to the part's own right-handed, Z-up axes (matches FreeCAD standard views; HIGH confidence on FreeCAD `Front` looking along +Y, MEDIUM on the exact iso vector):

| View | Camera sits at | Looks toward | Screen up | Screen right |
|------|----------------|--------------|-----------|--------------|
| `front` | -Y | +Y | +Z | +X |
| `back` | +Y | -Y | +Z | -X |
| `right` | +X | -X | +Z | +Y |
| `left` | -X | +X | +Z | -Y |
| `top` | +Z | -Z | +Y | +X |
| `bottom` | -Z | +Z | -Y | +X |
| `iso` | (+1, -1, +1) direction from centre | toward centre | +Z | front-right-top corner visible |

First-angle vs third-angle projection: **not applicable** to a single-view preview. It only decides how several orthographic views are ARRANGED on a drawing sheet (ISO/European first-angle vs ANSI/US third-angle); each individual view (front, top, right) is the same picture in both systems. Do not expose a projection-angle option; naming a view is enough.

Perspective vs orthographic: fix one globally. Recommendation for engineering parts: orthographic (parallel edges stay parallel, sizes comparable across views), but confirm the chosen renderer exposes it (LOW confidence it does in the pinned f3d). Not a client option.

### Honest units semantics (per format, the contract)

Principle: every number names its unit or says it has none; provenance says where the unit came from; the service never silently upgrades "unknown" to mm.

```
"units": {
  "length": "mm" | "unspecified",
  "source": "declared" | "unspecified",
  "declared": "mm" | "inch" | "cm" | "m" | ... | null
}
```
(Field names are illustrative; the reason/provenance vocabularies must be closed and doc-synced.)

| Source | What is reported | `length` | `source` | Notes |
|--------|------------------|----------|----------|-------|
| STEP | Values converted to mm by the kernel; original declared unit reported | `mm` | `declared` | If the file declares no usable length unit (broken exporter), report `unspecified`: the kernel assumes mm, which is an assumption, not a fact. Volume unit is `mm^3` only when `length` is `mm`. |
| IGES | Same as STEP, from Global Section flag (1 in, 2 mm, 3 named in param 15, 4 ft, 5 mi, 6 m, 7 km, 8 mil, 9 micron, 10 cm, 11 microinch) | `mm` | `declared` | The flag is known to be unreliable in practice; it is still "declared". Flag 3 needs the unit-name parameter. |
| STL | Raw coordinates, no conversion | `unspecified` | `unspecified` | Volume reported as cubic model units, `volume_unit: "unspecified^3"` or equivalent; never `mm^3`. |
| DXF | Raw coordinates in `$INSUNITS` unit | from code table (e.g. `mm`, `inch`, `m`) or `unspecified` for 0/absent | `declared` / `unspecified` | No conversion applied (units describe, not transform). Do not infer from `$MEASUREMENT` or from magnitude. |

### Differentiators (Competitive Advantage)

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| `assume_unit` opt (closed enum `mm|cm|m|inch`) for sources with no units, recorded as `source: "client_assumed"` | The real consumer wants mm for STL/DXF-without-units; this lets it say so without the service lying | LOW-MED | Provenance stays honest (same idea as `inspect`'s estimate carrying its origin). Only applies when source unit is unspecified; rejected (422) otherwise to avoid unit-override of declared units. |
| `background: opaque|transparent` (closed) | Transparent PNG for UI overlays | LOW | f3d `--no-background` exists. Default stays opaque (see table stakes). |
| Surface area in the dimensions report | Cheap once the shape is loaded (`Shape.Area`), useful for coating/finishing quotes | LOW | Closed-shell/face area is valid even for surface-only IGES. Closed-mesh area for STL. |
| `quality: draft|standard|fine` (closed) for STEP/IGES to STL and render tessellation | Lets a consumer trade file size for smoothness | LOW-MED | Maps to server constants for relative deflection. Cap resulting triangle count. |
| Mesh health findings for STL: `mesh_not_closed`, `non_manifold_edges`, `inverted_orientation`, `degenerate_triangles` as closed codes | Explains WHY volume is null; helps users fix files | MED | Verify the mesh checks the kernel provides in the pinned build (FreeCAD Mesh has `isSolid`/`hasNonManifolds`/`hasSelfIntersections` style methods; MEDIUM confidence). If all normals are inverted but mesh is closed, take the absolute volume and say so. |
| Per-body breakdown (`bodies[]` with bbox + volume) | Assemblies: see what makes up the total | MED | Defer; only the totals plus `solid_count` ship first. |
| Feature-edge outlines (silhouette + crease edges, not triangle edges) | Much more legible technical look | MED-HIGH | f3d `--edges` draws ALL cell boundaries: on a tessellated STEP that is a triangle wireframe. Needs angle-threshold feature edges or a post pass. Leave default off. |
| Ambient occlusion / anti-aliasing quality pass (f3d `--ambient-occlusion`, `--anti-aliasing`) | Better-looking previews | LOW | Fixed server constants, but each costs render time under software GL: decide by measurement. |
| Closed `layout: model|first_paper` opt for DXF svg/pdf | Handles paper-space-only drawings explicitly | LOW | Never an arbitrary layout NAME (open client string, breaks closed-opts rule). Auto-fallback in table stakes covers most cases. |
| Closed `page: auto|a4|a3` for DXF pdf | Printable output | LOW-MED | Scale-to-fit with margins; still no 1:1 promise. |
| IGES sewing to solid (when shells are closed) so volume can be reported | Many IGES parts are closed surface sets and users still expect a volume | HIGH | Sewing tolerance sensitivity, silent wrong volumes. Defer unless a consumer asks. |

### Anti-Features (Commonly Requested, Often Problematic)

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| "Size: X x Y x Z mm" text overlay on the PNG | The old service did it | Out of scope per milestone; burns unit claims into pixels (an STL becomes "mm" by fiat), cannot be machine-read, collides with consumer thumbnail cropping | Dimensions as a data output (JSON, new role); the consumer draws overlays |
| Client-supplied camera vectors / arbitrary angles / zoom | "Just let me pick the angle" | Breaks the closed-opts invariant; fit-to-view maths becomes the client's problem | Closed view enum; add a view to the enum when a real need appears |
| First/third-angle projection option, multi-view sheet (front+top+right on one image) | Looks like a drawing | A drawing-layout problem, not a preview problem; per-view contract already complete | Separate jobs per view |
| Copying the consumer's 500 B minimum and "skip silently" behaviour | Parity with PreviewGenerator | A valid one-triangle binary STL is 134 B; silent skip with no response was PreviewGenerator's worst trait (consumer got nothing and no error) | Structural fail-closed validation; every rejection is a stable error code |
| Silent success on garbage (blank PNG, bbox of nothing, `volume: 0`) | "Always return something" | The lesson recorded repeatedly in project memory: a scan of nothing is never clean | Typed failure, or `null` + reason |
| Inventing mm for unit-less sources by default | Consumers want a number | Unit-less STL in inches would quote 25.4x wrong silently | `unspecified` by default; opt-in `assume_unit` with provenance |
| Trusting `$EXTMIN/$EXTMAX` or `Shape.Volume` without checking the underlying condition | Easy, fast | Header extents are often stale; `Volume` on open IGES shells returns a number that means nothing | Compute extents from entities; gate volume on valid closed solids / closed manifold mesh |
| DWG support | Fusion/AcmeCAD path handled DWG | Out of scope: FreeCAD cannot read it, `libredwg-tools` absent in bookworm, ODA licence unresolved (commercial decision) | Explicit "dwg not supported" error code so consumers can route elsewhere |
| ACIS 3DSOLID in DXF / PMI / STEP colours and per-part appearance | "Complete" CAD fidelity | ACIS is proprietary; colour round-trip is lost by mesh intermediates; large surface area for little value | Neutral single-colour render; closed finding naming what was ignored |
| Per-part STL export of assemblies, BOM trees, assembly structure JSON | Natural next ask | Whole new product (structure traversal, naming, instance dedupe) | One merged STL; `solid_count` only |
| Raster output formats beyond PNG, ASCII STL output, DXF export, STEP export | Format creep | Each multiplies sniff/validate/test surface | Out of milestone; add by explicit request |
| Overriding a declared unit via `assume_unit` | "The file says inch but it is mm" | Lets one client silently re-scale; makes provenance meaningless | `assume_unit` only fills a gap; never overrides `declared` |

## Feature Dependencies

```
Fail-closed sniffers (step/iges/stl/dxf, dwg-reject)
    └──required by──> every operation (create-job gate, inspect triples)

Registry triples (render|convert, from, to)  ──auto──> /v1/inspect available triples

Closed opts (view/size/background/assume_unit/quality)
    └──requires──> ParseCADOpts strict parser + per-target applicability (AVOpts precedent)
    └──view applies only to 3D sources (step/stp/iges/stl); rejected for dxf

Geometry kernel load (FreeCAD/OCCT) ──> tessellation ──> render stage ──> PNG (role primary)
                              └──────> measurement ──> dimensions JSON (NEW role)
Tessellation ──shared by──> STEP/IGES to STL conversion

New output role `dimensions`
    └──requires──> CHECK widening migration + Go const + AddOutputWithRole call
    └──requires──> handleGetJob outputs[] already role-keyed (exists)
    └──must not disturb──> D-13 exactly-one-primary guard, terminal webhook primary-only

Honest-units contract
    └──requires──> per-format unit extraction (STEP/IGES FileUnits, DXF $INSUNITS, STL none)
    └──enhanced by──> assume_unit opt

dxf to svg/pdf ──independent of──> 3D kernel path (2D library path, its own sniff/limits)

Measured CAD_ENGINE_TIMEOUT / caps ──requires──> corpus incl. large STEP, dense STL, big DXF
Confined renderer (Xvfb vs offscreen EGL/OSMesa) ──gates──> render feature at all
```

### Dependency Notes

- **Render depends on the evaluation-phase renderer choice:** if the renderer needs Xvfb sockets under `/tmp`, the Landlock ruleset and `sharedTmpGuard` (concurrency exactly 1) rules from v1.15 apply; the feature list is unchanged but the phase sequencing must put the evaluation first.
- **Dimensions JSON requires the DB role migration before the worker can write it**, so migration lands in the same phase as the render pipeline, not after.
- **STEP/IGES to STL and render share tessellation:** build once; define the deflection constants once.
- **DXF render to PNG is an open scope question.** The milestone lists dxf for svg/pdf and a 2D bbox "with dimensions", but dimensions are the second output of a `render` job. If dxf `render` is supported, the PNG should come from the same 2D drawing pipeline (not the 3D renderer) and `view` must be rejected for it. If it is not, a DXF client cannot get its bbox at all. Recommend supporting dxf `render` to PNG with fixed top-down and flagging this for requirements.
- **`view` conflicts with 2D sources:** applicability validation (AVOpts `avThumbnailTargets` shape) must reject `view` for dxf rather than ignore it.

## MVP Definition

### Launch With (v1.16 core)

- [ ] Evaluation gate result: confined headless renderer chosen by measurement (blocks everything else)
- [ ] Sniffers + caps + error codes for step/stp/iges/stl/dxf, explicit dwg rejection
- [ ] `render` step/stp/iges/stl to PNG: `view` enum (7), `size`, default 1280x1280, opaque background, fit-to-view, neutral shading, fixed tessellation constants
- [ ] Dimensions JSON second output (new role): tight bbox, volume with closed reason codes, solid/triangle counts, honest `units` block per format
- [ ] STEP/IGES to STL (merged binary, mm)
- [ ] DXF to SVG and PDF (model space, visible layers, content-fit page, `$INSUNITS` provenance)
- [ ] `cmd/cad-worker`, queue, Dockerfile, compose, chart, KEDA, measured Landlock ruleset, preflight, measured timeout/caps, live gate
- [ ] SEED-007 preview contract documented on CAD: output role, fixed default size, closed views, failure semantics (typed, never silent), dimensions as a separate role

### Add After Validation (v1.x)

- [ ] `assume_unit` - trigger: first consumer complaint about `unspecified` on STL/DXF
- [ ] Surface area field - trigger: finishing/coating quote request
- [ ] `background: transparent`, `quality` enum - trigger: UI overlay or file-size complaints
- [ ] STL mesh health findings - trigger: users asking why volume is null
- [ ] DXF `layout`/`page` opts

### Future Consideration (v2+)

- [ ] Per-body breakdown, feature-edge outlines, IGES sewing, DWG (after the ODA licence decision), SEED-007 contract on the other engine classes

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| Confined headless render to PNG with closed views + fit | HIGH | HIGH | P1 |
| Fail-closed sniffers/caps/error codes (5 formats + dwg reject) | HIGH | MED | P1 |
| Dimensions JSON (tight bbox, volume + reasons, counts) | HIGH | MED | P1 |
| Honest units block per format | HIGH | MED | P1 |
| STEP/IGES to STL | HIGH | MED | P1 |
| DXF to SVG/PDF | MEDIUM-HIGH | MED | P1 |
| Worker/queue/chart/KEDA/Landlock/live gate | HIGH | HIGH | P1 |
| `assume_unit` | MEDIUM | LOW-MED | P2 |
| Surface area | MEDIUM | LOW | P2 |
| `background`, `quality` | LOW-MED | LOW | P2 |
| STL mesh health findings | MEDIUM | MED | P2 |
| DXF `layout`/`page` | LOW | LOW-MED | P3 |
| Feature-edge outlines, per-body breakdown | LOW-MED | MED-HIGH | P3 |
| IGES sewing to solid | MEDIUM | HIGH | P3 |

## Sensible limits (starting points; all must be confirmed by the measurement gate)

| Limit | Starting value | Basis | Confidence |
|-------|----------------|-------|------------|
| Max input bytes STEP/IGES/STL | 40 MiB | Consumer's current ceiling | MEDIUM (parity); real cap from memory gate, expect a Path-B reduction as with pandoc (memory, not time, was the binding constraint) |
| Max input bytes DXF | 5 MiB | Consumer's current ceiling (a limit of the commercial converter, not necessarily of the new path) | MEDIUM; can be raised if the 2D path measures well |
| Min input bytes | none (structural validation instead) | One-triangle STL is 134 B | HIGH |
| Triangle cap (STL input, and tessellated output) | decide by measurement; a few million is the order of magnitude where software-GL render time dominates | No authoritative source found | LOW |
| End-to-end time per job | measured; consumer saw up to 60 s/file on Fusion | Use the project's timing-gate formula (p95 x factor under CAP 900 s) | LOW until measured |
| Concurrency | 1 per container until memory is measured | OCCT STEP parse memory scales with entity count; pandoc precedent | MEDIUM |

## Competitor / Reference Analysis

| Feature | PreviewGenerator (Fusion + AcmeCAD) | Typical online quoting/preview services (Xometry-style upload analysis, Autodesk viewer pipelines) | Our approach |
|---------|-------------------------------------|---------------------------------------------------------------------------------------------------|--------------|
| Views | one view param, fit-to-view | fixed standard views + interactive | closed enum of 7, fit-to-view per view |
| Dimensions | `part_volume` + overlay text, always "mm" | bbox, volume, surface area, units auto-detected with user override | data JSON, units provenance, null+reason where undefined |
| Failure behaviour | no reply at all | typed error to user | stable error codes, never silent |
| Limits | 500 B / 40 MB / 5 MB DXF / 60 s | similar order | env-configurable, measured |
| Units on STL | implicitly mm | usually prompts or assumes mm | `unspecified` unless `assume_unit` |

Online-service rows are MEDIUM/LOW confidence (general knowledge of the category, not a specific verified product page).

## Sources

- Local: `/Users/apaderin/dev/notes/architecture/octoconv-cad-preview.md` (measurements 2026-09-23, HIGH, project-owned)
- Local: `/Users/apaderin/dev/notes/architecture/gsh-cad-preview-integration.md` (PreviewGenerator behaviour and limits, HIGH)
- Local: `internal/jobs/jobs.go`, `internal/db/migrations/0015_operation_and_output_roles.sql`, `internal/convert/avopts.go`, `internal/inspect/findings.go`, `internal/api/handlers.go` (existing role, opts and inspect mechanisms, HIGH)
- ezdxf units concept (modelspace units from `$INSUNITS`, `$MEASUREMENT` independent, no implicit unit conversion): https://ezdxf.readthedocs.io/en/stable/concepts/units.html (HIGH)
- ezdxf drawing add-on (SVG/PDF/Matplotlib/HPGL backends, no 3D rendering, PyMuPDF for PDF, layout support): https://ezdxf.readthedocs.io/en/stable/addons/drawing.html (HIGH for capabilities; the AGPL status of PyMuPDF is from general knowledge, MEDIUM, verify)
- `$INSUNITS` code table (via ezdxf docs / search results): https://ezdxf.readthedocs.io/en/stable/concepts/units.html (HIGH)
- OCCT units on STEP import (convert to mm, `STEPControl_Reader::FileUnits`): https://occt3d.com/dev/content/units-step-files/index.html and https://dev.opencascade.org/doc/occt-7.8.0/refman/html/classSTEPControl__Reader.html (MEDIUM-HIGH)
- IGES unit flag (param 14: 1 in, 2 mm): https://dev.opencascade.org/doc/occt-7.8.0/refman/html/classIGESData__BasicEditor.html and PTC IGES Linear Unit page (MEDIUM; full flag table 3-11 from the IGES spec, memory, verify)
- STL binary/ASCII ambiguity and `84 + 50*N` size check: https://graphics.rwth-aachen.de:9000/OpenMesh/OpenMesh/issues/47, https://lists.ros.org/pipermail/ros-users/2011-January/061948.html, https://www.3dmag.com/?p=13897 (MEDIUM-HIGH)
- f3d man page (options `--camera-position`, `--camera-direction`, `--camera-view-up`, `--resolution`, `--no-background`, `--up` default +Y, `--edges`, `--anti-aliasing`, `--ambient-occlusion`): https://man.archlinux.org/man/f3d.1.en (HIGH for option existence)
- FreeCAD standard views (Front looks along +Y, Z up, Isometric view): https://wiki.freecad.org/Std_ViewFront, https://wiki.freecad.org/Std_ViewIsometric (HIGH for front, MEDIUM for iso vector)
- FreeCAD/OCCT shell validity (solid = closed shell with coherent orientation; check `isValid()`/`Solids`/`Volume`): FreeCAD tracker and OCCT forum threads from search results (MEDIUM)
- Not verified: tight-bbox API availability in the pinned FreeCAD (`optimalBoundingBox`), f3d orthographic option, mesh health method names, STEP memory/time at 40 MB, triangle-count render cost.

---
*Feature research for: CAD preview and measurement engine class (v1.16)*
*Researched: 2026-10-02*
