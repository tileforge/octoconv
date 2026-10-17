---
phase: quick-261017-f0i
plan: 01
subsystem: final-live-gate
tags: [final-live-gate, cleanup, minio, mc, fail-closed, codex-p2, pr-37, phase-76-follow-up]
status: complete
requires: [76-11 final gate wrapper scripts/final-live-gate.sh, its stub suite 76-11-final-gate-test.sh, quick 261017-7y5 cad_cleanup exit status]
provides: [cad_bucket_list, cad_prefix_count, CLEANUP jobs_without_upload= and leftover_objects= fields, stateful mc stub]
affects: [scripts/final-live-gate.sh, .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-final-gate-test.sh]
tech-stack:
  added: []
  patterns: [discover objects from one checked listing of a container known to exist, never from per-item listings whose absent-item behaviour is unmeasured; verify deletion with a second checked listing; anchor the selection to an object that must exist so a wrong key shape fails loudly]
key-files:
  created:
    - .planning/todos/pending/2026-10-17-cad-cleanup-mc-ls-json-live-confirmation.md (git mv from 2026-10-17-cad-cleanup-mc-du-absent-prefix.md)
    - .planning/quick/261017-f0i-codex-round-3-cad-cleanup-fails-when-obj/evidence/
  modified:
    - scripts/final-live-gate.sh
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-final-gate-test.sh
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/76-11-SUMMARY.md
    - .planning/STATE.md
decisions:
  - "cad_cleanup discovers job objects from one recursive JSON listing of the bucket before deletion and verifies with a second one after; a failed, timed-out, error-carrying or unparseable listing fails the cleanup"
  - "The jobs_without_upload anchor stays: every valid job id must have a key under uploads/<id>/ in the first listing, else the cleanup fails and nothing is deleted for it, so a key shape the wrapper cannot read never passes as nothing to delete"
  - "The mc ls JSON shape is an assumption, marked unmeasured live; the todo stays pending, renamed via git mv, and owes only that live confirmation"
  - "STATE Pending Todos now lists all eight files in .planning/todos/pending/, including the 261017-doa selection-steps todo that was missing"
metrics:
  completed: 2026-10-17
  duration: "07:58 to about 09:35 UTC, most of it stub-suite runtime (seven mutation suites at about 6 min each)"
actuals:
  tokens: 8000
  tasks: 2
  commits: 2
plan_head_before: 82d3f46f7edd253aa4c2a8e10a7a8e0d90440d9c
plan_head_after: 1520527228544cc71ea2dbf077e73f8229427643
commits_list:
  - 7cc25f2 test(quick-261017-f0i): RED a failed or unread bucket listing must fail the cad cleanup
  - 1520527 fix(quick-261017-f0i): the cad cleanup lists the bucket before and after deletion and fails on a failed or unread listing
---

# Quick 261017-f0i: the cad cleanup lists the bucket before and after deletion, fail-closed Summary

**Complete, against stubs only.** A cad cleanup can no longer report `result=ok` while MinIO could not be listed, or
while job objects remain under a run prefix. The fix does not depend on how `mc` behaves on an absent prefix. The live
gate was not rerun. The JSON shape the new code reads is an assumption from upstream mc, unmeasured live, and owed
as a todo.

`commits: 2` and `plan_head_after` cover the test and fix commits. The docs commit for this SUMMARY and the STATE
commit come after it.

## The finding and the fix

Codex round 3 on PR #37 (P2, scripts/final-live-gate.sh:518 at 82d3f46). `cad_cleanup` listed each job prefix with
`mc du --json "local/octoconv/$p/$id/" | head -1 || true` and deleted the prefix only when the `objects` field was a
non-zero number. A listing that timed out or could not reach MinIO became an empty line, so the prefix was skipped
and the CLEANUP line still read `result=ok`. This was the open todo `2026-10-17-cad-cleanup-mc-du-absent-prefix.md`.
It was left open by 261017-7y5 because a fail-closed rule on that listing would have needed the unmeasured
absent-prefix behaviour: refused jobs have no `results/` prefix.

The fix (1520527) changes what is listed, not how one listing is read:
- `cad_bucket_list` (line 493) runs `tmo 300 docker compose exec -T minio mc ls --recursive --json local/octoconv`
  once, stdin from /dev/null, stderr discarded, into a file under `$STATE`. The exit status is captured (no
  always-true fallback).
- jq then turns each value with status `success`, a string `key` and a numeric `size` into `size TAB key`, and any
  other value into the line `BAD`. The jq exit status is captured as well. The listing succeeds only when both
  statuses are 0 and there is no `BAD` line.
