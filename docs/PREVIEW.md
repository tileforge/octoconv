# Preview contract

A preview is a picture of a file that a client can show in a listing or a UI. The contract is meant to hold for
every class that will produce previews, but only the `cad` class produces them today. A later class uses the
`render` operation of its own worker, with its own capability and its own worker identity. There is no separate
preview engine. What CAD did not decide, apart from that shape, stays under Not fixed.
`TestPreviewDocMatchesTheCode` in `internal/convert` holds the fixed values equal to the code.

## Fixed by CAD

| property | value | source |
|---|---|---|
| operation | `render` | a measurements preview is a `render` job. A DWG `convert` to png writes the square PNG and no measurements document |
| output | `png`, role `primary` | the PNG is the job's primary output |
| size | `256..2048`, default `1280` | `size` is the edge of a square frame in pixels |
| background | `opaque` (white #FFFFFF, the default) or `transparent` (alpha 0) | `background` |
| 3D framing | orthographic, the model's bounding box fitted with a margin of `0.06` of the frame on every side, centred. Studio lighting shows surface curvature. A sheet seen edge-on gets a thin profile in the surface color when it projects to less than one pixel | the 3D renderer |
| DXF framing | top-down, letterboxed with a margin of at least `0.03` of the frame, centred, never cropped or stretched | the DXF letterbox |
| 3D views | `iso` (the default), `front`, `back`, `left`, `right`, `top`, `bottom` | `view` |
| DXF views | `top` only; any `view` on a DXF render is refused with 422 `opts_not_applicable` | `view` |
| measurements | the worker computes the measurements together with the PNG. The optional `measurements` output requires its own upload link | docs/MEASUREMENTS.md |
| dimensions | enabled by default for 3D renders; `dimensions: false` returns the unannotated PNG | `dimensions` |
| pixel judge | the PNG is checked to show the model on a white page before anything is uploaded | docs/MEASUREMENTS.md, PNG check |

One material, color, and finish apply to the whole 3D model. See [CAD options](CAD.md#render-options) for values and defaults.

The 3D framing and pixel judge above describe the model image before annotation. With dimensions enabled,
the validated model image is uniformly reduced into an inset square occupying 70% of the output width,
starting at 14% from the left and 16% from the top (rounded to pixels). The heading, extension lines,
arrows and labels are composed around it; output size and background remain as requested. Labels use
the embedded Go Regular font. The values and unit provenance match the measurements JSON. Dimensions
along an axis hidden by the view, or spanning less than one pixel, appear only in the heading.
A STEP or IGES document that includes `bodies` also gets those lines for up to 12 solid boxes.

## Non-previewable input

No placeholder or substitute image is ever returned. Input that cannot be previewed fails the job with a code,
or is refused at create, and the job has no output.

| input | outcome |
|---|---|
| a drawing or model that shows nothing: empty, invisible, white or too faint | the job fails with `cad_empty_result` |
| a file the reader cannot read | the job fails with `cad_unreadable` |
| a model or drawing too complex to render within the limits | the job fails with `cad_too_complex` |
| a format with no preview, or a pair the class does not serve | refused at create with 422 `unsupported_conversion`; the job is not created |

## Later classes

A class that produces a preview uses `render` on its own worker. The capability and the worker identity belong
to that class. There is no separate preview engine.

The classes that can produce a picture of the file are `document`, `html`, `image`, `av`, `ocr`, `pandoc`, and
`tectonic`. `audio` has no page or frame. `probe` sanitizes a file and does not produce a picture. None of
those classes produces a preview today.

A later class takes these rules from CAD: the `render` operation, a primary PNG, a square size, and a refusal
with no placeholder image. 3D views, measurements, and layout stay with CAD. Page or frame selection, JPEG or
WebP, and the other items under Not fixed are decided by the class that needs them.

## Not fixed

These are open questions for the next class that produces previews. CAD did not have to answer them, so this
contract does not:

- Non-square sizes.
- JPEG or WebP output.
- Page or frame selection for multi-page or time-based input.
- Colour profile and gamma.
- DPI metadata.
- Sizes below 256.
- Cross-arch determinism: the same input on arm64 and amd64 is not promised to give the same pixels.
- Font fallback outside DejaVu.
- Caching and re-use of previews.
- The output roles `preview` and `thumbnail`, which the schema reserves and no engine writes.
