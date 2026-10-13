---
phase: quick-261012-gos
plan: 01
subsystem: convert, worker, api
status: complete
tags: [security, defense-in-depth, job-dir, symlink, multipart, tests]
requires: []
provides:
  - internal/convert/jobdirfs.go writeNewFile, OpenJobFile, ReadJobFile, ErrJobFileIrregular
  - guarded MultipartForm.RemoveAll in handleCreateJob and handleInspect
  - requireCADPython 3.10 floor
affects: [internal/convert, internal/worker, internal/api]
tech-stack:
  added: []
  patterns:
    - remove-then-O_EXCL write for every worker write into a job dir after an untrusted stage
    - O_NOFOLLOW|O_NONBLOCK open plus fstat regular-file check for every worker content read of a job-dir path after an untrusted stage
key-files:
  created:
    - internal/convert/jobdirfs.go
    - internal/convert/jobdirfs_test.go
    - internal/worker/jobdirread_test.go
    - internal/api/multipart_spill_test.go
  modified:
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
    - internal/convert/cadguard_test.go
    - internal/worker/worker.go
    - internal/worker/descriptors.go
    - internal/convert/whisper.go
    - internal/api/handlers.go
    - internal/api/inspect_handlers.go
    - .planning/phases/75-dxf-2d-pipeline/deferred-items.md
decisions:
  - One file, internal/convert/jobdirfs.go, holds every job-dir filesystem helper; the write helper is unexported (only package convert writes job-dir files after a stage), the read helpers are exported because internal/worker uses them.
  - OpenJobFile and ReadJobFile return the os path error unwrapped (it already names the op and the path), instead of the plan's extra "open %s:" wrap, which would print the path twice.
  - A hard link at a read path is read; Landlock denies REFER, so a confined stage cannot make one across directories.
  - The multipart fix lives in the two handlers, not in a test helper, because the real server path leaks too.
  - Coordinator follow-up: the output validators keep the old classification for every case that is not a planted irregular entry (missing is the old terminal "stat output" text, any other open failure is the old "open output" text), and an irregular entry is terminal under the missing-output text.
  - Coordinator follow-up: uploadFrom fails a planted (irregular) primary output terminally through the existing asynq.SkipRetry mechanism; every other open error stays transient.
metrics:
  duration: 62min
  completed: 2026-10-12
estimate:
  tokens: 400000
  tasks: 7
actuals:
  tokens: 40600
  tasks: 7
  commits: 19
plan_head_before: d34fbdf601fdbd692d349ee656915d21270a33e3
plan_head_after: eebd7ef14ae6e508eea8598e9c3d04c5d9997d08
---

# Phase quick-261012-gos Plan 01: Close the phase 75 deferred items Summary

**The worker now writes every job-dir file that follows an untrusted stage with remove-then-O_EXCL (`writeNewFile`). It reads every such path through an O_NOFOLLOW|O_NONBLOCK open with an fstat regular-file check (`OpenJobFile`/`ReadJobFile`). The API removes spilled multipart files when a handler returns. The CAD python tests skip below python 3.10 and name the version they found.**

`actuals.tokens` is chars/4 over `git diff d34fbdf..HEAD -- internal .planning`, which is 162,406 chars at eebd7ef (it now includes the PLAN and the first version of this SUMMARY). `commits: 19` counts the commits from d34fbdf to eebd7ef: the 16 plan commits, the first PLAN+SUMMARY commit 7576d41, and the two coordinator follow-up commits. This SUMMARY's update commit comes after those and is not in the count.

## Threat statement (what this does and does not close)

Both fixes are defense-in-depth. They do not close a live exploit.

- In `internal/sandbox/sandbox_linux.go:17-20`, the job directory's `accessReadWrite` is READ_FILE, READ_DIR, WRITE_FILE, TRUNCATE, MAKE_REG, MAKE_DIR, REMOVE_FILE and REMOVE_DIR. It does not include MAKE_SYM.
- At `:50-54`, `handledAccess` adds MAKE_SYM, MAKE_SOCK, MAKE_FIFO, MAKE_BLOCK, MAKE_CHAR and REFER, and none of them is granted anywhere. A confined stage therefore cannot create a symlink, a FIFO or a cross-directory hard link in the job dir.
- An unconfined stage runs as the worker user and already has all of the worker's access.
- What the helpers remove is the dependency on those ruleset bits. A planted link is never written through and its target is never read.

### Read-helper residuals

These are stated here and not coded around:

- O_NOFOLLOW guards only the final path component. The intermediate job-dir directories the worker creates (`.ocr-pdf-*`, `.verify-*`, `.sanitize-pdf-*`) could be swapped only by an unconfined stage.
- A hard link is a regular file, so the helper reads it. Planting a cross-directory hard link needs REFER, which Landlock denies, and Linux protected_hardlinks restricts it further. `TestOpenJobFileRefusesWhatIsNotARegularFile/a_hard_link_is_read` pins this decision.
- Write race: if a stage process re-plants between the remove and the create, O_CREATE|O_EXCL fails with EEXIST and the job fails closed.
- progressexec liveness: the two `os.OpenFile(fifoPath, O_WRONLY)` pokes (progressexec.go:177, :187) are not content writes. They have no O_CREATE and no O_TRUNC, write zero bytes and close at once. If a stage could substitute the FIFO, an unconfined one could make that open block. Landlock denies MAKE_FIFO and MAKE_SYM.

