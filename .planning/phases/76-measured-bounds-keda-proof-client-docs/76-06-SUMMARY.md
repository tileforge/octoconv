---
phase: 76-measured-bounds-keda-proof-client-docs
plan: 06
subsystem: cad-docs
tags: [cad, docs, error-codes, preview, d-07, ar-5, ar-8, sync-tests]
status: complete

requires:
  - phase: 76-03
    provides: evidence/76-03-derived.txt (DERIVED engine line with worst cell and route, KNOWN_LIMIT lines)
  - phase: 76-04
    provides: measured CAD constants, cad inspect estimate measured at 886 s
  - phase: 76-05
    provides: one ceiling per format for convert and render (Path-B not fired)
provides:
  - every volume cap of a file drawn from a DXF (PDF bytes, inflate, objects, values, string, decoded text; texts.json and SVG bytes) maps to cad_too_complex
  - docs/CAD.md, the client entry point of the cad class, held by TestCADDocMatchesTheCode and TestCADCodesDocMatchesTheMapping
  - docs/PREVIEW.md, the preview contract pinned on CAD, held by TestPreviewDocMatchesTheCode
  - docs/README.md index entries; SEED-007 pointer
  - evidence/76-06-red.txt, evidence/76-06-ci-local.txt
affects: [76-07, 76-08, 76-09]

actuals:
  tokens: 23549
  tasks: 3
  commits: 7
plan_head_before: 2a668056a8b5b105d4b6b8ac40f8b5b036f167d9
plan_head_after: ffdfac4f337a6cd1aebe73c5773a112085ced827

tech-stack:
  added: []
  patterns:
    - "A rejection over a volume cap of a file our own pipeline drew is wrapped fmt.Errorf(\"%w: %w\", ErrCADTooComplex, reject(...)); a go/parser test enforces it on every reject call and forces every cap constant to be classified"
    - "A client doc table is computed from the code in both directions: the registry, reflect over CADOpts, ValidateCADApplicability per option, the cad-worker env defaults, the chart cap and the evidence lines"
    - "A code table is derived from cadFailureCode over every parsed ErrCAD sentinel, the runner's finish calls, the reconciler's mark-failed literals and the classified errcode constants"

key-files:
  created:
    - docs/CAD.md
    - docs/PREVIEW.md
    - internal/convert/cad_doc_test.go
    - internal/convert/preview_doc_test.go
    - internal/worker/cad_codes_doc_test.go
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-06-red.txt
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-06-ci-local.txt
  modified:
    - internal/convert/caddxfpdf.go
    - internal/convert/caddxfsvg.go
    - internal/convert/caddxf.go
    - internal/convert/caddxfpdf_test.go
    - internal/convert/caddxf_test.go
    - internal/convert/cad_docs_sync_test.go
    - internal/worker/pinning_test.go
    - docs/ERROR-CODES.md
    - docs/README.md
    - .planning/seeds/SEED-007.md

key-decisions:
  - "D-07/AR-8 widened to the class: every limit on how much a file drawn from a DXF may hold is cad_too_complex, not only the PDF byte and inflate limits; shape limits (nesting, word, operands, CMap, reference chain) stay cad_empty_result"
  - "The CAD sync code set is computed, not fixed: it adds ambiguous_stl, dwg_unsupported and invalid_request to the plan's six; the async set adds reconciler_exhausted"
  - "PREV-01 is complete with this plan; CADW-06 continues in 76-07 (openapi, MCP, frontend)"

requirements-completed: [PREV-01]

duration: about 6 h 50 min wall (2026-10-15 02:01Z to 08:52Z), including a signing halt from about 02:40Z until 1Password was unlocked, and waits for load1 < 8 before the gate
completed: 2026-10-15
---

# Phase 76 Plan 06: CAD client docs, error-code truth and the preview contract Summary

**A DXF PDF above its 19 MiB limit, or one whose inflated content passes 64 times that limit, now fails with `cad_too_complex`, as does every other volume limit on a file the pipeline draws from a DXF. docs/CAD.md and docs/PREVIEW.md are contracts: three pure-Go tests compute every table from the code and the 76-03 evidence, in both directions. Mutations on either side make them fail. The gate passes from a fresh clone at e693dc8, with internal/convert at 394.965 s.**

