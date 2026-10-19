# OctoConv UI Repository Extraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish the verified frontend with its file history to private tileforge/octoconv-ui, then make the backend checkout independent of frontend sources.

**Architecture:** Export frontend/ history from the approved gsd/workspace-frontend branch into a standalone local repository. Give the web client its own build, tests, runtime API reverse proxy and deployment examples before publishing. Remove the backend copy only after the private remote is verified.

**Tech Stack:** Existing React 19, TypeScript 6, Vite 8, Vitest 4; Git subtree; GitHub; static-web container with an Nginx reverse proxy; Compose and optional Kubernetes deployment example.

**Spec:** docs/superpowers/specs/2026-10-19-frontend-repository-split-design.md

## Global Constraints
- Destination is private tileforge/octoconv-ui; the user selected this name and visibility.
- Use existing gsd/workspace-frontend worktree; do not modify the main checkout or erase the old branch.
- Preserve frontend file history with git subtree split. Never publish the full backend history or force-push.
- Keep the teal/graphite Workspace and all 57 existing frontend tests. Legacy remains removed.
- Keep Node >=20.19.0, existing dependency versions and the package lock. Use Node 26 for verification/build environments; verify container tags/digests before selecting them.
- No HTTP API, authentication, schema, server behavior or backend mandatory deployment changes.
- Never embed API keys in build settings, repository files or container images.
- No automatic merge or deployment. Backend work remains on its feature branch; bootstrap the new repository on main.

## Review Focus
- Export contains source history without backend files: prove by tree inspection and exact source comparison in Task 1.
- Proxy strips authentication or changes URLs: test Authorization, method, multipart bytes, query string and path preservation in Task 2.
- SSE is buffered or interrupted: test a first event reaches the client before the second event in Task 2.
- SPA fallback masks API errors: test deep links serve index.html but API 401/404 preserve status/body in Task 2.
- Removing frontend breaks backend tests: establish a green Go-only scan and run backend checks after removal in Task 4.

### Task 1: Export a verified standalone source history
**Files:** New local repository at /Users/apaderin/dev/octoconv-ui; source frontend/ contents become its root. Preserve the original backend LICENSE as LICENSE in Task 2.
**Interfaces:** Consumes the committed frontend tree in gsd/workspace-frontend. Produces a standalone repository with package.json, package-lock.json, src/, vite.config.ts, tsconfig.json and index.html at its root.

- [ ] Check live worktree status and record the source commit. If /Users/apaderin/dev/octoconv-ui exists, inspect it and reuse only if it belongs to this migration; never overwrite unrelated data. Confirm GitHub destination is absent or an explicitly intended empty repository.
- [ ] Run git subtree split --prefix=frontend HEAD in the source worktree, record the resulting commit, and initialize/fetch that split into the new local repository with main as its branch. Request filesystem/network escalation if needed; do not clone the entire backend as the destination.
- [ ] Compare tracked file paths and bytes against the source frontend tree; assert that there are no internal/, cmd/, .planning/ or backend docs/openapi.yaml paths. Inspect the split log for historical frontend commits. No test is needed for Git's own history algorithm.
- [ ] Run npm ci, npm test and npm run build in the exported repository. Expected: all 57 tests pass and production assets build. Keep the backend copy intact on any failure.

### Task 2: Make the web client independently operable
**Files:** Create README.md, LICENSE, .gitignore, .dockerignore, Dockerfile, deploy/nginx.conf.template, compose.example.yaml, deploy/kubernetes.example.yaml, .github/workflows/ci.yml and tests/proxy.test.mjs in the new repository. Update package.json and package-lock.json package name to octoconv-ui without changing dependencies.
**Interfaces:** The static web server listens on 8080. Environment variable OCTOCONV_API_UPSTREAM is a bare http(s) origin (no path/query/credentials), default http://api:8090. /v1, /v1/* and /healthz are proxied unchanged; other SPA routes resolve to index.html. Dev server retains VITE_API_PROXY_TARGET.

