# Phase 58 (INSP-06/SC6) evidence

**Status as of this commit: NO LIVE RUN HAS HAPPENED YET.** This directory
currently holds only this placeholder. It exists so the harness
(`scripts/inspect-burst-liveproof.sh`) has a destination to write into on
its very first invocation, and so the shape of what belongs here is
recorded before an operator ever runs it -- not because any evidence has
been gathered.

## What will land here

`scripts/inspect-burst-liveproof.sh` writes exactly two files per run, both
named with the same UTC `RUN_ID` so repeat runs never clobber a prior one:

- `inspect-burst-<RUN_ID>-poll.tsv` -- one row per independent `/healthz`
  poll (timestamp, HTTP status, duration), taken from a pod entirely
  separate from the burst driver.
- `inspect-burst-<RUN_ID>-burst.tsv` -- one row per `POST /v1/inspect`
  request across every round (round, index, fixture ordinal, HTTP status,
  body error code, duration), plus a trailing `#`-prefixed summary block
  (restartCount before/after, applied `INSPECT_MAX_*` ceilings, observed
  `kubectl top pod` peaks against `resources.limits`).

The operator's own `tee`-captured transcript of the run (Task 3 of this
plan's `how-to-verify` step 4:
`bash scripts/inspect-burst-liveproof.sh 2>&1 | tee /tmp/inspect-gate-<RUN_ID>.log`)
is moved into this directory under the same `RUN_ID` as the third
committed artifact, alongside a `README.md` rewrite (this file) that
replaces this placeholder with the actual analysis: what SC6 proved, what
it explicitly did NOT prove (at minimum: this gate runs at `api.replicas: 1`
only, so the D-07 caveat on the in-process inspect rate budget/concurrency
semaphore remains unverified at `api.replicas > 1`; and the chart's
NetworkPolicy admitting the poller/burst pods on `:8091`/`:8090` is
verified only by `helm template` rendering, not by a live negative test,
since OrbStack ships no NetworkPolicy-enforcing CNI -- the same
documented limitation this project's own `networkpolicy-metrics.yaml`
already records for its other ingress rules), and which `INSPECT_MAX_*` /
`resources.limits` values were left as-is versus recalibrated from the
observed peaks.

## Why this file exists now, empty of any run

This plan's own harness-authoring scope (Task 1: fixtures; Task 2: this
harness plus its values overlay) is separable from, and does not include,
actually running the live gate (Task 3, `type="checkpoint:human-verify"`,
`gate="blocking"`) -- that step requires an OrbStack operator to prepare
the environment (bring the compose stack down, assemble fresh `api`/
`worker` images sequentially with a pinned tag) in ways this harness
explicitly refuses to do on its own behalf (see the harness's own header
comment, "THE ORBSTACK OPERATOR CONTRACT"). Committing this placeholder
now, rather than leaving the directory absent, makes the harness's own
`mkdir -p "$EVIDENCE_DIR"` step a no-op on first run and gives a reviewer
of this commit a concrete, checkable answer to "where will the proof live"
before that proof exists.
