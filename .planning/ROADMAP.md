# Roadmap: OctoConv

## Milestones

- ✅ **v1.0–v1.12** — Phases 1–55 (archived; see `.planning/milestones/`)
- ✅ **v1.13 Inspect & Operations** — Phases 56–61 (shipped 2026-09-02)
- ✅ **v1.14 Typesetting & Debt** — Phases 62–64 (shipped 2026-09-05)
- ✅ **v1.15 Engine Confinement** — Phases 67–70 (shipped 2026-10-01)
- 🚧 **v1.16 CAD Engine** — Phases 71–76 (в работе, открыт 2026-10-02)

## Phases

### 🚧 v1.16 CAD Engine (Phases 71–76) — IN PROGRESS

Цель милстоуна: десятый engine-класс `cad` — офлайн-рендер превью и замер
геометрии для step/stp/iges/stl/dxf (OCCT + VTK/OSMesa, без X) под тем же
Landlock-конфайнментом, что и остальные восемь классов. Нумерация продолжается
с 71 (последняя фаза v1.15 — 70).

Фазы разрезаны **по рискам и по решениям, которые они разделяют**, а не по
форматам. Замер идёт первым, потому что от него зависят ruleset'ы, таймаут,
ресурсы пода и константы ракурса. Контрактная часть — снифферы и схема
measurements, самая рискованная для ревью — собирается без движка и ничего не
рекламирует. Класс входит в API **целиком, одним атомарным изменением
`AllEngines()`**, и только вместе с воркером, очередью, чартом и конфайнментом
(тесты контракта падают по имени, если что-то одно опущено). 3D и DXF разнесены
по тулчейнам (OCP + VTK против ezdxf + rsvg-convert). Таймаут измеряется
последним, когда существуют обе операции.

| # | Фаза | Требования | Состояние |
|---|---|---|---|
| 71 | Замер: ядро, рендерер, корпус, бюджеты | EVAL-01..04 | ✓ 2026-10-04 |
| 72 | Контракт без движка: снифферы, коды, схема measurements | CADIN-01..03, MEAS-05 | ✓ 2026-10-05 |
| 73 | Класс `cad` и срез `convert` под конфайнментом | CADIN-04, CCONV-01, CADW-01..03 | ✓ 2026-10-05 |
| 74 | 3D `render` и выход `measurements` | RNDR-01..05, MEAS-01..04 | ○ следующая |
| 75 | DXF: 2D-конвейер | RNDR-06, CCONV-02, CCONV-03 | ○ |
| 76 | Замеренные границы, KEDA, живой гейт, контракт превью и документация | CADW-04..06, PREV-01 | ○ |

- [x] **Phase 71: Evaluation Gate** - ядро геометрии, рендерер и 2D-путь DXF выбраны по замеру на настоящем образе; корпус, лимиты входа и лицензии известны до первой строки продакшен-кода (completed 2026-10-04)
- [x] **Phase 72: Engine-Free Contract Foundations** - fail-closed снифферы CAD-форматов, различимые коды отказов и версионированный контракт measurements — без регистрации движка (completed 2026-10-05)
- [x] **Phase 73: CAD Class & Convert Slice, Confined** - класс `cad` целиком под Landlock: STEP/IGES → STL сквозь очередь, воркер, preflight и классификацию ошибок (completed 2026-10-05)
- [x] **Phase 74: 3D Render & Measurements** - `render` step/stp/iges/stl → png с закрытыми опциями и вторым выходом `measurements` (completed 2026-10-06)
- [x] **Phase 75: DXF 2D Pipeline** - dxf → svg/pdf и `render` dxf → png через ezdxf + rsvg-convert (completed 2026-10-12)
- [ ] **Phase 76: Measured Bounds, KEDA Proof & Client Docs** - замеренный таймаут, scale-from-zero, живой гейт всех классов, контракт превью и клиентская документация

**Как читать привязку требований.** Каждое требование записано ровно в одной
фазе — той, где впервые строится механизм. Пять требований по формулировке
охватывают несколько классов входа или несколько стадий движка: CADIN-04,
CADW-02, CADW-03, MEAS-01, MEAS-03. Их владелец — фаза, где механизм появляется
(73 или 74); в фазах 74 и 75 для своей стадии/своего входа стоит **отдельный
критерий**, и верификатор этих фаз обязан проверять его, а не считать
требование закрытым с фазой-владельцем.

