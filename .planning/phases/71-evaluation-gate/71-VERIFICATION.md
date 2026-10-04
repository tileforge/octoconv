---
phase: 71-evaluation-gate
verified: 2026-10-04T11:52:59Z
status: passed
score: 7/7 must-haves verified
covered_files:
  - .planning/phases/71-evaluation-gate/71-01-PLAN.md
  - .planning/phases/71-evaluation-gate/71-01-SUMMARY.md
  - .planning/phases/71-evaluation-gate/71-02-PLAN.md
  - .planning/phases/71-evaluation-gate/71-02-SUMMARY.md
  - .planning/phases/71-evaluation-gate/71-03-PLAN.md
  - .planning/phases/71-evaluation-gate/71-03-SUMMARY.md
  - .planning/phases/71-evaluation-gate/71-04-PLAN.md
  - .planning/phases/71-evaluation-gate/71-04-SUMMARY.md
  - .planning/phases/71-evaluation-gate/71-05-PLAN.md
  - .planning/phases/71-evaluation-gate/71-05-SUMMARY.md
  - .planning/phases/71-evaluation-gate/71-06-PLAN.md
  - .planning/phases/71-evaluation-gate/71-06-SUMMARY.md
  - .planning/phases/71-evaluation-gate/71-07-PLAN.md
  - .planning/phases/71-evaluation-gate/71-07-SUMMARY.md
  - .planning/phases/71-evaluation-gate/71-08-PLAN.md
  - .planning/phases/71-evaluation-gate/71-08-SUMMARY.md
  - .planning/phases/71-evaluation-gate/71-CONTEXT.md
  - .planning/phases/71-evaluation-gate/71-DECISION.md
  - .planning/phases/71-evaluation-gate/evidence/71-01-decisions.txt
  - .planning/phases/71-evaluation-gate/evidence/71-01-lock.txt
  - .planning/phases/71-evaluation-gate/evidence/71-02-tracer.txt
  - .planning/phases/71-evaluation-gate/evidence/71-03-corpus-inventory.txt
  - .planning/phases/71-evaluation-gate/evidence/71-04-helpers.txt
  - .planning/phases/71-evaluation-gate/evidence/71-04-render.txt
  - .planning/phases/71-evaluation-gate/evidence/71-05-f3d.txt
  - .planning/phases/71-evaluation-gate/evidence/71-06-go-worker-rss.txt
  - .planning/phases/71-evaluation-gate/evidence/71-06-ladder-1g.txt
  - .planning/phases/71-evaluation-gate/evidence/71-06-ladder-2g.txt
  - .planning/phases/71-evaluation-gate/evidence/71-06-ladder-4g-ocp7.9.3.1.1.txt
  - .planning/phases/71-evaluation-gate/evidence/71-06-ladder-4g-ocp8.0.1.0.0.txt
  - .planning/phases/71-evaluation-gate/evidence/71-06-limits.txt
  - .planning/phases/71-evaluation-gate/evidence/71-06-quality.txt
  - .planning/phases/71-evaluation-gate/evidence/71-06-render-stacking.txt
  - .planning/phases/71-evaluation-gate/evidence/71-06-threads.txt
  - .planning/phases/71-evaluation-gate/evidence/71-07-landlock.txt
  - .planning/phases/71-evaluation-gate/evidence/71-07-libtrace.txt
  - .planning/phases/71-evaluation-gate/evidence/71-07-licences.txt
  - .planning/phases/71-evaluation-gate/evidence/71-07-prune.txt
  - .planning/phases/71-evaluation-gate/evidence/71-08-image-size.txt
  - .planning/phases/71-evaluation-gate/evidence/71-08-no-wiring.txt
  - Dockerfile.cad-measure
  - internal/convert/testdata/cad/PROVENANCE.md
  - scripts/cad-corpus-fetch.sh
  - scripts/cad-corpus-gen.py
  - scripts/cad-corpus-inventory.sh
  - scripts/cad-corpus-manifest.tsv
  - scripts/cad-derive-limits.py
  - scripts/cad-f3d-yardstick.sh
  - scripts/cad-go-worker-rss.sh
  - scripts/cad-image-size-measure.sh
  - scripts/cad-image-trace.sh
  - scripts/cad-measure/cad_dxf.py
  - scripts/cad-measure/cad_mesh.py
  - scripts/cad-measure/cad_render.py
  - scripts/cad-measure/common.sh
  - scripts/cad-measure/contact_sheet.py
  - scripts/cad-measure/counters.py
  - scripts/cad-measure/lock.sh
  - scripts/cad-measure/measure_wrap.py
  - scripts/cad-measure/pixelcheck.py
  - scripts/cad-measure/requirements-common.txt
  - scripts/cad-measure/requirements-ocp-7.9.3.1.1.txt
  - scripts/cad-measure/requirements-ocp-8.0.1.0.0.txt
  - scripts/cad-measure/requirements-vtk.txt
  - scripts/cad-measure/vtk-keeplist.txt
  - scripts/cad-measure/vtk_keeplist.py
  - scripts/cad-render-measure.sh
  - scripts/cad-render-stacking-probe.py
  - scripts/cad-rss-time-measure.sh
  - scripts/cad-sandbox-measure.sh
