---
phase: quick-261012-gos
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - internal/convert/jobdirfs.go
  - internal/convert/jobdirfs_test.go
  - internal/convert/caddxf.go
  - internal/convert/cadrender.go
  - internal/convert/cadrender_test.go
  - internal/convert/ocrpdf.go
  - internal/convert/sanitizepdf.go
  - internal/convert/sanitizeconverter.go
  - internal/convert/sanitizeverify.go
  - internal/convert/caddxfpdf.go
  - internal/convert/cadmeasure.go
  - internal/convert/cad.go
  - internal/convert/libreoffice.go
  - internal/convert/pandoc.go
  - internal/convert/av.go
  - internal/worker/worker.go
  - internal/worker/descriptors.go
  - internal/worker/jobdirread_test.go
  - internal/api/handlers.go
  - internal/api/inspect_handlers.go
  - internal/api/multipart_spill_test.go
  - internal/convert/cadguard_test.go
  - .planning/phases/75-dxf-2d-pipeline/deferred-items.md
autonomous: true
requirements: [quick-261012-gos]

estimate:
  tokens: 400000
  raw_tokens: 400000
  tasks: 7
  confidence: low

must_haves:
  truths:
    - "A symlink that the cad-mesh or cad-render stage leaves at out.png.measurements.json is never written through. The outside file stays byte-identical, and the sidecar is a new regular 0600 file holding a valid measurements document. TestCADRenderWritesTheSidecarAsANewFile failed against the old write and passes after the change."
    - "Every worker write into a job directory after an untrusted stage has run goes through one remove-then-O_EXCL helper, writeNewFile. The sites are the 3D sidecar, the DXF sidecar, both OCR PDF output writes, the sanitized-PDF redaction write, the sanitize report and the OOXML verify parts. Each site has a planted-link test that failed before its change."
    - "Every worker read of the content of a job-dir path after an untrusted stage has run goes through one no-follow open plus fstat regular-file check, OpenJobFile or ReadJobFile in internal/convert/jobdirfs.go. That covers the upload of the primary output, both side-output reads, the DXF and 3D CAD reads, the OCR page fragment, the sanitized-PDF read and the engine output validators. A link or non-regular file at any of those paths is refused, and its target is never read. Each site has a planted-link test that failed before its change."
    - "Every other create/write/rename/mkdir site and every other read/stat site in internal/convert and internal/worker is listed in the SUMMARY with the cited reason it is safe. The SUMMARY also says that the Landlock ruleset handles MAKE_SYM and REFER and grants them nowhere, so a confined stage cannot plant the link. Both fixes are defense-in-depth and must not be described as closing a live exploit."
    - "The terminal/retryable classification of a missing or empty engine output is unchanged. The validators keep their existing error texts (stat output, output is empty, missing magic) and sentinels (ErrAVOutputMissingOrEmpty), and go test ./internal/worker/ passes."
    - "An upload whose file part exceeds multipartInMemoryBudget (32 MiB) leaves no multipart-* file behind. This holds through the real chi server and through direct ServeHTTP, on POST /v1/jobs and POST /v1/inspect. It also holds on the malformed-query path, where ParseMultipartForm returns an error with the form already set. `go test ./internal/api/... -count=1` adds 0 multipart-* files to $TMPDIR; the SUMMARY records the count and sizes before the fix."
    - "requireCADPython skips with the found version when python3 is older than 3.10 and runs otherwise. With the 3.9.6 shim first on PATH, TestCADMeshRefusesPartialMeshingBeforeWritingTheSTL skips and names 3.9.6. With the 3.14 shim first, it runs and passes."
    - "gofmt is clean, go vet ./... is clean, and go test ./... -count=1 passes once with python 3.14 first on PATH and once with 3.9.6 first. internal/convert/exec.go is byte-identical to main. No comment lines are added. Every commit is signed (%G? = G) and none has a Co-Authored-By trailer. Nothing is pushed."
    - "deferred-items.md marks all three items resolved, with commit references, and gains no new open item."
  artifacts:
    - path: internal/convert/jobdirfs.go
      provides: "writeNewFile(path, body, perm); OpenJobFile(path) (*os.File, fs.FileInfo, error); ReadJobFile(path) ([]byte, error); ErrJobFileIrregular"
    - path: internal/convert/jobdirfs_test.go
      provides: "TestWriteNewFileNeverWritesThroughAPlantedLink, TestOCRPDFWritesTheOutputAsANewFile, TestSanitizeWritesAsNewFilesAfterItsStages, TestOpenJobFileRefusesWhatIsNotARegularFile, TestCADDXFReadsRefuseAPlantedLink, TestStageOutputReadsRefuseAPlantedLink, TestOutputValidatorsRefuseAPlantedLink"
    - path: internal/convert/cadrender_test.go
      provides: "TestCADRenderWritesTheSidecarAsANewFile (planted by cad-mesh, planted by cad-render)"
    - path: internal/worker/jobdirread_test.go
      provides: "TestWorkerReadsOfTheJobDirRefuseAPlantedLink (primary upload, sanitize report, CAD sidecar)"
    - path: internal/api/multipart_spill_test.go
      provides: "TestMultipartSpillFilesAreRemovedWhenTheHandlerReturns"
    - path: internal/convert/cadguard_test.go
      provides: "requireCADPython with the 3.10 floor"
  key_links:
    - from: "internal/convert/cadrender.go CADRenderConverter.Convert"
      to: "internal/convert/jobdirfs.go writeNewFile"
      via: "sidecar write after renderStage, perm 0600, wrapped as cad: write measurements"
    - from: "internal/convert/caddxf.go CADDXFRenderConverter.Convert"
      to: "internal/convert/jobdirfs.go writeNewFile and OpenJobFile"
      via: "replaces the DXF-only write helper and the DXF path's two no-follow opens, so there is one implementation of each"
    - from: "internal/worker/worker.go uploadFrom"
      to: "internal/convert/jobdirfs.go OpenJobFile"
      via: "the primary output is opened with no-follow and fstat-checked as regular; the FileInfo size feeds storage Upload"
    - from: "internal/worker/descriptors.go probeSideOutputs and cadSideOutputs"
      to: "internal/convert/jobdirfs.go ReadJobFile"
      via: "the sanitize report and the measurements sidecar are read through the helper"
    - from: "internal/api/handlers.go and internal/api/inspect_handlers.go ParseMultipartForm"
      to: "multipart.Form.RemoveAll"
      via: "defer registered right after the parse call and before its error check, on the handler's own request copy"
---

<objective>
Close the three deferred items of phase 75 (.planning/phases/75-dxf-2d-pipeline/deferred-items.md) on branch gsd/phase-75-dxf-2d-pipeline, and fix each one as a class, not only the named instance.

1. Job-dir access after an untrusted stage, on both sides.
   - Writes: the 3D measurements sidecar (internal/convert/cadrender.go:126) and every sibling found by the sweep get the remove-then-O_EXCL write that the DXF path already uses, through one shared helper.
   - Reads: every worker read of a job-dir path after a stage goes through one no-follow open plus regular-file check. This generalizes the DXF path's safe read. Scope was extended by the coordinator: the read side is closed here, not deferred.
   - Each fixed site gets a planted-link test that is RED first.
2. Multipart spill files. Planning found that this is a production leak, not only a test-hygiene problem (see the facts below). The item's own condition ("production code unchanged unless a real server path also leaks") therefore moves the fix into the two handlers. A real-server regression test and a before/after $TMPDIR count prove it.
3. requireCADPython skips below python 3.10 and names the version it found.

Purpose: phase 75 ships with no open deferred items and no new ones. The SUMMARY's threat statements stay inside what the code and the sandbox actually allow.
Output: the shared write and read helpers and their call sites, planted-link tests per site, the handler fix with its regression test, the python floor, the updated deferred-items.md, and a SUMMARY with both site inventories and the gate evidence.
</objective>

<execution_context>
@~/.claude/gsd-core/workflows/execute-plan.md
@~/.claude/gsd-core/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@./CLAUDE.md
@.planning/phases/75-dxf-2d-pipeline/deferred-items.md
@internal/convert/caddxf.go
@internal/convert/cadrender.go