## Task 1: the true code of an oversize DXF PDF (tracer)

RED, from `git archive ad326af` (evidence/76-06-red.txt), quoted:

```
caddxfpdf_test.go:355: err = cad: the drawing's PDF failed the content check: the PDF is 5387 bytes, over the 5386 byte limit, want it to wrap ErrCADTooComplex
caddxfpdf_test.go:367: err = cad: the drawing's PDF failed the content check: the inflated content exceeds 64 times the PDF byte limit, want it to wrap ErrCADTooComplex
pinning_test.go:1626: MarkFailed code = "cad_empty_result", want "cad_too_complex"   (byte limit, from the real judge)
pinning_test.go:1626: MarkFailed code = "cad_empty_result", want "cad_too_complex"   (inflate cap, from the real judge)
```

The same RED run also failed six volume rows in TestCADDXFArtifactVolumeCapsAreTooComplex and nine call sites in TestCADDXFArtifactVolumeRejectionsWrapTooComplex. The converter row "texts.json is above the SVG ceiling" failed for both svg and pdf.

GREEN (400fa44):

| File:line | Change |
|---|---|
| internal/convert/caddxfpdf.go:181 | the inflate cap is wrapped `fmt.Errorf("%w: %w", ErrCADTooComplex, cadPDFReject(...))` |
| caddxfpdf.go:257, :330 | a string longer than cadPDFMaxString, wrapped |
| caddxfpdf.go:460 | more values outside streams than cadPDFMaxValues, wrapped |
| caddxfpdf.go:656 | more objects than cadPDFMaxObjects, wrapped |
| caddxfpdf.go:1257 | more decoded text than cadPDFMaxText, wrapped |
| caddxfpdf.go:1485 | the PDF above maxBytes, wrapped |
| caddxfpdf.go:1721, :1728 | texts.json above maxBytes is refused by its stat size before it is read, and both refusals are wrapped |
| internal/convert/caddxfsvg.go:481 | the SVG above maxBytes, wrapped |
| internal/convert/caddxf.go:275 | the texts.json error under ErrCADSVGRejected was formatted `%v`, which dropped the chain; now `%w` |
| docs/ERROR-CODES.md:122, :148, :150 | the DXF paragraph and the cad_too_complex row name the PDF and volume limits; cad_empty_result says an oversize PDF is not this code |
| internal/convert/cad_docs_sync_test.go | TestErrorCodesDocStatesTheCADCeilingsOfTheCode also holds "a PDF above 19 MiB" and the new cad_too_complex wording |

`cad-pdf-check` on white_one_black.pdf exits the same before and after: 1 at `--max-bytes 5386`, 0 at 5387. Only the reason text gains the `cad: the model is too complex to convert:` prefix, and no script parses it.

TestCADCodesDocMatchesTheMapping (internal/worker/cad_codes_doc_test.go) does the following:

- parses the 17 package-level `ErrCAD*` vars of internal/convert with go/parser and requires a row for each, both ways;
- maps each sentinel through cadFailureCode, plus the fallback;
- adds the codes the runner passes to `finish(..., StatusFailed, Code*)` and the literal the reconciler marks with, and checks each is in AllFailureCodes;
- classifies every errcode constant, parsed from internal/errcode, as a cad code or not, and requires every `convert.CADReject*` mapping in internal/api to land on a cad code;
- compares the union with the `## Error codes` table of docs/CAD.md both ways, including the sync/async kind, and checks the `## Outputs` roles against jobs.OutputRolePrimary and jobs.OutputRoleMeasurements.

## Task 2: docs/CAD.md and TestCADDocMatchesTheCode

The document has the sections Formats, Operations and pairs, Render options, Option applicability, Outputs, Size ceilings (with the per-format table and the pipeline-limit table), Time limits, Error codes and Known limits.

TestCADDocMatchesTheCode (internal/convert/cad_doc_test.go) runs seven subtests, each checked both ways:

