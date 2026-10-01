---
phase: quick-261001-ncn
plan: 01
subsystem: deploy/chart
tags: [helm, minio, chainguard, pvc-ownership, mcImage, codex-pr31]
requires:
  - quick-261001-5ec (MinIO moved to cgr.dev/chainguard/minio, digest-pinned)
provides:
  - "init container migrate-data-ownership in the MinIO pod (chown -R -h --from=0 65532:65532, root, CHOWN only)"
  - "minio.mcImage restored as an optional override for every container that runs mc, falling back to minio.image"
affects:
  - deploy/chart/octoconv
  - scripts/ci-helm-checks.sh
  - internal/e2e/minio_image_pin_test.go
tech-stack:
  added: []
  patterns:
    - "render-derived docker probe: the init container is run under docker with flags taken from the helm render"
key-files:
  created: []
  modified:
    - deploy/chart/octoconv/templates/minio.yaml
    - deploy/chart/octoconv/templates/job-createbucket.yaml
    - deploy/chart/octoconv/templates/_helpers.tpl
    - deploy/chart/octoconv/values.yaml
    - scripts/ci-helm-checks.sh
    - internal/e2e/minio_image_pin_test.go
    - README.md
decisions:
  - "Init container reuses minio.image (GNU coreutils chown has --from and -h): no new image, registry or digest pin"
  - "Migrate only uid 0 elements (--from=0), not everything that is not 65532"
  - "Capabilities: drop ALL, add CHOWN only (DAC_READ_SEARCH conflicts with Pod Security Standard baseline)"
  - "mc containers use `.Values.minio.mcImage | default .Values.minio.image` on one exact line, held by TestMinioImagePinnedByDigest"
metrics:
  duration: "about 25 min"
  completed: 2026-10-01
status: complete
actuals:
  tokens: 4955
  tasks: 3
  commits: 3
plan_head_before: 90d465aa58f275a530d49c9ed48f1f73f4b19cf1
plan_head_after: 8ee53a802e67f9eff81079137715631a9868d68f
---

# Phase quick-261001-ncn Plan 01: chart migrates root-owned MinIO volume and restores minio.mcImage Summary

The MinIO pod now carries an init container that hands data written by the old root-run image to uid 65532 before the server starts, and `minio.mcImage` is back as an optional override (empty by default) for every container that runs `mc`. Both Codex P1 findings on PR #31 are closed.

## Commits (3 after 90d465a)

| Task | Commit | Message |
| ---- | ------ | ------- |
| 1 | c0ad4b7 | fix(chart): hand MinIO data written by the old root image to uid 65532 before the server starts |
| 2 | c13cfca | fix(chart): restore minio.mcImage as an optional override for every mc container |
| 3 | 8ee53a8 | docs: the Helm chart migrates MinIO data ownership itself |

`commits: 3` is measured from the ledger (`git rev-list --count 90d465a..HEAD`). Task 1 was amended once (see Deviations, item 3), so c0ad4b7 is the amended hash.

## What was done

**Task 1 (tracer).** `templates/minio.yaml` gets `initContainers: [migrate-data-ownership]`: image `.Values.minio.image`, exec form `chown -R -h --from=0 65532:65532 /data`, `runAsUser: 0`, `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`, `capabilities: drop [ALL], add [CHOWN]`, limits 100m / 64Mi, mounts `minio-data` at `/data`. `scripts/ci-helm-checks.sh` has a new section that builds one summary string from the render and compares it exactly with the expected one (image equals the server image, mountPath and chown target equal the server mountPath, flags, user, escalation, rorf, drop, add, owner, fsGroup); a render without the `minio` Deployment fails instead of passing.

Live proof (`evidence/ownership-probe.sh`, output in `evidence/ownership-probe.txt`, re-run in `ownership-probe.verify.txt`, last line `RESULT: PASS`). The probe builds every docker flag from the helm render (`-u`, `--cap-drop/--cap-add`, `--security-opt no-new-privileges`, `--read-only`, `--memory 64m`, `--cpus 0.100`, `--entrypoint`, argv); the command it ran:

