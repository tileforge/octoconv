# Phase 50: KEDA/Helm Chart Integration - Pattern Map

**Mapped:** 2026-08-01
**Files analyzed:** 7 (2 new templates, 3 modified chart files, 2 new scripts) + 1 confirm-only
**Analogs found:** 7 / 7

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|--------------------|------|-----------|-----------------|----------------|
| `deploy/chart/octoconv/templates/deployment-pandoc-worker.yaml` (NEW) | config (k8s Deployment manifest) | event-driven (asynq queue consumer) | `deploy/chart/octoconv/templates/deployment-ocr-worker.yaml` | exact (fast-engine sibling: no cgroup/OMP bits — closer structurally to audio/av-worker, but ocr is the most-recently-added and simplest full example) |
| `deploy/chart/octoconv/templates/scaledobject-pandoc.yaml` (NEW) | config (KEDA ScaledObject manifest) | event-driven (Prometheus-metric-driven autoscale) | `deploy/chart/octoconv/templates/scaledobject-ocr.yaml` (triad/guard shape) + `deploy/chart/octoconv/templates/scaledobject-document.yaml` (null-stabilization precedent) | exact |
| `deploy/chart/octoconv/values.yaml` (MODIFIED — add `keda.pandoc` + `pandocWorker` blocks) | config | CRUD (declarative values) | same file, `keda.ocr` + `ocrWorker` blocks (structure); `keda.document` block (null-stabilization comment style) | exact |
| `deploy/chart/octoconv/values-loadproof.yaml` (MODIFIED — add pandoc gate override) | config | CRUD (declarative overlay) | same file, `keda.ocr` override block | exact |
| `deploy/chart/octoconv/templates/configmap.yaml` (MODIFIED — add `PANDOC_*` keys) | config | CRUD (env contract) | same file, OCR env block (lines 62-66) | exact |
| `scripts/keda-pandoc-loadproof.sh` (NEW) | test (live E2E gate script) | request-response + batch (HTTP job submission, polling) | `scripts/keda-ocr-loadproof.sh` | exact |
| `scripts/keda-pandoc-downscale-survival.sh` (NEW) | test (live E2E gate script) | request-response + batch | `scripts/keda-ocr-downscale-survival.sh` | exact |
| `internal/queue/queue.go` (CONFIRM ONLY — no change) | service | pub-sub | n/a — `AllConvertQueues()` already returns `QueuePandoc` (line 814) | n/a |

## Pattern Assignments

### `deploy/chart/octoconv/templates/deployment-pandoc-worker.yaml` (config, event-driven)

**Analog:** `deploy/chart/octoconv/templates/deployment-ocr-worker.yaml` (full file, 84 lines — read in one pass, reproduced in full below since it is the near-verbatim clone target)

**Full analog** (`deployment-ocr-worker.yaml:1-84`):
```yaml
{{/*
ocr-worker Deployment — OCR engine class (text-layer extraction pipeline).
...
*/}}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ocr-worker
  labels:
    {{- include "octoconv.labels" . | nindent 4 }}
    app.kubernetes.io/component: ocr-worker
spec:
  {{- if and .Values.keda.enabled .Values.prometheus.enabled }}
  # spec.replicas intentionally omitted when KEDA/HPA owns this Deployment ...
  {{- else }}
  replicas: {{ .Values.ocrWorker.replicas }}
  {{- end }}
  selector:
    matchLabels:
      {{- include "octoconv.selectorLabels" (dict "component" "ocr-worker" "root" $) | nindent 6 }}
  template:
    metadata:
      labels:
        {{- include "octoconv.labels" . | nindent 8 }}
        {{- include "octoconv.selectorLabels" (dict "component" "ocr-worker" "root" $) | nindent 8 }}
        octoconv.io/tier: app
    spec:
      terminationGracePeriodSeconds: {{ .Values.ocrWorker.terminationGracePeriodSeconds }}
      containers:
        - name: ocr-worker
          image: "{{ .Values.ocrWorker.image.repository }}:{{ .Values.global.imageTag }}"
          imagePullPolicy: IfNotPresent
          {{- include "octoconv.commonEnv" . | nindent 10 }}
          ports:
            - name: metrics
              containerPort: 9090
          startupProbe:
            httpGet:
              path: /metrics
              port: 9090
            periodSeconds: 5
            failureThreshold: 24
          readinessProbe:
            httpGet:
              path: /metrics
              port: 9090
            periodSeconds: 5
            failureThreshold: 3
          livenessProbe:
            httpGet:
              path: /metrics
              port: 9090
            periodSeconds: 10
            failureThreshold: 6
          resources:
            limits:
              cpu: {{ .Values.ocrWorker.resources.limits.cpu | quote }}
              memory: {{ .Values.ocrWorker.resources.limits.memory | quote }}
```

