---
created: 2026-09-01
area: config
severity: medium
resolved: 2026-09-02
---

# PROBE_ENGINE_TIMEOUT is a placeholder, not a measurement

Phase 60 shipped the `probe` engine class with `PROBE_ENGINE_TIMEOUT=180s`. That
number is **invented**. It is distinct from the project's four measured timeouts
(pandoc 65s, audio 742s, ocr 744s, av 753s) — but distinctness is not
provenance, and copying any of those four is a recorded project antipattern
(PITFALLS.md Pitfall 7), which is precisely why an unmeasured value was left in
place rather than a borrowed one.

The build says so everywhere the number appears, so nothing currently
misrepresents it:

* `.env.example` — a block explaining what it is and how to replace it
* `docker-compose.yml` — all 11 services, each marked `# PLACEHOLDER`
* `deploy/chart/octoconv/templates/configmap.yaml`
* `internal/api/estimate.go` — `engineEstimateSource[EngineProbe]` is
  `EstimateSourceUnmeasured`, so `POST /v1/inspect` reports
  `estimate_source: unmeasured` for every sanitize offer

Values **derived** from it are provisional for the same reason and must move in
the same edit: `probeWorker.terminationGracePeriodSeconds` (190s), compose
`stop_grace_period` (200s), and the asynq `ShutdownTimeout` computed in
`cmd/probe-worker/main.go`.

## What is needed

A run of `scripts/probe-timeout-measure.sh <corpus-dir> <output-file>` against
documents representative of real load — the largest PDFs and OOXML packages that
actually arrive, including the worst case. The script builds a container from the
same debian suite as the worker image, runs the full pipeline (sanitize plus the
mandatory independent verification) at the deployed cgroup limits, prints the
nearest-rank distribution and a recommendation with its formula, and fails the
run if any file does not sanitize or does not verify.

## What was already measured, and why it was not enough

`.planning/phases/60-sanitize-eighth-engine-class/evidence/probe-timeout-20260901-repo-fixtures.txt`

Seven repository fixtures, 331 B – 11.6 KB, in the measurement container against
gs 10.00.0: median 0.101s, p95 0.322s, all seven sanitized and verified. The
recommendation it produces is **1s**, which would fire on the first real
document. The corpus is three orders of magnitude too small.

That run is still worth having: it proves the pipeline works against the
ghostscript the deployed image actually resolves to (10.00.0, not the developer
host's 10.07.1) and that the harness and its fail-closed corpus check work
before anyone points them at real documents.

## Two more reasoned-not-measured numbers landed with it

Both came out of PR #12 round 2 and share this item because they share its
cause -- no corpus:

* `defaultMaxEngineBytes[EngineProbe] = 50 MiB` (`internal/api/api.go`), half
  the ordinary document ceiling. The reasoning is the memory profile: the
  image walkers `os.ReadFile` the input, the OOXML path buffers every
  uncompressed part plus the rebuilt archive, and the PDF path hands the
  result to pdfcpu, which parses it into its own address space -- inside a
  1 GiB worker. That is an argument, not a measurement.
* `PROBE_WORKER_CONCURRENCY=1`, on the same argument.

The same corpus run that fixes the timeout should record peak RSS per input
size, which settles both.

## Risk while it stands

A probe timeout is classified **transient** (`isProbeTerminal`), so a too-tight
bound spends retries rather than failing jobs outright — but it spends them on
every busy minute, and `PROBE_MAX_RETRY=2` means a document that genuinely needs
more than the bound fails after three attempts. A too-loose bound holds a worker
slot for up to 180s per stuck job. 180s sits well under the 900s
`RECONCILER_ACTIVE_STALE_AFTER` cap, so neither direction currently risks the
reconciler.

---

## Related, separate: four OOXML formats the API cannot admit

`dotm`, `potm`, `ppsm` and `xltm` are removed from `sanitizeFormats`
(`internal/convert/sanitize.go`) even though `sanitizeOOXML` handles them
correctly.

`SniffContainer` identifies an OOXML package by its ROOT PART, and a `.dotm`
has the same root part (`word/document.xml`) as a `.docm`. A `.dotm` upload is
therefore detected as `docm` and rejected as a declared/detected mismatch
before it can reach the sanitizer. Registering those pairs advertised
something no request could invoke — the same defect class as a helper that
implements a decision and is never called (both found in PR #12).

Closing it needs detection to read `[Content_Types].xml`'s main-part override
rather than the root part's name. That touches the convert path and the pinned
`pre58_formats` golden, so it is a change with its own blast radius rather
than a line to add here.


---

## RESOLVED 2026-09-02

Measured: `PROBE_ENGINE_TIMEOUT=600s`, derived as
`ceil(2.0 s/page × 200 pages × 1.5)` from a run in the measurement container at
2 CPU / 1 GiB against gs 10.00.0. Evidence at
`.planning/milestones/v1.13-phases/60-sanitize-eighth-engine-class/evidence/probe-timeout-20260902-measured.txt`.

The two companion numbers this item also tracked did NOT close with it:

* the 50 MiB upload ceiling stays reasoned rather than measured — the run
  recorded wall time, not peak RSS per input size;
* `PROBE_WORKER_CONCURRENCY=1` likewise.

Neither is urgent: memory was never the binding constraint in the run (84 MiB
of 1 GiB while pinned at 99.9% of 2 CPUs — this engine is CPU-bound). Recorded
here rather than reopened, because the finding the measurement DID produce —
that there is no page ceiling at all — is the one that mattered and became
PROBE-01/02 in v1.14.
