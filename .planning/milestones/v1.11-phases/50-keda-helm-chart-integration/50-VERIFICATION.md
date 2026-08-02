---
phase: 50-keda-helm-chart-integration
verified: 2026-08-02T09:30:00Z
status: passed
score: 12/12 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: none
  previous_score: n/a
  gaps_closed: []
  gaps_remaining: []
  regressions: []
---

# Phase 50: KEDA/Helm Chart Integration — Verification Report

**Phase Goal:** The pandoc class autoscales in the cluster with production parity to the other six engine classes, and scale-from-zero is live-proven.
**Verified:** 2026-08-02
**Status:** passed
**Re-verification:** No — initial verification (performed AFTER the 50-REVIEW blocker-closure commit `2054c50`)

## Method

Goal-backward, adversarial. SUMMARY.md claims were treated as unverified narration. Every truth below was re-derived from one of:

- Live `helm template` renders (production `values-local.yaml`, and production + `values-loadproof.yaml`) — not from reading templates alone.
- Source files (`deploy/chart/octoconv/**`, `scripts/keda-pandoc-*.sh`, `internal/queue/queue.go`, `internal/metrics/queue_collector.go`, `cmd/api/main.go`, `docker-compose.yml`).
- Committed gate transcripts + evidence artifacts under `evidence/`.
- `git diff 4f14eaf..2054c50 -- deploy/chart/octoconv/` to establish exactly what changed between the live gate session and the shipped HEAD.
- `go build ./...`, `go vet`, `go test -run TestAllConvertQueuesCoversEveryEngine`, `bash -n` on both harnesses.

Live cluster gates were NOT re-run (OrbStack k8s intentionally off, per instruction). Their evidence was validated for *current-configuration applicability*, which is the load-bearing question after commit `2054c50`.

## Goal Achievement

### Observable Truths

Must-haves = 4 ROADMAP Success Criteria (the contract) merged with the 16 plan-frontmatter truths across 50-01..50-04, deduplicated to 12 distinct testable statements. No roadmap SC was dropped.

