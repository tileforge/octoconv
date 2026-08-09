---
phase: 57-operation-dimension-output-roles
plan: 02
subsystem: api
tags: [go, converter-registry, optional-interface, panic-on-collision]

# Dependency graph
requires:
  - phase: 57-operation-dimension-output-roles
    provides: "57-01 golden pinning (internal/api/golden_pinning_test.go) which byte-proves this plan's Classes() filter kept GET /v1/formats unchanged; unsupported_operation errcode this plan's downstream consumers (57-03/57-04) will use"
provides:
  - "internal/convert Registry keyed by (operation, from, to) triple instead of (from, to) pair"
  - "OperationConvert const + optional OperationalConverter interface (ProgressConverter precedent) + operationOf fallback helper"
  - "Registry.Register panics on triple collision (D-08); Registry.Replace performs the deliberate overwrite (D-09)"
  - "LookupOp/SupportsOp/EngineForOp (triple-aware); Lookup/Supports/EngineFor/Classes kept two-argument, delegating with OperationConvert (D-07)"
  - "Registry.Operations()/IsKnownOperation() (registry-derived operation enumeration, D-02) and Registry.OperationClasses() (full {operation->engine->pairs} tree, source for Plan 04's new /v1/formats key)"
  - "cmd/av-worker, cmd/ocr-worker, cmd/pandoc-worker moved from Register to Replace for their configured-instance re-registration"
  - "internal/convert/operations_test.go: triple-disjointness, pdf->pdf identity case, Register panic, Replace semantics, D-14 one-operation-per-engine invariant"
affects: [57-04, 57-06, 60]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Optional-interface capability declaration (OperationalConverter mirrors ProgressConverter exactly): type-assert with fallback, zero changes to the seven concrete converters"
    - "Fail-closed-at-init(): a registry collision that used to be a silent last-write-win is now a panic, converting a latent runtime misrouting defect into an unmissable startup failure"
    - "Register vs Replace as two distinct, differently-named entry points for 'insert new' vs 'I know I am overwriting this' -- readable in the caller's code instead of only in a comment"

key-files:
  created:
    - internal/convert/operations_test.go
  modified:
    - internal/convert/convert.go
    - internal/convert/converters.go
    - internal/convert/pairs_disjoint_test.go
    - cmd/av-worker/main.go
    - cmd/ocr-worker/main.go
    - cmd/pandoc-worker/main.go

key-decisions:
  - "Register's panic message is built purely from the colliding opKey fields and %T of both converters (no hardcoded strings) so it mechanically satisfies the plan's textual acceptance criteria (mentions the triple's from/to and the word 'Replace') without special-casing any particular pair"
  - "converters.go's seven Default.Register(...) call sites were left completely untouched at the code level; only new trailing comment paragraphs were appended next to the three placeholder registrations that are later re-registered via Replace in their respective worker cmd -- verified via git diff -U0 non-comment-line count == 0"
  - "pairs_disjoint_test.go's four pre-existing tests were left byte-for-byte identical; the new header comment was merged into the same comment block as TestAVAudioPairDisjointness's doc comment (no blank line inserted) specifically so the diff contains zero non-comment-prefixed added/removed lines, satisfying the plan's literal grep-based acceptance check"
  - "Two distinct synthetic converter types (sanitizeIdentityConverter, sanitizeIdentityConverterAlt) were used for the Register-collision and Replace-overwrite tests instead of two instances of one type, so LookupOp's returned %T can prove exactly WHICH converter answered a triple after the operation"

patterns-established:
  - "Registry.m's map key is now a package-private opKey{Operation, From, To} struct -- any future dimension added to the registry (if ever) should extend opKey the same additive way OperationalConverter extended Converter, never by widening Pair itself"

requirements-completed: [OPRN-02]

# Metrics
duration: ~50min
completed: 2026-08-09
---

# Phase 57 Plan 02: Operation-Aware Converter Registry Summary

**Registry.m re-keyed from (from, to) pairs to (operation, from, to) triples via an optional `OperationalConverter` interface, with `Register` now panicking on a triple collision instead of silently overwriting, `Replace` added for the three workers' deliberate re-registrations, and zero code-level changes to `internal/convert/converters.go`'s seven existing registrations.**

