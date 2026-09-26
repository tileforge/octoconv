---
phase: 69-html-probe-the-tmp-decision
reviewed: 2026-09-26T21:51:11Z
depth: standard
files_reviewed: 47
files_reviewed_list:
  - .env.example
  - cmd/chromium-worker/main.go
  - cmd/probe-worker/main.go
  - deploy/chart/octoconv/templates/configmap.yaml
  - deploy/chart/octoconv/templates/deployment-chromium-worker.yaml
  - deploy/chart/octoconv/templates/deployment-probe-worker.yaml
  - docker-compose.yml
  - internal/convert/chromium_test.go
  - internal/convert/chromium.go
  - internal/convert/htmlsandbox_test.go
  - internal/convert/htmlsandbox.go
  - internal/convert/ocrsandbox.go
  - internal/convert/pdflex_test.go
  - internal/convert/pdflexr4_test.go
  - internal/convert/pdflexr5_test.go
  - internal/convert/probebounds_test.go
  - internal/convert/probesandbox_test.go
  - internal/convert/probesandbox.go
  - internal/convert/progressexec.go
  - internal/convert/sandboxcoverage_test.go
  - internal/convert/sandboxexec_test.go
  - internal/convert/sandboxpreflight_test.go
  - internal/convert/sandboxpreflight.go
  - internal/convert/sandboxwrap.go
  - internal/convert/sanitize.go
  - internal/convert/sanitizeconverter.go
  - internal/convert/sanitizeicc_test.go
  - internal/convert/sanitizeimage_test.go
  - internal/convert/sanitizeimage.go
  - internal/convert/sanitizemainpart_test.go
  - internal/convert/sanitizeooxml_test.go
  - internal/convert/sanitizeooxml.go
  - internal/convert/sanitizepdf_test.go
  - internal/convert/sanitizepdf.go
  - internal/convert/sanitizepr14_test.go
  - internal/convert/sanitizepr14r2_test.go
  - internal/convert/sanitizereport_test.go
  - internal/convert/sanitizeverify_config_test.go
  - internal/convert/sanitizeverify_test.go
  - internal/convert/sanitizeverify.go
  - internal/e2e/sanitize_ceiling_e2e_test.go
  - internal/e2e/sanitize_confined_e2e_test.go
  - internal/worker/descriptors.go
  - internal/worker/engine_sandbox_retryable_test.go
  - internal/worker/worker.go
  - scripts/conf05-crosscheck.sh
  - scripts/sandbox-liveproof.sh
findings:
  critical: 1
  warning: 1
  info: 1
  total: 3
status: issues_found
---

# Phase 69: Code Review Report

**Reviewed:** 2026-09-26T21:51:11Z
**Depth:** standard
**Files Reviewed:** 47
**Status:** issues_found

## Summary

Phase 69 confines the html (chromium) and probe (pdfinfo/gs/unzip/file) engine
classes under Landlock, following the `engineExec` pattern established in
phase 68. The Landlock plumbing itself is careful and well-tested: the
shared-`/tmp` concurrency guard (`sharedTmpGuard`/D-03), the `/tmp` sweep
(D-04), the per-exec environment allowlist/TMPDIR handling
(`engineEnv`/`forJobRW`/`.withEnv`), the retryable-vs-terminal classification
of `ErrEngineSandboxUnavailable` (checked first in every classifier, with a
dedicated non-vacuity test in `engine_sandbox_retryable_test.go`), and the
preflight sufficiency/effectiveness judgments (judged by PDF/output *content*,
never bare exit status, per the documented ghostscript/chromium measurements)
all hold up under inspection. `scripts/sandbox-liveproof.sh` and
`scripts/conf05-crosscheck.sh` correctly avoid the bash 3.2
declared-but-empty-array pitfall the codebase has been bitten by before.

The one serious finding is unrelated to the Landlock ruleset itself: the
mechanism this phase's own `chromium.go` relies on to disable JavaScript
execution (D-05, `cspNoScriptMeta` injected via `injectCSPFirst`) can be
bypassed by content placed before the document's literal `<head>`/`<html>`
open tag, which HTML5's lenient parsing algorithm still executes — and the
existing regression test (`TestInjectCSPFirstPrecedesInHeadScript`) only
covers the *already-fixed* case (a script living inside a literal `<head>`),
not this one. Given `--no-sandbox` is required for this engine (per D-08) and
the Landlock ruleset grants broad read paths (`/proc`, `/sys/devices`,
`/etc/hosts`, `/etc/resolv.conf`, etc.), a working script-execution bypass is
a meaningful escalation, not just a policy violation.