```
docker run --rm --name octoconv-ncn-init --network none -u 0 --memory 64m --cpus 0.100 --cap-drop ALL --cap-add CHOWN --security-opt no-new-privileges --read-only -v VOL:/data --entrypoint chown $REF -R -h --from=0 65532:65532 /data
```

- Fixture: volume seeded by the pinned image run as root (`chmod 0777`, bucket `probe`, object `f.txt`); all 29 elements below the volume root are uid 0, plus two planted ones (`third-uid-file` 1234:1234, `already-65532`).
- Control: the pinned non-root MinIO on that volume dies (`exit 1`, `unable to rename (/data/.minio.sys/tmp -> …tmp-old/…) file access denied`, `FATAL Unable to initialize backend: Unable to write to the backend`).
- Migration: init exit 0; `migrated=29 untouched=2`; every former uid 0 element is 65532:65532 with the same mode and inode; the two planted elements are identical (uid:gid, ctime to the nanosecond, inode) before and after.
- Server on the migrated volume: `mc ready local` ready after 0 s; old object byte-for-byte (sha256 `d017cc18…481f`); new object written and read back; `mc ls` shows both; server uid 65532.
- Idempotence: a second init run leaves the whole 34-element snapshot byte-identical (owner, ctime, inode).
- Fresh volume with `ln -s /etc/passwd /data/link` and a `0:65532` file: init exit 0 under `--read-only`; the symlink itself (lstat) and the regrouped file become 65532:65532; second run is a no-op.
- Measured boundary: a `0700` directory owned by 1234 inside the volume makes init exit 1 with `chown: cannot read directory '/data/locked': Permission denied`, not a success report.
- Capability search (4k) was not needed: CHOWN alone passed f/g/i.
- Inversion probes against the new ci-helm-checks section, all FAIL as intended: dropped `add`, init mountPath `/other`, init image different, chown target `/other`, `-h` replaced by `-H`, `minio` Deployment renamed away (vacuity guard).

**Task 2.** `values.yaml` gains `minio.mcImage: ""`. `wait-for-minio` and `createbucket` (job-createbucket.yaml) and `wait-for-bucket` (`octoconv.waitForDeps` in _helpers.tpl) read `.Values.minio.mcImage | default .Values.minio.image | quote`; the MinIO pod (server and init) stays on `minio.image`. `TestMinioImagePinnedByDigest` now requires every `image:` line that reads `.Values.minio.*` to be exactly the server form or the mc form (>= 2 server lines, >= 3 mc lines), and every `mcImage` value in `values*.yaml` to be empty or `name@sha256:<64 lowercase hex>` with no tag (>= 1 `mcImage` key found; zero found fails). `mcImageRefProblem` plus the `TestMcImageRefRejectsUnpinnedForms` table cover it; the old rules that forbade a separate mc key were removed. A new section in `ci-helm-checks.sh` renders both cases and asserts, from a yq classification of every Deployment/Job container: no empty `image:`, the classifier is not blind (all `wait-for-minio`, `createbucket`, `wait-for-bucket` classified as mc; 13 mc containers), the mc set is identical in both renders, empty gives `minio.image` everywhere, set gives the override everywhere, the override reaches no non-mc container, and the MinIO pod containers stay on `minio.image`.

RED was recorded before the template change (`evidence/guard-test.txt`: 0 mc lines, 0 `mcImage` keys), then GREEN. Post-commit probes all failed as expected: (a) `mcImage: "minio/mc:latest"` -> `values.yaml:279 ... not pinned by digest`; (b) `wait-for-bucket` back on the server form -> Go test (2 < 3 mc lines) and the render section (override missing on every `wait-for-bucket`); (c) createbucket without `default` -> Go test (line shape) and the render section (empty image row).

