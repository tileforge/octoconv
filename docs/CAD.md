# CAD class

The `cad` engine converts and previews 3D models (STEP, IGES, STL) and 2D drawings (DXF). A client uploads a
file to `POST /v1/jobs` with a `target`, an optional `operation` (`convert` by default, or `render`) and
optional `opts`, and reads the result from `GET /v1/jobs/{id}`. This document is the client contract of the
class: what it accepts, what it returns, the limits it enforces and the codes it fails with. Tests in
`internal/convert` and `internal/worker` hold every table here equal to the code, in both directions.

## Formats

| format | aliases | operations |
|---|---|---|
| `step` | `stp` | `convert`, `render` |
| `iges` | `igs` | `convert`, `render` |
| `stl` | — | `render` |
| `dxf` | — | `convert`, `render` |
| `dwg` | — | `convert`, `render` |

An alias is accepted wherever a format is, in the upload's extension and in `target`, and in any letter case.
A DWG whose header is `AC10` plus two digits is format `dwg`. A pre-R2.5 signature with a dot, such as `AC1.40`, is not recognised.
`GET /v1/formats` lists the same pairs under its `operations` key.

## Operations and pairs

| operation | from | to |
|---|---|---|
| `convert` | `step` | `stl` |
| `convert` | `iges` | `stl` |
| `convert` | `dxf` | `svg` |
| `convert` | `dxf` | `pdf` |
| `convert` | `dwg` | `svg` |
| `convert` | `dwg` | `pdf` |
| `convert` | `dwg` | `png` |
| `render` | `step` | `png` |
| `render` | `iges` | `png` |
| `render` | `stl` | `png` |
| `render` | `dxf` | `png` |
| `render` | `dwg` | `png` |

A 3D `convert` writes a binary STL in millimetres. A DXF `convert` draws the drawing to SVG or PDF. A DWG `convert` draws it to SVG, PDF or a square PNG and writes no measurements document. A `render`
draws a square PNG preview and computes its measurements (see Outputs). Any other pair is
`unsupported_conversion`.

## Render options

`opts` is a JSON object. Every option is optional, and an omitted option takes its default.

| option | values | default |
|---|---|---|
| `view` | `iso`, `front`, `back`, `left`, `right`, `top`, `bottom` | `iso` |
| `size` | `256..2048` | `1280` |
| `quality` | `draft`, `standard`, `fine` | `standard` |
| `background` | `opaque`, `transparent` | `opaque` |
| `assume_unit` | `mm`, `cm`, `m`, `inch`, `foot`, `micrometre` | `none` |
| `dimensions` | `true`, `false` | `true` |
| `material` | `steel`, `copper`, `aluminum` | `steel` |
| `color` | `natural`, `blue`, `black` | `natural` |
| `finish` | `bare`, `polished`, `anodized`, `matte_powder_coat` | `bare` |
| `layout` | `1..255` characters | `none` |

`size` is the edge of the square PNG in pixels, an integer. `quality` is the tessellation density of a STEP or
IGES model. An IGES model with no solid is sewn at tolerance 1e-6 before meshing. Sewing runs for IGES only,
and a volume is reported only when every face joins a closed valid solid (docs/MEASUREMENTS.md).
`background` `opaque` is white #FFFFFF; `transparent` leaves the background at alpha 0.
Studio lighting and reflections reveal the curvature and creases of a 3D surface. If a sheet projects to less
than one pixel in the selected view, the preview shows a thin profile in the surface color.
`assume_unit` names the unit of a file that declares none; its default `none` assumes nothing, and it never
overrides a declared unit (docs/MEASUREMENTS.md, Unit sources). An unknown option, a value outside its set or
a `size` outside the range is `invalid_options`.

For a STEP, IGES, or STL preview, `material`, `color`, and `finish` set the appearance of the whole model.
`natural` shows the selected metal's color for bare, polished, and anodized surfaces. A natural matte
powder coat is neutral gray. Any combination is valid. For example, submit
`operation=render`, `target=png`, and `opts={"material":"aluminum","color":"blue","finish":"anodized"}`.
The surface settings affect only the PNG. Model geometry and measurements come from the input file.

