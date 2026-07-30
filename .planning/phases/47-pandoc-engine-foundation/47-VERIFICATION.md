---
phase: 47-pandoc-engine-foundation
verified: 2026-07-30T13:14:49Z
status: passed
score: 5/5 success criteria verified (6/6 requirements satisfied)
overrides_applied: 0
---

# Phase 47: Pandoc Engine Foundation Verification Report

**Phase Goal:** A standalone `PandocConverter` converts between markup-family formats (md/rst/org/latex/epub/html/docx) through a closed `(from→to)` allowlist, security-hardened from the first line (no raw HTML/TeX passthrough, `--sandbox`, no network), built and tested directly against the real pandoc binary — before any queue/worker plumbing.
**Verified:** 2026-07-30T13:14:49Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| SC1 | `PandocConverter` converts between any two markup formats in a closed `(from→to)` allowlist; unlisted pair rejected before any subprocess | ✓ VERIFIED | `pandocPairTable` (`internal/convert/pandoc.go:45-53`) is a hardcoded, non-cartesian map across md/rst/org/latex/html/docx/epub, excluding identity + epub→latex/org. `pandocPairSupported` gates `Convert` before `runCommand` is ever invoked (`pandoc.go:294-296`). `TestPandocPairsClosedSet`, `TestPandocPairSupportedMatchesPairs`, `TestPandocConvertUnsupportedPairRejectedBeforeSubprocess` all pass. `TestPandocConvertRealMatrix` proves md→html, md→docx, rst→html, latex→md, html→md, md→epub, docx→md all succeed against the real pandoc 3.10.1 binary (3.21s, all green). |
| SC2 | Structural-target output (docx/epub/html) produced with `--standalone`, validated by the same content-sniffer that guards uploads | ✓ VERIFIED | `pandocStructuralTargets` = {html, latex, docx, epub} (`pandoc.go:60-65`); `pandocConvertArgs` force-appends `--standalone` for these targets, deduped against opts (`pandoc.go:208-211`). `pandocValidateOutput` dispatches to `LooksLikeHTML`/`LooksLikeLaTeX`/`SniffContainer`/`LooksLikeText` per target (`pandoc.go:235-276`) — the same sniffers that gate uploads (`htmlsniff.go`, `docsniff.go`, `markupsniff.go`). Real-matrix test outputs (md→docx, md→epub, etc.) pass their respective sniffers. |
| SC3 | Every pandoc invocation hardened (`--sandbox`, no raw HTML/TeX passthrough, no network) via `runCommand`; crafted input cannot trigger shell-escape/`\input`/network-fetch | ✓ VERIFIED | `pandocConvertArgs` always appends `--sandbox` (`pandoc.go:205`); `pandocReaderSpec` builds the reader spec from a per-source table (md/html/epub → `-raw_html-raw_tex`, latex → `-raw_tex` only, rst/org/docx → bare) — never a blind blanket suffix, never `+raw_*` (confirmed `+raw_html`/`+raw_tex` occur 0 times as real usage via grep). `Convert` dispatches exclusively through the unmodified `runCommand` (`exec.go`, Setpgid + SIGKILL-on-timeout, no shell). Three SC3 canaries (`TestPandocSecurityShellEscapeDefenseInDepth`, `TestPandocSecurityInputLeakBlockedBySandbox`, `TestPandocSecurityNetworkFetchBlockedOffline`) pass against real pandoc 3.10.1 — **independently re-verified by the verifier** by running the equivalent raw `pandoc` invocations outside the Go test harness: `\write18{touch ...}` produced no sentinel file; `\input{<secret>}` produced a "Could not load include file" warning and the secret marker never appeared in output; the `.invalid` remote URL was left as an unresolved literal token with sub-second completion (no network hang). |
| SC4 | Closed `PandocOpts` allowlist rejects any unlisted opt before work; injection test proves client bytes never reach argv unvalidated | ✓ VERIFIED | `PandocOpts` (`pandocopts.go:38-44`) is a closed 5-field struct; `ParsePandocOpts` uses `checkStrictObject` (opts.go, reused verbatim) + `json.Decoder.DisallowUnknownFields` + a `pandocWrapAllowlist` map-gate on `Wrap` (`pandocopts.go:53-70`). `pandocArgs` emits every value as a discrete argv element, `--wrap=<enum>` the only value-bearing flag (`pandocopts.go:127-145`). `TestPandocOptsInjection` feeds 5 hostile crafted `wrap` payloads (`rm -rf /`, command substitution, flag-injection, newline-injection) and asserts all are rejected at `ParsePandocOpts` — none ever reaches `pandocArgs`. All opts tests pass (`TestParsePandocOpts`, `TestPandocOptsFromMap`, `TestPandocArgs`, `TestValidatePandocApplicability`, `TestPandocOptsInjection`). |
| SC5 | Uploads gated fail-closed by magic-byte/content validation across the markup family + oversize/pathological input rejected before conversion | ✓ VERIFIED (converter-level; upload-gate wiring correctly deferred to Phase 48) | `LooksLikeLaTeX` (UTF-8 + no-NUL + `\documentclass`/`\begin{`/leading control-word marker) and `LooksLikeText` (UTF-8 + no-NUL + no disallowed C0 control bytes) exist and are unit-tested fail-closed (`markupsniff.go:68-148`); the md/rst/org content-interchangeability limitation is explicitly documented as an "ACCEPTED LIMITATION" in `LooksLikeText`'s doc-comment (`markupsniff.go:126-141`), not silent. `enforcePandocInputSize`/`ErrPandocInputTooLarge` reject oversize input fail-closed before any subprocess and are called from `Convert` before `runCommand` (`pandoc.go:303-305`). `SniffContainer` recognizes epub via its `application/epub+zip` mimetype-first-entry signal, reusing the existing single-pass zip walk (`docsniff.go`). `MIMEType` extended for md/rst/org/latex/epub (`sniff.go`). `internal/api/handlers.go` is confirmed UNMODIFIED across the phase (see Standalone-Fence check below) — wiring these probes into the upload content-gate is explicitly Phase 48 scope per the plan, and this is the correct, intentional boundary, not a gap. |

