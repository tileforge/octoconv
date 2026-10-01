---
phase: quick-260810-r2a
plan: 01
subsystem: api
tags: [go, http, security, dos-hardening, zip, archive-zip, uint16-wraparound]

requires:
  - phase: quick-260810-slf
    provides: "zipDirectoryExceedsStructuralBound and its EOCD-peek structural pre-parse bound on ZIP central-directory size/entry-count, checked before archive/zip.NewReader ever runs"
provides:
  - "zipDirectoryExceedsStructuralBound's exceeds decision now reads ONLY the EOCD's declared central-directory byte size (dirBytes, a uint32) -- the entry-count field (a uint16 that wraps at 65536) is read for diagnostics only and never influences the decision"
  - "inspectMaxZipDirBytes is now derived as inspectMaxZipDirEntries * zipCentralDirMinRecordLen (~184 KiB) instead of an independently-chosen flat 4 MiB, so a directory containing more records than the entry ceiling can never declare a small-enough byte size to pass"
  - "ZIP64's 0xFFFFFFFF dirBytes sentinel is treated as exceeding the bound explicitly, not merely because the numeric comparison happens to also reject it"

affects: ["58-synchronous-inspection (the phase this bound belongs to)"]

tech-stack:
  added: []
  patterns:
    - "When a structural bound derives from an archive's own self-declared field, check whether that field's integer width can wrap before trusting a comparison against it -- if it can, re-derive the bound from a DIFFERENT field whose width cannot represent the same overflow at any bound value that would ever be set (dirBytes' uint32 vs entries' uint16 here)."
    - "A ceiling on 'count of things' can be made wrap-proof by deriving it from 'bytes containing those things divided by the minimum possible size of one thing' -- a lower bound on cost that any wrapping count field cannot misrepresent."

key-files:
  created: []
  modified:
    - internal/api/inspect_handlers.go
    - internal/api/inspect_handlers_test.go

key-decisions:
  - "Chose option (a) from the plan (derive the byte ceiling from the entry ceiling via the ZIP spec's 46-byte minimum central-directory record size) over option (b) (a bounded pre-scan counting PK\\x01\\x02 signatures). Option (a) costs zero additional reads -- the EOCD-only peek stays unchanged -- while option (b) would have added a whole new bounded-work concern (T-R2A-02 in the plan's own threat model) that itself needed bounding."
  - "entries is still read and returned from zipDirectoryExceedsStructuralBound (not removed) -- kept as a diagnostic-only value with an explicit code comment at the read site and in the function's own doc comment, per the plan's fallback instruction, so a future reader cannot accidentally 'simplify' the exceeds decision back to using it."
  - "The ZIP64 sentinel (dirBytes==0xFFFFFFFF) is checked explicitly even though the plain numeric comparison (dirBytes > inspectMaxZipDirBytes) already rejects it -- documented as intentional (never rely on a sentinel being caught by coincidence), matching the plan's explicit instruction."
  - "Re-derived inspectMaxZipDirBytes from inspectMaxZipDirEntries*46 (~184 KiB) rather than keeping the old independently-chosen 4 MiB alongside a bytes-only decision. This makes the correspondence between the two ceilings ONE-DIRECTIONAL (a legitimate archive within the entry ceiling but with unusually long filenames could, in principle, trip the byte ceiling even though AnalyzeContainer's own per-entry scan would have tolerated it) rather than exact -- documented in the constant's own comment as a deliberate, disclosed trade, since a real office/epub central directory is typically well under 100 KiB."

patterns-established:
  - "A structural pre-parse bound must derive its decision from the field whose integer width makes the ceiling being enforced UNREPRESENTABLE as a smaller number -- not merely from 'a field that happens to correlate with the thing being bounded today'."

requirements-completed: [PR9R2-F5-EOCD-UINT16-WRAP]

duration: ~20min
completed: 2026-08-10
audit_acknowledged:
  milestone: v1.15
  at: 2026-09-30
  status: unknown
---

# Quick Task 260810-r2a: Round-2 EOCD Structural Bound Fix Summary

**`zipDirectoryExceedsStructuralBound`'s exceeds decision now reads only the ZIP EOCD's declared central-directory byte size (a non-wrapping uint32), never the entry-count field (a uint16 that wraps at 65536) codex's round-2 review proved bypassable with a 65,537-minimal-record archive declaring entries=1.**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-08-10T20:25:00Z (approx)
- **Completed:** 2026-08-10T20:42:00Z
- **Tasks:** 1
- **Files modified:** 2

