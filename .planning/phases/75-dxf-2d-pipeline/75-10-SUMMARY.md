---
phase: 75-dxf-2d-pipeline
plan: 10
subsystem: cad live proof (DXF)
tags: [live-proof, dxf, svg, pdf, render, measurements, bombs, retry, cancel, compose]
status: complete

requires:
  - phase: 75-dxf-2d-pipeline
    provides: 75-09 exposure (bd90a59) and its tests; 75-08 measured rulesets, the R-11 drawing judge and the preflight; 75-04/75-05 cad-svg-check, cad-pdf-check and cad-pixel-judge --drawing
provides:
  - "scripts/cad-liveproof.sh: FORMATS with the eight cad pairs, INSPECT with the dxf offers, SANDBOX, DXF_SVG, DXF_PDF, DXF_RENDER, DXF_MEAS, DXF_OPTS_REJECT, DXF_BOMB, DXF_CEILING, DXF_RETRY and DXF_CANCEL; HDR adds cad_dxf_timeout_s and cad_rsvg_timeout_s"
  - "evidence/75-10-liveproof.txt: one run on a compose stack built from b2c0db8, tracked_dirt=0, SUMMARY,result=ALL PASS with every phase-74 section"
  - ".planning/ci-local/ci-local-20261012T040858Z.log: helm, gate and race on b2c0db8, ALL REQUESTED TIERS PASSED"
affects: [75-verification, 76]

tech-stack:
  added: []
  patterns:
    - "A live proof that refuses tracked dirt runs from a fresh local git clone at the commit, not by checking out someone's uncommitted change"
    - "Downloaded outputs are judged by the worker's own Go code (cad-svg-check, cad-pdf-check, cad-pixel-judge --drawing, cad-measurements-check) plus an independent python XML walk and poppler"

key-files:
  created:
    - .planning/phases/75-dxf-2d-pipeline/evidence/75-10-liveproof.txt
    - .planning/ci-local/ci-local-20261012T040858Z.log
  modified:
    - scripts/cad-liveproof.sh
    - .planning/phases/75-dxf-2d-pipeline/deferred-items.md

key-decisions:
  - "75-10: tracked dirt 0 was reached by running from a fresh `git clone` of the branch at b2c0db8 inside the scratchpad, not by checking out .planning/config.json; the script's git-dir guard rejects a worktree, a clone passes it, and the six services' compose config hashed identical in both trees"
  - "75-10: DXF_RENDER judges each PNG against the letterbox of the SVG from a convert job of the same file, so the letterbox comes from the served drawing, not from the render"
  - "75-10: log-format-postgres-1 no longer exists on this machine; it was neither stopped, started nor recreated (user correction to the plan)"

requirements-completed: [RNDR-06, CCONV-02, CCONV-03]

coverage:
  - id: D1
    description: "dxf to svg: model space, visible layers only, Деталь from cp1251, the special string exact, no script/foreignObject/style/use/image, no on* or href, no DOCTYPE, image/svg+xml, re-serialization byte-identical (SC1, CCONV-02)"
    requirement: CCONV-02
    verification:
      - kind: e2e
        ref: "evidence/75-10-liveproof.txt DXF_SVG x5 (legacy_cp1251, text_cases, layers_off_frozen, never_resolved, uncommon), result=ok"
        status: pass
    human_judgment: false
  - id: D2
    description: "dxf to pdf: %PDF-, one page, cad-pdf-check structure ok, pdftotext -raw holds Деталь and the special string (SC2, CCONV-03)"
    requirement: CCONV-03
    verification:
      - kind: e2e
        ref: "DXF_PDF x3 (legacy_cp1251, text_cases, uncommon), result=ok"
        status: pass
    human_judgment: false
  - id: D3
    description: "render dxf to png letterboxed at the requested size and background, accepted by the Go drawing judge against the SVG's letterbox; view and quality 422; 2D bbox from the entities with $INSUNITS units and assume_unit only for a missing unit (SC3, RNDR-06, MEAS-01, MEAS-03)"
    requirement: RNDR-06
    verification:
      - kind: e2e
        ref: "DXF_RENDER x4 (wide and tall at 1280 opaque, wide at 2048 transparent, legacy_cp1251 at 256), DXF_MEAS x7, DXF_OPTS_REJECT x4"
        status: pass
    human_judgment: false
  - id: D4
    description: "Every DXF bomb class ends failed with a terminal code, no retry, no output, no OOM; above the ceiling is 413; nothing drawable is cad_empty_result (SC4, CADIN-04)"
    requirement: CCONV-02
    verification:
      - kind: e2e
        ref: "DXF_BOMB x15 (7 cad_too_complex, 5 cad_unreadable, 3 cad_empty_result), DXF_CEILING x1"
        status: pass
    human_judgment: false
  - id: D5
    description: "A SIGTERM to cad-dxf or rsvg-convert is retried and the job completes; cancel during cad-dxf leaves no process; the startup log proves cad-dxf and rsvg-convert confined; every phase-74 section still passes (SC5, D-13, CADW-02)"
    requirement: CCONV-03
    verification:
      - kind: e2e
        ref: "DXF_RETRY x2, DXF_CANCEL x1, SANDBOX x1, and the unchanged 74 rows (convert 5, render 5, views 6, meas 10, quality 1, opts_reject 9, reject 2, bomb 12, render_retry, mesh_signal, empty, cancel_render, cancel, worker_kill)"
        status: pass
    human_judgment: false

