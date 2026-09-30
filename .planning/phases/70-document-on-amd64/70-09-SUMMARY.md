---
phase: 70-document-on-amd64
plan: 09
subsystem: infra
tags: [landlock, sandbox, e2e, liveproof, conf05, gap-closure, todo]

requires:
  - phase: 70-document-on-amd64
    provides: "html control rebuilt on a measurement (70-06), shared-/tmp concurrency guard and review Info items (70-07), veraPDF /proc grant narrowed (70-08)"
provides:
  - "e2e through the confined document worker on a stack rebuilt from 033978c: ci-local tier PASS plus a verbose run with the three PDF/A-path tests passing"
  - "the eight-class gate green: ALL PASS (40 checks) on images built from 5db6c74"
  - "eight line-exact MATCH lines; the /tmp/x inversion on the re-pinned veraPDF set fails the pinned test and the cross-check"
  - "WINDOWS.md entry 2 fixed, open_count 0"
  - "three pending todos (user decisions 4 and 5, plus the 70-07 host-/tmp finding); STATE.md's Pending Todos equal to the todo directory"
affects: [STATE.md, WINDOWS.md, deferred-items.md, 70-VERIFICATION.md re-run]

requirements-completed: [CONF-01, CONF-02, CONF-04, CONF-05, CONF-07]

actuals:
  tokens: 9000
  tasks: 3
  commits: 3
plan_head_before: 033978c1b86ad2760633a2cb80883a648917d49b
plan_head_after: 529c9c8397d4bccbd9e0e381a081711733e6da87

tech-stack:
  added: []
  patterns:
    - "Acceptance evidence is taken on a stack and images built from a recorded clean commit, with -count=1 and go1.26.4; the tier run and the verbose re-run both name their commit"

key-files:
  created:
    - .planning/phases/70-document-on-amd64/evidence/e2e-70-09.txt
    - .planning/phases/70-document-on-amd64/evidence/liveproof-8class-70-09.txt
    - .planning/phases/70-document-on-amd64/evidence/inversions-70-09.txt
    - .planning/todos/pending/2026-09-30-soffice-socket-grant-remove-file-alternative.md
    - .planning/todos/pending/2026-09-30-minio-images-off-docker-hub.md
    - .planning/todos/pending/2026-09-30-sweep-tests-must-not-touch-host-tmp.md
  modified:
    - .planning/WINDOWS.md
    - .planning/phases/70-document-on-amd64/deferred-items.md
    - .planning/STATE.md

key-decisions:
  - "The 70-07 finding (a test that builds ConfinedDocument/ConfinedChromium and reaches Convert sweeps the host's real /tmp) is its own todo, not a note inside the soffice todo: html keeps rw:/tmp whatever happens to soffice, so the hazard outlives the narrower grant"

status: complete
gate_result: "ALL PASS (40 checks)"
duration: ~55 min
completed: 2026-09-30
---

# Phase 70 Plan 09: Final e2e, eight-class gate, todos Summary

**On a stack and images built from the commit that carries every gap fix, real document jobs pass through the confined worker, the eight-class gate's last line is `ALL PASS (40 checks)`, all eight pinned sets match SANDBOX-PATHS.md, and the red-gate entry in the defect register is closed.**

## Eight-class gate, final line (exact)

```
ALL PASS (40 checks)
```

`evidence/liveproof-8class-70-09.txt`: 130 lines, `grep -c '^PASS: '` prints 40, no `FAIL:` and no `NOT RUN:` line, every class's build block names `commit: 5db6c74da4794eee51a10baa59c90fb68d10ffaa` (equal to `git rev-parse HEAD` at the run; `git diff --quiet -- internal/ cmd/ scripts/` held) and its own image id:

| class | image id |
|---|---|
| image | `sha256:8b177f279eae...` |
| pandoc | `sha256:861aa1f47d88...` |
| ocr | `sha256:e9216e329f84...` |
| av | `sha256:3d4963f9f619...` |
| audio | `sha256:04f7fe26f328...` |
| html | `sha256:4a0f1f966cf0...` |
| probe | `sha256:f767a8c9490c...` |
| document | `sha256:6a165ed0d9c5...` |
| document, emulated amd64 | `sha256:2ea092f37f26...` |

Points the human-check asked about:

- **The emulated amd64 document image refused** (`PASS: document emulated-amd64-refuses`): `document sandbox: landlock is unavailable on this kernel; ... Set DOCUMENT_SANDBOX_DISABLED=true to accept that deliberately`.
- **html's large-document block is the rebuilt control** (`PASS: html large-document-without-dev-shm`): `chromium: Chromium 154.0.8037.57`, `dev-shm-observe: control=1 production=0`, `large-doc: fixture=84543422 bytes pdf=20460395 bytes wall=5.09581053s`. This is the 70-06 control: an inotify observation with /dev/shm reachable, where a dash child proves the observation can see an entry (control=1) and the production render creates none (production=0).
- The build blocks print `tracked dirt: 3 file(s)`. Those are the three untracked pre-existing entries (`.gsd/`, `.vscode/`, `octoconv.code-workspace`); `git status --porcelain --untracked-files=no` prints nothing.
- The script's images are uniquely tagged and removed by the script itself; none is left (`docker images | grep liveproof` prints nothing).

