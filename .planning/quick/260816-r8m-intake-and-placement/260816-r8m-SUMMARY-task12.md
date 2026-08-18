# Quick task 260816-r8m — Tasks 1 & 2 Summary

Scope executed: **Task 1** (bound concurrent multipart intake) and **Task 2**
(sniff read-failures observable + structural-vs-IO classification) only.
Tasks 3 (`internal/inspect/media.go`) and 4 (`scripts/inspect-burst-liveproof.sh`)
are owned by other parallel executors and were not touched.

## Commits

| Task | Commit | Message |
|------|--------|---------|
| 1 | `5067894` | fix(quick-260816-r8m): bound concurrent multipart intake, not just analysis |
| 2 | `9794fc7` | fix(quick-260816-r8m): make sniff read-failures observable, classify by observed reader fact |

## Task 1 — Bound concurrent multipart intake

**Files:** `internal/api/inspect_handlers.go`, `internal/api/inspect_handlers_test.go`,
plus a minimal, mechanically-necessary addition to `internal/api/api.go` (see
Deviations below).

**Defect:** `handleInspect`'s own T-SLF-02 fix correctly moved the D-06
mechanism-2 concurrency slot (`s.inspectSem`) to *after* body intake, so a
slow-body client can no longer starve the four analysis slots. But that left
`r.ParseMultipartForm(multipartInMemoryBudget)` itself completely
unbounded: any number of requests can be inside it concurrently, each
buffering up to `multipartInMemoryBudget` (32 MiB) in the process heap. Under
the default `RATE_LIMIT_INSPECT_RPM=30` burst budget, one authenticated
client bursting 30 requests could buffer ~960 MiB — 94% of the deployed api
pod's 1 GiB `resources.limits.memory` — an OOM-restart risk the live SC6
gate never exercised (it only ran 9 concurrent requests).

**Fix:** a new, separate, non-blocking `inspectIntakeSem` (capacity 8,
`api.go`/`inspect_handlers.go`) wraps *only* the `ParseMultipartForm` call,
released immediately after it returns (success or failure) — not held for
the rest of the handler. Arithmetic documented at the constant's own
declaration: `8 × 32 MiB = 256 MiB` (25% of 1 GiB), vs. the unbounded
`~30 × 32 MiB ≈ 960 MiB` (94%) scenario the finding names. A saturated
intake stage returns the same honest `429 inspect_busy` (errcode dictionary,
never 500). A per-request body-read deadline
(`http.NewResponseController(w).SetReadDeadline`, 30s) closes the
"cmd/api/main.go configures no request-body timeout" half of the finding —
`cmd/api/main.go` was **not** touched; the deadline is set per-request from
inside the handler, which is the standard Go 1.20+ mechanism for this and
requires no server-wide config change.

Holding the intake slot for the client's own network-transfer duration is
correct here, not a repeat of the T-SLF-02 mistake: the resource this slot
protects (heap bytes actively being buffered) really is occupied for
exactly that long, unlike `s.inspectSem`'s CPU/DB-pool slot, which was never
actually consumed while waiting on the network.

**Test:** `TestInspectIntakeConcurrencyIsBounded` — starts
`inspectIntakeMaxConcurrent + 3` concurrent slow-body clients, proves
exactly 3 are rejected with `429 inspect_busy` while the other 8 remain
blocked inside `ParseMultipartForm` (never touching the semaphore result via
a data race — results are handed back through a channel, not a bare
`time.Sleep` + shared-recorder read).

**T-SLF-02 preserved:** `TestInspectSlowBodyDoesNotHoldConcurrencySlot`
passes unmodified — verified both before and after this change.

**Inversion:** disabled the `inspectIntakeSem` select block (`if false {...}`)
and its paired release, confirmed `TestInspectIntakeConcurrencyIsBounded`
fails (`rejected = 0, want exactly 3`), restored via `cp` from a pre-edit
backup (never `git checkout`).

## Task 2 — Sniff read-failures observable; structural vs. I/O classification

**Files:** `internal/api/inspect_handlers.go`, `internal/api/inspect_handlers_test.go`,
`internal/convert/docsniff.go`, `internal/convert/docsniff_test.go`.