**Clone recipe for `deployment-pandoc-worker.yaml`:**
- Rename `ocr-worker` → `pandoc-worker` everywhere (metadata.name, `app.kubernetes.io/component`, `selectorLabels` component arg, container name, image repository ref `.Values.pandocWorker.image.repository`, `.Values.pandocWorker.replicas`, `.Values.pandocWorker.terminationGracePeriodSeconds`, `.Values.pandocWorker.resources.limits.*`).
- No cgroup/OMP env block to drop — ocr-worker's own file already has none (that concern only applies to av-worker's header comment, not its body); pandoc needs no divergence here at all — a pure name-substitution clone.
- Keep `ports`/probes identical (`/metrics` on `9090`, same `periodSeconds`/`failureThreshold` — every worker Deployment in this chart uses this exact probe shape verbatim).
- Header doc comment: adapt to explain pandoc is a static-release binary needing no `--platform` pin (mirrors compose's own comment at `docker-compose.yml:832-836`) and that grace 85s is derived from `PANDOC_ENGINE_TIMEOUT=65s` (cite D-04b in `50-CONTEXT.md`), distinct from the ocr precedent's 744→774 arithmetic.
- `.Values.pandocWorker.image.repository` should be `"octoconv-pandoc-worker"` (mirrors `ocrWorker.image.repository: "octoconv-ocr-worker"`, `Dockerfile.pandoc-worker` already exists at repo root).

---

### `deploy/chart/octoconv/templates/scaledobject-pandoc.yaml` (config, event-driven)

**Analog A (triad + falsy-0 guard shape):** `deploy/chart/octoconv/templates/scaledobject-ocr.yaml` (full file, 65 lines)

**Analog B (null-stabilization precedent — this is the shape pandoc actually needs per D-01):** `deploy/chart/octoconv/templates/scaledobject-document.yaml` (full file, 79 lines)

**Full document analog** (the block pandoc's stabilization guard must mirror, `scaledobject-document.yaml:33-78`):
```yaml
{{- if and .Values.keda.enabled .Values.prometheus.enabled }}
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: worker-document-scaledobject
  namespace: {{ .Values.global.namespace }}
  labels:
    {{- include "octoconv.labels" . | nindent 4 }}
    app.kubernetes.io/component: worker
spec:
  scaleTargetRef:
    name: document-worker
  minReplicaCount: 0
  maxReplicaCount: {{ .Values.keda.document.maxReplicaCount }}
  pollingInterval: {{ .Values.keda.document.pollingInterval }}
  cooldownPeriod: {{ .Values.keda.document.cooldownPeriod }}
  {{- if and (hasKey .Values.keda.document "scaleDownStabilizationSeconds") (ne .Values.keda.document.scaleDownStabilizationSeconds nil) }}
  advanced:
    horizontalPodAutoscalerConfig:
      behavior:
        scaleDown:
          stabilizationWindowSeconds: {{ .Values.keda.document.scaleDownStabilizationSeconds }}
          policies:
            - type: Pods
              value: 1
              periodSeconds: 15
  {{- end }}
  fallback:
    failureThreshold: 3
    replicas: 1
  triggers:
    - type: prometheus
      metadata:
        serverAddress: http://prometheus.{{ .Values.global.namespace }}.svc.cluster.local:9090
        query: sum(octoconv_queue_depth{queue="document", state=~"pending|active|retry"})
        threshold: {{ .Values.keda.document.threshold | quote }}
        ignoreNullValues: "false"
{{- end }}
```

**Clone recipe for `scaledobject-pandoc.yaml`:**
- Same skeleton as above with `document` → `pandoc` substitutions: `worker-pandoc-scaledobject`, `scaleTargetRef.name: pandoc-worker`, `.Values.keda.pandoc.*`, PromQL `query: sum(octoconv_queue_depth{queue="pandoc", state=~"pending|active|retry"})`.
- The falsy-0 guard block (`{{- if and (hasKey ...) (ne ... nil) }}`) MUST be copied verbatim (HARD-03/D-06) even though `keda.pandoc.scaleDownStabilizationSeconds` is `null` in production — this is exactly the defensive-consistency posture the ocr file's header comment calls out (`scaledobject-ocr.yaml:18-21`) applied to pandoc's opposite (always-null-in-prod) case, mirroring `scaledobject-document.yaml`'s own always-null-in-prod case exactly.
- `fallback: {failureThreshold: 3, replicas: 1}` and `ignoreNullValues: "false"` copied verbatim (WR-01 triad).
- CO-DEPENDENCY GUARD `{{- if and .Values.keda.enabled .Values.prometheus.enabled }}` wraps the whole file, copied verbatim.
- Header doc comment: adapt from `scaledobject-document.yaml:1-32`'s "SCALE-DOWN STABILIZATION" explanation (production null / gate-only override via `values-loadproof.yaml`) — this is D-01's own template, per CONTEXT.md — rather than ocr's non-null 900s framing.

---

### `deploy/chart/octoconv/values.yaml` (config)

**Analog A (per-engine app block):** `ocrWorker:` block (`values.yaml:92-107`)
```yaml
ocrWorker:
  image:
    repository: "octoconv-ocr-worker"
  replicas: 1
  # 774s = OCR_ENGINE_TIMEOUT (744s, ConfigMap) + 30s, the same
  # ENGINE_TIMEOUT+30s chart convention as audioWorker (772) / avWorker
  # (783) -- >= 744 satisfies SC4. Distinct from compose's
  # stop_grace_period=764=744+10+10, which is the compose convention.
  terminationGracePeriodSeconds: 774
  resources:
    limits:
      # MUST match the Phase-45 scripts/ocr-timing-measure.sh /
      # docker-compose.yml ocr-worker deploy.resources.limits exactly --
      # the measured 744s timeout is only valid at these limits.
      cpu: "2"
      memory: "1Gi"
```

**Clone recipe — `pandocWorker:` block** (add after `ocrWorker:`, before `chromiumWorker:`):
```yaml
pandocWorker:
  image:
    repository: "octoconv-pandoc-worker"
  replicas: 1
  # 85s = PANDOC_ENGINE_TIMEOUT (65s, ConfigMap) + 10s asynq ShutdownTimeout
  # margin + 10s Docker/k8s SIGKILL margin, mirroring compose's own
  # stop_grace_period=85s derivation (docker-compose.yml pandoc-worker,
  # 49-03-SUMMARY.md) -- D-04b. NOT the ENGINE_TIMEOUT+30s convention used
  # by worker/documentWorker/audioWorker/avWorker/ocrWorker (those derive
  # from a different asynq ShutdownTimeout margin); pandoc mirrors its own
  # compose precedent exactly.
  terminationGracePeriodSeconds: 85
  resources:
    limits:
      # MUST match the 49-03 measurement container exactly (cpus=2.0,
      # memory=1g) -- Path-B lowered PANDOC_MAX_INPUT_BYTES to 4 MiB
      # specifically for 1g headroom (D-04a). Do NOT lower memory below 1Gi
      # without re-measuring.
      cpu: "2"
      memory: "1Gi"
```

**Analog B (null-stabilization comment style — D-01's template):** `keda.document:` block (`values.yaml:212-217`)
```yaml
  document:
    threshold: "1"
    maxReplicaCount: 2
    pollingInterval: 15
    cooldownPeriod: 120
    scaleDownStabilizationSeconds: null   # NEW (D-10/Pattern 1) — production default OFF (K8s 300s HPA default preserved); load-proof gate overrides via values-loadproof.yaml
```

**Analog C (numeric precedent — html's block has the SAME numbers D-02 derives independently):** `keda.html:` block (`values.yaml:218-222`)
```yaml
  html:
    threshold: "2"
    maxReplicaCount: 2
    pollingInterval: 10
    cooldownPeriod: 90
```

**Clone recipe — `keda.pandoc:` block** (add after `keda.ocr:`, at end of the `keda:` map):
```yaml
  pandoc:
    # DERIVED FROM PANDOC'S OWN BUDGET (D-02), NOT copied from html even
    # though the numbers are identical: threshold "2" = PANDOC_WORKER_CONCURRENCY
    # (ConfigMap) -- a second replica is justified only past depth 2, one
    # pod's full digestion capacity. pollingInterval 10 / cooldownPeriod 90:
    # seconds-scale jobs (measured p95=32.318s) -> fast reaction, early
    # return to 0.
    threshold: "2"
    maxReplicaCount: 2
    pollingInterval: 10
    cooldownPeriod: 90
    # NULL (D-01, mirrors document's FAST-engine profile above, NOT
    # audio/av/ocr's 900s): measured worst-case job is 65s
    # (PANDOC_ENGINE_TIMEOUT) -- the k8s 300s HPA default downscale
    # stabilization already covers this with >4x margin. A non-null knob
    # here would be cargo-culting the heavy engines whose ~742-753s jobs
    # genuinely exceed 300s. Gate-only override lives in
    # values-loadproof.yaml (D-04). Falsy-0 guard (HARD-03/D-06) still
    # applies in scaledobject-pandoc.yaml.
    scaleDownStabilizationSeconds: null
```

Also update the file's WR-06 invariant comment block (`values.yaml:196-204`) to append pandoc's cooldownPeriod to the retry-schedule-margin list, mirroring how each prior phase appended its own class (`... / ocr=180s` → `... / ocr=180s / pandoc=90s`), and confirm `pandocRetrySchedule`'s max backoff step in `internal/queue/queue.go` stays under 90s before locking this in (verify at plan/execution time, not assumed here).

---

### `deploy/chart/octoconv/values-loadproof.yaml` (config)

**Analog:** `keda.ocr:` override block (`values-loadproof.yaml:33-44`)
```yaml
  ocr:
    # Fast, deterministic 2->1 downscale (Phase 46 OCRP-02, SC3+SC4, mirrors
    # the av override above). Production keda.ocr.scaleDownStabilizationSeconds
    # (values.yaml) is 900s (D-01: 744s worst-case job + ~21% margin), which
    # would push both the SC3 scale-to-0 observation and the SC4 N->N-1
    # downscale far past any reasonable gate window. 15s here makes both
    # observable within scripts/keda-ocr-loadproof.sh /
    # scripts/keda-ocr-downscale-survival.sh's polling windows. The
    # ocr-worker Deployment's pod-termination grace period stays at its
    # PRODUCTION 774s value in every overlay -- that grace is exactly the
    # value SC4 validates and MUST NOT be lowered here or anywhere else.
    scaleDownStabilizationSeconds: 15
```

**Clone recipe — `keda.pandoc:` override block** (add to the existing `keda:` map in this file, alongside `document`/`av`/`ocr`):
```yaml
  pandoc:
    # Fast, deterministic 2->1 downscale (Phase 50, SC3+SC4, mirrors the ocr
    # override above). Production keda.pandoc.scaleDownStabilizationSeconds
    # (values.yaml) is null (D-01: k8s 300s HPA default already covers the
    # 65s worst-case job with >4x margin) -- unlike ocr/av/audio, this
    # override does not need to defeat a large production value, it only
    # needs to make the N->N-1 downscale land inside the gate's polling
    # window deterministically. 15s here mirrors the sibling overrides
    # exactly. The pandoc-worker Deployment's pod-termination grace period
    # stays at its PRODUCTION 85s value in every overlay -- that grace is
    # exactly the value SC4 validates and MUST NOT be lowered here or
    # anywhere else.
    scaleDownStabilizationSeconds: 15
```

Note: unlike `documentWorker.extraEnv` (used to force `DOCUMENT_WORKER_CONCURRENCY=1` for deterministic victim-pod selection), pandoc's downscale-survival gate does NOT need a concurrency override in this overlay if `PANDOC_WORKER_CONCURRENCY=2` from the ConfigMap already lets the ocr-style two-job (long-first, short-second) submission pattern land the short job on a distinct pod at concurrency 2 — confirm this against the ocr precedent's rationale (`documentWorker.extraEnv` comment, `values-loadproof.yaml:60-68`) during planning; if concurrency 2 makes victim-pod selection ambiguous, add a `pandocWorker.extraEnv: {PANDOC_WORKER_CONCURRENCY: "1"}` override mirroring `documentWorker.extraEnv`, which requires `deployment-pandoc-worker.yaml` to carry the same `extraEnv` block pattern shown below.

**`extraEnv` block pattern** (only needed if the concurrency override above is required) — from `deployment-document-worker.yaml:50-61`:
```yaml
          {{- if .Values.documentWorker.extraEnv }}
          # extraEnv (Phase 28 D-08): explicit `env:` entries always take
          # precedence over envFrom-sourced values for the same key
          # (k8s-documented behaviour) — this is how values-loadproof.yaml
          # forces DOCUMENT_WORKER_CONCURRENCY=1 without editing the
          # ConfigMap's production default ("2").
          env:
            {{- range $k, $v := .Values.documentWorker.extraEnv }}
            - name: {{ $k }}
              value: {{ $v | quote }}
            {{- end }}
          {{- end }}
```

---

### `deploy/chart/octoconv/templates/configmap.yaml` (config)

**Analog:** the OCR env block (`configmap.yaml:54-66`)
```yaml
  # Compose-parity byte-for-byte (docker-compose.yml ocr-worker block,
  # Phase 45 measured: worst-cell dense_eng p95=5.81 s/page, ceil(64 x 5.81
  # x 2.0) = 744s, 17.3% under the 900s reconciler CAP). OCR_ENGINE_TIMEOUT/
  # OCR_MAX_RETRY are the IN-02 parity values every queue.NewClient()-
  # constructing service inherits identically via this shared ConfigMap --
  # a mismatch would silently produce duplicate processing. 744s stays
  # under the 15m/900s RECONCILER_ACTIVE_STALE_AFTER so the reconciler
  # never double-processes an in-flight OCR job as abandoned.
  OCR_WORKER_CONCURRENCY: "1"
  OCR_ENGINE_TIMEOUT: "744s"
  OCR_MAX_RETRY: "2"
  OCR_MAX_PAGES: "64"
  OCR_MAX_IMAGE_PIXELS: "100000000"
```

**Source values to mirror byte-for-byte** (from `docker-compose.yml` pandoc-worker service, lines 866-904):
```yaml
PANDOC_WORKER_CONCURRENCY: "2"
PANDOC_MAX_INPUT_BYTES: "4194304"
PANDOC_ENGINE_TIMEOUT: "65s"
PANDOC_MAX_RETRY: "2"
```

**Clone recipe — new pandoc block** (add after the OCR block, before `VERAPDF_TIMEOUT`):
```yaml
  # Compose-parity byte-for-byte (docker-compose.yml pandoc-worker block,
  # Phase 49 measured: worst cell html_large->md (reader-heavy HTML
  # parsing) p95=32.318s, ceil(32.318 x 2.0)=65s, 92.8% under the 900s
  # reconciler CAP). PANDOC_ENGINE_TIMEOUT/PANDOC_MAX_RETRY are the IN-02
  # parity values every queue.NewClient()-constructing service inherits
  # identically via this shared ConfigMap -- a mismatch would silently
  # produce duplicate processing. PANDOC_WORKER_CONCURRENCY/
  # PANDOC_MAX_INPUT_BYTES are pandoc-worker-only (not read by
  # queue.NewClient()) but are baked into this shared ConfigMap anyway,
  # same posture as OCR_WORKER_CONCURRENCY/OCR_MAX_PAGES above (DEBT-05 --
  # every process carries the full retry/timeout surface even when unused).
  # PANDOC_MAX_INPUT_BYTES=4 MiB (Path-B, 49-03-SUMMARY.md): the memory
  # headroom lever at the 1Gi container limit -- do NOT lower memory below
  # 1Gi without re-measuring this value.
  PANDOC_WORKER_CONCURRENCY: "2"
  PANDOC_MAX_INPUT_BYTES: "4194304"
  PANDOC_ENGINE_TIMEOUT: "65s"
  PANDOC_MAX_RETRY: "2"
```

**IN-02 confirm:** `internal/queue/queue.go:814` — `AllConvertQueues()` already returns `QueuePandoc`; no code change needed. The only remaining IN-02 gap this phase closes is the chart's ConfigMap not yet carrying `PANDOC_ENGINE_TIMEOUT`/`PANDOC_MAX_RETRY` (confirmed absent by `grep -rn PANDOC deploy/chart/octoconv/` returning zero hits before this phase).

---

### `scripts/keda-pandoc-loadproof.sh` (test, request-response + batch)

**Analog:** `scripts/keda-ocr-loadproof.sh` (full file, 644 lines — read in one pass; every section below cites its exact source lines)

**Config/constants section to clone** (`keda-ocr-loadproof.sh:62-104`, s/ocr/pandoc/, s/OCR/PANDOC/):
```bash
NAMESPACE="octoconv"
KEDA_NAMESPACE="keda"
KEDA_VERSION="2.20.1"
CHART_DIR="deploy/chart/octoconv"
VALUES_LOCAL="deploy/chart/octoconv/values-local.yaml"

# The pandoc-worker image is NOT built by this script -- operator precondition.
PANDOC_IMAGE_TAG="octoconv-pandoc-worker:dev"

API_LOCAL_PORT="18097"   # NEXT distinct port after ocr's 18095/18096 pair -- never collide with a concurrently-running gate
API_BASE="http://127.0.0.1:${API_LOCAL_PORT}"
DB_LOCAL_PORT="15441"    # NEXT distinct port after ocr's 15439/15440 pair

...
EVIDENCE_DIR=".planning/phases/50-keda-helm-chart-integration/evidence"
```

**Preflight / KEDA install / octoconv install / queue-seed sections** (`keda-ocr-loadproof.sh:207-303`): copy verbatim structure — s/ocr/pandoc/ on image-tag check, and the STEP 4b `redis-cli SADD asynq:queues` line must list `pandoc` as the 7th queue name: `image document html audio av ocr pandoc webhook`.

**Fixture divergence (KEY DIFFERENCE from ocr):** ocr's `postJob` posts a *committed* fixture (`internal/convert/testdata/ocr/text_sample.png`, `keda-ocr-loadproof.sh:349-361`). Pandoc has **no committed testdata fixture** (`find ... -path "*testdata*pandoc*"` returns nothing) — the trigger job for SC3 (a fast scale-from-zero, does not need the 4 MiB worst-case fixture) can either (a) synthesize a small markdown file inline in the script (simplest — a scale-from-zero trigger only needs to be a valid, fast job, not the worst-case-timing fixture) or (b) reuse the in-container synthesis pattern from `scripts/pandoc-timing-measure.sh:130-160` (deterministic Markdown paragraph repeated to a target size, rendered through the image's own pinned pandoc). Recommend (a) for the loadproof script (SC3 doesn't need the 4 MiB worst-case shape — only the downscale-survival script does, per D-05) — a `postJobPath` posting a small inline-written `.md` file is sufficient and keeps this script simple, mirroring `keda-audio-loadproof.sh`'s committed-fixture `postJob` shape structurally even though the fixture itself is generated at run time rather than committed.

**`waitForReplicasAtLeast`/`waitForReplicasAtMost`/`toEpoch` helpers** (`keda-ocr-loadproof.sh:367-399`, `570-577`): copy verbatim, byte-identical (deployment name parameterized).

**Pod event-timeline capture (STEP 7)** (`keda-ocr-loadproof.sh:455-602`): copy structure verbatim — s/ocr/pandoc/, `-l "app.kubernetes.io/component=pandoc-worker"` selector, `SC3_PANDOC_FILE` evidence path, same watch-based `snapshotLoop` + process-group kill discipline (WR-04/29-REVIEW WR-01).

**Teardown** (`keda-ocr-loadproof.sh:157-200`): copy verbatim except `helm uninstall octoconv`/`keda` (unchanged) and the PASS/FAIL summary text (s/ocr/pandoc/, s/Phase 46/Phase 50/).

**Timing bound difference:** ocr's readiness wait is bounded 300s ("well under OCR_ENGINE_TIMEOUT=744s + cold-start margin", `keda-ocr-loadproof.sh:515`) — pandoc's equivalent bound can shrink significantly since `PANDOC_ENGINE_TIMEOUT=65s`; a much tighter bound (e.g. 90-120s) is both sufficient and faster-failing. Do not blindly copy the 300s constant.

---

### `scripts/keda-pandoc-downscale-survival.sh` (test, request-response + batch)

**Analog:** `scripts/keda-ocr-downscale-survival.sh` (full file, 745 lines — read in one pass; every section below cites its exact source lines)

**Config/constants + `values-loadproof.yaml` layering** (`keda-ocr-downscale-survival.sh:77-116`, `279-296`): copy structure verbatim, s/ocr/pandoc/. `helm install octoconv "$CHART_DIR" -f "$VALUES_LOCAL" -f "$VALUES_LOADPROOF"` line unchanged (both overlays already exist / are being added by this phase).

**Victim-pod selection (STEP 8-9, THE core pattern to clone)** (`keda-ocr-downscale-survival.sh:459-533`): copy structure verbatim — submit LONG job first, wait `status=active`, submit SHORT job second, wait replicas>=2, identify busy pod via `-o json | jq -r '[.items[] | select(.metadata.deletionTimestamp == null)][0]...'` (the WR-05-fixed jq shape, NOT the frozen jsonpath shape in the original `keda-load-proof.sh`), annotate `controller.kubernetes.io/pod-deletion-cost=-1000`.

**FIXTURE DIVERGENCE (the load-bearing difference, per D-05):**
- Pandoc has NO committed fixture at all (ocr has both LONG-via-pdfunite-synthesis AND SHORT-via-committed-png). Both LONG and SHORT jobs need synthesis or generation.
- D-05 requires: "reader-heavy html→md fixtures @≈4 MiB (`PANDOC_MAX_INPUT_BYTES` ceiling, the measured worst cell ≈32s)" so the genuine HPA 2→1 downscale lands inside a live job window (unlike ocr's ~90-120s LONG job, pandoc's worst-case single job is only ~32s — the LONG "in-flight job" here needs a job whose wall-clock spans the loadproof-overlay's 15s stabilization window, which a single ~32s html→md job already satisfies; no 3x-multiplication trick like ocr's `pdfunite` triple-up is needed since pandoc's own worst-case cell is already the target).
- Fixture generation: mirror `scripts/pandoc-timing-measure.sh:130-160`'s in-container synthesis pattern (deterministic Markdown repeated to size, rendered via the pinned pandoc binary inside a throwaway container from the already-built `octoconv-pandoc-worker:dev` image) — same `docker run -d --entrypoint sleep ... infinity` + `docker exec` + `docker cp` shape as `keda-ocr-downscale-survival.sh:441-449`'s `FIXTURE_CONTAINER` pattern, generating an ~4 MiB HTML source, then submitting it as the LONG job (`target: md`).
- SHORT job (for victim-pod pod2 identification, mirrors ocr's committed `text_sample.png`): since pandoc has no committed fixture either, generate or inline-write a trivially small `.md` or `.html` file (sub-second conversion) at run time — same shape as the loadproof script's own small-fixture choice above.

**Grace-period cross-check assertion** (`keda-ocr-downscale-survival.sh:540-541`): mirror exactly but assert against pandoc's production grace value:
```bash
POD_GRACE=$(kubectl get pod "$BUSY_POD" -n "$NAMESPACE" -o jsonpath='{.spec.terminationGracePeriodSeconds}' 2>/dev/null || true)
assert_eq "85" "$POD_GRACE" "SC4: busy pod $BUSY_POD spec.terminationGracePeriodSeconds is the PRODUCTION value (85) even under the loadproof overlay"
```

**SIGTERM-before-completion triple-check (STEP 12)** (`keda-ocr-downscale-survival.sh:616-730`): copy verbatim structure — (1) job reaches `done`, (2) exactly one `queued`→`active` `job_events` row via `psql`, (3) kubelet `Killing` event timestamp precedes `jobs.finished_at`, pod exits `Completed`/exit `0` (never `137`). This is the load-bearing SC4 proof shape and must not be altered beyond the `85` grace-value substitution and s/ocr/pandoc/ renames.

**Downscale window bound** (`keda-ocr-downscale-survival.sh:603`): `OCR_DOWNSCALE_WINDOW=120` — can stay the same or shrink slightly given pandoc's much shorter job; no need to widen it.

---

## Shared Patterns

### KEDA ScaledObject WR-01 fail-safe triad (verbatim across all 7 classes)
**Source:** `deploy/chart/octoconv/templates/scaledobject-ocr.yaml:50-63` (any sibling `scaledobject-*.yaml` is equivalent)
**Apply to:** `scaledobject-pandoc.yaml`
```yaml
  fallback:
    failureThreshold: 3
    replicas: 1
  triggers:
    - type: prometheus
      metadata:
        serverAddress: http://prometheus.{{ .Values.global.namespace }}.svc.cluster.local:9090
        query: sum(octoconv_queue_depth{queue="pandoc", state=~"pending|active|retry"})
        threshold: {{ .Values.keda.pandoc.threshold | quote }}
        ignoreNullValues: "false"
```

### Falsy-0 guard (HARD-03/D-06) for `scaleDownStabilizationSeconds`
**Source:** `deploy/chart/octoconv/templates/scaledobject-document.yaml:49` (identical in every `scaledobject-*.yaml`)
**Apply to:** `scaledobject-pandoc.yaml`
```yaml
{{- if and (hasKey .Values.keda.pandoc "scaleDownStabilizationSeconds") (ne .Values.keda.pandoc.scaleDownStabilizationSeconds nil) }}
```

### Co-dependency guard (keda.enabled AND prometheus.enabled)
**Source:** every `scaledobject-*.yaml`, e.g. `scaledobject-ocr.yaml:23,64`
**Apply to:** `scaledobject-pandoc.yaml`
```yaml
{{- if and .Values.keda.enabled .Values.prometheus.enabled }}
...
{{- end }}
```

### spec.replicas omission when KEDA owns the Deployment (WR-02)
**Source:** every `deployment-*-worker.yaml`, e.g. `deployment-ocr-worker.yaml:34-42`
**Apply to:** `deployment-pandoc-worker.yaml`
```yaml
  {{- if and .Values.keda.enabled .Values.prometheus.enabled }}
  # spec.replicas intentionally omitted when KEDA/HPA owns this Deployment ...
  {{- else }}
  replicas: {{ .Values.pandocWorker.replicas }}
  {{- end }}
```

### Common env contract (`octoconv.commonEnv` helper)
**Source:** `deploy/chart/octoconv/templates/_helpers.tpl:43-55`
**Apply to:** `deployment-pandoc-worker.yaml`
```yaml
{{- define "octoconv.commonEnv" -}}
envFrom:
  - configMapRef:
      name: octoconv-config
  - secretRef:
      name: octoconv-secret
{{- end }}
```
Used as `{{- include "octoconv.commonEnv" . | nindent 10 }}` inside the container spec — every worker Deployment pulls the FULL ConfigMap/Secret via `envFrom`, never a per-key `env:` list (DEBT-05).

### Metrics probe shape (port 9090, `/metrics`)
**Source:** every `deployment-*-worker.yaml`, e.g. `deployment-ocr-worker.yaml:59-79`
**Apply to:** `deployment-pandoc-worker.yaml` — identical `startupProbe`/`readinessProbe`/`livenessProbe` block, byte-for-byte (`cmd/pandoc-worker/main.go:172-184` confirms `METRICS_ADDR` defaults to port 9090, same as every other worker).

### Live-proof gate script skeleton (preflight / KEDA install / EXIT-trap teardown / evidence transcript)
**Source:** `scripts/keda-ocr-loadproof.sh:58-249` and `scripts/keda-ocr-downscale-survival.sh:73-296` (both scripts share this skeleton near-verbatim)
**Apply to:** both new pandoc gate scripts — `set -euo pipefail`, `cd "$(dirname "$0")/.."`, compose-down preflight guard, image-present check, KEDA-resolvable check, `helm install keda` (idempotent upgrade-in-place), apiservice-Available wait, `helm install octoconv -f values-local.yaml [-f values-loadproof.yaml]`, always-on Deployment wait loop, queue-registry seeding via `redis-cli SADD asynq:queues`, `exec > >(tee "$LOG_FILE") 2>&1` transcript capture, `trap teardown EXIT` with process-group-safe watcher kill + `helm uninstall` both releases.

### `assert_eq`/`assert_nonempty`/`assert_nonempty_redacted` helpers
**Source:** `scripts/keda-ocr-loadproof.sh:115-147` (identical in `keda-ocr-downscale-survival.sh:128-156`)
**Apply to:** both new pandoc gate scripts, copied byte-for-byte.

## No Analog Found

None — every file in scope has a direct, recent, structurally-identical analog in the codebase (this phase is explicitly framed in CONTEXT.md as "near-verbatim clone of OCR Phase 46").

## Metadata

**Analog search scope:** `deploy/chart/octoconv/` (all templates + values files), `scripts/keda-*.sh`, `docker-compose.yml` (pandoc-worker service + IN-02 parity blocks), `internal/queue/queue.go`, `cmd/pandoc-worker/main.go`, `scripts/pandoc-timing-measure.sh`
**Files scanned:** ~20 (chart templates, values files, both ocr/document/html ScaledObjects, both ocr loadproof/downscale scripts, configmap.yaml, docker-compose.yml pandoc-worker + api env blocks, queue.go)
**Pattern extraction date:** 2026-08-01
**Key gap vs. ocr precedent:** pandoc has NO committed testdata fixture (unlike ocr's `internal/convert/testdata/ocr/text_sample.png`) — both new scripts must synthesize fixtures at run time (small file for SC3/short-job, ~4 MiB html→md for SC4/long-job), mirroring `scripts/pandoc-timing-measure.sh`'s in-container synthesis pattern rather than ocr's `postJob`-committed-file / av's `pdfunite`-multiplication patterns.