## Task 1 (tracer): real document jobs through the confined worker

- **ci-local tier:** `ci-local 20260930T205313Z ... commit 033978c ... ### tier e2e: PASS ... ALL REQUESTED TIERS PASSED` (transcript `.planning/ci-local/ci-local-20260930T205313Z.log`; `ok ... internal/e2e 107.988s`). `033978c` was `git rev-parse HEAD` at the run. The next commit (`5db6c74`) adds only `evidence/e2e-70-09.txt` (`git diff --stat 033978c 5db6c74`: one file), so the gate's commit and the e2e's commit carry identical code.
- **Preconditions checked:** 70-06..70-08 committed; `git status --porcelain` over internal, cmd, scripts, deploy, Dockerfile.*, compose files, go.mod, go.sum printed nothing; docker native; `minio/minio:latest` and `minio/mc:latest` present (local chainguard aliases); ports 8090, 9100, 5434 free (the `dockerdev-*` stack was running and was not touched; it uses 3333, 5432, 9000-9001, etc.); `docker compose config` showed no `*_SANDBOX_DISABLED` set and every `*_WORKER_CONCURRENCY` at its default (I did not read `.env`; a hook protects it); Kubernetes not needed.
- **Overlay:** `yq '.services."document-worker".environment // {} | has("DOCUMENT_SANDBOX_DISABLED")'` prints `false`.
- **Verbose re-run** (`go test -v -count=1 ./internal/e2e/...`, freshly built overlay stack), `evidence/e2e-70-09.txt`: 52 `--- PASS` lines, `count of failing or skipped tests: 0` (`grep -c -- '--- FAIL\|--- SKIP'` on the file prints 0), `ok ... internal/e2e 112.905s`:
  ```
  --- PASS: TestDocumentConversionE2E (14.18s)
  --- PASS: TestPDFAExportE2E (6.08s)   --- PASS: .../PDFA (4.02s)   --- PASS: .../NoOptsRegression (2.06s)
  --- PASS: TestPDFANonCompliantE2E (2.05s)
  ```
  document-worker startup lines: `📄 DOCUMENT_WORKER_CONCURRENCY=1`, `🔒 document sandbox: landlock via /usr/local/bin/document-worker __sandbox-exec`, `🔒 document sandbox proven: soffice and veraPDF ran inside the job directory and were denied a file outside it`. The PDF/A path runs veraPDF under the set 70-08 left.