Facts measured during planning (do not re-derive; re-check only where a step says so):
- Branch gsd/phase-75-dxf-2d-pipeline, HEAD d34fbdf. Stay on it, commit there and never push. Run sequentially in /Users/apaderin/dev/octoconv with no worktrees. Every go command uses GOTOOLCHAIN=go1.26.4. Never use docker (the daemon is hung).
- The tree has unrelated dirt: .planning/config.json is modified, and .vscode/ and octoconv.code-workspace are untracked. Never stage any of them. Stage explicit paths only, and add .planning files with `git add -f`.
- Pin guard: run `bash /private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad/pin-guard.sh` before the first Edit/Write of each task and before every commit. If it exits non-zero, halt and report.
- Commits are SSH-signed through 1Password. Never pass --no-gpg-sign. If a commit fails to sign, halt and report. After each commit, `git log -1 --format=%G?` must print G. Per the user's global CLAUDE.md, never add a Co-Authored-By trailer, which overrides the harness default. Commit subjects use the `test|fix|docs(quick-261012-gos): ...` form.
- User rule: add no code comments of any kind, including doc comments on the new exported helpers. The user rule overrides the project convention. Design rationale goes in the SUMMARY.
- internal/convert/exec.go is byte-identical to main at d34fbdf (`git diff --quiet main -- internal/convert/exec.go` exits 0) and must stay that way.
- SCRATCH below means /private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad. Python shims already exist there: SCRATCH/py39/python3 links to /usr/bin/python3 (3.9.6) and SCRATCH/py314/python3 links to /opt/homebrew/bin/python3.14 (3.14.8). Both were checked with `python3 -c 'import sys; print(sys.version_info[:3])'`. Recreate them with `mkdir -p` and `ln -sf` if missing. /opt/homebrew/bin has no `python3` name, so a plain PATH reorder does not select 3.14. Use the shims.
- Host tools: pdfinfo, pdftotext, pdftoppm, tesseract and gs are in /opt/homebrew/bin. unzip and file are in /usr/bin.

Threat facts (item 1):
- internal/sandbox/sandbox_linux.go:17-20: the job directory's accessReadWrite is READ_FILE, READ_DIR, WRITE_FILE, TRUNCATE, MAKE_REG, MAKE_DIR, REMOVE_FILE and REMOVE_DIR. It contains no MAKE_SYM.
- :50-54: handledAccess adds MAKE_SYM, MAKE_SOCK, MAKE_FIFO, MAKE_BLOCK, MAKE_CHAR and REFER, and they are "granted nowhere".
- So under Landlock a confined stage can create neither a symlink, nor a FIFO, nor a cross-directory hard link in the job dir. An unconfined stage runs as the worker user and already has the worker's own access. Both fixes are therefore defense-in-depth: they stop depending on those ruleset bits. The SUMMARY must say exactly this.
- O_CREATE|O_EXCL fails with EEXIST when the path exists, including when it is a symlink, so a stage process that re-plants between the remove and the create makes the write fail closed.
- On the read side, O_NOFOLLOW makes the open of a final-component symlink fail with ELOOP. O_NONBLOCK makes the open of a FIFO return at once instead of blocking. An fstat on the opened descriptor then refuses anything that is not a regular file. Checking the opened descriptor rather than an Lstat first means there is no window between check and use.
- Read-helper residuals to state in the SUMMARY, not code around:
  - O_NOFOLLOW guards only the final component. Intermediate job-dir directories created by the worker (.ocr-pdf-*, .verify-*) could be swapped only by an unconfined stage.
  - A hard link is a regular file and is read. Planting a cross-directory hard link needs REFER under Landlock, and Linux protected_hardlinks restricts it as well.

Write-site inventory, built from `grep -nE 'os\.(WriteFile|Create|OpenFile|Rename|Mkdir|MkdirAll|MkdirTemp|Remove|Symlink|Link)\(|syscall\.Mkfifo' internal/convert/*.go internal/worker/*.go | grep -v _test.go`. The criterion is the orchestrator's: could an untrusted stage have written into that directory before this write?
| Site | Before the write | Planned disposition |
|---|---|---|
| cadrender.go:126 sidecar | guard, mesh or stats, and render stages, all with job-dir rw | FIX, Task 1 (named item) |
| caddxf.go:180 DXF sidecar via the DXF-only helper (:199) | dxf and rsvg stages | already remove+O_EXCL; switch to the shared helper, Task 1 |
| caddxf.go:358 cadDXFValidate clean.svg | dxf stage | already clear+O_EXCL; safe |
| ocrpdf.go:264 outPath (text-layer route) | pdfinfo and pdftotext via c.exec(outPath), job-dir rw | FIX, Task 2 |
| ocrpdf.go:319 outPath (page route) | pdftoppm and tesseract per page, same exec | FIX, Task 2 |
| sanitizepdf.go:304 outPath redaction | gs ps2write and pdfwrite (gs wrote outPath itself) | FIX, Task 2 |
| sanitizeconverter.go:165 SanitizeReportPath(outPath) | page ceiling (pdf), engine passes, verifySanitized (unzip, file) | FIX, Task 2 |
| sanitizeverify.go:874 `.verify-*/part-NNN` | unzip, and `file` from the previous iteration, in a dir inside the job dir | FIX, Task 2 |
| sanitizeooxml.go:542, sanitizeimage.go:488 outPath | nothing: both take `_ engineExec`, and sanitizePageCeilings (sanitizeconverter.go:200) has only "pdf" | safe; no stage has run in this attempt's dir |
| chromium.go:233 rendered.html | sweepSharedTmp (worker code) only; chromium runs after it at :239 | safe. The workDir is a fresh per-attempt MkdirTemp (internal/worker/worker.go:1395), and sharedTmpGuard (sandboxwrap.go:247) pins shared-/tmp classes to concurrency 1 |
| libreoffice.go:87, :92 MkdirAll | sweepSharedTmp only; soffice runs later | safe (same reasons as chromium) |
| libreoffice.go:174, ocr.go:261, tectonic.go:1382 Rename | engine stage | safe: rename(2) replaces the destination entry and never writes through a link there. A directory at the destination fails closed. Reads of the renamed entry are covered by the read tasks |
| cadsandbox.go:128 cadPrivateDir | guard stage (DXF) | safe: Mkdir, then an Lstat check that it is a 0700 directory, which refuses a planted link |
| cadsandbox.go:352, :440; sandboxpreflight.go:114, :117; tectonic.go:298; libvips.go:131; documentsandbox.go:383 | startup preflight on worker-owned fixtures in fresh MkdirTemp dirs; the outside dir is not stage-writable | safe |
| progressexec.go:95 Mkfifo, :177/:187 O_WRONLY poke | ffmpeg with job-dir rw | not a content write: no O_CREATE or O_TRUNC, zero bytes written, closed at once. Record the residual liveness note (an open on a substituted FIFO could block when unconfined; Landlock denies MAKE_FIFO and MAKE_SYM) in the SUMMARY only |
| internal/worker/worker.go:1584 downloadTo, materializeExtraInputs | nothing; fresh per-attempt dir | safe |
| audiosandbox.go:200, caddxf.go/cadrender.go deferred os.Remove | n/a | removal, not a write |

Read-site inventory, built from `grep -nE 'os\.(Open|ReadFile|Stat|OpenFile)\(|os\.ReadDir\(' internal/convert/*.go internal/worker/*.go | grep -v _test.go` (pdflex*.go and pdfdictscan.go have no hits). The criterion: does the worker read the content of a job-dir path after an untrusted stage has run?
| Site | What it reads, after which stage | Planned disposition |
|---|---|---|
| caddxf.go:166 PNG judge open; caddxf.go:348 cadDXFValidate source open | rsvg / dxf output, already O_NOFOLLOW | switch to OpenJobFile (one implementation), Task 3 |
| caddxfpdf.go:1366 cadPDFDecode (JudgeCADDXFPDF on outPath, :1448/:1510/:1571) | rsvg PDF | FIX, Task 3 |
| caddxfpdf.go:1501 cadDXFReadSVGFacts | dxf SVG | FIX, Task 3 |
| caddxfpdf.go:1590 parseCADDXFTexts (caddxf.go:287) | dxf texts.json | FIX, Task 3 |
| cadmeasure.go:55 parseCADMeasureDraft (caddxf.go:135, cadrender.go:90) | dxf / mesh / stats draft | FIX, Task 3 |
| cad.go:429 judgeBinarySTL (cad.go:166, cadrender.go:82) | mesh / stats STL | FIX, Task 4 |
| cadrender.go:112 PNG judge open | render PNG | FIX, Task 4 |
| ocrpdf.go:234 page fragment | tesseract page output | FIX, Task 4 |
| sanitizepdf.go:296 redaction read of outPath | gs pdfwrite output | FIX, Task 4 |
| libreoffice.go:227/:240 validatePDF (libreoffice and chromium) | soffice / chromium output | FIX, Task 5 |
| libreoffice.go:291, :311/:318 validateDocumentOutput | soffice output | FIX, Task 5 |
| pandoc.go:242/:251 pandocValidateOutput | pandoc output | FIX, Task 5 |
| av.go:501/:510 validateAVOutput | ffmpeg output | FIX, Task 5 |
| internal/worker/worker.go:1609 uploadFrom | every engine's primary output | FIX, Task 6 |
| internal/worker/descriptors.go:412 probeSideOutputs, :684 cadSideOutputs | sanitize report, measurements sidecar | FIX, Task 6 |
| cad.go:150, caddxf.go:228, cadrender.go:46 Stat(inPath) | before any stage of the attempt | safe |
| cad.go:408 classifyCADRsvg Stat(outPath) | metadata only | safe: every caller next runs cadDXFRegular (Lstat) and then the helper open |
| sanitizepdf.go:280 Stat(psPath) | metadata only | safe: the content is consumed only by the confined gs pdfwrite pass |
| libreoffice.go:171 Stat(producedPath) | metadata only | safe: the renamed entry is read only through the Task 5 validators and the Task 6 upload |
| av.go:593 Stat(inPath) | metadata for the disk guard | safe: the content is consumed only by confined ffmpeg |
| ocr.go:193 validateOCROutput, whisper.go:408 validateAudioOutput, tectonic.go:1408 | Stat only | safe: the content reaches the worker only at the Task 6 upload. Their texts are terminal signatures (internal/worker/worker.go:50-120) and stay unchanged |
| sanitizeverify.go:242 verifyPDF | outPath | safe: it reads the file the worker itself has just created with writeNewFile after the last gs pass, and verifyPDF takes `_ engineExec` |
| sanitizeverify.go:1311 verifyImage | outPath | safe: image sanitize runs no engine, and the read is the function's first statement, before its first x.run |
| chromium.go:216, markupsniff.go:344/:355, ocrguards.go:171, sanitizeimage.go:463, tectonic.go:938/:964/:1211 | inPath or the TeX source closure, read before any engine of the attempt (the closure scan precedes the tectonic invocation) | safe |
| audiosandbox.go:176/:183, avsandbox.go:117, cadsandbox.go:326/:347/:412/:435, documentsandbox.go:419, libvips.go:152, sandboxpreflight.go:120/:137/:160/:185, tectonic.go:331 | startup preflight on worker-owned fixtures | safe |
| htmlsandbox.go:118 ReadDir | sweep of the shared /tmp before the engine; RemoveAll does not follow links | safe |
| cgroup.go:60, tectonic.go:705/:801 | not job-dir paths | safe |
| progressexec.go:103 | FIFO read end, opened by the worker before the child starts | safe |