covered_digest: "v2:sha256:ab3975db4a0cfe9376900acfd2d66b8b20a7c69971f0df4166fc7820cc2b7057"
behavior_unverified: 0
overrides_applied: 0
re_verification: false
gaps: []
residual_non_blocking:
  - item: "Legal OK for libfreeimage / libraw / libjasper (GPL/LGPL/CDDL class) is NOT obtained"
    recorded_as: "evidence/71-07-licences.txt#LAWYER_OK (needed, blocking=no); DECISION section Licences; assumption A2"
    effect: "Criterion 4 satisfied as an engineering record (D-18, user-locked); not satisfied as legal clearance"
  - item: "Private PreviewGenerator flow not measured (files=0)"
  - item: "amd64 not measured; f3d not measurable natively on arm64"
  - item: "Ceilings for STL binary / STL ASCII / DXF are extrapolated above the top measured rung; STEP ceiling 73 MiB sits at 0.91 of the render-time cliff (80 MiB)"
  - item: "RSS ladder was run on the full image variant; final variant is pruned (RSS on pruned not re-measured)"
---

# Phase 71: Evaluation Gate Verification Report

**Phase Goal:** Выбор ядра геометрии, рендерера и 2D-пути DXF сделан по замеру на настоящем образе `debian:bookworm-slim` на нативной архитектуре, а не по прототипу; числа, от которых зависит всё остальное (RSS, время, размер образа, лимиты входа, лицензии), известны до того, как написана строка продакшен-кода.
**Verified:** 2026-10-04
**Status:** passed (with the non-blocking residuals listed above; see "Criterion 4 judgement")
**Re-verification:** No - initial verification
**Branch / head:** gsd/phase-71-evaluation-gate @ 9e98966; base for criterion 5: 4e595c0

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Core (OCP wheel), renderer (VTK + OSMesa vs f3d vs own rasteriser) and DXF 2D path are decided from measurement on the real bookworm-slim image, native arch, under Landlock, with pixel-content judgement on real parts | VERIFIED | `71-DECISION.md` sections Core / Renderer / DXF path. Native arm64, kernel 7.0.14-orbstack, base `debian:bookworm-slim@sha256:3783cc...` (`71-01-lock.txt#BASE_IMAGE`, `Dockerfile.cad-measure:3`). `71-04-render.txt#RENDER_SUMMARY parts=5,pngs=108,fail=0,views_differ=4,determinism=identical`; `CALOBS real_pngs=35`; `VIEWS` min_pair_iou 0.11-0.50 on 4 asymmetric parts (iso/front/top differ). Landlock: `71-07-landlock.txt#LANDLOCK_SUMMARY helpers=4,inputs=34,sufficient=34,different=0,leaked=0` (34 `set=candidate` SUFFICIENT rows, all `content=identical`; 20 identical cad_render rows with pixelcheck). DXF: `71-04-helpers.txt#SUMMARY rows=59,failed=1` (the one failure is explained: 3dsolids.dxf has nothing drawable). User approval of renderer recorded (`71-05-SUMMARY.md` User acceptance). Own rasteriser not built; D-16 table shows each failure condition did not occur. Caveat W3 below. |
| 2 | Repo has a corpus (or checksummed fetch script) with licence per file: STEP assemblies, surfaces-only IGES, large ASCII and binary STL, DXF with hatches/blocks/dimensions, legacy 8-bit | VERIFIED | 37 committed files in `internal/convert/testdata/cad/` (12 dxf, 2 iges, 19 step/stp, 4 stl); every one has its sha256 in `PROVENANCE.md` (I re-hashed all 37: 0 missing) and a `Licence:` line; 17 fetched files pinned in `scripts/cad-corpus-manifest.tsv` (9 columns: sha256, licence, author, source), `bash scripts/cad-corpus-fetch.sh --check` -> `corpus fetch ok: 17 files`; `python3 scripts/cad-corpus-gen.py --check-only` -> self-check passed. Coverage table in `PROVENANCE.md` maps every category; non-real categories are labelled (large ASCII STL = derived/synthetic, cp1251/Latin-1 = synthetic, MINSERT = synthetic) and the gaps are listed under "Not found in open form". Committed size 7038250 B total incl. PROVENANCE.md (matches DECISION), max file 1404884 B. |
| 3 | At `--cpus=2`: peak RSS and time, image size, cold pull recorded; input limits derived from measurement with the derivation shown, not copied from PreviewGenerator | VERIFIED | Ladders `71-06-ladder-{1g,2g,4g-ocp*}.txt` all `HDR,cpus=2`. `71-06-limits.txt#CEILING/LIMIT` show the formula `(0.70*L-floor-go)/k_worst` with numbers; regenerating with `python3 scripts/cad-derive-limits.py evidence --pin 7.9.3.1.1` reproduces the committed `71-06-limits.txt` byte-for-byte (diff empty). 40/5/60/500 are tabled in DECISION as "ориентир PreviewGenerator", not sources (`YARDSTICK`). Image: `71-08-image-size.txt#IMAGE_SIZE` pruned 1083.0 MiB / 299.3 compressed / 6 layers, full 1358.3 / 366.7; `COLD_PULL_DIST` pruned n=3 median 6.38 s (fresh dind per pull, overlayfs); network time labelled "рассчитано". Go worker RSS measured (24.9 MiB), replacing the 60 MiB assumption. Extrapolated ceilings are flagged as such in DECISION. |
| 4 | Licences of libraries carried by the OCP wheel (libfreeimage, libraw, libjasper) checked for internal use and decision recorded | VERIFIED as an engineering record; NOT legal clearance (see judgement below) | `71-07-libtrace.txt#D18_TRACE` (freeimage/libraw loaded at `import OCP` on both pins; libjasper only on 8.0.1.0.0; 7.9.3.1.1 `libjasper_ops=0`); `71-07-licences.txt`: 23 `LICENCE` rows, 23 VERIFIED, 0 ASSUMED; `D18_OUTCOME choice=record`; `RECORD use=только внутреннее использование...`; `LAWYER_OK=needed,blocking=no`. User decision relayed in `71-01-decisions.txt` and `71-01-SUMMARY.md` ("D-18 outcome: record"). Matches CONTEXT D-18, which itself says the lawyer flag "фазу не блокирует". |
| 5 | No production wiring: `AllEngines()`, `/v1/formats`, `/v1/inspect`, queue and chart unchanged; phase leaves only decision records, measurement scripts, measurement Dockerfile and corpus | VERIFIED | `git diff --name-status 4e595c0 HEAD`: every added file outside `.planning/` is under `scripts/` (28 files), `Dockerfile.cad-measure`, or `internal/convert/testdata/cad/` (38 files); the only `M` entries are `.planning/ROADMAP.md` and `.planning/STATE.md`. Zero changes in `cmd/`, `deploy/`, `frontend/`, `docs/`, `README.md`, `go.mod`, `go.sum`, `docker-compose*.yml`, `.github`, production Dockerfiles, and no `.go` file added under `internal/` (extensions added: dxf/iges/md/step/stl/stp only). Existing scripts untouched (no `M`/`D`/`R`). I re-ran with `GOTOOLCHAIN=go1.26.4`: `go build ./...` + `go vet ./...` OK; guard tests (AllEngines, README classes, ListFormats, Pre57/Pre58 golden, InspectOperationSpec, InspectSentinel, ConvertPayloadRoundTrip) pass; full `go test ./internal/convert/...` passes (68.8 s). |
| 6 | Every number in `71-DECISION.md` traces to an evidence line | VERIFIED | The 30 `DECISION,` summary lines were compared with `71-06-limits.txt` (PIN, CEILING x5, CAP x4, THREADS, QUALITY x3, MEMORY_LIMIT, PRIVATE_CORPUS), `71-08-image-size.txt` (IMAGE_SIZE, COLD_PULL_DIST), `71-07-licences.txt`, `71-04-render.txt`; all values match (e.g. ceilings 73/137/278/1363/217, caps 4266184/759890/2042/172677, STEP substitution `(0.70*4096-479.0-24.9)/32.115=73.6`). Spot-checked prose numbers: PIN_FORMULA 27.305 vs 22.144 (18.9%), 2449.73 vs 2304.32 s (5.9%); STACKING 61.57 s vs 1.18 s, 214128 triangles; PRUNE 564.1->288.7 MiB, 196/196 PNG identical, 48/48 other rows; FLEET largest 733.0 MiB; LANDLOCK 34/34. Not-measured items are labelled as such. |
| 7 | User decisions are recorded consistently (lock approval, D-18, libssl3 for f3d only, renderer approval, OCP pin 7.9.3.1.1 over formula 8.0.1.0.0) | VERIFIED | Lock approval: `71-01-decisions.txt` Task 2 "approved" + `71-01-SUMMARY.md:62`. D-18 `record`: `71-01-decisions.txt`, `71-01-SUMMARY.md:63-65`, `71-07-licences.txt#D18_OUTCOME`, DECISION summary `d18_outcome=record`. libssl3: `scripts/cad-f3d-yardstick.sh:80,136` and `71-05-f3d.txt#APPROVAL` (`not_in=Dockerfile.cad-measure and production images`); grep confirms `libssl` appears nowhere in `Dockerfile.cad-measure` or `scripts/cad-measure/`. Renderer approval: `71-05-SUMMARY.md` "User acceptance (D-11 step 2)" + DECISION summary. OCP pin: `71-06-limits.txt#PIN_DECISION choice=7.9.3.1.1 ... user decision overrides the formula result 8.0.1.0.0` kept next to `PIN_FORMULA choice=8.0.1.0.0`; `71-06-SUMMARY.md:58,69`; `Dockerfile.cad-measure:4 ARG OCP_VERSION=7.9.3.1.1`; lock `requirements-ocp-7.9.3.1.1.txt`; all Landlock/prune/image-size HDR lines carry `ocp=7.9.3.1.1`. No file contradicts another. All user answers reached the executor via the coordinator relay, which DECISION and the SUMMARYs disclose. |

