# Measurements contract

The measurements document is the JSON a CAD job reports about the geometry it read: units, bounding box,
solid counts, volume and area. This file is the contract; `internal/measurements` is the validator that
enforces it, and a test holds the two equal.

## Schema

A document is identified by the exact string `octoconv.measurements/v1` in its `schema` field. A breaking
change (renaming or removing a field, changing a vocabulary code, changing what a value means) bumps the
identifier to `v2`. Adding an optional field does not bump it, but the validator is strict: unknown fields
are rejected, so an additive field lands with a code change and a change to this document together.

Render jobs emit this document as the output with role `measurements`. Every length, area and volume in it is
expressed in `unit`: the engine measures in millimetres, and the producer divides lengths by the factor the
engine applied, areas by its square and volumes by its cube, so a file declared in inches reports inches. A
declared unit is reported only when the factor the engine applied equals the table value for that unit;
otherwise `unit` is `unspecified` with source `none` and the numbers are in the file's own unit when the
factor is known, unchanged when it is not.

The factor is the table value (`mm` 1, `cm` 10, `m` 1000, `inch` 25.4, `foot` 304.8, `micrometre` 0.001)
for the unit the STEP text declares. The mesh helper pins `xstep.cascade.unit` to `MM` and OCP converts by
the file's declared unit; the phase-74 unit probe (`evidence/74-01-wave0.txt`) measured the ratio between OCP's
vertices and the file's `VERTEX_POINT` coordinates on five STEP files, and the declared unit matched it on all
of them. The helper does not measure that ratio at run time. Two gaps stand:

- IGES inch scaling is unproven: the corpus has no inch IGES, so an IGES file that declares anything but
  millimetres is reported `unspecified` with source `none`.
- `nist_ctc_05_asme1_ap242-e1.stp` declares inch in its text while OCP's own file-units call reports no
  unit for it; the measured ratio is exactly 25.4, so it is reported as inch, declared, on the strength of the
  text and the offline measurement alone.

## Fields

| Field | Type | Presence | Constraint |
|---|---|---|---|
| `schema` | string | always | exactly `octoconv.measurements/v1` |
| `format` | string | always | one of the Formats below |
| `unit` | string | always | one of the Units below |
| `unit_source` | string | always | one of the Unit sources below; `unspecified` pairs with `none` and only with `none` |
| `bbox` | object `{min, max}` | always | two arrays of 3 numbers (2 for `dxf`); every coordinate finite with absolute value at most 1e12; `min` not above `max` on any axis; for `step` and `iges` not a single point; with a positive `volume`, `min` below `max` on every axis |
| `solids` | integer | `step` and `iges` only, absent for `stl` and `dxf` | at least 0 |
| `closed_solids` | integer | `step` and `iges` only, absent for `stl` and `dxf` | at least 0 and at most `solids` |
| `volume` | number or null | key always present | null or finite and strictly greater than 0; null exactly when `area` is null |
| `volume_null_reason` | string | present exactly when `volume` is null | one of the Null reasons below, allowed for the format and consistent with the solid counts |
| `area` | number or null | key always present | null or finite and strictly greater than 0; null exactly when `volume` is null |
| `area_null_reason` | string | present exactly when `area` is null | one of the Null reasons below, allowed for the format and consistent with the solid counts; equal to `volume_null_reason` |

`volume` and `area` are never omitted: a missing key and an explicit null are different things, and only the
second carries a reason.

## Formats

| Format | Notes |
|---|---|
| `step` | 3D; solid counts present |
| `iges` | 3D; solid counts present |
| `stl` | 3D mesh; no solid counts; carries no units |
| `dxf` | 2D; two coordinates per bbox corner; volume and area are always null |

An STL is read as binary when its size is exactly 84 bytes plus 50 times the triangle count stored at byte 80; any other STL is read as ASCII, and its keywords (`solid`, `facet normal`, `outer loop`, `vertex`, `endloop`, `endfacet`, `endsolid`) match in any letter case. A vertex coordinate that does not fit a 32-bit float makes the file unreadable (`cad_unreadable`). The PNG is drawn from the same triangles that were measured.

