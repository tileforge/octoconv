---
phase: 49-containerization-measured-timeout-gate
plan: 01
subsystem: infra
tags: [docker, pandoc, dockerfile, sha256-pinning, dual-arch]

# Dependency graph
requires:
  - phase: 48-pandoc-worker-command
    provides: "cmd/pandoc-worker/main.go (complete, unmodified) — PANDOC_ENGINE_TIMEOUT/PANDOC_MAX_INPUT_BYTES/PANDOC_WORKER_CONCURRENCY env plumbing"
provides:
  - "Dockerfile.pandoc-worker — a buildable, SHA-256-pinned static pandoc 3.10.1 image, TeX-free, dual-arch, USER nobody"
affects: [49-02-compose-env-ci, 49-03-measured-timeout, 49-04-live-e2e]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Pinned official static-release binary download (no source-build stage) with fail-closed sha256sum -c before extraction — the simplification over Dockerfile.ocr-worker/av-worker/audio-worker's source-build shape"
    - "ARG TARGETARCH-selected tarball+SHA pair for native dual-arch builds (no qemu)"

key-files:
  created: [Dockerfile.pandoc-worker]
  modified: []

key-decisions:
  - "Pinned pandoc 3.10.1 (resolved live via GitHub API on 2026-07-31, matches the version already anticipated in 49-CONTEXT.md D-02)"
  - "linux-amd64 SHA-256: 72948bf5784f560d5ad1876709daca27e0667f262da727bb33f77b58e52df2f5 (verified live by downloading and sha256sum-ing the tarball, not trusted from a checksum page)"
  - "linux-arm64 SHA-256: cd3963da375793a4804c65ae538b4f7b9c23f87cac7f6c74a1cf5e2fff7e8d59 (verified live, same method)"
  - "Only bin/pandoc is extracted from the tarball (share/man, pandoc-server, pandoc-lua all discarded) to keep the image lean"
  - "curl + ca-certificates are the ONLY runtime apt packages — no poppler-utils/tesseract (not needed) and above all no texlive/latex/tectonic/wkhtmltopdf/weasyprint (D-02's lean TeX-free goal; pandoc's ->pdf writer is not in pandocPairTable so TeX would be dead weight)"

patterns-established:
  - "Prebuilt-binary-download engine images (no compiler toolchain needed) can skip the source-build stage entirely when the upstream project ships an official static release per architecture — still requires the same fail-closed SHA-256 discipline as source-built engines"

requirements-completed: [PDCE-04]

# Metrics
duration: 25min
completed: 2026-07-31
---

# Phase 49 Plan 01: Dockerfile.pandoc-worker Summary

**A buildable, SHA-256-pinned, TeX-free `pandoc-worker` container image running pandoc 3.10.1 as USER nobody, verified live for both amd64 and arm64.**

## Performance

- **Duration:** 25 min
- **Started:** 2026-07-31T15:58:00Z
- **Completed:** 2026-07-31T16:25:26Z
- **Tasks:** 1
- **Files modified:** 1 (created)

## Accomplishments
- Authored `Dockerfile.pandoc-worker`: a Go build stage producing `/out/pandoc-worker` (unchanged Phase 48 binary) plus a runtime stage that downloads, SHA-256-verifies (fail-closed), and installs the official static pandoc 3.10.1 binary for the build's TARGETARCH — no engine source-build stage, no data-bake, the deliberate simplification over Dockerfile.ocr-worker.
- Resolved LIVE (not copied from a page): pandoc's latest stable release is 3.10.1; downloaded and `sha256sum`'d both the `linux-amd64` and `linux-arm64` release tarballs myself to pin their hashes, rather than trusting an unverified published checksum.
- Proved the image builds green natively for arm64 (this host) and cross-built for amd64 via `docker buildx --platform linux/amd64` — both selected the correct tarball+SHA via the `TARGETARCH` case statement and passed `sha256sum -c -`.
- Verified inside the built image: `pandoc --version` reports `pandoc 3.10.1`; none of `pdflatex`/`xelatex`/`tectonic`/`wkhtmltopdf`/`weasyprint` are present on PATH; the container runs as `nobody`; total image size is 317MB (lean — no multi-GB TeX toolchain).

## Task Commits

Each task was committed atomically:

1. **Task 1: Dockerfile.pandoc-worker — Go build stage + SHA-256-pinned static pandoc 3.x, debian-slim runtime, USER nobody, TeX-free, dual-arch** - `f75862f` (feat)

**Plan metadata:** (this commit)

## Files Created/Modified
- `Dockerfile.pandoc-worker` - Two-stage Dockerfile: golang:1.26-bookworm build stage (unchanged Go build of cmd/pandoc-worker) + debian:bookworm-slim runtime stage that downloads/verifies/installs a pinned static pandoc 3.10.1 binary selected by TARGETARCH, asserts its version at build time, and runs as USER nobody with no TeX toolchain.

## Decisions Made
- Resolved pandoc 3.10.1 as the current stable release via `https://api.github.com/repos/jgm/pandoc/releases/latest` on 2026-07-31 (matches 49-CONTEXT.md's anticipated local-dev version).
- Verified both amd64/arm64 tarball SHA-256 hashes by downloading and hashing them directly (not copied from any release notes page) — see key-decisions above for the exact hashes.
- Extracted only `bin/pandoc` from the release tarball (discarding `pandoc-server`, `pandoc-lua`, and `share/man`) since the worker only shells out to a bare `pandoc` on PATH (per the plan's interface contract).
- Included `curl` in the final runtime image (needed at build time to fetch the pinned tarball; kept in the final image is harmless and matches the plan's explicit instruction to install `ca-certificates` + `curl` only, no poppler/tesseract/TeX).

## Deviations from Plan

None - plan executed exactly as written. The Dockerfile shape (build stage, TARGETARCH case-selected pinned tarball, fail-closed SHA verification, version assertion, USER nobody, ENTRYPOINT, no init/reaper) matches the plan's action description precisely; no bugs, missing functionality, or blockers were encountered.

## Issues Encountered
None.

## User Setup Required

None - no external service configuration required. (The image is not yet wired into docker-compose or CI — that is Plan 02's scope.)

## Next Phase Readiness
- `Dockerfile.pandoc-worker` builds green and is ready for Plan 02 to wire into `docker-compose.yml` (the `pandoc-worker` service), IN-02 env propagation, and the CI bake matrix.
- No timeout value was finalized here — `PANDOC_ENGINE_TIMEOUT` remains Phase 48's placeholder (90s) until Plan 03's supervised measurement run.
- Both pinned SHA-256 hashes (amd64 + arm64) are recorded above for Plan 02/03/04 to reference without re-resolving.

---
*Phase: 49-containerization-measured-timeout-gate*
*Completed: 2026-07-31*

## Self-Check: PASSED
- FOUND: Dockerfile.pandoc-worker
- FOUND: f75862f (git log --oneline --all)
