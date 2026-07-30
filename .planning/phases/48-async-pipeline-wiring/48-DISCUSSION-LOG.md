# Phase 48: Async Pipeline Wiring - Discussion Log

> **Audit trail only.** Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-30
**Phase:** 48-async-pipeline-wiring
**Areas discussed:** Retry classification (isPandocTerminal), Content-sniff wiring + opts-dispatch, PandocUniqueTTL placeholder

---

## Stage-aware retry classification (isPandocTerminal)

| Option | Description | Selected |
|--------|-------------|----------|
| Timeout+engine terminal (like document) | Timeout = terminal (pathological input won't speed up on retry, DOC-08); pandoc parse/engine errors = terminal; only genuine infra transients retry | ✓ |
| Timeout transient (like OCR/image) | Timeout retryable (load spike) — but pandoc is deterministic, retries burn budget | |

**User's choice:** Timeout+engine terminal (like document)
**Notes:** pandoc is deterministic CPU-bound with no external services/network — a timeout means pathological/oversize input. Engine-scoped classifier before the shared isTerminal fallthrough (mirror isDocumentTerminal). Empty-but-valid output → done (SC4).

---

## Content-sniff wiring + opts-dispatch

| Option | Description | Selected |
|--------|-------------|----------|
| Mirror html/OCR precedent | detected=="" markup-family dispatch (latex→LooksLikeLaTeX; md/rst/org→LooksLikeText trust-declared; docx/epub via SniffContainer) + EnginePandoc opts-dispatch + enqueue-switch + enforcePandocInputSize | ✓ |
| Discuss details | Dig into pandoc-html vs chromium-html disambiguation | |

**User's choice:** Mirror html/OCR precedent
**Notes:** EngineFor's (source,target) routing disambiguates automatically (html→pdf=chromium, html→md=pandoc; docx→pdf=document, docx→md=pandoc). Registry.Register panics on duplicate pairs → PandocConverter.Pairs() must be disjoint (already is — no →pdf pairs); add a disjointness test.

---

## PandocUniqueTTL + retry schedule

| Option | Description | Selected |
|--------|-------------|----------|
| Placeholder timeout + short retry | Marked placeholder PANDOC_ENGINE_TIMEOUT (superseded Phase 49, OCR 300s→744s precedent); PandocUniqueTTL from own budget (never copied); short 2-entry retry | ✓ |
| Discuss | Other approach | |

**User's choice:** Placeholder timeout + short retry
**Notes:** Real timeout measured in Phase 49. TTL = f(PANDOC_ENGINE_TIMEOUT, PANDOC_MAX_RETRY) from pandoc's OWN budget — never copied (the project's flagged anti-pattern).

## Claude's Discretion
- Placeholder timeout/max-retry defaults + retry-schedule values; pandocFailureCode mapping; migration ordinal (0010); cmd/pandoc-worker env-resolver shape (mirror cmd/ocr-worker minus OCR cgroup bits).

## Deferred Ideas
- Container + measured timeout → Phase 49; KEDA/Helm → Phase 50; →pdf/broad formats/lua-filters → v2.
