---
created: 2026-09-02
area: sanitize
severity: medium
---

# Three sanitize findings merged unfixed (PR #12, review round 8)

PR #12 was merged on the operator's instruction with codex round 8 outstanding.
All three are **P2**, and none is a "threat survives and is reported verified"
case — the class that made rounds 1-7 blocking. They are recorded here in full
so the decision to merge over them is a decision, not a loss.

Trend at merge time: 26 findings across eight rounds, P1 count per round
3 → 3 → 1 → 1 → 1 → 0 → 1 → 0.

## 1. ICC profiles split across several APP2 segments

`internal/convert/sanitizeimage.go` (`isKeptJPEGAppSegment`) and its twin in
`sanitizeverify.go` (`jpegSegmentAllowed`).

A large ICC profile is legally split across multiple APP2 chunks. Only sequence
1 begins with the ICC header carrying the four-byte profile size; continuation
chunks start at arbitrary offsets inside the profile. Both functions read bytes
14–18 of **every** chunk as a declared size, so a continuation chunk whose
bytes there happen to decode small is dropped — corrupting a large colour
profile while the verifier accepts the remaining JPEG as clean.

**Fix shape:** validate the sequence number and chunk count fields, and check
the size against the ASSEMBLED profile rather than per chunk. Both
implementations need it; fixing one is the shape this PR named five times.

**Introduced by** round 7's own fix, which added the size check to close the
padded-Adobe hole. Narrower was right for APP14, which has a fixed shape; it
was wrong for ICC, which does not.

## 2. `endstream` found as a substring inside binary stream data

`pdfStreamRanges` (`sanitizepdf.go`) and `pdfNonStreamBytes`
(`sanitizeverify.go`).

Both locate a payload's end by searching for the literal bytes `endstream`. A
valid binary stream may contain that sequence before its real delimiter, which
truncates the protected range — so `/Producer(` or `%%Invocation:` later in the
same payload is still rewritten by the redaction, and `startxref`/`%%EOF` there
is still counted as a revision. The verifier repeats the same search, so it can
miss the corruption its scoping exists to prevent.

**Fix shape:** take the payload length from the stream dictionary's `/Length`
(resolving an indirect reference) instead of from the first byte occurrence.
`TestStreamRangesAgreeWithTheVerifier` already holds the two walkers to one
answer, so both move together or the test says so.

## 3. A relationship targeting the package's own main part

`relationshipDrivenDrops` removes any part named by a forbidden-type
relationship. A crafted package can point a `package` or properties
relationship at `word/document.xml` itself; the sanitizer then removes the
document and its content-type override, and `verifyOOXML` — which now requires
exactly one parseable `[Content_Types].xml` and nothing more — returns success.
The job delivers an OOXML file Office cannot open, with every class marked
verified.

**Fix shape:** require the expected main part (`word/document.xml`,
`xl/workbook.xml`, `ppt/presentation.xml` per format) and its content-type
declaration to survive, before returning success. Refusing is correct here:
a package whose main part had to be removed is not something to deliver.

## Why these were not treated as blocking

Rounds 1–7 each turned up at least one case where a construct the report
claimed removed and verified was still present, or where a valid output was
deleted. These three are: a colour profile corrupted, a redaction that can
still reach into a stream in an unusual file, and a broken-but-declared-clean
package from a crafted input. Real, worth fixing, and none of them makes the
report say something false about a threat.
