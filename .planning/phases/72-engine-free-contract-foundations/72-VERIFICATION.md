---
phase: 72-engine-free-contract-foundations
verified: 2026-10-05T07:20:00Z
status: passed
score: 5/5 must-haves verified
covered_files:
  - .planning/phases/72-engine-free-contract-foundations/72-01-PLAN.md
  - .planning/phases/72-engine-free-contract-foundations/72-01-SUMMARY.md
  - .planning/phases/72-engine-free-contract-foundations/72-02-PLAN.md
  - .planning/phases/72-engine-free-contract-foundations/72-02-SUMMARY.md
  - .planning/phases/72-engine-free-contract-foundations/72-03-PLAN.md
  - .planning/phases/72-engine-free-contract-foundations/72-03-SUMMARY.md
  - .planning/phases/72-engine-free-contract-foundations/72-04-PLAN.md
  - .planning/phases/72-engine-free-contract-foundations/72-04-SUMMARY.md
  - .planning/phases/72-engine-free-contract-foundations/72-05-PLAN.md
  - .planning/phases/72-engine-free-contract-foundations/72-05-SUMMARY.md
  - .planning/phases/72-engine-free-contract-foundations/72-06-PLAN.md
  - .planning/phases/72-engine-free-contract-foundations/72-06-SUMMARY.md
  - .planning/phases/72-engine-free-contract-foundations/72-07-PLAN.md
  - .planning/phases/72-engine-free-contract-foundations/72-07-SUMMARY.md
  - .planning/phases/72-engine-free-contract-foundations/evidence/72-sniff-divergence.txt
  - docs/ERROR-CODES.md
  - docs/FINDINGS.md
  - docs/MEASUREMENTS.md
  - docs/openapi.yaml
  - frontend/src/formats.test.ts
  - frontend/src/formats.ts
  - internal/api/cad_sniff_test.go
  - internal/api/formats_handlers_test.go
  - internal/api/handlers.go
  - internal/api/handlers_test.go
  - internal/api/inspect_handlers.go
  - internal/convert/cadsniff.go
  - internal/convert/cadsniff_golden_test.go
  - internal/convert/cadsniff_test.go
  - internal/convert/convert.go
  - internal/convert/convert_test.go
  - internal/convert/format_alias_sync_test.go
  - internal/convert/testdata/cad-sniff-golden.tsv
  - internal/db/migrations/0018_output_role_measurements.sql
  - internal/errcode/errcode.go
  - internal/inspect/bindings.go
  - internal/jobs/jobs.go
  - internal/jobs/output_role_test.go
  - internal/measurements/consistency_test.go
  - internal/measurements/doc_sync_test.go
  - internal/measurements/measurements.go
  - internal/measurements/measurements_test.go
  - internal/measurements/testdata/helpers-71-04-rows.txt
  - internal/worker/webhook_role_test.go
  - scripts/cad-corpus-gen.py
  - scripts/cad-sniff-differential.sh
  - scripts/cad-sniff-reader.py
  - scripts/cmd/cad-sniff/main.go
covered_digest: "v3:sha256:0d5e40dfab9777c6cebc727cbef05090860aaa724c603a3a31bac37aa02149a1"
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: passed
  previous_score: 5/5
  gaps_closed: []
  gaps_remaining: []
  regressions: []
  trigger: "Codex P2 on PR #33: measurements validator accepted documents whose null reasons contradict their own counts (fixes adddbfe, 202c615)"
---

# Phase 72: Engine-Free Contract Foundations Verification Report

**Phase Goal:** The review-riskiest surface, fail-closed validation of CAD content plus the measurements contract, is ready and proven by tests before any engine exists. The class is not registered and nothing is advertised.
**Verified:** 2026-10-05T07:20:00Z
**Status:** passed
**Re-verification:** Yes, after the Codex P2 fix on PR #33 (adddbfe, 202c615); the initial 5/5 verification is kept below