`dimensions` adds overall bounding-box dimension lines and a `Size: X × Y × Z unit` heading to a 3D
preview. Dimensions are enabled by default. Set the boolean `dimensions` to `false` to render the
selected surface without dimension lines or a heading.
When the measurements document includes `bodies`, the same kind of line is drawn for each solid box,
at most 12 boxes, largest diagonal first. A box that matches the overall box is omitted. The heading
stays the overall size. An STL has no per-solid boxes, so its preview stays overall-only.

Values and units come from the worker's measurements document, in X, Y, Z order.
They describe the axis-aligned bounding box in the file's coordinates, not hole diameters or tolerances.
Labels use two decimal places with a redundant trailing zero removed; very small or large values use
four significant digits, with scientific notation when needed. Unknown units are labeled `units unspecified`; units
supplied with `assume_unit` are labeled `assumed`. An STL is never silently treated as millimetres.
In an orthographic view, hidden or subpixel axes have no dimension line but remain in the heading.
Short dimensions use outward arrows; labels are moved apart to avoid overlap. If a separate label
cannot fit clearly within the frame, its value remains in the heading.
The annotation font is the embedded Go Regular font. DXF drawings retain their own dimension entities.

## Option applicability

| operation | format | accepted options |
|---|---|---|
| `convert` | `step` | `quality` |
| `convert` | `iges` | `quality` |
| `convert` | `dxf` | `layout` |
| `convert` | `dwg` | `layout` |
| `render` | `step` | `view`, `size`, `quality`, `background`, `assume_unit`, `dimensions`, `material`, `color`, `finish` |
| `render` | `iges` | `view`, `size`, `quality`, `background`, `assume_unit`, `dimensions`, `material`, `color`, `finish` |
| `render` | `stl` | `view`, `size`, `background`, `assume_unit`, `dimensions`, `material`, `color`, `finish` |
| `render` | `dxf` | `size`, `background`, `assume_unit`, `layout` |
| `render` | `dwg` | `size`, `background`, `assume_unit`, `layout` |

Submitting options requires an approved version 2 model render worker. Deployments with the version 1
render manifest accept previews with omitted or empty `opts`. Drawing and conversion workers currently
accept jobs with omitted or empty `opts` only.

A well-formed option outside this table for its operation and format is refused at create with 422
`opts_not_applicable`, before the upload is stored. A DXF or DWG is drawn top-down, so `view` does not apply to it.
`layout` names one paper layout. It is 1 to 255 characters and is not a closed set. Omitted, the drawing stays in
model space, except when model space has nothing drawable and exactly one paper layout has a top view: that sheet
is drawn. A name draws that layout even when model space has geometry and when several sheets exist. `Model` and a
name that is not in the file fail the drawing. `layout` does not apply to a STEP, IGES, or STL file.

## Outputs

| role | format | when |
|---|---|---|
| `primary` | the requested target: `stl` for a 3D `convert`, `svg` or `pdf` for a DXF `convert`, `svg`, `pdf` or `png` for a DWG `convert`, `png` for every `render` | every successful job |
| `measurements` | `json`, the document of docs/MEASUREMENTS.md | optional on a successful `render` |

The render worker computes and validates the measurements before uploading the PNG. The API publishes
the optional measurements JSON only when the worker assignment includes an output link for it. The current
scheduler supplies the primary PNG link only. On a successful job, `download_url` points to that PNG.

## Stages

A `cad` job reports the stage it is in. `GET /v1/jobs/{id}`, every `progress` event of
`GET /v1/jobs/{id}/events` and every progress webhook of a `cad` job carry three keys: `stage`, the word of
the current stage; `stage_index`, its position in the job's route, counted from 1; and `stage_count`, the
length of the route. `progress_pct` and `eta_seconds` of a `cad` job are always null, from `queued` to the
terminal status: the stage is the progress of this class. A job of any other class carries none of the three
keys.

The three keys are null while the job is `queued` and before its first stage starts. A new attempt and a
return to the queue reset them to null, and the route starts again from 1. On `done`, `failed` and
`canceled` the last reported stage stays: a `done` job reads the last stage of its route with `stage_index`
equal to `stage_count`, a failed job reads the stage it stopped in. When the event stream polls past more
than one stage, it sends one event for every stage it missed, in order and each with `status` `active`,
before the event of the current state.