## Write-site inventory (final)

Built from `grep -nE 'os\.(WriteFile|Create|OpenFile|Rename|Mkdir|MkdirAll|MkdirTemp|Remove|Symlink|Link)\(|syscall\.Mkfifo' internal/convert/*.go internal/worker/*.go | grep -v _test.go`, re-run after Task 2. The criterion: could an untrusted stage have written into that directory before this write? Line numbers are from the final tree.

| Site | Disposition | Evidence |
|---|---|---|
| cadrender.go sidecar (Convert, after renderStage) | FIX, Task 1: `writeNewFile(sidecar, body, 0o600)`, wrap "cad: write measurements" | TestCADRenderWritesTheSidecarAsANewFile (planted by cad-mesh, planted by cad-render) |
| caddxf.go DXF sidecar (was cadDXFWriteNew) | already remove+O_EXCL; moved to the shared helper, Task 1; cadDXFWriteNew deleted | TestCADDXFRenderWritesTheSidecarAsANewFile still passes |
| caddxf.go:342 cadDXFValidate clean.svg | safe: cadDXFClear, then O_CREATE\|O_EXCL | read at :333-345 |
| ocrpdf.go text-layer write and page-route write | FIX, Task 2: writeNewFile 0o644, "ocr: write output" | TestOCRPDFWritesTheOutputAsANewFile (the text-layer route, the page route) |
| sanitizepdf.go redaction write | FIX, Task 2: writeNewFile 0o644, "sanitize pdf: redact engine identity" | TestSanitizeWritesAsNewFilesAfterItsStages/the_pdf_redaction |
| sanitizeconverter.go report write | FIX, Task 2: writeNewFile 0o644, "sanitize: write report" | TestSanitizeWritesAsNewFilesAfterItsStages/the_report |
| sanitizeverify.go:874 `.verify-*/part-NNN` | FIX, Task 2: writeNewFile 0o600, same ErrSanitizeUnverifiable wrap | TestSanitizeWritesAsNewFilesAfterItsStages/the_verify_parts |
| sanitizeooxml.go:542, sanitizeimage.go:488 outPath | safe: both functions take `_ engineExec` (sanitizeooxml.go:424, sanitizeimage.go:462), and sanitizePageCeilings has only "pdf", so no stage has run in this attempt's dir | read |
| chromium.go:233 rendered.html | safe: only sweepSharedTmp (worker code) runs before it; chromium runs after it (:239), in a fresh per-attempt MkdirTemp dir (worker.go:1395); sharedTmpGuard pins shared-/tmp classes to concurrency 1 | read |
| libreoffice.go:87, :92 MkdirAll | safe for the same reason as chromium: soffice runs later | read |
| libreoffice.go:174, ocr.go:261, tectonic.go:1382 Rename | safe: rename(2) replaces the destination entry and never writes through a link. A directory at the destination fails closed. Reads of the renamed entry go through the Task 5 validators and the Task 6 upload | read |
| cadsandbox.go:128 cadPrivateDir | safe: Mkdir, then an Lstat check for a 0700 directory, which refuses a planted link | read at :127-139 |
| cadsandbox.go:352, :440; sandboxpreflight.go:56, :60, :114, :117; tectonic.go:281, :298; libvips.go:124, :131; documentsandbox.go:383 | safe: startup preflight on worker-owned fixtures in fresh MkdirTemp dirs | read |
| ocrsandbox.go:67, sanitizepdf.go:264, sanitizeverify.go:858, progressexec.go:85 MkdirTemp | safe: MkdirTemp creates a new random-named directory with mkdir(2), which fails on an existing entry | not in the planning table; classified here |
| progressexec.go:95 Mkfifo; :103 O_RDONLY\|O_NONBLOCK; :177/:187 O_WRONLY poke | not a content write (see the liveness residual above) | read |
| worker.go:1395 MkdirTemp, :1584 downloadTo os.Create | safe: a fresh per-attempt dir before any stage | read |
| audiosandbox.go:200, caddxf.go:62-63/:121-123/:224/:352, cadrender.go:62, ocrpdf.go:224/:232, sanitize.go:142 os.Remove | removal, not a write | read |
| jobdirfs.go:11, :18 | the helper itself | |

## Read-site inventory (final)

Built from `grep -nE 'os\.(Open|ReadFile|Stat|OpenFile)\(|os\.ReadDir\(' internal/convert/*.go internal/worker/*.go | grep -v _test.go`, re-run after Task 6. The criterion: does the worker read the content of a job-dir path after an untrusted stage has run?

