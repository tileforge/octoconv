---
created: 2026-10-17
area: final-live-gate
severity: low
origin: quick 261017-7y5 sweep
---

# The cad cleanup reads a failed per-prefix `mc du --json` as "nothing to delete"

## What

`cad_cleanup` in `scripts/final-live-gate.sh` lists each job prefix
(`local/octoconv/uploads/<id>/`, `local/octoconv/results/<id>/`) with
`mc du --json ... | head -1 || true` and deletes the prefix only when the `objects` field is a
non-zero number. A failed listing gives an empty line, so that prefix is skipped and the
CLEANUP line still reads `result=ok`. MinIO objects of a cad-liveproof job could then stay in
the shared bucket while the gate passes.

## Why it was not fixed

- The exit status and the JSON line of `mc du --json` on an ABSENT prefix are unmeasured.
  Refused jobs have no `results/` prefix, so a fail-closed rule on the exit status or on a
  non-success `status` could fail every live run.
- Quick 261017-7y5 was stubs-only by instruction (no docker), and a stub cannot settle what
  the real `mc` prints.

## Probe for the next live session

1. Inside the compose minio (`docker compose exec -T minio ...`, after `mc alias set local ...`),
   run `mc du --json local/octoconv/uploads/<an existing job id>/` and
   `mc du --json local/octoconv/results/<an id that has no results>/`. Record the rc and
   the first line of each.
2. Then pick one of these:
   - fail the cleanup on a non-success status that the probe shows only a real failure gives;
   - verify absence after the rm with a post-delete listing of each prefix, and fail the
     cleanup if any object is left.
