---
phase: 50-keda-helm-chart-integration
reviewed: 2026-08-02T00:00:00Z
depth: standard
files_reviewed: 9
files_reviewed_list:
  - deploy/chart/octoconv/templates/_helpers.tpl
  - deploy/chart/octoconv/templates/configmap.yaml
  - deploy/chart/octoconv/templates/deployment-pandoc-worker.yaml
  - deploy/chart/octoconv/templates/scaledobject-pandoc.yaml
  - deploy/chart/octoconv/values-loadproof.yaml
  - deploy/chart/octoconv/values.yaml
  - docker-compose.yml
  - scripts/keda-pandoc-downscale-survival.sh
  - scripts/keda-pandoc-loadproof.sh
findings:
  critical: 2
  warning: 12
  info: 7
  total: 21
status: issues_found
---

# Phase 50: Code Review Report

**Reviewed:** 2026-08-02
**Depth:** standard
**Files Reviewed:** 9
**Status:** issues_found

## Summary

Phase 50 adds the pandoc engine class to the Helm chart (Deployment, KEDA
ScaledObject, values, ConfigMap env parity), a load-proof overlay, and two
live-gate bash harnesses; `docker-compose.yml` only lowers
`PANDOC_MAX_INPUT_BYTES` 4 MiB -> 3 MiB.

**What verifiably holds up** (checked, not assumed):

- Env parity between the chart ConfigMap and the compose `pandoc-worker`
  block is byte-exact for all four pandoc keys: `PANDOC_WORKER_CONCURRENCY=2`,
  `PANDOC_MAX_INPUT_BYTES=3145728`, `PANDOC_ENGINE_TIMEOUT=65s`,
  `PANDOC_MAX_RETRY=2`. All four names match what `cmd/pandoc-worker/main.go`
  and `internal/queue/client.go` actually read.
- The falsy-0 guard in `scaledobject-pandoc.yaml` was verified by rendering,
  not by reading: with `values-loadproof.yaml` it emits
  `stabilizationWindowSeconds: 0`; without it the whole `advanced:` block is
  omitted (3 non-null siblings render, pandoc and document do not).
- The PromQL trigger metric/labels match `internal/metrics/queue_collector.go`
  (`octoconv_queue_depth`) and the sibling `scaledobject-ocr.yaml` shape.
- `octoconv.io/tier: app` on the pod template genuinely places pandoc-worker
  inside the `networkpolicy-metrics.yaml` podSelector — no netpol edit needed,
  as claimed.
- `terminationGracePeriodSeconds: 85` is consistent with the code:
  `cmd/pandoc-worker/main.go:161` sets `ShutdownTimeout = engineTimeout + 10s`
  = 75s, leaving a 10s SIGKILL margin.
- No secret leakage into committed transcripts: `manage-clients` prints the
  raw key on **stdout** (captured by `$( )`, never teed), `CLIENT_KEY` only
  reaches the transcript via `assert_nonempty_redacted`, and a grep of all 11
  committed `evidence/*.log` files for key/salt/password patterns returns
  nothing.
- Prometheus `scrapeInterval: 5s` is safe despite the default 10s
  `scrape_timeout` — Prometheus clamps an unset global scrape_timeout down to
  the interval rather than erroring.

**Key concerns:** the production memory budget for pandoc (3 MiB cap x
concurrency 2 at a 1Gi limit) has never been validated at production
concurrency — the only live 3 MiB evidence comes from a gate that pins
concurrency to 1 — and the project's own measured OOM cliff puts that
production combination in the thrash-to-OOM band. Separately, the SC4 gate's
central "SIGTERM before completion" assertion passes only because BSD
`strptime` is lenient about a timestamp the script itself corrupts; the
documented GNU fallback can never parse it. The rest is harness hardening:
argument handling, tool preflight, temp-file hygiene, resource cleanup on
failure paths, and a sizeable amount of stale 4 MiB documentation left behind
by the Path-B cap change.

## Narrative Findings (AI reviewer)

### Critical Issues

#### CR-01: Production pandoc memory budget (3 MiB cap x concurrency 2 at 1Gi) is unvalidated and sits in the project's own measured OOM band