An STL's volume is computed per shell, where a shell is the set of triangles joined through shared edges. When every shell encloses a positive volume, the volume is their sum and the area is the sum of all triangle areas. When every shell is inward, the reason is `negative_volume`. When the signs are mixed, volume and area are null with `stl_ambiguous_shells` instead of a difference of shells. This includes a hollow part whose cavity is modelled as an inward shell: it is reported as null, never as a wrong number. A shell of exactly zero volume makes the document `invalid_solid`.

**PNG check.** Before a PNG is returned, the worker checks that it shows the model, on the white page the DXF
route judges emptiness on (see Empty): a pixel is part of the picture when, laid over white, one of its
channels is below 250. That holds whatever `background` was asked for, so an opaque and a transparent PNG of
the same model get the same verdict, however little of the frame the model covers: a sparse assembly or a thin
part seen edge on passes on both. A 3D preview needs at least one such pixel, a long side of at least half the
frame, a margin of at least 3% of the frame on every side, and either a filled silhouette or a thin part with
dark edges along its length. A DXF preview needs ink that spans at least half its letterbox and stays inside
it. The only checks that look at the background are checks of the alpha channel: an opaque PNG may hold no
fully transparent pixel, and a transparent PNG must carry an alpha channel and leave its outer edge fully
transparent. The 3D renderer's anti-aliasing tints the background pixels next to the model's edges. An opaque
PNG shows them; a transparent PNG keeps them at alpha 0, so it has slightly fewer part pixels with the same
extent (in the corpus measured for this, up to 4% fewer for a filled model and 14% for a thin part at 256 px).
A PNG that fails the check is `cad_empty_result`.

For a 3D render with dimensions enabled (the default), this check runs on the model PNG before any
annotations. The validated image is then inset and annotated with the document's bounding-box lengths,
unit and unit source. Text or dimension lines cannot make an empty model pass the check. The measurements
JSON is unchanged by annotation; `dimensions: false` returns the checked model PNG directly.

## DXF drawings

A DXF document describes the same drawing as the picture. The `cad-dxf` helper draws model space only, with
visible layers only (a layer that is off or frozen is absent from both), and it skips XREF, IMAGE, UNDERLAY,
OLE, 3DSOLID, ACIS and proxy entities. Volume and area are always null with `not_applicable_2d`, and there are
no solid counts.

**Bounding box.** The bbox is the box of what is drawn: the drawn primitives plus the glyph box of every text.
It is never taken from the header's `$EXTMIN`/`$EXTMAX`, which a file can state wrongly, and it is not ezdxf's
`bbox.extents`, which counts dimension definition points that are never drawn. XLINE and RAY are infinite: they
are drawn clipped to the picture's frame and are excluded from the bbox. An isolated POINT is drawn and is part
of both the bbox and the frame, however far it is from the rest of the drawing: the frame shows exactly what
the file contains, and points are not filtered as strays.

**Transparency.** Transparency is paint, not geometry. A line, fill or text drawn with a transparency, whether
set on the entity, inherited from its layer (ByLayer) or from the block reference (ByBlock), is part of the bbox
and of the frame at any opacity, a fully transparent one included, which is how ezdxf's drawing backend
counts it. The SVG carries the opacity as `stroke-opacity` on lines and `fill-opacity` on fills and on `text`
(three decimals, absent when opaque; ezdxf rounds a 50% transparency to 0.498). The PDF and PNG are drawn from
that SVG: half-transparent text is drawn faint and is still required in the PDF's text layer, and fully
transparent text is not drawn, so the PDF check does not look for it. A drawing whose every element is fully
transparent has a frame but nothing visible and is refused with `cad_empty_result` as SVG, PDF and PNG alike,
before anything is rasterized; one visible element among fully transparent ones can be enough (see Empty), and
its frame and bbox still count the transparent ones. The PDF check counts a paint operation as ink only when its
stroke or fill alpha (`/CA`, `/ca`) is above 0, no soft mask covers it and its text rendering mode draws. A run
of MTEXT that sets its own colour (`\C` or `\c`) keeps the transparency of its MTEXT.

