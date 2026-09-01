---
phase: 60-sanitize-eighth-engine-class
plan: 06
status: complete-with-open-item
completed: 2026-09-01
commits: ["abd6f21"]
---

# 60-06 — The eighth engine class, and the one number it does not have

## What was built

`probe`: the eighth engine class and the first that converts nothing. Engine
constant, `OperationSanitize`, `SanitizeConverter` (identity pairs derived from
`sanitizeFormats`), task type `probe:sanitize`, queue, retry schedule,
`ProbeUniqueTTL`, `ProbeDescriptor`, `cmd/probe-worker`,
`Dockerfile.probe-worker`, a compose service, a chart deployment and the eighth
ScaledObject.

`POST /v1/jobs` with `operation=sanitize` no longer returns 422 — as a
consequence of registering a converter, not as a special case in the handler.

## The checkpoint, and what it resolved to

`autonomous: false`, blocking on SC5's **measured** timeout. The operator's
answer was to record the measurement and keep the placeholder rather than
adopt a number derived from an unrepresentative corpus.

`PROBE_ENGINE_TIMEOUT` stays at **180s, marked PLACEHOLDER** in `.env.example`,
all 11 compose services, the chart ConfigMap, and
`internal/api/estimate.go`'s provenance table — where `EngineProbe` is
`EstimateSourceUnmeasured`, so `POST /v1/inspect` reports
`estimate_source: unmeasured` on every sanitize offer. Values derived from it
(190s grace, 200s `stop_grace_period`, the asynq `ShutdownTimeout`) are
provisional for the same reason and must move in the same edit.

Open item:
`.planning/todos/pending/2026-09-01-probe-engine-timeout-unmeasured.md`.

**SC5 is therefore NOT satisfied.** Everything else in it is — own queue,
binary, container, fail-closed input validation, transient/terminal
classification, an eighth ScaledObject leaving the seven byte-unchanged — but
the criterion says ИЗМЕРЕННЫЙ, and 180s is not. Recording the phase as
complete would be the exact class of false claim the phase spent five plans
building machinery against.

## The measurement that was run, and why it was not enough

`evidence/probe-timeout-20260901-repo-fixtures.txt` (committed).

Seven repository fixtures, 331 B – 11.6 KB, in the measurement container at
the deployed limits: median 0.101s, p95 0.322s, all seven sanitized **and
verified**. Its own recommendation is 1s, which would fire on the first real
document.

It was still worth running. It establishes three things a production
measurement would otherwise have to discover the hard way:

* the pipeline works against the ghostscript the deployed image actually
  resolves to;
* the harness and its fail-closed corpus check both work before anyone points
  them at real documents;
* a floor — ~4 ms for an image, ~100 ms for a small PDF — so whatever the real
  number is, it is dominated by document size rather than by constant cost.

## The ghostscript version, found by looking

The developer host runs gs **10.07.1**. `debian:bookworm-slim`, which
`Dockerfile.probe-worker` builds on, resolves to **10.00.0**. A timeout
measured on the host would describe a machine nobody runs.

`scripts/probe-timeout-measure.sh` therefore builds `Dockerfile.probe-measure`
from the same suite and runs the harness inside it at `--cpus 2.0 --memory 1g`
— the same discipline `pandoc-timing-measure.sh` and `ocr-timing-measure.sh`
already apply by running inside their own engine images.

The pipeline was then **verified** against 10.00.0 rather than assumed: same
threat markers gone, `-dOmitInfoDate` honoured, no `/ObjStm`, PDF-1.4 output.

## Three defects the tests found

**init() order.** Go runs a package's `init()` functions in file-name order,
and `converters.go` sorts before every `sanitize*.go` — so `SanitizeConverter`
was registered while `sanitizeFormats` was still empty. `Pairs()` returned
nothing, no triple was registered, and `operation=sanitize` would have kept
returning 422 with the whole implementation sitting right there.
`sanitizeFormats` is now a map literal: package variables initialize by
dependency, not by file name.

What caught it was a check added in the same commit. `Default.Classes()` walks
`OperationConvert` only and is structurally blind to the first non-convert
engine — phase 57's own comment called that "a known, intentional consequence
Phase 60 will revisit". This is the revisit: the completeness test now also
cross-checks `OperationClasses()`, and asserts specifically that something
routes `EngineProbe`. Without it, `AllEngines()` would have listed an engine an
operator could set a quota against and no job could ever reach.