**File:** `deploy/chart/octoconv/templates/configmap.yaml:79-88`,
`deploy/chart/octoconv/values.yaml:121-129`, `docker-compose.yml`
(pandoc-worker env block)
**Severity:** BLOCKER

**Issue:** The ConfigMap ships `PANDOC_WORKER_CONCURRENCY: "2"` and
`PANDOC_MAX_INPUT_BYTES: "3145728"` against `pandocWorker.resources.limits.memory: "1Gi"`.
The comment block itself states the reason for the cap change: *"a ~4.09 MB
html->md conversion OOMKilled the real worker container (Go worker + pandoc
share the cgroup; concurrency 2 doubles exposure)."* The compose comment
records the measured cliff at the same 1g limit: *"4 MiB=~10.2s clean -> 6.5
MiB=~40s thrash -> 8 MiB=OOM."*

Two concurrent 3 MiB conversions present ~6 MiB of aggregate input to a single
1Gi cgroup — squarely inside the measured "thrash" band and one input-size
step from the measured OOM point. The only live evidence that 3 MiB survives
is `scripts/keda-pandoc-downscale-survival.sh`, and that gate installs
`values-loadproof.yaml`, which pins `pandocWorker.extraEnv.PANDOC_WORKER_CONCURRENCY: "1"`
(values-loadproof.yaml:109-119). The SC3 gate does run production tuning
(concurrency 2) but submits a ~4 KB fixture, so it exercises nothing.

Net: the exact configuration that ships (2 x 3 MiB in one 1Gi cgroup) has
never been run. The failure mode is an OOMKill / exit 137 mid-conversion —
precisely the condition the phase's own SC4 assertion treats as a hard failure
(`keda-pandoc-downscale-survival.sh:1044-1047`) — which costs the in-flight job
a retry and, at `PANDOC_MAX_RETRY=2`, can burn the whole retry budget.

**Fix:** Pick one and prove it before shipping:

```yaml
# Option A (safest, matches the only validated configuration):
# templates/configmap.yaml + docker-compose.yml
PANDOC_WORKER_CONCURRENCY: "1"

# Option B: keep concurrency 2, raise the ceiling and re-measure
# values.yaml pandocWorker.resources.limits.memory: "2Gi"
```

Option C is to extend the SC4 gate (or a compose-level measurement) to submit
two concurrent cap-sized jobs to a single pod at concurrency 2 and assert no
exit 137, then keep the current values with real evidence behind them.

#### CR-02: SC4(3) ordering assertion compares a corrupted timestamp and passes only by accident of BSD strptime leniency

**File:** `scripts/keda-pandoc-downscale-survival.sh:1010-1011`, `:1032-1042`
**Severity:** BLOCKER

**Issue:** `JOB_FINISHED_AT` is read with
`psql ... | tr -d '[:space:]'`. psql renders a timestamptz as
`2026-08-01 21:06:36.104457+00`; `tr -d '[:space:]'` deletes **all**
whitespace including the date/time separator, producing
`2026-08-0121:06:36.104457+00`. The committed evidence confirms this verbatim:

```
job_finished_at=2026-08-0121:06:36.104457+00
```
(`.planning/phases/50-keda-helm-chart-integration/evidence/sc4-pandoc-downscale-survival-20260801T210017Z.txt`)

Line 1032 then does:

```bash
JOB_FINISHED_EPOCH=$(date -u -j -f "%Y-%m-%d %H:%M:%S" "${JOB_FINISHED_AT%%.*}" +%s 2>/dev/null \
  || date -u -d "$JOB_FINISHED_AT" +%s 2>/dev/null || echo "0")
```

The format string contains a space; the input does not. It only works because
macOS/BSD `strptime` tolerates the mismatch (verified locally:
`date -u -j -f "%Y-%m-%d %H:%M:%S" "2026-08-0121:06:36" +%s` returns the
correct `1785618396`). The documented GNU fallback is dead code on this path —
on Linux the first branch fails (no `-j`), and `date -u -d "2026-08-0121:06:36.104457+00"`
rejects the mangled string, yielding `0` and a hard
`FAIL: SC4(3) -- could not parse ...`. The load-bearing
`SIGTERM_EPOCH < JOB_FINISHED_EPOCH` proof is therefore both
platform-accidental and emits a corrupted value into the committed evidence
artifact.

