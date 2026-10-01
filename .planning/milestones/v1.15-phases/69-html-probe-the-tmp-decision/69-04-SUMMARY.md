---
phase: 69-html-probe-the-tmp-decision
plan: 04
subsystem: security
tags: [chromium, csp, html, sandbox, landlock, g-69-2]

# Dependency graph
requires:
  - phase: 69-html-probe-the-tmp-decision (plans 01-03)
    provides: ConfinedChromium, htmlReadWrite/htmlReadOnly, the D-03/D-04 concurrency guard and shared-/tmp sweep, and the D-05 CSP-based JS-disable mechanism this plan closes a bypass in
provides:
  - injectCSPFirst spliced at the document start (BOM, then HTML whitespace only, then past a leading doctype's first '>' if present, else that position) -- no search of the document for a head/html tag at all
  - cspNoScriptMeta extended with frame-src 'none' and child-src 'none', refusing a framed/embedded raw upload (in.html) as a second live route to script execution
  - one shape table (18 top-level rows + 6 unit-only edge rows) exercised at both the unit level (chromium_test.go) and a live differential against the real chromium-headless-shell through the production Convert path (chromium_live_test.go)
  - scripts/sandbox-liveproof.sh html gained a permanent "csp-precedes-every-script" check (CSP_LIVE_ROWS=18), cross-compiling convert.test for the image's own architecture on go.mod's own toolchain
  - both guards shown to be load-bearing by their own live inversions, recorded in .planning/debug/csp-bypass-before-head.md
affects: [69-05, 69-VERIFICATION, 69-UAT]

# Actuals (#2632)
actuals:
  tokens: 10132
  tasks: 3
  commits: 5
  plan_head_before: 5a6ac7b4b23b32bd299d4012bece098306cf3db7
  plan_head_after: f2b814cd29907b2cb7f2ea1fcea5ad2999da40d2

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "One shape table (cspShapeTable/cspEdgeRows) shared between a unit invariant and a live differential -- the live test iterates the exact same table the unit test does, so the two levels can never silently diverge"
    - "Live gate cross-compiles the test binary for the image's own architecture on go.mod's own toolchain (never the host default), bind-mounts it read-only into a throwaway container, and runs it under the image's own tini -- proving the fix against the real engine inside the artifact that ships, not a developer machine"

key-files:
  created:
    - internal/convert/chromium_live_test.go
  modified:
    - internal/convert/chromium.go
    - internal/convert/chromium_test.go
    - scripts/sandbox-liveproof.sh
    - .planning/debug/csp-bypass-before-head.md

key-decisions:
  - "injectCSPFirst performs NO search of the document for a head/html tag at all -- it splices at the document start (past an optional BOM, then HTML whitespace, then past a leading doctype's first '>'), because source order is not tree-construction order and a byte search for a literal tag can be defeated by anything -- a script, a decoy tag, a comment -- that precedes it in the raw bytes (69-UAT G-69-2)"
  - "The whitespace set the splice skips is HTML's five characters (TAB, LF, FF, CR, SPACE) only, never LooksLikeHTML's wider trim set (which also accepts a vertical tab) -- reusing the sniff's leniency here was measured live to be a bypass (row x2)"
  - "frame-src 'none' and child-src 'none' were added to cspNoScriptMeta even though the UAT text did not name this route: the worker always writes the upload's raw copy into the job directory as in.html, which carries no CSP of its own, and framing it back as a separate document executed its script under the CSP as shipped (Planning-time measurement 3, rows f1/f2) -- required by the gap's own truth (\"governs every script in an uploaded HTML document\"), not by the letter of the original review text"
  - "Both guards were proven load-bearing by their own live inversions (Task 3), not merely by removing them and reasoning about the result: inversion A (the pre-fix splice restored) fails exactly the 12 rows the debug session's divergence table names; inversion B (frame-src/child-src removed) fails exactly f1 and f2"

requirements-completed: []

coverage:
  - id: D1
    description: "injectCSPFirst splices the CSP at the document start for every row of the shape table (18 top-level rows + 6 unit-only edge rows), never searching for a head/html tag"
    requirement: "CONF-01"
    verification:
      - kind: unit
        ref: "internal/convert/chromium_test.go#TestInjectCSPFirstLeadsEveryShape"
        status: pass
      - kind: unit
        ref: "internal/convert/chromium_test.go#TestInjectCSPFirstKeepsPrintStyleAtHeadClose"
        status: pass
    human_judgment: false
  - id: D2
    description: "cspNoScriptMeta refuses a framed/embedded raw upload (frame-src 'none', child-src 'none', alongside script-src/object-src 'none')"
    requirement: "CONF-01"
    verification:
      - kind: unit
        ref: "internal/convert/chromium_test.go#TestChromiumInjectsCSPNoScriptMeta"
        status: pass
    human_judgment: false
  - id: D3
    description: "Live differential over all 18 shape-table rows on the real chromium-headless-shell through the production ChromiumConverter.Convert path, wired into scripts/sandbox-liveproof.sh html as a repeatable gate"
    requirement: "CONF-01"
    verification:
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh html -- \"PASS: html csp-precedes-every-script\", 18/18 subtests, \"ALL PASS (8 checks)\""
        status: pass
    human_judgment: true
    rationale: "Task 3's own <verify><human-check> asks the user to review the SUMMARY's live evidence directly (the 18 passing rows, both inversions' exact failing sets, the clean re-run's commit/image id) -- G-69-2 was accepted as a blocker the user wanted proven live, not merely re-asserted by the executor."
  - id: D4
    description: "Both guards (the document-start splice and the frame-refusing CSP) shown to be load-bearing by inverting each on the live check and reverting"
    requirement: "CONF-01"
    verification:
      - kind: integration
        ref: "Inversion A (pre-fix splice restored, run id 1790479795-28344): FAILs exactly s1, s2, s4, s5, s6, s7, s8, s9, x1, x2, x3, x4; PASSes s3, x5, f1, f2, f3, f4"
        status: pass
      - kind: integration
        ref: "Inversion B (frame-src/child-src removed, run id 1790479891-35371): FAILs exactly f1, f2; every other row PASSes"
        status: pass
    human_judgment: true
    rationale: "Same human-check as D3 -- the user approves that each inversion fails exactly the rows its guard owns and no others."

duration: 50min
completed: 2026-09-27
status: complete
---

# Phase 69 Plan 04: Document-Start CSP Splice + Frame-Refusing CSP (G-69-2) Summary

**Closes UAT gap G-69-2: injectCSPFirst now splices the JS-disable CSP at the document start with no head/html tag search at all, cspNoScriptMeta refuses a framed/embedded copy of the raw upload, and both guards are proven live -- including their own live inversions -- over all 18 shape-table rows through the production ChromiumConverter.Convert path.**

## Performance

- **Duration:** ~50 min
- **Started:** 2026-09-27T02:51:00Z (approx.)
- **Completed:** 2026-09-27T03:41:00Z
- **Tasks:** 3
- **Files modified:** 5 (1 created)

## Accomplishments

- `injectCSPFirst` (internal/convert/chromium.go) rewritten to splice the CSP at the document start -- optional UTF-8 BOM, then only HTML whitespace (TAB, LF, FF, CR, SPACE), then past a leading doctype's first `'>'` if one is present, else that position. No search of the document for a `<head>`/`<html>` tag anywhere in the function body (verified structurally: `grep -cE 'headOpenPrefix|htmlOpenPrefix|bytes\.Index\('` over the function body is 0).
- `cspNoScriptMeta` now carries `frame-src 'none'` and `child-src 'none'` in addition to `script-src 'none'; object-src 'none'` -- refusing an `<iframe>`/`<frameset>` of the upload's own raw copy (`in.html`), the sub-document bypass measured live at planning time (`<object>`/`<embed>` were already refused by `object-src`).
- One shape table (`cspShapeTable`, 18 rows: s1-s9, x1-x5, f1-f4) plus six unit-only edge rows (`cspEdgeRows`, e1-e6) shared between `TestInjectCSPFirstLeadsEveryShape` (unit) and `TestChromiumCSPPrecedesEveryScriptLive` (live, `internal/convert/chromium_live_test.go`) -- the same table proves the structural invariant and the live differential, so the two levels cannot silently diverge.
- `scripts/sandbox-liveproof.sh html` gained a permanent 8th check, `"html csp-precedes-every-script"` (`CSP_LIVE_ROWS=18`): cross-compiles `convert.test` for the image's own architecture on go.mod's own toolchain (`GOTOOLCHAIN=go1.26.4`, never the host default), bind-mounts it read-only into a throwaway container, and runs it under the image's own `tini`.
- Both guards proven load-bearing by their own live inversions (Task 3), then cleanly reverted with `git checkout --`, and the clean re-run repeated on the committed tree.
- `.planning/debug/csp-bypass-before-head.md` (the diagnosis session this plan closes) updated with a timestamped Evidence entry recording the 18-row live result and both inversions, plus a `fix_applied` line naming the shipped fix.

## Task Commits

Each task followed its own RED-GREEN cycle (`tdd="true"`) and was committed atomically:

1. **Task 1: Row s1 end to end** (`tdd="true"`, type `tracer`)
   - `2fd8d56` (test) -- add failing test for the document-start splice (row s1); confirmed RED on pre-fix code (CSP landed after the literal `<head>` tag instead of at the document start)
   - `d7ce865` (feat) -- rewrite `injectCSPFirst`, delete the two obsolete tests asserting the old splice offsets, add `chromium_live_test.go`, wire the live gate; confirmed GREEN; live gate: `ALL PASS (8 checks)`
   - Tracer feedback gate (row 3 of the precedence chain: interactive, `end-of-phase`, automated-only `<verify>`): re-ran Task 1's exact `<verify>` command end-to-end after the commit -- passed, logged `⚡ Tracer verified end-to-end -- expanding`, continued to Task 2 without a checkpoint.
2. **Task 2: Every row at both levels, the frame-refusing CSP** (`tdd="true"`, type `auto`)
   - `e5fc05d` (test) -- extend the shape table to all 24 rows (18 + 6 edge); all 24 pass already (confirms Task 1's splice generalizes); `TestChromiumInjectsCSPNoScriptMeta` extended to require `frame-src`/`child-src` -- FAILS on current code (RED)
   - `8ef24d0` (feat) -- add `frame-src 'none'`/`child-src 'none'`; `CSP_LIVE_ROWS=18`; live gate: all 18 subtests PASS individually, `ALL PASS (8 checks)`
3. **Task 3: Both guards inverted, clean re-run, debug record completed** (type `auto`)
   - `f2b814c` (fix + docs) -- gofmt drift fix (Task 2's own formatting slip, caught by the clean re-run's `ci-local.sh gate`) + append the debug session's Evidence entry and `fix_applied` line

**Plan metadata commit:** (this commit, docs)

## Files Created/Modified

- `internal/convert/chromium.go` -- `injectCSPFirst` rewritten (document-start splice); `cspNoScriptMeta` extended with `frame-src`/`child-src 'none'`; `injectPrintCSS` untouched (byte-identical to 5747b40, verified by Task 3's gate)
- `internal/convert/chromium_test.go` -- `cspShapeRow`/`cspShapeTable`/`cspEdgeRows`, `validateCSPPrefixInvariant`, `TestInjectCSPFirstLeadsEveryShape`, `TestInjectCSPFirstKeepsPrintStyleAtHeadClose`, extended `TestChromiumInjectsCSPNoScriptMeta`; deleted `TestInjectCSPFirstPrecedesInHeadScript` and `TestInjectCSPFirstNoHeadFallsBackToHTMLOpen` (asserted the old splice offsets; their inputs now live as rows s3/e6)
- `internal/convert/chromium_live_test.go` (new) -- `liveConfinedChromium`, `TestChromiumCSPPrecedesEveryScriptLive`
- `scripts/sandbox-liveproof.sh` -- `CSP_LIVE_ROWS`, `run_html_csp_live_test`, `check_html_csp_live`, cross-compile wiring in the html branch
- `.planning/debug/csp-bypass-before-head.md` -- Evidence entry + `fix_applied` line appended; `status: diagnosed` and the existing content left as-is

## Decisions Made

See `key-decisions` in the frontmatter above.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] gofmt formatting drift introduced while extending the shape table**
- **Found during:** Task 3's clean re-run (`ci-local.sh gate`'s `gofmt -l .` check)
- **Issue:** Task 2's edit to `chromium_test.go` (rows e2/e3 in `cspEdgeRows`) left inline-comment alignment that `gofmt` would reformat -- `gofmt -l .` reported the file
- **Fix:** `gofmt -w internal/convert/chromium_test.go`
- **Files modified:** `internal/convert/chromium_test.go`
- **Verification:** `gofmt -l internal/convert/chromium_test.go` empty; full test suite and `ci-local.sh gate` pass afterward
- **Committed in:** `f2b814c`

---

**Total deviations:** 1 auto-fixed (1 bug, Rule 1)
**Impact on plan:** Purely cosmetic (gofmt formatting), caught and fixed within Task 3's own clean-re-run step before it could reach a commit. No scope creep, no behavior change.

## Issues Encountered

None.

## Live Evidence (for the human-check in Task 3's `<verify>`)

**Task 1 RED (row s1, pre-fix code):**
```
chromium_test.go:145: s1: injectCSPFirst output mismatch
     got: <!doctype html><script>...</script><html><head><meta ...CSP...><title>SAFE-...</title></head>...
    want: <!doctype html><meta ...CSP...><script>...</script><html><head><title>SAFE-...</title></head>...
```

**18-row live table (Task 2, commit `8ef24d0`, run id `1790479612-13724`):**
```
--- PASS: TestChromiumCSPPrecedesEveryScriptLive (7.82s)
    --- PASS: .../s1 (0.39s)  --- PASS: .../s2 (0.38s)  --- PASS: .../s3 (1.02s)
    --- PASS: .../s4 (0.44s)  --- PASS: .../s5 (0.41s)  --- PASS: .../s6 (0.37s)
    --- PASS: .../s7 (0.38s)  --- PASS: .../s8 (0.37s)  --- PASS: .../s9 (0.39s)
    --- PASS: .../x1 (0.30s)  --- PASS: .../x2 (0.32s)  --- PASS: .../x3 (0.33s)
    --- PASS: .../x4 (0.33s)  --- PASS: .../x5 (0.37s)  --- PASS: .../f1 (0.45s)
    --- PASS: .../f2 (0.73s)  --- PASS: .../f3 (0.45s)  --- PASS: .../f4 (0.38s)
csp-live: 18 rows: every control executed its script, every production render blocked it
```
Gate: `PASS: html csp-precedes-every-script` / `ALL PASS (8 checks)`.

**Inversion A (pre-fix splice restored, run id `1790479795-28344`) -- FAILs exactly 12, PASSes exactly 6:**
```
--- FAIL: .../s1  --- FAIL: .../s2  --- PASS: .../s3  --- FAIL: .../s4
--- FAIL: .../s5  --- FAIL: .../s6  --- FAIL: .../s7  --- FAIL: .../s8
--- FAIL: .../s9  --- FAIL: .../x1  --- FAIL: .../x2  --- FAIL: .../x3
--- FAIL: .../x4  --- PASS: .../x5  --- PASS: .../f1  --- PASS: .../f2
--- PASS: .../f3  --- PASS: .../f4
```
Matches the debug session's divergence table exactly (8/9-shape reproduction, generalized to the full 18-row table). Reverted with `git checkout -- internal/convert/chromium.go`.

**Inversion B (frame-src/child-src removed, run id `1790479891-35371`) -- FAILs exactly f1, f2:**
```
--- PASS: .../s1 .. .../x5 (all pass)  --- FAIL: .../f1  --- FAIL: .../f2  --- PASS: .../f3  --- PASS: .../f4
```
Reverted with `git checkout -- internal/convert/chromium.go`.

**Clean re-run (commit `f2b814c`, image `sha256:5df4e5c3a9418cd7e3aa226505534d8ee05bec38b077cfbed23fb2e043bad97f`):**
- `git diff --exit-code 5747b40 -- internal/convert/exec.go go.mod go.sum` -- exit 0 (D-12)
- `GOTOOLCHAIN=go1.26.4 scripts/ci-local.sh gate` -- `tier gate: PASS`
- `scripts/sandbox-liveproof.sh html` -- `ALL PASS (8 checks)`, 18/18 live rows

## User Setup Required

None -- no external service configuration required.

## Next Phase Readiness

- G-69-2 fully closed: the document-start splice and the frame-refusing CSP are both proven live, through the production `Convert` path, with both guards shown load-bearing by their own inversions.
- `CONF-01` is shared across plans 69-01 through 69-05; this plan's contribution is proven, but the requirement itself stays open until 69-05 (still unexecuted) also completes -- `requirements.ready-ids` confirms `0/1 ready` as of this SUMMARY.
- No blockers for phase 69 verification finalization once 69-05 lands.

---
*Phase: 69-html-probe-the-tmp-decision*
*Completed: 2026-09-27*

## Self-Check: PASSED

- All key files (`internal/convert/chromium.go`, `chromium_test.go`, `chromium_live_test.go`, `scripts/sandbox-liveproof.sh`, `.planning/debug/csp-bypass-before-head.md`, this SUMMARY) found on disk.
- All 5 task/deviation commits (`2fd8d56`, `d7ce865`, `e5fc05d`, `8ef24d0`, `f2b814c`) found in `git log`.
- Plan-level `<verification>` re-run on the committed tree (commit `f2b814c`): `ci-local.sh gate` -> `tier gate: PASS`; `git diff --exit-code 5747b40 -- internal/convert/exec.go go.mod go.sum` -> exit 0; `grep -c 'func TestChromiumCSPPrecedesEveryScriptLive('` -> 1; `grep -c 'csp-precedes-every-script'` -> 2.