Starting hypothesis was that tasks were completed and the goal missed. Each truth below was checked against code, tests that were re-run, and the committed evidence, not against SUMMARY claims. No truth fell.

## Re-verification after the Codex P2 fix (adddbfe, 202c615)

**Result:** still 5/5, status `passed`. No regression, no new gap. One warning on the STL pairing, below.

The branch was rebased onto the phase-71 tip in the meantime, so note 1 of the first report is closed: `scripts/cad-derive-limits.py` now carries 71's pin change and the 72 commits sit on top. Phase 72 itself still does not touch that file.

### What the fix changes (read in `internal/measurements/measurements.go`, not from the SUMMARY)

- `cadCountRules` checks a null reason against the document's own counts for `step` and `iges`: `no_solids` needs `solids` 0; `open_shell` and `invalid_solid` need `solids` above 0 and `closed_solids` 0; `negative_volume` needs `closed_solids` above 0. This matches the helper's own branches in `scripts/cad-measure/cad_mesh.py` (`closed == 0` selects `no_solids`, `open_shell` or `invalid_solid`; a negative volume with closed solids selects `negative_volume`).
- Volume and area are null together with equal reasons, or numbers together, for every format.
- A positive volume needs extent on every bbox axis; a `step` or `iges` bbox is never a single point. The helper already exits 3 on a zero-size box, so that rule restates a helper refusal.
- Truth 4 is intact: the original rejections (0, NaN, Inf, overflow, out-of-vocabulary, schema) are untouched and their tests pass. The fix only narrows what is accepted.

### Gates re-run

| Check | Result |
|-------|--------|
| `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./...`, `gofmt -l` | clean |
| `go test -count=1 ./...` | every package `ok` |
| `go test -v ./internal/measurements` | 23 tests PASS, including `TestNullReasonAgainstSolidCounts`, `TestAreaNullReasonAgainstSolidCounts`, `TestCodexContradictoryDocuments`, `TestVolumeAndAreaTravelTogether`, `TestPositiveVolumeNeedsExtentOnEveryAxis`, `TestCADBBoxIsNotAPoint`, `TestNullReasonFormatMatrix`, `TestPhase71HelperRowsValidate`, `TestMeasurementsDocNullReasonTableMatchesBehaviour` |
| `npm --prefix frontend run test` | 37 of 37 |
| Doc-sync mutation: `negative_volume` Solid counts cell changed from `closed_solids` > 0 to `solids` = 0 | `TestMeasurementsDocNullReasonTableMatchesBehaviour` fails, naming the contradicted cases; file restored byte-identical |
| Doc-sync mutation: `no_solids` cell changed to `solids` > 0 | fails on an unrecognized cell form; restored |
| Comment scan of the fix's added Go lines | no `//` or `/*` lines added |

### Is any observed real helper combination rejected? No, with caveats

`testdata/helpers-71-04-rows.txt` has 36 rows. Its name set equals the rc=0 mesh, stlstats and dxf rows of `.planning/phases/71-evaluation-gate/evidence/71-04-helpers.txt` (36 each, identical name lists). `TestPhase71HelperRowsValidate` builds a document per row and validates it; all pass. It also asserts that the key combinations (`step null`, `iges no_solids`, `step no_solids`, `stl null`, `stl stl_not_watertight`, four DXF unit cases) are present, so it cannot pass vacuously. Checked against the helper rows: `degenerate_plate.step` (volume 1000, area 20040, 0.1 thick) and `degenerate_rod.step` (volume 157.08, about 2 x 2 x 200) have extent on every axis; `open_shell.step`, `single_face.step` and `surfaces_only.iges` are `no_solids` with solids 0.

Caveats on the strength of that proof:
- The fixture keeps only `bbox_diag`, so the test builds a cube from it. The "extent on every axis" rule is never exercised against a real thin bbox. A positive volume cannot physically have zero extent and both thin real shapes above have it, so the risk is low, but the test does not prove it.
- The one mixed case the helper's code can produce (some solids closed, positive volume, no reason) is accepted; the helper emits no other solid-count combination.

