---
phase: "69"
slug: "html-probe-the-tmp-decision"
status: verified
# threats_open = count of OPEN threats at or above workflow.security_block_on severity (the blocking gate)
threats_open: 0
asvs_level: 1
created: "2026-09-29"
---

# Phase 69 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| client HTML → chromium engine | client markup rendered by a large third-party binary with --no-sandbox; file:// subresources and iframes resolved by the engine | untrusted document bytes (executable if the CSP does not govern them) |
| engine → shared /tmp | the only writable path html shares across tasks | other jobs' intermediate files |
| worker env → engine env | engines inherit only what engineEnv hands them; /proc/self/environ is inside the html grant | credentials if leaked into the env |
| operator config → worker startup | HTML_SANDBOX_DISABLED, PROBE_SANDBOX_DISABLED, HTML_WORKER_CONCURRENCY change confinement | confinement on/off |
| client document → gs / pdfinfo / unzip / file | untrusted PDF/OOXML parsed by third-party engines in the probe/verifier path | untrusted document bytes |
| job directory → sub-documents | the raw upload (in.html) sits beside rendered.html and could be loaded as a separate document | un-CSP'd copy of the upload |
| deployment config → engine runtime | chart/compose decide what the chromium container is given (/dev/shm) | none — availability only |
| evidence → phase/gap acceptance | acceptance rests on live transcripts; a transcript from the wrong build would accept an unproven system | proof integrity |
| future code → engine exec | a later change could reintroduce an unconfined exec or re-grant /tmp | confinement regression |

