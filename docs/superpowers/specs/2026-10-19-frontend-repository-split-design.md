# Separate OctoConv web client

## Purpose and agreed scope
The frontend belongs to a separate team and is optional in OctoConv installations. Keep the Go API independently buildable, testable and deployable. The web client keeps the approved teal/graphite Workspace, with Legacy removed. The destination GitHub repository is private tileforge/octoconv-ui; the user selected its name and visibility.

## Repository boundary
- The new repository contains the contents of frontend/ at its root: React application, API client, Vite configuration, lockfile and frontend tests.
- Export the frontend/ history using git subtree split after the approved UI changes are committed. Do not rewrite the backend repository or publish its complete history to the frontend repository.
- Add a standalone README, MIT license attribution from the source repository, gitignore, frontend test/build CI and a multistage static-web container build in the new repository.
- Keep docs/openapi.yaml and server behavior in the backend repository. Frontend documentation links to that contract and states the backend release/commit used for validation. Do not claim compatibility with untested releases.

## Deployment and configuration
The frontend ships as a separate static-web container. Its web server serves the SPA and proxies /v1 and /healthz to a runtime-configured OctoConv API upstream, preserving Authorization and streamed SSE responses. Local Vite development retains VITE_API_PROXY_TARGET. API keys are entered by a user; never embed them in a build or container environment.

Provide an opt-in Compose example that connects the frontend to an existing API service/network. Document Kubernetes deployment as a separate optional web deployment/service/ingress. Existing backend Compose and Helm installations continue to work with no frontend installed; do not introduce a required frontend image or fetch dependency.

## Migration order
1. Commit the teal palette and Legacy removal in gsd/workspace-frontend.
2. Export and verify frontend history in a separate local repository. Add independent packaging, CI, docs and attribution; run frontend tests, build, and container configuration checks.
3. After the user selects the repository name and approves this design, create the private destination and push the verified frontend history. Verify the remote files and privacy.
4. Only after that succeeds, remove frontend/ in the backend branch. Update README links and the Go no-message-text-branch test: it should scan Go consumers only; the frontend repository owns equivalent TypeScript checks. Run the backend check and full appropriate backend tests before a reviewable commit.
5. Keep the change on feature branches. Do not merge or deploy as part of this migration.

## Compatibility and acceptance
HTTP routes, payloads, authentication and the OpenAPI contract remain unchanged. The frontend's public describeError export and coded-error behavior remain covered while Legacy is deleted. Existing conversion, pagination, status streaming/polling, cancellation, credential isolation and all output links remain intact.

Success means both repositories can be checked out and verified independently; installing the backend does not require frontend sources or Node.js. The frontend has its original file history, its own CI and an independently configurable API upstream. Record any untested live worker/S3 or container-runtime behavior explicitly.

## Failure and recovery
Do not remove the backend copy until the separate repository exists and is verified. If export, remote creation or publication fails, preserve the backend copy and local export. Existing feature commits and worktree remain available. No force pushes, source history rewriting, deletion of the previous branch or automatic merge is required.

## Migration evidence (2026-10-19)
- Private destination: https://github.com/tileforge/octoconv-ui; local checkout: /Users/apaderin/dev/octoconv-ui.
- Source commit: 8c0aaa59092f3bfaa8123b5a7b702c0368d0fcd9. Split commit: e2e17d12ba057f2fdbdc665a9e5d47c7a1363d49. Source frontend and split root tree both equal 783aed46565ab534c3176e88fae6edb28c6c5388.
- Published UI head after the README clarification: 8e2623f (CI verified the same code/container at bc83b00c4f672d203b0fc9b27c03e6b90df99be2). The remote is PRIVATE and contains the frontend history only.
- UI verification: 57 tests, production build, Docker image build, 7 container/proxy tests, Compose config validation and Kubernetes client-side manifest parsing passed. No Kubernetes resources were applied.
- Manual CI: https://github.com/tileforge/octoconv-ui/actions/runs/39248568772 completed successfully. Workflow creation used the connected GitHub app after OAuth rejected the workflow path; no credential permissions were expanded and no force push was used.
- Independent review identified the remaining Go alias cross-source test and default frontend final-gate step. Both were removed from the backend boundary, preserving separate alias tests and adding CLI regression checks. No production API change.
- Browser dev preview now runs from the independent UI checkout on 127.0.0.1:5174. No worker/S3 conversion or production deployment is claimed by this migration.

### Backend verification limitation
`go build ./...`, coded-error boundary checks, format alias tests and backend-only gate CLI regressions passed. Full `go test ./...` completed with one failure in `TestLibreOfficeConverter_TimeoutKillsRealProcess` (soffice.bin did not appear). A targeted rerun on an unchanged archive of the source commit reproduced the same failure. Other results were 4833 passing test/subtest events and 232 conditional skips. Live worker/S3 acceptance is outside this verification. The LibreOffice failure was left visible rather than weakened or hidden.
