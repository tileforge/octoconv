---
phase: quick-260803-tm4
plan: 01
subsystem: config
tags: [go, env-parsing, ssrf, quota-kill-switch, security-posture, openapi, planning-hygiene]

requires:
  - phase: 51-52 (v1.12 Attribution/Quota)
    provides: QUOTA_ENFORCEMENT_DISABLED kill-switch (cmd/api/main.go), WEBHOOK_ALLOW_* SSRF opt-ins (internal/api/callbackurl.go), S3_USE_SSL storage config (internal/storage/storage.go)
provides:
  - "internal/envcfg.ParseBool/Bool: the single strict, non-logging boolean env parser shared by all five sites"
  - "an inline `# comment` trailing a boolean env value no longer silently defeats it at any of the five sites"
  - "docs/openapi.yaml's Error schema correctly scoped to non-429 responses"
  - "52-01-SUMMARY/52-02-PLAN/ROADMAP/STATE synced to the repository's actual Phase 52 completion state"
affects: [api startup, webhook SSRF guard, storage client startup, quota kill-switch, openapi spec, planning docs]

tech-stack:
  added:
    - "internal/envcfg: new package, zero project-internal dependencies (os + strings only)"
  patterns:
    - "Strict boolean env parser returning (value, recognized): recognized=false lets the caller decide whether to surface an unrecognized value via error (startup-only sites) or silent fail-closed default (per-request sites), without the parser itself ever logging"
    - "Consolidated startup validation block: all three cmd/api-owned booleans (QUOTA_ENFORCEMENT_DISABLED, WEBHOOK_ALLOW_PRIVATE_IPS, WEBHOOK_ALLOW_INSECURE_HTTP) are read and validated together in one loop, even though only two of the three are otherwise consumed by cmd/api itself -- WEBHOOK_ALLOW_INSECURE_HTTP is validated here purely because cmd/api is the only process able to see it at all"

key-files:
  created:
    - internal/envcfg/bool.go
    - internal/envcfg/bool_test.go
  modified:
    - internal/api/callbackurl.go
    - internal/api/callbackurl_test.go
    - internal/storage/storage.go
    - internal/storage/storage_test.go
    - cmd/api/main.go
    - .env.example
    - README.md
    - docs/openapi.yaml
    - .planning/phases/52-quota-enforcement-intra-class-fairness/52-01-SUMMARY.md
    - .planning/phases/52-quota-enforcement-intra-class-fairness/52-02-PLAN.md
    - .planning/ROADMAP.md
    - .planning/STATE.md

key-decisions:
  - "D-1: internal/envcfg lives as a new package depending only on os/strings, so both internal/api and internal/storage can import it with no cycle; it never logs, because CLAUDE.md states internal/ returns errors rather than logging, and some call sites are per-request, where a log line would flood on a single operator typo."
  - "D-2: ParseBool returns (value, recognized) rather than an error -- recognized is false only when a non-empty raw value fails to parse; empty/absent is legal configuration (recognized=true, caller's default used)."
  - "D-3: surfacing an unrecognized value is per-process-start, never per-request -- the two callbackurl.go sites discard `recognized` and fall back silently to the fail-closed default; storage.New returns an error (every cmd/* caller already treats it as fatal); cmd/api logs a warning once at startup for all three booleans it can see, including WEBHOOK_ALLOW_INSECURE_HTTP, which it does not otherwise consume, purely because it is the only process able to validate that var at all (.env.example documents WEBHOOK_ALLOW_* as API-side only; cmd/webhook-worker does not read either)."
  - "D-4: WEBHOOK_ALLOW_INSECURE_HTTP now gets the same startup 'this is relaxed' warning that WEBHOOK_ALLOW_PRIVATE_IPS already had (D-04) -- a deliberate, recorded widening of visibility, not incidental scope."
  - "D-5: firstField is untouched -- it keeps serving its ~28 numeric/duration call sites. ParseBool deliberately does not reuse it: firstField splits at the first space, which would accept \"true anything\" as \"true\", the precise defect being closed for QUOTA_ENFORCEMENT_DISABLED."
  - "internal/api/handlers.go:8 already imports \"log\" today -- a pre-existing inconsistency with the stated \"internal/ never logs\" convention. This task did not widen it: no logging was added anywhere in internal/ as part of this change (grep -c 'log\\.' == 0 on both touched internal/ files)."

