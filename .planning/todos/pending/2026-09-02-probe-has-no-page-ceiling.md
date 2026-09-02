---
created: 2026-09-02
area: sanitize
severity: medium
resolves_phase: 63
---

# The probe engine has no page ceiling, so its timeout is the only bound

Measuring `PROBE_ENGINE_TIMEOUT` (2026-09-02) established the cost model, and
the model is the finding: **cost is per PAGE, not per byte, and it is linear.**

```
 20 pages   39.738s   1.987 s/page
100 pages  200.437s   2.004 s/page
240 pages  481.308s   2.005 s/page
```

A 219 KB document took 481 seconds; a 2.2 MB one took 39.7. Byte size is
irrelevant. A full-page scan costs ~2.0s, a text page ~0.15s.

`PROBE_ENGINE_TIMEOUT=600s` is derived as `ceil(2.0 s/page × 200 pages × 1.5)`
— OCR's own shape, per-unit cost × ceiling × margin. **But probe has no
ceiling.** OCR bounds itself with `OCR_MAX_PAGES` and refuses an over-long
document up front with `too_many_pages`; probe accepts anything under the
50 MiB upload ceiling and discovers the problem by running out of time.

## What that costs today

A document beyond roughly 300 pages of full-page scans fails on timeout. The
failure is:

* **late** — after ten minutes of CPU, not at admission;
* **retried** — a probe timeout is classified TRANSIENT (`isProbeTerminal`),
  correctly, because a timeout normally means load. A document that is simply
  too long is not load, so it burns the full `PROBE_MAX_RETRY` budget
  reproducing the same failure;
* **opaque** — the client gets `engine_error`, not "this document has more
  pages than the service will sanitize".

Three attempts × 600s is thirty minutes of a worker for one document that could
have been refused in milliseconds.

## Fix shape

Mirror what OCR already does:

1. `PROBE_MAX_PAGES` (a page ceiling; 200 matches the timeout's derivation).
2. A guard that counts pages before the first ghostscript pass and returns a
   sentinel — `ErrProbeTooManyPages` — mapped to the existing `too_many_pages`
   failure code, which already exists in `worker.allFailureCodes` and is
   already documented in `docs/ERROR-CODES.md`.
3. Terminal classification: too-long is deterministic on the input, so it must
   NOT be retried. `isProbeTerminal` already has the shape for this.
4. Ideally the API refuses at admission the way it does for image pixel bounds,
   so no S3 byte is spent.

## Why it was not done in Phase 60

The phase's scope was the sanitize operation and its eighth engine class. The
page-cost model only became visible when the timeout was measured, which was
the last thing the phase did. Adding a new guard, a new env var, a new
admission gate and their tests is a phase's worth of work, not a line.

The timeout is a real bound in the meantime: nothing runs unbounded, and the
900s reconciler cap is never approached.
