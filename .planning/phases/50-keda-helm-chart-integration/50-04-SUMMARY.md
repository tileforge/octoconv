---
phase: 50-keda-helm-chart-integration
plan: 04
status: complete
completed: 2026-08-02
requirements: [PDCP-02]
---

# Plan 50-04 Summary: LIVE SC3 scale-from-zero + SC4 downscale-survival proof (operator-gated)

Both live gates PASS in ONE OrbStack k8s session (D-06), operator-approved ("approved", 2026-08-02).

## What ran

1. `docker compose down -v` (mutual exclusion) → `k8s.enable true` → three `:dev` images rebuilt/retagged (`octoconv-api:dev` with the Phase-48 QueuePandoc collector, `octoconv-pandoc-worker:dev`, `octoconv-webhook-worker:dev` — note: the compose service is `webhook-worker-1`, its image is `octoconv-webhook-worker-1:latest`, retagged).
2. **SC3** `scripts/keda-pandoc-loadproof.sh` — ALL 12 ASSERTIONS PASSED (run 20260801T202642Z): genuine zero → 0→1 on real `octoconv_queue_depth{queue="pandoc"}` backlog (job `1eddde76-8497-4134-93b3-f69c1e37fb87`, md→html) → `done` → drained back to 0. Production tuning only (values-local, null stabilization).
3. **SC4** `scripts/keda-pandoc-downscale-survival.sh` — three live runs:
   - Run 1 (N=4, 203134Z): FAIL via the documented idle-victim retry path (16 checks passed).
   - Run 2 (N=8, 203704Z): FAIL — **genuine product defect found**: victim container **OOMKilled (exit 137)** mid html→md conversion of a ~4.09 MB input at the 1Gi limit; attributed job reached `failed`.
   - Run 3 (N=8, 210017Z, after the Path-B fix below): **ALL 35 ASSERTIONS PASSED** — `victim_inflight_job=897238b2-76fc-4162-9737-98331cac32aa`, SIGTERM `21:06:19Z` strictly before `job_finished_at 21:06:36Z`, exit `0/Completed` (never 137), `queued_to_active_count=1`, `observed_pod_spec_grace=85`, `long_fixture_bytes=3043395 ≤ 3145728`, all 8 batch jobs `done`.
4. Teardown clean after every run (`octoconv` deployments = 0), `k8s.enable false` restored. Compose left down.

## Deviation (user-approved mid-checkpoint): Path-B cap 4 MiB → 3 MiB

The Run-2 OOM is a real finding the offline calibration could not see: the Go worker and pandoc share the pod's 1Gi cgroup (calibration timed pandoc alone), and production `PANDOC_WORKER_CONCURRENCY=2` doubles exposure. Per the user's explicit choice (Path-B, the 49-03 lever): `PANDOC_MAX_INPUT_BYTES` lowered 4194304→3145728 in `docker-compose.yml` + chart `configmap.yaml` (comments amended with the live evidence); `values.yaml`/`values-loadproof.yaml` cap references updated. Gate fixture recalibrated: the pandoc `markdown` writer cost scales with block+inline **element count**, not bytes (probe: sparse 3.05 MB = 12.4s; docx/rst/latex writers 6–8s; dense 3.5 MB = 41.6s), so the synthesized repeat block became short and markup-dense → calibration 3.04 MB / **26.953s**, inside the untouchable [15,50]s band. Committed as `c723e38`.

**D-05a re-confirmed at approval:** signed-webhook evidence remains covered by Phase 49 Plan 04's live compose E2E (`TestPandocConversionE2E`, HMAC-verified); not re-proven in-cluster.

## Evidence (committed)

- `evidence/sc3-pandoc-scale-from-zero-20260801T202642Z.txt` + `gate-transcript-loadproof-20260801T202642Z.log`
- `evidence/sc4-pandoc-downscale-survival-20260801T210017Z.txt` + `gate-transcript-downscale-20260801T210017Z.log` (PASS)
- `evidence/sc4-pandoc-downscale-survival-20260801T203704Z.txt` (the OOM run — kept as the defect record)
- `evidence/calibration-pandoc-downscale-fixture-20260801T205758Z.txt` (3 MiB/dense recalibration)

## Outcome

SC3 + SC4 of PDCP-02 closed live. Phase 50 execution complete — v1.11's last requirement is done pending phase verification.
