---
created: 2026-10-17
area: final-live-gate
severity: low
origin: quick 261017-7y5 sweep
reworded: 2026-10-17 (quick 261017-f0i)
---

# Confirm live the `mc ls --recursive --json` shape the cad cleanup reads

## What changed

Quick 261017-f0i removed the per-prefix `mc du --json` listing from `cad_cleanup` in
`scripts/final-live-gate.sh`. The absent-prefix question this todo used to ask no longer arises.
The cleanup now lists the whole bucket once before and once after deletion with
`mc ls --recursive --json local/octoconv`, and checks the exit status, the jq parse and the
status of every value.

## What is still unmeasured

The new cleanup rests on these assumptions about the compose MinIO's `mc`. They come from upstream
mc's `ls` output and were not measured live. The image's mc reports `version DEVELOPMENT.GOGET`
(quick 261001-5ec, evidence/chainguard-minio-measure.txt:27), so the release cannot be pinned
from the repo.

- `mc ls --recursive --json local/octoconv` prints one JSON object per object.
- Each object has `"status":"success"`, `"type":"file"`, a numeric `"size"` and a string `"key"`.
- `key` is relative to the bucket: `uploads/<id>/<ord>-<name>` and `results/<id>/<ord>-<name>`,
  without the bucket name.
- A failed listing gives a non-zero exit, a value whose status is not `success` (possibly
  pretty-printed over several lines), or plain text that jq cannot parse.
- The exit status of a listing that succeeds is 0.

## If an assumption is wrong

The cleanup fails closed. Every valid job must have at least one key under `uploads/<id>/` in the
first listing. If none is found, the CLEANUP line records `jobs_without_upload=N`, nothing is
deleted for that job, and the cad step FAILs. A failed listing records `unknown`, and the step
FAILs. The run is loud and never a silent pass. The job's objects are then left in the bucket for
manual cleanup.

## Probe for the next live session

Run these inside the compose minio (`docker compose exec -T minio ...`), after `mc alias set local ...`:

1. Run `mc ls --recursive --json local/octoconv`. Record the rc and the first two lines.
2. Run the same listing against a bucket that does not exist. Record the rc and the output.
3. After one cad-liveproof job, confirm that its keys appear as `uploads/<id>/...` relative to
   the bucket.

## Close criteria

- The probe matches: move this todo to `completed/` with `completed:` and `resolution:`.
- The probe does not match: adjust the jq selection in `cad_bucket_list` and the mc stub in
  `76-11-final-gate-test.sh` together, then rerun the stub suite.
