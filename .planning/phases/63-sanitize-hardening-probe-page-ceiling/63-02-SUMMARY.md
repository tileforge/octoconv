---
phase: 63-sanitize-hardening-probe-page-ceiling
plan: 02
subsystem: sanitize
tags: [probe, bounds, admission, terminal-classification, error-codes]
requires: [63-01]
provides: [PROBE_MAX_PAGES, convert.EnforceProbeMaxPages, convert.ErrProbeTooManyPages, errcode.TooManyPages, inspect.PDFPageCount]
affects: [internal/api, internal/convert, internal/inspect, internal/worker, internal/errcode, cmd/api, cmd/probe-worker, deploy/chart, docker-compose]
tech-stack:
  added: []
  patterns: [one-bound-two-gates, cheap-gate-may-abstain, inversion-proven-boundary, shared-default-constant]
key-files:
  created:
    - internal/convert/probebounds.go
    - internal/convert/probebounds_test.go
    - cmd/probe-worker/main_test.go
  modified:
    - internal/inspect/pdf.go
    - internal/inspect/bindings.go
    - internal/errcode/errcode.go
    - internal/errcode/doc_sync_test.go
    - internal/api/handlers.go
    - internal/api/api.go
    - internal/api/handlers_test.go
    - internal/convert/sanitizepdf.go
    - internal/convert/sanitizeconverter.go
    - internal/worker/descriptors.go
    - internal/worker/pinning_test.go
    - cmd/api/main.go
    - cmd/probe-worker/main.go
    - .env.example
    - docker-compose.yml
    - deploy/chart/octoconv/templates/configmap.yaml
    - deploy/chart/octoconv/values.yaml
    - docs/openapi.yaml
    - docs/ERROR-CODES.md
    - docs/SANITIZE.md
key-decisions:
  - "PROBE_MAX_PAGES=200 is not a new number: it is the 200 in PROBE_ENGINE_TIMEOUT's own ceil(2.0 s/page x 200 pages x 1.5)=600s derivation, so the ceiling and the timeout describe one document bound"
  - "The plan's arithmetic cited a 180s timeout and a ~90-page ceiling; the measured timeout is 600s and the project's own todo already named 200. Deviated to the measured number"
  - "The API gate reads the count through file's io.ReaderAt rather than a peek re-stitched onto the stream — the same pattern the zip-bomb guard already uses, and the only one that cannot truncate the upload"
  - "The page ceiling is enforced in SanitizeConverter.Convert, not inside SanitizeFile: the bound is configuration held by the converter, and SanitizeFile takes none"
  - "page_limit binds to errcode.TooManyPages in RejectBindings, NOT to a finding code — a long PDF carries no risk, unlike the pixel-bomb shape dimension_limit binds to"
  - "values.yaml documents the ceiling and does not expose it as a knob: no engine bound in this chart is a values.yaml value"
requirements-completed: [PROBE-01, PROBE-02]
duration: ~2h
completed: 2026-09-02
---

# Phase 63 Plan 02: Probe Page Ceiling Summary

`PROBE_MAX_PAGES=200` — one bound, enforced at admission (before the S3 write,
`422 too_many_pages`) and again in the worker (accurate pdfcpu count, terminal,
before the first ghostscript pass), through one exported comparison and one
shared default constant, with every boundary proven by inversion.

Tasks: 6/6. Files: 3 created, 20 modified. Commits: 4.

## What was built

**The bound** (`internal/convert/probebounds.go`). `DefaultProbeMaxPages = 200`,
`ErrProbeTooManyPages`, `EnforceProbeMaxPages(count, max)` with `max == 0`
meaning "not configured" — `ocrguards.go`'s own zero-value convention. The
error carries both numbers so an operator can tell "raise the ceiling" from
"split the document".

The sentinel is deliberately **not** `ErrOCRTooManyPages`. The two engines
report the same client-facing code on purpose; a shared error *value* would
have handed the probe path OCR's terminal classification and OCR's message on
a bound that is a different number from a different measurement.

**The admission gate** (PROBE-01, `internal/api/handlers.go`). Only for
`operation=sanitize` on a PDF, after the engine class is known and strictly
before `s.storage.Upload`. The count comes from a new narrow entry point,
`inspect.PDFPageCount`, rather than `AnalyzePDF` — which would also run seven
risk-token scans and the text-layer detector (the most expensive consumer of
the budget, since it inflates stream bodies) for an answer nobody asked for
here. pdfcpu is not called from `cmd/api`; phase 58 D-02a refused that and this
gate is a bounded byte scan precisely so it is allowed to be wrong.