- The bucket exists, so a failed listing is a real failure. No absent-prefix question arises.
- `cad_prefix_count` (line 500) counts and sums the keys that start with `uploads/<id>/` or `results/<id>/`. It uses
  awk `index($2, p) == 1` with the prefix passed by `-v`, never a regex built from the id.
- A valid id with no `uploads/` key increments `jobs_without_upload` and fails the cleanup.
- Each prefix with keys gets the unchanged `mc rm --recursive --force "local/octoconv/$p/$id/"`. A prefix with no keys
  (a refused job's `results/`) is skipped and is not a failure.
- A second checked listing after the rm calls counts `leftover_objects` under every run prefix of a valid id. A
  non-zero count, or a failed listing (`unknown`), fails the cleanup.
- `bucket_before`/`bucket_after`, the UUID filter, `invalid_ids`, `run_cad` and `step_cad` are unchanged. The
  per-prefix `mc du --json` and its sed parsing are gone, and so is its stub entry.

CLEANUP before and after:

```
before  CLEANUP,step=cad,run=1,scope=...,clients=...,other_clients_since=0,jobs=2,objects_deleted=8,bytes_deleted=400,invalid_ids=0,bucket_before=...,bucket_after=...,result=ok
after   CLEANUP,step=cad,run=1,scope=...,clients=...,other_clients_since=0,jobs=2,objects_deleted=8,bytes_deleted=400,jobs_without_upload=0,leftover_objects=0,invalid_ids=0,bucket_before=...,bucket_after=...,result=ok
        ...,jobs=0,objects_deleted=0,bytes_deleted=0,jobs_without_upload=unknown,leftover_objects=unknown,...,result=fail   (first listing failed)
        ...,jobs=2,objects_deleted=8,bytes_deleted=400,jobs_without_upload=0,leftover_objects=unknown,...,result=fail       (second listing failed)
        ...,jobs=2,objects_deleted=8,bytes_deleted=400,jobs_without_upload=0,leftover_objects=8,...,result=fail             (rm left objects)
        ...,jobs=2,objects_deleted=0,bytes_deleted=0,jobs_without_upload=2,leftover_objects=0,...,result=fail               (keys not readable as job prefixes)
```

`jobs=` counts the ids read in the loop. As before the fix, the loop does not run when the cleanup has already failed,
which now includes a failed first listing, so it reads 0 there.

A cleanup failure reaches the verdict through the path 261017-7y5 built: `cad_cleanup` returns non-zero,
`run_cad` records `cleanup=fail`, the cad STEP FAILs, the SUMMARY is FAIL, and the exit is 1. The STOPPED contract is
unchanged: return 2, STEP NOT_RUN, SUMMARY INCOMPLETE (the `term in cad` and `docker unresponsive in cad` cases still
pass).

## The anchor (design note)

The bucket-level design alone would fail open if the real key shape differed from the assumed one, for example
keys that carry the bucket name. Both listings would then select zero keys and nothing would be deleted. Leftover
would read 0 and the result would be ok. That is the defect class being fixed.

`jobs_without_upload` closes it. Every job row has an upload: internal/api/handlers.go:1044 writes
`storage.InputKey(jobID, 0, ...)` before `repo.Create` (1124), and nothing in internal/ deletes objects. Attempt 4 is
consistent with this: jobs=153, objects_deleted=302. So a valid job id with no key under `uploads/<id>/` means the
wrapper cannot read the listing. The cleanup fails, and the run is loud (cad FAIL, nothing deleted for that job),
never silent. The `STUB_MC_LS_KEYS=bucket` case and mutation M5 pin it.

## mc JSON assumptions (unmeasured live)

The compose MinIO image is `cgr.dev/chainguard/minio@sha256:fa23f6a6...` (docker-compose.yml:45). Its `mc` reports
`version DEVELOPMENT.GOGET` (quick 261001-5ec, evidence/chainguard-minio-measure.txt:27), so the mc release, and
therefore its JSON, cannot be pinned from the repo. The image has no grep, so all parsing stays on the host. These
are assumptions from upstream mc's `ls` contentMessage, **unmeasured** against this image:
- `mc ls --recursive --json local/octoconv` prints one JSON object per object.
- Each object has `"status":"success"`, `"type":"file"`, a numeric `"size"` and a string `"key"`, plus
  lastModified/etag/url/versionOrdinal/storageClass.
- `key` is relative to the bucket: `uploads/<id>/<ord>-<name>`.
- An error comes as a non-zero exit, as a value with a status other than success (possibly pretty-printed over
  several lines, which is why jq and not sed parses the stream), or as plain text.

If any of these is wrong, the cleanup fails closed, as described above. The live probe and the close criteria are
in `.planning/todos/pending/2026-10-17-cad-cleanup-mc-ls-json-live-confirmation.md`.

## TDD

| Run | Commit | Wrapper suite | Knobs suite | Evidence |
|---|---|---|---|---|
| base | 82d3f46 | 240 passed, 0 failed | 36 passed, 0 failed | evidence/base-final-gate.txt, base-ci-local-knobs.txt |
| RED | 7cc25f2 (test) on the base wrapper | 254 passed, 55 failed | not run | evidence/red-final-gate.txt |
| GREEN | 1520527 | 309 passed, 0 failed | not run | evidence/green-final-gate.txt |
| final | 1520527, code tree clean | 309 passed, 0 failed | 36 passed, 0 failed | evidence/final-final-gate.txt, final-ci-local-knobs.txt, final-head.txt |

In RED, every one of the 240 base PASS names still passed (0 missing by `comm`). Only new checks failed. 14 new
checks already passed on the base wrapper and pin what the fix must keep: the exact four `mc rm` targets, the
foreign keys kept in the stub bucket (`uploads/99999999-.../0-other.dxf` and the near-prefix `results/${U1}0/...`),
no key left under the four run prefixes, the four rm calls in the `ls after rm fails` and `rm leaves objects` cases,
and the refused job's cad PASS, ALL PASS, exit 0 and uploads rm for job 2.

**RED failures (55):**
- `full:` CLEANUP new fields, two bucket listings, no per-prefix disk usage.
- `ls timeout:` CLEANUP fail unknown, no mc rm, one listing, plus the six `cleanup_fail_checks` (CAD_RUN
  cleanup=fail, cad FAIL, frontend not run after fail, SUMMARY FAIL, exit 1, never ALL PASS).
- `ls after rm fails:` CLEANUP fail leftover unknown, two listings, plus the six.
- `ls STUB_MC_LS_TAIL=error:` and `ls STUB_MC_LS_TAIL=text:` CLEANUP fail unknown, no mc rm, plus the six each.
- `rm leaves objects:` CLEANUP leftover 8, plus the six.
- `keys not bucket-relative:` CLEANUP jobs_without_upload 2, no mc rm, plus the six.
- `refused job:` three mc rm, no rm of absent results, CLEANUP ok.
- `quiet rerun: both cleanups found the run objects`.

The base wrapper's per-prefix `mc du --json` stub always answered 2 objects, so on the base the refused job got 4 rm
calls and objects_deleted=8. That is the RED for "results is skipped".

**Stub.** The mc stub is now stateful. `reset_state` writes `$S/bucket` (two foreign keys of size 7) and
`$S/run_objects` (eight keys of size 50: two uploads and two results for each of U1 and U2), all in the assumed mc
shape. The cad-liveproof stub appends `run_objects` to `bucket` as its first action, so the quiet-rerun case's second
cleanup finds the objects again (`STUB_CAD_SKIP_PREFIX` leaves a prefix out). The listing case honours
`STUB_MC_LS_KEYS=bucket`, `STUB_MC_LS_TAIL=error|text`, `STUB_MC_LS_RC` and `STUB_MC_LS_ON`. The rm case honours
`STUB_MC_RM_FAIL` and `STUB_MC_RM_NOOP`, and otherwise removes the target's keys from `bucket`. Only env knobs and
double quotes were used; no new single-quoted PREP string was added.

## Mutations

Each mutation was run from a scratch tree: the GREEN wrapper with one perl substitution, under `scripts/`, plus a copy
of the test file at its repo-relative path. The repo wrapper was never edited; `cmp` at the end showed it identical
to the GREEN copy. The diffs and full FAIL lists are in evidence/mutations.txt.

| Mutation | Result | Caught by |
|---|---|---|
| M1 listing exit status ignored (`\|\| lrc=$?` to `\|\| true`) | 293 passed, 16 failed | the 9 `ls timeout:` checks and 7 `ls after rm fails:` checks |
| M2 BAD lines ignored | 301 passed, 8 failed | the 8 `ls STUB_MC_LS_TAIL=error:` checks |
| M3 jq exit status ignored | 301 passed, 8 failed | the 8 `ls STUB_MC_LS_TAIL=text:` checks |
| M4 leftover test dropped | 302 passed, 7 failed | the 7 `rm leaves objects:` checks |
| M5 uploads anchor dropped | 302 passed, 7 failed | the 7 `keys not bucket-relative:` checks |
| M6 trailing slash dropped from the counted prefix | 280 passed, 29 failed | `full:` exit 0, ALL PASS, cad PASS, CAD_RUN cleanup ok, cad cleanup line, CLEANUP new fields, and every other check whose cleanup now counts the near-prefix foreign key `results/${U1}0/0-near.png` (objects_deleted 9, bytes 407, leftover 1) |
| M7 second listing status ignored | 302 passed, 7 failed | the 7 `ls after rm fails:` checks |

No mutation survived.

## Shellcheck

evidence/shellcheck.txt, shellcheck 0.11.0 at 1520527:
- `-S warning` exits 0 on the wrapper and on the test file.
- Full-severity counts equal the 82d3f46 baseline: the wrapper has SC2001×1, SC2012×2, SC2086×1, SC2329×2, and the
  test file has SC2016×19.

## 261017-7y5 sweep table C

The table C row for line 518 (the per-prefix `mc du --json ... | head -1 || true`) is now fixed by 1520527. The other
rows are unchanged. `bucket_before`/`bucket_after` (now lines 519 and 559) are still informational fields.

## Attempt 4 (76-11)

Attempt 4's CLEANUP line (evidence/76-11-final-gate.txt line 44: jobs=153, objects_deleted=302, bucket_before
4.0GiB_1884_objects, bucket_after 3.1GiB_1582_objects, result=ok) was computed with the per-prefix listing unchecked.
objects_deleted=302 matches the bucket drop of 302 objects. That match cannot show a skipped prefix, because a
skipped prefix would be missing from both numbers. So leftovers at attempt 4 are not re-proven. This is said plainly
in the second Follow-up line of 76-11-SUMMARY.md. No 76-11 evidence transcript was edited.