**Score:** 7/7 truths verified (0 present-but-behavior-unverified; none of these truths is a state-transition or cancellation invariant, they are measurement records checked against their evidence lines).

### Criterion 4 judgement (asked explicitly)

The wording is "проверены для внутреннего использования и решение записано". Two readings:

- Engineering reading (the one the user locked in CONTEXT D-18): libraries are identified, licences read from upstream text, the use is bounded to "internal only, image not distributed", the decision (`record`, keep the libraries, no patchelf) is written, and "нужен OK юриста" is a flag that explicitly does not block the phase. Under this reading the criterion is met: 23/23 licence rows VERIFIED against upstream, D-18 trace measured, outcome recorded in four consistent places.
- Legal-clearance reading: not met. FreeImage (GPL-2.0/3.0/FIPL), LibRaw (LGPL-2.1/CDDL), jbigkit (GPL-2.0) and libgomp (GPL-3.0 + exception) are copyleft-class; assumption A2 (internal use without distribution triggers no obligations) is a working assumption, not a legal finding, and the lawyer OK is `needed`. The patchelf experiment was not run, so "loaded" is not shown to mean "unused" (A1).

Verdict: the criterion is satisfied as written and as scoped by D-18, and the goal ("лицензии ... известны до того, как написана строка продакшен-кода") is met in the sense that the licence picture is known and the risk is explicit. It does not make the libraries legally cleared, and the recorded "нужен OK юриста" must be treated as an open follow-up before any production image is built or distributed. It is not placed in `human_verification` because it is a deliberately non-blocking external action, not a check the verifier could not perform; if the maintainer reads criterion 4 as legal clearance, the correct status would be `human_needed` on that single item.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `71-DECISION.md` | Decision record with evidence refs | VERIFIED | 571 lines; machine-readable summary + sections; coverage table for criterion/D-xx/EVAL ids |
| `evidence/71-0{1..8}-*.txt` (27 files incl. PNG contact sheets) | Primary evidence | VERIFIED | All referenced `#LABEL` lines found |
| `Dockerfile.cad-measure` | Measurement image, digest-pinned base, `USER nobody`, stages venv/venv-pruned/venv-selected | VERIFIED | Not referenced by compose/CI/chart |
| `scripts/cad-*.sh/.py`, `scripts/cad-measure/*` | Measurement scripts | VERIFIED | `bash -n` and `py_compile` clean on all; `cad-derive-limits.py` reproduces limits evidence |
| `internal/convert/testdata/cad/` + `PROVENANCE.md` | Corpus with licences | VERIFIED | see truth 2 |
| `scripts/cad-corpus-manifest.tsv` + `cad-corpus-fetch.sh` | Checksummed fetch | VERIFIED | `--check` OK for 17 files |
| `scripts/cad-measure/requirements-*.txt` | Hash-pinned locks | VERIFIED | `--require-hashes`; user approval recorded |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| DECISION summary lines | `71-06-limits.txt` / `71-08-image-size.txt` / `71-07-licences.txt` | `evidence=file#LABEL` | WIRED | values equal |
| `71-06-limits.txt` | raw ladders | `cad-derive-limits.py` | WIRED | regeneration is identical |
| `Dockerfile.cad-measure` | lock files | `OCP_VERSION` build arg | WIRED | 7.9.3.1.1 default |
| keep-list | pruned image | `vtk_keeplist.py prune` | WIRED | `PRUNE_IDENTITY 196/196`, stale-list test fails as designed |