| operation | from | to | stages | stage_count |
|---|---|---|---|---|
| `convert` | `step` | `stl` | `guard` → `mesh` → `judge` | `3` |
| `convert` | `iges` | `stl` | `guard` → `mesh` → `judge` | `3` |
| `convert` | `dxf` | `svg` | `guard` → `dxf` → `ink` | `3` |
| `convert` | `dxf` | `pdf` | `guard` → `dxf` → `ink` → `rsvg` → `judge` | `5` |
| `render` | `step` | `png` | `guard` → `mesh` → `render` → `judge` | `4` |
| `render` | `iges` | `png` | `guard` → `mesh` → `render` → `judge` | `4` |
| `render` | `stl` | `png` | `mesh` → `render` → `judge` | `3` |
| `render` | `dxf` | `png` | `guard` → `dxf` → `ink` → `rsvg` → `judge` | `5` |
| `convert` | `dwg` | `svg` | `dwg` → `guard` → `dxf` → `ink` | `4` |
| `convert` | `dwg` | `pdf` | `dwg` → `guard` → `dxf` → `ink` → `rsvg` → `judge` | `6` |
| `convert` | `dwg` | `png` | `dwg` → `guard` → `dxf` → `ink` → `rsvg` → `judge` | `6` |
| `render` | `dwg` | `png` | `dwg` → `guard` → `dxf` → `ink` → `rsvg` → `judge` | `6` |

- `guard` checks the input before the heavy work starts.
- `dwg` reads the DWG and writes an ASCII DXF. The later stages draw that DXF.
- `mesh` builds the mesh of the model; for `stl` → `png` it is the `cad-mesh stats` step over the uploaded mesh.
- `render` draws the 3D preview.
- `dxf` draws the DXF to SVG and validates that SVG.
- `ink` rasterises the drawing once and checks that the raster has ink on it.
- `rsvg` writes the final PNG or PDF.
- `judge` checks the finished result.

`dxf` → `svg` has no `judge`. Its SVG is validated inside the `dxf` stage and no code runs after `ink`, so the
route ends there and a `done` job reads `ink` 3/3. `dwg` → `svg` also has no `judge`; a `done` job reads `ink` 4/4.

## Size ceilings

| format | operation | bytes | MiB | effective under the chart's 183 MiB transport cap |
|---|---|---|---|---|
| `step` | `convert` | 66060288 | 63 | 66060288 |
| `step` | `render` | 66060288 | 63 | 66060288 |
| `iges` | `convert` | 125829120 | 120 | 125829120 |
| `iges` | `render` | 125829120 | 120 | 125829120 |
| `stl` | `render` | 190840832 | 182 | 190840832 |
| `dxf` | `convert` | 123731968 | 118 | 123731968 |
| `dxf` | `render` | 123731968 | 118 | 123731968 |

A DWG has no row here and no format ceiling. The worker checks the ASCII DXF it writes against the DXF ceiling.
A file above its format's ceiling is refused at create with 413 `payload_too_large`, and the message names the
format and its ceiling in MiB. `POST /v1/inspect` reports the same ceiling as `max_bytes` on every `cad` offer,
and the worker checks it again before any engine runs (`input_too_large`). Convert and render share one ceiling
per format. The Helm chart's global `MAX_UPLOAD_BYTES` and `INSPECT_MAX_UPLOAD_BYTES` apply before these
ceilings and sit above every one of them plus the request's multipart framing, so a file at or below its format's
ceiling gets the per-format answer from both endpoints.

Inside the pipeline, the engine also refuses a model or drawing that passes the byte ceiling but is too large
to process. Each of these is `cad_too_complex`:

| limit | constant | value |
|---|---|---|
| STEP entities the guard admits | `CADMaxSTEPEntities` | 754762 |
| STEP assembly instances (NAUO) the guard admits | `CADMaxNAUO` | 1518 |
| triangles a render draws | `CADMaxRenderTriangles` | 3835382 |
| DXF INSERT/MINSERT instances after expansion | `CADMaxDXFExpandedInserts` | 170031 |
| DXF entities after expansion | `CADMaxDXFExpandedEntities` | 850155 |
| DXF block nesting depth | `CADMaxDXFNesting` | 16 |
| bytes of each SVG drawn from a DXF, and of its `texts.json` | `CADMaxDXFSVGBytes` | 140509184 |
| bytes of the PDF drawn from a DXF | `CADMaxDXFPDFBytes` | 19922944 |
| inflated PDF content, as a multiple of the PDF limit | `cadPDFInflateFactor` | 64 |

