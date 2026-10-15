---
phase: 76-measured-bounds-keda-proof-client-docs
plan: 07
subsystem: cad-client-surfaces
tags: [cad, openapi, mcp, frontend, vitest, d-16, ar-5, sync-tests]
status: complete

requires:
  - phase: 76-06
    provides: docs/CAD.md, TestCADCodesDocMatchesTheMapping and the computed CAD sync/async code sets
provides:
  - docs/openapi.yaml components.schemas.CADOpts; CADOpts offered in JobCreated/JobStatus opts; every cad pair in the render paragraph; the produced and reserved roles; every async cad code in error_code
  - TestCADOpenAPIMatchesTheCode (internal/convert) and the openapi error_code check in TestCADCodesDocMatchesTheMapping (internal/worker)
  - convertToolDescription(mode, outputDir) and MCP tags naming render, the CAD options, docs/CAD.md and the real roles, held by TestMCPDescriptionsMatchTheRegistry
  - frontend operationTable, operationsOf, targetFormatsForOperation; the operation select, the operation-grouped format table and the outputs-by-role list in App.tsx
  - evidence/76-07-red.txt, evidence/76-07-red-t3-junit.xml, evidence/76-07-red-t3-{1..5}.json, evidence/76-07-ci-local.txt
affects: [76-08, 76-09, 76-10, 76-11]

actuals:
  tokens: 12338
  tasks: 3
  commits: 8
plan_head_before: acb77f89c0f720b5db6f36c36fc4394a693489c7
plan_head_after: 1986b2fdc774beffd5da321808d8f47ba5992afe

tech-stack:
  added: []
  patterns:
    - "An openapi block is found by a key path walked by indentation (components: schemas: CADOpts:), and a folded description is read the way YAML folds it: lines joined by spaces, paragraphs by blank lines"
    - "The reserved output roles are computed: the last role CHECK in internal/db/migrations minus jobs.OutputRolePrimary, OutputRoleReport and OutputRoleMeasurements"
    - "An MCP description test compares the struct tag with what the in-memory server publishes, so the tag and the tool schema an agent reads are the same object"
    - "A frontend table is a pure function of the response; the vitest RED is a JUnit report the tdd-red-evidence classifier accepts"

key-files:
  created:
    - internal/convert/cad_openapi_doc_test.go
    - internal/mcpserver/descriptions_test.go
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-07-red.txt
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-07-red-t3-junit.xml
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-07-ci-local.txt
  modified:
    - docs/openapi.yaml
    - internal/worker/cad_codes_doc_test.go
    - internal/mcpserver/mcpserver.go
    - internal/mcpserver/tools.go
    - internal/mcpserver/client.go
    - frontend/src/formats.ts
    - frontend/src/formats.test.ts
    - frontend/src/App.tsx
    - frontend/src/styles.css

key-decisions:
  - "The openapi render paragraph lists every cad pair as op from->to tokens, so the test can compare it with the registry both ways rather than look for format words"
  - "The openapi error_code description names the computed async set of 76-06 (eight codes, including invalid_options and reconciler_exhausted), not the six the plan listed"
  - "The frontend sends operation only when it is not convert, so a convert request stays byte-identical to before (T-57-31)"
  - "CADW-06 is complete with this plan: docs/CAD.md and error codes (76-06), openapi, MCP and the frontend table (76-07)"

requirements-completed: [CADW-06]

duration: about 23 min wall (2026-10-15 08:56Z to 09:20Z), including a wait for load1 < 8 before the gate
completed: 2026-10-15
---

# Phase 76 Plan 07: openapi, MCP and the reference frontend brought to the cad class Summary

**docs/openapi.yaml now has a CADOpts schema read from cadopts.go, lists every cad pair under render and convert, names the produced and the reserved output roles, and names every async cad code. The MCP convert_file tool names render, the five CAD options, docs/CAD.md and the real roles. The reference frontend builds its format table and operation select from the /v1/formats operations key. Each surface is held by a test that reads the code, the registry or the response. The helm and gate tiers pass from a fresh clone at 8054f93, with internal/convert at 381.797 s.**

## Task 1: openapi (tracer)

RED at f26f893 (evidence/76-07-red.txt, from a `git archive` export), quoted:

```
cad_openapi_doc_test.go:192: ../../docs/openapi.yaml has no components: schemas: CADOpts:
cad_openapi_doc_test.go:297: the render paragraph does not point to docs/CAD.md
cad_openapi_doc_test.go:319: cad pairs of the render paragraph: the code has "convert dxf pdf", the document does not
   ... the same for convert dxf svg, convert iges stl, convert step stl, render dxf/iges/step/stl png
cad_openapi_doc_test.go:337: the outputs description does not say that role "preview" is reserved and never produced
cad_openapi_doc_test.go:337: the outputs description does not say that role "thumbnail" is reserved and never produced
cad_codes_doc_test.go:352: a cad job can end with "cad_empty_result", but the error_code description of docs/openapi.yaml does not name it
   ... the same for cad_mesh_timeout, cad_too_complex, cad_unreadable, engine_error, input_too_large, invalid_options, reconciler_exhausted
cad_codes_doc_test.go:361: the error_code description of docs/openapi.yaml does not point to docs/CAD.md
```

Semantic assessment: both target tests ran and failed on their own assertions, because the spec lacked what they check. Nothing failed to compile or load.

TestCADOpenAPIMatchesTheCode (internal/convert/cad_openapi_doc_test.go) has three subtests:

- **cadopts:**
  - The property set equals the CADOpts json tags, read by reflect.
  - Each string enum equals its closed map, and every enum value is accepted by ParseCADOpts.
  - size has minimum and maximum equal to CADMinRenderSize and CADMaxRenderSize, and ParseCADOpts refuses min-1 and max+1.
  - Each default equals `CADOpts{}.Resolved()`, and assume_unit, which resolves to nothing, may have no default.
  - The schema is `type: object` with `additionalProperties: false`, and its description points to docs/CAD.md.
  - The multipart `opts` description names CADOpts and docs/CAD.md.
  - JobCreated.opts and JobStatus.opts offer `$ref CADOpts`.
- **operations:** the multipart `operation` description quotes every key of `Default.OperationClasses()`. The paragraph that starts with `"render"` lists `op from->to` tokens, which are compared with every cad pair of every operation, both ways, and it points to docs/CAD.md.
- **roles:** the JobStatus.outputs description quotes "primary", "report" and "measurements". It also names every role that the last role CHECK in internal/db/migrations allows beyond those three (preview, thumbnail), with "reserved" and "never produced".

TestCADCodesDocMatchesTheMapping parses the JobStatus.error_code description. Every code in the computed async set must be named. Every `cad_*` token it names must be a code that a cad job or request can end with. It must point to docs/CAD.md.

GREEN (cf7d3f8):

- CADOpts has enums sorted, size 256..2048 with default 1280, and defaults iso, standard and opaque.
- The opts, render, outputs and error_code texts are as described above.

The description reader's folded-scalar join (spaces, not newlines) was corrected in the same commit. In the RED commit it joined lines with `\n`, which only affected the "never produced" phrase match.

Tracer gate: the task's `<verify>` was re-run end to end after GREEN and passed before Task 2 started.

## Task 2: MCP descriptions

Commits: b0545d6 (refactor), f105123 (test), 72f452c (GREEN).

The refactor moved the two convert_file descriptions unchanged into `convertToolDescription(mode ResultMode, outputDir string) string`. That lets the RED test compile and fail on assertions rather than on a missing symbol. All other mcpserver tests stayed green.

RED at f105123 (25 assertion lines in evidence/76-07-red.txt), quoted:

```
descriptions_test.go:171: the operation field description does not name "render", a registered operation
descriptions_test.go:171: the operation field description does not name "sanitize", a registered operation
descriptions_test.go:176: the operation field description does not mention "docs/CAD.md"
descriptions_test.go:184: the opts field description does not name the CAD option "view"   (and size, quality, background, assume_unit)
descriptions_test.go:195: the role field description does not name role "measurements"
descriptions_test.go:204: the role field description does not say the reserved roles are "reserved" and "never produced"
descriptions_test.go:212: mode "": the convert_file description does not name "render"
descriptions_test.go:212: mode "remote": the convert_file description does not name "render"
```

Semantic assessment: the target test ran and failed on the tags and descriptions it checks.

TestMCPDescriptionsMatchTheRegistry checks:

- The Operation tag names every key of `convert.Default.OperationClasses()`, docs/CAD.md, GET /v1/formats and operations.
- The Opts tag names every CADOpts json tag.
- The Role tag names the three jobs constants and the reserved roles of the CHECK, with "reserved" and "never produced".
- `convertToolDescription` names render and every format of a render pair, in both modes. It names the output dir only in local mode.
- Through an in-memory MCP session, the published convert_file description equals `convertToolDescription`, the published operation and opts descriptions equal the tags, and the get_job_status output schema carries the Role tag.