patterns-established:
  - "Strict inline-comment-tolerant boolean parsing (truncate at first '#', TrimSpace, exact-match 'true'/'false') as the house style for all future boolean env vars -- documented explicitly in internal/envcfg/bool.go's doc comment, including why it must not be folded back into firstField"

requirements-completed: [PR5-CR-BOOLENV, PR5-CR-OPENAPI-ERROR, PR5-CR-PLANDRIFT]

duration: ~35min
completed: 2026-08-03
---

# Quick Task 260803-tm4: Strict boolean env parsing + OpenAPI/planning cleanup Summary

**One shared `internal/envcfg.Bool` parser now serves all five boolean env sites, closing a backwards asymmetry where the quota kill-switch was lenient (`"true anything"` disabled enforcement) while the two webhook SSRF opt-ins were strict enough that their own documented inline comments silently defeated them (`WEBHOOK_ALLOW_PRIVATE_IPS=true   # staging` stayed OFF).**

## Deployment Risk

**Reproduced verbatim from the plan's `<deployment_risk>` block — read this before deploying.**

Two of the five sites are security opt-ins whose effective value CHANGES for some
configurations after this task:

| If a live environment currently has | Today | After this change |
|---|---|---|
| `WEBHOOK_ALLOW_PRIVATE_IPS=true   # comment` | flag OFF (comment defeats it) | **flag ON** — RFC1918 callback targets accepted |
| `WEBHOOK_ALLOW_INSECURE_HTTP=true   # comment` | flag OFF | **flag ON** — plain-http callback URLs accepted |
| `QUOTA_ENFORCEMENT_DISABLED=true anything` | enforcement DISABLED | **enforcement ON** (unrecognized → default) |
| `S3_USE_SSL=TRUE` / `=1` / `=yes` | silently false | **process refuses to start** with an explicit error |

The first two rows are the ones that matter: a flag that is off today turns on. This is
the intended fix (the operator wrote `true` and meant `true`), and the operator has
accepted it — but it is a real posture change, not a no-op refactor. Every deployed value
in this repository is already a bare `true`/`false` (`docker-compose.yml` sets
`S3_USE_SSL: "false"` at all ten services; `deploy/chart/octoconv/values.yaml:26` sets
`useSSL: "false"`; `.env.example` sets bare booleans on all four declarations), so no
in-repo configuration changes behavior. The risk is confined to hand-edited environments
outside this repository.

## Performance

- **Duration:** ~35 min
- **Completed:** 2026-08-03
- **Tasks:** 4
- **Files modified:** 14 (2 created, 12 modified)

## Accomplishments

- Added `internal/envcfg`, a pure (os + strings only, no other project imports), non-logging, table-driven-tested strict boolean parser (`ParseBool`/`Bool`) — truncates at the first `#`, trims whitespace, then accepts exactly `"true"`/`"false"`; everything else (`TRUE`, `1`, `yes`, `true anything`) is unrecognized and falls back to the caller's default.
- Rewired all five boolean env sites onto it: `internal/api/callbackurl.go` (both `WEBHOOK_ALLOW_*` reads, silently fail-closed per request), `internal/storage/storage.go` (`S3_USE_SSL`, returned as a startup error before any `minio.New` call), and `cmd/api/main.go` (`QUOTA_ENFORCEMENT_DISABLED` + both `WEBHOOK_ALLOW_*` vars, validated together in one consolidated startup warning block).
- Added the security-proof tests required by the plan: `WEBHOOK_ALLOW_PRIVATE_IPS="true   # for staging"` and `WEBHOOK_ALLOW_INSECURE_HTTP="true # staging"` now enable their respective opt-ins — both cases fail against the pre-change `os.Getenv(...) == "true"` comparison — while `TRUE`/`1`/`yes`/`true anything` are rejected everywhere and stay fail-closed.
- Added the mirrored D-4 "this is relaxed" startup warning for `WEBHOOK_ALLOW_INSECURE_HTTP`, matching the existing `WEBHOOK_ALLOW_PRIVATE_IPS` one (D-04) in wording and placement.
- Synced `README.md` (both sites), `.env.example` (all four boolean declarations plus the `WEBHOOK_ALLOW_*` header block), and the `cmd/api/main.go` QUOT-04/D-13 comment to describe the new rule instead of the old "only the literal `true`" claim.
- Corrected `docs/openapi.yaml`'s `Error` schema, which falsely claimed to serve every non-2xx response including 429 (all thirteen 429s actually `$ref` `QuotaError` via the shared `TooManyRequests` component); redocly lint still reports 0 errors and the same 2 pre-existing benign warnings.
- Corrected four planning documents that had drifted from the repository's actual state: `52-01-SUMMARY.md` no longer claims `QUOT-04` (it only delivers `QUOT-01`); `52-02-PLAN.md`'s `newClientLabelAllocator(` grep count is fixed from 1 to 2 (the live file genuinely has two matches); `ROADMAP.md`'s footer now says 11/15 v1.12 requirements complete with `/gsd:plan-phase 53` as next step (was "0/15 ... plan-phase 51"); `STATE.md`'s cursor now reflects Phase 52 as fully complete (9 of 9 plans) with Phase 53 as the next step.

