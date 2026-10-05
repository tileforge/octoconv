# Phase 71: Evaluation Gate - Research

**Researched:** 2026-10-03
**Domain:** замер CAD-стека (OCP + VTK/OSMesa + ezdxf/rsvg) на `debian:bookworm-slim`, native arm64, под Landlock; корпус, бюджет памяти, лицензии
**Confidence:** MEDIUM-HIGH (решающие факты о колёсах, импорте, рендере и лицензионной цепочке проверены в этой сессии на bookworm/arm64; корпус, кривая RSS и размеры образа ещё не измерены, это и есть работа фазы)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Корпус**
- **D-01:** Два источника. В репозиторий идут только публичные файлы с открытыми лицензиями. Реальные файлы компании (поток PreviewGenerator) замеряются локально и не коммитятся: в evidence попадают только числа (размер, RSS, время, счётчики) и sha256, без имён клиентов и содержимого.
- **D-02:** Файлы компании лежат в каталоге вне репо, путь задаётся env-переменной (имя выбирает планировщик). Пользователь кладёт файлы туда сам. Пустой или отсутствующий каталог фазу **не блокирует**: замер идёт по публичному корпусу, а в записи решения явно стоит «реальный поток не замерен». Не «чисто», а «не проверялось» (память: optimistic answers for unexamined input).
- **D-03:** Хранение. Малые файлы коммитятся в testdata, рядом лежит `PROVENANCE.md` (источник, автор, лицензия, sha256 на каждый файл). Большие файлы (STL на десятки МБ, большие STEP) качает скрипт из `scripts/` по URL с закреплённым sha256 в кэш вне репо. Несовпадение хеша — ошибка, а не предупреждение.
- **D-04:** Для коммитящихся файлов допустимы только PD / CC0 / CC-BY / MIT / BSD. CC-BY-SA и NC не берём. Атрибуция CC-BY записывается в `PROVENANCE.md`.
- **D-05:** Замер по размеру идёт **выше** потолка PreviewGenerator (40 МБ) — до 100–200 МБ STL/STEP или до обрыва по памяти или времени. Лимит должен стоять с видимым запасом до замеренного обрыва, а не там, где кончился корпус (память: tectonic measure-at-bound).
- Синтетические файлы-уловки, которых нет в открытом доступе (cp1251/Latin-1 DXF и STEP, IGES только из поверхностей, если не нашлось реального), делает детерминированный генератор. Он коммитится первым, до кода, который ими проверяется (протокол `scripts/sanitize-corpus.py`): без временных меток и случайности, с самопроверкой маркеров и ненулевым выходом при пропаже маркера.

**Бюджет памяти и лимиты входа**
- **D-06:** Memory limit пода `cad` выбирается **по замеру**: сначала кривая «пиковая RSS от размера входа» по каждому формату, затем наименьший из 1 / 2 / 4 GiB, при котором полезный потолок входа укладывается с запасом. Потолок кандидатов — 4 GiB. Во флоте у всех 1 GiB, у одного 2 GiB (`deploy/chart/octoconv/values.yaml`). Отклонение от этого допустимо, но должно быть записано с выводом.
- **D-07:** Запас: пиковая RSS **худшего** файла корпуса на потолке входа вместе с Go-воркером ≤ 70% limit пода. Множитель RSS/МиБ берётся по худшему файлу формата, не по среднему. Измеряется при `--cpus=2` и с limit пода, выставленным на контейнер (а не на хосте без лимита).
- **D-08:** Лимиты входа считаются **отдельно по формату**: STEP/IGES, STL (бинарный и ASCII отдельно, если множители заметно различаются), DXF. Вывод каждого числа показан в записи решения.
- **D-09:** Если даже при 4 GiB потолок STEP выходит ниже 40 МБ, **принимаем замеренный потолок**. В записи решения указывается, какая доля реального потока (если файлы компании есть) в него не входит. Большие файлы получают честный 413/422, а не OOM-килл.
- **D-10:** Фаза 71 также замеряет операционные счётчики на корпусе (число треугольников, сущностей STEP, вхождений NAUO/инстансов, вставок блоков DXF) и предлагает капы-**кандидаты** с выводом. Внедряет их фаза 73 (гард-стадия, CADIN-04) и фаза 75 для DXF. Байтовый потолок сам по себе от бомб не защищает.

**Планка качества рендера**
- **D-11:** Приёмка двухступенчатая. (1) Скрипт проверяет пиксельные критерии: кадр не пуст, деталь вписана с полем и не обрезана, три ракурса различаются. Код возврата не используется как критерий. (2) Чекпоинт `checkpoint:human-verify`: контактный лист (сетка «деталь × ракурс») на реальных деталях корпуса, пользователь смотрит и одобряет. Без одобрения выбор рендерера не записывается.
- **D-12:** Стиль: нейтральная светло-серая заливка, тёмные контурные рёбра (feature edges), сглаживание. Подбор света и FXAA — на усмотрение исполнителя, но в пределах этого стиля.
- **D-13:** Фон по умолчанию — ровный белый `#FFFFFF`, без градиента. Тот же белый используется для letterbox в DXF. Прозрачный фон — опция (RNDR-05, фаза 74).
- **D-14:** Проекция ортографическая для всех ракурсов, включая `iso`. Деталь вписывается в кадр по bbox, а не через `ResetCamera()`. Z вверх.

**Пороги отката и лицензии**
- **D-15:** Жёсткого потолка на размер образа нет. Размер и cold pull замеряются и записываются; сам по себе размер выбор рендерера не меняет. Цена большого образа учитывается при выборе `keda.cad.minReplicaCount` в фазе 76.
- **D-16:** С VTK + OSMesa уходим на запасной рендерер **только при функциональном провале**: не рендерит под Landlock на bookworm, не проходит пиксельные критерии или приёмку пользователя (D-11), недетерминирован в пределах одной архитектуры, не укладывается по времени при `--cpus=2`. f3d используется только для сравнения (только amd64, на нативном arm64 его не запустить). Собственный растеризатор — только если VTK провалился.
- **D-17:** VTK обрезается по keep-list (566 → ~290 МБ). Keep-list генерирует скрипт, а не пишется руками. Фаза 71 доказывает, что PNG с обрезкой и без совпадают на корпусе (одна архитектура, одна сборка). Рендер-тест должен падать, если после обновления VTK keep-list устарел.
- **D-18 (EVAL-04):** Сначала замеряем, загружает ли наш путь вообще `libfreeimage`, `libraw`, `libjasper` (trace/`ldd`/`LD_DEBUG` на всех операциях). Неиспользуемые удаляются из образа, и тест доказывает, что рендер и конвертация без них работают. На то, что осталось, пишется инженерная запись: лицензии, «только внутреннее использование, образ не распространяется вовне», плюс флаг «нужен OK юриста», который фазу **не блокирует**.

**Перенесено из прошлых фаз (не переобсуждалось)**
- «Нативная архитектура» = arm64 на OrbStack. Гейта на amd64 нет, выбор amd64-хоста остаётся за пользователем (фаза 70, D-03/D-05). Эмулированный amd64 не даёт Landlock: такой замер считается за отсутствие замера.
- FreeCAD, Xvfb и f3d в продакшен-образ не идут (REQUIREMENTS Out of Scope, research SUMMARY).
- Корпус, который читают Go-тесты, лежит в `internal/<pkg>/testdata/` с `PROVENANCE.md` рядом. Фикстуры для shell-гейтов лежат в `scripts/fixtures/` с `README.md` (`scripts/sanitize-corpus.py`).
- Гейты гоняются локально, не в GitHub CI (память: local gates).

### Claude's Discretion
- Пин OCP: 7.9.3.1.1 или 8.0.1.0.0. Решается замером на одном корпусе, код хелпера использует `CornerMin()/CornerMax()`, чтобы работать на обеих.
- Величина поля вокруг детали; константы тесселяции для `draft`/`standard`/`fine` (относительно диагонали bbox) выводятся из замера времени, RSS и визуального качества.
- Закрытые словари, которые нужны фазе 72: единицы (с происхождением), коды причин `null`-объёма, ракурсы. Ракурсы зафиксированы в RNDR-02; единицы и причины предлагаются по тому, что реально встретилось в корпусе, и записываются в решение как черновик контракта (окончательно закрепляет MEAS-05 в фазе 72).
- `LP_NUM_THREADS` и потоки мешера OCP при `--cpus=2`: исполнитель замеряет и выбирает.
- Где лежит запись решения (например, `71-DECISION.md` в каталоге фазы плюс `evidence/`), имена скриптов (`scripts/cad-*-measure.sh`), имя измерительного Dockerfile (`Dockerfile.cad-measure`), имя env-переменной для приватного корпуса, путь кэша для больших файлов.
- Кандидатные наборы путей Landlock для четырёх хелперов из трассировки `scripts/sandbox-paths-measure.sh`. Это гипотезы для фазы 73, а не окончательные гранты.

