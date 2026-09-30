# Phase 68: Four Straightforward Engine Classes - Research

**Researched:** 2026-09-23
**Domain:** Landlock filesystem confinement for four Go-shells-out engine converters (pandoc, tesseract/poppler, ffmpeg/ffprobe, whisper.cpp), replicating Phase 67's `image` pattern
**Confidence:** HIGH (the pattern is proven and merged; the four classes' code is read directly, not inferred)

## Summary

Phase 67 (PR #28, `eb28f67`, merged) already built and proved the entire shared mechanism this phase reuses: `sandbox.ResolveHelper` (startup decision + refusal contract), `sandbox.RunExecModeIfRequested`/`RunExecMode` (the `__sandbox-exec` argv-prefix mode that applies Landlock and then `execve`s the real engine), `convert.engineSandbox`/`wrapEngine` (per-converter argv wrapping), and the "preflight on the real engine, judge by output" pattern (`LibvipsPreflight`). Phase 68 is mechanically: write one measured `ro`-path list, one `<Class>Spec` builder, one `<Class>Preflight` function, and one `cmd/<class>-worker/main.go` wiring block per class — four times, following `internal/convert/libvips.go` + `cmd/worker/main.go` line for line.

The measured path sets already exist in `.planning/research/SANDBOX-PATHS.md` for `pandoc`, `ocr`, and `av` and are proven sufficient+effective — **except that the OCR measurement is incomplete for the PDF path** (see Pitfall 1, HIGH-confidence finding: the document does not grant `pdfinfo`/`pdftotext`, both of which the code invokes unconditionally before `pdftoppm`/`tesseract` ever run). `audio`'s measured set is also correct for the two-stage `ffmpeg`→`whisper-cli` pipeline **but does not cover the pre-`Convert()` ffprobe duration guard, which today lives in a different Go package (`internal/worker`) than the converter's sandbox field** (Pitfall 2, HIGH-confidence finding, likely the phase's one genuine design decision despite the phase description's "no new architectural decisions" framing). None of the four classes' images are pinned to `linux/amd64` — Landlock is measurable and enforceable natively for all four on this host, per `SANDBOX-PATHS.md`'s own platform table.

**Primary recommendation:** Copy `internal/convert/libvips.go` + `internal/convert/sandboxwrap.go` + `cmd/worker/main.go`'s sandbox block four times (one per class), re-measure OCR's PDF path with `scripts/sandbox-paths-measure.sh` before locking its `ro` list, and resolve the audio ffprobe-guard placement as an explicit Wave 0 decision before writing `whisper.go`'s wrapping.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Landlock ruleset construction / `execve` | Worker process (helper mode, `internal/sandbox`) | — | Landlock restricts the calling THREAD and is inherited across `execve`; only the process that becomes the engine can apply it (already built, Phase 67) |
| Per-class read-only path list | `internal/convert/<class>.go` | `.planning/research/SANDBOX-PATHS.md` (source of truth) | Each converter owns its own measured set, mirroring `libvipsReadOnly` |
| Startup refusal / escape hatch | `cmd/<class>-worker/main.go` | Helm chart `extraEnv` / compose env forwarding | `sandbox.ResolveHelper` is generic; each worker cmd supplies its own env var name and wires the remedy to be reachable (CONF-02/03) |
| Preflight-on-real-engine | `cmd/<class>-worker/main.go` (calls a `<Class>Preflight` in `internal/convert`) | — | Must exercise the SAME code path production jobs take, including any secondary binary (pdfinfo, ffprobe) — not just the primary engine (CONF-04) |
| Duration/codec pre-Convert guards | `internal/worker` (audio only) or `internal/convert` (av, ocr) | — | AV/OCR guards are self-contained methods on the converter; AUDIO's ffprobe duration guard is a package-level function in `internal/worker`, called before `Convert()` — the one cross-package wrinkle in this phase |

## User Constraints

No CONTEXT.md exists for this phase (user chose to plan without discuss-phase). No locked decisions, discretion areas, or deferred ideas to copy verbatim. The phase's own ROADMAP.md description and REQUIREMENTS.md stand as the only upstream constraints:

- **Requirements this phase must satisfy:** CONF-01, CONF-02, CONF-04, CONF-05 (CONF-03 already closed in Phase 67; CONF-06/07 are out of scope, belonging to Phases 69/70).
- **Depends on:** Phase 67's shared `sandbox.ResolveHelper` / `engineSandbox.wrapEngine` — already merged (`eb28f67`), read directly in this research.
- **Explicit non-goal from the phase description:** "новых архитектурных решений нет" (no new architectural decisions) — **partially contradicted by evidence**; see Pitfall 2. Treat that framing as the planner's working assumption, not as license to skip the audio ffprobe-guard decision.

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CONF-01 | Each of the four classes runs its engine under a Landlock ruleset; the engine cannot read a file outside the job directory and its granted paths | Per-class measured `ro` sets exist in SANDBOX-PATHS.md (pandoc, av complete; ocr incomplete for PDF path — Pitfall 1); `libvips.go`/`sandboxwrap.go` give the exact code shape to replicate four times |
| CONF-02 | A worker that needs confinement and cannot get it refuses to start, naming the escape-hatch env var | `sandbox.ResolveHelper(disabledEnv)` already implements this generically (`internal/sandbox/helper.go:19-46`); each `cmd/<class>-worker/main.go` needs its own `<CLASS>_SANDBOX_DISABLED` const and `log.Fatalf` call, mirroring `cmd/worker/main.go:54-57` |
| CONF-04 | Each worker preflights on the REAL engine before consuming any job, judging by OUTPUT not exit code | `LibvipsPreflight` (`internal/convert/libvips.go:108-157`) is the template; ghostscript's "exit 0, empty output" trap (SANDBOX-PATHS.md's Pitfall 8) is the reason this matters, and it recurs for OCR/AV/audio's own possible silent-degradation modes — a preflight that only feeds the primary engine an image, never a PDF, would not catch a `pdfinfo`/`pdftotext` grant gap (Pitfall 1) |
| CONF-05 | The granted path set matches SANDBOX-PATHS.md and is held by a TEST, not a comment | `TestLibvipsReadOnlySetIsTheMeasuredOne` (`internal/convert/libvipssandbox_test.go:57-84`) is the exact test shape to replicate four times — but re-measure OCR first (Pitfall 1) so the test pins a CORRECT set, not merely the currently-documented one |

## Standard Stack

No new external dependencies. This phase adds zero packages — it wires the existing `internal/sandbox` package (built in Phase 67) into four existing converters (`internal/convert/pandoc.go`, `ocr.go`+`ocrpdf.go`+`ocrguards.go`, `av.go`+`avduration.go`, `whisper.go`+`audioduration.go`) and four existing worker binaries (`cmd/pandoc-worker`, `cmd/ocr-worker`, `cmd/av-worker`, `cmd/audio-worker`).

### Core (already present, reused)
| Package | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `golang.org/x/sys/unix` | (existing, `go.mod`) | Landlock syscalls, already used by `internal/sandbox/sandbox_linux.go` | No new import needed |
| `internal/sandbox` | in-repo, Phase 67 | `ResolveHelper`, `RunExecModeIfRequested`, `Paths`/`Limits`/`Spec`/`ParseSpec` | Built and proven this milestone; do not reimplement |
| `internal/convert` (sandboxwrap.go) | in-repo, Phase 67 | `engineSandbox`, `wrapEngine`, `enginePathFor`, `spec()` | Same file every one of the four classes calls into |

### Package Legitimacy Audit

**Not applicable.** This phase installs no new external packages (npm/pip/cargo/apt). All binaries (`pandoc`, `tesseract`, `pdftoppm`/`pdfinfo`/`pdftotext`, `ffmpeg`/`ffprobe`, `whisper-cli`) are already pinned and baked into their respective Dockerfiles from prior phases (`Dockerfile.pandoc-worker`, `Dockerfile.ocr-worker`, `Dockerfile.av-worker`, `Dockerfile.audio-worker`) — verified by reading each Dockerfile directly. No `Package Legitimacy Audit` table is produced; nothing to disposition.

## Architecture Patterns

### System Architecture Diagram

```
                 startup (cmd/<class>-worker/main.go)
                          |
                          v
       sandbox.RunExecModeIfRequested(os.Args) -- returns true and never
       returns further if THIS invocation IS the helper re-exec (see below)
                          |
                          v (normal worker startup path)
       sandboxHelper, err := sandbox.ResolveHelper("<CLASS>_SANDBOX_DISABLED")
          |-- err != nil (Landlock unavailable, escape hatch unset) --> log.Fatalf, refuse to start (CONF-02)
          |-- err == nil, helper == nil (escape hatch = "true")       --> unconfined, logged loudly
          `-- err == nil, helper == [self, "__sandbox-exec"]          --> confined
                          |
                          v
       convert.Default.Replace(Confined<Class>(sandboxHelper, ...))
                          |
                          v
       <Class>Preflight(ctx, sandboxHelper)  -- runs a REAL conversion through
       the REAL helper before any queue job is consumed; judges by OUTPUT
       CONTENT, not exit code (CONF-04)
          `-- err != nil --> log.Fatalf, refuse to start
                          |
                          v
              srv.Start(mux)  -- now consumes the queue

  ---- per-job, inside <Class>Converter.Convert (internal/convert) ----

  enginePath, _ := enginePathFor("<engine binary name>")
  spec := <class>Spec(jobDir, enginePath, ...)     // sandbox.Paths{RW: jobDir, RO: measured set + enginePath}.Spec()
  name, args, _ := c.Sandbox.wrapEngine("<engine>", spec, engineArgs)
  runCommand(ctx, name, args...)
                          |
                          v (name == helper binary, args == [__sandbox-exec, spec..., --, enginePath, engineArgs...])
       RunExecMode(argv) -- runtime.LockOSThread(); ApplyLimits(); sandbox.Apply(paths);
       verify thread unchanged; syscall.Exec(enginePath, ...)   [internal/sandbox/execmode.go]
                          |
                          v
       the REAL engine now runs, execve-inherited-restricted: it can read/write
       only the job directory and whatever `ro:`/`rw:` paths were granted
```

### Recommended Project Structure

No new files/directories — additions land inside existing files:

```
internal/convert/
├── pandoc.go          # add: pandocReadOnly []string, pandocSpec(), Sandbox *engineSandbox field on PandocConverter, wrap the one runCommand("pandoc",...) call
├── pandocsandbox_test.go   # new: mirrors libvipssandbox_test.go
├── ocr.go              # add: Sandbox field on OCRConverter; wrap the one runCommand("tesseract",...) call (image path)
├── ocrpdf.go            # wrap THREE call sites: pdfinfo (per-page size), pdftoppm, tesseract (PDF path)
├── ocrguards.go         # wrap TWO call sites: pdfinfo (page count / whole-doc size), pdftotext (text-layer extraction)
├── ocrsandbox.go        # new (or fold into ocr.go): ocrReadOnly []string, ocrSpec()
├── ocrsandbox_test.go   # new
├── av.go                # add: Sandbox field on AVConverter; wrap FIVE call sites (ffprobe x2, ffmpeg x3)
├── avduration.go        # the ffprobe-stream call here is also part of the five
├── avsandbox.go         # new: avReadOnly []string, avSpec()
├── avsandbox_test.go    # new
├── whisper.go           # add: Sandbox field on AudioConverter; wrap TWO call sites (ffmpeg normalize, whisper-cli)
├── audiosandbox.go      # new: audioReadOnly []string, audioSpec()
├── audiosandbox_test.go # new
└── converters.go        # no change needed -- Default.Replace() in each cmd/*-worker/main.go already overrides the zero-value registration

internal/worker/
└── worker.go            # DECISION NEEDED (Pitfall 2): does enforceAudioGuardBeforeConvert's
                          # ffprobe call get confined too? If yes, this file and runner.go need
                          # a new sandbox-carrying parameter threaded from cmd/audio-worker.

cmd/pandoc-worker/main.go   # add: RunExecModeIfRequested guard, ResolveHelper, Preflight, log lines -- mirrors cmd/worker/main.go
cmd/ocr-worker/main.go      # same
cmd/av-worker/main.go       # same
cmd/audio-worker/main.go    # same (+ resolves Pitfall 2)

docker-compose.yml           # add 4x "<CLASS>_SANDBOX_DISABLED: \"${<CLASS>_SANDBOX_DISABLED:-}\"" lines
deploy/chart/octoconv/templates/
├── deployment-pandoc-worker.yaml  # ALREADY has extraEnv (Phase 28) -- no template change needed
├── deployment-ocr-worker.yaml     # NEEDS a new extraEnv block (does not exist today)
├── deployment-av-worker.yaml      # NEEDS a new extraEnv block (does not exist today)
└── deployment-audio-worker.yaml   # NEEDS a new extraEnv block (does not exist today)
```

### Pattern 1: Per-class Sandbox field + `Replace`-time construction (the Phase 67 shape)

**What:** Each converter struct (`PandocConverter`, `OCRConverter`, `AVConverter`, `AudioConverter`) gains a `Sandbox *engineSandbox` field, held behind a pointer (nil-safe, keeps the struct comparable — `TestLibvipsConverterStaysComparable` pins exactly this property and should be mirrored for each new struct that had value-type comparability before).
**When to use:** Every converter this phase touches.
**Example** (the exact code to replicate, `internal/convert/libvips.go:15-23,101-106`):
```go
// Source: internal/convert/libvips.go, PR #28 (eb28f67), read this session
type LibvipsConverter struct {
	Sandbox *engineSandbox
}

func ConfinedLibvips(helper []string) LibvipsConverter {
	return LibvipsConverter{Sandbox: &engineSandbox{Helper: helper, Confine: len(helper) > 0}}
}
```
For pandoc this is a one-field, one-call-site change. For OCR and AV, the SAME `*engineSandbox` field is shared across every `runCommand` call site inside that converter's methods (`ocrTesseractArgs`+pdfinfo+pdftoppm+pdftotext call sites all read `c.Sandbox`) — one field, many wrap points.

### Pattern 2: `wrapEngine` per call site, not per converter

**What:** `wrapEngine(name string, spec []string, args []string) (string, []string, error)` is called ONCE PER SUBPROCESS INVOCATION, not once per converter. `spec` (the `rw:`/`ro:` token list) can be the SAME static list built once per `Convert()` call (or even package-level, if the job directory can be threaded through it — libvips rebuilds `spec` per-call via `libvipsSpec(inPath, outPath, enginePath)` because the job directory changes per job).
**When to use:** OCR has 6 call sites across 3 files (`ocr.go`, `ocrpdf.go`, `ocrguards.go`); AV has 5 across 2 files (`av.go`, `avduration.go`); each needs its own `wrapEngine` call, but they can all share one `<class>Spec(jobDir, engineBinaryPaths...)` builder — grant EVERY binary the class ever invokes (tesseract AND pdftoppm AND pdfinfo AND pdftotext for OCR; ffmpeg AND ffprobe for AV) in one static `ro` list, rather than trying to grant only the specific binary about to run. This mirrors how SANDBOX-PATHS.md's `av` section already grants both `/usr/local/bin/ffmpeg` and `/usr/local/bin/ffprobe` unconditionally.
**Example** (`internal/convert/sandboxwrap.go:36-49`, read this session):
```go
func (s *engineSandbox) wrapEngine(name string, spec []string, args []string) (string, []string, error) {
	enginePath, err := exec.LookPath(name)
	if err != nil {
		return "", nil, fmt.Errorf("locate engine %s: %w", name, err)
	}
	if s == nil || len(s.Helper) == 0 {
		return enginePath, args, nil
	}
	wrapped := append(append(append(
		append([]string{}, s.Helper[1:]...),
		spec...),
		"--", enginePath), args...)
	return s.Helper[0], wrapped, nil
}
```
At each of OCR's/AV's call sites, replace `runCommand(ctx, "tesseract", args...)` with:
```go
var s []string
if c.Sandbox.confines() {
	s = ocrSpec(jobDir, enginePathFor("tesseract"), enginePathFor("pdftoppm"), enginePathFor("pdfinfo"), enginePathFor("pdftotext"))
}
name, args, err := c.Sandbox.wrapEngine("tesseract", s, args)
if err != nil { return fmt.Errorf("ocr: %w", err) }
if _, err := runCommand(ctx, name, args...); err != nil { ... }
```

### Pattern 3: Preflight must exercise every code path the ruleset needs to survive

**What:** `LibvipsPreflight` (`internal/convert/libvips.go:108-157`) generates a 2x2 PNG and converts it to WebP through the real helper, checking the OUTPUT (non-empty file), not the exit code — because ghostscript was measured to exit 0 with empty output when denied (SANDBOX-PATHS.md Pitfall 8).
**When to use:** For pandoc and AV, a single representative conversion (like libvips's) is sufficient — pandoc has one binary and one code path; AV's ffmpeg/ffprobe binaries are BOTH exercised even by a minimal transcode+probe. For **OCR specifically, a preflight that only feeds a small PNG through tesseract does NOT exercise pdfinfo/pdftoppm/pdftotext at all** — it would pass and still let every `pdf->txt/hocr/alto` job fail at `exec: permission denied` on `pdfinfo` in production, silently, the exact vips-executable-invisible-in-trace defect this project has already hit and documented twice. The OCR preflight MUST also convert a tiny generated PDF (or run `pdfinfo`+`pdftoppm` directly) under the same ruleset before the worker accepts jobs.
**Example to model** (`internal/convert/libvips.go:119-156`, read this session) — replicate the shape, but for OCR generate BOTH a PNG (image path) and a minimal single-page PDF (PDF path) fixture.

### Anti-Patterns to Avoid
- **Judging OCR's PDF-path readiness by the image-path preflight alone:** passes the letter of CONF-04 ("preflight on a real engine") while missing its purpose (proving the ACTUAL job mix can run). See Pitfall 1.
- **Building the OCR/AV `ro` list from `scripts/sandbox-paths-measure.sh trace` output alone, without also granting each binary's own path by hand:** the tool's own documentation states plainly that `execve`'d binaries never appear in an `openat` trace — this bit `vips` once already and will bite any of tesseract/pdftoppm/pdfinfo/pdftotext/ffmpeg/ffprobe/whisper-cli/pandoc identically if the manual addition step is skipped.
- **Threading `sandbox.Limits{CPUSeconds: N}` into these four converters:** REQUIREMENTS.md's Out of Scope table explicitly excludes a CPU budget for non-TeX engines ("У PNG и MP4 такого свойства нет"). Use `spec(rw, ro)` (zero `Limits{}`), exactly like `libvips.go:69-72`, not `SpecWith(sandbox.Limits{CPUSeconds: ...})`.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Landlock ruleset construction/ABI masking | A new `Apply`/`abiVersion` per class | `internal/sandbox.Apply`, already built and proven (ABI 8, measured) | One implementation, one place bugs get fixed |
| Argv wire format between converter and helper | A per-class spec string format | `sandbox.Paths.SpecWith` / `ParseSpec` (`internal/sandbox/sandbox.go:78-125`) | Already round-trip tested (`spec_test.go`); a second format is a second thing to keep in sync |
| Startup refusal / escape-hatch resolution | Per-class `if os.Getenv(...) == "true" {...} else if !landlockAvailable {...}` | `sandbox.ResolveHelper(disabledEnv)` | Generic already; only the env var NAME differs per class |
| Engine-binary lookup | Assuming a fixed path like `/usr/bin/vips` | `enginePathFor(name)` / `exec.LookPath` (`internal/convert/sandboxwrap.go:54-60`) | A missing binary must be reported as "missing binary," not as a confusing sandbox failure |

**Key insight:** Everything hand-rollable in this domain was already built and hardened in Phase 67. The only genuinely new work is (1) four measured `ro` path lists, three of which need re-verification/extension, and (2) deciding where the audio ffprobe guard's confinement boundary sits.

## Common Pitfalls

### Pitfall 1: OCR's measured path set is missing `pdfinfo` and `pdftotext` — the PDF path will fail immediately under Landlock
**What goes wrong:** `SANDBOX-PATHS.md`'s `ocr` section (lines 217-256, read this session) lists only two granted binaries: `/usr/local/bin/tesseract` and `/usr/bin/pdftoppm`. But the OCR converter's PDF-source code path invokes TWO MORE binaries unconditionally before either of those ever runs:
- `ocrPDFGuardStage` (`internal/convert/ocrpdf.go:166-167`, read this session) calls `totalPages, err := ocrPDFPageCount(ctx, pdfPath)` as its FIRST line, for every PDF-source job, regardless of `MaxPages` configuration — and `ocrPDFPageCount` (`internal/convert/ocrguards.go:67-72`) runs `runCommand(ctx, "pdfinfo", ocrPageCountArgs(pdfPath)...)`.
- The same guard stage, when `targetFormat == "txt"`, also calls `extractPDFTextLayer` (`internal/convert/ocrguards.go:269-272`), which runs `runCommand(ctx, "pdftotext", ocrTextLayerArgs(pdfPath)...)`.
- Per-page rendering additionally calls `pdfinfo` AGAIN for the per-page box size (`internal/convert/ocrpdf.go:107`, `ocrPDFPageSizeForPage`).

**Why it happens:** The `scripts/sandbox-paths-measure.sh` tool's `trace` phase drives the ENGINE BINARY directly (e.g. `tesseract img out --psm 3 ...`), not the Go worker's actual job-processing flow — so a measurement run against tesseract+pdftoppm alone never exercises the guard stage that only fires when `OCRConverter.Convert`/`convertPDF` is invoked with a PDF source through the real Go code. `SANDBOX-PATHS.md`'s own doc says the PDF path WAS exercised ("pdftoppm — OK... Форматы txt, hocr и alto проходят"), which means either the measurement bypassed `ocrPDFGuardStage` (e.g. invoked `pdftoppm`/`tesseract` directly rather than through the compiled worker), or `pdfinfo`'s binary happened to already be reachable through some grant not documented as intentional (unverified — no evidence found for this in the document).
**How to avoid:** Before writing OCR's `ro` list, re-run `scripts/sandbox-paths-measure.sh trace` and `verify` (both phases) against the REAL `pdfinfo`/`pdftotext`/`pdftoppm`/`tesseract` sequence a `pdf->txt`, `pdf->hocr`, and `pdf->alto` job actually takes — driving it through the compiled `ocr-worker` binary's `OCRConverter.Convert`, not through bare CLI invocations. Add `/usr/bin/pdfinfo` and `/usr/bin/pdftotext` (or wherever `poppler-utils` installs them — verify via `dpkg -L poppler-utils` in the built image) to the `ro` grant list, update `SANDBOX-PATHS.md`, and only then write the pinned-set test (CONF-05).
**Warning signs:** A `pdf->txt`/`pdf->hocr`/`pdf->alto` job that passes the OCR preflight (if the preflight only exercises tesseract on a bare image) but fails every real PDF job in production with `exec: permission denied` on `pdfinfo` — the exact silent-failure shape `vips`'s own binary grant already produced once (SANDBOX-PATHS.md Pitfall 1) and chromium's wrapper/interpreter grant produced a second time (Pitfall 2).

### Pitfall 2: Audio's ffprobe duration guard lives in a different Go package than the converter's `Sandbox` field
**What goes wrong:** `AudioConverter.Convert` (`internal/convert/whisper.go:313-381`) only shells out to `ffmpeg` (normalize) and `whisper-cli` (transcribe) — both self-contained and wrappable exactly like libvips. But BEFORE `Convert` is ever called, `internal/worker/runner.go:176` unconditionally calls `enforceAudioGuardBeforeConvert(ctx, job.Engine, inPath, maxDuration, convertFn)` (`internal/worker/worker.go:1080-1093`), which — when `engine == convert.EngineAudio` — calls `convert.EnforceMaxDuration(probeCtx, inPath, audioMaxDuration)` (`internal/convert/audioduration.go:104-110`), which runs `runCommand(ctx, "ffprobe", ffprobeDurationArgs(path)...)` (`internal/convert/audioduration.go:81`) with **no sandbox wrapping at all** — `internal/worker` has no visibility into `AudioConverter.Sandbox`, an unexported field on a struct that lives in a different package.
**Why it happens:** `enforceAudioGuardBeforeConvert` was factored out (Phase 30/31-era, T-30-08/IN-02) as a package-level function in `internal/worker` specifically so the guard-runs-before-Convert ordering is unit-testable without a live Handler — it was never designed with a per-engine sandbox handle in mind, because Phase 30/31 predates any Landlock work in this codebase.
**How to avoid:** This is a genuine (if narrow) design decision the plan must make explicitly, despite the phase description's framing:
  - **Option A — thread the sandbox through:** Add a sandbox-aware duration-probe function (e.g. `convert.EnforceMaxDurationSandboxed(ctx, path, max, sandbox *engineSandbox)` in `internal/convert`, exported so `internal/worker` can call it) and thread an `audioSandbox` value from `cmd/audio-worker/main.go` through `NewHandler`/`NewRunner`/`audioPreConvertGuard` (`internal/worker/runner.go:169-178`) down to the guard call. Touches `internal/worker/worker.go`, `runner.go`, and `NewHandler`'s signature.
  - **Option B — accept and document the gap:** Record explicitly that the audio class's ffprobe duration-probe runs unconfined (reading only the job's own already-materialized input file, so the practical exposure is narrower than an arbitrary-path read — but it is still an engine binary with full container read access, and ffprobe has its own history of format-parser CVEs). This does not satisfy CONF-01's letter ("движок не может прочитать файл вне каталога задачи и выданных ему путей" — true for THIS call only in the narrow sense that it's never asked to read outside the job dir, but it is never RESTRICTED from doing so either).
  Recommendation: Option A is a small, contained change (one new exported function, one new field threaded through three already-open files) and closes the gap completely; it should not be skipped merely because the phase description calls the four classes "straightforward."