**Отличие от SUMMARY.md исследования.** SUMMARY относил Deployment/ScaledObject
чарта к фазе 76. Здесь Deployment и ScaledObject (с временными ресурсами)
приходят в 73: иначе API объявил бы `cad`, а установка в k8s не имела бы
воркера, и джобы вечно висели бы в `queued` — тот самый дефект, который описывает
backlog 999.1. В 76 остаются замеренные ресурсы, scale-from-zero и выбор
`minReplicaCount`.

**Research-флаги** (из SUMMARY): 71 — исследование целиком (корпус, рамка кадра
VTK, потоки llvmpipe/OCP при cgroup-квоте, реальный RSS); 72 — корпус
реальных экспортёров для снифферов; 73 — замер Landlock-набора (python + C++
стек, `execve` не виден в трассе); 75 — санитизация SVG и покрытие сущностей DXF.
Фазы 74 и 76 идут по готовым шаблонам (sanitize/probe two-output, document
two-ruleset; стандартный KEDA-чарт).

Phase detail for shipped milestones lives in the collapsed sections below and in
`.planning/milestones/`.

## Phase Details

### Phase 71: Evaluation Gate

**Goal**: Выбор ядра геометрии, рендерера и 2D-пути DXF сделан по замеру на
настоящем образе `debian:bookworm-slim` на нативной архитектуре, а не по
прототипу; числа, от которых зависит всё остальное (RSS, время, размер образа,
лимиты входа, лицензии), известны до того, как написана строка продакшен-кода.
**Depends on**: Nothing (первая фаза милстоуна)
**Requirements**: EVAL-01, EVAL-02, EVAL-03, EVAL-04
**Success Criteria** (what must be TRUE):
  1. Решение о ядре (колесо OCP против запасных вариантов), рендерере (VTK +
     OSMesa против f3d и собственного растеризатора) и 2D-пути DXF записано с
     замером на настоящем образе, на нативной архитектуре, под Landlock;
     рендер оценён по пиксельному содержимому на реальных деталях (кадр не
     пуст, деталь вписана, три ракурса различаются), а не по коду возврата
  2. В репозитории есть корпус реальных CAD-файлов (или скрипт загрузки с
     контрольными суммами) с записанной лицензией на каждый файл: STEP-сборки,
     IGES только из поверхностей, большие ASCII и бинарные STL, DXF с хэтчами,
     блоками и размерами, legacy 8-bit (cp1251/Latin-1)
  3. Для корпуса при `--cpus=2` записаны пиковая RSS и время, размер образа и
     cold pull; лимиты входа по форматам выведены из замера с показанным
     выводом, а не скопированы у PreviewGenerator (40 МБ / 5 МБ / 60 с, порог 500 Б)
  4. Лицензии библиотек, которые несёт колесо OCP (`libfreeimage`, `libraw`,
     `libjasper`), проверены для внутреннего использования и решение записано
  5. Продакшен-проводки нет: `AllEngines()`, `/v1/formats`, `/v1/inspect`,
     очереди и чарт не изменились — фаза оставляет только записи решений,
     измерительные скрипты, измерительный Dockerfile и корпус
**Plans:** 8/8 plans complete in 8 waves (последовательно)

Plans:
**Wave 1**
- [x] 71-01-PLAN.md — hash-pinned lock-файлы колёс, legitimacy gate (просмотр пользователем) и выбор исхода D-18

**Wave 2** *(blocked on Wave 1 completion)*
- [x] 71-02-PLAN.md — tracer: одна реальная STEP-деталь через измерительный образ, OCP, VTK, пиксельный судья, RSS при `--cpus=2` и Landlock; детерминированный генератор синтетики

**Wave 3** *(blocked on Wave 2 completion)*
- [x] 71-03-PLAN.md — публичный корпус с лицензиями и PROVENANCE, манифест и загрузчик по sha256, лестница выше 40 МБ, счётчики D-10, приватный каталог

**Wave 4** *(blocked on Wave 3 completion)*
- [x] 71-04-PLAN.md — хелперы: единицы с происхождением, null-объём с причиной, 2D-путь DXF, рендер во всех ракурсах с пиксельными критериями и контактными листами

**Wave 5** *(blocked on Wave 4 completion)*
- [x] 71-05-PLAN.md — f3d как ориентир (эмулированный amd64) и приёмка рендера пользователем по контактным листам (D-11)

**Wave 6** *(blocked on Wave 5 completion)*
- [x] 71-06-PLAN.md — RSS и время до обрыва по форматам и лимитам, оба пина OCP, потоки, тесселяция, RSS Go-воркера, вывод лимита пода и потолков

