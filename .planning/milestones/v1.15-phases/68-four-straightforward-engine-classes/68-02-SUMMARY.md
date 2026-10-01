---
phase: 68-four-straightforward-engine-classes
plan: 02
subsystem: security
tags: [landlock, sandbox, measurement, ocr, av, audio, pandoc, conf05]

requires:
  - phase: 68-four-straightforward-engine-classes
    provides: "68-01: engineExec/forJob/run/sandboxFault, runCommandAs, ErrEngineSandboxUnavailable, the preflight toolkit -- the shared mechanism this plan's proven sets will be wired into by plans 03-06"
provides:
  - "re-measured pandoc/ocr/av/audio sections in SANDBOX-PATHS.md: full job-path argv inventory (not the primary engine alone), sufficiency/necessity/effectiveness evidence judged by content, and mechanically extractable conf05:<class>:readonly / conf05:<class>:engines marker blocks"
  - "scripts/conf05-crosscheck.sh: doc-vs-Go cross-check plans 03-06 pin their <class>ReadOnly/<class>Engines Go vars against, with a --doc mode that works today"
  - "OCR's proven set closes the pdfinfo/pdftotext gap (both run on every PDF job, never measured before) and the /usr/share/fonts font-substitution trap: without it, pdftoppm renders a font-less page and tesseract returns empty text, both at exit 0"
  - "audio's proven set adds ffprobe (covering the pre-Convert duration guard) and moves the model grant from the /models directory to the resolved /models/ggml-base.bin file"
affects: [68-03-ocr-sandbox, 68-04-av-sandbox, 68-05-audio-sandbox, 68-06]

actuals:
  tokens: 9400
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "conf05:<class>:readonly / conf05:<class>:engines marker blocks in SANDBOX-PATHS.md: the doc's proven set in a form scripts/conf05-crosscheck.sh extracts mechanically -- a missing block is an error (exit 2), never an empty pass"
    - "necessity by removal, one path at a time, judged by content: a candidate path only earns a place in the final set if removing it changes an argv's actual output, not just its exit code"

key-files:
  created:
    - scripts/conf05-crosscheck.sh
  modified:
    - .planning/research/SANDBOX-PATHS.md
    - scripts/sandbox-paths-measure.sh

key-decisions:
  - "OCR's final read-only set keeps /usr/share/fonts (proven NECESSARY by removal -- without it pdftoppm silently renders a different, font-less PNG and tesseract then returns EMPTY text, both at exit 0, the exact ghostscript-shaped trap SANDBOX-PATHS.md's method section already warns about) but drops /etc/fonts, /usr/share/fontconfig/conf.avail and the empty /usr/local/share/fonts, all three proven UNNECESSARY by the same removal test"
  - "audio's model grant moves from the /models directory to the resolved /models/ggml-base.bin FILE, at run time -- not a static entry in audioReadOnly -- because AUDIO_MODEL_PATH is configurable and the path is not known at Go-variable-declaration time; /proc/filesystems and /proc/self/status, both newly observed in this measurement, are proven unnecessary by removal and dropped"
  - "audio's engine set adds ffprobe (/usr/bin/ffprobe, confirmed present, same apt package as ffmpeg) so a future confined form of the pre-Convert duration guard (EnforceMaxDuration, called from internal/worker before Convert) has a measured set to build against -- this plan measures only; whether/how to confine that call site is plan 05's decision, per 68-RESEARCH.md Pitfall 2"
  - "pandoc's and av's candidate sets are UNCHANGED from the 2026-09-20 measurement even under the fuller job-path argv inventory this plan drives (nine pandoc pairs, thirteen av argvs) -- re-measurement confirms no prior gap existed for either class, it does not just repeat the old answer"
  - "[Rule 1 - Bug] scripts/sandbox-paths-measure.sh's trace phase aborted (pipefail + grep exiting 1 on zero matches, under set -e) whenever an engine's candidate set was genuinely empty -- found live on pandoc's first trace of this plan, before any content was written; fixed with a trailing `|| true` on the whole path-filtering pipeline, since a real empty result is success, not an error"