commits: 2
plan_head_before: 3573820bcb7921ff2a01e294cdaf52cedbbf922d
plan_head_after: b2d5b2e4a6ab02427ea0484dbf8a247c1bbe56a5
actuals:
  tokens: 21900
  tasks: 2
  commits: 2

duration: "2026-10-12T03:38Z to 05:03Z"
completed: 2026-10-12
---

# Phase 75 Plan 10: DXF live proof on the final build Summary

**Phase 75 is observed end to end on a compose stack built from b2c0db8.** dxf to svg, dxf to pdf and render dxf to png are served with outputs judged by the worker's own Go code. Fifteen DXF bombs end terminal with no retry and no OOM. A SIGTERM to cad-dxf or rsvg-convert is retried to done. A cancel during cad-dxf leaves no process. Every phase-74 section still passes. Result: `SUMMARY,result=ALL PASS` with `HDR,tracked_dirt=0`, on the first and only official run.

## Task commits

Both are SSH-signed (G).

1. **Task 1:** `b2c0db8` test(75-10). Adds the DXF sections to scripts/cad-liveproof.sh.
2. **Task 2:** `b2d5b2e` docs(75-10). Adds the evidence, the ci-local transcript and one deferred item.

## Task 1: what the script now checks

The 74 assertions are unchanged except for the FORMATS and INSPECT pair sets. The defaults (usage, default `--out`) are unchanged. The only `#` line is the shebang.

- **FORMATS.** Engines touching cad formats must be exactly dxf>pdf, dxf>svg, iges>stl and step>stl. The operations set must be exactly the eight pairs: convert dxf>pdf, dxf>svg, iges>stl, step>stl; render dxf>png, iges>png, step>png, stl>png. A length check pins the count at 8.
- **INSPECT.** legacy_cp1251.dxf must offer convert svg, convert pdf and render png on cad, each with max_bytes 123731968, estimate_source unmeasured and available. The set of cad offers must be exactly those three.
- **SANDBOX.** The cad-worker log since the run's `up` must contain the landlock line `(convert, render and dxf)` and the proven line naming cad-dxf and rsvg-convert.
- **DXF_SVG.** Each case checks:
  - done, with exactly one primary svg output and no outputs array;
  - the downloaded size equals job_outputs;
  - the presigned response is `image/svg+xml`;
  - cad-svg-check accepts it, and its `--out` re-serialization equals the download byte for byte (`cmp`);
  - a python `xml.etree` walk finds no script, foreignObject, style, use or image element, no attribute starting with `on`, and no href in any namespace. DOCTYPE, ENTITY and a raw `<script` are rejected on the bytes before parsing.
  - Per-case text checks: Деталь (cp1251); the special string exactly equal to one `<text>`; VISIBLE present with HIDDEN_OFF and HIDDEN_FROZEN absent from the bytes; at least 40 texts for uncommon.
- **DXF_PDF.** Each case checks:
  - the body starts with `%PDF-`;
  - the content type is `application/pdf`;
  - pdfinfo reports `Pages: 1`;
  - cad-pdf-check (structure only) accepts it;
  - `pdftotext -raw` contains Деталь, or the special string, or (for uncommon) non-empty text.
- **DXF_RENDER.** Each case checks:
  - `render_finish` passes: 2 rows, primary png at 0, measurements json at 1, and both urls fetched;
  - `cad-pixel-judge --drawing --letterbox-svg` accepts the PNG at the requested size and background, with alpha for transparent; the letterbox comes from the SVG of a convert job of the same file;
  - the measurements body passes cad-measurements-check with format dxf, has two coordinates per corner, and has volume and area null with `not_applicable_2d`.
