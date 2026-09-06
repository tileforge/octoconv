---
created: 2026-09-05
completed: 2026-09-06
area: tectonic
severity: medium
origin: found while testing whether multi-file projects are even possible (todo 2026-09-04-tex-multi-file-projects)
resolution: closed by Landlock confinement -- a FOURTH option this todo did not cost
---

# `--untrusted` does not confine file reads to the job's directory

## What was measured

Testing whether the engine can read a sibling file — the premise multi-file
LaTeX projects depend on — showed that it can read *any* file, by absolute path,
under `--untrusted`:

```latex
\newread\f \openin\f=/etc/hostname \read\f to \line ... \line
```

typeset the container's hostname **into the PDF the client gets back**. The same
read against `/proc/self/environ` returned the worker process's environment.

That environment carried `DATABASE_URL`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`,
`API_KEY_SALT` and `WEBHOOK_SIGNING_SECRET`. A `.tex` was a credential read.

TEX-04 proved `\write18` is refused, by running a hostile document. It never
asked whether `\input`/`\openin` could leave the job's directory, and nothing
else did either.

## What shipped

The engine's environment is now an allowlist — `PATH`, `HOME`,
`TECTONIC_CACHE_DIR`, plus `TMPDIR`/`SOURCE_DATE_EPOCH`/`LANG`/`LC_ALL` when
set. Verified live on the running worker: the child process's `/proc/<pid>/environ`
holds three variables and no secret.

That removes what the reads are *worth*. It does not confine the reads.

## What is still open

A document can read any world-readable file in its own container. Today that is
`/etc/passwd`-class files, the TeX bundle and the binary — nothing secret, and
the image contains nothing else. It stops being true the moment anything
sensitive is mounted into that container (a credentials file, a shared volume).

Options, in rough order of cost:

1. **Mount namespace per job** (`unshare -m`, bind only the job directory and
   the bundle read-only). Real confinement; needs the capability in the
   container, which the chart currently does not grant.
2. **Run the engine from a scratch directory with the bundle copied in**, so
   even a successful traversal finds nothing. Cheap, partial.
3. **Ask upstream** whether tectonic intends `--untrusted` to confine reads;
   its own docs describe it as disabling shell escape and search paths.

Option 1 is the only one that makes the guarantee structural, which is the bar
TEX-02 and TEX-04 are held to.

## How it was closed — and why none of the three options above were used

The list has a hole in it. It asks which *privileged* mechanism can confine a
process, and every answer costs a capability the chart does not grant. Landlock
inverts the question: an **unprivileged process restricts itself**, the
restriction is inherited across `execve`, and it cannot be lifted afterwards.
It needs no capability, no privileged securityContext and no change to the pod
spec. Measured first, as `nobody` inside the shipped image: `ABI version 8`.

`internal/sandbox` applies the ruleset; `cmd/tectonic-worker __sandbox-exec`
is the mode that applies it and then `syscall.Exec`s the engine, because
Landlock restricts the *caller* and there is no hook between fork and exec that
can run Go. The engine gets the job directory read-write, the bundle, the format
cache and the system directories read-only, and nothing else.

### Measured, live, through the API

The same document, submitted twice to the running service — `\openin` on
`/etc/hostname`, then `\newpage` if the read succeeded, so the outcome is the
PDF's **page count** and not something font encoding can obscure:

```
WITH the sandbox (the default)         1 page   3 022 bytes
TECTONIC_SANDBOX_DISABLED=true         2 pages  4 709 bytes
```

Directly in the container, under the same ruleset: `/etc/hostname`,
`/etc/passwd`, `/proc/self/environ` and `/root` all refused; writes to the
bundle, the format cache and `/tmp/../etc/passwd` all refused; the job directory
writable; an ordinary multi-file project still returns an 8 292-byte PDF.

### Two defects this found in the fix itself

Both came from running it rather than reasoning about it, which is the whole
argument for probing your own fix:

- **The ruleset would not build at all.** The bundle is a *file*, and Landlock
  rejects a rule granting directory rights on a non-directory with `EINVAL`, so
  every compile failed with `add rule .../octoconv-bundle.ttb: invalid
  argument`. `addPath` now narrows the grant on a non-directory.
- **No `/dev`.** Every `2>/dev/null` inside the sandbox failed with `EACCES`.
  `/dev/null` is now writable and `/dev/{urandom,random,zero}` readable — named
  one at a time, never `/dev`.

### What review then found, over four rounds

Nine findings from codex, and the two that matter most are the ones the live
proof could not have caught or actively hid:

- **Landlock is per-thread.** A goroutine may migrate between `Apply` and
  `syscall.Exec`, and execve carries the security state of the thread that
  CALLS it -- so the engine could have run entirely unconfined, intermittently,
  producing a correct PDF and no error. Every probe reported confinement
  because it happened to stay on one thread. `Apply` now locks the OS thread
  and never unlocks it, and the helper refuses to exec if the tid changed.
- **A recursive grant defeated the per-job grant, three times through three
  different doors**: `TMPDIR` pointing at the shared `/tmp`; then
  `TECTONIC_CACHE_DIR=/tmp`, a legitimate pre-warm layout; then a cache path
  that CLEANED somewhere harmless and RESOLVED onto `/tmp` through a symlink.
  Fixing the third as a third fix would have invited a fourth, so it is now an
  invariant on the whole spec -- no read-only grant may be an ancestor of a
  writable job directory -- compared on resolved paths.

The other seven were the sandbox refusing setups that WORKED before it: an
unset cache dir, an empty one, a stale format, an engine resolved outside the
granted roots, a lost diagnostic. That is this engine's other recurring defect
and the sandbox gave it a new cause.

### Five greens that were not evidence

Each looked like proof, and each was caught only by checking what made it
green. Recorded because the pattern is the finding:

1. an inversion that did not compile, so it exercised nothing;
2. a refusal that came from the wrong branch (the shared temp directory also
   holds no `*.fmt`);
3. a process-group scan run with a `ps` this image does not have -- zero
   survivors found among zero processes;
4. an inversion that SKIPPED rather than failed, because the test asked the
   function under test whether its own condition held;
5. a boot-time measurement that matched a stale log line from the previous
   container.

### What is deliberately NOT closed by this

The engine can still read what is granted: the bundle, the format cache and the
system directories. That is the irreducible set — it cannot typeset without
them — and none of it is secret. The claim is confinement, not isolation.

A kernel without Landlock makes the worker **exit at startup**;
`TECTONIC_SANDBOX_DISABLED=true` is the one documented way to accept that, and
it logs the cost on every boot. The sandbox failing to establish at job time is
classified **retryable** (`convert.ErrTeXSandboxUnavailable`), unlike every
other tectonic failure, because it is a property of the node rather than of the
document — left terminal, one bad node would fail every tex job routed to it
and tell each client their file was broken.
