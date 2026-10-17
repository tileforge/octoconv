---
phase: quick-261017-doa
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - scripts/keda-cad-loadproof.sh
  - scripts/keda-cad-downscale-survival.sh
  - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-09-keda-loadproof-test.sh
  - .planning/phases/76-measured-bounds-keda-proof-client-docs/76-09-SUMMARY.md
  - .planning/phases/76-measured-bounds-keda-proof-client-docs/76-10-SUMMARY.md
  - docs/CAD.md
  - internal/convert/cad_doc_test.go
  - .planning/quick/261017-doa-codex-round-2-contaminated-loadproof-tim/261017-doa-SUMMARY.md
  - .planning/quick/261017-doa-codex-round-2-contaminated-loadproof-tim/evidence/*
  - .planning/STATE.md
autonomous: true
requirements: [quick-261017-doa]

estimate:
  tokens: 95000
  raw_tokens: 95000
  tasks: 2
  confidence: low

must_haves:
  truths:
    - "A keda-cad-loadproof run whose calibration step or timed step records contaminated=1 never ends SUMMARY result=PASS: it writes FAIL,step=<step>,reason=... naming the contamination, the SUMMARY is result=FAIL and the script exits 1"
    - "A clean stubbed loadproof run is unchanged: both QUIET lines contaminated=0, CALIBRATION and DRAIN result=ok, SUMMARY result=PASS, exit 0"
    - "The loadproof RULE quiet_host line states that a contaminated step fails the proof, so the RULE and the verdict agree"
    - "keda-cad-downscale-survival keeps a contaminated LONG failing the run, and its contaminated calibration stays non-gating only with the exemption recorded in the evidence file and pre-registered in its RULE line, with the reason checked against every consumer of its calibration timings"
    - "Every contaminated / result=fail / verdict site in the seven live-proof scripts this branch changed is listed in the SUMMARY as propagates, fixed, or not fixed with a reason"
    - "The official 76-09 and 76-10 PASS verdicts stand because every QUIET line in their evidence records contaminated=0; 76-09-SUMMARY.md and 76-10-SUMMARY.md each carry a one-line follow-up citing those lines"
    - "docs/CAD.md near_white states the rule the code applies (background only when every channel, laid over white, is at or above 250; one channel below is ink) and TestCADDocMatchesTheCode probes the doc's own examples through every judge that uses cadBackgroundCutoff"
  artifacts:
    - path: scripts/keda-cad-loadproof.sh
      provides: "quiet_line returns the contamination status; both call sites fail on it; RULE quiet_host amended"
    - path: .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-09-keda-loadproof-test.sh
      provides: "PATH-stubbed full run of the loadproof: clean, hot calibration, hot timed"
    - path: docs/CAD.md
      provides: "near_white row with the all-channel background rule and two code-checked examples"
    - path: internal/convert/cad_doc_test.go
      provides: "near_white subtest that probes the row's examples through cadMeasure and the three exported judges"
    - path: .planning/quick/261017-doa-codex-round-2-contaminated-loadproof-tim/261017-doa-SUMMARY.md
      provides: "RED/GREEN counts, mutations, the sweep table, the 76-09/76-10 verdict statement"
  key_links:
    - from: "quiet_line exit status in scripts/keda-cad-loadproof.sh"
      to: "fail -> FAIL line, teardown SUMMARY result=FAIL, exit 1"
      via: "quiet_line ends with a test of c; each call site is followed by || fail"
      pattern: "quiet_line (calibration|timed) .*\\|\\| fail"
    - from: "docs/CAD.md near_white row examples"
      to: "cadMeasure Count and JudgeCADDrawingInk verdict"
      via: "regex-extracted (r, g, b) triples probed in the near_white subtest"
      pattern: "\\(249, 255, 255\\) is ink"
---

# Quick 261017-doa: a contaminated loadproof timing cannot report PASS; near_white states the real cutoff rule

## Objective

Codex round 2 on PR #37 (branch `gsd/phase-76-measured-bounds-keda-proof-client-docs`, commit there, do not
push), two P2 findings.

1. `scripts/keda-cad-loadproof.sh` `quiet_line` (lines 133-137) records `contaminated=1` but its last command
   is `ev`, so it returns 0, and both call sites (572 calibration, 684 timed) ignore it. The run continues to
   `GATE_OK=1` and the teardown writes `SUMMARY,...,result=PASS`, against the RULE line 299 ("its timings do
   not count"). The calibration timing is not decorative: docs/CAD.md line 161 quotes
   `evidence/76-09-keda-loadproof.txt CALIBRATION.submit_to_ready_s`. The sibling
   `scripts/keda-cad-downscale-survival.sh` already ends `quiet_line` with `[ "$c" = 0 ]` (line 166) and fails
   a contaminated LONG (line 979); its calibration call (line 731) is deliberately non-gating with
   `|| echo`, which today reaches only the console transcript.
2. docs/CAD.md line 220 `near_white` says paint with any channel at or above the cutoff is no ink. The code
   (`cadMeasure`, internal/convert/cadpixel.go line 226) skips a pixel only when all three channels, laid over
   white, are at or above `cadBackgroundCutoff` (250), so (249, 255, 255) is ink. All three exported judges
   (`JudgeCADPNG`, `JudgeCADDrawingPNG`, `JudgeCADDrawingInk`) measure through `cadMeasure`.
   <!-- planner-discipline-allow: any channel at or above -->

Purpose: the proof evidence and the client docs must say what the code does. Output: the loadproof fix and its
stub suite, the downscale exemption made explicit, the doc fix and a stronger doc-sync test, the sweep table, two
SUMMARY follow-ups, STATE.

Decisions taken here (no locked CONTEXT decisions exist for a quick task):
- Verdict for a contaminated loadproof step is FAIL (exit 1, `FAIL,step=...`, SUMMARY result=FAIL), not a new
  INCOMPLETE. The loadproof's vocabulary is PASS/FAIL only, the sibling downscale script already fails a
  contaminated timed row, and the 76-08 `rerun_contaminated_rows` rule says a contaminated row is never counted
  clean and only a later full PASS run clears it, which is exactly "rerun the proof".
- Both loadproof steps gate, calibration included, because its timing is a documented measurement.
- The downscale calibration exemption stays only if no consumer reads its calibration timings (Task 1 step 5
  checks). It is then recorded in the evidence file, not only on the console.

## Context

@.planning/STATE.md
@.planning/quick/261017-7y5-codex-p1-final-live-gate-cleanup-result-/261017-7y5-SUMMARY.md
@scripts/keda-cad-loadproof.sh
@scripts/keda-cad-downscale-survival.sh
@.planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-final-gate-test.sh
@docs/CAD.md
@internal/convert/cadpixel.go
@internal/convert/cad_doc_test.go

Constraints for every step:
- Run `bash /private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad/pin-guard.sh`
  before each Write/Edit and before each commit; halt on failure.
- No code comments at all: no new `#` line in any shell file other than a shebang, no `//` line in Go.
- `internal/convert/exec.go` stays byte-identical to `main`.
- Go runs as `GOTOOLCHAIN=go1.26.4`, with python 3.14 first on PATH:
  `PATH=/private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad/py314:$PATH`.
  Always `-count=1`. Never `go clean -cache`.
- Never run docker, kubectl, helm, orbctl or any live stack. The stub suite must prove its stubs answered.
- shellcheck 0.11.0: `-S warning` clean on every touched shell file, and no growth in any info/style code count
  per file against the base (record the base first).
- `.planning` files are staged with `git add -f`. Commits are SSH-signed; on a signing failure halt and report,
  never commit unsigned. No Co-Authored-By trailer. Never stage `.planning/config.json`, `.vscode/` or
  `octoconv.code-workspace`.

<tasks>

<task type="tracer" tdd="true">
  <name>Task 1: a contaminated loadproof step fails the proof (stub suite RED then GREEN), downscale exemption made explicit, the sweep</name>
  <files>scripts/keda-cad-loadproof.sh, scripts/keda-cad-downscale-survival.sh, .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-09-keda-loadproof-test.sh, .planning/phases/76-measured-bounds-keda-proof-client-docs/76-09-SUMMARY.md, .planning/phases/76-measured-bounds-keda-proof-client-docs/76-10-SUMMARY.md, .planning/quick/261017-doa-codex-round-2-contaminated-loadproof-tim/evidence/</files>
  <behavior>
    - clean: exit 0; `QUIET,step=calibration,...,contaminated=0` and `QUIET,step=timed,...,contaminated=0`; a `CALIBRATION,` line and a `DRAIN,` line with result=ok; `SUMMARY,mode=keda_cad_loadproof,...,result=PASS`; no line starting `FAIL,`; RESTORE result=ok; the kubectl, helm and docker stub call logs are non-empty
    - hot calibration (end load1 of the calibration step is 12, above quiet_load 11): `QUIET,step=calibration,...,contaminated=1`; a line starting `FAIL,step=calibration,reason=` containing `contaminated`; no `CALIBRATION,` line; no `TIMED_SUBMIT,` line; SUMMARY result=FAIL; never result=PASS; exit 1
    - hot timed (end load1 of the timed step is 12): `QUIET,step=timed,...,contaminated=1`; a line starting `FAIL,step=timed` containing `contaminated`; no `DRAIN,` line; SUMMARY result=FAIL; never result=PASS; exit 1
  </behavior>
  <action>
Step 0, base. Record `git rev-parse HEAD` into `.planning/quick/261017-doa-codex-round-2-contaminated-loadproof-tim/evidence/base.sha`. Record the per-file shellcheck base (full severity, `-f gcc`, code counts) of scripts/keda-cad-loadproof.sh and scripts/keda-cad-downscale-survival.sh into evidence/shellcheck-base.txt. At the base, downscale already fails `-S warning`: SC1010 at lines 882 and 907 (an unquoted `done` operand) and SC2034 `L_FMT` at line 462.

Step 1, the stub suite (RED). Create `.planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-09-keda-loadproof-test.sh`, executable, no comment lines, modelled on 76-11-final-gate-test.sh: `set -uo pipefail`, REPO from the script path, `T=$(mktemp -d)` with an EXIT trap removing it, the same `check`/`has`/`hasnt`/`hasre` helpers and the final `RESULT: N passed, M failed` line with exit status from FAILS. It runs the real `$REPO/scripts/keda-cad-loadproof.sh --evidence-dir <case dir>` once per case with `PATH="$T/bin:$PATH"` and `STUB_STATE=<case state dir>`, captures the exit code, and asserts on `<case dir>/76-09-keda-loadproof.txt`. Before the first case it asserts that under the stub PATH `command -v` resolves kubectl, helm, docker, orbctl, curl, go, lsof, sysctl and sleep to `$T/bin` and aborts the suite if not. Resolve the real sleep path before PATH changes.

Stub contract (every stub appends `$0 $*` to `$STUB_STATE/calls.<tool>`; state writes go to a temp file and are moved into place, because the sampler reads them concurrently):
- sleep: exec the real sleep with 0.05.
- sysctl `-n vm.loadavg`: if `$STUB_STATE/hot_once` exists, remove it and print `{ 12.00 9.00 9.00 }`; else if `$STUB_STATE/hot_on` exists print `{ 12.00 9.00 9.00 }`; else `{ 4.00 4.00 4.00 }`. The script's load1 reads field 2.
- orbctl `config get k8s.enable`: `true`. lsof: exit 1. go `run ./cmd/manage-clients create ...`: print `api key: stubkey0123456789`.
- helm: `search repo kedacore/keda --versions` prints a header and `kedacore/keda 2.20.1 2.20.1 KEDA`; `list -A -o json` prints `[]`; everything else exits 0.
- docker: both `ps` forms print nothing; `run --rm alpine df -k /` prints a df header and a line with Available 20000000; `image inspect octoconv-<img>:dev --format {{.Id}}` prints a fixed `sha256:` plus 64 hex per image; `run --name ...` finds the `-v` argument ending `:/out` and writes a non-empty `step-nauo-LinearSlide-MGN15x-Block-n1000.step` into that host directory; `rm -fv` exits 0.
- curl: parse `-o FILE`, `-X POST` and the URL (last argument). `/healthz`: write `{}` to FILE, print `200`. POST `/v1/jobs`: count n, id `00000000-0000-4000-8000-` plus n zero-padded to 12, write `{"job_id":"<id>"}` to FILE, store the last id, set the cad-worker counts to `1 1 1` for n=1 and `2 2 2` for n>=2, print `202`. GET `/v1/jobs/<id>`: print `{"status":"done"}`; when id is the last posted id set the counts to `0 0 0`; with `STUB_HOT=calibration` and job 1 touch `hot_once`; with `STUB_HOT=timed` and job 4 touch `hot_on`. Verified by reading the script: between the calibration GET and `quiet_line calibration` no other load1 read happens, and the sampler is stopped before `quiet_line timed` reads load1, so these knobs contaminate exactly the step end they target and never a wait_quiet start.
- kubectl: drop a leading `-n <ns>`, then dispatch on the rest. `config current-context` prints `orbstack`. `get nodes` exits 0; `get nodes -o json` gives one node with allocatable cpu `11`, memory `16Gi`. `get ns -o json` lists default and kube-system; `get ns <name>` exits 1. `get crd -o json` gives no items. `get apiservice v1beta1.external.metrics.k8s.io` exits 0, and its `-o jsonpath=...Available...` form prints `True`. `get pods -A -o json` gives one Running kube-system pod with nodeName, requests cpu 100m memory 128Mi and restartCount 0; `get pods -A --no-headers` prints nothing; `get pods -o json` gives no items. `get deployment <d>` with no `-o`, `scale`, `wait`, `port-forward`, `describe`, `delete`, `logs -l` and `get hpa` exit 0. `get deployment <d> -o json` gives spec.selector.matchLabels `app.kubernetes.io/component=<d>`, container 0 resources requests and limits cpu `2` memory `4Gi`, and for cad-worker spec.replicas, status.replicas, status.readyReplicas from the stored counts (default `0 0 0`), 0 for any other deployment. `get pod -l app.kubernetes.io/component=redis -o jsonpath=...` prints `redis-0`; `exec redis-0 -- redis-cli SADD asynq:queues ...` stores the members and `SMEMBERS` prints them one per line. `get scaledobject -o json` lists one item whose scaleTargetRef.name is cad-worker; `get scaledobject worker-cad-scaledobject -o json` has a Fallback condition `False` and externalMetricNames `["s0-redis-cad"]`. `get --raw /apis/external.metrics...` gives `{"items":[{"value":"0"}]}`. `get pod -l app.kubernetes.io/component=cad-worker -o json` lists as many pods as the stored status replicas, with creationTimestamp and a Ready True condition at the current UTC second. `get pod <name> -o json` gives the same Ready condition; `-o jsonpath={.status.containerStatuses[0].imageID}` prints `docker://` plus the cad-worker image id. `logs <pod>` prints a line containing `cad sandbox proven`. `get events --field-selector ... -o json` gives one item with firstTimestamp at the current UTC second and message `Container image already present on machine`. `exec deploy/postgres -- psql ... -tAc <sql>`: `select attempts` prints 1, `count(*)` prints 1, `select extract(epoch from finished_at)` prints the current epoch with milliseconds, `round(` prints 12.3.

Cases: clean (no knob), hot calibration (`STUB_HOT=calibration`), hot timed (`STUB_HOT=timed`), with the behavior block's assertions. Match the timed FAIL line by its `FAIL,step=timed` prefix, because `step "timed 0->2->0"` sets STEP to the whole string. Run the suite at the base, save its output to evidence/base-loadproof.txt (clean all pass, hot cases fail on the new assertions), commit the test file alone as `test(quick-261017-doa): RED a contaminated loadproof step must not reach PASS`.

Step 2, the fix (GREEN). In scripts/keda-cad-loadproof.sh: end `quiet_line` with `[ "$c" = 0 ]`, the same as downscale line 166. Follow the calibration call with `|| fail` and a reason saying the calibration step is contaminated (with its load1 start and end) and its timings do not count. Do the same for the timed call. Amend RULE `key=quiet_host` (line 299) so it also says a contaminated step fails the proof and only a later clean run counts. Leave every other line alone. Run the suite and save evidence/green-loadproof.txt. It must report 0 failed, and every check that passed at the base must still pass (compare the PASS names).

Step 3, mutations. Apply each to the script in place, run the suite, save the output, restore from a saved copy and `cmp` it against GREEN. M1: drop the trailing status test from `quiet_line`. M2: change the calibration `|| fail ...` to `|| true`. M3: the same for the timed call. Each must make at least one hot-case check fail. Write evidence/mutations.txt. Never commit a mutation.

Step 4, downscale, verdict kept, exemption made explicit. First grep docs/, every `.planning/phases/76-measured-bounds-keda-proof-client-docs/*.md`, internal/ and scripts/ for any consumer of a 76-10 calibration timing (`76-10-keda-downscale` together with `CALIBRATION`, `submit_to_ready_s`, `created_to_ready_s` or `drained_s`). Save the result to evidence/downscale-consumers.txt.
- If any consumer reports one of those timings as a measurement, make line 731 `|| fail` like the loadproof, and record that.
- Otherwise keep it non-gating. Replace its console-only `|| echo ...` with `|| ev` writing a `NOTE,key=quiet_calibration,step=calibration,contaminated=1,gates=0,reason=...` line into the evidence file. The reason must be accurate: no calibration timing is reported as a measurement, and its timed checks (0->1 within 180 s, drain within the drain bound) are upper bounds that load only makes harder to pass. The current text "the calibration checks are not timings" is inaccurate.
- Amend the downscale RULE `key=quiet_host` (line 407) to pre-register this: LONG's contaminated step fails the run, and the calibration's QUIET line is recorded and gates nothing, for that reason.
- Because the file is touched, clear its `-S warning` findings without changing behaviour: quote the `done` operand at lines 882 and 907, and put `_` for the unused `L_FMT` at line 462 (first grep that `L_FMT` has no other use).
- No harness exists for this script. In the SUMMARY say the NOTE branch is checked by code reading only. It sits on a branch whose status `||` already discards, so it cannot change the verdict.

Step 5, the sweep. List every site that records contaminated, result=fail/FAIL, CONTAMINATED, load_clean or another verdict, in every script from `git diff --name-only main...HEAD -- scripts/`: keda-cad-loadproof.sh, keda-cad-downscale-survival.sh, cad-liveproof.sh, cad-image-measure.sh, cad-dxf-bounds.sh, cad-inworker-measure.sh and final-live-gate.sh (ci-local.sh was swept in 261017-7y5; say so). For each site, follow its value to the exit status and the SUMMARY/VERDICT line. Leads to confirm by reading, not to copy:
- cad-image-measure: CONTAMINATED rows go to repeat_clean, which dies after MAX_ATTEMPTS.
- cad-inworker-measure: ROW_CLEAN gates ROWS, a row is retried 5 times and then fails, and the ROWS total check comes before SUMMARY.
- cad-liveproof: its ev load gate only waits. The 76-11 wrapper judges contamination from the load file, through quiet_rows, the rerun and QUIET_COVERAGE, to NOT_RUN and INCOMPLETE.
- final-live-gate: CONVERT_TIME contaminated leads to NOT_RUN.
- cad-dxf-bounds: contaminated NOTE rows are excluded from W, and the contaminated_rows count goes into SUMMARY.
Grouping the 48 cad-dxf-bounds hits by construct is fine, but every construct must be followed. Save raw grep output with line numbers to evidence/sweep.txt.
- A site that does not propagate in final-live-gate.sh: fix it with a new RED-then-GREEN case in 76-11-final-gate-test.sh.
- A site that does not propagate in a script with no stub suite: do not fix it here. Record it as not fixed (found in the sweep, no harness), add a todo under .planning/todos/pending/, and name it in the return to the orchestrator.

Step 6, the official evidence. Check yourself with `grep -n '^QUIET\|^SUMMARY'` that every QUIET line records contaminated=0 and SUMMARY result=PASS in evidence/76-09-keda-loadproof.txt (planning read QUIET at lines 63 and 78, SUMMARY at 81), evidence/76-10-keda-downscale.txt (67, 96, 98) and evidence/76-10-keda-downscale-calibration.txt (66, 69). The scaling subtest of TestCADDocMatchesTheCode (cad_doc_test.go lines 745-751) already pins the 76-09 values. Add to 76-09-SUMMARY.md and 76-10-SUMMARY.md a `## Follow-up` section (if absent) with exactly one non-empty line, in the form of the 76-11-SUMMARY follow-up: date, quick 261017-doa, Codex round 2 on PR #37, the script now fails a contaminated step (downscale: the calibration exemption is now recorded), and the PASS stands, citing the file and line numbers you measured. Do not edit any evidence .txt or transcript.

Step 7, gates and commit. Run bash -n on both scripts. Run `shellcheck -S warning` on both scripts and the new test file. Record full-severity counts per file to evidence/shellcheck.txt; no code count may grow against the base. The new test file must have no `-S warning` finding. Commit the script(s), the follow-ups and the evidence as `fix(quick-261017-doa): a contaminated loadproof step fails the proof`.
  </action>
  <verify>
    <automated>Q=.planning/quick/261017-doa-codex-round-2-contaminated-loadproof-tim && B=$(cat "$Q/evidence/base.sha") && E=.planning/phases/76-measured-bounds-keda-proof-client-docs/evidence && O=$(bash "$E/76-09-keda-loadproof-test.sh") && printf '%s\n' "$O" | tail -1 | grep -qE '^RESULT: [0-9]+ passed, 0 failed$' && bash -n scripts/keda-cad-loadproof.sh && bash -n scripts/keda-cad-downscale-survival.sh && shellcheck -S warning scripts/keda-cad-loadproof.sh scripts/keda-cad-downscale-survival.sh "$E/76-09-keda-loadproof-test.sh" && grep -qE 'quiet_line calibration .*\|\| fail' scripts/keda-cad-loadproof.sh && grep -qE 'quiet_line timed .*\|\| fail' scripts/keda-cad-loadproof.sh && D=$(git diff "$B" -- scripts/keda-cad-loadproof.sh scripts/keda-cad-downscale-survival.sh "$E/76-09-keda-loadproof-test.sh") && test -z "$(printf '%s\n' "$D" | grep -E '^\+[[:space:]]*#' | grep -v '^+#!')" && test -z "$(grep -n '^[[:space:]]*#' "$E/76-09-keda-loadproof-test.sh" | grep -v '^1:#!')" && git diff --quiet main -- internal/convert/exec.go && test "$(awk '/^## Follow-up/{f=1; next} f && NF' .planning/phases/76-measured-bounds-keda-proof-client-docs/76-09-SUMMARY.md | wc -l | tr -d ' ')" = 1 && test "$(awk '/^## Follow-up/{f=1; next} f && NF' .planning/phases/76-measured-bounds-keda-proof-client-docs/76-10-SUMMARY.md | wc -l | tr -d ' ')" = 1 && test -z "$(grep -h '^QUIET' "$E/76-09-keda-loadproof.txt" "$E/76-10-keda-downscale.txt" "$E/76-10-keda-downscale-calibration.txt" | grep -v ',contaminated=0$')"</automated>
  </verify>
  <done>Under PATH stubs, a contaminated calibration step or timed step of keda-cad-loadproof ends with FAIL, SUMMARY result=FAIL and exit 1, and a clean run still ends PASS with exit 0. RED (base) and GREEN outputs and three caught mutations are saved. The downscale exemption is either recorded in the evidence with an accurate, checked reason or converted to a fail, and its warnings are cleared. Every verdict site in the seven scripts is classified in evidence/sweep.txt. The 76-09 and 76-10 follow-ups cite the contaminated=0 lines. Two signed commits exist (RED, then fix).</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: near_white states the all-channel rule, the doc-sync test probes it on every judge, then SUMMARY and STATE</name>
  <files>internal/convert/cad_doc_test.go, docs/CAD.md, .planning/quick/261017-doa-codex-round-2-contaminated-loadproof-tim/261017-doa-SUMMARY.md, .planning/STATE.md</files>
  <behavior>
    - The near_white row states at least one `(r, g, b) is ink` example with a channel at or above cadBackgroundCutoff, and the code measures that pixel as ink: Count 1 from cadMeasure and from each exported judge's facts, and JudgeCADDrawingInk returns nil
    - The row states at least one `(r, g, b) is not` example whose smallest channel equals cadBackgroundCutoff, and the code measures it as background: Count 0 everywhere, and JudgeCADDrawingInk returns an error wrapping ErrCADEmptyResult
    - The row no longer contains the old any-channel wording; the existing known_limits value check (250) still passes
  </behavior>
  <action>
Step 1, routes. Confirm by reading internal/convert/cadpixel.go that JudgeCADPNG (3D PNG check), JudgeCADDrawingPNG (DXF PNG) and JudgeCADDrawingInk (the DXF empty check behind SVG, PDF and PNG) all measure through cadMeasure. Its only skip of a non-transparent pixel is the line 226 test that all three composited channels are at or above cadBackgroundCutoff; the white-row fast path skips only exact white. Confirm that scripts/cmd/cad-pixel-judge/main.go calls the exported judges. Note scripts/cad-measure/pixelcheck.py (unchanged on this branch, a measurement helper and not a product route) uses `(img < 250).any(axis=2)`, the same direction. Grep docs/ERROR-CODES.md, docs/PREVIEW.md and docs/MEASUREMENTS.md for `250`, `channel`, `near white`, `near-white` and `faint`. MEASUREMENTS lines 67 and 110 already say a pixel is ink when one channel is below 250, and planning found no claim in ERROR-CODES or PREVIEW. Record each hit and verdict for the SUMMARY. Fix any other doc that states the rule backwards.

Step 2, RED. In internal/convert/cad_doc_test.go, inside TestCADDocMatchesTheCode, add a subtest `near_white_rule`, no comment lines. It reads the near_white row through the existing cadDocTable for `## Known limits`, then:
- extracts every `\((\d{1,3}), (\d{1,3}), (\d{1,3})\) is ink` triple and every `\(...\) is not` triple, and fails if either list is empty;
- requires an ink example with some channel at or above cadBackgroundCutoff, and a not-ink example whose smallest channel equals cadBackgroundCutoff;
- fails if the row still contains the old any-channel wording.
For each triple, build an opaque white CADMinRenderSize square image.NRGBA with that one pixel at the centre. Call cadMeasure on it and expect Count 1 for ink and 0 for not. Encode it as PNG. Call JudgeCADPNG with CADBackgroundOpq, JudgeCADDrawingPNG with CADBackgroundOpq and the box from CADDrawingLetterbox(1, 1, size), and JudgeCADDrawingInk, and expect the same Count in each returned facts, ignoring their other verdicts. If reading shows a judge returns before measuring for a reason unrelated to the pixel, drop that judge's Count check and say why in the SUMMARY. JudgeCADDrawingInk must return nil for ink and an error matching ErrCADEmptyResult under errors.Is for not ink. Run `go test ./internal/convert -run 'TestCADDoc' -count=1`, save to evidence/red-doc.txt (near_white_rule fails, nothing else), commit the test alone as `test(quick-261017-doa): RED the near_white row must state the cutoff rule the code applies`.

Step 3, GREEN. Rewrite only the limit text of the docs/CAD.md near_white row (keep id, constant and value cells). Say that paint which, laid over a white page, has every channel at or above the cutoff is no ink, and that one channel below it is ink. Give the examples, so that (249, 255, 255) is ink and (250, 250, 250) is not. Add that the 3D PNG check and the DXF checks apply the same rule. No `|` in the cell. Run gofmt -l on the test file (empty output), `go vet ./...`, `go test ./internal/convert -run 'TestCADDoc' -count=1` (all pass), then the whole package `go test ./internal/convert -count=1`, using the python 3.14 PATH and GOTOOLCHAIN from the constraints. Save to evidence/green-doc.txt and evidence/convert-package.txt. Mutation: put the old any-channel wording back in a scratch edit, confirm near_white_rule fails, restore and `cmp` docs/CAD.md. Commit as `docs(quick-261017-doa): near_white states the all-channel background rule`.

Step 4, SUMMARY. Write `.planning/quick/261017-doa-codex-round-2-contaminated-loadproof-tim/261017-doa-SUMMARY.md` in the shape of 261017-7y5-SUMMARY.md: frontmatter with plan_head_before (base.sha), plan_head_after, commits_list and decisions. Body sections:
- both findings and the fixes;
- the TDD table (base, RED, GREEN, counts and evidence files) for both suites;
- the mutations;
- shellcheck base vs after, per file;
- the sweep table: Site | Records | Reaches exit/SUMMARY | Status (propagates / fixed / not fixed + reason) | Case. One row per site or construct across all seven scripts, the downscale calibration row giving the consumer-grep result, and a ci-local.sh note pointing to 261017-7y5;
- the route table for cadBackgroundCutoff (route, function, rule, same as doc);
- the doc grep results for ERROR-CODES, PREVIEW and MEASUREMENTS;
- the official 76-09 and 76-10 evidence lines with contaminated=0;
- Not done: no live run, no evidence transcript edited, nothing pushed.
Commit as `docs(quick-261017-doa): sweep table, near_white routes, 76-09/76-10 follow-ups`.

Step 5, STATE. Append one row to the `### Quick Tasks Completed` table of .planning/STATE.md: `| 261017-doa | <one-line description> | 2026-10-17 | <docs commit short sha> | [261017-doa-codex-round-2-contaminated-loadproof-tim](./quick/261017-doa-codex-round-2-contaminated-loadproof-tim/) |`. Use Edit with a scoped replacement anchored on the 261017-7y5 row, and touch no other section. Commit as `docs(state): record quick task 261017-doa`.
  </action>
  <verify>
    <automated>Q=.planning/quick/261017-doa-codex-round-2-contaminated-loadproof-tim && B=$(cat "$Q/evidence/base.sha") && PATH=/private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad/py314:$PATH GOTOOLCHAIN=go1.26.4 go test ./internal/convert -run 'TestCADDoc' -count=1 && test -z "$(GOTOOLCHAIN=go1.26.4 gofmt -l internal/convert)" && GOTOOLCHAIN=go1.26.4 go vet ./... && ! grep -q 'any channel at or above' docs/CAD.md && grep -q '(249, 255, 255) is ink' docs/CAD.md && test -z "$(git diff "$B" -- internal/convert/cad_doc_test.go | grep -E '^\+[[:space:]]*//')" && git diff --quiet main -- internal/convert/exec.go && test -s "$Q/261017-doa-SUMMARY.md" && test "$(grep -E '^\| 261017-doa \|' .planning/STATE.md | awk -F'|' '{print NF}')" = 7 && M=$(git log --format=%B "$B"..HEAD) && test -z "$(printf '%s\n' "$M" | grep -i 'co-authored-by')" && S=$(git log --format='%G?' "$B"..HEAD) && test -n "$S" && test -z "$(printf '%s\n' "$S" | grep -v '^G$')" && N=$(git diff --name-only "$B" HEAD) && test -z "$(printf '%s\n' "$N" | grep -E '^(\.planning/config\.json|\.vscode/|octoconv\.code-workspace)')"</automated>
  </verify>
  <done>The near_white row states the all-channel rule with two examples. The near_white_rule subtest probes those examples through cadMeasure and the three exported judges and fails on the old wording; RED and GREEN are recorded. `go test ./internal/convert -count=1` and `go vet ./...` pass on go1.26.4. The SUMMARY has the sweep, route and doc tables. STATE has the 261017-doa row. Every commit since base.sha is signed G, has no Co-Authored-By trailer, and touches none of the three excluded paths.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| live-proof script -> committed evidence | a script's SUMMARY line is what reviewers and the doc-sync tests trust as the proof verdict |
