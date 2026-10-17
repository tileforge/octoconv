---
phase: quick-261017-doa
plan: 01
subsystem: keda-cad-live-proofs
tags: [keda, loadproof, quiet-host, contamination, docs, near-white, codex-round-2, pr-37, phase-76-follow-up]
status: complete
requires: [scripts/keda-cad-loadproof.sh (76-09), scripts/keda-cad-downscale-survival.sh (76-10), docs/CAD.md Known limits, TestCADDocMatchesTheCode]
provides: [quiet_line status in the loadproof, FAIL on a contaminated calibration or timed step, the 76-09 loadproof stub suite, NOTE key=quiet_calibration in the downscale evidence, near_white all-channel wording, near_white_rule subtest, the contamination sweep of seven scripts]
affects: [scripts/keda-cad-loadproof.sh, scripts/keda-cad-downscale-survival.sh, docs/CAD.md, internal/convert/cad_doc_test.go]
tech-stack:
  added: []
  patterns: [a recorded contamination flag is also an exit status, and every caller acts on it; a doc example is probed through every judge that applies the rule]
key-files:
  created:
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-09-keda-loadproof-test.sh
    - .planning/todos/pending/2026-10-17-selection-steps-ignore-load-clean.md
    - .planning/quick/261017-doa-codex-round-2-contaminated-loadproof-tim/evidence/
  modified:
    - scripts/keda-cad-loadproof.sh
    - scripts/keda-cad-downscale-survival.sh
    - docs/CAD.md
    - internal/convert/cad_doc_test.go
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/76-09-SUMMARY.md
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/76-10-SUMMARY.md
decisions:
  - "A contaminated keda-cad-loadproof step is FAIL (exit 1, FAIL,step=..., SUMMARY result=FAIL), not a new INCOMPLETE; only a later clean run counts"
  - "The loadproof calibration step gates as well as the timed step, because docs/CAD.md quotes its submit_to_ready_s"
  - "The downscale calibration stays non-gating. No file under docs/, internal/ or scripts/ reads a 76-10 calibration timing, and its checks are upper bounds that load only makes harder to pass. A contaminated calibration now writes NOTE key=quiet_calibration gates=0 into the evidence, and RULE quiet_host pre-registers this"
  - "near_white states the rule cadMeasure applies: background only when every channel, laid over white, is at or above 250; one channel below is ink"
  - "Two selection steps that read load-contaminated rows without a stub harness are recorded as not fixed (todo). Both err toward a FAIL, never a false PASS"
metrics:
  completed: 2026-10-17
  duration: "about 07:00 to 07:45 UTC"
actuals:
  tokens: 16400
  tasks: 2
  commits: 4
plan_head_before: da51d711dc06479d7d9c30890a74cb349a73db2e
plan_head_after: 36d6aba9252e465d1bc7bd0c23332ceb09147d8f
commits_list:
  - 028cd81 test(quick-261017-doa): RED a contaminated loadproof step must not reach PASS
  - f4039cb fix(quick-261017-doa): a contaminated loadproof step fails the proof
  - 3e0cd29 test(quick-261017-doa): RED the near_white row must state the cutoff rule the code applies
  - 36d6aba docs(quick-261017-doa): near_white states the all-channel background rule
---

# Quick 261017-doa: a contaminated loadproof timing cannot report PASS; near_white states the real cutoff rule Summary

**Complete, against stubs only.** In `scripts/keda-cad-loadproof.sh`, a calibration or timed step with load1
above 11 at its start or its end now ends `FAIL,step=...`, `SUMMARY result=FAIL`, exit 1. Before, it recorded
`contaminated=1` and still ended `PASS`. The `near_white` row of docs/CAD.md now states the rule the code
applies, and a subtest probes the row's own examples through cadMeasure and all three exported judges.

## The findings and the fixes

