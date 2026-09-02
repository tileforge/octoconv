# OctoConv

## What This Is

OctoConv — внутренний асинхронный сервис конвертации файлов на Go для сервисов компании. Клиент отправляет файл через API, сервис кладёт его в S3-совместимое хранилище, ставит задачу в очередь (asynq/Redis), воркер запускает внешний движок конвертации и складывает результат обратно в S3. На `main` — три production-hardened класса движков: изображения через libvips (v1.0/v1.1), офисные документы через LibreOffice headless — включая кросс-конвертацию docx↔odt/xlsx↔ods/pptx↔odp и PDF/A-2b экспорт через validated opts (v1.2/v1.3), и HTML→PDF через chromium-headless с офлайн-рендерингом (v1.3). Вокруг них: обязательная API-key аутентификация с ротацией, rate limiting, HMAC-подписанная webhook-доставка через выделенные избыточные webhook-воркеры (переживает деплой/падение любого engine-воркера; reconciler-sweeper выбирается через Postgres advisory lock), корректный transient/terminal retry per-engine, fail-closed валидация содержимого по magic bytes (включая отказ OLE-CFB legacy/encrypted входов), защита от decompression bomb, TTL-очистка хранилища и полная наблюдаемость (Prometheus-метрики, реальный health-check, asynqmon-дашборд).

## Core Value

Внутренние сервисы компании могут безопасно (через аутентификацию по API-ключу) и надёжно поставить задачу конвертации файла (изображения, офисные документы, HTML) и получить результат — без риска для стабильности или безопасности продакшена.

## Current State

**v1.13 Inspect & Operations — SHIPPED 2026-09-02.** OctoConv перестал быть только
конвертером. Клиент может синхронно спросить, ЧТО у него за файл, до того как
заплатит за джобу — `POST /v1/inspect` отдаёт определённый по магическим байтам
формат, закрытый словарь из 14 находок с уровнями, и доступные для этого файла
тройки `(операция, цель)` с живой проверкой квоты и оценкой верхней границы
времени, несущей собственное происхождение. Эндпоинт ничего не пишет и ничего не
закрепляет, чем позже можно было бы обойти fail-closed валидацию при создании
джобы.

`operation` стало настоящим измерением: реестр перешёл с пар `(from, to)` на
тройки `(operation, from, to)`, `Register` теперь паникует на коллизии вместо
тихой перезаписи, два упорядоченных fail-closed гейта 422 отвергают мусор до
единого байта в S3, а выходы получили закрытый enum `role` — и `handleGetJob`
выбирает `download_url` по роли, а не по порядковому номеру, чем закрыт живой
дефект, когда первый загруженный выход слепо презайнился как «результат».

Восьмой engine-класс `probe` — первый, который ничего не конвертирует. PDF
пересобирается через PostScript; макро- и объектные части OOXML удаляются **по
содержимому и по типу relationship**, никогда по имени или каталогу; метаданные
изображений снимаются структурно, а сжатые данные копируются побайтно. Каждый
выход затем перепроверяется реализацией, которая его не удаляла — pdfcpu для
PDF, unzip и libmagic для OOXML, полный декод для изображений — и выход, который
не удалось проверить, удаляется, а не отдаётся. Отчёт собран из двух закрытых
словарей, без вердикта и без булева `clean`, и тест валит сборку на
утвердительном заявлении о безопасности.

Born-digital PDF перестали отвергаться: PDF с текстовым слоем в `txt` получает
этот слой, а `hocr`/`alto` по-прежнему рендерятся — потому что `pdftotext` не
даёт боксов слов и строк. Оператор получил `manage-clients cancel-job` вместо
сырого SQL, который чинил квоту и одновременно ломал её учёт.

**Что милстоун стоил, и это стоит помнить.** Фаза 60 прошла восемь раундов
внешнего ревью, 26 находок, 23 закрыты до мержа. Двенадцать из первых
четырнадцати — один и тот же класс: компонент работает, а связи с ним нет, либо
два компонента разделяют одно неверное допущение. Сработало не «больше тестов
того же вида», а то, что независимая проверка была сделана **шире** проверяемого
— и немедленно нашла два дефекта удаляющей стороны, которых восемь раундов
ревью не нашли.

**v1.12 Consumer Accounting & Fairness — SHIPPED 2026-08-07.** Платформа впервые знает своих потребителей. Каждый терминальный переход джобы (`done`/`failed`/`canceled`, включая восстановленные реконсилятором) пишет строку потребления в `client_usage_daily` **внутри той же транзакции**, что и охраняемая смена статуса — одним `INSERT...SELECT...ON CONFLICT` с коррелированными `sum(size_bytes)` и без предикатов по `ordinal`, так что байты суммируются по ВСЕМ входам и выходам (SEED-017 не потребует переписывания учёта). Кардинальность Prometheus ограничена явным численным потолком с коллапсом в `overflow`, и это ОТДЕЛЬНЫЙ счётчик — `octoconv_job_duration_seconds` остался `{engine,status}`, `octoconv_queue_depth` остался `{queue,state}`, оба закреплены пиннинг-тестами.

Поверх учёта — admission-гейт в транзакции: счёт занятости per-`(client, engine)` сериализуется с `INSERT INTO jobs` под `pg_advisory_xact_lock`, fail-closed на всех трёх путях создания джобы (REST + оба MCP-входа проксируют в тот же `POST /v1/jobs` — обхода нет по конструкции), отказ `429` + `Retry-After` + машиночитаемый `quota_exceeded`, по умолчанию observe-режим и сервисный kill-switch. Суточные лимиты считают и in-flight работу, закрывая единственную конфигурацию (`max_concurrent = nil`), где кап раньше пропускал всё до завершения джобы. Ретеншен стал исполняемым, а не описанным в комментарии к миграции: батчевый purge на существующем тике реконсилятора реально удаляет старые строки, доказано тестами, которые состаривают данные и проверяют, что их нет.

Оператор получил `GET /v1/system/usage` — один ответ, сшивающий потребление, настроенные лимиты и остаток, посчитанный по той же формуле, что и `evaluate()`, с честными оговорками про kill-switch и observe-режим и байт-идентичным no-leak 404. `minReplicaCount` уехал из семи захардкоженных нулей в значения Helm через валидирующий хелпер (закрыт SEED-011). А QUOT-05 закрыт живым гейтом, а не допущением: 10 раундов по 10 конкурентных запросов при `api.replicas: 2` — ровно K=2 допуска и 8 отказов каждый раунд, оба пода обслуживали оба исхода, 167 ассертов, evidence закоммичен. `internal/jobs/admission.go` теперь фиксирует обратное тому, что документирует `internal/ratelimit`: у квот НЕТ оговорки «верно только при `replicas: 1`».

Две фазы (52 и 53) реально прошли цикл `gaps_found` → закрытие → ре-верификация, а не прошли с первого раза.