**Score:** 5/5 success criteria verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/convert/pandocopts.go` | `PandocOpts` closed-allowlist strict-parse + discrete-argv builder | ✓ VERIFIED | 146 lines; `type PandocOpts`, `ParsePandocOpts`, `PandocOptsFromMap`, `ValidatePandocApplicability`, `pandocArgs`, `pandocWrapAllowlist` all present; `checkStrictObject`/`DisallowUnknownFields` both used |
| `internal/convert/pandocopts_test.go` | allowlist + strict-parse + wrap-enum + injection tests | ✓ VERIFIED | 230 lines; `TestParsePandocOpts`, `TestPandocOptsFromMap`, `TestPandocArgs`, `TestValidatePandocApplicability`, `TestPandocOptsInjection` — all pass |
| `internal/convert/convert.go` | `EnginePandoc` engine-class constant | ✓ VERIFIED | `EnginePandoc = "pandoc"` present in the const block (line 26) |
| `internal/convert/markupsniff.go` | `LooksLikeLaTeX`/`LooksLikeText` probes + `enforcePandocInputSize` guard + documented text-family limitation | ✓ VERIFIED | 174 lines; both funcs present, fail-closed, bounded, panic-safe; limitation documented in-code |
| `internal/convert/markupsniff_test.go` | probe pass/fail + size-guard + epub-container tests | ✓ VERIFIED | 258 lines; comprehensive marker/NUL/UTF-8/control-byte/no-panic/size-guard/epub tests, all pass |
| `internal/convert/sniff.go` | MIMEType registry entries for md/rst/org/latex/epub | ✓ VERIFIED | additive switch cases confirmed via diff; `text/markdown`, `text/x-rst`, `text/plain` (org, documented fallback), `application/x-latex`, `application/epub+zip` |
| `internal/convert/docsniff.go` | epub mimetype-first-entry container recognition | ✓ VERIFIED | `application/epub+zip` added to `odfMimetypes`, reuses the single-pass zip walk (no second archive/zip parse) |
| `internal/convert/pandoc.go` | `PandocConverter` Pairs/Engine/Convert; curated closed allowlist; hardened argv; output validation; empty-output-is-success | ✓ VERIFIED | 314 lines; all named functions present; no `if output=="" {error}` branch (confirmed by reading `pandocValidateOutput`, which explicitly returns `nil` on `info.Size()==0`) |
| `internal/convert/pandoc_test.go` | Pairs + unlisted-pair-reject tests; argv-hardening assertions; real-pandoc matrix; SC3 canary | ✓ VERIFIED | 526 lines, 13 test functions, all green including the real-binary matrix and 3 security canaries |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `pandoc.go` | `runCommand` (exec.go) | `runCommand(ctx, "pandoc", args...)` | ✓ WIRED | `pandoc.go:308`; exec.go byte-for-byte unchanged (confirmed via fence diff below) |
| `pandoc.go` | `pandocArgs`/`PandocOptsFromMap` (pandocopts.go) | opts→discrete argv | ✓ WIRED | `Convert` calls `PandocOptsFromMap(opts)` then `pandocConvertArgs` appends `pandocArgs(o)` |
| `pandoc.go` | `LooksLikeHTML`/`LooksLikeLaTeX`/`SniffContainer`/`LooksLikeText` | output validation | ✓ WIRED | `pandocValidateOutput` dispatches to each per target format |
| `pandocopts.go` | `checkStrictObject` (opts.go) | strict-decode helper | ✓ WIRED | `ParsePandocOpts` calls `checkStrictObject(raw)` before decode |

### Behavioral Spot-Checks (independently re-run by the verifier, outside the Go test harness)

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| `--sandbox` blocks `\input{secret}` disclosure | `pandoc -f latex-raw_tex --sandbox -t markdown attack_input.latex -o out.md` | `[WARNING] Could not load include file ...`; output = `Hello` only, no secret marker | ✓ PASS |
| `\write18` shell-escape is inert | `pandoc -f latex-raw_tex --sandbox -t html attack_write18.latex -o out.html` | pandoc errors (LaTeX macro undefined), no sentinel file created | ✓ PASS |
| Network-referencing HTML converts offline, no fetch | `pandoc -f html-raw_html-raw_tex --sandbox -t markdown attack_network.html -o out.md` | completes in ~1.1s; unresolved `.invalid` URL preserved literally in output | ✓ PASS |
| Real pandoc format-pair matrix | `go test ./internal/convert/ -run TestPandocConvertRealMatrix -v` | 7/7 subtests pass (md→html, md→docx, rst→html, latex→md, html→md, md→epub, docx→md) | ✓ PASS |

### Probe Execution

No `scripts/*/tests/probe-*.sh` probes declared or found for this phase (backend Go unit/integration tests serve this role; see Behavioral Spot-Checks above and full test run below).

### Automated Check Results

```
go build ./...          → clean, no errors
go vet ./...             → clean, no errors
gofmt -l <phase files>   → empty (all clean)
go test ./internal/convert/... -count=1 -v → PASS, ok (21.4s)
```

- Pandoc-related test count: 8 test funcs in `pandocopts_test.go` + 13 in `pandoc_test.go` + several in `markupsniff_test.go` — **0 skips** among any pandoc-related test.
- The only 3 `--- SKIP` entries in the full package run are `TestValidatePDFAOutputIntent` and 2 LibreOffice tests (unrelated engines requiring `verapdf`/`soffice`, not on this machine) — confirmed by grepping skip reasons, not pandoc-related.
- `--sandbox` present in `pandoc.go` (4 occurrences, including the real argv-building line); `--no-shell-escape` absent everywhere in `internal/convert/*.go` (correct — pandoc's CLI rejects this flag).
- `+raw_html` / `+raw_tex` (the raw-passthrough-ENABLING form) occur **0 times** as real code usage in `pandoc.go`.

### Standalone-Fence Check (critical)

`git diff --name-only fb9174f..HEAD` restricted to the guarded paths:

```
internal/api/*        → untouched
internal/queue/*       → untouched
internal/worker/*       → untouched
internal/convert/exec.go        → untouched
internal/convert/converters.go  → untouched
go.mod / go.sum          → untouched
```

Confirmed empty diff for all six guarded paths. The only `internal/convert/*.go` files touched across the phase (fb9174f..HEAD) are exactly: `convert.go`, `docsniff.go`, `markupsniff.go`, `markupsniff_test.go`, `pandoc.go`, `pandoc_test.go`, `pandocopts.go`, `pandocopts_test.go`, `sniff.go` — matches the plans' declared `files_modified` scope. `PandocConverter{}` does not appear anywhere in `converters.go`, `internal/api/`, `internal/worker/`, or `internal/queue/` (grep confirmed 0 matches) — the converter is genuinely unregistered, exactly as the phase requires.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|---|---|---|---|---|
| PDOC-01 | 47-03 | Closed `(from→to)` markup allowlist; unlisted pair → 422/rejection before any work | ✓ SATISFIED | `pandocPairTable` + `pandocPairSupported`; REQUIREMENTS.md marked `[x]` |
| PDOC-02 | 47-03 | Standalone output for structural targets, validated by content-sniffer | ✓ SATISFIED | `pandocStructuralTargets` + `pandocValidateOutput`; REQUIREMENTS.md marked `[x]` |
| PDCO-01 | 47-01 | Closed typed opts allowlist, strict-parse, injection test | ✓ SATISFIED | `PandocOpts`/`ParsePandocOpts`/`TestPandocOptsInjection`; REQUIREMENTS.md marked `[x]` |
| PDCE-01 | 47-02 | Fail-closed content validation across markup family (text-based, not only binary) | ✓ SATISFIED | `LooksLikeLaTeX`/`LooksLikeText`/`SniffContainer` (epub); REQUIREMENTS.md marked `[x]` |
| PDCE-02 | 47-03 | Sandboxed pandoc invocation; no shell-escape/`\input`/network fetch | ✓ SATISFIED | `--sandbox` + per-source reader table + `runCommand`; SC3 canaries pass (Go test + independently re-verified); REQUIREMENTS.md marked `[x]` (note: REQUIREMENTS.md's own prose still says "`--no-shell-escape`" — see Anti-Patterns/Notes below, a documentation-wording staleness, not a functional gap) |
| PDCE-03 | 47-02 | Oversize/pathological input rejected fail-closed before conversion | ✓ SATISFIED | `enforcePandocInputSize`/`ErrPandocInputTooLarge`, called before `runCommand`; REQUIREMENTS.md marked `[x]` |

No orphaned requirements: `.planning/REQUIREMENTS.md`'s traceability table maps exactly these 6 IDs to "Phase 47 | Complete"; PDCE-04 correctly remains mapped to Phase 49 (not this phase's scope).

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `.planning/REQUIREMENTS.md` / `.planning/ROADMAP.md` | PDCE-02 prose (REQUIREMENTS.md:22), Phase 47 SC3 text (ROADMAP.md:176,182) | Stale wording still says "`--no-shell-escape`" though the corrected, working mechanism is `--sandbox` (documented explicitly in 47-CONTEXT.md's "Hardening mechanism (CORRECTED)" decision and consistently implemented in code/tests) | ℹ️ Info | No functional impact — the checkbox/traceability status is correctly `[x]`/`Complete`, and the code is unambiguously correct and tested against the real binary. This is a documentation-text staleness for future readers of REQUIREMENTS.md/ROADMAP.md prose, not a phase gap. No debt markers (TBD/FIXME/XXX/TODO/HACK/PLACEHOLDER) found in any phase-47 source or test file. |

No blocker or warning-level anti-patterns found. `gofmt -l` clean across all phase-47 files.

### Human Verification Required

None. This phase is pure backend Go logic (converter core + content-validation probes), fully exercised against the real pandoc 3.10.1 binary by automated tests re-run and independently spot-checked by the verifier outside the Go test harness. No UI, visual, or external-service-dependent behavior requires human judgment.

### Gaps Summary

No gaps. All 5 ROADMAP success criteria are verified against actual, running code (not SUMMARY.md narrative): the closed `Pairs()` allowlist, `--standalone` structural output, the `--sandbox`+per-source-reader-table hardening (verified twice — once via the phase's own Go tests, once by the verifier independently re-running the raw `pandoc` commands), the closed `PandocOpts` injection defense, and the fail-closed markup-family content/size guards all exist, are substantive (not stubs), are wired together correctly, and produce real, observable non-effects against crafted attack input. The standalone-fence (no queue/worker/API/exec.go/go.mod touch, converter left unregistered) holds exactly as required. The only note is a stale documentation-wording item in REQUIREMENTS.md/ROADMAP.md prose (pre-dating the D-03 hardening-mechanism correction) — informational only, does not block phase completion.

---

*Verified: 2026-07-30T13:14:49Z*
*Verifier: Claude (gsd-verifier)*