- **formats:** source formats, aliases from formatAliases, and the operations of each, against `Default.OperationClasses()`.
- **operations:** every cad pair of every operation.
- **options:** the CADOpts json tags by reflect, the closed sets or the 256..2048 range, and the defaults from `CADOpts{}.Resolved()` (assume_unit resolves to `none`).
- **applicability:** for each (operation, format), the options that ValidateCADApplicability accepts when only that option is set.
- **ceilings:** `cadMaxBytes`, MiB, and min(ceiling, MAX_UPLOAD_BYTES) read from the chart ConfigMap. The pipeline-limit table is checked against the constants.
- **time_limits:** the `envDuration("CAD_*", convert.X)` defaults parsed from cmd/cad-worker/main.go; the two route sums; the 20 s I/O; the worst route and cell of the DERIVED engine line; and estimate.go's measured flag and 886 s.
- **known_limits:** every KNOWN_LIMIT key of evidence/76-03-derived.txt is an id, with the smallest refused bytes per format, the old IGES ceiling and the lowered one. Every named constant states its value, and every constant in the limit set appears.

docs/README.md indexes CAD.md and PREVIEW.md. It now says that the render operation emits the measurements document as role `measurements`, replacing "no engine emits it yet".

## Task 3: docs/PREVIEW.md and TestPreviewDocMatchesTheCode

- **Fixed by CAD** (10 rows): the operation, the output role, the size range and default, the backgrounds, the 3D margin read from cad-render's `MARGIN = 0.06`, the DXF letterbox margin `cadMarginMin` 0.03, the 3D views with default iso, the DXF view refused for every view value, and that refusal mapped to `errcode.OptsNotApplicable` in internal/api/handlers.go. It also covers measurements and the pixel judge. A row the test does not know fails.
- **Non-previewable input:** `cad_empty_result`, `cad_unreadable`, `cad_too_complex`, `unsupported_conversion` and `dwg_unsupported`, plus the sentence that no placeholder image is ever returned.
- **Not fixed:** the eleven items, held by the test.
- SEED-007 has the pointer line and stays `status: dormant`.

## Mutations (each reverted; `cmp` confirmed every restore)

| Mutation | Test | Result |
|---|---|---|
| drop the `ErrCADMeshTimeout` row of the sentinel table | TestCADCodesDocMatchesTheMapping | FAIL "internal/convert declares ErrCADMeshTimeout, which has no row" |
| delete the `cad_mesh_timeout` row of docs/CAD.md | same | FAIL "a cad job or request can end with \"cad_mesh_timeout\", but ... has no row" |
| engine_error kind `sync 422`; extra `unknown_preset` row | same | FAIL on the kind and on the extra code |
| view default iso to top | TestCADDocMatchesTheCode | FAIL "the default of view is \"top\", the code resolves \"iso\"" |
| bogus pair `render dxf jpg` | same | FAIL "the document has \"render dxf jpg\", the code does not" |
| STEP MiB 63 to 64 | same | FAIL |
| `view` added to render dxf in the doc | same | FAIL |
| quiet_host_bounds row deleted | same | FAIL "KNOWN_LIMIT quiet_host_bounds has no row" |
| CAD_DXF_TIMEOUT 185 | same | FAIL |
| R-10 value 0.0047 | same | FAIL |
| code: ValidateCADApplicability accepts view on dxf | same | FAIL "the code has \"view\", the document does not" |
| PREVIEW default size 1024 | TestPreviewDocMatchesTheCode | FAIL |
| PREVIEW 3D margin 0.05; DPI item removed; placeholder sentence negated; transparent as default | same | FAIL each |

## Gate (evidence/76-06-ci-local.txt)

- The gate ran from a fresh clone of a git bundle at e693dc8, with deploy/tectonic-bundle/octoconv-bundle.ttb copied in.
- Environment: GOTOOLCHAIN=go1.26.4 and python 3.14.8 first on PATH.
- The transcript shows no tracked-dirt warning, and the clone had 0 porcelain lines after the run.
- It started at 08:43:37Z with load1 8.83 (the wait loop saw 7.76 at 08:43:29Z) and ended at 08:50:56Z with load1 6.23.

| Tier | Result | internal/convert | internal/worker | internal/api | internal/errcode |
|---|---|---|---|---|---|
| gate | PASS: gofmt clean, go vet clean, go build clean, go test ./... ok | **394.965 s**, under the 540 s stop line (D-19) | 12.470 s | 11.630 s | 5.616 s |

The transcript ends `ALL REQUESTED TIERS PASSED` and has no FAIL line. Its `docker:` header line is empty because `docker` on PATH was a stub that exits 127 (deviation 7). No Go test calls docker. The new convert tests together take under 2 s (the volume-cap table about 0.8 s, the doc tests a few ms).