- [ ] Write failing container/proxy tests using Node's built-in test runner and a local fixture API. Test HTTP method, original path/query, Authorization header, upload body bytes, upstream 401/404 bodies, SPA deep-link fallback, health proxy and streaming arrival before stream completion. Example assertion: a fixture response 401 with {"code":"unauthorized"} remains HTTP 401 and identical JSON through /v1/jobs.
- [ ] Run the proxy tests against the initial static build to demonstrate the missing proxy/container behavior; record the intended failure. Do not change existing application behavior to accommodate fixtures.
- [ ] Add a multistage npm-ci/build image and static server image with version/digest pins verified against official registries at implementation time. Substitute only OCTOCONV_API_UPSTREAM in the proxy template; preserve Nginx variables. Disable SSE response buffering/cache, use a suitable long read timeout, preserve Authorization, and defer upload-size enforcement to the backend. Validate upstream input and fail startup for a path, query, credentials or newline.
- [ ] Add an opt-in Compose example exposing 8080 and joining an explicitly selected existing API network, with the upstream supplied at runtime. Add a Kubernetes example with a separate deployment/service and optional ingress; do not edit the backend chart to require it.
- [ ] Write standalone setup/build/test/container instructions, proxy configuration, API-key handling, MIT attribution, source repository/history provenance, and the backend commit/OpenAPI contract used for verification. Describe that live worker/S3 conversion is not established by fixture tests.
- [ ] Add read-only CI permissions and a workflow_dispatch job for npm ci, npm test, npm run build and container verification. Keep automatic runs disabled initially, consistent with the backend repository's existing Actions limit policy. No secrets or publication job is required.
- [ ] Run frontend tests/build, proxy tests, container build, Compose config validation and Kubernetes manifest parsing. If the container runtime is unavailable, report the specific unverified behavior and do not claim proxy verification passed. Commit standalone packaging/docs only after available checks pass.

### Task 3: Create and verify the private destination
**Files:** Remote private tileforge/octoconv-ui; local origin configuration. No backend deletion in this task.
**Interfaces:** Consumes the verified source/packaging commits. Produces a remote main branch with independent frontend history and confirmed PRIVATE visibility.

- [ ] Inspect GitHub authentication and org permissions without printing credentials. Recheck whether tileforge/octoconv-ui exists immediately before creation. Never overwrite or change the visibility of an existing unrelated repository.
- [ ] Create tileforge/octoconv-ui as private without auto-generated README/license commits, set origin, and push main without force.
- [ ] Verify remote privacy, main HEAD equality, representative files, and absence of backend paths. Preserve source/local export on any failure. Record destination URL and both source/split/remote commit IDs in migration evidence.

### Task 4: Decouple and verify the backend repository
**Files:** Delete frontend/ only in gsd/workspace-frontend. Modify README.md frontend section/tree entry and internal/mcpserver/no_text_matching_test.go. Update this design's migration evidence with final URLs/commits, without moving backend OpenAPI into the web repository.
**Interfaces:** Backend checkout no longer contains or requires frontend sources; TypeScript error-message checks remain in octoconv-ui. Existing Go consumer scan continues to run and reject message-based branching.

- [ ] Before deletion, run the Go scan with frontend absent in a disposable checkout to reproduce the current missing-file failure. Confirm this is the precise backend dependency being removed.
- [ ] After Task 3 succeeds, remove frontend/ from the backend branch and remove its TypeScript-specific pattern/path loop from the Go scan. Keep all Go consumer coverage and its fail-closed missing-Go-file behavior.
- [ ] Link README to https://github.com/tileforge/octoconv-ui; describe independent optional deployment and point to the new setup instructions. Preserve backend-only Compose/Helm defaults.
- [ ] Run go test ./internal/mcpserver -run '^TestNoConsumerBranchesOnErrorMessageText$' -count=1, go test ./..., go build ./... and git diff --check. Record integration tests that self-skip without services; never present them as live verification. Search tracked code/config for stale mandatory frontend file references and fix only actual build/test links.
- [ ] Perform one independent final review of both repositories for API compatibility, publication safety, optional deployment and secrets; cap total review/fix rounds at five. Commit backend separation, report checks and remaining environment limits. Do not merge, publish containers or deploy.

## Self-review
All spec requirements map to Tasks 1-4: history/boundary (1), independent operation/deployment/compatibility docs (2), private publication (3), backend independence and rollback boundary (4). Five failure conditions above have explicit checks. No implementation begins before user plan review and execution-method selection.