Classification facts (read side): internal/worker/worker.go:50-120 classifies engine failures by message substring. The signatures include "output is empty", "stat output", "audio: stat output" and "produced no output file". worker.go:400 checks errors.Is(err, convert.ErrAVOutputMissingOrEmpty). So a validator that switches to OpenJobFile must report any open/fstat failure under its existing "stat output" text, or under ErrAVOutputMissingOrEmpty for AV. That keeps a missing output classified exactly as today, and a refused link is classified like a missing output.

Multipart facts (item 2):
- chi wraps every request in a copy: chi v5.3.0 mux.go:87 does `r = r.WithContext(...)`, and middleware/request_id.go:76 does the same. The handler's ParseMultipartForm therefore sets MultipartForm on the copy.
- net/http's finishRequest removes only `w.req.MultipartForm`, the original request (GOROOT go1.26.4 src/net/http/server.go:1682). So the production server leaks every spill file.
- A stdlib-only scratch probe (httptest.NewServer, a 40 MiB part, ParseMultipartForm(32<<20)) printed `withContext=false new_spill_files=0` and `withContext=true new_spill_files=1`.
- ParseMultipartForm sets r.MultipartForm and then returns parseFormErr (request.go about 1385-1402). A malformed query string such as `?%zz` therefore gives an error with the form set. The defer must be registered before the error check.
- ReadForm removes its own temp files when it fails (mime/multipart/formdata.go:75-78), so the MaxBytesError path does not leak.
- multipartInMemoryBudget = 32 << 20 (internal/api/handlers.go:64). The only ParseMultipartForm calls in the repo are handlers.go:149 and inspect_handlers.go:441. Storage uploads run synchronously inside the handler (handlers.go:1045, :1079); no goroutine uses the form after the handler returns.

Test helpers that already exist:
- In internal/convert: fakeEngine(t, dir, name, script) (sandboxexec_test.go:93); pathWith(t, dir) (sandboxexec_test.go:106); cadDXFQuote (caddxf_test.go:44); the DXF planted-symlink test to mirror (caddxf_test.go:959-989); newCADDXFFixture and the cadDXF* output builders (caddxf_test.go); newCADRenderFixture, meshScript, renderScript and sidecar() (cadrender_test.go:67-140); cadBoxSTL; engineExec{}, which runs engines unconfined.
- In internal/worker: newFakeS3 (cad_outputs_test.go:32), newCADHarness and sidecarConverter (cad_outputs_test.go:210-225), and process().
- In internal/api: multipartBody and authed (handlers_test.go:703, :821), and the newFormatCeilingServer pattern for NewServer and Config (cad_ceiling_test.go:19).
- Sanitize fixtures in internal/convert/testdata/sanitize: threat_all_classes.pdf, ooxml_rel_targets_main.docx and macro_enabled.docm.

estimate-calibration: factor 1, 0 samples, confidence low.

Task split: the read side is four tasks (3 to 6) rather than one, so that no task touches more than five files. Every source change in them is a call-site swap to the shared helper; the planted-link tests are where the work is.

Coverage of the brief:
| Brief item | Task |
|---|---|
| 1 sidecar fix with the shared helper, callers updated, cadrender planted-symlink test RED then GREEN | Task 1 |
| 1 write sweep: per-site decision, a fix and a test for every reachable site, judged-safe sites with reasons in the SUMMARY | Task 2 (and the SUMMARY table) |
| 1 read side (coordinator extension): one no-follow open + regular-file helper generalized from the DXF path, every read site after a stage routed through it with a RED-first planted-link test, judged-safe read sites with reasons, no new deferred item | Tasks 3, 4, 5, 6 (and the SUMMARY table) |
| 2 multipart spill: production path confirmed leaking, fix, before/after $TMPDIR count (after = 0) | Task 7 |
| 3 requireCADPython 3.10 floor, verified with 3.9 first (skip) and 3.14 first (pass) | Task 7 |
| mark the three items resolved; all gates | Task 7 and the verification section |
</context>

<!-- planner-discipline-allow: os.WriteFile -->
<!-- planner-discipline-allow: cadDXFWriteNew -->
<!-- planner-discipline-allow: O_NOFOLLOW -->

<tasks>