| Site | Disposition | Task / test |
|---|---|---|
| caddxf.go PNG judge open, cadDXFValidate source open (both were already O_NOFOLLOW) | switched to OpenJobFile, so there is one implementation; same wraps | Task 3 (already safe, so no RED of their own) |
| caddxfpdf.go cadPDFDecode (through JudgeCADDXFPDF) | FIX: OpenJobFile; the FileInfo replaces f.Stat; "cad: open the PDF" wrap and the size bound kept | Task 3, TestCADDXFReadsRefuseAPlantedLink/cadPDFDecode_through_JudgeCADDXFPDF |
| caddxfpdf.go cadDXFReadSVGFacts | FIX: OpenJobFile, "cad: open the SVG" | Task 3, .../cadDXFReadSVGFacts |
| caddxfpdf.go parseCADDXFTexts | FIX: OpenJobFile, "cad: open texts.json", maxBytes LimitReader kept | Task 3, .../parseCADDXFTexts |
| cadmeasure.go parseCADMeasureDraft | FIX: OpenJobFile, ErrCADMeasure wrap and the draft byte cap kept | Task 3, .../parseCADMeasureDraft |
| cad.go judgeBinarySTL | FIX: OpenJobFile; same text "%w: no STL at %s: ..." with the cause now also %w | Task 4, TestStageOutputReadsRefuseAPlantedLink/the_mesh_STL |
| cadrender.go PNG judge open | FIX: OpenJobFile, ErrCADRenderRejected "no image at" kept | Task 4, .../the_render_PNG |
| ocrpdf.go page fragment | FIX: ReadJobFile, "ocr: read page output" | Task 4, .../the_OCR_page_fragment |
| sanitizepdf.go read before redaction | FIX: ReadJobFile, "sanitize pdf: read output: %w" (it was already %w) | Task 4, TestSanitizeWritesAsNewFilesAfterItsStages/the_pdf_redaction (now expects ErrJobFileIrregular) |
| libreoffice.go validatePDF (libreoffice and chromium) | FIX: statJobOutput (os.Stat for the old text, then an Lstat regular check) under "libreoffice: stat output: %w"; the old size checks; then OpenJobFile, where an irregular entry is "libreoffice: stat output" and any other failure is the old "libreoffice: open output: %w" | Task 5 + follow-up, TestOutputValidatorsRefuseAPlantedLink/validatePDF_* |
| libreoffice.go validateDocumentOutput pdf, PDF/A marker read, and non-pdf | FIX: validatePDF as above; the marker read uses ReadJobFile (irregular is "stat output", anything else keeps "read output for PDF/A check"); non-pdf is stat, size check, then open, same mapping as validatePDF | Task 5 + follow-up, .../validateDocumentOutput_{pdf,pdf/a,docx}_* |
| pandoc.go pandocValidateOutput | FIX: statJobOutput under "pandoc: stat output"; an empty output is still nil and is not opened; then OpenJobFile, irregular is "pandoc: stat output", anything else the old "pandoc: open output" | Task 5 + follow-up, .../pandocValidateOutput_html_* |
| av.go validateAVOutput | FIX: statJobOutput, failure is `%w: %v` with ErrAVOutputMissingOrEmpty (old text for missing); empty is still the bare sentinel; only thumbnails open (as before), any open failure is the sentinel (as before) | Task 5 + follow-up, .../validateAVOutput_png_* |
| whisper.go validateAudioOutput | FIX (follow-up): statJobOutput under the old "audio: stat output: %w"; still never opens the file | follow-up, .../validateAudioOutput_txt_* |
| worker.go uploadFrom | FIX: convert.OpenJobFile; the FileInfo size feeds Upload; an irregular entry is `asynq.SkipRetry` + "open output" (terminal), any other failure keeps the old transient "open output: %w" | Task 6 + follow-up, TestWorkerReadsOfTheJobDirRefuseAPlantedLink/the_primary_output, TestUploadFromRefusesAPlantedPrimaryTerminally, TestUploadFromOtherOpenErrorsStayTransient |
| descriptors.go probeSideOutputs | FIX: convert.ReadJobFile, "sanitize report missing beside the output: %w" | Task 6, .../the_sanitize_report |
| descriptors.go cadSideOutputs | FIX: convert.ReadJobFile, cause switched from %v to %w | Task 6, .../the_measurements_sidecar |
| cad.go:150, caddxf.go:212, cadrender.go:46 Stat(inPath) | safe: before any stage of the attempt | read |
| cad.go:408 classifyCADRsvg Stat(outPath) | safe: metadata only; every caller next runs cadDXFRegular (Lstat) and then the helper open | read |
| sanitizepdf.go:280 Stat(psPath) | safe: metadata only; the content is consumed only by the confined gs pdfwrite pass | read |
| libreoffice.go:171 Stat(producedPath) | safe: metadata only; the renamed entry is read only through the Task 5 validators and the Task 6 upload | read |
| av.go:589 Stat(inPath) | safe: disk-guard metadata; content consumed only by confined ffmpeg | read |
| ocr.go:193, tectonic.go:1408 | safe: Stat only; the content reaches the worker only at the Task 6 upload; their terminal-signature texts are unchanged | read |
| sanitizeverify.go:242 verifyPDF | safe: it reads the file the worker itself has just created with writeNewFile after the last gs pass (the read before that write is now ReadJobFile); verifyPDF takes `_ engineExec` | read |
| sanitizeverify.go:1311 verifyImage | safe: image sanitize runs no engine, and the read is the function's first statement | read |
| chromium.go:216, markupsniff.go:344/:355, ocrguards.go:171, sanitizeimage.go:463, tectonic.go:938/:964/:1211 | safe: inPath or the TeX source closure, read before any engine of the attempt | read |
| tectonic.go:614 Stat(abs) | safe: an existence check while building the sandbox spec's read-only paths; reads no job-dir content | not in the planning table; classified here |
| audiosandbox.go:176/:183, avsandbox.go:117, cadsandbox.go:326/:347/:412/:435, documentsandbox.go:419, libvips.go:152, sandboxpreflight.go:120/:137/:160/:185, tectonic.go:331 | safe: startup preflight on worker-owned fixtures | read |
| htmlsandbox.go:118 ReadDir | safe: sweep of the shared /tmp before the engine; RemoveAll does not follow links | read |
| cgroup.go:60, tectonic.go:705/:801 | safe: not job-dir paths | read |
| progressexec.go:103 | safe: the FIFO read end, opened by the worker before the child starts | read |

