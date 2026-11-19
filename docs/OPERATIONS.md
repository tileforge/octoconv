# Operating OctoConv

[Back to OctoConv](../README.md) · [Documentation](README.md)

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
```

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
`image`, `document`, `html`, `audio`, `av`, `ocr`, `pandoc`, `probe`, `tectonic`, `cad`

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

## Protocol image rollout

There is no down migration. Do not drop a column and do not edit a migration that schema_migrations has recorded. The first protocol worker is the image worker. The legacy image worker stays up. document, html, audio, av, ocr, pandoc, probe, tectonic, and cad stay on asynq. png to pdf stays 422. imagepdf does not get a ScaledObject. The image scaler query stays `sum(octoconv_queue_depth{queue="image", state=~"pending|active|retry"})`.

Forward order:

1. Apply migrations with the existing migrate path.
2. Approve the image manifest.
3. Start the protocol API listener.
4. Start one image protocol worker.

Legacy image jobs stay on asynq. Protocol image jobs stay protocol. A catalog miss enqueues the new image job. A queued protocol job is not in asynq, and FindStale does not return it. A done job keeps its job_outputs row. Do not change jobs.dispatch_owner.

Rollback order:

1. Keep the legacy image worker up.
2. Delete the image rows in worker_capability_formats before DrainWorker.
3. Keep the worker_capabilities row until no queued protocol image job remains. Count a job whose not_before is still in the future. A lease expiry with retry budget left returns that job to queued. Wait until no protocol image job is active. The protocol worker keeps pulling while the worker_capabilities row remains.
4. Call DrainWorker only after the queued protocol image jobs and the active protocol image jobs are both gone. DrainWorker stops new pulls. claimOnce returns an empty pull once drain_at is set.
5. Stop the protocol worker.
6. Leave the schema applied.

The other classes are a later milestone. This milestone does not migrate them.

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

**A second bound, on what the document costs rather than how long it takes.**
`TECTONIC_ENGINE_CPU_SECONDS` caps the engine's CPU consumption through `RLIMIT_CPU`. The
two are different quantities and only one of them is the document's fault: measured on the
same document at a 2-CPU quota, an idle node gave 0.96s wall / 0.95s CPU, and a node
oversubscribed 4× gave 3.67s wall / 1.08s CPU. Wall clock inflated fourfold while
consumption did not move — so a wall deadline alone refuses documents for their neighbours'
sins.

Both bounds are kept because they catch different things. Exhausting the CPU budget is
terminal: the document costs the same next time. The wall deadline still catches a process
that is stuck rather than working, since one that sleeps forever consumes no CPU at all —
and when it fires while the engine got less than half of one CPU, that is starvation and is
retried, because the deadline said the attempt took too long, not that the document did.
The budget is carried by the same helper that establishes the ruleset, so it applies even
where the filesystem confinement is deliberately off.

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

Everything is environment-variable based. [`.env.example`](../.env.example) is the full,
documented list — including how each measured limit was derived.

| Group | Variables |
|---|---|
| Infrastructure | `DATABASE_URL`, `DB_MAX_CONNS`, `REDIS_ADDR`, `S3_*`, `API_ADDR`, `HEALTH_ADDR`, `METRICS_ADDR` |
| Auth & limits | `API_KEY_SALT`, `RATE_LIMIT_IP_RPM`, `RATE_LIMIT_CLIENT_RPM`, `OPERATOR_CLIENT_IDS`, `QUOTA_ENFORCEMENT_DISABLED` |
| Upload guards | `MAX_UPLOAD_BYTES`, `MAX_IMAGE_PIXELS`, `MAX_DOCUMENT_UNCOMPRESSED_BYTES` |
| Per engine | `<CLASS>_WORKER_CONCURRENCY`, `<CLASS>_ENGINE_TIMEOUT`, `<CLASS>_MAX_RETRY`, plus class-specific ceilings (`AUDIO_MAX_DURATION_SECONDS`, `OCR_MAX_PAGES`, `TECTONIC_MAX_OUTPUT_BYTES`, `TECTONIC_ENGINE_CPU_SECONDS`, …). The image class predates the prefix: it is `WORKER_CONCURRENCY` and `ENGINE_TIMEOUT`, but `IMAGE_MAX_RETRY` |
| Inspect | `INSPECT_MAX_UPLOAD_BYTES`, `INSPECT_MAX_WORK_BYTES`, `INSPECT_MAX_WORK_DURATION`, `INSPECT_MAX_CONCURRENT`, `RATE_LIMIT_INSPECT_RPM` |
| Webhooks | `WEBHOOK_SIGNING_SECRET`, `WEBHOOK_PRESIGN_TTL`, `WEBHOOK_ALLOW_PRIVATE_IPS`, `WEBHOOK_ALLOW_INSECURE_HTTP` |
| Lifecycle | `RECONCILER_*`, `JOBS_RETENTION`, `USAGE_RETENTION`, `STORAGE_TTL`, `SSE_*`, `PROGRESS_*` |

> [!NOTE]
> `callback_url` validation blocks loopback, RFC1918, link-local and cloud metadata
> endpoints by default. `WEBHOOK_ALLOW_PRIVATE_IPS` opts into RFC1918 for internal-network
> deployments; loopback and link-local stay blocked with no override.

> [!NOTE]
> Multipart parts larger than the API's in-memory budget are written to `TMPDIR`. The
> chart mounts a disk-backed `emptyDir` there with a `sizeLimit` equal to the create
> intake slots × `MAX_UPLOAD_BYTES` plus the inspect intake and analysis slots ×
> `INSPECT_MAX_UPLOAD_BYTES`, and gives the api container equal `ephemeral-storage`
> request and limit. With local ephemeral-storage isolation enabled on a supported node
> filesystem layout, exceeding the api's storage limits or the `emptyDir` `sizeLimit`
> causes the kubelet to evict the api pod. These are eviction thresholds, not hard filesystem
> quotas; node-wide disk pressure can still evict other pods. See
> [Kubernetes local ephemeral storage](https://kubernetes.io/docs/concepts/storage/ephemeral-storage/).
> `docker-compose.yml` only moves the spill out of the container's writable layer onto the
> named volume `api_spill`, with no size limit.
> A spill file outlives an api container that dies mid-request: the `emptyDir` lives as long
> as the pod, and `api_spill` survives a recreate. So the api removes the regular
> `multipart-<digits>` files directly in `TMPDIR` at startup, before it listens, and logs how
> many it removed; `sizeLimit` only has to cover live intake slots.

[landlock]: https://docs.kernel.org/userspace-api/landlock.html
