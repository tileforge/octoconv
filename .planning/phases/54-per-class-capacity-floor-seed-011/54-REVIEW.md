---
phase: 54-per-class-capacity-floor-seed-011
reviewed: 2026-08-06T00:00:00Z
depth: standard
files_reviewed: 11
files_reviewed_list:
  - .github/workflows/ci.yml
  - deploy/chart/octoconv/templates/_helpers.tpl
  - deploy/chart/octoconv/templates/scaledobject-audio.yaml
  - deploy/chart/octoconv/templates/scaledobject-av.yaml
  - deploy/chart/octoconv/templates/scaledobject-document.yaml
  - deploy/chart/octoconv/templates/scaledobject-html.yaml
  - deploy/chart/octoconv/templates/scaledobject-image.yaml
  - deploy/chart/octoconv/templates/scaledobject-ocr.yaml
  - deploy/chart/octoconv/templates/scaledobject-pandoc.yaml
  - deploy/chart/octoconv/values-capacity-floor.yaml
  - deploy/chart/octoconv/values.yaml
findings:
  critical: 1
  warning: 6
  info: 6
  total: 13
status: fixes_applied
fixed_at: 2026-08-06
fixed:
  critical: 1
  warning: 4
skipped:
  warning: 2
  info: 6
---

# Phase 54: Code Review Report

**Reviewed:** 2026-08-06
**Depth:** standard
**Files Reviewed:** 11
**Status:** issues_found

## Summary

Reviewed the SEED-011/CAPA-01 per-class KEDA capacity-floor implementation: two new
named templates in `_helpers.tpl`, seven ScaledObject templates rewired to call them,
seven explicit `minReplicaCount: 0` defaults in `values.yaml`, a single-key
`values-capacity-floor.yaml` overlay, and a new `helm` CI job.

All claims below were verified by executing `helm v4.2.3` locally against the chart
(and against a deliberately mutated copy of the chart).

What holds up: the `hasKey` + `ne ... nil` falsy-0 guard on
`scaleDownStabilizationSeconds` is intact and correct in all five classes that carry
the knob (document/audio/av/ocr/pandoc) — verified by render: the default render emits
exactly three `stabilizationWindowSeconds: 900` blocks and `values-loadproof.yaml`
correctly renders a literal `stabilizationWindowSeconds: 0` for pandoc rather than
swallowing it. Template whitespace chomping in both helpers is clean (`minReplicaCount: 2`
renders with no stray space or newline). The dict-arg contract (`class` + `root`) is
consistently correct at all 14 call sites. The overlay produces exactly the intended
4-line diff. The `--show-only` invocations fail closed if the KEDA/Prometheus flags are
ever dropped (`Error: could not find template ...`), so the vacuity trap the comments
warn about is genuinely fenced off for those.

What does not hold up: the D-02 fail-fast validation — the headline safety property of
this phase — is **bypassable and silently wrong** for a wide class of realistic operator
inputs, because `int` coerces before any check runs (CR-01). Separately, the CI job that
is supposed to prove the feature works never asserts a single rendered *value*; a
mutated chart that silently clamps an operator's floor of 2 down to 1 passes the entire
`helm` job green (WR-02). And the two `helm lint` steps validate nothing at all under
helm v4 (WR-01).

## Critical Issues

### CR-01: D-02 validation is bypassable — `int` coercion silently swallows non-integer input and produces a floor of 0

**File:** `deploy/chart/octoconv/templates/_helpers.tpl:107-116`

**Issue:** The helper coerces *before* it validates:

```
{{- $min := int $cfg.minReplicaCount -}}
{{- $max := int $cfg.maxReplicaCount -}}
{{- if lt $min 0 -}}{{- fail ... -}}{{- end -}}
{{- if gt $min $max -}}{{- fail ... -}}{{- end -}}
```