1. **Loadproof (P2).** `quiet_line` ended with `ev`, so it returned 0. The calibration call (old line 572) and
   the timed call (old line 684) ignored it, and the run reached `GATE_OK=1` and `SUMMARY result=PASS`. That
   contradicted RULE `quiet_host` ("its timings do not count"), and docs/CAD.md line 161 quotes the
   calibration's `submit_to_ready_s`.
   - Fix: `quiet_line` ends with `[ "$c" = 0 ]`, the same as the downscale script.
   - Both calls are followed by `|| fail "the <step> step is contaminated (load1 <start> at its start, <end> at its end, above 11); its timings do not count"`.
   - RULE `quiet_host` now adds "it fails the proof; only a later clean run counts".
2. **Downscale, verdict kept.** Its `quiet_line` already returned the status, and LONG already failed on it
   (line 979). The calibration call (line 731) was non-gating through `|| echo`, which reached only the
   console.
   - It now writes `NOTE,key=quiet_calibration,step=calibration,contaminated=1,gates=0,reason=...` into the
     evidence file.
   - RULE `quiet_host` (line 407) pre-registers the exemption and gives its reason.
   - The old text "the calibration checks are not timings" was wrong, and it is gone.
   - The consumer check is in evidence/downscale-consumers.txt: no file under docs/, internal/ or scripts/
     reads a 76-10 calibration timing. The full run reads only the calibration file's SUMMARY (line 66) and
     its LONG_SELECT (line 485). 76-10-SUMMARY.md narrates the two calibration runs' timings, and both have
     QUIET `contaminated=0`.
   - The NOTE branch is checked by code reading only, because this script has no harness. It sits on the
     right-hand side of a `||` whose status was already discarded, so it cannot turn a FAIL into a PASS.
   - The file was touched, so its `-S warning` findings are cleared without any change in behaviour:
     `"done"` is quoted at lines 882 and 907 (SC1010), and the unused `L_FMT` becomes `_` (SC2034, no other
     use).
3. **near_white (P2).** The row said paint with any channel at or above the cutoff is no ink. cadMeasure
   (cadpixel.go line 226) skips a pixel only when all three channels, laid over white, are at or above 250.
   The row now reads: "Paint that, laid over a white page, has every channel at or above the cutoff is no
   ink; one channel below it is ink, so (249, 255, 255) is ink and (250, 250, 250) is not. The 3D PNG check
   and the DXF checks apply the same rule".

## TDD

| Suite | Base (RED) | GREEN | Evidence |
|---|---|---|---|
| 76-09-keda-loadproof-test.sh (clean, hot calibration, hot timed) | 23 passed, 11 failed. Clean passes 16/16; both hot cases fail every verdict check, and they end SUMMARY PASS with exit 0 | 34 passed, 0 failed. No base PASS name is missing | evidence/base-loadproof.txt, evidence/green-loadproof.txt |
| `go test ./internal/convert -run TestCADDoc -count=1` | only `near_white_rule` fails (old wording; 0 examples) | all pass | evidence/red-doc.txt, evidence/green-doc.txt |
| `go test ./internal/convert -count=1` (go1.26.4, Python 3.14.8) | | ok, 386.7 s | evidence/convert-package.txt |
| 76-11-final-gate-test.sh (final-live-gate.sh unchanged, rerun anyway) | | 240 passed, 0 failed | not saved; console only |

How the stub suite works:
- It runs the real script with `PATH="$T/bin:$PATH"`. It aborts unless kubectl, helm, docker, orbctl, curl,
  go, lsof, sysctl and sleep all resolve to `$T/bin`.
- It asserts that the kubectl, helm, docker, curl and sysctl stubs answered, and that no stub call was left
  unhandled.
- The hot knobs work through sysctl `vm.loadavg`: `hot_once` for the calibration end, `hot_on` from the last
  timed GET onward.

## Mutations

| # | Mutation | Result | File |
|---|---|---|---|
| M1 | drop `[ "$c" = 0 ]` from `quiet_line` | 23 passed, 11 failed (both hot cases) | evidence/mutations.txt |
| M2 | calibration `\|\| fail ...` to `\|\| true` | 28 passed, 6 failed (hot calibration) | evidence/mutations.txt |
| M3 | timed `\|\| fail ...` to `\|\| true` | 29 passed, 5 failed (hot timed) | evidence/mutations.txt |
| D1 | near_white text back to the any-channel wording | near_white_rule fails (old wording) | evidence/doc-mutations.txt |
| C1 | cadpixel.go line 226 `&&` to `\|\|` (the old doc rule, in code) | near_white_rule fails on cadMeasure and on all three judges | evidence/doc-mutations.txt |
| D2 | example `(249, 255, 255) is ink` to `(249, 249, 249) is ink` | near_white_rule fails (no ink example with a high channel) | evidence/doc-mutations.txt |