**Fix:** Do not strip internal whitespace from a timestamp; trim only the ends,
and normalize to RFC3339 in SQL so both `date` dialects parse it:

```bash
JOB_FINISHED_AT=$(psql "..." -tAc \
  "SELECT to_char(finished_at AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"') \
   FROM jobs WHERE id='${INFLIGHT_JOB_ID}';" | tr -d '[:space:]')
# now reuse the existing helper, which already handles both dialects:
JOB_FINISHED_EPOCH=$(toEpoch "$JOB_FINISHED_AT")
```

### Warnings

#### WR-01: Unknown CLI arguments silently run the full destructive cluster gate

**File:** `scripts/keda-pandoc-downscale-survival.sh:112-115`

**Issue:** Only an exact `--calibrate-only` in `$1` selects the offline mode.
Any typo (`--calibrate`, `-calibrate-only`, `--calibrate_only`) or stray
argument falls through and runs the full gate: `helm install` KEDA into the
cluster, `helm install` octoconv, port-forwards, job submission, then
`helm uninstall` of **both** releases in the EXIT trap. An operator reaching
for the cheap offline mode gets a cluster wipe-and-reinstall instead.

**Fix:**
```bash
CALIBRATE_ONLY=""
case "${1:-}" in
	--calibrate-only) CALIBRATE_ONLY="1" ;;
	"") ;;
	*) echo "usage: $0 [--calibrate-only]" >&2; exit 2 ;;
esac
```

#### WR-02: The attribution poller injects processes into the very cgroup whose memory headroom is under test

**File:** `scripts/keda-pandoc-downscale-survival.sh:848-855`

**Issue:** `attributionPoller` runs `kubectl exec -n "$NAMESPACE" "$VICTIM_POD" -- sh -c 'cat /proc/[0-9]*/cmdline ...'`
every 2 seconds for the entire observation window. Each exec spawns a `sh` plus
a `cat` **inside the pandoc-worker container**, i.e. inside the same 1Gi cgroup
that the Go worker and pandoc share — the exact shared-cgroup accounting that
CR-01/50-04 identified as the cause of the observed exit-137. The measurement
apparatus perturbs the property being measured, and it plausibly contributed
to the OOM that forced the cap from 4 MiB down to 3 MiB.

**Fix:** Reduce the footprint and the cadence, and record it in the evidence
file so the observer effect is at least accounted for:
```bash
# poll every 5s instead of 2s, and avoid the extra `sh` layer:
kubectl exec -n "$NAMESPACE" "$VICTIM_POD" -- cat /proc/self/../[0-9]*/cmdline ...
```
Better: derive attribution from the worker's own structured output (job id in
the pod log) rather than exec'ing into the memory-constrained container.

#### WR-03: `--calibrate-only` measures pandoc alone and therefore cannot detect the failure mode it is meant to guard

**File:** `scripts/keda-pandoc-downscale-survival.sh:313-388`

**Issue:** `runCalibration` starts a container from the worker image with
`--entrypoint sleep`, then times a bare `pandoc` invocation at `--cpus 2 --memory 1g`.
It asserts only a wall-clock band `[15, 50]s`. There is no memory assertion at
all. The script's own comments concede the gap: *"the pandoc-alone calibration
could not see"* the Go worker + pandoc shared-cgroup footprint. So the gate
that is supposed to make the job window "measured, not assumed" still leaves
the memory dimension entirely assumed — and memory, not time, is what actually
broke (exit 137).

**Fix:** Add a peak-memory assertion to the calibration, e.g. read the
container's cgroup peak after the run and fail if it exceeds a headroom
fraction of the 1Gi limit:
```bash
peak=$(docker exec "$FIXTURE_CONTAINER" cat /sys/fs/cgroup/memory.peak 2>/dev/null || echo 0)
# fail if peak > 60% of 1Gi, leaving room for the Go worker sharing the cgroup
```