Sprig's `int` is `cast.ToInt`, which discards its error and returns `0`. Helm's `--set`
strvals parser only produces a Go `int64` for a bare base-10 integer; every other token
arrives as a `string` or `bool`. The result is that both `fail` guards are unreachable
for any input `cast.ToInt` cannot parse, and the operator gets a silent floor of `0` —
i.e. no warm floor at all, with `helm template`/`helm upgrade` exiting 0.

Verified against the real chart (helm v4.2.3, all with `--show-only templates/scaledobject-image.yaml`):

| `--set keda.image.minReplicaCount=` | rendered `minReplicaCount` | rendered `fallback.replicas` | guard fired? |
|---|---|---|---|
| `2.9` | `0` | `1` | no |
| `99.9` (25x above `maxReplicaCount: 4`) | `0` | `1` | **no — ceiling guard bypassed** |
| `abc` (`--set-string`) | `0` | `1` | no |
| `null` | `0` | `1` | no |
| `-0.5` | `0` | `1` | **no — negative guard bypassed** |
| `" 3"` / `"3 "` (stray whitespace) | `0` | `1` | no |
| `true` | `1` | `1` | no |
| `010` (operator means ten) | — | — | fires, reporting `8` (octal) |
| `0x10` | — | — | fires, reporting `16` (hex) |

Three distinct harms, in order of severity:

1. **Silent loss of the configured floor.** `helm upgrade --set keda.image.minReplicaCount=2.0`
   (a completely natural way to type it) leaves the class scaled-to-zero-capable with
   no error. The operator believes cold-start protection is on; it is off. `_helpers.tpl:100-104`
   explicitly promises the opposite: *"Fails fast via `fail` (D-02) rather than rendering an
   unsafe manifest"*. `scaledobject-document.yaml:24-31` calls this exact failure mode
   *"the exact silent-bug class this phase exists to close."*
2. **The ceiling guard is bypassable.** `=99.9` is 25x `maxReplicaCount` and renders clean.
3. **Silent wrong-value coercion.** `=true` yields a real floor of 1; `=010` is read as
   octal 8, not 10.

Note the values-file path behaves differently and correctly (YAML `2.0` -> float64 -> 2;
YAML `99.9` -> 99 -> guard fires), which makes the `--set` divergence harder to notice —
the CI negative cases happen to use only bare integers (`99`, `-1`), so they never touch
the broken path.

**Fix:** Validate the raw value's kind before coercing, and reject non-integral numbers:

```gotemplate
{{- define "octoconv.kedaMinReplicaCount" -}}
{{- $cfg := index .root.Values.keda .class -}}
{{- $rawMin := $cfg.minReplicaCount -}}
{{- $rawMax := $cfg.maxReplicaCount -}}
{{- if not (or (kindIs "int" $rawMin) (kindIs "int64" $rawMin) (kindIs "float64" $rawMin)) -}}
{{- fail (printf "keda.%s.minReplicaCount must be an integer, got %v (kind %s) -- note `--set x=2.0` and `--set x=abc` both arrive as strings and would otherwise silently coerce to 0" .class $rawMin (kindOf $rawMin)) -}}
{{- end -}}
{{- $min := int $rawMin -}}
{{- if ne (float64 $min) (float64 $rawMin) -}}
{{- fail (printf "keda.%s.minReplicaCount must be a whole number, got %v" .class $rawMin) -}}
{{- end -}}
{{- $max := int $rawMax -}}
{{- if lt $min 0 -}}
{{- fail (printf "keda.%s.minReplicaCount (%v) must not be negative" .class $rawMin) -}}
{{- end -}}
{{- if gt $min $max -}}
{{- fail (printf "keda.%s.minReplicaCount (%v) must not exceed keda.%s.maxReplicaCount (%v)" .class $rawMin .class $rawMax) -}}
{{- end -}}
{{- $min -}}
{{- end }}
```

Then extend the CI negative cases to cover the bypasses, e.g. a case C asserting
`--set keda.image.minReplicaCount=2.9` exits non-zero, and a case D for `=abc`.