### Coordinator follow-up: classification restored (commits 76c9bca RED, eebd7ef GREEN)

The coordinator did not accept two behavior changes, and both are now reverted to the old classification. A planted irregular entry is the only new terminal case.

**Validators.** The new helper `statJobOutput` in jobdirfs.go runs `os.Stat`, which keeps the exact old error text, then an `os.Lstat` regular-file check. Neither uses a file descriptor. Each validator now does, in order:
1. statJobOutput. Any failure is the old missing-output text: `libreoffice: stat output: stat <path>: no such file or directory`, the same for pandoc and audio, and `av: output missing or empty: stat <path>: ...`. All of these are terminal, as before. An irregular entry (symlink, FIFO, directory) also fails here, under the same text and wrapping ErrJobFileIrregular, so it is terminal.
2. The old size checks, unchanged. An empty output is still never opened.
3. OpenJobFile, only where the old code opened. ErrJobFileIrregular (a re-plant after step 1) maps to the "stat output" text, which is terminal. Any other failure maps to the old `<engine>: open output: %w` text. For libreoffice and chromium that text is transient (retried), exactly as before. For pandoc it is terminal, exactly as before, because isPandocTerminal treats every "pandoc:" error as terminal. AV opens only thumbnails, as before, and reports any open failure as ErrAVOutputMissingOrEmpty, as before. Audio never opens, as before.

`TestOutputValidatorsRefuseAPlantedLink` pins five cases for each validator, including validateAudioOutput: a planted link, a planted FIFO (must not block), a missing output (exact old text), an empty output (exact old text), and a mode-000 output (exact old `open output: open <path>: permission denied` text, with fs.ErrPermission visible). `TestValidatorFailureTextsKeepTheirClassification` pins the worker side: for the document, html, pandoc, av and audio descriptors, a missing output and an irregular entry are terminal, and an open failure is transient for document and html. It is terminal for pandoc and av, which is the old behavior.

**uploadFrom.** An ErrJobFileIrregular from OpenJobFile now returns `fmt.Errorf("%w: open output: %w", asynq.SkipRetry, err)`. runner.go:294 sends any SkipRetry error to the existing terminal branch: MarkFailed with the descriptor's FailureCode, then a SkipRetry return, so there is no retry. Every other open error keeps the old `open output: %w` and stays transient.
- `TestUploadFromRefusesAPlantedPrimaryTerminally` drives NewRunner(...).Handle for all ten descriptors: each returns SkipRetry and records exactly one MarkFailed.
- `TestUploadFromOtherOpenErrorsStayTransient` uses a mode-000 primary: each descriptor returns a non-SkipRetry error carrying the old text, with no MarkFailed.

No terminal-signature entry was needed. The string lists in internal/worker/worker.go are still byte-identical to d34fbdf.

## RED evidence (each failed before its change)

- Task 1 (c092fa8): `--- FAIL: TestCADRenderWritesTheSidecarAsANewFile/a_symlink_planted_by_cad-mesh` and `/a_symlink_planted_by_cad-render`. Both reported `the worker wrote through a planted sidecar symlink: "{\"schema\":\"octoconv.measurements/v1\",...}"` and `sidecar = ... want a regular file`.
- Task 2 (48985d3): all five subtests failed on their own sites.
  - `TestOCRPDFWritesTheOutputAsANewFile/the_text-layer_route` and `/the_page_route`: the outside file changed to the pdftotext or tesseract text, and out.txt was `L---------`.
  - `TestSanitizeWritesAsNewFilesAfterItsStages/the_pdf_redaction`: "the worker wrote the redacted PDF through a planted link".
  - `/the_verify_parts`: the outside file became `<?xml ...><Relationships .../>`.
  - `/the_report`: the outside file became the report JSON, and the report path was `L---------`.
- Task 3 (982ace8): `TestCADDXFReadsRefuseAPlantedLink`, all four subtests: `/parseCADMeasureDraft` (read the draft), `/parseCADDXFTexts` (1 text, nil), `/cadDXFReadSVGFacts` (49 texts, nil) and `/cadPDFDecode_through_JudgeCADDXFPDF` (PaintOps:232, nil).
- Task 4 (cbdc2e1):
  - `TestStageOutputReadsRefuseAPlantedLink/the_mesh_STL` (12 triangles, nil), `/the_render_PNG` (Convert = nil) and `/the_OCR_page_fragment` (out.txt held the outside text) all failed.
  - `TestSanitizeWritesAsNewFilesAfterItsStages/the_pdf_redaction` failed with `sanitizePDF = <nil>`.
  - `/the_verify_parts` and `/the_report` still passed.