#### WR-04: Stale 4 MiB documentation throughout both gate scripts after the cap moved to 3 MiB

**File:** `scripts/keda-pandoc-downscale-survival.sh:100-101`, `:236-246`,
`:286-290`, `:327-330`, `:699-707`; `scripts/keda-pandoc-loadproof.sh:24`,
`:488-491`, `:502`

**Issue:** The constant is `PANDOC_MAX_INPUT_BYTES=3145728` (line 156), but the
surrounding prose still describes a 4 MiB world:
- header: *"the fixture is capped at the production 4 MiB PANDOC_MAX_INPUT_BYTES"*
- `synthesizeLongFixture` seed rationale: *"Seeded AGGRESSIVELY close to the 4 MiB ceiling"*,
  *"converges the render close to [3800000, 4194304] by attempt 2"* — that band
  is now entirely **above** the hard cap and above `FIXTURE_BAND_LOW=2200000`.
- the shrink-margin comment cites observed values `4584504 -> 4194489 -> 4194323`
  that can no longer occur.
- `runCalibration`: *"Path-B lowered PANDOC_MAX_INPUT_BYTES to 4 MiB"*.
- STEP 7 banner and `log` line: *"synthesize LONG (~4 MiB html) fixture"*.

Anyone re-tuning the fixture from these comments will target a size the worker
now rejects.

**Fix:** Update every 4 MiB / 4194304 / 3800000 reference to the current
3 MiB / 3145728 / 2200000 values, and re-derive the documented seed rationale
against the actual `md_target=3000000` seed.

#### WR-05: STEP 11's timing rationale contradicts the overlay the script installs

**File:** `scripts/keda-pandoc-downscale-survival.sh:870-876`

**Issue:** The comment reads *"with stabilizationWindowSeconds:15, pollingInterval: 3,
and prometheus.scrapeInterval:5s from the loadproof overlay"*. The overlay this
script installs sets `keda.pandoc.scaleDownStabilizationSeconds: 0`
(`values-loadproof.yaml:72`), not 15 — and that 0 is the whole point of the
falsy-0 guard the overlay is documented to exercise. The stated latency budget
for the 180s downscale window is therefore derived from the wrong number.

**Fix:** Change `stabilizationWindowSeconds:15` to `stabilizationWindowSeconds:0`
and re-check that the 180s `DOWNSCALE_WINDOW` derivation still reads correctly.

#### WR-06: Victim and survivor pods can resolve to the same pod, and the "newer pod" premise is unsupported

**File:** `scripts/keda-pandoc-downscale-survival.sh:787-792`

**Issue:** Two problems in the same three lines:

1. `VICTIM_POD` takes `.[-1]` and `SURVIVOR_POD` takes `.[0]` of the same
   filtered array. If that array has exactly one element — entirely possible if
   one of the two pods acquires a `deletionTimestamp` between
   `waitForNReplicasReady` returning and `PODS_JSON` being fetched — both
   variables get the **same** pod name and both `assert_nonempty` checks pass.
   The gate then annotates the "survivor" as the deletion victim and asserts
   collateral-freedom against a pod that no longer exists.
2. `sort_by(.metadata.creationTimestamp)` on a 0->2 scale-up: k8s
   `creationTimestamp` has one-second resolution and both pods are created by
   the same ReplicaSet sync, so they routinely share a timestamp. `sort_by` is
   then decided by the API server's (name-ordered) list order, not by age. The
   comment's premise — *"the last one to pick up work, so it has the most
   remaining job time"* — is not something this data can establish.

**Fix:**
```bash
assert_nonempty "$VICTIM_POD" "..."
assert_nonempty "$SURVIVOR_POD" "..."
if [ "$VICTIM_POD" = "$SURVIVOR_POD" ]; then
	echo "FAIL: SC4 -- victim and survivor resolved to the same pod ($VICTIM_POD); only one non-terminating replica was observed" >&2
	exit 1
fi
```
and soften the comment to state that victim selection is driven by the
`pod-deletion-cost` annotation, with creationTimestamp only breaking ties.