## Warnings

### WR-01: Both `helm lint` steps are non-validating — helm v4 downgrades template `fail` to an INFO log and exits 0

**File:** `.github/workflows/ci.yml:56-60`; doc claim at `deploy/chart/octoconv/templates/_helpers.tpl:102-103`

**Issue:** `_helpers.tpl:102-103` states the guard *"aborts `helm template`/`helm lint`"*.
That is false for the pinned toolchain. Verified:

```
$ helm lint deploy/chart/octoconv --set keda.enabled=true --set prometheus.enabled=true \
    <secrets> --set keda.image.minReplicaCount=99
level=INFO msg="funcMap fail" message="keda.image.minReplicaCount (99) must not exceed keda.image.maxReplicaCount (4)"
==> Linting deploy/chart/octoconv
1 chart(s) linted, 0 chart(s) failed
LINT RC=0
```

Same result for the negative-floor case. So the two `helm lint` invocations in the CI job
would pass even against a chart whose ScaledObject templates fail to render entirely.
They contribute no signal; the real coverage comes solely from the `helm template` steps
that follow. This matters because the surrounding comments present lint as part of the
validation surface, so a future editor could reasonably delete a `helm template` step and
believe lint still covers it.

**Fix:** Correct the `_helpers.tpl` doc comment to say `helm template`/`helm install`
only, and either drop the lint steps or make them assertive:

```bash
helm lint "$CHART" ... 2>&1 | tee /tmp/lint.out
if grep -q 'funcMap fail' /tmp/lint.out; then
  echo "FAIL: a template fail() fired during lint (helm lint exits 0 on these)"; exit 1
fi
```

### WR-02: The CI job never asserts a rendered *value* — a chart that silently halves the operator's floor passes green

**File:** `.github/workflows/ci.yml:68-113`

**Issue:** Every assertion is a *shape* assertion: "7 objects render", "these 6 classes
are byte-identical", "image differs", "all changed lines contain `minReplicaCount:` or
`replicas:`". Nothing asserts that the overlay's `minReplicaCount: 2` actually renders as
`2`, or that `fallback.replicas` renders as `2`.

Proven by mutation. I copied the chart, changed `_helpers.tpl:116` from `{{- $min -}}` to
`{{- min $min 1 -}}` (i.e. the operator's floor of 2 is silently clamped to 1, and the
D-05 `max(1, min)` fallback effect disappears entirely), and re-ran the exact CI script
against the mutated chart:

```
MUTATED-CHART CI EXIT=0
OK: document unchanged ... OK: image changed (expected)
OK: whole-chart diff scoped to minReplicaCount:/replicas: only
OK: negative case A failed as expected (rc=1)
OK: negative case B failed as expected (rc=1)
```

The job is green while the feature is broken. This also means
`values-capacity-floor.yaml:44-48`'s central claim — *"At 2 the rendered image ScaledObject
changes on BOTH the `minReplicaCount` line AND the `fallback.replicas` line, which is what
lets the CI diff prove D-05"* — is not actually enforced anywhere; the mutation removes the
`fallback.replicas` change and CI does not notice.

**Fix:** Add exact-value assertions after the render step:

```bash
echo "### floor value + D-05 fallback value (SC1/D-05)"
grep -qx '  minReplicaCount: 0' <(helm template ... --show-only templates/scaledobject-image.yaml)
grep -qx '  minReplicaCount: 2' <(helm template ... -f "$OVERLAY" --show-only templates/scaledobject-image.yaml)
grep -qx '    replicas: 2'      <(helm template ... -f "$OVERLAY" --show-only templates/scaledobject-image.yaml)
# and assert the default render's fallback is still byte-identical to the pre-phase value
grep -qx '    replicas: 1'      <(helm template ... --show-only templates/scaledobject-image.yaml)
```

### WR-03: Third-party action `azure/setup-helm@v4` is pinned only to a mutable major tag