**Empty.** One definition decides whether a drawing shows anything, and the same check runs on the SVG, PDF and
PNG routes, so the three agree on it. After the two SVGs are validated, the render-only SVG is drawn by
`rsvg-convert`, under the same sandbox and with the same arguments as the PNG preview, into the letterbox of a
2048 x 2048 frame on an opaque white page, and the drawing judge's J1 looks for ink: a pixel with a channel
below 250. A frame without such a pixel means the drawing is empty, and it is refused with `cad_empty_result`
as SVG, PDF and PNG alike, whatever the PNG's `background`. That covers a drawing whose every line, fill and
text is fully transparent, true colour white (or ACI 255, which is white too), too faint to darken the page
(an opacity of about 2% or less, such as an alpha of 1 of 255), of zero area (a SOLID with collinear corners, a
HATCH whose boundary encloses nothing) or a character with no glyph to draw (U+200B). The page is white on every
route. The PDF is drawn on an explicit white page. The SVG declares its page colour as `#ffffff` (a background
rectangle left at opacity 0), and ezdxf picks every colour for that white page, drawing ACI 7 black. The opaque
PNG is that white page. A PNG with a transparent background is judged on the same white page, so a drawing of
only white lines is empty there too, although its strokes would show on a dark viewer: emptiness belongs to the
drawing, not to the background a client asks for. The check frame is 2048 px, the largest `size` a render
accepts, because a thin line or a small glyph covers more of a pixel in a larger frame; a smaller PNG preview
can still lose a feature, see Known limit: features below one pixel. Content that stays fainter than the page
threshold even there, such as a text a hundredth of a pixel high beside a fully transparent line that sets a
large frame, counts as nothing, although an SVG or PDF viewer could zoom in on it. Two cheaper checks refuse
first and can only refuse what the ink check would: the helper and the Go validator count the elements whose
opacity is above 0, and a drawing with none is refused before anything is rasterized. The check is one more
`rsvg-convert` run in every DXF job, bounded by `CAD_RSVG_TIMEOUT` like the PDF and PNG runs.

**Fonts.** Text is measured with the TrueType face ezdxf resolves for its text style, and the SVG names that
same face, so the PDF and PNG are drawn with the face that sized the frame: `font-family` is the font's family
name, `font-weight` its weight class (`bold` for 700, the number for any other weight, absent at 400),
`font-style` is `italic` or `oblique` for a slanted face and `font-stretch` names a condensed or expanded one.
The image ships the six faces of `fonts-dejavu-core`: DejaVu Sans, DejaVu Sans Mono and DejaVu Serif, each Book
and Bold. A style that names a file the image does not have (an oblique DejaVu face, Arial, a `.shx` font) is
measured and drawn with `DejaVuSans.ttf`. A style that asks for a family by name with bold or italic (extended
font data, or an MTEXT `\f` code) gets that family's Book face from ezdxf, so its text is upright and regular.
A TEXT's oblique angle is not applied by ezdxf; its width factor is.

**Units.** Values are in drawing units, unscaled: the millimetre conversion described under Schema does not
apply to DXF, the engine applies no factor, and the document reports the unit the drawing declares. The unit
comes from the `$INSUNITS` header:

| `$INSUNITS` | `unit` | `unit_source` |
|---|---|---|
| 1 | `inch` | `declared` |
| 2 | `foot` | `declared` |
| 4 | `mm` | `declared` |
| 5 | `cm` | `declared` |
| 6 | `m` | `declared` |
| 13 | `micrometre` | `declared` |
| 0 or missing | `unspecified`, or the client's `assume_unit` | `none`, or `client_assumed` |
| any other code | `unspecified` | `none`; `assume_unit` does not apply |

