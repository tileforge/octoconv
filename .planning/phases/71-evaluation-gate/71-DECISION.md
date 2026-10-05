# Phase 71: Evaluation Gate - запись решений

**Дата:** 2026-10-04. **Ветка:** gsd/phase-71-evaluation-gate. **Читают:** фазы 72-76.

**Правило записи.** Каждое число ниже имеет ссылку на строку evidence в виде `evidence/ФАЙЛ#МЕТКА`
(метка - первое поле строки). Что не замерено, помечено «не замерено»; что взято из чужого источника или
рассчитано, помечено «ориентир PreviewGenerator», «рассчитано» или `[ASSUMED]`. Значения сводки совпадают со
строками evidence; `scripts/cad-derive-limits.py` пересчитывает лимиты из evidence и даёт тот же файл.

**Где измерено** (`evidence/71-08-image-size.txt#HDR`, образ финального варианта pruned): server_arch arm64
(нативная, OrbStack), kernel 7.0.14-orbstack-00380-ga7e0a2dc9535, image_id
sha256:79f5f6f41f8529a05378e4e81867f0fcd73883be1438b50e1a238dbf8ae91d01, OCP 7.9.3.1.1, vtk 9.7.1, ezdxf 1.4.4,
mesa 22.3.6-1+deb12u2, rsvg-convert 2.54.7, Python 3.11.2, `debian:bookworm-slim` по digest
(`evidence/71-01-lock.txt#BASE_IMAGE`), USER nobody. Замеры RSS и времени для лимитов - `--cpus 2`, лимит памяти на контейнер, пин
7.9.3.1.1, **`occt_parallel=off`, `LP_NUM_THREADS=2`** (выбранные настройки), образ `full` image_id
sha256:cce9074fe380 (`evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#HDR`). Хост шумный: 12 контейнеров
чужих проектов работали всё время (`evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#NOISE`).