<task type="tracer" tdd="true">
  <name>Task 1: One remove-then-O_EXCL helper, end to end through the 3D sidecar</name>
  <files>internal/convert/jobdirfs.go, internal/convert/jobdirfs_test.go, internal/convert/caddxf.go, internal/convert/cadrender.go, internal/convert/cadrender_test.go</files>
  <read_first>internal/convert/cadrender.go (whole file), internal/convert/caddxf.go:96-240 (CADDXFRenderConverter.Convert, cadDXFWriteNew, cadDXFClear), internal/convert/caddxf_test.go:959-989 (TestCADDXFRenderWritesTheSidecarAsANewFile) and :44 (cadDXFQuote), internal/convert/cadrender_test.go:60-175 (fixture, meshScript, renderScript, sidecar, the first test)</read_first>
  <behavior>
    - TestCADRenderWritesTheSidecarAsANewFile, subtest "a symlink planted by cad-mesh": a STEP render whose mesh stub also runs `ln -s <outside> <sidecar>` succeeds. The outside file still reads "untouched", Lstat(sidecar) is a regular file with perm 0600, and measurements.Validate accepts its bytes.
    - The same test, subtest "a symlink planted by cad-render": the plant is in the render stub. Same assertions.
    - TestWriteNewFileNeverWritesThroughAPlantedLink. A symlink at the path leaves its target byte-identical, and the path becomes a regular file with the body and the requested perm. A hard link at the path (os.Link from an outside file) leaves the outside file byte-identical. A stale regular file is replaced, checked with 0o600 and 0o644. A non-empty directory at the path returns an error and the directory is still there.
  </behavior>
  <action>
    Run the pin guard first.

    RED. In internal/convert/cadrender_test.go, add TestCADRenderWritesTheSidecarAsANewFile, mirroring TestCADDXFRenderWritesTheSidecarAsANewFile. Loop over the planters "cad-mesh" and "cad-render", with one t.Run per planter. In each subtest:
    - Create a newCADRenderFixture(t, "step").
    - Write an outside file holding "untouched" in a separate t.TempDir().
    - Build the plant line with cadDXFQuote around both paths: `ln -s` from the outside file to f.sidecar().
    - Put the plant line in front of exactly one stub's script: f.meshScript for cad-mesh, f.renderScript for cad-render. The guard stub is cadOKGuard().
    - Convert a STEP render with the same opts as TestCADRenderStepRunsGuardMeshRenderAndWritesTheSidecar, and assert the behavior above.
    Run `GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ -run TestCADRenderWritesTheSidecarAsANewFile -count=1 -v`. Both subtests must FAIL because the worker wrote through the link. Keep the failing lines for the SUMMARY. If either subtest passes at this point, the plant does not reach the written path: stop and fix the test before going on. Commit only cadrender_test.go as `test(quick-261012-gos): planted symlink at the 3D measurements sidecar`.

    GREEN. Create internal/convert/jobdirfs.go in package convert. Task 3 adds the read helpers to the same file. For now it holds one unexported function, writeNewFile(path string, body []byte, perm fs.FileMode) error, which does the following:
    - os.Remove(path), treating fs.ErrNotExist as success. Any other error returns as fmt.Errorf("remove %s: %w").
    - os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm), with the error as "create %s: %w".
    - Write the body. On error, close the file and return "write %s: %w".
    - Close, with the error as "close %s: %w".
    The callers add their own prefixes. Name and placement are the planner's discretion: the file holds every job-dir filesystem helper, shared by CAD, OCR, sanitize and the worker, so a CAD-prefixed name would mislead. Record this choice in the SUMMARY.
    Delete cadDXFWriteNew from caddxf.go. Its caller at caddxf.go:180 becomes writeNewFile(sidecar, body, 0o600), wrapped as fmt.Errorf("cad: write measurements: %w", err). cadDXFClear stays: the DXF path still uses it at other sites.
    Make the same replacement at cadrender.go:126, keeping 0o600 and the same "cad: write measurements" wrap.
    Create internal/convert/jobdirfs_test.go with TestWriteNewFileNeverWritesThroughAPlantedLink, covering the behavior above.
    Run the verify command below. Commit jobdirfs.go, jobdirfs_test.go, caddxf.go and cadrender.go as `fix(quick-261012-gos): write the 3D measurements sidecar as a new file through one shared helper`. Do not add any comment lines anywhere.
  </action>
  <verify>
    <automated>GOTOOLCHAIN=go1.26.4 go build ./... && GOTOOLCHAIN=go1.26.4 go vet ./internal/convert/ && test -z "$(gofmt -l internal)" && GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ -run 'TestCADRender|TestCADDXFRender|TestWriteNewFile' -count=1 && ! grep -rn 'cadDXFWriteNew' internal/ && ! grep -n 'os.WriteFile' internal/convert/cadrender.go && grep -q 'func writeNewFile(' internal/convert/jobdirfs.go && git diff --quiet main -- internal/convert/exec.go && D=$(git diff -U0 d34fbdf -- internal cmd) && ! printf '%s\n' "$D" | grep -qE '^\+[[:space:]]*//' && test "$(git log -1 --format=%G?)" = G</automated>
  </verify>
  <done>Two signed commits exist: a RED commit whose two subtests failed on the old write (failure lines kept for the SUMMARY), and a GREEN commit with the shared helper. Both CAD sidecars write through writeNewFile, the DXF-only helper is gone, and all CAD render and DXF render tests pass, including the 0600 mode assertion. exec.go is unchanged and no comment lines were added.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Sweep the other job-dir writes after an untrusted stage (OCR and sanitize)</name>
  <files>internal/convert/ocrpdf.go, internal/convert/sanitizepdf.go, internal/convert/sanitizeconverter.go, internal/convert/sanitizeverify.go, internal/convert/jobdirfs_test.go</files>
  <read_first>internal/convert/ocrpdf.go:95-330 (page-size parse, guard stage, ocrPDFPage, convertPDF), internal/convert/ocrguards.go:40-75 and :205-280 (parsePdfinfoPages, ocrTextLayerArgs, extractPDFTextLayer, ocrUsableTextLayerThreshold = 16), internal/convert/ocr.go (OCRConverter fields and their zero-value behavior, Convert routing for pdf), internal/convert/sanitizepdf.go:115-135 and :263-312, internal/convert/sanitizeconverter.go:117-170, internal/convert/sanitizeverify.go:815-900, internal/convert/sanitizeooxml_test.go (how a docx is run through sanitizeFile with engineExec{}), internal/convert/sandboxexec_test.go:93-115 (fakeEngine, pathWith)</read_first>
  <behavior>
    - TestOCRPDFWritesTheOutputAsANewFile, subtest "the text-layer route": fake pdfinfo prints a "Pages: 1" line. Fake pdftotext prints more than ocrUsableTextLayerThreshold non-whitespace characters and plants a symlink from outPath to an outside file. Convert of in.pdf to out.txt succeeds. The outside file is byte-identical, and out.txt is a regular file holding the pdftotext text.
    - The same test, subtest "the page route": pdftotext prints only whitespace, so the OCR route runs. Fake pdftoppm writes the page PNG, fake tesseract writes the page text, and one of them plants the outPath symlink. Same assertions, with out.txt holding the tesseract text.
    - TestSanitizeWritesAsNewFilesAfterItsStages, subtest "the pdf redaction": a gs wrapper delegates to the real gs. After the pdfwrite pass, it moves outPath to an outside file, keeps a byte snapshot, and symlinks outPath to the outside file. sanitizePDF(ctx, engineExec{}, in, out) on testdata/sanitize/threat_all_classes.pdf then leaves out as a regular file and the outside file byte-identical to its snapshot. Task 4 tightens this subtest when it closes the read just before this write.
    - Subtest "the verify parts": a docx goes through the sanitize path with a `file` wrapper that delegates to the real file(1). On each call, it plants a symlink to the outside file at every part-NNN under its argument's directory that does not exist yet. The outside file stays byte-identical.
    - Subtest "the report": the same docx path, with a `file` wrapper that plants a symlink at SanitizeReportPath(outPath). The outside file stays byte-identical, and the report is a regular file that parses as the sanitize report.
  </behavior>
  <action>
    Run the pin guard first.

    Re-verify the write inventory. Run the write-site grep from the context facts again. Read each FIX row and each judged-safe row at its cited line, and confirm the stated precondition yourself; do not take the table on trust. Any hit that is not in the table gets classified with the same criterion. If it is a write after an untrusted stage, it joins this task with its own planted-link test. If a judged-safe row turns out to be reachable, it also becomes a FIX with a test.

    RED. Append the two tests to internal/convert/jobdirfs_test.go.
    - Build the fakes with fakeEngine in a t.TempDir() and put them first with pathWith.
    - Resolve each real binary a wrapper delegates to (gs, file) with exec.LookPath before calling pathWith, and embed the absolute path, quoted with cadDXFQuote, in the wrapper script. Hardcode no host path.
    - Plant at exactly the path the worker writes; check that it is the same object. The pdftotext args are the pdf path and `-`, so the stub cannot see outPath: embed outPath in the script. For the verify parts, the `.verify-*` directory name is random, so derive it from the directory of the wrapper's last argument.
    - Give the OCRConverter fields whatever ocr.go needs for a 1-page PDF to be admitted (MaxPages, MaxImagePixels). Its nil Sandbox means c.exec gives an unconfined engineExec.
    - When a required real binary is absent, skip the subtest and name the binary. On this host none is absent, so the SUMMARY must show PASS, not SKIP, for every subtest.
    Run `GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ -run 'TestOCRPDFWritesTheOutputAsANewFile|TestSanitizeWritesAsNewFilesAfterItsStages' -count=1 -v`. Each of the five subtests must FAIL on its own site's assertion. A subtest that passes here does not reach its site: fix the test before going on. Keep the failing lines for the SUMMARY. Commit only jobdirfs_test.go as `test(quick-261012-gos): planted links at the OCR and sanitize job-dir writes`.

    GREEN. Replace the five whole-file writes with writeNewFile. Keep each site's current perm and error wrap:
    - ocrpdf.go:264 and :319: 0o644, wrapped as "ocr: write output: %w".
    - sanitizepdf.go:304: 0o644, wrapped as "sanitize pdf: redact engine identity: %w".
    - sanitizeconverter.go:165: 0o644, wrapped as "sanitize: write report: %w".
    - sanitizeverify.go:874: 0o600, wrapped as today with ErrSanitizeUnverifiable and the part name.
    Run the verify command. Commit the four source files as `fix(quick-261012-gos): write OCR and sanitize job-dir files as new files after their stages`.

    For the SUMMARY, keep the final write-site table: each fixed site with its test name, and each judged-safe site with the line you read and why it is safe. Include the progressexec liveness note and the Landlock statement from the context facts.
  </action>
  <verify>
    <automated>S=/private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad && GOTOOLCHAIN=go1.26.4 go vet ./internal/convert/ && test -z "$(gofmt -l internal)" && GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ -run 'TestOCRPDFWritesTheOutputAsANewFile|TestSanitizeWritesAsNewFilesAfterItsStages|TestWriteNewFile' -count=1 -v > "$S/t2-green.txt" 2>&1 && ! grep -qE -- '--- (FAIL|SKIP)' "$S/t2-green.txt" && test "$(grep -cE -- '--- PASS: Test(OCRPDFWritesTheOutputAsANewFile|SanitizeWritesAsNewFilesAfterItsStages)/' "$S/t2-green.txt")" -ge 5 && ! grep -n 'os.WriteFile' internal/convert/ocrpdf.go internal/convert/sanitizepdf.go internal/convert/sanitizeconverter.go internal/convert/sanitizeverify.go && GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ -run 'OCR|Sanitize|Probe' -count=1 && git diff --quiet main -- internal/convert/exec.go && D=$(git diff -U0 d34fbdf -- internal cmd) && ! printf '%s\n' "$D" | grep -qE '^\+[[:space:]]*//' && test "$(git log -1 --format=%G?)" = G</automated>
  </verify>
  <done>Two signed commits exist: RED, with all five subtests failing on their own sites, and GREEN. All five subtests PASS with no SKIP. The OCR, sanitize and probe tests still pass. No fixed file keeps a whole-file write. The write-site table, covering every inventory hit, is ready for the SUMMARY.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: One no-follow regular-file read helper, generalized from the DXF path, and the DXF and draft reads</name>
  <files>internal/convert/jobdirfs.go, internal/convert/jobdirfs_test.go, internal/convert/caddxf.go, internal/convert/caddxfpdf.go, internal/convert/cadmeasure.go</files>
  <read_first>internal/convert/caddxf.go:96-190 and :325-380 (the PNG judge open, cadDXFRegular, cadDXFValidate), internal/convert/caddxfpdf.go:1355-1380, :1440-1455 and :1495-1660 (cadPDFDecode, JudgeCADDXFPDF, cadDXFReadSVGFacts, the svg/texts/pdf trio, parseCADDXFTexts), internal/convert/cadmeasure.go:50-95 (parseCADMeasureDraft), internal/convert/caddxf_test.go (newCADDXFFixture, cadDXFRenderOutputs, cadDXFTestFile, the stub plumbing)</read_first>
  <behavior>
    - TestOpenJobFileRefusesWhatIsNotARegularFile:
      - A regular file opens, and the returned FileInfo has its size.
      - A symlink to an outside regular file is refused with errors.Is(err, ErrJobFileIrregular), and the returned file is nil.
      - A FIFO made with syscall.Mkfifo is refused with ErrJobFileIrregular within one second, with no writer present.
      - A directory is refused with ErrJobFileIrregular.
      - A missing path gives errors.Is(err, fs.ErrNotExist).
      - ReadJobFile returns the bytes of a regular file and refuses a symlink in the same way.
      - A hard link to an outside file is read. This is the documented decision; see the residuals.
    - TestCADDXFReadsRefuseAPlantedLink, one subtest per site. Each plants a symlink at exactly the path the site reads, pointing at an outside file whose content the old code accepts, then calls the site's function directly and asserts that it returns an error and that the outside content was not consumed:
      - parseCADMeasureDraft: the outside file is a valid draft.
      - parseCADDXFTexts: the outside file is a valid texts.json.
      - cadDXFReadSVGFacts: the outside file is a valid render SVG.
      - cadPDFDecode, through JudgeCADDXFPDF: the outside file is a valid fixture PDF.
      Every subtest FAILS on the current code, which reads through the link and succeeds.
  </behavior>
  <action>
    Run the pin guard first.

    RED. In internal/convert/jobdirfs_test.go, add both tests against the names this task introduces (OpenJobFile, ReadJobFile, ErrJobFileIrregular). They do not compile yet. To show RED, first add only TestCADDXFReadsRefuseAPlantedLink, written so that it compiles against the current code: assert a non-nil error and an outside file that was not consumed. Run `GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ -run TestCADDXFReadsRefuseAPlantedLink -count=1 -v`. Every subtest must FAIL. Keep the lines, and commit as `test(quick-261012-gos): planted links at the DXF and draft reads`. Build the outside contents from the existing DXF fixtures (cadDXFTestFile, cadDXFRenderOutputs). Do not invent new fixture files.

    GREEN. Add the read helpers to internal/convert/jobdirfs.go. They are exported because internal/worker uses them in Task 6. They carry no doc comments (user rule).
    - ErrJobFileIrregular, a package-level sentinel made with errors.New("not a regular file").
    - OpenJobFile(path string) (*os.File, fs.FileInfo, error):
      - Open with os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK.
      - An ELOOP from the open (errors.Is(err, syscall.ELOOP)) becomes fmt.Errorf("%w: %s is a symbolic link", ErrJobFileIrregular, path). Any other open error returns as fmt.Errorf("open %s: %w"), which keeps fs.ErrNotExist visible to errors.Is.
      - Stat the opened descriptor (f.Stat). If the mode is not regular, close the file and return fmt.Errorf("%w: %s is %v", ErrJobFileIrregular, path, mode type).
      - Otherwise return the file and its FileInfo.
    - ReadJobFile(path string) ([]byte, error): OpenJobFile, then io.ReadAll, then close.
    These generalize the DXF path's safe read. Switch the two existing no-follow opens at caddxf.go:166 and :348 to OpenJobFile, keeping each one's existing error wrap and sentinel. Those two were already safe, so they get no RED of their own; the switch makes one implementation. cadDXFRegular stays, because it maps missing and irregular outputs to distinct DXF sentinels before the open.
    Route caddxfpdf.go:1366, :1501 and :1590 and cadmeasure.go:55 through OpenJobFile. Keep every existing error wrap and sentinel at each site, so that callers' errors.Is checks and the DXF stage-failure classes are unchanged. Keep every existing size bound, such as the maxBytes readers.
    Add TestOpenJobFileRefusesWhatIsNotARegularFile. Run the verify command. All of internal/convert's CAD tests must still pass, including the DXF live-shape tests and the bomb tests. Commit as `fix(quick-261012-gos): read DXF and draft job-dir files through one no-follow regular-file helper`.
  </action>
  <verify>
    <automated>GOTOOLCHAIN=go1.26.4 go build ./... && GOTOOLCHAIN=go1.26.4 go vet ./internal/convert/ && test -z "$(gofmt -l internal)" && GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ -run 'TestOpenJobFile|TestCADDXFReadsRefuseAPlantedLink|CAD' -count=1 && grep -q 'func OpenJobFile(' internal/convert/jobdirfs.go && grep -q 'func ReadJobFile(' internal/convert/jobdirfs.go && ! grep -nE 'os\.(Open|ReadFile)\(' internal/convert/caddxfpdf.go internal/convert/cadmeasure.go && ! grep -n 'O_NOFOLLOW' internal/convert/caddxf.go && git diff --quiet main -- internal/convert/exec.go && D=$(git diff -U0 d34fbdf -- internal cmd) && ! printf '%s\n' "$D" | grep -qE '^\+[[:space:]]*//' && test "$(git log -1 --format=%G?)" = G</automated>
  </verify>
  <done>Two signed commits exist: RED, with every DXF and draft read subtest failing through the link, and GREEN. OpenJobFile, ReadJobFile and ErrJobFileIrregular exist in jobdirfs.go. The DXF path's two former no-follow opens and the four read sites use them. All CAD tests pass.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 4: Route the 3D CAD, OCR page and sanitized-PDF reads through the helper</name>
  <files>internal/convert/cad.go, internal/convert/cadrender.go, internal/convert/ocrpdf.go, internal/convert/sanitizepdf.go, internal/convert/jobdirfs_test.go</files>
  <read_first>internal/convert/cad.go:420-470 (judgeBinarySTL), internal/convert/cadrender.go:75-125, internal/convert/ocrpdf.go:200-240 (ocrPDFPage), internal/convert/sanitizepdf.go:263-312, the Task 2 tests in internal/convert/jobdirfs_test.go (TestOCRPDFWritesTheOutputAsANewFile fakes, TestSanitizeWritesAsNewFilesAfterItsStages "the pdf redaction")</read_first>
  <behavior>
    - TestStageOutputReadsRefuseAPlantedLink, subtest "the mesh STL": judgeBinarySTL on a symlink to an outside valid binary STL (cadBoxSTL bytes) returns an error wrapping ErrJobFileIrregular. The current code returns the triangle count.
    - Subtest "the render PNG": a STEP render whose render stub symlinks its PNG argument to an outside valid frame (renderPNGBytes at the requested size) instead of copying it. CADRenderConverter.Convert returns errors.Is(err, ErrCADRenderRejected). The current code accepts the frame.
    - Subtest "the OCR page fragment": the Task 2 OCR page-route fakes, with the tesseract stub writing the page text to an outside file and symlinking pageout.txt to it. Convert fails, and out.txt does not hold the outside text. The current code succeeds.
    - The Task 2 subtest "the pdf redaction" is changed to expect the refusal at the read before the write: sanitizePDF returns an error wrapping ErrJobFileIrregular, and the outside file is byte-identical to its snapshot. The current code succeeds after reading through the link.
  </behavior>
  <action>
    Run the pin guard first.

    RED. Add TestStageOutputReadsRefuseAPlantedLink to internal/convert/jobdirfs_test.go. Change "the pdf redaction" as described in the behavior. Reuse the Task 2 fake-engine builders and the cadrender fixture rather than duplicating them; extract a small test helper inside jobdirfs_test.go if two tests need the same fakes. Run `GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ -run 'TestStageOutputReadsRefuseAPlantedLink|TestSanitizeWritesAsNewFilesAfterItsStages' -count=1 -v`. The three new subtests and the changed "the pdf redaction" must FAIL, and "the verify parts" and "the report" must still pass. Keep the lines. Commit as `test(quick-261012-gos): planted links at the 3D, OCR page and sanitized-PDF reads`.

    GREEN.
    - cad.go:429: judgeBinarySTL opens through OpenJobFile, keeping its existing error text.
    - cadrender.go:112: the PNG judge open uses OpenJobFile, and a failure keeps the existing wrap: ErrCADRenderRejected with "no image at".
    - ocrpdf.go:234: ReadJobFile, keeping "ocr: read page output".
    - sanitizepdf.go:296: ReadJobFile, with the existing "sanitize pdf: read output" wrap switched from formatting the cause as text to wrapping it, so that errors.Is sees ErrJobFileIrregular.
    Run the verify command. All OCR, sanitize, probe and CAD tests must pass. Commit as `fix(quick-261012-gos): read 3D CAD, OCR page and sanitized-PDF job-dir files through the helper`.
  </action>
  <verify>
    <automated>GOTOOLCHAIN=go1.26.4 go vet ./internal/convert/ && test -z "$(gofmt -l internal)" && GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ -run 'TestStageOutputReadsRefuseAPlantedLink|TestSanitizeWritesAsNewFilesAfterItsStages|TestOCRPDFWritesTheOutputAsANewFile|CAD|OCR|Sanitize|Probe' -count=1 && ! grep -nE 'os\.(Open|ReadFile)\(' internal/convert/cad.go internal/convert/cadrender.go internal/convert/ocrpdf.go internal/convert/sanitizepdf.go && git diff --quiet main -- internal/convert/exec.go && D=$(git diff -U0 d34fbdf -- internal cmd) && ! printf '%s\n' "$D" | grep -qE '^\+[[:space:]]*//' && test "$(git log -1 --format=%G?)" = G</automated>
  </verify>
  <done>Two signed commits exist: RED, with the four subtests failing through the link, and GREEN. None of the four files opens or reads a job-dir path except through the helper. The CAD, OCR, sanitize and probe tests pass.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 5: Route the engine output validators through the helper without changing their classification</name>
  <files>internal/convert/libreoffice.go, internal/convert/pandoc.go, internal/convert/av.go, internal/convert/jobdirfs_test.go</files>
  <read_first>internal/convert/libreoffice.go:220-340 (validatePDF, validateDocumentOutput), internal/convert/pandoc.go:236-300 (pandocValidateOutput), internal/convert/av.go:490-530 (validateAVOutput), internal/worker/worker.go:40-125 and :395-410 (terminal signature lists, the ErrAVOutputMissingOrEmpty check), internal/worker/worker_test.go around :440 (classification table)</read_first>
  <behavior>
    - TestOutputValidatorsRefuseAPlantedLink: each subtest puts a symlink at the output path, pointing at an outside file that the validator accepts today, then calls the validator directly.
      - validatePDF on a valid PDF: the error contains "stat output".
      - validateDocumentOutput(ctx, engineExec{}, link, "pdf", false) on a valid PDF: the error contains "stat output".
      - validateDocumentOutput with "pdf" and wantPDFA true on a PDF carrying the PDF/A marker: the error contains "stat output". This subtest also covers the PDF/A marker read at :291.
      - validateDocumentOutput with "docx" on a valid docx (testdata/sanitize/ooxml_rel_targets_main.docx): the error contains "stat output".
      - pandocValidateOutput("html", link) on a valid HTML file: the error contains "pandoc: stat output".
      - validateAVOutput(link, "png") on a valid PNG: errors.Is(err, ErrAVOutputMissingOrEmpty).
      Every subtest FAILS on the current code. The current validators return nil, except the PDF/A subtest, which returns a different error after reading through the link.
    - For each validator, a missing output and an empty output still produce exactly the error text they produce today.
  </behavior>
  <action>
    Run the pin guard first.

    RED. Add TestOutputValidatorsRefuseAPlantedLink to internal/convert/jobdirfs_test.go. Also add a subtest per validator that pins today's texts for a missing path and for an empty regular file, read from the code before changing it. Those pass now and must still pass after the change. Run `GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ -run TestOutputValidatorsRefuseAPlantedLink -count=1 -v`. Every link subtest must FAIL. Keep the lines. Commit as `test(quick-261012-gos): planted links at the engine output validators`.

    GREEN. In each validator, replace the stat-then-open (or the whole-file read) with a single OpenJobFile, and take the size from the FileInfo it returns.
    - Report any OpenJobFile failure under the validator's existing missing-output text, so the terminal-signature lists in internal/worker/worker.go:50-120 classify a refused link exactly like a missing output: "libreoffice: stat output: %w" in validatePDF and validateDocumentOutput, "pandoc: stat output: %w" in pandocValidateOutput, and fmt.Errorf("%w: %v", ErrAVOutputMissingOrEmpty, err) in validateAVOutput.
    - Keep every other message ("output is empty", "output missing %PDF- magic bytes", the PDF/A marker message, the container mismatch messages) byte-identical.
    - The PDF/A marker read at :291 reads from the descriptor that validatePDF already checked: either restructure so one open serves both, or call ReadJobFile there. Do not reopen with a following open.
    - Never edit the terminal-signature lists in internal/worker.
    Run the verify command, which includes the worker package tests that pin the classification. Commit as `fix(quick-261012-gos): open engine outputs through the helper in the validators`.
  </action>
  <verify>
    <automated>GOTOOLCHAIN=go1.26.4 go vet ./internal/convert/ ./internal/worker/ && test -z "$(gofmt -l internal)" && GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ -run 'TestOutputValidatorsRefuseAPlantedLink|LibreOffice|Chromium|Pandoc|AV|Validate' -count=1 && GOTOOLCHAIN=go1.26.4 go test ./internal/worker/ -count=1 && ! grep -nE 'os\.(Open|ReadFile)\(' internal/convert/libreoffice.go internal/convert/pandoc.go internal/convert/av.go && W=$(git show d34fbdf:internal/worker/worker.go) && A=$(printf '%s\n' "$W" | grep -E '^[[:space:]]+"[^"]*",$') && B=$(grep -E '^[[:space:]]+"[^"]*",$' internal/worker/worker.go) && test -n "$A" && test "$A" = "$B" && git diff --quiet main -- internal/convert/exec.go && C=$(git diff -U0 d34fbdf -- internal cmd) && ! printf '%s\n' "$C" | grep -qE '^\+[[:space:]]*//' && test "$(git log -1 --format=%G?)" = G</automated>
  </verify>
  <done>Two signed commits exist: RED, with every link subtest failing, and GREEN. The validators open outputs only through OpenJobFile. Missing and empty outputs keep their exact texts, and internal/worker's tests, including the classification table, pass. The terminal-signature string lists in internal/worker/worker.go are identical to d34fbdf.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 6: Route the worker's upload and side-output reads through the helper</name>
  <files>internal/worker/worker.go, internal/worker/descriptors.go, internal/worker/jobdirread_test.go</files>
  <read_first>internal/worker/worker.go:1440-1480 and :1600-1625 (process upload sequence, uploadFrom), internal/worker/descriptors.go:405-430 and :675-700 (probeSideOutputs, cadSideOutputs), internal/worker/cad_outputs_test.go:1-120 and :200-260 (newFakeS3, the fake S3's recorded puts, newCADHarness, sidecarConverter), internal/worker/probe_sideoutput_test.go:30-110</read_first>
  <behavior>
    - TestWorkerReadsOfTheJobDirRefuseAPlantedLink, subtest "the primary output": a test converter registered in a newCADHarness-style harness makes outPath a symlink to an outside file with distinctive bytes. Its sidecar is a valid regular file, so only the primary is linked. h.process returns an error, and no object in the fake S3 holds the outside bytes. The current code uploads them.
    - Subtest "the sanitize report": probeSideOutputs called directly with a symlink at convert.SanitizeReportPath(outPath), pointing at an outside valid report, returns an error wrapping convert.ErrJobFileIrregular. The current code returns the outside bytes as a side output.
    - Subtest "the measurements sidecar": cadSideOutputs, called directly with a symlink at convert.CADMeasurementsPath(outPath) pointing at an outside valid measurements document, returns an error wrapping convert.ErrJobFileIrregular. The current code returns it.
  </behavior>
  <action>
    Run the pin guard first.

    RED. Create internal/worker/jobdirread_test.go with the test. Reuse newFakeS3 and the harness pattern from cad_outputs_test.go; define a minimal test converter type in the new file if sidecarConverter cannot plant a link. Build the outside report and measurements bytes the same way probe_sideoutput_test.go and cad_outputs_test.go build valid ones. Run `GOTOOLCHAIN=go1.26.4 go test ./internal/worker/ -run TestWorkerReadsOfTheJobDirRefuseAPlantedLink -count=1 -v`. All three subtests must FAIL. Keep the lines. Commit as `test(quick-261012-gos): planted links at the worker upload and side-output reads`.

    GREEN.
    - uploadFrom (worker.go:1609): open through convert.OpenJobFile, and use the returned FileInfo's size instead of a second stat. A failure keeps the existing "open output: %w" wrap.
    - probeSideOutputs (descriptors.go:412) and cadSideOutputs (:684): read through convert.ReadJobFile, keeping each one's existing error wrap but wrapping the cause with %w.
    Record in the SUMMARY how a refused primary output is classified by the existing retry policy (read it from process and the descriptors; do not change it). Retries are bounded, and the refusal repeats on every attempt.
    Run the verify command. Commit as `fix(quick-261012-gos): open the uploaded output and the side outputs through the helper`.

    For the SUMMARY, keep the final read-site table: each fixed site with its test name and task, and each judged-safe read site with the line you read and why it is safe. Re-run the read-site grep from the context facts at this point. Every hit must appear in the table, and any new hit gets the same treatment as in Task 2.
  </action>
  <verify>
    <automated>S=/private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad && GOTOOLCHAIN=go1.26.4 go vet ./internal/worker/ && test -z "$(gofmt -l internal)" && GOTOOLCHAIN=go1.26.4 go test ./internal/worker/ -count=1 && GOTOOLCHAIN=go1.26.4 go test ./internal/worker/ -run TestWorkerReadsOfTheJobDirRefuseAPlantedLink -count=1 -v > "$S/t6-green.txt" 2>&1 && ! grep -qE -- '--- (FAIL|SKIP)' "$S/t6-green.txt" && test "$(grep -c -- '--- PASS: TestWorkerReadsOfTheJobDirRefuseAPlantedLink/' "$S/t6-green.txt")" -ge 3 && ! grep -nE 'os\.(Open|ReadFile)\(' internal/worker/worker.go internal/worker/descriptors.go && git diff --quiet main -- internal/convert/exec.go && D=$(git diff -U0 d34fbdf -- internal cmd) && ! printf '%s\n' "$D" | grep -qE '^\+[[:space:]]*//' && test "$(git log -1 --format=%G?)" = G</automated>
  </verify>
  <done>Two signed commits exist: RED, with all three subtests failing, and GREEN. The worker reads no job-dir path except through the helper. All internal/worker tests pass, and the read-site table, covering every grep hit, is ready for the SUMMARY.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 7: Multipart spill removal in the handlers, the python 3.10 floor, and closing the deferred items</name>
  <files>internal/api/handlers.go, internal/api/inspect_handlers.go, internal/api/multipart_spill_test.go, internal/convert/cadguard_test.go, .planning/phases/75-dxf-2d-pipeline/deferred-items.md</files>
  <read_first>internal/api/handlers.go:140-160 and :195-230, internal/api/inspect_handlers.go:425-470, internal/api/cad_ceiling_test.go:1-45, internal/api/handlers_test.go:695-830 (multipartBody, authed), internal/api/api.go:270-360 (Config fields MaxUploadBytes, InspectMaxUploadBytes, InspectMaxWorkBytes), internal/api/routes.go:36-80, internal/convert/cadguard_test.go:40-55, internal/convert/cadmesh_test.go:40-60</read_first>
  <behavior>
    - TestMultipartSpillFilesAreRemovedWhenTheHandlerReturns sets TMPDIR to a fresh t.TempDir() with t.Setenv and does not run in parallel. It posts a file part of multipartInMemoryBudget + 1 MiB bytes, so ReadForm must spill. After each request completes, filepath.Glob(TMPDIR/multipart-*) is empty. For the real server, check after the response body is drained and ts.Close() has returned. Subtests:
      - "create through the real server": httptest.NewServer(srv.Routes()), POST /v1/jobs with the authed header.
      - "create through ServeHTTP": srv.Routes().ServeHTTP on a recorder.
      - "inspect through the real server": POST /v1/inspect.
      - "inspect through ServeHTTP".
      - "a malformed query with the form set": /v1/jobs?%zz. ParseMultipartForm returns the ParseForm error after it has set the form.
      - "over the upload limit": the body crosses MaxUploadBytes after the spill starts, and the response is 413. ReadForm cleans up its own files, so this subtest passes before the fix too. It guards the exit.
    - Every subtest except the last two also asserts that the parse succeeded. The response must not be the 400 "invalid multipart form" or the 413 "file exceeds size limit" body, so a green result cannot come from a request that never spilled.
    - requireCADPython: with SCRATCH/py39 first on PATH, every caller skips with "python3 3.9.6 at <path> is older than 3.10". With SCRATCH/py314 first, none skips and TestCADMeshRefusesPartialMeshingBeforeWritingTheSTL passes. If the interpreter cannot report its version, the test fails (t.Fatalf). It does not skip.
  </behavior>
  <action>
    Run the pin guard first.

    Baseline (item 2, before any internal/api change). Assert `git diff --quiet d34fbdf -- internal/api`. Then:
    - Save the sorted names of "$TMPDIR"/multipart-* to SCRATCH/mp-before.txt.
    - Run `GOTOOLCHAIN=go1.26.4 go test ./internal/api/... -count=1`.
    - Save the after list and diff the two with comm -13. Record the new file count and each size from `ls -l` for the SUMMARY. The deferred item measured 6 files at 66060289, 123731969 and 141557787 bytes.
    - Check with lsof that no process holds them, then delete only those new files.

    RED (item 2). Create internal/api/multipart_spill_test.go with TestMultipartSpillFilesAreRemovedWhenTheHandlerReturns, as described in the behavior.
    - Build the server with NewServer and the same fakes as newFormatCeilingServer. Set Config.MaxUploadBytes, InspectMaxUploadBytes and InspectMaxWorkBytes to 64 << 20, so a budget-plus-1-MiB part parses. For the over-limit subtest, build a second server whose MaxUploadBytes is budget + 4 MiB and post budget + 8 MiB.
    - Build the body with multipartBody. For the real-server subtests, build an http.Request with http.NewRequest against ts.URL and pass it through authed. If `?%zz` does not make ParseMultipartForm return an error on this toolchain, find a query that does (url.ParseQuery must fail on it) and record which one in the SUMMARY.
    Run `GOTOOLCHAIN=go1.26.4 go test ./internal/api/ -run TestMultipartSpillFilesAreRemovedWhenTheHandlerReturns -count=1 -v`. The real-server, ServeHTTP and malformed-query subtests must FAIL, which proves the production leak through chi's request copy. "over the upload limit" is expected to PASS. Keep the lines. Commit only the test file as `test(quick-261012-gos): multipart spill files outlive the handler`.

    GREEN (item 2).
    - handlers.go:149: split the combined if-statement. Call ParseMultipartForm into err, then add `if r.MultipartForm != nil { defer r.MultipartForm.RemoveAll() }` before the existing error branch, and keep the error branch exactly as it is.
    - inspect_handlers.go:441: add the same guarded defer immediately after `parseErr := r.ParseMultipartForm(...)`, before the semaphore release and the parseErr branch.
    - Use the handler's own r in both places: that is the copy chi passed in, and the object the parse set the form on. Add no test-helper change. The handler now removes its own form for every caller, and the ServeHTTP subtests prove it for the path the ceiling tests use. Record in the SUMMARY why the item's suggested test-helper fix was superseded: the item's own condition, a real server path that also leaks, was met.
    - Run the test; all subtests must pass.
    - Repeat the baseline measurement with the same steps. The new multipart-* count must be 0. Record it.
    Commit handlers.go and inspect_handlers.go as `fix(quick-261012-gos): remove spilled multipart files when the handler returns`.

    Item 3. In internal/convert/cadguard_test.go, after the LookPath in requireCADPython:
    - Run the found interpreter with exec.Command(p, "-c", "import sys; print('%d.%d.%d' % sys.version_info[:3])").Output(). On error, call t.Fatalf with p and the error.
    - Split the trimmed output on dots and parse major and minor with strconv.Atoi, calling t.Fatalf on a parse failure.
    - If major < 3, or major == 3 and minor < 10, call t.Skipf("python3 %s at %s is older than 3.10", version, p).
    - Return p.
    Check both interpreters:
    - `PATH=SCRATCH/py39:$PATH GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ -run CAD -count=1 -v` shows a SKIP naming 3.9.6 for TestCADMeshRefusesPartialMeshingBeforeWritingTheSTL and no FAIL.
    - The same command with SCRATCH/py314 first shows `--- PASS: TestCADMeshRefusesPartialMeshingBeforeWritingTheSTL` and no "older than 3.10" skip.
    Keep both result lines for the SUMMARY. Commit as `test(quick-261012-gos): skip the CAD python tests below python 3.10`.

    Close-out. In .planning/phases/75-dxf-2d-pipeline/deferred-items.md, add a `- **Resolved:** quick 261012-gos, <short sha(s)>: <one line>` bullet under each of the three items.
    - The 75-07 item's line says that the write side and the read side of the same class were both closed through the shared helpers.
    - The 75-10 item's line says that the real server path leaked too (chi's request copy), so the fix is in the two handlers.
    - Add nothing else to the file: no new section and no new open item. The coordinator closed the read side in this task instead of deferring it.
    Stage it with `git add -f` and commit as `docs(quick-261012-gos): mark the phase 75 deferred items resolved`.

    Then run every gate in the verification section and write the SUMMARY. It contains:
    - the write-site table from Task 2 and the read-site table from Task 6;
    - the Landlock statement and the read-helper residuals;
    - the RED failure lines of every task;
    - the multipart before/after counts and sizes;
    - both python result lines;
    - the gate transcript lines;
    - the commit list with %G?;
    - the helper name and export decisions.
  </action>
  <verify>
    <automated>S=/private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad && GOTOOLCHAIN=go1.26.4 go vet ./internal/api/ ./internal/convert/ && test -z "$(gofmt -l internal)" && GOTOOLCHAIN=go1.26.4 go test ./internal/api/ -run TestMultipartSpillFilesAreRemovedWhenTheHandlerReturns -count=1 -v > "$S/t7-mp.txt" 2>&1 && ! grep -qE -- '--- (FAIL|SKIP)' "$S/t7-mp.txt" && test "$(grep -c -- '--- PASS: TestMultipartSpillFilesAreRemovedWhenTheHandlerReturns/' "$S/t7-mp.txt")" -ge 6 && grep -q 'MultipartForm.RemoveAll' internal/api/handlers.go && grep -q 'MultipartForm.RemoveAll' internal/api/inspect_handlers.go && PATH="$S/py39:$PATH" GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ -run TestCADMeshRefusesPartialMeshingBeforeWritingTheSTL -count=1 -v > "$S/t7-py39.txt" 2>&1 && grep -q 'older than 3.10' "$S/t7-py39.txt" && PATH="$S/py314:$PATH" GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ -run TestCADMeshRefusesPartialMeshingBeforeWritingTheSTL -count=1 -v > "$S/t7-py314.txt" 2>&1 && grep -q -- '--- PASS: TestCADMeshRefusesPartialMeshingBeforeWritingTheSTL' "$S/t7-py314.txt" && awk '/^- \*\*Resolved:\*\* quick 261012-gos/{n++} END{exit !(n==3)}' .planning/phases/75-dxf-2d-pipeline/deferred-items.md && ! grep -q '^## From quick 261012-gos' .planning/phases/75-dxf-2d-pipeline/deferred-items.md && git diff --quiet main -- internal/convert/exec.go && D=$(git diff -U0 d34fbdf -- internal cmd) && ! printf '%s\n' "$D" | grep -qE '^\+[[:space:]]*//' && G=$(git log d34fbdf..HEAD --format=%G?) && test -n "$G" && test -z "$(printf '%s\n' "$G" | grep -v '^G$')" && B=$(git log d34fbdf..HEAD --format=%B) && ! printf '%s\n' "$B" | grep -qi 'co-authored-by'</automated>
  </verify>
  <done>The multipart test passes all six subtests. The real-server, ServeHTTP and malformed-query subtests failed before the handler change. The $TMPDIR multipart-* delta of `go test ./internal/api/... -count=1` was recorded before the fix (count and sizes) and is 0 after it. requireCADPython skips with 3.9.6 named and passes with 3.14. deferred-items.md has three Resolved bullets and nothing new. All commits are signed with no Co-Authored-By. The SUMMARY is written with every element listed in the action.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| confined engine stage -> worker | The stage runs on client bytes with read-write access to the job dir; whatever it leaves there is untrusted when the worker writes or reads that path afterwards |