## Performance

- **Duration:** ~50 min
- **Completed:** 2026-08-09T18:10:00Z
- **Tasks:** 3/3 completed
- **Files modified:** 7 (1 created, 6 modified)

## Accomplishments

- Added `OperationConvert = "convert"` and the optional `OperationalConverter { Operation() string }` interface to `internal/convert/convert.go`, following the exact `ProgressConverter` precedent (type-assert with fallback, zero impact on converters that don't implement it).
- Re-keyed `Registry.m` from `map[Pair]Converter` to an unexported `map[opKey]Converter` where `opKey{Operation, From, To}`; `Register` now panics with a message naming the colliding triple and both converters' `%T`, pointing at `Replace`, when a triple is already occupied — turning the previously silent last-write-wins behavior into a fail-closed startup check.
- Added `Replace` (unconditional overwrite, no panic), `LookupOp`/`SupportsOp`/`EngineForOp` (triple-aware reads), `Operations()`/`IsKnownOperation` (registry-derived operation enumeration), and `OperationClasses()` (full `{operation -> engine -> []Pair}` tree). `Lookup`/`Supports`/`EngineFor` kept their exact pre-existing two-argument signatures as one-line delegations to the `*Op` variants with `OperationConvert`; `Classes()` kept its exact signature and output, gaining only an internal filter on `key.Operation == OperationConvert`.
- Moved `cmd/av-worker`, `cmd/ocr-worker`, `cmd/pandoc-worker`'s single configured-instance re-registration each from `Default.Register(...)` to `Default.Replace(...)` (same argument, one-line change per file), and appended (not replaced) explanatory comment paragraphs next to the three corresponding placeholder registrations in `internal/convert/converters.go` — that file's seven `Default.Register(...)` call sites are byte-identical at the code level.
- Added `internal/convert/operations_test.go`: two synthetic `OperationalConverter` test types proving the `pdf->pdf` identity case under a non-convert operation is structurally unreachable from the two-argument `Lookup`/`EngineFor` (Success Criterion 2), `Register`'s panic-on-collision plus a same-pair-different-operation negative control, `Replace`'s no-panic overwrite-and-insert semantics, `Operations()`/`IsKnownOperation`'s registry-derived enumeration (today exactly `["convert"]`), the D-14 one-operation-per-engine-class invariant walked directly off `Default.OperationClasses()`, and `Classes()` vs `OperationClasses()`'s differing non-convert-operation visibility.

## Task Commits

Each task was committed atomically:

1. **Task 1: Перевести Registry на ключ-тройку и добавить OperationalConverter** - `089aafc` (feat) — `internal/convert/convert.go`
2. **Task 2: Перевести три воркера на Replace и зафиксировать panic-бэкстоп в комментариях converters.go** - `74506ae` (feat) — `cmd/av-worker/main.go`, `cmd/ocr-worker/main.go`, `cmd/pandoc-worker/main.go`, `internal/convert/converters.go`
3. **Task 3: Тесты дизъюнктности троек, identity-случая pdf→pdf, паники Register и инварианта D-14** - `7b5cf7c` (test) — `internal/convert/operations_test.go` (created), `internal/convert/pairs_disjoint_test.go`

## Files Created/Modified

- `internal/convert/convert.go` - `OperationConvert`, `OperationalConverter`, `operationOf`, `opKey`-keyed `Registry.m`, `Register` (now panics on collision), `Replace`, `LookupOp`/`SupportsOp`/`EngineForOp`, `Operations`/`IsKnownOperation`, `OperationClasses`; `Lookup`/`Supports`/`EngineFor`/`Classes` kept their signatures
- `internal/convert/converters.go` - comment-only additions next to the AV/OCR/Pandoc placeholder registrations noting the corresponding worker cmd now uses `Replace`; the seven `Default.Register(...)` calls themselves are unchanged
- `cmd/av-worker/main.go` - `convert.Default.Register(convert.AVConverter{...})` → `convert.Default.Replace(...)`, plus a comment explaining why
- `cmd/ocr-worker/main.go` - `convert.Default.Register(buildOCRConverter(...))` → `convert.Default.Replace(...)`, plus a comment explaining why
- `cmd/pandoc-worker/main.go` - `convert.Default.Register(buildPandocConverter(...))` → `convert.Default.Replace(...)`, plus a comment explaining why
- `internal/convert/operations_test.go` - new file: `sanitizeIdentityConverter`, `inspectIdentityConverter`, `sanitizeIdentityConverterAlt` synthetic `OperationalConverter`s; `TestSanitizeIdentityPairDisjointFromConvert`, `TestRegisterPanicsOnTripleCollision`, `TestReplaceOverwritesWithoutPanic`, `TestOperationsDerivedFromRegistry`, `TestOneOperationPerEngineClass`, `TestClassesExcludesNonConvertOperations`
- `internal/convert/pairs_disjoint_test.go` - header comment addition only, pointing at the new complementary suite; the four existing tests and their assertions are untouched

## Decisions Made

- **Panic message built from live data, not a hardcoded string.** `Register`'s panic message interpolates the colliding `opKey`'s `Operation`/`From`/`To` fields and both converters' `%T` — this mechanically satisfies the plan's acceptance criteria (message must mention the triple and "Replace") for ANY colliding triple, not just the `pdf`/`sanitize` test case, and needs no special-casing.
- **converters.go code untouched; only trailing comments appended.** All three placeholder-registration comment blocks (AV/OCR/Pandoc) got a new paragraph appended after their existing text, never edited in place — kept the `git diff -U0` non-comment-line count at exactly 0 for both plan-mandated checks (Task 1 and Task 2 acceptance criteria).
- **pairs_disjoint_test.go's new header comment merged into the adjacent doc comment (no blank line inserted).** A blank `+` line in a `git diff -U0` output does not match the acceptance criteria's `grep -v '^[+-]\s*//'` comment-only filter (an empty line has no `//` to match), so the added paragraph was joined directly onto `TestAVAudioPairDisjointness`'s existing doc comment with a `//` separator line instead of a blank line — verified the diff's non-comment-line count is 0.
- **Two distinct synthetic converter Go types for the collision/replace tests.** `sanitizeIdentityConverter` and `sanitizeIdentityConverterAlt` are separate types (not two instances of one type) specifically so `LookupOp`'s returned value's `%T` can prove which converter is actually installed after a `Register` panic recovery or a `Replace` overwrite — a single shared type would make that assertion untestable.

## Deviations from Plan

None - plan executed exactly as written. All three tasks matched their `<action>` specs; every listed `<acceptance_criteria>` command was run literally, with real output captured below.

## Issues Encountered

- **gofmt on the new test file.** The first draft of `internal/convert/operations_test.go` had column-misaligned struct-method one-liners; `gofmt -w` reformatted it before the first test run. No logic changed, purely whitespace.
- **First `pairs_disjoint_test.go` edit left a trailing blank line** between the new header comment and the first existing test's doc comment, which counted as a non-comment diff line under the plan's literal `grep -vc '^[+-]\s*//'` check (an empty `+` line has no `//` to match). Fixed by joining the new paragraph directly onto the existing doc comment with a `//` line instead of a blank line; re-verified the check now prints 0.

## User Setup Required

None - no external service configuration required.

## Acceptance Criteria — literal command output

Task 1:

```
$ grep -c 'type OperationalConverter interface' internal/convert/convert.go
1
$ grep -c 'OperationConvert = "convert"' internal/convert/convert.go
1
$ grep -c 'type opKey struct' internal/convert/convert.go
1
$ grep -v '^//' internal/convert/convert.go | grep -c 'func (r \*Registry) LookupOp(op, from, to string) (Converter, bool)'
1
$ grep -v '^//' internal/convert/convert.go | grep -c 'func (r \*Registry) Replace(c Converter)'
1
$ grep -v '^//' internal/convert/convert.go | grep -c 'func (r \*Registry) Operations() \[\]string'
1
$ grep -v '^//' internal/convert/convert.go | grep -c 'func (r \*Registry) OperationClasses() map\[string\]map\[string\]\[\]Pair'
1
$ grep -v '^//' internal/convert/convert.go | grep -c 'func (r \*Registry) Classes() map\[string\]\[\]Pair'
1
$ grep -v '^//' internal/convert/convert.go | grep -c 'panic('
1
$ git diff -U0 HEAD -- internal/convert/converters.go | grep '^[+-]' | grep -v '^[+-][+-]' | grep -vc '^[+-]\s*//'
0
$ go build ./...
(exit 0)
$ go test ./internal/convert/ -count=1
ok  	github.com/apaderin/octoconv/internal/convert	21.701s
$ go test ./internal/api/ -run 'TestPre57Golden' -count=1
ok  	github.com/apaderin/octoconv/internal/api	0.418s
```

Task 2:

```
$ grep -c 'convert.Default.Replace(' cmd/av-worker/main.go cmd/ocr-worker/main.go cmd/pandoc-worker/main.go
cmd/av-worker/main.go:1
cmd/ocr-worker/main.go:1
cmd/pandoc-worker/main.go:1
$ grep -rn 'convert.Default.Register(' cmd/
(no output, exit 1)
$ grep -rn 'Default.Register(' internal/convert/converters.go | wc -l
7
$ git diff -U0 HEAD -- internal/convert/converters.go | grep '^[+-]' | grep -v '^[+-][+-]' | grep -vc '^[+-]\s*//'
0
$ go build ./...
(exit 0)
$ go vet ./...
(exit 0)
```

Task 3:

```
$ go test ./internal/convert/ -count=1
ok  	github.com/apaderin/octoconv/internal/convert	22.646s
$ go test ./internal/convert/ -run 'TestSanitizeIdentityPairDisjointFromConvert' -count=1 -v
--- PASS: TestSanitizeIdentityPairDisjointFromConvert (0.00s)
PASS
$ go test ./internal/convert/ -run 'TestRegisterPanicsOnTripleCollision' -count=1 -v
--- PASS: TestRegisterPanicsOnTripleCollision (0.00s)
PASS
$ go test ./internal/convert/ -run 'TestOneOperationPerEngineClass' -count=1 -v
--- PASS: TestOneOperationPerEngineClass (0.00s)
PASS
$ grep -c 'Operation() string' internal/convert/operations_test.go
3
$ grep -c '"pdf"' internal/convert/operations_test.go
13
$ git diff -U0 HEAD -- internal/convert/pairs_disjoint_test.go | grep '^[+-]' | grep -v '^[+-][+-]' | grep -vc '^[+-]\s*//'
0
$ gofmt -l internal/convert
(empty)
```

Overall verification:

```
$ go build ./...                                              -> exit 0
$ go vet ./...                                                 -> exit 0
$ gofmt -l .                                                   -> (empty)
$ go test ./internal/convert/ -count=1                         -> ok, 21.708s
$ go test ./internal/api/ -run 'TestPre57Golden' -count=1      -> ok, 1.062s
$ go test ./... -count=1                                       -> all packages ok (including DB-backed internal/jobs, internal/db, internal/e2e -- DATABASE_URL was available, none skipped)
$ git diff --diff-filter=D --name-only HEAD~1 HEAD (each commit) -> (empty, no deletions in any of the three task commits)
```

## Next Phase Readiness

- `internal/convert.Default` now exposes the full `OperationClasses()` tree; Plan 04 can render the new `operations` key in `GET /v1/formats` directly from it without touching `internal/convert`.
- `LookupOp`/`SupportsOp`/`EngineForOp` are ready for `internal/api/handlers.go` (Plan 03/04) to read `job.Operation` and route admission through, and for `internal/worker`'s consolidated `Runner` to do the same on the consumption side.
- `Register`'s panic-on-collision means Phase 60's `Default.Register(SanitizeConverter{})` will fail loudly and immediately if its declared triple ever collides with an existing one -- no silent-misroute risk to design around.
- No blockers for 57-04 (which reads `Classes()`/`OperationClasses()` and the golden-pinning suite from 57-01) or 57-06.

---
*Phase: 57-operation-dimension-output-roles*
*Completed: 2026-08-09*

## Self-Check: PASSED

All seven created/modified files confirmed present on disk (`ls -la`); all three task commit hashes (`089aafc`, `74506ae`, `7b5cf7c`) confirmed present in `git log --oneline --all`.
