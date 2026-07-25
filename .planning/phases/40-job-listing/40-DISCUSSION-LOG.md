# Phase 40 Discussion Log

**Date:** 2026-07-24
**Mode:** discuss (default)

## Framing

`GET /v1/jobs` listing. Carried forward without re-asking (research + code): keyset on `(created_at DESC, id DESC)`, SQL-layer `client_id` scope (never Go-filter), denormalized `progress_pct` (column exists since Phase 38, null until Phase 41), one query / no N+1, the existing `jobs_client_created_idx` index, server-max page size. Scouted `handleGetJob`'s terse `map[string]any` shape as the field-naming precedent.

## Gray areas presented (all 4 selected)

**A. Cursor format** — opaque base64 vs explicit query params.
- **Decision:** opaque base64 `next_cursor` (encodes `{created_at, id}`); `?cursor=<opaque>`; malformed → 400.

**B. Response envelope + item shape** — rich summary vs minimal.
- **Decision:** `{"jobs":[...], "next_cursor": ...}`; rich item (job_id/status/engine/source→target/progress_pct/created/started/finished + error_code on failed); NO download_url (presign-N too costly), NO opts.

**C. Filters** — single-value + fail-closed vs multi-value.
- **Decision:** single-value `status`+`engine` (AND), unknown → 422; multi-value + date-range → v2.

**D. Page size** — default/max.
- **Decision:** default 20, max 100, env-tunable (`JOBS_LIST_DEFAULT_PAGE_SIZE`/`JOBS_LIST_MAX_PAGE_SIZE`); `?limit` clamped to [1,100].

## Decisions captured (see 40-CONTEXT.md D-01..D-09)

- Query/ownership: D-01 (keyset limit+1), D-08 (SQL client_id scope + two-key leak test), D-07 (no N+1), D-09 (fixed created_at DESC, id DESC).
- Cursor: D-03 (opaque base64, 400 on malformed).
- Response: D-02/D-04 (envelope + rich item, no download_url/opts).
- Filters: D-05 (single-value, 422 fail-closed).
- Page size: D-06 (default 20/max 100, env).

## Deferred

- Multi-value filters, date-range, sort direction, download_url/opts in listing → v2/other. Index extension only if EXPLAIN shows a real problem.
