---
phase: 72-engine-free-contract-foundations
plan: 01
subsystem: api
tags: [cad, step, content-sniffing, fixtures, go]
requires:
  - phase: 71-cad-engine-measurement
    provides: the committed CAD corpus (37 files), the three corpus walkers and the generator the reject fixtures extend
provides:
  - convert.SniffCADWindows, one CAD detector over a 4 KiB head and a 4 KiB tail, called by handleCreateJob and detectInspectFormat (STEP accept rule)
  - NormalizeFormat folds stp to step
  - TestCADTracerSTEPBothEndpoints over real, legacy 8-bit, over-window and truncated STEP files
  - scripts/cad-corpus-gen.py --reject and 15 committed rejection and precedence fixtures with generator-emitted provenance
affects: [72-04, 72-05, 72-06, 73]
status: complete
commits: 2
plan_head_before: da625c6c2860ad6f8d297d71d3310591d4a334ea
plan_head_after: 64600689b05eb73c1c49baae7d4b299a9a796737
actuals:
  tokens: 29000
  tasks: 2
  commits: 2
tech-stack:
  added: []
  patterns:
    - "pure decision over already-read windows (SniffCADWindows) with the I/O in ReadCADWindows and ReadCADTail, ReadAt only"
    - "fixtures generated and committed before the rule that judges them (D-10)"
    - "provenance emitted by the generator and diffed against the marked region of PROVENANCE.md"
key-files:
  created:
    - internal/convert/cadsniff.go
    - internal/api/cad_sniff_test.go
    - internal/convert/testdata/cad/reject/ (15 files)
  modified:
    - internal/convert/convert.go
    - internal/api/handlers.go
    - internal/api/inspect_handlers.go
    - scripts/cad-corpus-gen.py
    - scripts/cad-corpus-inventory.sh
    - scripts/cad-image-trace.sh
    - scripts/cad-rss-time-measure.sh
    - internal/convert/testdata/cad/PROVENANCE.md
key-decisions:
  - "STEP accept rule: head (leading ASCII whitespace stripped) starts with ISO-10303-21;, head contains HEADER;, tail with trailing ASCII whitespace stripped ends with END-ISO-10303-21;"
  - "No UTF-8 validation and no bytes >= 0x80 check on CAD bytes (D-03); a UTF-8 BOM is not stripped, so BOM files stay rejected until the differential decides (orchestrator decision 2)"
  - "reject/ is excluded from the three phase-71 walkers with ! -path '*/reject/*' (orchestrator decision 3)"
requirements-completed: [CADIN-01, CADIN-02]
---

# Phase 72 Plan 01: STEP tracer and rejection fixtures Summary

One CAD detector, `convert.SniffCADWindows`, now serves both `POST /v1/jobs` and `POST /v1/inspect` for STEP, and 15 deterministic rejection and precedence fixtures are committed with provenance before any rule that judges them.

## Accomplishments

- `internal/convert/cadsniff.go`: `CADRejection` (only `CADRejectNone`), `CADResult`, `CADHeadBytes`/`CADTailBytes` (4096), `IsCADDeclared`, `ReadCADTail`, `ReadCADWindows` (two `ReadAt` calls, never `Read`), `SniffCADWindows` with the STEP accept rule only. Every other input returns the zero `CADResult`, so the existing chain fails closed with `unrecognized_content`.
- `NormalizeFormat("stp") == "step"`, so a `.stp` upload's declared source equals the detected format.
- `handleCreateJob`: CAD branch after the md/rst/org branch and before `IsOLECFB`. A read error leaves `detected` empty; `rest` is never rebound.
- `detectInspectFormat`: CAD block in the sniff-window case after the text-envelope matchers and before the OLE-CFB matcher, outside the `textShaped` condition. When the window already holds the whole file the tail is sliced from it (no second read, no second charge); otherwise `ReadCADTail` is charged with `tr.Consume`, and a tail read error sets `sniffReadFailed`.
- `TestCADTracerSTEPBothEndpoints`: LM8UU.step, nist_ctc_01 `.stp`, legacy_cp1251.step, legacy_latin1.step and a 1.1 MiB padded STEP all answer `422 unsupported_conversion` with `unsupported conversion: step -> png` in create and `format: step`, `declared_format: step`, `extension_mismatch` clean in inspect. A half file and the padded file with its last 64 bytes cut are not accepted by either endpoint.
- `scripts/cad-corpus-gen.py --reject [--out-dir] [--check-only] [--provenance]`: stdlib only, deterministic, 15 fixtures, structural self-checks (stl_ambiguous must satisfy the size formula, start with `solid` and end with an `endsolid` line, so a silently non-ambiguous fixture fails first). `--provenance` prints the PROVENANCE.md block from the files on disk; the marked region in PROVENANCE.md is byte-identical to it.
- The three walkers skip `reject/`; the committed group still counts 37 files.