**Defect 1 (PR9R8-F2/F3, inspect_handlers.go:371):** `detectInspectFormat`'s
ZIP branch was a success-only conditional —
`else if cr, cerr := convert.SniffContainer(...); cerr == nil && ...` —
so any non-nil `cerr` silently left `detected` empty, identical to a
genuinely unrecognized upload. `inspect.ReasonReadFailed` existed in the
vocabulary and in a unit test but was unreachable on the actual production
sniff path.

**Defect 2 (PR9R8-F4, docsniff.go:93):** `SniffContainer` classified
`zip.NewReader`'s error as "operational" (not `ErrNotAZip`) unless it
matched one of three enumerated sentinels
(`zip.ErrFormat`/`zip.ErrAlgorithm`/`zip.ErrChecksum`). `io.ErrUnexpectedEOF`
(truncated central-directory header) is not on that list and was
misclassified as a read failure even though it is a structural fact about
the bytes, not a failure to read them.

**Fix:** `docsniff.go` gains `readerAtRecorder`, a wrapper over
`io.ReaderAt` that records whether it *itself* ever observed a real
(non-EOF) `ReadAt` error. `SniffContainer` now classifies by that OBSERVED
FACT — `rr.failed` — rather than by matching the parser's returned error
against a closed list of names. This is a strict generalization: it
correctly classifies both the original three sentinels *and* any future
unenumerated structural error, proven by `io.ErrUnexpectedEOF` (never on
the old list) now classifying correctly.

`detectInspectFormat` (`inspect_handlers.go`) now inspects `cerr` on every
non-nil outcome: `errors.Is(cerr, convert.ErrNotAZip)` → structural →
`inspectZipMalformedFormat` sentinel → `unknown/malformed_input`;
otherwise → operational → `inspectZipReadFailedFormat` sentinel →
`unknown/read_failed`. Two new `FamilyResult` helpers
(`zipReadFailedFamilyResult`, `zipMalformedFamilyResult`) mirror the
existing `zipDirectoryCeilingFamilyResult` pattern, and
`extensionMismatchFamilyResult` gained matching cases so
`CodeExtensionMismatch` doesn't fall through to the generic
`format_unrecognized` case either.

**Tests (docsniff_test.go):**
- `TestSniffContainer_ReadFailureIsNotErrNotAZip` — a genuine `ReadAt`
  failure after the 4-byte prefix is never `ErrNotAZip`.
- `TestSniffContainer_TruncatedCentralDirectoryHeaderIsStructuralNotReadFailure`
  — `io.ErrUnexpectedEOF` (never on the old 3-sentinel list) classifies as
  `ErrNotAZip`.
- `TestSniffContainer_InconsistentEOCDCommentLengthIsStructuralNotReadFailure`
  — see deviation note below; still proves an EOCD lying about its own
  comment length is never a read failure.