### Data-Flow Trace (Level 4)

Not applicable: no rendered dynamic data; the artifacts are evidence files and scripts. The only data-flow property that matters (limits derived from measured rows, not literals) is covered by the regeneration check.

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Production still builds/vets | `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./...` | clean | PASS |
| Wiring guard tests | `go test ./internal/{convert,api,queue} -run 'AllEngines|READMEEngineClasses|ListFormats|Pre57Golden|Pre58Golden|InspectOperationSpec|InspectSentinel|ConvertPayloadRoundTrip'` | ok x3 | PASS |
| Whole convert package | `go test ./internal/convert/... -count=1` | ok 68.8 s | PASS |
| Synthetic generator self-check | `python3 scripts/cad-corpus-gen.py --check-only` | self-check passed | PASS |
| Fetched corpus checksums | `bash scripts/cad-corpus-fetch.sh --check` | 17 files ok | PASS |
| Limits derivation | `python3 scripts/cad-derive-limits.py evidence --pin 7.9.3.1.1` vs committed | identical | PASS |
| Committed corpus hashes | sha256 of 37 files vs `PROVENANCE.md` | 0 mismatch | PASS |
| Multi-hour measurements | not re-run (per instruction) | - | SKIP |

### Probe Execution

No `probe-*.sh` declared by the phase. SKIPPED.

