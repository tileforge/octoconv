# Phase 69: HTML, Probe & the /tmp Decision - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-09-25
**Phase:** 69-html-probe-the-tmp-decision
**Areas discussed:** Стратегия /tmp, Как preflight судит html, Обёртка chromium, Границы probe

---

## Стратегия /tmp

Pre-question measurement: `unshare -Urm` in `debian:bookworm-slim` under Docker 29.4.0 → `Operation not permitted` (root and uid 65534).

| Option | Description | Selected |
|--------|-------------|----------|
| Одна задача на контейнер | HTML_WORKER_CONCURRENCY 2→1, свой /tmp у контейнера | ✓ |
| Общий /tmp осознанно | /tmp rw, concurrency 2, известная дыра | |
| Снять seccomp-запрет | unshare + bind job/tmp на /tmp, свой seccomp-профиль | |

| Option (остатки между задачами) | Selected |
|--------|----------|
| Чистить /tmp перед каждой задачей | ✓ |
| Чистить после задачи | |
| Не чистить | |

| Option (как удержать concurrency=1) | Selected |
|--------|----------|
| Отказ стартовать при sandbox on и concurrency>1 | ✓ |
| Захардкодить 1 | |
| Комментарий + дефолт | |

| Option (probe и TMPDIR для gs) | Selected |
|--------|----------|
| Убрать /tmp из набора probe, если замер подтвердит | ✓ |
| Единая схема для обоих | |

---

## Как preflight судит html

| Option | Description | Selected |
|--------|-------------|----------|
| Маркер в несжатом поле PDF | <title>/<a href> → /Title или URI; переиспользовать requireOutput/requireDenied | ✓ |
| poppler-utils в образ chromium | pdftotext, лишний пакет в production | |
| Сравнение с эталоном | размер/байты против «файл отсутствует», хрупко | |

---

## Обёртка chromium

Pre-question inspection: wrapper is `#!/bin/sh -e` + `/usr/lib/chromium/chromium-headless-shell "$@"`.

| Option | Description | Selected |
|--------|-------------|----------|
| Через обёртку, как замерено | dash + обёртка + /usr/lib/chromium, argv не меняется | ✓ |
| Напрямую /usr/lib/chromium/… | убрать dash, изменить argv, перезамер | |

---

## Границы probe

| Option | Description | Selected |
|--------|-------------|----------|
| Только exec'и внешних движков | pdfcpu in-process → deferred | ✓ |
| Плюс Landlock на процесс probe-worker | AllThreadsSyscall, отдельная фаза | |

| Option (file(1) не замерен) | Selected |
|--------|----------|
| Замерить заново по exec-местам кода, обновить документ | ✓ |
| Убрать exec file, детектировать в Go | |

---

## Claude's Discretion

- Sweep implementation and fault classification
- D-03 refusal message wording
- /Title vs URI annotation for the html marker

## Deferred Ideas

- Confining in-process parsing (pdfcpu etc.)
- Per-task /tmp via user+mount namespaces (blocked by default seccomp)
- Todo `2026-09-06-other-engines-still-unconfined.md` reviewed, not folded (close after phase 70)
