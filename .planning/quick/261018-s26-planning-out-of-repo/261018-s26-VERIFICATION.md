---
phase: quick-261018-s26-planning-out-of-repo
verified: 2026-10-18T18:45:00Z
status: passed
score: 6/6 must-haves verified
covered_files:
  - ".env.example"
  - ".planning/quick/261018-s26-planning-out-of-repo/261018-s26-PLAN.md"
  - ".planning/quick/261018-s26-planning-out-of-repo/261018-s26-SUMMARY.md"
  - "cmd/audio-worker/main.go"
  - "cmd/av-worker/main.go"
  - "cmd/chromium-worker/main.go"
  - "cmd/document-worker/main.go"
  - "cmd/ocr-worker/main.go"
  - "cmd/pandoc-worker/main.go"
  - "cmd/probe-worker/main.go"
  - "cmd/worker/main.go"
  - "deploy/chart/octoconv/templates/deployment-chromium-worker.yaml"
  - "docker-compose.yml"
  - "docs/CAD.md"
  - "docs/SANDBOX-PATHS.md"
  - "internal/api/cad_ceiling_test.go"
  - "internal/convert/audiosandbox_test.go"
  - "internal/convert/avsandbox_test.go"
  - "internal/convert/cad_doc_test.go"
  - "internal/convert/cad_test.go"
  - "internal/convert/caddxfpdffloor_test.go"
  - "internal/convert/cadrender_test.go"
  - "internal/convert/htmlsandbox_test.go"
  - "internal/convert/libvips.go"
  - "internal/convert/libvipssandbox_test.go"
  - "internal/convert/ocrsandbox_test.go"
  - "internal/convert/pandocsandbox.go"
  - "internal/convert/pandocsandbox_test.go"
  - "internal/convert/probesandbox_test.go"
  - "internal/convert/testdata/evidence/74-3d-render-measurements/74-01-wave0.txt"
  - "internal/convert/testdata/evidence/75-dxf-2d-pipeline/75-03-guard.txt"
  - "internal/convert/testdata/evidence/75-dxf-2d-pipeline/75-05-bounds.txt"
  - "internal/convert/testdata/evidence/76-measured-bounds-keda-proof-client-docs/76-01-stack.txt"
  - "internal/convert/testdata/evidence/76-measured-bounds-keda-proof-client-docs/76-03-derived.txt"
  - "internal/convert/testdata/evidence/76-measured-bounds-keda-proof-client-docs/76-09-image-size.txt"
  - "internal/convert/testdata/evidence/76-measured-bounds-keda-proof-client-docs/76-09-keda-loadproof.txt"
  - "scripts/conf05-crosscheck.sh"
  - "scripts/sandbox-liveproof.sh"
covered_digest: "v3:sha256:46235892d89d165753a6250825c7c8091171ed24bf02cee3043db527e6299d1c"
behavior_unverified: 0
overrides_applied: 0
---

# Quick task 261018-s26: .planning out of the build path. Verification Report

**Goal:** Make the octoconv repo self-sufficient without .planning. A fresh clone with no .planning passes go build/vet/test, ci-helm-checks.sh and the non-docker ci-local tiers, and nothing tracked outside .planning reads a file under .planning at build/test/CI time.
**Verified:** 2026-10-18T18:45Z, at HEAD 8985b9dd (base 31c35171)
**Status:** passed
**Re-verification:** No, this is the initial verification.

Unless a row says otherwise, every result below comes from commands the verifier ran itself. The executor's evidence files were read only to compare against them. Where a result rests on executor evidence, the row says so.

## Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Scratch worktree of HEAD with .planning deleted: gofmt clean, build/vet/test -count=1 exit 0 (go1.26.4), ci-helm-checks exit 0, ci-local helm gate race PASS, .planning still absent afterwards, no tracked dirt besides the deletions | VERIFIED | Own detached worktree `verify-s26-wt` at 8985b9dd. Path and toplevel were asserted before deleting its .planning, which removed 1690 entries. Results: `gofmt -l .` empty; `go build ./...` rc=0; `go vet ./...` rc=0; `go test -count=1 -v -timeout 15m ./...` rc=0, 32 packages ok, 0 `--- FAIL`, 0 `(cached)`; `bash scripts/ci-helm-checks.sh` rc=0; .planning still absent afterwards; 0 tracked changes besides ` D .planning/`; no untracked files left. The 234 skips are all environment skips (DATABASE_URL, REDIS_ADDR, E2E_BASE_URL, ffmpeg/soffice/tectonic not present). None mentions planning or evidence. The gate tier runs exactly the four commands I ran. The race tier (`go test -race ./...`) was **not re-run by me**. Its result comes from the executor transcript t3-scratch-cilocal.txt (cwd s26-wt, `### tier race: PASS`, `ALL REQUESTED TIERS PASSED`, rc=0, 64 `ok` lines, 0 cached). -race cannot add a file read, so this does not weaken the .planning-independence claim. |
| 2 | E1-E7 are byte-identical tracked copies; every reader names its copy; with a copy moved aside its tests FAIL (never SKIP) with no-such-file on the testdata path while the .planning original is present | VERIFIED | `cmp`=0 for all 7, and the git blob at 31c35171:<source> equals the blob at HEAD:<copy> for every row. The sha256 values match the plan. Second worktree `verify-s26-inv` (with .planning present): every copy was moved aside in turn. E1/api 4 FAIL; E1/convert 3 FAIL; E2 1; E3 2; E4 2; E5 2; E6 1; E7 1. All runs had rc=1, 0 SKIP and 0 PASS, and every error was `open .../testdata/evidence/...: no such file or directory` (api: `../convert/testdata/...`). After each run `git diff --quiet HEAD` was clean. In the scratch run every reader test PASSED: TestCADConstantsComeFromThe76Evidence, TestCADRenderReChecksTheDerivedCeilingOfEveryFormat, TestCADPathBFiredForNoFormat, TestCADDocMatchesTheCode, TestCADDXFConstantsComeFromTheEvidence, TestCADDXFPDFTextFloorComesFromTheEvidence, TestCADPinnedRenderCapsComeFromTheEvidence, TestCADEveryOperationRefusesAboveTheDerivedFormatCeiling. |
| 3 | docs/SANDBOX-PATHS.md is byte-identical to the planning copy; no tracked file outside .planning names the old path; conf05 reads docs/, exits 0, output identical before/after and without .planning | VERIFIED | `cmp`=0, sha256 98a7a428...923c. `git grep '\.planning/research/SANDBOX-PATHS' HEAD -- ':!.planning'` rc=1. `DOC_FILE="docs/SANDBOX-PATHS.md"`. conf05 for all 8 classes in the scratch worktree without .planning: rc=0, and `cmp` against the executor's pre-change t2-conf05-before.txt =0. With docs/SANDBOX-PATHS.md moved aside, conf05 exits 1 with `error: docs/SANDBOX-PATHS.md not found`, so the copy is load-bearing. |
| 4 | Modified files differ from 31c35171 only by M1-M6, no mode change, no comment line added, exec.go identical to main | VERIFIED | `git diff --name-status 31c35171 8985b9dd`: 8 A, 28 M, 36 total. I read all 39 `-`/`+` pairs of the M files. Each pair is the same line with only the planning path substring replaced (M1-M6), and nothing was added unpaired or reworded. `git diff --summary`: only `create mode 100644` for the 8 new files, no mode changes. `git diff --quiet main -- internal/convert/exec.go` rc=0, both at HEAD and in the worktree. |
| 5 | SUMMARY holds the sha256 table, the classification table covering every referencing file, the PROVENANCE finding, the D-07 list, the proof results and the gsd-tools answer with code citations and a fixture run | VERIFIED | All sections are present. The union of `git grep -l '\.planning'` (outside .planning) at 31c35171 and HEAD is 81 files, and the classification table lists exactly those 81 files. gsd-tools pointers confirmed by reading the code: commands.cjs:1456 resolveCommitDocsPolicy, :1766 cmdCommit (:1785 passes `() => isGitIgnored(cwd, '.planning')`), :2581 cmdCommitToSubrepo; config-loader.cjs:207 isGitIgnored runs `check-ignore -q --no-index`; configuration.cjs:82 detectSubRepos skips names that start with `.`. In the main checkout `git check-ignore -q --no-index -- .planning` rc=0. t3-gsd-fixture.txt has the `== variant i/ii/iii ==` headers with skipped_gitignored / nothing_to_commit / committed:true. |
| 6 | go test ./... -count=1 passes in the main checkout at the final HEAD | VERIFIED | From executor evidence t3-main-gotest.txt: cwd main checkout, HEAD 8985b9dd, go1.26.4, rc=0, 32 ok, 0 FAIL. I did not re-run the full suite there, because my one full run was the stronger scratch run on the same commit. I spot-ran the one main-checkout-sensitive test, TestProbeMaxPagesHasExactlyOneReader (it walks every .go file under the repo root, including .planning): ok. |