| docs -> client services | internal clients read docs/CAD.md to predict which near-white drawings are refused as cad_empty_result |
| test harness -> host tooling | the stub suite must never reach the real kubectl, helm, docker or orbctl |

## STRIDE Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation Plan |
|-----------|----------|-----------|----------|-------------|-----------------|
| T-261017-doa-01 | Repudiation | keda-cad-loadproof.sh quiet_line and its two call sites | high | mitigate | quiet_line returns the contamination status, both sites fail on it, and the hot-calibration and hot-timed stub cases plus mutations M1-M3 pin it (Task 1) |
| T-261017-doa-02 | Tampering | keda-cad-downscale-survival.sh calibration exemption | medium | mitigate | consumer grep decides the exemption, the NOTE line and the RULE pre-registration make it visible in the evidence file (Task 1 step 4) |
| T-261017-doa-03 | Information disclosure | docs/CAD.md near_white row | medium | mitigate | the row's examples are probed through cadMeasure and the three exported judges by near_white_rule (Task 2) |
| T-261017-doa-04 | Elevation of privilege | 76-09-keda-loadproof-test.sh PATH stubs | high | mitigate | the suite aborts unless every tool resolves to $T/bin, and asserts the stub call logs are non-empty; the script runs only with --evidence-dir inside $T |
| T-261017-doa-SC | Tampering | npm/pip/cargo installs | high | accept | no package is installed in this task |
</threat_model>