**Warning signs:** A `TestSandboxIsOnUnlessSomebodyWroteItDown`-style test (mirroring `internal/sandbox/helper_test.go`) that only exercises `AudioConverter.Convert` directly would pass while the actual job-processing path (`Handle` → `process()` → `enforceAudioGuardBeforeConvert` → `EnforceMaxDuration`) still runs an unconfined `ffprobe`. Any CONF-05 path-set test for audio must therefore also assert something about this call site, or explicitly note the accepted gap.

### Pitfall 3: Helm chart `extraEnv` is not yet wired for three of the four target deployments
**What goes wrong:** CONF-02 requires the worker's refusal message to name a REACHABLE remedy. `deploy/chart/octoconv/templates/deployment-pandoc-worker.yaml` already has an `extraEnv` block (added in Phase 28, unrelated to sandboxing, confirmed by reading the file: lines 62-79). But `deployment-ocr-worker.yaml`, `deployment-av-worker.yaml`, and `deployment-audio-worker.yaml` have **no `extraEnv` block at all** (confirmed by `grep -n extraEnv` returning nothing for all three, read this session). Shipping `cmd/ocr-worker`'s refusal message naming `OCR_SANDBOX_DISABLED=true` as the remedy, without adding the chart plumbing to set it, reproduces EXACTLY the codex PR #28 finding this milestone already fixed once for `image`/`tectonic` ("the documented remedy was unreachable: neither compose nor the chart could set IMAGE_SANDBOX_DISABLED").
**Why it happens:** `extraEnv` blocks were added per-deployment-template on an as-needed basis in earlier phases (Phase 28 added it to `document-worker`, `tectonic-worker`, `probe-worker`, `pandoc-worker`, and the generic `api`; `av-worker`/`ocr-worker`/`audio-worker` never needed a load-proof override before, so they never got the block).
**How to avoid:** Add an `extraEnv` block to `deployment-av-worker.yaml`, `deployment-ocr-worker.yaml`, and `deployment-audio-worker.yaml`, copying `deployment-pandoc-worker.yaml:62-79` verbatim (adjusting the values-key name to `avWorker.extraEnv`/`ocrWorker.extraEnv`/`audioWorker.extraEnv`). `pandoc-worker`'s template needs NO change — only a values.yaml/docs mention.
**Warning signs:** `helm template` renders no way to set `<CLASS>_SANDBOX_DISABLED` for three of the four workers even though `values.yaml` schema would accept it if the block existed.

