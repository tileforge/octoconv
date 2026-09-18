---
quick_id: 260918-rdm
type: execute
description: переписать README.md по скиллу create-readme — короче, структурнее, только то, что на main
files_modified:
  - README.md
autonomous: true
---

<objective>
README.md — 566 строк, 4114 слов, и растёт линейно от каждой фазы: девять
классов движков, а раздел «Conversion engines» знает пять; девять милстоунов
шипнуто после v1.8, а Roadmap заканчивается на v1.8; `/v1/inspect`,
`/v1/jobs` (список), `/v1/jobs/{id}/events`, `/v1/jobs/{id}/cancel`,
`/v1/system/usage` не описаны нигде; `docs/openapi.yaml`, `docs/ERROR-CODES.md`,
`docs/SANITIZE.md`, `docs/TYPESETTING.md` не залинкованы ни разу.

Переписать по скиллу `create-readme` (`~/.agents/skills/create-readme/SKILL.md`):
короче, сканируемо, GFM + GitHub admonitions, без секций Contributing и License
(для них есть отдельные файлы), без злоупотребления эмодзи.

Output: README, который описывает состояние `main` — включая Landlock-песочницу
(#21) и не включая CPU-бюджет (#22) и Correlation-Id (#23), которые ещё в ревью.
</objective>

<tasks>

<task type="auto">
  <name>Task 1: Переписать README.md</name>
  <files>README.md</files>
  <read_first>
    - `internal/convert/readme_sync_test.go` — README под тестом. Маркер
      `<!-- engine-classes:` и строка бэктик-токенов сразу после него держатся
      равными `convert.AllEngines()` в обе стороны; отдельный тест падает на
      фразах вида "nine engine classes". Маркер переносить вместе с секцией,
      не удалять; количество классов не писать словом или цифрой.
    - `.planning/ROADMAP.md` — шипнутые милстоуны (последний v1.14, 2026-09-05)
    - `internal/api/routes.go` — полный список маршрутов
    - `internal/convert/converters.go`, `ocr.go`, `pandoc.go`,
      `sanitizeconverter.go`, `tectonic.go` — форматные пары девяти классов
      и две операции (`convert`, `sanitize`)
  </read_first>
  <action>
Структура: заголовок + badges + абзац → ToC → Why → Engines (таблица вместо
буллетов, все девять классов) → Architecture (диаграмма + дерево в `<details>`)
→ Quick Start (5 шагов; tex-бандл и Landlock — в admonitions/`<details>`)
→ API (таблица маршрутов + семантика статусов) → Quotas (сжать ~100 строк до
~25, операционный разбор «застрявшая джоба ест бюджет» — в `<details>`)
→ Sandboxing → Configuration (категории + ссылка на `.env.example`)
→ Documentation (ссылки на `docs/*`) → MCP (stdio + HTTP одной секцией)
→ Frontend → Roadmap (до v1.14, шипнутое — в `<details>`).

Не выдумывать: всё, что попадает в README, должно быть на `main`. Не описывать
`TECTONIC_ENGINE_CPU_SECONDS` (#22) и `Correlation-Id` (#23).

Секции Contributing и License удалить — так велит скилл; `LICENSE` в репозитории
остаётся, badge на него сохранить.
  </action>
  <verify>go test ./internal/convert/ -run 'TestREADME' -count=1 -v 2>&1 | tail -20; wc -l README.md</verify>
  <acceptance_criteria>
    - `TestREADMEEngineClassesMatchAllEngines` и `TestREADMEDoesNotHardcodeTheEngineCount` зелёные
    - Нет секций `## Contributing` и `## License`
    - Все девять классов перечислены в таблице движков
    - Roadmap доходит до v1.14
    - `/v1/inspect`, `/v1/jobs`, `/v1/jobs/{id}/events`, `/v1/jobs/{id}/cancel`, `/v1/system/usage` описаны
    - `docs/openapi.yaml`, `docs/ERROR-CODES.md`, `docs/SANITIZE.md`, `docs/TYPESETTING.md` залинкованы
    - Ни `TECTONIC_ENGINE_CPU_SECONDS`, ни `Correlation-Id` не упомянуты
    - Файл заметно короче 566 строк
    - Каждая ссылка на файл репозитория указывает на существующий путь
  </acceptance_criteria>
  <done>README описывает `main` целиком и читается за пару минут</done>
</task>

</tasks>

<verification>
- `go test ./internal/convert/ -run TestREADME -count=1` — зелёный
- Проверить каждую относительную ссылку README на существование файла
- `grep -n "TECTONIC_ENGINE_CPU_SECONDS\|Correlation-Id" README.md` — пусто
</verification>
