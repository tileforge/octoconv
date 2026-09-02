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
value that is not a whole number, a negative value, or one exceeding that
class's own maxReplicaCount aborts the render naming the class and the
offending value(s) -- SC3's verification surface is the render itself, not
a live KEDA admission webhook that would only fire at `helm install` time.

WHAT "ABORTS" MEANS HERE, precisely (WR-01): `fail` aborts `helm template`,
`helm install` and `helm upgrade` with a non-zero exit. It does NOT abort
`helm lint` -- helm v4.2.3 downgrades a template `fail` to an INFO log line
(`level=INFO msg="funcMap fail"`) and still reports `0 chart(s) failed`,
exit 0. So `helm lint` is deliberately NOT part of this guard's verification
surface, and the CI job's D-02 negative cases use `helm template` only.

WHY THE RAW KIND IS VALIDATED BEFORE ANY `int` COERCION (CR-01): sprig's
`int` is cast.ToInt, which discards its parse error and returns 0. Helm's
`--set` strvals parser only produces a Go int64 for a bare base-10 integer;
`2.0`, `2.9`, `99.9`, `-0.5`, `abc`, `" 3"` and an empty value all arrive as
a string, and `true`/`false` as a bool. Coercing first therefore made BOTH
guards below unreachable for every one of those inputs and handed the
operator a silent floor of 0 -- i.e. no warm floor at all, with the render
exiting 0 -- which is precisely the silent-bug class this phase exists to
close. Checking `kindOf` on the RAW value first is what keeps the two guards
reachable. maxReplicaCount is validated the same way so an absent or null
ceiling (kind "invalid") is reported as its own misconfiguration rather than
being coerced to 0 and blamed on minReplicaCount.
*/}}
{{- define "octoconv.kedaMinReplicaCount" -}}
{{- $cfg := index .root.Values.keda .class -}}
{{- $rawMin := $cfg.minReplicaCount -}}
{{- $rawMax := $cfg.maxReplicaCount -}}
{{/* A values.yaml/-f number arrives as float64 (helm parses YAML via JSON), a
     bare `--set` integer as int64; anything else is NOT a usable integer. A
     float64 additionally has to be integral -- 2.9 must not become 2. */}}
{{- $minIsWhole := or (kindIs "int" $rawMin) (kindIs "int64" $rawMin) -}}
{{- if kindIs "float64" $rawMin -}}
{{- $minIsWhole = eq $rawMin (floor $rawMin) -}}
{{- end -}}
{{- if not $minIsWhole -}}
{{- fail (printf "keda.%s.%s must be a whole number, got %#v (kind %s) -- helm's --set parser yields a string for anything that is not a bare base-10 integer (2.0, 2.9, abc, an empty value) and a bool for true/false, all of which sprig's `int` would silently coerce to 0, discarding the configured floor" .class "minReplicaCount" $rawMin (kindOf $rawMin)) -}}
{{- end -}}
{{- $maxIsWhole := or (kindIs "int" $rawMax) (kindIs "int64" $rawMax) -}}
{{- if kindIs "float64" $rawMax -}}
{{- $maxIsWhole = eq $rawMax (floor $rawMax) -}}
{{- end -}}
{{- if not $maxIsWhole -}}
{{- fail (printf "keda.%s.%s must be a whole number, got %#v (kind %s) -- helm's --set parser yields a string for anything that is not a bare base-10 integer (2.0, 2.9, abc, an empty value) and a bool for true/false, all of which sprig's `int` would silently coerce to 0, discarding the configured floor" .class "maxReplicaCount" $rawMax (kindOf $rawMax)) -}}
{{- end -}}
{{- $min := int $rawMin -}}
{{- $max := int $rawMax -}}
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

{{/*
octoconv.waitForPostgres — an initContainer that blocks until postgres accepts
connections.

WHY AN INIT CONTAINER AND NOT A RETRY IN THE BINARY. Every Go entrypoint in
this project treats a startup dependency failure as fatal, and that convention
is worth keeping: "fatal means fatal" is what makes a non-zero restartCount a
signal rather than noise. A bounded retry inside main() would weaken it in ten
binaries to solve an ordering problem that belongs to the orchestrator.

WHAT IT FIXES. A fresh `helm upgrade --install` brings pods up before postgres
accepts connections; each one exits(1), kubelet restarts it, and the pod reaches
restartCount 2-3 before it is ever Ready. That is not free: it pays
CrashLoopBackOff's exponential delay on every cold install, and it burns the
metric this project's own live gates read as ground truth. A restartCount that
is routinely 2-3 for benign reasons is one nobody will trust at 4.

APPLIED TO EVERY POD THAT NEEDS POSTGRES, not just api. All ten entrypoints
have the same log.Fatalf shape; fixing the one that was reported and leaving
nine identical instances is the defect pattern the previous milestone spent
eight review rounds learning to stop repeating.

Uses the same pinned postgres image the chart already deploys, so this adds no
new supply-chain surface: pg_isready ships in it.
*/}}
{{- define "octoconv.waitForPostgres" -}}
initContainers:
  - name: wait-for-postgres
    image: {{ .Values.postgres.image | quote }}
    imagePullPolicy: IfNotPresent
    command:
      - sh
      - -c
      - |
        until pg_isready -h postgres.{{ .Values.global.namespace }}.svc.cluster.local -p 5432 -U {{ .Values.postgres.user }} >/dev/null 2>&1; do
          echo "waiting for postgres..."
          sleep 2
        done
        echo "postgres is accepting connections"
    resources:
      limits:
        cpu: "100m"
        memory: "64Mi"
{{- end }}
