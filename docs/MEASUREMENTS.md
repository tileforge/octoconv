# Measurements contract

The measurements document is the JSON a CAD job reports about the geometry it read: units, bounding box,
solid counts, volume and area. This file is the contract; `internal/measurements` is the validator that
enforces it, and a test holds the two equal.

## Schema

A document is identified by the exact string `octoconv.measurements/v1` in its `schema` field. A breaking
change (renaming or removing a field, changing a vocabulary code, changing what a value means) bumps the
identifier to `v2`. Adding an optional field does not bump it, but the validator is strict: unknown fields
are rejected, so an additive field lands with a code change and a change to this document together.

No engine emits this document yet. Phase 74 writes it as the output with role `measurements` of a render
job; until then the contract and its validator exist so the producer is written against a fixed, tested
target.

## Fields

| Field | Type | Presence | Constraint |
|---|---|---|---|
| `schema` | string | always | exactly `octoconv.measurements/v1` |
| `format` | string | always | one of the Formats below |
| `unit` | string | always | one of the Units below |
| `unit_source` | string | always | one of the Unit sources below; `unspecified` pairs with `none` and only with `none` |
| `bbox` | object `{min, max}` | always | two arrays of 3 numbers (2 for `dxf`); every coordinate finite with absolute value at most 1e12; `min` not above `max` on any axis |
| `solids` | integer | `step` and `iges` only, absent for `stl` and `dxf` | at least 0 |
| `closed_solids` | integer | `step` and `iges` only, absent for `stl` and `dxf` | at least 0 and at most `solids` |
| `volume` | number or null | key always present | null or finite and strictly greater than 0 |
| `volume_null_reason` | string | present exactly when `volume` is null | one of the Null reasons below, allowed for the format |
| `area` | number or null | key always present | null or finite and strictly greater than 0 |
| `area_null_reason` | string | present exactly when `area` is null | one of the Null reasons below, allowed for the format |

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

| Reason | Formats | Observed on the phase-71 corpus |
|---|---|---|
| `no_solids` | `step`, `iges` | observed: `surfaces_only.iges`, `iges_no_newlines.iges`, `open_shell.step`, `single_face.step` |
| `open_shell` | `step`, `iges` | not observed on corpus; OCP repairs orientation / reads open shell as 0 solids |
| `invalid_solid` | `step`, `iges` | not observed on corpus; OCP repairs orientation / reads open shell as 0 solids |
| `negative_volume` | `step`, `iges`, `stl` | not observed on corpus; OCP repairs orientation / reads open shell as 0 solids |
| `stl_not_watertight` | `stl` | observed: two NASA STL files |
| `not_applicable_2d` | `dxf` | every DXF volume and area |

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
- For `step` and `iges`, a positive `volume` or `area` needs at least one closed solid.
- A number and a reason never appear together; a null without a reason is rejected.
- Reasons are per format: `step` and `iges` allow `no_solids`, `open_shell`, `invalid_solid`,
  `negative_volume`; `stl` allows `stl_not_watertight`, `negative_volume`; `dxf` allows only
  `not_applicable_2d`, and its volume and area must both be null with it.
- `solids` and `closed_solids` are present for `step` and `iges` and absent for `stl` and `dxf`.

## Keeping this document honest

`internal/measurements/doc_sync_test.go` parses the tables above and compares them with the closed
vocabularies and the document's field set in both directions, and checks that this file names the schema
string. A code in the document the Go code does not declare is a promise of something that does not happen;
a code Go declares that the document omits is silence about something that does. When the test fails, correct
whichever side is wrong; never relax the test.