| #  | Truth | Status | Evidence |
|----|-------|--------|----------|
| 1  | (SC1) `pandoc-worker` Deployment + KEDA `ScaledObject` ship in the chart with `minReplicaCount:0`, grace/stabilization derived from the measured `PANDOC_ENGINE_TIMEOUT=65s`, WR-01 fail-safe triad verbatim | VERIFIED | Render (`values-local.yaml`): ScaledObject `worker-pandoc-scaledobject` → `minReplicaCount: 0`, `maxReplicaCount: 2`, `pollingInterval: 10`, `cooldownPeriod: 90`, `threshold: "2"`, `fallback: {failureThreshold: 3, replicas: 1}`, `ignoreNullValues: "false"`, `query: sum(octoconv_queue_depth{queue="pandoc", state=~"pending|active|retry"})`. Deployment `pandoc-worker` → `terminationGracePeriodSeconds: 85`, `cpu: "2"`, `memory: "1Gi"`. Triad is byte-identical to `scaledobject-ocr.yaml` (diffed). Grace derivation 65→75→85 confirmed against `cmd/pandoc-worker/main.go` ShutdownTimeout = engineTimeout+10s and compose `stop_grace_period: 85s`. |
| 2  | (SC2) `QueuePandoc` registered in the always-on api queue-depth collector so KEDA resolves the pandoc backlog at genuinely zero replicas | VERIFIED | `internal/queue/queue.go:814` `AllConvertQueues()` includes `QueuePandoc`; `cmd/api/main.go:98` registers `metrics.NewQueueDepthCollector(..., append(queue.AllConvertQueues(), queue.QueueWebhook)...)`; `internal/metrics/queue_collector.go` emits `octoconv_queue_depth{queue,state}` for pending/active/scheduled/retry/archived. `go test -run TestAllConvertQueuesCoversEveryEngine` PASS. Live-confirmed by SC3: `pandoc-worker` at genuine 0 replicas, scaler still resolved the backlog and scaled 0→1. |
| 3  | (SC3) Scale-from-zero live-proven for pandoc with timestamped evidence (0→1 on real queue depth, back to 0 after drain) | VERIFIED | `evidence/gate-transcript-loadproof-20260801T202642Z.log` — `ALL 12 ASSERTIONS PASSED`, incl. "Deployment status.replicas before any job (genuine zero) == 0", "pandoc-worker scaled 0→1 after trigger job 1eddde76-…", "trigger job reaches done", "full-cycled back to 0 replicas (observed: 0)". Timestamped pod event timeline in `evidence/sc3-pandoc-scale-from-zero-20260801T202642Z.txt` (real `.firstTimestamp`, not relative age). Installed with `values-local.yaml` ONLY (production tuning, null stabilization) — verified at `scripts/keda-pandoc-loadproof.sh:304`. |
| 4  | (SC4a) `PANDOC_*` env parity (IN-02) across all queue-client-constructing services | VERIFIED | `grep -rl queue.NewClient cmd/` → 9 binaries. All corresponding chart Deployments (api, worker, document/chromium/audio/av/ocr/pandoc/webhook workers, + mcp-http) include `octoconv.commonEnv` → `envFrom: configMapRef octoconv-config`, which carries `PANDOC_ENGINE_TIMEOUT: "65s"`, `PANDOC_MAX_RETRY: "2"`, `PANDOC_WORKER_CONCURRENCY: "1"`, `PANDOC_MAX_INPUT_BYTES: "3145728"`. Compose side: 10 queue-client services each carry `PANDOC_ENGINE_TIMEOUT: "65s"` + `PANDOC_MAX_RETRY: "2"` (10/10 literal occurrences). Chart↔compose byte-exact on all four keys. |
| 5  | (SC4b) A pandoc job survives a genuine N→N-1 HPA downscale without a premature SIGKILL | VERIFIED | `evidence/gate-transcript-downscale-20260801T210017Z.log` — `ALL 35 ASSERTIONS PASSED`. Genuine HPA event: `grep -n "kubectl scale\|scale --replicas"` over both harnesses returns nothing; STEP 11 is observe-only, bounded 180s, metric-driven. Raw evidence: `sigterm_killing_event_ts=2026-08-01T21:06:19Z` < `pod_terminated_finished_at=2026-08-01T21:06:36Z`, `pod_terminated_reason=Completed`, `pod_terminated_exit_code=0` (never 137), `queued_to_active_count=1`, `observed_pod_spec_grace=85`, all 8 batch jobs `done`. |
| 6  | (P1) Production values render NO `advanced`/behavior block, while the falsy-0 guard (`hasKey` AND `ne … nil`) is present in template source | VERIFIED | Production render: pandoc ScaledObject has no `advanced:` key. Source `scaledobject-pandoc.yaml:` `{{- if and (hasKey .Values.keda.pandoc "scaleDownStabilizationSeconds") (ne .Values.keda.pandoc.scaleDownStabilizationSeconds nil) }}`. |
| 7  | (P2) Prometheus `scrape_interval` is values-driven; production default 15s unchanged | VERIFIED | `_helpers.tpl:77` `scrape_interval: {{ .Values.prometheus.scrapeInterval | default "15s" }}`. Production render → `scrape_interval: 15s`; overlay render → `scrape_interval: 5s`. `prometheus.yaml:43` `checksum/config` hashes the same named template — the two renders produce different checksums (`cad37c16…` vs `01d95cb8…`), so a change genuinely rolls the Prometheus pod. |
| 8  | (P2) Overlay render emits literal `stabilizationWindowSeconds: 0` while grace stays 85 and cpu/memory stay production | VERIFIED | Overlay render: `advanced.horizontalPodAutoscalerConfig.behavior.scaleDown.stabilizationWindowSeconds: 0`, `pollingInterval: 3`; same manifest Deployment: `terminationGracePeriodSeconds: 85`, `cpu: "2"`, `memory: "1Gi"`. Proves the falsy-0 guard emits 0 rather than swallowing it. |
| 9  | (P2) `scripts/keda-pandoc-loadproof.sh` is a self-contained, teardown-safe SC3 harness with run-time fixture synthesis | VERIFIED | File present, `+x`, `bash -n` clean. Contains preflight (kubectl reach, compose-down guard, image-present, KEDA version resolvable), asynq queue-registry seeding incl. `pandoc`, in-container fixture synthesis, EXIT-trap teardown. Transcript confirms teardown ran: "octoconv namespace deployments remaining: 0". |
| 10 | (P3) `scripts/keda-pandoc-downscale-survival.sh` is a self-contained SC4 harness with deterministic `/proc` victim-job attribution, a production-85 grace assertion under the overlay, and a `--calibrate-only` offline mode | VERIFIED | File present, `+x`, `bash -n` clean. Transcript: "victim pod … spec.terminationGracePeriodSeconds is the PRODUCTION value (85) even under the loadproof overlay == 85"; "INFLIGHT_JOB_ID resolved deterministically from /proc attribution (last sample at-or-before the Killing event) == 897238b2-…". `--calibrate-only` branch present at `:112-115`. Installs `values-local.yaml -f values-loadproof.yaml` (`:549`). |
| 11 | (P3) Fixture and job window calibrated OFFLINE at production limits, inside [15,50]s and safely under `PANDOC_ENGINE_TIMEOUT=65s` | VERIFIED | `evidence/calibration-pandoc-downscale-fixture-20260801T205758Z.txt`: `container_cpus=2`, `container_memory=1g`, `fixture_bytes=3043395` (≤ cap 3145728), `wall_seconds=26.953`, band [15,50], timeout 65s. Re-calibrated after the Path-B cap change — the stale 4 MiB calibration from 20260801T201900Z is superseded and both are retained. |
| 12 | (P4) Both proofs captured as timestamped committed evidence from ONE OrbStack session, cluster torn down clean | VERIFIED | All evidence timestamps fall inside 2026-08-01 20:19Z–21:06Z contiguously. `git show --stat c723e38` shows all 13 evidence files committed. Both transcripts end with an explicit `=== TEARDOWN ===` block reporting "octoconv namespace deployments remaining: 0". SUMMARY's `k8s.enable false` restore is operator-attested (not machine-verifiable post-hoc; consistent with the cluster being off now). |