## Task Commits

Each task was committed atomically:

1. **Task 1: internal/envcfg — the strict boolean parser and its table test** - `a4739c0` (feat)
2. **Task 2: rewire all five boolean sites onto envcfg, prove the security-relevant change, sync docs** - `32f5296` (fix)
3. **Task 3: docs/openapi.yaml — correct the Error schema's stale scope claim** - `9273f7c` (docs)
4. **Task 4: correct four planning documents that no longer match the repository** - `d5d5933` (docs)

## Files Created/Modified

- `internal/envcfg/bool.go` — `ParseBool(raw string, def bool) (bool, bool)` and `Bool(key string, def bool) (bool, bool)`; package doc comment states the never-logs rationale; `ParseBool`'s doc comment reproduces the full behavior table and explicitly names why `firstField` is not reused
- `internal/envcfg/bool_test.go` — table-driven `TestParseBool` (15 cases covering every behavior-table row plus `True`/`on` variants) and `TestBoolReadsEnv` (3 `t.Setenv`-based cases)
- `internal/api/callbackurl.go` — both `WEBHOOK_ALLOW_*` reads now call `envcfg.Bool`, discarding `recognized` with an inline comment explaining why (per-request/per-resolved-IP paths must fail closed silently)
- `internal/api/callbackurl_test.go` — added `TestIsBlockedIPAllowPrivateInlineComment` (security proof), `TestIsBlockedIPRejectsUnrecognizedFlagValues`, `TestValidateCallbackURLInsecureHTTPInlineComment` — pure additions, no existing assertion touched
- `internal/storage/storage.go` — `S3_USE_SSL` now parsed via `envcfg.Bool`; an unrecognized value returns an error placed after the required-vars check but before `minio.New`, so no network call happens on a misconfigured flag
- `internal/storage/storage_test.go` — added `TestNewRejectsUnrecognizedUseSSL` (two subtests: unrecognized value errors without a live MinIO; a recognized value with an inline comment does not itself produce an `S3_USE_SSL` error)
- `cmd/api/main.go` — one consolidated startup block reads and validates `QUOTA_ENFORCEMENT_DISABLED`, `WEBHOOK_ALLOW_PRIVATE_IPS`, `WEBHOOK_ALLOW_INSECURE_HTTP` via `envcfg.Bool`, warning once per unrecognized value; existing `WEBHOOK_ALLOW_PRIVATE_IPS` "relaxed" warning kept, `WEBHOOK_ALLOW_INSECURE_HTTP`'s mirrored warning added (D-4); the QUOT-04/D-13 comment block rewritten to describe the new rule
- `.env.example` — `S3_USE_SSL`, `WEBHOOK_ALLOW_*` header block, and `QUOTA_ENFORCEMENT_DISABLED` all updated to describe strict-with-inline-comment parsing and the deployment-risk consequence
- `README.md` — the Quotas prose bullet and the config table row for `QUOTA_ENFORCEMENT_DISABLED` updated to the new rule
- `docs/openapi.yaml` — `Error` schema's `description` field rewritten to state it covers every non-2xx response EXCEPT 429 (429 uses `QuotaError`); nothing else in the file changed
- `.planning/phases/52-quota-enforcement-intra-class-fairness/52-01-SUMMARY.md` — `requirements-completed` corrected from `[QUOT-01, QUOT-04]` to `[QUOT-01]`
- `.planning/phases/52-quota-enforcement-intra-class-fairness/52-02-PLAN.md` — the `newClientLabelAllocator(` grep-count acceptance criterion corrected from 1 to 2, with both matching sites named explicitly
- `.planning/ROADMAP.md` — the trailing v1.12 footer paragraph updated from "0/15 complete... plan-phase 51" to "11/15 complete... plan-phase 53"
- `.planning/STATE.md` — `stopped_at`, `Plan:` (9 of 9), `Pending Todos`, `Session Continuity`, and `Operator Next Steps` all advanced to reflect Phase 52's actual completion and Phase 53 as the real next step