### Deferred Ideas (OUT OF SCOPE)
None — discussion stayed within phase scope.
(Reviewed todos, not folded: `2026-09-29-amd64-first-deployment-preflight-check.md` — `cad` добавится в список, когда появится класс (фаза 73+); три todo про `/proc`, soffice-сокет и sweep-тесты совпали только по слову «phase» и к фазе 71 не относятся.)
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| EVAL-01 | Выбор ядра, рендерера и 2D-пути DXF записан по замеру на настоящем образе bookworm, native, под Landlock, с оценкой по пикселям | §Pre-verified (колёса, импорт, OSMesa-рендер на bookworm/arm64 как `nobody`, Landlock доступен на этом ядре); §Рубрика решений; §Пиксельные критерии; §Матрица Landlock |
| EVAL-02 | Корпус реальных CAD-файлов с лицензиями (STEP-сборки, IGES-поверхности, большие ASCII/бинарные STL, DXF с хэтчами/блоками/размерами, legacy 8-bit) | §Корпус: проверенные источники и лицензии (FreeCAD-library CC-BY 3.0, NASA-3D-Resources, NIST, ezdxf MIT), отвергнутые источники, генератор синтетики |
| EVAL-03 | Пиковая RSS и время при `--cpus=2`, размер образа и cold pull; лимиты входа выведены из замера | §Метод замера RSS/времени (контейнер на файл, `memory.peak` + `ru_maxrss`), формула D-07, §Образ и cold pull, §Потоки |
| EVAL-04 | Лицензии библиотек колеса OCP проверены, решение записано | §EVAL-04: цепочка `libTKService -> libfreeimage -> libraw (-> libjasper)` проверена; удаление файла ломает `import OCP`; лицензии |
</phase_requirements>

## Summary

Стек из STACK.md подтверждён заново в этой сессии на настоящем `debian:bookworm-slim` (aarch64, glibc 2.36, Python 3.11.2): колесо `cadquery-ocp-novtk` 7.9.3.1.1 и 8.0.1.0.0 ставятся (`--no-deps` + крошечный `cadquery-ocp-proxy` той же версии), читают STEP, дают объём `858.6283305884542` и bbox, мешат и пишут STL; `vtk` 9.7.1 с `libosmesa6` рендерит PNG 1280x1280 через `vtkOSOpenGLRenderWindow` под `nobody`, `env -i`, `--cpus=2`. Landlock на этом ядре OrbStack работает (probe установил ruleset, канарейка отказана, `echo` выполнен). Прототипного риска «не запускается» нет; фаза про числа и решения, и три вещи из CONTEXT надо скорректировать до планирования.

**Три находки, которые меняют план.** (1) D-18 «неиспользуемые библиотеки удаляются из образа» напрямую невыполнимо: `OCP.cpython-311-*.so` имеет `DT_NEEDED` на `libTKService`, тот на `libfreeimage`, тот на `libraw` (в 8.0.1 ещё на `libjasper`); при импорте `libfreeimage` и `libraw` оказываются в `/proc/self/maps`, а удаление файла даёт `ImportError: libfreeimage-3-...so: cannot open shared object file`. Выполнимы только запись лицензий или правка вендорного бинарника (`patchelf --remove-needed`); решение за планировщиком/пользователем. (2) Набор лицензий зависит от пина: 7.9.3.1.1 несёт `libfreeimage` и `libraw` без `libjasper`, 8.0.1.0.0 добавляет `libjasper` (проверено по файлам колеса и по `/proc/self/maps`); это довод в пользу 7.9.3.1.1 по умолчанию. (3) `numpy` 2.5.x требует Python >=3.12, на bookworm 3.11.2 пин `numpy<=2.4.6`; генерировать lock-файл надо на 3.11 внутри bookworm-контейнера, а не на хосте.

Корпусная часть: проверенные открытые источники есть для реальных STEP (FreeCAD-library, CC-BY 3.0, ~2900 STEP, до 52 МБ, без LFS), больших бинарных STL (NASA-3D-Resources, 342 STL, до 52 МБ) и DXF (репозиторий ezdxf, MIT, 103 DXF, до 1,8 МБ). Выше ~52 МБ реальных открытых файлов нет: 100-200 МБ получаются только детерминированным масштабированием (копии реальной детали; NAUO-инстансинг отдельно), и это надо назвать в записи решения как синтетику. Реальных файлов с cp1251 и IGES только из поверхностей в открытом доступе не найдено: их делает генератор (D-05, протокол `sanitize-corpus.py`), при этом попытка получить «дюймовый» STEP через `Interface_Static "write.step.unit"="IN"` дала миллиметровый заголовок, так что генератор должен писать заголовок сам.

**Primary recommendation:** Планировать как конвейер «генератор и корпус (первый коммит) -> измерительный образ с хеш-пинами на bookworm -> замер на файл в контейнере с `--cpus=2 --memory=N` (`memory.peak` + `ru_maxrss`) -> пиксельные критерии + контактный лист на чекпоинте -> матрица Landlock через `sandbox-paths-measure.sh` -> `71-DECISION.md` + `evidence/`»; пин OCP по умолчанию 7.9.3.1.1, 8.0.1.0.0 измерять как кандидата; D-18 переформулировать в «замерить загрузку, попробовать `patchelf`-вариант как ограниченный эксперимент, записать лицензии при любом исходе».

## Architectural Responsibility Map

Фаза не добавляет продакшен-возможностей; «уровни» здесь — где живёт измерение, чтобы задачи не смешивали хост и контейнер.

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Генерация синтетического корпуса | Хост (python3 + OCP/ezdxf в измерительном образе) | — | Детерминизм; для STEP/IGES нужен OCP, значит запускается в образе, а не на macOS |
| Загрузка больших файлов с sha256 | Хост (`scripts/`, кэш вне репо) | — | D-03: несовпадение хеша = ошибка |
| Замер RSS/времени/OOM | Контейнер `--cpus=2 --memory=N`, один на файл | Хост (агрегация в CSV/evidence) | Только cgroup контейнера даёт честный потолок; `memory.peak` кумулятивен на контейнер |
| Рендер и пиксельные критерии | Контейнер (VTK/OSMesa, `nobody`) | Хост-скрипт на Pillow/numpy из того же образа | Критерии судят PNG, а не код возврата |
| Контактный лист | Контейнер (Pillow из измерительного образа) | Хост: просмотр человеком | На macOS-хосте Pillow не гарантирован |
| Трасса и проверка путей Landlock | `scripts/sandbox-paths-measure.sh` + `scripts/cmd/sandbox-probe` | — | Готовый инструмент; образ должен быть native |
| Размер образа, cold pull | Docker daemon (OrbStack) + локальный `registry:2` | — | Реестра компании нет; см. Pitfall «cold pull на loopback» |
| Запись решения | `.planning/phases/71-evaluation-gate/71-DECISION.md` + `evidence/*.txt` | — | Уже принятый прецедент evidence; `.planning/` в `.gitignore`, файлы форс-добавлены (`git ls-files .planning` = 1150) |

## Standard Stack

Версии проверены по PyPI JSON 2026-10-03 [VERIFIED: pypi.org JSON API]; импортируемость на bookworm проверена запуском (см. Pre-verified).

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `cadquery-ocp-novtk` | **7.9.3.1.1** (2026-05-28; cp311 `manylinux_2_31_aarch64` 62 178 457 B sha256 `0b566f9e00f0e4255996b2bcb18b580dc758f451fd2b7546542fae08261db535`) | STEP/IGES, объём, bbox, мешинг, STL | Пин по умолчанию: меньше набор лицензий (нет `libjasper`) |
| `cadquery-ocp-novtk` | **8.0.1.0.0** (2026-09-05; cp311 `manylinux_2_28_aarch64` 61 262 067 B sha256 `90fb7182fb322b81f8b7fe3934dc3a780cfb58c2e5f02eb6ac1628b0f762c461`) | Кандидат для сравнения | Новее; добавляет `libjasper`, `libbz2`, `Imath`; `requires_python <3.15,>=3.11` |
| `cadquery-ocp-proxy` | равен версии OCP (7.9.3.1.1 / 8.0.1.0.0), 3-3,3 КБ | Обязательная зависимость `Requires-Dist` колеса | Без неё `--require-hashes` по полному набору не сойдётся; в STACK.md не упомянута |
| `vtk` | **9.7.1** (2026-09-28; cp311 `manylinux_2_28_aarch64` 123 МБ) | Offscreen-рендер | Колесо с рантайм-откатом на OSMesa; ставить `--no-deps` (декларирует `matplotlib>=2.0.0`) |
| `ezdxf` | **1.4.4** (2026-05-14; MIT; `requires_python >=3.10`; cp311 aarch64+x86_64) | DXF, 2D bbox, SVG-бэкенд | Зависимости `pyparsing>=3.0.0`, `typing_extensions>=4.6.0`, `numpy`, `fonttools`; `Pillow` нужен `drawing`-аддону вручную |
| Python (Debian) | 3.11.2 (проверено в контейнере) | Интерпретатор хелперов | Единственный Python на bookworm |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `numpy` | **<=2.4.6** (2.5.x требует Python >=3.12; 2.4.6 имеет cp311 aarch64 колесо) | зависимость ezdxf, пиксельный анализ | Пиновать явно, генерируя lock на 3.11 |
| `pillow` | 12.3.0 (cp311 aarch64+x86_64) | контактный лист, пиксельные критерии, `ezdxf.addons.drawing` | Измерительный образ |
| `fonttools` | 4.66.1; `pyparsing` 3.3.3; `typing_extensions` 4.16.0 | зависимости ezdxf | По хешу |
| `trimesh` | 5.1.1 (2026-10-02, MIT; STACK.md называл 5.1.0) | статистика STL-источника | Только если нужен для STL; иначе numpy signed-tetra |
| `libosmesa6`, `libgl1`, `libx11-6`, `librsvg2-bin`, `fonts-dejavu-core` (apt, bookworm) | mesa 22.3.6 / llvmpipe LLVM 15.0.6 (проверено), librsvg 2.54.7 [CITED: STACK.md] | GL-софт, загрузка OCP, DXF -> PDF/PNG | Как в STACK.md |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| VTK + OSMesa | f3d | Нет Linux-arm64 сборки: в релизе v3.5.0 (2026-04-05) для Linux только `x86_64` [VERIFIED: GitHub releases API f3d-app/f3d]; на нативном arm64 не запустить, значит только yardstick на amd64 и без Landlock |
| VTK + OSMesa | свой растеризатор (numpy/Go) | Только при функциональном провале VTK (D-16) |
| `patchelf`-правка `libTKService` | оставить как есть + запись лицензий | См. §EVAL-04 |