After each mutation the file was restored and checked with `cmp` against GREEN (or HEAD for cadpixel.go). No
mutation was committed.

## Shellcheck 0.11.0 (evidence/shellcheck-base.txt, evidence/shellcheck.txt)

| File | Base, full severity | After | `-S warning` after |
|---|---|---|---|
| scripts/keda-cad-loadproof.sh | none | none | clean |
| scripts/keda-cad-downscale-survival.sh | 3 SC1010, 1 SC2016, 1 SC2034 (4 warnings) | 1 SC2016 (info) | clean |
| 76-09-keda-loadproof-test.sh | new | none | clean |

## Sweep (evidence/sweep.txt: raw grep with line numbers, then one classification line per site)

The scripts are `git diff --name-only main...HEAD -- scripts/`. ci-local.sh was swept in quick 261017-7y5
(see its SUMMARY) and is not swept again.

| Site | Records | Reaches exit/SUMMARY | Status | Case |
|---|---|---|---|---|
| keda-cad-loadproof.sh:136, 574, 687 | QUIET contaminated | quiet_line status, `\|\| fail`, then FAIL, exit 1 and SUMMARY result=FAIL | fixed | hot calibration, hot timed |
| keda-cad-loadproof.sh:248, 300 | SUMMARY result=FAIL; RULE quiet_host | exit forced non-zero; RULE amended | propagates; fixed | clean |
| keda-cad-downscale-survival.sh:165, 979 | QUIET; LONG contaminated | `[ "$c" = 0 ]`, `\|\| fail` | propagates | code reading |
| keda-cad-downscale-survival.sh:731 | calibration contaminated | NOTE gates=0 in the evidence; RULE :407 pre-registers it. Consumer grep: no docs/, internal/ or scripts/ reader of a calibration timing | fixed (recorded; non-gating by design) | code reading, no harness |
| keda-cad-downscale-survival.sh:312, 858, 862, 920 | SUMMARY, BUSY, ORDER result=FAIL | each followed by fail, or exit forced | propagates | code reading |
| keda-cad-downscale-survival.sh:437 | long_select reads INWORKER_ROW without load_clean | a loaded row can raise a selection wall. The live LONG wall must lie in the band (:980), and LONG's quiet line gates (:979) | not fixed: fail-safe, no harness. The one load_clean=0 row on file (dxf_pdf L170030 rep 3) is not a render candidate, so the pick M2 is unaffected | todo |
| cad-liveproof.sh:75-88 | ev load gate (waits, never fails) | the -load.txt file goes to final-live-gate quiet_rows :539, bad.N, rerun :622 and QUIET_COVERAGE :640, then NOT_RUN and INCOMPLETE; a missing load file gives QUIET result=fail :593 and cad FAIL | propagates via the wrapper | 76-11 "quiet rerun", "quiet both", "no load file" |
| cad-liveproof.sh:92, 275, 673 | SUMMARY result=FAIL; determinism verdicts | fail(): exit 1 | propagates | code reading |
| cad-image-measure.sh:274, 308, 143 | CONTAMINATED rows; wait_quiet die | the wall is not appended; repeat_clean dies after 6 attempts; the DIST and DECISION lines use clean walls only | propagates | code reading |
| cad-inworker-measure.sh:495-498, 567, 596-598, 606 | ROW_CLEAN / load_clean | only clean rows count in ROWS; 5 tries then fail; the ROWS total is checked before SUMMARY | propagates | code reading |
| final-live-gate.sh:399-404 | CONVERT_TIME contaminated | NOT_RUN ci-local, INCOMPLETE, exit 2 | propagates | 76-11 "d19 loaded" |
| final-live-gate.sh:593, 640 | QUIET cad; QUIET_COVERAGE | ok=0 or NOT_RUN | propagates | 76-11 cases above |
| final-live-gate.sh:354, 374, 449, 756-774 | IMAGES_CHECK, CI_LOCAL, TECTONIC result=fail; FC/NRC | STEP FAIL; VERDICT and SUMMARY with CODE | propagates | 261017-7y5 cases |
| cad-dxf-bounds.sh A: NOTE contaminated_row (host sleep) :2457 | excluded_from_W=1 | contaminated_lines :2344; skipped by maxwall :2478, load :4238, load_rows :4450, derive :4639; counted in SUMMARY contaminated_rows :2625 | propagates | code reading |
| cad-dxf-bounds.sh B: NOTE host_sleep_attempt :2468 | STOP host_sleep, GROUP_END result=STOP | b76 finish :2598-2600 STOPs unless the group then ends ok exactly once | propagates | code reading |
| cad-dxf-bounds.sh C: load_contaminated_row :4488, 4528 | excluded_from_W=1 | the quiet loop re-measures, or b76_stop :4125; derive needs QUIET_END ok :4648 and takes W from clean rows :4699, 4732 | propagates | code reading |
| cad-dxf-bounds.sh D: inworker load_clean :4607 | inworker_clean | derive STOP below 3 clean rows :4687 | propagates | code reading |
| cad-dxf-bounds.sh E: select() :4303 | BASIS W drops host-sleep rows only | a loaded wall gives a larger W and a stricter classification. Only derive output (76-03-derived.txt) is cited by docs/CAD.md, and derive uses clean rows | not fixed, no harness. Not traced to the end: whether a select-step classification reaches a published claim without derive judging it again | todo |
| cad-dxf-bounds.sh F: ladder rmax :3955-3964, memory rung n==3 :4048 | all rows of the run | a loaded wall only refuses a rung earlier; a retried row makes n != 3, which gives verdict=fail | propagates (fail-safe) | code reading |
| cad-dxf-bounds.sh G: TEETH :1099, FLOOR_FILE :1194, stack_smoke :2035 result=fail | X,stop; floor STOP | check_state :950 finish 1; sys.exit(1) gives rc=1 :1262; exit 1 | propagates | code reading |
| cad-dxf-bounds.sh H: CADVERDICT reads (verdict_of, count_pass, guard_probe, verdict_json) | engine outputs | non-ok where ok is expected gives class=fail or band=failed, then finish STOP :2616-2618, or b76_stop / X,stop, then exit 1 | propagates | code reading |
| ci-local.sh | | | swept in 261017-7y5 | |

