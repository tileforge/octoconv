---
phase: quick-261018-bmr
plan: 01
subsystem: api-transport-caps-and-deploy-config
tags: [v1.16-audit, td-1, helm, configmap, upload-cap, inspect, cad-ceilings, docker-compose]
status: complete
requires: [convert.CADMaxSTLBytes and the per-format CAD ceilings (cad.go), Server.sizeLimit, jobs.SafeInputFilename and jobs.MaxInputParts, maxOptsBytes]
provides: [TestChartTransportCapsAdmitTheLargestCeilingFile, TestDeployedTransportCapsCoverEveryAdvertisedCeiling, chart MAX_UPLOAD_BYTES and INSPECT_MAX_UPLOAD_BYTES at 191889408, INSPECT_MAX_UPLOAD_BYTES 191889408 in compose, .env.example and values-inspect-gate.yaml, two pending todos]
affects: [deploy/chart/octoconv/templates/configmap.yaml, docker-compose.yml, .env.example, deploy/chart/octoconv/values-inspect-gate.yaml, scripts/keda-cad-downscale-survival.sh, docs/CAD.md, docs/ERROR-CODES.md, docs/openapi.yaml, scripts/fixtures]
tech-stack:
  added: []
  patterns: [a transport cap derived from the code's own ceilings plus a measured request envelope, one request builder shared by the end-to-end tracer and the arithmetic test, a two-way exemption that fails when it stops being needed]
key-files:
  created:
    - internal/api/transport_cap_test.go
    - .planning/todos/pending/2026-10-18-api-multipart-spill-unbounded-disk.md
    - .planning/todos/pending/2026-10-18-callback-url-no-length-bound.md
    - .planning/quick/261018-bmr-helm-upload-cap-at-least-the-measured-ca/evidence/
  modified:
    - deploy/chart/octoconv/templates/configmap.yaml
    - docker-compose.yml
    - .env.example
    - deploy/chart/octoconv/values-inspect-gate.yaml
    - scripts/keda-cad-downscale-survival.sh
    - docs/CAD.md
    - docs/ERROR-CODES.md
    - docs/openapi.yaml
    - scripts/fixtures/README.md
    - scripts/fixtures/gen_inspect_burst_pdf.py
    - .planning/todos/completed/2026-10-18-helm-upload-cap-below-cad-ceilings.md (moved from pending/)
    - .planning/STATE.md
decisions:
  - "D1 (a): av exempt exactly in the test; av's effective k8s ceiling rises from 100 MiB to 183 MiB as a side effect, because the transport cap is read before the engine is known; the chart's 100 MiB was drift from D-07, not an av decision"
  - "D2 (a): the Go fallbacks (NewServer InspectMaxUploadBytes 100 MiB, cmd/api MAX_UPLOAD_BYTES 2 GiB) unchanged; every shipped configuration sets both keys"
  - "D3: pending todo filed for the api pod's unbounded multipart spill disk; a second pending todo filed (orchestrator) for callback_url having no length bound"
  - "The two chart caps stay literals in configmap.yaml, not chart values"
metrics:
  completed: 2026-10-18
  duration: "05:51 to 06:20 UTC"
actuals:
  tokens: 13000
  tasks: 3
  commits: 4
plan_head_before: e3212610e136406d8aaa0529863920c90d3d26c2
plan_head_after: f4b33930392748917071ce69cf410324d8a5b672
requirements-completed: [quick-261018-bmr]
---

# Quick 261018-bmr: the Helm upload cap covers every measured CAD ceiling, Summary

The chart's `MAX_UPLOAD_BYTES` and `INSPECT_MAX_UPLOAD_BYTES`, and the `INSPECT_MAX_UPLOAD_BYTES` of
docker-compose, `.env.example` and the inspect-gate overlay, are now 191889408 (183 MiB). That is
the next whole MiB above the STL ceiling (190840832) plus the 4938-byte envelope of the largest
request the API bounds. A tracer streams the largest admissible STL through both endpoints at the
chart's own caps, and a second test holds every shipped configuration to every advertised ceiling.
Both tests were RED first.

`commits: 4` counts the commits up to `plan_head_after`. The Task 3 commit that carries this
SUMMARY is the fifth.

## Commits

| Task | Commit | What |
|---|---|---|
| 1 RED | 7a9dc2d4 | `test`: tracer `TestChartTransportCapsAdmitTheLargestCeilingFile` and the shared `writeTransportCapRequest` |
| 1 GREEN | 8154b6ed | `fix`: ConfigMap caps to 191889408; docs/CAD.md table under the 183 MiB cap |
| 2 RED | 679f3ab8 | `test`: `TestDeployedTransportCapsCoverEveryAdvertisedCeiling` |
| 2 GREEN | f4b33930 | `fix`: compose, .env.example and inspect-gate inspect caps; keda `TRANSPORT_CAP`; .env.example CAD comments |
| 3 | (this commit) | `docs`: ERROR-CODES, openapi, fixtures texts, todo bookkeeping, STATE, SUMMARY |

All commits are SSH-signed (`%G?` = G). None has a Co-Authored-By trailer.

## RED and GREEN

- **Task 1 RED** (`evidence/t1-red.txt`, base e321261): create got 413 `{"code":"payload_too_large","error":"file exceeds size limit"}`, and inspect got the same 413.
- **Task 1 GREEN** (`evidence/t1-green.txt`): create returns 202, the job is on the cad queue and the stub counted 190840784 uploaded bytes in one upload. Inspect returns 200 with the cad render png offer `Available` and `MaxBytes` 190840832. `TestCADDocMatchesTheCode` passes, all 9 subtests. The rendered ConfigMap carries both keys at "191889408" (`evidence/helm-render.txt`, helm v4.3.0, offline `helm template`).
- **Task 2 RED** (`evidence/t2-red.txt`): fails on exactly `docker-compose.yml api`, `.env.example` and `chart -f values-inspect-gate.yaml`, 14 groups each, all on `INSPECT_MAX_UPLOAD_BYTES`. The groups cover the cad iges, stl and dxf ceilings and the 100 MiB image, document, html, audio and ocr ceilings. The `chart` record passes.
- **Task 2 GREEN** (`evidence/t2-green.txt`): passes. It logs `largest requirement 190845770 bytes (render cad stl->png ceiling 190840832 plus envelope 4938); next whole MiB 191889408 (183 MiB)`.

## Mutations (`evidence/t2-mutations.txt`; each reverted, and each file cmp-identical to its pre-mutation copy afterwards)

| # | Mutation | Result |
|---|---|---|
| M1 | configmap `MAX_UPLOAD_BYTES` back to 104857600 | FAIL: names `chart` and all six `chart -f` overlays on MAX, plus INSPECT above MAX. The tracer also fails: create 413 |
| M2 | compose api `INSPECT_MAX_UPLOAD_BYTES` back | FAIL: names `docker-compose.yml api` only (14 groups) |
| M3 | av exemption removed | FAIL: names av (3 groups per key) in every chart record, compose and .env.example, plus the tracer-tie error (largest ceiling 2 GiB on av) |
| M4 | tectonic exemption added | FAIL: `the tectonic exemption is not needed` |
| M5 | values-inspect-gate `INSPECT_MAX_UPLOAD_BYTES` back | FAIL: names `chart -f values-inspect-gate.yaml` only |
| M6 | chart `INSPECT_MAX_UPLOAD_BYTES` 192937984 (above MAX) | FAIL: INSPECT above MAX in `chart` and in five overlays, plus the cross-chart check. The inspect-gate overlay is not named because it overrides INSPECT to 191889408 |

## long_select replay (`evidence/long-select-replay.txt`)

I replayed the selector and the candidate program from the edited script's own text, at 104857600
and at 191889408, over the committed 76-03 evidence (band 180..590, ceilings from the DERIVED lines).
At both values I get the same four candidates and two eligible, and the candidate and pick diffs are
empty. The pick is `render_step|M2|...|1474086|...|194.90|eligible` both times.

## Final gates (`evidence/final-gates.txt`, go1.26.4)

- `gofmt -l .`: empty. `go vet ./...`: clean.
- `git diff --quiet main -- internal/convert/exec.go internal/api/api.go internal/api/handlers.go internal/api/inspect_handlers.go internal/convert/cad.go cmd/api/main.go` exits 0, and `exec.go` is cmp-identical to main.
- Full `go test ./... -count=1` exits 0: 32 packages ok, 17 without test files, 0 FAIL.
- `bash scripts/ci-helm-checks.sh` exit 0. `bash -n` and `shellcheck -S warning` on the keda script: 0 findings.
- Doc tests (`evidence/t3-docs.txt`): errcode, inspect and api pass, as do `TestErrorCodesDocStatesTheCADCeilingsOfTheCode`, `TestCADOpenAPIMatchesTheCode`, `TestCADDocMatchesTheCode` and `TestCADCodesDocMatchesTheMapping`.
- Comment-line counts: configmap 174, compose 1001, values-inspect-gate 120, keda 1 and gen_inspect_burst_pdf.py 21 are all unchanged. `.env.example` went from 571 to 570 (the DXF sentence was deleted). The test file has no comments.

## Residuals, recorded and not fixed

- **av (D1).** av's effective k8s ceiling rises from 100 MiB to 183 MiB. The transport cap is read before detection, so no knob can hold av alone. 183 MiB is inside av's own 2 GiB contract, and the av worker still bounds the work (`AV_MAX_DURATION_SECONDS`, `AV_DISK_SAFETY_FACTOR`). The chart's 100 MiB was scaffolded in phase 24 (8c7a97b), before D-07 raised compose and `.env.example` to 2 GiB in phase 35 (f324d33), and phase 37 never touched the key. It was drift, not an av decision.
- **av boundary in compose and .env.example.** `MAX_UPLOAD_BYTES` there equals av's 2 GiB ceiling exactly. An av file in the last 4938 bytes below 2 GiB plus the envelope exceeds the cap and gets the generic 413, not the per-format answer. The test's exemption covers this. It is recorded here and not fixed.
- **Fields the API does not bound.** 191889408 leaves 1043638 bytes above the largest requirement. `callback_url` has no length check, and the document's own filename does not go through `SafeInputFilename`, so either can push the largest STL into the generic 413. Filed as `.planning/todos/pending/2026-10-18-callback-url-no-length-bound.md`.
- **Spill disk (D3).** The api pod's multipart spill has no disk bound in the chart or in compose, and this change scales the chart's bound 1.83x. Filed as `.planning/todos/pending/2026-10-18-api-multipart-spill-unbounded-disk.md`.
- **Go fallbacks (D2).** These are unchanged: `NewServer` InspectMaxUploadBytes 100 MiB and cmd/api MAX_UPLOAD_BYTES 2 GiB. The doc comments in `internal/api/api.go`, `cmd/api/main.go` and `internal/api/inspect_handlers.go:230` describe the 100 MiB fallback, and those files must match main.

## Deviations from Plan

1. **[Rule 3] Builder signature.** `writeTransportCapRequest` takes a `*multipart.Writer`, not an `io.Writer`, and returns only the write error. The tracer streams through an `io.Pipe`, so the content type has to be known before the goroutine finishes writing. The caller reads it from `mw.FormDataContentType()` instead of from a return value. The bytes written are the same.
2. **[Rule 1] Replay line range.** The plan gives the selector as lines 427-431, but line 431 is the closing `' "$CELLS" > ...` line. The first replay copied it, the awk selector failed with a syntax error, and the evidence showed empty candidate tables with `diff exit=0`. That was a false green, and I caught it before saving. The replay now uses 427-430, and the evidence header records why.
3. **[Rule 1] Tracer-tie message.** In the first mutation run, M3 printed about 200 lines, each claiming an av pair "shares the largest ceiling with cad render stl->png". That claim was wrong. The check now collects every non-exempt entry at the largest ceiling and fails once unless that set is exactly `render cad stl->png` at `convert.CADMaxSTLBytes`. The change was made before the Task 2 GREEN commit and is part of it. The RED output in `t2-red.txt` comes from the earlier version, whose requirement checks are identical.
4. **Ten pending todos, not nine.** The orchestrator asked for a second pending todo, about `callback_url`, so STATE says "Ровно десять" and lists ten bullets, which match `.planning/todos/pending/`. The plan's T3 automated check `ls .planning/todos/pending/*.md | wc -l = 9` therefore gives 10. That is intended, not a pass of the original check.
5. **Wording in scripts/fixtures/README.md.** The sentence that named the value now says "the shipped `INSPECT_MAX_UPLOAD_BYTES` is 183 MiB" rather than attributing 183 MiB to `api.go`'s DEFAULT. `api.go`'s fallback stays 100 MiB (D2), so the original wording would have become false.

## Known Stubs

None.

## Threat Flags

None. No new endpoint or auth path. The spill-disk exposure (T-bmr-01) and the callback_url observation are filed as todos.

## Self-Check: PASSED

- Commits 7a9dc2d4, 8154b6ed, 679f3ab8 and f4b33930 are ancestors of HEAD, all with `%G?` = G.
- `internal/api/transport_cap_test.go`, both new pending todos, the completed todo and all ten evidence files exist.
- The plan's T1 and T2 checks print T1-OK and T2-OK. The T3 check prints T3-OK with its pending-count clause set to 10 instead of 9 (deviation 4).