- **DXF_MEAS.** wrong_extents has bbox 10,20 to 40,30 with mm declared (the header extents are wrong on purpose). units_1 is inch/declared, units_13 micrometre/declared and units_0 unspecified/none. units_0 with assume_unit mm is mm/client_assumed. units_3 with assume_unit mm stays unspecified/none. far_point has max x ≥ 1e5, and its PNG is accepted by the drawing judge (R-4, R-11).
- **DXF_OPTS_REJECT.** Each case returns 422 with no job_id and an unchanged jobs row count:
  - render `view top` and render `quality fine`: opts_not_applicable;
  - render `view sideways`: invalid_options;
  - convert svg `size 1024`: opts_not_applicable.
- **DXF_BOMB.** Each bomb is a convert svg job, against a fresh container baseline. It must end failed with the expected code. Its events must have no `active>active`. It must have 0 output rows and no download_url or outputs. OOMKilled, RestartCount, StartedAt and memory.events oom_kill must be unchanged. The wall time is recorded.
- **DXF_CEILING.** legacy_cp1251.dxf is padded with 999 comment pairs to 123731969 bytes, the same builder as the 75-09 Go test. The response must be 413 payload_too_large with `dxf file exceeds the 118 MiB limit for this conversion`, no job_id and no new jobs row.
- **DXF_RETRY.** The 50 MiB replicate is checked against its 75-02 sha256 `5d14734c…f652`.
  - **cad-dxf case.** SIGTERM to cad-dxf during dxf→svg. The job must end done with `active>active` and one svg output, and the SVG is judged again by cad-svg-check, the re-serialization cmp and the walk.
  - **rsvg-convert case.** SIGTERM to rsvg-convert during dxf→pdf. The job must end done with `active>active` and one pdf output, and the PDF is judged by `%PDF-`, pdfinfo and cad-pdf-check.
- **DXF_CANCEL.** The watcher finds cad-dxf for a 50 MiB svg job, then the job is canceled. The pid must be gone within 5 s, and no cad-dxf may be left. The status must be canceled, with no download_url and no output rows.

## Task 2: the official run

`scripts/cad-liveproof.sh --out .planning/phases/75-dxf-2d-pipeline/evidence/75-10-liveproof.txt` was run once, from a fresh clone of the branch at b2c0db8 (see deviation 2), with python 3.14 first on PATH and GOTOOLCHAIN=go1.26.4. It exited 0.

- **HDR:**
  - commit `b2c0db84654c348959d693fcb40d8c52cca476ee`, `tracked_dirt=0`;
  - api image `sha256:3e5f913a…`, cad-worker image `sha256:fa1ddcfe…` (1,172,325,149 bytes);
  - timeouts: engine 886 s, render 480 s, dxf 162 s, rsvg 141 s;
  - `sandbox_disabled=false`, `server_arch=arm64`, `started_by_run=postgres;redis;minio;api;cad-worker`.