The sweep found no site in final-live-gate.sh that fails to propagate, so 76-11-final-gate-test.sh is unchanged.
It found two not-fixed sites in scripts without a suite, both in the todo
`.planning/todos/pending/2026-10-17-selection-steps-ignore-load-clean.md`.

## cadBackgroundCutoff routes

| Route | Function | Rule | Same as doc |
|---|---|---|---|
| 3D render PNG (cadrender.go:116; sandbox self-check cadsandbox.go:330) | JudgeCADPNG, then cadMeasure | a non-transparent pixel is skipped only when r, g and b laid over white are all >= 250 (cadpixel.go:226); the white-row fast path skips only exact 0xff rows | yes |
| DXF PNG (caddxf.go:170; cadsandbox.go:422) | JudgeCADDrawingPNG, then cadMeasure | same | yes |
| DXF empty check behind SVG, PDF and PNG (caddxf.go:310, 2048 px) | JudgeCADDrawingInk, then cadMeasure | same; no ink gives ErrCADEmptyResult | yes |
| scripts/cmd/cad-pixel-judge/main.go:71-75 | the three exported judges | same | yes |
| scripts/cad-measure/pixelcheck.py:53, 90 (measurement helper, unchanged on this branch) | `(img < 250).any(axis=2)` | ink when any channel is < 250 | yes, the same direction |

Each judge measures right after `cadReadFrame`. No judge returns before measuring for a reason unrelated to the
pixel (the letterbox from `CADDrawingLetterbox(1, 1, 256)` is valid), so no Count check was dropped.

## Other docs that state the cutoff