- Task 5 (7a25d55): all six `TestOutputValidatorsRefuseAPlantedLink/*_a_planted_link` subtests failed. Five returned nil, and the PDF/A one returned `libreoffice: verapdf: pdf/a validation error: locate engine verapdf ...` after reading through the link. The 12 missing and empty pin subtests passed before and after.
- Task 6 (ef27f77): `TestWorkerReadsOfTheJobDirRefuseAPlantedLink`, all three subtests.
  - `/the_primary_output`: "the outside bytes reached the object store", and MarkDone was called.
  - `/the_sanitize_report`: 1 output, nil.
  - `/the_measurements_sidecar`: 1 output, nil.
- Coordinator follow-up (76c9bca):
  - Every `TestOutputValidatorsRefuseAPlantedLink/*_a_missing_output` subtest failed, because the text read `open <path>` where the old text was `stat <path>`. This covered validatePDF, validateDocumentOutput pdf, pdf/a and docx, pandoc and AV.
  - Every libreoffice and pandoc `*_an_output_that_cannot_be_opened` subtest failed, because the text was `stat output: open ...: permission denied` where the old one was `open output: ...`.
  - `validateAudioOutput_txt_a_planted_link` and `_a_planted_fifo` failed: the link returned nil, and the FIFO returned "audio: output is empty".
  - `TestWorkerReadsOfTheJobDirRefuseAPlantedLink/the_primary_output` failed with "want a SkipRetry wrapping ErrJobFileIrregular".
  - All ten `TestUploadFromRefusesAPlantedPrimaryTerminally/*` subtests failed: there was no SkipRetry and MarkFailed was called 0 times.
- Task 7 (63ad6c9): `TestMultipartSpillFilesAreRemovedWhenTheHandlerReturns/create_through_the_real_server`, `/create_through_ServeHTTP`, `/inspect_through_the_real_server`, `/inspect_through_ServeHTTP` and `/a_malformed_query_with_the_form_set` each failed with "1 multipart spill files outlived the handler". `/over_the_upload_limit` passed, as expected.

## Multipart spill (item 2)

- Production leak confirmed: the real-server subtests failed before the fix.
- `?%zz` works on go1.26.4. ParseMultipartForm returns the ParseForm error with the form already set, the handler answers 400, and the spill was left behind before the fix.
- Before the fix, `go test ./internal/api/... -count=1` was run on a `git archive` of d34fbdf in the scratchpad, after disk space was freed. It left **6 new multipart-\* files**: 66060289, 66060289, 123731969, 123731969, 141557787 and 141557787 bytes (about 0.66 GB). Those are exactly the deferred item's sizes. No process held them (lsof), and they were deleted.
  - That archive run had 8 unrelated FAILs, because the archive did not contain docs/openapi.yaml or .env.example.
  - The first in-tree before-fix run, on the full disk, left only 3 files (66060289, 66060289, 141557787). Its 141 MiB and 123 MiB ceiling subtests failed with "invalid multipart form" because of ENOSPC, so that count is only a lower bound.
- After the fix (fcd561b), `go test ./internal/api/... -count=1` gives `ok` and **0 new multipart-\* files**. The full-suite gates 3 and 4 also each left 0.
- The item's suggested test-helper fix was superseded. Its own condition, "production code unchanged unless a real server path also leaks", was met: chi passes the handler a request copy (`r.WithContext`), and net/http's finishRequest removes only `w.req.MultipartForm`. The defer is registered right after ParseMultipartForm and before the error check, on the handler's own `r`. In handleInspect it comes before the semaphore release.

## Python floor (item 3)

- With `SCRATCH/py39` first on PATH: `cadmesh_test.go:124: python3 3.9.6 at .../scratchpad/py39/python3 is older than 3.10`. All 17 `TestCADMeshRefusesPartialMeshingBeforeWritingTheSTL/*` subtests SKIP, and there is no FAIL. The full CAD run has 40 skip lines naming 3.9.6.
- With `SCRATCH/py314` first: `--- PASS: TestCADMeshRefusesPartialMeshingBeforeWritingTheSTL (2.68s)`, with no "older than 3.10" line.
- An interpreter that cannot report its version is a `t.Fatalf`, not a skip.

## Gate transcript (re-run after the coordinator follow-up at eebd7ef, from the repo root)

These are the final numbers. The first run after Task 7 had the same results.

1. `test -z "$(gofmt -l .)"`: PASS
2. `GOTOOLCHAIN=go1.26.4 go vet ./...`: PASS (rc 0, no output)
3. `PATH=SCRATCH/py314:$PATH GOTOOLCHAIN=go1.26.4 go test -p 2 ./... -count=1`: PASS (rc 0, 31 `ok` packages, no FAIL). The multipart-* delta was 0.
3a. Gate 3 was re-run with `-v` at eebd7ef: 31 `ok`, 0 `--- FAIL`, none of this plan's tests skipped, and the multipart-* delta was 0.
4. `PATH=SCRATCH/py39:$PATH GOTOOLCHAIN=go1.26.4 go test -p 2 ./... -count=1 -v`: PASS (rc 0, 31 `ok`, 0 `--- FAIL`). There were 40 "python3 3.9.6 ... is older than 3.10" skips, none of this plan's new tests skipped, and the multipart-* delta was 0.
4a. The terminal-signature string lists in internal/worker/worker.go are byte-identical to d34fbdf.
5. `git diff --quiet main -- internal/convert/exec.go`: PASS
6. No added comment lines in `git diff -U0 d34fbdf -- internal cmd`: PASS
7. Write-site and read-site greps re-run: every hit is in the tables above. Four MkdirTemp hits and tectonic.go:614 were not in the planning tables and are classified there.
8. `git log d34fbdf..HEAD --format='%h %G? %s'`: every line is G, and no body has Co-Authored-By (count 0).
9. `git status -sb`: the branch has no upstream configured and nothing was pushed. Only the pre-existing dirt is unstaged: .planning/config.json, .vscode/ and octoconv.code-workspace.

