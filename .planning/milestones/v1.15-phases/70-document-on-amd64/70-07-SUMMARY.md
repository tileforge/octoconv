---
phase: 70-document-on-amd64
plan: 07
subsystem: infra
tags: [sandbox, landlock, asynq, concurrency, preflight, sigterm, verapdf, gap-closure]
status: complete

requires:
  - phase: 70-document-on-amd64
    provides: "sharedTmpGuard, ConfinedDocument and ConfinedChromium (70-02, 70-03), the startup preflights, 70-VERIFICATION.md gap 2 and 70-REVIEW.md WR-01, IN-01..IN-03"
provides:
  - "sharedTmpGuard refuses any concurrency other than exactly 1 when the sandbox confines and the writable set grants the shared /tmp (asynq runs NumCPU tasks for a value below 1), in the one shared guard, so document and html both get it"
  - "Live refusals for 0 and -1 on the document and chromium images built from this tree (evidence/startup-guards-70-07.txt, part 1)"
  - "Every worker's startup preflight derives from the signal context; measured on the document image: exit 0.31-0.33 s after SIGTERM, was 3.0-3.75 s"
  - "A whitespace job directory / TMPDIR is refused with TMPDIR named: per PDF/A job before any engine, and at startup by the preflight in 0.6 s (was a 60 s soffice timeout with no cause)"
  - "verapdf-differential.sh: rest is local in nonvacuity; the EXIT-trap corpus removal logs instead of failing"
affects: [70-09, scripts/sandbox-liveproof.sh]

requirements-completed: [CONF-01, CONF-04]

actuals:
  tokens: 8700
  tasks: 3
  commits: 8
plan_head_before: c9af933cef17aadc6bb2495417887613c6c6a4af
plan_head_after: 9ee04291b442d4df25cdcecb60013a1e561401a1

tech-stack:
  added: []
  patterns:
    - "A guard that protects a shared resource states the one safe value (exactly 1), not a bound: 'at most 1' let through the values asynq reinterprets"
    - "A startup check on an environment-derived path runs before any engine, so the operator reads the cause instead of an engine timeout"

key-files:
  created:
    - .planning/phases/70-document-on-amd64/evidence/startup-guards-70-07.txt
  modified:
    - internal/convert/sandboxwrap.go
    - internal/convert/sandboxexec_test.go
    - internal/convert/documentsandbox.go
    - internal/convert/documentsandbox_test.go
    - internal/convert/htmlsandbox_test.go
    - internal/convert/libreoffice.go
    - internal/convert/libreoffice_test.go
    - cmd/document-worker/main.go
    - cmd/chromium-worker/main.go
    - cmd/worker/main.go
    - cmd/pandoc-worker/main.go
    - cmd/ocr-worker/main.go
    - cmd/av-worker/main.go
    - cmd/audio-worker/main.go
    - cmd/probe-worker/main.go
    - cmd/tectonic-worker/main.go
    - scripts/verapdf-differential.sh

key-decisions:
  - "Refused, never clamped: a configured concurrency below 1 while confined with the shared /tmp is a startup refusal (prohibition CONF-01 honoured); the above-1 error text is unchanged so the gate's concurrency-guard checks still match"
  - "The whitespace-TMPDIR check also lives in DocumentPreflight, for every confined start, not only the PDF/A path: measured live, the PDF/A step is never reached because soffice hangs first"

patterns-established:
  - "A test that builds a confined converter must not use ConfinedDocument/ConfinedChromium if it can reach Convert: they wire the sweep to the real /tmp. Build the converter directly and point sharedTmp at t.TempDir()"

requirements_completed: [CONF-01, CONF-04]
---

# Phase 70 Plan 07: Shared-/tmp concurrency guard and the review's Info items Summary

**`sharedTmpGuard` now refuses every concurrency other than exactly 1 for a confined class whose ruleset grants the shared /tmp, refusals proven live on both built images; preflights end on SIGTERM in all eight workers with a preflight; a whitespace TMPDIR is refused with its cause named.**