| Doc | Hits for 250 / channel / near white / faint | Verdict |
|---|---|---|
| docs/MEASUREMENTS.md | lines 66-67 and 109-110: a pixel is ink when one of its channels, laid over white, is below 250 | correct direction, unchanged |
| docs/ERROR-CODES.md | line 150 "too faint or too small to see" (no channel rule); line 122 has no cutoff | no claim, unchanged |
| docs/PREVIEW.md | line 32 "white or too faint" (no channel rule) | no claim, unchanged |

## The official 76-09 and 76-10 evidence

| File | Line | Content |
|---|---|---|
| evidence/76-09-keda-loadproof.txt | 63 | QUIET step=calibration load1 10.46 to 8.71, contaminated=0 |
| | 78 | QUIET step=timed load1 8.82 to 8.49, load1_max_sampled=11.58, contaminated=0 |
| | 81 | SUMMARY result=PASS |
| evidence/76-10-keda-downscale.txt | 67 | QUIET step=calibration 8.49 to 7.15, contaminated=0 |
| | 96 | QUIET step=long 7.22 to 7.23, load1_max_sampled=11.27, contaminated=0 |
| | 98 | SUMMARY result=PASS |
| evidence/76-10-keda-downscale-calibration.txt | 66 | QUIET step=calibration 9.36 to 8.38, contaminated=0 |
| | 69 | SUMMARY result=PASS |

Both PASS verdicts stand, and 76-09-SUMMARY.md and 76-10-SUMMARY.md each have a one-line `## Follow-up` that
cites these lines.

Observation, not changed here: the pre-registered rule judges load1 at a step's start and end only. In two
official steps the sampled maximum went above 11 mid-step (76-09 timed 11.58, 76-10 LONG 11.27). Both are
`contaminated=0` under the rule that was registered before those runs.

## Deviations from Plan

1. **[Rule 1] Two variable lines in the loadproof.** `CAL_L1=$(load1)` and `T_L1=$(load1)` hold the end
   load1 so the FAIL reason can name it. It is still one load1 read per step, at the same point as before.
2. **[Rule 2] Stricter test helpers and extra checks.**
   - `hasnt` and `nore` require the evidence file to exist, because a scan of a missing file is not clean.
   - Extra checks: the curl and sysctl stubs answered, the pod list follows the replica count, and no stub
     call was left unhandled.
3. **[Rule 1] A stub bug was fixed before RED was recorded.** On macOS, `seq 1 0` counts down, so with 0
   replicas the pod-list stub listed pods 1 and 0. It now uses a while loop, and the "pod list follows the
   replica count" check holds it.
4. **The NOTE reason wording.** The plan's phrase "no calibration timing is reported as a measurement" is
   replaced by the checkable "no file under docs/ internal/ or scripts/ reads a calibration timing". The
   reason: 76-10-SUMMARY.md does narrate the (clean) calibration timings.
5. **Extra mutations and an extra run.** C1 and D2 go beyond the planned doc mutation. The 76-11 suite was
   rerun although final-live-gate.sh did not change.
6. **The pin guard was missed once.** It was not run before one in-place python edit of the new test file,
   right after its Write. The guard passed before and after it, from the same cwd.

## Not done

- No live run: no docker, kubectl, helm or orbctl was invoked. Every result above comes from stubs or Go tests.
- No evidence .txt or transcript in the phase-76 evidence dir was edited. The only file added there is
  76-09-keda-loadproof-test.sh.
- Nothing was pushed.
- The two not-fixed selection sites wait in the todo.

## Self-Check: PASSED

- All created files exist: 76-09-keda-loadproof-test.sh, the todo, and evidence/{base.sha, shellcheck-base.txt,
  base-loadproof.txt, green-loadproof.txt, mutations.txt, downscale-consumers.txt, sweep.txt, shellcheck.txt,
  red-doc.txt, green-doc.txt, convert-package.txt, doc-mutations.txt}.
- Commits 028cd81, f4039cb, 3e0cd29 and 36d6aba are ancestors of HEAD. All are signed G, and none has a
  Co-Authored-By trailer.
- `git diff --quiet main -- internal/convert/exec.go` exits 0.
- No added `#` line in either script or the suite, and no `//` line added to cad_doc_test.go.