### Requirements Coverage

| Requirement | Source Plans | Description | Status | Evidence |
|-------------|--------------|-------------|--------|----------|
| EVAL-01 | 71-01, 02, 04, 05, 06, 07, 08 | core / renderer / DXF path chosen by measurement, native, Landlock, pixel content | SATISFIED | truth 1; amd64 not measured (declared out of scope by phase 70 D-03/D-05) |
| EVAL-02 | 71-02, 03, 08 | corpus with licences | SATISFIED | truth 2 |
| EVAL-03 | 71-03, 06, 08 | RSS/time at cpus=2, image size, cold pull, derived limits | SATISFIED | truth 3 |
| EVAL-04 | 71-01, 07, 08 | licences checked for internal use, decision recorded | SATISFIED as engineering record; legal OK open | truth 4 + judgement |

All four IDs appear in PLAN frontmatter and in REQUIREMENTS.md (traceability table maps EVAL-01..04 to Phase 71 only). No orphaned requirements. REQUIREMENTS.md still shows them `Pending` / unchecked; updating that is the orchestrator's step, not a gap in the phase.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| scripts/cad-*, Dockerfile.cad-measure, PROVENANCE.md, DECISION | - | `TBD`/`FIXME`/`XXX` (word match, excluding `mktemp XXXXXX`) | none found | no debt markers |