## Time limits

| variable | default seconds | covers |
|---|---|---|
| `CAD_GUARD_TIMEOUT` | 21 | the pre-read guard of a STEP, IGES or DXF job |
| `CAD_MESH_TIMEOUT` | 324 | tessellation of a STEP or IGES model, or reading an STL; past it the job is `cad_mesh_timeout` |
| `CAD_RENDER_TIMEOUT` | 521 | drawing a 3D PNG; past it the job is `cad_too_complex` |
| `CAD_DXF_TIMEOUT` | 186 | drawing a DXF to SVG; past it the job is `cad_too_complex` |
| `CAD_DWG_TIMEOUT` | 186 | reading a DWG into ASCII DXF, the same number as the DXF deadline and not a separate measurement; past it the job is `cad_too_complex` |
| `CAD_RSVG_TIMEOUT` | 217 | each of the two `rsvg-convert` runs of a DXF or DWG job, the ink check and the PDF or PNG; past it the job is `cad_too_complex` |
| `CAD_ENGINE_TIMEOUT` | 886 | the whole attempt, set on the queue task |

The guard, mesh, render, DXF and rsvg defaults were measured, not assumed. Each of those deadlines is 1.5 times
the slowest wall measured for it at the input ceilings in the production worker, and the 3D render takes the
remainder of the engine budget. The DWG stage starts at the same 186 s as the DXF deadline; that number is not
a separate measurement. The engine timeout is the longest of the three routes plus a measured 20 s of I/O:
21 + 324 + 521 + 20 = 886 for 3D, 21 + 186 + 2 x 217 + 20 = 661 for DXF, and 21 + 186 + 186 + 2 x 217 + 20 = 847
for a DWG drawing. The worst route is render_iges, on the cell IR16MiB
(`internal/convert/testdata/evidence/76-measured-bounds-keda-proof-client-docs/76-03-derived.txt`). For `cad`,
`POST /v1/inspect` reports `estimate_source: measured` and an estimated maximum of 886 s at the default.

## Scaling

The `cad` worker scales with KEDA on the depth of its queue (pending, active and retrying tasks). Each source is
a value of the chart or a field of a measurement line in
`internal/convert/testdata/evidence/76-measured-bounds-keda-proof-client-docs/`.

| item | value | source |
|---|---|---|
| `threshold`, queued tasks per replica | 1 | `values.yaml keda.cad.threshold` |
| `minReplicaCount` | 0 | `values.yaml keda.cad.minReplicaCount` |
| `maxReplicaCount` | 2 | `values.yaml keda.cad.maxReplicaCount` |
| `pollingInterval`, seconds | 10 | `values.yaml keda.cad.pollingInterval` |
| `cooldownPeriod`, seconds | 90 | `values.yaml keda.cad.cooldownPeriod` |
| `scaleDownStabilizationSeconds` | 900 | `values.yaml keda.cad.scaleDownStabilizationSeconds` |
| `CAD_WORKER_CONCURRENCY`, jobs per replica | 1 | `configmap CAD_WORKER_CONCURRENCY` |
| production image, uncompressed MiB | 1118.1 | `evidence/76-09-image-size.txt IMAGE_SIZE.uncompressed_mib` |
| production image, compressed MiB | 315.5 | `evidence/76-09-image-size.txt IMAGE_SIZE.compressed_mib` |
| cold pull from a local registry into an empty daemon, maximum of 3, seconds | 7.11 | `evidence/76-09-image-size.txt COLD_PULL_DIST.wall_max` |
| transfer of the compressed image at 100 Mbit/s, seconds | 26.5 | `evidence/76-09-image-size.txt PULL_CALC[bandwidth_mbit=100].transfer_s` |
| transfer at 1000 Mbit/s, seconds | 2.6 | `evidence/76-09-image-size.txt PULL_CALC[bandwidth_mbit=1000].transfer_s` |
| transfer at 10000 Mbit/s, seconds | 0.3 | `evidence/76-09-image-size.txt PULL_CALC[bandwidth_mbit=10000].transfer_s` |
| startup preflight, container start to `cad sandbox proven`, maximum of 3, seconds | 7.67 | `evidence/76-09-image-size.txt PREFLIGHT_DIST.max` |
| cold start: cold pull + 100 Mbit/s transfer + preflight, seconds | 41.28 | `evidence/76-09-image-size.txt DECISION.cold_start_s` |
| premise: cold start at most 0.1 x the 886 s engine timeout (1 holds, 0 fails) | 1 | `evidence/76-09-image-size.txt DECISION.premise_holds` |
| decision: `minReplicaCount` | 0 | `evidence/76-09-image-size.txt DECISION.value` |
| OrbStack: scale from zero, job submit to pod Ready, seconds | 31.6 | `evidence/76-09-keda-loadproof.txt CALIBRATION.submit_to_ready_s` |
| OrbStack: three jobs above `maxReplicaCount`, submit to 2 Ready replicas, seconds | 46.9 | `evidence/76-09-keda-loadproof.txt TRANSITION[ready=2].since_submit_s` |
| OrbStack: drain to 0 after the last job finished, seconds | 98.6 | `evidence/76-09-keda-loadproof.txt DRAIN.drain_s` |
| OrbStack: image pull events of the cad pod | absent | `evidence/76-09-keda-loadproof.txt CALIBRATION.pull_events` |