**File:** `.github/workflows/ci.yml:26`

**Issue:** The comment block at lines 18-21 explicitly identifies this as *"this file's
first non-`actions/*`, non-`docker/*` action (T-54-03)"* and responds with a
`permissions: contents: read` scope — but then pins it to `@v4`, a mutable tag the
publisher (or anyone who compromises the publisher) can repoint at arbitrary code. The
job body's careful pinning of the *helm binary* to `v4.2.3` (lines 28-32, with an explicit
supply-chain rationale about non-reproducible renders) makes the unpinned *action* a
direct inconsistency: the reasoning applied to the tool was not applied to the thing that
downloads the tool. Same applies to `actions/checkout@v7`, `docker/setup-buildx-action@v4`,
`docker/bake-action@v7`, `actions/upload-artifact@v7` in the pre-existing jobs.

**Fix:** Pin to a commit SHA with the tag as a trailing comment:

```yaml
- uses: azure/setup-helm@<full-40-char-sha>  # v4.x.y
  with:
    version: v4.2.3
```

### WR-04: No workflow-level `permissions:` — only the new `helm` job is least-privilege

**File:** `.github/workflows/ci.yml:1-8` (missing), `:146`, `:165`, `:180`, `:221`

**Issue:** The `helm` job correctly declares `permissions: contents: read`. The four other
jobs (`gate`, `race`, `docker-build`, `e2e`) declare none, so they inherit the
repository/organization default `GITHUB_TOKEN` scope, which on older repos is
read-write-all. `docker-build` and `e2e` execute third-party actions and build/run the
full compose stack; a write-scoped token in those jobs is a meaningful blast radius.
Adding the restriction at job scope only (rather than workflow scope) also means every
future job added to this file silently defaults back to the permissive scope.

**Fix:** Hoist to workflow level and let jobs widen if ever needed:

```yaml
on: [push, pull_request]

permissions:
  contents: read
```

### WR-05: The anti-vacuity FAIL diagnostic is unreachable in the case it exists to catch

**File:** `.github/workflows/ci.yml:69-74`

**Issue:** Under `set -euo pipefail`, an assignment whose command substitution exits
non-zero terminates the script. `grep -c` exits 1 when it finds zero matches, so if the
render contains **zero** ScaledObjects — precisely the vacuity scenario the block at lines
68-75 exists to detect and report — the script dies at line 69 with no output at all. The
carefully-worded `"FAIL: expected 7 ScaledObjects in both renders, got default=$DEFAULT_COUNT overlay=$OVERLAY_COUNT"`
message only ever prints for counts of 1-6 or 8+. Verified: `( set -e; V=$(grep -c nope /etc/hosts); echo reached )`
prints nothing and exits 1.

The job still fails closed, so this is a diagnosability defect rather than a correctness
hole — but it is inconsistent with the deliberate `|| true` guard the author applied to the
very same hazard 35 lines later (lines 104-107).

**Fix:** Apply the same `|| true` treatment:

```bash
DEFAULT_COUNT=$(grep -c '^kind: ScaledObject' "$DEFAULT_RENDER" || true)
OVERLAY_COUNT=$(grep -c '^kind: ScaledObject' "$OVERLAY_RENDER" || true)
```

### WR-06: The whole-chart isolation regex is unanchored and permits changes it claims to forbid

**File:** `.github/workflows/ci.yml:107,110`

**Issue:** `grep -vE '(minReplicaCount:|replicas:)'` matches those substrings anywhere on a
line. `replicas:` therefore also matches the `replicas:` field of every Deployment in the
chart (`api`, `webhook-worker`, `mcp-http`, `asynqmon`, `prometheus`, …) and any comment
line containing the word. So the assertion labelled *"every differing line in the full-chart
diff must be a `minReplicaCount:`/`replicas:` line"* would silently accept an overlay that
rescaled the API Deployment — which is not "isolation to one ScaledObject" as the section
header and `values-capacity-floor.yaml:14-18` both claim. The per-class `--show-only` loop
above does not compensate: it only covers the six sibling ScaledObjects, never the
Deployments.

