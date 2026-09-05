---
phase: 64-tectonic-ninth-engine-class
plan: 02
status: complete
completed: 2026-09-04
requirements: [TEX-01, TEX-02, TEX-04]
---

# 64-02 — the converter, the image, and two guarantees that had to be run to be believed

All five tasks are done. Two things went differently from the plan, and both are
worth more than the tasks that went to plan.

## The wave boundary was wrong, and registering the converter proved it

Task 2 says to register `TectonicConverter` in `converters.go`, with the loop —
queue, descriptor, worker cmd, chart — deferred to 64-04. Doing exactly that
broke **nine contracts at once** and advertised `latex → pdf` on `GET /formats`
with no worker behind it: the precise defect Phase 60 shipped with sanitize.

A registry entry is not a local edit. It is a promise the rest of the system is
then expected to keep — engine-class queue, sweep route, upload ceiling,
published timeout, estimate source, README entry, DB CHECK value. So the loop
was closed here in one commit (`fb9d0c2`) instead of left open across two plans:

- `0016_tectonic_engine.sql` widens `jobs_engine_check`, **verified both ways on
  a live database**: `tectonic` inserts, `nonsense` is still refused. A migration
  that only proves the new value is accepted has not shown the constraint still
  constrains.
- `cmd/tectonic-worker` fails **at startup** with no bundle, not per job. A
  worker that cannot typeset anything should not sit consuming a queue turning
  one misconfiguration into a stream of failures that each look like a bad
  document.
- `pre58_pinning_test` gets its second permitted exception through a new
  `withoutEngine` helper that asserts the addition is **present** before
  stripping it — a strip that silently matched nothing would hide the next
  regression rather than tolerate this one.

`TECTONIC_ENGINE_TIMEOUT` is 300s and is a **placeholder**, stated plainly
because that exact sentence went stale in four files for the probe engine while
the value beside it had already changed. 64-05 measures it.

## TEX-04 — the obvious inversion proves nothing

Dropping `--untrusted` proves nothing, because shell-escape is **off by
default**: the document does not execute either way. Three configurations, all
measured before the test was written:

| argv | sentinel |
|---|---|
| `--untrusted` | none |
| `--untrusted` **and** `-Z shell-escape` | none ← the guarantee |
| `-Z shell-escape` alone | **written** ← the fixture is not inert |

The third row is what makes the second worth anything. Two earlier fixtures were
exactly the failure it guards against: one used a relative sentinel path, so the
command *had* run and the file landed in tectonic's cwd; the other put both
vectors in one document, where `\input` aborted before `\write18` was reached.
`\input{|cmd}` turns out not to be implemented in tectonic at all.

## TEX-02 — same shape, and it found a real defect

| configuration | result |
|---|---|
| `-b bundle`, `--network none` | PDF, 1 page, 2s ← the guarantee |
| no `-b`, `--network none` | exit 101, names `relay.fullyjustified.net` ← the network really is severed |
| no `-b`, network on | PDF, `note: downloading latex.ltx` ← the risk is real |

The third row is the threat executed: with no bundle, the engine downloads what
the **document** asked for. That is tectonic's default behaviour, not a
hypothesis. Full transcript in `evidence/64-02-tex02.txt`.

**And row 1 failed on the first attempt, for no bundle reason:**

```
error: cannot write format file texput.fmt
caused by: Permission denied (os error 13)
```

`USER nobody` has no home, so the format cache resolved somewhere unwritable and
tectonic fell back to the working directory. **Every job would have failed.** The
Go tests could not see this — they run as the developer. Only the image, under
the user it actually deploys as, could.

Fixed with `TECTONIC_CACHE_DIR` on a nobody-owned directory, **warmed at build
time**: an unwarmed cache rebuilds the 36 MiB format on every job, which would
put format compilation inside the clock 64-05 measures. The build then asserts a
`*.fmt` exists rather than trusting the warm-up's exit status.

## The bundle comes from the build context, not a URL

The first Dockerfile `curl`'d it from this repository's release and got a 404:
**the repository is private**, so the asset is not anonymously reachable, and
authenticating would put a credential inside every image build. The digest is
still verified — against the bytes that land in the image, which is the property
that matters. A URL would only have proved that something at that URL matched.

## What 64-03 inherits

`-p` is in the argv and must stay: the engine's chatter is the only source of a
diagnosable failure, and 64-03's package-name extraction reads it. Capture
tectonic's **real** output as a fixture before writing that parser — predicting
an engine's error text is the same mistake as predicting a parser.
