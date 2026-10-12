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
    - internal/api/handlers.go
    - internal/api/inspect_handlers.go
    - .planning/phases/75-dxf-2d-pipeline/deferred-items.md
decisions:
  - One file, internal/convert/jobdirfs.go, holds every job-dir filesystem helper; the write helper is unexported (only package convert writes job-dir files after a stage), the read helpers are exported because internal/worker uses them.
  - OpenJobFile and ReadJobFile return the os path error unwrapped (it already names the op and the path), instead of the plan's extra "open %s:" wrap, which would print the path twice.
  - A hard link at a read path is read; Landlock denies REFER, so a confined stage cannot make one across directories.
  - The multipart fix lives in the two handlers, not in a test helper, because the real server path leaks too.
metrics:
  duration: 62min
  completed: 2026-10-12
estimate:
  tokens: 400000
  tasks: 7
actuals:
  tokens: 13900
  tasks: 7
  commits: 16
plan_head_before: d34fbdf601fdbd692d349ee656915d21270a33e3
plan_head_after: 8bad15b5c267b7d2b2b5497acc007f530d840f9e
---

# Phase quick-261012-gos Plan 01: Close the phase 75 deferred items Summary

**The worker now writes every job-dir file that follows an untrusted stage with remove-then-O_EXCL (`writeNewFile`). It reads every such path through an O_NOFOLLOW|O_NONBLOCK open with an fstat regular-file check (`OpenJobFile`/`ReadJobFile`). The API removes spilled multipart files when a handler returns. The CAD python tests skip below python 3.10 and name the version they found.**

`actuals.tokens` is chars/4 over `git diff d34fbdf..HEAD -- internal .planning`, which is 55,609 chars. `commits: 16` counts the commits from d34fbdf to 8bad15b. This SUMMARY's own commit comes after those and is not in the count.

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
- Classification edge: an open of an output that exists but fails for another reason (EACCES or EMFILE) used to surface as "libreoffice: open output", which was retryable. It now surfaces under "libreoffice: stat output", which is terminal in the chromium/libreoffice lists, exactly like a missing output. This was the plan's instruction (report every OpenJobFile failure under the missing-output text), and it is recorded here as a behavior change for that rare case.

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
| libreoffice.go validatePDF (libreoffice and chromium) | FIX: one OpenJobFile; size from its FileInfo; failure reported as "libreoffice: stat output: %w" | Task 5, TestOutputValidatorsRefuseAPlantedLink/validatePDF_* |
| libreoffice.go validateDocumentOutput pdf, PDF/A marker read, and non-pdf | FIX: validatePDF as above; the marker read uses ReadJobFile; non-pdf uses one OpenJobFile under "libreoffice: stat output" | Task 5, .../validateDocumentOutput_{pdf,pdf/a,docx}_* |
| pandoc.go pandocValidateOutput | FIX: one OpenJobFile under "pandoc: stat output"; an empty output is still nil | Task 5, .../pandocValidateOutput_html_* |
| av.go validateAVOutput | FIX: one OpenJobFile, failure is `%w: %v` with ErrAVOutputMissingOrEmpty; empty is still the bare sentinel | Task 5, .../validateAVOutput_png_* |
| worker.go uploadFrom | FIX: convert.OpenJobFile; the FileInfo size feeds Upload; "open output: %w" kept | Task 6, TestWorkerReadsOfTheJobDirRefuseAPlantedLink/the_primary_output |
| descriptors.go probeSideOutputs | FIX: convert.ReadJobFile, "sanitize report missing beside the output: %w" | Task 6, .../the_sanitize_report |
| descriptors.go cadSideOutputs | FIX: convert.ReadJobFile, cause switched from %v to %w | Task 6, .../the_measurements_sidecar |
| cad.go:150, caddxf.go:212, cadrender.go:46 Stat(inPath) | safe: before any stage of the attempt | read |
| cad.go:408 classifyCADRsvg Stat(outPath) | safe: metadata only; every caller next runs cadDXFRegular (Lstat) and then the helper open | read |
| sanitizepdf.go:280 Stat(psPath) | safe: metadata only; the content is consumed only by the confined gs pdfwrite pass | read |
| libreoffice.go:171 Stat(producedPath) | safe: metadata only; the renamed entry is read only through the Task 5 validators and the Task 6 upload | read |
| av.go:589 Stat(inPath) | safe: disk-guard metadata; content consumed only by confined ffmpeg | read |
| ocr.go:193, whisper.go:408, tectonic.go:1408 | safe: Stat only; the content reaches the worker only at the Task 6 upload; their terminal-signature texts are unchanged | read |
| sanitizeverify.go:242 verifyPDF | safe: it reads the file the worker itself has just created with writeNewFile after the last gs pass (the read before that write is now ReadJobFile); verifyPDF takes `_ engineExec` | read |
| sanitizeverify.go:1311 verifyImage | safe: image sanitize runs no engine, and the read is the function's first statement | read |
| chromium.go:216, markupsniff.go:344/:355, ocrguards.go:171, sanitizeimage.go:463, tectonic.go:938/:964/:1211 | safe: inPath or the TeX source closure, read before any engine of the attempt | read |
| tectonic.go:614 Stat(abs) | safe: an existence check while building the sandbox spec's read-only paths; reads no job-dir content | not in the planning table; classified here |
| audiosandbox.go:176/:183, avsandbox.go:117, cadsandbox.go:326/:347/:412/:435, documentsandbox.go:419, libvips.go:152, sandboxpreflight.go:120/:137/:160/:185, tectonic.go:331 | safe: startup preflight on worker-owned fixtures | read |
| htmlsandbox.go:118 ReadDir | safe: sweep of the shared /tmp before the engine; RemoveAll does not follow links | read |
| cgroup.go:60, tectonic.go:705/:801 | safe: not job-dir paths | read |
| progressexec.go:103 | safe: the FIFO read end, opened by the worker before the child starts | read |

