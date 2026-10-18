---
created: 2026-10-15
area: cad
severity: low
origin: вопрос пользователя во время v1.16 (фаза 76)
audit_acknowledged:
  milestone: v1.16
  at: 2026-10-18
---

# CAD-задания не показывают прогресс: `progress_pct` остаётся `null` до конца

## Decision (2026-10-18)

The operator decided this goes into the next milestone, ahead of other candidates.

## What

Прогресс есть только у двух движков:
- `av` — ffmpeg `-progress`, процент по обработанному времени к длительности источника
  (`internal/convert/progress.go`, `internal/convert/av.go` `newTranscodeProgressReporter`);
- `ocr` — по страницам с ETA (`internal/convert/ocrprogress.go`).

Клиент видит его через `GET /v1/jobs/{id}` (`progress_pct`, `eta_seconds`), SSE
`GET /v1/jobs/{id}/events` (`internal/api/sse.go`) и опциональный progress-webhook
(`SetProgressWebhook`/`SetProgressThrottle`, `internal/worker/worker.go`).

CAD-маршруты (STEP/IGES/STL → convert/render, DXF → svg/pdf/png) идут минутами
(замеры 76-02/76-03: сетка до ~216 с, cad-dxf до ~124 с, rsvg до ~127 с на этап),
а клиент видит только `queued` → `active` → `done|failed`.

## Idea

Движки (OpenCascade, cad-render, ezdxf, rsvg-convert) не сообщают долю работы,
честного процента нет. Вместо него — этап и номер этапа:
- 3D: guard → mesh → render → judge;
- DXF: guard → cad-dxf → rsvg (ink) → rsvg (pdf/png) → judge.

Варианты:
1. Новое поле `stage` (enum) + `stage_index`/`stage_count` в статусе, SSE и progress-webhook;
   `progress_pct` остаётся `null` (не выдумываем проценты).
2. Ступенчатый `progress_pct` по границам этапов (например доля от измеренных сроков этапов
   76-03) — проще для клиентов, но процент не отражает реальную работу внутри этапа.

Предпочтение: вариант 1; ETA — сумма сроков оставшихся этапов как верхняя граница, если она
полезна клиентам.

## Constraints

- Контракт: новое поле — расширение openapi.yaml, docs/CAD.md; SSE эмитит только при изменении.
- Запись прогресса идёт через неблокирующий `UpdateProgress` с троттлингом; этапов мало,
  нагрузка на БД пренебрежимая.
- Не трогать `internal/convert/exec.go`.
