---
phase: 64-tectonic-ninth-engine-class
plan: 05
status: complete
completed: 2026-09-04
requirements: [TEX-05]
operator_decision: "2 MiB input ceiling + 271s timeout, chosen from three measured options before any of it was written"
---

# 64-05 — the number, and the bound that makes it a number

## What the measurement said

`ceil(25.76 s/pass × 7 passes × 1.5) = 271s`, in the worker image at the chart's
own 2 CPU / 1 GiB, `--network none`, `USER nobody`.

Every term is measured or the engine's own:

- **7 passes** is tectonic's own rerun ceiling. It stops and says so —
  `TeX rerun seems needed, but stopping at 6 passes` — and every bibliography
  document in the corpus hits it, at every size. That is why the derivation is
  not a p95: with pass counts of 2, 3 and 7 for the same body text, a percentile
  reports which classes somebody put in the corpus.
- **25.76 s/pass** comes from a 2 143 444-byte document (745 pages, 180.3s),
  not from a small one scaled up.
- **×1.5** covers two named things: the 2.5% of the job outside the engine
  (measured through the live API — 184.9s in-worker against 180.3s engine-only)
  and the gap between this host and a deployment. OCR and pandoc needed ×2.0
  because their tails are open; this tail is closed by the pass ceiling.

## The measurement's real output was a finding

Cost is **linear in source size** at a fixed pass ceiling: 1 MiB → 373 pages →
90.0s, 2 MiB → 745 pages → 180.3s. And this engine had inherited the
document-shaped engines' **100 MiB** upload ceiling — roughly 36 000 pages,
~9000s, **ten times** the 900s reconciler cap.

So the timeout bounded nothing. The lever is the Path-B move OCR already made —
bound the input, never shrink the margin — and the two constants are now pinned
as one decision, the way `PROBE_MAX_PAGES=200` and `PROBE_ENGINE_TIMEOUT=600s`
are. The operator chose the pair from three measured options before anything was
written.

## Three things the harness caught that reading would not have

1. **Three corpus files did not compile** — a blank line inside an `equation`
   environment. The fail-closed corpus check found them before any timing was
   recorded, which is the entire reason that check exists.
2. **The first run's numbers were noise.** `single-l` (30 pages) timed *faster*
   than `single-s` (1 page): the per-page term was inside the jitter. Hence the
   `xl` size and three repetitions per file keeping the **slowest** — a bound has
   to hold on the unlucky run.
3. **"Measured at the ceiling" was very nearly a claim nobody had checked.** The
   document the number comes from is 2 143 444 bytes — **2.2% above** the
   2 097 152-byte ceiling that now refuses it. Conservative, but the wording was
   wrong in nine places, and it was found by POSTing the measured file to the
   live API and getting a 413.

## Every dependent number re-derived in the same edit

Grace periods 281s/291s. `estimate_source` → `measured`. The KEDA
stabilization **re-argued rather than assumed**: it held at `null`, which
probe's did *not* when its own placeholder was measured (180s → 600s took the
profile to 900s). Every "300s is a PLACEHOLDER" comment corrected rather than
left to rot — that is the exact failure this phase already found once, in
`cmd/tectonic-worker`, where probe's comment had been copied wholesale.

`envExampleClaimsMeasured`'s placeholder branch now has **no live caller**,
because every timeout in this build is measured. Rather than leave a branch that
looks alive, `TestEnvExampleClaimsMeasured` exercises both scopes directly.

## Verified on the running service

```
{"operation":"convert","target":"pdf","engine":"tectonic",
 "estimated_max_seconds":271,"estimate_source":"measured",
 "available":true,"max_bytes":2097152}
```

Both halves of the decision on the wire, from the API built out of this tree
with the real worker consuming the queue.
