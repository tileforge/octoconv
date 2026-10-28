# Global job search and chronological sorting

Search all retained jobs of the authenticated client by case-insensitive literal substring of the primary source filename or job ID. Accept `q` (trimmed, at most 256 UTF-8 bytes, no NUL) and `sort=newest|oldest` (default newest) on GET /v1/jobs. Combine with existing status/engine filters before pagination. Expose optional source_filename and an additive query echo {q,sort} so newer UIs can detect unsupported query behavior on old backends.

Keep keyset pagination on (created_at,id), reversing both direction and comparison for oldest. Preserve legacy cursors for the default unsearched newest order; extended cursors bind q and sort and reject mismatches. Client scope remains unconditional in SQL; query values stay bound parameters. Inputs are selected deterministically at ordinal 0 via one lateral lookup (legacy duplicate inputs must not duplicate jobs). No schema migration: reuse tenant/time and input lookup indexes. Literal substring matching can scan a client's retained history; do not claim full-text index performance.

Frontend: explicit search submit/clear controls, newest/oldest selector, server-reported historical filenames, reset cursor/selection on query changes, distinguish no matches from an empty history. Never silently perform page-only search or display unfiltered results when an old backend ignores query parameters.

Approaches considered: client-page filtering is insufficient; fetching the entire history into the browser is unbounded. Use server-side filtering before existing keyset pagination.
