{{/*
Expand the name of the chart.
*/}}
{{- define "octoconv.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "octoconv.fullname" -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels. Applied to every object in this chart, including the
stateful backbone (postgres/redis/minio). Deliberately does NOT include
octoconv.io/tier — that label is added per-template ONLY on the 5 app
Deployments in plan 02 (see templates/postgres.yaml, redis.yaml, minio.yaml
for the tier-label boundary note). The plan-02 metrics NetworkPolicy
podSelector matches exactly on octoconv.io/tier: app, so keeping it off
these labels is what prevents the default-deny policy from black-holing
5432/6379/9000.
*/}}
{{- define "octoconv.labels" -}}
app.kubernetes.io/name: {{ include "octoconv.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/part-of: octoconv
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels, keyed by a "component" arg (e.g. "postgres", "api",
"worker"). Callers pass a dict with `component` and the root context, e.g.:
  {{- include "octoconv.selectorLabels" (dict "component" "postgres" "root" $) }}
*/}}
{{- define "octoconv.selectorLabels" -}}
app.kubernetes.io/name: {{ include "octoconv.name" .root }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end }}

{{/*
Shared env contract: every Deployment in plan 02 pulls the identical
non-secret + secret env surface from these two objects via envFrom, so the
ConfigMap/Secret in this plan is the single choke point for the env
contract (DEBT-05 — every binary carries the full retry/timeout surface).
*/}}
{{- define "octoconv.commonEnv" -}}
envFrom:
  - configMapRef:
      name: octoconv-config
  - secretRef:
      name: octoconv-secret
{{- end }}

{{/*
Prometheus scrape-config content (D-02/WR-05). Shared by templates/prometheus.yaml's
ConfigMap `data.prometheus.yml` body AND its pod-template `checksum/config`
annotation. prometheus.yaml is a single file holding the Deployment,
ConfigMap, AND Service together — hashing the WHOLE FILE from inside that
same file's pod-template annotation is a self-reference that recurses
infinitely and fails `helm template`/`helm lint`. Hashing only THIS
named-template's rendered content avoids the recursion while still rolling
the Prometheus pod whenever the scrape config actually changes.

Phase 50 (PDCP-02, values-driven scrape_interval): defaults to the same
production 15s via `| default "15s"` — the value is values-driven ONLY so
values-loadproof.yaml can shorten the metric-propagation lag for the pandoc
SC4 downscale gate, whose live job window is seconds-scale
(PANDOC_ENGINE_TIMEOUT=65s). Because this same named-template's rendered
content is what the checksum/config annotation above hashes, changing
.Values.prometheus.scrapeInterval correctly rolls the Prometheus pod too.
*/}}
{{- define "octoconv.prometheusScrapeConfig" -}}
global:
  scrape_interval: {{ .Values.prometheus.scrapeInterval | default "15s" }}
scrape_configs:
  - job_name: octoconv-api
    metrics_path: /metrics
    static_configs:
      - targets:
          - api.{{ .Values.global.namespace }}.svc.cluster.local:9090
{{- end }}

{{/*
Per-class KEDA replica floor (SEED-011/CAPA-01, D-01/D-02/D-03). Callers
pass a dict with `class` (the keda.<class> key, e.g. "image") and `root`
(the root context $), e.g.:
  {{- include "octoconv.kedaMinReplicaCount" (dict "class" "image" "root" $) }}
Resolves the per-class config with `index .root.Values.keda .class` and
emits the raw integer .Values.keda.<class>.minReplicaCount, with NO
surrounding whitespace (callers place it inline after `minReplicaCount: `)
and deliberately NO conditional wrapper (D-01): minReplicaCount is a
required scalar on every ScaledObject whose default is the falsy 0, so
wrapping it in an `if` would reintroduce exactly the HARD-03/D-06 falsy-0
trap that scaleDownStabilizationSeconds needed a hasKey+ne-nil guard to
escape (see scaledobject-av.yaml's FALSY-0 GUARD comment) -- removing the
conditional removes the trap instead of repeating the guard.
Fails fast via `fail` (D-02) rather than rendering an unsafe manifest: a
negative value, or one exceeding that class's own maxReplicaCount, aborts
`helm template`/`helm lint` naming the class and both offending values --
SC3's verification surface is the render itself, not a live KEDA admission
webhook that would only fire at `helm install` time.
*/}}
{{- define "octoconv.kedaMinReplicaCount" -}}
{{- $cfg := index .root.Values.keda .class -}}
{{- $min := int $cfg.minReplicaCount -}}
{{- $max := int $cfg.maxReplicaCount -}}
{{- if lt $min 0 -}}
{{- fail (printf "keda.%s.minReplicaCount (%d) must not be negative" .class $min) -}}
{{- end -}}
{{- if gt $min $max -}}
{{- fail (printf "keda.%s.minReplicaCount (%d) must not exceed keda.%s.maxReplicaCount (%d)" .class $min .class $max) -}}
{{- end -}}
{{- $min -}}
{{- end }}

{{/*
Per-class KEDA fallback floor (SEED-011/CAPA-01, D-05). Same call
convention as octoconv.kedaMinReplicaCount:
  {{- include "octoconv.kedaFallbackReplicas" (dict "class" "image" "root" $) }}
Emits max(1, minReplicaCount) so a sustained Prometheus scaler outage
cannot silently undo an operator-configured floor -- at the shipped
default (a zero floor) this still emits 1, matching today's hardcoded
fallback replica count byte-for-byte (SC1). Deliberately computed
via an `include` of octoconv.kedaMinReplicaCount rather than re-reading
.Values directly: this is what makes D-02's validation fire from the
fallback.replicas call site too, so the guard cannot be bypassed by
reaching this path alone (T-54-02).
*/}}
{{- define "octoconv.kedaFallbackReplicas" -}}
{{- max 1 (int (include "octoconv.kedaMinReplicaCount" .)) -}}
{{- end }}
