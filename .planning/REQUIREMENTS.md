# Requirements: OctoConv — v1.11 Pandoc Engine

**Defined:** 2026-07-30
**Core Value:** Внутренние сервисы компании могут безопасно (через аутентификацию по API-ключу) и надёжно поставить задачу конвертации файла и получить результат — без риска для стабильности/безопасности продакшена.

## v1 Requirements

Седьмой engine-класс: pandoc markup/document interconvert. Один зрелый offline CLI-бинарник, CPU-only. Всё поверх существующего async-контура; клон проверенного engine-class паттерна (image/document/html/audio/av/ocr). **Только markup↔markup — →pdf отложен в v2.**

### PDOC — Core interconvert

- [ ] **PDOC-01**: Клиент может сконвертировать документ между markup-форматами через pandoc — закрытый `(from→to)` allowlist по markup-семейству (`md`, `rst`, `org`, `latex`, `epub`, `html`, `docx`); неподдерживаемая пара → 422 до любой работы (mirror существующих `Pairs()`-наборов).
- [ ] **PDOC-02**: Выход — корректный **standalone**-документ там, где формат этого требует (docx/epub/html получают полноценную структуру через `--standalone`), а не голый фрагмент; результат валидируется тем же content-sniffer'ом, что и вход.

### PDCO — Options

- [ ] **PDCO-01**: Клиент может задать per-job `opts` через валидированный закрытый allowlist (напр. `standalone`, `toc`, `wrap`, `section-divs`; типизированная структура, strict-parse mirror `OCROpts`/`AudioOpts`); неизвестный/незалистенный opt → 422 до любой работы; клиентские байты никогда не попадают в argv pandoc без валидации (injection-тест).

### PDCE — Engine safety / hardening

- [ ] **PDCE-01**: Загрузки гейтятся fail-closed magic-byte/content-валидацией до запуска pandoc; declared/detected mismatch → 422 (text/markup-семейство сниффится корректно, не только бинарные форматы вроде docx/epub).
- [ ] **PDCE-02**: pandoc исполняется в песочнице — raw HTML/TeX passthrough **отключён**, `--no-shell-escape`, **без сети**, через hardened `runCommand` (Setpgid + SIGKILL-on-timeout, resource-лимит, `USER nobody`). Клиентский ввод не может вызвать shell-escape / `\input` / сетевой fetch.
- [ ] **PDCE-03**: Оверсайз/патологический ввод (размерный cap; pandoc-specific bomb — напр. чрезмерно вложенные структуры/раздувание при конвертации) отвергается fail-closed ДО конвертации — memory/DoS-гард.
- [ ] **PDCE-04**: `PANDOC_ENGINE_TIMEOUT` **измерен** собственным гейтом (не скопирован у соседнего движка), под глобальным reconciler-CAP; пустой-но-валидный выход завершается `done`, не `failed`.

### PDCP — Async pipeline & scale

- [ ] **PDCP-01**: Pandoc встроен в полный async-контур end-to-end (миграция добавляет `pandoc` в `jobs.engine` enum, очередь `pandoc`, `cmd/pandoc-worker`, stage-aware transient/terminal retry, reconciler-routing, API-routing); single input → single target формат, поверх неизменной v1.9 progress/webhook инфры.
- [ ] **PDCP-02**: pandoc-воркер контейнеризован (`Dockerfile.pandoc-worker`, pandoc pinned, **без TeX** — лёгкий образ) и автоскейлится через KEDA scale-from-zero с измеренным таймаутом; compose + Helm chart интеграция; IN-02 env-паритет `PANDOC_*` across всех queue-client сервисов.

## v2 Requirements

- **PDOC-V2-01**: →pdf вывод через внешний движок (weasyprint HTML/CSS или tectonic LaTeX) — отложено: тяжелит образ/cold-start, а docx→pdf/html→pdf уже покрыты LibreOffice/chromium; включить при реальном спросе на markup→pdf.
- **PDOC-V2-02**: Расширенный format-allowlist (textile, mediawiki, asciidoc, ipynb, typst и т.д.) — по спросу, сверх markup-семейства v1.
- **PDOC-V2-03**: Пользовательские `--lua-filter`/шаблоны/reference-doc — требует отдельного sandbox/upload-контракта для недоверенного кода.

## Out of Scope

| Feature | Reason |
|---------|--------|
| →pdf в pandoc-движке (v1) | Тянет TeX/HTML-движок (тяжёлый образ, медленный scale-from-zero); docx→pdf = LibreOffice, html→pdf = chromium уже покрывают |
| docx→pdf / html→pdf через pandoc | Дублирование существующих document/html движков — pandoc владеет ТОЛЬКО markup-семейством |
| Raw HTML/TeX passthrough, shell-escape, `--lua-filter` из клиентского ввода | Security: reintroduce injection/`\input`/RCE surface на недоверенном вводе |
| Сетевые ресурсы при конвертации (remote images/includes) | Offline-constraint проекта; no-network в воркере |
| Новые Go-зависимости | pandoc — CLI-subprocess через существующий hardened `runCommand`, как все прочие движки |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| PDOC-01 | TBD | Pending |
| PDOC-02 | TBD | Pending |
| PDCO-01 | TBD | Pending |
| PDCE-01 | TBD | Pending |
| PDCE-02 | TBD | Pending |
| PDCE-03 | TBD | Pending |
| PDCE-04 | TBD | Pending |
| PDCP-01 | TBD | Pending |
| PDCP-02 | TBD | Pending |

**Coverage:**
- v1 requirements: 9 total
- Mapped to phases: 0 (roadmap pending)
- Unmapped: 9 ⚠️

---
*Requirements defined: 2026-07-30*
*Last updated: 2026-07-30 at milestone v1.11 start*
