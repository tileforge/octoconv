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