**Wave 7** *(blocked on Wave 6 completion)*
- [x] 71-07-PLAN.md — трасса библиотек на всех операциях, обрезка VTK по сгенерированному keep-list (D-17), исход D-18 и лицензии колеса OCP, матрица Landlock четырёх хелперов

**Wave 8** *(blocked on Wave 7 completion)*
- [x] 71-08-PLAN.md — размер образа и cold pull, `71-DECISION.md` с проверяемыми ссылками на evidence, гейт отсутствия продакшен-проводки (критерий 5)

### Phase 72: Engine-Free Contract Foundations

**Goal**: Самая рискованная для ревью поверхность — fail-closed валидация
CAD-содержимого и контракт measurements — готова и доказана тестами до того, как
существует хоть один движок. Класс не зарегистрирован и ничего не рекламируется.
**Depends on**: Phase 71 (корпус, закрытые словари единиц/причин/ракурсов, лимиты входа)
**Requirements**: CADIN-01, CADIN-02, CADIN-03, MEAS-05
**Success Criteria** (what must be TRUE):
  1. `POST /v1/inspect` на step/stp/iges/igs/stl/dxf возвращает формат,
     определённый по содержимому; та же функция детекции подключена в цепочку
     создания джобы (проверено тестом на уровне детектора, пока класс не
     зарегистрирован); legacy 8-bit файлы корпуса (cp1251/Latin-1) не отвергаются
     за кодировку
  2. Обрезанные STEP/IGES/DXF, STL, проходящий и как ASCII, и как бинарный, и
     бинарный DXF отвергаются 422 с отдельными стабильными кодами из
     `docs/ERROR-CODES.md`; расхождение сниффера с настоящим ридером (OCP, ezdxf)
     закрыто дифференциальным тестом на корпусе, таблица расхождений закоммичена
  3. `dwg` распознаётся по сигнатуре `AC10xx` и отвергается отдельным кодом
     «dwg не поддерживается», а не как неизвестный формат
  4. JSON measurements имеет версию `schema`; закрытые словари (единицы, коды
     причин) задокументированы и сверены с кодом тестом; валидатор отвергает
     `volume: 0` без замкнутого солида, NaN/Inf и значения вне словаря; роль
     `measurements` принята enum, CHECK-ограничением (миграция) и `handleGetJob`,
     который не выбирает её как `download_url`
  5. Ничего не рекламируется: `AllEngines()` не изменён, `/v1/formats` не
     показывает пар `cad`; алиасы `stp`/`igs` нормализуются (включая копию во
     фронтенде)
**Plans:** 7/7 plans complete in 4 waves (последовательно)

Plans:
**Wave 1**
- [x] 72-01-PLAN.md — tracer: валидный STEP/STP через один CAD-детектор в создании джобы и в inspect; 15 фикстур отказов закоммичены раньше правил, которые их судят
- [x] 72-02-PLAN.md — контракт measurements: `schema` `octoconv.measurements/v1`, закрытые словари, fail-closed валидатор, `docs/MEASUREMENTS.md` с тестом сверки
- [x] 72-03-PLAN.md — роль выхода `measurements`: константа, миграция 0018, тесты на живом Postgres, никогда не `download_url`

**Wave 2** *(blocked on Wave 1 completion)*
- [x] 72-04-PLAN.md — четыре кода 422 (errcode, ERROR-CODES.md, openapi) и полный набор правил STEP/IGES/STL/DXF/DWG на корпусе, фикстурах и мутациях
- [x] 72-05-PLAN.md — алиасы `stp`/`igs` в Go и во фронтенде с тестом сверки; guard-тесты «ничего CAD не рекламируется»

**Wave 3** *(blocked on Wave 2 completion)*
- [x] 72-06-PLAN.md — отказы подключены к созданию джобы (422) и к inspect (sentinel-форматы), привязки причин, тест согласия по каждой фикстуре

**Wave 4** *(blocked on Wave 3 completion)*
- [x] 72-07-PLAN.md — дифференциальный прогон против OCP и ezdxf, закоммиченная таблица расхождений, golden под TestSniffCADGolden; гейт фазы

### Phase 73: CAD Class & Convert Slice, Confined

