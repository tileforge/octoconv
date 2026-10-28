# Job history search

GET /v1/jobs accepts q and sort=newest|oldest in addition to status, engine, cursor and limit. Omission preserves newest-first ordering and legacy cursor compatibility. q is trimmed, valid UTF-8, at most 256 bytes and cannot contain NUL. Matching is a case-insensitive literal substring of the primary source filename or job UUID; %, _ and backslash are literal characters. Only the authenticated client's retained jobs are searched. Retention-deleted names are not reconstructed.

Filtering happens before keyset pagination. Pass the same q and sort on subsequent pages; a mismatched cursor returns 400 invalid_cursor. Query validation returns 422 invalid_request. The response adds source_filename when available and echoes query:{q,sort} only when either query parameter is explicit. Clients can use the echo to detect older servers that silently ignore unsupported query parameters.

No migration or extension is required. Queries reuse the tenant/time and primary-input lookup indexes, selecting at most one ordinal-zero input deterministically even for legacy duplicates. Literal substring search may scan the client's retained history and is not a full-text search index. Confirm query plans and tenant history sizes before enabling high-volume search workloads; database cancellation follows the request context.

Examples (authenticate as for other API requests):

- /v1/jobs?q=report&sort=newest&limit=20
- /v1/jobs?q=report&sort=oldest&cursor=PREVIOUS_NEXT_CURSOR

Existing responses, filters and opaque cursors remain accepted. No client ID query parameter can alter ownership scope.