#### WR-07: No preflight for required host tooling or for the values files

**File:** `scripts/keda-pandoc-downscale-survival.sh:477-506`,
`scripts/keda-pandoc-loadproof.sh:233-262`

**Issue:** STEP 1 checks kubectl reachability, compose-down, the worker image,
and the KEDA chart version — but not the host tools the run depends on:
- `jq` (downscale, `waitForNReplicasReady:761-778`): absent `jq` is swallowed
  by `2>/dev/null || echo "0"`, so the gate burns the full 240s and reports the
  misleading *"pandoc-worker never reached 2 Ready replicas"* instead of
  "install jq".
- `psql` (downscale, `batchDepth:745-748`, `:959`, `:1010`): absent `psql`
  aborts under `set -e` only **after** KEDA and the entire chart are installed
  and jobs are in flight.
- Neither script checks `[ -f "$VALUES_LOCAL" ]` / `[ -f "$VALUES_LOADPROOF" ]`,
  though `values-local.yaml` is gitignored and thus absent on a fresh clone —
  the failure surfaces mid-`helm install`, after KEDA is already deployed.

**Fix:**
```bash
for tool in kubectl helm docker jq psql curl go awk; do
	command -v "$tool" >/dev/null 2>&1 || { echo "FAIL: required tool '$tool' not found in PATH" >&2; exit 1; }
done
for f in "$VALUES_LOCAL" "$VALUES_LOADPROOF"; do
	[ -f "$f" ] || { echo "FAIL: values file $f not found (values-local.yaml is gitignored -- create it first)" >&2; exit 1; }
done
```

#### WR-08: Fixed, predictable filenames in /tmp

**File:** `scripts/keda-pandoc-loadproof.sh:338`, `:340`, `:347`, `:379`, `:679`;
`scripts/keda-pandoc-downscale-survival.sh:576`, `:578`, `:585`

**Issue:** Both scripts write to hardcoded paths in a world-writable directory:
`/tmp/keda-pandoc-loadproof-{api,db}-pf.log`, `/tmp/keda-pandoc-loadproof-healthz.json`,
`/tmp/keda-pandoc-loadproof-job.json`, `/tmp/keda-pandoc-loadproof-post-${filename}.json`,
and the downscale equivalents. On a multi-user host a pre-planted symlink at any
of these paths redirects the shell's `>` truncate/write to an arbitrary
attacker-chosen file with the operator's privileges. It also makes two
concurrent runs of the same script clobber each other's status files — which
matters, because the whole point of the distinct port allocations
(18097/15441 vs 18098/15442, documented at `:105` and `:136`) is to permit
concurrent runs. Note the downscale script already hardened *one* of these
(`postJobPath` adds `-$$-${RANDOM}`, line 613) but left the rest.

**Fix:** Route every scratch file through the already-created `mktemp -d`
workdir (or a dedicated one created before STEP 1, since `$WORKDIR` currently
does not exist yet at port-forward time):
```bash
RUNTMP=$(mktemp -d)
kubectl port-forward ... >"$RUNTMP/api-pf.log" 2>&1 &
```

#### WR-09: Temp workdirs leak on every failure path

**File:** `scripts/keda-pandoc-loadproof.sh:496`/`:704`,
`scripts/keda-pandoc-downscale-survival.sh:709`/`:1072`, `:322-323`/`:387`

**Issue:** `WORKDIR=$(mktemp -d)` is cleaned by `rm -rf "$WORKDIR"` only on the
final happy path. Every assertion failure exits through the EXIT trap, which
cleans the fixture container, port-forwards, and helm releases — but never the
workdir. Each leaked directory holds a ~3 MiB HTML fixture; the committed
evidence directory shows eight downscale runs, most of which did not reach the
happy path. `runCalibration`'s `cal_workdir` (line 323) has the same shape: the
`rm -rf` at line 387 is skipped whenever the band assertion at :357 exits.

