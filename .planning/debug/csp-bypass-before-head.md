---
status: diagnosed
trigger: "DATA_START\nUAT gap G-69-2 (from .planning/phases/69-html-probe-the-tmp-decision/69-UAT.md and 69-REVIEW.md CR-01): The JS-disable CSP injected by injectCSPFirst governs every script in an uploaded HTML document, including a <script> placed before the literal <head>/<html> tag -- goal: find_root_cause_only, confirm/refute EMPIRICALLY against real chromium-headless-shell, do not fix code.\nDATA_END"
created: 2026-09-27T00:20:00Z
updated: 2026-09-27T00:52:58Z
---

## Current Focus

hypothesis: CONFIRMED (see Resolution) -- injectCSPFirst's byte-search injection point does not correspond to where chromium's HTML5 parser actually places head content when a literal <head>/<html> tag is preceded by other markup (a script, or any other tag/comment matching or not matching the "<head"/"<html" byte search), so the CSP frequently lands outside <head> in the real DOM and is rejected entirely by chromium, not merely "governed only from that point forward."
test: empirical differential test against the real chromium-headless-shell binary shipped in Dockerfile.chromium-worker (153.0.8010.52), 9 crafted shapes x 2 variants (current injectCSPFirst vs CR-01's proposed candidate fix), each converted via the exact production chromiumPrintArgs argv, PDF text-extracted with pdftotext and cross-checked against chromium's own console diagnostics.
expecting: shapes with content before the literal <head>/<html> tag show the script's effect (document.write('RAN')) present in the output PDF under current code; candidate fix shows it absent in all 9 shapes including the already-fixed control case.
next_action: none -- root cause confirmed, goal is find_root_cause_only; return diagnosis to caller. Docker artifacts (image octoconv-chromium-worker:debug-probe) removed; no docker compose was used; no tracked files were modified.
bug_class: bohrbug
reasoning_checkpoint: null
tdd_checkpoint: null

## Symptoms

expected: The JS-disable CSP injected by injectCSPFirst (internal/convert/chromium.go) disables JavaScript for every script in an uploaded HTML document converted by the chromium (html->pdf) engine, regardless of where a <script> tag is textually positioned relative to the literal <head>/<html> open tag.
actual: injectCSPFirst locates the injection point via a bare byte substring search for "<head" then "<html" (internal/convert/chromium.go:99-113). A script (or any other markup) placed before the literal <head>/<html> tag in the uploaded HTML is moved by chromium's HTML5 parser's implicit-head-creation algorithm into an auto-created <head>/<body> before the literal tag is ever reached (the literal <head> is then simply dropped as a duplicate). The CSP, spliced immediately after the byte offset of the literal "<head"/"<html" match, ends up positioned in the DOM's <body> (not <head>), and is REJECTED OUTRIGHT by chromium ("delivered via a <meta> element outside the document's <head>, which is disallowed. The policy has been ignored.") rather than merely taking effect "too late." Any script anywhere in the document then runs unrestricted.
errors: none (chromium exits 0, produces a valid PDF, only a benign INFO-level console log records the CSP rejection: "The Content Security Policy ... was delivered via a <meta> element outside the document's <head>, which is disallowed. The policy has been ignored.")
reproduction: Convert an HTML document shaped like `<!doctype html><script>document.write('RAN')</script><html><head>...` (or any of the 9 shapes below) through ChromiumConverter.Convert; the produced PDF contains "RAN" (i.e. the script executed).
started: Present since injectCSPFirst was introduced (Plan 04 / CR-01 partial fix in phase 69); CR-01's own regression tests only cover scripts positioned AFTER the injection point, never before it (69-REVIEW.md IN-01).

## Eliminated

(none -- single hypothesis, confirmed on first empirical test; see RCA branching note in Resolution for why this stays single-cause)

## Evidence

- timestamp: 2026-09-27T00:20:00Z
  checked: internal/convert/chromium.go (injectCSPFirst, injectPrintCSS, chromiumPrintArgs, Convert), internal/convert/htmlsniff.go (LooksLikeHTML), internal/convert/chromium_test.go
  found: injectCSPFirst does a bare bytes.Index for "<head" then "<html" with no tag-boundary check (contrast with htmlTagMarker in htmlsniff.go, which DOES check the following byte). LooksLikeHTML only requires the upload to *start with* "<!doctype html" or "<html" (immediately, after BOM/whitespace trim) -- it does not require canonical document shape, so content between the doctype and the literal <html>/<head> tag is permitted by the sniff gate. Existing regression tests (TestInjectCSPFirstPrecedesInHeadScript, TestInjectCSPFirstNoHeadFallsBackToHTMLOpen) only place the adversarial <script> AFTER the injection point.
  implication: the vulnerability the code review (CR-01) describes is structurally present and untested; textual/parser-order divergence is real code as written, but needed empirical confirmation against a real browser (project rule: don't predict a parser).

- timestamp: 2026-09-27T00:35:00Z
  checked: Dockerfile.chromium-worker (built as octoconv-chromium-worker:debug-probe via `docker build -f Dockerfile.chromium-worker -t octoconv-chromium-worker:debug-probe .`), chromium-headless-shell --version inside the built image
  found: Chromium 153.0.8010.52 (the exact binary the production chromium-worker container ships and runs).
  implication: subsequent differential tests run against the real, production-identical engine binary, not a theoretical parser reading.

- timestamp: 2026-09-27T00:45:00Z
  checked: 9 crafted HTML shapes (s1-s9, see below), each rendered through BOTH the current injectCSPFirst (copied byte-for-byte from chromium.go into a standalone scratch Go program, scratchpad/csp_probe/gen.go) and CR-01's proposed candidate fix (splice right after "<!doctype...>" or at byte 0), then each of the 18 resulting HTML files fed to the real chromium-headless-shell binary with the EXACT production argv from chromiumPrintArgs (chromium.go:234-249: --headless --disable-gpu --no-sandbox --disable-dev-shm-usage --no-pdf-header-footer --user-data-dir=<profile> --proxy-server=127.0.0.1:9 --proxy-bypass-list=<-loopback> --host-resolver-rules='MAP * ~NOTFOUND' --print-to-pdf=<out> file://<rendered>), output PDF text-extracted with pdftotext and cross-checked against chromium's own stderr CONSOLE log lines.
  found: DIVERGENCE TABLE (RAN = document.write('RAN') text found in the produced PDF, i.e. script executed; console column quotes chromium's own diagnostic):
  | # | Shape | Current: script ran? | Current: chromium console | Candidate: script ran? | Candidate: chromium console |
  |---|---|---|---|---|---|
  | s1 | `<!doctype html><script>RAN</script><html><head>...` (canonical CR-01 case) | YES (bypass) | "CSP...delivered via a <meta> element outside the document's <head>, which is disallowed. The policy has been ignored." | NO (blocked) | "Executing inline script violates...CSP...script-src 'none'...action has been blocked." |
  | s2 | `<html><script>RAN</script><head>...` (starts with literal `<html`, script before literal `<head`) | YES (bypass) | same "outside <head>...ignored" | NO (blocked) | same "...blocked." |
  | s3 | control: `<html><head><script>RAN</script>...` (script legitimately inside literal head -- the already-fixed CR-01 case) | NO (blocked) | same "...blocked." | NO (blocked) | same "...blocked." |
  | s4 | `<header>decoy</header>` before `<script>` before real `<head>` (decoy triggers implicit head-close ahead of the script) | YES (bypass) | "outside <head>...ignored" | NO (blocked) | "...blocked." |
  | s5 | `<script>RAN</script>` before `<header>decoy</header>` before real `<head>` | YES (bypass) | "outside <head>...ignored" | NO (blocked) | "...blocked." |
  | s6 | `<script>RAN</script>` before an HTML comment containing literal text `<head>` before real `<head>` | YES (bypass) | **no CSP console message at all** | NO (blocked) | "...blocked." |
  | s7 | HTML comment containing literal text `<head>` before `<script>RAN</script>` before real `<head>` | YES (bypass) | **no CSP console message at all** | NO (blocked) | "...blocked." |
  | s8 | uppercase tags (`<!DOCTYPE HTML><SCRIPT>...<HTML><HEAD>...`) | YES (bypass) | "outside <head>...ignored" | NO (blocked) | "...blocked." |
  | s9 | UTF-8 BOM prefix + s1 shape | YES (bypass) | "outside <head>...ignored" | NO (blocked) | "...blocked." |
  implication: 8 of 9 shapes are LIVE, confirmed bypasses under current code (only the already-fixed in-head control, s3, is blocked); ALL 9 are blocked under the candidate fix, including every decoy variant. The failure mechanism is stronger than "CSP governs only content after it in source order": chromium's own diagnostics show the CSP is not merely late, it is either (a) relocated by the parser into `<body>` and REJECTED OUTRIGHT ("delivered...outside the document's <head>...disallowed...ignored" -- s1,s2,s4,s5,s8,s9), or (b) never parsed as markup at all because the bare substring match ("<head") fires on text INSIDE an HTML comment, splicing the CSP's literal bytes into the comment's content where it is inert (s6,s7 -- no CSP console message whatsoever, confirming it was swallowed as comment text, not installed as a policy).

- timestamp: 2026-09-27T00:50:00Z
  checked: git status/diff on internal/, Dockerfile.chromium-worker, docker-compose.yml after the probe; docker image/container cleanup
  found: no tracked files modified (git diff clean); no `docker compose` was invoked at any point (a single throwaway `docker build`/`docker run --rm` against Dockerfile.chromium-worker was used instead, to avoid the documented OrbStack compose+k8s daemon-wedge risk while the k8s stand was live); the probe image `octoconv-chromium-worker:debug-probe` was removed after the run; all crafted shapes/PDFs live only in the session scratchpad, never in the repo.
  implication: investigation left zero residue in the tracked tree, consistent with the find_root_cause_only / do-not-fix mandate.

## Resolution

root_cause: injectCSPFirst (internal/convert/chromium.go:99-113) chooses the CSP's injection *byte offset* by a bare, tag-boundary-free substring search for "<head" then "<html" in the raw upload, which assumes textual/source order equals HTML5 tree-construction order. It does not. LooksLikeHTML (htmlsniff.go) only requires the upload to literally start with "<!doctype html"/"<html", so a client can place a <script> tag -- or any other element/decoy text (a `<header>` tag, or literal text "<head>" sitting inside an HTML comment) -- between that required prefix and the literal `<head>`/`<html` tag injectCSPFirst searches for. When chromium's real HTML5 parser encounters that earlier content while still in "before html"/"before head" insertion mode, it implicitly opens `<html>`/`<head>`, executes/inserts the earlier content there or in the auto-created `<body>`, and then DISCARDS the later literal `<head>`/`<html>` tag as a duplicate. Because injectCSPFirst spliced the CSP relative to that now-discarded literal tag's byte offset, the CSP either (a) lands inside the parser's now-`<body>` context and is rejected outright by chromium ("delivered via a <meta> element outside the document's <head>, which is disallowed") rather than merely taking effect late, or (b) when the false-positive match is inside an HTML comment, is spliced as literal comment text and never parsed as markup at all. Either way, no active CSP exists when the pre-head script executes, so D-05's JS-disable is defeated. Empirically confirmed live against chromium-headless-shell 153.0.8010.52 (the production binary) for 8/9 crafted shapes; the 9th (script legitimately inside the literal `<head>`, CR-01's already-fixed case) remains correctly blocked, confirming the defect is specific to content preceding the literal `<head>`/`<html>` tag, not a general CSP-injection failure.
fix: not applied (goal: find_root_cause_only, per objective -- plan-phase --gaps handles the fix)
verification: not applicable in this mode -- empirical confirmation performed instead (see Evidence divergence table); the CR-01-proposed candidate fix (splice the CSP unconditionally right after `<!doctype ...>` or at byte 0, never searching for `<head`/`<html`) was differentially tested against the same 9 shapes and blocked the script in all 9, including the 6 that reproduce the bypass under current code and the 2 decoy-specific shapes (s6/s7) that are not literally the same mechanism the review text emphasizes -- so the candidate fix direction is empirically validated as sufficient for the cases tested, not merely spec-argued.
oracle_type: derived (chromium's own console diagnostics + produced PDF text content served as the oracle, cross-checked two independent signals rather than one)
files_changed: []