## Accomplishments

- **The exact bypass codex described is closed.** `zipDirectoryExceedsStructuralBound`'s `exceeds` assignment no longer reads `entries` at all -- only `dirBytes == 0xFFFFFFFF || dirBytes > inspectMaxZipDirBytes`. A `grep -n "exceeds ="` on the production file shows exactly one line, and it contains no reference to `entries`.
- **`inspectMaxZipDirBytes` is re-derived, not merely re-used.** It is now `inspectMaxZipDirEntries * zipCentralDirMinRecordLen` (4096 * 46 = 188,416 bytes, ~184 KiB) instead of an independently-chosen flat 4 MiB. Because every real central-directory record costs at least 46 bytes, a directory containing more than `inspectMaxZipDirEntries` records can never declare a `dirBytes` value under this bound -- closing the exact gap the finding described (65,537 records * 46 bytes = 3,014,702 bytes, comfortably under round 1's 4 MiB but now comfortably OVER the new ~184 KiB bound).
- **ZIP64's `0xFFFFFFFF` sentinel is treated as exceeding explicitly**, documented as intentional rather than relying on the plain numeric comparison to reject it by coincidence.
- **Round-1 behavior on the primary regression test is unchanged and untouched.** `TestInspectZipDirectoryOverBoundReportsUnknownNot500` passed with zero edits both before and after this fix.
- **Legitimate multi-part archives still pass, proven with a fixture.** New `TestInspectZipDirectoryWithinBoundToleratesRealisticOfficeArchive` uses a 51-part docx-shaped fixture (not the 2-entry toy fixture the pre-existing test used) and still resolves to `format=docx`.
- **The reviewer's exact scenario is reproduced as a test, both at the unit level and end-to-end through the real HTTP handler**, and both pass against the fix.

## Task Commits

1. **Task 1: Bound not trusting a wrapping field** - `dff1cab` (fix)

**SUMMARY commit:** (this file, committed separately below)

## Files Created/Modified

- `internal/api/inspect_handlers.go` - `zipDirectoryExceedsStructuralBound`'s `exceeds` decision now reads `dirBytes` only; `entries` is read and returned for diagnostics but never influences the decision; new `zipCentralDirMinRecordLen` (46) constant; `inspectMaxZipDirBytes` re-derived from `inspectMaxZipDirEntries * zipCentralDirMinRecordLen`; every touched doc comment rewritten to explain the uint16-wraparound reasoning and the new one-directional correspondence between the entry and byte ceilings.
- `internal/api/inspect_handlers_test.go` - `TestZipDirectoryExceedsStructuralBoundIgnoresWrappedEntryCount` (unit-level, exact reviewer arithmetic: entries=1, dirBytes=3,014,702), `TestInspectReviewerScenarioReportsUnknownNot500` (same scenario end-to-end through `handleInspect`), `TestZipDirectoryExceedsStructuralBoundTreatsZip64SentinelAsExceeding` (dirBytes=0xFFFFFFFF), `TestInspectZipDirectoryWithinBoundToleratesRealisticOfficeArchive` (51-part fixture proving legitimate archives still pass); replaced the now-stale `TestInspectZipDirectoryBoundInversion` (its premise -- a bare EOCD with entries set but dirBytes left at zero -- tested exactly the mechanism round 2 proved untrustworthy) with the above; `reviewerScenarioZipFixture` helper.

## Decisions Made

See `key-decisions` in frontmatter above. In one line each:
- Derived the byte ceiling from the entry ceiling (46-byte minimum record size) rather than adding a bounded pre-scan -- zero extra reads, per the plan's own stated preference.
- Kept `entries` as a diagnostic-only return value rather than removing it, with an explicit comment warning against reintroducing it into the decision.
- Checked the ZIP64 `0xFFFFFFFF` sentinel explicitly even though numerically redundant, to make the intent unambiguous.
- Accepted that the entry/byte ceiling correspondence is now one-directional (documented trade, not an oversight).

## Deviations from Plan

None - plan executed exactly as written. The plan explicitly offered two options (derive from bytes, or a bounded pre-scan) and recommended the byte-derivation approach as preferred; that is what was implemented.

## Issues Encountered

- Two temporary `const` blocks in the new tests (`trueDirBytes`, `wrappedEntries` computed via bit-shift expressions) failed to compile with `constant ... overflows byte` -- Go rejects a compile-time-constant conversion that would truncate. Fixed by declaring them as regular (non-const) variables and using `encoding/binary.LittleEndian.PutUint16/PutUint32` instead of manual byte-shift assignment, which performs the truncation at runtime. Caught immediately by `go build` failing before any test ran.

## Verification Performed (literal output pasted)

**`go build ./... && go vet ./...`:**

```
BUILD_VET_OK
```

(no output from either command; explicit echo confirms both succeeded)

**`gofmt -l internal/api`:** empty (no output)

**`go test ./internal/api/ -count=1 -race`:**

```
ok  	github.com/apaderin/octoconv/internal/api	5.154s
```

**`go test ./... -count=1 -race`:** every package `ok`, full output tail:

```
ok  	github.com/apaderin/octoconv/internal/reconciler	3.573s
ok  	github.com/apaderin/octoconv/internal/storage	3.662s
ok  	github.com/apaderin/octoconv/internal/webhook	3.770s
ok  	github.com/apaderin/octoconv/internal/worker	9.373s
```

**Golden fixtures, `git rev-list --count HEAD -- <file>` for each of the three `pre58_*.golden.json` files:** all three printed `1` (untouched, exactly one commit each).

**`git diff --stat -- go.mod go.sum`:** empty (zero new dependencies).

**`grep -n "exceeds =" internal/api/inspect_handlers.go`:**

```
479:		exceeds = dirBytes == 0xFFFFFFFF || dirBytes > inspectMaxZipDirBytes
```

Confirms the decision depends on `dirBytes` only.

### Inversion (D-16), performed and reverted

Temporarily reverted BOTH the decision formula and the byte ceiling to their round-1 values to reproduce the exact historical bypass (reverting only the formula while keeping the new, smaller derived ceiling would not have reproduced it, since the new ~184 KiB ceiling alone already catches the reviewer's 2.9 MiB scenario):

```go
inspectMaxZipDirBytes = 4 << 20 // INVERSION-TEMP
...
exceeds = entries > inspectMaxZipDirEntries || dirBytes > inspectMaxZipDirBytes // INVERSION-TEMP
```

Ran the two tests that directly exercise the reviewer's scenario:

```
=== RUN   TestInspectReviewerScenarioReportsUnknownNot500
    inspect_handlers_test.go:389: Format = "", want "zip_directory_too_large" -- entries=1 (wrapped) must NOT let this upload past the structural bound
    inspect_handlers_test.go:392: Coverage.Complete = true, want false
    inspect_handlers_test.go:395: Coverage.Reason = "", want "work_ceiling_bytes"
--- FAIL: TestInspectReviewerScenarioReportsUnknownNot500 (0.00s)
=== RUN   TestZipDirectoryExceedsStructuralBoundIgnoresWrappedEntryCount
    inspect_handlers_test.go:521: exceeds = false, want true -- entries=1 (wrapped, looks tiny) must NOT be able to wave a 3014702-byte declared directory (~2.9 MiB, 65537 real minimal records) through the structural bound
--- FAIL: TestZipDirectoryExceedsStructuralBoundIgnoresWrappedEntryCount (0.00s)
FAIL
FAIL	github.com/apaderin/octoconv/internal/api	1.331s
```

Both failed exactly reproducing the reported bypass. Reverted both lines back to the fixed versions; re-ran the same two tests plus the full `internal/api` suite -- both pass, full suite green (see above). `grep -n "INVERSION-TEMP"` across both files returned nothing after the revert, confirming no residue.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- The structural bound this quick task fixes belongs to Phase 58 (Synchronous Inspection), which is otherwise complete pending its own live operator gate (SC6, `autonomous: false`). This fix does not reopen or block that gate -- it strengthens a mechanism SC6 will calibrate/exercise regardless.
- No further findings from PR #9 round 2 are known to remain unaddressed by this task; this quick task covered exactly `PR9R2-F5-EOCD-UINT16-WRAP`, the single finding scoped to it.

---
*Quick task: 260810-r2a*
*Completed: 2026-08-10*

## Self-Check: PASSED

Both modified files verified present on disk (`internal/api/inspect_handlers.go`, `internal/api/inspect_handlers_test.go`); commit `dff1cab` verified present in `git log --oneline --all`.