## Critical Issues

### CR-01: JS-disable CSP can be bypassed by content preceding the literal `<head>`/`<html>` tag

**File:** `internal/convert/chromium.go:89-113` (`injectCSPFirst`), used by `internal/convert/chromium.go:177`

**Issue:**

`injectCSPFirst` is the *only* mechanism that disables JavaScript execution in
chromium-headless-shell (D-05's own doc comment: the launch flags
`--blink-settings=scriptEnabled=false` and `--disable-javascript` were both
measured to not work, so the CSP meta tag is it). It decides where to splice
the CSP by a raw byte search:

```go
func injectCSPFirst(html []byte, csp string) []byte {
	lower := bytes.ToLower(html)
	if idx := bytes.Index(lower, headOpenPrefix); idx >= 0 {   // "<head"
		if end := bytes.IndexByte(html[idx:], '>'); end >= 0 {
			return spliceAt(html, idx+end+1, csp)
		}
	}
	if idx := bytes.Index(lower, htmlOpenPrefix); idx >= 0 {   // "<html"
		...
	}
	return spliceAt(html, 0, csp)
}
```

Two independent problems compound here:

1. **Textual position is not parse order.** `LooksLikeHTML`
   (`internal/convert/htmlsniff.go`) only requires the upload to *start with*
   `<!doctype html` or `<html` (after BOM/whitespace trimming) — it does not
   require a canonical `<html><head>...</head><body>` shape. A client can
   supply:

   ```html
   <!doctype html>
   <script>fetch('file:///etc/hosts').then(r=>r.text()).then(t=>document.title=t)</script>
   <html><head><title>x</title></head><body></body></html>
   ```

   `injectCSPFirst` finds the real, literal `<head>` (there is no decoy
   needed) and splices the CSP right after it — but per the HTML5 tree
   construction algorithm, a `<script>` encountered while still in the
   "before html"/"before head" insertion modes causes the parser to
   *implicitly* create `<html>`/`<head>`, insert the script as the head's
   first child, and **execute it synchronously**, before the parser ever
   reaches the literal `<head>` tag later in the byte stream (which is then
   simply ignored: "in head" insertion mode drops a second `head` start tag).
   The CSP therefore lands in the DOM *after* the script has already run.
   `injectCSPFirst`'s own doc comment states the CSP "only governs content
   that appears AFTER it in **source order**" — but source order and DOM
   insertion order diverge exactly in this case, which is the one case the
   function does not defend against.

2. **The marker search itself has false positives.** `headOpenPrefix =
   []byte("<head")` and `htmlOpenPrefix = []byte("<html")` are matched as
   bare substrings, with no check that the next byte is `>`, `/`, or
   whitespace (contrast this with `htmlTagMarker`'s handling in
   `htmlsniff.go`, which *does* check `rest[0]` for exactly this reason). Any
   incidental occurrence of these five/six bytes — a custom tag name like
   `<header>`/`<headache>`/`<htmlx>`, or literal text — that happens to
   appear before a genuine `<head>`/`<html>` tag will be matched first,
   moving the injection point later than intended. This independently
   defeats the CR-01 fix that is already in place for the "script lives
   inside `<head>`" case whenever such a decoy precedes it.

The existing regression tests
(`TestInjectCSPFirstPrecedesInHeadScript`,
`TestInjectCSPFirstNoHeadFallsBackToHTMLOpen`) only cover a script placed
*after* the head-open tag, or with no head tag at all (falls back to
`<html`). Neither covers a script preceding the literal `<head>`/`<html>`
tag, which is the gap above.

**Impact:** With `--no-sandbox` required for this engine (D-08's own comment:
"omitting it fails with 'No usable sandbox!'"), a working script bypass means
attacker-controlled JavaScript executes inside the chromium renderer that
converts every html→pdf job. The network-block flags
(`--proxy-server=127.0.0.1:9`, `--host-resolver-rules=MAP * ~NOTFOUND`) still
apply and should prevent DNS/HTTP exfiltration, but `file://` resource loads
and any local capability chromium exposes to script are not equivalently
constrained by those flags — they are bounded only by the Landlock read-only
set (`/proc`, `/sys/devices`, `/etc/hosts`, `/etc/resolv.conf`,
`/etc/nsswitch.conf`, fonts/libs, …), which was designed and measured on the
assumption that script is *disabled*, not on the assumption that a script
might run and enumerate/read those paths itself. This also means the
project's own documented threat "file:// subresources and iframes are
resolved by the engine" (69-01-PLAN.md's threat table) is defended by
Landlock alone once JS is live, rather than by both the CSP *and* Landlock as
intended.

