# Phase 72: Engine-Free Contract Foundations - Discussion Log

> **Audit trail only.** Do not use this file as input to planning, research or execution agents.
> CONTEXT.md holds the decisions; this log keeps the alternatives that were considered.

**Date:** 2026-10-04
**Phase:** 72-engine-free-contract-foundations
**Areas discussed:** Detection rules, Error-code granularity, Inspect vs job-creation behaviour, Differential test, Measurements schema, Output role, Aliases
**Mode:** auto. On 2026-10-04 the user delegated every answer to Claude's recommended option. Each line below records the question and that recommended selection.

---

## Detection rules
[auto] Detection: should the sniffer validate encoding? Selected "byte-level ASCII markers, no UTF-8 validation" (recommended).
Alternative: "reuse ValidateSniffPrefix", rejected because it would reject the legacy cp1251/Latin-1 corpus.

[auto] Detection: how is truncation detected? Selected "bounded head and tail windows via ReadAt" (recommended).
Alternative: "full streaming parse", rejected because it costs too much at the API layer.

[auto] Detection: what happens to an STL that is valid both as ASCII and as binary? Selected "reject as ambiguous" (recommended, required by CADIN-02).

## Error-code granularity
| Option | Description | Selected |
|--------|-------------|----------|
| Per failure class (4 codes) | truncated_cad_file, ambiguous_stl, binary_dxf_unsupported, dwg_unsupported; the format goes in message | ✓ |
| Per format and failure | e.g. truncated_step, truncated_iges ... | |
| Single invalid_cad_content | the detail goes only in message | |

**Choice:** per failure class (recommended). It matches the D-02 rule in ERROR-CODES.md and still gives separate stable codes.

## Inspect vs job creation
[auto] Selected "job creation runs the CAD check before the pair lookup; inspect reports the same verdict and never returns 422" (recommended). This keeps Phase 58 D-03, and keeps both endpoints in agreement.

## Differential test
[auto] Selected "a script runs OCP/ezdxf in the 71 measurement image, commits a divergence table and a golden file, and a Go test asserts the golden verdicts" (recommended).
Policy: a false rejection of a real file is not allowed. A sniffer that accepts a file the reader then rejects is allowed if it is listed.

## Measurements schema
[auto] Selected the schema string `octoconv.measurements/v1` (recommended), over an integer version.
[auto] Units: selected `unspecified` (the REQUIREMENTS wording), over the draft's `unknown`.
[auto] Unit source: selected `declared` | `client_assumed` | `none`.
[auto] Reason codes that were never observed: selected "keep them and document them as not observed" (recommended), over dropping them.

## Output role
[auto] Selected "constant, migration 0018 widening the CHECK constraint, and handleGetJob never presigns measurements" (recommended).

## Aliases
[auto] Selected "stp→step and igs→iges in Go and in the frontend, with a sync test" (recommended).

## Claude's Discretion
- Names, the tail-window size, the bbox bound, and how the script reuses common.sh.

## Deferred Ideas
- Byte ceilings and caps go to phases 73 and 76.
- The render time guard goes to phase 76.
- Four keyword-only todos (score 0.2) were not folded in.