**Goal**: Класс `cad` появляется в API целиком и сразу под замеренным
Landlock-ruleset: STEP/IGES → STL сквозь очередь, воркер, preflight и
классификацию ошибок. Самый узкий вертикальный срез, который задевает каждую
точку интеграции класса.
**Depends on**: Phase 72
**Requirements**: CADW-01, CADW-02, CADW-03, CADIN-04, CCONV-01
**Success Criteria** (what must be TRUE):
  1. Клиент ставит `convert` step/stp/iges/igs → stl и получает ровно один
     бинарный STL в миллиметрах (STEP, объявивший дюймы, даёт геометрию в мм);
     `cad` появляется в `/v1/formats` и `/v1/inspect` целиком, одним
     атомарным изменением `AllEngines()` вместе с очередью, маршрутом enqueue,
     реконсилером, картами таймаутов/оценок/размеров, compose, Deployment и
     ScaledObject чарта; оценка времени в inspect помечена `unmeasured`, остальные
     девять классов и метки метрик `{engine,status}` / `{queue,state}` не изменились
  2. `cad-worker` отказывается стартовать без Landlock, называя переменную
     аварийного выхода, и до обращения к Postgres доказывает конфайнмент
     preflight'ом на реальном движке, судя по содержимому выхода, а не по коду
     возврата; набор путей стадии mesh записан в `SANDBOX-PATHS.md` и удерживается
     тестом и `conf05-crosscheck.sh`; движок, направленный на файл вне каталога
     задачи, получает отказ
  3. Вход-бомба (самоссылающийся STEP, STL с лживым числом треугольников,
     нечисловые координаты) завершает джобу терминальной ошибкой в замеренных
     границах памяти и времени: воркер жив, OOMKilled нет
  4. Ошибки классифицируются по этапам: недоступность песочницы — повторяемая;
     нечитаемый, пустой, сверх потолка вход и таймаут стадии mesh — терминальные с
     отдельным кодом, без цикла ретраев; убийство воркера посреди джобы не
     оставляет живых процессов движка и не держит джобу до полного таймаута
**Plans:** 6/6 plans complete in 6 waves (последовательно)

Plans:
**Wave 1**
- [x] 73-01-PLAN.md — tracer: units_inch.step → бинарный STL в мм через продакшен-образ, хелпер и настоящий ruleset; спайки бомб на настоящем ридере OCP, правила гарда и таймауты стадий выведены из evidence

**Wave 2** *(blocked on Wave 1 completion)*
- [x] 73-02-PLAN.md — гард-хелпер по правилам спайков, CADConverter с двумя конфайненными стадиями, судья STL, сентинели по стадиям, CADPreflight; ничего не регистрируется

**Wave 3** *(blocked on Wave 2 completion)*
- [x] 73-03-PLAN.md — замер наборов Landlock обеих стадий на продакшен-образе, раздел `## cad` в SANDBOX-PATHS.md, conf05-crosscheck для cad

**Wave 4** *(blocked on Wave 3 completion)*
- [x] 73-04-PLAN.md — решение о кодах ошибок (checkpoint), CADDescriptor, задача `cad:job`, `cmd/cad-worker` в финальном образе, проверки sandbox-liveproof

**Wave 5** *(blocked on Wave 4 completion)*
- [x] 73-05-PLAN.md — потолки по формату (413), затем атомарное включение `cad` одним коммитом: реестры, маршруты, реконсилер, карты, compose, чарт

**Wave 6** *(blocked on Wave 5 completion)*
- [x] 73-06-PLAN.md — живые доказательства на финальном дереве: конфайнмент образа (включая эмулированный amd64), сквозной convert, бомбы без OOM, cancel и убийство воркера

### Phase 74: 3D Render & Measurements