---

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Status |
|-----------|----------|-----------|----------|-------------|------------|--------|
| T-69-01 | Information Disclosure | ChromiumConverter.Convert | high | mitigate | forJobRW ruleset (rw job dir + /tmp + /dev/null, ro measured paths + wrapper); HTMLPreflight negative run; live gate | closed |
| T-69-02 | Information Disclosure | /proc/self/environ via iframe | high | mitigate | engineEnv("") only PATH/HOME/LANG/LC_ALL; `TestChromiumEngineEnvCarriesNoSecrets` (htmlsandbox_test.go:388) | closed |
| T-69-03 | Information Disclosure | shared /tmp across concurrent tasks | high | mitigate | sharedTmpGuard refuses confined concurrency > 1; gate concurrency-guard | closed |
| T-69-04 | Information Disclosure | leftovers of a killed task in /tmp | medium | mitigate | sweepSharedTmp (htmlsandbox.go:115) before every engine run | closed |
| T-69-05 | Spoofing (false confinement proof) | HTMLPreflight | high | mitigate | /Title marker from production Convert path; engine-level negative; requireOutsideGrants | closed |
| T-69-06 | DoS / Repudiation | cmd/chromium-worker startup | high | mitigate | refusal + preflight before Postgres; gates refuses-without-landlock, preflight-catches-restrict-self (sandbox-liveproof.sh:230,296) | closed |
| T-69-07 | Tampering (misclassification) | isHTMLTerminal | medium | mitigate | ErrEngineSandboxUnavailable checked first (worker.go:253); control row | closed |
| T-69-08 | Tampering | compose / .env.example / chart | medium | mitigate | HTML_SANDBOX_DISABLED `${VAR:-}` forwarding, documented but unset | closed |
| T-69-09 | Tampering (drift) | D-08 wrapper triad | low | mitigate | pinned-set test; gate wrapper-as-measured (sandbox-liveproof.sh:369) | closed |
| T-69-10 | Denial of Service | sweep of a root-owned /tmp entry | low | accept | see AR-69-01 | closed |
| T-69-11 | Information Disclosure | gs/pdfinfo/unzip/file | high | mitigate | per-job forJobRW ruleset (probesandbox.go:92); ProbePreflight (probesandbox.go:179) content-judged denials; live gate | closed |
| T-69-12 | Info Disclosure / Tampering | TMPDIR handling | high | mitigate | TMPDIR only in engineEnv(jobDir); no process-wide `os.Setenv("TMPDIR")` in internal/ (negative grep clean) | closed |
| T-69-13 | Denial of Service | scratch dirs under /tmp with no /tmp grant | high | mitigate | MkdirTemp under the job dir: sanitizepdf.go:264, sanitizeverify.go:855 | closed |
| T-69-14 | Tampering (misclassification) | classifyEngineFailure / isProbeTerminal | high | mitigate | sentinel preserved at wrap points, checked first (descriptors.go:313); control table | closed |
| T-69-15 | Spoofing (false confinement proof) | ProbePreflight | high | mitigate | per-engine positive/negative judged by content; file(1) exit-0 denial judged by output; requireOutsideGrants | closed |
| T-69-16 | Information Disclosure | in-process parsing (pdfcpu, Go OOXML/image) | medium | accept | see AR-69-02 | closed |
| T-69-17 | Tampering | compose / .env.example / chart | medium | mitigate | PROBE_SANDBOX_DISABLED `${VAR:-}` forwarding, documented but unset | closed |
| T-69-18 | Repudiation (false green) | live proof | high | mitigate | images rebuilt from tree, -count=1, pinned toolchain, transcripts carry commit/run/image ids (69-VERIFICATION.md) | closed |
| T-69-19 | Information Disclosure | future unconfined exec in html/probe files | high | mitigate | TestConfinedClassesExecOnlyThroughTheSandbox (sandboxcoverage_test.go:463); chromium.go inversion performed 2026-09-27 (69-UAT.md test 1) | closed |
| T-69-20 | Tampering (drift) | pinned sets vs SANDBOX-PATHS.md | medium | mitigate | six-class conf05 cross-check incl. readwrite (conf05-crosscheck.sh) | closed |
| T-69-21 | Information Disclosure | OOXML/image verifier engines under ruleset | medium | mitigate | confined e2e checks magic bytes and absence of vbaProject.bin (sanitize_confined_e2e_test.go:72) | closed |
| T-69-22 | Elevation of Privilege | injectCSPFirst → renderer | critical | mitigate | CSP spliced at document start (chromium.go:145); 24 unit rows; 18 live differential rows; UAT test 2 pass | closed |
| T-69-23 | Elevation of Privilege | framed/embedded raw upload (in.html) | critical | mitigate | frame-src 'none', child-src 'none', object-src 'none' (chromium.go:55) | closed |
| T-69-24 | Repudiation (false green) | live CSP check | high | mitigate | per-row control, /Title sufficiency, exact subtest count CSP_LIVE_ROWS (sandbox-liveproof.sh:21) | closed |
| T-69-25 | Tampering (rendering regression) | doctype/BOM handling | medium | mitigate | doctype/BOM position asserted per row; injectPrintCSS (chromium.go:91) composition test | closed |
| T-69-26 | Tampering | host-built test binary mounted into image | low | accept | see AR-69-03 | closed |
| T-69-27 | Denial of Service | chromium-worker without /dev/shm mount | medium | mitigate | large-document-without-dev-shm gate (sandbox-liveproof.sh:563) with negative control; UAT test 3 pass | closed |
| T-69-28 | Repudiation (false green) | large-document check | high | mitigate | /dev/shm size logged; negative control fails in the engine on /dev/shm; fixture/PDF floors asserted | closed |
| T-69-29 | Tampering (drift) | chart/compose/gate vs SANDBOX-PATHS.md | low | mitigate | no dshm/shmSize/shm_size in deploy/ or docker-compose.yml (negative grep clean); comments cross-reference SANDBOX-PATHS.md | closed |
| T-69-SC | Tampering | package installs | low | accept | see AR-69-04 | closed |

*Status: open · closed · open — below high threshold (non-blocking)*
*Severity: critical > high > medium > low — only open threats at or above workflow.security_block_on count toward threats_open*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-69-01 | T-69-10 | A sweep failure on a root-owned /tmp entry is retryable and loud; the preflight's Convert sweeps too, so the worker refuses to start on such a node; the image's /tmp has no root-owned files | 69-01-PLAN threat model | 2026-09-29 |
| AR-69-02 | T-69-16 | In-process parsing (pdfcpu, Go OOXML/image) is not an engine exec; out of scope per D-09, recorded in 69-CONTEXT Deferred as a separate design | 69-02-PLAN threat model | 2026-09-29 |
| AR-69-03 | T-69-26 | Host-built test binary is a read-only bind mount into a throwaway --rm container, never baked into or shipped with an image | 69-04-PLAN threat model | 2026-09-29 |
| AR-69-04 | T-69-SC | No new dependency: go.mod/go.sum unchanged by this phase (last touched in 60-04) | 69-04/69-05-PLAN threat model | 2026-09-29 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-09-29 | 30 | 30 | 0 | /gsd-secure-phase 69 (orchestrator, ASVS L1 grep-depth short-circuit) |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-09-29
