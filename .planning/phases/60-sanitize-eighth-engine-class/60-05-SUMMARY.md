---
phase: 60-sanitize-eighth-engine-class
plan: 05
status: complete
completed: 2026-09-01
commits: ["0e4b7b5"]
---

# 60-05 — The report, and the sentence it refuses to say

## What was built

`internal/convert/sanitizereport.go`, `docs/SANITIZE.md`,
`internal/convert/doc_sync_test.go`, `internal/convert/sanitizereport_test.go`,
and a `SanitizeReport` schema in `docs/openapi.yaml`.

## A second closed vocabulary, because free text cannot be checked

`LostCapabilities` was `[]string` carrying machine-safe tokens by convention.
It is now `LostCapability`, a closed vocabulary alongside `ThreatClass`, with
`AllLostCapabilities()` and a doc table held equal to it.

The reason is mechanical rather than aesthetic. A doc-sync test can hold a set
of tokens equal to a set of tokens; it cannot compare prose to prose. With
`[]string`, the first person to write `"forms may not work"` instead of a token
would have broken nothing and been caught by nothing — and that is the exact
failure mode phase 59 found in `ocrguards.go`, where a comment promised a route
that had never existed and nothing could notice, because a comment cannot fail.

## Three fields, three different claims

```json
{ "format": "...", "removed": [...], "lost_capabilities": [...],
  "verified": [...], "guarantee_scope": "docs/SANITIZE.md#scope-of-the-guarantee" }
```

`removed` and `verified` are separate because they are separate claims. "We
removed JavaScript" describes our code path; "we confirmed no JavaScript is
present in the output" describes the bytes the client is about to receive.
SANI-04 requires the second, and a client cannot tell them apart if they share
a field.

`removed` and `lost_capabilities` are separate for D-02's reason: a lost form is
not a found attack.

There is **no** `clean` boolean, no verdict, no summary sentence.
`TestReportHasNoVerdictField` pins the shape rather than the prose, because a
structure is what an SDK generator turns into a client-visible field. The
absence is load-bearing.

## The ban took two rounds, and both failures were the same shape

The honest prose that states the ban contains every banned word:

```
"does not determine whether a document was malicious"
"may assert that a file is safe, clean, virus-free, or free of malicious content"
"There is no `clean` boolean"
```

A matcher firing on the word "safe" reddens on all of them, and the outcome
would be that the *test* gets deleted, not the prose — and the ban with it. So
the matcher discards sentences carrying a negation cue and then looks for
affirmative frames in what is left.

**Round 1.** It reported `findings.go`'s own statement of the ban as a
violation of the ban. Both markdown and Go comments hard-wrap, so

```
// No constant, and no text assembled from these constants, may assert that a
// file is safe, clean, virus-free, or free of malicious content.
```

arrived as two lines with the negation cue stranded on the first. Splitting on
newlines was the bug. The matcher now rejoins wrapped lines into sentences
before splitting them, with a blank line ending a paragraph so two unrelated
paragraphs never merge.

**Round 2.** `docs/SANITIZE.md`'s own description of this test tripped it:
"neither this document nor the Go vocabulary contains an affirmative claim…".
`neither`/`nor` were not in the cue list. Note `"no "` carries a trailing space
deliberately — without it, `nor`, `none` and `normalize` would all read as
negations.

Both are now pinned by `TestMatcherIsNarrowEnoughToSurvive`, which carries a
must-match table of real affirmative claims and a must-not-match table of the
guarded files' actual sentences. **A future widening of the matcher that starts
firing on honest prose fails its own test before it fails anyone else's** —
which is what stops the check from being deleted the first time it is wrong.

## docs/SANITIZE.md

Two tables, closed against the two vocabularies. The threat table's last column
— "Verified by" — is what makes it a proof rather than a promise: for every
class, the implementation that confirms absence is not the one that removed. A
verifier blind in the same place as the sanitizer proves only its own
blindness.

The scope section names the three things the service does **not** do
(determine malice, detect outside the enumerated classes, replace antivirus)
and is written as a description of the method, not as a disclaimer. A
disclaimer reads like something to skip; a description of the method reads like
something to understand, and understanding it is what lets a client decide
whether `sanitize` is the right tool.

The losses table says plainly that the PDF losses are all one consequence of
rebuilding through PostScript — the construction that makes the removals
believable is the same construction that costs the forms and the tag tree — and
that the image path's empty list means no loss was incurred rather than none
was measured.

## openapi

`SanitizeReport` is a schema, the `operation` field documents `sanitize`
including the scope sentence, and the outputs array points at the report. A
client generating an SDK may never open the document, so the boundary has to be
in the contract too. `role` is deliberately left an open string.

## A naming drift in the plan

`60-05-PLAN.md`'s `key_links` names the pattern `AllSanitizeClasses`. The
function is `AllThreatClasses()`, declared by 60-02. Used the real name.

## Inversions (D-16)

| Change | Failure |
|---|---|
| `launch_action` row deleted from the document | "declared in Go but missing from the document: [launch_action]" |
| invented `pdf_encryption_removed` row added | "present in the document but not declared in Go: [pdf_encryption_removed]" |
| guarantee-boundary phrase reworded | `TestSanitizeDocCarriesTheGuaranteeBoundary` names the exact missing phrase |
| "After sanitization the output is safe to share." added | `TestNoAffirmativeSafetyClaims` quotes the sentence back |

## Gates

`gofmt -l .` clean, `go vet ./...` clean, `go build ./...` clean,
`go test ./... -count=1` exit 0, `yq -e '.' docs/openapi.yaml` exit 0.

## Carried into 60-06

The report exists and is tested, but nothing writes it as a job output yet —
wiring `role=report` into the worker is 60-06's job, along with registering
`sanitize` as the eighth engine class.