**Fix:** Anchor to the exact rendered indentation of the two fields that are legitimately
allowed to change, and assert the exact count:

```bash
OUT_OF_SCOPE=$(echo "$CHANGED_LINES" | { grep -vE '^[<>] (  minReplicaCount: |    replicas: )[0-9]+$' || true; } | wc -l | tr -d ' ')
CHANGED_COUNT=$(echo "$CHANGED_LINES" | wc -l | tr -d ' ')
if [ "$CHANGED_COUNT" -ne 4 ]; then
  echo "FAIL: expected exactly 4 changed lines (min/fallback x default/overlay), got $CHANGED_COUNT"; exit 1
fi
```

## Info

### IN-01: `fail` messages report the coerced value, not the operator's input

**File:** `deploy/chart/octoconv/templates/_helpers.tpl:111,114`

**Issue:** `printf "... (%d) ..." $min` prints the post-`int` value. `--set keda.image.minReplicaCount=0x10`
produces *"keda.image.minReplicaCount (16) must not exceed ... (4)"* — the operator never
typed 16 and has to work out that their input was read as hex. `010` likewise reports `8`.
**Fix:** Print the raw value with `%v` (covered by the CR-01 patch above).

### IN-02: `[ "$d1" == "$d2" ]` uses a bashism inconsistent with the `!=` form used 10 lines earlier

**File:** `.github/workflows/ci.yml:91` (vs `:81`)

**Issue:** `==` inside `[` is undefined in POSIX and only works because `shell: bash` is
declared. Not a live bug, but it is gratuitously inconsistent with line 81's `[ "$d1" != "$d2" ]`.
**Fix:** Use `!=`/`=` consistently, or `[[ ]]` consistently.

### IN-03: Temp files are only cleaned on the success path

**File:** `.github/workflows/ci.yml:63-64,114`

**Issue:** `rm -f "$DEFAULT_RENDER" "$OVERLAY_RENDER"` sits inline at line 114, so any
`exit 1` between lines 65 and 113 leaks both files. Harmless on an ephemeral runner, but
the script is also plainly intended to be runnable locally.
**Fix:** `trap 'rm -f "$DEFAULT_RENDER" "$OVERLAY_RENDER"' EXIT` right after the `mktemp` calls.

### IN-04: The `helm` job has no `timeout-minutes`

**File:** `.github/workflows/ci.yml:16-18`

**Issue:** `docker-build` (20) and `e2e` (25) set explicit timeouts; the new `helm` job does
not, so a hung `azure/setup-helm` download burns the 6-hour default.
**Fix:** Add `timeout-minutes: 10` alongside `runs-on`.

### IN-05: The "MANDATORY" KEDA/Prometheus flags are hand-repeated across 8 invocations

**File:** `.github/workflows/ci.yml:57,60,65,66,79,80,89,90`

**Issue:** The comment at lines 48-54 correctly identifies dropping these flags as the
central vacuity hazard and says "Do not 'clean up' these flags" — but then relies on a
human repeating them verbatim 8 times, while the *other* mandatory flag group is already
factored into `$SECRETS_SET`. The `--show-only` sites fail closed if the flags are lost
(verified: `Error: could not find template templates/scaledobject-image.yaml in chart`),
so this is a maintainability risk rather than a live hole.
**Fix:** Fold them into one variable, e.g.
`COMMON_SET="--set keda.enabled=true --set prometheus.enabled=true $SECRETS_SET"`, and use
`$COMMON_SET` everywhere — one place to get wrong instead of eight.

### IN-06: `values.yaml` ships hardcoded MinIO credentials as chart defaults

**File:** `deploy/chart/octoconv/values.yaml:189-190`