### Pitfall 4: `docker-compose.yml` must forward all four new env vars, not just declare them with a default
**What goes wrong:** Mirroring `IMAGE_SANDBOX_DISABLED: "${IMAGE_SANDBOX_DISABLED:-}"` (`docker-compose.yml:250`) and `TECTONIC_SANDBOX_DISABLED: "${TECTONIC_SANDBOX_DISABLED:-}"` (`docker-compose.yml:1592`) is required for each of the four new services (`pandoc-worker`, `ocr-worker`, `av-worker`, `audio-worker`) — a bare `PANDOC_SANDBOX_DISABLED: "true"` hardcoded in compose (rather than forwarded from the shell/`.env`) would make the escape hatch permanently on for local dev, defeating CONF-02's "unset means confined" default.
**Why it happens:** Copy-paste from an existing service block that predates the sandbox var is easy to get subtly wrong (literal value instead of `${VAR:-}` interpolation).
**How to avoid:** Use the exact `"${<VAR>:-}"` interpolation syntax, and do NOT set a default of `"true"` anywhere in checked-in compose/chart defaults — mirrors `.env.example`'s own comment: "IMAGE_SANDBOX_DISABLED is DELIBERATELY NOT SET HERE... unconfined engine should cost a deliberate line."
**Warning signs:** `TestComposeReplicatesEveryEngineVarToEveryQueueClientService`-style mechanical tests exist for `ENGINE_TIMEOUT`/`MAX_RETRY` parity (`internal/queue/compose_in02_test.go:114`) but NOT for sandbox-disabled var forwarding — there is no existing automated check that would catch this class of mistake for the new vars, so manual review of the compose diff is the only gate.