`minReplicaCount` is 0. A cold `cad` replica costs about 41 s before its first job: 7.11 s to pull and unpack
the 315.5 MiB image, 26.5 s to move it over a 100 Mbit/s link and 7.67 s for the sandbox preflight. The check was
fixed before the measurement: the cold start must be at most a tenth of the 886 s engine timeout, 88.6 s. At
41.28 s the premise holds, so a cold start costs little next to a job that may run for minutes. An idle replica,
in contrast, holds 2 CPU and 4 GiB on every cluster all day. On the OrbStack shared image store the pull is near
0; the registry cold pull is measured separately, as for the 71-08 evaluation image and the v1.7 phase 33 audio
worker. The image, the pull and the preflight were measured on arm64 with load1 at most 11 at the start and the
end of every row.

The OrbStack rows come from the chart with `values-local.yaml` only, so the cooldown, the stabilization window and
the concurrency under test are the production values. One render took the worker from 0 to 1. Three converts
against a maximum of 2 brought it to exactly 2 Ready replicas, each job ran once, and the Deployment went from 2
straight to 0 one cooldown after the last job, without waiting for the 900 s stabilization window. These were
measured on OrbStack's shared image store, where the image is already present and the pod logs no pull, and a
registry-backed cluster adds the cold pull above.

## Error codes

Every code below is also in [docs/ERROR-CODES.md](ERROR-CODES.md), which owns the full wording and the
consumer contract. `kind` says whether the code comes back from the request itself (`sync`, with its HTTP
status) or as the `error_code` of a failed job (`async`). Codes every class shares, such as `unauthorized`,
`rate_limited` or `not_found`, are only in ERROR-CODES.md.

| code | kind | when |
|---|---|---|
| `invalid_request` | sync 400 | CAD content under a non-CAD extension, compressed or binary-flag IGES, a pre-R2.5 DWG signature, or a binary STL under another CAD extension: the content is not recognised |
| `unsupported_conversion` | sync 422 | a pair the class does not serve, for example an STL `convert` or a DXF or DWG to PNG `convert` (use `render`) |
| `truncated_cad_file` | sync 422 | a STEP, IGES, DXF or STL whose structure ends early |
| `ambiguous_stl` | sync 422 | an STL that is valid both as binary and as ASCII |
| `binary_dxf_unsupported` | sync 422 | a binary DXF; re-export as ASCII DXF |
| `payload_too_large` | sync 413 | the file is above its format's ceiling (see Size ceilings) or above the deployment's transport cap; a DWG has no format ceiling; nothing is stored |
| `opts_not_applicable` | sync 422 | a well-formed option that does not apply to the operation and source format (see Option applicability) |
| `invalid_options` | sync 422 and async | an unknown option, a value outside its closed set or range, or a stored option that fails re-validation in the worker |
| `input_too_large` | async | the worker re-checks the format ceiling before any engine runs and the file is above it |
| `cad_too_complex` | async | the guard's entity or assembly caps; for DXF the expanded INSERT or entity caps, a block cycle or nesting deeper than the cap; the reader ran out of memory or was killed; a DWG read passed `CAD_DWG_TIMEOUT`; a render, drawing or `rsvg-convert` stage passed its deadline; or a file the pipeline drew from the drawing (the SVGs, `texts.json`, the PDF) is above its size or volume limit, including a DXF drawing's PDF above the PDF size limit or the inflate limit |
| `cad_unreadable` | async | the reader refuses the file's structure or values |
| `cad_empty_result` | async | no geometry to mesh; a drawing that shows nothing on a white page; or a PDF or PNG that shows no recognisable content |
| `cad_mesh_timeout` | async | tessellation did not finish within `CAD_MESH_TIMEOUT` |
| `engine_error` | async | the measurements could not be produced, the drawing's SVG failed the safety check, a render or drawing stage failed on every attempt, the requested paper layout is not in the file, or an unexpected engine failure |
| `reconciler_exhausted` | async | the job stopped making progress and the reconciler's recovery attempts ran out |