**Issue:** `rootUser: "minioadmin"` / `rootPassword: "minioadmin"` are literal defaults in
the shipped chart, unlike every other credential in this chart, which the CI comment at
lines 41-45 notes is deliberately kept out of `values.yaml` and supplied only via an
overlay or `--set`. Pre-existing and outside this phase's diff, but it is an inconsistent
application of that stated rule inside a file under review.
**Fix:** Move `minio.rootUser`/`rootPassword` into the `secrets:` block that
`templates/secret.yaml` already requires an overlay to populate, so a bare `helm install`
fails loudly rather than deploying a well-known credential pair.

---

## Fixes Applied

**Fixed at:** 2026-08-06
**Fixed by:** Claude (gsd-code-fixer)
**Scope:** CR-01 + in-scope WARNINGs. WR-03/WR-04 deliberately skipped (recorded decisions,
not oversights). All INFO findings out of scope for this pass.

**Summary:** 1 critical + 4 warnings fixed across 6 atomic commits; 2 warnings + 6 info skipped.

### CR-01 — D-02 validation bypassable via `int` coercion — **FIXED**

**Commits:** `adecb1f` (`deploy/chart/octoconv/templates/_helpers.tpl`),
`a918397` (`.github/workflows/ci.yml` — regression cases)

**What changed:** `octoconv.kedaMinReplicaCount` now validates `kindOf` on the RAW value before
any `int` coercion, for **both** `minReplicaCount` and `maxReplicaCount`. Accepted kinds are
`int`/`int64`, or a `float64` that is integral (`eq $raw (floor $raw)`) — which is what
values-file numbers arrive as, since helm parses YAML via JSON. Anything else hits a **third**
`fail`. The two pre-existing `fail` message format strings are byte-for-byte unchanged
(`"keda.%s.minReplicaCount (%d) must not be negative"` and
`"keda.%s.minReplicaCount (%d) must not exceed keda.%s.maxReplicaCount (%d)"`), so the CI greps
and 54-01-SUMMARY.md's recorded contract still hold. An absent/null `maxReplicaCount` (kind
`invalid`) is now reported as its own misconfiguration instead of coercing to `0` and being
mis-blamed on `minReplicaCount`.

IN-01's `%v`-raw-value suggestion was deliberately **not** taken: the brief requires the two
existing message strings unchanged, and after the kind guard `%d` prints the operator's exact
input for every value that reaches those guards (`010`/`0x10` now fail the kind check upstream
only if they arrive as strings — a bare `--set x=010` still parses to int64 8, which is
unchanged pre-existing helm/Go behavior, not something this guard can see).

**Verification evidence** (helm v4.2.3, all via `bash -c`, `--show-only templates/scaledobject-image.yaml`):

| `--set keda.image.minReplicaCount=` | before | after |
|---|---|---|
| `99.9` | rc=0, `minReplicaCount: 0` | rc=1, `... must be a whole number, got "99.9" (kind string)` |
| `-0.5` | rc=0, `minReplicaCount: 0` | rc=1, same guard |
| `2.9` | rc=0, `minReplicaCount: 0` | rc=1, same guard |
| `2.0` | rc=0, `minReplicaCount: 0` | rc=1, same guard |
| `abc` | rc=0, `minReplicaCount: 0` | rc=1, same guard |
| `true` | rc=0, `minReplicaCount: 1` | rc=1, `got true (kind bool)` |
| `""` | rc=0, `minReplicaCount: 0` | rc=1, `got "" (kind string)` |
| `null` | rc=0, `minReplicaCount: 0` | rc=1, `got <nil> (kind invalid)` |
| `99` | rc=1 (exceeds guard) | rc=1, **message unchanged verbatim** |
| `-1` (ocr) | rc=1 (negative guard) | rc=1, **message unchanged verbatim** |
| `2` | rc=0, `2`/`2` | rc=0, `2`/`2` (unchanged) |
| `0` | rc=0, `0`/`1` | rc=0, `0`/`1` (unchanged) |