**Goal**: Клиент получает PNG-превью детали и вместе с ним честные размеры; два
выхода одной джобы видны оба или не виден ни один.
**Depends on**: Phase 73
**Requirements**: RNDR-01, RNDR-02, RNDR-03, RNDR-04, RNDR-05, MEAS-01, MEAS-02, MEAS-03, MEAS-04
**Success Criteria** (what must be TRUE):
  1. Клиент ставит `render` step/stp/iges/stl → png и получает непустое
     изображение детали (по содержимому пикселей: кадр не пуст, деталь вписана с
     полем); по умолчанию ракурс `iso`, размер 1280×1280, непрозрачный фон;
     одна деталь с разных ракурсов даёт разные изображения, ось Z вверх
  2. Закрытые опции валидируются: неизвестный ракурс (вне `iso`, `front`, `back`,
     `left`, `right`, `top`, `bottom`), размер вне диапазона или выше потолка и
     неизвестный `quality` — 422; `quality` (`draft`/`standard`/`fine`) меняет
     тесселяцию и для рендера, и для конверсии step/iges → stl (более тонкий
     уровень даёт не меньше треугольников на той же детали);
     `background: transparent` даёт PNG с альфа-каналом, где фон полностью прозрачен
  3. Каждая успешная `render`-джоба отдаёт два выхода: PNG с ролью `primary` и
     JSON с ролью `measurements`; `download_url` указывает на PNG; если любой из
     двух не удалось получить и проверить, джоба падает и клиент не видит ни
     одного — доказано инверсией через запись в репозиторий, а не утверждением
  4. Размеры честны: точный bbox и число солидов; объём и площадь поверхности
     только по замкнутым солидам, иначе `null` с закрытым кодом причины (IGES из
     одних поверхностей, открытый STL), никогда не `0`; единицы по источнику с
     происхождением (STEP/IGES — объявленная в файле, STL — `unspecified`);
     `assume_unit` заполняет только отсутствующую единицу с `source:
     client_assumed` и никогда не переопределяет объявленную (STEP в дюймах с
     `assume_unit=mm` остаётся в дюймах)
  5. Стадия рендера (VTK, без OCP) работает под своим замеренным ruleset,
     записанным в `SANDBOX-PATHS.md` и удерживаемым тестом; preflight судит по
     пикселям; сбой стадии рендера повторяемый, сбой стадии mesh — терминальный
     (расширяет CADW-02, CADW-03)
**Plans**: 7/7 plans complete

Plans:
- [x] 74-01-PLAN.md — Wave 0: vtk legitimacy checkpoint, cad-render in the pruned production image (tracer, prune identity), 2048 px cost, cap, STL ceiling and unit-factor probes
- [x] 74-02-PLAN.md — cad-mesh measures, quality and STL stats; draft to measurements document in the declared unit; invalid_solid for STL
- [x] 74-03-PLAN.md — closed CAD options at API and worker; Go pixel judge with the thin-part rule
- [x] 74-04-PLAN.md — 886 s timeout budget everywhere; CADRenderConverter (unregistered); render failure classification
- [x] 74-05-PLAN.md — Repo.AddOutputs with inversion; atomic two-output process(); render exposed in one commit
- [x] 74-06-PLAN.md — render and mesh Landlock sets measured and crosschecked; judge calibrated against pixelcheck; preflight proves render
- [x] 74-07-PLAN.md — live proof of render and measurements end to end

### Phase 75: DXF 2D Pipeline

**Goal**: Клиент получает векторный чертёж и превью DXF через другой тулчейн
(ezdxf + rsvg-convert) под тем же конфайнментом; выход, собранный из
недоверенного текста, безопасен по построению.
**Depends on**: Phase 74 (переиспользует путь второго выхода `measurements`; половина convert — dxf → svg/pdf — зависит только от 73)
**Requirements**: RNDR-06, CCONV-02, CCONV-03
**Success Criteria** (what must be TRUE):
  1. `dxf → svg` даёт вектор из model space только по видимым слоям (выключенные
     и замороженные отсутствуют), текст не теряется (включая кириллицу из
     cp1251-DXF); SVG — well-formed XML без скриптов, `foreignObject`,
     обработчиков событий и внешних ссылок, в том числе для DXF, чей текст
     содержит `&<>"'` (round-trip тест)
  2. `dxf → pdf` даёт валидный PDF, страница которого содержит чертёж (не пуста,
     текст извлекается)
  3. `render` dxf → png рисует чертёж сверху с теми же правилами размера и фона,
     что и 3D; неквадратный чертёж вписан в кадр без обрезки; опция ракурса для
     `dxf` — 422; вторым выходом идёт `measurements` с 2D bbox из сущностей (не из
     `$EXTMIN/$EXTMAX`), без объёма, единицы по `$INSUNITS` или `unspecified`
     (расширяет MEAS-01, MEAS-03)
  4. Вход-бомба DXF (MINSERT-взрыв, циклические ссылки блоков, нечисловые
     координаты) завершает джобу терминальной ошибкой в замеренных границах без
     OOM; XREF и IMAGE не разрешаются никогда (расширяет CADIN-04)
  5. Стадии ezdxf и `rsvg-convert` работают каждая под своим замеренным ruleset,
     записанным в `SANDBOX-PATHS.md` и удерживаемым тестом; preflight судит по
     содержимому; шрифт доступен под ruleset, текст не пропадает (расширяет CADW-02)
**Plans**: 10/10 plans complete in 9 waves (последовательно)