### Judgement on applying volume/area pairing to STL (WARNING, not a blocker)

Facts, from the helper source and a throwaway probe test (deleted):
- `stlstats` in `cad_mesh.py` emits `volume` and `volume_null_reason` and no area at all. No real STL combination ever carried an area.
- The `area` key was already required by the 72-02 validator before this fix, so a helper-shaped STL document (volume, no area key) was already rejected with "area: missing". The fix does not newly make real STL output inexpressible. My first verification did not test this.
- What the fix adds for STL: volume as a number with a null area is rejected, and a null volume with a computed area is rejected. Before, a numeric volume with a null area and `stl_not_watertight` was accepted, which was incoherent because the reason does not describe a watertight mesh. Pairing removes that incoherence.
- The cost: a non-watertight mesh can never report a surface area although a triangle-area sum is well defined for it, and a watertight STL document needs an area that no phase-71 evidence ever measured. `TestPhase71HelperRowsValidate` supplies `stlAreaStandIn = 2200.0` for watertight STL rows, so for STL it proves only that the helper's volume and reason fields are accepted, not that a real STL document validates.
- The doc's rationale for pairing (the producer derives both from the same closed solids) is true for `step` and `iges`, where `cad_mesh.py` sets both in one branch, and is not true for STL. `docs/MEASUREMENTS.md` does not tell the Phase 74 producer that it must compute an STL area.

Call: defensible and fail-closed, and it fits the goal (a stricter contract before any engine exists), so it does not change the status. It is an unevidenced extension of a CAD-only fact. Before Phase 74 plans, either restrict the pairing and equal-reason rule to `step` and `iges` and let STL carry an independent area, or keep it and state in `docs/MEASUREMENTS.md` that the STL producer computes area from triangles for watertight meshes and nulls both otherwise. Either is a small contract decision; leaving it unrecorded risks Phase 74 meeting it as a validator rejection.

### Fingerprint

Refreshed with `verification.fingerprint` (phase directory first, xargs -0). It now also covers `internal/measurements/consistency_test.go` and `internal/measurements/testdata/helpers-71-04-rows.txt`; the changed `measurements.go`, `doc_sync_test.go`, `docs/MEASUREMENTS.md` and the 72-02 plan and summary are re-hashed. 48 files in total.

---

# Initial verification (unchanged, kept for the record)

## Goal Achievement