**Installation (измерительный образ; финальные хеши получить `pip-compile --generate-hashes` внутри bookworm 3.11):**
```bash
pip install --require-hashes -r cad-requirements.txt
pip install --no-deps --require-hashes vtk==9.7.1
```

**Version verification:** выполнено `curl https://pypi.org/pypi/<pkg>/json` для всех пакетов выше; cp311+aarch64 колёса существуют для OCP (оба пина), vtk, pillow, fonttools, numpy<=2.4.6, ezdxf. `trimesh`, `pyparsing`, `typing_extensions` — pure-Python `py3-none-any`.

## Package Legitimacy Audit

Seam `gsd_run query package-legitimacy check --ecosystem pypi ...` выполнен 2026-10-03.

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| cadquery-ocp-novtk | PyPI | серия с 2024-12 | не получены seam'ом | github.com/CadQuery/OCP (по README колеса) | SUS (too-new, unknown-downloads, no-repository) | Flagged — см. примечание |
| cadquery-ocp-proxy | PyPI | 2026-09-05 | n/a | n/a | SUS | Flagged |
| vtk | PyPI | давний, vtk.org | n/a | vtk.org | SUS (too-new = свежий релиз 9.7.1) | Flagged |
| ezdxf | PyPI | давний | n/a | github.com/mozman/ezdxf | SUS (unknown-downloads, no-repository) | Flagged |
| trimesh, pillow, numpy, fonttools, pyparsing, typing_extensions | PyPI | давние | n/a | известные | SUS (unknown-downloads / too-new последнего релиза) | Flagged |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** все перечисленные. Все вердикты SUS вызваны отсутствием у PyPI-провайдера seam'а счётчиков загрузок (`weeklyDownloads: null`) и свежестью последнего релиза, а не признаками подмены; все имена — давно существующие проекты, которые STACK.md уже использовал в прототипе. Ни один пакет не имеет `postinstall` (`postinstall: null`). Вместо чекпоинта на каждый пакет планировщику: **один** `checkpoint:human-verify` на сгенерированный `cad-requirements.txt` (имена, версии, хеши) до первой сборки образа. Имена пакетов, пришедшие из STACK.md (прототип этой же команды), отмечены `[ASSUMED]` до этого просмотра; существование и хеши колёс подтверждены по PyPI JSON.

## Pre-verified in this session (запуск, а не рассуждение)

Все команды — `docker run` на `debian:bookworm-slim` (aarch64, OrbStack, 11 CPU, 15,66 GiB), если не указано иное.

| # | Факт | Результат | Источник |
|---|------|-----------|----------|
| V1 | Платформа | `Python 3.11.2`, `Debian GNU/Linux 12 (bookworm)`, `aarch64`, `ldd (Debian GLIBC 2.36-9+deb12u14) 2.36` | [VERIFIED: docker run] |
| V2 | OCP 7.9.3.1.1 ставится на bookworm и импортируется | `OCP imported /opt/venv/lib/python3.11/site-packages/OCP/__init__.py`; нужны `libgl1`, `libx11-6` | [VERIFIED: docker run] |
| V3 | Что отображено в память при `import OCP` (7.9.3.1.1) | `freeimage MAPPED`, `libraw MAPPED`, `jasper not mapped`, `libGL MAPPED`, `libX11 MAPPED`, `TKService MAPPED`, `TKOpenGl MAPPED` | [VERIFIED: /proc/self/maps в docker run] |
| V4 | Те же для 8.0.1.0.0 | `freeimage MAPPED`, `libraw MAPPED`, `jasper MAPPED` | [VERIFIED: docker run] |
| V5 | Удаление `libfreeimage` из колеса 7.9.3.1.1 | `ImportError: libfreeimage-3-4329e704.18.0.so: cannot open shared object file: No such file or directory` | [VERIFIED: docker run] |
| V6 | Цепочка `DT_NEEDED` (pyelftools по файлам колёс) | 7.9.3.1.1: `OCP*.so -> libTKService -> libfreeimage-3-4329e704.18.0.so -> libraw-985e3cba.so.19.0.2`; `libTKOpenGl -> libGL.so.1, libX11.so.6`. 8.0.1.0.0: то же и `libraw-d8c07843.so.19.0.2 -> libjasper-d8a83ae9.so.4.0.0`. `DT_FLAGS`/`BIND_NOW` отсутствуют (ленивое связывание) | [VERIFIED: pyelftools по распакованным колёсам] |
| V7 | `OCP` требует `libTKService` напрямую | `OCP.cpython-311-aarch64-linux-gnu.so` NEEDED содержит `libTKOpenGl`, `libTKV3d`, `libTKService` (7.9.3.1.1 и 8.0.1.0.0) | [VERIFIED: pyelftools] |
| V8 | SBOM колёс | 7.9.3.1.1 собрано на Ubuntu 20.04 (`libfreeimage3 3.18.0+ds2-1ubuntu3.1`, `libraw19 0.19.5-1ubuntu1.4`, без jasper); 8.0.1.0.0 на AlmaLinux 8 (`freeimage 3.17.0-5.el8`, `LibRaw 0.19.5-6.el8_10`, `jasper-libs 2.0.14-6.el8_10`). Поле `licenses` в SBOM пусто (`None`) | [VERIFIED: auditwheel.cdx.json] |
| V9 | STEP -> объём/bbox/STL на bookworm как `nobody`, `env -i` | `volume 858.6283305884542`, `CornerMin 0.0`, `CornerMax 20.0`, `stl True`; `Bnd_Box.CornerMin/CornerMax` работают на 7.9.3 и 8.0.1 (`corner 495.0` в обоих) | [VERIFIED: docker run] |
| V10 | VTK 9.7.1 OSMesa-рендер как `nobody`, `env -i`, `--cpus=2` | PNG создан; `OpenGL renderer string: llvmpipe (LLVM 15.0.6, 128 bits)`, `OpenGL version string: 4.5 (Core Profile) Mesa 22.3.6`; render 0.43 с (дефолт), 0.38 с (`LP_NUM_THREADS=2`) на тривиальной детали | [VERIFIED: docker run] |
| V11 | Базовая RSS (floor) | После `import OCP` + сборка + запись коробки: `maxrss MiB 254`; render-процесс после импорта VTK: `maxrss MiB 255`. Размеры на диске: `vtkmodules` 566M, `OCP` 149M, `cadquery_ocp_novtk.libs` 95M | [VERIFIED: docker run] |
| V12 | Контейнер с `--cpus=2 --memory=1g` не показывает квоту | `nproc` = `11`; `os.cpu_count 11`, `affinity 11`; `/sys/fs/cgroup/cpu.max` = `200000 100000`; `memory.max` = `1073741824`; `memory.peak` существует | [VERIFIED: docker run] |
| V13 | Потоки OCCT | `OSD_ThreadPool.DefaultPool_s(2)` вернул пул с `NbThreads()` = `11` (аргумент не сузил пул в моём вызове); `os.sched_setaffinity(0,{0,1})` вызов проходит. На 400 копиях детали (0,4-0,8 с) различия шумовые: `default` wall 0.63 s / user+sys 1.85 s против `serial` 0.81 s / 1.29 s; вывод о потоках на таком объёме сделать нельзя | [VERIFIED: docker run; вывод по потокам — недостаточный объём, нужна тяжёлая сетка] |
| V14 | Landlock работает на ядре OrbStack 7.0.14 | `sandbox-probe` (собран `GOARCH=arm64`) под `nobody`, `PROBE_RW=/job`, `PROBE_RO=/lib:/usr/lib:/usr/bin/echo:/etc/ld.so.cache`: вывод `landlock-ok` | [VERIFIED: docker run] |
| V15 | Попытка получить «дюймовый» STEP через `Interface_Static.SetCVal_s("write.step.unit","IN")` | Заголовок содержит `0 INCH`; `STEPControl_Reader.FileUnits` вернул `['millimetre']`. Вывод: генератор не может полагаться на этот флаг, нужен ручной заголовок; единицы источника нужно сверять по тексту STEP независимо от `FileUnits` | [VERIFIED: docker run; обратный вывод о FileUnits не сделан] |
| V16 | Окружение | Свободно ~19 GiB на диске хоста (96% занято), Docker: образов 38,3 GB (19,6 GB reclaimable), 12 запущенных контейнеров других проектов; на хосте остались прототипы `cadeval-*` (0,84-1,84 GB) | [VERIFIED: df, docker system df] |