A code outside this table (yards, miles, angstroms, survey units and the rest) names a unit the vocabulary has
no word for. It is reported `unspecified`, and `assume_unit` does not fill it, because the file did state a
unit and the client's guess would contradict it.

**Known limit: kerning.** The text in the PNG and PDF is laid out by pango, which kerns; the bbox uses ezdxf's
text layout, which does not. A rendered line of text can therefore be up to 3.3% narrower than its extent in
the bbox, with the same start point and height.

**Known limit: characters outside the font.** A character the resolved DejaVu face has no glyph for is drawn as
U+FFFD in the PNG and PDF. The SVG keeps it exactly.

**Known limit: microscopic text.** Text whose rendered size on the PDF page is below 0.00469174 pt may be
missing from the PDF's text layer; the PDF check does not require it. The SVG keeps it.

**Known limit: features below one pixel.** A PNG preview in which part of the drawing falls below one pixel at
the requested `size` can be refused with `cad_empty_result`. A larger `size` helps. SVG and PDF are refused
only when nothing shows in the 2048 px check frame (see Empty), which is also the largest preview.

## Units

| Unit | Seen on the phase-71 corpus | Notes |
|---|---|---|
| `mm` | yes | STEP `SI:MILLI_METRE`, IGES `MM`, DXF `$INSUNITS` 4 |
| `cm` | no | in the vocabulary, never observed; DXF `$INSUNITS` 5 |
| `m` | yes | DXF `$INSUNITS` 6 |
| `inch` | yes | STEP `CBU:INCH`, DXF `$INSUNITS` 1 |
| `foot` | no | in the vocabulary, never observed; DXF `$INSUNITS` 2 |
| `micrometre` | no | in the vocabulary, never observed; DXF `$INSUNITS` 13 |
| `unspecified` | yes | every STL; a DXF whose `$INSUNITS` is 0, missing or a code outside the DXF drawings table; pairs with unit source `none` |

The draft vocabulary of phase 71 had a unit word for "not recognised". It is not a value: `unspecified`
replaces it, and it means the file states no unit, not that the reader failed to understand one.

## Unit sources

| Source | Meaning |
|---|---|
| `declared` | the unit comes from the file: STEP or IGES text, or the DXF `$INSUNITS` header |
| `client_assumed` | the client supplied the unit because the file had none; fills only a missing unit, never overrides a declared one |
| `none` | no unit is known; pairs with `unspecified` |

An STL never has source `declared`: the format carries no units.

## Null reasons

| Reason | Formats | Solid counts | Observed on the phase-71 corpus |
|---|---|---|---|
| `no_solids` | `step`, `iges` | `solids` = 0 | observed: `surfaces_only.iges`, `iges_no_newlines.iges`, `open_shell.step`, `single_face.step` |
| `open_shell` | `step`, `iges` | `solids` > 0 and `closed_solids` = 0 | not observed on corpus; OCP repairs orientation / reads open shell as 0 solids |
| `invalid_solid` | `step`, `iges`, `stl` | `solids` > 0 and `closed_solids` = 0 | not observed on corpus; for `stl` a watertight mesh whose total volume, or the volume of any one shell, is exactly 0 or not finite |
| `negative_volume` | `step`, `iges`, `stl` | `closed_solids` > 0 | not observed on corpus; for `step` and `iges` some closed solid has a negative volume (OCP's STEP reader repairs a reversed solid's orientation on read); for `stl` every shell has a negative volume |
| `stl_not_watertight` | `stl` | none | observed: two NASA STL files |
| `stl_ambiguous_shells` | `stl` | none | not observed on corpus; a watertight STL whose shells (triangles joined through shared edges) enclose volumes of both signs, such as a hollow part whose cavity is an inward shell or a disjoint inverted body |
| `not_applicable_2d` | `dxf` | none | every DXF volume and area |