### Observable Truths (ROADMAP success criteria 1-5, merged with PLAN must-haves)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | `POST /v1/inspect` on step/stp/iges/igs/stl/dxf returns the format detected by content; the same detector feeds create; legacy 8-bit files are not rejected for encoding | VERIFIED | One function, `convert.SniffCADWindows` (`internal/convert/cadsniff.go`), is called from `handleCreateJob` (`handlers.go`, via `ReadCADWindows`) and from `detectInspectFormat` (`inspect_handlers.go`), with the same 4 KiB head and 4 KiB tail. The inspect path takes its head from the 1 MiB window and reads the tail separately only when size exceeds the window; `paddedSTEP` (>1.1 MiB) exercises that branch. `TestCADCreateInspectAgree` runs every committed public, synthetic and reject fixture (52+ files) plus in-memory cases and asserts per file that create and inspect never disagree (accepted-by-both, rejected-by-both with the identical code string, or unrecognized-by-both). It passes. `cadsniff.go` contains no UTF-8 check; `legacy_cp1251.{step,dxf}` and `legacy_latin1.{step,dxf}` are `agree` rows in the golden with the sniffer accepting and both OCP and ezdxf accepting. |
| 2 | Truncated STEP/IGES/DXF, STL valid as both ASCII and binary, and binary DXF are rejected 422 with separate stable codes in `docs/ERROR-CODES.md`; sniffer-vs-reader divergence closed by a differential test; table committed | VERIFIED | `errcode` declares `truncated_cad_file`, `ambiguous_stl`, `binary_dxf_unsupported`, `dwg_unsupported` and lists them in `all`. `docs/ERROR-CODES.md` heading reads "Twenty-one codes" and carries four new rows. `docs/openapi.yaml` `Error.code` enum carries all four, and `InspectReport.format` names each sentinel in its own description. The pre-existing sync tests pass. `TestCADCreateRejections` and `TestSniffCADRejectFixtures` and `TestSniffCADMutations` pass. The divergence table is `.planning/phases/72-engine-free-contract-foundations/evidence/72-sniff-divergence.txt` (69 ROW, 7 DIVERGENCE, `SUMMARY,rows=69,agree=62,later_stage=0,policy=7,violation=0`). The golden `internal/convert/testdata/cad-sniff-golden.tsv` is enforced by `TestSniffCADGolden`. See the differential section below for the spot-check of the 0-violation claim. |
| 3 | `dwg` is recognized by the `AC10xx` signature and rejected by a code distinct from unknown-format | VERIFIED | `SniffCADWindows` checks `AC10` plus two ASCII digits at offset 0 and returns `CADRejectDWG`, mapped to `errcode.DWGUnsupported` (`dwg_unsupported`, 422). Unknown content on a CAD-declared extension returns no verdict and leaves the pre-existing `invalid_request` unrecognized-content path, which is a different code. `TestSniffCADDWG`, `TestCADCreateRejections` (create) and the agreement test (inspect sentinel `dwg_unsupported`) pass. A binary STL whose 80-byte header reads `AC1027` stays `stl` (size-formula precedence, `TestSniffCADBinarySTLHeaderPrecedence`). |
| 4 | Measurements JSON carries versioned `schema`; closed vocabularies documented and checked against code by test; validator rejects `volume: 0`, NaN/Inf and out-of-vocabulary values; role `measurements` accepted by the enum, the CHECK (migration) and `handleGetJob`, which never selects it as `download_url` | VERIFIED | `internal/measurements/measurements.go`: `Schema = "octoconv.measurements/v1"`, vocabularies `units` (7), `unitSources` (3), `nullReasons` (6), `formats` (4). Independent throwaway probe (written, run, deleted) confirmed rejection of: volume 0 with closed=1, volume 0 with closed=0, area 0, `NaN`, `Infinity`, `1e999`, negative, unit `unknown`, unit `yard`, positive volume with 0 closed solids, `unspecified`+`declared`, null with no reason, out-of-vocabulary reason; and acceptance of a valid document and a null+`no_solids` document. `docs/MEASUREMENTS.md` was mutated twice (`micrometre` to `micron`, `stl_not_watertight` to `stl_x`); `doc_sync_test.go` failed both times in the right direction; the file was restored byte-identical. Role: `jobs.OutputRoleMeasurements = "measurements"`; migration `0018_output_role_measurements.sql` drops and re-adds `job_outputs_role_check` with five roles; `handleGetJob` and `setWebhookDownloadURL` both select `download_url` by `o.Role == jobs.OutputRolePrimary` and are unchanged (no diff in those functions), so `measurements` can never be chosen. `TestGetJob_DownloadURLNeverMeasurements` covers measurements sorting before primary and the measurements-only case; `webhook_role_test.go` covers the webhook. Live Postgres 18 run by this verifier: `TestOutputRoleRejectsValueOutsideAllowlist` and the `MarkDone` cases passed, including `AcceptsPrimaryPlusMeasurements` and `RejectsJobWithOnlyMeasurementsOutput`. |
| 5 | Nothing advertised: `AllEngines()` unchanged, no `cad` pair in `/v1/formats`, `stp`/`igs` normalize in Go and in the frontend | VERIFIED | `AllEngines()` has the same nine classes and is not in the phase diff. `internal/convert/converters.go`, `internal/queue`, `cmd/`, compose and chart are untouched by the phase (empty diff stat). `TestDefaultRegistersNoCADPair` and `TestListFormats_AdvertisesNoCADFormat` pass. `NormalizeFormat` has `stp`->`step`, `igs`->`iges` in the new `formatAliases` map; `frontend/src/formats.ts` has the same two cases; `TestFormatAliasesMatchFrontend` parses the TS file and fails on drift either way; frontend vitest passes 37/37. |

