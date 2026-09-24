# Что каждому движку нужно читать — замеры

Подготовка к фазе, закрывающей
`.planning/todos/pending/2026-09-06-other-engines-still-unconfined.md`. Сам
todo говорит, что настоящая работа здесь — узнать, что каждому бинарнику нужно
читать, и что отвечать на это следует прогоном, а не рассуждением. Это файл
ответов.

Инструмент: `scripts/sandbox-paths-measure.sh` (две фазы) и
`scripts/cmd/sandbox-probe`.

## Метод, и почему одной фазы мало

**`trace`** прогоняет настоящую конверсию под `strace` и печатает пути, которые
движок успешно открыл. Это ОБНАРУЖЕНИЕ кандидатов, и оно **недосчитывает**.

**`verify`** строит из кандидатов настоящий Landlock-ruleset, прогоняет под ним
те же конверсии, а затем повторяет одну с входным файлом ВНЕ каталога задачи.
Достаточность решает код возврата движка; действенность — провал последнего
прогона.

### Ловушки, каждую вскрыл прогон или ревью, а не рассуждение

**1. Исполняемый файл.** Первый набор для libvips выглядел полным — библиотеки,
`ld.so.cache`, пара записей `/proc` и `/sys`. Под настоящим ruleset **все пять
пар упали** с `exec: permission denied`: `/usr/bin/vips` не появляется в трассе
`openat` ни разу, потому что его не открывают, а `execve`'ят.

Обобщать это в правило нельзя, и первая редакция этого документа обобщила зря:
**chromium свой бинарник открывает**, потому что пере-`exec`ает себя для
дочерних процессов. Правило звучит иначе — трасса про исполняемый файл ничего
не обещает ни в ту, ни в другую сторону, и добавлять его в набор надо руками.

**2. Обёртка и интерпретатор.** `/usr/bin/chromium-headless-shell` — это 61 байт
`#!/bin/sh`, а не бинарник. Гранта на него мало: нужны ещё `/usr/bin/dash` и
настоящий `/usr/lib/chromium/chromium-headless-shell`. До прогона это выглядело
как «движок выдан».