## Architecture Patterns

### System Architecture Diagram

```
 публичные источники ──► scripts/cad-corpus-fetch.sh ──sha256 (ошибка при несовпадении)──► кэш вне репо ─┐
 (FreeCAD-library, NASA, NIST, ezdxf)                                                                     │
 scripts/cad-corpus-gen.py (детерминированный, самопроверка маркеров) ──► синтетика (cp1251, IGES-surf, ─┤
                                  масштабированные STEP/STL, вырожденные детали)                          │
 каталог компании (env, вне репо, может быть пуст) ───────────────────────────────────► только числа+sha256┤
                                                                                                          ▼
 Dockerfile.cad-measure (bookworm-slim, venv из хеш-lock, USER nobody) ──► образ (native arm64)
                                      │
        ┌─────────────────────────────┼───────────────────────────────┬───────────────────────────┐
        ▼                             ▼                               ▼                           ▼
  замер на файл:                рендер 3 ракурса ×              матрица Landlock:           размер образа,
  docker run --cpus=2           деталь (VTK/OSMesa)             sandbox-paths-measure.sh    registry:2 push/pull,
  --memory=N, 1 файл            → PNG + пиксельные              trace → verify (контент)    licence-trace
  memory.peak + ru_maxrss       критерии (скрипт)               для cad_mesh/render/dxf/    (LD_DEBUG / maps)
  wall, OOM-флаг, счётчики      → контактный лист               rsvg-convert
        │                             │                               │                           │
        │                             ▼                               │                           │
        │                  checkpoint:human-verify (D-11)             │                           │
        ▼                             │                               ▼                           ▼
   CSV кривая RSS(size)                ▼                         кандидатные наборы         evidence/*.txt
   по форматам ──► лимиты, memory limit (D-06..D-09)            путей (гипотезы для 73)
        └───────────────────────────────┴───────────────► .planning/phases/71-.../71-DECISION.md
```

### Recommended Project Structure
```
Dockerfile.cad-measure                      # измерительный образ (имя на усмотрение планировщика)
scripts/
├── cad-corpus-gen.py                       # детерминированный генератор; в первом коммите фазы
├── cad-corpus-fetch.sh                     # закреплённые URL + sha256, кэш вне репо
├── cad-rss-time-measure.sh                 # контейнер на файл
├── cad-render-measure.sh                   # рендер, пиксельные критерии, контактный лист
├── cad-measure/                            # одноразовые Python-прототипы хелперов (НЕ продакшен, НЕ embed)
│   ├── cad_mesh.py  cad_render.py  cad_dxf.py  pixelcheck.py  contact_sheet.py  vtk_keeplist.py
│   └── cad-requirements.txt                # с хешами
└── fixtures/ или internal/convert/testdata/cad/   # путь выбирает планировщик (CONTEXT: вероятно testdata/cad)
    └── PROVENANCE.md                       # источник, автор, лицензия, sha256 на файл
.planning/phases/71-evaluation-gate/
├── 71-DECISION.md
└── evidence/*.txt
```
Продакшен-прототипы хелперов фазы 73 здесь не пишутся; прототипы нужны, чтобы трассировать пути и мерить RSS, и должны быть помечены как таковые. `internal/`, `cmd/`, `deploy/`, `docker-compose.yml` не меняются (критерий 5).

### Pattern 1: замер на файл в отдельном контейнере
**What:** один `docker run --rm --cpus=2 --memory=<N>` на (файл, операция, повтор). Внутри обёртка запускает хелпер как дочерний процесс, ждёт и печатает `resource.getrusage(RUSAGE_CHILDREN).ru_maxrss` (KiB на Linux), затем читает `/sys/fs/cgroup/memory.peak` и `memory.events` (`oom_kill`).
**When to use:** вся кривая RSS(size) и поиск обрыва (D-05).
**Why:** прецедент `scripts/{pandoc,ocr,av,audio}-*-measure.sh` читает `memory.peak` через `docker exec`, но он кумулятивен на контейнер («across all cells run so far in this container»): для кривой по файлам нужен чистый контейнер на ячейку. `memory.peak` включает кэш страниц (карты .so и чтение входа), `ru_maxrss` — резидентность процесса; записывать оба и ориентироваться на больший для OOM-запаса.
**Example:**
```python
import os, resource, subprocess, sys, time

t0 = time.monotonic()
rc = subprocess.run(sys.argv[1:], check=False).returncode
wall = time.monotonic() - t0
child_kib = resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss
peak = int(open("/sys/fs/cgroup/memory.peak").read())
events = dict(l.split() for l in open("/sys/fs/cgroup/memory.events"))
print(f"rc={rc} wall_s={wall:.2f} child_rss_mib={child_kib // 1024} cg_peak_mib={peak // 2**20} oom_kill={events.get('oom_kill', '?')}")
```
OOM-убитый контейнер даёт `rc=137` и/или `oom_kill>0`; такой ряд — «обрыв», он обязателен в кривой (D-05), а не исключаемый выброс.

### Pattern 2: пиксельные критерии (D-11 ступень 1), судят PNG
Фон `#FFFFFF` (D-13), серая заливка с контрастом к фону больше порога AA. Маска детали = пиксели, у которых любой канал < 250. Критерии (пороги предварительные, калибруются на первых корпусных деталях и записываются в evidence):
- **не пуст:** доля маски в `[0.02, 0.90]` и доля тёмных «рёберных» пикселей (D-12) ≥ 0,1%; чисто белый кадр или кадр из одних рёбер не проходит.
- **вписан с полем и не обрезан:** bbox маски не касается рамки; минимальное поле ≥ ~3% стороны; `max(w,h)` bbox маски в диапазоне вписывания (при орто-проекции по bbox ожидается около `1 - 2*поле`).
- **ракурсы различаются:** для designated-асимметричных деталей попарное расхождение масок (IoU < 0,97 либо средняя абсолютная разность выше порога) для трёх ракурсов `iso`/`front`/`top`. Для симметричных деталей (куб) совпадение силуэтов не дефект; критерий применять только к асимметричным фикстурам и фиксировать это в скрипте.
- **вырожденные детали:** плоская пластина, стержень, одна грань (толщина ~0) не должны давать NaN-камеру или пустой кадр; такие фикстуры входят в генератор.
- **детерминизм (D-16):** два прогона на одной архитектуре, sha256 PNG, сравнение; прогон с `LP_NUM_THREADS` по умолчанию и с фиксированным значением.
- **обрезанный VTK (D-17):** sha256 PNG с обрезкой и без на всём корпусе.
Код возврата рендера критерием не служит; предыдущие классы проекта несколько раз наступали на «файл создан, содержимое пустое» (SANDBOX-PATHS.md, ловушка 8).

### Pattern 3: keep-list для обрезки VTK генерирует скрипт
Прогнать через рендер-хелпер все пути (все 7 ракурсов, оба размера, `vtkFeatureEdges`, FXAA, прозрачный фон), после каждого собрать список отображённых `vtkmodules/*.so` из `/proc/self/maps`; keep-list = объединение. Удалить остальные `.so` и повторить рендер корпуса в обрезанном образе, сравнить sha256 PNG. Риск: модули, подгружаемые лениво (object factory, `vtkRenderingFreeType` при тексте), в трассе не появятся, если путь не выполнен; поэтому обязателен тест «рендер падает, когда keep-list устарел» и покрытие всех веток рендер-хелпера в трассе. Объём: 566M -> ~290M по STACK.md (замерено прототипом); в этой сессии проверен только размер необрезанных `vtkmodules` (566M).