Plans:
**Wave 1**
- [x] 75-01-PLAN.md — Wave 0: ezdxf lock-subset checkpoint (R-6), tracer cad-dxf + rsvg-convert under Landlock in the production image (text as text), phase fixtures

**Wave 2** *(blocked on Wave 1 completion)*
- [x] 75-02-PLAN.md — cad-dxf complete: skip and count, never resolve, frame and bbox, units draft, texts.json, font failure, backstops, fidelity; real outputs as fixtures

**Wave 3** *(blocked on Wave 2 completion)*
- [x] 75-03-PLAN.md — cad-guard DXF mode; differential vs ezdxf and the unguarded helper; R-8 scope; nesting derivation; guard cost at the ceiling
- [x] 75-04-PLAN.md — Go SVG allowlist with re-serialization, stdlib PDF judge, drawing mode of the pixel judge and the letterbox; three CLIs

**Wave 4** *(blocked on Wave 3 completion)*
- [x] 75-05-PLAN.md — Wave 0 bounds (R-7, R-5): ceiling, insert and entity caps, stage timeouts and output byte ceilings derived from measured rows

**Wave 5** *(blocked on Wave 4 completion)*
- [x] 75-06-PLAN.md — dxf option contract and measurements branch; pinned constants, sentinels, classification; job-local HOME stages; CAD_DXF_TIMEOUT and CAD_RSVG_TIMEOUT

**Wave 6** *(blocked on Wave 5 completion)*
- [x] 75-07-PLAN.md — CADDXFConverter and CADDXFRenderConverter (unregistered); worker mapping and ERROR-CODES

**Wave 7** *(blocked on Wave 6 completion)*
- [x] 75-08-PLAN.md — cad-dxf and rsvg-convert Landlock sets measured and crosschecked; drawing judge calibrated against pixelcheck; preflight proves both stages

**Wave 8** *(blocked on Wave 7 completion)*
- [x] 75-09-PLAN.md — DXF pairs exposed in one commit; HTTP option, ceiling and inspect tests; worker two outputs

**Wave 9** *(blocked on Wave 8 completion)*
- [x] 75-10-PLAN.md — live proof of the DXF pipeline end to end with the 74 regression

### Phase 76: Measured Bounds, KEDA Proof & Client Docs

**Goal**: Границы класса измерены, а не скопированы; класс доказан вживую на
образах из финального коммита; контракт превью и клиентская документация
записаны и привязаны к коду.
**Depends on**: Phase 74, Phase 75 (таймаут осмыслен, только когда существуют обе операции)
**Requirements**: CADW-04, CADW-05, CADW-06, PREV-01
**Success Criteria** (what must be TRUE):
  1. `CAD_ENGINE_TIMEOUT` измерен по обеим операциям (convert и render, включая
     2D) на реальном корпусе на потолке входа; значение — максимум по операциям и
     спарено с потолком размера входа, формула и худшая ячейка записаны; оценка
     времени в inspect помечена как замеренная
  2. Scale-from-zero `cad` через KEDA доказан на OrbStack k8s (0→N→0 с
     таймстампами) вместе с downscale-survival; `keda.cad.minReplicaCount` выбран
     осознанно, с записанным замером размера образа и cold pull
  3. Живой гейт всех классов, включая `cad`, завершается `ALL PASS` на образах,
     собранных из финального коммита; убийство воркера посреди render оставляет
     ноль живых процессов; повторный прогон того же входа на той же архитектуре
     даёт тот же JSON, а PNG сравнивается по допуску, а не по хешу
  4. Клиентская документация — `docs/CAD.md`, коды ошибок, `docs/openapi.yaml`,
     MCP-описания и таблица форматов референсного фронтенда — сверена с кодом
     тестом, как это уже сделано для `docs/ERROR-CODES.md`
  5. Контракт превью (SEED-007) записан и закреплён на CAD: формат, размеры,
     фон, поведение на непревьюируемом входе; всё, что CAD не заставил решить,
     явно помечено «не зафиксировано»
**Plans**: 11 plans in 11 waves (последовательно)

Plans:
**Wave 1**
- [x] 76-01-PLAN.md — tracer and premise test: pre-registered stacking probe on the production engine image; per-format byte_bound decision and the render-deadline rule (AR-1)

**Wave 2** *(blocked on Wave 1 completion)*
- [x] 76-02-PLAN.md — bounds76: exact-ceiling cells, N=5 stage rows at the admissible maxima, the L170031 refusal row

