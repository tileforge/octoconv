---
phase: 69-html-probe-the-tmp-decision
reviewed: 2026-09-27T00:00:00Z
depth: standard
files_reviewed: 7
files_reviewed_list:
  - deploy/chart/octoconv/templates/deployment-chromium-worker.yaml
  - deploy/chart/octoconv/values.yaml
  - docker-compose.yml
  - internal/convert/chromium.go
  - internal/convert/chromium_live_test.go
  - internal/convert/chromium_test.go
  - scripts/sandbox-liveproof.sh
findings:
  critical: 0
  warning: 1
  info: 1
  total: 2
status: issues_found
---

# Phase 69: Code Review Report (incremental re-review)

**Reviewed:** 2026-09-27T00:00:00Z
**Depth:** standard
**Files Reviewed:** 7
**Status:** issues_found

## Summary

This is an incremental re-review scoped to the diff since `0c90bb6` — gap-closure
plans 69-04 (document-start CSP splice + `frame-src`/`child-src 'none'`, closing
UAT gap G-69-2) and 69-05 (large-document `/dev/shm` live check, closing UAT gap
G-69-3). The prior review's findings (CR-01: JS-disable CSP bypassable by
content preceding the literal `<head>`/`<html>` tag; WR-01: chart's `/dev/shm`
rationale contradicted the phase's own measurement; IN-01: no regression
coverage for pre-head scripts) were addressed in the earlier review cycle and
are re-verified here as closed — see "Prior findings re-verified" below.

**Focus per the task brief:**

1. *Can the CSP splice position still be preceded by any script-executing
   construct the HTML parser accepts (BOM, whitespace, comments, doctype
   variants, processing instructions, uppercase, NUL)?* I traced
   `injectCSPFirst` byte-by-byte against the HTML5 tokenizer's own state
   machine (markup-declaration-open requires an exact, unspaced `DOCTYPE`
   match; DOCTYPE state ends at the first `>` even inside a quoted
   identifier; ASCII whitespace is exactly TAB/LF/FF/CR/SPACE, not vertical
   tab) and cross-checked every claim against the 24-row shape table
   (`cspShapeTable`/`cspEdgeRows`, `chromium_test.go`) and the 18-row live
   differential (`chromium_live_test.go`). `LooksLikeHTML` (unchanged,
   `htmlsniff.go`) still gates that nothing whatsoever — comment, PI,
   script, NUL — can precede the required `<!doctype html`/`<html` prefix,
   and rejects NUL/invalid-UTF-8 upstream in `ValidateSniffPrefix`. I could
   not construct a shape that reaches `ChromiumConverter.Convert` (i.e.
   passes `LooksLikeHTML`) and still executes a script ahead of the spliced
   CSP. I did not find a bypass. See the WARNING below for a real, but
   non-security, maintainability gap in how the fix's own live coverage is
   kept in sync.
2. *Could the live tests pass vacuously?* `liveConfinedChromium` does
   `t.Skip` when `OCTOCONV_CHROMIUM_LIVE != "1"`, but
   `scripts/sandbox-liveproof.sh`'s `check_html_csp_live` and
   `check_html_large_document_live` both explicitly grep for `--- SKIP` and
   treat its presence as a hard FAIL, and `check_html_csp_live` additionally
   counts `--- PASS: .../<row>` lines and fails unless the count exactly
   equals a fixed row count — so neither check can pass by silently skipping
   or by only running a subset of rows. Both live tests judge success by PDF
   *content* (`requireOutput`/`requireAbsent`), never chromium's exit status
   (documented and re-confirmed: chromium exits 0 and writes a blank
   652-byte PDF on denial), and the large-document test's negative control
   (running the identical fixture *without* `--disable-dev-shm-usage`)
   asserts the run must fail with `/dev/shm` in stderr — i.e. the method is
   proven capable of detecting the very dependency it claims is absent, not
   merely asserting an untested absence. I ran the full non-live suite
   (`go test ./internal/convert/... -run 'TestInjectCSPFirst|TestChromium|TestInjectPrintCSS' -v`)
   and confirmed both live tests skip cleanly outside the image while all 24
   `TestInjectCSPFirstLeadsEveryShape` subtests and the composition/argv
   tests pass. I did not find a vacuous-pass path.