patterns-established:
  - "Pattern: a set is measured through the REAL job-path argv sequence (every guard-stage call site included), not the primary engine alone -- OCR's re-measure is the worked example, mirrored by plans 03/04's own converter wiring"

requirements-completed: []  # CONF-01/CONF-05 are declared by every sibling plan in this phase (68-01, 03-06); requirements.ready-ids reports not-ready until every declaring plan has a SUMMARY (shared-ID gate, #2388) -- not an oversight

coverage:
  - id: D1
    description: "OCR's proven set closes the pdfinfo/pdftotext gap and the /usr/share/fonts font-substitution trap, with a necessity-by-removal table distinguishing the one path that changes content (kept) from three that don't (dropped)"
    requirement: CONF-05
    verification:
      - kind: other
        ref: "scripts/conf05-crosscheck.sh --doc ocr (structural: marker blocks well-formed, readonly=7, engines=4)"
        status: pass
    human_judgment: true
    rationale: "The structural doc check above is mechanically re-runnable and passes, but the substantive claim -- that pdftoppm/tesseract silently degrade to a font-less render and empty text at exit 0 without /usr/share/fonts, and that the three other candidate font paths make no content difference -- was proven this session via live docker measurement against freshly built images and is recorded as evidence tables in SANDBOX-PATHS.md's ocr section, not as a repo-checked-in automated test. A verifier should re-run the equivalent docker measurement (or audit the recorded tables) to independently confirm the content claims before relying on them."
  - id: D2
    description: "pandoc's and av's candidate sets are reconfirmed unchanged across a fuller job-path argv inventory (nine pandoc source/target pairs, thirteen av argvs including the AVPreflight lavfi source generator) -- proving no prior gap for either class, not merely repeating the 2026-09-20 answer"
    requirement: CONF-05
    verification:
      - kind: other
        ref: "scripts/conf05-crosscheck.sh --doc pandoc av (structural: pandoc readonly=0 engines=1, av readonly=7 engines=2)"
        status: pass
    human_judgment: true
    rationale: "Same class of gap as D1: the structural doc check passes, but the sufficiency claim (byte-identical / stream-summary-identical output under the confined ruleset across all 22 argvs combined) was proven by live docker measurement this session, recorded as evidence tables, not as a re-runnable repo test."
  - id: D3
    description: "audio's proven set adds ffprobe (covering the pre-Convert duration guard) and moves the model grant to the resolved /models/ggml-base.bin file; /proc/filesystems and /proc/self/status proven unnecessary by removal"
    requirement: CONF-05
    verification:
      - kind: other
        ref: "scripts/conf05-crosscheck.sh --doc audio (structural: readonly=7 with no bare /models line, engines=3: ffmpeg/ffprobe/whisper-cli)"
        status: pass
    human_judgment: true
    rationale: "Same class of gap as D1/D2: the structural doc check passes; the content claims (transcript text identical with and without the two removed /proc paths, ffprobe/ffmpeg/whisper-cli all denied against an outside fixture) were proven by live docker measurement this session and recorded as evidence tables, not as a repo-checked-in automated test."
  - id: D4
    description: "scripts/conf05-crosscheck.sh mechanically extracts and validates the four classes' conf05 marker blocks: --doc mode works before any *sandbox.go Go var exists, a missing marker block is a hard error (exit 2, never an empty pass), and the doc-vs-Go diff path is ready for plans 03-06 to pin against"
    requirement: CONF-05
    verification:
      - kind: other
        ref: "scripts/conf05-crosscheck.sh --doc pandoc ocr av audio exits 0, prints four DOC lines with the exact counts the plan's acceptance criteria name"
        status: pass
      - kind: other
        ref: "inversion: deleting the <!-- conf05:ocr:engines --> marker line made --doc ocr exit 2 naming the class; the line was restored and the doc verified byte-identical to before the deletion"
        status: pass
    human_judgment: false

duration: 31min
completed: 2026-09-24
status: complete
---

# Phase 68 Plan 02: OCR/AV/Audio/Pandoc Sandbox Re-Measurement Summary