Also: `--set keda.image.maxReplicaCount=null` and `=4.5` now abort naming
`keda.image.maxReplicaCount`. Whitespace discipline preserved — the default full-chart render is
**byte-identical** to the pre-fix render (`diff` exits 0, zero output). Restoring the pre-fix
`_helpers.tpl` (`adecb1f^`) into a chart copy makes the CI job fail at
`--set keda.image.minReplicaCount=[99.9] rendered successfully`.

### WR-01 — `helm lint` is non-validating for template `fail` — **FIXED**

**Commits:** `adecb1f` (`_helpers.tpl` doc comment), `b64a12d` (`ci.yml` comment)

The `_helpers.tpl` claim *"aborts `helm template`/`helm lint`"* was replaced with a precise
statement: `fail` aborts `helm template` / `helm install` / `helm upgrade`; helm v4.2.3 downgrades
it to `level=INFO msg="funcMap fail"` during `helm lint` and still exits 0, so lint is **not** part
of this guard's verification surface. A matching comment was added above the CI job's two lint
steps so a future editor does not delete a `helm template` step believing lint covers it.

Per the brief, **no fake lint assertion was added** — the job's D-02 negative cases already rely on
`helm template` only, and the review's suggested `grep 'funcMap fail'` post-check would assert on
an undocumented log-line format. Verified: `helm lint` still exits 0 for both the default values and
the capacity-floor overlay.

### WR-02 — CI never asserted a rendered *value* — **FIXED**

**Commit:** `3a050da` (`.github/workflows/ci.yml`)

Added four exact-line assertions (`grep -qxF`) on the image `--show-only` renders already computed
by the isolation check: default `  minReplicaCount: 0` + `    replicas: 1`; overlay
`  minReplicaCount: 2` + `    replicas: 2`. Failure prints the actual floor/fallback lines.

**Verification evidence — mutation test.** Copied the chart, changed the helper's emit from
`{{- $min -}}` to `{{- min $min 1 -}}` (the review's own mutation: operator floor of 2 silently
clamped to 1, D-05 fallback effect erased). Before this fix that mutation passed the whole job with
`MUTATED-CHART CI EXIT=0`. Now:

```
MUTATED-CHART CI EXIT=1
FAIL: overlay floor (the overlay's only key must actually reach the manifest) --
      the rendered image ScaledObject does not contain the exact line: [  minReplicaCount: 2]
actual floor/fallback lines were:
17:  minReplicaCount: 1
23:    replicas: 1
```

### WR-05 — anti-vacuity diagnostic unreachable under `set -euo pipefail` — **FIXED**

**Commit:** `fc101ae` (`.github/workflows/ci.yml`)

Both count captures now use `|| true`, matching the guard the author already applied to the later
`grep -v` capture. The `FAIL: expected 7 ScaledObjects ... got default=0 overlay=0` diagnostic is
now reachable in the zero-match case it exists for. Verified: the job still exits 0 and still
reports `OK: 7 ScaledObjects in both renders`.

### WR-06 — unanchored isolation regex permitted Deployment rescales — **FIXED**

**Commit:** `f53371f` (`.github/workflows/ci.yml`)

The pattern is now anchored to the exact rendered indentation of the only two fields allowed to
change — `^[<>] (  minReplicaCount: |    replicas: )[0-9]+$` — and the changed-line count is
asserted to be exactly `4`.

