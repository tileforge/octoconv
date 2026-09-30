---
phase: 70-document-on-amd64
plan: 05
subsystem: infra
tags: [landlock, sandbox, document-worker, e2e, liveproof, conf05, inversions, todo]

requires:
  - phase: 70-document-on-amd64
    provides: "document confined in code (70-03), deployment config and the emulated-amd64 refusal (70-04), measured sets on the native image (70-02), DIVERGENCES: 0 (70-01)"
provides:
  - "e2e fault-injection shim running inside soffice's ruleset: fixture beneath /usr/lib, sh and sed only, byte-identical copy proven under the real ruleset"
  - "TestDocumentConversionE2E, TestPDFAExportE2E (both subtests), TestPDFANonCompliantE2E passing through the confined worker on a stack rebuilt from this tree"
  - "five class-closure inversions shown to fail and reverted; eight-class line-exact cross-check (eight MATCH lines)"
  - "eight-class live gate transcript: 39 of 40 checks PASS, 1 FAIL that is not caused by this phase (see Open issue)"
  - "CONF-07 and the Phase 70 goal and criterion 1 reworded (D-04), the single amd64 follow-up todo (D-05), the folded todo closed"
affects: [REQUIREMENTS.md, ROADMAP.md, STATE.md, SANDBOX-PATHS.md, docker-compose.e2e.yml, .env.example]

requirements-completed: [CONF-01, CONF-02, CONF-04, CONF-05, CONF-07]

actuals:
  tokens: 5725
  tasks: 3
  commits: 4
plan_head_before: 0f18bbd066edf6a372135ecfb8ab4ba448dd3aa8
plan_head_after: bf623804440c53259c58cd537d8b0e0ba05c51c9

tech-stack:
  added: []
  patterns:
    - "An e2e fault-injection shim inside a confined engine is adapted to the ruleset (mount point under a granted directory, listed binaries only, no /dev/shm-style side doors), never the ruleset to the shim"
    - "A class-closure inversion adds a line instead of replacing one, so the mutant compiles and the failing output is the guard's own"

key-files:
  created:
    - .planning/todos/pending/2026-09-29-amd64-first-deployment-preflight-check.md
    - .planning/phases/70-document-on-amd64/evidence/liveproof-8class-70-05.txt
    - .planning/phases/70-document-on-amd64/evidence/e2e-70-05.txt
    - .planning/phases/70-document-on-amd64/evidence/inversions-70-05.txt
  modified:
    - docker-compose.e2e.yml
    - internal/e2e/testdata/verapdf-shim/soffice
    - .env.example
    - .planning/research/SANDBOX-PATHS.md
    - .planning/REQUIREMENTS.md
    - .planning/ROADMAP.md
    - .planning/STATE.md
    - .planning/phases/70-document-on-amd64/deferred-items.md
    - .planning/WINDOWS.md
  moved:
    - ".planning/todos/pending/2026-09-06-other-engines-still-unconfined.md -> .planning/todos/completed/"

key-decisions:
  - "The shim's trigger detection and copy use sed, not grep and cp: sofficeReadOnly lists /usr/bin/sed and does not list grep or cp, and the production grant was not widened. No redirect to /dev/null either: soffice's set grants /dev/null read-only"
  - "The fixture mounts at /usr/lib/octoconv-e2e/..., beneath /usr/lib, which the measured soffice set grants for LibreOffice's program tree"
  - "The 70-04 comment in .env.example is reworded, not the test: the word 'measured' in DOCUMENT_WORKER_CONCURRENCY's comment made TestEstimateSourceMatchesEnvExampleProvenance read DOCUMENT_ENGINE_TIMEOUT (not measured) as measured"
  - "The html large-document check is NOT fixed here: it fails identically on 705f962 with today's base image, so it is html's and pre-dates this phase; recorded in deferred-items.md and WINDOWS.md"

status: complete
gate_result: "FAILURES PRESENT (40 checks run) -- 39 PASS, 1 FAIL (html large-document-without-dev-shm), pre-existing"
duration: ~55 min
completed: 2026-09-30
---