`grep -c 'convertDesc :=' internal/mcpserver/mcpserver.go` prints 0. NewServer calls convertToolDescription directly.

## Task 3: the reference frontend

RED at 4ede12e. operationTable, operationsOf and targetFormatsForOperation were declared as empty stubs, so the tests fail on assertions. Two reports of the same `git archive` export:

- **tap-flat:** 5 failed, 37 passed. The tdd-red-evidence classifier refused it as invalid_record (Non-TAP data, Malformed TAP). vitest prints a deep-equal failure's multi-line expected value unindented inside the TAP YAML block.
- **JUnit** (evidence/76-07-red-t3-junit.xml): the classifier returned `RED_EVIDENCE_OK (target_test_failed)` for all five targets:
  - operationTable equals the response rows
  - operationTable changes with the response
  - operationTable without the operations key is the engines under convert
  - operationTable order is stable
  - targetFormatsForOperation folds aliases

Each failed with `AssertionError: expected [] to deeply equal ...`.

GREEN (8054f93):

- **formats.ts:**
  - `operationTable` walks `resp.operations`, or `{convert: resp.engines}` when that key is absent, and sorts by operation, engine, from and to with code-unit comparison.
  - `operationsOf` is the sorted set of operations of the table.
  - `targetFormatsForOperation` filters the table by operation and normalized source.
- **App.tsx:**
  - An Operation select fed by operationsOf, default convert. It falls back to the first operation if convert is absent.
  - Targets come from targetFormatsForOperation once a file is chosen, and from the operation's rows before that.
  - createJob gets `operation: operation === defaultOperation ? undefined : operation`.
  - A "Supported conversions" card has one `<h3>` per operation and an engine/from/to table from operationTable.
  - A done job with outputs lists role, format, size and a download link (`rel="noopener noreferrer"`).
- **styles.css:** a three-column form grid, plus the table and outputs styles.

Results:

| Check | Result |
|---|---|
| `npm test` | `Test Files 4 passed (4)`, `Tests 42 passed (42)` |
| `npm run build` | `tsc --noEmit` clean; vite `✓ built in 188ms` |
| `TestFormatAliasesMatchFrontend` | ok |
| `TestNoConsumerBranchesOnErrorMessageText` | ok |
| `git diff --exit-code main -- frontend/package.json frontend/package-lock.json` | exit 0 |
| `grep -c "operationTable(" App.tsx` | 1 |
| `grep -c "operation:" App.tsx` | 1 |
| `grep -cE "'(step\|iges\|stl\|dxf\|png\|svg\|pdf)'" App.tsx` | 0 |

## Mutations (each reverted; `cmp` confirmed every restore)

| Mutation | Test | Result |
|---|---|---|
| `fine` removed from the CADOpts quality enum | TestCADOpenAPIMatchesTheCode | FAIL "CADOpts.quality enum: the code has \"fine\", the document does not" |
| `cad_mesh_timeout` removed from the error_code description | TestCADCodesDocMatchesTheMapping | FAIL "a cad job can end with \"cad_mesh_timeout\", but the error_code description ... does not name it" |
| size default 1280 to 1024 | TestCADOpenAPIMatchesTheCode | FAIL "CADOpts.size default is \"1024\", the code resolves 1280" |
| extra `dxf->jpg` in the render paragraph | same | FAIL "the document has \"render dxf jpg\", the code does not" |
| CADOpts additionalProperties true | same | FAIL |
| thumbnail dropped from the outputs description | same | FAIL "role \"thumbnail\" is reserved and never produced" |
| bogus `cad_overload` in error_code | TestCADCodesDocMatchesTheMapping | FAIL "names \"cad_overload\", which no cad job or request can end with" |
| MCP Role tag drops measurements | TestMCPDescriptionsMatchTheRegistry | FAIL |
| MCP Opts tag drops view | same | FAIL |
| render sentence removed from the remote description | same | FAIL for mode "remote" |
| MCP Role tag drops thumbnail | same | FAIL |
| a hard-coded `render cad stl->png` row pushed in operationTable | vitest | 4 FAIL |
| operationTable built from engines only, ignoring operations | vitest | 4 FAIL |

## Gate (evidence/76-07-ci-local.txt)

