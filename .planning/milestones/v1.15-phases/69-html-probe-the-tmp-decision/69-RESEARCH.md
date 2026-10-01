# Phase 69: HTML, Probe & the /tmp Decision - Research

**Researched:** 2026-09-25
**Domain:** Landlock filesystem confinement for the `html` (chromium-headless-shell) and `probe` (gs/pdfinfo/unzip/file) engine classes, plus the `/tmp` decision CONF-06 asks this phase to close
**Confidence:** HIGH — every D-item this phase's CONTEXT.md flagged as "not yet measured" was measured live this session against freshly built `octoconv-chromium-worker:measure69` and `octoconv-probe-worker:measure69` images; the two hardest findings (D-05's TMPDIR result, and a real sandbox-fault-swallowing bug in `classifyEngineFailure`/the verifiers) are grounded in reading the actual code, not analogy from phase 68.

## Summary

This phase closes two independent things: the `/tmp` decision CONF-06 (already locked in CONTEXT.md as D-01…D-05, "starts with this decision, not the code"), and the phase-68 pattern applied to two more classes. The mechanism (`internal/sandbox`, `engineExec`, `sandboxpreflight.go`) is unchanged from phase 68 and needs no new code — only new per-class wiring, exactly like `ocrsandbox.go`/`avsandbox.go` were phase-68's wiring for a multi-call-site, multi-engine class.

**D-05 is now decided by measurement, not left pending.** Ghostscript's two invocations (`ps2writeArgs`/`pdfwriteArgs` in `sanitizepdf.go`) honour `TMPDIR` exactly: with `TMPDIR=/job/tmp` set, every `gs_*` scratch file (four per pass, both passes) is created and unlinked inside `/job/tmp`, and **zero** files touch bare `/tmp` — measured with `strace -e trace=openat` against the real `octoconv-probe-worker:measure69` image, both passes, on the project's own `threat_all_classes.pdf` fixture. Probe's grant set can drop `/tmp` entirely, **provided two Go-level changes ride along that are not optional extras — they are the precondition for the measurement to mean anything in production**: `sanitizePDF`'s own `os.MkdirTemp("", "octoconv-sanitize-pdf-*")` (sanitizepdf.go:256) and `verifyOOXML`'s `os.MkdirTemp("", "octoconv-verify-*")` (sanitizeverify.go:829) both currently create their scratch directories under the **system** temp dir (`os.TempDir()`, i.e. bare `/tmp` unless `TMPDIR` is already set), not inside the job directory. Under a ruleset with no `/tmp` grant, both calls fail closed immediately — every sanitize job breaks before a single engine runs. This is a **required code change**, discovered by reading the code this session, not merely a wiring task.

**D-06/D-07 are now decided by measurement, not left open.** Both candidate fields work: a per-run marker placed in `<title>` lands verbatim, uncompressed, as `/Title (marker)` in the PDF's `/Info` dictionary (object 1), and the same marker placed in `<a href>` lands verbatim, uncompressed, as `/URI (http://…/marker)` inside a `/Annot`/`/Link` object — **both** confirmed present for an inside file and **both** absent (object 1 carries no `/Title` key at all, output is the documented 652-byte blank PDF) for a denied outside file, under a real Landlock ruleset built from the measured html set. Either field satisfies D-06/D-07; `/Title` is the simpler one to generate (no extra `<a>` markup, no annotation object to hunt for) and is the recommendation.

**D-08 is confirmed with one small correction to CONTEXT.md's own wording.** The wrapper is genuinely `#!/bin/sh -e` followed by `/usr/lib/chromium/chromium-headless-shell "$@"` — 61 bytes, unchanged — but the script does **not** contain the word `exec`; CONTEXT.md's paraphrase ("`exec /usr/lib/chromium/chromium-headless-shell "$@"`") describes the effective behaviour, not the literal bytes. This has no consequence for the grant set (Landlock persists across `fork` exactly as it does across `execve`), but the plan should quote the wrapper's real bytes, not the paraphrase, if it repeats the claim.