Plan verification: `GOTOOLCHAIN=go1.26.4 go vet ./...` exits 0; `gofmt -l .` is empty; `git diff --exit-code main -- internal/convert/exec.go` exits 0. Added `//` or `#` lines in *.go, *.yaml and *.yml over 2a66805..HEAD: 0.

## Deviations from Plan

1. **[Fix the class] More volume caps than D-07 named.** The plan asked to wrap only the PDF byte and inflate rejections and to make "no other change". On the orchestrator's instruction to fix the class, every rejection over a volume limit of a file our pipeline drew from a DXF is now cad_too_complex: PDF objects, values outside streams, a single string, decoded text, texts.json bytes and SVG bytes. A go/parser test holds the rule on every `cadPDFReject`/`cadSVGReject` call. `grep -c 'ErrCADTooComplex, cadPDFReject' caddxfpdf.go` is 9. The 3D PNG cap of 96 MiB (cadPixelMaxBytes) is not part of the class: a 2048 px PNG cannot reach it, so it is a shape limit, not a volume of the model.
2. **[Rule 1] caddxf.go:275 lost the error chain** (`%v`), so a texts.json volume rejection would still have reached the client as engine_error. Fixed with `%w`; the converter-level test row proves it.
3. **[Rule 2, D-19] texts.json is refused by its stat size before it is read.** Without that, refusing an oversize texts.json read up to 134 MiB first.
4. **Sync and async code sets are computed.** The plan's frontmatter assumption listed six sync codes. The code also emits ambiguous_stl, dwg_unsupported and invalid_request (unrecognised CAD content) for cad uploads, and a cad job can end with reconciler_exhausted. All four are documented and held. Every errcode constant must be classified, so a new one fails the test until someone decides.
5. **Extra content in docs/CAD.md and PREVIEW.md.** CAD.md adds a pipeline-limit table under Size ceilings (STEP entities, NAUO, render triangles, DXF caps, SVG, PDF, inflate), held by the test. PREVIEW.md adds a dwg_unsupported row to Non-previewable input.
6. **TDD order.** Task 1's RED commit (ad326af) was verified in a `git archive` export, not with a stash, because stash is prohibited. Tasks 2 and 3 each committed the test first (9545e5f, 0d5bf07). Each was verified failing in an export of that commit, then the docs were committed.
7. **Docker stub for the gate.** `scripts/ci-local.sh` calls `docker info` in its header, and the daemon is hung and must not be called. The gate ran with a stub `docker` first on PATH that exits 127. The gate tier itself uses no docker. No docker tier (docker-build, e2e) was requested or run.
8. **Gate start load** was 8.83, just above the < 8 the earlier plans waited for; the loop saw 7.76 eight seconds before the start. The 394.965 s is under 540 s, so D-19 does not stop the plan.
9. **Signing halt.** Commits failed three times with `1Password: failed to fill whole buffer` at about 02:40Z. Work stopped, uncommitted, until the orchestrator confirmed 1Password was unlocked. Every commit of this plan is SSH-signed (G).
10. **Files outside files_modified:** internal/convert/caddxfsvg.go, caddxf.go, caddxf_test.go, cad_docs_sync_test.go, and the evidence files.

## Known Stubs

None. CAD.md has no `## Scaling` section yet; 76-09 adds it by plan.

## Threat Flags

None. No new endpoint, auth path or file access in production code. T-76-11 is mitigated by the three both-way tests and the mutations above. T-76-12 is mitigated by the pinning rows built from the real judge.

## Self-Check: PASSED

- Files exist: docs/CAD.md, docs/PREVIEW.md, internal/convert/cad_doc_test.go, internal/convert/preview_doc_test.go, internal/worker/cad_codes_doc_test.go, evidence/76-06-red.txt, evidence/76-06-ci-local.txt.
- All seven commits from 2a66805 are in HEAD, SSH-signed (G), with no Co-Authored-By trailer:
  - ad326af test (RED)
  - 400fa44 fix (GREEN)
  - 9545e5f test
  - 229da01 docs
  - 0d5bf07 test
  - e693dc8 docs
  - ffdfac4 test (evidence)