### Pattern 4: матрица Landlock через существующий инструмент
`scripts/sandbox-paths-measure.sh trace <image> <fixture-dir> <cmd...>` затем `verify <image> <fixture-dir> <ro-paths> <cmd...>` для четырёх хелперов: `cad_mesh.py` (STEP, IGES, STL), `cad_render.py`, `cad_dxf.py`, `rsvg-convert` (pdf, png). Из SANDBOX-PATHS.md применимы все 8 ловушек; для Python-стека особенно: (1) `/usr/bin/python3.11` и цели симлинков venv добавляются руками (execve не виден в `openat`); (2) трасса идёт под `nobody` через `setpriv`; (3) трасса пишет путь до разыменования; (4) критерий достаточности для рендера и DXF — содержимое (пиксельные критерии, sha256 против unconfined), не код возврата; (5) полная матрица (формат × операция), а не один STEP. Образ должен быть нативным: скрипт сам предупреждает при расхождении архитектуры образа и сервера. `verify` печатает `ruleset established`/канарейку по `PROBE_CANARY` (по умолчанию `/etc/hostname`, должна быть читаема до `Apply`).
Наборы — гипотезы для фазы 73 (CONTEXT); запись в 71 помечается «кандидат», не «грант».

### Anti-Patterns to Avoid
- **Судить рендер по коду возврата или размеру PNG** (белый кадр правильного размера проходит).
- **Один контейнер на весь корпус** для `memory.peak` (кумулятивность скрывает кривую).
- **Эмулированный amd64** как «замер под Landlock» (V14 относится только к native arm64).
- **Перенос лимитов PreviewGenerator** (40 МБ / 5 МБ / 60 с, 500 Б) как отправной точки: они — ориентир для сравнения (CONTEXT specifics).
- **`ResetCamera()`** для вписывания (D-14): обрезал правый край детали в прототипе [CITED: STACK.md].

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Трасса путей и проверка Landlock | свой strace-обвязчик | `scripts/sandbox-paths-measure.sh` + `scripts/cmd/sandbox-probe` | Восемь ловушек уже оплачены; проверено V14 |
| Применение Landlock | свой syscall-код | `internal/sandbox` через `sandbox-probe` | Тот же код, что в продакшене |
| Генерация больших/квирковых файлов | рандомные ad-hoc файлы | детерминированный `scripts/cad-corpus-gen.py` по `scripts/sanitize-corpus.py` | Протокол: без времени и случайности, самопроверка маркеров, ненулевой выход |
| Загрузка корпуса | `curl` без проверки | скрипт с закреплённым sha256 (по образцу `scripts/tectonic-bundle-fetch.sh`) | D-03 |
| Хеш-пины колёс | ручной список | `pip-compile --generate-hashes` на bookworm 3.11 | Транзитивные зависимости (numpy, proxy) легко пропустить |
| Контактный лист | своя вёрстка PNG | Pillow `Image.paste` в измерительном образе | `render_evidence.py` не подходит (CONTEXT) |
| Парсинг единиц STEP в вывод | доверие к `FileUnits` | независимая выборка `SI_UNIT`/`CONVERSION_BASED_UNIT` из текста DATA-секции + сверка (V15) | `FileUnits` не проверен на дюймовом файле |
| Потолок байт как защита от бомб | только `MAX_UPLOAD_BYTES` | счётчики D-10 (треугольники, сущности, NAUO, вставки) | Байты не защищают от самоссылающегося STEP/MINSERT |

**Key insight:** в этой фазе самый дорогой дефект — измерить не то, что едет в продакшен (другая версия, другой пользователь, хост без лимита, `ResetCamera`, кумулятивный `memory.peak`); у проекта на это есть названные антипаттерны, и каждый пункт таблицы их закрывает.

## EVAL-04: лицензии и что реально выполнимо

**Цепочка загрузки (V3-V7).** `libfreeimage` и `libraw` загружаются при `import OCP` через `DT_NEEDED`; `libjasper` только в 8.0.1.0.0. Раздел D-18 «замерить, загружает ли наш путь, неиспользуемые удалить» даёт: загружает всегда (на этапе импорта, до любой операции), удалить файл нельзя. «Загружается» и «вызывается» различаются: `libTKService` содержит визуализацию (`Image_AlienPixMap`), которую путь STEP/IGES/STL не вызывает; это [ASSUMED] (не проверено ltrace/uprobe).

**Варианты для планировщика (нужно подтверждение пользователя, D-18 меняет смысл):**
1. **Записать и оставить** (рекомендуется по умолчанию): список библиотек колеса, их лицензии, «только внутреннее использование, образ не распространяется», флаг «нужен OK юриста» без блокировки. Пин 7.9.3.1.1 исключает `libjasper`.
2. **Ограниченный эксперимент `patchelf --remove-needed libfreeimage...`** на `libTKService` (и `libraw` на `libfreeimage`), затем удалить `.so`, затем прогнать корпус и убедиться, что импорт и все операции работают (ленивое связывание: `DT_FLAGS` пусты, V6). Это правка вендорного бинарника: добавляет `patchelf` в сборочную стадию и точку отказа при обновлении OCP, поэтому в продакшен-решение превращать только если юрист скажет «нет».
Оба исхода записываются, фаза не блокируется (D-18).

**Лицензии (для инженерной записи):**
| Библиотека | Где в колесе | Лицензия | Источник |
|---|---|---|---|
| FreeImage | `libfreeimage` (7.9.3.1.1 и 8.0.1.0.0) | двойная: GPL v2 / v3 **или** FreeImage Public License (FIPL, «MPL-flavour»); при распространении требуется приложить лицензию и благодарность | [CITED: freeimage.sourceforge.io/license.html через WebSearch-сводку; прямой WebFetch страницы заблокирован] |
| LibRaw | `libraw` (оба) | двойная: LGPL 2.1 **или** CDDL 1.0 | [VERIFIED: libraw.org/about, WebFetch] |
| JasPer | `libjasper` (только 8.0.1.0.0) | JasPer License 2.0, разрешительная: использование, копирование, распространение, условие сохранять уведомления | [VERIFIED: github.com/jasper-software/jasper LICENSE.txt, WebFetch] |
| OpenCASCADE (OCCT) | `libTK*` | LGPL 2.1 с дополнительным исключением (дважды: STACK.md и репозиторий Open-Cascade-SAS/OCCT показывает `LGPL-2.1`) | [CITED: STACK.md; GitHub API repos/Open-Cascade-SAS/OCCT license `LGPL-2.1`]; текст исключения не читался: [ASSUMED] |
| OCP (привязки) | `OCP/` | Apache-2.0 | [VERIFIED: METADATA колеса, `License: Apache-2.0`] |
| libtiff, libjpeg, libpng, lcms2, openjpeg, libwebp, OpenEXR/Imath, freetype, fontconfig | `.libs/` | разрешительные/LGPL-класса; не перепроверялись | [ASSUMED] |
Поле `licenses` в SBOM колеса пусто (V8), поэтому запись строится по апстримам, а не по метаданным колеса. Утверждение «внутреннее использование без распространения не запускает обязательств copyleft» — [ASSUMED], это именно то, что отдаётся юристу флагом.

## Корпус: проверенные источники и лицензии

| Источник | Что даёт | Лицензия | Проверка в сессии | Замечания |
|---|---|---|---|---|
| `FreeCAD/FreeCAD-library` | ~2900 STEP, 3 IGES; до 52 МБ (`Raspberry Pi 4B.step` 52 486 193 B), 40,8 МБ (`ramps-1_4.step`), 18,2 МБ, 15 МБ (`EmergencyButton-Disk_LAY37.step`), 9,0 МБ (`MIT E Vent T-Slot.STEP`), 8,2 МБ (`Sprocket ... z114.step`) | README: «All Parts ... licensed under CC-BY 3.0», авторство в git log | дерево репозитория и README прочитаны; `.gitattributes` = `*.FCStd diff=fcinfo` (LFS нет); raw-URL отдаёт `content-length: 9044371` для `MIT E Vent T-Slot.STEP`, совпадает с размером в дереве | Лицензия заявлена репозиторием; авторство каждой детали и право загрузчика репозиторием не проверяются. Платы (Raspberry Pi, RAMPS) имеют внешний апстрим с иными лицензиями: брать механику (Mechanical/Medical/Generic) и не платы. Реальные файлы разных CAD-экспортёров, кириллицы нет |
| `nasa/NASA-3D-Resources` | 342 `.stl`, до 52 041 984 B (`Pillars of Creation (full).stl`), много 20-48 МБ (бинарные, сеточные карты рельефа), 0 STEP/IGES/DXF | README: «free and without copyright»; ссылка на NASA Images and Media Usage Guidelines | дерево репозитория (`truncated: false`) и README | Среди авторов есть не-NASA (Ball Aerospace, DigitalSpace, Conti): пофайловая атрибуция нужна, это не «чистый PD» по умолчанию. Рельефные сетки не похожи на механические детали; брать Curiosity/SLS/Roman для детали-подобных STL |
| NIST MBE PMI (FTC/CTC/STC) | STEP AP203/AP242, сборки (FTC 07-10, CTC 02/04, MTC) | «не подлежат авторскому праву, public domain»; «can be used without any restrictions» | WebSearch + WebFetch страницы NIST | Файлы небольшие; хороши как реальные STEP с PMI; размеры не получены |
| `mozman/ezdxf` | 103 DXF (MIT): `hatches_2.dxf` 208 793 B, `columns_R2007.dxf`, `acad_table_with_blk_ref.dxf` 156 603 B, `uncommon.dxf` 343 110 B, `text_fonts.dxf` 301 744 B, `houses_of_parliament_georeferenced.dxf` 228 897 B, `3dsolids.dxf` (ACIS), максимум 1,8 МБ | MIT (репозиторий) | дерево репозитория, `license.spdx_id = MIT` | Многие файлы сгенерированы самим ezdxf (Pitfall 26: «фикстуры из одного генератора»); происхождение каждого файла проверить, реальные экспортёры (AutoCAD) отметить отдельно |

