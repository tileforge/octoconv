---
phase: 72-engine-free-contract-foundations
plan: 05
subsystem: api
tags: [cad, format-aliases, frontend, contract-sync, go, typescript]
requires:
  - phase: 72-engine-free-contract-foundations
    provides: NormalizeFormat stp alias and the CAD tracer (72-01)
provides:
  - formatAliases map in internal/convert/convert.go (jpeg, tif, htm, tex, stp, igs) consumed by NormalizeFormat
  - frontend normalizeFormat folds stp to step and igs to iges
  - TestFormatAliasesMatchFrontend, which parses formats.ts and fails naming the alias on drift in either direction
  - TestDefaultRegistersNoCADPair and TestListFormats_AdvertisesNoCADFormat guard tests
affects: [72-06, 73]
status: complete
commits: 2
plan_head_before: 11c81b6492b26eb5aa7247439e445b378563831f
plan_head_after: 5dd24909c3317a7b958382785ad61947e5f65e3e
actuals:
  tokens: 9000
  tasks: 2
  commits: 2
tech-stack:
  added: []
  patterns:
    - "Go-to-TypeScript table sync by regexp over formats.ts, compared both ways, naming the offender"
    - "guard tests walk the live registry and the live HTTP response, with a zero-walk vacuity failure"
key-files:
  created:
    - internal/convert/format_alias_sync_test.go
  modified:
    - internal/convert/convert.go
    - internal/convert/convert_test.go
    - internal/api/formats_handlers_test.go
    - frontend/src/formats.ts
    - frontend/src/formats.test.ts
key-decisions:
  - "formatAliases is an unexported map so the sync test can range over it; NormalizeFormat is behaviour-preserving"
  - "The tex rationale comment moved with its map entry verbatim; no new code comments"
requirements-completed: [CADIN-01]
---

# Phase 72 Plan 05: CAD format aliases and no-CAD guards Summary

`stp` and `igs` now normalise to `step` and `iges` identically in Go and the frontend, a test holds the two alias tables equal, and two guard tests prove no CAD class or pair is registered or advertised.

## Accomplishments

- `NormalizeFormat` reads `formatAliases` (jpeg, tif, htm, tex, stp, igs) instead of a switch; trimming, dot-stripping and lowercasing are unchanged.
- `frontend/src/formats.ts` gains `case 'stp'` and `case 'igs'`; its doc comment lists the full table and now names `TestFormatAliasesMatchFrontend` in place of the stale Go line-number reference. `formats.test.ts` covers both aliases and `sourceFormatOfFilename('part.STP')` and `('part.igs')`.
- `TestFormatAliasesMatchFrontend` extracts `case '<from>': return '<to>';` from formats.ts, fails if it parses nothing, and reports aliases missing in TypeScript, extra in TypeScript, and different targets. `TestNormalizeFormatCADAliases` pins the Go behaviour including identity for step, iges, stl, dxf and dwg.
- `TestDefaultRegistersNoCADPair` walks `Default.Classes()` and `Default.OperationClasses()`; `TestListFormats_AdvertisesNoCADFormat` decodes GET /v1/formats (engines and operations trees). Both fail on a `cad` class or any CAD-format pair, and on a zero walk or an empty engines map.
- `AllEngines()`, the README engine-class table and `readme_sync_test.go` are untouched.

## Verification

- `go build ./...`, `go vet` on both packages, `go test ./internal/convert/ ./internal/api/ -count=1` green; `npm --prefix frontend run test` 37/37.
- Mutation probe (plan gate): removing the `igs` case from formats.ts made `TestFormatAliasesMatchFrontend` fail naming `igs` (`ALIAS-SYNC-FIRES`), and formats.ts was restored afterwards. A second probe adding a spurious `xyz` case failed it in the other direction.
- Registry probe: a temporary stub converter registering a `cad` step->stl pair made `TestDefaultRegistersNoCADPair` fail for both the class and the pair; the stub file was removed. The HTTP test runs in a separate test binary and was not exercised by that stub; it shares the same registry-derived response path covered by the existing tree tests.

## Deviations from Plan

None - plan executed exactly as written. The first mutation-probe run reported BLIND only because of a piped `tee` masking the exit code in my own harness; the test output showed it firing and a clean rerun reported FIRES.

## Known Stubs

None.

## Threat Flags

None.

## Task Commits

1. Task 1: `670cef0` feat(72-05): fold stp and igs aliases in Go and the frontend under a sync test
2. Task 2: `5dd2490` test(72-05): guard that no CAD class or pair is registered or advertised

Both commits are SSH-signed (verified `G`).

## Self-Check: PASSED

- FOUND: internal/convert/format_alias_sync_test.go
- FOUND: commits 670cef0 and 5dd2490 are ancestors of HEAD
