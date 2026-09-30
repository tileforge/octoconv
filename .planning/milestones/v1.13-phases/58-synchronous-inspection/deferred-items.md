# Deferred Items — Phase 58

## Plan 58-03

- **`helm lint deploy/chart/octoconv` / `helm template ... ` with NO `-f` fails
  regardless of Phase 58**, pre-existing and out of scope for this plan:
  `templates/secret.yaml` dereferences `.Values.secrets.postgresPassword`
  (and siblings), and `values.yaml` deliberately carries no `secrets:` block
  by design ("Real dev-credential values live in values-local.yaml, never
  here" — values.yaml's own header comment). This has been true since
  whichever earlier phase introduced `secret.yaml`; Task 3 of this plan
  touches `deployment-api.yaml` / `service-api.yaml` /
  `networkpolicy-metrics.yaml` / `configmap.yaml` / `values.yaml`, none of
  which are the cause. All Task 3 acceptance-criteria `helm lint`/
  `helm template` runs in 58-03-SUMMARY.md were therefore run with
  `-f deploy/chart/octoconv/values-local.yaml` (the chart's own supplied
  dev-secrets overlay, already used by the existing `values-local.yaml`
  file) to get past this pre-existing gap and actually exercise the
  templates this plan changed. Not fixed here — out of scope (Rule 3
  scope boundary: pre-existing failure in a file this plan does not touch).
  status: acknowledged

## Codex PR#9 round 22 (2026-08-17)

- **A bounded EPUB external-reference scan** (`container.go`, closed in the
  honest direction by reporting `external_reference: unknown/parser_unavailable`
  for epub instead of a false `clean`). The real scan needs its own parser and
  its own ceilings: `META-INF/container.xml` → OPF path → OPF manifest →
  bounded set of XHTML/CSS/SVG parts → ordinary `href`/`src` attributes
  carrying a URL scheme (epub does not use ODF's `xlink:href` convention), with
  a new per-part count limit alongside the existing `maxRelsPartsScanned` /
  `maxRelsPartBytes`. Out of scope for a review round: it is a new parser, not
  a correction to an existing one. D-01 governs the interim label — no parser
  written is an honest unknown, not a failure.
  status: acknowledged

## Codex PR#9 round 25 (2026-08-18) — recorded, not fixed

- **Does `read_failed` make a report's coverage incomplete?** Today it does
  not. `buildCoverage` (`internal/inspect/report.go`) marks
  `Coverage.Complete = false` only for the two ceiling reasons, on D-02a's
  stated rule that "structural blindness about the FILE never makes the REPORT
  selective; only exhausting THIS SERVICE'S OWN byte/time budget does". A read
  failure is on neither side of that line as written: it is not blindness about
  the file, it is our own operational failure — and so a report can currently
  say `coverage.complete: true` while every check in it says
  `unknown/read_failed`, i.e. "I inspected this fully" and "I could not read
  it" in one body.

  Reached while closing the round-25 sniff-read-failure finding: the new path
  inherits this behavior rather than diverging from its three existing siblings
  (zip read-failed, image read-failed, media read-failed), because a local
  inconsistency would be worse than a consistent open question.

  Not fixed here because it is a CONTRACT decision, not a bug fix:
  `Coverage.Reason` would gain a non-ceiling value, `MarkTruncated`'s allow-list
  would have to widen (it silently drops non-ceiling reasons today), and
  `docs/openapi.yaml`'s coverage description would need to match. PR9R13 already
  set the precedent in the other direction for `malformed_input` on the zip path
  by marking the tracker — which, note, would attribute an I/O failure to
  `work_ceiling_bytes` if copied here, so that mechanism is NOT the right one.

  Decide deliberately: either read_failed joins the ceiling reasons in making
  coverage partial (with its own reason value), or the docs state plainly that
  `complete` means "no ceiling was hit", not "everything was inspected".
  status: acknowledged