| client upload -> API process | Authenticated internal clients post multipart bodies up to the per-route ceilings; parts above 32 MiB spill to the API host's temp dir |
| test host -> CAD python tests | Interpreter version decides whether a helper test is meaningful |

## STRIDE Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation Plan |
|-----------|----------|-----------|----------|-------------|-----------------|
| T-261012-gos-01 | Tampering / Elevation of privilege | worker writes at job-dir paths after a stage (3D/DXF sidecars, OCR PDF output, sanitized PDF, sanitize report, OOXML verify parts) | medium | mitigate | writeNewFile removes the entry, then creates it with O_EXCL. Neither a symlink nor a hard link left by a stage is written through. Planted-link tests per site, RED first. Severity is medium because Landlock already denies MAKE_SYM and REFER on the job dir (sandbox_linux.go:17-20, :50-54); this removes the dependency on that one bit |
| T-261012-gos-02 | Tampering | re-plant race between the remove and the O_EXCL create by a still-running stage process | low | accept | O_CREATE together with O_EXCL fails with EEXIST on any existing entry, including a symlink, so the job fails closed |
| T-261012-gos-03 | Information disclosure | worker reads of job-dir paths after a stage: the primary upload (worker.go:1609), side outputs (descriptors.go:412, :684), CAD reads (cad.go:429, cadrender.go:112, cadmeasure.go:55, caddxfpdf.go:1366/:1501/:1590), the OCR page fragment (ocrpdf.go:234), the sanitized-PDF read (sanitizepdf.go:296) and the engine output validators | medium | mitigate | OpenJobFile/ReadJobFile open with O_NOFOLLOW and O_NONBLOCK and fstat-check that the file is regular, so a planted link's target is never read or uploaded. Planted-link tests per site, RED first. Medium for the same Landlock reason as T-01 |
| T-261012-gos-04 | Denial of service | a FIFO or device planted at a read path that blocks the open or reads forever | low | mitigate | O_NONBLOCK on the open plus the regular-file check refuses it at once. The helper unit test covers a FIFO with no writer |
| T-261012-gos-05 | Information disclosure | residuals of the read helper: intermediate directory swap, hard links | low | accept | O_NOFOLLOW guards the final component. Intermediate job-dir directories are created by the worker, and only an unconfined stage could swap them. A cross-directory hard link needs REFER, which Landlock denies, and is limited further by protected_hardlinks. Both are stated in the SUMMARY |
| T-261012-gos-06 | Repudiation | a refused output misclassified as retryable or terminal against today's policy | low | mitigate | The validators keep their missing-output texts and sentinels. internal/worker/worker.go is untouched by the validator task, and the worker classification tests pass |
| T-261012-gos-07 | Denial of service | multipart spill files leaked by the production API: chi's request copy means net/http finishRequest never sees the form | high | mitigate | A guarded defer RemoveAll right after ParseMultipartForm, before the error check, in handlers.go and inspect_handlers.go. Real-server and ServeHTTP tests, plus the malformed-query path where the form is set together with an error. The $TMPDIR delta of the api test run is 0 |
| T-261012-gos-08 | Repudiation | a CAD python test that silently skips or passes for the wrong reason | low | mitigate | The skip names the version and path. An unreadable version is t.Fatalf. The gate runs under both 3.9.6 and 3.14 |
| T-261012-gos-SC | Tampering | npm/pip/cargo installs | high | accept | No package is added or installed. Only stdlib and existing module dependencies are used |
</threat_model>