**D-10 is now decided by measurement, not by the incomplete 2026-09-20 record.** Re-measuring the full probe exec surface (`pdfinfo`, `gs` ×2, `unzip` ×2, `file` ×2 — six call sites, four distinct binaries, all six confirmed by `grep` and read directly) against real fixtures (the sanitize corpus PDF, a macro-enabled `.docm`, a `.jpg`) surfaces two corrections to the current SANDBOX-PATHS.md `probe` section: it is missing `/usr/share/poppler` (opened by `pdfinfo`'s glyph/cmap tables on a real PDF; kept as a conservative grant — see Assumptions) and it never accounted for `file`'s magic database at all. That second half resolves in probe's favour: `/etc/magic` is opened in the unconfined trace, but a removal test (both denied) shows `file --mime-type` still returns the exact correct MIME string on stdout (`image/jpeg`, `text/xml`) with only a discarded stderr warning, and the compiled default (`/usr/lib/file/magic.mgc`, reached through the already-granted `/usr/lib`) is what actually answers the query — so `/etc/magic` is **not** needed and should not be added.

A second, independent, code-verified finding: **`classifyEngineFailure` (sanitizepdf.go) and every branch of `verifyOOXML`/`verifyImage` that wraps an engine failure currently discard `ErrEngineSandboxUnavailable` by using `%v` instead of `%w`, or by falling through to a generic "malformed input" wrap.** Once probe's exec sites route through `engineExec`, a node that cannot establish Landlock will have its retryable sandbox fault silently rewritten into `ErrSanitizeMalformedInput`/`ErrSanitizeUnverifiable` — both TERMINAL per `isProbeTerminal` — misclassifying a node fault as a bad client file. This is the same class of defect phase 68 fixed once already ("sandbox-fault sentinel checked before class-prefix text in all four engine-scoped terminal classifiers") and it recurs here in a sharper form: for probe the swallowing happens **before** the terminal classifier ever runs, inside the error-classification helper itself.

**Primary recommendation:** wire html and probe exactly like OCR (68-RESEARCH.md's "several call sites, one class" template — `x := c.exec(outPath)` built once, threaded as an explicit `engineExec` parameter through every free function that execs), fix `classifyEngineFailure`/the two verifiers to preserve `ErrEngineSandboxUnavailable` before wrapping, relocate `sanitizePDF`'s and `verifyOOXML`'s temp directories inside the job directory, set `TMPDIR` for gs's children to that same directory, and drop `/tmp` from probe's grant set while keeping it (plus `/dev/null`, plus the D-01/D-03/D-04 concurrency-1 + sweep machinery) for html.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Landlock ruleset construction / `execve` | Worker process (helper mode, `internal/sandbox`) | — | Unchanged from phase 67/68 — restricts the calling thread, inherited across fork+exec |
| html grant set / preflight | `internal/convert/chromium.go` + new `htmlsandbox.go` | `.planning/research/SANDBOX-PATHS.md` §html (source of truth, reconfirmed this session) | Single call site, mirrors `pandocsandbox.go`'s shape exactly |
| probe grant set / preflight | `internal/convert/sanitize*.go` + new `probesandbox.go` | `.planning/research/SANDBOX-PATHS.md` §probe (needs correcting, see D-10) | Multi-call-site, multi-format class — mirrors `ocrsandbox.go`'s shape, NOT libvips'/pandoc's single-call-site shape |
| `/tmp` policy per class | `cmd/chromium-worker/main.go` (keeps /tmp) / `cmd/probe-worker/main.go` + `sanitizepdf.go`/`sanitizeverify.go` (drops /tmp, needs Go-level tmp-dir relocation) | — | D-05's finding is per-class, not a shared default — html cannot honour TMPDIR (measured phase 68-era, reconfirmed unchanged this session), probe can (measured this session) |
| Startup refusal / concurrency guard (D-03) | `cmd/chromium-worker/main.go`, `cmd/probe-worker/main.go` | Helm chart `extraEnv` / compose forwarding | html needs BOTH the sandbox-disabled refusal AND a NEW concurrency>1-with-confinement refusal; probe needs only the former, since it drops /tmp and D-03 explicitly does not apply once /tmp leaves the grant set |
| Sandbox-fault preservation through error classification | `internal/convert/sanitizepdf.go` (`classifyEngineFailure`), `internal/convert/sanitizeverify.go` (`verifyOOXML`, `verifyImage`) | `internal/worker/descriptors.go` (`isProbeTerminal`), `internal/worker/worker.go` (`isHTMLTerminal`) | The classification bug found this session lives BEFORE the terminal classifier — fixing only `isProbeTerminal` would not be sufficient |

## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** One task per container. html and probe workers run at most one engine task at a time. `HTML_WORKER_CONCURRENCY` default goes 2 → 1 in `cmd/chromium-worker/main.go`, `docker-compose.yml` and `deploy/chart/octoconv/templates/configmap.yaml`. Probe is already 1. html throughput scales by replicas, not by in-process concurrency.
- **D-02:** Alternatives rejected and recorded: per-task private `/tmp` via mount namespace is measured impossible under Docker's default seccomp; Landlock cannot remap `/tmp`; `TMPDIR` does not move chromium's shared memory; shared `/tmp` at concurrency 2 leaves a known cross-task read path.
- **D-03:** A confined html or probe worker whose configured concurrency is > 1 refuses to start and says why, whenever the class's grant set contains the shared `/tmp`. A test holds it.
- **D-04:** Sweep `/tmp` before every task, before the engine runs — covers leftovers from a SIGKILLed previous task. Safe only because of D-01/D-03.
- **D-05:** Probe may drop `/tmp` entirely if research measures that ghostscript honours `TMPDIR=<job>/tmp`. **MEASURED THIS SESSION: it does.** Probe's grant set loses `/tmp`; D-03/D-04 need not apply to probe.
- **D-06:** Under a ruleset, chromium renders a blank 652-byte PDF when a read is denied instead of failing. "Output exists" proves nothing. The preflight puts a per-run marker into a PDF field that chromium writes uncompressed, either `<title>`→`/Title` or `<a href>`→URI annotation.
- **D-07:** Research must prove on the real image that the chosen field carries the marker verbatim in raw PDF bytes: present inside, absent outside. **MEASURED THIS SESSION: both fields work; fallback to poppler-utils was not needed.**
- **D-08:** Keep exec'ing through the wrapper `/usr/bin/chromium-headless-shell`. Grant set includes the wrapper, its interpreter `/usr/bin/dash`, and the real binary under `/usr/lib/chromium`. **RECONFIRMED THIS SESSION** (with the wrapper-text correction above).
- **D-09:** Probe confinement covers only the external engine execs (`pdfinfo`, `gs`, `unzip`, `file`) through `engineExec`. In-process parsing (pdfcpu, Go-side OOXML/zip/image handling) is NOT confined this phase — recorded in Deferred.
- **D-10:** Re-measure probe from the code's exec sites, not the document's list. `sanitizeverify.go` execs `file --mime-type` (found at lines 850 and 1327 this session, matching CONTEXT.md's citation), omitted from the old set. **DONE THIS SESSION** — see Measurements.
- **D-11:** Shared `engineExec` (one ruleset per task, applied once), `sandbox.ResolveHelper` fail-closed start (CONF-02), the escape hatches, a real-engine preflight before Postgres/queue, retryable classification of sandbox faults, `sandboxcoverage_test.go` extended, `conf05-crosscheck.sh html probe`, `scripts/sandbox-liveproof.sh html probe`.
- **D-12:** `internal/convert/exec.go`'s `runCommand` (Setpgid + group SIGKILL) stays byte-for-byte unchanged. **Confirmed unmodified — no plan should touch this file except through the already-established infra-file allow-list in `sandboxcoverage_test.go`.**

### Claude's Discretion

- Exact sweep implementation (who sweeps, and how a sweep failure is classified: retryable vs fatal). Recommendation below (Common Pitfalls / Sweep Design).
- The exact wording of the D-03 refusal message.
- Which of `/Title` vs URI annotation to use for D-06 — **both measured to work; recommend `/Title`** (see Summary and Measurements).

### Deferred Ideas (OUT OF SCOPE)

- Confining in-process parsing of untrusted input (pdfcpu, Go-side OOXML/zip/image parsing) — not an engine exec, CONF-01 is about engines; a separate design (multi-threaded Go worker Landlock, `AllThreadsSyscall`, TLS/S3/Postgres grants).
- Per-task private `/tmp` via user+mount namespaces — blocked by default seccomp (measured phase 68-era). Revisit only if the deployment ever ships a custom seccomp profile for other reasons.

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CONF-01 | Each of the seven native classes runs its engine under a Landlock ruleset; the engine cannot read a file outside the job directory and its granted paths | html's and probe's exact exec sites enumerated and re-measured this session (chromium.go:187; sanitizepdf.go:221,264,281; sanitizeverify.go:791,841,850,1327); corrected grant sets given below |
| CONF-02 | A worker that needs confinement and cannot get it refuses to start, naming the escape-hatch variable | `sandbox.ResolveHelper` reused unchanged; `HTML_SANDBOX_DISABLED`/`PROBE_SANDBOX_DISABLED` need adding to `cmd/chromium-worker/main.go`/`cmd/probe-worker/main.go` (neither wired today — confirmed by reading both files this session) |
| CONF-04 | Preflight on the real engine, judged by output content, not exit code | `sandboxpreflight.go`'s `requireOutput`/`requireDenied` reused; probe's denial mode is measured LOUD (non-zero exit + permission-denied text on all four engines, unlike chromium's silent blank-PDF) but the same content-based helper should still be used for consistency (see Measurements) |
| CONF-05 | Granted path set matches SANDBOX-PATHS.md, held by a test, not a comment | `sandboxcoverage_test.go`'s `sandboxPendingFiles` entries for `chromium.go`/`sanitizepdf.go`/`sanitizeverify.go` (each tagged "phase 69") must be removed and the files reclassified into `sandboxClassFileEngines`/`sandboxClassFileGroups`; `conf05-crosscheck.sh` needs new `html`/`probe` cases added to its `class_go_file`/`class_go_readonly_var`/`class_go_engines_var` switches (currently only pandoc/ocr/av/audio) |
| CONF-06 | The `/tmp` decision for html and probe is made and recorded with a reason | Already locked in CONTEXT.md D-01…D-05; this research supplies the missing measurement (D-05) that the decision was conditioned on |

## Standard Stack

No new external dependencies. This phase wires the existing `internal/sandbox` package into two existing converters (`internal/convert/chromium.go`, and `internal/convert/sanitizepdf.go`+`sanitizeverify.go`+`sanitizeconverter.go`) and two existing worker binaries (`cmd/chromium-worker`, `cmd/probe-worker`).

### Package Legitimacy Audit

**Not applicable.** No new packages (npm/pip/cargo/apt) in this phase. All binaries (`chromium-headless-shell`, `gs`, `pdfinfo`, `unzip`, `file`) are already baked into their Dockerfiles from prior phases and were used, unmodified, throughout this session's measurements.

**Version note (not a legitimacy concern, but worth recording):** the built `octoconv-chromium-worker:measure69` image ships `Chromium 153.0.8010.52` — `chromium.go`'s own comment cites `150.0.7871.100` as the version live-tested for Plan 04's `--blink-settings=scriptEnabled=false`/`--disable-javascript` findings. The base image has moved since. Nothing in this session's measurements depended on that finding (CSP-based JS-disable, not the launch flags), so no action is required, but the plan should not assume the comment's version string still matches the shipped image.

## Architecture Patterns

### System Architecture Diagram

```
                 startup (cmd/chromium-worker/main.go, cmd/probe-worker/main.go)
                          |
                          v
       sandbox.RunExecModeIfRequested(os.Args)  -- NEITHER worker calls this today;
       must be added, mirroring cmd/pandoc-worker/main.go's existing block
                          |
                          v
       sandboxHelper, err := sandbox.ResolveHelper("<CLASS>_SANDBOX_DISABLED")
          |-- err != nil --> log.Fatalf, refuse to start (CONF-02)
          `-- err == nil  --> confined or explicitly unconfined
                          |
                          v
       NEW (html only, D-03): if sandboxHelper confines AND
       envInt("HTML_WORKER_CONCURRENCY", 1) > 1 --> log.Fatalf, refuse to start
       (the grant set still contains shared /tmp -- concurrency 1 is the
       invariant that makes the shared-/tmp grant safe at all)
                          |
                          v
       convert.Default.Replace(ConfinedChromium(sandboxHelper))  /
       convert.Default.Replace(buildSanitizeConverter(...).WithSandbox(sandboxHelper))
                          |
                          v
       <Class>Preflight(ctx, converter)  -- real conversion through the real
       helper, judged by MARKER CONTENT (D-06/D-07 for html; requireOutput/
       requireDenied for probe too, even though probe's denial is already loud)
          `-- err != nil --> log.Fatalf, refuse to start
                          |
                          v
              srv.Start(mux)

  ---- per job ----

  html (ONE call site, mirrors pandoc):
    x := c.Sandbox.forJob(jobDir, htmlReadOnly, htmlEngines)
    x.run(ctx, "chromium-headless-shell", args...)

  probe (SIX call sites across sanitizepdf.go/sanitizeverify.go, mirrors OCR):
    x := c.exec(outPath)                       // built ONCE in SanitizeConverter.Convert
    os.Setenv("TMPDIR", <job-scoped tmp dir>)   // BEFORE the two gs calls, process-wide,
                                                 // safe only because PROBE_WORKER_CONCURRENCY=1
    enforcePDFPageCeiling(ctx, x, inPath, max)  // -> probePDFPageCount -> x.run(ctx,"pdfinfo",...)
    SanitizeFile(ctx, x, format, inPath, outPath)
      sanitizeFormats[format](ctx, x, inPath, outPath)   // sanitizePDF uses x for pdfinfo+gs+gs;
                                                          // sanitizeOOXML/sanitizeImage ignore x (no exec)
      VerifySanitized(ctx, x, format, outPath)
        verifiers[format](ctx, x, format, outPath)       // verifyPDF ignores x (pdfcpu, no exec);
                                                          // verifyOOXML/verifyImage use x for unzip/file
```

### Recommended Project Structure

```
internal/convert/
├── chromium.go          # add: Sandbox *engineSandbox field, wrap the ONE runCommand call
├── htmlsandbox.go        # new: htmlReadOnly []string, htmlEngines, WithSandbox, HTMLPreflight (D-06/D-07 marker)
├── htmlsandbox_test.go    # new: mirrors pandocsandbox_test.go
├── sanitizeconverter.go  # add: Sandbox *engineSandbox field on SanitizeConverter; build x := c.exec(outPath) once in Convert; thread x into enforcePDFPageCeiling/SanitizeFile
├── sanitize.go            # CHANGE: SanitizeFile(ctx, x engineExec, format, inPath, outPath); sanitizeFormats map value type gains an x param (unused in sanitizeOOXML/sanitizeImage)
├── sanitizepdf.go         # CHANGE: sanitizePDF/probePDFPageCount/enforcePDFPageCeiling take x engineExec; classifyEngineFailure gets a NEW branch preserving ErrEngineSandboxUnavailable; os.MkdirTemp("", ...) at line 256 -> os.MkdirTemp(jobDir, ...); gs calls set TMPDIR
├── sanitizeverify.go       # CHANGE: VerifySanitized/verifiers map/verifyPDF/verifyOOXML/verifyImage take x engineExec; verifyOOXML's os.MkdirTemp("", ...) at line 829 -> os.MkdirTemp(jobDir, ...); the four %v-wraps around unzip/file failures become sandbox-fault-preserving
├── probesandbox.go         # new: probeReadOnly []string, probeEngines, WithSandbox, ProbePreflight (multi-format, mirrors ocrsandbox.go's shape)
├── probesandbox_test.go    # new
└── sandboxcoverage_test.go # EDIT: remove chromium.go/sanitizepdf.go/sanitizeverify.go from sandboxPendingFiles; add them to sandboxClassFileEngines/sandboxClassFileGroups

cmd/chromium-worker/main.go   # add: RunExecModeIfRequested guard, ResolveHelper, D-03 concurrency guard, Preflight -- mirrors cmd/pandoc-worker/main.go
cmd/probe-worker/main.go      # same shape, no D-03 guard needed (drops /tmp)

docker-compose.yml             # add HTML_SANDBOX_DISABLED / PROBE_SANDBOX_DISABLED forwarding (neither exists today); HTML_WORKER_CONCURRENCY "2" -> "1" (5 occurrences, see Measurements/grep)
deploy/chart/octoconv/templates/
├── deployment-chromium-worker.yaml  # NEEDS a new extraEnv block (none exists today -- confirmed by reading the file)
└── deployment-probe-worker.yaml     # ALREADY has extraEnv (Phase 28) -- no template change needed, only the compose-side var above

internal/worker/
├── worker.go            # isHTMLTerminal: add the ErrEngineSandboxUnavailable check explicitly (see Pitfall 1)
├── descriptors.go        # isProbeTerminal: add the same check (necessary but NOT sufficient alone -- see Pitfall 1)
└── engine_sandbox_retryable_test.go  # add "html" and "probe" rows to the existing 4-row table

scripts/conf05-crosscheck.sh   # add "html"/"probe" to class_go_file/class_go_readonly_var/class_go_engines_var (today only pandoc/ocr/av/audio)
```

### Pattern 1: html is a single-call-site class — copy pandoc's shape, not OCR's

**What:** `ChromiumConverter` has exactly one `runCommand` call (`chromium.go:187`), like `PandocConverter`. `pandocsandbox.go` is the literal template: `pandocReadOnly []string`, `pandocEngines = []string{"pandoc"}`, a `WithSandbox` method, and a `PandocPreflight` built on `sandboxpreflight.go`'s `newPreflightWorkspace`/`requireOutput`/`requireDenied`.

**Deviation from the pandoc template that MUST be made explicit:** pandoc's static binary needs nothing beyond `pandocEngines` (empty `pandocReadOnly`) — `forJob` resolves `"pandoc"` via `exec.LookPath` and grants exactly that one path. html's wrapper shape breaks this: `htmlEngines = []string{"chromium-headless-shell"}` only auto-grants `/usr/bin/chromium-headless-shell` (the 61-byte wrapper). The interpreter (`/usr/bin/dash`) and the real ELF (`/usr/lib/chromium/chromium-headless-shell`) are **never looked up by name** by the Go code — `forJob`'s `engines []string` mechanism cannot discover them. They must be literal entries in `htmlReadOnly` (the real binary is already covered if `htmlReadOnly` includes `/usr/lib` wholesale, per the existing measured set; `/usr/bin/dash` is not covered by anything else and needs its own line).

**Example** (`internal/convert/pandocsandbox.go:29-33`, read this session — the shape to copy):
```go
func (c PandocConverter) WithSandbox(helper []string) PandocConverter {
	c.Sandbox = newEngineSandbox(helper)
	return c
}
```

### Pattern 2: probe is a multi-call-site, multi-engine, multi-format class — copy OCR's shape

**What:** `x := c.exec(outPath)` is built ONCE, at the top of a method with access to the job's `outPath`, then threaded as an explicit `x engineExec` parameter through every free function that execs — `ocrsandbox.go:56-58`'s `func (c OCRConverter) exec(outPath string) engineExec { return c.Sandbox.forJob(filepath.Dir(outPath), ocrReadOnly, ocrEngines) }` is the exact shape.

**The complication OCR does not have:** OCR's `x` is built inside `OCRConverter.Convert` and threaded through free functions in the SAME package that all take an `OCRConverter` receiver or a bare `x engineExec` argument directly. Probe's call chain instead runs through **two dispatch maps with fixed function-value types** — `sanitizeFormats map[string]func(context.Context, string, string) (SanitizeResult, error)` (sanitize.go:70) and `verifiers map[string]func(context.Context, string, string) ([]ThreatClass, error)` (sanitizeverify.go:128) — neither of which has a slot for `engineExec` today. Threading `x` through means **changing both map value types**, and therefore every function assigned into them: `sanitizePDF`, `sanitizeOOXML`, `sanitizeImage` for the first map; `verifyPDF`, `verifyOOXML`, `verifyImage` for the second. Two of the six (`sanitizeOOXML`, `verifyPDF`) never exec anything and will carry an unused `x` parameter — that is fine in Go and is the honest cost of a uniform dispatch signature (mirrors 68-RESEARCH.md Pattern 2's "grant every binary the class ever invokes, not just the one about to run").

**One ruleset per task, applied once, even though the task branches by format:** build `x` from a STATIC `probeEngines = []string{"gs", "pdfinfo", "unzip", "file"}` unconditionally, in `SanitizeConverter.Convert`, before dispatching on format — exactly as OCR's `ocrEngines` includes `pdftotext`/`pdfinfo` even for a job that only exercises the image route. A PDF job never touches `unzip`/`file`; an OOXML job never touches `gs`/`pdfinfo`. Granting all four unconditionally is cheap (four extra `ro:` entries the ruleset never uses) and avoids a second, format-conditional ruleset-building path that would need its own test.

**Example of the required signature change** (illustrative, not exact code — the plan should write this):
```go
// sanitize.go, CHANGED
var sanitizeFormats = map[string]func(context.Context, engineExec, string, string) (SanitizeResult, error){
	"pdf":  sanitizePDF,        // uses x
	"docm": sanitizeOOXML,      // x unused -- no exec in this file
	...
}

func SanitizeFile(ctx context.Context, x engineExec, format, inPath, outPath string) (SanitizeResult, error) {
	...
	res, err := fn(ctx, x, inPath, outPath)
	...
	verified, verr := VerifySanitized(ctx, x, norm, outPath)
	...
}
```

### Pattern 3: preserve `ErrEngineSandboxUnavailable` through classification, not just at the terminal classifier

**What:** `classifyEngineFailure` (sanitizepdf.go:348-356) checks `context.DeadlineExceeded`/`Canceled` first, then `exec.ErrNotFound`/`fs.ErrNotExist`, then falls through to `fmt.Errorf("%w: %s: %v", ErrSanitizeMalformedInput, stage, err)` — note `%v`, not `%w`, for the underlying `err`. Once `engineExec.run` wraps a sandbox setup failure as `ErrEngineSandboxUnavailable`, this function will not recognise it (it is neither a deadline nor an ErrNotFound) and will flatten it into `ErrSanitizeMalformedInput` via `%v` — **the sentinel is discarded before `isProbeTerminal` ever runs**. The same shape recurs three more times in `sanitizeverify.go`: `verifyOOXML`'s three `fmt.Errorf("%w: ...: %v", ErrSanitizeUnverifiable, ..., err)` wraps (unzip listing, unzip extract, file classify) and `verifyImage`'s one (`file` classify) all use `%v`.

**Why this matters more than the html case:** `isHTMLTerminal` delegates to `isTerminal` via `timeoutIsTerminal`, and `isTerminal`'s substring scan (`terminalChromiumSignatures = ["output is empty", "output missing %pdf- magic bytes"]`) happens not to match `ErrEngineSandboxUnavailable`'s wrapped text — so html's classifier is *accidentally* safe today (falls through to `return false`, i.e. transient, which is correct). Probe's bug is not accidental-safe: `classifyEngineFailure` actively rewrites the sentinel into the WRONG one before any classifier sees it.

**How to fix:** add one branch to `classifyEngineFailure`, checked before the generic `%v` fallback:
```go
if errors.Is(err, convert.ErrEngineSandboxUnavailable) {
	return err // preserve the sentinel unwrapped -- isProbeTerminal must see it
}
```
and change the three `verifyOOXML`/one `verifyImage` wraps from `%v` to check-and-preserve the same way, mirroring the four existing precedents in `worker.go` (`isPandocTerminal`/`isOCRTerminal`/`isAVTerminal`/`isAudioTerminal`, each with its own `if errors.Is(err, convert.ErrEngineSandboxUnavailable) { return false }` at the top). Then add the same explicit check to `isProbeTerminal` (descriptors.go:313) and `isHTMLTerminal` (worker.go:253) — for html this converts an accidental safety into a stated invariant, matching the project's own "hold the invariant by refusing to start, not with a comment" posture (D-03) applied to error classification instead of startup.

**Test to extend:** `internal/worker/engine_sandbox_retryable_test.go` currently has exactly 4 rows (pandoc/ocr/av/audio). Add "html" (`isHTMLTerminal`, wrapping `fmt.Errorf("chromium: %w", convert.ErrEngineSandboxUnavailable)`) and "probe" (`isProbeTerminal`, wrapping through `classifyEngineFailure`'s new branch) rows.

### Pattern 4: `/tmp` relocation is a Go-level change, not just a Landlock grant change

**What:** two `os.MkdirTemp("", "...")` calls (sanitizepdf.go:256, sanitizeverify.go:829) create scratch directories under the system temp dir. Landlock has nothing to do with fixing this — it is the difference between `os.MkdirTemp("", pattern)` (uses `os.TempDir()`, i.e. `$TMPDIR` or `/tmp`) and `os.MkdirTemp(dir, pattern)` (creates inside `dir`). Both must become `os.MkdirTemp(jobDir, pattern)` where `jobDir` is `filepath.Dir(outPath)` — the same directory every other confined class treats as the sole writable grant (`forJob`'s own doc comment: "unlike libvipsSpec, the input's own directory is never granted separately... The job directory is the ONLY writable path").

**Then, and only then**, ghostscript's own `TMPDIR` env var can point at that same directory (or a subdirectory of it) for the two `gs` invocations. Since `engineExec.run` has no env-override parameter today (unlike `runCommandLimitedEnv`, which does but is unused by `engineExec`), the simplest correct fix is `os.Setenv("TMPDIR", scratchDir)` in the **probe-worker's own process**, set once per job before the `gs` calls — safe specifically because `PROBE_WORKER_CONCURRENCY` is already 1 (D-01), so no other job's `gs` invocation can observe a different `TMPDIR` value concurrently. This is a narrower, more auditable change than adding an env-passing parameter to the shared `engineExec`/`wrapEngine` infrastructure, and does not touch `exec.go` (D-12).

### Anti-Patterns to Avoid

- **Fixing `isProbeTerminal` without fixing `classifyEngineFailure`/the two verifiers first:** the sentinel is destroyed upstream; a downstream check on an already-rewrapped `ErrSanitizeMalformedInput` cannot recover it.
- **Granting `/usr/share/misc` or `/etc/magic` "to be safe" for `file(1)`:** measured this session to be unnecessary — `/usr/lib` (already granted) covers the resolved `magic.mgc` symlink target, and `/etc/magic`'s absence produces a discarded stderr warning, not a wrong answer.
- **Threading `x engineExec` only into `sanitizePDF` and skipping `sanitizeOOXML`/`sanitizeImage`/`verifyPDF`:** the map value types are shared — a partial change does not compile, and `sandboxcoverage_test.go`'s AST scanner will flag any remaining bare `runCommand`/`exec.Command` call as unclassified once `chromium.go`/`sanitizepdf.go`/`sanitizeverify.go` leave `sandboxPendingFiles`.
- **Building probe's `engineExec` per-format instead of once, statically, for all four engines:** matches 68-RESEARCH.md's own documented anti-pattern (OCR/AV Pattern 2) — a format-conditional ruleset is a second thing to keep in sync with the format dispatch, for no measured benefit.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Landlock ruleset construction | A new per-class `Apply` | `internal/sandbox.Apply` (unchanged, D-12) | One implementation, proven in phases 67/68 |
| Per-job ruleset sharing across several call sites | A new spec-rebuilding helper for probe | `engineSandbox.forJob` (`sandboxwrap.go:127-141`, unchanged) | Already built for exactly OCR's shape; probe's shape is identical (several call sites, one static engine list) |
| Preflight scaffolding (paired inside/outside workspace, marker generation) | New directory/marker plumbing for html/probe | `sandboxpreflight.go`'s `newPreflightWorkspace`/`place`/`requireOutput`/`requireDenied`/`preflightMarker` | Used unmodified by pandoc/OCR/AV/audio; the D-06/D-07 marker IS `preflightMarker()`'s output placed in `<title>` |
| Startup refusal / escape-hatch resolution | Per-worker `if os.Getenv(...) == "true"` | `sandbox.ResolveHelper(disabledEnv)` | Generic; only the env var name differs |

**Key insight:** everything mechanically reusable was already built in phases 67/68. The genuinely new work this phase is (1) the two Go-level `os.MkdirTemp` relocations plus the `TMPDIR` env threading (D-05's precondition), (2) the sandbox-fault-preservation fix through `classifyEngineFailure`/the verifiers (found by reading the code, not anticipated by CONTEXT.md), and (3) the dispatch-map signature change probe's shape forces that OCR's shape did not.

## Runtime State Inventory

Not applicable — this is a confinement/wiring phase, not a rename/refactor/migration. No stored data, live service config, OS-registered state, or renamed secrets are involved.

## Common Pitfalls

### Pitfall 1: the sandbox-fault sentinel is destroyed before the terminal classifier ever sees it (probe)

See Pattern 3 above for the full mechanism and fix. **Warning sign this would produce in production:** a node that cannot establish Landlock (a genuinely retryable, environment-level fault) would have every sanitize job on it marked permanently FAILED with "the uploaded document could not be parsed for sanitization" or "sanitized output could not be independently verified" — a class of false-permanent-failure indistinguishable, from the client's side, from a real bad upload, on every job the node touches until it is drained.

### Pitfall 2: `os.MkdirTemp("", ...)` under a `/tmp`-free ruleset fails EVERY sanitize job, not just the ones that need scratch space

`sanitizePDF` calls `os.MkdirTemp("", "octoconv-sanitize-pdf-*")` unconditionally, before either `gs` invocation — this line runs for every PDF sanitize job, confined or not. If probe's grant set drops `/tmp` (per D-05) without first relocating this call inside the job directory, **every single PDF sanitize job fails at job-directory creation**, not merely the ones exercising a rare code path — this is a total, immediate outage for the PDF half of probe's traffic, and it would pass any test that does not run the real Landlock-confined code path (a bare-runCommand unit test with no ruleset applied would never see it).

### Pitfall 3: the wrapper/engines split for html does not fit `forJob`'s single-mechanism model

See Pattern 1. **Warning sign:** if `htmlReadOnly` omits `/usr/bin/dash` (assuming `htmlEngines = ["chromium-headless-shell"]` covers "everything the wrapper needs"), the preflight fails with `exec: permission denied` on the wrapper's own interpreter — the same class of defect the project has already hit twice (libvips' invisible-in-trace binary, chromium's own wrapper+interpreter, both documented in SANDBOX-PATHS.md's "Ловушки" §2).

### Pitfall 4: `conf05-crosscheck.sh` and `sandboxcoverage_test.go` both need extending, not just the converter code

`scripts/conf05-crosscheck.sh`'s `class_go_file`/`class_go_readonly_var`/`class_go_engines_var` functions currently `die "unknown class"` for anything other than pandoc/ocr/av/audio — running `conf05-crosscheck.sh html probe` (which CONTEXT.md D-11 explicitly names as part of "what phase 68 already built, replicated here") will fail immediately unless these three functions gain `html`/`probe` cases. Separately, `sandboxcoverage_test.go`'s `sandboxPendingFiles` map has `chromium.go`, `sanitizepdf.go`, `sanitizeverify.go` each tagged `"phase 69"` — these entries must be REMOVED (not left in place) and the same three files added to `sandboxClassFileEngines`/`sandboxClassFileGroups`, or the AST scanner will either (a) still treat every literal-named low-level exec in those files as allowed (if left in `sandboxPendingFiles`, defeating CONF-05's own enforcement) or (b) fail the build entirely (if simply removed from `sandboxPendingFiles` without the classification being added).

### Pitfall 5: chart escape hatch is reachable for probe already, but NOT for html

`deployment-probe-worker.yaml` already has an `extraEnv` block (Phase 28, confirmed by reading the file — `probeWorker.extraEnv` range at line 84). `deployment-chromium-worker.yaml` has **no `extraEnv` block at all** (confirmed by `grep -n extraEnv` returning nothing). Shipping `cmd/chromium-worker`'s refusal message naming `HTML_SANDBOX_DISABLED=true` as the remedy without adding the chart plumbing reproduces the exact codex PR #27/#28 finding this milestone already fixed once for `image`/`tectonic`. `docker-compose.yml` forwards NEITHER `HTML_SANDBOX_DISABLED` NOR `PROBE_SANDBOX_DISABLED` today (grep for both across the whole file returns zero matches) — both need `"${VAR:-}"` interpolation added to their respective service blocks, matching `IMAGE_SANDBOX_DISABLED`'s and `TECTONIC_SANDBOX_DISABLED`'s existing pattern.

### Pitfall 6: `HTML_WORKER_CONCURRENCY: "2"` appears FIVE times in docker-compose.yml, not once

`grep -n 'HTML_WORKER_CONCURRENCY: "2"'` finds it in every service block that unconditionally reads every engine's config var (`worker`, `document-worker`, `av-worker`, `ocr-worker`, `audio-worker`, `pandoc-worker`, `probe-worker`, `tectonic-worker`, `webhook-worker` — DEBT-05's documented cross-reading), plus the `chromium-worker` block itself where it is actually consumed. D-01 only requires the value that `cmd/chromium-worker/main.go`'s own `envInt("HTML_WORKER_CONCURRENCY", 2)` default and the `chromium-worker` service's own compose value change to 1; the other seven "unconditionally-read-but-unused-here" copies exist purely for `queue.TestComposeReplicatesEveryEngineVarToEveryQueueClientService`'s IN-02 parity check and must stay byte-identical across all of them (changing only one and leaving seven stale would itself fail that test, or worse, pass it if the test only checks presence, not value — verify against the test's actual assertion before assuming which).

### Pitfall 7 (Sweep Design, Claude's Discretion): who sweeps `/tmp`, and how a sweep failure classifies

D-04 requires html's worker to sweep `/tmp` before every task, to clear leftovers from a SIGKILLed previous task's `chromium` (shm files) or (were probe still sharing `/tmp`, which it will not) `gs` (`gs_*` files). Since D-01 pins concurrency to 1, the sweep can safely run in the worker's own request path (no concurrent task's files can be swept out from under it) rather than needing a separate reconciler-style out-of-band sweeper. Recommendation: a small `sweepTmp(dir string) error` helper called at the top of `ChromiumConverter.Convert` (or in the runner immediately before `Convert`, mirroring where the audio ffprobe duration guard runs today) that does `os.ReadDir("/tmp")` and removes every entry, and classifies its own failure as **retryable** (a permission race or a file another process is still holding is an environment blip, not a statement about the client's HTML) rather than terminal — this mirrors the project's own established polarity for `ErrEngineSandboxUnavailable` (a node-level fault, not a file-level one).

## Code Examples

### The verified D-06/D-07 marker shape (from Measurements below)

```go
// preflight fixture -- mirrors ocrPreflightPDF/pandocsandbox.go's markdown fixture shape
marker, err := preflightMarker() // "octoconv-preflight-<16 hex chars>"
html := []byte("<html><head><title>" + marker + "</title></head><body></body></html>")
```
Measured raw PDF bytes for the inside (granted) file, `octoconv-chromium-worker:measure69`, the real `chromium.go` argv:
```
1 0 obj
<</Title (octoconv-preflight-...)
/Creator (Chromium)
/Producer (Skia/PDF m153)
...
```
For the outside (denied) file, object 1 has no `/Title` key at all (`<</Creator (Chromium)/Producer (Skia/PDF m153).../ModDate (...)>>`), matching the documented 652-byte blank-PDF shape exactly.

### The D-05 TMPDIR fix (illustrative)

```go
// sanitizepdf.go, sanitizePDF -- CHANGED
func sanitizePDF(ctx context.Context, x engineExec, inPath, outPath string) (SanitizeResult, error) {
	jobDir := filepath.Dir(outPath)
	tmpDir, err := os.MkdirTemp(jobDir, ".sanitize-pdf-") // WAS: os.MkdirTemp("", "octoconv-sanitize-pdf-*")
	if err != nil {
		return SanitizeResult{}, fmt.Errorf("sanitize pdf: tmpdir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if x.sandbox.confines() {
		os.Setenv("TMPDIR", tmpDir) // safe: PROBE_WORKER_CONCURRENCY=1 (D-01)
	}
	...
	if _, err := x.run(ctx, "gs", ps2writeArgs(inPath, psPath)...); err != nil { ... }
```

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `/usr/share/poppler` should be kept in probe's grant set even though a single-fixture removal test (ASCII-only PDF metadata) showed `pdfinfo` still worked without it | Measurements / D-10 | Low if kept (an unused directory grant costs nothing security-relevant — it is bundled image content, not host-sensitive); if a future non-ASCII/CID-font-titled PDF DOES need it and it is removed instead, `pdfinfo` could silently mis-decode a title/author field while still exiting 0 (the same "content, not exit code" trap the project already caught once for OCR's `/usr/share/fonts`). Recommendation is to KEEP it, so this assumption is conservative by construction. |
| A2 | `os.Setenv("TMPDIR", ...)` in `cmd/probe-worker`'s own process is safe because `PROBE_WORKER_CONCURRENCY` stays 1 | Pattern 4 | Medium if wrong — if a future change raises probe's concurrency without re-deriving this, two concurrent jobs would race on the process-global `TMPDIR`, and one job's `gs` scratch files could land in the OTHER job's tmp directory (readable, not exfiltration outside the container, but a job-isolation violation). The D-03 guard pattern (refuse to start above concurrency 1 when the grant set depends on it) is currently scoped to html/`/tmp` only in CONTEXT.md; the plan should decide whether probe's TMPDIR-based design also needs an analogous guard, or whether it is adequately covered by probe's PROBE_WORKER_CONCURRENCY already defaulting to 1 with no compose/chart override anywhere (confirmed: `grep PROBE_WORKER_CONCURRENCY` across compose/chart finds only the one `"1"` default, no override). |
| A3 | `os.Environ()` inherited into the re-exec'd helper (RunExecMode) carries the `os.Setenv` change made in the ORIGINAL probe-worker process, without needing to thread it through `runCommandCore`'s `env []string` parameter | Pattern 4 | Low — verified by reading `execmode.go`'s `syscall.Exec(cmd[0], cmd, os.Environ())` this session: the re-exec'd process's own `os.Environ()` is read at that point, and since `os.Setenv` mutates the CURRENT process's environment (inherited by `exec.Command`'s child with nil `Env`, which is the helper re-exec), the chain holds. If this reasoning is wrong, the failure mode is loud (gs falls back to bare `/tmp`, which is denied, and every PDF sanitize job fails immediately) rather than silent. |

## Open Questions

1. **Does `TestComposeReplicatesEveryEngineVarToEveryQueueClientService` assert VALUE equality or mere PRESENCE for `HTML_WORKER_CONCURRENCY` across the 8-9 service blocks?**
   - What we know: the var appears in every `queue.NewClient()`-constructing service's block (Pitfall 6), and the test exists specifically to catch drift in these cross-referenced vars.
   - What's unclear: whether changing only the `chromium-worker` block's value to `"1"` (leaving the other seven at `"2"`) passes or fails this test.
   - Recommendation: read `internal/queue/compose_in02_test.go` (cited in 68-RESEARCH.md Pitfall 4) before touching docker-compose.yml, and change ALL eight/nine occurrences together if the test asserts value equality — matching the IN-02 pattern's own stated purpose (a value used only by ONE process but READ unconditionally by all, so all copies must agree).

2. **Does probe need an analogous D-03-style concurrency guard for its TMPDIR strategy, even though CONTEXT.md scoped D-03 to html/`/tmp` only?**
   - What we know: probe's concurrency is already pinned to 1 everywhere it's configured (no override anywhere in compose/chart), and D-05's own text says "D-03/D-04 need not apply to probe" if TMPDIR works.
   - What's unclear: whether that's because CONTEXT.md considered probe's concurrency-1 default sufficiently durable on its own, or because the TMPDIR-based design genuinely needs no guard by construction (a shared ruleset with no /tmp grant literally cannot leak between concurrent tasks the way a shared /tmp COULD, regardless of concurrency — since gs's TMPDIR is set per-process, a concurrency-2 probe worker running two `Convert()` calls on separate goroutines WOULD race on the single process-wide `os.Setenv`, which is a real hazard the D-03/D-04 reasoning doesn't directly address for a Go-level env race rather than a filesystem-level one).
   - Recommendation: the plan should make this explicit rather than silently relying on "D-05 already said D-03 doesn't apply" — the goroutine-level env race (A2 above) is a genuinely different hazard from the mount-level one D-03/D-04 were designed against, and deserves its own one-line decision (most likely: rely on PROBE_WORKER_CONCURRENCY=1 with no code guard, since asynq's own concurrency model means one worker process handles one task at a time when Concurrency=1 — verify this against `asynq.Config{Concurrency: 1}`'s actual guarantee before assuming it).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Docker / docker-compose (OrbStack, arm64) | Building/running measurement images | ✓ | Docker 29.4.0-era, arm64 native | — |
| Landlock (kernel) | Both classes' preflight/Apply | ✓ (native arm64, confirmed by direct measurement this session — same host phase 67/68 used) | ABI 8 (carried over) | — |
| `scripts/cmd/sandbox-probe` | Live ruleset measurement | ✓ (built for arm64 this session) | — | — |
| `octoconv-chromium-worker:measure69` | D-06/D-07/D-08 measurements | ✓ (built this session, `sha256:87373303fb5c...`) | Chromium 153.0.8010.52 | — |
| `octoconv-probe-worker:measure69` | D-05/D-10 measurements | ✓ (built this session, `sha256:4a9936e1f813...`) | gs 10.00.0, pdfinfo 22.12.0 (poppler), unzip 6.00 | — |

**Missing dependencies with no fallback:** none.

## Validation Architecture

*(Included per explicit orchestrator instruction; `.planning/config.json` has `workflow.nyquist_validation: false`, which would otherwise skip this section.)*

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` |
| Config file | none — `go test ./...` |
| Quick run command | `GOTOOLCHAIN=go1.26.4 go test ./internal/convert/... ./internal/sandbox/... ./internal/worker/...` |
| Full suite command | `GOTOOLCHAIN=go1.26.4 go test ./...` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CONF-01 | html/probe engines denied outside job dir under a real ruleset | integration (built image) | `scripts/sandbox-liveproof.sh html probe` (per D-11) | ❌ Wave 0 — extend the existing script's class list |
| CONF-02 | Both workers refuse to start without Landlock, naming the var | unit | generic `TestSandboxIsOnUnlessSomebodyWroteItDown`-style test, already exists for `sandbox.ResolveHelper` | ✅ generic; per-class const name check is new but trivial |
| CONF-04 | Preflight judges by content (marker present/absent), not exit code | unit | new `TestHTMLPreflight`/`TestProbePreflight`, mirroring `TestPandocPreflight`/OCR's `OCRPreflight` shape | ❌ Wave 0 |
| CONF-05 | Granted set matches SANDBOX-PATHS.md, held by a test | unit + script | `TestHTMLReadOnlySetIsTheMeasuredOne`/`TestProbeReadOnlySetIsTheMeasuredOne` (mirrors `libvipssandbox_test.go`) + `scripts/conf05-crosscheck.sh html probe` (needs extending, Pitfall 4) | ❌ Wave 0 |
| CONF-06 | `/tmp` decision recorded with a reason | doc + test | D-03's concurrency guard needs a unit test ("a test holds it" per CONTEXT.md D-03); no code exists yet | ❌ Wave 0 |
| Pattern 3 fix | Sandbox fault never misclassified as terminal | unit | extend `internal/worker/engine_sandbox_retryable_test.go` with html/probe rows | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./internal/convert/... ./internal/sandbox/... ./internal/worker/...`
- **Per wave merge:** `go test ./...` plus `go vet ./...`
- **Phase gate:** full suite green, plus `scripts/sandbox-liveproof.sh html probe` on rebuilt (non-`measure69`) images before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `internal/convert/htmlsandbox.go` + `htmlsandbox_test.go` — new
- [ ] `internal/convert/probesandbox.go` + `probesandbox_test.go` — new
- [ ] `sandboxcoverage_test.go` reclassification (Pitfall 4)
- [ ] `scripts/conf05-crosscheck.sh` html/probe cases (Pitfall 4)
- [ ] `internal/worker/engine_sandbox_retryable_test.go` html/probe rows (Pattern 3)
- [ ] D-03 concurrency-guard unit test (CONF-06)

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-------------------|
| V1 Architecture | yes | Landlock ruleset applied in the process that then `execve`s the engine — unchanged mechanism from phases 67/68 |
| V12 File and Resources | yes | This phase's subject: html/probe confined to job dir + measured read-only paths |
| V5 Input Validation | yes (pre-existing) | Unchanged — PROBE_MAX_PAGES, HTML input size caps, etc. are untouched by this phase |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|----------------------|
| Untrusted client file processed by a subprocess with full container read access | Information Disclosure | Landlock `ro`/`rw` restriction, inherited across fork+execve (this phase, both classes) |
| A wrapper/interpreter/real-binary triad not fully covered by the `engines []string` auto-resolve mechanism | Information Disclosure / Denial of Service | `htmlReadOnly` must literally list `/usr/bin/dash`; `/usr/lib` wholesale already covers the real binary (Pitfall 3) |
| A sandbox-fault sentinel silently rewritten into a client-blaming terminal error | Denial of Service (of legitimate jobs on a faulty node) | Preserve `ErrEngineSandboxUnavailable` through every wrap point before the terminal classifier (Pattern 3/Pitfall 1) — this is this phase's own most significant original finding |
| A temp-file creation call outside the granted writable path, invisible until the ruleset actually has no `/tmp` grant | Denial of Service (every job on the affected code path) | Relocate `os.MkdirTemp` calls inside the job directory BEFORE removing `/tmp` from the grant set, not after (Pattern 4/Pitfall 2) |

## Measurements

All commands run 2026-09-25, OrbStack Docker, native arm64 host (no emulation, Landlock genuinely available — same posture phase 67/68 measured). Images: `octoconv-chromium-worker:measure69` (`sha256:87373303fb5c479fdcfa0c69daedea80fb52b09d9b0d9cd3c7ae8d5288f2d305`), `octoconv-probe-worker:measure69` (`sha256:4a9936e1f813301d6dbf695bab09f828bd409f3321b7d5eb93baba40b1239132`), both built from commit `18b6cc50ea2f744116752b23fd5192e47ec4e468` via `docker build -f Dockerfile.chromium-worker .` / `docker build -f Dockerfile.probe-worker .`. No containers left running (all runs used `--rm`; verified with `docker ps -a | grep octoconv` → empty after the session).

### D-08: wrapper contents, verbatim

```
$ docker run --rm --entrypoint sh octoconv-chromium-worker:measure69 -c 'cat -A /usr/bin/chromium-headless-shell; wc -c /usr/bin/chromium-headless-shell'
#!/bin/sh -e$
$
/usr/lib/chromium/chromium-headless-shell "$@"$
61 /usr/bin/chromium-headless-shell
```
No `exec` keyword present (correction to CONTEXT.md's paraphrase — see Summary). Interpreter: `/usr/bin/dash` (133640 bytes, confirmed present). Real binary: `/usr/lib/chromium/chromium-headless-shell` (203557792 bytes). Image user: `nobody` (uid 65534), matching D-08's own claim.

### D-06/D-07: chromium Title/URI marker, unconfined baseline

Input `rendered.html`:
```html
<html><head><title>OCTOCONVMARK1790369266</title></head>
<body><p>hello</p><a href="http://example.invalid/OCTOCONVMARK1790369266">link</a></body></html>
```
Real argv from `chromium.go` (minus the CSP/CSS injection, which is orthogonal to marker placement):
```
$ docker run --rm -v $WORK/job:/job -v $WORK:/out --shm-size=256m \
    --entrypoint chromium-headless-shell octoconv-chromium-worker:measure69 \
    --headless --disable-gpu --no-sandbox --disable-dev-shm-usage \
    --no-pdf-header-footer --user-data-dir=/job/chrome-profile \
    --proxy-server=127.0.0.1:9 --proxy-bypass-list='<-loopback>' \
    --host-resolver-rules='MAP * ~NOTFOUND' \
    --print-to-pdf=/out/unconfined.pdf file:///job/rendered.html
7567 bytes written to file /out/unconfined.pdf
```
Raw bytes:
```
1 0 obj
<</Title (OCTOCONVMARK1790369266)
/Creator (Chromium)
/Producer (Skia/PDF m153)
...
5 0 obj
<</Type /Annot
/Subtype /Link
...
/A <</Type /Action
/S /URI
/URI (http://example.invalid/OCTOCONVMARK1790369266)>>
```
Both fields present, both uncompressed, both verbatim.

### D-06/D-07: under a real Landlock ruleset (html measured set + rw:/job:/tmp:/dev/null)

Inside file:
```
$ docker run --rm -v $WORK/job:/job -v $WORK/outsidefs:/outsidefs --shm-size=256m \
    -e PROBE_RW="/job:/tmp:/dev/null" \
    -e PROBE_RO="/usr/bin/chromium-headless-shell:/usr/bin/dash:/lib:/usr/lib:/usr/share:/usr/local/share:/etc/fonts:/etc/ld.so.cache:/etc/localtime:/etc/nsswitch.conf:/etc/host.conf:/etc/hosts:/etc/resolv.conf:/proc:/sys/devices:/dev/urandom" \
    --entrypoint /job/.sandbox-probe octoconv-chromium-worker:measure69 \
    chromium-headless-shell --headless --disable-gpu --no-sandbox --disable-dev-shm-usage \
    --no-pdf-header-footer --user-data-dir=/job/chrome-profile \
    --proxy-server=127.0.0.1:9 --proxy-bypass-list='<-loopback>' \
    --host-resolver-rules='MAP * ~NOTFOUND' \
    --print-to-pdf=/job/inside.pdf file:///job/rendered.html
7567 bytes written to file /job/inside.pdf   # identical size to unconfined
```
`grep -a -o "/Title[^)]*)" inside.pdf` → `/Title (OCTOCONVMARK1790369266)`; `grep -a -o "/URI ([^)]*)" inside.pdf` → `/URI (http://example.invalid/OCTOCONVMARK1790369266)`.

Outside file (same ruleset, `file:///outsidefs/rendered.html`, not granted):
```
652 bytes written to file /job/outside.pdf   # the documented blank-PDF size
```
Full `strings -a outside.pdf` on object 1: `<</Creator (Chromium)/Producer (Skia/PDF m153)/CreationDate (...)/ModDate (...)>>` — **no `/Title` key at all**. No `/Annot`/`/URI` object anywhere in the file (the page has no `/Annots` array). `grep -a -c "OCTOCONVMARK..." outside.pdf` → 0.

### D-05: does ghostscript honour TMPDIR? (real `sanitizepdf.go` argv, real image)

Pass 1 (`ps2writeArgs`), `TMPDIR=/job/tmp`, `strace -ff -e trace=open,openat,creat,unlink,unlinkat`, traced as `nobody` via `setpriv --reuid 65534 --regid 65534`:
```
openat(AT_FDCWD, "/job/tmp/gs_7Iqr9G", O_RDWR|O_CREAT|O_EXCL, 0600) = 4
openat(AT_FDCWD, "/job/tmp/gs_iaQL8Y", O_RDWR|O_CREAT|O_EXCL, 0600) = 5
openat(AT_FDCWD, "/job/tmp/gs_Q4li01", O_RDWR|O_CREAT|O_EXCL, 0600) = 6
openat(AT_FDCWD, "/job/tmp/gs_tiHXAo", O_RDWR|O_CREAT|O_EXCL, 0600) = 7
unlinkat(AT_FDCWD, "/job/tmp/gs_tiHXAo", 0) = 0
unlinkat(AT_FDCWD, "/job/tmp/gs_Q4li01", 0) = 0
unlinkat(AT_FDCWD, "/job/tmp/gs_iaQL8Y", 0) = 0
unlinkat(AT_FDCWD, "/job/tmp/gs_7Iqr9G", 0) = 0
```
Pass 2 (`pdfwriteArgs`), same `TMPDIR`: identical shape, four new `gs_*` names, all under `/job/tmp`. **Grep for any `"/tmp/` path (bare, not `/job/tmp`) across both trace files: zero matches, both passes.** `ls /job/tmp` after both runs: empty (transient, unlinked on completion — same shape as html's own `/tmp` note in SANDBOX-PATHS.md).

Baseline (no `TMPDIR` override, default `/tmp`):
```
$ docker run --rm -v $WORK/job:/job --entrypoint sh octoconv-probe-worker:measure69 -c '
    unset TMPDIR; gs -q -dNOPAUSE -dBATCH -dSAFER -sDEVICE=ps2write -sOutputFile=/job/out.ps /job/in.pdf & sleep 0.05
    ls /tmp | grep gs_; wait'
-rw------- 1 nobody nogroup   0 ... gs_AMRsyi
-rw------- 1 nobody nogroup  85 ... gs_FZRpC1
-rw------- 1 nobody nogroup  56 ... gs_jnTlfg
-rw------- 1 nobody nogroup 139 ... gs_l0wshx
```
Confirms the ORIGINAL SANDBOX-PATHS.md claim ("gs writes to /tmp") for the default case, and that `TMPDIR` fully redirects it.

### D-10: full probe exec-site enumeration and re-measurement

Six call sites confirmed by `grep -n "runCommand(" internal/convert/sanitizepdf.go internal/convert/sanitizeverify.go` this session:
```
sanitizepdf.go:221   runCommand(ctx, "pdfinfo", ocrPageCountArgs(inPath)...)
sanitizepdf.go:264   runCommand(ctx, "gs", ps2writeArgs(inPath, psPath)...)
sanitizepdf.go:281   runCommand(ctx, "gs", pdfwriteArgs(psPath, outPath)...)
sanitizeverify.go:791   runCommand(ctx, "unzip", "-Z1", path)
sanitizeverify.go:841   runCommand(ctx, "unzip", "-p", path, escapeZipGlob(part))
sanitizeverify.go:850   runCommand(ctx, "file", "--mime-type", "-b", extracted)
sanitizeverify.go:1327  runCommand(ctx, "file", "--mime-type", "-b", path)
```
(matches CONTEXT.md D-10's own citation of "lines ~850, ~1327" exactly). Four distinct binaries: `gs`, `pdfinfo`, `unzip`, `file`.

Trace (real fixtures: `threat_all_classes.pdf`, `macro_enabled.docm`, `exif_gps.jpg`, all from `internal/convert/testdata/sanitize/`), aggregated candidate set beyond the old (2026-09-20) `probe` section:
```
NEW vs old measured set:
  /etc/magic                      (file's optional local override)
  /usr/share/misc/magic.mgc       (symlink -> ../../lib/file/magic.mgc, i.e. /usr/lib/file/magic.mgc)
  /usr/share/poppler/{cMap,cidToUnicode,nameToUnicode,unicodeMap}/...   (pdfinfo, via libpoppler)
already covered by the existing /lib, /usr/lib, /usr/share/ghostscript, /usr/share/color,
/usr/share/fonts, /var/lib/ghostscript, /etc/{ld.so.cache,localtime,papersize} grants:
  all /lib/aarch64-linux-gnu/*.so*, all /usr/share/ghostscript/10.00.0/{Resource,iccprofiles}/*
```

Necessity-by-removal, `/etc/magic`:
```
$ (ruleset WITHOUT /etc/magic) file --mime-type -b /job/in.jpg
image/jpeg
file: cannot read magic file `/etc/magic' (Permission denied) (No such file or directory)
```
stdout (what `runCommand` returns and the code parses) is correct; the warning is stderr-only and discarded by the caller on a zero exit. **`/etc/magic` is not needed.**

Necessity-by-removal, `/usr/share/misc` (i.e. relying on the already-granted `/usr/lib` to cover the resolved symlink target):
```
$ (ruleset with /usr/lib granted, /usr/share/misc NOT granted) file --mime-type -b /job/extracted2.xml
text/xml
$ file --mime-type -b /job/in.jpg
image/jpeg
```
Both correct. **No separate grant on `/usr/share/misc` is needed** — `/usr/lib` (already in the old set) covers it.

Necessity-by-removal, `/usr/share/poppler` (single ASCII-metadata fixture only — see Assumption A1):
```
$ (ruleset WITHOUT /usr/share/poppler) gs ... -sDEVICE=ps2write ...   # rc=0, 166407 bytes, unchanged
$ (ruleset WITHOUT /usr/share/poppler) pdfinfo /job/in.pdf
Title:           Secret Internal Title
Author:          jane.doe@corp.example
... (full, unchanged output)
```
Content unaffected on this fixture; **recommendation is to keep the grant anyway** (see A1 — cheap, low-risk, was observed opened on a real corpus fixture through the real code path).

Sufficiency by content, full corrected ruleset (`ro: /lib,/usr/lib,/usr/share/ghostscript,/usr/share/color,/usr/share/fonts,/usr/share/poppler,/var/lib/ghostscript,/etc/ld.so.cache,/etc/localtime,/etc/papersize` + engines `gs,pdfinfo,unzip,file`; `rw: /job` only, no `/tmp`, `TMPDIR=/job/tmp`):
```
gs ps2write   rc=0  166407 bytes (== unconfined)
gs pdfwrite   rc=0  3359 bytes (== unconfined)
pdfinfo       rc=0  full correct metadata
unzip -Z1     rc=0  5 parts listed correctly
unzip -p      rc=0  249-byte document.xml, correct content
file (xml)    rc=0  text/xml
file (jpg)    rc=0  image/jpeg
pdftotext on the confined-run's final out.pdf: "Body text that must survive sanitization intact."
   (real sanitized content, NOT the 166046-byte ghostscript skeleton -- confirms sufficiency by CONTENT, not just exit code/size)
```

Effectiveness (same ruleset, files placed OUTSIDE `/job`, never granted):
```
gs ps2write (outside pdf)   -> "Error: /undefinedfilename ... Unrecoverable error, exit code 1"
                               166046-byte skeleton written (matches SANDBOX-PATHS.md's own documented
                               skeleton size exactly), grep for real content markers: 0 matches
pdfinfo (outside pdf)       -> "I/O Error: Couldn't open file ...: Permission denied."
unzip -Z1 (outside docm)    -> "error: cannot open zipfile ... Permission denied"
file (outside jpg)          -> "writable, regular file, no read permission" (stdout; the actual
                               mime-type answer is replaced by this text, not a silent wrong answer)
```
All four engines fail LOUDLY (non-zero exit and/or a permission-denied message in the primary output) when denied — unlike chromium's silent-success/blank-output failure mode, probe's engines do not need the "output exists but is blank" trap guarded against in the same way, though the preflight should still use `requireOutput`/`requireDenied` for consistency with every other class (CONF-04's letter, not just its narrowest reading).

## Sources

### Primary (HIGH confidence — read directly or measured this session)
- `internal/convert/chromium.go`, `sanitize.go`, `sanitizepdf.go`, `sanitizeverify.go`, `sanitizeconverter.go`, `exec.go`, `sandboxwrap.go`, `sandboxpreflight.go`, `libvips.go`, `pandocsandbox.go`, `ocrsandbox.go`, `ocr.go`, `ocrpdf.go`, `ocrguards.go`, `sandboxcoverage_test.go` — read in full this session
- `internal/sandbox/execmode.go` — read in full, confirms the `os.Environ()` inheritance chain A3 relies on
- `internal/worker/worker.go`, `internal/worker/descriptors.go` (terminal classifiers, all eight) — read in full this session
- `cmd/chromium-worker/main.go`, `cmd/probe-worker/main.go` — read in full this session, confirms neither has ANY sandbox wiring today
- `docker-compose.yml`, `deploy/chart/octoconv/templates/{configmap,deployment-chromium-worker,deployment-probe-worker,deployment-pandoc-worker}.yaml`, `deploy/chart/octoconv/values.yaml` — grepped and read this session
- `scripts/sandbox-paths-measure.sh`, `scripts/conf05-crosscheck.sh`, `scripts/cmd/sandbox-probe/main.go` — read in full this session
- `.planning/research/SANDBOX-PATHS.md` — full file read, both `html` and `probe` sections
- `.planning/phases/68-four-straightforward-engine-classes/68-RESEARCH.md` — full file read (the pattern this phase replicates)
- Live measurements: `octoconv-chromium-worker:measure69` and `octoconv-probe-worker:measure69`, built this session from commit `18b6cc5`, all commands and raw output reproduced verbatim in Measurements above

### Secondary (MEDIUM confidence)
- None — no web search or Context7 lookups were used; this phase is entirely in-repo pattern replication plus live measurement.

### Tertiary (LOW confidence)
- None.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — zero new dependencies, all code read directly
- /tmp decision measurements (D-05/D-06/D-07/D-08/D-10): HIGH — every claim backed by a command and raw output in this session, against real built images
- Sandbox-fault-preservation finding (Pattern 3): HIGH — traced through actual code (`classifyEngineFailure`, both verifiers, both terminal classifiers), not inferred by analogy
- `/usr/share/poppler` necessity: MEDIUM — removal tested on one fixture only, conservative recommendation given (keep it)

**Research date:** 2026-09-25
**Valid until:** should remain valid through this phase's execution; the chromium version drift noted in Standard Stack (150→153) means any FUTURE research reusing this document should re-verify the wrapper/wrapper-user/no-sandbox-flag claims against whatever image is current at that time, though nothing measured this session depended on the version delta.