**Отвергнуто по D-04:** Burkardt DXF-примеры (GNU LGPL), KiCad-модели (CC-BY-SA), Open Desk Lab (CC-BY-SA), Prusa (Open Community License), OCCT test-data и `pythonocc-core` (LGPL), `CadQuery` примеры (Apache-2.0 в D-04 не перечислена; если нужны, явный запрос пользователю). [CITED: результаты WebSearch, GitHub API].

**Не найдено в открытом виде (честная запись):** реальные файлы с cp1251/Latin-1 (КОМПАС и подобные), реальный IGES только из поверхностей, реальный DXF с размерами из CAD-экспортёра с лицензией из D-04, любые открытые STEP/STL выше ~52 МБ. Для этого: генератор (D-05) и масштабирование. Масштабированные файлы помечаются в `PROVENANCE.md` как «производная от X, коэффициент N, генератор G», потому что их RAM-профиль (копии против NAUO-инстансинга) разный и оба нужны для D-10.

**Малые против больших (D-03):** порог «малый» в CONTEXT не задан. Рекомендация: коммитить ≤ ~2 МиБ на файл и ~15 МиБ суммарно (оценка, [ASSUMED]); остальное через скрипт-загрузчик с sha256.

## Образ и cold pull

- Реестра компании в окружении нет. Воспроизводимый вариант: локальный `registry:2` на `localhost`, `docker push`, чтение размеров слоёв из манифеста (реальные сжатые размеры), `docker rmi` и `docker pull`. Время pull на loopback измеряет распаковку, не сеть. В записи решения: сжатый размер, время распаковки на loopback и расчётное время передачи при заданных полосах (подписать «рассчитано»). Это закрывает и AVX-02, который в backlog не измерялся [CITED: PROJECT.md:134].
- Ориентиры флота на этом хосте (`docker images`): document-worker 769MB, audio-worker 687MB, chromium-worker 682MB, прототипы `cadeval-vtk` 1.13GB, `cadeval-bw` 1.42GB [VERIFIED: docker images]. Оценка STACK.md: ~1,1 ГБ после обрезки (без сжатия), не измерена.
- Диск почти заполнен (V16): сборка нескольких вариантов образа (7.9 и 8.0, обрезанный и нет) плюс 100-200 МБ корпуса требуют освобождения места. Удаление `cadeval-*` и build cache касается данных пользователя: оформлять как операторский шаг с подтверждением, а не как молчаливый `prune` (память: aggressive prune уже стоил проекту пересборки tesseract). Собирать образы последовательно (память: четыре клина демона OrbStack при параллельных тяжёлых сборках).

## Потоки при `--cpus=2`

V12: контейнер с `--cpus=2` видит 11 процессоров (`nproc`, `os.cpu_count`, `sched_getaffinity`); квота читается только из `/sys/fs/cgroup/cpu.max` (`200000 100000`). Следствия, которые надо измерить на **тяжёлой** сетке (мой тест на 400 копиях занял <1 с и не различил режимы):
- llvmpipe: размер пула из хоста; `LP_NUM_THREADS` ставится внутри хелпера (env движка — allowlist `PATH/HOME/LANG/LC_ALL/TMPDIR` [CITED: STACK.md; PITFALLS Pitfall 9]). Сравнить 1, 2, дефолт на кадре с ≥500k треугольников.
- OCCT: `BRepMesh_IncrementalMesh(..., inParallel)` использует `OSD_ThreadPool`; `DefaultPool_s(2)` в моём вызове пул не сузил (`NbThreads()` = 11), поэтому рабочие рычаги для проверки: `inParallel=False`, `os.sched_setaffinity` из `cpu.max` (идея PITFALLS Pitfall 18, повторно использует логику `internal/convert/cgroup.go`: `parseCPUMax` — floor, минимум 1), `OSD_ThreadPool.SetNbDefaultThreadsToLaunch`. Что из этого реально ограничивает потоки — нужно измерить по `user+sys` против wall при `--cpus=2`.
- Шумовой фон: на хосте одновременно работает 12 контейнеров других проектов (V16); каждый замер времени повторять минимум 3 раза, отчёт min/median/max, и записывать `docker stats --no-stream` до прогона. Иначе время при `--cpus=2` будет временем соседей.

## Бюджет памяти: как посчитать (D-06..D-09)

Формула из D-07: `RSS_worst(size) + Go_worker <= 0.70 × limit`. Для линейной модели `RSS(size) = floor + k × size`:
`size_max = (0.70 × limit − floor − Go_worker) / k_worst`, где `k_worst` — худший множитель RSS/МиБ по формату, `floor` — RSS пустого пайплайна (V11: 254-255 МиБ, импорт OCP/VTK), `Go_worker` нужно измерить (число в проекте не найдено; допущение «десятки МиБ» — [ASSUMED]).

Иллюстрация, **не вывод**: при `k` = 18 из единственного синтетического образца STACK.md (66,7 МБ STEP -> 1,2 ГБ RSS, один образец на одной машине), floor 254 МиБ и Go ≈ 60 МиБ: limit 1 GiB даёт `size_max ≈ (717 − 254 − 60) / 18 ≈ 22 МиБ`; 2 GiB даёт ≈ 62 МиБ; 4 GiB ≈ 150 МиБ. Эта арифметика показывает, почему D-09 (потолок STEP может оказаться ниже 40 МБ) вероятен, но реальные `k` по формату и по худшему файлу корпуса — работа фазы. Модель может быть не линейной (NAUO-инстансинг, самоссылки): кривую надо строить точками на лестнице размеров и искать обрыв.

Лестница размеров (предложение): STEP 1/2/5/10/20/40/80/150 МиБ; STL бинарный 1/5/10/25/52 (реальный NASA)/100/200; STL ASCII тех же объёмов (конвертация детерминированная из реальных бинарных); DXF 0,2-1,8 МиБ реальные + масштабированные до десятков МиБ. Остановка — при OOM или при превышении бюджета времени на ячейку (по образцу `OCTOCONV_PROBE_BUDGET`).

Счётчики D-10 снимать теми же прогонами: число треугольников (по размеру STL `84+50n`, по заголовку), число сущностей STEP (`#N=` строки DATA), `NEXT_ASSEMBLY_USAGE_OCCURRENCE`, число вставок блоков DXF и `MINSERT`; кап-кандидат = с запасом до обрыва, вывод показать.

## Рубрика решений (что должен содержать `71-DECISION.md`)

| Решение | Вариант по умолчанию | Переключатель | Что записать |
|---|---|---|---|
| Ядро | OCP 7.9.3.1.1 | 8.0.1.0.0, если на корпусе заметно (оставить порог планировщику) меньше RSS/времени или чинит файл корпуса; либо если юрист возражает против `libjasper`, то 7.9.3 | таблица RSS/время/ошибки по двум пинам на одном корпусе, лицензионная разница (V3/V4) |
| Рендер | VTK 9.7.1 + OSMesa, орто, bbox-вписывание, feature edges, FXAA | запасной рендерер только при провале по D-16 | пиксельные метрики, детерминизм, время при `--cpus=2`, контактный лист и одобрение пользователя |
| 2D DXF | ezdxf SVG -> `rsvg-convert` (pdf, png) | — | время, шрифты (`fonts-dejavu-core`; без шрифта текст теряется [CITED: STACK.md]), поведение на hatch/блоках/размерах/legacy-кодировке корпуса |
| memory limit | наименьший из 1/2/4 GiB по D-06/D-07 | — | формула с подставленными числами по каждому формату |
| Потолки по формату | выводятся | — | STEP/IGES, STL bin, STL ASCII (если k расходятся), DXF |
| Капы-кандидаты (D-10) | выводятся | внедряет 73/75 | число и вывод |
| Образ | как есть (D-15) | — | размер, сжатый размер, cold pull, вклад обрезки VTK |
| Лицензии | см. EVAL-04 | — | таблица + флаг юриста |
| Черновик словарей для 72 | единицы + происхождение, причины `null`-объёма, ракурсы (RNDR-02) | окончательно MEAS-05 | что встретилось в корпусе |

Не фиксировать `quality` -> константы до измерения времени/RSS/вида на тяжёлых и вырожденных деталях; стартовые значения STACK.md (`lin = k × diag`, draft 0,004, standard 0,001, fine 0,0003; angular 0,5/0,35/0,2 рад) — отправная точка, [CITED: STACK.md], не вывод.

## Common Pitfalls