- `TestReaderAtRecorder_ObservesOnlyRealReadFailures` — direct unit test of
  the wrapper itself (the reviewer's own suggested landmark): EOF is not a
  failure, a real error is recorded and preserved, the first failure is
  sticky.

**Test (inspect_handlers_test.go):**
`TestInspectSniffReadFailureIsObservableAsReadFailed` exercises
`detectInspectFormat` → `dispatchInspectFamilies` → `inspect.BuildReport`
directly with a fake `multipart.File` whose `ReadAt` succeeds only for the
4-byte prefix — the exact production functions `handleInspect` calls on its
own sniff path. Asserts `unknown/read_failed` on the container-domain codes
and `CodeExtensionMismatch`, and asserts *no* check carries
`format_unrecognized`.

**Two inversions performed (both confirmed failing, then restored via `cp`,
never `git checkout`):**
1. `docsniff.go`: reverted `SniffContainer`'s classification to the old
   3-sentinel allowlist → `TestSniffContainer_TruncatedCentralDirectoryHeaderIsStructuralNotReadFailure`
   failed (`unexpected EOF classified as an operational read failure`).
2. `inspect_handlers.go`: disabled the `else if cerr != nil` branch in
   `detectInspectFormat` → `TestInspectSniffReadFailureIsObservableAsReadFailed`
   failed (`detected = "", want "zip_read_failed"`).

## Deviations from Plan

### Auto-fixed / adjusted issues

**1. [Rule 2 — missing critical functionality] Added `inspectIntakeSem` as a
`Server`-scoped field, touching `internal/api/api.go` (not in Task 1's
`<files>` list).**
- **Found during:** Task 1 design.
- **Issue:** The acceptance criteria require a genuine, testable concurrency
  bound with an explicit `429` rejection at the intake stage. A correct,
  per-process bound needs persistent state shared across concurrent
  requests. This codebase's own documented architectural constraint
  (`CLAUDE.md`/README: "the ONLY global mutable-at-init state ... no other
  singletons/globals") rules out a second package-level global. The only
  correct placement, mirroring the existing `s.inspectSem` pattern exactly,
  is a `Server` struct field — which is declared in `api.go`.
- **Fix:** added `inspectIntakeSem chan struct{}` to `Server` and its one-line
  initializer in `NewServer`, both minimal and mechanical. All logic
  (constant, arithmetic, acquire/release, tests) stays in the two files the
  task named. `api.go` itself does not fall under the task's explicit
  exclusion (only `internal/inspect/media.go` and `scripts/` were named as
  off-limits, owned by other parallel executors).
- **Files modified:** `internal/api/api.go` (2 small additions).
- **Commit:** `5067894`.

**2. [Deviation from literal finding text, documented per "verify against the
goal, not the requirement text"] The literal `"zip: invalid comment length"`
error string named in the round-8 finding is unreachable in this project's
vendored Go 1.26 toolchain.**
- **Found during:** Task 2 test construction.
- **Issue:** Empirically constructing the fixture and reading
  `$GOROOT/src/archive/zip/reader.go` line by line showed that
  `findSignatureInBlock`'s own truncated-comment guard
  (`n+directoryEndLen+i > len(b)`) is mathematically identical to
  `readDirectoryEnd`'s own later `l > len(b)` check on the same field —
  whichever window (1024B or 65KiB) locates the EOCD signature always ends
  exactly at the true end of file, so any input that would trip the later
  check has already made the signature locator report "not found" first,
  producing `zip.ErrFormat` instead. This makes the exact quoted error
  message dead code in the current stdlib.
- **Action:** kept the fixture (same reviewer-named defect class: an EOCD
  lying about its own comment length) and adjusted the test
  (`TestSniffContainer_InconsistentEOCDCommentLengthIsStructuralNotReadFailure`)
  to assert the actual, verified property (`errors.Is(err, ErrNotAZip)`)
  rather than a specific Go error string that cannot occur. Documented the
  empirical finding in both the fixture's and the test's own doc comments.
  Added `TestReaderAtRecorder_ObservesOnlyRealReadFailures` as a more direct,
  toolchain-independent proof of the same underlying mechanism.
- **Files modified:** `internal/convert/docsniff_test.go`.
- **Commit:** `9794fc7`.

None of the other acceptance-criteria items required adjustment.

## Known Stubs

None — both tasks are fully wired end-to-end; no placeholder/empty-data
paths were introduced.

## Threat Flags

None — both fixes narrow existing DoS/observability surface (the intake
bound reduces attack surface; the read-failure classification reduces
misclassification, not new surface). No new endpoints, auth paths, or trust
boundaries were introduced.

## Self-Check

- `internal/api/inspect_handlers.go` — FOUND
- `internal/api/inspect_handlers_test.go` — FOUND
- `internal/api/api.go` — FOUND
- `internal/convert/docsniff.go` — FOUND
- `internal/convert/docsniff_test.go` — FOUND
- Commit `5067894` — FOUND in `git log --oneline`
- Commit `9794fc7` — FOUND in `git log --oneline`
- `go build ./...` — clean
- `go vet ./...` — clean
- `gofmt -l internal cmd` — empty (clean)
- `go test ./... -count=1 -race` — all packages `ok` (verified full-repo run,
  not just the touched packages)

## Self-Check: PASSED