<verification>
- `bash .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-09-keda-loadproof-test.sh` ends `RESULT: N passed, 0 failed`.
- `bash .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-final-gate-test.sh` still ends `RESULT: N passed, 0 failed` (and it gains cases only if the sweep fixed a wrapper site).
- `PATH=<py314>:$PATH GOTOOLCHAIN=go1.26.4 go test ./internal/convert -count=1` and `go vet ./...` pass; `gofmt -l` is empty.
- `shellcheck -S warning` is clean on every touched shell file, and no per-file code count grew against evidence/shellcheck-base.txt.
- `git diff --quiet main -- internal/convert/exec.go` exits 0. No evidence .txt or transcript in the phase-76 evidence dir is modified (`git diff --name-only "$B" HEAD -- .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/` lists only the new test file, plus 76-11-final-gate-test.sh if the sweep changed it).
- Nothing pushed.
</verification>

<success_criteria>
- A contaminated keda-cad-loadproof step can no longer end SUMMARY result=PASS, and this is proven against stubs, RED then GREEN, with three caught mutations.
- The downscale calibration exemption either stands with an evidence-file NOTE and a checked reason, or is converted to a fail.
- Every verdict site in the seven scripts is classified in the SUMMARY.
- The 76-09 and 76-10 PASS verdicts are confirmed by their contaminated=0 QUIET lines and noted in their SUMMARYs.
- docs/CAD.md near_white matches cadMeasure on every route, held by a test that probes the doc's own examples.
</success_criteria>

<output>
Create `.planning/quick/261017-doa-codex-round-2-contaminated-loadproof-tim/261017-doa-SUMMARY.md` when done (Task 2 step 4), then the STATE row (Task 2 step 5).
</output>