- Teardown: `docker compose down` without -v on the verbose run (the ci-local tier's own teardown uses `-v`); `docker ps | grep -c octoconv` prints 0, ports free.
- Two false starts in my own harness, no effect on the evidence: the first verbose-run script wrote to an unset `$SP` (read-only root, nothing ran under `go test`; its stack was torn down by the trap) and was repeated; the `e2e-70-09.txt` header line initially contained the string `--- FAIL/SKIP`, which the acceptance grep would have counted, and was reworded.

## Task 2: cross-check, inversion, local gate, the gate, the window

- **Inversion on the set 70-08 left** (`evidence/inversions-70-09.txt`): `/tmp/x` added to `verapdfReadOnly` ->
  ```
  --- FAIL: TestVerapdfReadOnlySetIsTheMeasuredOne (0.00s)
      documentsandbox_test.go:59: verapdfReadOnly = [... "/dev/random" "/tmp/x"], this test knows [... "/dev/random"] -- re-measure with scripts/document-sandbox-measure.sh and update SANDBOX-PATHS.md rather than editing the expectation
  MISMATCH: verapdf
  readonly diff (doc vs go, - doc / + go):
  11a12
  > /tmp/x
  ```
  (cross-check exit 1). Reverted with `git checkout -- internal/convert/documentsandbox.go`; `git diff --quiet -- internal/ cmd/ scripts/` holds.
- **Eight MATCH lines** (`scripts/conf05-crosscheck.sh pandoc ocr av audio html probe document`, exit 0):
  ```
  MATCH: pandoc readonly (0) engines (1)
  MATCH: ocr readonly (7) engines (4)
  MATCH: av readonly (7) engines (2)
  MATCH: audio readonly (7) engines (3)
  MATCH: html readonly (15) engines (1) readwrite (2)
  MATCH: probe readonly (10) engines (4) readwrite (0)
  MATCH: soffice readonly (9) engines (1) readwrite (1) socket (1)
  MATCH: verapdf readonly (11) engines (1) readwrite (1)
  ```
  veraPDF's read-only count is 11 (70-05: 9) — the 70-08 narrowing of `/proc` to three named entries.
- **Local gate:** `ci-local 20260930T213012Z ... commit 5db6c74 ... ### tier gate: PASS ... ### tier helm: PASS ... ALL REQUESTED TIERS PASSED`, go1.26.4, no cluster needed.
- **D-13:** `git diff --exit-code 7744ddf -- internal/convert/exec.go` exits 0.
- **Window closed only after the final line:** `gsd-tools windows fixed 2` ok; `grep -m1 '^open_count:' .planning/WINDOWS.md` prints `open_count: 0` (`fixed_count: 2`). `70-VERIFICATION.md` not edited.

## Task 3: the record

- Three todos under `.planning/todos/pending/` (Russian, the register of the amd64 preflight todo): `2026-09-30-soffice-socket-grant-remove-file-alternative.md` (user decision 4: `sock:` with MAKE_SOCK plus REMOVE_FILE, not measured, not built; keeps rw:/tmp with concurrency 1, the guard and the sweep), `2026-09-30-minio-images-off-docker-hub.md` (user decision 5: quick task after the milestone, cgr.dev/chainguard/minio; says why it is a todo and not only a deferred-items line; `grep -c 'deferred-items'` prints 5), `2026-09-30-sweep-tests-must-not-touch-host-tmp.md` (the 70-07 finding, per the user's request to record it).
- `deferred-items.md` (English, its own register): the html item now says resolved, with the 70-06 and 70-09 evidence paths; the MinIO item points at the new todo. Original text kept.
- `STATE.md` "### Pending Todos": the count sentence says five and one entry per file; `ls .planning/todos/pending/*.md | wc -l` prints 5, every file's basename is named in STATE.md. `git diff` of STATE.md is confined to that block (13 insertions, 1 deletion); the trail of removed items below it is untouched. The block previously listed one item and had not picked up the `/proc` todo that 70-08 created; it does now.
- `git diff --quiet b5e0f3f -- docker-compose.yml deploy/` holds: no compose or chart change for MinIO.

## Deviations from Plan

**1. [User instruction, plan extension] A third todo file**
- The plan specifies two todos; the task brief added "a todo (or note in an existing one)" for the 70-07 host-/tmp finding. I made a third file rather than a note, for the reason in key-decisions. STATE.md names all five pending files as the plan requires.

**2. [Process] `evidence/inversions-70-09.txt` is an extra evidence file** (not in `files_modified`): it keeps the inversion's failing output, the cross-check and the gate/helm tier lines next to the gate transcript, as 70-05 did.

**3. [Process] Tasks 1 and 2's evidence commits were made after the runs**, so e2e names `033978c` and the gate/ci-local gate name `5db6c74`; code between them is identical (only an evidence file added).

**Total deviations:** 0 auto-fixed bugs, 1 plan extension at the user's instruction, 2 process notes. No code file changed in this plan (`git diff 033978c HEAD -- internal cmd scripts deploy` is empty).

## Open item for the reader: the human check

Task 2's `<human-check>` (the user approves the live evidence, as for 70-05) is **not self-approved**. Read, in this order: `evidence/liveproof-8class-70-09.txt` (commits and image ids, the emulated refusal, html's block with `dev-shm-observe: control=1 production=0`), `evidence/e2e-70-09.txt`, `evidence/inversions-70-09.txt`, and the 70-06 evidence it rests on: `evidence/chromium-devshm-measure-70-06.txt`, `evidence/liveproof-html-70-06.txt`. The re-verification should also read `evidence/liveproof-8class-70-05.txt` (the red transcript this replaces), `evidence/e2e-70-05.txt` and the 70-07/70-08 evidence files.

## Environment notes

OrbStack docker (arm64), Kubernetes untouched (helm tier needed no cluster), go1.26.4 on every go command. The `dockerdev-*` containers were never stopped, removed or pruned; no port clash occurred (octoconv 8090/9100/5434 free before and after). Docker jobs ran one at a time. After the runs: no octoconv container, no liveproof image, the compose stack down. `.planning/ci-local/` transcripts are gitignored and left in place. No Co-Authored-By trailer or agent attribution in any commit. ROADMAP.md and REQUIREMENTS.md not touched (the orchestrator owns those).

## Stubs and threat flags

Known stubs: none. Threat flags: none beyond the register (T-70-35: stack and images rebuilt from the recorded commits, -count=1, go1.26.4, commits and image ids in the transcripts, window closed only after `ALL PASS (40 checks)`; T-70-36: eight MATCH lines and the `/tmp/x` inversion; T-70-37: the overlay never sets `DOCUMENT_SANDBOX_DISABLED`).

## Self-Check: PASSED

- Files exist: the three evidence files, the three new todos, `.planning/phases/70-document-on-amd64/deferred-items.md`; `.planning/todos/pending/` holds five files, all named in STATE.md.
- Commits `5db6c74`, `01bb3e5`, `529c9c8` exist; `git rev-list --count 033978c..HEAD` measured 3 before this SUMMARY commit.
- `grep -c '^PASS: '` on the gate transcript prints 40 and its last line is `ALL PASS (40 checks)`; `git diff --exit-code 7744ddf -- internal/convert/exec.go` exits 0; `git diff --quiet -- internal/ cmd/ scripts/` holds; `open_count: 0`.

---
*Phase: 70-document-on-amd64*
*Completed: 2026-09-30*
