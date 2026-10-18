---
phase: 72-engine-free-contract-foundations
plan: 04
subsystem: api
tags: [cad, error-codes, content-sniffing, step, iges, dxf, stl, dwg, go]
requires:
  - phase: 72-engine-free-contract-foundations
    provides: SniffCADWindows tracer, ReadCADWindows and the committed reject fixtures (72-01)
provides:
  - errcode.TruncatedCADFile, AmbiguousSTL, BinaryDXFUnsupported, DWGUnsupported (twenty-one sync codes)
  - docs/ERROR-CODES.md rows and CAD content check scope; openapi Error.code enum entries
  - convert.CADRejectTruncated, CADRejectAmbiguousSTL, CADRejectBinaryDXF, CADRejectDWG, AllCADRejections
  - the full DWG, binary DXF, STEP, IGES, DXF and STL rule set in SniffCADWindows
affects: [72-05, 72-06, 72-07, 73]
status: complete
commits: 2
plan_head_before: 65cd7810cb5bb703de8ebbfcb3f437d9f109893d
plan_head_after: 1612962da3124246300d2fe368dd5dd976e9c1f0
actuals:
  tokens: 5600
  tasks: 3
  commits: 2
tech-stack:
  added: []
  patterns:
    - "rejection as a typed value from internal/convert, wire-code mapping deferred to internal/api (no errcode import)"
    - "STL size formula takes precedence over every magic rule for a declared stl"
key-files:
  created:
    - internal/convert/cadsniff_test.go
  modified:
    - internal/errcode/errcode.go
    - docs/ERROR-CODES.md
    - docs/openapi.yaml
    - internal/convert/cadsniff.go
key-decisions:
  - "D-05 wire codes: four-per-class (auto-selected under user delegation, 2026-10-04)"
  - "ASCII STL endsolid line must not contain a NUL byte, so NUL padding stays truncated even without a final newline"
requirements-completed: [CADIN-02, CADIN-03]
---

# Phase 72 Plan 04: CAD rejection codes and rule set Summary

Four stable 422 codes (`truncated_cad_file`, `ambiguous_stl`, `binary_dxf_unsupported`, `dwg_unsupported`) are declared, documented in both contracts and backed by a complete content rule set that classifies the whole committed corpus with no false rejection and rejects every committed reject fixture with the right class. Endpoint wiring is still plan 72-06.

## Decision checkpoint

Task 1 (`four-per-class` vs `per-format-truncation`): `four-per-class`, auto-selected under user delegation (2026-10-04). Task 2 implements exactly those four strings.

## Accomplishments

- `errcode`: four constants and `all` entries before `InternalError`; the package comment now reads "closed at twenty-one codes".
- `docs/ERROR-CODES.md`: "Twenty-one codes", four 422 rows after `unsupported_package`, and a "CAD content checks" paragraph listing what falls through as unrecognized (compressed or binary-flag IGES, dotted pre-R2.5 DWG signatures, a binary STL under another CAD extension, CAD content under a non-CAD extension) and the `unsupported_conversion` answer while no CAD engine is registered. `docs/openapi.yaml` Error.code enum carries all four. The existing doc and openapi sync tests hold them equal to the Go constants.
- `SniffCADWindows` order: binary-STL precedence for a declared stl, DWG (`AC10` plus two digits), binary DXF sentinel, STEP (missing `HEADER;` or `END-ISO-10303-21;` is truncated), IGES (stride from the head, record count from size arithmetic, last record `T` with S/G/D/P counts), DXF (999 pairs skipped, `0`/`SECTION` head, `0`/`EOF` tail), then STL only when declared stl (ambiguous, accepted, truncated or no verdict). No BOM stripping, no NUL tolerance, strict binary STL size equality.
- `internal/convert` does not depend on `internal/errcode`.

## Verification

- `go build ./...`, `go vet ./...` and `go test ./internal/convert/ ./internal/errcode/ ./internal/inspect/ ./internal/api/ -count=1` green on go1.26.4 (internal/api includes `TestCADTracerSTEPBothEndpoints`, the Pre59 pins and the error-code source tests).
- Task 2 gate printed `CODES-OK` (21 rows in the synchronous table).
- Eight `TestSniffCAD*` top-level tests pass: corpus, reject fixtures (two-way against the directory listing), mutations, garbage, binary-STL header precedence, DWG, bounded reads, `AllCADRejections`.
- Mutation checks on the rules: removing the STL precedence fails `stl_binary_ac10_header.stl` and every `TestSniffCADBinarySTLHeaderPrecedence` case; removing the NUL clause fails the new "trailing nul on the endsolid line" case.
- Acceptance greps: no comment lines in either file, zero BOM byte references in `cadsniff.go`, `internal/inspect/findings.go` and `internal/api/testdata` unchanged since d034437, reject fixtures commit 6460068 is an ancestor of the rule commit (D-10).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] ASCII STL with NUL padding and no final newline would have been accepted**
- **Found during:** Task 3 (comparing the research skeleton with the behavior list)
- **Issue:** the skeleton's `asciiTail` accepts a last line `endsolid box` followed by NUL bytes, while the plan requires NUL padding on ASCII STL to be truncated. A file that ends with a newline is caught anyway because the NUL then forms its own last line.
- **Fix:** `asciiTail` additionally requires the last line to contain no NUL byte; added a test that trims the final newline before appending 1 and 512 NULs.
- **Files modified:** internal/convert/cadsniff.go, internal/convert/cadsniff_test.go
- **Commit:** 1612962

Task 3 was committed as one commit (tests and rules together); the red state was a compile failure on the undefined rejection constants.

## Known Stubs

None.

## Threat Flags

None.

## Task Commits

- Task 2: `7a4126a` feat(72-04): declare the four CAD rejection codes and document them in both contracts
- Task 3: `1612962` feat(72-04): classify and reject truncated, ambiguous, binary-DXF and DWG CAD uploads by content

## Self-Check: PASSED