### Warnings (non-blocking, all disclosed in DECISION unless noted)

- W1. Ladder RSS/time was measured on the `full` image variant (`HDR,image=...-full-...` in all 4g/2g/1g ladders); the shipped candidate is `pruned`. Pruning removes only never-mapped VTK modules and PNG output is byte-identical (196/196), so RSS impact should be nil, but it was not re-measured and DECISION does not say so. Phase 76 should confirm on the pruned image.
- W2. Ceilings 278 / 1363 / 217 MiB (STL binary / STL ASCII / DXF) lie above the highest measured rung (196 / 199 / 197 MiB); DXF at 197 MiB already takes 139.6 s. STEP 73 MiB is 0.91 of the stacked-copies render cliff (80 MiB; fallback proposal 64 MiB). Downstream phases must treat these as candidates, not guarantees.
- W3. Success criterion 1 says "OCP wheel versus fallback options". The record compares only the two OCP pins; no alternative geometry core was measured, and DECISION does not state in one sentence that none was needed because OCP met every gate. The conclusion is defensible (nothing failed), the sentence is missing.
- W4. Private PreviewGenerator flow: `files=0`; limits/caps/time are not validated against real company files ("не замерено", not "чисто").
- W5. amd64 and native f3d not measured (f3d has no linux-arm64 build); image size/cold pull are arm64 only.
- W6. Legal OK open; assumptions A1, A2, A5, A8 remain assumptions (patchelf experiment not run).
- W7. Render of stacked identical copies takes 61.57 s vs 1.18 s for the same triangle count; no cap stops it; handed to phase 76 explicitly.
- W8. Pod memory 4096 MiB deviates from fleet 1Gi/2Gi; recorded with derivation (D-06).
- W9. Renderer approval and lock approval reached executors through the coordinator relay rather than directly; disclosed in `71-05-SUMMARY.md` and DECISION.

### Human Verification Required

None required for phase closure. Recommended follow-ups outside verification: legal review of libfreeimage/libraw/jbigkit/libgomp before any image leaves the company or ships; re-measure RSS on the pruned image in phase 76.

### Gaps Summary

No gaps. Every roadmap success criterion is backed by evidence that exists in the repository and was cross-checked against the scripts or the primary evidence rows; production wiring against base 4e595c0 is empty; user decisions are recorded consistently across evidence, SUMMARYs and DECISION.

---

_Verified: 2026-10-04_
_Verifier: Claude (gsd-verifier)_