An **unknown** count admits. That is the design, not a gap, and it is pinned by
a test so a later fail-closed change is a contract change rather than a fix.

**The worker gate** (PROBE-02, `internal/convert/sanitizepdf.go`,
`sanitizeconverter.go`, `internal/worker/descriptors.go`). pdfcpu counts the
pages before the first ghostscript pass — permitted here for the three
conditions `sanitizeverify.go`'s header already records. `isProbeTerminal`
returns true for the ceiling and `probeFailureCode` maps it to the existing
`CodeTooManyPages`.

**The vocabulary.** `errcode.TooManyPages` is the second code shared across the
sync and async vocabularies after `invalid_options`, with its own pin test so a
failure names which code drifted. `docs/ERROR-CODES.md`, `docs/openapi.yaml`
and `docs/SANITIZE.md` follow.

## Verification

All gates run against live Postgres + Redis + MinIO:

| Gate | Result |
|------|--------|
| `gofmt -l cmd internal` | clean |
| `go vet ./...` | pass |
| `go build ./...` | pass |
| `go test ./... -count=1` | all packages ok |
| `scripts/ci-helm-checks.sh` | pass (all non-zero exits are its own negative cases) |
| `yq` over `docs/openapi.yaml`, `docker-compose.yml` | parse |

### Inversions (D-05 — every new boundary fails when removed)

Recorded verbatim at `evidence/63-02-inversions.txt`. Each removal fails the
intended test and the message names what was removed:

| # | Boundary removed | Failing test | Message |
|---|------------------|--------------|---------|
| 1 | Gate moved BELOW `s.storage.Upload` | `TestCreateJobSanitizeRejectsOverPageCeilingBeforeStorage` | names the storage key the double was called with |
| 2 | `ErrProbeTooManyPages` branch of `isProbeTerminal` | `TestPinnedProbePageCeilingAndTimeoutAreOpposites` | `want errors.Is(err, asynq.SkipRetry)` |
| 3 | Ceiling call removed from `SanitizeConverter.Convert` | `TestSanitizeConverterEnforcesPageCeilingBeforeGhostscript` | `Convert = <nil>, want an error wrapping ErrProbeTooManyPages` |
| 4 | `cmd/api` given its own `200` literal | `TestProbeMaxPagesHasOneDefault` | names `cmd/api/main.go` |
| 5 | Unknown count made fail-closed | `TestCreateJobSanitizeAdmitsUnknownPageCount` | `status = 422, want 202` |

## Deviations from Plan

**[Rule 1 — wrong input] The plan's derivation used a stale timeout.** Task 2
computed the ceiling from "`PROBE_ENGINE_TIMEOUT` is 180 s, so the timeout
alone admits roughly 90 pages". The measured value is **600s** (phase 60,
committed the same day, `PROBE_ENGINE_TIMEOUT=600s`), and its own derivation in
`.env.example` is `ceil(2.0 s/page × 200 pages × 1.5)`. The project's todo
`2026-09-02-probe-has-no-page-ceiling.md` already said "200 matches the
timeout's derivation". A 90-page ceiling would have refused work the
measurement *proved* the service does (240 pages completed in 481s). Took 200.
The two stale comments that still said 180s/"PLACEHOLDER" (`internal/api/api.go`,
`cmd/api/main.go`, both beside a value already reading 600s) were corrected in
the same edit — a derivation citing a number its own file contradicts is worse
than no comment.

**[Rule 2 — better mechanism available] The API gate uses `ReadAt`, not a
peek.** The plan specified a bounded peek re-stitched onto `rest`, copying
`convert.Dimensions`. `handleCreateJob` already contains the better pattern for
this exact need — the zip-bomb guard reads through `file` (`io.ReaderAt`) with
the stated reason that `ReadAt` never disturbs Sniff's cursor. A peek can be
wrong in one direction (bytes consumed and not returned) and that direction
silently truncates the client's upload; `ReadAt` cannot make that mistake.
The plan's byte-identity requirement is still asserted, on a fixture
deliberately longer than the scan window.

**[Rule 2 — placement] The worker gate lives in `SanitizeConverter.Convert`,
not inside `sanitizePDF`.** The bound is configuration, held by the converter
`cmd/probe-worker` replaces at startup (exactly `OCRConverter.MaxPages`'s
shape). `SanitizeFile` takes no configuration, and threading it through would
have changed that function's signature at twenty test call sites, none of which
are about the ceiling. Every production path reaches the sanitizer through
`Convert`. Scoped by `sanitizePageCeilings`, a one-entry map, because the
per-page cost model is a fact about ghostscript rasterizing a PDF — OOXML and
image sanitization have no per-page cost, and phase 60 established that
inventing one without a measurement is not worth doing.