**Score:** 5/5 truths verified (0 present, behavior-unverified)

Behavior-dependent truths (create/inspect agreement, never-download_url, fail-closed validation) each have a passing behavioral test that this verifier re-ran; none rest on symbol presence alone.

### Differential Evidence Spot-Check (the 0-violation claim)

- The golden has 69 data rows: public 19, synthetic 18, reject 15, fetched 17. Public + synthetic = 37, matching the phase-71 corpus count, so the reject group is correctly excluded from the 71 walkers (the three 71 scripts (inventory, image-trace, rss-time) gained `! -path '*/reject/*'` and nothing else).
- An awk pass over the golden finds zero public or fetched rows where the sniffer does not accept or the reader does not accept. `policy` column: 62 `agree`, 7 `policy:*` (truncated x3, bom, ambiguous-stl, trailing-nul, dwg), 0 `violation`, 0 `later-stage`.
- All 7 policy rows are `reject/` group files; no real file is policy-labelled.
- The evidence file's ROW name/sha/bytes/sniff columns equal the golden's exactly (diff empty).
- `TestSniffCADGolden` re-ran with 69 subtests PASS and 0 SKIP: all 17 fetched files are present in `~/.cache/octoconv/cad-corpus/fetched` with matching hashes, so the sniffer verdict was recomputed for every row, not only the committed ones. The test fails on a missing or extra committed file, a changed sha256, a verdict change, and a recomputed D-09 violation (read in `cadsniff_golden_test.go`).
- Spot-checked rows: `public/nist_stc_06_asme1_ap242-e3.stp` (1,006,401 bytes) step/ocp-step accepted; `reject/step_truncated.step` sniffer truncated and OCP parse=0 (genuine agreement); `reject/step_trailing_nul.step` sniffer truncated while OCP accepts, labelled `policy:trailing-nul`; `reject/stl_binary_trailing_byte.stl` sniffer truncated and the numpy reader falls back to ASCII and finds no triangles; `reject/dxf_bom.dxf` unrecognized by the sniffer, ezdxf strict fails with OSError and recover accepts, `policy:bom`.
- The reader columns (OCP, ezdxf, numpy) come from the committed run in the 71 image (header names the image id, and `image_tag_before` equals `image_tag_after` = `octoconv-cad-measure:ocp7.9.3.1.1-pruned-dc621b94ee5d`). This verifier did not re-run that image (instructed not to); the sniffer-side columns and hashes were recomputed independently, and the reader-side columns are taken from the committed artifact. Nothing under `scripts/cad-measure/` or `Dockerfile.cad-measure` changed.

### Deferred Items