**Fix:** Declare `WORKDIR=""` alongside the other teardown-visible globals and
clean it in `teardown()`:
```bash
[ -n "$WORKDIR" ] && rm -rf "$WORKDIR"
```

#### WR-10: Asymmetric fixture resize can overshoot the cap and exhaust all three attempts

**File:** `scripts/keda-pandoc-downscale-survival.sh:280-294`

**Issue:** The overshoot branch scales proportionally with a fixed 100 KB
margin, which the comment correctly explains. The undershoot branch instead
**doubles** `md_target`. From a render just below `FIXTURE_BAND_LOW`
(e.g. 2,150,000 bytes), doubling jumps the target to roughly 4.3 MB of HTML —
far above the 3,145,728 cap — spending attempt 3 on a shrink that may or may
not land inside the narrow `[2200000, 3145728]` band. Failure here occurs at
STEP 7, i.e. after KEDA and the whole chart are already installed and the
cluster time is spent.

**Fix:** Use the same proportional formula in both directions; it converges from
below as well as from above:
```bash
md_target=$((md_target * (PANDOC_MAX_INPUT_BYTES - 100000) / html_bytes))
```

#### WR-11: ScaledObject hardcodes `metadata.namespace` from values while its scale target is release-namespaced

**File:** `deploy/chart/octoconv/templates/scaledobject-pandoc.yaml:37`

**Issue:** The ScaledObject pins `namespace: {{ .Values.global.namespace }}`
("octoconv"), while `deployment-pandoc-worker.yaml` (like every sibling
Deployment) carries no explicit namespace and therefore lands in the Helm
release namespace. `helm install octoconv ./chart -n staging` puts the
Deployment in `staging` and the ScaledObject in `octoconv`; KEDA then resolves
`scaleTargetRef: pandoc-worker` inside `octoconv`, finds nothing (or worse, a
same-named workload from another release), and pandoc-worker silently never
autoscales — no error surfaces at install time. This mirrors
`scaledobject-ocr.yaml:28` and the other siblings, so it is a replicated
pre-existing defect rather than a newly invented one, but Phase 50 replicates
it once more.

