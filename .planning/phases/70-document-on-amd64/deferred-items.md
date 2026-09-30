# Phase 70 deferred items

## minio/minio:latest and minio/mc:latest are no longer pullable (found in 70-01, 2026-09-30)

`docker compose up` (and therefore `scripts/ci-local.sh e2e`) failed on this host with
`pull access denied for minio/mc` and the same for `minio/minio`: the Docker Hub repositories
no longer serve those images and the local copies had been reclaimed. docker-compose.yml lines
~31 (`minio`) and ~53 (`createbucket`) still reference them.

Worked around on this host only, outside the repo: `docker tag cgr.dev/chainguard/minio:latest
minio/minio:latest` and `... minio/mc:latest` (the chainguard image ships minio, mc and sh).
No tracked file was changed. A durable fix (pin a reachable minio/mc source in compose and the
chart) is out of scope for phase 70 and is not attempted here.

**Tracked (70-09, user decision 5, 2026-09-30):** `.planning/todos/pending/2026-09-30-minio-images-off-docker-hub.md`
-- a quick task after the milestone moving compose and the chart to `cgr.dev/chainguard/minio`. It is a
todo and not only this entry because this file lives in the phase directory, archived at milestone close,
and the close surfaces quick tasks, seeds and todos, not a phase's deferred-items file. Nothing in
docker-compose.yml or the chart changed in phase 70.

## html large-document negative control no longer fails on current Chromium (found in 70-05, 2026-09-30)

`scripts/sandbox-liveproof.sh html` (and so the eight-class gate) ends `FAILURES PRESENT` on
exactly one check, `html large-document-without-dev-shm`:
`TestChromiumLargeDocumentNeedsNoDevShmLive` fails at its negative control
(`chromium_live_test.go:405`: "engine exited 0 without --disable-dev-shm-usage -- the method cannot
see a /dev/shm dependency"). The production render itself passes; only the control, which is
supposed to prove the method could see a /dev/shm dependency, no longer sees one.

Not caused by phase 70: the same failure reproduces on a `git worktree` of `705f962` (the phase 69
tip, before any phase 70 change) built today. `Dockerfile.chromium-worker` installs
`chromium-headless-shell` from Debian unpinned; the phase 69 measurements ran Chromium 153.0.8010.52
(69-RESEARCH.md), images built today carry 154.0.8037.57. Likely Chromium 154 falls back to a granted
path when /dev/shm is denied, so the flag-less run succeeds; not measured further, because the fix
is html's and out of scope here. The claim html needs no /dev/shm still holds (the confined
production run passes without it); what is broken is the control's ability to falsify it.

Left as is: no change to chromium_live_test.go, the html ruleset or the script. A durable fix
belongs to an html follow-up (either a control that cannot be defeated by a fallback, or pinning the
Chromium version the gate is measured against). The other 39 checks of the 40 pass.

**Resolved (70-06, 70-09).** Measured and the control rebuilt in 70-06
(`evidence/chromium-devshm-measure-70-06.txt`, `evidence/liveproof-html-70-06.txt`), no Chromium pin:
Chromium 154 creates nothing under /dev/shm in any condition (it falls back to memfd_create), so the
control now observes /dev/shm with an inotify watch outside the sandbox and a dash child that does
create an entry proves the observation can see one (`dev-shm-observe: control=1 production=0` on
Chromium 154.0.8037.57). The eight-class gate is green on images built from the
fixing commit: `evidence/liveproof-8class-70-09.txt` ends `ALL PASS (40 checks)`. WINDOWS.md entry 2
is fixed, open_count 0.
