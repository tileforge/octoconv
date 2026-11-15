# Preview contract

A preview is a picture of a file that a client can show in a listing or a UI. The contract is meant to hold for
every class that will produce previews, but only the `cad` class produces them today, so this document fixes
only what CAD forced a decision on. Everything CAD did not decide is listed under Not fixed and stays open for
the next class. `TestPreviewDocMatchesTheCode` in `internal/convert` holds the fixed values equal to the code.

## Fixed by CAD

| property | value | source |
|---|---|---|
| operation | `render` | a measurements preview is a `render` job. A DWG `convert` to png writes the square PNG and no measurements document |
| output | `png`, role `primary` | the PNG is the job's primary output |
| size | `256..2048`, default `1280` | `size` is the edge of a square frame in pixels |
| background | `opaque` (white #FFFFFF, the default) or `transparent` (alpha 0) | `background` |
| 3D framing | orthographic, the model's bounding box fitted with a margin of `0.06` of the frame on every side, centred | the 3D renderer |
| DXF framing | top-down, letterboxed with a margin of at least `0.03` of the frame, centred, never cropped or stretched | the DXF letterbox |
| 3D views | `iso` (the default), `front`, `back`, `left`, `right`, `top`, `bottom` | `view` |
| DXF views | `top` only; any `view` on a DXF render is refused with 422 `opts_not_applicable` | `view` |
| measurements | a second output with role `measurements`, written together with the PNG or not at all | docs/MEASUREMENTS.md |
| dimensions | enabled by default for 3D renders; `dimensions: false` returns the original unannotated PNG | `dimensions` |
| pixel judge | the PNG is checked to show the model on a white page before anything is uploaded | docs/MEASUREMENTS.md, PNG check |

The full option table and where each option applies are in docs/CAD.md.

The 3D framing and pixel judge above describe the model image before annotation. With dimensions enabled,
the validated model image is uniformly reduced into an inset square occupying 70% of the output width,
starting at 14% from the left and 16% from the top (rounded to pixels). The heading, extension lines,
arrows and labels are composed around it; output size and background remain as requested. Labels use
the embedded Go Regular font. The values and unit provenance match the measurements JSON. Dimensions
along an axis hidden by the view, or spanning less than one pixel, appear only in the heading.

## Non-previewable input

No placeholder or substitute image is ever returned. Input that cannot be previewed fails the job with a code,
or is refused at create, and the job has no output.

| input | outcome |
|---|---|
| a drawing or model that shows nothing: empty, invisible, white or too faint | the job fails with `cad_empty_result` |
| a file the reader cannot read | the job fails with `cad_unreadable` |
| a model or drawing too complex to render within the limits | the job fails with `cad_too_complex` |
| a format with no preview, or a pair the class does not serve | refused at create with 422 `unsupported_conversion`; the job is not created |

## Not fixed

These are open questions for the next class that produces previews. CAD did not have to answer them, so this
contract does not:

- Whether other classes use the `render` operation of their own engine or a separate preview engine.
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