**Verification evidence:** appending `api: {replicas: 3}` to `values-capacity-floor.yaml` in a chart
copy (accepted by the old unanchored `(minReplicaCount:|replicas:)` substring match, since it also
matches every Deployment's `replicas:` field) now fails with
`FAIL: diff touched lines outside the image ScaledObject's minReplicaCount:/replicas: fields`.
Unmutated chart: `OK: whole-chart diff is exactly the 4 expected image-ScaledObject floor/fallback lines`.

### WR-03 — SHA-pin `azure/setup-helm` — **SKIPPED (recorded decision, not an oversight)**

Plan 54-02's threat model **T-54-03** explicitly considered and rejected SHA pinning: this file's
four other third-party actions (`actions/checkout@v7`, `actions/setup-go@v6`,
`docker/setup-buildx-action@v4`, `docker/bake-action@v7`, `actions/upload-artifact@v7`) are all
tag-pinned, and pinning one job differently was judged inconsistent security theatre rather than a
real improvement. The compensating controls that decision rests on are in place and unchanged:
`permissions: contents: read` on the job, and the helm **binary** pinned via the `version: v4.2.3`
input so render behavior cannot drift without a commit here. Reversing this is a repo-wide
supply-chain policy change, not a phase-54 fix.

### WR-04 — workflow-level `permissions:` — **SKIPPED (out of phase scope)**

`gate`, `race`, `docker-build` and `e2e` all predate this phase. Adding a workflow-level
`permissions:` key (or job-level keys on those four) would modify lines outside this phase's diff
and break the *"the `ci.yml` change is purely additive"* invariant that plan 54-02 asserts and whose
acceptance criterion greps for it
(`git diff .github/workflows/ci.yml | grep -c '^-[^-]'` must be `0`). Confirmed still holding after
all fixes: `git diff f7eaea8..HEAD -- .github/workflows/ci.yml | grep -c '^-[^-]'` → `0`.
Worth doing as its own hardening change; it is not a phase-54 regression.

### IN-01 … IN-06 — **SKIPPED (INFO severity, out of scope for this pass)**

IN-01 is additionally superseded in practice: after CR-01's raw-kind guard, `%d` prints the exact
operator input for every value that can still reach the two numeric guards. IN-06 (`values.yaml`
MinIO credentials) is pre-existing and outside this phase's diff, as the review itself notes.

### Post-fix invariant sweep (all run locally, all via `bash -c`)

| # | Invariant | Result |
|---|---|---|
| 1 | `helm lint` exits 0, default and `-f values-capacity-floor.yaml` | PASS |
| 2 | Exactly 7 ScaledObjects in both renders | PASS (7 / 7) |
| 3 | Whole-chart diff non-empty; every `^[<>]` line is `minReplicaCount:`/`replicas:` | PASS (exactly 4 lines) |
| 4 | Per-class isolation: 6 classes byte-identical, `image` differs | PASS |
| 5 | `=99` names both keys; `ocr=-1` names its key; both messages verbatim | PASS |
| 6 | `99.9` / `-0.5` / `2.9` / `abc` / `true` / `""` each exit non-zero | PASS (all rc=1; were rc=0) |
| 7 | `=2` renders `2`/`2` on image, other six stay `0`/`1` | PASS |
| 8 | `hasKey`+`ne … nil` guard intact in document/audio/av/ocr/pandoc; loadproof pandoc renders literal `stabilizationWindowSeconds: 0` | PASS (5/5 files, literal `0` rendered) |
| 9 | `git diff` empty on `internal/convert/exec.go`, `values-local.yaml`, `values-e2e.yaml`, `values-loadproof.yaml` | PASS (0 lines, vs. working tree and vs. `f7eaea8`) |
| 10 | `git diff f7eaea8..HEAD -- .github/workflows/ci.yml \| grep -c '^-[^-]'` is 0 | PASS (`0`) |
| 11 | The `helm` job's entire embedded script run verbatim locally | PASS (`EXIT=0`) |
| 12 | `go build ./...` and `go test ./...` | PASS (build OK; all packages `ok`) |

Invariant 11 was executed by extracting the job's `run:` block with
`ruby -ryaml` and running it with `bash` from the repo root (never zsh — an unquoted `$FLAGS`
does not word-split there, which would render zero ScaledObjects and pass every check vacuously).

---

_Reviewed: 2026-08-06_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
_Fixes applied: 2026-08-06 by Claude (gsd-code-fixer)_
