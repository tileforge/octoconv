---
phase: quick-261018-bmr
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - internal/api/transport_cap_test.go
  - deploy/chart/octoconv/templates/configmap.yaml
  - docs/CAD.md
  - docker-compose.yml
  - .env.example
  - deploy/chart/octoconv/values-inspect-gate.yaml
  - scripts/keda-cad-downscale-survival.sh
  - docs/ERROR-CODES.md
  - docs/openapi.yaml
  - scripts/fixtures/README.md
  - scripts/fixtures/gen_inspect_burst_pdf.py
  - .planning/todos/pending/2026-10-18-helm-upload-cap-below-cad-ceilings.md
  - .planning/todos/completed/2026-10-18-helm-upload-cap-below-cad-ceilings.md
  - .planning/todos/pending/2026-10-18-api-multipart-spill-unbounded-disk.md
  - .planning/STATE.md
  - .planning/quick/261018-bmr-helm-upload-cap-at-least-the-measured-ca/evidence/*
  - .planning/quick/261018-bmr-helm-upload-cap-at-least-the-measured-ca/261018-bmr-SUMMARY.md
autonomous: true
requirements: [quick-261018-bmr]

estimate:
  tokens: 170000
  raw_tokens: 170000
  tasks: 3
  confidence: low

must_haves:
  truths:
    - "With the Helm chart's own ConfigMap caps, POST /v1/jobs carrying the largest admissible binary STL (190840784 bytes, 3816814 triangles, the largest binary STL at or below convert.CADMaxSTLBytes) inside the largest request the API bounds (a 255-byte filename, opts of exactly maxOptsBytes, operation render, target png) returns 202 and reaches the cad queue with every byte uploaded, and POST /v1/inspect of the same request returns 200 with the cad render png offer available and max_bytes 190840832; at e321261 both return the generic 413"
    - "For every shipped configuration of the API (the chart ConfigMap default, every api.extraEnv value in values.yaml and in every values-*.yaml overlay, the docker-compose api service, .env.example), MAX_UPLOAD_BYTES and INSPECT_MAX_UPLOAD_BYTES are each at least every ceiling GET /v1/formats advertises (operation, engine, source format, read through Server.sizeLimit with that configuration's PANDOC_MAX_INPUT_BYTES) plus the measured envelope of that route's largest bounded request; INSPECT_MAX_UPLOAD_BYTES is never larger than MAX_UPLOAD_BYTES; the av engine is the one exemption, and the test fails if the exemption stops being needed"
    - "The new chart, compose, .env.example and inspect-gate values are 191889408 (183 MiB), the next whole MiB above convert.CADMaxSTLBytes plus the 4938-byte envelope, a number the test derives and prints rather than a chosen one; per-engine and per-format ceilings, multipartInMemoryBudget, inspectIntakeMaxConcurrent, inspectIntakeReadDeadline and every Go default are unchanged (git diff main is empty for internal/api/api.go, handlers.go, inspect_handlers.go, internal/convert/cad.go, cmd/api/main.go; internal/convert/exec.go byte-identical)"
    - "docs/CAD.md's size table (held by TestCADDocMatchesTheCode) shows every effective ceiling equal to the format ceiling under the chart's 183 MiB cap, and no shipped text (docs/CAD.md, docs/ERROR-CODES.md, docs/openapi.yaml, .env.example, values-inspect-gate.yaml, scripts/fixtures) still describes the old chart cap or an inspect cap that must be strictly smaller than MAX_UPLOAD_BYTES"
    - "scripts/keda-cad-downscale-survival.sh pins TRANSPORT_CAP to the chart's new value, and its long_select replayed offline over the committed 76-03 evidence gives the same four render candidates, the same statuses and the same LONG (render_step M2, 194.90 s) as at the old value"
    - "The todo 2026-10-18-helm-upload-cap-below-cad-ceilings.md is in .planning/todos/completed/ with completed and resolution lines; a new pending todo records the API pod's unbounded multipart spill disk; STATE.md has the 261018-bmr Quick Tasks row and a Pending Todos list that matches .planning/todos/pending/"
  artifacts:
    - path: internal/api/transport_cap_test.go
      provides: "TestChartTransportCapsAdmitTheLargestCeilingFile (tracer, streamed end-to-end through Routes) and TestDeployedTransportCapsCoverEveryAdvertisedCeiling (every shipped configuration x every advertised ceiling, exact av exemption, INSPECT <= MAX)"
      contains: "TestDeployedTransportCapsCoverEveryAdvertisedCeiling"
    - path: deploy/chart/octoconv/templates/configmap.yaml
      provides: "MAX_UPLOAD_BYTES and INSPECT_MAX_UPLOAD_BYTES at 191889408"
      contains: "INSPECT_MAX_UPLOAD_BYTES: \"191889408\""
    - path: docs/CAD.md
      provides: "size table under the chart's 183 MiB transport cap, paragraph stating the chart caps sit above every ceiling plus the framing"
      contains: "effective under the chart's 183 MiB transport cap"
    - path: .planning/todos/pending/2026-10-18-api-multipart-spill-unbounded-disk.md
      provides: "follow-up: the API pod's ParseMultipartForm spill has no disk bound in the chart or compose"
  key_links:
    - from: "TestChartTransportCapsAdmitTheLargestCeilingFile"
      to: "TestDeployedTransportCapsCoverEveryAdvertisedCeiling"
      via: "one request builder writes the tracer's streamed body and measures the envelope the arithmetic test adds to each ceiling, so the envelope is what the handler actually admitted, not a prediction"
      pattern: "writeTransportCapRequest"
    - from: "TestDeployedTransportCapsCoverEveryAdvertisedCeiling"
      to: "Server.sizeLimit"
      via: "the ceilings are read from the same function handleCreateJob and the inspect offers read, on a server built per configuration"
      pattern: "sizeLimit\\("
    - from: "TestDeployedTransportCapsCoverEveryAdvertisedCeiling"
      to: "TestChartTransportCapsAdmitTheLargestCeilingFile"
      via: "the arithmetic test asserts that the largest non-exempt advertised ceiling is cad stl render, the file the tracer posts"
      pattern: "CADMaxSTLBytes"
    - from: "deploy/chart/octoconv/templates/configmap.yaml MAX_UPLOAD_BYTES"
      to: "internal/convert/cad_doc_test.go ceilings subtest and scripts/keda-cad-downscale-survival.sh TRANSPORT_CAP"
      via: "both read the chart's MAX_UPLOAD_BYTES; the doc table header and the script's fail-closed premise move with it"
      pattern: "MAX_UPLOAD_BYTES"
---

<objective>
Close todo 2026-10-18-helm-upload-cap-below-cad-ceilings.md (v1.16 audit TD-1) on branch `gsd/helm-upload-cap-cad-ceilings` (from main e321261, tag v1.16; commit there, do not push).

Goal, stated as the client sees it: no shipped deployment answers a file the service advertises a ceiling for with the generic transport 413 while the file is at or below that ceiling. Today the chart (100 MiB) refuses IGES, STL and DXF files between 100 MiB and their ceilings, and every deployment's inspect cap (100 MiB) refuses them on /v1/inspect. The same defect sits at the boundary of every 100 MiB per-engine ceiling in the chart: a 100 MiB image plus the multipart framing is above a 100 MiB cap.

The fix raises the chart's MAX_UPLOAD_BYTES and INSPECT_MAX_UPLOAD_BYTES, and the INSPECT_MAX_UPLOAD_BYTES of compose, .env.example and the inspect-gate overlay, to 191889408 (183 MiB): the next whole MiB above the largest non-av ceiling (STL, 182 MiB) plus the measured envelope of the largest request the API bounds. A test computes that requirement from the code and holds every shipped configuration to it, RED first. Per-engine and per-format ceilings are not changed. The av engine is exempted in the test (decision D1).

Output: internal/api/transport_cap_test.go, the configuration values, every text that described the old caps, the keda script premise, the todo bookkeeping, STATE.md, SUMMARY.
</objective>

<decisions>
Resolved by the orchestrator before execution. The tasks below implement the recommended option of each; a flipped decision needs a plan revision, not an executor judgment.

- **D1 (av).** Recommended (a): exempt av in the test with an exact, two-way exemption. Consequence, stated not hidden: av's effective k8s ceiling rises from 100 MiB to 183 MiB, because the transport cap is read before the engine is known and no other knob can hold av alone. Justification: the chart's 100 MiB predates D-07 (chart scaffolded 2026-07-14 in 8c7a97b, phase 24; D-07 raised compose and .env.example to 2 GiB on 2026-07-21 in f324d33, phase 35, which left Helm to phase 37, and phase 37 never touched the key), so it is drift, not an av decision; 183 MiB is inside av's own 2 GiB contract and the av worker still bounds the work (AV_MAX_DURATION_SECONDS 90) and checks free disk against the input (AV_DISK_SAFETY_FACTOR 3.0). Not chosen (b): the full class at 2 GiB plus framing in the chart and every inspect cap. That needs a bound on the API pod's spill storage (emptyDir sizeLimit and ephemeral-storage, none exist), a new inspect body deadline (30 s for 2 GiB needs about 68 MiB/s), and it reverses Phase 58 D-06 (inspect takes no multi-GiB body). The exemption also covers compose's and .env.example's MAX_UPLOAD_BYTES, which equal av's 2 GiB ceiling, so the last 4938 bytes below that ceiling still get the generic 413 there; this is recorded, not fixed.
- **D2 (Go fallback defaults).** Recommended (a): leave NewServer's InspectMaxUploadBytes fallback (100 MiB) and cmd/api's MAX_UPLOAD_BYTES fallback (2 GiB) unchanged; the test covers every shipped configuration, and every one of them sets both keys. Not chosen (b): raise the NewServer fallback to 183 MiB and add "env unset" as a configuration in the test. That is one Go line plus the doc comments at internal/api/api.go:346, :604, cmd/api/main.go:250 and internal/api/inspect_handlers.go:230, which are true today only of the 100 MiB fallback.
- **D3 (follow-up).** Recommended: file a pending todo for the API pod's unbounded multipart spill (Task 3). It predates this change; this change scales it by 1.83x in k8s.
- **Discretion (no decision needed).** The two chart caps stay literals in configmap.yaml rather than becoming chart values. Every numeric knob in that ConfigMap is a literal; operators override api keys through api.extraEnv (values-inspect-gate.yaml already does for INSPECT_MAX_UPLOAD_BYTES); a numeric chart value renders as float64 (1.91889408e+08) unless quoted; and a second override path for one key is a second place to disagree.
</decisions>

<execution_context>
@~/.claude/gsd-core/workflows/execute-plan.md
@~/.claude/gsd-core/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@.planning/todos/pending/2026-10-18-helm-upload-cap-below-cad-ceilings.md
@internal/api/cad_ceiling_test.go
@internal/e2e/chart_grace_test.go

Read with offsets: internal/api/api.go 160-180, 270-360, 460-560, 600-610; internal/api/handlers.go 50-65, 140-165, 630-720, 860-870; internal/api/inspect_handlers.go 200-275, 425-470; internal/api/handlers_test.go 195-215 (fakeStorage.Upload), 225-292 (fakeQueue), 700-760 (multipart helpers); internal/api/inspect_handlers_test.go 52-60 (doInspect); internal/convert/cad_doc_test.go 505-560 (ceilings subtest); scripts/keda-cad-downscale-survival.sh 25-35, 360-400, 425-460; internal/e2e/minio_image_pin_test.go 90-135 (yaml v3 use).

Session constants:
- `S=/private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad`; `TMPDIR=$S/qbmr` (create it) for every go test and mktemp, so multipart spill files land there.
- Python 3.14 first on PATH for every go test: `PATH=$S/py314:$PATH`.
- Pin guard before every write and every commit: `bash $S/pin-guard.sh`.
- Quick dir `Q=.planning/quick/261018-bmr-helm-upload-cap-at-least-the-measured-ca`, evidence under `$Q/evidence/`. Record `git rev-parse HEAD` as `$Q/evidence/base.sha` first.
- helm v4.3.0 at /opt/homebrew/bin/helm, offline `helm template` only. Secrets set: `--set secrets.databaseUrl=x --set secrets.apiKeySalt=x --set secrets.webhookSigningSecret=x --set secrets.s3AccessKey=x --set secrets.s3SecretKey=x --set secrets.postgresPassword=x`.

Research (planner, from the code at e321261; stop and report if the executor's base disagrees):
- Create path: `http.MaxBytesReader(w, r.Body, s.maxUploadByte)` wraps the whole multipart body, then `ParseMultipartForm(multipartInMemoryBudget)` (32 MiB, handlers.go:64,148-149). At most 32 MiB of file data stays in the heap per request (plus net/http's 10 MiB allowance for non-file values); the rest spills through os.TempDir to /tmp in the container's writable layer (Dockerfile.api sets no TMPDIR), removed by `MultipartForm.RemoveAll` on return. The per-engine/per-format ceiling `sizeLimit` runs only after the whole body is parsed and the format detected (handlers.go:707). All ten engines are gated (defaultMaxEngineBytes, api.go:478): image, document, html, audio, ocr, pandoc 100 MiB (pandoc 3 MiB in every shipped configuration via PANDOC_MAX_INPUT_BYTES), probe 50 MiB, tectonic 2 MiB, av 2 GiB, cad per format through defaultMaxFormatBytes (step 63, iges 120, stl 182, dxf 118 MiB; cad.go:24-36).
- Inspect path: the same shape with `s.inspectMaxUploadBytes` (inspect_handlers.go:440-441), at most 8 concurrent intakes (`inspectIntakeMaxConcurrent`), each with a 30 s body read deadline (`inspectIntakeReadDeadline`), then at most INSPECT_MAX_CONCURRENT (4) analyses that read at most INSPECT_MAX_WORK_BYTES (8 MiB).
- Chart: the api container has limits cpu 2 and memory 1Gi, no requests, no volumes, no emptyDir and no ephemeral-storage; no pod in the chart declares ephemeral-storage. Compose api: cpus 2.0, memory 1g, no tmpfs.
- What 100 to 183 MiB costs: heap unchanged (32 MiB per request; inspect intake 8 x 32 MiB = 256 MiB of 1 GiB; analysis 4 x 8 MiB). Worst-case transient spill per api pod rises 1.83x: inspect at most 12 files (8 intake + 4 analysis) of up to 183 MiB, about 2.1 GiB; create bounded only by RATE_LIMIT_IP_RPM 60 and RATE_LIMIT_CLIENT_RPM 120. No class's admission widens except av in k8s (D1). A body of 183 MiB through inspect needs at least 6.1 MiB/s to finish inside the 30 s read deadline (3.3 MiB/s at 100 MiB); in-cluster clients are far above that.
- Why the chart has 100 MiB: see D1. 73-CONTEXT R-04 and 76-CONTEXT kept it as scope control ("would widen every class", "an operator decision, outside this phase"); the research above shows admission widens only for av. Inspect's 100 MiB is Phase 58 D-06, a starting value, identical in chart and compose.
- Envelope, measured with mime/multipart.Writer: a file part named with 251 'a' plus ".stl" (255 bytes, the largest name jobs.SafeInputFilename admits, maxInputFilenameLen at internal/jobs/inputnames.go:32), fields target=png, operation=render and opts of exactly maxOptsBytes (4096) bytes, empty file content: 4938 bytes. convert.CADMaxSTLBytes 190840832 + 4938 = 190845770, next whole MiB 183 = 191889408, leaving 1043638 bytes for the fields the API does not bound (callback_url has no length check; the document's own filename is not passed through SafeInputFilename).
- The largest binary STL at or below the STL ceiling: 84 + 50 x 3816814 = 190840784 bytes. convert.sniffSTL accepts an all-zero header with a consistent count (cadsniff.go:220-247), and 3816814 triangles is below CADMaxRenderTriangles.
- Other readers of the chart caps: internal/convert/cad_doc_test.go:31,510-520 builds the docs/CAD.md table header from the chart's MAX_UPLOAD_BYTES (must be whole MiB) and wants the last column to equal min(ceiling, cap); scripts/keda-cad-downscale-survival.sh:30 pins `TRANSPORT_CAP=104857600` and :367 fails unless the chart equals it; values-inspect-gate.yaml:46-50 restates INSPECT_MAX_UPLOAD_BYTES as "UNCHANGED from the ConfigMap's own" default; scripts/inspect-burst-liveproof.sh:503-506 only requires the key to be set on the pod. No MCP server or frontend caps uploads.
- Planner's offline replay of long_select (the awk program at keda-cad-downscale-survival.sh:433-453 over `$CELLS`/`$INWORKER` of 76-03, band 180..590, ceilings from 76-03-derived.txt): at both 104857600 and 191889408, four render candidates, two eligible, none over the transport cap, pick render_step M2 (1474086 bytes, 194.90 s).
- Comment-line baselines (`grep -c '^[[:space:]]*#'`): .env.example 571, docker-compose.yml 1001, configmap.yaml 174, values-inspect-gate.yaml 120, keda-cad-downscale-survival.sh 1, gen_inspect_burst_pdf.py 21. shellcheck -S warning on the keda script: 0 findings.
- Tests that read the edited files (regression set): ./internal/api/ ./internal/e2e/ ./internal/errcode/ ./internal/inspect/ ./internal/queue/ ./internal/mcpserver/ ./cmd/tectonic-worker/, and in ./internal/convert/ TestCADDocMatchesTheCode, TestCADOpenAPIMatchesTheCode, TestErrorCodesDocStatesTheCADCeilingsOfTheCode, TestREADMECADRowNamesEveryCADPair, and in ./internal/worker/ TestCADCodesDocMatchesTheMapping.

Rules for every task: no code comments in any language and no new comment line in any file (the comment-line count of every edited file must not grow; an existing comment may be edited in place only where it becomes false). `internal/convert/exec.go` byte-identical to main. `GOTOOLCHAIN=go1.26.4` for every go command; never `go clean -cache`. Never run docker, kubectl against any cluster, helm install or any live stack; `helm template` and `scripts/ci-helm-checks.sh` are offline and allowed. Run repo scripts with bash. gofmt -l clean, `go vet ./...` clean. `.planning` paths need `git add -f`; never add `.planning/config.json`, `.vscode/` or `octoconv.code-workspace`. Commits are SSH-signed: check `git log -1 --format='%G?'` prints G after each, halt and report on a signing failure, never commit unsigned. No Co-Authored-By trailer. Do not push.
</context>

<tasks>

<task type="tracer" tdd="true">
  <name>Task 1: The chart's caps admit the largest-ceiling CAD file end to end, create and inspect (RED, GREEN)</name>
  <files>internal/api/transport_cap_test.go, deploy/chart/octoconv/templates/configmap.yaml, docs/CAD.md, .planning/quick/261018-bmr-helm-upload-cap-at-least-the-measured-ca/evidence/*</files>
  <precondition>`git rev-parse HEAD` is e3212610e136406d8aaa0529863920c90d3d26c2 on branch gsd/helm-upload-cap-cad-ceilings, and `git status --porcelain` lists only `.planning/config.json`, `.vscode/` and `octoconv.code-workspace`.</precondition>
  <read_first>
    - internal/api/cad_ceiling_test.go (newCADCeilingServer, cadZeroSTLAbove, postCADInspect, the offer assertions)
    - internal/api/handlers_test.go 195-215, 225-292, 700-760
    - internal/convert/cad_doc_test.go 505-560
    - docs/CAD.md 84-102
  </read_first>
  <behavior>
    - RED at e321261: TestChartTransportCapsAdmitTheLargestCeilingFile fails; create answers 413 with the generic "file exceeds size limit" body and inspect answers 413, both at the chart's own caps
    - GREEN: create answers 202, the job is on the cad queue (fakeQueue.enqueuedCAD equals the created id), the storage stub counted exactly 190840784 uploaded bytes; inspect answers 200 with a cad render png offer whose Available is true and MaxBytes is 190840832
    - GREEN keeps TestCADDocMatchesTheCode passing with the table header naming 183 MiB and every effective cell equal to its ceiling
  </behavior>
  <action>
Create internal/api/transport_cap_test.go (package api, no comments). It reads the chart cap literals from ../../deploy/chart/octoconv/templates/configmap.yaml with two anchored regexps, one for a line of exactly two spaces, MAX_UPLOAD_BYTES, colon, a quoted digit string, and one for INSPECT_MAX_UPLOAD_BYTES of the same shape; each must match exactly once, otherwise the test fails saying the value is not a literal it can read (fail closed: a templated value is not vouched for).

Add the request builder that Task 2 reuses, named writeTransportCapRequest. It writes, with mime/multipart.Writer onto a given io.Writer, a file part named fileName with the content of a given io.Reader, then (only when the engine takes several inputs, convert.EngineTakesMultipleInputs) jobs.MaxInputParts minus one further file parts with distinct empty contents and distinct names of the same maximal length, then the fields target, operation and opts, where opts is a JSON object of exactly maxOptsBytes bytes (an opening brace, spaces, a closing brace). It returns the form content type. The maximal filename length comes from a helper that probes jobs.SafeInputFilename on names of increasing length (n minus four 'a' characters plus ".stl") from 5 up to 4096 and returns the largest accepted length; it fails if no length is refused (the bound it relies on would be gone) or if the result is below 5.

Write TestChartTransportCapsAdmitTheLargestCeilingFile, skipped under testing.Short like its siblings in cad_ceiling_test.go. It builds a Server with NewServer and the same fake dependencies newCADCeilingServer uses, Config{MaxUploadBytes: chart MAX, InspectMaxUploadBytes: chart INSPECT} and nothing else, and a storage stub that embeds *fakeStorage and overrides Upload to copy into io.Discard while counting bytes (fakeStorage.Upload would hold the whole file in memory). The file is a binary STL streamed, never materialised: 80 zero bytes, the little-endian uint32 triangle count (convert.CADMaxSTLBytes minus 84) divided by 50, then 50 times that count zero bytes from an io.LimitReader over a zero reader. The request body is written into an io.Pipe by a goroutine calling writeTransportCapRequest with operation render, target png and the maximal filename ending in ".stl", and passed to httptest.NewRequest through authed, with the returned content type. Assert the create outcome in the behavior block. Then send the same builder's request to /v1/inspect through a second pipe, decode inspectResponse as postCADInspect does, and assert the inspect outcome in the behavior block. Close the pipe writer with the builder's error so a builder failure surfaces as a parse error, not a hang.

RED: run the test at the base tree and save the output to $Q/evidence/t1-red.txt; it must fail on both endpoints with status 413. Commit the test alone (test(quick-261018-bmr): ...).

GREEN: in configmap.yaml set the MAX_UPLOAD_BYTES and INSPECT_MAX_UPLOAD_BYTES values to "191889408" (183 MiB, derived in the research; Task 2's test prints the same derivation), touching no other line. In docs/CAD.md change the size table header's MiB number to 183 (TestCADDocMatchesTheCode builds the header from the chart), set the effective cell of every row to its bytes cell, and rewrite the paragraph after the table (its last sentence, from "The Helm chart sets" to the end) to say that the chart's global MAX_UPLOAD_BYTES and INSPECT_MAX_UPLOAD_BYTES apply before these ceilings and sit above every one of them plus the request's multipart framing, so a file at or below its format's ceiling gets the per-format answer from both endpoints; name no number in that prose that the docs test does not hold. Run the test, TestCADDocMatchesTheCode and helm template of the ConfigMap; save the outputs to $Q/evidence/t1-green.txt and $Q/evidence/helm-render.txt. Commit (fix(quick-261018-bmr): ...).
  </action>
  <verify>
    <automated>cd /Users/apaderin/dev/octoconv && S=/private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad && mkdir -p $S/qbmr && PATH=$S/py314:$PATH TMPDIR=$S/qbmr GOTOOLCHAIN=go1.26.4 go test ./internal/api/ -run 'TestChartTransportCapsAdmitTheLargestCeilingFile' -count=1 && PATH=$S/py314:$PATH TMPDIR=$S/qbmr GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ -run 'TestCADDocMatchesTheCode' -count=1 && helm template octoconv deploy/chart/octoconv --set secrets.databaseUrl=x --set secrets.apiKeySalt=x --set secrets.webhookSigningSecret=x --set secrets.s3AccessKey=x --set secrets.s3SecretKey=x --set secrets.postgresPassword=x --show-only templates/configmap.yaml > $S/qbmr/cm.yaml && grep -qE '^  MAX_UPLOAD_BYTES: "191889408"$' $S/qbmr/cm.yaml && grep -qE '^  INSPECT_MAX_UPLOAD_BYTES: "191889408"$' $S/qbmr/cm.yaml && test "$(grep -c 'refused at the transport with the generic message' docs/CAD.md)" = 0 && test "$(grep -c '^[[:space:]]*#' deploy/chart/octoconv/templates/configmap.yaml)" -le 174 && echo T1-OK</automated>
  </verify>
  <done>t1-red.txt shows both endpoints at 413 on the base tree; the test passes after the ConfigMap change; the rendered ConfigMap carries both keys at "191889408"; TestCADDocMatchesTheCode passes; two signed commits (RED test, GREEN fix); docs/CAD.md no longer describes a transport refusal below the ceilings; configmap.yaml's comment-line count did not grow.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Every shipped configuration covers every advertised ceiling plus the envelope (RED, GREEN, mutations), and the keda premise moves with the chart</name>
  <files>internal/api/transport_cap_test.go, docker-compose.yml, .env.example, deploy/chart/octoconv/values-inspect-gate.yaml, scripts/keda-cad-downscale-survival.sh</files>
  <read_first>
    - internal/api/transport_cap_test.go (Task 1)
    - internal/e2e/minio_image_pin_test.go 90-135 (go.yaml.in/yaml/v3 in tests)
    - .env.example 15-35 and 580-625
    - deploy/chart/octoconv/values-inspect-gate.yaml 25-62
    - scripts/keda-cad-downscale-survival.sh 25-35, 360-400, 425-460
  </read_first>
  <behavior>
    - RED after Task 1: TestDeployedTransportCapsCoverEveryAdvertisedCeiling fails naming the docker-compose api service, .env.example and the values-inspect-gate.yaml overlay, each for INSPECT_MAX_UPLOAD_BYTES against the cad iges, stl and dxf ceilings and the 100 MiB engine ceilings, and it does not name the chart default
    - GREEN: the test passes and logs the largest requirement (190845770 bytes) and its next whole MiB (191889408)
    - M1 configmap MAX_UPLOAD_BYTES back to the base value: the test names the chart default and every overlay; the Task 1 tracer fails with 413
    - M2 docker-compose api INSPECT_MAX_UPLOAD_BYTES back to the base value: the test names compose
    - M3 the av exemption removed: the test names av in the chart, compose and .env.example
    - M4 a second exemption for tectonic added: the test fails saying the tectonic exemption is not needed
    - M5 values-inspect-gate.yaml INSPECT_MAX_UPLOAD_BYTES back to the base value: the test names that overlay
    - M6 the chart's INSPECT_MAX_UPLOAD_BYTES set above its MAX_UPLOAD_BYTES: the test names INSPECT above MAX in the chart
  </behavior>
  <action>
Extend internal/api/transport_cap_test.go (no comments).

Configurations. A helper returns one record per shipped configuration with a name, MAX_UPLOAD_BYTES, INSPECT_MAX_UPLOAD_BYTES and PANDOC_MAX_INPUT_BYTES (zero when unset). The chart: the ConfigMap literals from Task 1 (PANDOC_MAX_INPUT_BYTES read the same way) are the defaults; values.yaml and every file filepath.Glob finds for ../../deploy/chart/octoconv/values-*.yaml are decoded with go.yaml.in/yaml/v3, and any api.extraEnv entry for one of the three keys overrides the default (Helm deep-merges an overlay's api.extraEnv over values.yaml's, and extraEnv beats envFrom). Emit "chart" plus one "chart -f <file>" per overlay. Because the test checks the default and every override value of every file, any combination of overlays resolves each key to a checked value. The compose record comes from services.api.environment of ../../docker-compose.yml (MAX and INSPECT required, fail closed if absent). The .env.example record comes from anchored line regexps of the form KEY=digits followed by whitespace or end of line (the firstField convention), both required. Every value must parse as a positive int64. Anti-vacuity: at least one overlay and at least four records.

Ceilings. For each record build a Server with NewServer and the fake dependencies, Config{MaxUploadBytes, InspectMaxUploadBytes, MaxPandocInputBytes from the record}; walk convert.Default.OperationClasses() and, for every operation, engine and pair, read limit and gated from srv.sizeLimit(engine, pair.From). An advertised pair that is not gated fails the test (the transport cap would be its only bound). Anti-vacuity: at least seven cad entries.

Requirement. For each entry, envelope = the byte count writeTransportCapRequest writes for that operation, engine and pair.To with an empty file and the maximal filename (count through an io.Writer that only counts); need = limit + envelope. For every record and entry whose engine is not exempt, MAX_UPLOAD_BYTES and INSPECT_MAX_UPLOAD_BYTES must each be at least need; a failure names the record, the key, operation, engine, pair, limit, envelope, need, and the next whole MiB above the largest need, so the person editing a configuration reads the number from the test. Log that largest need and its next whole MiB. Separately: in every record INSPECT_MAX_UPLOAD_BYTES is at most MAX_UPLOAD_BYTES, and across the chart records the largest INSPECT value is at most the smallest MAX value.

Exemption (D1). A map from engine to a reason string holds exactly convert.EngineAV with the reason that av's 2 GiB ceiling is kept out of the API pod's pre-detection spill in the chart and out of POST /v1/inspect everywhere (Phase 58 D-06), decision D1 of quick 261018-bmr. The test fails if an exempt engine advertises no pair, or if no record violates the requirement for it (the exemption is no longer needed). Tie to the tracer: the largest non-exempt limit over every record must be convert.CADMaxSTLBytes on the cad render stl entry; otherwise the test fails saying the tracer no longer posts the worst case.

RED: run TestDeployedTransportCapsCoverEveryAdvertisedCeiling, save to $Q/evidence/t2-red.txt (expected failures in the behavior block), commit the test alone.

GREEN, each value "191889408" (191889408 in .env.example): docker-compose.yml api INSPECT_MAX_UPLOAD_BYTES (MAX_UPLOAD_BYTES stays 2 GiB); .env.example INSPECT_MAX_UPLOAD_BYTES with its inline comment's leading MiB number changed to 183 and nothing else in that line; values-inspect-gate.yaml INSPECT_MAX_UPLOAD_BYTES, with the MiB number in the comment directly above it changed to 183 so "UNCHANGED from the ConfigMap's own ... default" stays true. In .env.example's CAD comment block, the two lines starting "# In the Helm chart the global MAX_UPLOAD_BYTES and INSPECT_MAX_UPLOAD_BYTES" become two lines saying that every deployment's global MAX_UPLOAD_BYTES and INSPECT_MAX_UPLOAD_BYTES are above these ceilings plus the multipart framing, so the ceilings apply as stated; in the DXF paragraph, the sentence starting "In the Helm chart MAX_UPLOAD_BYTES" is deleted, keeping "# cap is 134 MiB and the PDF cap 19 MiB." as its line (comment lines may only shrink). In scripts/keda-cad-downscale-survival.sh set TRANSPORT_CAP to 191889408 and nothing else.

Replay the script's long_select offline at both values, from the script's own text so the replay runs the program that ships: write lines 427-431 of the edited script (the CELLSPEC block selector) and lines 435-453 (the candidate program, after the two `awk -F, -v cap=...` invocation lines and before `' "$INWORKER" "$W/cellspec" > "$W/candidates"`) to two files under $S/qbmr; run the first over .planning/milestones/v1.16-phases/76-measured-bounds-keda-proof-client-docs/evidence/76-03-cells.txt, then the second with `-F,` and -v cap at each value, -v lo=180, -v hi=590 and -v c_step, c_iges, c_stl, c_dxf from the DERIVED lines cad_<format>_ceiling_bytes of 76-03-derived.txt, over 76-03-inworker.txt and the selected block; pick as the script does (status eligible, sort by field 12 numerically, first). Save both candidate tables, their diff and both picks to $Q/evidence/long-select-replay.txt. Stop and report if the tables differ or the pick is not render_step M2 at 194.90 s.

Mutations M1 to M6 from the behavior block, one at a time, each reverted before the next (`git diff --quiet` on the file afterwards); save their outputs to $Q/evidence/t2-mutations.txt. Run `bash scripts/ci-helm-checks.sh` and the regression set from the context; save to $Q/evidence/t2-green.txt. Commit (fix(quick-261018-bmr): ...).
  </action>
  <verify>
    <automated>cd /Users/apaderin/dev/octoconv && S=/private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad && PATH=$S/py314:$PATH TMPDIR=$S/qbmr GOTOOLCHAIN=go1.26.4 go test ./internal/api/ -run 'TestDeployedTransportCapsCoverEveryAdvertisedCeiling|TestChartTransportCapsAdmitTheLargestCeilingFile' -count=1 -v > $S/qbmr/t2-verify.log 2>&1 && grep -q '191889408' $S/qbmr/t2-verify.log && bash -n scripts/keda-cad-downscale-survival.sh && test "$(shellcheck -S warning scripts/keda-cad-downscale-survival.sh | grep -c '^In ')" = 0 && bash scripts/ci-helm-checks.sh >/dev/null && test "$(grep -c '^[[:space:]]*#' .env.example)" -le 571 && test "$(grep -c '^[[:space:]]*#' deploy/chart/octoconv/values-inspect-gate.yaml)" -le 120 && test "$(grep -c 'apply first, so the effective' .env.example)" = 0 && test "$(grep -c 'the effective DXF ceiling there is' .env.example)" = 0 && echo T2-OK</automated>
  </verify>
  <done>t2-red.txt shows compose, .env.example and the inspect-gate overlay failing and the chart passing; the test passes and logs 191889408; M1-M6 each fail as stated and are reverted; the replay shows identical candidate tables and the same LONG; ci-helm-checks passes; the regression set passes; comment-line counts did not grow; two signed commits.</done>
</task>

<task type="auto">
  <name>Task 3: Client docs, stale fixture texts and the planning bookkeeping</name>
  <files>docs/ERROR-CODES.md, docs/openapi.yaml, scripts/fixtures/README.md, scripts/fixtures/gen_inspect_burst_pdf.py, .planning/todos/pending/2026-10-18-helm-upload-cap-below-cad-ceilings.md, .planning/todos/completed/2026-10-18-helm-upload-cap-below-cad-ceilings.md, .planning/todos/pending/2026-10-18-api-multipart-spill-unbounded-disk.md, .planning/STATE.md, .planning/quick/261018-bmr-helm-upload-cap-at-least-the-measured-ca/261018-bmr-SUMMARY.md</files>
  <read_first>
    - docs/ERROR-CODES.md line 120 (the CAD paragraph, held in part by TestErrorCodesDocStatesTheCADCeilingsOfTheCode)
    - docs/openapi.yaml 1260-1272
    - scripts/fixtures/README.md 34-62
    - scripts/fixtures/gen_inspect_burst_pdf.py 52-62
    - .planning/todos/completed/2026-09-30-minio-images-off-docker-hub.md (frontmatter convention)
    - .planning/STATE.md 237-335 (Quick Tasks Completed, Pending Todos)
  </read_first>
  <action>
docs/ERROR-CODES.md, the CAD paragraph: replace its last sentence (from "In the Helm chart the global" to the end of the paragraph) with one sentence saying that every shipped deployment (the Helm chart, docker-compose.yml, .env.example) sets the global MAX_UPLOAD_BYTES and INSPECT_MAX_UPLOAD_BYTES above the largest of these ceilings plus the request's multipart framing, so a CAD file at or below its ceiling gets the per-format answer, never the generic transport 413. Keep every other part of the paragraph byte-identical (TestErrorCodesDocStatesTheCADCeilingsOfTheCode reads it).

docs/openapi.yaml, the /v1/inspect 413 description: the clause comparing INSPECT_MAX_UPLOAD_BYTES with POST /v1/jobs' own cap claims the inspect cap is strictly the smaller one, which is false in the chart now that both are 183 MiB; reword that clause to "separate from, and never larger than, POST /v1/jobs' own MAX_UPLOAD_BYTES" (Task 2's test holds INSPECT at most MAX); the rest unchanged.

scripts/fixtures/README.md: the gate section states the upload ceiling the overlay restates. Change its MiB and byte numbers to 183 MiB and 191,889,408 (the heading's range, the sentence naming INSPECT_MAX_UPLOAD_BYTES's value in .env.example and the ConfigMap, the "at its ... default" clause, the ceiling table row and the fixture table's column header), so no comma-grouped form of the old value remains; the fixture rows' "yes" answers stay true. scripts/fixtures/gen_inspect_burst_pdf.py: in the comment above TARGET_TOTAL_BYTES change the MiB number in the parenthesis naming the INSPECT_MAX_UPLOAD_BYTES default to 183; no other byte of the file changes.

Todo: `git mv` .planning/todos/pending/2026-10-18-helm-upload-cap-below-cad-ceilings.md to .planning/todos/completed/, and add to its frontmatter `completed: 2026-10-18` and a `resolution:` line naming quick task 261018-bmr, the 191889408 caps in the chart (MAX and INSPECT) and in compose, .env.example and values-inspect-gate (INSPECT), the test that holds every shipped configuration to every advertised ceiling plus the envelope, and the av exemption (D1).

New pending todo .planning/todos/pending/2026-10-18-api-multipart-spill-unbounded-disk.md in the shape of the closed one (frontmatter created 2026-10-18, area deploy, severity medium, origin quick task 261018-bmr research; sections What, Idea, Constraints): the API spills every multipart file part beyond 32 MiB to /tmp in the container's writable layer; the chart declares no emptyDir, sizeLimit or ephemeral-storage for any pod and compose no tmpfs; per api pod the spill is bounded by concurrent creates times MAX_UPLOAD_BYTES (only the rate limits bound the count) plus 12 times INSPECT_MAX_UPLOAD_BYTES, and a burst fills the node's disk, where DiskPressure evicts other pods first among those without requests; 261018-bmr scaled the chart's bound 1.83x. Idea: an emptyDir with a sizeLimit mounted at a TMPDIR the api reads, with an ephemeral-storage request and limit derived from the caps and the intake concurrency, or a create-path intake semaphore like inspect's.

STATE.md: add the Quick Tasks Completed row for 261018-bmr (date 2026-10-18, the hash of Task 2's GREEN commit, the directory link) with a one-line description of what changed and what stayed open (av exempt, Go fallbacks unchanged). In Pending Todos, keep the count at nine: replace the bullet about the Helm chart's upload cap with a bullet for the new spill todo, and add a paragraph in the style of the existing "снят потому, что он сделан" paragraphs naming the closed todo's path under todos/completed/. Verify the bullets name exactly the files in .planning/todos/pending/.

Run the doc regression tests (./internal/errcode/, ./internal/inspect/, ./internal/api/, and in ./internal/convert/ TestErrorCodesDocStatesTheCADCeilingsOfTheCode, TestCADOpenAPIMatchesTheCode, TestCADDocMatchesTheCode, and in ./internal/worker/ TestCADCodesDocMatchesTheMapping), save to $Q/evidence/t3-docs.txt, write the SUMMARY (record D1-D3 as implemented, the av residual including the 4938-byte boundary in compose, the unbounded callback_url observation, and every RED/GREEN/mutation/replay evidence path), commit (docs(quick-261018-bmr): ...).
  </action>
  <verify>
    <automated>cd /Users/apaderin/dev/octoconv && S=/private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad && PATH=$S/py314:$PATH TMPDIR=$S/qbmr GOTOOLCHAIN=go1.26.4 go test ./internal/errcode/ ./internal/inspect/ -count=1 && PATH=$S/py314:$PATH TMPDIR=$S/qbmr GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ -run 'TestErrorCodesDocStatesTheCADCeilingsOfTheCode|TestCADOpenAPIMatchesTheCode|TestCADDocMatchesTheCode' -count=1 && PATH=$S/py314:$PATH TMPDIR=$S/qbmr GOTOOLCHAIN=go1.26.4 go test ./internal/worker/ -run 'TestCADCodesDocMatchesTheMapping' -count=1 && test "$(grep -c 'the effective ceiling there is the smaller of the two' docs/ERROR-CODES.md)" = 0 && test "$(grep -c 'separate from and smaller than' docs/openapi.yaml)" = 0 && test "$(grep -c '104,857,600' scripts/fixtures/README.md)" = 0 && test "$(grep -c '^[[:space:]]*#' scripts/fixtures/gen_inspect_burst_pdf.py)" -le 21 && test -f .planning/todos/completed/2026-10-18-helm-upload-cap-below-cad-ceilings.md && test ! -e .planning/todos/pending/2026-10-18-helm-upload-cap-below-cad-ceilings.md && test -f .planning/todos/pending/2026-10-18-api-multipart-spill-unbounded-disk.md && grep -q '^| 261018-bmr |' .planning/STATE.md && test "$(ls .planning/todos/pending/*.md | wc -l | tr -d ' ')" = 9 && echo T3-OK</automated>
  </verify>
  <done>No shipped text describes the old chart cap or a strictly smaller inspect cap; the doc tests pass; the todo is in completed/ with completed and resolution lines; the spill todo exists; STATE.md has the row and nine pending bullets matching the directory; one signed commit; SUMMARY written.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| client -> API transport | an authenticated internal client's multipart body is read and spilled before the engine is known |
| API process -> api container disk | ParseMultipartForm spills file parts beyond 32 MiB to /tmp with no disk bound |
| shipped configuration -> running API | chart, compose and .env.example values decide which ceiling a client can reach |

## STRIDE Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation Plan |
|-----------|----------|-----------|----------|-------------|-----------------|
| T-bmr-01 | Denial of Service | api pod spill disk (chart) | medium | accept | the bound rises from 100 MiB to 183 MiB per request (1.83x); heap per request stays 32 MiB; clients are authenticated internal services behind RATE_LIMIT_IP_RPM and RATE_LIMIT_CLIENT_RPM; compose already accepts 2 GiB with the same mechanism; the missing disk bound predates this change and is filed as todo 2026-10-18-api-multipart-spill-unbounded-disk.md (D3) |
| T-bmr-02 | Denial of Service | api heap during intake | high | mitigate | multipartInMemoryBudget, inspectIntakeMaxConcurrent and inspectIntakeReadDeadline are not touched: `git diff main -- internal/api/api.go internal/api/handlers.go internal/api/inspect_handlers.go` is empty at the end; the inspect heap bound stays 8 x 32 MiB of the 1Gi limit |
| T-bmr-03 | Tampering | configuration drift between caps and ceilings | medium | mitigate | TestDeployedTransportCapsCoverEveryAdvertisedCeiling reads every shipped configuration and every advertised ceiling through sizeLimit; M1, M2, M5, M6 prove it names each configuration; M3, M4 prove the av exemption is exact |
| T-bmr-04 | Elevation of Privilege | av admission in k8s | low | accept | av's effective k8s ceiling becomes 183 MiB, inside its own 2 GiB contract; the av worker bounds duration (AV_MAX_DURATION_SECONDS) and checks free disk (AV_DISK_SAFETY_FACTOR) before ffmpeg (D1) |
| T-bmr-05 | Denial of Service | /v1/inspect body deadline | low | accept | a 183 MiB body must arrive within the unchanged 30 s read deadline (at least 6.1 MiB/s); clients are in-cluster services |
| T-bmr-SC | Tampering | npm/pip/cargo installs | high | accept | no package is installed; go.yaml.in/yaml/v3 is already a direct requirement in go.mod |
</threat_model>

<verification>
T1-OK, T2-OK and T3-OK print. `GOTOOLCHAIN=go1.26.4 go vet ./...` and `gofmt -l internal/` are clean. `git diff --quiet main -- internal/convert/exec.go internal/api/api.go internal/api/handlers.go internal/api/inspect_handlers.go internal/convert/cad.go cmd/api/main.go` exits 0. `git log --format='%G?' $(cat .planning/quick/261018-bmr-helm-upload-cap-at-least-the-measured-ca/evidence/base.sha)..HEAD` is all G and `git log --format=%B` of the same range has no Co-Authored-By line. `git status --porcelain` lists only `.planning/config.json`, `.vscode/` and `octoconv.code-workspace`.
</verification>

<success_criteria>
In the chart, compose and .env.example a CAD file at or below its format's ceiling reaches the per-format check on both POST /v1/jobs and POST /v1/inspect, proven end to end for the largest one at the chart's own caps and held for every advertised ceiling of every shipped configuration by a test that was RED first; no ceiling, heap bound or Go default changed; av's exception is explicit and exact; every text and script premise that described the old cap is true again; the todo is closed and the follow-up is filed.
</success_criteria>

<output>
Create `.planning/quick/261018-bmr-helm-upload-cap-at-least-the-measured-ca/261018-bmr-SUMMARY.md` when done.
</output>
