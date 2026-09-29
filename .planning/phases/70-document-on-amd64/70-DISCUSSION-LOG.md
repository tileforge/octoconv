# Phase 70: Document on amd64 - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-09-29
**Phase:** 70-document-on-amd64
**Areas discussed:** Где взять amd64 / снять пин, Локальная разработка, /tmp и concurrency, Конфайнмент veraPDF

---

## Где взять amd64 / снять пин

| Option | Description | Selected |
|--------|-------------|----------|
| Сначала замерить снятие пина | verapdf stage pinned amd64, native final image; unpin if veraPDF equivalent, else fall back to amd64 node | ✓ |
| amd64-узел, пин не трогаем | As CONF-07 is written; needs a real amd64 machine | |
| Оба | Unpin and also run on amd64 | |

**User's choice:** Measure the unpin first.

| Option | Description | Selected |
|--------|-------------|----------|
| Остановиться и спросить | Checkpoint; choosing a host is an outward-facing user action | ✓ |
| GitHub Actions workflow_dispatch | Native amd64 runner, scarce minutes | |
| Своя amd64 VM / узел | User-provided | |

**User's choice:** Fallback = stop and ask.

| Option | Description | Selected |
|--------|-------------|----------|
| Дифференциальный тест по корпусу | Same corpus through amd64 (emulated) and arm64; verdicts and failed rules match; no native .so in /opt/verapdf | ✓ |
| Существующих тестов достаточно | PDFA e2e + verapdf_test green on arm64 | |

**User's choice:** Differential corpus test.

| Option | Description | Selected |
|--------|-------------|----------|
| Так же, как у семи классов | Directory grants, preflight guards prod, one DEBT entry for all eight classes | ✓ |
| Требовать amd64-гейт для document | Phase not closed without real amd64 run | |

**User's choice:** Same as the seven classes.

---

## Локальная разработка

| Option | Description | Selected |
|--------|-------------|----------|
| Отказ, включение руками | `${DOCUMENT_SANDBOX_DISABLED:-}` empty in compose; worker refuses and names the variable | ✓ |
| В compose отключено по умолчанию | Convenient, but compose stops mirroring prod | |

**User's choice:** Refusal, opt out by hand.

| Option | Description | Selected |
|--------|-------------|----------|
| Один отказ + живой прогон под эмуляцией | Shared refusal message; live gate runs the --platform linux/amd64 image on arm64 and requires refusal before Postgres | ✓ |
| Разные сообщения | GOARCH vs uname emulation detection | |

**User's choice:** One refusal + live emulation step.

---

## /tmp и concurrency

| Option | Description | Selected |
|--------|-------------|----------|
| Как в probe: сначала замер TMPDIR | Measure TMPDIR / java.io.tmpdir / -XX:-UsePerfData; no /tmp grant if all writes move, else phase 69 decision | ✓ |
| Сразу решение 69 | Concurrency 1 + refuse + sweep without measuring | |

**User's choice:** Measure first.

---

## Конфайнмент veraPDF

| Option | Description | Selected |
|--------|-------------|----------|
| Свой ruleset, без права на запись | Separate measured set for java; content-judged preflight on the XML report | ✓ |
| Общий ruleset с soffice | One set per class | |
| veraPDF вне скоупа | Leave pending with DEBT | |

**User's choice:** Its own read-only ruleset.

---

## Claude's Discretion

- soffice preflight conversion and marker field (rule fixed: content-judged, raw-byte marker, no new packages)
- Escape-hatch variable name (`<CLASS>_SANDBOX_DISABLED` pattern)
- Composition of the D-02 differential corpus

## Deferred Ideas

- amd64 production confirmation for all eight confined classes (one DEBT entry)
- In-process parsing of untrusted input in Go workers (carried from phase 69 D-09)