**Two hand-maintained completeness guards.**
`TestAllConvertQueuesCoversEveryEngine` failed not because the eighth queue was
missing but because its own hardcoded list had not been updated. It now derives
the expected set from `convert.AllEngines()`. A guard that needs updating in
the same commit as the thing it guards is checking that someone remembered
twice, not that the two agree.

**The report had nothing delivering it.** Plan 60-05 built `SanitizeReport` and
60-06's task list did not include emitting it, so the phase would have ended
with a report shape no job produces — SC4 unmet with every piece present. See
below.

## Beyond the plan, deliberately

`EngineDescriptor` gains an optional `SideOutputs` hook, mirroring
`PreConvertGuard`: nil for all seven other engines (pinned by
`TestOnlyProbeDeclaresSideOutputs`), non-nil for probe. `SanitizeConverter`
writes the report beside its output; `probeSideOutputs` reads it back and
`process()` stores it as the `role=report` output at ordinal 1, after the
primary is stored.

Three choices worth naming:

* **A sidecar file, not a widened `Converter` interface.** `Convert` has no
  channel for a second result, and widening it would touch seven engines with
  no report to give. A file in the job's own temp directory is a legitimate
  channel between two steps of one pipeline, and it has a property a returned
  value would not: the report is written by the same call that produced the
  output, so the two cannot go stale relative to each other.
* **After the primary upload, not before.** A report describing a result the
  client cannot fetch is a record of nothing.
* **A missing sidecar fails the job.** The alternative — succeed with the
  sanitized file and no report — hands back a file that went through an
  operation named *sanitize* with no statement of what was removed from it,
  which is the outcome SANI-05 exists to prevent.

`TestNewRunnerInstallsSideOutputs` exists because the descriptor declaring the
hook proves nothing: `NewRunner` is what copies it onto the `Handler` that
`process()` reads, and deleting that one line left every other test green.

## Terminal/transient, derived rather than copied

| Failure | Class | Why |
|---|---|---|
| verification found residue | terminal | deterministic on the output, which is deterministic on the input |
| verification could not run | terminal | the verifier reaches the same verdict on retry |
| ghostscript timeout | **transient** | depends on load, not on the file — the OPPOSITE of `isPandocTerminal`, same polarity as `isOCRTerminal`, reached independently: both wrap an external renderer whose worst case is load |
| ghostscript parse failure | terminal | a file gs cannot read now is a file gs cannot read in twenty seconds |
| options present | terminal | sanitize takes none; a populated Opts map is a corrupt column value (D-10) |

Both verification outcomes stay distinguishable in the client-facing message,
and a test asserts they differ: "still contained something" says the service is
at fault, "could not be checked" says the service refuses to make a claim.

## The three golden pins, all changed, none regenerated

| Golden | What changed | rev-list |
|---|---|---|
| `pre58_formats` | `operations` gained `sanitize` | 1 |
| `pre59_inspect_pdf` | each report's `operations` gained sanitize offers | 1 |
| `pre60_sanitize_rejection` | the sanitize case moved from Gate 1 to Gate 2 | 1 |

All three golden files are **byte-untouched** on disk. The assertions narrowed
instead, each to one named exception recorded in its own suite's header, with
everything else still compared as the exact bytes it was pinned as:

* `pre58` splits both bodies at the top level with `json.RawMessage` and
  compares `engines` and `operations.convert` byte-for-byte. The `engines`
  half is the load-bearing one: probe serves no convert pair, so the eighth
  class must be invisible there — the claim phase 57's `Classes()` filter was
  written to make true, checkable for the first time now that a non-convert
  operation exists.
* `pre59` compares every body field and every non-sanitize `operations`
  element as raw bytes; the exception subtracts elements, it does not relax
  the comparison of what remains.
* `pre60` asserts sanitize is now refused by Gate 2 with
  `unsupported_conversion` — a stronger statement than the case merely
  vanishing, because a 202 there would mean the pair gate was lifted too, and
  nothing asked for that.

Regenerating would have deleted the evidence that the 422 ever existed.