**Конфигурация строк evidence.** Все лимиты, потолки и капы ниже выведены только из строк с тегами `par=off,lp=2`
(`evidence/71-06-limits.txt#CONFIG`: 448 строк использовано, 662 строки другой конфигурации исключены).
Первый замер 4 GiB (`evidence/71-06-ladder-4g-ocp7.9.3.1.1.txt`), лестницы 2 GiB и 1 GiB
(`evidence/71-06-ladder-2g.txt`, `evidence/71-06-ladder-1g.txt`) и лестница пина 8.0.1.0.0
(`evidence/71-06-ladder-4g-ocp8.0.1.0.0.txt`) сняты при `par=on,lp=default`: это история, ни одно число
сводки из них не получено. Причина перемера (замечание ревью PR #32, решение пользователя): при `lp=2` RSS
рендера выше (1113 против 873 MiB на STL 100 MiB в `evidence/71-06-threads.txt`), а при `par=off` RSS меша
ниже, поэтому прежние лимиты рендера были оптимистичны.

## Сводка DECISION (машиночитаемая)

```text
DECISION,key=ocp_pin,value=7.9.3.1.1,evidence=evidence/71-06-limits.txt#PIN_DECISION
DECISION,key=renderer,value=vtk-9.7.1+osmesa,evidence=evidence/71-04-render.txt#RENDER_SUMMARY
DECISION,key=renderer_user_approval,value=approved_2026-10-04,evidence=71-05-SUMMARY.md#User acceptance (D-11 step 2); evidence/71-05-contact-f3d.png
DECISION,key=dxf_path,value=ezdxf-svg+rsvg-convert,evidence=evidence/71-04-helpers.txt#SUMMARY
DECISION,key=memory_limit_mib,value=4096,evidence=evidence/71-06-limits.txt#MEMORY_LIMIT_DECISION; evidence/71-06-limits.txt#L_SENSITIVITY
DECISION,key=ceiling_step_mib,value=63,evidence=evidence/71-06-limits.txt#CEILING; evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#LADDER_STOP
DECISION,key=ceiling_iges_mib,value=155,evidence=evidence/71-06-limits.txt#CEILING; evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#LADDER_STOP
DECISION,key=ceiling_stl_binary_mib,value=277,evidence=evidence/71-06-limits.txt#CEILING; evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#LADDER_STOP
DECISION,key=ceiling_stl_ascii_mib,value=877,evidence=evidence/71-06-limits.txt#CEILING; evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#LADDER_STOP
DECISION,key=ceiling_dxf_mib,value=118,evidence=evidence/71-06-limits.txt#CEILING; evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#LADDER_STOP
DECISION,key=cap_render_triangles,value=4014975,evidence=evidence/71-06-limits.txt#CAP; evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#LADDER_STOP
DECISION,key=cap_step_entities,value=754762,evidence=evidence/71-06-limits.txt#CAP; evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#LADDER_STOP
DECISION,key=cap_nauo,value=1518,evidence=evidence/71-06-limits.txt#CAP; evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#LADDER_STOP
DECISION,key=cap_dxf_expanded_inserts,value=170031,evidence=evidence/71-06-limits.txt#CAP; evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#LADDER_STOP
DECISION,key=lp_num_threads,value=2,evidence=evidence/71-06-limits.txt#THREADS_DECISION; evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#HDR
DECISION,key=occt_parallel,value=off,evidence=evidence/71-06-limits.txt#THREADS_DECISION; evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#HDR
DECISION,key=limits_config,value=par=off;lp=2,evidence=evidence/71-06-limits.txt#CONFIG; evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#HDR
DECISION,key=quality_draft,value=0.004/0.5,evidence=evidence/71-06-limits.txt#QUALITY
DECISION,key=quality_standard,value=0.001/0.35,evidence=evidence/71-06-limits.txt#QUALITY
DECISION,key=quality_fine,value=0.0003/0.2,evidence=evidence/71-06-limits.txt#QUALITY
DECISION,key=d18_outcome,value=record,evidence=evidence/71-07-licences.txt#D18_OUTCOME
DECISION,key=image_pruned_uncompressed_mib,value=1083.0,evidence=evidence/71-08-image-size.txt#IMAGE_SIZE
DECISION,key=image_pruned_compressed_mib,value=299.3,evidence=evidence/71-08-image-size.txt#IMAGE_SIZE
DECISION,key=image_full_uncompressed_mib,value=1358.3,evidence=evidence/71-08-image-size.txt#IMAGE_SIZE
DECISION,key=cold_pull_median_s,value=6.38,evidence=evidence/71-08-image-size.txt#COLD_PULL_DIST
DECISION,key=private_corpus,value=files=0;status=реальный поток не замерен,evidence=evidence/71-03-corpus-inventory.txt#PRIVATE_CORPUS
DECISION,key=commit_threshold,value=2MiB-per-file;15MiB-total;ASSUMED-A5,evidence=evidence/71-03-corpus-inventory.txt#INV
```

`quality_*` - пары `linear_deflection_coefficient/angular_deflection_rad` относительно диагонали bbox. Размеры в
сводке - MiB (1 MiB = 1048576 байт). `d18_outcome=record` - строка `D-18 outcome: record` в `71-01-SUMMARY.md`.

## Что решено и что остаётся открытым

| Вопрос | Решение | Кто решил |
|---|---|---|
| Ядро геометрии | колесо `cadquery-ocp-novtk` **7.9.3.1.1** | пользователь, вопреки результату формулы (8.0.1.0.0) |
| Рендерер | VTK 9.7.1 + OSMesa, f3d только ориентир | пользователь (`approved`, контактные листы), D-11 |
| 2D-путь DXF | ezdxf -> SVG -> `rsvg-convert` | по замеру |
| Лимит памяти пода `cad` | 4096 MiB (во флоте 1Gi, один сервис 2Gi) | по формуле D-06/D-07 на строках `par=off,lp=2`; не меняется при перемере (`L_SENSITIVITY`) |
| Конфигурация замера лимитов | 4 GiB перемерен при `occt_parallel=off`, `LP_NUM_THREADS=2` (ревью PR #32, P1) | пользователь: «перемерить 4g на 7.9.3.1.1» |
| D-18 | `record`: библиотеки остаются, лицензии записаны | пользователь (`evidence/71-01-decisions.txt`) |
| `libssl3` в разовом образе f3d | допущен только там | пользователь |
| Lock-файлы Python | допущены, установка только `pip --require-hashes` | пользователь |

Открыто и переходит дальше (подробности в разделах ниже): нужен OK юриста (не блокирует); защита времени
рендера в фазе 76 (стек копий: 61,6 с против 1,2 с); приватный поток не замерен; наборы Landlock зависят от
`lp=2` и `parallel=off`; лимит 4 GiB отклоняется от флотского 1Gi; лестницы 2 GiB и 1 GiB и сравнение пинов сняты при
`par=on,lp=default` (история); amd64 не замерен.

## Ядро (EVAL-01)

Пин: **7.9.3.1.1** (`evidence/71-06-limits.txt#PIN_DECISION`). Формула плана дала **8.0.1.0.0**
(`evidence/71-06-limits.txt#PIN_FORMULA`), пользователь выбрал 7.9.3.1.1 и остался при этом выборе.

| Порог | Значение на корпусе | Выполнен |
|---|---|---|
| (a) 8.0 читает файл, который 7.9 не читает | 0 файлов, обратно 0 | нет |
| (b) k_worst STEP-меша минимум на 15% ниже | 7.9: 27.305, 8.0: 22.144, разница 18.9% | да, но не устойчиво |
| (c) сумма времени мешера минимум на 20% ниже по 53 общим файлам | 2449.73 с против 2304.32 с, 5.9% | нет |

Источник: `evidence/71-06-limits.txt#PIN_FORMULA`. Пороги посчитаны по строкам `par=on,lp=default` обоих пинов
(`evidence/71-06-limits.txt#PIN_CONFIG`): лестница 8.0.1.0.0 есть только в этой конфигурации, и пины сравниваются на
строках одной конфигурации; перемер 4 GiB при `par=off,lp=2` в сравнение не входит и результат формулы не меняет
(`PIN_FORMULA`, `PIN_ROBUSTNESS` совпали с прежними байт в байт). Порог (b) зависит от определения RSS
(`evidence/71-06-limits.txt#PIN_ROBUSTNESS`): 18.9% по max(`ru_maxrss`, `memory.peak`), 4.8% только по `ru_maxrss`,
8.8% только по `memory.peak`; наклон на 40-200 MiB одинаков (14.81 MiB на MiB у 7.9.3.1.1 и 14.88 у 8.0.1.0.0),
разница - в базе около 16 MiB. Причины пользователя: выигрыш не устойчив, 8.0 добавляет `libjasper`, `libbz2` и
Imath в лицензионный набор, нужен шим совместимости в `cad_mesh.py`.

Отказы чтения: ни один из 55 файлов STEP/IGES не падает ни на одном пине, кроме файла с 100000 экземпляров
(timeout на обоих) и IGES 493 MiB (OOM на обоих) - `evidence/71-06-ladder-4g-ocp8.0.1.0.0.txt#PIN_COMPARE`.
Число треугольников на общих файлах совпадает, у нескольких отличается на единицы.

Разница лицензионного набора: `libjasper` только на 8.0.1.0.0 (`libjasper_ops=0` на 7.9.3.1.1 против 7 на 8.0.1.0.0,
`evidence/71-07-libtrace.txt#D18_TRACE`; `evidence/71-07-licences.txt#LICENCE` JasPer, ocp=8.0.1.0.0).
`CornerMin()`/`CornerMax()`: один и тот же `cad_mesh.py` отработал на обоих пинах; единственная правка под 8.0 -
запасной импорт `OCP.collections.Sequence_TCollection_AsciiString` вместо `OCP.TColStd.TColStd_SequenceOfAsciiString`
(`evidence/71-06-ladder-4g-ocp8.0.1.0.0.txt#NOTE`, регрессия 7.9.3.1.1 на 31 файле без различий).
Колёса: хеши aarch64 обоих пинов совпали с независимым замером (`evidence/71-01-lock.txt#CROSSCHECK`, `match=yes`).

Пересмотр пина оправдан, если на реальном потоке сработает порог (a) или (c); порог (b) без устойчивой причины
пин не меняет.

## Рендерер (EVAL-01, D-11..D-14, D-16)

**Выбор: VTK 9.7.1 + OSMesa** (`evidence/71-02-tracer.txt#HDR`, `evidence/71-08-image-size.txt#HDR`: vtk 9.7.1,
mesa 22.3.6-1+deb12u2). Запись сделана после ответа `approved` пользователя на контактные листы (D-11 ступень 2):
`71-05-SUMMARY.md`, раздел «User acceptance». Ответ дан 2026-10-04 и дошёл до исполнителя плана 71-05 через
оркестратор, а не напрямую. Листы: `evidence/71-04-contact-views.png`, `evidence/71-04-contact-quality.png`,
`evidence/71-04-contact-robustness.png`, `evidence/71-04-contact-dxf.png`, `evidence/71-05-contact-f3d.png`.

Пиксельные критерии (D-11 ступень 1, код возврата не используется): `RENDER_SUMMARY,parts=5,pngs=108,fail=0,
views_differ=4,determinism=identical` (`evidence/71-04-render.txt#RENDER_SUMMARY`). Калиброванные пороги
(`evidence/71-04-render.txt#CAL`): доля заливки 0.02-0.90, тёмные пиксели не менее 0.001, поле не менее 0.03;
режим «сливер» (проекция уже 0.02) требует штрих на 80% кадра; режим чертежа: заливка не менее 0.0003, тёмные
не менее 0.0002; три ракурса различаются при IoU не выше 0.97. Наблюдено на 35 реальных PNG:
`CALOBS,real_pngs=35,frac_min=0.1159,frac_max=0.6537,dark_min=0.0046,margin_px_min=76`
(`evidence/71-04-render.txt#CALOBS`). Ракурсы iso/front/top различаются: `min_pair_iou` от 0.1096 до 0.5022 на
четырёх несимметричных деталях (`evidence/71-04-render.txt#VIEWS`).

Стиль (D-12..D-14, `evidence/71-04-render.txt#VIEWDEF`): ортографическая проекция, Z вверх, камера = центр bbox +
направление x 2 x диагональ bbox, заливка 0.8:0.8:0.8, рёбра по углу 30 градусов, белый фон, `ResetCamera()` не
используется (подгонка по bbox).

Детерминизм: два iso-рендера каждой реальной детали побайтно равны (`RENDER_SUMMARY` выше); на 196 PNG полного и
обрезанного образа различий нет (`evidence/71-07-prune.txt#PRUNE_IDENTITY`). Граница утверждения: одна
архитектура, одна сборка.

Под Landlock на нативном arm64: все 34 входа четырёх хелперов дают результат, равный неограниченному запуску;
рендер дополнительно проходит pixelcheck (`evidence/71-07-landlock.txt#SUFFICIENT`, 20 строк `helper=cad_render`
с `content=identical`; `evidence/71-07-landlock.txt#LANDLOCK_SUMMARY`).

Время при `--cpus=2`: 35 реальных рендеров 1280 px - от 0.84 до 1.04 с стенного времени, child RSS 270-274 MiB
(`evidence/71-04-render.txt#REN`); тяжёлые реальные входы при `lp=2` - медиана 3.61 с для STL 52 MB и 2.20 с для STEP 9 MB
на 436842 треугольников (`evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#DIST`). **Оговорка для фазы 76:** на файле из
36 совпадающих копий детали тот же рендер занимает 61.57 с, а с разнесёнными копиями 1.18 с при тех же 214128
треугольниках (`evidence/71-06-render-stacking.txt#STACKING`); время уходит до самого рендера VTK
(`t_total_s` 61.238 против `t_render_s` 1.048). Ни кап по размеру, ни кап по треугольникам такой вход не
останавливает: шагу рендера нужен собственный контроль времени, это работа фазы 76.

f3d как ориентир: у релиза v3.5.0 нет сборки Linux arm64 (`evidence/71-05-f3d.txt#F3D_ASSETS`,
`linux_arm64=0`, latest тоже v3.5.0), поэтому его нельзя замерить ни нативно, ни под Landlock. Сравнение на
эмулированном amd64: медиана f3d 3326 мс против 1258 мс у VTK на тех же 15 парах (`evidence/71-05-f3d.txt#F3D`,
`evidence/71-05-f3d.txt#VTK`; эмуляция пессимистична, основанием для решения не служит). f3d не рисует feature
edges, поэтому пиксельный критерий VTK даёт `ok=0` для 13 из 15 его картинок (`evidence/71-05-f3d.txt#F3DPIX`,
информационно). Тарболл f3d закреплён по sha256 (`evidence/71-05-f3d.txt#F3D_TARBALL`). `libssl3` в разовом образе
f3d допущен пользователем (`evidence/71-05-f3d.txt#APPROVAL`), в `Dockerfile.cad-measure` и продакшен-образы не
попадает.

Собственный растеризатор не строился. По каждому условию функционального провала D-16 есть доказательство, что
его не было:

| Условие D-16 | Доказательство | Итог |
|---|---|---|
| не рендерит под Landlock на bookworm | `evidence/71-07-landlock.txt#LANDLOCK_SUMMARY`: 34 из 34 входов | не наступило |
| не проходит пиксельные критерии | `evidence/71-04-render.txt#RENDER_SUMMARY`: `fail=0` из 108 PNG | не наступило |
| не проходит приёмку пользователя | `71-05-SUMMARY.md`: `approved` | не наступило |
| недетерминирован в пределах архитектуры | `evidence/71-04-render.txt#RENDER_SUMMARY`: `determinism=identical`; `evidence/71-07-prune.txt#PRUNE_IDENTITY` | не наступило |
| не укладывается по времени при `--cpus=2` | `evidence/71-04-render.txt#REN`: до 1.04 с; `evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#DIST`: до 3.73 с (p95 тяжёлого STL); исключение - стек копий (выше) | не наступило для обычных входов; стек копий передан фазе 76 |

## 2D-путь DXF (EVAL-01)

**Выбор: ezdxf 1.4.4 рисует модель в SVG (`cad_dxf.py`), `rsvg-convert` 2.54.7 делает PNG и PDF.**
`evidence/71-04-helpers.txt#SUMMARY`: `rows=59,failed=1,oom=0,timeout=0` - единственный отказ объяснён в
`evidence/71-04-helpers.txt#FINDING` (`3dsolids.dxf`, в модели только четыре ACIS 3DSOLID, рисовать нечего, `rc=3`).
Все `rsvg-png` дают 1280x1280 с `pix=ok`, все PDF начинаются с `%PDF-` (`evidence/71-04-helpers.txt#PDFCHK`).
Восемь DXF-чертежей проходят пиксельный режим `drawing` (`evidence/71-04-render.txt#DRW`).

- **Legacy 8-bit:** кодировка берётся из `$DWGCODEPAGE`: `legacy_cp1251.dxf` читается как `Деталь`
  (`ANSI_1251`), `legacy_latin1.dxf` как `Bügel Ø5` (`evidence/71-04-helpers.txt#ROW`).
- **Пропущенные типы:** `uncommon.dxf` рисуется без своего 3DSOLID и без отказа (`skipped_types:3DSOLID-1`), для
  `3dsolids.dxf` нужен типизированный отказ вместо пустой страницы - наблюдение для фазы 75
  (`evidence/71-04-helpers.txt#FINDING`).
- **SVG:** на всех рисуемых файлах `script`, `foreignObject` и внешних `href` по счётчику нет
  (`svg_counts` в `evidence/71-04-helpers.txt#ROW`); фильтрация не строилась, это наблюдение для фазы 75.
- **Шрифты:** `fonts-dejavu-core` стоит в образе (`Dockerfile.cad-measure`); без `/usr/share/fonts` DXF с текстом
  завершается с кодом 0, но с другим SVG, поэтому каталог входит в набор Landlock `cad_dxf`
  (`evidence/71-07-landlock.txt#LANDLOCK_MINIMAL`).
- **Landlock:** минимальные наборы `cad_dxf` и `rsvg-convert` - в разделе Landlock ниже.

## Память и потолки (EVAL-03, D-05..D-09)

Формула D-07: `size_max = (0.70 x L - floor - Go) / k_worst`, где L - лимит пода, floor - наибольший пиковый RSS
конвейера на файлах до 1 MiB, k_worst - наибольшее `(peak - floor)` на MiB среди файлов от 1 MiB (худший файл, не
средний), Go - RSS Go-воркера. Счётчиковые серии (`step-nauo`, `dxf-minsert`) в k не входят, ими занимаются капы.
Go-воркер: VmHWM 24.9 MiB после одной задачи (`evidence/71-06-go-worker-rss.txt#GO_WORKER`); замер на живом
воркере класса image как аналоге будущего cad-воркера (так и записано в строке: `analogue=image-class Go worker
not cad-worker`); допущение A7 (около 60 MiB) заменено замером.

Подстановка при L = 4096 MiB, 0.70 x L = 2867.2 MiB (`evidence/71-06-limits.txt#LIMIT`):

| Формат | floor | Go | k_worst, MiB/MiB | худший файл (sha256, префикс) | size_max при 1 / 2 / 4 GiB | Потолок при 4 GiB |
|---|---|---|---|---|---|---|
| STEP | 478.0 | 24.9 | 37.362 | `step-copies-LinearSlide-MGN15x-Block-1MiB.step`, 1094537 байт (9bea6097ff46) | 5.7 / 24.9 / 63.3 | **63 MiB** |
| IGES | 272.0 | 24.9 | 16.486 | `freecad-festo-valve-5-2.igs`, 11321208 байт (0ab66a4ad85b) | 25.5 / 68.9 / 155.9 | **155 MiB** |
| STL бинарный | 480.0 | 24.9 | 8.498 | плитка 196 MiB (2045f91fb675) | 24.9 / 109.3 / 278.0 | **277 MiB** (277.97 вниз) |
| STL ASCII | 272.0 | 24.9 | 2.930 | производный 200 MiB (d42f47a7e934) | 143.3 / 388.0 / 877.2 | **877 MiB** |
| DXF | 115.0 | 24.9 | 23.017 | `dxf-replicate-uncommon-2MiB.dxf`, 2050076 байт (48cacc1f3f4d) | 25.1 / 56.2 / 118.5 | **118 MiB** |

Источник строк: `evidence/71-06-limits.txt#LIMIT`, `evidence/71-06-limits.txt#CEILING` (строки `par=off,lp=2`).

**Прежние значения при `par=on,lp=default` (история, `evidence/71-06-ladder-4g-ocp7.9.3.1.1.txt`):** потолки STEP 73, IGES 137,
STL бинарный 278, STL ASCII 875, DXF 217 MiB; k_worst 32.115 / 18.709 / 8.493 / 2.935 / 12.568; floor 479 / 272 / 481 /
272 / 114 (STL ASCII пересчитан по правилу «rc=0 и pix=ok», до правки стояли 1363, k 1.705, floor 518). Меш при `par=off` ниже по RSS (копии freecad 150.9 MiB: 2908 -> 2378 MiB; IGES 274 MiB: 3131 -> 2569), рендер
при `lp=2` выше (STL-плитка 100 MiB: 837 -> 1114; STL-плитка 196 MiB: 1527 -> 1692; копии freecad 150.9 MiB: 2663 ->
3131 MiB; IGES 274 MiB: 1370 -> 1737).

**Честная оговорка: сдвиги у STEP (73 -> 63) и DXF (217 -> 118) идут не от настройки потоков.** Формула берёт
худший файл от 1 MiB. Новый худший STEP-файл - производный 1.04 MiB с пиком 517 MiB при floor 478 MiB, то есть
39 MiB над floor на 1 MiB: точка у floor, `worst_point_governs=yes` (`k_median` 7.103). Реальный STEP 9 MB даёт
(757 - 478) / 8.63 = 32.3 MiB/MiB и потолок около 73 MiB (при `par=off,lp=2`: меш 418, рендер 757 MiB,
`evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#DIST`). У DXF худший файл теперь 2 MiB с пиком 160 MiB при floor
115 MiB; ezdxf и `rsvg-convert` от `par` и `lp` не зависят, поэтому 217 -> 118 это шум `memory.peak` (включает page
cache) на файле у floor; сама ступень 100 MiB даёт 1350 MiB при 1355 прежде, k около 12.5, потолок около 218. Правило D-07
(худший файл, максимум `ru_maxrss` и `memory.peak`) не менялось, поэтому в сводке стоят более консервативные 63 и 118;
подтверждено измерением: STEP полный конвейер проходит до 150.9 MiB, DXF до 197.6 MiB при 2497 MiB.

**Правка по ревью PR #32 (Codex P1, «Reject rows whose pixel validation failed»): потолок STL ASCII 1353 -> 877 MiB.**
Прежний `Cell.status()` считал `ok` любой замер с `rc=0`, даже если в той же строке `pix=fail`. Так срез `stl-ascii-nasa-sls-block1-1MiB.stl`
(render, 4 GiB, `par=off,lp=2`: `rc=0`, `pix=fail`, `cg_peak_mib=514`) попал в floor STL ASCII (514.0 вместо 272.0) и занизил
потолок. Теперь замер считается `ok` только при `rc=0` и успешных вердиктах вывода (таблица в `71-06-SUMMARY.md`, раздел
Addendum 2); строка с отказом печатается как `INVALID_OUTPUT` (`evidence/71-06-limits.txt#INVALID_OUTPUT`) и не попадает в floor,
FIT, обрывы и капы. Изменилось одно число формата: floor 514.0 -> 272.0, k_worst 1.720 -> 2.930, потолок 1353 -> 877; L = 4096
и остальные потолки, капы, FIT и `L_SENSITIVITY` не изменились. Причина отказа: срез состоит из первых 4396 треугольников
`nasa-sls-block1`, на кадре вертикальная полоса 93 x 1030 пикселей (`frac=0.0022` при пороге 0.02; `mode=part`, правило «сливера» не
сработало, `proj_aspect` в evidence не записан), то есть рендер не пуст и не упал, а `pixelcheck.py` отвергает тонкую, но настоящую деталь
(`evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#PIX`, строка перед `ROW` этого файла).

Пример подстановки, STEP: `(0.70 x 4096 - 478.0 - 24.9) / 37.362 = 63.3`, вниз до 63 MiB
(`evidence/71-06-limits.txt#CEILING`). STL бинарный и ASCII имеют отдельные потолки: множители k расходятся на
65.5% при пороге 25% (`evidence/71-06-limits.txt#STL_K`; до правки 79.8%, вывод тот же).

Лимит пода (D-06): **4096 MiB.** Наименьший из 1/2/4 GiB, при котором потолок STEP достигает 40 MiB (ориентир
PreviewGenerator): size_max STEP 5.7 / 24.9 / 63.3 MiB при 1 / 2 / 4 GiB; при 1 GiB бинарный STL 24.9 MiB
оказывается ниже реального файла 49.6 MiB (`evidence/71-06-limits.txt#MEMORY_LIMIT_DECISION`,
`evidence/71-06-limits.txt#REAL_MAX`). **Это отклонение от флотских 1Gi (у одного сервиса 2Gi,
`deploy/chart/octoconv/values.yaml`), записанное с выводом.** Цена: у пода cad в четыре раза больше памяти, чем у
типового воркера флота, это вход для размера узлов и `keda.cad.minReplicaCount` в фазе 76. Лимит выведен при
`--cpus 2`; при другом `cpu` числа не переносятся.

**Обоснование выбора L после перемера.** L выбирается по формуле на кривой 4 GiB, снятой при `par=off,lp=2`, а не по
лестницам 2 GiB и 1 GiB (они при `par=on,lp=default` и в число не входят). Строки `L_SENSITIVITY`
(`evidence/71-06-limits.txt#L_SENSITIVITY`): для 2 GiB потолку STEP 40 MiB нужен k_worst не выше 23.267 против
измеренных 37.362 (в 1.61 раза выше); для 1 GiB нужен не выше 5.347 (в 6.99 раза). Даже реальный STEP 9 MB с k 32.3
не проходит 2 GiB, так что выбор L не зависит от шума у floor и от того, что 2g/1g снимались при прежних настройках:
L = 4096 остаётся. Лестницы 2 GiB и 1 GiB остаются историей находки обрывов (`reason=oom`) и по `LADDER_STOP` в числа
не превращаются.

D-09: потолок STEP при 4 GiB (63 MiB) выше 40 MiB, поэтому формулировка «принимаем замеренный потолок ниже 40 МБ»
не применяется. Применяется замеренный потолок, а большие файлы получают честный 413/422, а не OOM-килл.
**Оговорка:** потолок STEP лежит близко к временному обрыву рендера на стеке копий (обрыв при 80 MiB,
`ceiling_over_timeout_cliff=0.79`, `timeout_margin_80=ok`, до перемера было 0.91 и `FLAG`); запас 20% выполнен
только потому, что потолок снизился с 73 до 63 MiB. Если контроль времени фазы 76 не закроет стек копий,
строка по-прежнему предлагает 64 MiB (`recommended_ceiling_mib_if_the_timeout_cliff_applies=64`,
`evidence/71-06-limits.txt#CEILING`), то есть выше нового потолка 63 MiB: контроль времени всё равно нужен.

Где потолок только частично измерен: для STL бинарного и STL ASCII потолки по формуле лежат выше верхней
ступени лестницы на 4 GiB без обрыва, измеренный потолок 196 и 199 MiB (`beyond_top_rung=1`,
`measured_ceiling_mib`); числа 277 и 877 выше этих ступеней - экстраполяция. DXF после перемера (118 MiB) лежит
ниже верхней ступени 197.6 MiB и целиком внутри измеренной области. У IGES обрыв по OOM на 493.3 MiB, потолок
155 MiB составляет 0.31 от него (`ceiling_over_oom_cliff=0.31`). У STEP меш доходит до 209.7 MiB (LinearSlide) и
205.5 MiB (freecad), но полный конвейер (меш и рендер) проходит только до 150.9 MiB (`top_rung_mib=150.9`): у рядов
LinearSlide рендер оборвался по времени на 80 MiB, у freecad на 205.5 MiB, и точки без рендера в `top_rung` и в
`FIT` не входят (`partial`: конвейер замерен не целиком, правка по ревью PR #32, P2). Потолок 63 MiB внутри измеренной
области.

Доля реального потока выше потолка: **реальный поток не замерен** (`evidence/71-06-limits.txt#PRIVATE_CORPUS`,
`files=0`). Наибольшие реальные файлы публичного корпуса лежат ниже всех потолков: STEP 17.4 MiB, IGES 10.8 MiB,
STL бинарный 49.6 MiB, DXF 0.3 MiB, реального ASCII STL выше 3 КБ нет (`evidence/71-06-limits.txt#REAL_MAX`).
Всё выше этих размеров - производные файлы (стопки копий, плитки, тиражированное пространство модели, один
прототип много раз). Пустое «приватное» поле - это «не проверялось», а не «чисто».

Обрывы на лестнице (`evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#LADDER_STOP`; при `par=on,lp=default`:
`evidence/71-06-ladder-2g.txt#LADDER_STOP`, `evidence/71-06-ladder-1g.txt#LADDER_STOP`): обрывы по памяти найдены при 1 и 2 GiB для STEP, бинарного STL, DXF и
IGES; при 4 GiB (`par=off,lp=2`): STEP-меш до 209.7 MiB без обрыва, рендер STEP-копий упирается во время (80 MiB у
LinearSlide и 205.5 MiB у freecad, таймаут ячейки 900 с, причина - стек копий), `step-nauo` рендер падает по OOM при 5000
экземплярах, IGES по OOM на 493 MiB, `dxf-minsert` обрывается на 1000000 ячеек. Цена `par=off` по времени: меш
`step-nauo` на 10000 экземплярах теперь обрывается по таймауту 900 с (при `par=on` проходил за 518.6 с), меш копий
freecad 150.9 MiB 381 с вместо 332 с, IGES 274 MiB 157 с вместо 136 с. Пик RSS конвейера при 4 GiB: `memory.peak` включает page cache,
первый повтор ячейки может показывать RSS заметно выше `ru_maxrss`; берётся большее, поэтому floor и множители
консервативны.

### Числа PreviewGenerator: только ориентир

| Величина | Значение | Использование |
|---|---|---|
| Потолок входа STEP | 40 МБ | ориентир PreviewGenerator (`evidence/71-06-limits.txt#YARDSTICK`), точка сравнения для D-06, не источник потолка |
| Вторая величина | 5 МБ | ориентир PreviewGenerator (текст `REQUIREMENTS.md`, EVAL-03; назначение величины там не расшифровано), не использована |
| Время | 60 с | ориентир PreviewGenerator (тот же текст), не использовано: таймаут замеряет фаза 76 |
| Порог | 500 Б | ориентир PreviewGenerator (тот же текст), не использован |

## Капы-кандидаты (D-10)

Байтовый потолок от бомб не защищает: на корпусе замерены счётчики, и капы выведены тем же линейным правилом
(`evidence/71-06-limits.txt#CAP`). **Кандидаты, не внедрение:** внедряют фаза 73 (гард-стадия, CADIN-04) и фаза 75
(DXF).

| Счётчик | Кандидат | Вывод | Последняя ступень с запасом | Первый OOM |
|---|---|---|---|---|
| треугольники рендера | 4014975 | 579.16 MiB на млн треугольников по худшему файлу (копии freecad 20 MiB, 873.7 тыс. треугольников, пик рендера 1023 MiB при `lp=2`), `(0.70 x 4096 - 517.0 - 24.9) / 579.16` | 8.8 млн | 16.1 млн (файл на 5000 экземпляров) |
| сущности STEP | 754762 | 3132.51 MiB на млн сущностей, худший файл 9 MB | 1.51 млн (полный конвейер, стопки копий) | нет |
| экземпляры NAUO | 1518 | 1.56 MiB на экземпляр при n=1000 | 2500 | 5000 (рендер) |
| развёрнутые вставки DXF (столбцы x строки MINSERT) | 170031 | 0.02 MiB на вставку | 100000 (78.3 с, 1719 MiB) | 1000000 |

Прежние кандидаты при `par=on,lp=default` (история): 4266184 / 759890 / 2042 / 172677 (`evidence/71-06-ladder-4g-ocp7.9.3.1.1.txt`).
Кап треугольников стал ниже из-за `lp=2` (рендер при `lp=2` ест больше памяти), кап NAUO - из-за множителя 1.56 против 1.16
MiB на экземпляр при n=1000 (меш при `par=off` на том же файле даёт 2035 MiB пика). Последняя ступень сущностей STEP
стала 1.51 млн вместо 3.92 млн: точки без замеренного рендера (стопки LinearSlide 150 и 200 MiB) больше не считаются
пройденным конвейером (`partial`); значение капа от этого не меняется (оно линейное).

Оговорки: у NAUO узкое место - время (стенное время меша при `par=off` 56.4 с на 1000, 123.4 с на 2500, 249.4 с на 5000;
10000 экземпляров обрываются по таймауту 900 с, при `par=on` меш на 10000 проходил за 518.6 с), память ломается на
рендере; в публичном корпусе нет ни одного MINSERT, кап DXF выведен из
сгенерированной серии `dxf-minsert` (максимум INSERT в реальных файлах - 7); таймауты ячейки в значения капов не
входят. Все четыре кандидата считаются при `cpus=2` и 4096 MiB.

## Потоки и тесселяция

- `LP_NUM_THREADS=2` (llvmpipe) и `occt_parallel=off` при `--cpus 2` (`evidence/71-06-limits.txt#THREADS_DECISION`,
  первичные строки `evidence/71-06-threads.txt#THREADS_ROW`). Правило: сумма медиан стенного времени по тяжёлым
  входам, все варианты в пределах 10% от быстрейшего считаются равными, тогда выигрывает меньший `user+sys`, затем
  меньше потоков. Меш: параллельно 123.71 с, выключено 126.21 с, affinity 118.96 с по сумме; `user+sys` 146.52 /
  126.11 / 139.03 с; потоков 11 / 1 / 11. Рендер: `LP_NUM_THREADS` 1 / 2 / по умолчанию - 9.77 / 8.63 / 8.87 с,
  значение 1 медленнее на 13%, значение 2 лучше по `user+sys` (10.80 против 11.48 с) и по потокам (15 против 33).
  Выбор потоков не пересматривался. Цена выбора по памяти (`evidence/71-06-threads.txt#THREADS_ROW`,
  `evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#DIST`): `lp=2` поднимает RSS рендера (STL 100 MiB 873 -> 1113 MiB),
  `par=off` опускает RSS меша и удлиняет меш на больших входах (см. «Память и потолки»); перемер 4 GiB учёл обе
  стороны.
- Константы тесселяции остаются стартовыми значениями из `STACK.md` (`evidence/71-06-limits.txt#QUALITY`):
  draft 0.004 / 0.5, standard 0.001 / 0.35, fine 0.0003 / 0.2. На самом тяжёлом реальном STEP (9 MB) fine даёт
  820472 треугольников (20.4% капа рендера 4014975; до перемера 19.2% от 4266184), пик конвейера 896 MiB (31% бюджета 4g, 2867 MiB), draft - 273208
  треугольников и 669 MiB; вырожденные фикстуры дают 2 / 100 / 12 треугольников и все 63 рендера проходят
  пиксельный критерий (`evidence/71-06-quality.txt#QUALITY_ROW`). Замечаний на чекпоинте 71-05 не было.
  Три строки `QUALITY` составлены вручную из `QUALITY_ROW` и капа (`evidence/71-06-quality.txt#QUALITY`). Пики конвейера
  этого пункта (896 и 669 MiB) сняты при `par=on,lp=default` и не перемерялись; доли от капа и бюджета - ориентир.
- **Наборы Landlock ниже получены именно с `LP_NUM_THREADS=2` и `occt_parallel=off`.** При других настройках
  могут понадобиться `/sys/devices/system/cpu` и `/proc/cpuinfo`.

## Образ (EVAL-03, D-15, D-17)

Замер: `scripts/cad-image-size-measure.sh`, `evidence/71-08-image-size.txt`; pull в пустой одноразовый демон
(новый контейнер dind на каждый pull, `fresh_daemon_per_pull=1`, `evidence/71-08-image-size.txt#DIND`) через
локальный `registry:2`; `registry` и `docker:dind` закреплены по digest индекса
(`evidence/71-08-image-size.txt#REGISTRY`). Драйвер хранилища dind - `overlayfs` (снапшоттер containerd), не vfs, так
что время репрезентативно для этого хоста (`evidence/71-08-image-size.txt#DIND`).

| Вариант | Без сжатия, MiB | Сжато по манифесту, MiB | Слоёв | Cold pull, с (loopback) |
|---|---|---|---|---|
| pruned (финальный) | 1083.0 | 299.3 | 6 | медиана 6.38 (мин 6.04, макс 7.11, n=3) |
| full (та же сборка) | 1358.3 | 366.7 | 6 | 7.75 (n=1) |

Источник: `evidence/71-08-image-size.txt#IMAGE_SIZE`, `evidence/71-08-image-size.txt#LAYER`,
`evidence/71-08-image-size.txt#COLD_PULL`, `evidence/71-08-image-size.txt#COLD_PULL_DIST`. В каждой строке
`COLD_PULL` `fetched` равен `layers`, `already_exists=0`: ни один слой не был в демоне. Разница full и pruned
(рассчитано) - 275.3 MiB без сжатия, совпадает с `delta_image_mib=275.3` из
`evidence/71-07-prune.txt#PRUNE_SIZE`, и 67.4 MiB в сжатом виде.

**Время pull на loopback - это скачивание без сети плюс распаковка, а не сетевое время.** Сетевое время рассчитано
(`evidence/71-08-image-size.txt#PULL_CALC`, сжатые байты x 8 / полоса, рассчитано): для pruned 25.1 с при 100 Мбит/с,
2.5 с при 1 Гбит/с, 0.3 с при 10 Гбит/с; для full 30.8 / 3.1 / 0.3 с. Полное время холодного узла примерно равно
сетевому времени плюс распаковка из `COLD_PULL`; это оценка, а не замер сети. Образ собран и замерен на arm64;
размер amd64-сборки не замерен.

Сравнение с флотом на этом хосте (как есть, не пересобиралось, `evidence/71-08-image-size.txt#FLEET`): самый
большой образ флота - `octoconv-document-worker` 733.0 MiB, затем `octoconv-audio-worker` 655.1 и
`octoconv-chromium-worker` 650.3; образ cad (pruned, 1083.0 MiB) больше любого из них.

**Порога на размер нет (D-15).** Размер записан, выбор рендерера им не меняется
(`evidence/71-08-image-size.txt#D15`); цена передана фазе 76 как вход для `keda.cad.minReplicaCount`: образ
1083.0 MiB без сжатия, 299.3 MiB сжатый, cold pull 6.38 с на loopback плюс сетевое время из таблицы выше.

**Обрезка VTK по keep-list (D-17).** `scripts/cad-measure/vtk_keeplist.py collect` собирает список из
`/proc/self/maps` всех веток рендера: 77 записей `vtkmodules/`, повторная генерация побайтно равна
(`evidence/71-07-libtrace.txt#KEEPLIST_REGEN`, `evidence/71-07-libtrace.txt#COLLECTED`). `prune` режет VTK в
стадии venv до `COPY`: 564.1 -> 288.7 MiB, образ 1358.3 -> 1083.0 MiB, слоя с whiteout нет
(`evidence/71-07-prune.txt#PRUNE_SIZE`). 196 из 196 PNG полного и обрезанного образов побайтно равны
(`evidence/71-07-prune.txt#PRUNE_IDENTITY`), 48 из 48 строк остальных операций совпали
(`evidence/71-07-prune.txt#PRUNE_OTHER`). Устаревший список: `prune` отказывает при другой версии VTK
(`evidence/71-07-prune.txt#STALE_TEST`, `case=version`, rc 1); если в списке не хватает модуля, `prune` проходит,
а рендер под nobody падает с `No module named 'vtkmodules.vtkFiltersCore'` (`case=missing_module`) - поэтому
рендер-тест фазы 74 обязан запускаться на обрезанном образе, это основа его проверки. Механизм - основа
рендер-теста фазы 74: тест падает на устаревшем списке.

## Лицензии (EVAL-04, D-18)

Результат трассы (D-18, `evidence/71-07-libtrace.txt#D18_TRACE`): `import OCP` грузит `libfreeimage` и `libraw` на
обоих пинах через `DT_NEEDED` (цепочка `libTKService -> libfreeimage -> libraw`), `libjasper` - только на
8.0.1.0.0 (`libraw -> libjasper`); на 7.9.3.1.1 из 38 операций 7 грузят `libfreeimage` и `libraw`,
`libjasper_ops=0`. Рендер, DXF, `rsvg-convert` и `stlstats` не грузят ни одну из трёх. Все 68 из 68 файлов колеса
7.9.3.1.1 загружаются при `import OCP` (`UNLOADED` пуст), поэтому удалять нечего
(`evidence/71-07-licences.txt#D18_OUTCOME`: `removed=none,reason=loaded_at_import`).

**Выбор пользователя по D-18: `record`** (`evidence/71-01-decisions.txt`, `71-01-SUMMARY.md`): библиотеки остаются,
пишется инженерная запись, эксперимент `patchelf` не проводился (`experiment=not_run`). «Загружена» не значит
«вызывается» (допущение A1 остаётся допущением: ответить может только эксперимент `patchelf`).

Таблица лицензий (`evidence/71-07-licences.txt#LICENCE`; VERIFIED - ожидаемый текст лицензии найден по адресу
источника во время выборки, это опознание лицензии, а не разрешение на использование):

| Компонент | Лицензия | Статус | Пин |
|---|---|---|---|
| OCCT | LGPL-2.1 + исключение Open CASCADE 1.0 (читано на тегах V7_9_3 и V8_0_1) | VERIFIED | оба |
| OCP | Apache-2.0 | VERIFIED | оба |
| FreeImage | GPL-2.0 / GPL-3.0 / FIPL-1.0 (двойная, выбор за пользователем библиотеки) | VERIFIED | оба |
| LibRaw | LGPL-2.1 / CDDL-1.0 (одна из двух) | VERIFIED | оба |
| JasPer | JasPer-2.0 | VERIFIED | только 8.0.1.0.0 |
| jbigkit (libjbig0) | GPL-2.0, or-later на странице не сказано | VERIFIED | 7.9.3.1.1 |
| libgomp | GPL-3.0 + GCC Runtime Library Exception | VERIFIED | оба |
| zstd | BSD-3-Clause / GPL-2.0 | VERIFIED | 7.9.3.1.1 |
| OpenEXR, libpng, libjpeg-turbo, libtiff, lcms2, openjpeg, libwebp, jxrlib, freetype, fontconfig, xz, libuuid | пермиссивные или FTL/IJG-класса (подробно в строках `LICENCE`) | VERIFIED | оба |
| bzip2, fmt | bzip2-1.0.6, MIT | VERIFIED | только 8.0.1.0.0 |

ASSUMED-строк нет: после двух правок шаблонов поиска (libuuid, jbigkit) все 23 строки получили VERIFIED
(`evidence/71-07-licences.txt#LICENCE`). Допущение A3 закрыто чтением текста исключения; допущение A4 закрыто
просмотром апстримов, но его формулировка «разрешительные/LGPL-класса» подтвердилась не полностью: copyleft
несут также jbigkit (GPL-2.0) и libgomp (GPL-3.0 с исключением среды выполнения GCC).

Инженерная запись (`evidence/71-07-licences.txt#RECORD`): **только внутреннее использование, образ не
распространяется вовне.** **Допущение A2** (внутреннее использование без распространения не запускает
обязательств copyleft у FreeImage, LibRaw, jbigkit, libgomp) - рабочее допущение, а не юридический вывод.
**Нужен OK юриста** (`LAWYER_OK=needed,blocking=no`): библиотеки юридически не очищены, пересмотр запрошен и фазу
не блокирует. Если образ когда-нибудь выйдет за границу компании, запись теряет силу и вопрос нужно открыть
заново. Эксперимент `patchelf` не проводился.

## Кандидаты Landlock для фазы 73 (кандидат, не грант)

Пути получены на нативном arm64 под пользователем образа с реальным набором правил Landlock, боевым argv
(`/opt/venv/bin/python -I -B ...`) и содержимым-судьёй (сигнатура результата, а не код возврата)
(`evidence/71-07-landlock.txt#LANDLOCK_SUMMARY`: 4 хелпера, 34 входа, все 34 достаточны, `different=0`, `leaked=0`,
58 строк таблицы удаления). **Это кандидаты, не гранты.** Минимальные наборы (`evidence/71-07-landlock.txt#LANDLOCK_MINIMAL`),
запись `rw=/job`:

| Хелпер | Минимальный набор ro |
|---|---|
| `cad_mesh` | `/opt/cad-measure`, `/opt/venv`, `/usr/bin/python3.11`, `/usr/lib/aarch64-linux-gnu`, `/usr/lib/python3.11` |
| `cad_render` | те же пять |
| `cad_dxf` | те же пять плюс `/usr/share/fonts` |
| `rsvg-convert` | `/usr/bin/rsvg-convert`, `/usr/lib/aarch64-linux-gnu` |

- **Следы над-отчитывают:** таблица удаления убрала `/etc/ld.so.cache`, `/etc/nsswitch.conf`, `/etc/passwd`,
  `/usr/lib/locale`, `/usr/share/zoneinfo`, `/usr/local/share`, `/sys/devices/system/cpu`, `/proc/cpuinfo`,
  `/proc/filesystems`, `/proc/self/maps` (`evidence/71-07-landlock.txt#REMOVAL`).
- **/proc, /sys, /tmp:** ни одному набору не нужны (`evidence/71-07-landlock.txt#PROC_SYS` `entries=none`,
  `evidence/71-07-landlock.txt#TMP_NEED` `none`).
- **Эффективность:** все четыре строки `EFFECT` - снаружи без Landlock читается, под Landlock отказано
  (`evidence/71-07-landlock.txt#EFFECT`); у `cad_mesh` и `cad_render` под отказом хелпер завершается кодом 2 без
  текста про права, контроль без Landlock на том же файле проходит.
- **`/usr/share/fonts`** для `cad_dxf` обязателен молча: без него `legacy_cp1251.dxf` даёт код 0 и другой SVG
  (поэтому достаточность проверялась по содержимому).
- **Зависимости:** наборы получены при `LP_NUM_THREADS=2` и `occt_parallel=off`; пересмотреть при смене набора
  шрифтов и при смене потоков.
- Повторная проверка на текущем образе: `--recheck` дал `RECHECK-OK` для всех четырёх хелперов (`71-07-SUMMARY.md`).

## Черновик словарей для фазы 72 (окончательно закрепляет MEAS-05)

**Единицы (с происхождением).** Что встретилось в 46 STEP, 4 IGES, 17 DXF корпуса
(`evidence/71-03-corpus-inventory.txt#CNT`, `evidence/71-04-helpers.txt#UNITS`):

| Источник | Наблюдённые значения | Не встречалось |
|---|---|---|
| текст STEP | `SI:MILLI_METRE` (39 файлов), `CBU:INCH` (4), `CBU:inch` (1), `CBU:MILLIMETRE` (2) | сантиметры, метры, футы, микрометры |
| текст IGES | `unit_flag=2`, `unit_name=MM` (4 файла) | прочие флаги |
| DXF `$INSUNITS` | 1 (дюймы, 9 файлов), 4 (мм, 2), 6 (м, 2), нет (4) | прочие коды |

Предлагаемое происхождение: текст файла, заголовок `$INSUNITS`, «не указано». Предлагаемый словарь единиц:
mm, cm, m, inch, foot, micrometre, unknown; встречались mm, inch и (в DXF) m, остальные по корпусу не проверены.
**Расхождение с `FileUnits`:** OCP возвращает пустое имя для `CONVERSION_BASED_UNIT('inch')` в нижнем регистре
(`nist_ctc_05_asme1_ap242-e1.stp`, `unit_agree=0`), поэтому единица берётся из текста файла, а расхождение с
`FileUnits` - флаг, не источник истины; масштабирование геометрии OCP для этого файла не доказано (доказано только
для `units_inch.step`: 6 in^3 читается как 98322.384 мм^3) (`evidence/71-04-helpers.txt#FINDING`).

**Коды причин null-объёма** (`evidence/71-04-helpers.txt#ROW`, `evidence/71-04-helpers.txt#FINDING`):

| Код | Встретился на |
|---|---|
| `no_solids` | `surfaces_only.iges`, `iges_no_newlines.iges`, `open_shell.step`, `single_face.step` |
| `stl_not_watertight` | `nasa-hubble-all-pieces.stl`, `nasa-jwst-all-pieces-without-supports.stl` |
| `open_shell`, `invalid_solid`, `negative_volume` | **не встречались:** коды есть в `cad_mesh.py`, но OCP читает открытую оболочку как 0 тел, а `reversed_solid.step` как объём +6 (ориентацию чинит ридер) |

Нуль в `volume` не пишется никогда: объём и площадь только по замкнутым валидным телам, иначе `null` с кодом.

**Ракурсы - семь** (RNDR-02, `evidence/71-04-render.txt#VIEWDEF`): iso (1,-1,1), front (0,-1,0), back (0,1,0),
left (-1,0,0), right (1,0,0) с up (0,0,1); top (0,0,1) с up (0,1,0); bottom (0,0,-1) с up (0,-1,0).
Константы камеры: ортографическая проекция, расстояние 2 x диагональ bbox, поле по умолчанию 0.06, угол
feature edges 30, заливка 0.8:0.8:0.8, FXAA включён.

**Прочие наблюдения для фазы 72** (`evidence/71-04-helpers.txt#FINDING`): STEP не несёт кодировку, `legacy_cp1251.step`
читается как latin-1 (`Äåòàëü`), хотя геометрия читается; сниффер не должен заявлять 8-bit кодировку STEP по
тексту; DXF-близнецы декодируются через `$DWGCODEPAGE`. `BRepBndLib.Add_s` на `V-Slot-20x20-100mm.step` вернул
бокс ±1e100: фаза 73 не должна выводить отклонение из непроверенного bbox всей формы.

## Корпус (EVAL-02, D-01..D-05)

Состав и лицензия каждого файла - `internal/convert/testdata/cad/PROVENANCE.md`, таблица Coverage. Сводка по
группам (`evidence/71-03-corpus-inventory.txt#INV`): **committed 37, fetched 17, scaled 33**; счётчики D-10 на 87
файлах (`evidence/71-03-corpus-inventory.txt#CNT`: 46 STEP, 4 IGES, 20 STL, 17 DXF).

- **Committed (D-03):** малые файлы лежат в `internal/convert/testdata/cad/` с `PROVENANCE.md` (источник, автор,
  лицензия, sha256 на каждый файл). Размер проверен командой `git ls-files` при записи: все файлы вместе 7038250
  байт, наибольший `nasa-jwst-all-pieces-without-supports.stl` 1404884 байт - внутри порога A5.
- **Fetched (D-03):** 17 больших файлов закреплены в `scripts/cad-corpus-manifest.tsv` по sha256 и лицензии, качает
  `scripts/cad-corpus-fetch.sh` в кэш вне репозитория; несовпадение хеша - ошибка и удаление файла
  (`71-03-SUMMARY.md`, прогон с подменой).
- **Derived (не коммитятся):** 33 детерминированные ступени до 200 MiB STEP, бинарного и ASCII STL, 50 MiB DXF и
  100000 NAUO; каждая подписана базой и коэффициентом (`PROVENANCE.md`, раздел Derived).
- **Synthetic:** 18 фикстур делает детерминированный генератор `scripts/cad-corpus-gen.py`, он закоммичен раньше
  кода, который их проверяет; cp1251 и Latin-1 (STEP и DXF) только синтетические.
- **Лицензии (D-04):** допускаются только PD, CC0, CC-BY, MIT, BSD; атрибуция CC-BY в `PROVENANCE.md`.
  Замечания для пользователя, не блокирующие: NIST PMI-файлы опираются на формулировку «можно использовать без
  ограничений» и федеральное авторство; у NASA есть сторонние вкладчики без атрибуции по файлам; авторство
  отдельных деталей FreeCAD-library не проверено по файлам (A8) (`71-03-SUMMARY.md`).
- **Не найдено в открытом виде** (`PROVENANCE.md`, раздел «Not found in open form»): реальный STEP или DXF с
  сырым cp1251 или Latin-1; реальный IGES из поверхностей не из FreeCAD; DXF с размерами от CAD-экспортёра под
  разрешённой лицензией; DXF с MINSERT; открытый STEP или STL выше около 52 MB; разнообразие экспортёров среди
  малых деталей (все детали FreeCAD-library экспортированы FreeCAD).
- **Приватный поток PreviewGenerator (D-01, D-02): реальный поток не замерен.** Каталог не задан или пуст,
  `PRIVATE_CORPUS,files=0,status=реальный поток не замерен`
  (`evidence/71-03-corpus-inventory.txt#PRIVATE_CORPUS`, повторено в `evidence/71-06-limits.txt#PRIVATE_CORPUS`).
  Это «не проверялось», а не «чисто»: ни потолки, ни капы, ни время не проверены на реальных файлах компании.
  Механизм готов и проверен на подставных файлах: с одним файлом `SECRET-client-x.step` в evidence попадают
  только sha256, формат, размеры и счётчики без имени и содержимого (`71-03-SUMMARY.md`, `71-06-SUMMARY.md`).

## Пороги и допущения

Пороги: коммит - 2 MiB на файл и 15 MiB суммарно (**A5, остаётся допущением**,
`evidence/71-03-corpus-inventory.txt#INV`); пиксельные пороги - в разделе «Рендерер»
(`evidence/71-04-render.txt#CAL`); переход пина - три порога в разделе «Ядро»; выбор потоков - допуск 10% с
тай-брейком по `user+sys` (`evidence/71-06-limits.txt#THREADS_DECISION`); порог раздельных потолков STL - 25%
расхождения k (`evidence/71-06-limits.txt#STL_K`, расхождение 79.9%); порог 5% для обнаружения whiteout (выбор планировщика) - разность образов 275.3 MiB против разности `vtkmodules` 275.4 MiB, расхождение 0.01% (`evidence/71-07-prune.txt#PRUNE_SIZE`, `gap_pct_of_delta_vtk=0.01`, `whiteout_free=1`).

Журнал допущений:

| # | Допущение | Статус | Ссылка |
|---|---|---|---|
| A1 | `libTKService` не вызывается на пути STEP/IGES/STL, `patchelf` работоспособен | остаётся допущением (загружена, вызов не доказан, эксперимент не проводился) | `evidence/71-07-licences.txt#D18_OUTCOME` |
| A2 | внутреннее использование без распространения не запускает copyleft | остаётся допущением, нужен OK юриста | `evidence/71-07-licences.txt#RECORD` |
| A3 | OCCT: LGPL 2.1 + исключение | закрыто замером (текст исключения прочитан на V7_9_3 и V8_0_1) | `evidence/71-07-licences.txt#LICENCE` |
| A4 | остальные библиотеки колеса разрешительные/LGPL-класса | закрыто замером (все VERIFIED), формулировка уточнена: jbigkit GPL-2.0 и libgomp GPL-3.0 с исключением | `evidence/71-07-licences.txt#LICENCE` |
| A5 | порог малого файла 2 MiB и 15 MiB суммарно | остаётся допущением (корпус укладывается, порог не пересматривался) | `evidence/71-03-corpus-inventory.txt#INV` |
| A6 | пиксельные пороги критериев | закрыто замером (калибровка по реальным деталям; добавлены режимы «сливер» и «чертёж») | `evidence/71-04-render.txt#CAL`, `evidence/71-04-render.txt#CALOBS` |
| A7 | RSS Go-воркера около 60 MiB | закрыто замером: 24.9 MiB (воркер класса image как аналог) | `evidence/71-06-go-worker-rss.txt#GO_WORKER` |
| A8 | происхождение файлов FreeCAD-library не нарушает CC-BY 3.0 | остаётся допущением (по файлам не проверено) | `internal/convert/testdata/cad/PROVENANCE.md` |
| A9 | имена пакетов существуют, хеши колёс подтверждены | закрыто замером (хеши aarch64 совпали с независимым замером) и одобрено пользователем | `evidence/71-01-lock.txt#CROSSCHECK` |
| A10 | лестница размеров поймает обрыв | закрыто частично: обрывы пойманы при 1 и 2 GiB и на IGES, NAUO, MINSERT; на 4 GiB (`par=off,lp=2`) STEP-меш, STL и DXF не ломаются до верхней ступени около 200 MiB, полный конвейер STEP проходит до 150.9 MiB; потолки STL выше верхней ступени - экстраполяция | `evidence/71-06-limits.txt#CEILING`, `evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#LADDER_STOP` |

## Не замерено

- **amd64.** Гейта на amd64 нет, выбор amd64-хоста за пользователем (фаза 70, D-03/D-05); эмуляция не даёт Landlock,
  такой замер считается за отсутствие замера. Размер образа и cold pull замерены только для arm64.
- **Приватный поток** - реальный поток не замерен (раздел «Корпус»).
- **f3d** на нативном arm64 и под Landlock: сборки нет (`evidence/71-05-f3d.txt#F3D_ASSETS`).
- **Юридическая чистота лицензий** - нужен OK юриста (раздел «Лицензии»).
- **Эксперимент `patchelf`** - не проводился (D-18 выбран как `record`).
- **Повторы у потолка при 2 и 1 GiB:** обе серии шли с одним повтором; ступени, которые проходят при 90% лимита,
  одиночные замеры. Реальные файлы с пиком RSS при 4 GiB ниже 40% лимита пропущены (`--floor-from`) и принятые
  проходящими выведены, а не измерены (`evidence/71-06-ladder-2g.txt#LADDER_STOP`, поле `floor_skipped`).
- **Потолки выше верхней ступени 4 GiB** для STL бинарного и ASCII - экстраполяция (раздел «Память и потолки»).
- **Время рендера на входах со стеком копий** - известный риск без кода защиты (фаза 76).
- **Лестницы 2 GiB, 1 GiB и пина 8.0.1.0.0 при `par=off,lp=2`:** не перемерялись (решение пользователя: только 4 GiB на
  7.9.3.1.1). Они остаются историей `par=on,lp=default` (`evidence/71-06-limits.txt#CONFIG_ROWS`), в числа не входят;
  выбор L = 4096 от них не зависит (`evidence/71-06-limits.txt#L_SENSITIVITY`).
- **Сдвиги потолков STEP (73 -> 63) и DXF (217 -> 118)** идут от точек у floor под правилом худшего файла, а не от
  настройки потоков (раздел «Память и потолки»); реальный наклон даёт около 73 и 218 MiB.
- **Шум хоста:** единичные замеры времени и сравнение пина по порогу (c) несут шум соседних контейнеров;
  сравнение 5.9% лежит внутри него (`evidence/71-06-ladder-4g-ocp7.9.3.1.1.txt#NOISE`; перемер 4 GiB: `evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#NOISE`, 137 запусков, сосед в среднем 14.4% ядра, пик 143.6%).
- **Cold pull** `full`: один прогон, не распределение (n=1); сеть не замерена, только рассчитана.
- **Отклонения планов, записанные в SUMMARY:** `71-04-SUMMARY.md` (нет `text_fonts.dxf` в корпусе),
  `71-06-SUMMARY.md` (три строки `QUALITY` составлены вручную), `71-07-SUMMARY.md` (выход 3 `cad_render` недостижим из
  STL), `71-08-SUMMARY.md` (свежий демон dind на каждый pull вместо `rmi` и `prune` внутри одного).

## Покрытие источников

| Пункт | Раздел DECISION | Evidence | Статус |
|---|---|---|---|
| GOAL (выбор ядра, рендерера, 2D-пути; числа до кода) | все разделы, сводка | `evidence/71-06-limits.txt#PIN_DECISION`, `evidence/71-04-render.txt#RENDER_SUMMARY` | покрыто |
| EVAL-01 | Ядро, Рендерер, 2D-путь DXF | `evidence/71-06-limits.txt#PIN_DECISION`, `evidence/71-07-landlock.txt#LANDLOCK_SUMMARY` | покрыто (amd64 не замерен) |
| EVAL-02 | Корпус | `evidence/71-03-corpus-inventory.txt#INV` | покрыто; приватный поток не замерен |
| EVAL-03 | Память и потолки, Образ | `evidence/71-06-limits.txt#CEILING`, `evidence/71-08-image-size.txt#COLD_PULL_DIST` | покрыто |
| EVAL-04 | Лицензии | `evidence/71-07-licences.txt#LICENCE` | покрыто как инженерная запись; нужен OK юриста |
| D-01 | Корпус | `evidence/71-03-corpus-inventory.txt#INV` | покрыто для публичного; приватный не замерен |
| D-02 | Корпус, Память и потолки | `evidence/71-03-corpus-inventory.txt#PRIVATE_CORPUS` | покрыто формулировкой «реальный поток не замерен» |
| D-03 | Корпус | `evidence/71-03-corpus-inventory.txt#INV` | покрыто |
| D-04 | Корпус | `internal/convert/testdata/cad/PROVENANCE.md` | покрыто |
| D-05 | Память и потолки | `evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt#LADDER_STOP` | покрыто (потолки выше верхней ступени помечены) |
| D-06 | Память и потолки | `evidence/71-06-limits.txt#MEMORY_LIMIT_DECISION` | покрыто |
| D-07 | Память и потолки | `evidence/71-06-limits.txt#LIMIT` | покрыто |
| D-08 | Память и потолки | `evidence/71-06-limits.txt#CEILING` | покрыто |
| D-09 | Память и потолки | `evidence/71-06-limits.txt#CEILING` | покрыто (потолок STEP выше 40 MiB, формулировка не применяется) |
| D-10 | Капы-кандидаты | `evidence/71-06-limits.txt#CAP` | покрыто (кандидаты, внедряют фазы 73 и 75) |
| D-11 | Рендерер | `evidence/71-04-render.txt#RENDER_SUMMARY`, `71-05-SUMMARY.md` | покрыто (`approved`) |
| D-12 | Рендерер | `evidence/71-04-render.txt#VIEWDEF` | покрыто |
| D-13 | Рендерер | `evidence/71-04-render.txt#TRANSPARENT` | покрыто (белый фон, прозрачный проверен) |
| D-14 | Рендерер | `evidence/71-04-render.txt#VIEWDEF` | покрыто |
| D-15 | Образ | `evidence/71-08-image-size.txt#D15` | покрыто |
| D-16 | Рендерер | `evidence/71-07-landlock.txt#LANDLOCK_SUMMARY` | покрыто (запасной рендерер не нужен) |
| D-17 | Образ | `evidence/71-07-prune.txt#PRUNE_IDENTITY` | покрыто |
| D-18 | Лицензии | `evidence/71-07-licences.txt#D18_OUTCOME` | покрыто (`record`) |
| RESEARCH: рубрика выбора пина | Ядро | `evidence/71-06-limits.txt#PIN_FORMULA` | покрыто |
| RESEARCH: допущения A1-A10 | Пороги и допущения | `evidence/71-06-limits.txt#CEILING` | покрыто, у каждого статус |
| RESEARCH: открытый вопрос 1 (D-18 удалением невыполним) | Лицензии | `evidence/71-07-libtrace.txt#EXPECTATION` | покрыто, `record` |
| RESEARCH: открытый вопрос 2 (Landlock для 2D-пути) | Кандидаты Landlock | `evidence/71-07-landlock.txt#LANDLOCK_MINIMAL` | покрыто |
| RESEARCH: открытый вопрос 3 (файлы компании) | Корпус | `evidence/71-03-corpus-inventory.txt#PRIVATE_CORPUS` | не замерено: каталог пуст |
| RESEARCH: открытые вопросы 4-5 (место корпуса, реальные cp1251 и IGES) | Корпус | `internal/convert/testdata/cad/PROVENANCE.md` | покрыто: корпус в `testdata/cad`, реальных cp1251 и IGES-из-поверхностей не из FreeCAD нет |
| RESEARCH: открытый вопрос 6 (репрезентативность NASA STL) | Память и потолки | `evidence/71-06-limits.txt#LIMIT` | покрыто: кривая STL дополнена плитками и реальным STL, потолки выше верхней ступени помечены |
| Критерий 5 (нет продакшен-проводки) | следующий раздел | `evidence/71-08-no-wiring.txt#NO-WIRING` | покрыто |

## Критерий 5

Продакшен-проводки нет: относительно коммита 4e595c0 нет изменённых и добавленных файлов в `internal` (кроме
`internal/convert/testdata/cad/`), `cmd`, `deploy`, `frontend`, `docs`, `README.md`, `go.mod`, `go.sum`,
`docker-compose*.yml`, `.github` и продакшен-Dockerfile; существующие скрипты не тронуты; защитные тесты
`AllEngines()`, `/v1/formats`, `/v1/inspect` и очереди проходят на go1.26.4 - `evidence/71-08-no-wiring.txt`
(последняя строка `NO-WIRING`; гейт проверен на исходном дереве и в обратную сторону).

## Индекс артефактов фазы

- **Скрипты:** `scripts/cad-measure/` (`lock.sh`, `common.sh`, `measure_wrap.py`, `cad_mesh.py`, `cad_render.py`,
  `cad_dxf.py`, `pixelcheck.py`, `contact_sheet.py`, `counters.py`, `vtk_keeplist.py`, `vtk-keeplist.txt`, lock-файлы),
  `scripts/cad-rss-time-measure.sh`, `scripts/cad-render-measure.sh`, `scripts/cad-corpus-gen.py`,
  `scripts/cad-corpus-fetch.sh`, `scripts/cad-corpus-manifest.tsv`, `scripts/cad-corpus-inventory.sh`,
  `scripts/cad-f3d-yardstick.sh`, `scripts/cad-go-worker-rss.sh`, `scripts/cad-derive-limits.py`,
  `scripts/cad-render-stacking-probe.py`, `scripts/cad-image-trace.sh`, `scripts/cad-sandbox-measure.sh`,
  `scripts/cad-image-size-measure.sh`.
- **Образ:** `Dockerfile.cad-measure` (стадии `venv`, `venv-pruned`, `venv-selected`).
- **Корпус:** `internal/convert/testdata/cad/` (`public/`, `synthetic/`, `PROVENANCE.md`); большие файлы - кэш вне
  репозитория (`OCTOCONV_CAD_CACHE`), приватный каталог - `OCTOCONV_CAD_PRIVATE_CORPUS`, не коммитится.
- **Evidence:** `evidence/71-01-lock.txt`, `evidence/71-01-decisions.txt`, `evidence/71-02-tracer.txt`,
  `evidence/71-03-corpus-inventory.txt`, `evidence/71-04-helpers.txt`, `evidence/71-04-render.txt`,
  `evidence/71-04-contact-views.png`, `evidence/71-04-contact-robustness.png`, `evidence/71-04-contact-quality.png`,
  `evidence/71-04-contact-dxf.png`, `evidence/71-05-f3d.txt`, `evidence/71-05-contact-f3d.png`,
  `evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt` (источник лимитов), `evidence/71-06-ladder-4g-ocp7.9.3.1.1.txt` (история
  `par=on,lp=default`), `evidence/71-06-ladder-4g-ocp8.0.1.0.0.txt`,
  `evidence/71-06-ladder-2g.txt`, `evidence/71-06-ladder-1g.txt`, `evidence/71-06-threads.txt`,
  `evidence/71-06-quality.txt`, `evidence/71-06-go-worker-rss.txt`, `evidence/71-06-render-stacking.txt`,
  `evidence/71-06-limits.txt`, `evidence/71-07-libtrace.txt`, `evidence/71-07-prune.txt`,
  `evidence/71-07-licences.txt`, `evidence/71-07-landlock.txt`, `evidence/71-08-image-size.txt`,
  `evidence/71-08-no-wiring.txt`.