**Score:** 12/12 truths verified

### The two questions this verification was specifically asked to press on

**Q1 — Does SC1's "production parity" claim still hold after `PANDOC_WORKER_CONCURRENCY` 2→1?**

Yes, and parity is stronger than before, not weaker. Parity in this chart has never meant identical numbers — it means each engine class ships the same asset set with per-class values derived from its own measured budget. Checked concretely:

- Asset parity: 7 `deployment-*-worker` templates and 7 `scaledobject-*` templates exist; pandoc has both. No sibling has an asset pandoc lacks (no per-engine Service, PDB, ServiceMonitor, or netpol entry exists for any engine — `networkpolicy-metrics.yaml` selects on `octoconv.io/tier: app`, which the pandoc pod template carries, verified in the render).
- Concurrency parity: the ConfigMap now reads `AUDIO_WORKER_CONCURRENCY: "1"`, `AV_WORKER_CONCURRENCY: "1"`, `OCR_WORKER_CONCURRENCY: "1"`, `PANDOC_WORKER_CONCURRENCY: "1"`. Pandoc joined the memory-bound trio's posture rather than the document/html pair's "2". Cluster-wide parallelism ceiling `maxReplicaCount × concurrency` = 2, identical to audio/av/ocr.
- The change is measurement-backed, which is the project's own standard: `evidence/blocker1-memory-probe-20260802.txt` records peak ANON 909 MiB/1024 at concurrency 1 vs 1022 MiB/1024 at concurrency 2, on the real image at `--cpus 2 --memory 1g` with a 3.04 MB dense fixture.
- Compose and chart remained in lockstep through the change (`docker-compose.yml:877` = `"1"`, `configmap.yaml:89` = `"1"`), so IN-02 parity (T4) is unbroken.

**Q2 — Are the SC3/SC4 live proofs still valid evidence for the CURRENTLY shipped configuration?**

Yes. This was checked by diffing the chart between the live-gate commit and HEAD rather than by accepting the argument as stated:

```
git diff 4f14eaf 2054c50 -- deploy/chart/octoconv/   (non-comment lines)
-  PANDOC_WORKER_CONCURRENCY: "2"
+  PANDOC_WORKER_CONCURRENCY: "1"
```

That is the **only** functional chart delta since the live session. Nothing in the Deployment shape, the ScaledObject, the KEDA tuning, the collector, or the grace period moved.