### Pitfall 1: удаление «неиспользуемой» библиотеки ломает импорт
**What goes wrong:** `rm libfreeimage*.so` -> `ImportError` при `import OCP` (V5).
**Why:** `DT_NEEDED` в цепочке `OCP.so -> libTKService -> libfreeimage` (V6/V7).
**How to avoid:** считать D-18 как «замерить `/proc/self/maps` после каждой операции, затем либо записать, либо `patchelf`-эксперимент»; до любой правки образа прогнать `python -I -B -c "import OCP"`.
**Warning signs:** `cannot open shared object file` в хелпере; в продакшене такая ошибка выглядела бы как «файл клиента не конвертируется» (PITFALLS Pitfall 10: ImportError в обёртке = неисправность узла).

### Pitfall 2: пиковая RSS не отделена от кэша и от соседних ячеек
**What goes wrong:** `memory.peak` кумулятивен по контейнеру и включает кэш страниц; общий контейнер на корпус даёт один пик на всё.
**How to avoid:** Pattern 1; записывать `ru_maxrss` и `memory.peak`; не мерить `pip install` и замер в одном контейнере (в моём V9-прогоне `memory.peak` был 1,27 ГБ из-за установки, не из-за OCP).

### Pitfall 3: хост шумный
**What goes wrong:** 12 чужих контейнеров на тех же 11 CPU дают плавающее время при `--cpus=2`.
**How to avoid:** ≥3 повтора, min/median/max, `docker stats` до прогона, не запускать замер одновременно со сборкой образов.

### Pitfall 4: lock-файл собран не на той среде
**What goes wrong:** на macOS-хосте или на Python 3.12+ резолвится `numpy 2.5.x`, которого нет для 3.11; либо теряется `cadquery-ocp-proxy`.
**How to avoid:** `pip-compile --generate-hashes` внутри `debian:bookworm-slim` (3.11.2) для каждой архитектуры, оба набора хешей (aarch64 и x86_64) для будущего amd64.

### Pitfall 5: генератор «дюймовых» и legacy-файлов не создаёт того, что обещает
**What goes wrong:** `write.step.unit=IN` дал миллиметровый заголовок (V15); `FileUnits` на таком файле вернул `millimetre`, и «проверка единиц» прошла бы на файле без единиц.
**How to avoid:** самопроверка маркеров генератора (как в `sanitize-corpus.py`): для дюймового STEP искать `INCH`/`CONVERSION_BASED_UNIT` в байтах; для cp1251 искать конкретные 8-битные байты и отсутствие `\X2\`; для DXF — `$DWGCODEPAGE`; для IGES-поверхностей — отсутствие `MANIFOLD_SOLID_BREP`-эквивалента и подсчёт типов в D-секции.

### Pitfall 6: пустой каталог компании читается как «чисто»
**What goes wrong:** отсутствие файлов принимается за хорошие результаты.
**How to avoid:** D-02: в записи решения строка «реальный поток не замерен» формируется скриптом при пустом/отсутствующем каталоге, и это условие тестируется (скрипт должен печатать её именно по счёту файлов = 0, а не по отсутствию ошибок).

### Pitfall 7: запуск под root или без лимитов
**What goes wrong:** Landlock-ruleset и пути под root отличаются (ловушка 3 SANDBOX-PATHS); `--memory` без `--cpus` не показывает троттлинг.
**How to avoid:** `USER nobody` в `Dockerfile.cad-measure`; все замеры с `--cpus=2 --memory=N` на контейнере; в evidence писать `uname -m`, версии движков и digest образа (Pitfall 29: версия движка в каждом транскрипте).

### Pitfall 8: масштабирование STEP копированием вместо инстансинга
**What goes wrong:** N независимых копий геометрии и N ссылок NAUO на один продукт дают разную RAM и время; кап по размеру файла отвечает только на первое.
**How to avoid:** генерировать оба варианта и замерить отдельно (D-10).

### Pitfall 9: cold pull «на loopback» выдан за сетевой
**What goes wrong:** время `docker pull` с `localhost:5000` измеряет только распаковку.
**How to avoid:** писать так в evidence и добавлять расчётную часть с формулой.

### Pitfall 10: keep-list из неполного прогона
**What goes wrong:** обрезанный образ рендерит корпусные детали, но падает на пути, которого не было в трассе (прозрачный фон, `size` вне дефолта, вырожденная деталь).
**How to avoid:** трассировать объединение всех путей рендер-хелпера; D-17-тест на устаревание: рендер всех веток в обрезанном образе обязан завершиться, иначе падать.

## Code Examples

### Загрузка с закреплённым хешем (несовпадение = ошибка)
```bash
set -euo pipefail
url="$1"; want="$2"; dest="$3"
mkdir -p "$(dirname "$dest")"
[ -f "$dest" ] || curl -fsSL --retry 3 -o "$dest.part" "$url" && mv "$dest.part" "$dest"
got=$(shasum -a 256 "$dest" | awk '{print $1}')
[ "$got" = "$want" ] || { echo "sha256 mismatch for $dest: got $got want $want" >&2; rm -f "$dest"; exit 1; }
```
Источник паттерна: `scripts/tectonic-bundle-fetch.sh` [CITED: репозиторий, имя файла; содержимое не перечитывалось, образец не копировался].

### Проверка маски рендера
```python
import sys
import numpy as np
from PIL import Image