- **Build.** compose re-ran `COPY . .` and both `go build` steps from the b2c0db8 context. The image ids equal those of the build from 3573820, which is expected because b2c0db8 changes only the script.
- **SUMMARY line:** `SUMMARY,result=ALL PASS,formats=1,inspect=9,convert=5,quality=1,render=5,views=6,meas=10,opts_reject=9,reject=2,bomb=12,render_retry=1,mesh_signal=1,empty=1,cancel_render=1,cancel=1,worker_kill=1,image=1,sandbox=1,dxf_svg=5,dxf_pdf=3,dxf_render=4,dxf_meas=7,dxf_opts_reject=4,dxf_bomb=15,dxf_ceiling=1,dxf_retry=2,dxf_cancel=1`
- **Observed values**, all from the evidence file:
  - **DXF_SVG.**
    - legacy_cp1251: 1 text, Деталь.
    - text_cases: 18 texts, special string exact.
    - layers_off_frozen: only VISIBLE.
    - never_resolved: 6 elements, no href and no image element.
    - uncommon: 49 texts, 238 elements.
    - All are `image/svg+xml` with a byte-identical re-serialization.
  - **DXF_PDF.**
    - legacy_cp1251: 6 text runes, Деталь extracted.
    - text_cases: 147 runes, special string extracted.
    - uncommon: 299 runes, 232 paint ops.
    - All are one page.
  - **DXF_RENDER.**
    - wide at 1280: ink box 1123x13 inside letterbox 1203x92.
    - tall at 1280: 13x1123 inside 92x1203.
    - wide at 2048 transparent: alpha=1, frac_transparent 0.9982.
    - legacy_cp1251 at 256: box 225x54 inside 241x70.
    - Every bbox has two coordinates per corner, and volume and area are null.
  - **DXF_MEAS.**
    - wrong_extents: bbox 10:20:40:30.
    - units_1 inch, units_13 micrometre, units_0 unspecified/none.
    - units_0 with assume mm: mm/client_assumed. units_3 with assume mm: unspecified/none.
    - far_point: bbox 0:0:100000:100000, drawing judge ok with 4 ink pixels spanning 1123x1123.
    - No row has a numeric volume.
  - **DXF_BOMB.**
    - cad_too_complex: minsert_over_inserts, entities_over, dimension_minsert, cyclic_self, cyclic_ab, cyclic_case, nest_d17.
    - cad_unreadable: nonnumeric_abc, nonnumeric_nan, nonnumeric_inf, nonnumeric_underscore, coord_1e400.
    - cad_empty_result: 3dsolids, all_hidden, single_point.
    - Every one has events `->queued;queued>active;active>failed`, outputs 0, oom_kill 0 and restart_count 0, and took 2.2–2.4 s (the 2 s poll).
  - **DXF_RETRY.** cad-dxf pid 3209 and rsvg-convert pid 4671 were signalled. Both jobs ended with events `->queued;queued>active;active>active;active>done`, and their outputs were judged ok.
  - **DXF_CANCEL.** cad-dxf pid 4886 was gone 0.3 s after the cancel, and no cad-dxf was left.
  - **74 regression.** Every 74 row is ok:
    - RENDER_RETRY done after the retry;
    - MESH_SIGNAL failed cad_unreadable with no retry;
    - CANCEL_RENDER gone in 0.3 s;
    - WORKER_KILL orphans 0, done in 116.1 s, 3,820,000 triangles.
- **The plan's awk gate** counts 5 svg, 3 pdf, 4 render, 7 meas, 4 opts, 15 bomb, 2 retry, 1 cancel and 1 ceiling rows, with no bad row. `^SUMMARY,result=ALL PASS` and `^HDR,tracked_dirt=0$` match. The evidence holds no FAIL or NOT RUN line.

## Local gates

- **ci-local.** `GOTOOLCHAIN=go1.26.4 scripts/ci-local.sh helm gate race` ran in the same clean clone at b2c0db8, so the transcript has no `TRACKED CHANGES` marker.
  - helm, gate and race PASS, `ALL REQUESTED TIERS PASSED`.
  - Transcript: `.planning/ci-local/ci-local-20261012T040858Z.log`.
  - No tier was cached: the race tier has 0 `(cached)` lines.
  - The transcript carries the standing warning that local helm is v4.3.0 while ci.yml pins v4.2.3.
- **Uncached go test.** `GOTOOLCHAIN=go1.26.4 go test ./... -count=1` in the main tree, with python 3.14 first on PATH: rc 0, 31 packages ok, no FAIL. It ran after the live proof, not alongside it.
- **exec.go.** `git diff --exit-code main -- internal/convert/exec.go` is empty.
- **engine75 tags.** `docker images | grep -c engine75-` printed 0, checked before the daemon stopped answering.

## Deviations from Plan

1. **[User correction] log-format-postgres-1.** The container does not exist on this machine, so it was neither stopped nor started nor recreated, and no trap for it was registered. Port 5434 was free: no container published it, and lsof showed no listener. No other project's container, image or volume was stopped, removed or pruned.
2. **[User rule] Tracked dirt.** The only tracked modification is the orchestrator's `.planning/config.json` (`_auto_chain_active: true`). Following the user's rule, it was not checked out or stashed.
   - **How the run got tracked_dirt=0.** Both the live run and ci-local ran from a fresh `git clone --branch gsd/phase-75-dxf-2d-pipeline` of this repository at b2c0db8, placed at `scratchpad/lp/octoconv` so the compose project is still `octoconv`.
   - **Why a clone and not a worktree.** The script's guard (`git rev-parse --git-dir` = `.git`, from 73-06) rejects a worktree; a clone passes it unchanged.
   - **Why the missing .env does not matter.** The clone has no `.env`. The compose config of the six services (environment, ports, volumes, deploy) hashed identically in both trees (`a815508f…`), so it changed nothing.
   - **Evidence path.** The evidence and the transcript were copied from the clone into the main tree (checked with `cmp`) and committed there. The clone was then deleted.
   - **config.json.** It was never touched. Its sha256 now is `44908671c19353e21e040a96757fa2c3dd1b206427bf965b4c0e28d24d66fdba`, and its diff is the same one line as at the start.