- **SC4 evidence is now *more* representative, not less.** The SC4 run installed `values-loadproof.yaml`, whose `pandocWorker.extraEnv` sets `PANDOC_WORKER_CONCURRENCY: "1"`. I confirmed in the overlay render that this emits an explicit `env:` entry (`- name: PANDOC_WORKER_CONCURRENCY / value: "1"`), which by k8s precedence beats the `envFrom` ConfigMap value. So the victim pod in the passing run *executed at concurrency 1* — exactly the value production now ships. The runtime configuration proven equals the runtime configuration shipped.
- This also closes the code review's CR-01 loop properly: the review's objection was that "2 × 3 MiB in one 1Gi cgroup has never been run". After the fix, the shipped combination is "1 × 3 MiB in one 1Gi cgroup", and that combination *was* run live under real load — 8 jobs, 2 pods, `long_fixture_bytes=3043395`, zero exit-137, all `done`. The offline probe's 114 MiB headroom at concurrency 1 is corroborated by a live run that included the Go worker's own RSS in the same cgroup.
- **SC3's residual delta is immaterial.** SC3 ran with the ConfigMap at "2" and a ~4 KB fixture, submitting a single job. Concurrency is not load-bearing for a 0→1→0 single-job scale-from-zero: it governs in-pod parallelism, not the scaler's resolution of `octoconv_queue_depth{queue="pandoc"}` at zero replicas, nor the 0→1 or cooldown-to-0 transitions. The 3 MiB cap change is likewise irrelevant at 4 KB. The env *plumbing* SC3 exercised (envFrom-only, no overlay) is byte-identical today; only the integer changed.
- **The BLOCKER-2 timestamp fix does not retroactively weaken SC4.** The committed evidence still contains the pre-fix corrupted string `job_finished_at=2026-08-0121:06:36.104457+00`, but the load-bearing ordering is independently established by two *uncorrupted* kubelet-sourced values in the same file: `sigterm_killing_event_ts=2026-08-01T21:06:19Z` and `pod_terminated_finished_at=2026-08-01T21:06:36Z`. The ordering proof holds without the psql value at all. The fix (`to_char(... 'YYYY-MM-DD"T"HH24:MI:SS"Z"')` + shared `toEpoch()`) is present at `:1018-1019` and `:1040`, making the assertion portable to GNU `date` for future runs.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `deploy/chart/octoconv/templates/deployment-pandoc-worker.yaml` | pandoc-worker Deployment, tier:app, commonEnv, probes, extraEnv hook | VERIFIED | Renders; `name: pandoc-worker`, `octoconv.io/tier: app`, grace 85, startup/readiness/liveness on :9090/metrics, `spec.replicas` correctly omitted under KEDA ownership |
| `deploy/chart/octoconv/templates/scaledobject-pandoc.yaml` | KEDA ScaledObject, fast-engine null-stabilization, WR-01 triad | VERIFIED | Renders; `queue="pandoc"` retry-inclusive PromQL, triad byte-identical to ocr sibling, co-dependency guard on `keda.enabled AND prometheus.enabled` |
| `deploy/chart/octoconv/values.yaml` | `keda.pandoc` (D-01/02/03) + `pandocWorker` (D-04a/b) | VERIFIED | `pandocWorker:` block with grace 85 + cpu 2/mem 1Gi and derivation comments; `keda.pandoc` with threshold 2 / max 2 / poll 10 / cooldown 90 / stabilization null, threshold re-derived on its own terms after the concurrency change |
| `deploy/chart/octoconv/templates/configmap.yaml` | `PANDOC_*` in shared ConfigMap (IN-02) | VERIFIED | 4 keys present; comment block records both the 3 MiB Path-B cap and the concurrency-1 measurement with the evidence numbers |
| `deploy/chart/octoconv/values-loadproof.yaml` | `keda.pandoc` gate override + `pandocWorker.extraEnv` | VERIFIED | `scaleDownStabilizationSeconds: 0`, `pollingInterval: 3`, `prometheus.scrapeInterval: "5s"`, `pandocWorker.extraEnv.PANDOC_WORKER_CONCURRENCY: "1"` (now documented as parity, not override) |
| `deploy/chart/octoconv/templates/_helpers.tpl` | values-driven `scrapeInterval` | VERIFIED | `:77`; production 15s / overlay 5s render-confirmed |
| `scripts/keda-pandoc-loadproof.sh` | SC3 harness | VERIFIED | 34 KB, `+x`, `bash -n` clean, 12 assertions, EXIT-trap teardown |
| `scripts/keda-pandoc-downscale-survival.sh` | SC4 harness + `--calibrate-only` | VERIFIED | 55 KB, `+x`, `bash -n` clean, 35 assertions, `/proc` attribution, BLOCKER-2 fix applied |
| `.planning/phases/50-…/evidence/` | timestamped SC3 + SC4 + calibration evidence | VERIFIED | 17 files; SC3 pass set, SC4 pass set, SC4 OOM defect record retained, 2 calibrations, BLOCKER-1 memory probe |
| `docker-compose.yml` | compose-side parity for the two changed values | VERIFIED | `PANDOC_MAX_INPUT_BYTES: "3145728"`, `PANDOC_WORKER_CONCURRENCY: "1"` |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `scaledobject-pandoc.yaml` | `deployment-pandoc-worker.yaml` | `scaleTargetRef.name: pandoc-worker` | WIRED | Names match in the render; live-proven (KEDA actually scaled the Deployment 0→1 and 2→1) |
| `scaledobject-pandoc.yaml` | `internal/queue/queue.go` `QueuePandoc` | PromQL `octoconv_queue_depth{queue="pandoc"}` | WIRED | Metric name matches `internal/metrics/queue_collector.go`; label value matches `QueuePandoc = convert.EnginePandoc`; live-resolved at 0 replicas in SC3 |
| `deployment-pandoc-worker.yaml` | `configmap.yaml` | `octoconv.commonEnv` → `envFrom configMapRef octoconv-config` | WIRED | Rendered `envFrom` present; live pod describe in SC3 evidence confirms "Environment Variables from: octoconv-config ConfigMap Optional: false" |
| `values-loadproof.yaml` | `scaledobject-pandoc.yaml` | stabilization override through the falsy-0 guard | WIRED | Overlay render emits literal `0`; production render omits the block entirely |
| `values.yaml prometheus.scrapeInterval` | `prometheus.yaml` pod template | `checksum/config` over `octoconv.prometheusScrapeConfig` | WIRED | Two renders produce two different checksums — the pod genuinely rolls |
| `keda-pandoc-loadproof.sh` | live Deployment | `helm install -f values-local.yaml` + real job | WIRED | Live transcript, 12/12 |
| `keda-pandoc-downscale-survival.sh` | victim pod grace 85 | genuine HPA 2→1, SIGTERM-before-completion | WIRED | Live transcript, 35/35 |
| `keda-pandoc-downscale-survival.sh` | `job_events` | psql assertion of exactly one `queued→active` | WIRED | `queued_to_active_count=1` in evidence |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|--------|
| pandoc ScaledObject trigger | `octoconv_queue_depth{queue="pandoc",state=~"pending\|active\|retry"}` | api `/metrics` ← `queueDepthCollector.Collect()` ← asynq `Inspector.GetQueueInfo("pandoc")` ← Redis | Yes — live scaler acted on it (0→1 in SC3, 2→1 in SC4); `batch_queue_depth` history in SC4 evidence shows 8→7→…→2 | FLOWING |
| `pandoc-worker` Deployment env | `PANDOC_*` | `octoconv-config` ConfigMap via `envFrom` | Yes — `cmd/pandoc-worker/main.go` reads all four names (verified in 50-REVIEW, re-confirmed here); live pod describe shows the ConfigMap bound | FLOWING |
| Prometheus scrape config | `scrape_interval` | `.Values.prometheus.scrapeInterval` | Yes — 15s/5s renders differ and roll the pod checksum | FLOWING |