- The gate ran `scripts/ci-local.sh helm gate` from a fresh clone of a git bundle at 8054f93, with deploy/tectonic-bundle/octoconv-bundle.ttb copied in.
- Environment: GOTOOLCHAIN=go1.26.4, python 3.14 first on PATH, and a stub `docker` that exits 127 (see deviation 4).
- tracked_dirt=0: the transcript has no TRACKED-changes marker, and the clone had 0 porcelain lines after the run.
- It started at 09:12:06Z with load1 7.75 and ended at 09:19:19Z with load1 8.93.

| Tier | Result | internal/convert | internal/worker | internal/api | internal/errcode | internal/mcpserver |
|---|---|---|---|---|---|---|
| helm | PASS | - | - | - | - | - |
| gate | PASS: gofmt clean, go vet clean, go build clean, go test ./... ok | **381.797 s**, under the 540 s stop line (D-19) | 11.323 s | 8.887 s | 5.308 s | 5.477 s |

The transcript ends `ALL REQUESTED TIERS PASSED` and has no FAIL line. The helm tier ran helm v4.3.0 against the v4.2.3 pinned in ci.yml; the script's own warning says so. vitest is run by no gate tier (research Pitfall 9), so `npm test` and `npm run build` were run explicitly above.

Plan verification:

- `git diff --exit-code main -- internal/convert/exec.go frontend/package-lock.json` exits 0.
- Added `//`, `#`, `/*` or `*` lines in *.go, *.yaml, *.yml, *.ts and *.tsx over acb77f8..HEAD: 0.

## Deviations from Plan

1. **[Rule 2] The error_code description names eight async codes, not six.** 76-06 computes the async set, and it also contains invalid_options (a stored option failing re-validation) and reconciler_exhausted. The test requires the computed set, so the description names all eight.
2. **[Rule 2] The render paragraph also lists the convert pairs** (step->stl, iges->stl, dxf->svg, dxf->pdf), because "every CAD pair" in the registry includes them. JobCreated.opts and JobStatus.opts also offer CADOpts, because the API echoes the normalized CADOpts of a cad job (internal/api/handlers.go:1011).
3. **TDD order in Tasks 2 and 3.** A separate refactor commit (b0545d6) and empty TS stubs in the RED commit (4ede12e) make the RED fail on assertions instead of on a missing symbol. A missing symbol is an INVALID_RED under the TDD reference.
4. **Docker stub for the gate,** as in 76-06. Docker is up, but no tier of this plan needs it, so the header's `docker info` hit a stub on PATH that exits 127. The `docker:` header line is therefore empty. No Go test calls docker.
5. **The Go REDs are transcripts, not classifier records.** The tdd-red-evidence classifier has no Go adapter, and no Go JUnit reporter is installed (installing one is outside Rule 3). The vitest RED went through the classifier.
6. **Files outside files_modified:** frontend/src/styles.css (grid and table styles), and the evidence files.
7. **The test helper fix in the GREEN commit:** the openapi description reader of f26f893 joined folded lines with newlines. cf7d3f8 joins them with spaces, as YAML does.

## Known Stubs

None. The RED stubs of 4ede12e were replaced in 8054f93.

## NOT RUN

- The reference frontend was not opened in a browser against a live API. The table, select and outputs list are proven by vitest on formats.ts and by `tsc`/vite build only. No live-API proof was required by the plan.
- The docker-build, e2e and race tiers were not requested and did not run.

## Threat Flags

None. The frontend renders server strings by React interpolation only. The output links are the presigned URLs the API returns, with `rel="noopener noreferrer"` (T-76-13). T-76-14 is mitigated by the three tests above and the mutations. No npm package was added and the lockfile is unchanged (T-76-SC).

## Self-Check: PASSED

- Files exist: internal/convert/cad_openapi_doc_test.go, internal/mcpserver/descriptions_test.go, evidence/76-07-red.txt, evidence/76-07-red-t3-junit.xml, evidence/76-07-ci-local.txt.
- All eight commits from acb77f8 are in HEAD, SSH-signed (G), with no Co-Authored-By trailer:
  - f26f893 test (RED)
  - cf7d3f8 docs (GREEN)
  - b0545d6 refactor
  - f105123 test (RED)
  - 72f452c feat (GREEN)
  - 4ede12e test (RED)
  - 8054f93 feat (GREEN)
  - 1986b2f test (evidence)