None. Everything the phase owns is verified; items belonging to Phase 73+ (class registration, engines, `cad` advertisement) are not gaps.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/convert/cadsniff.go` | single CAD detector | VERIFIED | 246 lines, substantive, no comments, wired from both endpoints |
| `internal/convert/testdata/cad/reject/*` | 15 rejection fixtures, committed before rules | VERIFIED | 15 files; commit `142c544` precedes `66dfb6a` (rules) |
| `internal/convert/testdata/cad-sniff-golden.tsv` | golden | VERIFIED | 69 rows, enforced by `TestSniffCADGolden` |
| `.planning/.../evidence/72-sniff-divergence.txt` | divergence table | VERIFIED | equals golden on shared columns |
| `scripts/cad-sniff-differential.sh`, `scripts/cad-sniff-reader.py`, `scripts/cmd/cad-sniff/main.go` | differential driver | VERIFIED | present, build under `go build ./...` |
| `internal/measurements/measurements.go` + tests | versioned fail-closed validator | VERIFIED | 14 tests pass |
| `docs/MEASUREMENTS.md` | contract | VERIFIED | sync test fails on drift in both directions (mutation-proven) |
| `internal/db/migrations/0018_output_role_measurements.sql` | CHECK widening | VERIFIED | applied and exercised on live Postgres 18 |
| `internal/errcode/errcode.go`, `docs/ERROR-CODES.md`, `docs/openapi.yaml` | four 422 codes | VERIFIED | in sync, existing sync tests pass |
| `frontend/src/formats.ts` + Go alias sync test | aliases | VERIFIED | both sides, test-held |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `handlers.go` handleCreateJob | `convert.SniffCADWindows` | `ReadCADWindows` then `SniffCADWindows`, 422 before pair lookup | WIRED | rejection codes map one-to-one to errcode constants |
| `inspect_handlers.go` detectInspectFormat | `convert.SniffCADWindows` | window head plus tail, `cadRejectionCode` map | WIRED | sentinel strings equal the create codes; added to `inspectPublicSentinelFormats` |
| `inspect/bindings.go` RejectBindings | errcode constants | four `ErrCode` entries | WIRED | `TestCADRejectionCodeMapComplete` passes |
| `doc_sync_test.go` | `docs/MEASUREMENTS.md` | table parse | WIRED | mutation-proven |
| `format_alias_sync_test.go` | `frontend/src/formats.ts` | parse | WIRED | |
| `cadsniff_golden_test.go` | golden and corpus | recompute | WIRED | 69/69 |
| `handleGetJob` / `setWebhookDownloadURL` | role selection | `o.Role == OutputRolePrimary` | WIRED | measurements cannot be selected |

### Data-Flow Trace (Level 4)

Not applicable in the render-data sense; the relevant flow is bytes to verdict to HTTP code. For both endpoints the verdict derives from file bytes (`ReadAt` windows), not a static value. The agreement test over real fixtures proves the flow end to end.

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| build, vet | `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./...` | clean | PASS |
| full Go suite | `go test ./...` | every package `ok` | PASS |
| gofmt | `gofmt -l internal scripts cmd` | empty | PASS |
| frontend | `npm --prefix frontend run test` | 4 files, 37 tests pass | PASS |
| golden | `go test -count=1 -v -run TestSniffCADGolden ./internal/convert` | 69 PASS, 0 SKIP | PASS |
| CAD create/inspect | `go test -run 'CAD|Cad' ./internal/convert ./internal/api` | 18 tests PASS | PASS |
| live DB | compose `postgres` started, `go run ./cmd/migrate`, `go test -count=1 ./internal/jobs ./internal/db ./internal/api ./internal/worker` with `DATABASE_URL` set | all `ok`, role and `MarkDone` tests ran and passed (not skipped) | PASS |
| validator probe | temp test file, 15 cases, deleted afterwards | all as expected | PASS |

The compose postgres was stopped again afterwards (it was stopped before). `go run ./cmd/migrate` reported "migrations applied" on the local dev database volume, which is idempotent and is the only state change.

### Probe Execution

Step 7c: the phase declares `scripts/cad-sniff-differential.sh` as a probe-like driver. It runs the 71 measurement image, which this verification was told not to run; its committed output was verified instead (above). No `scripts/*/tests/probe-*.sh` exist for this phase. Status: SKIPPED by instruction, committed evidence cross-checked.

### Requirements Coverage

PLAN frontmatter requirement IDs: 72-01 CADIN-01, CADIN-02; 72-02 MEAS-05; 72-03 MEAS-05; 72-04 CADIN-02, CADIN-03; 72-05 CADIN-01; 72-06 CADIN-01, CADIN-02, CADIN-03; 72-07 CADIN-02. Union is exactly CADIN-01, CADIN-02, CADIN-03, MEAS-05, which equals the four IDs REQUIREMENTS.md maps to Phase 72 (traceability rows 107-110). No orphaned requirement.

| Requirement | Source Plans | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| CADIN-01 | 72-01, 72-05, 72-06 | content recognition, one logic on both endpoints, legacy 8-bit accepted | SATISFIED | truths 1 and 5 |
| CADIN-02 | 72-01, 72-04, 72-06, 72-07 | truncated / ambiguous STL / binary DXF rejected with separate codes; differential closes sniffer-vs-reader | SATISFIED | truth 2 |
| CADIN-03 | 72-04, 72-06 | dwg by signature, distinct code | SATISFIED | truth 3 |
| MEAS-05 | 72-02, 72-03 | versioned schema, closed vocabularies, synced by test | SATISFIED | truth 4 |

`.planning/REQUIREMENTS.md` still shows these four as `[ ]` / `Pending`. That is a tracking update for the orchestrator, not a code gap.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| (phase-modified Go, SQL, TS, py, sh) | - | TBD / FIXME / XXX debt markers | none | grep of the added lines found none |
| `internal/convert/cadsniff.go` | - | stubs, empty returns | none | every `return CADResult{}` is the deliberate "no verdict" outcome |

Code-comment rule (user CLAUDE.md: no comments except external constraints): scanned every added line in `*.go`, `*.ts`, `*.sql`, `*.sh`, `*.py` (`git diff origin/gsd/phase-71-evaluation-gate...HEAD -U0`). No new narrative comment was added in `cadsniff.go`, `cadsniff_*_test.go`, `cad_sniff_test.go`, `measurements*.go`, `0018_*.sql`, `cad-sniff-*.{sh,py}`, `scripts/cmd/cad-sniff/main.go`, or the handler additions. The only comment text in the diff is edits and moves of comments that already existed: the `formats.ts` doc comment (two aliases appended and a stale line reference replaced), the `tex` alias comment moved verbatim into the `formatAliases` map in `convert.go`, the `errcode.go` count comment ("seventeen" to "twenty-one") and one `output_role_test.go` comment. No bypass found.

### Human Verification Required

None. Everything the phase claims is checkable by tests or committed evidence, and the checks that could run here ran.

### Notes (non-blocking, for the orchestrator)

1. **Branch base is behind the phase-71 tip.** `git merge-base HEAD origin/gsd/phase-71-evaluation-gate` is `74ba6f7`; the 71 branch has three newer commits (`655f863`, `56bc497`, `4186be9`: limits deriver pin refusal and re-verify). A two-dot diff against the 71 tip therefore shows phantom reversals in `scripts/cad-derive-limits.py`; the three-dot diff (the real phase delta) does not touch that file. The two sides share no changed file (`comm` of the two file lists is empty), so a rebase should be clean, but the branch should be rebased onto the 71 tip before the PR.
2. **DXF with a UTF-8 BOM is unrecognized** (`reject/dxf_bom.dxf`, ezdxf recover accepts, strict raises OSError). It is recorded as `policy:bom`, consistent with the STEP BOM row where OCP also rejects, and the golden test allows it only on a `reject/` row. If a real exporter ever emits BOM-prefixed DXF it would get `invalid_request`; no such file exists in the 36 real files checked. It is a deliberate, documented fail-closed choice, not a violation.
3. The sniffer reads a fixed 4 KiB head and tail; STEP header detection (`HEADER;`) is limited to that head. A real STEP with a header section longer than 4 KiB would be classified truncated. All 17 fetched real exports (up to 52 MB) and the 19 public files pass, so the risk is theoretical.
4. `REQUIREMENTS.md` and `ROADMAP.md` checkbox state were not touched, as instructed.

### Gaps Summary

No gaps. The riskiest review surface (the CAD detector, its four rejection codes, the differential golden and the measurements contract) exists, is wired to both endpoints, is held equal to its documentation by drift-failing tests, and is proven with tests that were re-run here, including a live Postgres run. No engine is registered, `AllEngines()` and `/v1/formats` are unchanged, and the aliases exist in both Go and the frontend.

---

_Verified: 2026-10-05T07:20:00Z_
_Verifier: Claude (gsd-verifier)_