**Task 3.** README note gets one sentence saying the Helm chart needs no manual step. `GOTOOLCHAIN=go1.26.4 scripts/ci-local.sh helm gate`: `### tier helm: PASS`, `### tier gate: PASS`, transcript `.planning/ci-local/ci-local-20261001T141737Z.log`, `commit: 8ee53a8` = HEAD, no `TRACKED CHANGES NOT IN THIS COMMIT`. The only WARNING is the pre-existing one: helm v4.3.0 locally vs v4.2.3 pinned in ci.yml. After the work, no `octoconv-ncn-*` containers or volumes remain, and the dockerdev container set equals `evidence/dockerdev-before.txt`.

## Deviations from Plan

### Deliberate narrowings (from the objective)

1. **[Scope] Only elements owned by uid 0 are migrated (`--from=0`), not everything that is not 65532.** The old image ran as root, so everything it wrote is uid 0; the new image writes as 65532. An element with a third uid (for example an operator who once overrode `minio.image` with an image of another uid) does not belong to the chart, and a blind chown of the whole tree to 65532 would break that operator's volume. The probe keeps `third-uid-file` 1234:1234 untouched.
2. **[Scope] Capabilities are drop ALL + add CHOWN only.** Root owns everything the old image wrote, so it traverses the tree as the owner with no DAC capabilities, and directories MinIO creates as 65532 are 0755/0777, readable as "other". `DAC_READ_SEARCH` would be needed only for subtrees owned by someone else with restrictive modes; it conflicts with Pod Security Standard baseline (CHOWN is allowed), so a MinIO pod in a namespace labelled baseline would stop being admitted. The previous chart passed baseline. The boundary is measured (probe step j): an unreadable subtree gives a non-zero exit with `Permission denied`, not a success report.

### Auto-fixed issues

3. **[Rule 1 - Bug] The first version of the ci-helm-checks init-container section was vacuous for mount/image/target.** Found by the inversion probe: with the init mountPath changed to `/other` the section still passed. Cause: yq's `X | (select(.) | "same") // "differs"` returns `"same"` for a `false` input. Replaced with `if ... then "same" else "differs" end`, amended into the Task 1 commit, and re-probed (mountPath, image, chown target and `-H` now all FAIL). Both probe rounds are in `evidence/ownership-probe.txt`.
4. **[Rule 3 - Blocking] yq `length` over a multi-document render printed one value per document**, so the probe's init-container count was wrong; replaced with a name listing counted with `grep -c .`.

### Comment lines

No new explanatory comments were added. The comment lines in the diff are edits of phrases the work made false (values.yaml minio block, createbucket initContainers comment, `octoconv.waitForDeps` block, the test file doc comment) and three reworded count comments in the test (`// Templates: ...`, `// minio and migrate-data-ownership in minio.yaml.`, `// wait-for-minio and createbucket in job-createbucket.yaml, ...`) that replace the two comments the old counters had. The log is in `evidence/guard-test.txt`.

## Not proven

