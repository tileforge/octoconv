# Phase 54: Per-Class Capacity Floor (SEED-011) - Pattern Map

**Mapped:** 2026-08-06
**Files analyzed:** 10 (2 modified non-template, 8 modified templates/workflow, 1 created)
**Analogs found:** 10 / 10 (this is a chart-only phase; every "new" file has a same-chart sibling to copy from — there is no "no analog" bucket)

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|--------------------|------|-----------|-----------------|----------------|
| `deploy/chart/octoconv/values.yaml` (7× `keda.<class>` blocks) | config | transform (values → template render) | same file, `keda.audio`/`keda.av`/`keda.ocr` blocks (comment register) | exact |
| `deploy/chart/octoconv/templates/_helpers.tpl` (new `define`) | utility (template helper) | transform | `octoconv.commonEnv`, `octoconv.prometheusScrapeConfig` in same file | exact |
| `deploy/chart/octoconv/templates/scaledobject-image.yaml` | config (K8s manifest template) | request-response (rendered manifest → K8s API) | the other 6 `scaledobject-*.yaml` (near-identical) | exact |
| `scaledobject-document.yaml`, `-html.yaml`, `-audio.yaml`, `-av.yaml`, `-ocr.yaml`, `-pandoc.yaml` | config | request-response | `scaledobject-image.yaml` (shortest, no advanced block) | exact |
| `deploy/chart/octoconv/values-capacity-floor.yaml` (new) | config (values overlay) | transform | `values-loadproof.yaml` | exact |
| `.github/workflows/ci.yml` (new `helm` job) | CI job | batch | `gate` job, same file | role-match (gate has no chart step, but is the "short, no services, no cluster" shape) |

## Pattern Assignments

### `deploy/chart/octoconv/templates/_helpers.tpl` — new `define`

**Analog:** `octoconv.commonEnv` and `octoconv.prometheusScrapeConfig`, same file.

**Full existing `define` to copy the idiom from** (`_helpers.tpl:43-55`):
```
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
```

Note the idiom: comment block *above* `{{- define }}` (not inside it) explaining the "why", not just the "what" — matches this chart's project-wide comment convention (long rationale, references phase/decision IDs). `{{- define "name" -}}` chomps whitespace on both sides of the opening tag; `{{- end }}` chomps only the leading side. The body is raw YAML/text emitted verbatim by `include`.