No HOLLOW / DISCONNECTED / STATIC artifacts found.

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Chart renders at production values | `helm template … -f values-local.yaml` | exit 0, 1660 lines, 7 ScaledObjects incl. pandoc | PASS |
| Chart renders under gate overlay | `helm template … -f values-local.yaml -f values-loadproof.yaml` | exit 0, `stabilizationWindowSeconds: 0`, grace still 85 | PASS |
| Collector covers every engine class | `go test ./internal/queue -run TestAllConvertQueuesCoversEveryEngine` | `--- PASS` | PASS |
| Repo still builds / vets | `go build ./...` ; `go vet ./internal/queue ./internal/metrics` | clean | PASS |
| Harness syntax | `bash -n` on both gate scripts | clean | PASS |
| Chart lint | `helm lint deploy/chart/octoconv` | FAILS on `templates/secret.yaml:16` nil pointer | PRE-EXISTING, not this phase — `secret.yaml` last touched by `8c7a97b` (Phase 24-01); the file is not in any Phase 50 `files_modified` and the failure is a missing `.Values.secrets.postgresPassword` in the bare default values, reproducible on `main` before this phase |
| Live k8s gates | (not run) | — | SKIP — OrbStack k8s intentionally off; verified from committed evidence per instruction |

### Probe Execution