**Wave 3** *(blocked on Wave 2 completion)*
- [x] 76-03-PLAN.md — in-worker rows (I/O, Go peak, attempt wall) and the derivation of every bound into 76-03-derived.txt

**Wave 4** *(blocked on Wave 3 completion)*
- [x] 76-04-PLAN.md — measured constants pinned to the evidence, IN-02 and grace chain, inspect estimate flipped to measured

**Wave 5** *(blocked on Wave 4 completion)*
- [x] 76-05-PLAN.md — Path-B operation-aware render ceiling where the evidence fired it, else nothing changes

**Wave 6** *(blocked on Wave 5 completion)*
- [x] 76-06-PLAN.md — D-07 PDF backstop code, docs/CAD.md and docs/PREVIEW.md held to the code both ways, SEED-007 pointer

**Wave 7** *(blocked on Wave 6 completion)*
- [ ] 76-07-PLAN.md — openapi CADOpts and codes, MCP descriptions against the registry, frontend operation table with vitest

**Wave 8** *(blocked on Wave 7 completion)*
- [ ] 76-08-PLAN.md — cad-liveproof: kill mid-render and mid-PNG-rsvg, determinism by tolerance, guard cap edge, stacked refusal, measured inspect; official run

**Wave 9** *(blocked on Wave 8 completion)*
- [ ] 76-09-PLAN.md — production image size, cold pull and preflight; minReplicaCount 0 recorded; KEDA 0->2->0 loadproof; docs/CAD.md Scaling

**Wave 10** *(blocked on Wave 9 completion)*
- [ ] 76-10-PLAN.md — KEDA downscale-survival of a long render; k8s restored to its pre-phase state

**Wave 11** *(blocked on Wave 10 completion)*
- [ ] 76-11-PLAN.md — ci-local e2e knobs and the all-classes final gate from a fresh clone at the final commit, ALL PASS

## Progress

**Execution Order:** фазы идут в числовом порядке: 71 → 72 → 73 → 74 → 75 → 76

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 71. Evaluation Gate | 8/8 | Complete    | 2026-10-04 |
| 72. Engine-Free Contract Foundations | 7/7 | Complete    | 2026-10-05 |
| 73. CAD Class & Convert Slice, Confined | 6/6 | Complete    | 2026-10-05 |
| 74. 3D Render & Measurements | 7/7 | Complete    | 2026-10-06 |
| 75. DXF 2D Pipeline | 10/10 | Complete    | 2026-10-12 |
| 76. Measured Bounds, KEDA Proof & Client Docs | 6/11 | In Progress | - |

Предыдущий милстоун — **v1.15 Engine Confinement** — закрыт 2026-10-01: все
восемь классов движков работают под замеренным Landlock-ruleset; живой гейт
восьми классов — `ALL PASS (40 checks)`. Открытые follow-up — в
`.planning/todos/pending/`.

<details>
<summary>✅ v1.15 Engine Confinement (Phases 67–70) — SHIPPED 2026-10-01</summary>

- [x] Phase 67: Image Confinement & Shared Sandbox — 2026-09-23, PR #28
- [x] Phase 68: Four Straightforward Engine Classes (6/6 plans) — 2026-09-24
- [x] Phase 69: HTML, Probe & the /tmp Decision (5/5 plans) — 2026-09-29
- [x] Phase 70: Document on amd64 (9/9 plans, incl. 4 gap closure) — 2026-10-01

Full detail: `.planning/milestones/v1.15-ROADMAP.md` · phases archived to
`.planning/milestones/v1.15-phases/`

</details>

### Шипнуто вне милстоуна (2026-09-19)

Две фазы выполнены и влиты, когда ни один милстоун не был открыт. Записаны
здесь как есть: придумывать им милстоун задним числом значило бы подогнать
историю под форму документа.

