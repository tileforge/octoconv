# Requirements: OctoConv — v1.10 OCR Engine

**Defined:** 2026-07-26
**Core Value:** Внутренние сервисы компании могут безопасно и надёжно поставить задачу конвертации файла и получить результат — без риска для стабильности/безопасности продакшена.

## v1 Requirements

Шестой engine-класс: OCR / извлечение текста. Classic CPU-OCR (Tesseract), offline, CPU-only. Всё поверх существующего async-контура; онлайн-прогресс переиспользует v1.9-инфру движок-агностично.

### OCR — Core extraction

- [x] **OCR-01**: Клиент может извлечь текст из изображения (jpg/png/tiff/webp) через classic CPU-OCR и получить plain-txt результат.
- [ ] **OCR-02**: Клиент может извлечь текст из сканированного/image-only PDF; воркер растеризует его постранично (bounded, memory-safe — `pdftoppm -f N -l N`, не all-at-once) и OCR-ит каждую страницу.
- [x] **OCR-03**: Клиент может получить hOCR/ALTO вывод (координаты слов/строк + confidence) наряду с txt (корректное XML-экранирование, bbox в пространстве DPI растеризации).
- [ ] **OCR-04**: In-flight многостраничный OCR-job отдаёт честный page-granular `progress_pct` + `eta_seconds` (через GET/SSE/webhook, переиспользуя v1.9-инфру без изменений); одиночное изображение отдаёт `null`→100.

### OCRO — Options

- [x] **OCRO-01**: Клиент может задать язык(и)/скрипт OCR per-job через валидированный `opts` (closed allowlist, комбинируемо напр. `eng+rus`; mirror AudioOpts + injection-тест). Неизвестный язык → 422 до любой работы.

### OCRE — Engine safety / hardening

- [ ] **OCRE-01**: Загрузки гейтятся fail-closed magic-byte валидацией (изображения + PDF `%PDF-`) до любого OCR; declared/detected mismatch → 422.
- [ ] **OCRE-02**: Оверсайз-ввод (число страниц PDF сверх `OCR_MAX_PAGES`, либо размеры изображения сверх cap) отвергается fail-closed ДО растеризации/декода — memory-bomb гард (аналог `MAX_IMAGE_PIXELS`/AVE-05, enforced pre-render).
- [ ] **OCRE-03**: Born-digital PDF (уже содержит текстовый слой) отвергается fail-closed (422) в v1, а не переОCR-ится вслепую (detect-and-passthrough отложен в v2).
- [ ] **OCRE-04**: `OCR_ENGINE_TIMEOUT` (и `OCR_MAX_PAGES`) **измерены** собственным гейтом (`scripts/ocr-timing-measure.sh`, seconds-per-page), а не скопированы у соседнего движка; таймаут под глобальным reconciler-CAP.
- [ ] **OCRE-05**: Оба новых untrusted-процесса (растеризатор `pdftoppm` + `tesseract`) исполняются через hardened `runCommand` (Setpgid + SIGKILL-on-timeout, ресурс-лимит, `nobody`); пустой/безтекстовый ввод завершается `done` с пустым txt (не `failed`).

### OCRP — Async pipeline & scale

- [ ] **OCRP-01**: OCR встроен в полный async-контур end-to-end (submit → done → download): миграция `0009_ocr_engine.sql` (добавляет `ocr` в `jobs.engine` enum), очередь `ocr`, `cmd/ocr-worker`, stage-aware transient/terminal retry, API-роутинг; single job → single target формат (mirror AudioConverter). Реестр SEED-010 остаётся dormant (compile-time `Registry.Register`).
- [ ] **OCRP-02**: OCR-воркер контейнеризован (`Dockerfile.ocr-worker`, Tesseract source-built + tag-pinned, `tessdata_fast` SHA-pinned, poppler-utils) и автоскейлится через KEDA scale-from-zero, с таймаутами из измеренных OCRE-04 значений; compose + Helm chart интеграция.

## v2 Requirements

- **OCR-V2-01**: Detect-and-passthrough для born-digital PDF (извлечь существующий текстовый слой вместо pixel-OCR) — нужен свой opts/output контракт.
- **OCR-V2-02**: Searchable-PDF вывод (текстовый оверлей на скан).
- **OCR-V2-03**: Deskew/denoise preprocessing (unpaper/leptonica) — evidence-gated, если измеренная точность на реальных сканах потребует.
- **OCR-V2-04**: Self-hosted VLM путь (Surya/GOT-OCR2/Qwen2-VL) для грязного/рукописного/layout→markdown — отдельный GPU-контур (SEED-009 путь «(b)»).
- **OCR-V2-05**: OSD / auto-orientation (детект поворота + авто-ротация повёрнутых сканов через `--psm 0` pre-pass) — deferred из Phase 43 (D-05a), evidence-gated как deskew: не бесплатно (лишний subprocess/страница), PSM 3 = no-OSD.

## Out of Scope

| Feature | Reason |
|---------|--------|
| Cloud OCR / cloud VLM API | Нарушает offline-constraint проекта (как для audio/av) |
| Self-hosted VLM (GPU) в этой вехе | Ломает CPU-only контур; отдельная веха (v2) |
| Layout/semantic understanding, layout→markdown | VLM-территория, не classic OCR |
| Handwriting recognition | Tesseract слаб; вне classic-OCR |
| Searchable-PDF overlay (v1) | Третий output-формат сверх locked txt+hOCR/ALTO; → v2 |
| Dynamic capability registry | SEED-010 отложен; OCR садится на compile-time схему как image/av |

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| OCR-01 | Phase 43 | Complete |
| OCR-02 | Phase 43 | Pending |
| OCR-03 | Phase 43 | Complete |
| OCR-04 | Phase 43 | Pending |
| OCRO-01 | Phase 43 | Complete |
| OCRE-01 | Phase 44 | Pending |
| OCRE-02 | Phase 43 | Pending |
| OCRE-03 | Phase 43 | Pending |
| OCRE-04 | Phase 45 | Pending |
| OCRE-05 | Phase 44 | Pending |
| OCRP-01 | Phase 44 | Pending |
| OCRP-02 | Phase 46 | Pending |

**Coverage:**
- v1 requirements: 12 total
- Mapped to phases: 12 (Phases 43-46)
- Unmapped: 0

---
*Requirements defined: 2026-07-26*
*Last updated: 2026-07-26 after v1.10 roadmap creation — traceability finalized (Phases 43-46, 12/12 mapped, no orphans)*