**A `define` that takes a `dict` arg** (the shape D-03's helper needs, since it must receive the class name) — `octoconv.selectorLabels`, `_helpers.tpl:32-41`:
```
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
```
This is the direct analog for D-03: the doc comment states the call convention inline (`dict "key" val "root" $`), and the define's own doc comment is where the call-site contract lives — there's no separate "how to call this" doc elsewhere in the chart. The new `minReplicaCount` define should follow this exact convention, since (unlike `commonEnv`, called with plain `.`) it needs a `dict` carrying at minimum the class key (`"image"`, `"av"`, ...) and the root context (`$`) to reach `.Values.keda.<class>`.

**Real `include ... (dict ...)` call site** (`deployment-api.yaml:23`):
```
      {{- include "octoconv.selectorLabels" (dict "component" "api" "root" $) | nindent 6 }}
```
And `commonEnv`'s no-arg call site (`deployment-api.yaml:36`):
```
          {{- include "octoconv.commonEnv" . | nindent 10 }}
```
These are the two call-site shapes available: bare `.` (context is already the right scope) vs. `(dict ...)` (caller must construct scope, typically because a string key like a component/class name must ride alongside the root `$`). Each `scaledobject-*.yaml` include site will need the `dict` form, passing its own class string plus `$`, e.g. (illustrative, not prescriptive — naming is Claude's Discretion per D-03/CONTEXT.md):
```
  minReplicaCount: {{ include "octoconv.minReplicaCount" (dict "class" "image" "root" $) }}
```

---

### `deploy/chart/octoconv/templates/scaledobject-*.yaml` — the falsy-0 guard NOT being reused (read-only reference)

**Source:** `scaledobject-av.yaml:18-22` (header comment) + `:39` (the guard line).

Header comment block:
```
FALSY-0 GUARD (HARD-03/D-06 fix #1, kept for defensive consistency even
though keda.av.scaleDownStabilizationSeconds is always non-null in
production): the block below is gated on BOTH `hasKey` AND `ne ... nil`,
not `hasKey` alone.
```

The guard line itself:
```
  {{- if and (hasKey .Values.keda.av "scaleDownStabilizationSeconds") (ne .Values.keda.av.scaleDownStabilizationSeconds nil) }}
```

**Do not copy this pattern for `minReplicaCount`** — CONTEXT.md D-01 is explicit that this phase deliberately avoids it: `minReplicaCount`'s template output has no conditional at all, so there is nothing for a falsy `0` to be swallowed by. This guard exists only in `scaledobject-{document,audio,av,ocr,pandoc}.yaml` (5 of 7 — `html` and `image` have no `advanced:` block and thus no guard, see below), gating the *unrelated* `scaleDownStabilizationSeconds`/`advanced.horizontalPodAutoscalerConfig` block. When editing these five files to add the `minReplicaCount` include, the guard block (and everything inside its `{{- if }}...{{- end }}`) must be left untouched and the new `include` line placed outside it, adjacent to `maxReplicaCount`/`pollingInterval`/`cooldownPeriod` (see next section for exact position).

---

### `deploy/chart/octoconv/templates/scaledobject-image.yaml` — full ScaledObject, structural analog for all 7

**Analog:** self (shortest of the 7 — no `advanced:`/guard block, so the position of `minReplicaCount` relative to `spec:` and `fallback:` is easiest to see undistracted).

Full file (`scaledobject-image.yaml:1-49`):
```
{{/*
KEDA ScaledObject — image worker (libvips). Scales the `worker` Deployment
from 0 on the summed octoconv_queue_depth for queue image, counting
pending + active + retry states, via the Prometheus scaler (D-03 —
retry-inclusive, never scheduled/archived. Retry-state tasks are imminent
work; excluding them left a zeroed class blind to a retry backlog until
the reconciler's next sweep, ~12.6 min worst case).

CO-DEPENDENCY GUARD: gated on BOTH keda.enabled AND prometheus.enabled
together, not keda.enabled alone. The trigger's serverAddress points at the
in-chart prometheus Service (templates/prometheus.yaml), which only renders
when prometheus.enabled=true — the `and` guard prevents a ScaledObject from
ever dangling against a Service that was never created.
*/}}
{{- if and .Values.keda.enabled .Values.prometheus.enabled }}
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: worker-image-scaledobject
  namespace: {{ .Values.global.namespace }}
  labels:
    {{- include "octoconv.labels" . | nindent 4 }}
    app.kubernetes.io/component: worker
spec:
  scaleTargetRef:
    name: worker
  minReplicaCount: 0
  maxReplicaCount: {{ .Values.keda.image.maxReplicaCount }}
  pollingInterval: {{ .Values.keda.image.pollingInterval }}
  cooldownPeriod: {{ .Values.keda.image.cooldownPeriod }}
  fallback:
    failureThreshold: 3
    replicas: 1
  triggers:
    - type: prometheus
      metadata:
        serverAddress: http://prometheus.{{ .Values.global.namespace }}.svc.cluster.local:9090
        query: sum(octoconv_queue_depth{queue="image", state=~"pending|active|retry"})
        threshold: {{ .Values.keda.image.threshold | quote }}
        # Sustained absence of a PromQL result (e.g. api unreachable or
        # unscrapeable) now reads as a scaler ERROR, not queue-empty (D-01)
        # — fallback.replicas: 1 holds one replica for this class instead
        # of falsely scaling to zero with a live backlog behind an outage.
        # Trade-off: a benign one-shot fallback blip is possible on fresh
        # install / a short api restart before the first enqueue — accepted,
        # fail-safe toward availability (same philosophy as webhook-worker's
        # fail-closed default).
        ignoreNullValues: "false"
{{- end }}
```

**The two lines this phase touches, in every one of the 7 files:**
- `minReplicaCount: 0` → replace with a one-line `include` (D-03), e.g. `minReplicaCount: {{ include "octoconv.minReplicaCount" (dict "class" "image" "root" $) }}`
- `  fallback:` / `    replicas: 1` → `replicas:` becomes `max(1, minReplicaCount)` via "the same helper family" (D-05) — likely a second `include` call or a second key in the same rendered helper output, e.g. `replicas: {{ include "octoconv.minReplicaCountFallback" (dict "class" "image" "root" $) }}`. Naming/structure (one `define` vs two) is Claude's Discretion per D-03's note.

**Structural safety check across all 7 files — confirmed uniform, safe for a scripted/uniform edit:**

| File | `minReplicaCount` position | `fallback` block present | `advanced`/falsy-0 guard present |
|------|------------------------------|----------------------------|-------------------------------------|
| `scaledobject-image.yaml:27` | directly after `scaleTargetRef.name`, before `maxReplicaCount` | yes (`:31-33`), unconditional | no |
| `scaledobject-document.yaml:45` | same relative position | yes (`:60-62`), unconditional | yes (`:49-59`), between `cooldownPeriod` and `fallback` |
| `scaledobject-html.yaml:24` | same relative position | yes (`:28-30`), unconditional | no |
| `scaledobject-audio.yaml:36` | same relative position | yes (`:51-53`), unconditional | yes (`:40-50`) |
| `scaledobject-av.yaml:35` | same relative position | yes (`:50-52`), unconditional | yes (`:39-49`) |
| `scaledobject-ocr.yaml:35` | same relative position | yes (`:50-52`), unconditional | yes (`:39-49`) |
| `scaledobject-pandoc.yaml:44` | same relative position | yes (`:59-61`), unconditional | yes (`:48-58`) |

No divergence found: every file has exactly one `minReplicaCount: 0` line at the same relative position (immediately after `scaleTargetRef:`'s `name:`, immediately before `maxReplicaCount:`), and exactly one unconditional `fallback:` block with `replicas: 1` at the same relative position (immediately before `triggers:`). The `advanced:`/falsy-0 guard block (present in 5 of 7 — all but `image` and `html`) sits *between* `cooldownPeriod` and `fallback`, i.e. it does not sit between `minReplicaCount` and `maxReplicaCount`, so it does not interfere with editing `minReplicaCount`. A uniform edit strategy (same two line-replacements, same relative anchors) is safe for all 7 files — no file needs special-casing beyond substituting its own class name into the `dict "class" "..."` argument.

---

### `deploy/chart/octoconv/values.yaml` — comment register for `keda.<class>` blocks

**Analog:** `keda.audio` block, `values.yaml:260-269` (chosen over av/ocr as the first occurrence of this comment pattern):
```
  audio:
    threshold: "1"
    maxReplicaCount: 2
    pollingInterval: 15
    cooldownPeriod: 180
    # NON-NULL (unlike document's null above): 742s worst-case exceeds the
    # k8s 300s HPA default, so this is load-bearing in production, not just
    # a load-proof-overlay knob. 900s = 742s + ~21% margin, aligned with the
    # 15m/900s reconciler cap -- a genuine downscale never races a live job.
    scaleDownStabilizationSeconds: 900
```

Pattern to match for the new `minReplicaCount: 0` line in each of the 7 blocks: a `#`-prefixed comment directly above (or beside) the key, in the same "why this value, not a competing value" register the rest of the file uses. Since D-01 sets `minReplicaCount: 0` identically across all seven classes (no per-class divergence in the *value*), the comment doesn't need per-class justification — one comment explaining *why it's explicit* is enough, e.g. anchored on the `image` block (first `keda.*` entry, `values.yaml:244-248`) with a note that every class repeats the same explicit `0`:
```
  image:
    threshold: "5"
    maxReplicaCount: 4
    pollingInterval: 5
    cooldownPeriod: 60
    # minReplicaCount: 0 explicit here and on all 7 classes (SEED-011/D-01)
    # — the template renders this value directly with NO conditional
    # wrapper (unlike scaleDownStabilizationSeconds's hasKey/ne-nil guard
    # below), so a bare key with no comment would look like a missing
    # feature rather than a deliberate default. Operators override per
    # class via values-capacity-floor.yaml.
    minReplicaCount: 0
```
The file-level header (`values.yaml:1-6`, `"This is the COMPLETE per-service value contract..."`) and the `keda:` block header (`values.yaml:222-241`, explaining the co-dependency guard and the `WR-06` cooldown invariant) are further examples of this register — long, decision-ID-anchored, explaining "why not the alternative."

---

### `deploy/chart/octoconv/values-capacity-floor.yaml` (new) — overlay structure

**Analog:** `values-loadproof.yaml`, full file (`values-loadproof.yaml:1-121`, excerpted for structure).

Header comment convention (`:1-11`), states the exact `helm install`/`upgrade -f` invocation and layering order:
```
# Load-proof overlay — layered ON TOP of values-local.yaml, NEVER standalone
# (same convention as values-e2e.yaml):
#
#   helm install octoconv deploy/chart/octoconv \
#     -f deploy/chart/octoconv/values-local.yaml \
#     -f deploy/chart/octoconv/values-loadproof.yaml -n octoconv
#
# Sets the gate-only knobs needed for the live 0→N→0 load-proof gates
# (plans 28-02/28-03, 46-02/46-03, 50-02/50-03/50-04) to have deterministic
# timing; all are dev/test-only and are never applied to a production
# values file.
```

Partial `keda:` override for a single class, with rationale comment tied to production value it's overriding (`:13-22`):
```
keda:
  document:
    # Fast, deterministic 2->1 downscale (Phase 28 D-07/D-08, RESEARCH.md
    # Pitfall 1). cooldownPeriod (values.yaml) governs only the 1->0
    # transition; the standard-HPA-governed N->N-1 step defaults to a 300s
    # stabilization window that would push the SC3 downscale past the long
    # document job's own DOCUMENT_ENGINE_TIMEOUT=300s and silently
    # invalidate the scenario's timing assumptions. 15s makes the downscale
    # observable within the gate's polling window.
    scaleDownStabilizationSeconds: 15
```

For `values-capacity-floor.yaml` (D-04: demonstrates SC3's "example non-zero override" for exactly one class, distinct from `values-loadproof.yaml` because that gate proves scale-*from*-zero and a floor would defeat it), the direct structural template is: header comment stating this is a standalone example (simpler than the `-f values-local.yaml -f ...` layering, since this overlay isn't meant to run a live gate — confirm exact usage instructions against what D-06's CI job actually invokes), then a single `keda.<class>.minReplicaCount: N` key with a rationale comment in the same register — e.g., picking `av` or `ocr` (both already carry "job duration exceeds k8s defaults" framing that pairs naturally with "worth holding a warm replica"). Choice of class and value is Claude's Discretion per CONTEXT.md.

Also note `values-loadproof.yaml:46-72` (the `pandoc` block) is a second, more elaborate example of a single-class partial override with an explicit `pollingInterval` sibling key — useful if the new overlay needs more than one key.

---

### `.github/workflows/ci.yml` — new `helm` job

**Analog:** `gate` job, same file, `ci.yml:13-25` (chosen per the brief: short, no external services, no cluster):
```
  # Tier 1: fast gate — gofmt/vet/build/test, no external services.
  # internal/e2e self-skips here (E2E_BASE_URL unset), keeping this tier
  # offline-safe (SC1).
  gate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
          cache: true
          cache-dependency-path: go.sum
      - run: test -z "$(gofmt -l .)"
      - run: go vet ./...
      - run: go build ./...
      - run: go test ./... -timeout 5m
```

Key conventions to match:
- `runs-on: ubuntu-latest` — every job in this file uses this runner, no self-hosted/matrix.
- `uses: actions/checkout@v7` — pinned major version, first step of every job.
- Each job has a `# Tier N: <one-line purpose>` comment directly above the job key, in the same register as the `gate`/`race`/`docker-build`/`e2e` comments — the new `helm` job should follow suit (e.g. `# Chart validation: helm lint + helm template + diff isolation (SC2/SC3, D-06). No cluster, no services.`).
- No `needs:` on `gate` (it's the first tier); the new `helm` job is chart-only and independent of the Go tiers, so it likely has no `needs:` either (or `needs: gate` if sequencing after the fastest tier is preferred — Claude's Discretion per CONTEXT.md "Placement... within `ci.yml`, and whether it runs on all pushes or only when `deploy/chart/**` changes").
- Since the `helm` job needs no Go toolchain, `actions/setup-go@v6` is **not** an analog dependency — it will instead need a `helm`-install step. No existing job in this file installs `helm`; there is no in-repo analog for that specific step, so the executor should reach for the standard `azure/setup-helm` action (not present in this file — flagged below as a gap, not a contradiction).
- `concurrency:` at file level (`ci.yml:5-7`) already cancels in-progress runs per ref — applies automatically to the new job, no per-job change needed.
- `on: [push, pull_request]` (`ci.yml:3`) is file-wide; if D-06's discretion point ("only when `deploy/chart/**` changes") is exercised, it would need a `paths:` filter — note this file currently has **no** path-filtered trigger anywhere (the trigger block is a single unconditional `[push, pull_request]` list), so a path-filtered job would be a first for this file. A simpler, more consistent-with-existing-style option is an unconditional job (like all 4 existing ones) that just runs fast (no cluster = seconds, per D-06's own rationale) rather than introducing this file's first path filter.

---

## Shared Patterns

### Whitespace-chomping / comment convention in `_helpers.tpl`
**Source:** `_helpers.tpl`, all 6 existing `define`s.
**Apply to:** the new D-03 `define`.
```
{{/*
<rationale comment, references phase/decision IDs, explains "why">
*/}}
{{- define "octoconv.<name>" -}}
<body>
{{- end }}
```
The doc comment always sits *above* `{{- define }}`, never inside it. `{{- define ... -}}` chomps both sides (no leading/trailing blank line in rendered output); `{{- end }}` chomps only its own leading whitespace.

### CO-DEPENDENCY GUARD comment
**Source:** every `scaledobject-*.yaml`, e.g. `scaledobject-html.yaml:9-10`.
**Apply to:** no new file needs this guard added — it already wraps every ScaledObject (`{{- if and .Values.keda.enabled .Values.prometheus.enabled }}`) — but D-06's CI job must set **both** `--set keda.enabled=true --set prometheus.enabled=true` or every `helm template` assertion passes vacuously (explicitly called out in CONTEXT.md `<specifics>`).

### Falsy-0 guard (`HARD-03/D-06`)
**Source:** `scaledobject-av.yaml:39`, `scaledobject-document.yaml:49`, `scaledobject-audio.yaml:40`, `scaledobject-ocr.yaml:39`, `scaledobject-pandoc.yaml:48` — all identical shape, substituting the class name:
```
{{- if and (hasKey .Values.keda.<class> "scaleDownStabilizationSeconds") (ne .Values.keda.<class>.scaleDownStabilizationSeconds nil) }}
```
**Apply to:** nothing new in this phase — read-only reference per D-01's explicit rejection of this pattern for `minReplicaCount`. Preserve verbatim while editing these 5 files for the unrelated `minReplicaCount`/`fallback` changes.

### Per-class values.yaml divergence comments
**Source:** `values.yaml:265-269` (`audio`), `:275-281` (`av`), `:287-293` (`ocr`), `:311-319` (`pandoc`).
**Apply to:** the new `minReplicaCount: 0` line in all 7 `keda.<class>` blocks (see values.yaml section above) — bare uncommented keys are inconsistent with this file's established style.

---

## No Analog Found

None — every file in this phase's scope has a close, same-chart analog. The one partial gap (not a missing analog, but a missing *action*) is the `helm` CLI setup step for the new CI job: no existing job in `ci.yml` installs `helm`, so that one step has no in-repo precedent (flagged above under the `ci.yml` section).

## Metadata

**Analog search scope:** `deploy/chart/octoconv/` (all templates + values + overlays), `.github/workflows/ci.yml`
**Files scanned:** 9 `scaledobject-*.yaml`-adjacent template files read directly (7 ScaledObjects + `_helpers.tpl` + 3 Deployment files grepped for `include` call sites), `values.yaml`, `values-loadproof.yaml`, `values-e2e.yaml`, `ci.yml`
**Pattern extraction date:** 2026-08-06