- The chart on a live cluster: OrbStack Kubernetes was off. `fsGroup` plus the init container on a real local-path PVC, and Pod Security admission of the pod, were not run. What was proven is the rendered init container under docker with the rendered argv, user, capabilities, read-only rootfs, no-new-privileges and limits.
- Operators who override `minio.image` with an image whose `chown` lacks GNU `--from`: the init container then fails loudly (pod stays in Init with chown's message) rather than silently skipping. Residual risk for that operator; the default image was measured (GNU coreutils 9.11, `--from`, `--no-dereference` present; no `find`, `xargs`, `busybox`).
- amd64: everything ran on the host architecture (arm64 image resolution); the digest is an index digest, but no amd64 run was done.
- The symlink check proves "the link itself changes and init exits 0 with a read-only rootfs"; a fresh container's `/etc/passwd` being 0:0 afterwards is trivially true and is recorded as such, not as independent evidence.

## Known Stubs

None.

## Threat Flags

None. The init container is the only new privileged surface and is covered by T-261001-ncn-01..03 in the plan (drop ALL, CHOWN only, no escalation, read-only rootfs, exec form, `-h`, `--from=0`).

## PR body addendum (English, for PR #31)

> **Addendum: two P1 follow-ups from review**
>
> **1. Existing MinIO PVCs written by the old root image.** On hostPath/local-path volumes `fsGroup` is not applied, so a PVC filled by the old root-run image left `.minio.sys` owned by root and the new uid 65532 MinIO exited at startup (`Unable to write to the backend`). The MinIO pod now has an init container `migrate-data-ownership` that runs `chown -R -h --from=0 65532:65532 /data` from the same pinned `minio.image` before the server starts.
> - It reuses `minio.image` (GNU coreutils in the Chainguard image), so there is no new image to mirror, pin or pull; `find` and `busybox` are not in the image and are not needed.
> - `--from=0` changes only elements owned by root: a volume already handed over is not touched at all (ctime unchanged), and files of any other uid are left alone. `-h` never follows a symlink out of the volume.
> - It runs as uid 0 with `drop: [ALL]`, `add: [CHOWN]`, `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`, 100m / 64Mi, exec form with no shell and no chart values in argv. `DAC_READ_SEARCH` is deliberately not added: it is not allowed by the Pod Security Standard baseline, which the previous chart satisfied. A subtree root cannot read makes the init container fail loudly instead of reporting success.
>
> **2. `minio.mcImage` is back.** It is an optional value, empty by default, used by every container that runs `mc`: `wait-for-minio` and `createbucket` in the Job and `wait-for-bucket` in every app Deployment. Empty falls back to `minio.image`; the MinIO pod itself always uses `minio.image`. A non-empty value must be pinned by digest without a tag.
>
> **How it was checked.**
> - The rendered init container (argv, user, capabilities, read-only rootfs, no-new-privileges, limits all read from `helm template`) was run under docker against a volume seeded by the pinned image as root. Control: the non-root MinIO dies on that volume with `Unable to write to the backend`. After the init container: `mc ready local` succeeds, the old object is byte-identical and a new write works; a second run leaves owner, ctime and inode unchanged; a symlink to `/etc/passwd` is not followed; an unreadable subtree gives a non-zero exit.
> - `scripts/ci-helm-checks.sh` asserts the init container shape and, for both an empty and a set `mcImage`, the image of every container that runs `mc` (13 today), that no `image:` is empty, and that the MinIO pod stays on `minio.image`. `TestMinioImagePinnedByDigest` pins the two allowed `image:` line forms and the digest pinning of any `mcImage`. Each gate was inverted to confirm it fails.
> - `scripts/ci-local.sh helm gate`: helm PASS, gate PASS on the PR head.
>
> **Not verified.** Not run on a live cluster (Kubernetes was off): `fsGroup` plus the init container on a real local-path PVC and PSS admission are untested. An operator who overrides `minio.image` with an image whose `chown` has no `--from` gets a loud init failure. No amd64 run.

## Self-Check: PASSED

- FOUND: deploy/chart/octoconv/templates/minio.yaml (migrate-data-ownership), values.yaml (`mcImage: ""`), job-createbucket.yaml, _helpers.tpl, scripts/ci-helm-checks.sh, internal/e2e/minio_image_pin_test.go, README.md
- FOUND commits: c0ad4b7, c13cfca, 8ee53a8 (`git log --oneline 90d465a..HEAD`)
- FOUND evidence: dockerdev-before.txt, tool-measure.txt, ownership-probe.sh, ownership-probe.txt, ownership-probe.verify.txt, guard-test.txt
- No octoconv-ncn-* containers or volumes; dockerdev set unchanged; tracked tree clean
