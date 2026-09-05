---
created: 2026-09-05
area: tectonic
severity: medium
origin: found while testing whether multi-file projects are even possible (todo 2026-09-04-tex-multi-file-projects)
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
