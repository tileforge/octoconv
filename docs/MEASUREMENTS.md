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

## Units

| Unit | Seen on the phase-71 corpus | Notes |
|---|---|---|
| `mm` | yes | STEP `SI:MILLI_METRE`, IGES `MM`, DXF `$INSUNITS` 4 |
| `cm` | no | in the vocabulary, never observed |
| `m` | yes | DXF `$INSUNITS` 6 |
| `inch` | yes | STEP `CBU:INCH`, DXF `$INSUNITS` 1 |
| `foot` | no | in the vocabulary, never observed |
| `micrometre` | no | in the vocabulary, never observed |
| `unspecified` | yes | every STL; a DXF without `$INSUNITS`; pairs with unit source `none` |

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
| `invalid_solid` | `step`, `iges`, `stl` | `solids` > 0 and `closed_solids` = 0 | not observed on corpus; for `stl` a watertight mesh whose volume is exactly 0 or not finite |
| `negative_volume` | `step`, `iges`, `stl` | `closed_solids` > 0 | not observed on corpus; OCP repairs orientation / reads open shell as 0 solids |
| `stl_not_watertight` | `stl` | none | observed: two NASA STL files |
| `not_applicable_2d` | `dxf` | none | every DXF volume and area |

The Solid counts column is the rule a null reason must satisfy against the document's own `solids` and
`closed_solids`; it applies to `step` and `iges`, the only formats that carry the counts, and `none` means no
rule. The rule is `cad_mesh.py`'s reason logic read back: it names a reason only when `closed_solids` is 0
(`no_solids` when `solids` is 0, otherwise `open_shell` when any solid has an open shell, otherwise
`invalid_solid`) and `negative_volume` only when a closed solid exists, because the volume it reports is the sum
over the closed solids. When some solids are closed and others are not, the helper reports the sum over the
closed ones and no reason; a null volume with `closed_solids` above 0 and a reason other than `negative_volume`
is therefore a contradiction, not a partial result. `internal/measurements/doc_sync_test.go` probes
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
  `negative_volume`; `stl` allows `stl_not_watertight`, `invalid_solid`, `negative_volume`; `dxf` allows only
  `not_applicable_2d`, and its volume and area must both be null with it.
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