`-p 2` in gates 3 and 4 is an environment adjustment; see the deviations.

## Commits

| Hash | %G? | Subject |
|---|---|---|
| c092fa8 | G | test(quick-261012-gos): planted symlink at the 3D measurements sidecar |
| 4939340 | G | fix(quick-261012-gos): write the 3D measurements sidecar as a new file through one shared helper |
| 48985d3 | G | test(quick-261012-gos): planted links at the OCR and sanitize job-dir writes |
| 07b9c41 | G | fix(quick-261012-gos): write OCR and sanitize job-dir files as new files after their stages |
| 982ace8 | G | test(quick-261012-gos): planted links at the DXF and draft reads |
| e90c0c4 | G | fix(quick-261012-gos): read DXF and draft job-dir files through one no-follow regular-file helper |
| cbdc2e1 | G | test(quick-261012-gos): planted links at the 3D, OCR page and sanitized-PDF reads |
| de21bc3 | G | fix(quick-261012-gos): read 3D CAD, OCR page and sanitized-PDF job-dir files through the helper |
| 7a25d55 | G | test(quick-261012-gos): planted links at the engine output validators |
| d572972 | G | fix(quick-261012-gos): open engine outputs through the helper in the validators |
| ef27f77 | G | test(quick-261012-gos): planted links at the worker upload and side-output reads |
| 9b14473 | G | fix(quick-261012-gos): open the uploaded output and the side outputs through the helper |
| 63ad6c9 | G | test(quick-261012-gos): multipart spill files outlive the handler |
| fcd561b | G | fix(quick-261012-gos): remove spilled multipart files when the handler returns |
| cbca9c8 | G | test(quick-261012-gos): skip the CAD python tests below python 3.10 |
| 8bad15b | G | docs(quick-261012-gos): mark the phase 75 deferred items resolved |
| 7576d41 | G | docs(quick-261012-gos): plan and summary for closing the phase 75 deferred items |
| 76c9bca | G | test(quick-261012-gos): pin the old validator classification and a terminal refusal of a planted primary output |
| eebd7ef | G | fix(quick-261012-gos): keep the old output classification and fail a planted primary output terminally |

## Helper name and export decisions

- `internal/convert/jobdirfs.go` holds every job-dir filesystem helper, shared by CAD, OCR, sanitize and the worker. A CAD-prefixed name would mislead, so `cadDXFWriteNew` was deleted, not generalized under its old name. `cadDXFClear` and `cadDXFRegular` stay, because the DXF path still uses them for its distinct missing and irregular sentinels.
- `writeNewFile(path, body, perm)` is unexported, because only package convert writes job-dir files after a stage. Callers add their own prefixes.
- `OpenJobFile(path) (*os.File, fs.FileInfo, error)`, `ReadJobFile(path) ([]byte, error)` and `ErrJobFileIrregular` are exported, because internal/worker uses them. None of them has a doc comment (user rule).

## Deviations from Plan

1. **[Rule 3 - Blocking] Disk exhaustion.** During Task 2, `go build ./...` failed with ENOSPC: the data volume had 1.8 GiB free and later fell to about 140 MiB. Most of the disk is held by another project's session directory, /private/tmp/claude-501/-Users-apaderin-work-gsh-intake-service, which grew from 35 to 36 GB while this plan ran. That directory was not touched.
   - I ran `go clean -cache` on the regenerable Go build cache (3.3 GiB) and ran heavy builds and tests with `-p 2`.
   - Before the cache clear, the in-tree before-fix multipart count was cut short by ENOSPC, so it was re-measured on a git archive of d34fbdf (see item 2).
2. **[Rule 1 - Bug] The triangle-cap test fixture placed its sparse STL with a symlink.** `statsScriptLinking` did this, and the hardened mesh read correctly refuses a symlink. The fixture now uses a hard link (`ln`), which is the documented "a hard link is read" decision. It still avoids writing a 100 MB file. Commit de21bc3, internal/convert/cadrender_test.go.
3. **OpenJobFile and ReadJobFile return the os path error unwrapped.** The plan wrapped them as `open %s: %w`, `stat %s: %w` and `read %s: %w`. The `*PathError` already names the op and the path, so the extra wrap would print the path twice. errors.Is(fs.ErrNotExist) still holds (tested). Commit d572972. Since the follow-up, the validators no longer report OpenJobFile's error as a missing output; they stat first, so a missing output has its old exact text again.
4. **Some causes are now also wrapped with %w.** judgeBinarySTL and cadSideOutputs wrap the cause with a second `%w`, keeping the visible text, so that errors.Is sees ErrJobFileIrregular, as the plan's tests require.
5. **The deferred-items Resolved bullets sit at column 0** (`- **Resolved:** ...`) right under each item's sub-bullets, so the plan's own verify awk (`/^- \*\*Resolved:\*\* quick 261012-gos/`, exactly 3) matches. No new section and no new open item.
6. **Task 3 and Task 4 verify runs used SCRATCH/py314 first on PATH.** With the host's default python3 (3.9.6), the pre-existing item-3 failure of TestCADMeshRefusesPartialMeshingBeforeWritingTheSTL was the only failure in the `CAD` run. That is the item Task 7 fixes, not a regression.