## Performance

- **Tasks:** 3 of 3
- **Commits:** 8 measured from the ledger (`c9af933..9ee0429`): 3 RED test commits, 4 fix commits, 1 evidence commit. The SUMMARY commit follows.
- **Files:** 17 modified, 1 created; `internal/convert/exec.go` byte-for-byte unchanged against 7744ddf; `go.mod`/`go.sum` unchanged.

## Accomplishments

### Task 1: gap 2 (WR-01)

RED (commit 3db710a) against the old guard, `go test -v -run 'TestSharedTmpGuard|TestConfinedDocument|TestConfinedChromium'`:

```
documentsandbox_test.go:151: ConfinedDocument(helper, 0, ...) err = <nil>, want it to name DOCUMENT_WORKER_CONCURRENCY=0 and the shared /tmp
documentsandbox_test.go:151: ConfinedDocument(helper, -1, ...) err = <nil>, ...
htmlsandbox_test.go:142: ConfinedChromium(helper, 0, ...) err = <nil>, want it to name HTML_WORKER_CONCURRENCY=0 and the shared /tmp
htmlsandbox_test.go:142: ConfinedChromium(helper, -1, ...) err = <nil>, ...
sandboxexec_test.go:510: expected an error   (x2)
--- FAIL: TestConfinedDocument  --- FAIL: TestConfinedChromium  --- FAIL: TestSharedTmpGuard
    --- FAIL: TestSharedTmpGuard/confined_shared_/tmp_concurrency_0
    --- FAIL: TestSharedTmpGuard/confined_shared_/tmp_concurrency_-1
```

GREEN (3be14e2): the guard returns nil for an unconfined sandbox, a writable set without /tmp, and exactly 1; below 1 returns `<ENV>=<n> with the sandbox enabled: asynq treats a concurrency below 1 as the CPU count, so several tasks would run over the container's shared /tmp ...; set <ENV>=1`; above 1 is unchanged. All three tests pass, including `TestSharedTmpGuard` rows for 0 and -1 (confined refuses; unconfined and no-/tmp rows stay nil) and ConfinedDocument/ConfinedChromium with a nil helper at -1, 0, 1, 2, 4 (no error, no sweep).

Live (evidence part 1, images built from 3be14e2): document and chromium, each at 0 and -1, confined, dead Postgres port. All four: exit 1, the refusal line naming `<VAR>=<n> with the sandbox enabled` and the shared /tmp, no `sandbox proven`, no `postgres:` line. The script asserts each of those and that the exit status is non-zero, so a run that never reached the worker cannot pass (see deviation 4).

### Task 2: IN-01, the class

Eight mains (`worker`, `pandoc`, `ocr`, `av`, `audio`, `probe`, `chromium`, `document`) now derive `preflightCtx` from the signal `ctx`; `grep -rn 'preflightCtx, cancelPreflight := context.WithTimeout(ctx,' cmd/ | wc -l` prints 8 and no `context.Background()` preflight remains. Tectonic's `preflightSandbox` and `preflightEngine` take `ctx` first and derive their timeouts from it; `preflightEngine` is passed main's signal ctx. The document and chromium guard comments say anything but 1 is refused.

Measured on the document image (SIGTERM 1 s after start, tini forwarding):

| | exit after signal | log |
|---|---|---|
| before (image from 3be14e2, cmd untouched) | 3.75 s, 3.02 s | preflight ran to completion after the signal, printed `sandbox proven`, then `postgres: ... context canceled` |
| after | 0.31 s, 0.33 s, 0.33 s | `preflight: ... verapdf killed: context canceled`, no `sandbox proven` |

Limit was 3 s.

### Task 3: IN-02 and IN-03