| Probe | Command | Result | Status |
|-------|---------|--------|--------|
| `scripts/keda-pandoc-loadproof.sh` | requires live cluster | not re-run | SKIP (evidence-verified: `ALL 12 ASSERTIONS PASSED`, teardown clean) |
| `scripts/keda-pandoc-downscale-survival.sh` | requires live cluster | not re-run | SKIP (evidence-verified: `ALL 35 ASSERTIONS PASSED`, teardown clean) |
| `scripts/keda-pandoc-downscale-survival.sh --calibrate-only` | starts a Docker container, ~30s, mutates local docker state | not run | SKIP (evidence-verified: `calibration-…-20260801T205758Z.txt`, 26.953s in band) |

No `scripts/*/tests/probe-*.sh` convention exists in this repository; the phase's declared verification mechanism is the two gate harnesses, both of which have committed pass transcripts.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| PDCP-02 | 50-01, 50-02, 50-03, 50-04 (all four declare it) | pandoc worker containerized (`Dockerfile.pandoc-worker`, pinned, TeX-free) and KEDA scale-from-zero autoscaled with a measured timeout; compose + Helm chart integration; IN-02 `PANDOC_*` env parity across all queue-client services | SATISFIED (substance) | Container half closed in Phase 49 (verified 4/4, commit `46dd87e`); KEDA/Helm half closed here — truths 1–5. Measured timeout 65s traced from `49-03-SUMMARY.md` through `configmap.yaml`, `values.yaml` grace derivation, and `docker-compose.yml`. |

**Orphaned requirements:** none. `grep "Phase 50" .planning/REQUIREMENTS.md` maps exactly one ID (PDCP-02), which all four plans declare.

**Bookkeeping note (not a goal gap):** `.planning/REQUIREMENTS.md` still shows `- [ ] **PDCP-02**` and `| PDCP-02 | Phase 50 | Pending |`. The requirement's substance is fully satisfied and verified above; only the ledger checkbox/status is stale. Precedent in the same file (PDCE-04, marked Complete with a Phase 49 note) shows this is normally closed at phase/milestone completion, i.e. downstream of verification. Flagging so the milestone close-out does not miss it.

### Anti-Patterns Found

Debt-marker gate: `grep -n -E "TBD|FIXME|XXX|TODO|HACK|PLACEHOLDER"` across all nine phase-modified files returns **zero matches**. No blocker-class debt markers.