## Known Stubs

None.

## Self-Check: PASSED

All created files exist, and all 16 commit hashes are ancestors of HEAD (verified below before the SUMMARY commit).

## Live regression (2026-10-12, at 46e742c, gate budget fixed at 4c1c1ef)

**Result: all gates PASS after one budget change.** At 46e742c the ci-local `gate` tier FAILED under its 5m `go test` timeout (rows 1a–1c), while b2c0db8 passed it. The cause was a test-time budget, not a behavior change. Every live gate on running stacks passed at 46e742c. Commit 4c1c1ef raised the gate timeout to 10m, and the re-run at 4c1c1ef passed (row 1d).

How it was run:
- Every run used a fresh `git clone --branch gsd/phase-75-dxf-2d-pipeline` at 46e742c in the scratchpad (`rg/octoconv`). The compose project is `octoconv` and tracked dirt is 0, which every transcript header confirms.
- `GOTOOLCHAIN=go1.26.4`, with python 3.14 first on PATH.
- The VM free-space watchdog ran for every docker step, and host free space was checked before each one. Lowest values seen: VM 15.3 GiB, host 20.4 GiB.
- Transcripts are in `evidence/`, all prefixed `rg-`.

| # | Gate | Result | Evidence |
|---|------|--------|----------|
| 1a | `scripts/ci-local.sh helm gate race docker-build e2e`, run 1 | **AT LEAST ONE TIER FAILED.** helm PASS. gate FAIL: `internal/convert` hit the 5m timeout at 304.8s. race PASS. docker-build FAIL and e2e FAIL: the clone has no `deploy/tectonic-bundle/octoconv-bundle.ttb`, which is a gitignored release asset, so the compose build stopped at the COPY. | `rg-ci-local-run1-20261012T113321Z.log` |
| 1b | the same command, run 2, after copying the bundle from the main tree. `scripts/tectonic-bundle-fetch.sh --check` matched the Dockerfile pin f42d5eed…, and the file is ignored, so tracked dirt stayed 0. | **AT LEAST ONE TIER FAILED.** helm PASS. **gate FAIL**: `internal/convert` 305.6s, 5m timeout. race PASS (`internal/convert` 462.0s). docker-build PASS (every bake target). e2e PASS (`internal/e2e` 119.2s, full stack built from 46e742c). | `rg-ci-local-run2-20261012T115951Z.log` |
| 1c | gate tier back to back from two clean clones: b2c0db8 first, then 46e742c | b2c0db8 **PASS** (`internal/convert` 285.1s). 46e742c **FAIL** (304.9s, 5m timeout). | `rg-gate-b2c0db8-20261012T124216Z.log`, `rg-gate-46e742c-20261012T124733Z.log` |
| 1d | `scripts/ci-local.sh helm gate race` at 4c1c1ef (gate timeout 10m), from a fresh clone with the bundle copied in; tracked dirt 0, no TRACKED CHANGES marker | **ALL REQUESTED TIERS PASSED.** helm PASS. gate PASS (`internal/convert` 307.5s, which would have failed at 5m). race PASS (`internal/convert` 483.0s). docker-build and e2e were not re-run: both passed at 46e742c (row 1b), and 4c1c1ef changes only the `go test` timeout of the gate step, which neither tier runs. | `rg-ci-local-run3-4c1c1ef-20261012T134203Z.log` |
| 2 | `scripts/cad-liveproof.sh --out …/rg-cad-liveproof.txt` | **PASS.** `SUMMARY,result=ALL PASS`, with `HDR,commit=46e742c…` and `HDR,tracked_dirt=0`. All 27 SUMMARY counters equal 75-10's: every phase-74 section (formats 1, inspect 9, convert 5, render 5, views 6, meas 10, bomb 12, retries, cancels, worker_kill 1, sandbox 1) and every phase-75 section (dxf_svg 5, dxf_pdf 3, dxf_render 4, dxf_meas 7, dxf_opts_reject 4, dxf_bomb 15, dxf_ceiling 1, dxf_retry 2, dxf_cancel 1). | `rg-cad-liveproof.txt`, `rg-cad-liveproof.transcript.log` |
| 3 | `scripts/sandbox-liveproof.sh <class>` for all 9 classes the script knows | **PASS**, every class. image 4/4, pandoc 4/4, ocr 4/4, av 4/4, audio 4/4, html 9/9, probe 4/4, document 7/7, cad 5/5. 0 FAIL, 0 NOT RUN. The arm64 server ran the emulated-amd64 checks for document and cad. | `rg-sandbox-liveproof-<class>.txt`, `rg-sandbox-liveproof-progress.log` |
| 4a | `scripts/cancel-liveproof.sh` (minimal stack: postgres, redis, minio, createbucket, api, av-worker) | **PASS**, 12 checks. The ffmpeg PID was gone after cancel. | `rg-cancel-liveproof.txt` |
| 4b | `scripts/av-progress-liveproof.sh` | **PASS**, 40 checks. A completed transcode reached `progress_pct=100` and done. | `rg-av-progress-liveproof.txt` |
| 4c | `scripts/sse-liveproof.sh` | **PASS**, 15 checks. The streamed job reached done. | `rg-sse-liveproof.txt` |