- RED (cdb9b7d), `TestConvertPDFARefusesWhitespaceJobDir`: the two refusal subtests failed (`produced no output file for "pdf": stat .../job with space/in.pdf`, and `soffice ran, want the refusal before any engine`) while its two controls passed, which shows the fake soffice on PATH is reached. GREEN (f16dab1): PDF/A with a space or a tab in the job directory fails before soffice, naming TMPDIR; the same call without `pdf_profile` and PDF/A in a clean directory reach the engine as before.
- Second RED (0cd098e), `TestDocumentPreflightRefusesWhitespaceTMPDIR`: failed with `libreoffice: locate engine soffice: ... want a refusal naming TMPDIR`. GREEN (9ee0429): `requireNoWhitespaceJobDir` is shared by Convert (PDF/A only) and DocumentPreflight (every confined start). See deviation 1 for why.
- Live (evidence part 3, image from 9ee0429): `TMPDIR="/tmp/a b"` refuses in 0.6 s with `the job directory ".../tmp" contains whitespace, and the veraPDF launcher word-splits JAVA_OPTS ...; point TMPDIR at a path without whitespace`; control `TMPDIR=/tmp/ab` proves the sandbox and fails only at the dead Postgres.
- IN-03: `rest` added to `nonvacuity`'s `local` line (grep count 1); the EXIT trap's `rm -rf "$CORPUS"` now logs `could not remove container-owned files under ...` instead of failing. Checked by running the same construct under `set -euo pipefail` over a directory the host user cannot remove: prints the message, exit 0. `bash -n` passes. The script itself was not run end to end (it builds two images and needs the amd64 emulation run; nothing in this plan changes what it measures).

## Task Commits

| Commit | Type | What |
|---|---|---|
| 3db710a | test | 0 and -1 rows and cases, RED |
| 3be14e2 | fix | `sharedTmpGuard` refuses below 1 |
| 78d678b | docs | live refusals, 4 runs |
| 73e7b79 | fix | preflight contexts from the signal context, tectonic helpers take ctx, SIGTERM timings |
| cdb9b7d | test | PDF/A whitespace job directory, RED |
| f16dab1 | fix | PDF/A whitespace refusal in Convert, IN-03 script fixes |
| 0cd098e | test | preflight whitespace TMPDIR, RED |
| 9ee0429 | fix | `requireNoWhitespaceJobDir` in DocumentPreflight, evidence part 3 |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] The startup preflight did not name the whitespace TMPDIR; it timed out for 60 s**
- **Found during:** Task 3, live check on the image built with only the Convert-level PDF/A check
- **Issue:** The plan's truth said the preflight "exercises the same PDF/A path", so a whitespace TMPDIR refuses the start with that cause. Measured: the preflight's first step is a plain soffice conversion (no PDF/A) in the same job directory; with `TMPDIR="/tmp/a b"` it ran to the 60 s preflight timeout (`soffice killed: context deadline exceeded`) and the PDF/A step was never reached. The worker refused to start, but with no cause and after a minute.
- **Fix:** Factored the check into `requireNoWhitespaceJobDir` (documentsandbox.go) and call it in `DocumentPreflight` right after the workspace exists, before any engine; Convert keeps it for PDF/A jobs only, as the plan's behaviour block requires (a job without `pdf_profile` is not rejected). Job directories come from `os.MkdirTemp("")`, so a whitespace TMPDIR can never reach a job on a worker that started.
- **Files modified:** internal/convert/documentsandbox.go, internal/convert/libreoffice.go, internal/convert/documentsandbox_test.go
- **Commits:** 0cd098e (RED), 9ee0429
- **Not established:** why soffice hangs in a whitespace directory (an unescaped `file://` profile URL is the obvious suspect; I did not isolate it). The comment on the helper states only what was measured.

**2. [Plan discrepancy] `preflightSandbox` in tectonic-worker has no caller**
- **Found during:** Task 2
- **Issue:** The plan says to pass the signal ctx "at both call sites"; only `preflightEngine` is called from main. `preflightSandbox` is dead code today.
- **Fix:** Gave both helpers the `ctx` parameter as planned and passed it at the one real call site. Did not wire `preflightSandbox` in and did not delete it: neither is in scope.

