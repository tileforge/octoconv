# Local setup

[Back to OctoConv](../README.md) · [Documentation](README.md)

**Requires** Go 1.27.3+, Docker and Docker Compose, with Landlock available in
the Linux Docker host or VM. Run commands from the repository root.

### 1. Start the infrastructure

```bash
cp .env.example .env
# Set API_KEY_SALT to your own random value before creating any API keys.
docker compose up -d --wait postgres redis minio
docker compose run --rm createbucket
```

| Service | Host port | Notes |
|---|---|---|
| postgres | `5434` | `octo / octo-pass / octo_db` |
| redis | `6379` | asynq broker |
| minio | `9100` API, `9101` console | `minioadmin / minioadmin`, bucket auto-created |
| api | `8090` | HTTP API |
| engine workers | — | one service per engine class, each resource-limited, each running as `nobody` |
| webhook-worker-1/2 | — | sole webhook consumer + reconciler sweeper |
| asynqmon | `127.0.0.1:8980` | read-only queue dashboard |

> [!NOTE]
> MinIO runs from `cgr.dev/chainguard/minio`, pinned by digest (Docker Hub no longer serves
> the official MinIO images). The image carries `mc`, which the bucket-creation job uses, and
> the process runs as uid 65532. To bump the digest, follow the comment above the `minio`
> service in `docker-compose.yml`; it updates both compose image lines and the chart's
> `minio.image` and `minio.mcImage`.
>
> A `minio_data` volume written by the old root-run image makes MinIO exit at startup with
> `Unable to initialize backend: Unable to write to the backend`. Either keep the data and
> hand the volume to the new user once (with the stack stopped):
>
> ```bash
> docker run --rm -u 0 -v octoconv_minio_data:/data --entrypoint /bin/sh \
>   cgr.dev/chainguard/minio@sha256:fa23f6a6f62645654530ff94aa077d1cc0d0e44c8f1cce02ab039873612edc72 \
>   -c 'chown -R 65532:65532 /data'
> ```
>
> or drop the dev objects with `docker volume rm octoconv_minio_data`.
>
> The Helm chart needs no manual step: an init container on the MinIO pod hands root-owned files to
> `minio.dataOwnership.uid`/`gid` (65532 by default; the same values set the pod's `runAsUser`, `runAsGroup`
> and `fsGroup`) before the server starts. `minio.dataOwnership.migrate: false` removes that container, and
> `minio.dataOwnership.image` (pinned by digest, needs GNU `chown`) replaces `minio.image` for it.

> [!IMPORTANT]
> The containerized `api` presigns URLs against the internal `minio:9000` endpoint, which
> your host cannot reach. To download results from your machine, run `api` locally (step 3)
> so links point at `localhost:9100`.

### 2. Run migrations

```bash
set -a && . ./.env && set +a
go run ./cmd/migrate
```

### 3. Start the services

```bash
docker compose up -d --build worker webhook-worker-1
set -a && . ./.env && set +a
go run ./cmd/api                        # HTTP API on :8090
```

To build the full stack instead, fetch the TeX bundle first with
`scripts/tectonic-bundle-fetch.sh` (requires authenticated `gh`). Stop the locally
running API before starting its container on the same port.

The Compose API currently has its own hardcoded `API_KEY_SALT`. Before starting
it, change `services.api.environment.API_KEY_SALT` in `docker-compose.yml` to
read the same value used by the local `manage-clients` command:

```yaml
API_KEY_SALT: ${API_KEY_SALT:?Set API_KEY_SALT in .env}
```

Otherwise, keys created from `.env` will be rejected with `401` by the container.
Downloads still have the presigned-URL caveat above:

```bash
docker compose up -d --build
```

### 4. Issue an API key

In a second terminal, load the same environment:

```bash
set -a && . ./.env && set +a
go run ./cmd/manage-clients create "my-service"
# client id: <uuid>
# api key (save now, shown once): <raw-key>
```

Keys are printed exactly once and never stored or logged in plaintext — only a salted
SHA-256 hash lives in Postgres. Rotate without downtime through a second active slot:

```bash
go run ./cmd/manage-clients add-key <client-id>
go run ./cmd/manage-clients revoke <client-id> <primary|secondary>
```

### 5. Convert something

```bash
curl -H "Authorization: ApiKey <raw-key>" \
  -F file=@photo.png -F target=webp http://localhost:8090/v1/jobs
# {"job_id":"...","status":"queued"}

curl -H "Authorization: ApiKey <raw-key>" "http://localhost:8090/v1/jobs/<job_id>"
# {"job_id":"...","status":"done","download_url":"..."}
```

Add `-F callback_url=https://your-service/hooks/octoconv` to skip polling and receive a
signed webhook on completion, or stream progress from `GET /v1/jobs/{id}/events`.