| Фаза | Что | PR | Влито |
|---|---|---|---|
| 65 | CPU-бюджет движка TeX (`TECTONIC_ENGINE_CPU_SECONDS`, `RLIMIT_CPU`) — граница на то, чего документ СТОИТ, а не сколько он занимает часы | [#22](https://github.com/paderinandrey/octoconv/pull/22) `5579e59` | 2026-09-19 |
| 66 | Observability: `job_id` сквозь логи, `Correlation-Id` от вызывающего, запись, соединяющая request id с job_id (OBS-01..04) | [#23](https://github.com/paderinandrey/octoconv/pull/23) `b95b38d` | 2026-09-19 |

У фазы 66 планировочные артефакты лежат в
`.planning/phases/66-observability-correlation/`. **У фазы 65 их нет вовсе** —
ни CONTEXT, ни PLAN, ни SUMMARY: работа шла по ветке `phase-65-cpu-budget` без
них. Это записано как факт, а не замолчано: единственный след решений фазы —
комментарии в коде, замеры в `.env.example` и история ревью PR #22.

Тем же днём влит [#24](https://github.com/paderinandrey/octoconv/pull/24)
(`ddc150c`) — переписанный README и контрактные тесты, держащие
`docs/openapi.yaml` равным маршрутам живого роутера.

<details>
<summary>✅ v1.14 Typesetting &amp; Debt (Phases 62–64) — SHIPPED 2026-09-05</summary>

- [x] Phase 62: Operational Debt Tail (1/1 plans) — 2026-09-02, PR #13
- [x] Phase 63: Sanitize Hardening & Probe Page Ceiling (2/2 plans) — 2026-09-03, PRs #14, #15
- [x] Phase 64: Tectonic — Ninth Engine Class (5/5 plans) — 2026-09-05, PR #16

Full detail: `.planning/milestones/v1.14-ROADMAP.md` · phases archived to
`.planning/milestones/v1.14-phases/`

</details>

<details>
<summary>✅ v1.13 Inspect &amp; Operations (Phases 56–61) — SHIPPED 2026-09-02</summary>

- [x] Phase 56: JobRunner Consolidation & Structured Error Codes (7/7 plans) — 2026-08-08
- [x] Phase 57: Operation Dimension & Output Roles (7/7 plans) — 2026-08-09
- [x] Phase 58: Synchronous Inspection (8/8 plans) — 2026-08-18
- [x] Phase 59: Born-Digital PDF Routing (7/7 plans) — 2026-08-30
- [x] Phase 60: Sanitize — Eighth Engine Class (6/6 plans) — 2026-09-02
- [x] Phase 61: v1.12 Debt Tail (3/3 plans) — 2026-08-30

Full detail: `.planning/milestones/v1.13-ROADMAP.md` · phases archived to
`.planning/milestones/v1.13-phases/`

</details>

<details>
<summary>✅ v1.0 – v1.12 (Phases 1–55) — archived</summary>

Each milestone's full phase detail and requirements live in
`.planning/milestones/v{X.Y}-ROADMAP.md` and `-REQUIREMENTS.md`.

</details>

## Carried forward, not in this milestone

- **SEED-018 job pipelines** — `md → pdf` через pandoc→tectonic одной джобой.
  Первая джоба, пересекающая границу движка.
- Спящие семена в `.planning/seeds/`, включая другие engine-классы
  (archive, preview, tabular), чьи триггеры формально сработали на
  «милстоун добавляет новый класс» и которые сознательно не взяты. SEED-005
  (CAD) взят в v1.16; SEED-007 (контракт превью) — только на CAD (PREV-01),
  остальные классы — PREVV2-01.

## Backlog

### Phase 999.1: Per-Deployment Worker Selection (BACKLOG)

**Goal:** Каждая установка поднимает только нужный ей набор engine-классов, а API
честно отказывает в классах, которых в установке нет. Сейчас в чарте ни у
одного воркера нет `enabled` — любая установка поднимает все девять.

Решено вместо выноса воркеров в отдельные репозитории (обсуждено 2026-09-23):
воркеры и так собираются раздельно (свой `cmd/*-worker`, Dockerfile,
deployment), а общий контракт (`jobs.Repo` transitions, миграции, asynq payload,
S3-ключи, коды ошибок, `Converter`, `sandbox`) живёт в `internal/` и при
раздельных репо потребовал бы версионируемого SDK и дал бы version skew в
рантайме. Пересмотреть, если движками начнут владеть разные команды или
разойдутся релизные циклы.

Состав:

1. `<name>Worker.enabled` в `deploy/chart/octoconv/values.yaml` для каждого
   воркера (включая KEDA `scaledobject-*`) + готовые наборы values; `profiles:`
   в `docker-compose.yml`
2. **Главное:** API отказывает (явный 4xx с кодом ошибки) на пару форматов,
   чей engine-класс не развёрнут, вместо вечного `queued`. Сейчас валидация
   идёт по `convert.Default`, где зарегистрированы все классы. Источник правды —
   один список включённых классов (напр. `ENABLED_ENGINES`), выводимый из тех же
   values, чтобы чарт и API не могли разойтись. `/formats`, inspect и MCP
   отдают только включённые классы
3. Опционально: CI собирает/тестирует только затронутые воркеры по путям

**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)