**3. Трасса шла под root, а движок работает под `nobody`.** Контейнер целиком
запускался с `--user root`, чтобы поставить strace, — и трассируемая команда
тоже. Chromium под root сообщал `/root/.config` и `/root/.fontconfig`, которых
рабочий процесс не касается. Теперь strace ставится под root, а сама команда
запускается под пользователем образа через `setpriv`; после фикса путей под
`/root` — ноль (codex, PR #27).

**4. Фильтр трассы врал в обе стороны.** `strace -f` чередует процессы и рвёт
вызов на `<unfinished ...>` и `<... resumed>`: первая строка несёт путь и не
несёт результата, поэтому неуспешное открытие просачивалось в набор «успешных»
(311 таких строк на один прогон chromium — так в кандидаты попал
несуществующий `/etc/vulkan`, и `Apply` упал на нём). Лечится `-ff` (файл на
процесс, чередования нет) и приёмом только результата `= <неотрицательное>`.

**5. Канарейка, ставшая бесполезной.** Пробник читает файл вне грантов и по
неудаче заключает, что конфайнмент есть. Но неудача доказывает это, только если
без ruleset чтение бы удалось: опечатка в `PROBE_CANARY`, каталог вместо файла
или файл, который этот пользователь и так не читает, дают ошибку, к Landlock
отношения не имеющую. Теперь канарейка читается ДО `Apply` (иначе отказ), а
после — требуется именно отказ по правам (codex, PR #27).

**6. Пробник собирался под архитектуру СЕРВЕРА, а не образа.** Для
`document-worker`, закреплённого на amd64, на arm64-хосте это дало бы
нативный arm64-пробник: Landlock установился бы успешно, эмулировался бы только
движок, и скрипт выдал бы набор путей вместо единственно верного ответа
«Landlock здесь недоступен». Теперь архитектура берётся из образа (codex, PR #27).

**7. Трасса записывает путь ДО разыменования, Landlock смотрит ПОСЛЕ.**
`/usr/share/ghostscript/10.00.0/iccprofiles` — симлинк на `/usr/share/color`,
а `Resource/Font/NimbusSans-Regular` — на `/usr/share/fonts`. Грант на
`/usr/share/ghostscript` не покрывает ни того, ни другого. Разыменование
самого гранта `sandbox.Apply` делает (потому и работал `/etc/localtime`,
который тоже симлинк) — не покрывается симлинк ВНУТРИ выданного каталога.
Фаза `trace` теперь печатает `путь -> цель`, когда они различаются.

**8. «Файл создан» и «файл непустой» — не проверка содержимого.** Ghostscript
на запрет чтения выдаёт скелет PostScript **того же размера**, что и при
успехе: 166046 байт и там, и там. Я на этом основании отчитался «ps2write OK»,
и это было неверно — круг давал PDF без единого слова исходного текста.
Настоящий критерий: прогнать `pdftotext` и сравнить текст. Тот же капкан ранее
поймал меня на chromium, где вывод отличался размером и я проверил содержимое;
здесь размер совпал, и проверка по размеру дала ложное «да».

Отсюда порядок: трасса даёт гипотезу, ruleset выносит приговор, а приговор
выносится по содержимому результата. И отдельно — инструмент сам нуждается в
проверке: шесть из восьми ловушек выше нашлись в нём или в моей проверке, а не
в движках.

**Добавлено фазой 68.** Набор измеряется через РЕАЛЬНУЮ последовательность
argv задачи, а не только главный движок: OCR-повтор нашёл `pdfinfo` и
`pdftotext`, которых не было в наборе, потому что первый замер прогонял
`tesseract`/`pdftoppm` напрямую, а не тот же путь, что проходит PDF-задача
через `ocrPDFGuardStage`. И вывод, зависящий от шрифта, судится по
содержимому, а не по коду возврата: `pdftoppm` без `/usr/share/fonts`
рендерит другую картинку с кодом 0, а `tesseract` на ней тоже с кодом 0
возвращает пустой текст.

## Где замер возможен

Замерено 2026-09-19 (подробности и таблица — в todo): Landlock работает в
нативных контейнерах и **недоступен эмулируемому amd64-процессу** на arm64-хосте.

| Сервис | Платформа | Замер здесь |
|---|---|---|
| worker, chromium-worker, audio-worker, av-worker, ocr-worker, pandoc-worker, probe-worker | нативная | **возможен** |
| document-worker | `platform: linux/amd64` (ради `verapdf/cli`, amd64-only) | **невозможен**, нужен amd64-узел |

## Что уже внедрено

| Класс | Замерен | Внедрён |
|---|---|---|
| `tectonic` | — (PR #21) | **да** |
| `image` | да | **да** (эта ветка) |
| `pandoc`, `ocr`, `av`, `audio` | да | **да (фаза 68)** |
| `html`, `probe` | да | нет (фаза 69) |
| `document` | нельзя на этом хосте | нет (фаза 70) |

Общая часть вынесена: `sandbox.ResolveHelper` (разрешение хелпера и отказ
стартовать), `engineSandbox.wrapEngine` (сборка argv), `engineExec.run`/`.stream`
(единый путь exec'а: каждая задача получает свой ruleset ровно один раз) и
preflight-набор (`sandbox.Preflight`-подобные проверки, доказывающие на
реальном движке и на реальном хелпере, что конверсия внутри каталога задачи
проходит, а файл снаружи — запрещён). Фаза 68 держит это тестом
(`internal/convert/sandboxcoverage_test.go`): любой exec движка в
`internal/convert`, не прошедший через `engineExec`, ломает сборку, если файл
не отнесён явно к классу будущей фазы. Каждому движку остаётся свой список
путей и свой preflight.

## image (libvips) — замерено и доказано 2026-09-20

Образ `octoconv-worker:latest`, движок `vips copy`.

**Read-write:** каталог задачи.

**Read-only:**

| Путь | Зачем |
|---|---|
| `/usr/bin/vips` | сам движок; в трассе НЕ виден |
| `/lib` | разделяемые библиотеки (125+ на одну конверсию) |
| `/usr/lib` | загрузчики форматов, включая heif |
| `/etc/ld.so.cache` | единственное, что нужно из `/etc` |
| `/proc/self/auxv`, `/proc/self/status` | |
| `/proc/filesystems`, `/proc/cpuinfo` | |
| `/sys/devices/system/cpu/online`, `/sys/devices/system/cpu/possible` | определение числа ядер |

Из `/etc` не нужно ничего, кроме `ld.so.cache`. `/tmp` не нужен. Домашний
каталог не нужен.

**Достаточность.** Пять пар форматов под этим ruleset:
`png→webp` (1868 б), `png→jpg` (3500 б), `png→tiff` (48210 б),
`jpg→png` (756 б), `png→heic` (2730 б) — все `rc=0` с непустым выходом.

**Действенность.** Тот же движок, тот же образ, входной файл
`/var/tmp/outside.png`:

- без ruleset — прочитан, 1868 байт на выходе;
- под ruleset — `unable to open for read / unix error: Permission denied`,
  выходного файла нет.

То есть конфайнмент наследуется через `execve` и действует на сам движок, а не
только на процесс, который ruleset установил.

## html (chromium) — замерено и доказано 2026-09-20

Образ `octoconv-chromium-worker:latest`, argv как в `internal/convert/chromium.go`.

**Read-write:** каталог задачи, **`/tmp`**, `/dev/null`.

`/tmp` не убирается в каталог задачи: chromium кладёт туда разделяемую память
(`/tmp/.org.chromium.Chromium.*`), и `TMPDIR=/job/tmp` этого **не меняет** —
проверено, движок падает с `Creating shared memory in /tmp/... failed:
Permission denied` и `GPU process isn't usable`. Файлы транзитные: после
прогона в `/tmp` их ноль. Для фазы это вопрос к решению, а не к замеру: общий
`/tmp` означает, что параллельные задачи видят файлы друг друга. Варианты —
свой tmpfs на `/tmp` в контейнере, либо снять `--disable-dev-shm-usage` и
выдать `/dev/shm` (с ним набор проходит и без него, так что флаг сейчас держит
chromium именно в `/tmp`).

**Read-only:**

| Путь | Зачем |
|---|---|
| `/usr/bin/chromium-headless-shell` | обёртка `#!/bin/sh`, 61 байт |
| `/usr/bin/dash` | её интерпретатор |
| `/lib`, `/usr/lib` | библиотеки и настоящий бинарник в `/usr/lib/chromium` |
| `/usr/share`, `/usr/local/share` | шрифты |
| `/etc/fonts`, `/etc/ld.so.cache`, `/etc/localtime`, `/etc/nsswitch.conf`, `/etc/host.conf`, `/etc/hosts`, `/etc/resolv.conf` | поимённо, а не `/etc` целиком |
| `/proc`, `/sys/devices` | самоинспекция и число ядер |
| `/dev/urandom` | |

`/dev/shm` и `/run` в наборе **не нужны** — проверено снятием.

**Достаточность.** PDF под ruleset — 22074 байта, столько же, сколько без него.
Остаётся одна безобидная ошибка: `Failed to adjust OOM score of renderer` —
это запись в `/proc`, выданный только на чтение.

**Действенность, и почему «файл не создан» здесь не годится как критерий.**
Под ruleset chromium **всё равно произвёл PDF** из файла вне каталога задачи —
652 байта. Соблазн заключить «утечка» велик, и он был бы неверен:

| PDF | Размер | Текст внутри (`pdftotext`) |
|---|---|---|
| несуществующий файл, без ruleset | 652 б | пусто |
| настоящий файл, без ruleset | 22074 б | `Sandbox path probe…` |
| настоящий файл, **под ruleset** | **652 б** | **пусто** |

То есть под ruleset движок выдал ровно то же, что для отсутствующего файла.
Проверять надо содержимое, а не факт создания файла: chromium на отказ чтения
рендерит пустую страницу, а не падает.

## pandoc — замерено и доказано 2026-09-20

Образ `octoconv-pandoc-worker:latest`, argv как в `pandocConvertArgs`.

**Read-write:** каталог задачи. **Read-only:** `/usr/local/bin/pandoc`.

**И больше ничего.** Ни `/lib`, ни `/etc`, ни `/proc`. Причина — бинарник на
175 МБ слинкован статически (`ldd`: `not a dynamic executable`), так что фаза
`trace` честно вернула **ноль** путей вне каталога задачи. Пустой результат
здесь проверен, а не принят: сначала выяснена причина, потом он засчитан.

**Достаточность.** Пять целей из markdown под этим ruleset: `html` 4225 б,
`docx` 10847 б, `epub` 5091 б, `latex` 2103 б, `rst` 275 б — все непустые.

**Действенность.** Вход `/var/tmp/outside.md`: без Landlock прочитан (346 б на
выходе), под Landlock — `withBinaryFile: permission denied`.

**Чего этот замер НЕ показывает.** Собственный флаг pandoc `--sandbox` в том же
прогоне внешний файл прочитал — и это не его провал: он ограничивает ввод-вывод
«названными файлами», а внешний файл и был назван входом. Флаг существует
против файлов, на которые ссылается *документ*, и такой тест здесь не ставился.
Landlock ограничивает процесс независимо от argv, то есть закрывает другое и
более широкое.

### Замер фазы 68 — кандидаты

Коммит `f0c55718540c1ca14b5d9553961ab54a2257a5aa`, образ
`octoconv-pandoc-worker:measure68`
(`sha256:ce4e62d9cb98fe503674342b5ec12e2c088e2643949f7ef85e8a72baf1bd6f80`,
arm64). Повод для повторного замера — не сомнение в предыдущем результате, а
дисциплина: каждый класс фазы 68 проходит один и тот же протокол (полный
инвентарь argv, включая пары, не измерявшиеся 2026-09-20), чтобы конкретно
для pandoc подтвердить «и больше ничего» на девяти прогонах вместо пяти.

**Бинарник на job-пути:**

| Бинарник | Путь в образе |
|---|---|
| `pandoc` | `/usr/local/bin/pandoc` (не симлинк) |

**Трассированные argv** (девять: каждый читатель из `pandocPairTable` один раз
на допустимую цель, плюс `md→docx` и `md→epub` — структурные писатели с
`--standalone`; все с `--sandbox`, пути `/job`):

```
pandoc -f rst -t html --sandbox --standalone /job/in.rst -o /job/out_rst_html.html
pandoc -f org -t html --sandbox --standalone /job/in.org -o /job/out_org_html.html
pandoc -f latex-raw_tex -t html --sandbox --standalone /job/in.latex -o /job/out_latex_html.html
pandoc -f html-raw_html-raw_tex -t markdown --sandbox /job/in.html -o /job/out_html_md.md
pandoc -f docx -t markdown --sandbox /job/in.docx -o /job/out_docx_md.md
pandoc -f epub-raw_html-raw_tex -t markdown --sandbox /job/in.epub -o /job/out_epub_md.md
pandoc -f markdown-raw_html-raw_tex -t docx --sandbox --standalone /job/in.md -o /job/out_md_docx.docx
pandoc -f markdown-raw_html-raw_tex -t epub --sandbox --standalone /job/in.md -o /job/out_md_epub.epub
pandoc -f markdown-raw_html-raw_tex -t html --sandbox --standalone /job/in.md -o /job/out_md_html.html
```

**Кандидатный набор:** ноль путей вне `/job` на всех девяти прогонах — то же
самое «ничего», что и 2026-09-20. Новых путей нет («новое» отсутствует).

**Инструментальная находка (Rule 1, не про pandoc).**
`scripts/sandbox-paths-measure.sh`'s фаза `trace` при НУЛЕВОМ числе найденных
путей падала до печати финальной подсказки и до очистки `.trace.*`: конвейер
`grep '^/' | grep -v '^/job/' | sort -u` при отсутствии совпадений возвращает
код 1 от `grep`, а `pipefail` вместе с `set -e` останавливал скрипт посреди
дела. Пустой результат здесь — не ошибка, а ответ; добавлен `|| true` на весь
конвейер (`scripts/sandbox-paths-measure.sh`, ветка `trace`). Без этого фикса
не удалось бы завершить ни один из девяти прогонов pandoc.

**Достаточность** (содержимое, не код возврата — sha256 конфайнд vs
unconfined, тот же движок, тот же образ, набор `ro: /usr/local/bin/pandoc`):

| argv | Сравнение | Результат |
|---|---|---|
| `rst→html` | sha256 | равно, 4324 б |
| `org→html` | sha256 | равно, 4276 б |
| `latex→html` | sha256 | равно, 4399 б |
| `html→md` | sha256 | равно, 198 б |
| `docx→md` | sha256 | равно, 111 б |
| `epub→md` | sha256 | равно, 271 б |
| `md→docx` | sha256 (контейнер целиком) | равно, 11086 б |
| `md→epub` | sha256 (контейнер целиком) | равно, 5061 б |
| `md→html` | sha256 | равно, 4275 б |

**Необходимость через удаление.** Не применимо — кандидатный набор не вырос
относительно замера 2026-09-20 (`pandocReadOnly=[]`), удалять нечего.

**Действенность.** Вход `/var/tmp/outside/pandoc.md` (тот же markdown, что и
для трассы): без ruleset — прочитан, `pandoc.md→html` содержит "Sample
Heading" (grep находит совпадение); под ruleset (`ro:
/usr/local/bin/pandoc`) — `withBinaryFile: permission denied`, файл
`/job/eff_confined.html` не создан.

<!-- conf05:pandoc:readonly -->
<!-- /conf05:pandoc:readonly -->
<!-- conf05:pandoc:engines -->
pandoc
<!-- /conf05:pandoc:engines -->

## ocr (tesseract + pdftoppm) — замерено и доказано 2026-09-20

Образ `octoconv-ocr-worker:latest`. **Два** бинарника: PDF растеризуется
постранично через `pdftoppm` (`ocrRasterizePageArgs`), и только потом идёт
`tesseract`.

**Read-write:** каталог задачи.

**Read-only:**

| Путь | Зачем |
|---|---|
| `/usr/local/bin/tesseract`, `/usr/bin/pdftoppm` | оба движка |
| `/lib`, `/usr/local/lib` | библиотеки; `/lib` в этом образе — **симлинк на `usr/lib`** |
| `/usr/local/share/tessdata` | модели; в прогоне открылась только `eng.traineddata`, но выдавать надо каталог — рядом лежит `rus.traineddata`, и задача вправе её попросить |
| `/etc/ld.so.cache`, `/etc/localtime` | вторая — для `pdftoppm` |
| `/sys/devices/system/cpu/possible` | |

**Достаточность.** `tesseract` — 3/3 по коду возврата; `pdftoppm` — OK. Форматы
`txt`, `hocr` и `alto` проходят.

**Действенность.** Вход `/var/tmp/outside.png`: без ruleset распознан
(`Hello OctoConv OCR`), под ruleset — `Error during processing.`, выходного
файла нет.

### Две ошибки МОЕЙ проверки, не движка

**`alto` писался не туда, куда я смотрел.** Я счёл его провалившимся, потому
что искал `v.alto`, а tesseract для этого конфига пишет `outputbase.xml` —
файл был на месте, 1491 байт. Это прямо задокументировано в `ocr.go`
(«alto configfile writes outputbase.xml, NOT outputbase.alto»), то есть
проект предупреждал, а я проверил не тот объект.

**Критерий «файл существует» слабее кода возврата.** В разовых циклах я судил
по наличию файла на хосте через bind-mount и один раз получил провал
(`could not create TXT output file`), который не воспроизвёлся ни разу в 8
последующих прогонах того же набора и ни разу по коду возврата (3/3). Причина
не установлена — вероятнее всего задержка проброса файлов на macOS, а не
ruleset. Вывод для метода: судить по коду возврата движка, как это и делает
фаза `verify`; наличие файла на хосте — признак слабее.

### Замер фазы 68 — кандидаты

Коммит `f0c55718540c1ca14b5d9553961ab54a2257a5aa`, образ
`octoconv-ocr-worker:measure68`
(`sha256:3ef142879bc2e94f2056ac37acad9e2330c3ed0b2dfa56dcd0fb071d21a5238f`,
arm64).

**Повод.** Замер 2026-09-20 прогнал только `tesseract` и `pdftoppm` напрямую
как бинарники — не через реальный job-путь Go-кода. Но `ocrPDFGuardStage`
(`ocrpdf.go`) на КАЖДОЙ PDF-задаче сначала зовёт `pdfinfo` (счётчик страниц,
`ocrPDFPageCount`), а на PDF→txt — ещё и `pdftotext`
(`extractPDFTextLayer`); `ocrPDFPageSizeForPage` зовёт `pdfinfo` повторно,
на каждую страницу отдельно. Ни `pdfinfo`, ни `pdftotext` не были в
документированном наборе 2026-09-20 — задача pdf→txt/hocr/alto под старым
набором упала бы на первом же вызове `pdfinfo` с `exec: permission denied`.

**Бинарники на job-пути:**

| Бинарник | Путь в образе |
|---|---|
| `tesseract` | `/usr/local/bin/tesseract` |
| `pdftoppm` | `/usr/bin/pdftoppm` |
| `pdfinfo` | `/usr/bin/pdfinfo` — **новое**, ранее не измерялся |
| `pdftotext` | `/usr/bin/pdftotext` — **новое**, ранее не измерялся |

**Трассированные argv** (двенадцать: `pdfinfo` в форме счётчика страниц и
форме на одну страницу — на обоих PDF; `pdftotext` в стдаут на
born_digital.pdf; `pdftoppm` в `/job/pg/` на обоих PDF; `tesseract` на
отрендеренных страницах и на `text_sample.png`, для `txt`/`hocr`/`alto`, с
`-l eng` и один раз с `-l eng+rus`):

```
pdfinfo /job/born_digital.pdf
pdfinfo -f 1 -l 1 /job/born_digital.pdf
pdfinfo /job/multipage_image_only.pdf
pdfinfo -f 1 -l 1 /job/multipage_image_only.pdf
pdftotext /job/born_digital.pdf -
pdftoppm -r 300 -f 1 -l 1 -singlefile -png /job/born_digital.pdf /job/pg/bd_page
pdftoppm -r 300 -f 1 -l 1 -singlefile -png /job/multipage_image_only.pdf /job/pg/mi_page
tesseract /job/pg/bd_page.png /job/pg/bd_out --psm 3 --oem 1 -l eng
tesseract /job/pg/mi_page.png /job/pg/mi_out --psm 3 --oem 1 -l eng hocr
tesseract /job/pg/mi_page.png /job/pg/mi_out2 --psm 3 --oem 1 -l eng alto
tesseract /job/text_sample.png /job/pg/img_out --psm 3 --oem 1 -l eng
tesseract /job/text_sample.png /job/pg/img_out2 --psm 3 --oem 1 -l eng+rus
```

**Кандидатный набор** (пути вне `/job`, все двенадцать прогонов объединены):

`/etc/fonts/*` (fonts.conf, conf.d, conf.avail/*.conf) — **новое**,
`/etc/ld.so.cache`, `/etc/localtime`,
`/lib/aarch64-linux-gnu/*.so*` (покрывается грантом `/lib`),
`/sys/devices/system/cpu/possible`,
`/usr/local/lib/libleptonica.so.6`, `/usr/local/lib/libtesseract.so.5`
(покрывается грантом `/usr/local/lib`),
`/usr/local/share/fonts` — **новое** (пустой каталог),
`/usr/local/share/tessdata/configs/{alto,hocr}`,
`/usr/local/share/tessdata/{eng,rus}.traineddata`
(покрывается грантом `/usr/local/share/tessdata`),
`/usr/share/fontconfig/conf.avail/*.conf` — **новое**,
`/usr/share/fonts`, `/usr/share/fonts/truetype/dejavu/*.ttf` — **новое**.

Находка совпадает с ожиданием исследования фазы: образ несёт
`/etc/fonts` и `/usr/share/fonts/truetype/dejavu`, и born-digital PDF с
невстроенным Helvetica заставляет `pdftoppm` их открыть — заменить шрифт
нечем без файлов подстановки.

**Достаточность** (содержимое, не код возврата — конфайнд vs unconfined,
кандидатный набор целиком: `/lib:/usr/local/lib:/usr/local/share/tessdata:
/etc/ld.so.cache:/etc/localtime:/etc/fonts:/usr/share/fonts:
/usr/share/fontconfig/conf.avail:/usr/local/share/fonts:
/sys/devices/system/cpu/possible` + четыре движка):

| argv | Сравнение | Результат |
|---|---|---|
| `pdfinfo born_digital.pdf` | sha256 стдаут | равно |
| `pdfinfo -f 1 -l 1 born_digital.pdf` | sha256 стдаут | равно |
| `pdfinfo multipage_image_only.pdf` | sha256 стдаут | равно |
| `pdfinfo -f 1 -l 1 multipage_image_only.pdf` | sha256 стдаут | равно |
| `pdftotext born_digital.pdf -` | sha256 стдаут | равно |
| `pdftoppm` born_digital.pdf → PNG | sha256 | **равно** (рендер со шрифтом-заменой побайтово идентичен) |
| `pdftoppm` multipage_image_only.pdf → PNG | sha256 | равно |
| `tesseract` born_digital page → txt | текст | равно: «This is a born-digital PDF with real extractable text. OctoConv OCR guard test fixture - Phase 43.» |
| `tesseract` multipage page → hocr | текст без тегов (bbox/ppageno/scan_res) | равно; единственная разница в сырых байтах — встроенный путь к файлу (`/job/base/...` vs `/job/full/...`), артефакт замера, не движка |
| `tesseract` multipage page → alto | текст без тегов (`<fileName>`) | равно, та же причина расхождения байтов |
| `tesseract` text_sample.png `-l eng` → txt | текст | равно: «Hello OctoConv OCR» |
| `tesseract` text_sample.png `-l eng+rus` → txt | текст | равно: «Hello OctoConv OCR» |

**Необходимость через удаление** (по одному пути кандидатного набора, на
`pdftoppm born_digital→PNG` + `tesseract →txt`, сравнение по sha256 PNG и по
тексту):

| Путь | Убран | Результат |
|---|---|---|
| `/etc/fonts` | да | PNG равно, текст равно — **не нужен** |
| `/usr/share/fontconfig/conf.avail` | да | PNG равно, текст равно — **не нужен** |
| `/usr/local/share/fonts` (пустой каталог) | да | PNG равно, текст равно — **не нужен** |
| `/usr/share/fonts` | да | **PNG другой** (иной sha256), **текст пустой** — код возврата и `pdftoppm`, и `tesseract` остался 0 на обоих. **Нужен.** |

Последняя строка — та самая ловушка ghostscript (пункт 8 «Метода») в новом
месте: без `/usr/share/fonts` `pdftoppm` рендерит другую (пустую от текста)
картинку с кодом возврата 0, а `tesseract` на ней тоже с кодом 0 возвращает
ПУСТОЙ текст — то же самое «файл создан, содержимое неверно», которое
проверка по коду возврата не увидит.

**Итоговый набор** (кандидатный минус три доказанно ненужных):
`/lib`, `/usr/local/lib`, `/usr/local/share/tessdata`, `/etc/ld.so.cache`,
`/etc/localtime`, `/usr/share/fonts`, `/sys/devices/system/cpu/possible` +
движки `tesseract`, `pdftoppm`, `pdfinfo`, `pdftotext`. Повторно все
двенадцать argv под ЭТИМ итоговым набором — те же результаты, что в таблице
достаточности выше (включая побайтовое совпадение `bd_page.png` и текст
born-digital после замены шрифта).

**Действенность** (вход `/var/tmp/outside/ocr.pdf` = born_digital.pdf,
`/var/tmp/outside/ocr.png` = text_sample.png, итоговый набор):

| Движок | Внешняя фикстура | Без ruleset | Под ruleset |
|---|---|---|---|
| `pdfinfo` | ocr.pdf | читает метаданные | `I/O Error: Couldn't open file ... Permission denied.` |
| `pdftoppm` | ocr.pdf | пишет PNG (68974 б) | `I/O Error: ... Permission denied.`, файла нет |
| `tesseract` | ocr.png | «Hello OctoConv OCR» | `Error, cannot read input file ...: Permission denied` / `Error during processing.`, файла нет |

<!-- conf05:ocr:readonly -->
/lib
/usr/local/lib
/usr/local/share/tessdata
/etc/ld.so.cache
/etc/localtime
/usr/share/fonts
/sys/devices/system/cpu/possible
<!-- /conf05:ocr:readonly -->
<!-- conf05:ocr:engines -->
tesseract
pdftoppm
pdfinfo
pdftotext
<!-- /conf05:ocr:engines -->

## av (ffmpeg + ffprobe) — замерено и доказано 2026-09-20

**Read-write:** каталог задачи. **Read-only:** `/usr/local/bin/ffmpeg`,
`/usr/local/bin/ffprobe`, `/lib`, `/etc/ld.so.cache`, `/dev/urandom`,
`/proc/self/status`, `/proc/sys/vm/overcommit_memory`,
`/sys/devices/system/cpu/{online,possible}`.

**Достаточность:** `mp4→webm` 48574 б, `mp4→mp3` 16839 б, `mp4→jpg` 9254 б,
`ffprobe` печатает `h264`/`aac`. **Действенность:** вход `/var/tmp/outside.mp4` —
`Error opening input: Permission denied`, выходного файла нет.

Фикстуру пришлось сгенерировать: `scripts/fixtures/inspect-burst-pathological.mp4`
намеренно битый (он для нагрузочных тестов), и на нём замер измерял бы разбор
ошибки, а не конверсию.

### Замер фазы 68 — кандидаты

Коммит `f0c55718540c1ca14b5d9553961ab54a2257a5aa`, образ
`octoconv-av-worker:measure68`
(`sha256:311f90024a04933d3496be0e24f919fb5027909b9d8de1e1322d3cf0f11f91c2`,
arm64). Повод — замер 2026-09-20 не прогонял ffprobe'овский probe аудио-кодека
(`ffprobeAudioCodecArgs`, AVC-05 stream-copy проверка), прогресс-обёрнутый
транскод, hevc-ветку, извлечение аудио во все три формата, все три миниатюры
и argv генерации lavfi-источника preflight'а (план 04).

**Бинарники на job-пути:**

| Бинарник | Путь в образе |
|---|---|
| `ffmpeg` | `/usr/local/bin/ffmpeg` |
| `ffprobe` | `/usr/local/bin/ffprobe` |

**Трассированные argv** (тринадцать: три формы `ffprobe`; `mp4`-транскод с
`-progress file:/job/progress.txt` спереди — обычный файл вместо FIFO, саму
FIFO доказывает preflight плана 04; `mp4→webm`; hevc-транскод; извлечение
аудио в mp3/wav/m4a; миниатюры в jpg/png/webp; argv генерации lavfi-источника
из инвентаря):

```
ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 -protocol_whitelist file,crypto file:/job/in.mp4
ffprobe -v error -select_streams v -show_entries stream=index,codec_name,width,height:stream_disposition=attached_pic -of json -protocol_whitelist file,crypto file:/job/in.mp4
ffprobe -v error -select_streams a:0 -show_entries stream=codec_name -of csv=p=0 -protocol_whitelist file,crypto file:/job/in.mp4
ffmpeg -progress file:/job/progress.txt -y -nostdin -protocol_whitelist file,crypto -i file:/job/in.mp4 -map 0:0 -map 0:a:0? -c:v libx264 -preset veryfast -crf 23 -c:a aac -b:a 128k -movflags +faststart -threads 2 file:/job/out_transcode_mp4.mp4
ffmpeg -y -nostdin -protocol_whitelist file,crypto -i file:/job/in.mp4 -map 0:0 -map 0:a:0? -c:v libvpx-vp9 -b:v 1M -c:a libopus -threads 2 file:/job/out_transcode.webm
ffmpeg -y -nostdin -protocol_whitelist file,crypto -i file:/job/in.mp4 -map 0:0 -map 0:a:0? -c:v libx265 -preset veryfast -crf 28 -c:a aac -b:a 128k -movflags +faststart -threads 2 file:/job/out_hevc.mp4
ffmpeg -y -nostdin -protocol_whitelist file,crypto -i file:/job/in.mp4 -vn -c:a libmp3lame -q:a 2 file:/job/out_extract.mp3
ffmpeg -y -nostdin -protocol_whitelist file,crypto -i file:/job/in.mp4 -vn -c:a pcm_s16le file:/job/out_extract.wav
ffmpeg -y -nostdin -protocol_whitelist file,crypto -i file:/job/in.mp4 -vn -c:a copy file:/job/out_extract.m4a
ffmpeg -y -nostdin -protocol_whitelist file,crypto -ss 1 -i file:/job/in.mp4 -map 0:0 -frames:v 1 -c:v mjpeg -q:v 2 file:/job/out_thumb.jpg
ffmpeg -y -nostdin -protocol_whitelist file,crypto -ss 1 -i file:/job/in.mp4 -map 0:0 -frames:v 1 -c:v png file:/job/out_thumb.png
ffmpeg -y -nostdin -protocol_whitelist file,crypto -ss 1 -i file:/job/in.mp4 -map 0:0 -frames:v 1 -c:v libwebp file:/job/out_thumb.webp
ffmpeg -f lavfi -i testsrc=size=64x64:rate=10:duration=1 -c:v libx264 -pix_fmt yuv420p file:/job/out_lavfi.mp4
```

**Кандидатный набор.** Все пути вне `/job` — под уже документированными
директориями (`/lib/aarch64-linux-gnu/lib{c,gcc_s,m,mp3lame,numa,opus,stdc++,
vpx,webp,x264,x265,z}.so*`, покрывается `/lib`; `/etc/ld.so.cache`;
`/dev/urandom`; `/proc/self/status`; `/proc/sys/vm/overcommit_memory`;
`/sys/devices/system/cpu/{online,possible}`). Новых путей нет («новое»
отсутствует) — набор совпадает с версией 2026-09-20.

**Достаточность** (содержимое — конфайнд vs unconfined, набор из уже
документированного read-only + `/usr/local/bin/ffmpeg` +
`/usr/local/bin/ffprobe`):

| argv | Сравнение | Результат |
|---|---|---|
| `ffprobe` duration | sha256 стдаут | равно |
| `ffprobe` stream (json) | sha256 стдаут | равно |
| `ffprobe` audio-codec | sha256 стдаут | равно |
| `mp4`-транскод (progress) | сводка ffprobe (кодек/ширина/высота/sample_rate/channels/duration) | равно |
| `mp4→webm` | сводка ffprobe | равно |
| hevc-транскод | сводка ffprobe | равно |
| extract→mp3 | sha256 (контейнер целиком) | **равно** |
| extract→wav | сводка ffprobe | равно |
| extract→m4a (stream-copy, source уже aac) | сводка ffprobe | равно |
| thumb→jpg | sha256 | равно |
| thumb→png | sha256 | равно |
| thumb→webp | sha256 | равно |
| lavfi-источник | сводка ffprobe | равно |

**Необходимость через удаление.** Не применимо — кандидатный набор не вырос
относительно 2026-09-20, удалять нечего.

**Действенность** (вход `/var/tmp/outside/av.mp4` = копия `in.mp4`):

| Движок | Без ruleset | Под ruleset |
|---|---|---|
| `ffprobe` (duration) | `2.000000` | `Error opening input: Permission denied` (206 codes) — нет вывода |
| `ffmpeg` (миниатюра) | пишет jpg (9016 б) | `[in#0] Error opening input: Permission denied`, файла нет |

<!-- conf05:av:readonly -->
/lib
/etc/ld.so.cache
/dev/urandom
/proc/self/status
/proc/sys/vm/overcommit_memory
/sys/devices/system/cpu/online
/sys/devices/system/cpu/possible
<!-- /conf05:av:readonly -->
<!-- conf05:av:engines -->
ffmpeg
ffprobe
<!-- /conf05:av:engines -->

## audio (whisper.cpp + ffmpeg) — замерено и доказано 2026-09-20

Две стадии: нормализация через ffmpeg, затем `whisper-cli`.

**Read-write:** каталог задачи. **Read-only:** `/usr/local/bin/whisper-cli`,
**`/usr/bin/ffmpeg`** (в этом образе ffmpeg лежит НЕ там, где в av-образе),
`/lib`, `/usr/lib`, `/usr/local/lib`, `/models`, `/etc/ld.so.cache`,
`/proc/cpuinfo`, `/sys/devices/system/cpu/{online,possible}`.

Трасса показывала открытия `/` и `/usr/local/bin/` как каталогов — проверено
снятием, без них всё работает.

**Достаточность:** нормализация 352078 б, распознавание 108 б
(`And so my fellow Americans ask not what…`). **Действенность:** вход извне
без ruleset распознаётся, под ruleset — `error: input file not found`.
Движок сообщает «не найден», а не «отказано в доступе»: по тексту его ошибки
отличить запрет нельзя.

### Замер фазы 68 — кандидаты

Коммит `f0c55718540c1ca14b5d9553961ab54a2257a5aa`, образ
`octoconv-audio-worker:measure68`
(`sha256:9a4b7fc398cd7e0d374eeff750f0a62e8fdfc1dff95ce79701fe65ccd1a6a5e1`,
arm64).

**Повод.** Замер 2026-09-20 прогнал нормализацию и распознавание, но не
`ffprobe`'овский guard длительности (`EnforceMaxDuration`), который
`internal/worker` зовёт из `enforceAudioGuardBeforeConvert` ДО `Convert` —
код в другом пакете, чем поле `Sandbox` конвертера. Этот замер прогоняет
именно его форму (`ffprobeDurationArgs`) на всех трёх типах входа этого
класса (wav, m4a, видео-контейнер), чтобы набор был готов покрыть и его —
решение о конфайнменте самого guard'а (вариант A/B из 68-RESEARCH.md) остаётся
за планом реализации, здесь только измерено, что ему нужно, если он тоже
пойдёт через `forJob`.

**Бинарники на job-пути:**

| Бинарник | Путь в образе |
|---|---|
| `ffmpeg` | `/usr/bin/ffmpeg` (apt, не там, где в av-образе) |
| `ffprobe` | `/usr/bin/ffprobe` — **новое**, ранее не измерялся (лежит рядом с `ffmpeg`, тот же apt-пакет) |
| `whisper-cli` | `/usr/local/bin/whisper-cli` |

**Трассированные argv** (шесть: форма ffprobe-guard'а на jfk.wav, sample.m4a
и in.mp4 — видео-контейнер, который этот класс тоже принимает; нормализация
ffmpeg jfk.wav→norm.wav; `whisper-cli` по `whisperArgs` на norm.wav для txt и
json):

```
ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 -protocol_whitelist file,crypto file:/job/jfk.wav
ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 -protocol_whitelist file,crypto file:/job/sample.m4a
ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 -protocol_whitelist file,crypto file:/job/in.mp4
ffmpeg -y -nostdin -protocol_whitelist file,crypto -i file:/job/jfk.wav -map 0:a:0 -ar 16000 -ac 1 -c:a pcm_s16le file:/job/norm.wav
whisper-cli -m /models/ggml-base.bin -f /job/norm.wav -of /job/out_txt -otxt -l auto -t 2
whisper-cli -m /models/ggml-base.bin -f /job/norm.wav -of /job/out_json -ojf -l auto -t 2
```

**Кандидатный набор** (сверх уже документированного): `/proc/filesystems` —
**новое**; `/proc/self/status` — **новое**; `/models/ggml-base.bin` (ФАЙЛ, не
каталог `/models`) — **новое по форме гранта**; `/usr/bin/ffprobe` —
**новое**, движок. `/` и `/usr/local/bin/` снова открылись как каталоги
(`whisper-cli`) — то же самое, что уже проверено и отброшено 2026-09-20.

**Достаточность** (содержимое — конфайнд vs unconfined, кандидатный набор:
уже документированный + `/proc/filesystems` + `/proc/self/status` +
`/models/ggml-base.bin` + `/usr/bin/ffprobe`):

| argv | Сравнение | Результат |
|---|---|---|
| `ffprobe` jfk.wav | sha256 стдаут | равно (`11.000000`) |
| `ffprobe` sample.m4a | sha256 стдаут | равно |
| `ffprobe` in.mp4 | sha256 стдаут | равно |
| `ffmpeg` нормализация | sha256 (PCM WAV) | равно |
| `whisper-cli` → txt | текст | равно: «And so my fellow Americans ask not what your country can do for you, ask what you can do for your country.» |
| `whisper-cli` → json | текст сегмента | равно, тот же транскрипт |

**Необходимость через удаление** (все шесть argv, попарно, сравнение по
sha256/тексту):

| Путь | Убран | Результат |
|---|---|---|
| `/proc/filesystems` | да | все шесть равны — **не нужен** |
| `/proc/self/status` | да | все шесть равны — **не нужен** |

**Итоговый набор:** `/lib`, `/usr/lib`, `/usr/local/lib`, `/etc/ld.so.cache`,
`/proc/cpuinfo`, `/sys/devices/system/cpu/online`,
`/sys/devices/system/cpu/possible` (статический список) + движки `ffmpeg`,
`ffprobe`, `whisper-cli` (разрешаются во время выполнения через PATH, как у
остальных классов) + модель `/models/ggml-base.bin` — ФАЙЛ, выдаваемый
отдельно от статического списка, во время выполнения, из
`AUDIO_MODEL_PATH` (по умолчанию тот же путь): грант на файл, а не на
каталог `/models` целиком, потому что путь к модели конфигурируем и не
известен статически на этапе объявления Go-переменной.

**Действенность** (вход `/var/tmp/outside/audio.wav` = копия jfk.wav,
итоговый набор):

| Движок | Без ruleset | Под ruleset |
|---|---|---|
| `ffprobe` | `11.000000` | `Permission denied`, нет вывода |
| `ffmpeg` | пишет norm.wav (352078 б) | `Permission denied`, файла нет |
| `whisper-cli` | «And so my fellow Americans…» | `error: input file not found`, файла нет — движок сообщает «не найден», а не «отказано», как и в замере 2026-09-20 |

<!-- conf05:audio:readonly -->
/lib
/usr/lib
/usr/local/lib
/etc/ld.so.cache
/proc/cpuinfo
/sys/devices/system/cpu/online
/sys/devices/system/cpu/possible
<!-- /conf05:audio:readonly -->
<!-- conf05:audio:engines -->
ffmpeg
ffprobe
whisper-cli
<!-- /conf05:audio:engines -->

## probe (gs + pdfinfo + unzip) — замерено и доказано 2026-09-20

Вопреки ожиданию, класс **не** чисто внутрипроцессный: `sanitizepdf.go` зовёт
`pdfinfo` и дважды `gs` (PDF → PostScript → PDF), `sanitizeverify.go` — `unzip`.

**Read-write:** каталог задачи, **`/tmp`** (gs пишет туда `gs_*`).

**Read-only:** `/usr/bin/{gs,pdfinfo,unzip}`, `/lib`, `/usr/lib`,
`/usr/share/ghostscript`, **`/usr/share/color`**, **`/usr/share/fonts`**,
`/var/lib/ghostscript`, `/etc/{ld.so.cache,localtime,papersize}`.

**Достаточность и действенность — по СОДЕРЖИМОМУ:** после круга под ruleset
`pdftotext` извлекает `This is a born-digital PDF with real extractable text`;
из файла вне каталога задачи, под тем же ruleset, — пусто.

## Ещё не замерено

Ничего из нативных. Остался только `document` (LibreOffice + veraPDF), и он
замеряется не здесь: `platform: linux/amd64`, а эмулируемому amd64 Landlock
недоступен.

`document` (LibreOffice) — только на amd64-узле, по причине выше.