I also confirmed by direct testing that this incremental scope is otherwise
clean: `go build ./...`, `go vet ./internal/convert/...`, `shellcheck
scripts/sandbox-liveproof.sh`, and `helm lint`/`helm template --show-only
templates/deployment-chromium-worker.yaml` (after supplying dummy secrets) all
pass with no diagnostics, and no stale `shmSize`/`shm_size`/`headOpenPrefix`
references remain anywhere in the tree after the `/dev/shm` mount and the old
tag-search removals.

## Prior findings re-verified (from diff_base's review cycle)

- **CR-01** (JS-disable CSP bypassable by pre-`<head>` content): fixed by
  `injectCSPFirst`'s document-start splice (`internal/convert/chromium.go`);
  independently re-derived and confirmed correct against the real HTML5
  tokenizer semantics in this review, not merely re-asserted.
- **WR-01** (chart `/dev/shm` rationale contradicted the phase's own
  measurement): resolved — the `/dev/shm` `emptyDir` block, `shmSize` values
  key, and `docker-compose.yml`'s `shm_size: "256m"` were all removed
  together, and the removal is backed by a new live differential test
  (`TestChromiumLargeDocumentNeedsNoDevShmLive`) with a genuine negative
  control, not just an updated comment.
- **IN-01** (no regression coverage for pre-head scripts): resolved — the
  24-row shape table plus the 18-row live differential now covers exactly
  this gap, including uppercase, BOM, vertical-tab, quoted-doctype-`>`, and
  sub-document (iframe/frameset/object/embed) variants.

## Warnings

### WR-02: The live CSP row count is a hand-maintained magic number, not derived from the shape table

**File:** `scripts/sandbox-liveproof.sh:21-24`, cross-referenced against
`internal/convert/chromium_test.go:83-171` (`cspShapeTable`)

**Issue:** `CSP_LIVE_ROWS=18` is a literal integer that must be kept in sync
by hand with the row count `cspShapeTable` actually returns (currently
s1-s9 + x1-x5 + f1-f4 = 18). `check_html_csp_live` does fail loudly if the
counts diverge (`pass_count -ne CSP_LIVE_ROWS`), so a future contributor who
adds or removes a shape-table row without touching this constant gets a
clear gate failure rather than a silent false pass — this is not a vacuous-
pass risk. But it is still a maintainability trap: the failure message
("found N passing subtests, want 18") gives no hint that the fix is to
update `CSP_LIVE_ROWS`, and a contributor unfamiliar with this file could
spend real time trying to make the Go side match a stale shell constant
instead of the reverse.

**Fix:** Either derive the expected count at gate time (e.g. have the Go test
binary itself print the row count via a small `-test.run
'^TestCSPShapeRowCount$'` helper that the shell script parses, or emit the
count as part of `TestChromiumCSPPrecedesEveryScriptLive`'s own `t.Logf`
line so the shell script can `grep` it instead of hardcoding it), or at
minimum add a comment at the `cspShapeTable`/`cspEdgeRows` call site in
`chromium_test.go` reminding the editor to update
`scripts/sandbox-liveproof.sh`'s `CSP_LIVE_ROWS` in the same change.

## Info

### IN-02: Test-only process-exec hardening duplicates exec.go's logic without a drift guard

**File:** `internal/convert/chromium_live_test.go:249-293` (`liveEngineRun`)

**Issue:** `liveEngineRun` re-implements the process-group hardening
(`Setpgid`, SIGKILL-the-group on context cancellation, `exec.ExitError`
unwrapping) that `internal/convert/exec.go`'s `runCommandCore` already
implements, deliberately kept as a separate copy so `exec.go` can stay
byte-identical across this phase (D-12, per the function's own doc comment).
This is a reasonable short-term tradeoff, but there is no test asserting the
two implementations stay equivalent — if `exec.go`'s hardening is extended
later (e.g. an additional signal, an additional environment scrub, a change
to how `ctx.Done()` is raced against `cmd.Wait()`), this test's local copy
will silently diverge from the production path it claims to exercise, and
the large-document live test would then be validating a slightly different
execution model than `ChromiumConverter.Convert` actually uses in
production.

**Fix:** No action required for this phase (the tradeoff is explicit and
documented), but when `exec.go`'s hardening next changes, revisit whether
`liveEngineRun` should be updated in the same change, or whether the shared
logic can be factored into something both `exec.go` and this test call
without `exec.go` itself changing shape.

---

_Reviewed: 2026-09-27T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