**Score:** 6/6 truths verified, 0 present but behavior-unverified.

## Independent search: does anything tracked outside .planning read .planning?

- **Go, non-comment lines naming planning** (`git grep -i planning -- '*.go'` with comments removed): exactly one line, `internal/convert/cad_test.go:147 const decision`. It is used only inside a `t.Errorf` message and never opened. No `../../.planning` is left anywhere.
- **Indirect path construction:** I reviewed every `filepath.Join`/`os.ReadDir`/`WalkDir`/`Glob` in Go code. All of them target testdata, migrations, the chart, temp dirs or cmd/. Two walk from the repo root: probebounds_test.go:119 reads `.go` files, and mcpserver no_text_matching_test.go globs internal/mcpserver. Neither needs .planning to exist. The only `t.Skip` on a missing file is cadsniff_golden_test.go:214 (`$OCTOCONV_CAD_CACHE` / `~/.cache`), which is unrelated to .planning. No test skips when a planning file is missing, so no dependency can pass silently.
- **Scripts run by Go tests:** the only exec of a shell is the inline `/bin/sh -c` at cad_test.go:943. The helpers are cmd/cad-worker/helper/* and testdata/cadmesh-fake. Tests mention measurement scripts (cad-dxf-bounds.sh, cad-render-probe.sh, cad-worker-sandbox-measure.sh, quota-burst-driver.sh, ci-local.sh) only in messages and comments.
- **ci-local.sh tiers:** helm runs `./scripts/ci-helm-checks.sh`; gate runs gofmt, vet, build and `go test -count=1 ./...`; race runs `go test -race ./...`. None reads .planning. Line 49 `EVIDENCE_DIR="${CI_LOCAL_EVIDENCE_DIR:-.planning/ci-local}"` plus `mkdir -p` is a write-only default for the transcript, not a read (see Info).
- **ci-helm-checks.sh:** its only .planning mention is a comment on line 10. It runs only `helm lint`, `helm template` and yq on mktemp copies, plus one `grep -rn ... scripts/`. The `helm install/upgrade` mentions are in echo text and comments. It passed in the tree without .planning.
- **.github/workflows/ci.yml:** runs ci-helm-checks.sh, tectonic-bundle-fetch.sh (no .planning mention) and the go/docker steps.

## Required Artifacts

| Artifact | Status | Details |
|----------|--------|---------|
| internal/convert/testdata/evidence/76-.../76-03-derived.txt (E1) | VERIFIED | cmp=0; read by api + convert; load-bearing in both |
| internal/convert/testdata/evidence/75-dxf-2d-pipeline/75-05-bounds.txt (E5) | VERIFIED | cmp=0; load-bearing (2 tests) |
| internal/convert/testdata/evidence/74-3d-render-measurements/74-01-wave0.txt (E7) | VERIFIED | cmp=0; load-bearing |
| E2, E3, E4, E6 copies | VERIFIED | cmp=0; each load-bearing |
| docs/SANDBOX-PATHS.md | VERIFIED | cmp=0; conf05 fails without it |
| 261018-s26-SUMMARY.md | VERIFIED | present, all required sections; uncommitted (see Info) |

## Key Link Verification

| From | To | Status | Details |
|------|----|--------|---------|
| internal/api/cad_ceiling_test.go cad76DerivedEvidence | ../convert/testdata/evidence/76-.../76-03-derived.txt | WIRED | line 295; inversion fails at cad_ceiling_test.go:340 naming that path |
| internal/convert/cad_doc_test.go cadDocEvidDir | testdata/evidence/76-.../ (docs/CAD.md Scaling sources) | WIRED | line 26; E3/E4 inversion fails at cad_doc_test.go:809 |
| scripts/conf05-crosscheck.sh DOC_FILE | docs/SANDBOX-PATHS.md | WIRED | exact line match; fails without the file |

## Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Build/vet/test without .planning | worktree, .planning rm'd; gofmt, build, vet, `go test -count=1 -v ./...` | all rc=0; 32 ok | PASS |
| Helm checks without .planning | `bash scripts/ci-helm-checks.sh` | rc=0 | PASS |
| conf05 without .planning | `bash scripts/conf05-crosscheck.sh pandoc ocr av audio html probe document cad` | rc=0; cmp to pre-change output 0 | PASS |
| Copies load-bearing (7 evidence + SANDBOX-PATHS) | mv aside, run readers, .planning originals present | all FAIL, 0 SKIP; restored clean | PASS |
| Race tier | not re-run | executor transcript only | (executor evidence) |

## Commits

| Commit | Parent | Signature | Trailer |
|--------|--------|-----------|---------|
| e0ffdfb3 test: the 76-03 derived evidence is read from a tracked copy in testdata | 31c35171 | gpgsig present, `%G?`=G (andy.paderin@gmail.com) | none, no Co-Authored-By |
| 8985b9dd test: every evidence file a test reads, and SANDBOX-PATHS.md, live outside .planning | e0ffdfb3 | gpgsig present, `%G?`=G | none, no Co-Authored-By |

## Anti-Patterns Found

None in the changed lines. They are path substitutions only, with no new comments, TBD/FIXME/XXX or stubs.

## Info (non-blocking)

1. scripts/ci-local.sh:49 still defaults its transcript dir to `.planning/ci-local` and creates it with `mkdir -p`. This is a write, not a read. In a clone without .planning it creates a gitignored dir. After the split the transcripts land inside the planning repo unless CI_LOCAL_EVIDENCE_DIR is set. The proofs, mine and the executor's, kept .planning out of the scratch tree by setting it or by not running ci-local, so the default path itself was not exercised.
2. Prose citations of .planning paths stay in 57 tracked files (251 refs), including 8 inside the byte-identical docs/SANDBOX-PATHS.md and 128 in testdata/cad/PROVENANCE.md. They dangle after the split. This is intentional (class c) and not read by anything.
3. Measurement scripts (class b) still write evidence under .planning. After a re-derivation the testdata copy must be synced by hand (D-05).
4. 76-01-stack.txt lines 19-21 contain `/Users/apaderin/.cache/...`. These bytes were already in history.
5. The plan's Task 3 commit of SUMMARY and evidence was not made (orchestrator override). The branch holds exactly the two named commits.
6. During this verification, frontend/src/App.tsx, api.ts and styles.css in the main checkout became modified (mtime 21:40-21:41 local). This verifier did not cause it, because no command of mine touched the main checkout's working files. They were left untouched. They are unrelated to this task and not part of either commit.

## Gaps Summary

None. The goal is achieved at 8985b9dd. A tree with no .planning builds, vets, tests and passes the helm and conf05 checks. Every file a test or CI step used to read from .planning is a tracked, byte-identical copy, and each copy is proven load-bearing. Nothing tracked outside .planning reads .planning at build, test or CI time.

---

_Verified: 2026-10-18T18:45Z_
_Verifier: Claude (gsd-verifier)_