**[Rule 3 — missing prerequisite found by an existing gate] `RejectBindings`
needed the new reason.** `TestRejectBindingsCoverEveryContentValidationReason`
failed on the first full run: every `reason=` token logged by a "content
validation rejected:" line must have a D-04 binding. Added `page_limit` as an
**errcode** binding rather than a finding — the contrast with `dimension_limit`
is the substance: huge declared pixel dimensions *are* a decompression-bomb
shape, so the number itself is the risk signal; a 300-page PDF carries no risk
at all and binding it as a finding would put a risk claim in a report about a
file that has none.

**[Rule 2 — chart convention] `values.yaml` documents the ceiling instead of
exposing it.** The plan asked for `PROBE_MAX_PAGES` in `values.yaml`. No engine
bound in this chart is a values.yaml knob — `OCR_MAX_PAGES` and
`PROBE_ENGINE_TIMEOUT` both live only in the ConfigMap template, because they
are derived numbers with their derivations beside them, not per-install
preferences. Inventing a knob for one variable would have been a new pattern
with one member. `values.yaml` gets the cross-reference on `probeWorker`, where
`terminationGracePeriodSeconds` already cites the same chain.

**[Addition — not in the plan] `docker-compose.yml`.** Not in `files_modified`,
but the api and probe-worker services are where a developer's local stack reads
its config, and a bound present in the chart and absent from compose is a bound
that does not exist locally. Added to both services only (matching
`OCR_MAX_PAGES`'s per-service placement, not `PROBE_ENGINE_TIMEOUT`'s IN-02
replication — `PROBE_MAX_PAGES` is not a `uniqueTTL` input).

**Total deviations:** 6 (1 wrong-input correction, 3 mechanism/placement,
1 prerequisite found by an existing gate, 1 addition).
**Impact:** the ceiling is the number the timeout was already derived from
rather than one that contradicts it; the admission read cannot truncate an
upload; no new chart pattern was introduced for a single variable.

## Anti-drift, which this plan owed extra

63-CONTEXT.md D-04 puts one rule in two places on purpose — the shape this
project has been burned by four times — so the plan made the holding test
non-optional. Two tests hold it, from the two directions it can break:

* **Configuration** — `TestProbeMaxPagesHasOneDefault` scans every non-test Go
  file that *reads* `"PROBE_MAX_PAGES"` and requires each to resolve its default
  through `DefaultProbeMaxPages`. This is aimed at the failure that actually
  happened to this engine's other bound: two literals, silently diverging.
* **Behaviour** — `TestBothProbeGatesResolveOneBound` drives the real API
  handler and `convert.EnforceProbeMaxPages` over `max-1, max, max+1, 3000` and
  fails if they ever disagree at the boundary.

## Review round 1 (PR #15, codex) — three findings, all confirmed by running

