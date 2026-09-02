---
created: 2026-08-18T02:10:00.000Z
title: Inspect honest-reporting — three open items from the PR #9 review loop
area: inspect
files:
  - internal/inspect/report.go (buildCoverage, ~line 220)
  - internal/inspect/status.go (Coverage.MarkTruncated, ~line 177)
  - internal/api/inspect_handlers.go (detectInspectFormat, the bool-returning detectors)
  - internal/convert/markupsniff.go, internal/convert/olecfb.go (LooksLike*/IsOLECFB signatures)
  - docs/openapi.yaml (InspectReport.coverage description)
origin: codex review rounds 20-27 on PR #9 (phase 58)
resolves_phase: 62
---

## Why this file exists

Rounds 20-27 of the PR #9 review were one defect class in eight costumes: a
check that examined less than its verdict claimed, and published the optimistic
answer for the part it never examined. Every reported instance is fixed
(`aeb0768` through `337add1`). What remains are the items that are NOT
patchable inside a review round, because each is a contract or API decision.
They are recorded here so the phase can proceed to its live gate instead of
another round of the same conversation.

## Item 1 — Does `read_failed` make a report's coverage incomplete?

**Today it does not.** `buildCoverage` marks `Coverage.Complete = false` only
for the two ceiling reasons, on D-02a's stated rule: "structural blindness
about the FILE never makes the REPORT selective; only exhausting THIS SERVICE'S
OWN byte/time budget does". A read failure sits on neither side of that line as
written — it is not blindness about the file, it is our own operational failure.

**Consequence:** a response can carry `coverage.complete: true` while every
check in it says `unknown/read_failed`. "I inspected this fully" and "I could
not read it", in one body. True of all four read_failed paths (zip, image,
media, and the sniff-chain path added in `6a534a4`).

**Why it was not fixed in the loop:** it widens `Coverage.Reason`'s vocabulary
beyond ceiling reasons, `MarkTruncated` silently drops non-ceiling reasons today
and would have to widen with it, and `docs/openapi.yaml`'s coverage description
would have to match. Note the mechanism used for the malformed-zip path in
PR9R13 — marking the tracker — is NOT reusable here: it would attribute an I/O
failure to `work_ceiling_bytes`, which is the very misattribution this phase
spent rounds 9-16 removing.

**Decide deliberately, either way:**
- read_failed joins the ceiling reasons in making coverage partial, carrying its
  own reason value; or
- the docs state plainly that `complete` means "no ceiling was hit", not
  "everything was inspected", and the field is renamed or re-described
  accordingly.

## Item 2 — Detectors that cannot report a read failure at all

`detectInspectFormat`'s chain now carries read failures from all three sniffers
that CAN report one (`convert.Sniff`, `SniffVideo`, `SniffAudio` — each returns
`("", rest, nil)` for an unrecognized upload, so a non-nil error is
unambiguous). The remaining detectors return a bare `bool`:

- `convert.LooksLikeHTML(file, size)`
- `convert.LooksLikeLaTeX(file, size)`
- `convert.LooksLikeText(file, size)`
- `convert.IsOLECFB(file)`

A read failure inside any of them is indistinguishable from "not this format",
so an upload whose temp file dies during exactly those reads still reports
`format_unrecognized` rather than `read_failed`. The same class as round 27's
finding, one API layer down.

**Why it was not fixed in the loop:** each signature is shared with the
conversion path (`internal/api/handlers.go`), where the bool is all the caller
wants. Options: return `(bool, error)` and update both callers; or give inspect
its own wrapper that pre-reads the window once and hands the bytes to the
matchers (which would also fold these reads into the operation's byte
allowance — see the round-23 sweep). The second is probably better and is
strictly larger than a review-round patch.

## Item 3 — CI has never run for this branch

Every CI run since 2026-08-16 20:49 fails in ~2s with no step logs and the
GitHub annotation "The job was not started because recent account payments have
failed or your spending limit needs to be increased". The `helm` job (no Go code
involved) fails identically, and `race`, `e2e` and `docker-build` have never
executed at all for any of the round 20-27 commits.

Local gates are the ONLY evidence behind every one of those commits:
`gofmt -l .`, `go build ./...`, `go vet ./...`, `go test ./...` (25 packages),
`go test -race` on the touched packages, and `redocly lint docs/openapi.yaml`.

**This blocks more than review comfort:** phase 58's own INSP-06 live gate
(plan 58-08, wave 5) is an operator-run proof, and the phase should not close
claiming CI-verified isolation while CI has not run since before this work
began. Needs an account-level fix in GitHub Billing & plans — nothing in this
repo can resolve it.

**Status 2026-08-18 — billing returns at the END of August, so this stands for
about two weeks.** `scripts/ci-local.sh` now runs the same tiers locally and
records a transcript under `.planning/ci-local/ci-local-<RUN_ID>.log`. All five
pass on `b68c9da`:

| tier | result | caveat |
|---|---|---|
| helm | PASS | local helm v4.2.4 vs the v4.2.3 `ci.yml` pins — a different renderer; the script warns |
| gate | PASS | — |
| race | PASS | the race detector's first execution on this branch since 2026-08-16 |
| docker-build | PASS | all 10 bake targets, built for darwin/arm64 rather than ubuntu-latest |
| e2e | PASS | full compose stack + migrations + `internal/e2e` in 127s |

The live gate (wave 5) needs no GitHub at all — it runs against OrbStack from an
in-cluster pod — so the phase is NOT blocked for those two weeks.

**Still owed when Actions returns:** push and let the real pipeline run. The
local transcript is a substitute under duress: host architecture, a different
helm patch version, and none of CI's own caching or artifact mechanics. The
phase's closing report must say "CI unavailable since 2026-08-16 for billing;
equivalent jobs run locally on `<commit>`, transcript `<file>`, with these
caveats" — never "CI green".
