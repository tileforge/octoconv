# CAD class

The `cad` engine converts and previews 3D models (STEP, IGES, STL) and 2D drawings (DXF). A client uploads a
file to `POST /v1/jobs` with a `target`, an optional `operation` (`convert` by default, or `render`) and
optional `opts`, and reads the result from `GET /v1/jobs/{id}`. This document is the client contract of the
class: what it accepts, what it returns, the limits it enforces and the codes it fails with. Tests in
`internal/convert` and `internal/worker` hold every table here equal to the code, in both directions.

## Outputs

| role | format | when |
|---|---|---|
| `primary` | the requested target: `stl` for a 3D `convert`, `svg` or `pdf` for a DXF `convert`, `png` for every `render` | every successful job |
| `measurements` | `json`, the document of docs/MEASUREMENTS.md | every successful `render`, never a `convert` |

The two outputs of a `render` are written together or not at all. The worker validates the measurements
document before it uploads anything, uploads the PNG and the JSON, and only then records both outputs in one
transaction and marks the job `done`. A render that fails at any step records neither, so a client never sees
a PNG without its measurements or the other way round.

## Error codes

Every code below is also in [docs/ERROR-CODES.md](ERROR-CODES.md), which owns the full wording and the
consumer contract. `kind` says whether the code comes back from the request itself (`sync`, with its HTTP
status) or as the `error_code` of a failed job (`async`). Codes every class shares, such as `unauthorized`,
`rate_limited` or `not_found`, are only in ERROR-CODES.md.

| code | kind | when |
|---|---|---|
| `invalid_request` | sync 400 | CAD content under a non-CAD extension, compressed or binary-flag IGES, a pre-R2.5 DWG signature, or a binary STL under another CAD extension: the content is not recognised |
| `unsupported_conversion` | sync 422 | a pair the class does not serve, for example an STL `convert` or a DXF to PNG `convert` (use `render`) |
| `truncated_cad_file` | sync 422 | a STEP, IGES, DXF or STL whose structure ends early |
| `ambiguous_stl` | sync 422 | an STL that is valid both as binary and as ASCII |
| `binary_dxf_unsupported` | sync 422 | a binary DXF; re-export as ASCII DXF |
| `dwg_unsupported` | sync 422 | a DWG file; re-export as DXF |
| `payload_too_large` | sync 413 | the file is above its format's ceiling (see Size ceilings) or above the deployment's transport cap; nothing is stored |
| `opts_not_applicable` | sync 422 | a well-formed option that does not apply to the operation and source format (see Option applicability) |
| `invalid_options` | sync 422 and async | an unknown option, a value outside its closed set or range, or a stored option that fails re-validation in the worker |
| `input_too_large` | async | the worker re-checks the format ceiling before any engine runs and the file is above it |
| `cad_too_complex` | async | the guard's entity or assembly caps; for DXF the expanded INSERT or entity caps, a block cycle or nesting deeper than the cap; the reader ran out of memory or was killed; a render, drawing or `rsvg-convert` stage passed its deadline; or a file the pipeline drew from the drawing (the SVGs, `texts.json`, the PDF) is above its size or volume limit, including a DXF drawing's PDF above the PDF size limit or the inflate limit |
| `cad_unreadable` | async | the reader refuses the file's structure or values |
| `cad_empty_result` | async | no geometry to mesh; a drawing that shows nothing on a white page; or a PDF or PNG that shows no recognisable content |
| `cad_mesh_timeout` | async | tessellation did not finish within `CAD_MESH_TIMEOUT` |
| `engine_error` | async | the measurements could not be produced, the drawing's SVG failed the safety check, a render or drawing stage failed on every attempt, or an unexpected engine failure |
| `reconciler_exhausted` | async | the job stopped making progress and the reconciler's recovery attempts ran out |
