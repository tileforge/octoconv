# Phase 40: Job Listing - Context

**Gathered:** 2026-07-24
**Status:** Ready for planning

<domain>
## Phase Boundary

A client can list and page through their OWN conversion jobs via `GET /v1/jobs` — keyset-paginated, hard-scoped to the caller's `client_id` at the SQL layer, with optional `status`/`engine` filters, returning a rich per-job summary (incl. the denormalized `progress_pct`). Architecturally independent of Phases 39 and 41 — it only reads the `jobs` row (using the columns Phase 38 added). This is JLIST-01/02.

**In scope:** `Repo.List` (keyset SQL, client-scoped, filters, one query), the `GET /v1/jobs` handler (auth'd, opaque cursor, envelope + item shape, page-size clamp, filter validation), and a cross-client leak test.
**Out of scope:** progress-population (Phase 41 — `progress_pct` is simply selected here, null until then), any write, `download_url`/presigning in the listing, multi-value filters, date-range, sort direction (all v2).
</domain>

<decisions>
## Implementation Decisions

### Query + ownership (JLIST-01/02)
- **D-01:** `Repo.List(ctx, clientID, filter, cursor, limit)` — one keyset SQL query: `SELECT <fields> FROM jobs WHERE client_id=$1 [AND status=$] [AND engine=$] [AND (created_at, id) < ($cursorCreatedAt, $cursorID)] ORDER BY created_at DESC, id DESC LIMIT $limit+1`. Fetch `limit+1` rows to detect whether a next page exists (the extra row, if present, is dropped from the response and its predecessor's position seeds `next_cursor`). Returns `[]JobSummary` + the next-cursor position.
- **D-08 (ownership, JLIST-01):** `client_id=$1` is ALWAYS in the SQL WHERE (never a Go-side filter) — a listing can never return another client's jobs, mirroring the existing 404-not-403 ownership model. A dedicated two-API-key leak test (client A cannot see client B's jobs) is REQUIRED (research Pitfall: cross-client leakage must be tested, not inferred from the single-job 404 test).
- **D-07 (no N+1):** the query selects everything the item needs directly from the `jobs` row (`progress_pct` is denormalized on `jobs` per Phase 38) — NO per-row `Outputs`/`Inputs`/`PresignGet` calls. Exactly one DB round-trip per page.
- **D-09 (ordering):** fixed `ORDER BY created_at DESC, id DESC` (newest first). No `sort`/direction param in v1 — keyset requires a single stable order; `id DESC` is the tie-break for the (rare, ns-resolution) same-`created_at` case.

### Cursor (opaque keyset)
- **D-03:** Pagination cursor is an OPAQUE base64 string encoding the keyset position `{created_at (RFC3339Nano), id (uuid)}`. Client passes it back as `?cursor=<opaque>`; the server decodes+validates it (malformed/undecodable → `400`). First page = no `cursor` param. The response's `next_cursor` is built from the last returned row ONLY when a further page exists (the `limit+1` probe row was present); otherwise `next_cursor` is `null`. Clients never construct the keyset tuple, and the API stays decoupled from the internal ordering columns.

### Response shape
- **D-02 / D-04:** `200` with envelope `{"jobs": [ <item>, … ], "next_cursor": "<opaque>"|null}`; an empty result is `{"jobs": [], "next_cursor": null}`. Each item (rich summary, field names matching `handleGetJob`'s conventions):
  - `job_id`, `status`, `engine`, `source_format`, `target_format`, `progress_pct` (nullable — column exists since Phase 38, stays `null` until Phase 41 populates it), `created_at`, `started_at` (nullable), `finished_at` (nullable), and `error_code` ONLY when `status=failed`.
  - **NO `download_url`** (presigning N objects per page is expensive; the per-job `GET /v1/jobs/{id}` provides it). **NO `opts`** (verbose; per-job GET has it).

### Filters
- **D-05:** Optional `?status=<one>` and `?engine=<one>` query params, single-value, combined with AND. Each is validated fail-closed against the known status/engine sets (`jobs.StatusQueued…`, the engine allow-list) — an unknown/invalid value → `422` (matches the project's fail-closed `422`-before-work idiom). Multi-value (`status=active,queued`) and date-range are deferred to v2.

### Page size
- **D-06:** `?limit=N` clamped to `[1, MAX]`; when absent or unparseable, use `DEFAULT`. A request above `MAX` is clamped to `MAX` (not rejected). Two env vars, `os.Getenv` per the project's config discipline: `JOBS_LIST_DEFAULT_PAGE_SIZE` (default 20) and `JOBS_LIST_MAX_PAGE_SIZE` (default 100). Defaults are measure-don't-assume starting points (RTF/timeout-gate precedent), tunable without a code change.

### Claude's Discretion
- Cursor internal encoding (`base64("<rfc3339nano>|<uuid>")` vs base64-JSON) — executor's call; it MUST round-trip losslessly and reject a malformed/tampered cursor with `400`.
- Whether `jobs_client_created_idx (client_id, created_at DESC)` needs `, id DESC` appended for the keyset tie-break — DEFAULT is reuse the existing index (ns-resolution `created_at` makes same-timestamp ties near-impossible; `id` is a cheap residual filter). If planning's `EXPLAIN` shows a real problem, a small index extension (a new migration) is acceptable — but do NOT add it speculatively.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The read path + conventions to mirror
- `internal/api/handlers.go` — `handleGetJob` (:664): the `map[string]any` response shape + field names (`job_id`, `status`, `error_code`) the list item mirrors, and the 404-not-403 ownership pattern the list scopes at the SQL layer instead.
- `internal/api/routes.go` (:39-41) — where `GET /v1/jobs` registers (auth'd `/v1` group, alongside `POST /jobs` and `GET /jobs/{id}`).
- `internal/api/formats_handlers.go` / `internal/api/presets_handlers.go` — existing list-endpoint style (`writeJSON`, bare vs enveloped) for consistency.
- `internal/jobs/repo.go` — existing query/scan style (pgx), and the `jobs_client_created_idx (client_id, created_at DESC)` index (`0001_init.sql:69`) the keyset reuses.
- `internal/jobs/jobs.go` — the `Job` struct + the `Status*` consts and the engine set to validate filters against; likely a new `JobSummary` type for the listing.
- `internal/api/api.go` — the `Repo` interface (add `List`).
- `internal/db/migrations/0007_job_progress.sql` — the `progress_pct` column (Phase 38) the listing selects.

### Requirements + research
- `.planning/REQUIREMENTS.md` — JLIST-01 (client-scoped listing, filters), JLIST-02 (keyset pagination, server-max page, denormalized progress, no N+1).
- `.planning/research/FEATURES.md` (listing contract) + `.planning/research/PITFALLS.md` (offset-drift, N+1, cross-client leak — all mitigated by the decisions above).
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `jobs_client_created_idx (client_id, created_at DESC)` — the keyset index already exists (no new index expected).
- `handleGetJob`'s ownership + `writeJSON`/`writeError` + field-naming conventions — mirrored/reused.
- `progress_pct` column (Phase 38) — selected directly; no wiring needed here.

### Established Patterns
- Fail-closed `422`-before-work validation (mirrors the upload path) for unknown filter values.
- pgx query/scan in `internal/jobs/repo.go`; the `Repo` interface in `internal/api/api.go` gains one method.
- `os.Getenv`-only config for the page-size knobs.

### Integration Points
- `internal/jobs.Repo.List` (new) ← `internal/api` handler `handleListJobs` (new) ← `GET /v1/jobs` route. Reads only the `jobs` table; no storage/queue/worker involvement.
</code_context>

<specifics>
## Specific Ideas

A clean, boring, correct read path: one client-scoped keyset query per page, an opaque cursor, a useful dashboard-row item (no expensive presigns), fail-closed filter validation, and a bounded page size. The one guarantee that must be tested, not assumed, is cross-client isolation (a two-key leak test). `progress_pct` rides along null-for-now until Phase 41 fills it.
</specifics>

<deferred>
## Deferred Ideas

- Multi-value filters (`status=active,queued`) and date-range filtering — v2.
- Sort direction / `sort` param — v2 (keyset needs a fixed order).
- `download_url` / `opts` in list items — kept on the per-job GET, not the listing.
- Extending `jobs_client_created_idx` with `id DESC` — only if a real `EXPLAIN` problem surfaces during planning; not speculative.
</deferred>

---

*Phase: 40-job-listing*
*Context gathered: 2026-07-24*
