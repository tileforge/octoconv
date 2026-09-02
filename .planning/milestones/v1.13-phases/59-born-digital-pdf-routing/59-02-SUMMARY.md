---
phase: 59-born-digital-pdf-routing
plan: 02
subsystem: inspect
tags: [coverage, honesty, read-failed, doc-sync, inversion-proof]

requires:
  - phase: 58-synchronous-inspection
    provides: "Coverage/MarkTruncated, the closed Reason vocabulary, D-02a's structural-vs-load distinction, doc-sync test machinery"
  - plan: 59-01
    provides: "Golden pin of the inspect report — the baseline this change had to leave undisturbed"
provides:
  - "makesReportIncomplete: the single closed declaration of which reasons lose coverage"
  - "read_failed makes a report selective, reported verbatim, with its code in truncated_checks"
  - "Reason table enforced from both sides — prose and Yes/No column"
affects: [59-05]

tech-stack:
  added: []
  removed: []
status: complete
---

# Plan 59-02 — Summary

Commit `f03a704`. Closes the first folded todo (`2026-08-18-inspect-honest-reporting-open-items.md`, item 1).

## The defect, reproduced

`buildCoverage` marked a report selective for exactly two reasons, and
`MarkTruncated` actively *defended* that by discarding any other reason as a
no-op. So a response in which every check said `unknown/read_failed` shipped
`coverage.complete: true` — the report claimed to have examined in full an
upload it could not read at all.

`TestBuildReportAllChecksReadFailedIsNotComplete` is that exact body.

## What changed, and what deliberately did not

`read_failed` joins the two ceilings. What unites the three is not fault but
**evidence**: part of the upload was never examined. A bigger budget fixes the
ceilings and not the read failure — but the coverage claim never asked what
would have fixed it.

**`malformed_input` stays out, and that exclusion is the load-bearing half.**
"This file is broken" is a finished answer *about a file the analyzer looked
at*; `read_failed` means nothing was seen. The two sound alike and are opposite
in exactly the way this field cares about. Widen the set further and
`complete: false` starts firing for any PDF whose objects live in an `/ObjStm` —
the common case — and the word stops carrying information.

Final rule: **three of ten reasons** lose coverage.

## Three declarations became one

The set had been written out three times — in `MarkTruncated`, in
`MarkTruncatedByCharacteristic`, and inline in `buildCoverage`. That is how the
codebase came to have one gate admitting a reason and another dropping it.
`makesReportIncomplete` is now the only declaration, and it is **allow-shaped**:
a list you add to fails safe; a list you must remember to subtract from fails
open — toward claiming completeness the service does not have.

`read_failed` reaches the client **as itself**. D-11 rejects the PR9R13
tracker-marking mechanism here precisely because it would attribute an I/O
failure to `work_ceiling_bytes`.

### One decision the plan left open

`TruncatedChecks` — a `read_failed` code **does** go in. `Complete:false` beside
an empty list tells a client the report is selective while refusing to say about
what. `docs/openapi.yaml`'s description was widened to match rather than left
describing only the ceiling case.

## Inversion — run twice, because the change has two failure directions

| Inversion | Red tests |
|---|---|
| **Too narrow** — drop `read_failed` from the predicate | `TestMarkTruncatedAdmitsExactlyTheCoverageLosingReasons`, `TestMarkTruncatedByCharacteristicUsesTheSameGate`, `TestBuildReportAllChecksReadFailedIsNotComplete`, `TestBuildReportCharacteristicReadFailedIsNotComplete` — **4** |
| **Too wide** — also admit `malformed_input` | `TestMarkTruncatedAdmitsExactlyTheCoverageLosingReasons`, `TestMarkTruncatedByCharacteristicUsesTheSameGate`, `TestBuildReportStructuralReasonsStillKeepCoverageComplete` — **3** |

A single inversion would have demonstrated only half of it. The second one is
the interesting one: it proves the tests defend the *narrowness* of the set, not
merely its membership.

The expectation table is independent of the implementation — a test that asks
`makesReportIncomplete` what it does and then agrees proves nothing. It is a map
over the whole vocabulary, and `TestEveryDeclaredReasonHasACoverageExpectation`
fails on any reason `AllReasons()` declares that the map does not mention, so an
eleventh reason cannot arrive with nobody having decided its side.

## Documentation enforced from both sides

- `TestDocDeclaresSelectiveReasons` — the sentence a client actually reads.
- `TestDocMarksExactlyTheSelectiveReasons` — the Yes/No column, parsed per row,
  rejecting any cell that is neither.

Neither is redundant: prose can be updated while the table rots, or the reverse.
The previous version of that section said *"Only the last two reasons"* and
stayed there while the code moved — the same drift that produced
`TestReasonsDocMatchesGoConstants` in the first place.

## Gate

| Check | Result |
|---|---|
| `go build ./...` / `go vet ./...` / `gofmt -l` | clean |
| `go test ./...` | green, 30 packages |
| `TestPre59GoldenInspectPDF` | **unchanged** |
| `yq` on `docs/openapi.yaml` | parses; `InspectCoverage.reason` names all three |
| `git diff go.mod go.sum` | empty |

The untouched golden matters: all four PDF fixtures are small, valid and
readable, so the widening caught nothing it was not meant to catch. Had that pin
moved, the change would have been over-broad and the pin would have said so.

## Handoff to 59-05

`TestBuildReportCharacteristicReadFailedIsNotComplete` exercises the
`<name>_reason` characteristic path with a non-ceiling reason for the first
time. That is the exact mechanism `text_layer_reason` will travel — 59-05 now
depends on a path that has been driven, not merely assumed.