The Solid counts column is the rule a null reason must satisfy against the document's own `solids` and
`closed_solids`; it applies to `step` and `iges`, the only formats that carry the counts, and `none` means no
rule. The rule is `cad_mesh.py`'s reason logic read back: it names a reason only when `closed_solids` is 0
(`no_solids` when `solids` is 0, otherwise `open_shell` when any solid has an open shell, otherwise
`invalid_solid`) and `negative_volume` only when a closed solid exists and any closed solid has a negative
volume; otherwise it reports the sum over the closed solids. When some solids are closed and others are not,
the helper reports the sum over the closed ones and no reason; a null volume with `closed_solids` above 0 and a
reason other than `negative_volume` is therefore a contradiction, not a partial result. `internal/measurements/doc_sync_test.go` probes
`Document.Validate` over every format and every pair of counts up to 3 and compares the outcome with this table.

## Validation rules

The validator fails closed: anything it cannot positively accept is rejected, and every rejection wraps one
error so a caller can tell a contract violation from an I/O error.

- The document is one JSON object, with nothing after it. Unknown fields are rejected at every level.
- `schema` equals the schema string exactly. `format`, `unit`, `unit_source` and every reason are in their
  vocabularies; there is no pass-through for a value the code does not know.
- `unspecified` pairs with `none` and only with `none`. `stl` is never `declared`.
- Every bbox coordinate is finite and at most 1e12 in absolute value, and `min` is not above `max`. The bound
  is the one the phase-71 helper (`scripts/cad-measure/cad_mesh.py`, `FINITE_LIMIT`) already applies: it sits
  eight orders above the largest coordinate measured on the corpus and far below the plus or minus 1e100 box
  OCP's bounding-box call returned for one real file, so a sentinel box is rejected, not reported.
- `volume` and `area` are null or strictly positive and finite. Zero is rejected outright: phase 71 never
  produced a legitimate zero, and a zero beside a successful read hides a failed one. NaN, infinities and
  numeric overflow such as `1e999` are rejected by the decoder, and again on a struct built in code.
- For `step` and `iges`, a positive `volume` or `area` needs at least one closed solid, and `closed_solids`
  never exceeds `solids`.
- A number and a reason never appear together; a null without a reason is rejected.
- Reasons are per format: `step` and `iges` allow `no_solids`, `open_shell`, `invalid_solid`,
  `negative_volume`; `stl` allows `stl_not_watertight`, `stl_ambiguous_shells`, `invalid_solid`,
  `negative_volume`; `dxf` allows only `not_applicable_2d`, and its volume and area must both be null with it.
- For `step` and `iges`, a null reason must fit the solid counts as the Solid counts column of the Null reasons
  table says. A document such as `solids` 1, `closed_solids` 1, `volume` null with `no_solids` is rejected: the
  reason says the file holds no solid and the counts say it holds one.
- `volume` and `area` are null together or numbers together, because the producer derives both from the same
  closed solids and a null on one side has no reason that is true while the other side holds a number. When both
  are null their reasons are equal.
- A positive `volume` needs a bbox with extent on every axis, and a `step` or `iges` bbox is never a single
  point (the helper refuses a zero-size box instead of reporting one).
- `solids` and `closed_solids` are present for `step` and `iges` and absent for `stl` and `dxf`.

## Keeping this document honest

`internal/measurements/doc_sync_test.go` parses the tables above and compares them with the closed
vocabularies and the document's field set in both directions, checks that this file names the schema
string, and runs the Null reasons table against `Document.Validate` over every format and every pair of solid
counts up to 3, so a rule changed on either side fails. `consistency_test.go` feeds every `step`, `iges`, `stl`
and `dxf` combination the phase-71 helpers actually produced (`testdata/helpers-71-04-rows.txt`, the rc 0 rows
of `71-04-helpers.txt`) through the validator, so a rule that would reject real output fails. A code in the document the Go code does not declare is a promise of something that does not happen;
a code Go declares that the document omits is silence about something that does. When the test fails, correct
whichever side is wrong; never relax the test.