## Code Examples

### The exact converter-side wrap shape to replicate (pandoc, simplest case)
```go
// Source: internal/convert/libvips.go Convert(), adapted -- read this session
func (c PandocConverter) Convert(ctx context.Context, inPath, outPath string, opts map[string]any) error {
	// ... existing pair/opts/size validation unchanged ...

	enginePath, err := enginePathFor("pandoc")
	if err != nil {
		return fmt.Errorf("pandoc: %w", err)
	}
	args := pandocConvertArgs(src, tgt, inPath, outPath, o)
	var s []string
	if c.Sandbox.confines() {
		s = pandocSpec(filepath.Dir(outPath), enginePath)
	}
	name, args, err := c.Sandbox.wrapEngine("pandoc", s, args)
	if err != nil {
		return fmt.Errorf("pandoc: %w", err)
	}
	if _, err := runCommand(ctx, name, args...); err != nil {
		return fmt.Errorf("pandoc: pandoc: %w", err)
	}
	return pandocValidateOutput(tgt, outPath)
}
```

### The exact worker-cmd startup shape to replicate (four times)
```go
// Source: cmd/worker/main.go:36-71, adapted -- read this session
func main() {
	if sandbox.RunExecModeIfRequested(os.Args) {
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	sandboxHelper, err := sandbox.ResolveHelper(pandocSandboxDisabledEnv) // const per class
	if err != nil {
		log.Fatalf("pandoc sandbox: %v", err)
	}
	convert.Default.Replace(convert.ConfinedPandoc(sandboxHelper, pandocMaxInputBytes)) // fold sandbox into existing builder
	if len(sandboxHelper) == 0 {
		log.Printf("⚠️  %s=true — the engine can read every file in this container", pandocSandboxDisabledEnv)
	} else {
		log.Printf("🔒 pandoc sandbox: landlock via %s", strings.Join(sandboxHelper, " "))
	}

	preflightCtx, cancelPreflight := context.WithTimeout(context.Background(), 30*time.Second)
	if err := convert.PandocPreflight(preflightCtx, sandboxHelper); err != nil {
		cancelPreflight()
		log.Fatalf("pandoc sandbox preflight: %v", err)
	}
	cancelPreflight()

	// ... rest of existing cmd/pandoc-worker/main.go unchanged ...
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|---------------|--------|
| Engines run with full container read access (all classes except `tectonic`, `image`) | Landlock-confined engines, job-dir read-write + measured read-only paths | Phase 67 (`image`), this phase (four more) | Closes the read-side of the class of bug PR #19/#21 fixed the write-side of |

**Deprecated/outdated:** None — this phase does not deprecate anything; it extends an existing, current pattern.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `pdfinfo`/`pdftotext` install to `/usr/bin/` in the `ocr-worker` image (same as `pdftoppm`, via the same `poppler-utils` apt package) | Pitfall 1 | Low — verify with `dpkg -L poppler-utils` in the built image before writing the grant path; if wrong, the path string in the `ro` list is simply different, not a design change |
| A2 | Debian's `ffmpeg` apt package (used by `audio-worker`'s runtime stage) also installs `/usr/bin/ffprobe` alongside `/usr/bin/ffmpeg` | Pitfall 2 | Low-Medium — if `ffprobe` is NOT present in the audio-worker image at all, `EnforceMaxDuration`'s `runCommand(ctx, "ffprobe", ...)` would already be failing today (unconfined), which is a pre-existing-bug finding independent of this phase, not something this phase introduces |
| A3 | No mechanical test today would catch a missing `extraEnv`/compose-forwarding wiring gap for the four new sandbox-disabled vars (Pitfalls 3/4) | Pitfalls 3, 4 | Medium — if wrong (i.e., some other test does catch this), the plan can skip writing a new one; if right, the plan should add one or rely on manual review, and should say which |

**If this table is empty:** N/A — three assumptions listed above, all low-to-medium risk and independently verifiable in Wave 0 with a single `docker exec`/`dpkg -L` check.

## Open Questions

1. **Does the plan confine the audio ffprobe duration-guard (Pitfall 2, Option A), or explicitly accept the gap (Option B)?**
   - What we know: the guard is currently unconfined and lives in a different package than the converter's `Sandbox` field; Option A is a contained ~3-file change.
   - What's unclear: whether the phase's "no new architectural decisions" framing was meant to preclude this, or simply didn't anticipate it (this research found no evidence the phase author was aware of the cross-package split when writing the phase description).
   - Recommendation: Option A. It is a small, mechanical extension of an established pattern (threading a value through `NewHandler`/`NewRunner`, both of which already thread `audioMaxDuration` the same way), not a new architecture.

2. **What is OCR's re-measured `ro` set once `pdfinfo`/`pdftotext` are included (Pitfall 1)?**
   - What we know: the two missing binaries and their call sites (with line numbers).
   - What's unclear: their exact installed path and whether they need any additional shared-library grants beyond what `tesseract`/`pdftoppm` already require (poppler tools may pull in `libpoppler`, `libfontconfig`, etc. not yet in the measured set).
   - Recommendation: Wave 0 task — re-run `scripts/sandbox-paths-measure.sh trace`+`verify` against the real `ocr-worker` image, driving all THREE OCR PDF targets (`txt` with a text layer, `txt` without one / scanned, `hocr`, `alto`), before writing any pinned-set test.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Landlock (kernel) | All four classes' Apply/preflight | ✓ (native arm64 OrbStack host, per SANDBOX-PATHS.md's platform table) | ABI 8, measured 2026-09-19/20 | — |
| `scripts/sandbox-paths-measure.sh` | Re-measuring OCR's PDF path (Pitfall 1) | ✓ (in-repo) | — | — |
| Docker / docker-compose | Building/running the four worker images for measurement and live gates | ✓ (used throughout this milestone) | — | — |

**Missing dependencies with no fallback:** None.

**Missing dependencies with fallback:** None — all four classes' images build natively (no `platform: linux/amd64` pin), unlike `document-worker`/Phase 70.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` |
| Config file | none — `go test ./...` |
| Quick run command | `go test ./internal/convert/... ./internal/sandbox/... ./internal/worker/...` |
| Full suite command | `go test ./...` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CONF-01 | Engine denied a file outside job dir + grants under real ruleset | integration (built image) | live gate: run each worker's built binary in helper mode against an outside-job-dir fixture (mirrors the "Действенность" proofs in SANDBOX-PATHS.md) | ❌ Wave 0 — write one live-gate script per class, or extend `scripts/sandbox-paths-measure.sh verify` |
| CONF-02 | Worker refuses to start without Landlock, names the var | unit | `go test ./internal/sandbox/... -run TestSandboxIsOnUnlessSomebodyWroteItDown` (generic, already exists) + a per-class const-name assertion if desired | ✅ (generic contract test exists; per-class const name is trivially checked by reading the file, no new test strictly required) |
| CONF-04 | Preflight judges by output, not exit code | unit | `go test ./internal/convert/... -run TestPandocPreflight\|TestOCRPreflight\|TestAVPreflight\|TestAudioPreflight` | ❌ Wave 0 — four new preflight functions + tests, mirroring `LibvipsPreflight` |
| CONF-05 | Granted path set matches SANDBOX-PATHS.md, held by test | unit | `go test ./internal/convert/... -run TestPandocReadOnlySetIsTheMeasuredOne\|TestOCRReadOnlySetIsTheMeasuredOne\|TestAVReadOnlySetIsTheMeasuredOne\|TestAudioReadOnlySetIsTheMeasuredOne` | ❌ Wave 0 — four new pinned-set tests, mirroring `TestLibvipsReadOnlySetIsTheMeasuredOne` (`internal/convert/libvipssandbox_test.go:57-84`) |