## Decisions Made

See `key-decisions` in the frontmatter for D-1 through D-5 (all pre-recorded in the plan, not re-opened here). The one additional note worth calling out explicitly:

- **`internal/api/handlers.go:8`'s pre-existing `"log"` import is a known, unaddressed inconsistency** with CLAUDE.md's "library code (`internal/*`) never logs, it only returns errors" convention. This task did not treat that import as license to widen the pattern — no logging was added to `internal/` anywhere as part of this change. Both `grep -c 'log\.' internal/api/callbackurl.go` and the same check on `internal/storage/storage.go` output `0`. (An early draft of `storage.go`'s comment inadvertently matched the grep pattern by mentioning `log.Fatalf` in prose — reworded to describe the behavior without the literal substring, since the check is a strict textual grep, not a semantic one.)
- **Three, not two, booleans validated at cmd/api startup.** The plan's D-3 table lists `WEBHOOK_ALLOW_INSECURE_HTTP` as read only by `internal/api/callbackurl.go`, never by `cmd/api` for its own purposes — yet `cmd/api` is the *only* process capable of validating it (`.env.example:38-40` states this is API-side only; `cmd/webhook-worker` never reads either `WEBHOOK_ALLOW_*` var). The consolidated startup block therefore reads and validates all three vars `cmd/api` cares about in this sense, even though `WEBHOOK_ALLOW_INSECURE_HTTP`'s parsed value isn't otherwise consumed by `cmd/api` itself — its sole purpose there is surfacing an unrecognized value once, per D-3.

## Deviations from Plan

No substantive deviations — all D-1..D-5 design decisions and the deployment-risk framing were followed as written. One minor in-flight correction, tracked as a Rule 1 fix:

**1. [Rule 1 - Bug] `ParseBool`/`Bool` signatures used named return values, which fail the plan's literal grep acceptance criteria**
- **Found during:** Task 1, verification step
- **Issue:** The plan's `<interfaces>` block shows `func ParseBool(raw string, def bool) (value bool, recognized bool)` with named returns, but the acceptance criteria's grep pattern (`func ParseBool(raw string, def bool) (bool, bool)`) expects unnamed return types. Written as specified in `<interfaces>`, the function failed its own acceptance grep.
- **Fix:** Changed both `ParseBool` and `Bool` to unnamed return types (`(bool, bool)`), keeping the named-parameter documentation (`value`, `recognized`) in the doc comment prose instead of the signature itself.
- **Files modified:** `internal/envcfg/bool.go`
- **Commit:** `a4739c0` (folded into Task 1's single commit; this was caught and fixed before that commit landed)

## Issues Encountered

- **Worktree base drift at start.** The worktree's `HEAD` (`d7ac603`) was an ancestor of the plan's required base commit (`2e229bd`, which itself contains the plan document on `phase-52-quota-enforcement`). Per the `<worktree_branch_check>` protocol, corrected via `git reset --hard 2e229bd` against a confirmed-clean working tree before any task work began.
- **One accidental write outside the worktree.** An early `mkdir -p` for the new package (missing the worktree-prefixed path) created an empty directory in the shared checkout rather than the isolated worktree. Caught immediately (the subsequent `Write` call was refused by the sandbox with an explicit "edit the worktree copy" error), the stray empty directory was removed with `rmdir` (safe: empty, untracked, no git operation), and all subsequent file operations used the correct worktree-prefixed path.
- No other issues. Full suite (`go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./... -count=1` against live Postgres on `localhost:5434`) is green.

## User Setup Required

None for this repository's own deployments — every in-repo boolean env value is already a bare `true`/`false` with no environment change needed. **Operators of hand-edited deployment environments outside this repository should re-check `WEBHOOK_ALLOW_PRIVATE_IPS` and `WEBHOOK_ALLOW_INSECURE_HTTP` for inline `#` comments** — see Deployment Risk above; a value like `WEBHOOK_ALLOW_PRIVATE_IPS=true   # staging` that was silently ineffective before this change now genuinely enables the flag.

## Next Phase Readiness

- All named CodeRabbit findings on open PR #5 are closed (`PR5-CR-BOOLENV`, `PR5-CR-OPENAPI-ERROR`, `PR5-CR-PLANDRIFT`).
- `.planning/STATE.md`'s `Operator Next Steps` now correctly points at `/gsd:plan-phase 53` (Operator Usage Visibility, OPSV-01/02) as the next milestone step.
- No blockers.

## Self-Check

Verifying claims made in this summary before finalizing.

**Files exist:**
- FOUND: internal/envcfg/bool.go
- FOUND: internal/envcfg/bool_test.go
- FOUND: internal/api/callbackurl.go
- FOUND: internal/api/callbackurl_test.go
- FOUND: internal/storage/storage.go
- FOUND: internal/storage/storage_test.go
- FOUND: cmd/api/main.go
- FOUND: .env.example
- FOUND: README.md
- FOUND: docs/openapi.yaml
- FOUND: .planning/phases/52-quota-enforcement-intra-class-fairness/52-01-SUMMARY.md
- FOUND: .planning/phases/52-quota-enforcement-intra-class-fairness/52-02-PLAN.md
- FOUND: .planning/ROADMAP.md
- FOUND: .planning/STATE.md

**Commits exist:**
- FOUND: a4739c0 (Task 1)
- FOUND: 32f5296 (Task 2)
- FOUND: 9273f7c (Task 3)
- FOUND: d5d5933 (Task 4)

**Whole-repo sweep (all green):** `go build ./...` → clean; `go vet ./...` → clean; `gofmt -l .` → empty; `go test ./... -count=1` (live Postgres on `localhost:5434`) → all `ok`, no skips other than the pre-existing MinIO-gated `TestRoundTrip`.

**Finding-specific isolated proofs:**
- `go test ./internal/envcfg/ -count=1 -v` → 2 top-level PASS, 18 subtest PASS lines (behavior table fully covered)
- `go test ./internal/api/ -run 'TestIsBlockedIPAllowPrivateInlineComment|TestIsBlockedIPRejectsUnrecognizedFlagValues|TestValidateCallbackURLInsecureHTTPInlineComment' -count=1 -v` → 3/3 PASS (security proof)
- `go test ./internal/storage/ -run TestNewRejectsUnrecognizedUseSSL -count=1 -v` → PASS, with the docker stack's live MinIO not required (dummy S3_* values)
- `go test ./internal/api/ -run 'TestIsBlockedIP$|TestIsBlockedIPAllowPrivate$|TestValidateCallbackURL$|TestValidateCallbackURLAllowPrivate$' -count=1` → PASS (pre-existing assertions unweakened)
- `git diff -- internal/api/callbackurl_test.go internal/storage/storage_test.go | grep -c '^-[^-]'` → 0 (test edits are pure additions)
- `npx --yes @redocly/cli@latest lint docs/openapi.yaml` → 0 errors, 2 pre-existing warnings (info.license, healthz missing 4XX), unchanged
- `git status --porcelain -- .planning/ | grep -v "260803-tm4"` → exactly 4 lines (ROADMAP.md, STATE.md, 52-01-SUMMARY.md, 52-02-PLAN.md); `.planning/REQUIREMENTS.md` untouched

## Self-Check: PASSED