The following are carried forward from `50-REVIEW.md` as still-open WARNINGs. None of them blocks the phase goal — all live in the gate harnesses (operator tooling, not the production data path) or are pre-existing chart-wide patterns that pandoc merely replicates. Recorded here so they are not silently lost at milestone close.

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `keda-pandoc-downscale-survival.sh` | 112-115 | WR-01: unknown CLI arg falls through to the destructive cluster gate instead of erroring | Warning | Operator typo on `--calibrate-only` triggers a full KEDA+chart install/uninstall cycle |
| `keda-pandoc-downscale-survival.sh` | 848-855 | WR-02: attribution poller `kubectl exec`s into the 1Gi cgroup under memory test | Warning | Measurement apparatus perturbs the measured property |
| `keda-pandoc-downscale-survival.sh` | 313-388 | WR-03: `--calibrate-only` asserts wall-clock only, no memory assertion | Warning | The dimension that actually broke (memory) is still unasserted offline; partially compensated by `evidence/blocker1-memory-probe-20260802.txt` |
| both harnesses | 12 occurrences | WR-04: stale `4 MiB` / `4194304` / `3800000` prose after the 3 MiB Path-B cap | Warning | Anyone re-tuning the fixture from these comments targets a size the worker now rejects |
| `keda-pandoc-downscale-survival.sh` | 872 | WR-05: comment says `stabilizationWindowSeconds:15`; the overlay it installs sets `0` | Warning | The 180s window derivation is documented against the wrong number |
| `keda-pandoc-downscale-survival.sh` | 787-792 | WR-06: victim and survivor can resolve to the same pod; `sort_by(creationTimestamp)` premise unsupported at 1s resolution | Warning | A degenerate single-element array would silently invalidate the collateral-freedom assertion |
| both harnesses | preflight | WR-07: no preflight for `jq`/`psql`/values-file presence | Warning | Failures surface after KEDA + chart are already installed |
| both harnesses | multiple | WR-08: fixed predictable `/tmp` paths | Warning | Symlink-redirect risk on a multi-user host; concurrent runs clobber each other |
| both harnesses | multiple | WR-09: `$WORKDIR` leaks (~3 MiB fixture each) on every failure path | Warning | 17 evidence files imply several non-happy-path exits already leaked |
| `keda-pandoc-downscale-survival.sh` | 280-294 | WR-10: asymmetric fixture resize can overshoot the cap | Warning | Can burn all three attempts after cluster time is spent |
| `scaledobject-pandoc.yaml` | 37 | WR-11: `metadata.namespace` pinned from values while the scale target is release-namespaced | Warning | `helm install -n staging` would silently never autoscale. **Pre-existing across all 7 siblings** (`scaledobject-ocr.yaml:28` et al.) — pandoc replicates it, which is literally parity; fix belongs to a chart-wide change |
| `keda-pandoc-downscale-survival.sh` | 742-748, 959, 1010 | WR-12: job IDs string-interpolated into SQL on a write-capable connection | Warning | Not exploitable today (values constrained by the extraction regexes); one regex change away from injection |
| `deployment-pandoc-worker.yaml` | 45-52 | New (found here): extraEnv comment still says "the ConfigMap's production default (\"2\")" after the 2→1 fix | Info | Doc staleness only |
| `50-02-SUMMARY.md` | 36 | New (found here): claims SC3 proved "production KEDA tuning (… concurrency 2)" | Info | Historically accurate at write time; superseded by `2054c50`. Does not affect SC3's validity (see Q2) |
| `evidence/sc4-…210017Z.txt` | 97 | Residual corrupted `job_finished_at` from the pre-BLOCKER-2 run | Info | Ordering proof stands on the two uncorrupted kubelet timestamps in the same file; script is fixed for future runs |
| `values.yaml` / `50-CONTEXT.md` D-03 | — | New (found here): D-03's "concurrency 2 → already 4 parallel conversions" rationale for `maxReplicaCount: 2` is stale (now 2 parallel) | Info | The value itself is unchanged and now *more* conservative; pandoc's ceiling matches audio/av/ocr exactly |

### Human Verification Required

None outstanding. The two items in this phase that genuinely required a human — the live OrbStack SC3 and SC4 cluster gates — were executed as operator-gated checkpoints during 50-04, produced committed timestamped transcripts, and were explicitly approved ("approved", 2026-08-02). Re-running them was out of scope for this verification by instruction, and their applicability to the current shipped configuration is established analytically in Q2 above rather than assumed.

No `<verify><human-check>` blocks were found on `auto` tasks in any of the four PLAN files, so there is nothing deferred to harvest.

### Gaps Summary

None. All four ROADMAP Success Criteria and all twelve merged must-haves are verified against the codebase and committed evidence, not against SUMMARY narration.

The two most likely failure modes for this phase were specifically hunted and did not materialize:

1. **"The shipped config was never proven."** Falsified by diffing the chart across the post-gate fix commits: the sole functional delta is `PANDOC_WORKER_CONCURRENCY: "2" → "1"`, and the passing SC4 run already executed at concurrency 1 because the loadproof overlay's explicit `env:` entry overrode the ConfigMap. The shipped runtime configuration is the one that was live-proven under real load.
2. **"The blocker fixes were cosmetic."** Falsified by reading the fixed code: BLOCKER-1's fix is present in both `configmap.yaml:89` and `docker-compose.yml:877` with a committed measurement backing it, and the `keda.pandoc.threshold` was honestly re-derived rather than left with a now-broken "= concurrency" rationale; BLOCKER-2's fix is present at `keda-pandoc-downscale-survival.sh:1018-1019` + `:1040`, routing both timestamps through the single `toEpoch()` helper.

Twelve review WARNINGs remain open. They are harness robustness, doc staleness, and one replicated chart-wide namespace defect — worth a follow-up `/gsd-quick`, but none of them prevents the pandoc class from autoscaling with production parity, and none of them invalidates the live proofs.

---

_Verified: 2026-08-02_
_Verifier: Claude (gsd-verifier)_