**3. [Rule 1 - Bug, in my own test] A RED test swept the host's real /tmp**
- **Found during:** Task 3, first RED run of the preflight whitespace test
- **Issue:** I built the converter with `ConfinedDocument`, which wires the sweep to the real `/tmp`; `DocumentPreflight` reached `Convert`, which runs `sweepSharedTmp("/tmp", ...)` on the macOS host. It stopped at its first entry (`/private/tmp/0618-6745-6b69-40e3`, root-owned, refused by the sticky bit) with `permission denied`. Checked immediately: `/private/tmp` still held its 45 entries, the session scratchpad included. Nothing was deleted, but only because that one entry sorts first and cannot be removed; the next run on a different `/tmp` could have emptied it.
- **Fix:** The test builds `LibreOfficeConverter{}.WithSandbox(helper)` directly and sets `sharedTmp = t.TempDir()`, and is commented as to why. Re-ran the RED and GREEN with the `/tmp` entry count unchanged (45) before and after. The committed test (0cd098e) has the safe form.

**4. [Rule 1 - Bug, in my own evidence run] The first live refusal run was vacuous**
- **Found during:** Task 1
- **Issue:** macOS has no `timeout`; all four `docker run`s printed `command not found`, and my "no sandbox proven / no postgres:" checks passed on empty output.
- **Fix:** Reran with a watchdog like the gate's `run_gated`, and made the script require a non-zero exit, the `<VAR>=<n> with the sandbox enabled` line and the shared /tmp, in addition to the two absences. The committed evidence is from the second run.

**5. [Wording] "shared /tmp" had to be contiguous**
- **Found during:** Task 1 GREEN
- **Issue:** My first below-1 message said "share the container's /tmp"; the tests and the plan require the phrase `shared /tmp`.
- **Fix:** Reworded to "run over the container's shared /tmp".

## Issues Encountered

- **One unexplained failure.** The first full `go test ./internal/convert/` after Task 1's GREEN failed without my capturing which test (a `grep` for FAIL lines came back empty on the next invocation, and the package then passed). It has passed on every later run: 3 times natively (59.0 s, 61.9 s, and the final 76.8 s), plus the targeted set on Linux in `golang:1.26-bookworm`. I cannot name the cause; the package takes about a minute and includes timing tests, so load from the two parallel image builds running at that moment is the likely candidate, but I did not prove it.
- The SIGTERM-mid-preflight error reads `the engines cannot convert under the sandbox: ... context canceled`. It is accurate about the cause in its tail but its prefix suggests a sandbox fault. Not changed (outside the plan); worth a one-line follow-up if an operator ever reads it.

## Known Stubs

None.

## Threat Flags

None. The new checks only refuse inputs earlier; no new endpoint, auth path or file access.

## Verification

- `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./... && go test -count=1 ./internal/convert/ ./internal/worker/ ./cmd/...`: all ok.
- `git diff --exit-code 7744ddf -- internal/convert/exec.go`: exit 0.
- `grep -c 'with the sandbox enabled' internal/convert/sandboxwrap.go`: 2.
- Task 1 automated verify: both variables at 0 and -1 present in the evidence file.
- Scratch images (`octoconv-document-worker:plan-70-07-fix*`, `octoconv-chromium-worker:plan-70-07-fix`) removed by exact tag; no octoconv container left; the eight `dockerdev-*` containers untouched. STATE.md and ROADMAP.md not touched.

## Self-Check: PASSED

- FOUND: internal/convert/sandboxwrap.go, documentsandbox.go, libreoffice.go, scripts/verapdf-differential.sh, evidence/startup-guards-70-07.txt
- FOUND commits: 3db710a, 3be14e2, 78d678b, 73e7b79, cdb9b7d, f16dab1, 0cd098e, 9ee0429