## Deviations from Plan

**1. Mutations ran from a scratch tree, not by editing the repo wrapper and restoring it.** The scratch tree is the
mutated wrapper under `scripts/` plus a copy of the test file, which finds the wrapper through its own relative
`REPO`. This is the same check with less risk to the working file. The `cmp` against GREEN was still run.

**2. Three checks beyond the `<behavior>` list.** They are `full: mc rm targets exact` (all four rm lines, in order),
`ls timeout: one listing`, and `quiet rerun: both cleanups found the run objects`. Each pins a claim the plan makes
in prose: "mc rm targets byte-identical", "no rm" after a failed first listing, and "the quiet-rerun case's second
cleanup finds them again".

**3. macOS has no `timeout(1)`.** The coordinator asked for long commands to be run with a timeout after a stalled
stream. The first RED run used `timeout 900` and exited 127 before running anything. It was rerun in the background
with the harness's own time limit. The 127 run produced no evidence, and red-final-gate.txt is the later complete run.

## Not done

- The live gate was not rerun, and no docker, kubectl or helm command ran. The new behaviour is proven against stubs
  only, and the mc JSON shape is unmeasured (todo).
- `internal/convert/exec.go` is byte-identical to main: `git diff --quiet main -- internal/convert/exec.go` exits 0.
  `scripts/ci-local.sh` is unchanged.
