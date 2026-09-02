---
phase: 59-born-digital-pdf-routing
plan: 06
subsystem: worker, errcode, convert
tags: [error-codes, vocabulary, doc-sync, pinning-protocol, inversion-proof]

requires:
  - plan: 59-04
    provides: "The route that made the code unproducible, and the test proving no path refuses a born-digital PDF"
  - phase: 56-jobrunner-consolidation-structured-error-codes
    provides: "The frozen failure-code vocabulary, doc_sync_test.go, and the pinning suite whose protocol this plan had to satisfy"
provides:
  - "worker.HistoricalFailureCodes() — codes no longer producible but still served"
  - "docs/ERROR-CODES.md historical section, under the same mechanical parity test"
  - "Disjointness invariant between the active and historical vocabularies"
affects: []

tech-stack:
  added: []
  removed: []
status: complete
---

# Plan 59-06 — Summary

Commit `18e8bdb`. Closes BDPD-02's contract half.

## Retirement is a move, not a deletion

Nothing can produce `born_digital` since 59-04. But `jobs.error_code` is
persisted and `GET /v1/jobs/{id}` serves whatever was written when the job
failed — so deleting the code from the contract would have fixed one inaccuracy
by creating another: the document would stop describing a value the API
demonstrably still returns.

| List | Count | Meaning |
|---|---|---|
| `AllFailureCodes()` | 13 | what this build can produce |
| `HistoricalFailureCodes()` | 1 | what it cannot, but clients may still receive |

Three tests hold it together: parity for each list against the document, and
**disjointness** — so a half-finished retirement (added to one list, never
removed from the other) cannot pass.

The document parser needed no teaching. `extractTableCodes` stops at the next
`## ` line, so a new section is invisible to the active check **by
construction** rather than by careful placement.

## The pinning row, and why the justification is in the file

`internal/worker/pinning_test.go` declares any assertion change a protocol
violation requiring explicit justification. The protocol is scoped to plan
56-04's refactor, so this edit is not literally covered by it — **and that
scoping is exactly why the justification is written into the file rather than
treated as permission.** "Out of scope by design" is itself a claim; a suite
whose rows can be removed on an unstated claim has stopped being a defence.

Diff: **one deleted row, 41 lines of explanation.** Nothing else in the file.

## The proof outgrew the test that established it

`ErrOCRBornDigital` is deleted outright — and that broke
`TestBornDigitalSentinelIsUnreachable`, which named it. The test could not
survive its own success: a test referencing a deleted identifier does not
compile.

That is the stronger outcome, not a problem. Unreachability is now enforced by
the **compiler** rather than by a passing assertion. What remains is
`TestBornDigitalHasNoRefusalPath`, checking the message rather than the type —
deliberately weaker in isolation, and the right weaker check: a refusal
reintroduced under a *new* sentinel would not have been caught by `errors.Is`
against the old one either.

Both citations of the old name (in `pinning_test.go` and `descriptors.go`) were
updated. A justification pointing at a test that no longer exists is worse than
none.

## Inversions

| Inversion | Red |
|---|---|
| `born_digital` left in the **active** list as well | `TestErrorCodesDocMatchesGoConstants`, `TestActiveAndHistoricalFailureCodesAreDisjoint`, `TestBornDigitalIsRetiredNotDeleted` — **3** |
| Its row deleted from the document's historical section | `TestHistoricalCodesDocMatchesGoConstants` — **1** |

## Gate

| Check | Result |
|---|---|
| `go build` / `go vet` / `gofmt -l` | clean |
| `go test ./...` | green, 30 packages |
| `grep -r 'ErrOCRBornDigital' internal/` | only comments recording the history |
| `git diff --stat internal/worker/pinning_test.go` | 41 insertions, 1 deletion |
| `TestPre59GoldenInspectPDF` | unchanged |

## Note for a future retirement

The active/historical split is now general machinery, not a one-off. The next
code to be retired moves between two lists and gains a documented row; the
disjointness and parity tests cover it without modification. That was the point
of introducing the distinction under test rather than at the first incident.