## Known limits

| id | limit | constant | value |
|---|---|---|---|
| R-1 | Text in the PNG and PDF is kerned by pango and can be up to 3.3% narrower than its extent in the measured bbox | — | — |
| R-9 | A character the resolved DejaVu face has no glyph for is drawn as U+FFFD in the PNG and PDF; the SVG keeps it exactly | — | — |
| R-10 | PDF text whose rendered size is below the floor may be missing from the PDF's text layer; the SVG keeps it | `CADDXFPDFTextFloorPt` | 0.00469174 |
| R-11 | A feature below one pixel can make a PNG preview `cad_empty_result`; a larger `size`, up to the maximum, helps | `CADMaxRenderSize` | 2048 |
| arm64_only | Every bound here and the DXF sandbox rulesets were measured on arm64 only; there is no amd64 measurement | — | — |
| text_oblique | A DXF TEXT's oblique angle is not drawn; its width factor is | — | — |
| font_family_style | A text style that asks for bold or italic by family name is drawn in that family's regular (Book) face | — | — |
| font_fallback | A text style naming a font file the image does not ship is drawn with `DejaVuSans.ttf` | — | — |
| near_white | Paint that, laid over a white page, has every channel at or above the cutoff is no ink; one channel below it is ink, so (249, 255, 255) is ink and (250, 250, 250) is not. The 3D PNG check and the DXF checks apply the same rule | `cadBackgroundCutoff` | 250 |
| empty_white_page | Empty means no ink on a white page of the maximum size, on every route; a transparent PNG is judged on the same white page, so a transparent PNG of only white lines is refused | `CADDXFInkSize` | 2048 |
| empty_result_causes | A drawing whose every element is fully transparent, white, too faint, of zero area or a glyphless character is `cad_empty_result` as SVG, PDF and PNG alike | — | — |
| iges_inch_unproven | IGES inch scaling is unproven: an IGES file that declares anything but millimetres is reported `unspecified` | — | — |
| stl_shell_overlap | Positive STL shells, and closed STEP or IGES solids, are summed without an overlap check, so intersecting bodies count their intersection twice | — | — |
| render_cost_not_bounded_by_input_bytes | The cost of a 3D render is not bounded by the input bytes: a heavy assembly of any 3D format may be refused as `cad_too_complex` at the render deadline far below its size ceiling. The smallest refused measured inputs were 598732 bytes (STEP), 32905602 bytes (IGES) and 614484 bytes (STL). Split the assembly or simplify it | `CADDefaultRenderTimeout` | 521 |
| stacked_render_refused | Geometry made of many coincident copies may be refused as `cad_too_complex` at the render deadline at any input size; split the assembly or remove the duplicates | `CADDefaultRenderTimeout` | 521 |
| iges_ceiling_lowered_by_memory | The IGES ceiling was lowered from 141557760 bytes to fit the mesh stage's memory in the pod | `CADMaxIGESBytes` | 125829120 |
| iges_instancing_unprobed | IGES subfigure instancing (entities 308 and 408) was not probed for render cost | — | — |
| quiet_host_bounds | The time limits were measured on a host with load1 at most 11; a loaded node runs slower | — | — |
| short_stages_in_io | Stages shorter than 0.2 s were not observed separately; their time is inside the measured I/O allowance | `CADIOAllowance` | 20 |