## Additivity, proven

`helm template` before and after, per class: the seven existing ScaledObjects
render **byte-identical**. The one diff observed was a single trailing newline
in the pandoc render, isolated to helm's multi-manifest join by re-rendering
with the new template moved aside — the pandoc object's own content is
unchanged.

`scripts/ci-helm-checks.sh` moved from 7 ScaledObjects to 8 and now includes
probe in its per-class isolation loop; the gate exits 0.

Migration 0015's "intentional divergence #1" is marked **CLOSED in place**
rather than deleted: a note describing a divergence that no longer exists is
the same class of defect as a comment promising a route that never existed.

## Inversions (D-16)

| Defence removed | Failure |
|---|---|
| `Default.Register(SanitizeConverter{})` | "AllEngines() lists \"probe\" but no registered converter routes it" |
| probe dropped from `AllConvertQueues()` | "contains engine \"probe\" 0 times, want exactly 1" |
| `ProbeDescriptor.SideOutputs` undeclared | "the sanitize report would never be stored" |
| timeout reclassified to pandoc's polarity | "isProbeTerminal(timeout) = true, want false" |
| `NewRunner`'s install line deleted | "the descriptor declares the hook and nothing would call it" |

## Gates

`gofmt -l .` clean · `go vet ./...` clean · `go build ./...` clean ·
`go test ./... -count=1` exit 0 · `OCTOCONV_REQUIRE_BINS=1` exit 0 ·
`scripts/ci-helm-checks.sh` exit 0 · `docker build -f Dockerfile.probe-worker`
succeeds with gs/unzip/file present and `USER nobody` · `yq -e '.'` parses
`docker-compose.yml` and `docs/openapi.yaml`.

Local only — no GitHub Actions run.

---

## Review round 1 (codex, PR #12) — six findings, all real

Recorded here rather than folded into the sections above, because what the
phase shipped and what review found are two different facts and merging them
would hide the second. Fixed in `4fa4b39`, each with an inversion reproducing
the reported failure.

**The severe one: the report was never stored as `role=report`.**
`jobs.Repo.AddOutput` omits the role column so the DB default applies; `Role`
was set on the struct and silently discarded. Every sanitize job would have
ended with two primary outputs and failed `MarkDone`'s D-13 guard on every
attempt — the operation did not work at all.

`AddOutput`'s doc comment had said since phase 57 that a caller writing
`role='report'` "needs a dedicated write path for that column". The
instruction was written for this exact moment and not read. The tests could
not catch it: `TestProbeSideOutputsCarriesTheReport` exercised the hook in
isolation, and nothing drove `process()` against a real repository.

**Three more that a passing test suite was blind to for the same reason —
each test checked the piece rather than its use:**

* an OOXML attachment was found by folder (`*/embeddings/`) rather than by
  relationship type, so `word/media/attached.pdf` survived while
  `embedded_file` appeared in both `removed` and `verified`;
* `SanitizedOOXMLExtension` implemented D-06 and was called from nowhere — its
  own unit test stayed green throughout, because it tested the helper, not the
  registry the worker reads;
* colour management (`APP2`/ICC, `APP14`/Adobe) was dropped with the metadata,
  making this phase's losslessness claim false in the sense a client means:
  scan bytes identical, picture changed.

**Two where the check itself was wrong:**

* the image verifier scanned the whole file for chunk types, and compressed
  data eventually contains any four-byte sequence — a false positive there
  does not mislabel a job, it *deletes a correct output*;
* a negation cue sheltered a whole sentence, so `"The output is safe and does
  not contain macros."` passed the safety-claim ban. Third correction to that
  matcher: the must-not-match table had made it narrow enough to survive, and
  narrow is not the same as correct.

### What this says about the phase's own machinery

Five plans went into making "we checked" mechanically different from "we said
so", and the review still found six things — three of which were tests that
proved a component rather than its use. The pattern is specific and worth
carrying forward: **a test that calls the function directly cannot see that
nothing else calls it.** Two of the six were dead code paths, and one was a
field passed to a function that ignored it.

The verification the phase did build worked exactly as intended where it was
pointed — 60-04's verifier found the ghostscript leak on its first run. It was
never pointed at the seams between components, and that is where all six lived.