3. **[Process] Development runs before the official run (not evidence).** The DXF sections were developed against a stack built from 3573820 in the clone. They ran through a trimmed copy of the script that kept the 74 helpers and the DXF block, dropped `--build` and fixed UP_AT. These runs went to scratch files:
   - **dev1 FAIL:** the harness required a proven line after UP_AT on an already-running stack.
   - **dev2 FAIL:** my bug. `DXF_MEAS wrong_extents: unit mm, want any`: the unit check did not honour "any". Fixed by skipping the unit check for "any" and by asserting wrong_extents as mm/declared.
   - **dev3 ALL PASS.**
   - After dev3, `image` was added to the walk's forbidden elements. The official run is the first run with that change, and it passed.
4. **[Strength] Checks beyond the plan's list:**
   - a raw `<script` byte check on every SVG;
   - the image element forbidden;
   - content type `application/pdf` on dxf→pdf;
   - pdftotext text present for uncommon;
   - alpha on the transparent render;
   - no cad-dxf left after the cancel;
   - the replicate's sha256 pinned to the 75-02 evidence;
   - wrong_extents unit asserted.
5. **[Rule 3 - Environment] Leaked test temp files filled the shared disk.** During the uncached go test, host free fell from 18 to 10 GiB and the VM showed 6.0 G free.
   - **Cause.** 74 `multipart-*` files of 66060289, 123731969 and 141557787 bytes (8.4 GB) were left in `$TMPDIR` by the internal/api ceiling tests. They call `ServeHTTP` on a recorder, so nothing calls `MultipartForm.RemoveAll`. The files came from the go test and ci-local runs of 75-09 and 75-10.
   - **Fix.** All 74 were removed after `lsof` showed no process held them, and host free went back to 17 GiB.
   - **Follow-up.** The test fix is recorded in deferred-items.md; it is out of scope here. Production is not affected, because a real net/http server removes the form.

## Docker daemon stopped answering after the gates

- **Last successful daemon calls, 04:23Z:**
  - `docker compose -p octoconv stop` (nothing octoconv was running);
  - `docker images | grep -c engine75-` → 0;
  - `docker system df`, with 3.58 GB of build cache reclaimable;
  - `docker run --rm alpine df -h /` → overlay 41.3G, 35.2G used, **6.0G free**. alpine had to be pulled, so it had been removed in the meantime by something other than this plan.
- **From 04:43Z to 05:02Z** every docker CLI call hung: `docker run`, `docker version` and `docker info`.
  - `orbctl status` says Running.
  - The OrbStack vmgr log shows a `vmgr sleep` at 04:23:07Z and no docker agent event after 04:23:49Z. k8s reflector lines show `TLS handshake timeout` against 127.0.0.1:26443.
- **What could and could not be measured.**
  - No df inside the VM was possible: docker is the only way in, and there is no orb Linux machine.
  - The host shows 16 GiB free, and the VM data image allocates 32G on the host.
  - The watchdog's lowest VM reading during the live run was 7.14 GiB (03:57Z, during the build).
- **Response.** As instructed, OrbStack was not restarted, and my own hung CLI probes were killed.
- **Not done because of the hang:** `docker builder prune -f` for the build cache of the two rebuilds. The `octoconv-api` and `octoconv-cad-worker` images are kept, as required. No `engine75-` tag was ever made by this plan.
- **All gates were complete before the hang.**
- **Compose stack.** It was stopped twice: by the script's own trap and by `docker compose -p octoconv stop` at 04:23Z. No octoconv container was running at that point.

## Known Stubs

None.

## Threat Flags

None beyond the plan's register:
- **T-75-35:** the HDR names b2c0db8 with tracked_dirt=0, the images were built from that commit, and no FAIL is recorded as a pass.
- **T-75-36:** the other project's postgres does not exist (deviation 1).
- **T-75-37:** the 74 sections re-ran unchanged and passed.
- **T-75-SC:** no package was added.

## Self-Check: PASSED

- **Files.** These exist:
  - scripts/cad-liveproof.sh, with DXF_BOMB and DXF_RETRY;
  - evidence/75-10-liveproof.txt, with `SUMMARY,result=ALL PASS` and `HDR,tracked_dirt=0`;
  - `.planning/ci-local/ci-local-20261012T040858Z.log`, ending ALL REQUESTED TIERS PASSED;
  - deferred-items.md, with the 75-10 entry.
- **Commits.** b2c0db8 and b2d5b2e are ancestors of HEAD, and both show signature G.
- **State.** STATE.md and ROADMAP.md were not updated; the orchestrator does phase tracking.