### Sampling Rate
- **Per task commit:** `go test ./internal/convert/... ./internal/sandbox/...`
- **Per wave merge:** `go test ./...` plus `go vet ./...`
- **Phase gate:** Full suite green, plus a live gate in the BUILT images for each of the four classes (mirrors Phase 67's "Proven in the built image" success criterion) before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] Re-measure OCR's full PDF path (`pdfinfo`/`pdftotext`/`pdftoppm`/`tesseract`) with `scripts/sandbox-paths-measure.sh` — Pitfall 1
- [ ] Decide and record the audio ffprobe-guard confinement approach (Option A vs B) — Pitfall 2
- [ ] `internal/convert/pandocsandbox_test.go`, `ocrsandbox_test.go`, `avsandbox_test.go`, `audiosandbox_test.go` — new, mirror `libvipssandbox_test.go`
- [ ] Four new `<Class>Preflight` functions and their tests
- [ ] `deploy/chart/octoconv/templates/deployment-{ocr,av,audio}-worker.yaml` — add `extraEnv` blocks (Pitfall 3)

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-------------------|
| V1 Architecture | yes | Landlock ruleset applied in the process that then `execve`s the engine (already established, `internal/sandbox`); this phase only extends the granted-path lists |
| V5 Input Validation | yes (pre-existing, unchanged) | Each converter's existing guard stages (size caps, page caps, duration caps) — this phase does not touch validation logic, only subprocess confinement |
| V6 Cryptography | no | Not applicable to this phase |
| V12 File and Resources | yes | This IS the phase's subject: engines confined to job dir + measured read-only paths, closing the untrusted-file-read class documented in `.planning/research/SANDBOX-PATHS.md`'s own "why it exists" framing |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|----------------------|
| Untrusted client file processed by a subprocess with full container read access | Information Disclosure | Landlock `ro`/`rw` path restriction, inherited across `execve` (this phase) |
| A binary invisible to `strace`'s `openat` trace (resolved via `execve`, not `open`) silently omitted from the granted set | Information Disclosure / Denial of Service | Manual addition of every invoked binary's own path to the `ro` list, verified by a real conversion under the real ruleset (`verify` phase of the measurement tool), not by trace alone |
| A guard/probe subprocess invoked OUTSIDE the converter's own sandboxed code path (audio's ffprobe, Pitfall 2) | Information Disclosure | Explicit design decision to either confine it too, or document the residual risk |