**P1 — the ceiling refused the documents sanitize exists for.** The worker
counted pages with `api.PageCountFile`, which performs full pdfcpu validation,
and pdfcpu's validator rejects several of the constructs this operation removes.
Measured on the repository's own `threat_all_classes.pdf`: `validation error
(obj#:12): document catalog: catalog AA: additional action rootDict.AA: action O
not allowed for source root`. With `PROBE_MAX_PAGES` set, every threat-carrying
PDF would have failed terminally as malformed instead of being disarmed.

**The suite stayed green because of this plan's own deviation.** The gate lives
in `SanitizeConverter.Convert` (deviation 3 above), while nearly every sanitize
test calls `SanitizeFile` directly and bypasses it. The placement is still the
right call for the reasons given — but it moved the tests off the path, and
nothing here noticed.

**P2 — and the count was not cancellable.** `api.PageCountFile` takes no
context, so the comment claiming `PROBE_ENGINE_TIMEOUT` bounded it was false; at
concurrency 1 the worker is held past its own deadline.

Both fixed by one change: count with `pdfinfo` through `runCommand`. It reports
the same fixture as 1 page, it is a subprocess so the process-group kill applies,
and it is the mechanism `ocr-worker` already uses — two engines answering one
question two ways is how this got in.

**P3 — the admission gate could reject on payload.** The risk-token scan always
filtered through the opaque-range set; the page-count walk did not. A two-page
PDF with `/Type /Pages /Count 300` in a content stream measured as 300 and would
have been refused. Fixed structurally (both the marker and the `/Count` position
are range-checked, and a ceiling-hit range scan abstains), plus the gate now
refuses the last-wins tie-break `AnalyzePDF` takes.

**The inversions then found a bad test rather than bad code.** Inversion 7 passed
with the opaque filtering removed entirely: the test accepted an abstention as
success, and without filtering the walk sees both counts, they disagree, and the
unambiguity rule abstains — two mechanisms masking each other with neither under
test. Requiring a definite 2 isolates the filtering, and is also what the gate
needs: if every PDF with a text-drawing stream abstained, it would refuse
nothing.

Round-2 inversions (6, 7, 8) recorded in `evidence/63-02-inversions.txt`.
Fixed in `4d11d50`.

## Self-probe between rounds — the fix for P1 introduced two misattributions

Run on the standing rule that a fix is not evidence, before the next review
round arrived. The pdfinfo counter wrapped every `runCommand` error as
`ErrSanitizeMalformedInput`, so two failures that are **ours** became terminal
failures blamed on the client's document: a cancelled or timed-out count (the
exact opposite of the polarity `ProbeDescriptor` argues for a timeout — and the
existing "a deadline stays transient" test injects at `processFn`, so this new
path ran straight past it), and a missing `pdfinfo` binary.

Fixed by routing through the classifier that already existed rather than the
second one the counter had grown. `classifyGSFailure` → `classifyEngineFailure`:
nothing in the rule was ever ghostscript-specific, and a second copy of a
classification rule is how P1 got in. It gained a could-not-start branch, the
same distinction `internal/inspect` draws between `ReasonReadFailed` and
`ReasonMalformedInput`. `pdfinfo`'s presence in the image is now recorded from
the built image (`/usr/bin/pdfinfo`, poppler 22.12.0), not inferred from the
Dockerfile line. Fixed in `9fd7cb6`; codex independently reported the same
defect minutes later.

## Review round 2 (codex) — the bound was replaced, not patched

`findCountAfter` took the first `/Count` within 512 bytes after the marker. That
is proximity, not structure, and it had now been bypassed twice: by a stream
payload (round 1) and by `<< /Count 2 /Type /Pages ... >>` followed by an
`/Outlines` dictionary carrying its own `/Count` — key order in a PDF dictionary
is arbitrary, so the forward search stepped over the page tree's own count.
Measured: a valid two-page PDF reported as 300.

`RETROSPECTIVE.md` records the rule from a different bound in this repo that
took four bypasses to learn. `pagesDictCount` now finds the dictionary the
marker is a direct key of and accepts a `/Count` only at that depth.

**Binding to a dictionary needed a ceiling of its own.** Resolving one per
marker turns a per-marker window into a sum over however many markers a file
carries; measured 0.02s → 5.02s on the package's dense-marker test. Two bounds
because they are two resources: `pdfDictScanWindow` for one lookup,
`pdfDictScanBudget` for their sum, with exhaustion reported as
`work_ceiling_bytes` like every other ceiling here. Package tests: 2.8s before
the phase, 35s at the first version of this fix, 1.03s now. Fixed in `f8b1768`.

## Review round 3 (codex) — the byte scanner was deleted

Two findings, both reproduced (300 instead of 2): `/Count300` matched as the key
`/Count` by prefix, and `/Extra [ /Count 300 ]` read as a direct key.

`internal/convert/pdflex.go`'s own header already lists "`/Length` matched
inside `/Length1`" and a "`/Length` inside `[ ... ]` read as a key of the
enclosing dictionary" (PR #14 round 4). Those are these two findings, one key
name over. **The lesson was already written down in this repository, and the
byte scanner in `internal/inspect` repeated it anyway** — because it had no
model of what a token is.

Four bypasses of one bound in three rounds (stream payload, neighbouring
dictionary, prefixed name, array value), so the derivation moved rather than
gaining a third and fourth comparison. `convert.PDFTypedDictUints` answers the
question on the tokenizer that models PDF's four lexical contexts, whole-name
tokens, array nesting and stream payloads. `internal/inspect` loses
`pagesDictCount`, `enclosingDictStart`, `parseUintAfter`, both dictionary-scan
bounds, and the opaque-range filtering for this walk; the per-marker
amplification the budget existed for is gone by construction.

**This crossed a documented line and was escalated rather than decided.** Phase
58 D-02a refused a parser on untrusted bytes in the API process. That decision
was about pdfcpu — a large third-party dependency that builds a document model;
this is a bounded tokenizer of this project's own. The operator was shown the
three options (use the tokenizer / patch twice more / drop the admission gate
and change PROBE-01) and chose the first. Fixed in `09e85b2`.

## Self-probe of the tokenizer — an indirect /Count read as the object number

Run before the next round arrived, against the classes `pdflex.go`'s header
enumerates. Six probes, five already correct (a comment inside the dictionary, a
hex-string value spelling the key, `/Type` given as an array, a nested Pages
dictionary, a stray `>>`), one real: `/Count 5 0 R` returned **5**, the object
number. `pdfLooksLikeRef` was already applied to `/Length` in the same function
and had not been applied to the key's own value. Fixed in `e0d8a6c`; codex
reported the same defect in round 4.

## Review round 4 (codex) — one of the three was introduced by this PR

**P1: 113 MB per request for a value that was discarded.** When the walk moved
to the tokenizer, the opaque-range set stopped being needed — and the call that
builds it was left behind for its ceiling bit, slice thrown away. Measured on an
8 MiB comment-heavy input: **113,202,766 B and 35 ms per call**; after removal,
**8,388,768 B and 6.2 ms**. The API pod is limited to 1 GiB and this route holds
no concurrency semaphore, unlike `POST /v1/inspect`. Pinned by a test asserting
allocated bytes, not elapsed time.

**P2: a payload decoy outranked the xref.** The object index was "scan first,
fill gaps from the xref". `scanPDF` resynchronises to the next `endstream` when
a `/Length` is unresolvable, so a payload carrying an early `endstream` makes it
resume inside the payload where a fake object header can wait — and the
precedence then refused to let the file's own declared structure replace it. The
xref now overwrites. Fixed in `a380eff`.

## Four rounds of inversions proved nothing until they were themselves fixed

The most useful finding of this plan, and worth stating as a ratio: **eight
inversions were written across four rounds, and five of them passed against
deliberately broken code.** In every case the test was wrong, not the fix.

- **Inversion 7** accepted an abstention as success; with the opaque filtering
  removed, two mechanisms masked each other and neither was under test.
- **Inversion 10** failed to COMPILE rather than failing its test (orphaned
  imports).
- **Inversion 12** timed the call and allowed two seconds — a threshold loose
  enough to be stable in CI was too loose to isolate the boundary, since the
  marker walk's own cap already kept the unbudgeted case under it.
- **Inversions 13, 14 and 15** all passed with their property removed: every
  fixture put its trap BEFORE the dictionary's real `/Count`, copied from how
  the findings read against a scanner where the first forward match won. The
  tokenizer keeps the last value, so the real key overwrote the trap and the row
  passed regardless.

- **Inversion 17** took three attempts. The first fixture had no early
  `endstream`, so the resync jumped the payload whole and never read the decoy;
  the second placed the decoy BEFORE the real object, which `scanPDF`'s
  last-write-wins index then overwrote. Both passed against broken code. Only
  the decoy placed AFTER makes the two indexes disagree. Resolved by printing
  what the index actually held rather than reasoning about what it should hold.

A fifth near-miss belongs in the same list: the round-3 stream row was caught by
the tokenizer's *string* handling, not by its payload jump, so it proved a
different property than the one it named. Only a bare dictionary inside a
payload isolates that boundary.

## Two inversions proved nothing until they were fixed

Worth recording separately, because in both cases the test was wrong rather than
the code:

- **Inversion 10** failed to COMPILE instead of failing its test (orphaned
  imports). Redone with the imports dropped.
- **Inversion 12** passed with the budget removed: it timed the call and allowed
  two seconds, and the marker walk's own iteration cap keeps the unbudgeted
  worst case under that. A threshold loose enough to be stable in CI was too
  loose to isolate the boundary. It now asserts the reason
  (`work_ceiling_bytes` vs `malformed_input`), which is deterministic.

Together with round 1's inversion 7 — which passed because the test accepted an
abstention as success while two mechanisms masked each other — that is three
inadequate tests found by inverting rather than by reading.

## Issues Encountered

None outstanding.

## Next Phase Readiness

Phase 63 is complete: 63-01 (merged, PR #14) and 63-02. Ready to ship — PR,
`@codex` review, then phase 64 (tectonic, the ninth engine class).

## Self-Check: PASSED

- Every file in `key-files.created` exists on disk.
- `git log --oneline --grep="63-02"` returns 4 commits.
- Every task's verification re-run after the last edit: full suite green.
- Every plan-level gate re-run: gofmt / vet / build / test / helm / yq all pass.
- All five inversions re-run and their output recorded.