**Phase 56 (JobRunner Consolidation & Structured Error Codes) complete — 2026-08-08.** Первая фаза v1.13 закрыта: все семь конверсионных хендлеров теперь проходят через один `Runner`/`EngineDescriptor` code-path (таймаут, лимит входа, opts-схема, retry-классификация, unique-lock TTL и progress — поля дескриптора, а не ветвления), терминальный исход эмитится ровно одним вызовом в ровно одном месте и структурно не может выстрелить до коммита транзакции (закрыт тот же класс дефекта, что ревью PR #4 нашло в семи местах — включая живой `internal/reconciler/reconciler.go` RUNR-02). Закрытый версионированный контракт кодов ошибок (`internal/errcode`, `docs/ERROR-CODES.md`) введён один раз для REST 4xx и async-отказов; референсный фронтенд и оба MCP-сервера выбирают поведение по коду, а не по тексту сообщения — доказано механическими no-text-matching тестами. Стадийные асимметрии, лейблы метрик (`{engine,status}`/`{queue,state}`) и `HandleWebhookDeliver` пин-тестами подтверждены нетронутыми. RUNR-01–03, ERRC-01–03 закрыты.

<details>
<summary>v1.12 Consumer Accounting & Fairness — цели милстоуна (закрыт)</summary>

**Goal:** превратить OctoConv из «сервиса, который умеет конвертировать» в общую платформу, которая знает своих потребителей — кто сколько потребляет, в каких границах, и как не дать одному потребителю выжрать класс движка целиком.

**Key context / locked решения:** сервис внутренний — **никакого биллинга и публичной многоарендности**, это учёт и защита от соседа, а не монетизация. Всё аддитивно и обратно совместимо: существующие клиенты ничего не замечают, пока оператор не выставит квоту. Postgres остался системой записи — новых хранилищ не заведено; архитектура очередей и KEDA не переписана (FAIR-02 подтверждён нулевым диффом).

**Почему не альтернативы (рассмотрены и отклонены):** SEED-010 (динамический реестр возможностей) экономит разработческую стоимость добавления движка, которая уже низка благодаря отлаженному 4-фазному шаблону, но ослабляет свойство безопасности. Восьмой engine-класс — самый предсказуемый вариант, но продукт уже семь раз умеет «сконвертировать файл»; это не закрывало то, чего платформе не хватало. Пер-клиентские суб-очереди (FAIR-V2-01) отложены как evidence-gated: они переименовывают очереди и ломают PromQL всех семи `ScaledObject`.

</details>

<details>
<summary>v1.11 Pandoc Engine — SHIPPED 2026-08-02</summary>

**v1.11 Pandoc Engine — SHIPPED 2026-08-02.** OctoConv теперь имеет СЕМЬ engine-классов: изображения (libvips), офисные документы (LibreOffice), HTML→PDF (chromium-headless), аудио (whisper.cpp), видео (ffmpeg), OCR (Tesseract) и **markup-интерконверсию (pandoc)**. Pandoc-класс: standalone `PandocConverter` по закрытому `(from→to)` allowlist'у markup-семейства (md/rst/org/latex/epub/html/docx, БЕЗ →pdf — им владеют LibreOffice/chromium), security-locked с первой строки (raw HTML/TeX passthrough отключён, `--sandbox`, no-network, typed `PandocOpts` с strict-parse и discrete-argv) → полный async-контур (очередь `pandoc`, `cmd/pandoc-worker`, timeout-is-terminal retry по document-прецеденту, reconciler-routing) поверх **неизменной** v1.9 progress/SSE/webhook инфры (ноль pandoc-упоминаний в `internal/webhook`) → лёгкий TeX-free контейнер с SHA-256-пиненным статическим pandoc 3.10.1 (dual-arch) → **измеренный `PANDOC_ENGINE_TIMEOUT=65s`** (worst-cell html_large→md p95=32.318s × 2.0, 92.8% под 900s CAP) → KEDA scale-from-zero + downscale-survival live-proven на OrbStack k8s. 4 фазы (47-50), 14 планов, 9/9 требований, аудит 6/6 межфазных стыков.

Две находки пришли от живых гейтов, а не от ревью кода: 4 MiB html→md **OOMKilled** воркер в 1Gi-контейнере (Path-B: `PANDOC_MAX_INPUT_BYTES` 4→3 MiB), а замер под прод-лимитами показал, что pandoc — движок **memory-bound**, а не «лёгкий»: ~300 MiB anon на каждый MiB входа, то есть `PANDOC_WORKER_CONCURRENCY=2` гарантировал OOM (2→1 в compose, чарте и Go-дефолте). Пропускная способность теперь берётся из KEDA scale-out, а не из внутриподовой параллельности.

</details>

<details>
<summary>v1.10 OCR Engine (text extraction) — SHIPPED 2026-07-27</summary>

OctoConv теперь имеет ШЕСТЬ engine-классов: изображения (libvips), офисные документы (LibreOffice), HTML→PDF (chromium-headless), аудио (whisper.cpp), видео (ffmpeg) и **OCR/извлечение текста** (Tesseract). OCR-класс: standalone `OCRConverter` извлекает txt/hOCR/ALTO из изображений и сканированных/image-only PDF, постранично стримингово (memory-bomb-safe, born-digital-PDF-safe) → полный async-контур (очередь `ocr`, `cmd/ocr-worker`, stage-aware retry, reconciler) поверх неизменной v1.9 progress/SSE/webhook инфры (page-granular прогресс, null→100 для одиночной картинки) → контейнер с source-built Tesseract 5.5.3 + poppler-utils, offline, SHA-пиненный `tessdata_fast` (eng/rus) → **измеренный `OCR_ENGINE_TIMEOUT=744s`** (worst-cell p95=5.81 s/page, Path-B `OCR_MAX_PAGES` 200→64) → KEDA scale-from-zero live-proven на OrbStack k8s. 4 фазы (43-46), 15 планов, 12/12 требований.

</details>

**v1.9 Job Progress & Control — SHIPPED 2026-07-25.** Длинные конвертации стали прозрачными и управляемыми, всё поверх существующего контура (Postgres lifecycle, hardened `runCommand`, webhook, asynq): аддитивная схема прогресса (`0007`/`0008`, zero behavior change) → **отмена end-to-end** (`POST /v1/jobs/{id}/cancel`, guarded `{queued,active}→canceled`, custom Redis pub/sub `job:cancel` + in-memory CancelFunc-registry, реальная смерть ffmpeg-процесса live-proven 12/12, non-resurrection) → **листинг** (`GET /v1/jobs`, client-scoped keyset-пагинация, opaque cursor, two-key leak-тест) → **progress core** (ffmpeg `-progress` через named-FIFO поверх разрешённого `file:`-протокола — не `pipe:`, `runCommandStreaming` leak-safe, exec.go byte-for-byte неизменен; non-locking throttled write, EMA-ETA; SIGKILL-mid-stream live-proven 40/40 — ноль orphaned + ноль leaked goroutines + frozen pct) → **live-push** (SSE `GET /v1/jobs/{id}/events` DB-poll leak-safe-by-construction, live-proven 15/15; opt-in HMAC progress-webhook из detached-горутины, никогда не в terminal retry/dead-letter бюджете). 15 планов, 13/13 требований (PROG/CNCL/JLIST/PUSH), 3 live-гейта.

<details>
<summary>v1.8 AV Engine (video/ffmpeg) — SHIPPED 2026-07-23</summary>

Пятый engine-класс — обработка видео через ffmpeg в отдельном `av-worker` — в полном контуре: fail-closed magic-bytes валидация → standalone `AVConverter` (транскод / audio-extract / thumbnail, stream-copy fast-path) → async-пайплайн (очередь `av`, stage-aware `isAVTerminal`) → контейнер с ffmpeg **n8.1.2 из исходников** и **RTF-замеренным** `AV_ENGINE_TIMEOUT=753s` → KEDA scale-from-zero + downscale-survival live-proven. Требования AVC/AVO/AVE/AVT validated.

</details>

<details>
<summary>v1.7 Audio Engine & Hardening — SHIPPED 2026-07-18</summary>

Четвёртый engine-класс (офлайн whisper.cpp-транскрипция) в полном контуре: fail-closed валидация → async-пайплайн со stage-aware retry → контейнер с RTF-измеренным таймаутом → KEDA scale-from-zero (live-proven). Плюс закрыт hardening-хвост v1.6. Аудит 12/12 требований, интеграция 22/22, E2E 2/2.

</details>

<details>
<summary>Shipped Milestone: v1.10 OCR Engine (text extraction) — SHIPPED 2026-07-27 (goal & features)</summary>

**Goal:** Клиент может извлечь машиночитаемый текст из изображений и сканированных PDF через тот же async-контур, что и остальные движки — offline, детерминированно, с онлайн-прогрессом. Шестой engine-класс.

**Target features:**
- **Классический CPU-OCR движок** (tesseract, выбран через eval-gate в research); CPU-воркер-паттерн, без GPU
- **Вход: изображения + скан-PDF** — jpg/png/tiff/webp + image-only PDF → `pdf→image` растериза-престейдж → постраничный OCR
- **Вывод: txt + hOCR/ALTO** (текст + координаты слов)
- **Онлайн-прогресс** — page-granular поверх готовой v1.9-инфры (SSE/webhook/`progress_pct`), ETA из сек/страница; одиночная картинка → null→100
- **Полный engine-class контур** — очередь `ocr` + `cmd/ocr-worker` + контейнер + KEDA scale-from-zero, fail-closed magic-byte валидация, stage-aware retry, измеренный таймаут
- **Язык/скрипт** — per-job opt через валидированный `opts` (closed allowlist, как AudioOpts)

**Key context:** offline-constraint (облако вне игры) · CPU-only (не GPU) · переиспользует v1.9 progress/SSE/webhook движок-агностично · реестр SEED-010 отложен (сажаем «по-старому» на compile-time схему, как image/av). Основано на SEED-009. 4 фазы (43-46), 15 планов, 12/12 требований, source-built Tesseract 5.5.3 + poppler, `OCR_ENGINE_TIMEOUT=744s` (измерено), KEDA scale-from-zero live-proven.

</details>

<details>
<summary>Next-milestone backlog (отложенные кандидаты)</summary>

- **Новые классы движков:** SEED-005 CAD (открытый SDK/licensing/offline вопрос), SEED-006 Archive/zip (security-first), SEED-007 File-preview.
- **Ops / production-readiness:** AVX-02 (registry cold-pull замер), K8SV2-01 (k8s в CI, kind/k3d), K8SV2-03 (`is_operator` колонка).
- **Расширения движков:** AVX-01 (video trim/crop), DOCV3-03 (шрифты/CJK-RTL для HTML→PDF).
- **Тех-долг v1.8:** av-worker env-parser тесты, wav-демуксер justification.

_SEED-001 RESOLVED (транскрипция закрыта v1.7/v1.8). SEED-008 → в скоупе v1.9._

</details>

<details>
<summary>Shipped Milestone: v1.8 AV Engine (video/ffmpeg) — SHIPPED 2026-07-23 (goal & features)</summary>

**Goal:** Пятый engine-класс — обработка видео через ffmpeg в отдельном av-воркере по проверенному паттерну (своя очередь, свой контейнер, свои таймауты, KEDA), включая сквозную цепочку видео → транскрипт через существующий whisper-пайплайн.

**Target features:**
- Транскод видео (mov/avi/mkv/webm → mp4 H.264 и т.п.) с собственным RTF-гейтом по образцу аудио — самая дорогая операция класса
- Извлечение аудио: видео → mp3/wav/m4a
- Превью/thumbnail: кадр из видео → jpg/png/webp (таймкод через opts)
- Видео → транскрипт: mp4/mov → txt/srt/vtt/json одним job'ом (ffmpeg-экстракция + whisper)
- Отдельный `cmd/av-worker`: очередь `av`, Dockerfile с ffmpeg, fail-closed magic-bytes валидация видео-контейнеров, transient/terminal retry, compose + chart + KEDA ScaledObject

**Key context:** воркеры остаются офлайн (ffmpeg локальный, без внешних API); паттерн v1.7 переиспользуется целиком (RTF-гейт → measured timeout, stage-aware retry, IN-02 пропагация, scale-from-zero proof); способ реализации видео→транскрипт решён — межочередная цепочка на существующий audio-воркер. SEED-001 остаётся в банке семян — не выбран в этот milestone.

</details>

<details>
<summary>v1.7 Audio Engine & Hardening — SHIPPED 2026-07-18</summary>

**Goal:** Четвёртый engine-класс — офлайн-транскрипция аудио через whisper.cpp по проверенному паттерну (отдельная очередь/воркер/бинарник, hardened exec, KEDA) — плюс закрытие hardening-хвоста v1.6.

**Target features:**
- Hardening-хвост v1.6: WR-01 (семантика пустого PromQL при падении api), live-скрипт OPER-01 + проброс OPERATOR_CLIENT_IDS в compose, гейт-тулинг warnings из 28-REVIEW, K8S-02 direct-dial перепроверка
- Audio engine-класс: audio-форматы → транскрипт через whisper.cpp CLI в отдельном контейнере; magic-bytes валидация, transient/terminal retry, отдельная asynq-очередь + KEDA ScaledObject, chart-интеграция
- SEED-001 фундамент: контракт результата транскрипции (формат, таймстампы) спроектирован с прицелом на будущий разбор урока

**Key context:** движок локальный (без внешних API — воркеры остаются офлайн); анализ ошибок отложен; k8s-в-CI отложен (K8SV2-01). Прошлое состояние: v1.6 shipped 2026-07-17 (см. Context).

</details>

<details>
<summary>v1.6 Kubernetes & KEDA — SHIPPED 2026-07-17</summary>

**Goal:** Весь стек OctoConv поднимается в Kubernetes из Helm-чарта, воркеры автоскейлятся KEDA по глубине очередей (0→N→0 под нагрузкой — доказано), MCP получает in-cluster HTTP-эндпоинт, а пресеты — system-scope REST.

**Target features:**
- Helm-чарт + полный стек на OrbStack k8s (SEED-004): Deployments/probes, Secrets/ConfigMaps, migrate/createbucket как Jobs; четыре известные мины переноса закрыты (METRICS_ADDR localhost-bind, host-gateway E2E-трюк, one-shot ordering, compose-DNS в presigned URL)
- KEDA ScaledObjects per engine-class по существующей Prometheus queue_depth метрике
- Нагрузочная проверка автоскейла: скейл 0→N→0 с наблюдаемыми критериями
- MCP streamable HTTP (MCPV2-01): контейнер в кластере, внутренний эндпоинт
- system-пресеты REST (PRAPIV2-01)

**Key context:** первый инфраструктурный милстоун; research first. Прошлое состояние: v1.5 shipped 2026-07-13 (см. Context).

</details>

## Current Milestone: v1.14 Typesetting & Debt

**Goal:** дать клиенту настоящую вёрстку — `latex → pdf` девятым engine-классом —
и закрыть четыре долга, которые v1.13 оставил названными, а не исправленными.

**Target features:**

- **tectonic как девятый engine-класс, только `latex → pdf`.** Очередь,
  `cmd/tectonic-worker`, образ, ScaledObject — по шаблону, который к девятому
  разу стоит один план. Нетривиальна здесь ровно одна вещь: tectonic по
  умолчанию **тянет пакеты из сети** во время сборки документа, а дать воркеру
  egress ради клиентского `\usepackage` — тот самый вектор, который `sanitize`
  вычищает из чужих файлов. Значит offline-бандл, вшитый в образ и запиненный
  по версии, отсутствие сетевого доступа как проверяемое свойство, и проверка
  ПРОГОНОМ, а не чтением документации, что `\write18` выключен и не включается
  из содержимого документа (фаза 60 отвергла `olevba` ровно на таком замере
  после того, как ресёрч рекомендовал его непрогнанным).
- **Потолок страниц у probe.** Замер таймаута v1.13 показал, что стоимость
  постраничная (2.0 с/стр), поэтому документ за ~300 сканированных страниц
  падает по таймауту вместо отказа на входе — поздно, с ретраями (таймаут по
  замыслу транзиентный, а слишком длинный документ — не нагрузка) и с
  невнятным `engine_error`. Форма починки — та, что у OCR: `PROBE_MAX_PAGES`,
  guard до первого прохода gs, терминальная классификация, отказ при приёме.
- **Три находки восьмого раунда ревью PR #12**, влитые неисправленными по
  решению оператора: ICC-профиль из нескольких APP2-чанков, `endstream` как
  подстрока внутри бинарного потока, relationship, удаляющий главную часть
  собственного пакета.
- **Крашлуп API до готовности Postgres.**
- **Флак soak-теста реконсилятора** (делит базу с параллельными пакетами,
  подтверждён в двух разных фазах) **и открытые пункты честной отчётности
  inspect.**

**Key context:** `md → pdf` через цепочку pandoc→tectonic сознательно вне
границы. Это первая джоба, пересекающая границу движка, — со своими вопросами
про квоты, ретраи и частичный отказ; она принадлежит SEED-018 (job pipelines),
а не этому милстоуну. Нумерация фаз продолжается с 62.

<details>
<summary>v1.13 Inspect &amp; Operations — цели милстоуна (закрыт 2026-09-02)</summary>

### v1.13 Inspect & Operations

**Goal:** превратить OctoConv из конвертера форматов в processing API — ввести измерение
«операция» с двумя первыми не-конвертерными возможностями (`inspect`, `sanitize`) и убрать
×7 тариф, который сегодня платит каждая сквозная забота, проходящая через семь копий одного
хендлера.

**Target features:**

- **SEED-020 — JobRunner + структурированные коды ошибок** (фундамент). Семь хендлеров по
  94–163 строки в файле на 1989 строк → один runner; пер-энджинные различия (таймаут, лимит
  входа, opts-схема, disk/page-гарды) как данные, не ветки. `HandleWebhookDeliver` **не**
  входит — другая форма (retry/dead-letter семантика). Post-commit хуки: ровно один вызов в
  одном месте, структурно невозможно выстрелить до коммита транзакции — именно этот дефект
  ревью PR #4 нашло в семи местах сразу. Наружу — машиночитаемые коды ошибок вместо
  фиксированных строк, которые фронт и оба MCP матчат текстом; попутно закрывается
  `invalid_options` metrics-гэп (терминальные отказы не попадают ни в
  `octoconv_client_job_outcomes_total`, ни в `octoconv_job_outcomes_total`).
- **Синхронный `POST /v1/inspect`** — отчёт в теле ответа: реальный формат по magic bytes,
  базовые характеристики (размер, страницы, длительность, разрешение, кодеки), риски
  (шифрование, макросы OOXML, JS и вложения в PDF, внешние ссылки, bomb-признаки), наличие
  текстового слоя, доступные `(operation, target)` с движком, оценкой длительности и проверкой
  против текущей квоты клиента. Границы обязаны быть явными требованиями, а не допущением:
  потолок работы + ограниченная выборка страниц с честной пометкой выборочности в отчёте, и
  собственный rate-бюджет — синхронный путь минует v1.12 admission-гейт.
- **SEED-021 — born-digital PDF перестаёт быть тупиком.** Сегодня `pdf` — source-формат ровно
  в одном месте (`ocrSourceFormats`), OCR отвергает его через `ErrOCRBornDigital`, а
  комментарий советует «идти в document-движок», который pdf на вход не принимает вовсе.
  Inspect становится маршрутизатором вместо слепого прогона через Tesseract на 744s таймауте.
- **Измерение `operation` + роли outputs.** `operation` уже есть в CHECK миграции
  `0001_init.sql` (`convert/extract/archive/inspect/render`), но API его не принимает и реестр
  им не ключуется. Реестр по `(operation, from, to)`, `convert` по умолчанию — существующие
  клиенты не замечают ничего. `role` у `job_outputs`: `primary`/`report`/`preview`/`thumbnail`.
- **`sanitize` — первая асинхронная не-convert операция.** `pdf→pdf` и office→office: удаление
  макросов, JavaScript и вложений из PDF, EXIF/GPS/IPTC/XMP (SEED-013, зарезервированный enum
  `probe`), внешних ссылок. Отчёт «что найдено и вырезано» — второй output с `role=report`, то
  есть первый реальный потребитель новой модели ролей. Отдельная операция, не неявное
  поведение конвертации.
- **Хвост долга v1.12** (по образцу Phase 54 — независимый гигиенический хвост, не шумящий в
  диффе основной работы): 52 WR-01 — in-flight джоба тратит суточный кап без временнóй
  границы, поэтому навсегда застрявшая джоба блокирует бюджет клиента каждый следующий
  UTC-день, а у оператора нет средства кроме сырого SQL; плюс самопротиворечивая секция квот в
  README (52 WR-09) и `PurgeUsage`, считающий cutoff по Go-часам там, где весь v1.12 перешёл
  на часы Postgres.

**Key context:** основано на Notion «OctoConv — новые воркеры и развитие processing pipeline»
(рассмотрены три направления, рекомендовано — Document Processing API; медиа и CAD остаются
подключаемыми engine classes, но не определяют ядро продукта). Семена в скоупе: **SEED-020,
SEED-021, SEED-013** (scrub-половина). **SEED-010 сознательно не берём** — третий отказ с тем
же аргументом: динамический capability-реестр экономит дешёвое (добавление движка по
отлаженному 4-фазному шаблону) ценой compile-time allowlist того, что вообще может быть
выполнено. Нумерация фаз продолжается с **56**. Обратная совместимость абсолютная: `operation`
по умолчанию `convert`. Инварианты, которые милстоун не трогает: `internal/convert/exec.go`
byte-for-byte, `TestJobDurationLabelsPinned`/`TestQueueDepthLabelsPinned` (KEDA-контракт),
`Repo.transition` и его гарантии — JobRunner двигает только оркестрацию вокруг него.

</details>

## Requirements

### Validated

- ✓ **Inspect & Operations (RUNR-01..03, ERRC-01..03, OPRN-01..03, ROLE-01/02, INSP-01..06, BDPD-01/02, SANI-01..06, DEBT-09..11) — v1.13** — 28/28 требований, 6 фаз, 38 планов. Синхронный `POST /v1/inspect` с закрытым словарём находок и честным полем полноты; `operation` как валидируемое сквозное измерение с ролями выходов; `sanitize` на восьмом engine-классе `probe`, где чистота доказывается реализацией, не участвовавшей в удалении, а неподтверждённый выход удаляется, а не отдаётся; отчёт, который заявляет только проверенные классы и механически не может заявить общей безопасности; born-digital PDF маршрутизируются, а не отвергаются; закрытый контракт из 28 кодов ошибок, привязанный к документу двусторонним тестом. `PROBE_ENGINE_TIMEOUT=600s` **измерен**, а не подобран: стоимость оказалась постраничной (1.99–2.01 с/стр на трёх точках), поэтому граница выведена как `ceil(2.0 с/стр × 200 стр × 1.5)` по форме, которую в проекте уже использует OCR, а не как p95 по стенным часам — который при постраничной стоимости измерял бы состав корпуса, а не движок.

- ✓ **Attribution & accounting (ACCT-01..04, RETN-01) — v1.12 Phase 51** — потребление каждого клиента копится в `client_usage_daily` (PK `client_id,engine,day,status`), запись идёт **внутри той же транзакции**, что и охраняемый терминальный переход (`MarkDone`/`MarkFailed`/`Cancel`), одним `INSERT...SELECT...ON CONFLICT` с коррелированными `sum(size_bytes)` по `job_inputs`/`job_outputs` — без предикатов по `ordinal`, поэтому N входов/выходов SEED-017 не потребуют переписывания учёта. Exactly-once доказано `-race` на живом Postgres, включая инверсионный прогон (вынос записи из транзакции ломает тест) и soak-тест, который реально выжидает `ActiveStaleAfter`, форсирует гонку stale-recovery реконсилятора и проверяет `job_count = 1`, а не факт восстановления. `octoconv_client_job_outcomes_total` — ОТДЕЛЬНЫЙ счётчик с явным численным потолком (`METRICS_CLIENT_ID_CEILING`, дефолт 200) и коллапсом лишних/битых id в стабильный `overflow`; `octoconv_job_duration_seconds` остался `{engine,status}`, `octoconv_queue_depth` — `{queue,state}`, оба закреплены пиннинг-тестами. Ретеншен исполняемый: батчевый purge на тике реконсилятора удаляет старые `jobs` (каскадом `job_events`/`job_inputs`/`job_outputs`/`webhook_deliveries`) и `client_usage_daily` на своём 400-дневном окне, доказано тестами, которые состаривают данные и проверяют, что строк нет. 5 планов, верификация 11/11.
- ✓ **Quotas & intra-class fairness (QUOT-01..04, FAIR-01/02) — v1.12 Phase 52** — `client_limits` по `(client_id, engine)`, где отсутствие лимита — литеральный `NULL`, а не sentinel-число; admission-гейт сериализует счёт занятости с `INSERT INTO jobs` под `pg_advisory_xact_lock` в одной транзакции с пиненным `ReadCommitted`, fail-closed на всех трёх путях создания джобы (доказано интеграционным тестом через REST И реальный MCP-инструмент, а не выведено из проксирования); отказ — `429` + `Retry-After` + `quota_exceeded`, машинно отличимый от `rate_limited`, никогда не 403; observe-режим по умолчанию плюс сервисный kill-switch. Суточные капы считают in-flight (`queued`+`active`) допущенную работу, что закрыло единственную конфигурацию (`max_concurrent = nil`), где кап раньше пропускал всё до завершения джобы, а `Cancel` пишет в ledger, закрывая обход `create → cancel → create`. FAIR-01 даёт структурную гарантию K из M; FAIR-02 подтверждён нулевым диффом по всем семи `scaledobject-*.yaml`, `internal/queue/` и коллектору глубины очереди. 9 планов (5 + 4 на закрытие гэпа), верификация 9/9 после цикла `gaps_found` → закрытие → ре-верификация.
- ✓ **Operator usage visibility (OPSV-01/02) — v1.12 Phase 53** — `GET /v1/system/usage` за существующим `OPERATOR_CLIENT_IDS`-механизмом отдаёт одним ответом потребление per-`(client, engine)` из роллапа, настроенные лимиты и остаток, посчитанный по той же формуле, что и `evaluate()` (поле в поле, включая исключение `bytes_out` из байтового бюджета); простаивающий безлимитный клиент виден как синтетическая строка и отличим от несуществующего `client_id`, при этом не-оператор получает байт-идентичный no-leak 404. Утверждения о правде честные: `remaining_matches_admission_gate` учитывает kill-switch, per-row `enforced` повторяет условие отказа `admitLocked`, а день берётся из часов Postgres, а не хоста API. 7 планов (4 + 3 на закрытие гэпа), верификация 12/12 после ре-верификации.
- ✓ **Multi-replica quota correctness (QUOT-05) — v1.12 Phase 55** — доказано живым прогоном, а не выведено из дизайна: при `api.replicas: 2` десять независимых раундов по десять конкурентных запросов дали ровно K=2 допуска и 8 отказов `quota_exceeded` в КАЖДОМ раунде, ноль `rate_limited`, оба пода обслуживали оба исхода; 167 ассертов, TSV-evidence закоммичен и перепроверяем `awk`-ом. Гейт имеет реальную историю падений и структурных, а не пороговых починок: опровергнутое допущение о доступности ClusterIP с хоста (драйвер переехал в под внутри namespace) и неоднозначность «переиспользование слота или нарушение капа» на раунде 4/10 (тяжёлая фикстура, самопроверяющаяся граница разброса и per-round проверка из Postgres, что ни одна допущенная джоба не стала терминальной внутри залпа) — `==` ни разу не ослаблялся до `<=`. `internal/jobs/admission.go` фиксирует отсутствие оговорки «верно только при `replicas: 1`»: структурный аргумент первым, живое подтверждение вторым, границы прогона (K=2, движок image, ровно 2 реплики) названы границами. 7 планов, верификация 10/10.
- ✓ **Per-class capacity floor (CAPA-01 / SEED-011) — v1.12 Phase 54** — `minReplicaCount` настраивается на класс через `keda.<class>.minReplicaCount` (явный `0` на всех семи блоках `values.yaml`), валидация и рендер — одна пара `define` в `_helpers.tpl` (fail-fast на отрицательном значении, на превышении `maxReplicaCount` и на любом не-целом входе — `kindOf` проверяется ДО `int`-приведения, иначе sprig'овый `cast.ToInt` молча даёт `0`); `fallback.replicas = max(1, minReplicaCount)`, поэтому Prometheus-outage не отменяет пол. Дефолтный рендер байт-в-байт равен пре-фазовому, кроме комментариев (SC1). Первое CI-покрытие чарта: job `helm` в `ci.yml` без кластера — lint, anti-vacuity-гейт на 7 ScaledObject, пер-классовая изоляция (6 пустых `--show-only` диффов + 1 непустой), ассерты на реальные значения, 4 негативных кейса. 2 плана, верификация 11/11 must-haves.
- ✓ **Pandoc / markup interconvert engine class (PDOC-01/02, PDCO-01, PDCE-01..04, PDCP-01/02) — v1.11** — седьмой engine-класс: `PandocConverter` по закрытому markup-allowlist'у (md/rst/org/latex/epub/html/docx, →pdf сознательно исключён, поэтому пары не конфликтуют с chromium/LibreOffice — guard-тест на disjointness); hardened вызов (`--sandbox`, raw HTML/TeX passthrough off, no-network, typed `PandocOpts` со strict-parse и discrete-argv, injection-тест); очередь `pandoc` + `cmd/pandoc-worker` + timeout-is-terminal `isPandocTerminal` + fresh `PandocUniqueTTL`, поверх неизменной v1.9 инфры; TeX-free образ с SHA-256-пиненным pandoc 3.10.1 (dual-arch, USER nobody); **измеренный** `PANDOC_ENGINE_TIMEOUT=65s` (p95 32.318s × 2.0) + Path-B `PANDOC_MAX_INPUT_BYTES` 3 MiB и `PANDOC_WORKER_CONCURRENCY=1` — оба выведены из живых OOM-находок, не скопированы; IN-02 env-паритет по 10 compose-сервисам и чарту; KEDA scale-from-zero (12/12) + downscale-survival (35/35) live-proven. 4 фазы, 14 планов, 9/9 требований.
- ✓ **OCR / text-extraction engine class (OCR-01..04, OCRO-01, OCRE-01..05, OCRP-01/02) — v1.10** — шестой engine-класс: standalone `OCRConverter` (Tesseract, PSM/OEM-пиненный) извлекает txt из изображений и потраничного скан-PDF (`pdftoppm -f N -l N` стриминг, page-count guard, born-digital-PDF fail-closed 422), hOCR/ALTO через real XML-энкодер (round-trip `&`/`<`/`>` тест); `ocr` очередь + `cmd/ocr-worker` + stage-aware `isOCRTerminal` + fresh `OCRUniqueTTL`, поверх неизменной v1.9 progress/SSE/webhook инфры (page-granular рамп, одиночная картинка null→100); source-built Tesseract 5.5.3 + poppler-utils контейнер, SHA-пиненный `tessdata_fast` (eng/rus), измеренный `OCR_ENGINE_TIMEOUT=744s` (worst-cell p95=5.81 s/page, Path-B `OCR_MAX_PAGES` 200→64); KEDA scale-from-zero + downscale-survival live-proven на OrbStack. 4 фазы, 15 планов, 12/12 требований.
- ✓ **Job Progress & Control (PROG-01..05, CNCL-01..04, JLIST-01/02, PUSH-01/02) — v1.9** — прогресс/ETA в реальном времени, листинг и отмена джоб поверх существующего контура: аддитивная схема (`0007`/`0008`), отмена end-to-end с реальной смертью ffmpeg (custom Redis pub/sub `job:cancel`, live-proven 12/12, non-resurrection), client-scoped keyset-листинг (two-key leak-тест), ffmpeg `-progress` через named-FIFO/`file:` (exec.go byte-for-byte неизменен, non-locking throttled write, EMA-ETA, SIGKILL-mid-stream live-proven 40/40 ноль orphaned + ноль leaked goroutines), SSE-стрим (DB-poll leak-safe-by-construction, live-proven 15/15) + opt-in HMAC progress-webhook (detached, никогда не в terminal retry-бюджете). 13/13 требований, 3 live-гейта, integration coherent.
- ✓ **AV / video engine class (AVC-01..05, AVO-01..03, AVE-01..05, AVT-01) — v1.8** — пятый engine-класс: fail-closed video magic-bytes sniffers, standalone `AVConverter` (transcode/audio-extract/thumbnail, stream-copy fast-path, `-protocol_whitelist file,crypto`), `av` очередь + `cmd/av-worker` + stage-aware retry, ffmpeg n8.1.2-from-source container, RTF-measured `AV_ENGINE_TIMEOUT=753s` + fail-closed re-encode source-bound (OOM-DoS closed), KEDA scale-from-zero + downscale-survival live-proven, видео→транскрипт через audio-воркер (6-я conversion chain)

<!-- Существующий код, вертикальный срез image/libvips на ветке feat/scaffold-and-infra. -->

- ✓ API принимает файл через multipart `POST /v1/jobs`, валидирует пару форматов (422 при неподдерживаемой) и лимит размера (413), ставит задачу в очередь — existing
- ✓ Воркер конвертирует изображения (png/jpg/webp/heic/tiff) через libvips, запуская внешний бинарник с таймаутом и убийством всей process group — existing
- ✓ Жизненный цикл задачи отслеживается в PostgreSQL (`queued → active → done/failed`) с append-only журналом переходов (`job_events`) — existing
- ✓ `GET /v1/jobs/{id}` отдаёт статус и presigned download URL готового результата — existing
- ✓ Graceful shutdown API и воркера — existing
- ✓ Ветка `feat/scaffold-and-infra` влита в `main` — Phase 1 (уже была слита до начала фазы, подтверждено при планировании)
- ✓ API-ключи для клиентов через таблицу `clients` (`cmd/manage-clients` CLI: create/add-key/revoke), salted SHA-256 хеш, два активных слота на ротацию без даунтайма — Phase 1
- ✓ Обязательная аутентификация на всех `/v1/*` (hard cutover, 401), `/healthz` остаётся публичным, cross-client доступ → 404 (никогда 403) — Phase 1
- ✓ Rate limiting: per-client лимит по `client_id` (429 + Retry-After) и pre-auth IP-guard (`middleware.ClientIPFromRemoteAddr`, не спуфится) — Phase 1
- ✓ Webhook-доставка результата (`jobs.callback_url` + `webhook_deliveries`) вместо поллинга статуса: HMAC-SHA256-подписанный payload с timestamp, bounded retry (`MaxRetry=6`, экспоненциальный backoff + jitter, ~30 мин окно), dead-letter после исчерпания попыток, каждая попытка доставки записана в `webhook_deliveries` — Phase 2 (12/12 must-haves, live e2e verified)
- ✓ Воркер различает transient/terminal ошибки конвертации; transient-ошибки реально ретраятся asynq'ом по собственному быстрому расписанию image-очереди (2с/5с/15с, `IMAGE_MAX_RETRY`), с `asynq.Unique`-локом против дублей — Phase 3 (5/5 must-haves, live e2e verified)
- ✓ Postgres-driven reconciler восстанавливает задачи, зависшие в `queued`/`active`, идемпотентно (enqueue-first + `asynq.ErrDuplicateTask`-guard), с ограничением числа попыток и terminal-fail + webhook по исчерпании, все действия в `job_events` — Phase 3
- ✓ Валидация содержимого файла по magic bytes (жёсткий список сигнатур под 5 зарегистрированных форматов) отклоняет несовпадения с 422 до записи в S3 — Phase 4 (5/5 must-haves, live e2e verified)
- ✓ MinIO ILM lifecycle-правило автоматически удаляет `uploads/`/`results/` по TTL (7 дней по умолчанию), без ручной очистки — Phase 4
- ✓ Prometheus-метрики (исходы задач, длительность, webhook-доставки, reconciler-действия, глубина очереди) на отдельном localhost-only `/metrics`; реальный `/healthz` (пинг Postgres/Redis/S3, 503 при деградации); asynqmon-дашборд для визуальной инспекции очереди — Phase 4
- ✓ SSRF-валидация `callback_url` снимает блокировку RFC1918 приватных адресов через явный флаг `WEBHOOK_ALLOW_PRIVATE_IPS`; loopback/link-local/metadata-endpoint остаются заблокированы всегда — Phase 5 (4/4 success criteria, live e2e verified)
- ✓ Reconciler дополнительно находит `done`/`failed` задачи с потерянным webhook enqueue (нет строк в `webhook_deliveries`) и инициирует ровно одну повторную доставку, защищено `asynq.Unique`-локом на webhook-очереди — Phase 6 (RECON-04, live e2e verified)
- ✓ Восстановление зависших `queued`/`active` задач подтверждено автоматическим soak-тестом на реальном прошедшем времени (не mock-часах) — Phase 6 (RECON-05)
- ✓ Защита от decompression bomb: zero-dependency парсеры заявленных размеров изображения для всех 5 форматов (png/jpg/webp/heic/tiff), настраиваемый лимит `MAX_IMAGE_PIXELS` (100МП по умолчанию) — Phase 7 (VALID-03, live e2e verified)
- ✓ Отдельная `document` asynq-очередь (`TypeDocumentConvert`/`QueueDocument`) по паттерну engine-class routing, derived unique-lock TTL и no-jitter retry-расписание, `EnqueueDocumentConvert` — Phase 10 (DOC-08)
- ✓ Reconciler маршрутизирует восстановление зависших задач по `jobs.engine` (image/document), с fail-closed skip и метрикой для нераспознанного engine — Phase 10 (DOC-09)
- ✓ Отдельный бинарник `cmd/document-worker` со своим `DOCUMENT_ENGINE_TIMEOUT`/`DOCUMENT_WORKER_CONCURRENCY`; истечение `DOCUMENT_ENGINE_TIMEOUT` классифицируется terminal (в отличие от image-движка, где таймаут — transient) — Phase 10 (DOC-07, DOC-08)
- ✓ Docker-образ разделён: `Dockerfile.worker` снова libvips-only, LibreOffice изолирован в `Dockerfile.document-worker` с tini как PID 1 — Phase 10 (DOC-07)
- ✓ Engine-aware API-роутинг: `handleCreateJob` выбирает очередь по контенту (`Converter.Engine()`/`Registry.EngineFor`), документы минуют image-only dimension-check; Content-Type parity для pdf и 6 документных форматов — Phase 11 (DOC-10, live e2e verified: все 6 пар docx/xlsx/pptx/odt/ods/odp → pdf + подписанный webhook)

- ✓ Кросс-конвертация внутри документного класса (docx↔odt, xlsx↔ods, pptx↔odp) через явную (source,target) filter-таблицу LibreOffice; выход валидируется тем же SniffContainer, что и вход — Phase 13 (CONV-01, CONV-02, live e2e verified, 6 пар)
- ✓ OLE-CFB входы (legacy binary doc/xls/ppt и запароленные OOXML) отклоняются одним чётким 422 до записи в S3/Postgres — Phase 13 (SAFE-01, live verified оба под-случая)
- ✓ Validated `opts`: закрытый allowlist (типизированная структура), клиентские байты никогда не попадают в argv движка (injection-тест); PDF/A-2b экспорт с worker-side OutputIntent-проверкой — Phase 14 (OPTS-01/02, verified 9/9, live PDF/A на LO 7.4)
- ✓ HTML→PDF через chromium-headless (третий engine-class): офлайн-рендеринг (live-canary: ноль сетевых соединений по всем векторам), JS отключён CSP-инъекцией, print-опции через тот же opts-механизм — Phase 15 (HTML-01/02/03, verified 4/4 + security 14/14)
- ✓ Webhook-доставка развязана с engine-воркерами: выделенный `cmd/webhook-worker` ×2 реплики — единственный consumer webhook-очереди; reconciler-sweeper ровно один на флот (Postgres advisory lock, mutex-guarded conn lifecycle после gap-closure 16-05); SC1-3 live-verified — Phase 16 (WEBH-01)
- ✓ Унаследованный tech debt v1.0–v1.2 закрыт (extra_hosts, engine-константы, E2E-таймауты, gofmt, compose-audit) — Phase 12 (DEBT-01..05)

- ✓ 4-уровневый CI (GitHub Actions): gate → -race → bake 5 образов (per-target gha cache, free-disk) → live compose-E2E (advisory на PR / required на main, teardown if:always, логи артефактом, concurrency-отмена) — Phase 19 (CI-01..04, live-proven run 29207810893; первый ран сам вскрыл 429-конфиг-баг E2E-окружения и доказал failure-path)
- ✓ Именованные пресеты: internal/presets (shadowing client>system в SQL, bump-on-update, no-leak 422), cmd/manage-presets (5 глаголов), preset=<name> в POST /v1/jobs c XOR, re-валидацией хранимых opts и TOCTOU-речеком; provenance в jobs.preset_name/version — Phase 18 (PRST-01..04, live 33/33 ×3)
- ✓ Хвост v1.3-долга закрыт: мёртвая webhook-обвязка удалена, fakeEnqueuer race-safe, image E2E-тест; бонус — терминальный PGAdvisoryLock.Close (DEFER-17-01) — Phase 17 (DEBT-06..08)

- ✓ REST self-service пресетов (/v1/presets, узкий DTO, no-leak, 409) + /v1/formats (registry-derived) — Phase 20 (PRAPI-01..03, live 42/42 ×3)
- ✓ MCP-сервер: cmd/mcp-server (stdio, go-sdk v1.6.1 — первая новая зависимость с v1.0), 5 инструментов, блокирующий convert_file с progress, zero-privilege HTTP-клиент с редакцией ключа — Phase 21 (MCP-01..05, live stdio-гейт ×2; SEED-003 implemented)
- ✓ CFB-различение: собственный bounded-парсер директории (cycle-guard, fuzz 3.5M/0), три различённых 422 — Phase 22 (CFB-01..02)
- ✓ Настоящая ISO 19005-2b валидация PDF/A: veraPDF в document-worker (Debian-JRE, amd64 pin), terminal fail-closed, замеренный go/no-go (p95 4.65s/10s) — Phase 23 (PDFA-01..02)
- ✓ Полный стек разворачивается в k8s одной командой (`helm install deploy/chart/octoconv` + values-local) на OrbStack и проходит E2E внутри кластера как Job (9/9, presigned FQDN, NetworkPolicy-scoped /metrics, migrate/createbucket ordering) — Phase 24 (K8S-01..03)
- ✓ MCP доступен как in-cluster streamable-HTTP-эндпоинт: cmd/mcp-http, per-request caller-key pass-through (под без ключей), presigned-only результаты, live-гейт с реальной конверсией и 401/403-кейсами — Phase 25 (MCPH-01/02)
- ✓ Operator-only REST для system-пресетов: /v1/system/presets за OPERATOR_CLIENT_IDS env-allowlist (fail-closed/fail-loud), byte-identical no-leak 404, ноль миграций; попутно закрыт version-collision в repo.Create (deactivate→recreate) — Phase 26 (OPER-01, verification 5/5)
- ✓ KEDA-автоскейл per engine-class: queue-depth экспозиция перенесена на always-on api (все 4 очереди; воркеры больше не регистрируют коллектор), per-class asynq ShutdownTimeout (8s-дефолт делал grace-периоды мёртвыми), in-chart Prometheus + 3 ScaledObject (minReplicaCount 0, pending+active PromQL, двойной флаг keda.enabled&&prometheus.enabled), webhook-worker жёстко 2 реплики без ScaledObject; live-гейт scripts/keda-gate.sh 18/18 на OrbStack (SC1 через external metrics API при 0 реплик, все 3 класса 0→1, image полный цикл →0) — Phase 27 (KEDA-01/02, verification 8/8; залповый 0→N→0 под нагрузкой — Phase 28)
- ✓ Load-proof автоскейла с таймстамп-доказательством: залп 20 image-джобов при истинном нуле → 4 реплики за 11s (SC1 ≥2/60s), drain +76s, scale-to-zero +136s; 178s document-конверсия пережила настоящий KEDA/HPA-даунскейл 2→1 (SIGTERM за 142.8s до завершения, exit 0, ровно один queued→active, 188s запаса grace); evidence (CSV+PNG+транскрипт 27/27+таймстампы) закоммичен в phases/28/evidence/; попутно WR-02 закрыт (условный spec.replicas на scaled-классах) + values-gated HPA scaleDown stabilization override — Phase 28 (KEDA-03, verification 10/10; четвёртый OrbStack-клин задокументирован и восстановлен k8s hard-cycle) — milestone v1.6 phases complete
- ✓ Hardening-хвост v1.6 закрыт: WR-01 (ignoreNullValues:false на всех 3 ScaledObject — падение api больше не читается как пустая очередь, fallback держит 1 реплику) + WR-05 checksum (prometheus config-hash через named-template, без рекурсии) + WR-06 (retry-в-PromQL-триггере); OPER-01 live-гейт `/v1/system/presets` против compose (61/61, OPERATOR_CLIENT_IDS проброшен); шесть гейт-тулинг warning'ов; K8S-02 presigned direct-dial с хоста без обхода (keda-gate.sh 21/21) — Phase 29 (HARD-01..04, verification 4/4; live-re-run keda-load-proof.sh отложен на Phase 33, watcher-kill упрочён pkill-fallback)
- ✓ Standalone AudioConverter (четвёртый engine-класс, standalone — регистрация в Registry отложена на Phase 31 по scope fence): двухстадийный ffmpeg→whisper-cli v1.9.1 пайплайн (пиненный тег, -DGGML_NATIVE=OFF, SHA-256-пин модели ggml-base), txt/srt/vtt/json через Pair (16 пар), target=json с сегментными+пословными таймстампами (схема live-верифицирована против реального бинаря); ID3v2-aware fail-closed SniffAudio (synchsafe-скип, footer-flag, m4aBrands только M4A/M4B); ffprobe duration-гард с float-space валидацией (NaN/Inf/negative/overflow — CR-01 amd64-обход закрыт); AudioOpts через checkStrictObject c closed allowlist (auto/en/ru/es/fr/de) + injection-тест; hallucination-on-silence — accepted residual risk — Phase 30 (AUD-01..04, verification 5/5; code review 1 Critical + 4 Warning найдены и исправлены)
- ✓ Audio-класс встроен в async-контур end-to-end: миграция 0006 (jobs.engine += 'audio'), регистрация AudioConverter, выделенная audio-очередь (TypeAudioConvert/QueueAudio, свой retry schedule, AudioUniqueTTL 2570s > worst-case 2450s — T-03-10), stage-aware isAudioTerminal (Key Decision 1: ffmpeg-стадия/ffprobe-детерминированные — terminal, whisper-стадия timeout — transient, duration_exceeded — отдельный error_code), duration-гард реально вызывается в process() до Convert (T-30-08 закрыт, IN-02 pinning-тест), SniffAudio врезан в API с байт-точным тестом (rest-reader, не file.ReadAt), reconciler-роутинг + SC4-тест нулевых ложных recovery, cmd/audio-worker; live E2E: jfk.wav → 202 → queued→active→done за ~2.8s, транскрипт совпал — Phase 31 (AUD-05, verification 4/4; code review 6 Warning найдены и исправлены, вкл. ffprobe-ветку классификатора и budget-floor против S3-столов)
- ✓ Audio-worker контейнеризован с measured RTF-гейтом: Dockerfile.audio-worker (3-stage, whisper.cpp v1.9.1 по commit-hash пину с rev-parse guard, -DGGML_NATIVE=OFF, модель запечена с SHA-256 fail-closed, образ 682MB arm64); cgroup v2 детекция CPU-лимита → whisper -t (AUDIO_THREADS → cpu.max → NumCPU, ParseInt+positivity против Inf/NaN); scripts/audio-rtf-measure.sh: p95 RTF=0.206 (N=10, --cpus=2/-t 2/1g, arm64-каверза записана) → AUDIO_ENGINE_TIMEOUT=742s GO (17.6% под 900s CAP), NO-GO-рычаг применён: AUDIO_MAX_DURATION_SECONDS 14400→1800, AUDIO_WORKER_CONCURRENCY=1 (RSS ~728MiB); compose-сервис + IN-02 7-way пропагация (742s ×7) + stale 5m CAP-оверрайды webhook-worker'ов исправлены на 15m + stop_grace_period 762s; CI bake matrix; повторяемый TestAudioConversionE2E в internal/e2e (PASSED live 8.10s через контейнер, signed webhook, DB-proof) — Phase 32 (AUD-06/07, verification 6/6; plan-checker поймал 2 блокера до исполнения — stale CAP-дрейф и echo-верify; code review 5 Warning исправлены)
- ✓ Audio-класс автоскейлится в k8s с production-паритетом: audio-worker Deployment (terminationGracePeriodSeconds 772 > 752 ShutdownTimeout > 742 ENGINE_TIMEOUT) + KEDA ScaledObject с WR-01 триадой verbatim (ignoreNullValues:"false", fallback.replicas:1, retry-inclusive PromQL) и первым non-null production scaleDownStabilizationSeconds 900 (742s-задачи не влезают в 300s HPA-дефолт); configmap: 5 AUDIO_* ключей + chart-сторона stale 5m→15m фикса; QueueAudio в api-коллекторе (5 очередей); scripts/keda-audio-loadproof.sh (клон, frozen-скрипты byte-unchanged); live proof: 0→1 scale-from-zero 10/10 с запечённой моделью, Pulling→Pulled ≈0 на OrbStack shared store (bake-in подтверждён, реверсивен, registry cold-pull отложен до реального registry); отложенный Phase-29 re-run keda-load-proof.sh: SC1/SC2 live PASS, его SC3 BUSY_POD упал громко на уже принятом WR-05 jsonpath-резидуале (эмпирически подтверждён, скрипт не тронут) — Phase 33 (AUD-08, verification 4/4; code review 2 Warning исправлены) — **milestone v1.7 phases complete (29-33)**

- ✓ Клиент может отправить аудиофайл (mp3/wav/m4a/ogg) и получить транскрипт (whisper.cpp v1.9.1, офлайн) через тот же async-пайплайн, что и остальные классы — v1.7 (live E2E compose 8.10s + k8s scale-from-zero 10/10)
- ✓ Audio-класс встроен в полный контур: fail-closed валидация содержимого, stage-aware retry-семантика, KEDA-скейлинг, chart с production-паритетом — v1.7 (audit 12/12, integration 22/22)

### Active

<!-- Следующий милстоун не определён. Запустить /gsd:new-milestone — он создаст свежий REQUIREMENTS.md. -->

Скоуп v1.13 Inspect & Operations (детальные REQ-ID — в `.planning/REQUIREMENTS.md` после
шага requirements):

- [ ] JobRunner: семь конверсионных хендлеров → один код-путь, пер-энджинные различия как данные (SEED-020)
- [ ] Структурированные коды ошибок наружу + закрытие `invalid_options` metrics-гэпа (SEED-020)
- [ ] Синхронный `POST /v1/inspect` с явными границами работы и собственным rate-бюджетом
- [ ] Born-digital PDF маршрутизируется по результату inspect, а не отвергается вслепую (SEED-021)
- [ ] Измерение `operation` в API и реестре (`convert` по умолчанию) + `role` у `job_outputs`
- [ ] `sanitize` как первая асинхронная не-convert операция + отчёт как output с `role=report` (SEED-013)
- [ ] Хвост долга v1.12: 52 WR-01 (застрявшая джоба и суточный кап), 52 WR-09 (README), `PurgeUsage` на часах Postgres

Отложено, не выбрано в v1.13:

- [ ] SEED-017 (PDF split/merge) — первый конкретный спрос на N входов/выходов, под который учёт байт уже построен
- [ ] SEED-018 (составные pipeline, parent/child jobs) — следующий шаг после того, как операций станет больше одной
- [ ] SEED-019 — партиционирование `jobs`/`job_events` вместо удаления строк, если объём перерастёт батчевый purge
- [ ] SEED-006 (архивы), SEED-014 (оптимизация размера), SEED-007 (превью), SEED-005 (CAD) — новые классы из Notion-волны, после `operation`
- [ ] FAIR-V2-01 (суб-очереди/weighted-планировщик) — evidence-gated: только если данные OPSV-01 покажут, что admission-капа реально не хватает

### Out of Scope

- CAD-движок — открытый вопрос в спеке (нативные форматы: OSS vs коммерческий SDK vs cloud API), не решён — отложен
- Другие классы движков (av/ffmpeg, archive, probe) — следующий этап развития, не этот (HTML→PDF и кросс-конвертация документов переехали в Active скоуп v1.3)
- Полный контракт ядра (Handler/Capability/Input/Output/Progress) — решено расширять существующий `Converter`/`Registry` вместо рефакторинга (v1.2 — второй движок укладывается в текущую абстракцию)
- KEDA-автоскейл / полноценная Kubernetes-оркестрация — инфраструктурная задача вне фокуса кодовых фаз
- Публичный релиз и проверка имени (npm/PyPI/Docker Hub/домен) — сервис внутренний, не актуально

## Context

- Полная архитектура и зафиксированный стек задокументированы в Notion: «Сервис конвертации файлов — стек и архитектура (Go)» и «OctoConv — стек и модель данных» (там же — полный DDL модели данных: `clients`, `presets`, `jobs`, `job_inputs`, `job_outputs`, `job_events`, `webhook_deliveries`).
- Статус реализации на 2026-06-30 зафиксирован в Notion-странице «OctoConv — статус реализации» — сделан только image/libvips срез, 7 коммитов на ветке `feat/scaffold-and-infra`, не влито в `main`.
- Рядом существовавший каталог `octo-conv` (Rust-прототип) не используется — разошёлся со спекой; текущая реализация на Go написана с нуля.
- Клиенты сервиса — внутренние сервисы компании, не внешние потребители. Это снижает требования к публичной документации/биллингу, но не снимает требований к auth и rate limiting.
- **Milestone v1.0 (Hardening MVP) shipped 2026-07-08.** 4 фазы, 15 планов, ~38 задач, ~6100 строк Go, 9 дней. Полный отчёт: `.planning/milestones/v1.0-ROADMAP.md`, `.planning/milestones/v1.0-REQUIREMENTS.md`, `.planning/milestones/v1.0-MILESTONE-AUDIT.md`.
- **Milestone v1.1 (Tech Debt Cleanup) shipped 2026-07-08** (тот же день — короткий закрывающий milestone). 3 фазы, 7 планов, 13 задач, 2 дня разработки. Закрыл все 5 tech-debt пунктов из v1.0-аудита без единого переноса. Полный отчёт: `.planning/milestones/v1.1-ROADMAP.md`, `.planning/milestones/v1.1-REQUIREMENTS.md`, `.planning/milestones/v1.1-MILESTONE-AUDIT.md`.
- Изначальные технические долги из `.planning/codebase/CONCERNS.md` (single-attempt processing, отсутствие таймаута вне конвертации, статичный `/healthz`, отсутствие content-валидации) — все закрыты в рамках Phase 3/4.
- Схема БД полностью используется: `clients` (auth, Phase 1), `callback_url`/`webhook_deliveries` (Phase 2); `presets` остаются неиспользуемыми — вне скопа обоих milestone.
- v1.1-аудит (`v1.1-MILESTONE-AUDIT.md`) прошёл без блокеров и без tech debt (4/4 требования, 5/5 точек интеграции, живые smoke-тесты всех новых механизмов по отдельности и в комбинации против пересобранного docker-стека) — впервые за проект milestone закрылся с нулевым переносом.
- Code review при исполнении Phase 2 (v1.0) нашёл и сразу исправил 2 критических дефекта: webhook-доставка следовала HTTP-редиректам (SSRF-обход валидации `callback_url`) и off-by-one в расписании retry-backoff (сокращал заявленное ~30-минутное окно до ~16 минут). Оба исправления покрыты тестами.
- **Milestone v1.2 (Document Engine Class) shipped 2026-07-10.** 4 фазы (8–11), 13 планов (вкл. gap-closure 11-04), 71 коммит, +2754 строк Go (без .planning), ~2 дня. Второй класс движков: docx/xlsx/pptx/odt/ods/odp → PDF через LibreOffice headless в отдельном контейнере, live E2E по всем 6 парам. Аудит: 10/10 требований, 10/10 интеграционных связей. Полный отчёт: `.planning/milestones/v1.2-ROADMAP.md`, `-REQUIREMENTS.md`, `-MILESTONE-AUDIT.md`.
- **Milestone v1.12 (Consumer Accounting & Fairness) shipped 2026-08-07.** 5 фаз (51–55), 30 планов, 79 задач, 130 коммитов, +13230/−167 строк (без .planning), ~5 дней. Первый милстоун, не добавляющий движок: учёт потребления, квоты, справедливость, видимость для оператора. Один живой гейт — multi-replica quota correctness при `api.replicas: 2` (10/10 раундов, 167 ассертов, evidence закоммичен). Аудит: `tech_debt` — 15/15 требований, 5/5 фаз, 9/9 межфазных стыков, 1/1 E2E-поток, 0 блокеров, 16 непроблокирующих пунктов долга записаны. Фазы 52 и 53 прошли реальный цикл `gaps_found` → закрытие → ре-верификация. Полный отчёт: `.planning/milestones/v1.12-*`.
- Перенесённый из v1.12 долг (детали — в STATE.md Deferred Items): самый весомый — 52 WR-01, in-flight джоба тратит суточный кап без временнóй границы, поэтому навсегда застрявшая джоба блокирует бюджет клиента каждый следующий UTC-день, и у оператора нет средства это снять кроме сырого SQL; связка «доступность квот ↔ здоровье `cmd/webhook-worker`» не задокументирована. Плюс самопротиворечивая секция квот в README и `PurgeUsage`, считающий cutoff по Go-часам там, где весь милстоун перешёл на часы Postgres.
- **Milestone v1.7 (Audio Engine & Hardening) shipped 2026-07-18.** 5 фаз (29-33), 18 планов, 44 задачи, ~170 коммитов, +4495/−38 строк (без .planning, фазы 30-33), ~2 дня. Четвёртый engine-класс: аудио-транскрипция whisper.cpp v1.9.1 офлайн (mp3/wav/m4a/ogg → txt/srt/vtt/json с пословными таймстампами), RTF-измеренный AUDIO_ENGINE_TIMEOUT=742s, KEDA scale-from-zero live-proven 10/10. Аудит: 12/12 требований, 22/22 интеграции, 2/2 E2E. Полный отчёт: `.planning/milestones/v1.7-*`.
- **Milestone v1.6 (Kubernetes & KEDA) shipped 2026-07-17.** 5 фаз (24-28), 14 планов, 33 задачи, 129 коммитов, +5978/−56 строк (без .planning), ~3.5 дня. Первый инфраструктурный милстоун: Helm-чарт на OrbStack k8s, KEDA scale-from-zero per engine-class, таймстампированный load-proof 0→4→0 (evidence в phases/28/evidence/), MCP streamable-HTTP, operator system-presets REST. Аудит: passed 9/9, advisory tech debt (OPER-01 live-script gap, WR-01 trigger semantics, gate-tooling warnings). Четвёртый задокументированный OrbStack-клин случился и восстановлен в ходе load-proof. Полный отчёт: `.planning/milestones/v1.6-*`.
- **Milestone v1.5 (MCP Access & Document Fidelity) shipped 2026-07-13.** 4 фазы (20–23), 10 планов, 71 коммит, +5537/−84 строк (без .planning). Аудит: 12/12, 6/6 интеграции. Первый measured go/no-go гейт (veraPDF JVM). Полный отчёт: `.planning/milestones/v1.5-*`.
- **Milestone v1.4 (CI, Presets & Debt Cleanup) shipped 2026-07-13.** 3 фазы (17–19), 8 планов, 54 коммита, +2261/−60 строк (без .planning), ~2 дня. Аудит: 11/11 требований, 6/6 интеграции. Репозиторий публичный; CI живой (badge passing). Полный отчёт: `.planning/milestones/v1.4-*`.
- **Milestone v1.3 (Document Class v2) shipped 2026-07-12.** 5 фаз (12–16), 17 планов (вкл. gap-closure 16-05), 147 коммитов, +4773/−145 строк (без .planning), ~2 дня. Аудит: 14/14 требований, 7/7 интеграционных проверок, 8/8 E2E-потоков. Полный отчёт: `.planning/milestones/v1.3-ROADMAP.md`, `-REQUIREMENTS.md`, `-MILESTONE-AUDIT.md`.
- Tech debt, перенесённый из v1.3-аудита (advisory): мёртвая webhook-обвязка в `cmd/document-worker`/`cmd/chromium-worker` (WR-02/WR-03 из 16-REVIEW); data race в `fakeEnqueuer` тест-хелпере при full-package `-race`; нет dedicated image E2E-теста; SEED-001 dormant.

## Constraints

- **Tech stack**: Go 1.26, chi (API), asynq + Redis (очередь), PostgreSQL 18 (система записи), S3/MinIO (хранилище) — зафиксировано в Notion-спеке, не пересматривается на этом этапе
- **Auth**: API-ключи через существующую таблицу `clients` — не вводить отдельный внешний auth-провайдер
- **Deployment**: Docker / docker-compose для локальной разработки; Kubernetes + KEDA — будущее, вне текущего фокуса
- **Сlients**: только внутренние сервисы компании — публичная многоарендность и биллинг не требуются на этом этапе

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Слить `feat/scaffold-and-infra` в `main` в начале этапа | Дальнейший hardening должен идти поверх `main`, а не в изолированной ветке | ✓ Good — уже была слита к моменту планирования Phase 1 |
| Auth + rate limiting — первый приоритет hardening | API сейчас полностью публичный без аутентификации — самый большой риск | ✓ Good — Phase 1 закрыта, 12/12 must-haves, включая gap-closure по spoofable IP-guard |
| Все пункты hardening (auth, webhooks, reconciler, magic-bytes+TTL, наблюдаемость) — в v1 этого этапа, auth первым | Все критичны для production-готовности; различается только порядок реализации по убыванию риска | ✓ Good — все 4 фазы закрыты, milestone v1.0 shipped 2026-07-08, 24/24 требования, 0 блокеров |
| Retry-safety должен предшествовать reconciler'у внутри Phase 3 | Reconciler поверх однопопыточного воркера дублировал бы обработку задач | ✓ Good — оба закрыты в одной фазе, живой E2E подтвердил отсутствие дублей |
| Content validation, storage TTL и observability объединены в одну закрывающую фазу | Все три независимы друг от друга и от auth/webhook/reconciler критического пути | ✓ Good — Phase 4 закрыта одним блоком, 5 планов в 3 волнах |
| pandoc владеет ТОЛЬКО markup-семейством, →pdf исключён (v1.11) | docx→pdf уже за LibreOffice, html→pdf за chromium; pandoc-PDF потянул бы TeX и убил лёгкий образ | ✓ Good — пары disjoint by construction, `EngineFor` разводит `(html,pdf)`→chromium и `(html,md)`→pandoc без единой строки нового роутинга |
| `PANDOC_ENGINE_TIMEOUT=65s` выведен измерением, не скопирован у соседа (v1.11) | Копирование чужого таймаута — зафиксированный антипаттерн проекта; p95 меряется на своём корпусе | ✓ Good — worst-cell html_large→md p95=32.318s × 2.0 = 65s, 92.8% под 900s CAP, Path-B рычаг наготове |
| KEDA-профиль pandoc — «быстрый движок» (`scaleDownStabilizationSeconds: null`), а не 900s соседей (v1.11) | 65s worst-case покрыт дефолтными 300s HPA с 4× запасом; ненулевой knob был бы cargo-cult тяжёлых движков | ✓ Good — falsy-0 guard всё равно на месте, gate-оверлей переопределяет его в 0 и этим же проверяет guard вживую |
| `PANDOC_WORKER_CONCURRENCY` 2→1 и `PANDOC_MAX_INPUT_BYTES` 4→3 MiB — по живым OOM, а не по предположению (v1.11) | Офлайн-калибровка мерила pandoc в одиночку и структурно не видела, что Go-воркер делит тот же cgroup | ✓ Good — замер дал ~300 MiB anon на MiB входа; при concurrency 2 оставалось 1 MiB до OOM. Pandoc оказался memory-bound, а не «лёгким» — пропускная способность теперь из KEDA scale-out |
| Detected-формат (не расширение) — источник истины для pair-check в Phase 4 | Расширение может лгать; magic bytes — единственный проверяемый факт о содержимом | ✓ Good — reorder подтверждён живым 422 на несовпадении |
| `/metrics` на отдельном localhost-only порту, а не на публичном `API_ADDR` | Операционные данные (глубина очереди, исходы задач) не должны быть доступны любому клиенту с сетевым доступом к API | ✓ Good — подтверждено: порт не публикуется на хост вообще |
| SSRF-валидация `callback_url` блокирует весь RFC1918/loopback без исключений | Принято в Phase 2 как безопасный дефолт | ✓ Good — Phase 5 добавила узкий opt-out только для RFC1918 (`WEBHOOK_ALLOW_PRIVATE_IPS`); loopback/link-local/metadata-endpoint остаются заблокированы всегда |
| Reconciler webhook-gap sweep: `asynq.Unique` на webhook-очереди с TTL, деривированным из реального retry-бюджета (зеркалит `ImageUniqueTTL`) | Защита от двойной доставки при гонке sweep-тиков; TTL должен учитывать jitter `WebhookRetryDelay`, иначе получится average-case, а не worst-case | ✓ Good — Phase 6, TTL=2477.5с подтверждён тестами, live-verified без дублей |
| Decompression-bomb защита: свои zero-dependency парсеры размеров вместо golang.org/x/image или shell-out в vipsheader | Согласуется с философией zero-new-deps из Phase 4; избегает нового process-exec surface в API | ✓ Good — Phase 7, все 5 форматов (включая HEIC) защищены одинаково, 0 новых зависимостей |
| CAD и остальные классы движков — вне скопа этого этапа | Открытый вопрос по CAD SDK не решён; остальные движки — следующий этап роста, не текущий hardening | — Pending |
| document-движок расширяет существующий `Converter`/`Registry`, а не вводит Handler/Capability/Input/Output контракт | Второй движок (LibreOffice) укладывается в текущую абстракцию без изменений; полноценный контракт остаётся отложен до появления реальной потребности (напр. progress-репортинга) | ✓ Good — v1.2: LibreOfficeConverter + `Engine()`/`EngineFor` вписались в реестр без ломки контракта; live E2E по всем 6 парам |
| HTML→PDF исключён из v1.2 | LibreOffice слабо рендерит современный CSS/JS; нужен отдельный chromium-based движок — самостоятельное решение, не расширение LibreOffice-движка | ✓ Good — реализован в v1.3 Phase 15 как третий engine-class по шаблону v1.2 |
| Кросс-конвертация через явную (source,target) filter-таблицу, а не generic вычисление фильтра | Явная таблица = проверяемый allowlist; generic вычисление рискует тихо включить непроверенные пары | ✓ Good — v1.3 Phase 13: 6 симметричных пар, все live-verified на LO 7.4 |
| OLE-CFB: один 422 на оба случая (legacy и encrypted), без парсинга CFB-директории | Оба случая всё равно неконвертируемы; различение требует настоящего CFB-парсера — отложено (DOCV3-02) | ✓ Good — v1.3 Phase 13: 8-байтовый magic-детект, live-verified |
| PDF/A: sanity-чек OutputIntent вместо полной ISO 19005 (veraPDF) валидации | veraPDF = Java-стек в контейнере воркера; для внутренних клиентов достаточно структурного маркера | ✓ Good — v1.3 Phase 14; полная валидация отложена (DOCV3-01) |
| Webhook-доставка: выделенный webhook-worker ×2 + Postgres advisory lock для singleton-sweeper (вместо leader election или фиксированного «главного» воркера) | Простейший примитив, дающий exactly-one-sweeper на флот без новых зависимостей; консьюмеры симметричны | ✓ Good — v1.3 Phase 16: SC1-3 live-verified, ~11s failover; conn-lifecycle гэпы (CR-01/WR-01) закрыты в 16-05 с mutex + -race тестом; v1.4 Phase 17 добавила терминальный Close (DEFER-17-01) |
| Пресеты: preset XOR явные opts, client затеняет system, bump-on-update, re-валидация хранимых opts при каждом использовании | Сохранённым конфигам нельзя доверять после смены allowlist; XOR исключает неоднозначный merge | ✓ Good — v1.4 Phase 18: live 33/33; TOCTOU-речек перед INSERT |
| CI: e2e-уровень advisory на PR / required на main; bake по compose-файлу; тест-лимиты только в e2e-оверрайде | PR не блокируется флаки-инфраструктурой, main защищён; одна декларация build-таргетов; продакшен-лимиты неприкосновенны | ✓ Good — v1.4 Phase 19: run 2 полностью зелёный; 429-урок первого рана зафиксирован |
| Отдельный `cmd/document-worker` бинарник/контейнер вместо второго `asynq.Server` внутри image-воркера | Тяжёлый footprint LibreOffice не должен попадать в контейнер image-воркера; ресурсная изоляция по классам движков | ✓ Good — v1.2 Phase 10: Dockerfile.worker снова libvips-only, LibreOffice изолирован с tini как PID 1 |
| Engine-класс определяется по контент-детектированному формату (`EngineFor(detected, target)`), не по расширению файла | Расширение подконтрольно атакующему; magic-bytes/структурный sniff — единственный проверяемый факт | ✓ Good — v1.2 Phase 11: fail-closed default на нераспознанный engine, live-verified |
| Resource-exhaustion через сложный документ (DOC-V2-05) — accepted residual risk v1.2 | Митигируется только `DOCUMENT_ENGINE_TIMEOUT` + потолком конкуренции document-воркера; активный анализ сложности отложен | — Pending (принятый риск, пересмотреть при росте нагрузки) |
| `file://` residual read внутри chromium-worker — accepted residual risk v1.3 (Phase 15) | Live-tested (Plan 04, item 6): `<img src="file:///usr/share/pixmaps/debian-logo.png">` (world-readable, non-input file) успешно загрузился внутри рендера под USER nobody — passive subresource loads (img/link/script src) читают ЛЮБОЙ файл, доступный uid nobody, включая потенциально workDir других одновременно выполняющихся job'ов (0700 не изолирует общий UID). Активный `fetch()`/XHR к `file://` при этом блокируется самим Chromium — подтверждено отдельно. Матчит DOC-V2-05 precedent (internal-only clients trust model) | — Pending (принятый риск; митигация — bind-mount только собственного workDir job'а — отложена как будущая опция, не блокирует Phase 15) |
| tini как PID 1 в `Dockerfile.chromium-worker` — сохранён, несмотря на неподтверждённую живым тестом необходимость именно для этой invocation-формы | Live-tested (Plan 04, item 7): `runCommand`-точное поведение (SIGKILL всей process-группы через `-PGID`) НЕ оставило зомби-процессов ни с tini, ни без него (3 повтора без tini, 1 с tini) — вероятно потому что весь одномоментный SIGKILL убивает parent+children синхронно, а не оставляет осиротевших детей. Tini оставлен как defence-in-depth (совпадает с собственным биасом D-09 "keep it" + сигнал-форвардинг для graceful shutdown), изменений в Dockerfile не внесено | ✓ Good — поведение задокументировано честно, а не предположено; изменений нет |
| Stage-aware классификация таймаутов аудио (Key Decision 1, v1.7): ffmpeg-стадия/детерминированный ffprobe — terminal, whisper-timeout на валидированном аудио — transient | Малформленный вход не ретраится впустую; честный timeout дорогой транскрипции получает свежий CPU; blanket-terminal отвергнут как строго менее корректный | ✓ Good — Phase 31: TestIsAudioTerminal pinning; code review добавил недостающую ffprobe-ветку и budget-floor против S3-столов до продакшена |
| Модель `base` по умолчанию, реверсивно через build-arg (Key Decision 2, v1.7) | `small` утяжеляет образ к ~1GB и бьёт по scale-from-zero; выбор не запечатан навсегда | ✓ Good — Phase 32: образ 682MB, RTF p95=0.206 на base достаточен (742s timeout с запасом) |
| Model bake-in (не volume) с обязательным measured load-proof (Key Decision 3, v1.7) | Простейший вариант при офлайн-ограничении, но обязан быть доказан измерением, не предположением | ✓ Good — Phase 33: 0→1 scale-from-zero 10/10 live; Pulling→Pulled ≈0 на OrbStack shared store; registry cold-pull задокументирован как неизмеримый локально — решение реверсивно |
| AUDIO_ENGINE_TIMEOUT измеряется RTF-гейтом, не копируется от соседних классов | Копированная константа для самого дорогого класса — прямой путь к гонке T-03-10 или мёртвым таймаутам | ✓ Good — Phase 32: 742s из p95×2.0 формулы; NO-GO-рычаг сработал по назначению (max duration 4h→30min вместо раздувания таймаута) |
| Замороженные gate-скрипты (keda-load-proof.sh, keda-gate.sh) byte-unchanged; новая функциональность — только новыми скриптами | Стабильность доказательной базы: прошедшие live-гейты не мутируют задним числом | ✓ Good — Phase 33: WR-05 дефект замороженного скрипта эмпирически подтверждён и честно задокументирован вместо тихого фикса; forward-fix отложен осознанно |
| `OCR_ENGINE_TIMEOUT`/`OCR_MAX_PAGES` измеряются seconds-per-page гейтом (`scripts/ocr-timing-measure.sh`), не копируются от AV/AUDIO | Cost-модель OCR — per-page, а не per-input-duration; копирование соседнего таймаута — прямой путь к T-03-10 гонке или мёртвому таймауту (PITFALLS.md Pitfall 7) | ✓ Good — Phase 45 Plan 03: supervised run ALL GREEN (arch=arm64, cpus=2.0, memory=1g, N=10, omp_thread_limit=2, представительный синтезированный корпус — committed unit-test fixtures 2 KB давали заниженные 0.148 s/page, опасный 60s таймаут); worst cell dense_eng p95=5.81 s/page; D-03 provisional OCR_MAX_PAGES=200 пробил 900s CAP (ceil(200×5.81×2.0)=2324s) → Path-B NO-GO рычаг сработал (200→64); OCR_ENGINE_TIMEOUT=ceil(64×5.81×2.0)=744s (17.3% запас под 900s reconciler CAP), byte-identical на всех 9 queue-client сервисах, stop_grace_period 764s; 744s ≠ AV(753s) ≠ AUDIO(742s) |
| Language-pack scope: короткий SHA-пиненный `tessdata_fast` allowlist (eng/rus), а не полный upstream tessdata/tessdata_best | Bloat напрямую бьёт по KEDA scale-from-zero cold-start экономике; зеркалит дисциплину bake-in модели audio-воркера | ✓ Good — Phase 45: eng.traineddata/rus.traineddata SHA-256-пиненные на конкретный commit `tessdata_fast`, matching closed `OCROpts` allowlist |
| OCR — 6-й hardcoded engine-класс (compile-time `Registry.Register`, hardcoded `EngineOCR`, миграция `0009_ocr_engine.sql`); SEED-010 (динамический реестр движков) остаётся dormant | Согласуется с паттерном image/document/html/audio/av; обобщение `converters.go`'s `init()` — не повод этого milestone | ✓ Good — Phase 43/44: OCRConverter зарегистрирован тем же путём, что и остальные пять движков, ноль изменений в форме реестра |
| `internal/convert/progress.go`'s общие типы (`ProgressUpdate`/`ProgressSink`/`ProgressConverter`) — zero edits для OCR | Page-granular прогресс OCR должен быть собственным маленьким `ocrprogress.go` page-EMA эстиматором поверх того же `sink.Update(...)` контракта, не веткой внутри ffmpeg-специфичного `progressBlockParser` | ✓ Good — Phase 44: v1.9 progress/SSE/webhook инфра переиспользована engine-агностично, byte-unchanged fence подтверждён тестом |
| Dockerfile.ocr-worker должен запекать tesseract `configs`/`tessconfigs` рядом с `tessdata_fast`, а не только `.traineddata`-файлы | Live E2E (45-04) вскрыл реальный продакшн-баг: без configs `->hocr`/`->alto` не производили вывод ("Can't open hocr"), тогда как `->txt` работал через built-in дефолт | ✓ Good — Phase 45 Plan 04: COPY добавлен, over-strict exact-two-files assertion пересужена до `*.traineddata` (commit c1c39fc) |
| `PANDOC_ENGINE_TIMEOUT` измеряется собственным wall-sec/conversion гейтом (`scripts/pandoc-timing-measure.sh`), не копируется от OCR/AV/AUDIO | Cost-модель pandoc — per-conversion (NO MAX_PAGES-style multiplier), но копирование соседнего таймаута — тот же PITFALLS.md Pitfall 7 риск, что и у OCR/AV/AUDIO | ✓ Good — Phase 49 Plan 03: supervised run ALL GREEN (arch=arm64, cpus=2.0, memory=1g, N=10/cell, nearest-rank p95, wall-sec/conversion); worst cell html_large→md (reader-heavy HTML parsing) p95=32.318s (md→html 25.37s, md→docx 23.34s, html→docx 15.99s, latex→html 6.06s, latex→docx 3.90s; small/medium cells < 0.4s); PANDOC_ENGINE_TIMEOUT=ceil(32.318×2.0)=65s (92.8% запас под 900s reconciler CAP), byte-identical на всех 10 queue-client сервисах, stop_grace_period 85s (65+10+10); 65s ≠ OCR(744s) ≠ AV(753s) ≠ AUDIO(742s) ≠ 90s placeholder. Path-B рычаг СРАБОТАЛ, но BY MEMORY, не по CAP (честная находка гейта): zip-out цели (docx/epub) OOM-killed на 1g memory limit при 8 MiB input ("Killed", 0 байт вывода) — cliff 4 MiB=~10.2s clean → 6.5 MiB=~40s thrash → 8 MiB=OOM; `PANDOC_MAX_INPUT_BYTES` понижен 50 MiB (50<<20 placeholder) → 4 MiB (4194304) для стабильного запаса памяти; worst-cell 32.318s измерен ИМЕННО при этом 4 MiB потолке |
| Учёт пишется в ТОЙ ЖЕ транзакции, что и охраняемый терминальный переход, а не соседним вызовом (v1.12 D-05) | Воркер, умерший между закоммиченным `MarkDone` и отдельной записью учёта, оставил бы джобу терминальной, а потребление — не записанным навсегда: ни ретрай asynq, ни реконсилятор терминальную джобу не пересматривают | ✓ Good — Phase 51: доказано инверсионным прогоном (вынос записи из транзакции даёт `job_count = 2`) и soak-тестом, форсирующим гонку stale-recovery реконсилятора |
| Байты суммируются по ВСЕМ входам/выходам (`Repo.Inputs`/`Repo.Outputs`), хотя сегодня джоба всегда 1:1 (v1.12, SEED-017) | «Взять ordinal 0» сработало бы сегодня и было бы естественным — и стало бы молча неверным в день, когда приземлится PDF split/merge; ровно тот дрейф, на котором проект дважды терял время в v1.11. Суммирование по спискам сейчас не стоит ничего | ✓ Good — Phase 51: коррелированные `sum(size_bytes)` без предикатов по `ordinal`, `grep -c ordinal` по `usage.go` → 0, тест на 2 входа/2 выхода |
| Кардинальность `client_id` ограничена явным численным потолком с коллапсом в `overflow`, а не «списком реальных клиентов» (v1.12 ACCT-04) | `cmd/manage-clients` может заводить валидных клиентов бесконечно, поэтому «настоящий client id» — не граница | ✓ Good — Phase 51: `METRICS_CLIENT_ID_CEILING` (дефолт 200), first-seen-first-served без вытеснения, битый/пустой id не порождает серию |
| Квота — атомарный счёт в Postgres под `pg_advisory_xact_lock` в одной транзакции с `INSERT`, а не in-process счётчик (v1.12 QUOT-05) | `internal/ratelimit` сам документирует, что верен только при `api.replicas: 1`; повторять это в квотах нельзя. Голый `SELECT count(*)` перед вставкой позволил бы двум репликам увидеть «ниже капа» и обеим допустить | ✓ Good — Phase 55: живой прогон при `api.replicas: 2` дал ровно K=2 каждый раунд ×10; `admission.go` фиксирует отсутствие оговорки структурным аргументом + живым подтверждением |
| Суточный кап считает in-flight (`queued`+`active`) допущенную работу (v1.12 D-15, поправка от 2026-08-03) | Без этого конфигурация `max_concurrent = nil` пропускала всё до завершения джоб — кап не enforce'ил ничего. Обратная сторона принята сознательно: застрявшая джоба тратит бюджет, пока не станет терминальной (52 WR-01) | ⚠️ Revisit — механизм верен и fail-closed, но операторского средства снять застрявшую джобу нет; перенесено в долг v1.12 |
| Живой гейт чинится структурно, а не ослаблением ассерта (v1.12 Phase 55) | Раунд 4/10 упал на `admitted expected [2], got [3]` — скорее всего легитимное переиспользование слота, которое гейт не мог отличить от нарушения капа. Ослабить `==` до `<=` было бы способом «пройти», а не узнать | ✓ Good — Phase 55: тяжёлая фикстура, чтобы допущенная джоба не успевала стать терминальной внутри залпа, самопроверяющаяся граница разброса и per-round проверка из Postgres, что премисса `exact-K` истинна ДО того, как её утверждают |
| `minReplicaCount` валидируется через `kindOf` ДО приведения типа (v1.12 Phase 54, CR-01) | Sprig'овый `int` — это `cast.ToInt`, молча возвращающий `0`: `--set minReplicaCount=99.9` обходил обе проверки И стирал заданный оператором пол | ✓ Good — Phase 54: 6 bypass-входов (`2.9`/`99.9`/`-0.5`/`true`/`abc`/`""`) теперь прерывают рендер; регрессионные кейсы в CI-job'е |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd:complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-09-02 after v1.13 (Inspect & Operations) shipped*