## Sources

### Primary (HIGH confidence — read directly this session)
- `internal/sandbox/helper.go`, `sandbox.go`, `execmode.go`, `sandbox_linux.go`, `limits_unix.go`, `helper_test.go` — the entire shared mechanism, Phase 67
- `internal/convert/sandboxwrap.go`, `libvips.go`, `libvipssandbox_test.go` — the exact pattern to replicate
- `cmd/worker/main.go`, `cmd/tectonic-worker/main.go` (lines 100-230) — the two existing worker-cmd wiring precedents
- `internal/convert/pandoc.go`, `ocr.go`, `ocrpdf.go`, `ocrguards.go`, `av.go`, `avduration.go`, `whisper.go`, `audioduration.go` — every `runCommand` call site for all four classes, read in full
- `internal/worker/worker.go` (lines 1055-1093), `internal/worker/runner.go` (lines 130-178) — the audio ffprobe-guard cross-package finding
- `.planning/research/SANDBOX-PATHS.md` — the measured path sets and the method's own documented pitfalls
- `Dockerfile.pandoc-worker`, `Dockerfile.ocr-worker`, `Dockerfile.av-worker`, `Dockerfile.audio-worker` — binary install locations, confirming SANDBOX-PATHS.md's per-image path claims (e.g. audio's `ffmpeg` via apt lands at `/usr/bin/ffmpeg`, av's self-built `ffmpeg` lands at `/usr/local/bin/ffmpeg`)
- `docker-compose.yml` (worker/tectonic-worker/pandoc-worker/ocr-worker/av-worker/audio-worker service blocks), `deploy/chart/octoconv/templates/deployment-{worker,tectonic-worker,pandoc-worker,av-worker,ocr-worker,audio-worker}.yaml`, `.env.example` — extraEnv/compose-forwarding wiring state, confirming the Pitfall 3/4 gaps
- `git show eb28f67 --stat` and full diff read via file contents — Phase 67's own commit message and scope
- `.planning/REQUIREMENTS.md`, `.planning/ROADMAP.md`, `.planning/STATE.md` — phase scope, requirement IDs, milestone history

### Secondary (MEDIUM confidence)
- None used — no web search or Context7 lookups were needed; this phase is entirely in-repo pattern replication with all facts verifiable by reading the codebase directly.

### Tertiary (LOW confidence)
- None.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — zero new dependencies, all code read directly
- Architecture: HIGH — the pattern is a proven, merged precedent (Phase 67); the two real findings (Pitfalls 1, 2) are supported by direct line-level code reading, not inference
- Pitfalls: HIGH — every pitfall in this document is backed by a specific file:line citation read this session, not by analogy alone

**Research date:** 2026-09-23
**Valid until:** Should remain valid through Phase 68's execution (no external dependency drift risk); re-verify SANDBOX-PATHS.md's OCR section immediately if Phase 69/70 touch `poppler-utils` or `ghostscript` versions before Phase 68 lands.