### Classification of a refused primary output (Task 6, read, not changed)

When uploadFrom refuses a planted link, `process` returns `open output: not a regular file: <path> is a symbolic link` with no further wrap.
- No terminal-signature substring matches it (worker.go:50-120), and it carries no CAD sentinel, so every descriptor's IsTerminal (runner.go:294) treats it as transient.
- The job stays active and asynq retries it within the engine's retry budget. Each attempt re-runs the conversion in a fresh `MkdirTemp` work dir (worker.go:1395), so the refusal repeats whenever the stage plants again.
- When the budget is exhausted, the reconciler marks the job failed.
- For CAD renders the path is mostly unreachable: CADRenderConverter's own PNG judge refuses an irregular PNG first with ErrCADRenderRejected, which is terminal.

### Validator texts (Task 5)

The terminal-signature string lists in internal/worker/worker.go are byte-identical to d34fbdf, which the Task 5 verify checked. `go test ./internal/worker/ -count=1` passes unchanged.
- An empty output keeps its exact text: "libreoffice: output is empty", the bare `ErrAVOutputMissingOrEmpty` ("av: output missing or empty"), and nil for pandoc.
- A missing output keeps the validator's prefix ("libreoffice: stat output: ", "pandoc: stat output: ", and `ErrAVOutputMissingOrEmpty` + ": "). Only the os error inside it changed, from `stat <path>: no such file or directory` to `open <path>: no such file or directory`, because the single open replaced the stat. The pin subtests assert the prefix, the path and the ENOENT suffix.

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

## Gate transcript (after Task 7, from the repo root)

1. `test -z "$(gofmt -l .)"`: PASS
2. `GOTOOLCHAIN=go1.26.4 go vet ./...`: PASS (rc 0, no output)
3. `PATH=SCRATCH/py314:$PATH GOTOOLCHAIN=go1.26.4 go test -p 2 ./... -count=1`: PASS (rc 0, 31 `ok` packages, no FAIL). The multipart-* delta was 0.
4. `PATH=SCRATCH/py39:$PATH GOTOOLCHAIN=go1.26.4 go test -p 2 ./... -count=1 -v`: PASS (rc 0, 31 `ok`, 0 `--- FAIL`). There were 40 "python3 3.9.6 ... is older than 3.10" skips, none of this plan's new tests skipped, and the multipart-* delta was 0.
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

## Helper name and export decisions

- `internal/convert/jobdirfs.go` holds every job-dir filesystem helper, shared by CAD, OCR, sanitize and the worker. A CAD-prefixed name would mislead, so `cadDXFWriteNew` was deleted, not generalized under its old name. `cadDXFClear` and `cadDXFRegular` stay, because the DXF path still uses them for its distinct missing and irregular sentinels.
- `writeNewFile(path, body, perm)` is unexported, because only package convert writes job-dir files after a stage. Callers add their own prefixes.
- `OpenJobFile(path) (*os.File, fs.FileInfo, error)`, `ReadJobFile(path) ([]byte, error)` and `ErrJobFileIrregular` are exported, because internal/worker uses them. None of them has a doc comment (user rule).

## Deviations from Plan

1. **[Rule 3 - Blocking] Disk exhaustion.** During Task 2, `go build ./...` failed with ENOSPC: the data volume had 1.8 GiB free and later fell to about 140 MiB. Most of the disk is held by another project's session directory, /private/tmp/claude-501/-Users-apaderin-work-gsh-intake-service, which grew from 35 to 36 GB while this plan ran. That directory was not touched.
   - I ran `go clean -cache` on the regenerable Go build cache (3.3 GiB) and ran heavy builds and tests with `-p 2`.
   - Before the cache clear, the in-tree before-fix multipart count was cut short by ENOSPC, so it was re-measured on a git archive of d34fbdf (see item 2).
2. **[Rule 1 - Bug] The triangle-cap test fixture placed its sparse STL with a symlink.** `statsScriptLinking` did this, and the hardened mesh read correctly refuses a symlink. The fixture now uses a hard link (`ln`), which is the documented "a hard link is read" decision. It still avoids writing a 100 MB file. Commit de21bc3, internal/convert/cadrender_test.go.
3. **OpenJobFile and ReadJobFile return the os path error unwrapped.** The plan wrapped them as `open %s: %w`, `stat %s: %w` and `read %s: %w`. The `*PathError` already names the op and the path, so the extra wrap would print the path twice. errors.Is(fs.ErrNotExist) still holds (tested). Commit d572972.
4. **Some causes are now also wrapped with %w.** judgeBinarySTL and cadSideOutputs wrap the cause with a second `%w`, keeping the visible text, so that errors.Is sees ErrJobFileIrregular, as the plan's tests require.
5. **The deferred-items Resolved bullets sit at column 0** (`- **Resolved:** ...`) right under each item's sub-bullets, so the plan's own verify awk (`/^- \*\*Resolved:\*\* quick 261012-gos/`, exactly 3) matches. No new section and no new open item.
6. **Task 3 and Task 4 verify runs used SCRATCH/py314 first on PATH.** With the host's default python3 (3.9.6), the pre-existing item-3 failure of TestCADMeshRefusesPartialMeshingBeforeWritingTheSTL was the only failure in the `CAD` run. That is the item Task 7 fixes, not a regression.

## Known Stubs

None.

## Self-Check: PASSED

All created files exist, and all 16 commit hashes are ancestors of HEAD (verified below before the SUMMARY commit).
