---
phase: 72-engine-free-contract-foundations
plan: 02
subsystem: api
tags: [measurements, json-contract, validation, doc-sync, go]
requires:
  - phase: 71-evaluation-gate
    provides: the vocabulary draft, the observed units and null reasons, and the FINITE_LIMIT bound in cad_mesh.py
provides:
  - internal/measurements, the versioned measurements document (schema octoconv.measurements/v1), closed vocabularies and a fail-closed validator
  - docs/MEASUREMENTS.md, the contract document, held equal to the code by a doc-sync test
affects: [72-04, 74]
status: complete
commits: 2
plan_head_before: 018fca39faf4791cd37f411a47fe4ce92f9fdbd2
plan_head_after: b86fbf8d4f3efa57be289a38f40f713d9b46df27
actuals:
  tokens: 8000
  tasks: 3
  commits: 2
tech-stack:
  added: []
  patterns:
    - "closed string vocabularies as untyped consts with an unexported slice and an All*() that returns a copy"
    - "two-pass decode: map[string]json.RawMessage for key presence, then a DisallowUnknownFields decode into the struct"
    - "doc-to-code sync test with first-cell token extraction, both directions, no vacuous pass"
key-files:
  created:
    - internal/measurements/measurements.go
    - internal/measurements/measurements_test.go
    - internal/measurements/doc_sync_test.go
    - docs/MEASUREMENTS.md
  modified:
    - README.md
    - docs/README.md
key-decisions:
  - "D-11 wire identifier: string-schema-v1 (auto-selected under user delegation, 2026-10-04)"
  - "A reason key that is present but empty is rejected, so a document cannot carry an empty reason beside a number"
  - "solids and closed_solids are rejected on stl and dxf in Document.Validate as well as in Validate(raw), so a struct built in code obeys the same rule as decoded JSON"
requirements-completed: [MEAS-05]
---

# Phase 72 Plan 02: Measurements contract Summary

`internal/measurements` defines the `octoconv.measurements/v1` document, its closed unit, unit-source, null-reason and format vocabularies and a validator that fails closed, and `docs/MEASUREMENTS.md` is held equal to all of it by a test that was proven to fire.

## Accomplishments

- `measurements.go`: `Schema`, `MaxAbsCoordinate` (1e12), `ErrInvalid`, the `Unit*`, `Source*`, `Reason*` and `Format*` constants, `AllUnits` (7), `AllUnitSources` (3), `AllNullReasons` (6), `AllFormats` (4), each returning a copy, `BBox`, `Document`, `Validate(raw)` and `Document.Validate()`. Every rejection wraps `ErrInvalid` and names the field.
- `Validate(raw)`: one JSON object with no trailing data, unknown fields rejected at every level, `schema`, `format`, `unit`, `unit_source`, `bbox`, `volume` and `area` keys required (null is explicit), solid counts required for step and iges and forbidden for stl and dxf.
- `Document.Validate()`: schema equality, vocabulary membership, `unspecified` pairs with `none` and only with `none`, stl never `declared`, bbox arity (2 for dxf, 3 otherwise), finite coordinates with |v| <= 1e12 and min <= max, solid counts, volume and area null-iff-reason, strictly positive and finite when present (0 rejected outright), a positive value needs `closed_solids >= 1` on step and iges, per-format reason subsets, dxf volume and area always null with `not_applicable_2d`.
- Tests: 6 valid shapes, 53 single-mutation rejections, 8 raw non-finite literals (bare `NaN`, `Infinity`, `-Infinity`, `1e999` in volume, area and bbox), 7 structural rejections (empty, non-object, trailing data, truncated), NaN/+Inf/-Inf on a struct built in code, exact vocabulary members, copy safety, and the inclusive 1e12 bound (exactly 1e12 passes, 1.0000001e12 does not). Assumption A8 is confirmed: Go's decoder rejects bare `NaN`, `Infinity` and `1e999`.
- `docs/MEASUREMENTS.md`: sections Schema, Fields, Formats, Units (with the phase-71 "seen" column), Unit sources, Null reasons (with the exact "not observed on corpus; OCP repairs orientation / reads open shell as 0 solids" note for the three unobserved codes), Validation rules, Keeping this document honest.
- `doc_sync_test.go`: `TestMeasurementsDocMatchesVocabularies` (four tables, subtests), `TestMeasurementsDocMatchesFields` (Fields table against `Document`'s json tags via reflect), `TestMeasurementsDocNamesSchema`.
- README.md Documentation table row and a paragraph in docs/README.md. The README engine-class table is untouched.

## Verification

- `GOTOOLCHAIN=go1.26.4 go build ./...`, `go vet ./internal/measurements/`, `go test ./internal/measurements/ ./internal/convert/ -count=1`: green (convert includes TestREADME).
- `grep -c '"octoconv.measurements/v1"' measurements.go` = 1, `DisallowUnknownFields` = 1, `"unknown"` = 0, no `//` lines in any of the three Go files; `grep -c 'docs/MEASUREMENTS.md' README.md` = 1, `grep -c 'MEASUREMENTS.md' docs/README.md` = 1.
- Mutation gate: renaming the `micrometre` row to `micron` in the doc made `TestMeasurementsDocMatchesVocabularies` fail (`DOC-SYNC-FIRES`); renaming `area_null_reason` to `area_why` made `TestMeasurementsDocMatchesFields` fail naming both sides. The file was restored byte-identically (`cmp`) after each probe. The plan's `git diff --quiet -- docs/MEASUREMENTS.md` form is vacuous here because the file was untracked at probe time, so `cmp` against a saved copy was used instead.
- Both commits are SSH-signed (`G`).

## Commits

- `954ad5a` feat(72-02): add the versioned measurements document and its fail-closed validator
- `b86fbf8` docs(72-02): publish the measurements contract and hold it equal to the code by test

## Deviations from Plan

None - plan executed as written. Notes that are not deviations:

- Tests and implementation for Task 2 were written in one pass and run together, so a red run of the tests against a missing implementation was not observed separately; the mutation probes in Task 3 are the evidence that the doc-sync half fires, and each rejection case in Task 2 names the field it expects.
- `Document.Validate` also rejects `solids` and `closed_solids` on stl and dxf (the plan states it for `Validate(raw)` only).

## Auto-selected decisions

- Task 1 (D-11, wire identifier): auto-selected `string-schema-v1` under user delegation (2026-10-04). Task 2 implements exactly `"schema": "octoconv.measurements/v1"`.

## Known Stubs

None.

## Threat Flags

None. The package is pure and reads nothing; the enforcement point for T-72-05 and T-72-06 is phase 74, where the producer calls `Validate` before storing the artifact.

## Self-Check: PASSED

- FOUND: internal/measurements/measurements.go, measurements_test.go, doc_sync_test.go, docs/MEASUREMENTS.md
- FOUND commits 954ad5a and b86fbf8 as ancestors of HEAD
