---
phase: quick-260802-9de
plan: 01
subsystem: api
tags: [go, chi, pandoc, utf8, upload-validation, deployment-parity]

requires:
  - phase: 47-49 (v1.11 Pandoc Engine)
    provides: PandocConverter, enforcePandocInputSize, LooksLikeText/LooksLikeLaTeX, PANDOC_MAX_INPUT_BYTES deployment wiring
provides:
  - "Config.MaxPandocInputBytes: API-side mirror of the worker's PANDOC_MAX_INPUT_BYTES, folded into NewServer's D-07 default map"
  - "readSniffPrefix rune-boundary trim: a size-cap-truncated UTF-8 prefix is judged on complete runes only"
  - "docker-compose/chart/.env.example parity for the now-shared PANDOC_MAX_INPUT_BYTES env var"
affects: [pandoc engine, upload validation, deployment docs]

tech-stack:
  added: []
  patterns:
    - "Scalar-into-default-map fold: a single env-driven scalar (MaxPandocInputBytes) overrides one key of an otherwise-defaulted map, only inside the nil-map branch, preserving the map's existing 'non-nil is used as-is' contract"
    - "Truncation-aware UTF-8 validation: trim a cap-severed trailing partial rune by deriving declared length from the lead byte's own bits (never via utf8.RuneError, which would also launder genuinely invalid bytes)"

key-files:
  created: []
  modified:
    - internal/api/api.go
    - cmd/api/main.go
    - internal/api/handlers_test.go
    - internal/convert/markupsniff.go
    - internal/convert/markupsniff_test.go
    - docker-compose.yml
    - .env.example
    - deploy/chart/octoconv/templates/configmap.yaml

key-decisions:
  - "Config.MaxPandocInputBytes precedence: folded into NewServer's D-07 default map ONLY when MaxEngineBytes is left nil; a caller-supplied MaxEngineBytes map still owns every key including pandoc's, and the scalar has zero effect in that case."
  - "Trim condition: readSniffPrefix trims a trailing partial rune ONLY when size > cap AND read >= cap (the cap genuinely severed the prefix); a file that fits entirely within cap is judged unchanged, so a dangling lead byte at true EOF still fails closed."
  - "leadByteRuneLen derives declared rune length from the lead byte's own bit pattern (not utf8.DecodeRune/utf8.RuneError) so that 0xFF/0xFE -- never legal UTF-8 lead bytes -- declare length 0, are never trimmed, and still fail utf8.Valid."
  - "LooksLikeHTML's identical mid-rune defect in htmlsniff.go's own inlined prefix logic is a recorded, deliberate follow-up -- NOT fixed in this plan (see Deviations)."

patterns-established:
  - "Env-driven scalar folded into a default map inside NewServer's nil-map defaulting branch, rather than duplicating the whole map at the cmd/ call site"

requirements-completed: [PR2-F1, PR2-F2]

duration: ~30min
completed: 2026-08-02
---

# Quick Task 260802-9de: Close remaining PR#2 review findings Summary

**Pandoc upload ceiling now driven by PANDOC_MAX_INPUT_BYTES (413-before-upload, not 202-then-async-fail), and readSniffPrefix trims a size-cap-severed trailing UTF-8 rune before validation so large valid text/LaTeX files stop being falsely rejected.**

## Performance

- **Duration:** ~30 min
- **Completed:** 2026-08-02
- **Tasks:** 3
- **Files modified:** 8

## Accomplishments