**Fix:** Drop the explicit namespace (let Helm's `-n` govern) or template it
from the release:
```yaml
  namespace: {{ .Release.Namespace }}
```

#### WR-12: Job IDs interpolated directly into SQL executed against the live database

**File:** `scripts/keda-pandoc-downscale-survival.sh:742-748`, `:959-960`, `:1010-1011`

**Issue:** `BATCH_IDS_SQL` is assembled by `printf "'%s',"` over API-returned
job ids and spliced into `SELECT ... WHERE id IN ${BATCH_IDS_SQL}`;
`INFLIGHT_JOB_ID` is likewise spliced into two `WHERE id='${...}'` clauses. The
values are currently constrained (`grep -o '"job_id":"[^"]*"'` excludes `"`,
and the attribution regex only admits hex/hyphens), so this is not exploitable
today — but the `psql` connection carries write-capable credentials, and a
single change to the API response shape or the extraction regex turns this into
injection against a live database.

**Fix:** Pass values as psql variables instead of concatenating:
```bash
psql "$DSN" -v jid="$INFLIGHT_JOB_ID" -tAc \
  "SELECT count(*) FROM job_events WHERE job_id = :'jid' AND from_status='queued' AND to_status='active';"
```

### Info

#### IN-01: Unused loop variable

**File:** `scripts/keda-pandoc-loadproof.sh:678` (also `:210`, `:280`, `:346`
and the downscale equivalents)
**Issue:** `for i in $(seq 1 24)` never uses `i` (shellcheck SC2034 — the only
finding shellcheck reports across both files).
**Fix:** `for _ in $(seq 1 24)`.

#### IN-02: Duplicate YAML keys in the rendered pod template labels

**File:** `deploy/chart/octoconv/templates/deployment-pandoc-worker.yaml:51-52`
**Issue:** `octoconv.labels` and `octoconv.selectorLabels` both emit
`app.kubernetes.io/name` and `app.kubernetes.io/instance`, so the rendered
manifest contains duplicate keys in one mapping (confirmed by `helm template`).
The API server's YAML->JSON path tolerates this and keeps the last occurrence,
and every sibling Deployment does the same, so behavior is correct — but strict
YAML tooling (`yamllint`, some GitOps validators) rejects it.
**Fix:** Chart-wide, not phase-50-specific: have `octoconv.selectorLabels` emit
only `app.kubernetes.io/component` when composed with `octoconv.labels`.

#### IN-03: Fixture build loop re-reads the whole file on every append

**File:** `scripts/keda-pandoc-downscale-survival.sh:263-265`
**Issue:** `while [ "$(wc -c < /tmp/work/base.md)" -lt "$MD_TARGET_BYTES" ]`
runs `wc -c` over a file growing to 3 MB, appending ~200 bytes per iteration —
roughly 15,000 iterations reading tens of GB cumulatively. This runs on every
gate execution *and* every `--calibrate-only` dry run.
**Fix:** Track the byte count in a shell variable, or build once with a
repeat-count (`yes "$para" | head -n N`) sized from the target.

#### IN-04: KEDA "supply chain" mitigation is a version-resolution check only

**File:** `scripts/keda-pandoc-loadproof.sh:72-79`, `:257-262`;
`scripts/keda-pandoc-downscale-survival.sh:501-506`
**Issue:** The header claims T-50-SC (KEDA supply chain) is mitigated because
"KEDA v2.20.1 is re-verified resolvable (never an unpinned/latest pull)". The
check only proves the version string still appears in `helm search repo`
output; there is no `helm install --verify`, no provenance file, no digest pin.
A compromised or re-tagged chart at that version passes.
**Fix:** Either add `--verify` with a keyring, or soften the header claim to
what it actually establishes (version pinning, not provenance).

#### IN-05: `prometheus.scrapeInterval` is interpolated unvalidated into the Prometheus config

**File:** `deploy/chart/octoconv/templates/_helpers.tpl:77`,
`deploy/chart/octoconv/values.yaml:202-210`
**Issue:** `scrape_interval: {{ .Values.prometheus.scrapeInterval | default "15s" }}`
renders unquoted with no format validation. A numeric value (`5`) or a typo
(`5sec`) produces a config Prometheus rejects at load — surfacing as a
crash-looping Prometheus pod rather than a `helm install` error, which in turn
takes the KEDA scaler's data source down. (The 5s value itself is safe: an
unset `scrape_timeout` is clamped down to the interval by Prometheus, so no
"scrape timeout greater than scrape interval" error occurs.)
**Fix:** `scrape_interval: {{ .Values.prometheus.scrapeInterval | default "15s" | quote }}`
plus a `fail`-guard on a duration-shaped regex.

#### IN-06: `synthesizeLongFixture`'s documented contract is not enforced on the gate path

**File:** `scripts/keda-pandoc-downscale-survival.sh:229-232` vs `:710-712`
**Issue:** The helper's header states *"Requires FIXTURE_CONTAINER to already be
running (--cpus/--memory already applied by the caller)"*. `runCalibration`
does apply them (line 331); STEP 7 does not (line 711:
`docker run -d --name "$FIXTURE_CONTAINER" --entrypoint sleep ...` with no
limits). Harmless for synthesis, but the stated precondition is silently
violated on the primary path.
**Fix:** Either apply `--cpus 2 --memory 1g` at line 711 too, or scope the
comment to the calibration caller.

#### IN-07: Comment says "strictly under" the cap; the code (correctly) allows equality

**File:** `scripts/keda-pandoc-downscale-survival.sh:146-149` vs `:277`
**Issue:** The comment states the fixture *"must stay strictly under"*
`PANDOC_MAX_INPUT_BYTES`, but the acceptance test is
`[ "$html_bytes" -le "$PANDOC_MAX_INPUT_BYTES" ]`. The code is right —
`enforcePandocInputSize` (`internal/convert/markupsniff.go:169`) rejects only
`info.Size() > maxBytes` — so the comment is what is wrong.
**Fix:** Reword to "at or under the cap (the worker rejects only `size > cap`)".

---

_Reviewed: 2026-08-02_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