What the passing gates cover of the changed code:
- **Multipart cleanup.** `handleCreateJob`'s guarded `MultipartForm.RemoveAll` is exercised by every upload in e2e, cad, cancel, av-progress and sse. `handleInspect`'s is exercised by cad-liveproof's 9 INSPECT lines, which go through `POST /v1/inspect`.
- **Worker upload and engine validators, per class:**
  - e2e covers libreoffice (document, cross-format, PDF/A), chromium (html), image, audio, ocr, pandoc, sanitize and tectonic (multi-file LaTeX).
  - The av proofs cover av, with completed transcodes.
  - cad-liveproof covers cad: 3D, DXF, render sidecars and the measurements read.

**Gate 1 diagnosis.**
- **The mechanism is a time budget.** ci.yml's gate job runs `go test ./... -timeout 5m` (line 64), and ci-local mirrors it. `internal/convert` was already near that limit before this task: 281.0s at 75-09 and 259.5s at 75-10.
- **What this task added,** from standalone `go test -json` runs back to back (`rg-convert-timing.txt`): the package went from 271.0s at b2c0db8 to 295.0s at 46e742c.
  - The 8 new top-level tests account for 10.7s. The largest are TestSanitizeWritesAsNewFilesAfterItsStages 3.41s, TestStageOutputReadsRefuseAPlantedLink 2.45s, TestCADRenderWritesTheSidecarAsANewFile 2.39s and TestOCRPDFWritesTheOutputAsANewFile 2.36s.
  - `requireCADPython` now starts python once per call: about 0.1s, at 24 call sites.
  - The rest is run-to-run variance in python-heavy CAD guard tests.
- **Why both gate timeouts name TestEngineExecSandboxFaultSentinel:** it is the test running when the package crosses 300s. In the standalone run it started at offset 276.5s and passed in 0.28s, so it is not a hang.
- **Why the task's own gate missed it:** gates 3 and 4 above ran `go test -p 2 ./... -count=1` with the default 10m timeout, not CI's 5m.
- **Options considered:**
  1. Raise the gate `-timeout` in ci.yml and ci-local together.
  2. Make the slow new tests share their fixtures or run with `t.Parallel()`.
  3. Have `requireCADPython` cache its version probe. This saves about 2.5s, which is not enough on its own.
- **Change made (coordinator decision): option 1, commit 4c1c1ef.**
  - `.github/workflows/ci.yml:64` and `scripts/ci-local.sh:136` both go from `-timeout 5m` to `-timeout 10m`, the race tier's existing value. The race tier runs the same package set slower (462–485s) and passes at 10m.
  - No test changed. Neither file has prose explaining the 5m figure.
  - A grep for `timeout 5m` across the repo finds nothing else that pins the gate value:
    - `helm … --wait --timeout 5m` in the keda scripts is a helm install wait;
    - `OCTOCONV_PROBE_BUDGET` 5m is a measurement budget;
    - the compose and webhook "5m" mentions are delivery timeouts;
    - `internal/e2e/e2e_test.go:587` describes that test's own timeout, and that test self-skips in the gate tier (the e2e tier runs with `-timeout 30m`).

**Not run, with reasons:**
- **`scripts/presets-acceptance.sh` and `scripts/presets-rest-acceptance.sh`.** Each brings up the full e2e stack (all 13 services) to prove preset logic, which this task did not touch. Their job-creation path is the same multipart handler the e2e tier drove on that same full stack.
- **`scripts/inspect-burst-liveproof.sh`, `scripts/quota-multireplica-liveproof.sh` and every `scripts/keda-*.sh`.** They need OrbStack Kubernetes enabled, which needs an OrbStack restart. That is ruled out for this run, and compose and k8s must not run hot together.

**Cleanup.**
- **Compose stack.** `docker compose -p octoconv stop`, never `down -v`; no octoconv container is running. The e2e tier's own teardown ran `down -v` inside ci-local, which is that script's designed behavior.
- **Images removed:**
  - the octoconv-{audio,chromium,document,ocr,pandoc,probe,tectonic}-worker and webhook-worker-1/2 images this run built;
  - `hibiken/asynqmon:0.7.2` and `busybox:1.36.1`, which this run pulled;
  - six dangling builds from this run.
- **Images kept:**
  - octoconv-api, octoconv-worker and octoconv-cad-worker, which existed before the run and now carry 46e742c builds;
  - octoconv-av-worker, which the stopped stack uses.
  - Their three previous builds are left dangling and untouched.
- **Build history and cache.** The run's 67 build-history records were removed. Its build-cache records were pruned by ID, except the 7 that back the kept images. No other project's image, container, volume or cache record was touched.
- **Clones.** The scratch clones were deleted after the evidence was copied.
