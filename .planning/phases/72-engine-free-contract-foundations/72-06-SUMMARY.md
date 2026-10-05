---
phase: 72-engine-free-contract-foundations
plan: 06
subsystem: api
tags: [cad, error-codes, inspect, contract-sync, step, iges, dxf, stl, dwg, go]
requires:
  - phase: 72-engine-free-contract-foundations
    provides: SniffCADWindows rule set, CADRejection values and the four errcode constants (72-04); stp/igs aliases (72-05)
provides:
  - POST /v1/jobs answers 422 truncated_cad_file, ambiguous_stl, binary_dxf_unsupported and dwg_unsupported before the pair lookup
  - POST /v1/inspect reports the same four strings as Report.Format, answering 200, with extension_mismatch unknown (malformed_input or parser_unavailable)
  - RejectBindings entries and FINDINGS rows for the four reason tokens (twenty-three classes)
  - InspectReport.format description in docs/openapi.yaml naming the four sentinels
  - TestCADCreateRejections, TestCADCreateInspectAgree, TestCADRejectionCodeMapComplete, TestCADSentinelsDocumentedInInspectFormat, TestInspectCADTailReadFailureIsReadFailed
affects: [72-07, 73]
status: complete
commits: 2
plan_head_before: 93fa9bf99fd4b42b1a1a8b7a5f2e32f9a9f472d8
plan_head_after: eae0d5b713795f072867286f1b22efd6b7076d34
actuals:
  tokens: 4700
  tasks: 2
  commits: 2
tech-stack:
  added: []
  patterns:
    - "inspect sentinel strings are the errcode constants themselves, so create and inspect cannot disagree on a spelling"
    - "one table test posts every committed fixture plus in-memory cases to both endpoints and classifies the pair of verdicts"
key-files:
  created: []
  modified:
    - internal/api/handlers.go
    - internal/api/inspect_handlers.go
    - internal/inspect/bindings.go
    - docs/FINDINGS.md
    - docs/openapi.yaml
    - internal/api/cad_sniff_test.go
key-decisions:
  - "Rejections surface in inspect as sentinel formats, not findings: no inspect Code added, findings.go and the pre5* goldens untouched (orchestrator decision 1)"
requirements-completed: [CADIN-01, CADIN-02, CADIN-03]
---

# Phase 72 Plan 06: CAD rejections on both endpoints Summary

Job creation now refuses truncated, ambiguous, binary-DXF and DWG uploads with their stable 422 codes before the (source, target) pair lookup, and inspect reports the identical strings as sentinel formats with a 200, proven equal on every committed fixture.

## Accomplishments

- `handleCreateJob`: the CAD branch switches on the rejection and writes one literal `content validation rejected` log line per class (`reason=truncated_cad_file`, `ambiguous_stl`, `binary_dxf_unsupported`, `dwg_unsupported`), then a fixed-message 422. The only variable in a message is `res.Format` from convert's closed set; filename and bytes are never echoed. A valid CAD upload still falls through to `unsupported_conversion`.
- `internal/inspect/bindings.go` and `docs/FINDINGS.md`: four `ErrCode` bindings and four table rows; prose count "twenty-three classes", table rows counted at 23.
- `inspect_handlers.go`: `cadRejectionCode` map, the four errcode constants appended to `inspectPublicSentinelFormats`, rejection sets `detected` to the mapped code, and `extensionMismatchFamilyResult` reports unknown/malformed_input (truncated, ambiguous) or unknown/parser_unavailable (binary DXF, DWG). A tail read failure still sets `sniffReadFailed`.
- `docs/openapi.yaml`: the `InspectReport.format` description names the four sentinels with the check reason each carries.
- Tests: `TestCADCreateRejections` (table matched to the reject directory in both directions, 15 files), `TestCADCreateInspectAgree` (52 corpus files plus 4 in-memory cases, each classified by create's code and checked against inspect's format and extension_mismatch), `TestCADRejectionCodeMapComplete`, `TestCADSentinelsDocumentedInInspectFormat` (slices the description, not the Error enum), `TestInspectCADTailReadFailureIsReadFailed`.

## Verification

- `go build ./...`, `go vet ./...` and `go test ./... -count=1` green on go1.26.4.
- Task 1 and Task 2 verify commands pass; `WIRING-OK` printed (Pre59 golden commit count is 1).
- `git diff --quiet d034437 -- internal/api/testdata internal/inspect/findings.go` clean; `TestAllCodesCount` passes.
- Mutation check: replacing `detected = cadRejectionCode[...]` with `detected = ""` makes `TestCADCreateInspectAgree` fail 13 subtests; restored before commit.
- No comment lines in `internal/api/cad_sniff_test.go`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Filename-echo assertion applied to rows that use the pre-existing unrecognized path**
- **Found during:** Task 1
- **Issue:** the draft test asserted no filename in any 422 message, but the existing `invalid_request` unrecognized-content message ("unrecognized file content for <name>") already echoes the filename; that path is outside this plan.
- **Fix:** the assertion applies only to rows that carry a fixed CAD message (the four new codes).
- **Files modified:** internal/api/cad_sniff_test.go
- **Commit:** 37306ed

## Known Stubs

None.

## Threat Flags

None.

## Task Commits

- Task 1: `37306ed` feat(72-06): reject truncated, ambiguous, binary-DXF and DWG CAD uploads at job creation
- Task 2: `eae0d5b` feat(72-06): report CAD rejections from inspect as sentinel formats equal to the create codes

## Self-Check: PASSED