- Nothing was pushed.

## Threat model

- **T-f0i-01 (Repudiation, cad_cleanup listing):** mitigated. The listing rc, the jq rc, BAD lines, the uploads
  anchor and the post-delete leftover count each fail the cleanup. M1-M5 and M7 prove each one is load-bearing.
- **T-f0i-02 (Tampering, mc rm targets):** mitigated. The rm target is still `local/octoconv/<p>/<id>/` for
  UUID-validated ids only. The listing decides only whether rm runs, never what it targets. `full: mc rm targets
  exact`, the foreign-key checks, and the bad-id case (exactly 2 rm, no bare prefix) pin it. M6 shows the counted
  prefix keeps its trailing slash.
- **T-f0i-03 (DoS, live stack):** mitigated. Stubs only. `grep -nwE 'prune|rmi|image rm|volume rm|down -v|down
  --volumes'` on the wrapper finds nothing.
- **T-f0i-SC (package installs):** accepted. No package was installed.

## Self-Check: PASSED

- The wrapper, the test file, the renamed todo and the ten evidence files named above exist and are non-empty.
- The old todo path `2026-10-17-cad-cleanup-mc-du-absent-prefix.md` no longer exists.
- Commits 7cc25f2 and 1520527 are ancestors of HEAD and both signed G. Neither has a Co-Authored-By trailer.
- The Task 1 automated verify from the plan passed on 1520527.
- `actuals.tokens` is chars/4 over the realized diff of the wrapper, the test file, 76-11-SUMMARY.md and STATE.md, plus the
  full todo and this SUMMARY. Evidence transcripts are excluded.