## Verification

- `GOTOOLCHAIN=go1.26.4 go build ./...`, `go vet ./internal/convert/ ./internal/api/`, `go test ./internal/api/ ./internal/inspect/ ./internal/convert/ -count=1`: green (includes TestPre58/59, TestInspectSentinelFormatsMatchTheContract, TestNoLiteralErrorCodesInAPISource, TestRejectBindings, TestAllEngines, TestREADME).
- Task 2 gate: two generator runs diff clean against each other and against the committed `reject/`; 37 committed corpus files, 15 reject files; provenance region diff clean, 15 sections; `REJECT-FIXTURES-OK`. The ambiguity gate printed `AMBIGUITY-CHECK-FIRES` (the synthetic binary STL swapped in for stl_ambiguous.stl fails the self-check).
- `git diff --stat d034437 -- internal/api/testdata internal/convert/testdata/cad/synthetic` is empty; no `(72-` commit touches `scripts/cad-measure` or `Dockerfile.cad-measure`; `scripts/cad-measure/common.sh tag 7.9.3.1.1 pruned` prints `octoconv-cad-measure:ocp7.9.3.1.1-pruned-0d09b7ba39ad`, unchanged (the hash covers only Dockerfile.cad-measure and scripts/cad-measure).
- Task 2 precondition: `pgrep` for the three phase-71 scripts printed nothing and the four scripts were clean in `git status`.
- Tracer gate: the Task 1 verify command was re-run end to end after the commit's final edit and passed before Task 2 started.

## Commits

- `c7b1dc9` feat(72-01): detect STEP by content in create and inspect through one CAD sniffer
- `6460068` test(72-01): commit CAD rejection fixtures and their generator before any rule judges them

Both are SSH-signed (`Good "git" signature`). The reject/ commit is the latest commit touching `internal/convert/testdata/cad/reject`, so it precedes every later cadsniff rejection value (D-10 ordering for 72-04).

## Deviations from Plan

None - plan executed as written. Notes that are not deviations:

- Two small refactors in the generator keep the synthetic output byte-identical (checked against the committed files): `comments_999_pairs()` split out of `build_comments_999()` so the truncated DXF can drop the final pair, and `stl_binary_blob(header, tris)` split out of `build_stl_binary()`.
- `ReadAt` is inlined in both `ReadCADTail` and `ReadCADWindows` rather than shared through a helper, so the acceptance count of at least two `ReadAt` lines holds.
- The existing `--out-dir` default moved from `DEFAULT_OUT` to `None` resolved per mode, as the plan prescribed; the synthetic default is unchanged.

## Design notes (rationale kept out of the code)

- The decision function takes `size` and `declared` although the STEP rule uses neither; plan 72-04 needs both for the STL size formula and the declared-extension gating, so the signature is fixed now to avoid touching both call sites again.
- The create branch runs only when `convert.Sniff`, the ZIP check and the text matchers found nothing, so a PNG renamed `.step` keeps its existing mismatch 422 (T-72-01).
- `dwg_ac1027.dwg`'s provenance sentence names the verdict class it exists to exercise; the actual rejection rule is 72-04's.

## Known Stubs

None.

## Threat Flags

None. The new surface (two bounded `ReadAt` calls per upload) is the one T-72-02 and T-72-03 cover.

## Auto-selected decisions

No `checkpoint:decision` task in this plan; nothing was auto-selected under user delegation (2026-10-04).

## Self-Check: PASSED

- FOUND: internal/convert/cadsniff.go, internal/api/cad_sniff_test.go, internal/convert/testdata/cad/reject/ (15 files)
- FOUND commits c7b1dc9 and 6460068 as ancestors of HEAD