<verification>
Run from the repo root after Task 7. Run the pin guard first. Record each line in the SUMMARY.
1. `test -z "$(gofmt -l .)"`
2. `GOTOOLCHAIN=go1.26.4 go vet ./...`
3. Take a $TMPDIR multipart-* snapshot. Run `PATH=SCRATCH/py314:$PATH GOTOOLCHAIN=go1.26.4 go test ./... -count=1`; it must pass. The multipart-* delta must be 0.
4. `PATH=SCRATCH/py39:$PATH GOTOOLCHAIN=go1.26.4 go test ./... -count=1` must pass. The CAD python tests skip and name 3.9.6.
5. `git diff --quiet main -- internal/convert/exec.go`
6. `D=$(git diff -U0 d34fbdf -- internal cmd) && ! printf '%s\n' "$D" | grep -qE '^\+[[:space:]]*//'`. No comment lines were added.
7. Re-run the write-site and read-site greps from the context facts. Every hit appears in the SUMMARY tables.
8. `git log d34fbdf..HEAD --format='%h %G? %s'`: every line shows G. No commit body carries Co-Authored-By.
9. `git status -sb` shows the branch ahead of its upstream, with no push performed. Only the pre-existing dirt (.planning/config.json, .vscode/, octoconv.code-workspace) stays unstaged.
</verification>

<success_criteria>
- All three deferred items of phase 75 are fixed as classes and marked resolved, with commit references, in deferred-items.md. No new deferred item is added.
- Every worker write into a job dir after an untrusted stage uses writeNewFile, and every worker content read of a job-dir path after an untrusted stage uses OpenJobFile or ReadJobFile. Each fixed site has a planted-link test that failed before its change. Every other write and read site is in the SUMMARY with its reason.
- Output classification is unchanged: the validators keep their texts and sentinels, and the worker tests pass.
- The API removes spilled multipart files on every exit through the real server. `go test ./internal/api/... -count=1` leaves 0 new multipart-* files, and the before count and sizes are recorded.
- requireCADPython enforces the 3.10 floor, with the version in the skip message. It is verified under 3.9.6 and 3.14.
- All gates in the verification section pass, and exec.go is byte-identical to main.
</success_criteria>

<output>
Create `.planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/261012-gos-SUMMARY.md` when done, and commit it with `git add -f`.
</output>