- The API rejects a pandoc upload above the configured `PANDOC_MAX_INPUT_BYTES` with 413 before any S3 write, job row, or queue slot — closing the gap where the API accepted what the worker's `enforcePandocInputSize` would certainly reject later (PR#2 finding-1).
- `readSniffPrefix` no longer rejects a valid, oversized text/LaTeX file merely because the 1 MiB sniff cap happened to sever a multi-byte rune mid-encoding — the fix is scoped to truncated prefixes only and remains fail-closed against genuinely invalid UTF-8 (PR#2 finding-2).
- Deployment surface (`docker-compose.yml`, `.env.example`, the Helm chart's ConfigMap) and three shipped comments now correctly reflect that `PANDOC_MAX_INPUT_BYTES` is read by both `cmd/api` and `cmd/pandoc-worker`, not worker-only.

## Task Commits

Each task was committed atomically:

1. **Task 1: Drive the API's pandoc upload ceiling from PANDOC_MAX_INPUT_BYTES** - `5dea811` (feat)
2. **Task 2: Trim an incomplete trailing rune before validating a truncated sniff prefix** - `ed8536c` (fix)
3. **Task 3: Deployment parity and comment truthfulness for the now-shared env var** - `19b7eae` (docs)

_Note: tasks 1 and 2 are TDD-flagged in the plan; both were developed RED-then-GREEN locally (new tests confirmed failing via `go vet`/`go test` before the corresponding production-code fix was written) but each landed as a single atomic per-task commit, consistent with this quick task's "commit each task atomically" constraint rather than splitting into separate test/feat commits._

## Files Created/Modified

- `internal/api/api.go` - `Config.MaxPandocInputBytes` scalar (doc-commented with its precedence rule); `NewServer`'s D-07 default-map block folds it into `MaxEngineBytes[convert.EnginePandoc]` when the map is left nil
- `cmd/api/main.go` - `MaxPandocInputBytes: envInt64("PANDOC_MAX_INPUT_BYTES", 0)` added to the `api.Config` literal; `MaxEngineBytes` still left nil, no map duplicated at this call site
- `internal/api/handlers_test.go` - 4 new/extended tests: `TestCreateJob_PandocOversizedRejectedBeforeUpload` (finding-1 regression guard), `TestCreateJob_PandocWithinConfiguredCapAccepted`, `TestNewServer_MaxPandocInputBytesOverridesDefault`, and `TestNewServer_MaxEngineBytesDefaults` extended to seven engines
- `internal/convert/markupsniff.go` - `trimPartialTrailingRune` + `leadByteRuneLen` helpers; `readSniffPrefix` calls the trim only when `size > cap && read >= cap`, with a re-check for an emptied buffer
- `internal/convert/markupsniff_test.go` - 5 new tests: two straddling-rune positive cases (finding-2 regression guard, one each for `LooksLikeText`/`LooksLikeLaTeX`), two fail-closed negative guards (impossible lead byte at the cap boundary; dangling lead byte at true EOF on a non-truncated file), plus confirmed the two existing `*OversizedInputBoundedRead` tests still pass unchanged
- `docker-compose.yml` - added `PANDOC_MAX_INPUT_BYTES: "3145728"` to the `api` service (byte-identical to `pandoc-worker`); corrected the `pandoc-worker-only` comment above the `pandoc-worker` service's own entry
- `.env.example` - corrected the `pandoc-worker-only` clause on the `PANDOC_MAX_INPUT_BYTES` line, preserving the full measured-provenance narrative
- `deploy/chart/octoconv/templates/configmap.yaml` - corrected the grouped comment that had lumped `PANDOC_MAX_INPUT_BYTES` in with the genuinely worker-only `PANDOC_WORKER_CONCURRENCY`; the ConfigMap key itself needed no change (the api Deployment already inherits it via `octoconv.commonEnv` → `envFrom: configMapRef: octoconv-config`)

## Decisions Made

- **Config precedence rule:** `Config.MaxPandocInputBytes` is folded into `NewServer`'s D-07 default map *only* inside the existing `if cfg.MaxEngineBytes == nil` branch, immediately after the map literal, overwriting the `EnginePandoc` entry when the scalar is `> 0`. This deliberately preserves the map field's pre-existing documented contract ("a non-nil map is used as-is") — a caller that supplies its own full `MaxEngineBytes` map still owns every key in it, including pandoc's; the scalar has no effect in that case. Zero means "not configured," leaving the 100 MiB default in place — an absent env var is never a behaviour change (verified by `TestNewServer_MaxEngineBytesDefaults`).
- **Exact trim condition:** `readSniffPrefix` calls `trimPartialTrailingRune` only when `size > cap && int64(read) >= cap` — both halves are required. `size > cap` proves more bytes genuinely exist beyond the read prefix (this really was a truncation, not the whole file); `read >= cap` proves the read actually reached the cap rather than stopping short for some other reason (e.g. a client that overstated size). When the whole file fits within the cap, no trim happens at all, and a dangling lead byte at true EOF is correctly judged as genuinely invalid UTF-8 (`TestLooksLikeTextIncompleteRuneAtEOFStillRejected`). The trim itself scans back at most `utf8.UTFMax-1` bytes for a rune-start byte (`utf8.RuneStart`), then derives the *declared* encoding length directly from that lead byte's own bit pattern via `leadByteRuneLen` — deliberately not via `utf8.DecodeRune`/`utf8.RuneError`, since that would also flag a genuinely invalid lead byte (0xFF, 0xFE — neither ever legal in UTF-8) as "incomplete" and trim it away. Those bytes declare length 0 in `leadByteRuneLen`, so they survive untouched into the subsequent `utf8.Valid` check and are still rejected (`TestLooksLikeTextInvalidUTF8AtSniffCapStillRejected`).
- **LooksLikeHTML duplicate-defect follow-up:** `htmlsniff.go`'s `LooksLikeHTML` carries its own inlined copy of the identical prefix-read-then-`utf8.Valid` logic and therefore has the identical mid-rune defect. This is a real, adjacent bug but was explicitly out of scope for this plan (`<non_goals>`) — it has its own regression-test surface and was left untouched. `git diff --stat` confirms `htmlsniff.go` was not modified by this plan. Recommended as a follow-up `/gsd:quick` task with the same shape as Task 2 here (add `trimPartialTrailingRune` call to `LooksLikeHTML`'s own inlined `readSniffPrefix`-equivalent, or better, have it call the now-shared `readSniffPrefix`/`trimPartialTrailingRune` in `markupsniff.go` instead of duplicating the logic).

## Deviations from Plan

None — plan executed exactly as written. All three tasks' `<action>` and `<verify>` blocks were followed as specified; all `must_haves.truths` and `success_criteria` are satisfied (see Self-Check below). The only judgment call made was the TDD commit granularity noted under Task Commits above (single atomic commit per task rather than separate RED/GREEN commits), which is consistent with the quick-task execution convention and does not change what was verified: for both TDD tasks, the new tests were confirmed to fail (compile error for Task 1's `MaxPandocInputBytes` field; genuine test failures for Task 2's two straddling-rune tests) before the corresponding production code was written.

## Issues Encountered

None. One workflow note: verification commands in this session had to be run without an explicit `cd /Users/apaderin/dev/octoconv` prefix — the worktree sandbox correctly rejected that pattern (it would have targeted the main repo checkout, not this worktree). All `go build`/`go vet`/`go test`/`gofmt` and file-parity checks were instead run directly from the worktree's own working directory, which is where all commits and edits actually landed.

## User Setup Required

None — no external service configuration required. `PANDOC_MAX_INPUT_BYTES` is already set in `docker-compose.yml`'s `api` service and in the Helm chart's `octoconv-config` ConfigMap (both `"3145728"`, byte-identical to `pandoc-worker`); no operator action is needed to pick up the new API-side gate on next deploy.

## Next Phase Readiness

- Both remaining PR#2 review findings are closed. Nothing else is pending from that review.
- The recorded `LooksLikeHTML` duplicate-defect follow-up (see Decisions Made) is a good candidate for a small future `/gsd:quick` task; it was deliberately not pursued here per `<non_goals>`.
- No blockers. The next milestone per `.planning/STATE.md`'s Operator Next Steps is `/gsd-new-milestone`.

## Self-Check

Verifying claims made in this summary before finalizing.

**Files exist:**
- FOUND: internal/api/api.go
- FOUND: cmd/api/main.go
- FOUND: internal/api/handlers_test.go
- FOUND: internal/convert/markupsniff.go
- FOUND: internal/convert/markupsniff_test.go
- FOUND: docker-compose.yml
- FOUND: .env.example
- FOUND: deploy/chart/octoconv/templates/configmap.yaml

**Commits exist:**
- FOUND: 5dea811
- FOUND: ed8536c
- FOUND: 19b7eae

**htmlsniff.go untouched:** confirmed via `git diff --stat` across all three task commits — `internal/convert/htmlsniff.go` does not appear.

**Whole-repo sweep (all green):** `gofmt -l . | grep -v '^\.claude/'` → empty; `go build ./...` → clean; `go vet ./...` → clean; `go test ./internal/... ./cmd/... -count=1` → all `ok`.

**Finding-specific isolated proof:** `go test ./internal/api/ -run TestCreateJob_PandocOversizedRejectedBeforeUpload -count=1` → PASS; `go test ./internal/convert/ -run TestLooksLikeTextRuneStraddlingSniffCap -count=1` → PASS.

## Self-Check: PASSED

---
*Phase: quick-260802-9de*
*Completed: 2026-08-02*
