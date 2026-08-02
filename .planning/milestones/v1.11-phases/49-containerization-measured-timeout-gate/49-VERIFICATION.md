---
phase: 49-containerization-measured-timeout-gate
verified: 2026-08-01T04:25:02Z
status: passed
score: 4/4 must-haves verified
overrides_applied: 0
---

# Phase 49: Containerization + Measured Timeout Gate Verification Report

**Phase Goal:** A running `pandoc-worker` container in docker-compose passes a full live E2E, with `PANDOC_ENGINE_TIMEOUT` sized from a MEASURED gate (not copied), in a lean TeX-free image. Requirement: PDCE-04.
**Verified:** 2026-08-01T04:25:02Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (mapped to Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| SC1 | Lean, TeX-free, SHA-pinned `Dockerfile.pandoc-worker` builds and converts an uploaded markup doc end-to-end with signed webhook confirmed | ✓ VERIFIED | See SC1 detail below |
| SC2 | `scripts/pandoc-timing-measure.sh` measures real wall-sec/conversion cost across a representative corpus at production-equivalent limits, driving a documented go/no-go | ✓ VERIFIED | See SC2 detail below |
| SC3 | `PANDOC_ENGINE_TIMEOUT` is DERIVED (not copied), stays under 900s CAP, recorded as Key Decision with conditions | ✓ VERIFIED | See SC3 detail below |
| SC4 | IN-02 env parity across all queue-client services; pandoc-worker in CI bake matrix | ✓ VERIFIED | See SC4 detail below |

**Score:** 4/4 truths verified

---

### SC1 — Lean TeX-free image + live E2E

**Dockerfile.pandoc-worker** (`/Users/apaderin/dev/octoconv/Dockerfile.pandoc-worker`):
- Two-stage build: `golang:1.26-bookworm` (unchanged Phase-48 `cmd/pandoc-worker` build) → `debian:bookworm-slim` runtime.
- Pandoc 3.10.1 installed from the **official static-release tarball**, selected by `ARG TARGETARCH` (amd64/arm64), verified **fail-closed** via `sha256sum -c -` before extraction (lines 43–56). Hashes pinned inline (amd64 `72948bf5...`, arm64 `cd3963da...`).
- Only `bin/pandoc` extracted (no `pandoc-server`/`pandoc-lua`/man pages).
- Runtime apt packages: `ca-certificates` + `curl` only — **no texlive/tectonic/wkhtmltopdf/weasyprint**.
- `USER nobody` set before `ENTRYPOINT`.
- Version assertion at build time: `pandoc --version | grep -q "pandoc 3\."`.

**Live image inspection** (built image `octoconv-pandoc-worker:dev`, present locally, ID `e86a83118fea`):
```
$ docker run --rm --entrypoint pandoc octoconv-pandoc-worker:dev --version
pandoc 3.10.1
Features: +server +lua

$ docker run --rm --entrypoint sh octoconv-pandoc-worker:dev -c '
  for b in pdflatex xelatex tectonic wkhtmltopdf weasyprint texlive latex; do
    command -v "$b" >/dev/null 2>&1 && echo "FOUND: $b"
  done; whoami'
(no FOUND lines) -> nobody

$ docker image inspect octoconv-pandoc-worker:dev --format '{{.Config.User}}'
nobody
```
Image size 317MB (lean, no multi-GB TeX toolchain) — matches SUMMARY claim.

**Live E2E (operator-attested, not re-run per task instructions):** `internal/e2e/e2e_test.go` `TestPandocConversionE2E` (lines 1416–1458) drives:
- `MD_ToHTML` subtest: upload → poll → presigned download, asserts real `<h1` structural HTML + the fixture heading text (`pandocHeadingText = "OctoConv Pandoc Sample"`, matches `internal/e2e/testdata/sample.md`), and `assertSignedWebhook` (HMAC-verified).
- `MD_ToDOCX` subtest: upload → poll → presigned download, asserts container format via `convert.SniffContainer` == `"docx"` (real zip/OOXML sniff, not a byte-count stub — verified by reading `assertDownloadIsFormat`, lines 540–562).
- Operator-attested result (49-04-SUMMARY.md): `PASS TestPandocConversionE2E (4.13s)` — `MD_ToHTML 2.05s`, `MD_ToDOCX 2.03s`; DB `job_events` show md→docx/md→html/html→md all `done`.
- Offline self-skip confirmed live in this session: `go test ./internal/e2e/... -run TestPandocConversionE2E -count=1` → `SKIP: TestPandocConversionE2E ... E2E_BASE_URL not set` (correct offline behavior, does not fabricate a pass).

**Verdict: SC1 VERIFIED** (static artifact + live image inspection performed in this session; live compose E2E accepted as operator-attested per task scope, backed by concrete PASS evidence and wired, non-stub assertion helpers).

---

### SC2 — Measured timing gate

**`scripts/pandoc-timing-measure.sh`** (executable, `chmod 755` confirmed, 313 lines):
- `shellcheck -S warning` → clean (no output). `bash -n` → OK.
- Metric explicitly `WALL-CLOCK SECONDS PER CONVERSION` — header comment (lines 3–19) explicitly states **no per-page divisor, no RTF ratio**; the `RAW_SEC_PER_CONVERSION` computation (`$1/1000`, "division-by-one... unambiguous: WALL-SEC-PER-CONVERSION") confirms this in code, not just comments.
- Corpus: sources `{md, latex, html, docx}` × sizes `{small, medium, large}` × representative text-out/zip-out target pairs restricted to `pandocPairTable` (8 src:tgt pairs × 3 sizes = 24 cells), synthesized inside the container.
- Runs at `--cpus=2.0 --memory=1g` (matches compose `pandoc-worker` resource limits exactly, per section 2 and later cross-checked against `docker-compose.yml`).
- Section 8 "DERIVATION HELP" **prints** the formula `ceil(worst_cell_p95 x 2.0)` and the 900s CAP constraint but explicitly **never assigns/commits** a `PANDOC_ENGINE_TIMEOUT` value; exit code reflects measurement-pipeline integrity only, independent of any p95 value observed.

**Verdict: SC2 VERIFIED.**

---

### SC3 — Measured, derived, CAP-safe timeout + Key Decision

- `docker-compose.yml`: `PANDOC_ENGINE_TIMEOUT: "65s"` appears **10 times**, one per queue-client service, all byte-identical (`sort -u` over all 10 occurrences collapses to a single distinct value, differing only in a trailing comment). `PANDOC_MAX_INPUT_BYTES: "4194304"` (4 MiB) appears once, `pandoc-worker`-service-only, correctly NOT propagated to other services (comment: "NOT read by queue.NewClient()").
- 65s < 900s reconciler CAP (92.8% margin per comments); 65 ≠ 744 (OCR) / 753 (AV) / 742 (AUDIO) / 90 (prior placeholder) — confirmed distinct.
- `stop_grace_period: 85s` on `pandoc-worker` = 65s + 10s (ShutdownTimeout margin) + 10s (Docker SIGKILL margin), matching the documented derivation.
- Key Decision recorded in `.planning/PROJECT.md` (line 262, Decisions table) **and** `.planning/STATE.md` (lines 168–169, Decisions log) with full conditions: arch=arm64, cpus=2.0, memory=1g, N=10/cell, nearest-rank p95, worst cell `html_large→md` p95=32.318s, Path-B lever fired **by memory** (docx/epub OOM at 8 MiB on 1g, cliff at 4 MiB clean / 6.5 MiB thrash / 8 MiB OOM), `PANDOC_MAX_INPUT_BYTES` lowered 50 MiB → 4 MiB.
- `docker compose config` parses cleanly (verified this session, exit 0, no errors).

**Verdict: SC3 VERIFIED.**

---

### SC4 — IN-02 env parity + CI bake matrix

- All 10 queue-client services (`api`, `worker`, `webhook-worker-1`, `webhook-worker-2`, `document-worker`, `chromium-worker`, `audio-worker`, `av-worker`, `ocr-worker`, `pandoc-worker`) — confirmed by grepping every top-level compose service block and cross-referencing against the 9 Go `cmd/*/main.go` files calling `queue.NewClient()` (webhook-worker runs twice as `-1`/`-2`) — carry identical `PANDOC_ENGINE_TIMEOUT: "65s"` and `PANDOC_MAX_RETRY: "2"`.
- `.github/workflows/ci.yml`: `pandoc-worker` present in the `docker-build` tier bake `set:` (both `cache-to` and `cache-from`) and in the `e2e` tier bake `set:` (`cache-from`) — confirmed at lines 81–82 and 119.

**Verdict: SC4 VERIFIED.**

---

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `Dockerfile.pandoc-worker` | Pinned static pandoc, TeX-free, dual-arch, USER nobody | ✓ VERIFIED | Read in full; live image inspected (pandoc 3.10.1, no TeX binaries, `nobody`, 317MB) |
| `scripts/pandoc-timing-measure.sh` | p95 wall-sec/conversion measurement, prints-never-commits | ✓ VERIFIED | shellcheck-clean, `bash -n` OK, executable, full 313-line read |
| `docker-compose.yml` `pandoc-worker` service | Build, resource limits, measured timeout, IN-02 parity | ✓ VERIFIED | `docker compose config` parses; grep confirms 10-service parity |
| `.planning/PROJECT.md` / `.planning/STATE.md` | Key Decision recorded with conditions | ✓ VERIFIED | Both files contain matching, detailed entries |
| `.github/workflows/ci.yml` | pandoc-worker in bake matrix | ✓ VERIFIED | Present in docker-build + e2e tiers |
| `internal/e2e/e2e_test.go` `TestPandocConversionE2E` | md→html + md→docx live E2E, signed webhook | ✓ VERIFIED | Read in full; wired to non-stub `assertDownloadIsFormat`/`assertSignedWebhook`/`assertDownloadContainsText`; self-skips offline (confirmed live this session) |
| `.planning/REQUIREMENTS.md` PDCE-04 | Marked complete | ✓ VERIFIED | `[x] PDCE-04` with full derivation summary, line 24; traceability table line 59 = "Complete" |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `Dockerfile.pandoc-worker` | pandoc static tarball | `sha256sum -c -` fail-closed | WIRED | Confirmed in Dockerfile RUN block; live image reports correct pinned version |
| `scripts/pandoc-timing-measure.sh` | `Dockerfile.pandoc-worker` | `docker build -f Dockerfile.pandoc-worker` | WIRED | Section 1 of script |
| `scripts/pandoc-timing-measure.sh` p95 | `docker-compose.yml` `PANDOC_ENGINE_TIMEOUT` | `ceil(worst_p95 x 2.0)`, recorded Key Decision | WIRED | 32.318s → 65s traced through PROJECT.md/STATE.md/compose comments consistently |
| `docker-compose.yml` `pandoc-worker` | every queue-client service | identical `PANDOC_ENGINE_TIMEOUT`/`PANDOC_MAX_RETRY` (IN-02) | WIRED | Verified via grep across all 10 service blocks |
| `internal/e2e/e2e_test.go` | presigned html/docx download | `pollUntilDone` → `download_url` → `assertDownloadContainsText`/`assertDownloadIsFormat` | WIRED | Both helpers read and validate real response bodies (HTML tag/text, `convert.SniffContainer`) |
| `internal/e2e/e2e_test.go` | signed webhook | `assertSignedWebhook` HMAC check | WIRED | Called in `MD_ToHTML` subtest |

### Automated Checks (run this session)

| Check | Command | Result |
|-------|---------|--------|
| Build | `go build ./...` | PASS (no output, exit 0) |
| Vet | `go vet ./...` | PASS (no output, exit 0) |
| Pandoc E2E (offline) | `go test ./internal/e2e/... -run TestPandocConversionE2E -count=1` | PASS — self-skips (`E2E_BASE_URL not set`), exit 0 |
| Compose parse | `docker compose config >/dev/null` | PASS, exit 0 |
| Shellcheck | `shellcheck -S warning scripts/pandoc-timing-measure.sh` | PASS, no warnings |
| Shell syntax | `bash -n scripts/pandoc-timing-measure.sh` | PASS |
| Live image TeX-free | `docker run --rm --entrypoint sh octoconv-pandoc-worker:dev -c '...'` | PASS — no TeX binaries found |
| Live image version | `docker run --rm --entrypoint pandoc octoconv-pandoc-worker:dev --version` | `pandoc 3.10.1` (matches pin) |
| Live image user | `docker image inspect ... .Config.User` | `nobody` |

**Note on live E2E:** Per task scope, the live compose `TestPandocConversionE2E` run was **not re-executed** in this verification session (operator-attested). The operator-attested evidence (`49-04-SUMMARY.md`): `PASS TestPandocConversionE2E (4.13s)`, `MD_ToHTML 2.05s` / `MD_ToDOCX 2.03s`, DB `job_events` showing md→docx/md→html/html→md all `done`, worker with no retry-exhaust. This verification confirms the artifacts that produced that pass (test code, helpers, fixture, compose service, measured timeout) all exist, are substantive (not stubs), and are correctly wired — satisfying the goal-backward bar for accepting the attestation.

### Invariant Checks

| Invariant | Expected | Result |
|-----------|----------|--------|
| `internal/convert/exec.go` unchanged | No Phase-49 commit touches it | ✓ Confirmed — not in any of the 9 Phase-49 commits' file lists |
| `internal/convert/progress.go` unchanged | No Phase-49 commit touches it | ✓ Confirmed — not touched |
| `internal/api/sse.go` unchanged | No Phase-49 commit touches it | ✓ Confirmed — not touched |
| `go.mod`/`go.sum` unchanged | No new Go dependency | ✓ Confirmed — last commit touching go.mod is Phase 36, unrelated to Phase 49 |
| Lean image | No TeX toolchain | ✓ Confirmed live (317MB, no pdflatex/xelatex/tectonic/wkhtmltopdf/weasyprint/texlive/latex on PATH) |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| PDCE-04 | 49-01/02/03/04 | `PANDOC_ENGINE_TIMEOUT` measured by dedicated gate, under global CAP; empty-but-valid → done | ✓ SATISFIED | `.planning/REQUIREMENTS.md` line 24 marked `[x]`, full derivation text matches PROJECT.md/STATE.md/docker-compose.yml exactly; empty-but-valid→done closed in Phase 48 (out of this phase's scope, correctly not re-claimed here) |

No orphaned requirements found for Phase 49 in `.planning/REQUIREMENTS.md`'s Phase-49 mapping.

### Anti-Patterns Found

None. No `TBD`/`FIXME`/`XXX`/`TODO`/`HACK` markers in `Dockerfile.pandoc-worker`, `scripts/pandoc-timing-measure.sh`, or the new/modified `internal/e2e/e2e_test.go` sections. References to "placeholder" in `docker-compose.yml` are historical derivation-narrative comments (documenting the 90s→65s / 50MiB→4MiB transition per PDCE-04's "measured not copied" requirement), not live unresolved placeholders — all current values for pandoc-worker are the final measured numbers.

### Data-Flow Trace (Level 4)

Not applicable in the strict sense (this phase produces infra/measurement artifacts, not a UI rendering pipeline), but the equivalent trace was performed: `scripts/pandoc-timing-measure.sh`'s printed p95 → `.planning/PROJECT.md`/`STATE.md` Key Decision → `docker-compose.yml` `PANDOC_ENGINE_TIMEOUT` value — all three show the identical number (32.318s → 65s) with consistent conditions (arch/cpu/mem/N), i.e. the "data" (measured p95) genuinely flows into the committed configuration rather than being independently asserted.

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Runtime pandoc reports pinned version | `docker run --rm --entrypoint pandoc octoconv-pandoc-worker:dev --version` | `pandoc 3.10.1` | ✓ PASS |
| Image is TeX-free | grep for pdflatex/xelatex/tectonic/wkhtmltopdf/weasyprint/texlive/latex on PATH inside container | none found | ✓ PASS |
| Image runs unprivileged | `docker image inspect ... .Config.User` | `nobody` | ✓ PASS |
| Pandoc E2E self-skips offline (does not fake-pass) | `go test ./internal/e2e/... -run TestPandocConversionE2E -count=1` | `SKIP ... E2E_BASE_URL not set` | ✓ PASS |
| Compose config parses with all 10-service parity | `docker compose config >/dev/null` | exit 0 | ✓ PASS |

### Probe Execution

No `scripts/*/tests/probe-*.sh` convention or explicit probe declarations found for Phase 49 (this phase's verification apparatus is the timing-measure script + go test, both covered above under Behavioral Spot-Checks). SKIPPED — no probes applicable.

### Human Verification Required

None. All must-haves were verifiable via static inspection, local image inspection, and automated command execution. The one item requiring live infrastructure (the full compose E2E run) is explicitly operator-attested per task scope with concrete, checkable evidence (exact PASS timings, DB state) rather than a bare narrative claim, and the artifacts underlying that attestation were independently verified as substantive and wired in this session.

### Gaps Summary

No gaps found. All four Success Criteria (SC1–SC4) are verified against actual codebase artifacts, not SUMMARY.md narrative alone. Key findings that increase confidence beyond the SUMMARYs:
- The pandoc-worker image was independently pulled and inspected live in this session (not just read as Dockerfile source) — pandoc 3.10.1, no TeX binaries, `nobody` user, 317MB, all confirmed empirically.
- The measurement script's "prints but never commits" claim was verified in the actual script logic (the exit-code/derivation-help section), not just the header comment.
- IN-02 parity across all 10 services was verified by direct grep/count, not assumed from the SUMMARY's claim.
- The Key Decision was cross-checked as present, consistent, and detailed in both PROJECT.md and STATE.md.
- `go build`, `go vet`, the offline e2e self-skip, and `docker compose config` all pass cleanly in this session.
- No unchanged-invariant file (exec.go/progress.go/sse.go/go.mod) was touched by any Phase-49 commit.

---

_Verified: 2026-08-01T04:25:02Z_
_Verifier: Claude (gsd-verifier)_