**Re-measured all four classes' real job-path argv against freshly built images, closed OCR's undocumented pdfinfo/pdftotext gap, caught a live font-substitution trap (pdftoppm+tesseract silently degrading to empty OCR text at exit 0 without `/usr/share/fonts`), and shipped `scripts/conf05-crosscheck.sh` so plans 03-06 can pin their Go path sets against the doc mechanically.**

## Performance

- **Duration:** 31 min
- **Started:** 2026-09-24T00:04:00Z (approx., anchored to the first `measure68` docker build)
- **Completed:** 2026-09-24T00:34:59Z
- **Tasks:** 3
- **Files modified:** 3 (1 created, 2 modified)

## Accomplishments

- Built all four `measure68` images natively (arm64, this host's real Landlock) and recorded every job-path binary's resolved path in the image.
- Traced and content-verified the full real argv inventory per class: pandoc (9 source/target pairs, was 5), OCR (12: both `pdfinfo` forms x2 PDFs, `pdftotext`, both `pdftoppm` renders, five `tesseract` invocations), av (13: three `ffprobe` forms, progress-wrapped transcode, webm, hevc, three audio-extract targets, three thumbnail targets, the AVPreflight lavfi source generator), audio (6: three `ffprobe` duration forms including a video-container source, ffmpeg normalize, two `whisper-cli` output formats).
- OCR: added `pdfinfo` and `pdftotext` to the granted engines (both run on every real PDF job and were never measured before); found and fixed, by necessity-by-removal, that `/usr/share/fonts` is REQUIRED for born-digital PDF font substitution — without it `pdftoppm` exits 0 with a different (font-less) render and `tesseract` then exits 0 with EMPTY text.
- Audio: added `ffprobe` to the granted engines (covers the pre-Convert duration guard) and changed the model grant from the `/models` directory to the resolved `/models/ggml-base.bin` file.
- Pandoc and av: reconfirmed their existing sets are complete and correct under a fuller argv inventory — no gap found for either class.
- `scripts/conf05-crosscheck.sh`: new doc-vs-Go cross-check, `--doc` mode works today (before any `*sandbox.go` exists for ocr/av/audio); a missing conf05 marker block is `exit 2`, a Go var not found is `exit 3` (distinct from an actual mismatch, `exit 1`).
- Fixed a pre-existing bug in `scripts/sandbox-paths-measure.sh`'s `trace` phase that aborted before printing its own trailing note whenever an engine's candidate set was genuinely empty (pandoc's own case).

## Task Commits

Each task was committed atomically:

1. **Task 1 (blocking-fix half): trace phase's empty-result abort** — `6b46373` (fix)
2. **Tasks 1+2+3: re-measured sections with sufficiency/necessity/effectiveness evidence and conf05 blocks** — `af3fcd1` (docs)
3. **Task 3: conf05-crosscheck.sh doc-vs-Go cross-check** — `34c6581` (feat)

**Plan metadata:** committed separately after this SUMMARY (docs).

_Note: the blocking script fix (6b46373) was discovered mid-Task-1 and had to land before any trace could complete; Tasks 1 and 2's SANDBOX-PATHS.md content (candidates, then evidence) was written and committed together in af3fcd1 rather than as two separate commits, since both were produced in the same measurement pass per class rather than as genuinely separable increments._

## Files Created/Modified

- `scripts/conf05-crosscheck.sh` - doc-vs-Go cross-check for CONF-05, `--doc` mode + Go-var diff mode
- `.planning/research/SANDBOX-PATHS.md` - four "Замер фазы 68 — кандидаты" subsections (binaries, traced argv, candidates, sufficiency/necessity/effectiveness tables, final proven sets, conf05 marker blocks) appended to the pandoc/ocr/av/audio sections; two sentences added to "Метод"
- `scripts/sandbox-paths-measure.sh` - Rule 1 bugfix: `trace` no longer aborts on a genuinely empty path result

## Decisions Made

See `key-decisions` in frontmatter. Most consequential for plans 03-06: OCR's final `ocrReadOnly` must include `/usr/share/fonts` (proven necessary, not merely plausible) and must NOT include `/etc/fonts`/`/usr/share/fontconfig/conf.avail`/`/usr/local/share/fonts` (proven unnecessary) — a set built from the raw trace alone, without the removal step, would have shipped three unneeded grants and, worse, risked dropping the one that actually matters if someone "cleaned up" the font paths without re-testing content.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `scripts/sandbox-paths-measure.sh`'s `trace` phase aborted on a genuinely empty path result**
- **Found during:** Task 1, the very first pandoc trace run of this plan
- **Issue:** `cat .trace.* | grep -oE ... | sed ... | grep '^/' | grep -v '^/job/' | sort -u > .paths` under `set -euo pipefail` exits nonzero whenever zero paths match (an intermediate `grep` stage returns 1 on no matches, and `pipefail` propagates that through the whole pipeline) — `set -e` then aborted the script before it ever printed its own trailing "NOT shown, and needed anyway" note or cleaned up `.trace.*`. A statically-linked engine (pandoc) legitimately produces exactly this empty result on every argv.
- **Fix:** appended `|| true` to the whole pipeline; a genuinely empty candidate set is success, not an error.
- **Files modified:** `scripts/sandbox-paths-measure.sh`
- **Verification:** re-ran all nine pandoc traces after the fix — all completed with `exit=0` and printed the expected trailing note; re-ran the full OCR/av/audio trace batches (36 more invocations total) with no further occurrence.
- **Committed in:** `6b46373`

---

**Total deviations:** 1 auto-fixed (1 Rule 1 bug). **Impact:** necessary for the trace tool to complete at all against a statically-linked engine; no scope creep — the fix is scoped to the exact failure mode found, not a rewrite of the pipeline.

## Issues Encountered

None beyond the deviation above.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `scripts/conf05-crosscheck.sh` and all four classes' conf05 marker blocks are ready for plans 03-06 to pin their `<class>ReadOnly`/`<class>Engines` Go vars against.
- OCR's proven set (`/lib`, `/usr/local/lib`, `/usr/local/share/tessdata`, `/etc/ld.so.cache`, `/etc/localtime`, `/usr/share/fonts`, `/sys/devices/system/cpu/possible` + `tesseract`/`pdftoppm`/`pdfinfo`/`pdftotext`) is ready for 68-03.
- av's proven set (unchanged from 2026-09-20) is ready for 68-04.
- audio's proven set (`/lib`, `/usr/lib`, `/usr/local/lib`, `/etc/ld.so.cache`, `/proc/cpuinfo`, `/sys/devices/system/cpu/online`, `/sys/devices/system/cpu/possible` + `ffmpeg`/`ffprobe`/`whisper-cli`, model file granted separately at run time) is ready for 68-05 — including the open design question 68-RESEARCH.md Pitfall 2 flags (whether/how the pre-Convert duration guard itself gets confined), which this plan deliberately leaves to 68-05's own decision.
- CONF-01/CONF-05 stay "In Progress" in REQUIREMENTS.md until every plan in this phase that declares them (01, 03-06) has its own SUMMARY — the shared-ID gate (#2388) working as intended.
- No blockers for 68-03 through 68-06.

---
*Phase: 68-four-straightforward-engine-classes*
*Completed: 2026-09-24*

## Self-Check: PASSED

`scripts/conf05-crosscheck.sh` (created, executable) and `.planning/research/SANDBOX-PATHS.md` (modified) verified present on disk with `[ -f ]`. `scripts/sandbox-paths-measure.sh` (modified) verified present. All three task commits (`6b46373`, `af3fcd1`, `34c6581`) verified present via `git log --oneline --all`. `scripts/conf05-crosscheck.sh --doc pandoc ocr av audio` exits 0 and prints the exact readonly/engines counts the plan's acceptance criteria name (pandoc 0/1, ocr 7/4, av 7/2, audio 7/3). Every plan-level `<verification>` command re-run and passing. No Go source was touched by this plan (measurement/doc-only), so `go build`/`go vet`/`go test` are unaffected and were not re-run as a gate for this plan specifically.