**Fix:**

Stop trying to find the "right" tag at all — splice the CSP unconditionally
at the earliest point nothing else can precede, mirroring the function's own
existing (but currently last-resort) `position 0` fallback:

```go
func injectCSPFirst(html []byte, csp string) []byte {
	// Nothing may legitimately precede a doctype declaration except
	// whitespace/BOM (already stripped before this runs); inserting right
	// after it -- or at position 0 when there is none -- is the only
	// injection point no script in the document can execute ahead of,
	// regardless of where a literal <head>/<html> tag later appears in the
	// byte stream (HTML5's implicit-head-creation defeats any textual
	// "<head"/"<html" search: see CR-01's original finding, and its gap).
	lower := bytes.ToLower(html)
	if idx := bytes.Index(lower, []byte("<!doctype")); idx == 0 {
		if end := bytes.IndexByte(html, '>'); end >= 0 {
			return spliceAt(html, end+1, csp)
		}
	}
	return spliceAt(html, 0, csp)
}
```

At minimum, add a regression test asserting the CSP precedes a `<script>`
placed *before* the literal `<head>`/`<html>` tag (with and without a
`<!doctype html>` prefix), and tighten `headOpenPrefix`/`htmlOpenPrefix`
matching to require a following `>`, `/`, or whitespace byte (matching
`htmlsniff.go`'s own precedent) if the tag-search approach is kept at all.

## Warnings

### WR-01: Deployment chart's `/dev/shm` rationale is unreconciled with this phase's own Landlock measurement

**File:** `deploy/chart/octoconv/templates/deployment-chromium-worker.yaml:9-12`

**Issue:** The chart mounts a 256Mi `medium: Memory` emptyDir at `/dev/shm`
with the comment: "Even with `--disable-dev-shm-usage` some Chromium internal
paths still touch `/dev/shm`, so an explicit `medium:Memory` emptyDir sized
256Mi recreates the compose behaviour exactly." This phase's own measurement
in `.planning/research/SANDBOX-PATHS.md` ("html" section) states the
opposite, explicitly verified by removal: "`/dev/shm` и `/run` в наборе **не
нужны** — проверено снятием" ("not needed — verified by removal"), and
`htmlReadOnly`/`htmlReadWrite` (`internal/convert/htmlsandbox.go`) grant
neither path. Under the now-confined worker, any internal chromium touch of
`/dev/shm` that the k8s comment claims still happens would be *denied* by
Landlock (the path is not in either grant list) — so either the k8s comment
is stale (the mount/volume can likely be removed, since the ruleset already
proves sufficiency without it) or there is a real, unaddressed gap where a
production-scale document could touch `/dev/shm` in a way the preflight's
small fixture never exercises, and Landlock would turn that into a job
failure that the sandbox-liveproof gate's own fixture would never catch.
Since this file is part of this phase's changeset (the `extraEnv` block was
added to it) but the `/dev/shm` rationale was not revisited, the two
statements about the same fact now openly disagree in the tree.

**Fix:** Reconcile the comment with the phase 69 measurement — either drop
the `/dev/shm` volume/mount now that the ruleset has been measured not to
need it, or, if there's a reason production-scale documents might still touch
it (the compose file's `shm_size: "256m"` predates this phase and was kept),
say so explicitly and cross-reference the SANDBOX-PATHS.md finding so a
future reader doesn't have to notice the contradiction independently.

## Info

### IN-01: No regression coverage for scripts placed before the head/html open tag

**File:** `internal/convert/chromium_test.go:65-107`

**Issue:** `TestInjectCSPFirstPrecedesInHeadScript` and
`TestInjectCSPFirstNoHeadFallsBackToHTMLOpen` are the only two `injectCSPFirst`
regression tests, and both position the adversarial `<script>` *after* the
point `injectCSPFirst` will inject the CSP. Neither exercises a script
positioned before a literal `<head>`/`<html>` tag (CR-01's gap), nor a decoy
tag (`<header>`, `<htmlx>`, …) preceding the genuine one.

**Fix:** Add both cases as named regression tests once CR-01 is fixed, so a
future refactor of `injectCSPFirst` cannot reintroduce either gap silently.

---

_Reviewed: 2026-09-26T21:51:11Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