# Phase 70 Plan 05: Class held on rebuilt images, record brought in line Summary

**Real document jobs (plain conversions, the PDF/A export and the veraPDF terminal-fail gate) pass through the confined worker with the e2e shim adapted to soffice's ruleset; the five class-closure guards fail when inverted; the eight-class cross-check matches; the eight-class live gate passes 39 of 40 checks and its one failure (an html negative control) reproduces on the phase 69 tip, so it is recorded, not fixed here.**

## Open issue (read first): the gate's last line is not `ALL PASS`

`scripts/sandbox-liveproof.sh image pandoc ocr av audio html probe document` ended:

```
FAILURES PRESENT (40 checks run)
```

40 is the expected total (33 for the seven classes phase 69 left, plus document's 7 on the read-write `/tmp` route, 70-04). 39 checks PASS, including every document check: `confined-startup`, `refuses-without-landlock`, `escape-hatch`, `preflight-catches-restrict-self`, `emulated-amd64-refuses`, `concurrency-guard`, `sweeps-shared-tmp`.

The one FAIL is `html large-document-without-dev-shm`: `TestChromiumLargeDocumentNeedsNoDevShmLive` fails at its negative control ("engine exited 0 without --disable-dev-shm-usage -- the method cannot see a /dev/shm dependency"). The production render in the same test passes.

Why I did not fix it, and why I believe it is not phase 70's:

- It reproduces identically on a `git worktree` of `705f962` (the phase 69 tip, before any phase 70 change), built and run today (`evidence/liveproof-8class-70-05.txt`, last section).
- `Dockerfile.chromium-worker` installs `chromium-headless-shell` from Debian unpinned. Phase 69 measured Chromium 153.0.8010.52 (69-RESEARCH.md); images built today carry 154.0.8037.57. I have not measured what 154 does instead (my guess: it falls back to a granted path when /dev/shm is denied). That is a guess and is labelled as one in deferred-items.md.
- The fix belongs to html (a control a fallback cannot defeat, or pinning the gate's Chromium). Editing chromium_live_test.go or the html ruleset to turn a gate green would be exactly the kind of change CONF-05's prohibition is about.

So plan truth 3 ("ends ALL PASS (39 checks) or (40 checks)") is **not met as written**. It is recorded in `deferred-items.md`, in `.planning/WINDOWS.md` (kind `unmet-truth`, open, which blocks `/gsd-ship` until waived or fixed), and is the thing Task 2's human-check should look at first.

## Task 1 (tracer): document jobs through the confined worker on a rebuilt stack

Commit `6a8e69e` touches only `docker-compose.e2e.yml` and `internal/e2e/testdata/verapdf-shim/soffice` (`git show --name-only --format= 6a8e69e`; no file under internal/convert/, not SANDBOX-PATHS.md).

- **The finding held.** The worker resolves `soffice` to the e2e shim (PATH has /usr/local/bin first), so once document is confined the shim runs inside soffice's ruleset. The original shim read its fixture from `/e2e-fixtures` (outside every grant) and copied with `cp` and `grep` (not in the measured set).
- **Overlay:** the fixture mounts at `/usr/lib/octoconv-e2e/verapdf_noncompliant_marker.pdf` (beneath `/usr/lib`, in the `conf05:soffice:readonly` block, checked against `scripts/conf05-crosscheck.sh --doc soffice`). `DOCUMENT_SANDBOX_DISABLED` is never set (`yq '.services."document-worker".environment // {} | has("DOCUMENT_SANDBOX_DISABLED")'` prints `false`). Header comment updated.
- **Shim:** `sed -n "/MARKER/{=;q;}"` for detection, `${input##*/}` / `${base%.*}` for the stem, `sed -n p "$FIXTURE" > "$outdir/$stem.pdf"` for the copy. My first draft kept `2>/dev/null`; run under the real ruleset the shim failed to detect the trigger (`cannot create /dev/null: Permission denied`, soffice's set grants /dev/null read-only) and the real soffice converted trigger.docx instead. Removed, re-run.
- **Byte identity under the real ruleset** (image built from this tree, `sha256:8bfc7b4b...`, the same id as 70-04's native image):

  ```
  docker run --rm --cpus 2.0 --memory 1g -v <job>:/tmp/job -v shim:/usr/local/bin/soffice:ro \
    -v marker.pdf:/usr/lib/octoconv-e2e/verapdf_noncompliant_marker.pdf:ro -e TMPDIR=/tmp/job/tmp \
    --entrypoint sh <image> -c '/usr/local/bin/document-worker __sandbox-exec rw:/tmp/job rw:/tmp sock:/tmp \
      ro:/usr/lib ro:/usr/bin/dash ro:/usr/bin/basename ro:/usr/bin/dirname ro:/usr/bin/ls ro:/usr/bin/sed \
      ro:/etc/libreoffice ro:/usr/share/fonts ro:/dev/urandom ro:/usr/local/bin/soffice -- \
      /usr/local/bin/soffice --headless ... --convert-to pdf:writer_pdf_Export --outdir /tmp/job /tmp/job/in.docx'
  exit=0   cmp in.pdf internal/e2e/testdata/verapdf_noncompliant_marker.pdf -> BYTE-IDENTICAL (three runs, two fresh job dirs)
  sha256 30745d32...cc873 on both
  ```

  One earlier attempt, immediately after `rm -rf` of the host job directory, printed `cannot create /tmp/job/in.pdf: Directory nonexistent`; the same command on the same and on a fresh directory passed three times afterwards. I attribute it to the host bind mount of a just-recreated directory, not to the ruleset, but did not isolate it (the e2e stack uses the container's own /tmp, not a host mount).
- **Plain stack** (`docker compose up -d --build`, no overlay, commit `0f18bbd` + the two files): `/healthz` ok; document-worker log:
  ```
  📄 DOCUMENT_WORKER_CONCURRENCY=1
  🔒 document sandbox: landlock via /usr/local/bin/document-worker __sandbox-exec
  🔒 document sandbox proven: soffice and veraPDF ran inside the job directory and were denied a file outside it
  🐙 document-worker starting (queue=document)
  ```
  no "the engines can read every file" line. `docker compose down` (without -v), no octoconv container left, ports 8090/9100/5434 free. (Preconditions checked: `DIVERGENCES: 0` is the last line of the differential evidence; the only compose service with a `platform:` key is asynqmon; no `.env` override was read, compose renders `DOCUMENT_SANDBOX_DISABLED: ""`.)
- **ci-local e2e tier:** `ci-local 20260930T161430Z ... commit 6a8e69e ... ### tier e2e: PASS ... ALL REQUESTED TIERS PASSED` (transcript `.planning/ci-local/ci-local-20260930T161430Z.log`; it is not verbose, so I re-ran the suite once more with `-v` on a freshly built overlay stack and kept that transcript, `evidence/e2e-70-05.txt`, `ok ... internal/e2e 113.463s`, document-worker again printing `🔒 document sandbox proven`):
  ```
  --- PASS: TestDocumentConversionE2E (12.46s)   (sample.docx .xlsx .pptx .odt .ods .odp, all PASS)
  --- PASS: TestPDFAExportE2E (6.08s)   --- PASS: .../PDFA (4.03s)   --- PASS: .../NoOptsRegression (2.04s)
  --- PASS: TestPDFANonCompliantE2E (2.03s)
  ```
  No `--- FAIL`, no `--- SKIP` anywhere in the verbose run (52 PASS lines).

## Task 2: the class held

Commit: none for code (every file edited was reverted; `git diff --quiet -- internal/ cmd/ scripts/` held after each and at the end). Evidence commit `5e5e6fd`.

**Five inversions** (`evidence/inversions-70-05.txt`), each run, shown failing for the stated reason, reverted:

| Inversion | Result |
|---|---|
| literal `runCommand(ctx, "soffice", ...)` added in libreoffice.go | `TestConfinedClassesExecOnlyThroughTheSandbox` FAIL: `libreoffice.go:146: libreoffice.go calls runCommand directly` |
| literal `runCommand(vctx, "verapdf", ...)` added in verapdf.go | same test FAIL: `verapdf.go:187: verapdf.go calls runCommand directly` |
| sentinel check removed from `isDocumentTerminal` | `TestEngineSandboxUnavailableIsRetryableForEveryConfinedClass/document` FAIL (classified the sandbox fault terminal) |
| `/tmp/x` added to `sofficeReadOnly` | `TestSofficeReadOnlySetIsTheMeasuredOne` FAIL; `scripts/conf05-crosscheck.sh soffice` prints `MISMATCH: soffice` with `> /tmp/x`, exit 1 |
| `LANDLOCK_ACCESS_FS_MAKE_SOCK` added to `accessReadWrite` (scoped-socket route) | `TestNothingMayCreateSymlinksOrSpecialFiles` FAIL (`creating a socket is GRANTED`), and `TestSocketGrantIsSocketOnly` FAIL; both pass on the baseline and after the revert. The linux-only tests ran in `golang:1.26-bookworm` with `GOTOOLCHAIN=go1.26.4` (the host is darwin, as in 70-02) |

No harness safety check refused an inversion; none was skipped. The edge truth (line-exact comparison) was shown directly with two more temporary edits: a trailing slash on `/usr/share/fonts` and two swapped neighbours both print `MISMATCH`.

**Eight-class cross-check**, `scripts/conf05-crosscheck.sh pandoc ocr av audio html probe document` (exit 0):
```
MATCH: pandoc readonly (0) engines (1)
MATCH: ocr readonly (7) engines (4)
MATCH: av readonly (7) engines (2)
MATCH: audio readonly (7) engines (3)
MATCH: html readonly (15) engines (1) readwrite (2)
MATCH: probe readonly (10) engines (4) readwrite (0)
MATCH: soffice readonly (9) engines (1) readwrite (1) socket (1)
MATCH: verapdf readonly (9) engines (1) readwrite (1)
```
`grep -cxF 'var sandboxPendingFiles = map[string]string{}' internal/convert/sandboxcoverage_test.go` prints 1.

**Local gate:** first run `ci-local 20260930T163130Z ... commit 6a8e69e`: `### tier gate: FAIL`, `### tier helm: PASS`. The gate failure was real and caused by this phase (next section). After the fix: `ci-local 20260930T163356Z ... commit 18c805d ... ### tier gate: PASS ... ### tier helm: PASS ... ALL REQUESTED TIERS PASSED` (gofmt clean, vet clean, `go test -count=1 ./...` on go1.26.4, helm lint/template checks with no cluster; one recorded warning, local helm v4.3.0 vs v4.2.3 pinned in ci.yml).

**Live gate**, commit `18c805d`, go1.26.4, images freshly built (ids in the evidence file: image `8989e757...`, pandoc `19f10e76...`, ocr `3d7640d2...`, av `f69cc0e1...`, audio `deb590c0...`, html `49d0c077...`, probe `3b8b6962...`, document `8bfc7b4b...`, emulated amd64 document `28363c16...`): final line `FAILURES PRESENT (40 checks run)`, see the Open issue. The emulated amd64 image refused: `document sandbox: landlock is unavailable on this kernel; ... Set DOCUMENT_SANDBOX_DISABLED=true to accept that deliberately`, no `🔒`, no `postgres:`.

## Task 3: the record

Commit `bf62380`. SANDBOX-PATHS.md "Что уже внедрено": document is `да (фаза 70)`, and the paragraph says `sandboxPendingFiles` is empty, document runs two rulesets per task (D-10) and the shared part gained the socket-only `sock:` grant. REQUIREMENTS.md CONF-07 and ROADMAP.md's Phase 70 goal and criterion 1 say what happened and keep the emulation reason; the ROADMAP diff has two hunks, both inside the Phase 70 section (`git diff -U0`), nothing else touched, no checkbox or progress row changed (the orchestrator owns those; CONF-xx checkboxes untouched). `.planning/todos/pending/` holds exactly one file, `2026-09-29-amd64-first-deployment-preflight-check.md`; the folded todo was `git mv`'d to `completed/` with a closing section naming it; STATE.md's Pending Todos names the new todo and lists the closed one as "сделано, а не передумано". Task 3's automated verify prints OK; `grep -n CONF-07` shows the `эмул` line.

## Deviations from Plan

**1. [Rule 1 - Bug] `.env.example` comment made DOCUMENT_ENGINE_TIMEOUT read as measured**
- **Found during:** Task 2, `ci-local gate` (`--- FAIL: TestEstimateSourceMatchesEnvExampleProvenance`: engine document, `.env.example measured=true, engineEstimateSource says measured=false`).
- **Issue:** 70-04 added "(measured, 70-02)" to the `DOCUMENT_WORKER_CONCURRENCY` line, in the same paragraph as `DOCUMENT_ENGINE_TIMEOUT`; the test reads "measur" anywhere in the paragraph. 70-04's own verification ran only `./internal/queue/... ./cmd/...`, so it passed there.
- **Fix:** reworded to "(SANDBOX-PATHS.md, 70-02)". The timeout's provenance (not measured) stays as it is.
- **Files:** `.env.example`. **Commit:** `18c805d`. Verified: `go test ./internal/api/` ok, then the full gate tier PASS.

**2. [Scope boundary] html large-document negative control left failing**, see Open issue. Not fixed, recorded in deferred-items.md and WINDOWS.md.

**3. [Process] Task 1 committed before its e2e run**, so the ci-local transcript names the commit it ran on (an uncommitted tree prints "TRACKED CHANGES NOT IN THIS COMMIT"). Nothing needed fixing afterwards, so no follow-up commit changed what ran.

**4. [Process] Verbose e2e re-run** because the ci-local tier's `go test` is not `-v` and the plan's fails_when requires the PASS lines.

**Total deviations:** 1 auto-fixed bug, 1 deferred pre-existing failure, 2 process notes. **Impact:** plan truth 3 is unmet as written; everything else is met.

## Environment notes

minio/minio and minio/mc were the local chainguard aliases (70-01 deferred item); no tracked file changed for it, and it did not affect the e2e runs. Kubernetes stayed off; helm checks needed no cluster. Docker was not used concurrently (compose stack, e2e and gate ran one at a time). Scratch images removed, compose down after every run, no octoconv container left; the phase 69 worktree used for the baseline was removed. `git diff --exit-code 5560b2a -- internal/convert/exec.go` exits 0 (D-13). No Co-Authored-By trailer or agent attribution in any commit.

## Stubs and threat flags

Known stubs: none. Threat flags: none beyond the plan's register (T-70-18 to T-70-22: stack and images rebuilt from this tree with `-count=1`, pinned toolchain, run ids and commits in the transcripts; `sandboxPendingFiles` empty and literal-exec inversions fail; eight-class line-exact cross-check; MAKE_SOCK inversion fails the untouched invariant test; the overlay never disables the sandbox and Task 1's commit touches no grant).

## Self-Check: PASSED

- Files exist: the three evidence files, `.planning/todos/pending/2026-09-29-amd64-first-deployment-preflight-check.md`, `.planning/todos/completed/2026-09-06-other-engines-still-unconfined.md`; `.planning/todos/pending/2026-09-06-other-engines-still-unconfined.md` absent.
- Commits `6a8e69e`, `18c805d`, `5e5e6fd`, `bf62380` exist; `git rev-list --count 0f18bbd..HEAD` measured 4 before this SUMMARY commit.
- `git diff --quiet -- internal/ cmd/ scripts/` holds; `git diff --exit-code 5560b2a -- internal/convert/exec.go` exits 0.
- The self-check passes on the claims above; it does NOT pass the plan's gate line, which is stated as unmet, not glossed.

---
*Phase: 70-document-on-amd64*
*Completed: 2026-09-30*
