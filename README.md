# 🐙 OctoConv

**An async, hardened file-conversion service in Go.** Upload a file over HTTP, get it back
converted — with mandatory auth, per-client quotas, signed webhooks, and engines that run
confined.

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](go.mod)

A client uploads a file through the REST API; OctoConv stores it in S3-compatible object
storage, queues a job, and a worker runs an external conversion engine and writes the result
back. PostgreSQL is the system of record for job state; Redis ([asynq][asynq]) is the queue
broker. Each engine class has its own queue and its own worker deployment, so pools scale
independently.

> [!NOTE]
> OctoConv is built for service-to-service use inside one organization. Authentication,
> quotas and isolation are real; public multi-tenancy and billing are deliberately not.

**Contents** — [Why](#why-octoconv) · [Engines](#engines-and-operations) ·
[Architecture](#architecture) · [Quick start](#quick-start) · [API](#api) ·
[Quotas](#quotas-and-fairness) · [Sandboxing](#sandboxing-the-tex-engine) ·
[Configuration](#configuration) · [Documentation](#documentation) · [MCP](#mcp-servers) ·
[Roadmap](#roadmap)

## Why OctoConv

Most "convert my file" services are a thin wrapper around a CLI tool with no safety net, or
a SaaS you don't control. OctoConv sits in between: a small, auditable Go service you
self-host, designed around the failure modes that actually bite —

- **A worker crashes mid-conversion.** A reconciler sweeps Postgres for stranded jobs and
  recovers them idempotently, under a bounded retry cap.
- **A webhook delivery silently drops** — a Redis blip, a restart between `MarkDone` and
  enqueue. The reconciler catches that gap too, and delivers exactly once.
- **A client lies about file type.** Every upload is sniffed by magic bytes and container
  structure, never trusted by extension, before it reaches storage or an engine.
- **A "PNG" is a 65535×65535 decompression bomb.** Declared dimensions — or, for office
  documents, declared uncompressed ZIP size — are checked before any decoding happens.
- **An engine hangs or forks.** Every invocation goes through a wrapper that puts the child
  in its own process group and SIGKILLs the group on timeout — verified against a real
  forking tree (LibreOffice's `oosplash` → `soffice.bin`), not assumed.
- **A document is a program.** A `.tex` has macros, control flow and file input. The TeX
  engine runs under a Landlock ruleset that grants the job's own directory and nothing else
  — see [Sandboxing the TeX engine](#sandboxing-the-tex-engine).
- **One client eats the cluster.** Per-client, per-engine caps on concurrency, daily bytes
  and daily job count, rolled out through an observe-then-enforce mode.

## Engines and operations

Every job carries an **operation** as well as a format pair. `convert` is the default;
`sanitize` rebuilds a file in its own format with active content stripped.

| Class | Converts | Engine |
|---|---|---|
| `image` | `png` `jpg` `webp` `heic` `tiff`, any-to-any | [libvips][libvips] |
| `document` | `docx` `xlsx` `pptx` `odt` `ods` `odp` → `pdf`; `docx`↔`odt`, `xlsx`↔`ods`, `pptx`↔`odp` | LibreOffice headless |
| `html` | `html` → `pdf` | headless Chromium |
| `audio` | `mp3` `wav` `m4a` `ogg` and video containers → `txt` `srt` `vtt` `json` | whisper.cpp |
| `av` | `mov`/`avi`/`mkv`/`webm` → `mp4`; `mp4` → `webm`; audio extract; thumbnail | ffmpeg |
| `ocr` | `jpg` `png` `tiff` `webp` `pdf` → `txt` `hocr` `alto` | Tesseract |
| `pandoc` | `md` `rst` `org` `latex` `html` `docx` `epub`, across a curated allowlist | pandoc |
| `probe` | `sanitize`: `pdf` `docx` `xlsx` `pptx` `jpg` `png` rebuilt in the same format, and `docm` `xlsm` `pptm` rebuilt into their macro-free extension; also serves `POST /v1/inspect` | in-process |
| `tectonic` | `latex` → `pdf`, single file or multi-file project | [tectonic][tectonic] |

`GET /v1/formats` returns this table as the service itself sees it — derived from the
converter registry, never hand-maintained.

## Architecture

```
                 multipart upload                 asynq (Redis)
Client ────────────────────────▶ API ───────────────────────────▶ Worker
                                  │                                  │
                                  ▼                                  ▼
                            PostgreSQL                        external engine,
                        (jobs, job_events,                  via a hardened process
                         webhook_deliveries)                  wrapper (+ Landlock
                                  │                            for tex)
                                  │                                  │
                                  │                                  ▼
                                  │                          S3 / MinIO storage
                                  ▼
                          presigned download URL
                          or signed webhook push
```

A job row is written to Postgres **before** the task is enqueued, so a crash in between
leaves an inspectable row rather than an orphaned message. Every status change goes through
one guarded transition that locks the row, checks an allow-list of source statuses, and
appends to an append-only event log in the same transaction.

<details>
<summary>Repository layout</summary>

```
cmd/
  api/                — HTTP server: upload, inspect, status, presigned downloads
  worker/             — image conversion (libvips)
  document-worker/    — office documents (LibreOffice)
  chromium-worker/    — HTML → PDF (headless Chromium)
  audio-worker/       — audio transcription (whisper.cpp)
  av-worker/          — video transcode/extract/thumbnail (ffmpeg)
  ocr-worker/         — text extraction (Tesseract)
  pandoc-worker/      — markup-family conversion (pandoc)
  probe-worker/       — sanitize operation
  tectonic-worker/    — LaTeX → PDF, Landlock-confined
  webhook-worker/     — signed webhook delivery + reconciler sweep
  mcp-server/         — stdio MCP server
  mcp-http/           — streamable-HTTP MCP server for in-cluster use
  manage-clients/     — operator CLI: API keys, quotas, in-flight jobs
  manage-presets/     — operator CLI: named conversion presets
  migrate/            — apply embedded SQL migrations
internal/
  api/                — routes, handlers, auth, rate limiting, SSRF guard, quota gate
  convert/            — Converter interface + registry, hardened exec, engines
  sandbox/            — Landlock ruleset construction and the exec-mode helper
  jobs/               — job repository with guarded state transitions
  queue/              — asynq task/queue definitions, retry schedules
  worker/             — shared JobRunner: download → convert → upload → commit
  reconciler/         — stranded-job and webhook-gap recovery
  webhook/            — HMAC signing, delivery, dead-lettering
  inspect/            — synchronous inspection and the findings vocabulary
  mcpserver/          — MCP tool implementations, shared by both MCP binaries
  cancel/ presets/ ratelimit/ errcode/ metrics/ storage/ auth/ clients/ db/
  envcfg/ e2e/        — env parsing shared by every cmd; the live end-to-end suite
deploy/chart/         — Helm chart with KEDA queue-depth autoscaling
docs/                 — OpenAPI spec and reference docs
frontend/             — reference UI (React + Vite)
```

</details>

## Quick start

**Requires** Go 1.26+, Docker and Docker Compose.

### 1. Start the infrastructure

```bash
cp .env.example .env        # adjust ports/credentials if needed
docker compose up -d
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
docker compose up -d --build worker     # containerized image worker
set -a && . ./.env && set +a
go run ./cmd/api                        # HTTP API on :8090
```

Or fully containerized, subject to the presigned-URL caveat above:

```bash
docker compose up -d --build
```

### 4. Issue an API key

```bash
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

curl -H "Authorization: ApiKey <raw-key>" http://localhost:8090/v1/jobs/<job_id>
# {"job_id":"...","status":"done","download_url":"..."}
```

Add `-F callback_url=https://your-service/hooks/octoconv` to skip polling and receive a
signed webhook on completion, or stream progress from `GET /v1/jobs/{id}/events`.

## API

Every `/v1/*` route requires `Authorization: ApiKey <raw-key>`. `GET /healthz` is public and
really pings Postgres, Redis and S3.

| Route | Purpose |
|---|---|
| `POST /v1/jobs` | Create a conversion job from a multipart upload |
| `GET /v1/jobs` | List the caller's jobs, paginated |
| `GET /v1/jobs/{id}` | Job status, and a presigned download URL once `done` |
| `GET /v1/jobs/{id}/events` | Server-sent progress stream for one job |
| `POST /v1/jobs/{id}/cancel` | Cancel a `queued` or `active` job |
| `POST /v1/inspect` | Inspect a file synchronously — no job, no storage write |
| `GET /v1/formats` | Supported operations and format pairs, from the live registry |
| `/v1/presets` | Named conversion presets: create, list, show, update, deactivate |
| `/v1/system/presets`, `/v1/system/usage` | Operator-only; non-operators get `404`, never `403` |

**Status semantics**

- No key, or an invalid/revoked one → `401`
- A job belonging to another client, or one that doesn't exist → `404`; the API never
  confirms the existence of jobs you don't own
- Over the rate limit → `429` with `Retry-After` and `code: "rate_limited"`
- Over quota → `429` with `Retry-After` and `code: "quota_exceeded"` — a consumption limit,
  never an authorization boundary
- Unsupported pair, mismatched content, oversized declared dimensions, embedded macros or a
  zip-bomb shape → `422`, rejected before anything touches storage

The full contract is [`docs/openapi.yaml`](docs/openapi.yaml), held equal to the routes the
service actually registers by a test that walks the live router. Every machine-readable
error code is frozen in [`docs/ERROR-CODES.md`](docs/ERROR-CODES.md).

<details>
<summary>A LaTeX job can be a project, not just a file</summary>

Repeat the `file` part. The first one is the document that gets typeset; the rest are
materialised beside it under their own names, so `\input{preamble}`, `\bibliography{refs}`
and `\includegraphics{fig.png}` resolve:

```bash
curl -H "Authorization: ApiKey <raw-key>" -F target=pdf -F source=latex \
  -F file=@paper.tex -F file=@preamble.tex -F file=@refs.bib -F file=@fig.png \
  http://localhost:8090/v1/jobs
```

Up to 32 parts, and the size limit applies to their **sum**, not to each — a thousand
one-byte attachments would otherwise be a thousand uploads charged as one small job. An
attachment named after a file the worker owns (`in.tex`, `out.pdf`) is refused rather than
silently renamed, and every name is flattened to its base, so `../../etc/passwd` cannot name
anything outside the job.

</details>

## Quotas and fairness

Per-client, per-engine limits sit in front of `POST /v1/jobs`: **concurrent** (queued+active)
jobs, **daily bytes** uploaded, and **daily job count**. Absent means unlimited; an explicit
`max-concurrent 0` denies that engine to that client entirely.

A pre-upload check rejects a request already over its limit, and an authoritative check
under a per-(client, engine) lock decides admission once the body has arrived. The guarantee
is **no over-admission**, not "nothing is uploaded" — a burst from one client can waste
uploads that the authoritative check then rejects.

```bash
go run ./cmd/manage-clients set-limit <client-id> <engine> <max-concurrent|-> <max-daily-bytes|-> <max-daily-jobs|->
go run ./cmd/manage-clients clear-limit <client-id> <engine>
go run ./cmd/manage-clients show-limits <client-id>
go run ./cmd/manage-clients set-mode <client-id> <observe|enforce>
go run ./cmd/manage-clients list-inflight [<client-id>] [<older-than>]
go run ./cmd/manage-clients cancel-job <job-id>
```

`-` is the token that writes NULL/unlimited — never a large sentinel number. `set-limit`
rejects an `<engine>` that isn't a known class and prints the valid set, so a typo fails
loudly instead of writing a limit no job can ever match:

<!-- engine-classes: kept equal to convert.AllEngines() by
     internal/convert/readme_sync_test.go. Add a class in Go and this list must follow;
     the count is deliberately not written out, so a new engine cannot make this
     paragraph false from a commit that never touches the README. -->
`image`, `document`, `html`, `audio`, `av`, `ocr`, `pandoc`, `probe`, `tectonic`

`clear-limit` and `show-limits` deliberately do not validate the engine — a row written
before that check existed must still be listable and removable through the CLI.

> [!TIP]
> Every client starts in `observe`: limits are evaluated and each would-be rejection is
> counted as `octoconv_quota_would_reject_total{client_id,engine,reason}`, but nothing is
> rejected. Watch that counter for a full UTC day before `set-mode <client-id> enforce`.

> [!WARNING]
> `QUOTA_ENFORCEMENT_DISABLED=true` is the service-wide kill switch — verdicts are still
> computed and counted, but nothing is rejected. An **empty or absent value does not disable
> enforcement**; any unrecognized value is warned about at startup and leaves enforcement ON.

<details>
<summary>Why a stuck job can exhaust a client's budget — and what to do</summary>

The daily cap counts **in-flight** (`queued`+`active`) jobs alongside the day's completed
work, because an admitted job is already consuming the allowance it was admitted against. A
job leaves that count only on a terminal transition, and a job that never reaches one holds
part of its client's budget with no time bound — again on every subsequent UTC day.

That is the price of the daily cap being real, not a defect. But stuck jobs normally clear
through the reconciler's sweeper, and that sweeper runs inside **`cmd/webhook-worker`** under
a fleet-wide advisory lock. **With every webhook-worker down, stranded jobs stop being
recovered** — so "clients are running out of quota" can be a symptom of a stopped
webhook-worker rather than of client traffic.

```bash
go run ./cmd/manage-clients list-inflight                # everything holding budget
go run ./cmd/manage-clients list-inflight <client-id> 2h # one client, stuck 2h+
go run ./cmd/manage-clients cancel-job <job-id>
```

Cancel through that command, never with a hand-written status update. `cancel-job` goes
through the same guarded transition the client-facing cancel uses, which books the job's
contribution to the daily rollup in the same transaction that frees its slot. A direct
UPDATE frees the slot and never books the contribution — it repairs the quota and breaks its
accounting at once, which is worse than the stuck job, because nothing looks wrong
afterwards.

Accounting notes: a canceled job is still charged the input bytes and job count it consumed,
because a free cancel would make `create → cancel → create` an unbounded way around a daily
cap. A job in flight across UTC midnight keeps counting on the new day. Its ledger row is
written for the UTC day its terminal transaction *started*, since PostgreSQL's `now()` is
transaction-start time — a known, accepted narrow window, not a refund path.

</details>

## Sandboxing the TeX engine

A `.tex` is a program. `--untrusted` stops it from running shell commands and it does *not*
stop it reading files — measured, not assumed, on a document that typeset the container's
`/etc/hostname` into the PDF a client got back.

So the engine runs under a [Landlock][landlock] ruleset that grants the job's own directory
(read-write), the TeX bundle, the format cache and the system directories holding the binary
(read-only), and nothing else — no `/etc`, no `/proc`, no mounted secret. It needs no
capability and no privileged container: an unprivileged process restricts *itself*, and the
restriction is inherited across `execve` and cannot be lifted.

The engine gets the format **files** it needs, not the directory holding them: Landlock
grants are recursive, so a cache pointed at `/etc/ssh` or a home directory would otherwise
grant the credentials sitting beside the format. `TECTONIC_CACHE_DIR` is validated at
startup for the same reason — it may not be a system directory, may not contain the
directory jobs are materialised in, and must hold regular `*.fmt` files, since a symlink
named `*.fmt` would grant whatever it points at. `.env.example` lists every refusal.

> [!CAUTION]
> On a kernel without Landlock the worker **exits at startup** rather than typesetting client
> documents unconfined. Startup does not merely ask the kernel whether Landlock exists — a
> policy can answer yes and then deny the ruleset — it **compiles a document** through the
> real sandbox with the configured bundle and cache, so a host where the engine cannot run
> confined fails once, at boot, instead of on every job. If that is deliberate, set
> `TECTONIC_SANDBOX_DISABLED=true`: the only value that disables it, and one that logs what
> it costs on every boot.

<details>
<summary>Building the tex worker needs one extra step</summary>

`Dockerfile.tectonic-worker` copies a 122 MiB TeX bundle out of the build context, and that
file is a release asset rather than a committed one — so a fresh clone does not have it and
the build fails with `"/deploy/tectonic-bundle/octoconv-bundle.ttb": not found`. Fetch it
once, with `gh` authenticated against this repository:

```bash
scripts/tectonic-bundle-fetch.sh          # download and verify the digest
scripts/tectonic-bundle-fetch.sh --check  # verify only
```

The digest is checked twice on purpose: by that script before the build starts, and again
inside the image against the bytes that actually land there.

</details>

## Configuration

Everything is environment-variable based. [`.env.example`](.env.example) is the full,
documented list — including how each measured limit was derived.

| Group | Variables |
|---|---|
| Infrastructure | `DATABASE_URL`, `DB_MAX_CONNS`, `REDIS_ADDR`, `S3_*`, `API_ADDR`, `HEALTH_ADDR`, `METRICS_ADDR` |
| Auth & limits | `API_KEY_SALT`, `RATE_LIMIT_IP_RPM`, `RATE_LIMIT_CLIENT_RPM`, `OPERATOR_CLIENT_IDS`, `QUOTA_ENFORCEMENT_DISABLED` |
| Upload guards | `MAX_UPLOAD_BYTES`, `MAX_IMAGE_PIXELS`, `MAX_DOCUMENT_UNCOMPRESSED_BYTES` |
| Per engine | `<CLASS>_WORKER_CONCURRENCY`, `<CLASS>_ENGINE_TIMEOUT`, `<CLASS>_MAX_RETRY`, plus class-specific ceilings (`AUDIO_MAX_DURATION_SECONDS`, `OCR_MAX_PAGES`, `TECTONIC_MAX_OUTPUT_BYTES`, …). The image class predates the prefix: it is `WORKER_CONCURRENCY` and `ENGINE_TIMEOUT`, but `IMAGE_MAX_RETRY` |
| Inspect | `INSPECT_MAX_UPLOAD_BYTES`, `INSPECT_MAX_WORK_BYTES`, `INSPECT_MAX_WORK_DURATION`, `INSPECT_MAX_CONCURRENT`, `RATE_LIMIT_INSPECT_RPM` |
| Webhooks | `WEBHOOK_SIGNING_SECRET`, `WEBHOOK_PRESIGN_TTL`, `WEBHOOK_ALLOW_PRIVATE_IPS`, `WEBHOOK_ALLOW_INSECURE_HTTP` |
| Lifecycle | `RECONCILER_*`, `JOBS_RETENTION`, `USAGE_RETENTION`, `STORAGE_TTL`, `SSE_*`, `PROGRESS_*` |

> [!NOTE]
> `callback_url` validation blocks loopback, RFC1918, link-local and cloud metadata
> endpoints by default. `WEBHOOK_ALLOW_PRIVATE_IPS` opts into RFC1918 for internal-network
> deployments; loopback and link-local stay blocked with no override.

## Documentation

| Document | What's in it |
|---|---|
| [`docs/openapi.yaml`](docs/openapi.yaml) | OpenAPI 3.1 spec, held equal to `internal/api/`'s registered routes by a sync test |
| [`docs/ERROR-CODES.md`](docs/ERROR-CODES.md) | Frozen list of every machine-readable error code, sync and async |
| [`docs/SANITIZE.md`](docs/SANITIZE.md) | What the `sanitize` operation removes, and what it deliberately does not |
| [`docs/TYPESETTING.md`](docs/TYPESETTING.md) | The TeX engine: bundle, cache, confinement, measured limits |
| [`docs/FINDINGS.md`](docs/FINDINGS.md) | The inspection findings vocabulary |

## MCP servers

Both expose the same tools — `convert_file`, `get_job_status`, `download_result`,
`list_supported_formats`, `list_presets` — to any [Model Context Protocol][mcp] client. Each
is a zero-privilege HTTP client of the public API: it holds a client API key, never a
database, S3 or Redis credential.

**`cmd/mcp-server`** speaks stdio, for a local agent:

```bash
go build -o octoconv-mcp ./cmd/mcp-server
```

```json
{
  "mcpServers": {
    "octoconv": {
      "command": "/path/to/octoconv-mcp",
      "env": {
        "OCTOCONV_BASE_URL": "http://localhost:8090",
        "OCTOCONV_API_KEY": "<raw-key-from-manage-clients>"
      }
    }
  }
}
```

Also reads `OCTOCONV_OUTPUT_DIR`, `OCTOCONV_CONVERT_TIMEOUT`, `OCTOCONV_POLL_INTERVAL`, and
`OCTOCONV_S3_DIAL_ADDR` for the operator-only case of redialing presigned downloads.

**`cmd/mcp-http`** speaks streamable HTTP, for in-cluster deployment
(`deploy/chart/octoconv/templates/deployment-mcp-http.yaml`, gated by `mcpHttp.enabled`):

- **Auth is per-request, not per-process.** The pod holds no API key. Every request carries
  `Authorization: ApiKey <key>`, rejected with `401` before any JSON-RPC code runs, and a
  session-key binding rejects (`403`) a request whose `Mcp-Session-Id` doesn't match the key
  that created it.
- **Results are presigned-only** — HTTP mode never writes to the pod's filesystem.
- **Config is `OCTOCONV_BASE_URL` + `MCP_HTTP_ADDR` only** (default `:8070`).
- **ClusterIP only**, behind a NetworkPolicy that default-denies ingress except `:8070` from
  within the release namespace. Reach it with
  `kubectl -n octoconv port-forward svc/mcp-http 8070:8070`.

## Frontend

A reference UI in [`frontend/`](frontend/) (React + Vite + TypeScript): enter an API key,
pick a file and target format, submit, poll until the download link is ready.

```bash
cd frontend && npm install && npm run dev   # :5173, proxies /v1 and /healthz to :8090
```

## Roadmap

OctoConv ships in small, audited milestones. Each one closes with a live end-to-end
verification pass, not just green unit tests. **v1.14 (Typesetting & Debt)** shipped
2026-09-05.

<details>
<summary>Shipped milestones</summary>

| Milestone | What landed |
|---|---|
| v1.0 Hardening MVP | Mandatory auth + rate limiting, signed webhooks with retry/dead-letter, retry classification, stranded-job reconciler, magic-byte validation, storage TTL, Prometheus + health |
| v1.1 Tech Debt Cleanup | Opt-in RFC1918 webhooks, reconciler coverage for dropped deliveries, wall-clock soak test, image decompression-bomb guard |
| v1.2 Document Engine | Office → PDF via LibreOffice, structural zip-bomb/macro validation, dedicated `document-worker` |
| v1.3 Document Class v2 | Cross-format document pairs, OLE-CFB rejection, HTML/Chromium class, dedicated `webhook-worker` |
| v1.4 CI, Presets & Debt | First CI workflow, named presets, registry-derived `GET /v1/formats` |
| v1.5 MCP & Fidelity | stdio MCP server, veraPDF ISO-19005 validation, encrypted-vs-legacy OLE-CFB classification |
| v1.6 Kubernetes & KEDA | Helm chart, KEDA queue-depth autoscaling, MCP over HTTP, operator-gated system presets |
| v1.7 Audio Engine | Transcription via whisper.cpp |
| v1.8 AV Engine | Transcode, audio extraction, thumbnails via ffmpeg |
| v1.9–v1.12 | OCR and pandoc engine classes, SSE progress streaming, per-client quotas and fairness, job listing, retention |
| v1.13 Inspect & Operations | Synchronous `POST /v1/inspect`, structured error codes, JobRunner consolidation, born-digital PDF routing, `sanitize` operation, `/v1/system/usage` |
| v1.14 Typesetting & Debt | LaTeX → PDF via tectonic, multi-file projects, Landlock confinement of the engine's file reads |

</details>

**Planned**

- Job pipelines crossing an engine boundary — `md → pdf` as pandoc → tectonic in one job
- Kubernetes validation in CI (kind/k3d)
- An `is_operator` column instead of the env allowlist
- Trim/crop as validated closed-opts for video
- Dependency-advisory tracking for pinned engine binaries

[asynq]: https://github.com/hibiken/asynq
[libvips]: https://www.libvips.org/
[tectonic]: https://tectonic-typesetting.github.io/
[landlock]: https://docs.kernel.org/userspace-api/landlock.html
[mcp]: https://modelcontextprotocol.io