img = np.asarray(Image.open(sys.argv[1]).convert("RGB"), dtype=np.uint8)
mask = (img < 250).any(axis=2)
ys, xs = np.nonzero(mask)
size = img.shape[0]
frac = mask.mean()
x0, x1, y0, y1 = xs.min(), xs.max(), ys.min(), ys.max()
margin = min(x0, y0, size - 1 - x1, size - 1 - y1)
dark = (img < 80).all(axis=2).mean()
ok = 0.02 <= frac <= 0.90 and margin >= int(0.03 * size) and dark >= 0.001
print(f"frac={frac:.4f} margin_px={margin} dark={dark:.4f} bbox=({x0},{y0},{x1},{y1}) ok={ok}")
sys.exit(0 if ok else 1)
```
Пороги предварительные (калибруются на корпусе, [ASSUMED]).

### Список отображённых модулей VTK после рендера
```python
import re
maps = open("/proc/self/maps").read()
used = sorted({m for m in re.findall(r"(/[^\s]*vtkmodules/[^\s]+\.so[^\s]*)", maps)})
print("\n".join(used))
```
Вызывать в конце рендер-хелпера и объединять по всем ветвям.

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| FreeCAD + Xvfb + f3d (ARCHITECTURE/PITFALLS, v1.16 до исследования) | OCP-novtk + VTK/OSMesa без X | исследование 2026-10-02 | Части PITFALLS про Xvfb/`/tmp`/сокеты устарели [CITED: SUMMARY.md] |
| f3d как рендерер | не подходит как нативный на arm64 (нет Linux-arm64 в v3.5.0) | v3.5.0, 2026-04-05 | только yardstick на amd64 |
| numpy без пина | `numpy<=2.4.6` на Python 3.11 | numpy 2.5.x требует >=3.12 | lock на 3.11 |
| OCP 7.9.x | 8.0.1.0.0 (2026-09-05), `requires_python >=3.11`, `manylinux_2_28` | 2026-09 | доп. `libjasper`; `Bnd_Box.CornerMin/Max` работают на обоих (V9) |

**Deprecated/outdated:** STACK.md: «`trimesh` 5.1.0» (актуально 5.1.1); «`Bnd_Box.Get()` is unusable in 8.0.1»: в этой сессии `hasattr(b,'Get')` = `True` на 8.0.1, но сам вызов не выполнялся, поэтому утверждение не подтверждено и не опровергнуто; остаёмся на `CornerMin/CornerMax`, как в CONTEXT.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `libTKService` (визуализация, `Image_AlienPixMap`) не вызывается на пути STEP/IGES/STL, поэтому вариант `patchelf --remove-needed` работоспособен | EVAL-04 | Эксперимент вывалится в runtime-ошибку при редком пути; закрывается прогоном корпуса |
| A2 | «Внутреннее использование без распространения не запускает обязательств copyleft (GPL/LGPL) у FreeImage/LibRaw» | EVAL-04 | Нужен OK юриста; для этого и флаг в D-18 |
| A3 | Лицензия OCCT «LGPL 2.1 + исключение»; текст исключения не читался | EVAL-04 | Влияет на формулировку записи, не на блокировку |
| A4 | Лицензии остальных вендорных библиотек колеса (tiff, jpeg, png, lcms2, openjp2, webp, OpenEXR, freetype, fontconfig) разрешительные/LGPL-класса | EVAL-04 | Запись неполна; пройтись по апстримам |
| A5 | Порог «малый файл» ≤ ~2 МиБ на файл, ≤ ~15 МиБ суммарно | Корпус | Раздутый репозиторий либо лишний сетевой скачок |
| A6 | Пороги пиксельных критериев (0,02-0,90, поле ≥ 3%, тёмные ≥ 0,1%) | Pattern 2 | Ложные срабатывания; калибруются на корпусе |
| A7 | RSS Go-воркера «десятки МиБ» (≈60 МиБ в иллюстрации) | Бюджет памяти | Сдвиг потолков; измерить на живом воркере |
| A8 | Происхождение файлов FreeCAD-library не нарушает CC-BY 3.0 (заявлено репозиторием, не проверено пофайлово) | Корпус | Юридический риск коммита; смягчается тем, что коммитятся только малые, большие качаются по хешу |
| A9 | Имена пакетов `cadquery-ocp-novtk`, `cadquery-ocp-proxy`, `vtk`, `ezdxf` и др. взяты из прототипа STACK.md; существование и хеши колёс подтверждены по PyPI | Package Legitimacy | Slopsquat-риск закрывается просмотром lock-файла (один checkpoint) |
| A10 | Лестница размеров корпуса (1-150 МиБ STEP, до 200 МиБ STL) | Бюджет памяти | Может не поймать обрыв; расширять до OOM по D-05 |

## Open Questions

1. **D-18 буквально невыполним удалением файлов.**
   - Known: `import OCP` грузит `libfreeimage` и `libraw` (и `libjasper` в 8.0.1) через `DT_NEEDED`; удаление ломает импорт (V3-V7).
   - Unclear: хочет ли пользователь `patchelf`-вариант (правка вендорных бинарников) или достаточно записи + пин 7.9.3.
   - Recommendation: по умолчанию «записать и оставить», `patchelf` — ограниченный эксперимент с записью исхода; вынести пользователю на чекпоинте плана.
2. **Нужна ли Landlock-трасса для `rsvg-convert` и DXF-пути в этой фазе, если фаза 75 их внедряет?** CONTEXT и критерий 1 требуют 2D-путь «по замеру под Landlock» → да, как кандидатный набор.
3. **Файлы компании.** Каталог и env-переменную создаёт пользователь (D-02); пока пуст, запись говорит «не замерен».
4. **Где лежит корпус.** CONTEXT: вероятно `internal/convert/testdata/cad/`; `.dockerignore` исключает `*.md` и `bin/`, но не testdata, поэтому корпус лучше монтировать в контейнер, а не COPY.
5. **Реальные cp1251/IGES-surface файлы.** Открытых не найдено; если у пользователя есть безопасные для коммита, это заменит синтетику.
6. **Достаточно ли репрезентативны рельефные STL NASA** для кривой STL: нет, для кривой нужны и детали-подобные; в плане предусмотреть выбор по триаж-признаку (объём, число треугольников).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Docker (OrbStack) | все замеры | ✓ | Server 29.4.0, linux/arm64, 11 CPU, 15,66 GiB | — |
| Landlock в ядре | замер под Landlock | ✓ | ядро `7.0.14-orbstack-00380-ga7e0a2dc9535` (V14: `landlock-ok`) | — (эмуляция amd64 не даёт, считается отсутствием замера) |
| Go (для сборки `sandbox-probe`) | `sandbox-paths-measure.sh verify` | ✓ | локально `go1.27.1 darwin/arm64`; `go.mod` требует `go 1.26.4` | probe строится `CGO_ENABLED=0 GOOS=linux`; память «pinned toolchain, not local»: инструмент, не продукт, но отметить версию в evidence |
| `debian:bookworm-slim`, `golang:1.26-bookworm` | образы | ✓ | локально, digest `debian@sha256:3783cc01769c7b2b1b83a5c5ad96c815348e28ed7da68e2e3687004faa906251` (arm64) | — |
| Сеть из контейнера (apt, pip, strace для `trace`) | сборка, `trace` | ✓ (использовалась в сессии) | — | — |
| `registry:2` | cold pull | не проверялся | — | `docker save | gzip` как оценка сжатого размера, с пометкой |
| `gh` CLI | поиск/проверка источников корпуса | ✓ | — | — |
| macOS-хост Pillow/numpy | контактный лист | не проверялся | — | запускать в измерительном образе |
| Свободное место | образы + корпус | ⚠ ~19 GiB на хосте | 96% занято | операторский шаг освобождения (удаление `cadeval-*`, build cache) с подтверждением |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** `registry:2` (не проверен), место на диске.

## Security Domain

`security_enforcement` в `.planning/config.json` не задан -> считается включённым. Фаза не добавляет сетевых поверхностей и продакшен-кода; применимо к измерительному контуру.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | — |
| V3 Session Management | no | — |
| V4 Access Control | no | — |
| V5 Input Validation | yes (недоверенные CAD-файлы идут в OCCT/ezdxf/librsvg) | замер только в контейнере: `USER nobody`, `--network none` на замерах, `--cpus/--memory`, Landlock-ruleset в `verify`; закреплённый sha256 корпуса |
| V6 Cryptography | no (sha256 для целостности — `shasum`, не самописное) | `shasum -a 256` |
| V10/V14 Supply chain (V14 Config в ASVS 4.x) | yes | `pip --require-hashes`, база по digest, один human-verify на lock |

### Known Threat Patterns

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Подмена скачанного корпуса | Tampering | sha256, несовпадение = ошибка (D-03) |
| Slopsquat/подмена пакета | Tampering | хеш-пины, один человеческий просмотр lock-файла, `--no-deps` для `vtk` |
| Бомба в публичных/сгенерированных файлах (самоссылки, MINSERT) | DoS | `--memory`/`--cpus`, бюджет времени на ячейку, OOM записывается как результат |
| Утечка клиентских имён и содержимого из приватного потока | Information Disclosure | в evidence только числа и sha256 (D-01), скрипт не печатает имена файлов приватного каталога |
| Запись SVG/PDF из недоверенного DXF в открытое место | Tampering | вывод остаётся в evidence-каталоге; санитизация — фаза 75, не 71 |

## Sources

### Primary (HIGH confidence)
- Запуски в этой сессии на `debian:bookworm-slim`/aarch64 (V1-V16): импорт OCP, `/proc/self/maps`, `pyelftools` по колёсам, VTK/OSMesa-рендер, `nproc`/`cpu.max`, `sandbox-probe`.
- PyPI JSON API (`https://pypi.org/pypi/<pkg>/json`): `cadquery-ocp-novtk`, `cadquery-ocp-proxy`, `vtk`, `ezdxf`, `trimesh`, `numpy`, `pillow`, `fonttools`, `pyparsing`, `typing_extensions`.
- GitHub API: `f3d-app/f3d` releases (нет Linux-arm64), `nasa/NASA-3D-Resources`, `FreeCAD/FreeCAD-library` (+README), `mozman/ezdxf`, `Open-Cascade-SAS/OCCT`.
- Репозиторий: `scripts/sandbox-paths-measure.sh`, `scripts/cmd/sandbox-probe/main.go`, `scripts/probe-timeout-measure.sh`, `Dockerfile.probe-measure`, `scripts/sanitize-corpus.py`, `internal/convert/cgroup.go`, `.planning/research/{SUMMARY,STACK,PITFALLS,SANDBOX-PATHS}.md`, `.planning/REQUIREMENTS.md`.
- LibRaw: https://www.libraw.org/about (WebFetch). JasPer: https://raw.githubusercontent.com/jasper-software/jasper/master/LICENSE.txt (WebFetch).

### Secondary (MEDIUM confidence)
- FreeImage license (dual FIPL / GPL v2-v3): WebSearch-сводка с https://freeimage.sourceforge.io/license.html (WebFetch страницы заблокирован).
- NIST MBE PMI: https://www.nist.gov/el/systems-integration-division-73400/mbe-pmi-validation-and-conformance-testing/download-free-cad (WebFetch + WebSearch).
- NASA 3D Resources: https://github.com/nasa/NASA-3D-Resources, https://science.nasa.gov/3d-resources/ (WebSearch + README).

### Tertiary (LOW confidence)
- Лицензия OCCT и исключение: STACK.md и GitHub-метка `LGPL-2.1`; текст исключения не читался.
- Лицензии вторичных вендорных библиотек колеса — не проверялись.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — версии и хеши по PyPI, запуск на bookworm; MEDIUM по вердиктам seam'а (SUS из-за отсутствия данных о загрузках).
- Architecture / метод замера: HIGH — опирается на существующие скрипты и инструменты, V12/V14 проверены.
- Корпус: MEDIUM — источники и лицензии подтверждены, пофайловый отбор и провенанс не сделаны; открытых файлов >52 МБ и cp1251/IGES-surface нет.
- Pitfalls: MEDIUM-HIGH — часть воспроизведена (V5, V15), часть из PITFALLS.md.
- Лицензии: MEDIUM — основные три проверены, юридический вывод [ASSUMED].

**Research date:** 2026-10-03
**Valid until:** ~2026-10-17 для версий пакетов (OCP 8.0.x и VTK 9.7.x свежие, быстро движутся); корпусные источники и лицензионные выводы — 30 дней.

Раздел Validation Architecture опущен: `workflow.nyquist_validation` в `.planning/config.json` равен `false`. Прим.: инструменты `research-plan`/`research-store` не использовались, источники проверялись напрямую запусками и API-запросами.
