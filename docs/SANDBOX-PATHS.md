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
| worker, probe-worker | нативная | **возможен** |
| document-worker | нативная (с фазы 70 в `linux/amd64` закреплён только build-этап `verapdf`, а не образ) | **возможен** — `scripts/document-sandbox-measure.sh` |

document-worker до фазы 70 был примером образа, который здесь замерить нельзя:
весь образ был закреплён на `linux/amd64` ради `verapdf/cli`, а эмулируемому
amd64-процессу Landlock недоступен. Фаза 70 сняла закрепление (в `/opt/verapdf`
только jar-файлы, shell-скрипт и данные), и теперь образ собирается и запускается
нативно.

## Что уже внедрено

| Класс | Замерен | Внедрён |
|---|---|---|
| `tectonic` | — (PR #21) | **да** |
| `image` | да | **да** (эта ветка) |
| `pandoc` | да | **да (фаза 68)** |
| `ocr` | да (историческая запись ниже) | ruleset в `tileforge/octoconv-worker-ocr`; встроенный процесс удалён |
| `av` | да (историческая запись ниже) | ruleset в `tileforge/octoconv-worker-av`; встроенный процесс удалён |
| `audio` | да (историческая запись ниже) | ruleset в `tileforge/octoconv-worker-audio`; встроенный процесс удалён |
| `probe` | да | **да (фаза 69)** |
| `html` | да (историческая запись ниже) | ruleset в `tileforge/octoconv-worker-html`; встроенный процесс удалён |
| `document` | да (фаза 70, ниже) | **да (фаза 70)** |
| `cad` (guard + mesh) | да (фаза 73, ниже) | наборы лежат в `cadsandbox.go`; класс не зарегистрирован |

Общая часть вынесена: `sandbox.ResolveHelper` (разрешение хелпера и отказ
стартовать), `engineSandbox.wrapEngine` (сборка argv), `engineExec.run`/`.stream`
(единый путь exec'а: каждая задача получает свой ruleset ровно один раз) и
preflight-набор (`sandbox.Preflight`-подобные проверки, доказывающие на
реальном движке и на реальном хелпере, что конверсия внутри каталога задачи
проходит, а файл снаружи — запрещён). Фаза 68 держит это тестом
(`internal/convert/sandboxcoverage_test.go`): любой exec движка в
`internal/convert`, не прошедший через `engineExec`, ломает сборку, если файл
не отнесён явно к классу будущей фазы. Каждому движку остаётся свой список
путей и свой preflight. Фаза 69 добавила к общей части `forJobRW` (класс может
разделять с job dir ещё один writable-путь сверх самого каталога задачи —
сегодня использует только `html`, ради `/tmp`), `engineExec.withEnv`/`engineEnv`
(среда движка ЗАМЕНЯЕТСЯ целиком на allowlist `PATH`/`HOME`/`LANG`/`LC_ALL` +
опциональный `TMPDIR`, никогда не наследуется процессом-воркером; это закрывает
СОБСТВЕННЫЙ `/proc/self/environ` движка, а не чужие `/proc/<pid>/environ` — читаются ли
они, зависит от гранта `/proc` класса и замерено (N3, раздел «/proc и другие
процессы» в документе ниже; `tectonic` `/proc` не выдаёт вовсе,
`tectonicSystemReadOnly`)) и общий guard на разделяемый `/tmp`, ключ которого —
сам GRANT SET (любой класс, чей writable-набор содержит разделяемый `/tmp`),
а не имя класса. Allowlist `sandboxcoverage_test.go`'s `sandboxPendingFiles`
теперь пуст: каждый движковый класс в `internal/convert` идёт через
`engineExec`, и тест держит это для всего пакета. Фаза 70 добавила к общей части
две вещи, нужные только `document`: у него на каждую задачу **два ruleset** —
один у soffice, другой у veraPDF, каждый со своим замеренным набором (D-10;
veraPDF не получает ни права записи, кроме `/dev/null`, ни общего `/tmp`), — и
**право на сокет** (`sock:`, `Paths.Socket`): создать Unix-сокет под названной
директорией и больше ничего. Это единственное право, которого нет ни у одного
другого класса: оно не входит ни в `accessReadWrite`, ни в `accessReadOnly`, и
`TestNothingMayCreateSymlinksOrSpecialFiles` продолжает утверждать, что ни
каталог задачи, ни системное дерево сокет создать не могут.

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

Замер ниже — запись встроенного chromium-воркера. Этот процесс удалён. Ruleset теперь живёт в `tileforge/octoconv-worker-html`. Цифры ниже — историческая запись, их не пересчитывали.

Образ `octoconv-chromium-worker:latest`, argv того встроенного процесса, который этот раздел записывает.

**Read-write:** каталог задачи, **`/tmp`**, `/dev/null`.

`/tmp` не убирается в каталог задачи: chromium кладёт туда разделяемую память
(`/tmp/.org.chromium.Chromium.*`), и `TMPDIR=/job/tmp` этого **не меняет** —
проверено, движок падает с `Creating shared memory in /tmp/... failed:
Permission denied` и `GPU process isn't usable`. Файлы транзитные: после
прогона в `/tmp` их ноль. Общий `/tmp` означает, что параллельные задачи видят
файлы друг друга — это решение, а не замер; решение зафиксировано ниже, в
разделе «Решение по /tmp (CONF-06, фаза 69)».

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

**Переподтверждение на большом документе (G-69-3, 2026-09-27).** Замер выше
делался на маленькой фикстуре; здесь — на документе продакшен-масштаба через
конфайненный продакшен-путь `ChromiumConverter.Convert`, на лимитах
деплоя (`--cpus=2 --memory=2g`, те же, что у чарта
`chromiumWorker.resources.limits` и compose-сервиса `chromium-worker`):
HTML-фикстура из 18 секций с разрывом страницы, крупные несжимаемые PNG
(шум, зафиксированный сид) и таблица на 5000 строк — 84 543 422 байта
(~80,6 МиБ, между 64 и 100 МиБ, потолком загрузки html-движка). PDF —
20 460 780 байт (выше 10-МиБ порога, `/Title` с маркером на месте), 5,94 с.
Реальный `/dev/shm` контейнера в момент прогона —
`size=8212480k` (~7,8 ГиБ, дефолт docker/OrbStack на этой машине, а не
«~64Mi», которые предполагала сессия диагностики) — и это не имеет значения:
ruleset не выдаёт `/dev/shm` ни при каком размере. Повторяется постоянно
проверкой `scripts/sandbox-liveproof.sh html`, чек
`html large-document-without-dev-shm` (`internal/convert/chromium_live_test.go`,
`TestChromiumLargeDocumentNeedsNoDevShmLive`). С закрытием G-69-3 ни чарт, ни
compose, ни сама эта проверка больше не монтируют и не задают размер
`/dev/shm` для chromium-worker.

**Что делает Chromium с общей памятью, замерено заново (70-06, 2026-09-30).**
В фазе 69 негативный контроль этой проверки гонял движок без
`--disable-dev-shm-usage` и требовал, чтобы прогон УПАЛ на `/dev/shm`, пока
ruleset его не выдаёт (Chromium 153.0.8010.52 так и падал: `Creating shared
memory in /dev/shm/... failed: Permission denied`, код 133). На Chromium
154.0.8037.57 (Debian `154.0.8037.57-1~deb12u1`, образ из этого дерева) контроль
перестал падать, и gate покраснел (`liveproof-8class-70-05.txt`). Замер на
образе, собранном из этого дерева, `strace -ff` под пользователем образа, на
документе ~80 МиБ, на лимитах деплоя, семь условий (с флагом и без, без
ruleset, под продакшен-ruleset, под ним же плюс `rw:/dev/shm`, и `/dev/shm` в
64 МиБ): **ни в одном условии движок не создал ничего в `/dev/shm`** — 0
созданий, 0 отказов, 0 `mkdir`, 0 `unlink`; общая память берётся из
`memfd_create("shared-memory-region", ...)` (61 успешный вызов на прогон), и
прогон при отказанном `/dev/shm` завершается кодом 0 с PDF, несущим `/Title`
(20 454 608 байт). Флаг на этой версии лишь убирает один `statfs("/dev/shm")`;
он не несёт нагрузки. Поэтому вывод «зависимость от `/dev/shm` = прогон
падает при отказе» неверен: отказ — лишь одна из возможных реакций, а
движок может отступить в другой механизм. Классификация из замера:
`DEVSHM-WITH-FLAG-REACHABLE: unused`, `DEVSHM-NO-FLAG-REACHABLE: unused`,
`DEVSHM-NO-FLAG-DENIED: falls back to memfd_create`. Подробности и сырой вывод:
`.planning/phases/70-document-on-amd64/evidence/chromium-devshm-measure-70-06.txt`.

*Пересобранный контроль.* Проверка теперь не выводит зависимость из отказа, а
смотрит на `/dev/shm` напрямую: наблюдательный прогон идёт под продакшен-ruleset
плюс одна строка, `rw:/dev/shm` (только в тесте, `htmlReadWrite` не менялся), то
есть `/dev/shm` ДОСТУПЕН и никакой откат не скроет желание им пользоваться;
`inotify` в процессе теста, вне песочницы, должен увидеть запись, созданную
контрольной программой (`dash`, выданный `htmlReadOnly`, — движок сам на этой
версии ничего там не создаёт, поэтому флаг не несёт нагрузки), и не должен
увидеть ни одной записи от продакшен-argv. Счётчики печатаются строкой
`dev-shm-observe: control=<n> production=<n>`, версия движка — строкой
`chromium: <версия>`. Проверено инверсиями на собранном образе
(`liveproof-html-70-06.txt`): слепой наблюдатель роняет контроль
(«the method cannot see a /dev/shm dependency»), запись, появившаяся под
продакшен-прогоном, роняет продакшен-наблюдение. Инверсии «убрать
`rw:/dev/shm`» и «убрать флаг» на 154 провалиться не могут по построению (движок
ничего не создаёт при любом раскладе) и не запускались.

*Chromium не закреплён.* Пакет `chromium-headless-shell` в удалённом
`Dockerfile.chromium-worker` был без версии — решение пользователя
2026-09-30: Debian убирает старые версии, закрепление гниёт и оставляет браузер
без патчей. Проверка логирует версию, а контроль не опирается на поведение
конкретной версии. Непроверенным остаётся одно: поведение при запрещённом
`memfd_create` (например, seccomp-профилем) — в такой среде Chromium мог бы
вернуться к `/dev/shm`; в замеренном деплое (docker/OrbStack, профиль по
умолчанию) `memfd_create` доступен.

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

<!-- conf05:html:readwrite -->
/tmp
/dev/null
<!-- /conf05:html:readwrite -->
<!-- conf05:html:readonly -->
/usr/bin/dash
/lib
/usr/lib
/usr/share
/usr/local/share
/etc/fonts
/etc/ld.so.cache
/etc/localtime
/etc/nsswitch.conf
/etc/host.conf
/etc/hosts
/etc/resolv.conf
/proc
/sys/devices
/dev/urandom
<!-- /conf05:html:readonly -->
<!-- conf05:html:engines -->
chromium-headless-shell
<!-- /conf05:html:engines -->

## Решение по /tmp (CONF-06, фаза 69)

html держит `/tmp` на чтение-запись: chromium кладёт туда разделяемую память,
и `TMPDIR` её не уводит (замерено 2026-09-20, раздел html выше). Следствие
(D-01): одна задача на контейнер — `HTML_WORKER_CONCURRENCY` по умолчанию
равен `1`, пропускная способность масштабируется репликами, а не
внутрипроцессной параллельностью; уполовиненная пропускная способность на под
принята сознательно.

Инвариант держится отказом стартовать, а не комментарием (D-03): конфайненный
воркер, чей ruleset выдаёт `/tmp` на запись, а настроенная параллельность выше
1, отказывается стартовать и называет переменную и причину. Гард привязан к
НАБОРУ прав (любой класс, чей writable-набор содержит `/tmp`), а не к имени
класса.

Подметание `/tmp` перед каждой задачей (D-04): каждая запись `/tmp`, кроме той,
что держит каталог ТЕКУЩЕЙ задачи, удаляется до запуска движка. Это покрывает
файлы, оставленные предыдущей задачей, убитой по SIGKILL (таймаут), и каталог
задачи, оставленный воркер-процессом, который умер (Docker сохраняет `/tmp`
перезапущенного контейнера). Безопасно только благодаря D-01/D-03. Сбой
подметания — узловая неисправность: ретраится, а не вменяется файлу клиента.

Заметим, что воркер создаёт каждый каталог задачи под `os.TempDir()`
(`internal/worker/worker.go`), так что для html сам каталог задачи лежит
внутри выданного `/tmp`; при параллельности 1 единственная задача там —
текущая.

Отклонённые альтернативы с их замерами (D-02): (a) приватный `/tmp` на задачу
через mount namespace — измеримо невозможно под сиккомп-профилем Docker по
умолчанию: `unshare -Urm` возвращает `Operation not permitted`, и как root, и
как uid 65534 (Docker 29.4.0, `debian:bookworm-slim`, 2026-09-25); потребовал
бы отдельного seccomp-профиля в compose и чарте, что добавляет поверхность
атаки ядра именно тем контейнерам, которые мы конфайним; (b) Landlock
разрешает или запрещает пути и не умеет переназначать `/tmp`; (c) `TMPDIR` не
уводит разделяемую память chromium; (d) общий `/tmp` при параллельности 2 —
известный межзадачный путь чтения.

probe отказывается от `/tmp` (D-05), решено замером: с `TMPDIR=/job/tmp`
ghostscript создал и удалил все четыре файла `gs_*` за проход внутри него, и
не открыл ничего под голым `/tmp`, за оба прохода (strace -ff,
octoconv-probe-worker:measure69, 2026-09-25). С этим едут два условия
(реализуются планом 69-02): `sanitizePDF` и `verifyOOXML` создают свои
временные каталоги внутри каталога задачи, а `TMPDIR` выставляется в
окружении САМОГО движка на каждый exec, никогда не для всего процесса —
общий для процесса `TMPDIR`, указывающий внутрь каталога одной задачи, сломал
бы создание каталога следующей задачи, как только этот каталог удалён, и
гонялся бы при параллельности выше 1.

С пустым общим writable-набором параллельные задачи probe не делят ничего, так
что гард D-03 не срабатывает для probe по его собственному условию, и
подметание не нужно.

html-набор включает `/proc` целиком, а значит и `/proc/self/environ`; поэтому
движок получает окружение из белого списка (`PATH`, `HOME`, `LANG`, `LC_ALL`)
— тот же ответ, что tectonic дал на ту же замеренную утечку. Это закрывает
собственный `environ` движка; чужие записи `/proc` под тем же грантом замерены
один раз (N3, 70-08, образ из этого дерева, движок `/usr/bin/dash` через
`chromium-worker __sandbox-exec`, канарейка в окружении контейнера доказана без
ruleset): `/proc/1/environ`, `/proc/<ppid>/environ`, `/proc/self/root/etc/hostname` —
**отказ**; `/proc/1/cmdline`, `/proc/<ppid>/cmdline`, `/proc/<ppid>/status` —
**открылись**; свои `environ` и `cmdline` — открылись (`evidence/verapdf-proc-measure-70-08.txt`,
раздел HTML). Там же: сужать html так же, как veraPDF, нельзя по форме дела —
chromium пере-`exec`ает себя для дочерних процессов (Ловушки §1), и запись
`/proc/self/...`, резолвленная helper'ом в его pid, не покроет zygote, renderer и GPU:
у каждого свой pid. Что chromium читает в `/proc`, в этой фазе не трассировалось
(решение пользователя 3: не перемерять каждый класс). Остаток — в
`.planning/todos/pending/2026-09-30-proc-grant-other-process-entries.md`.

Внешний файл preflight'а html лежит под `/var/tmp`, потому что файл под `/tmp`
был бы внутри гранта.

## pandoc — замерено и доказано 2026-09-20

Замер ниже — запись встроенного pandoc-воркера. Этот процесс удалён. Ruleset теперь живёт в `tileforge/octoconv-worker-pandoc`. Цифры ниже — историческая запись, их не пересчитывали. Внешний замер образа `octoconv-pandoc:confinement-measure` (`sha256:b0f21dbad746564393fb755a93d086f41e9397c8470be6e17dae704c6bd8c892`, ядро `Linux version 7.2.12-orbstack-00394-gbecdd6130068`) дал выход 3429 байт. Протокольный набор после этого не стартовал (выход 1).

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

Замер ниже — запись встроенного ocr-воркера. Этот процесс удалён. Ruleset теперь живёт в `tileforge/octoconv-worker-ocr`. Цифры ниже — историческая запись, их не пересчитывали. Внешний замер образа `octoconv-ocr:confinement-measure` (`sha256:f7db87f152b06681c8077deb2319839773a8abbf835012fc68cdb5aa86640c84`, ядро `Linux version 7.2.12-orbstack-00394-gbecdd6130068`) дал текстовый слой 29 байт. Протокольный набор после этого не стартовал (выход 1).

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

Замер ниже — запись встроенного av-воркера. Этот процесс удалён. Ruleset теперь живёт в `tileforge/octoconv-worker-av`. Цифры ниже — историческая запись, их не пересчитывали. Внешний замер образа `octoconv-av:confinement-measure` (`sha256:4ead4f7bf2bb1cb4a47cac59c15fe6c334a489e8f590d0011946c54e36e5a0df`, ядро `Linux version 7.2.12-orbstack-00394-gbecdd6130068`) дал выход 2791 байт.

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

Замер ниже — запись встроенного audio-воркера. Этот процесс удалён. Ruleset теперь живёт в `tileforge/octoconv-worker-audio`. Цифры ниже — историческая запись, их не пересчитывали.

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

## probe (gs + pdfinfo + unzip + file) — переизмерено и доказано 2026-09-26 (фаза 69, D-10)

Первоначальный замер 2026-09-20 (ниже был вычеркнут) охватывал только `gs`,
`pdfinfo` и `unzip` и пропустил `file(1)` целиком. D-10 переизмерил класс по
СОБСТВЕННЫМ exec-точкам кода, а не по старому документу — семь вызовов,
четыре бинарника:

| Файл:строка | Бинарник | Вызов |
|---|---|---|
| `internal/convert/sanitizepdf.go:222` | `pdfinfo` | подсчёт страниц перед первым проходом gs |
| `internal/convert/sanitizepdf.go:272` | `gs` | проход 1: PDF → PostScript (`ps2writeArgs`) |
| `internal/convert/sanitizepdf.go:289` | `gs` | проход 2: PostScript → санированный PDF (`pdfwriteArgs`) |
| `internal/convert/sanitizeverify.go:813` | `unzip` | листинг частей санированного пакета (`-Z1`) |
| `internal/convert/sanitizeverify.go:867` | `unzip` | извлечение одной части (`-p`) для классификации |
| `internal/convert/sanitizeverify.go:875` | `file` | классификация MIME извлечённой OOXML-части |
| `internal/convert/sanitizeverify.go:1351` | `file` | классификация MIME санированного изображения |

**Read-write:** каталог задачи — и **только** он. `/tmp` в этот раз НЕ
предоставляется (см. решение по CONF-06 ниже) — `probeReadWrite` пуст.

**Read-only** (`probeReadOnly`, ровно в этом порядке — держится тестом
`TestProbeReadOnlySetIsTheMeasuredOne`):

| Путь | Причина |
|---|---|
| `/lib` | динамические библиотеки всех четырёх движков |
| `/usr/lib` | сами движки (`gs`/`pdfinfo`/`unzip`/`file`) и скомпилированная база `file(1)` (`/usr/lib/file/magic.mgc`, резолвится через уже выданный `/usr/share/misc/magic.mgc`) |
| `/usr/share/ghostscript` | ресурсы и ICC-профили ghostscript |
| `/usr/share/color` | системные ICC-профили, которые `gs` читает при цветоконверсии |
| `/usr/share/fonts` | шрифты для рендеринга PDF-страниц |
| `/usr/share/poppler` | таблицы cmap/cidToUnicode `pdfinfo` (libpoppler) — см. "Консервативный грант" ниже |
| `/var/lib/ghostscript` | состояние ghostscript (шрифтовой кэш и т.п.) |
| `/etc/ld.so.cache` | резолвер динамического линковщика |
| `/etc/localtime` | часовой пояс для меток времени движков |
| `/etc/papersize` | размер бумаги по умолчанию для ghostscript |

**Найдено НЕНУЖНЫМ удалением (D-10), НЕ выдано:**

- `/etc/magic` — `file --mime-type -b` под ruleset БЕЗ этого гранта всё
  равно печатает верный `image/jpeg` в stdout (что и парсит код); в stderr
  появляется `file: cannot read magic file '/etc/magic' (Permission denied)
  (No such file or directory)`, которое runCommand отбрасывает при нулевом
  коде выхода.
- `/usr/share/misc` — тот же приём: без отдельного гранта на этот путь
  `file(1)` всё равно правильно отвечает `text/xml`/`image/jpeg`, потому что
  уже выданный `/usr/lib` покрывает результат резолва символьной ссылки
  `/usr/share/misc/magic.mgc → /usr/lib/file/magic.mgc`.

**Консервативный грант:** `/usr/share/poppler` оставлен, хотя удаление на
единственной ASCII-фикстуре метаданных не изменило вывод `pdfinfo` —
недорогой грант (это содержимое образа, не хост), а его отсутствие рискует
молчаливым неверным декодированием non-ASCII полей Title/Author при
`rc=0` — тот же класс ловушки, что уже поймали на `/usr/share/fonts` для OCR.

**D-05 (TMPDIR): измерено 2026-09-25**, `strace -ff -e
trace=open,openat,creat,unlink,unlinkat` под `TMPDIR=/job/tmp` на реальном
`octoconv-probe-worker` образе, оба прохода `gs` на
`testdata/sanitize/threat_all_classes.pdf`: каждый `gs_*` scratch-файл
создаётся и удаляется внутри `/job/tmp`; grep по обеим трассам на голый
`"/tmp/` (не `/job/tmp`) — ноль совпадений. Полное решение и альтернативы —
в разделе "Решение по /tmp (CONF-06, фаза 69)" ниже; здесь фиксируется
только сам факт: TMPDIR передаётся в СОБСТВЕННОЕ окружение движка
(`engineEnv(jobDir)` через `engineExec.withEnv`), никогда не
`os.Setenv`-ом в процессе воркера.

**Действенность — по СОДЕРЖИМОМУ, никогда по коду выхода** (движки этого
класса, в отличие от chromium, отказывают ГРОМКО — но `file(1)` всё равно
выходит с кодом 0 при отказе, что и делает суждение по содержимому
обязательным, а не факультативным):

| Движок | Вне каталога задачи | Внутри (для сравнения) |
|---|---|---|
| `gs` (обе фазы) | код возврата 1, `/undefinedfilename … Unrecoverable error`; на выходе — 166046-байтный "скелет" без единого маркера содержимого | `rc=0`, реальный санированный вывод, маркер найден |
| `pdfinfo` | `I/O Error: Couldn't open file …: Permission denied.` | полные корректные метаданные |
| `unzip` | `error: cannot open zipfile … Permission denied` | листинг из 5 частей, содержимое верное |
| `file` | `writable, regular file, no read permission` (это САМ ответ на stdout — не предупреждение и не ошибка; **код выхода 0**) | верный MIME (`text/xml`/`image/jpeg`) |

<!-- conf05:probe:readwrite -->
<!-- /conf05:probe:readwrite -->
<!-- conf05:probe:readonly -->
/lib
/usr/lib
/usr/share/ghostscript
/usr/share/color
/usr/share/fonts
/usr/share/poppler
/var/lib/ghostscript
/etc/ld.so.cache
/etc/localtime
/etc/papersize
<!-- /conf05:probe:readonly -->
<!-- conf05:probe:engines -->
gs
pdfinfo
unzip
file
<!-- /conf05:probe:engines -->

## document (soffice + veraPDF) — замерено и доказано 2026-09-30 (фаза 70, план 02)

Два движка одного образа и два ОТДЕЛЬНЫХ набора (D-10): veraPDF не получает того,
что нужно soffice, и наоборот. Инструмент — `scripts/document-sandbox-measure.sh`.
Образ собирается из этого дерева (нативный, arm64 на arm64-сервере; эмулируемый
образ скрипт отвергает), оба движка запускаются через БОЕВОЙ формат
(`/usr/local/bin/document-worker __sandbox-exec rw:... sock:... ro:... -- <движок>`),
а каждая проверка судит по СОДЕРЖИМОМУ: текст фикстуры внутри `content.xml` /
`word/document.xml`, `%PDF-` и `/GTS_PDFA` для PDF/A, атрибут `isCompliant`
veraPDF. Код выхода не судится нигде. Набор берётся отсюда же, из блоков conf05 ниже
(через `scripts/conf05-crosscheck.sh --doc soffice verapdf`), так что документ и
прогон не могут разойтись. Транскрипты всех прогонов —
`.planning/phases/70-document-on-amd64/evidence/document-sandbox-measure.txt`.

### Источники кандидатов

`scripts/sandbox-paths-measure.sh trace` (docx→odt; docx→PDF/A с суффиксом из
`internal/convert/opts.go`, `TMPDIR=/job/tmp`; `verapdf -f 2b --format xml` с
`JAVA_OPTS=-Djava.io.tmpdir=… -XX:-UsePerfData`), результаты трассы с
резолвом символьных ссылок (Ловушки §7), плюс списки `execve` из спайка 70-01
(evidence/soffice-ipc-spike.txt): исполняемые файлы в `openat`-трассе не видны
(Ловушки §1-2). Отсюда первый, широкий набор (`/usr/bin` каталогом, `/etc/fonts`,
`/proc` и т. д.), который затем СУЖАЛСЯ удалением: каждая запись убиралась по одной,
и оставалась, только если без неё проверка по содержимому (или по времени, или по
виду JVM на контейнер) падала.

### Право на сокет и маршрут /tmp (D-09, маршрут `scoped-socket`)

`soffice.bin` создаёт AF_UNIX-сокет `/tmp/OSL_PIPE_<uid>_SingleOfficeIPC_<hash>`
(каталог выбирается только из `/tmp`, затем `/var/tmp`; `TMPDIR`,
`-env:OSL_SOCKET_PATH`, `--nolockcheck` и запуск `soffice.bin` напрямую его не
двигают — evidence/soffice-ipc-spike.txt M1-M6; с закрытыми `/tmp` и `/var/tmp`
soffice выходит с «no valid pipe path found»). Поэтому `TMPDIR` (который уводит
остальные scratch-записи soffice в каталог задачи) на сокет не влияет.

Решение пользователя в 70-01: отдельный вид гранта `sock:<каталог>` =
`LANDLOCK_ACCESS_FS_MAKE_SOCK` и ничего больше (`Paths.Socket`, константа
`accessMakeSocket`; `accessReadWrite` и `accessReadOnly` не тронуты, тест
`TestNothingMayCreateSymlinksOrSpecialFiles` проходит без изменений).

**Гипотеза «только сокет» проверена ПЕРВОЙ и не подтвердилась.** С `sock:/tmp` без
`rw:/tmp` bind проходит (сокет создан), первый процесс `soffice.bin` подключается
к нему и тут же делает `unlinkat` — `EACCES`: для unlink нужен
`LANDLOCK_ACCESS_FS_REMOVE_FILE` на `/tmp`, а сокет-грант его не несёт. Он выходит
с кодом 81 (протокол перезапуска), второй не может занять тот же путь
(`ECONNREFUSED` на осиротевший сокет) и выходит с 1. **Все конверсии падают**
(S1-S5, F1, V1, N2 в `final-B2-socket-only`), а в `/tmp` остаётся осиротевший
`OSL_PIPE_*`. Тот же набор с `rw:/tmp` — `ALL PASS`, `/tmp` после конверсии чист.

**/tmp route: read-write.** `/tmp` выдаётся на чтение-запись И остаётся `sock:/tmp`
(MAKE_SOCK не входит в `accessReadWrite`: без `sock:` bind падает с EACCES
даже при `rw:/tmp` — `B-soffice-sock-drop-tmp`; без `rw:` — прогон выше).
C1 доказывает, что оба гранта несут нагрузку: S1 без обоих не даёт текста.
Это вторая ветка D-09: для класса действуют правила фазы 69 для разделяемого
writable-`/tmp` (concurrency 1, guard, sweep) — в 70-03 (код) и 70-04
(конфигурация). `DOCUMENT_WORKER_CONCURRENCY` остаётся 1, не 2.

**pipe residue: absent** при выбранном маршруте (soffice сам удаляет сокет при
выходе, `TMP-RESIDUE` всех прогонов read-write — none); при «только сокет» —
present. Имя сокета зависит от `/etc/passwd`: с ним `OSL_PIPE_65534_SingleOfficeIPC_<hash>`,
без него (итоговый набор его не выдаёт) `OSL_PIPE_SingleOfficeIPC_<hash>` — sweep в
70-03 обязан искать `OSL_PIPE_*`, а не имя с uid. Остаток в `/tmp` образа —
`hsperfdata_root` — не артефакт прогона: его оставляет постустановка JRE при сборке
(root-владелец, `nobody` туда не пишет); `TMP-RESIDUE` считается относительно него.

### soffice — итоговый набор

Read-write: `/tmp` (каталог задачи — неявно; `/dev/null` НЕ нужен). Socket: `/tmp`.

| Путь | Причина (что падает без него) |
|---|---|
| `/usr/lib` | сам движок и все библиотеки (`/lib` — символьная ссылка на `usr/lib`, тот же грант, второй раз не нужен) |
| `/usr/bin/dash` | `#!/bin/sh` обёртки `soffice` |
| `/usr/bin/basename`, `/usr/bin/dirname`, `/usr/bin/ls`, `/usr/bin/sed` | обёртка (строки 46-52) разрешает свой каталог через `dirname`, `basename`, `ls -l \| sed`; без любого из них `oosplash: not found`, exit 127 |
| `/etc/libreoffice` | без реестра конфигурации `soffice` зависает (`uno::RuntimeException`, ни вывода, ни выхода): убит по таймауту 60 с, exit 124 |
| `/usr/share/fonts` | без шрифтов все конверсии падают |
| `/dev/urandom` | экспорт PDF (идентификатор файла): S5, F1, V1, N2 падают, S1-S4 (ODF/OOXML) проходят |

`/usr/bin/soffice` (сам движок, символьная ссылка на `/usr/lib/libreoffice/program/soffice`)
добавляется в ro: отдельно, как остальные движки в `forJobRW`. **`/usr/bin` целиком не
выдаётся.**

### soffice — таблица удаления (от широкого набора, по одной записи)

Прогоны `soffice-*` в evidence. Формат: убранная запись → какие проверки упали.

| Убрано | Результат | Решение |
|---|---|---|
| `/usr/bin` каталогом → семь литеральных бинарников | ALL PASS (только с `ls`: первый набор без `ls` — exit 127) | каталог не выдан |
| `/usr/bin/dash`, `basename`, `dirname`, `ls`, `sed` | S1-S5, F1, V1, N2 падают | оставлены |
| `/usr/bin/grep` | ALL PASS | убран: `grep -q` в строке 145 обёртки только при отладочных `--record/--backtrace/--strace/--valgrind` |
| `/usr/bin/uname` | ALL PASS | убран: `case "$(uname -s)"` даёт эффект только на OpenBSD/NetBSD/AIX, на Linux — пустая ветка |
| `/usr/bin/gpg`, `gpgconf`, `gpgsm`, `javaldx` | не выдавались, ALL PASS | не нужны (гипотеза A2 подтверждена) |
| `/lib` | ALL PASS | убран: дубль `/usr/lib` |
| `/usr/lib` (при выданном `/lib`) | ALL PASS | тот же каталог; в итоговом наборе `/usr/lib` убран → все падают |
| `/etc/libreoffice` | S1-S5, F1, V1, N2 падают (таймаут) | оставлен |
| `/etc/fonts`, `/usr/share/fontconfig`, `/var/cache/fontconfig`, `/usr/local/share/fonts` | ALL PASS, F1 тоже | убраны: подстановка Calibri→Carlito, Cambria→Caladea, Arial→LiberationSans сохраняется (механизм не выяснялся; F1 доказывает результат) |
| `/usr/share/fonts` | S1-S5, F1, V1, N2 падают | оставлен |
| `/etc/ld.so.cache`, `/etc/localtime`, `/etc/nsswitch.conf`, `/etc/passwd`, `/etc/group` | ALL PASS | убраны (`/etc/passwd`: см. имя сокета выше) |
| `/proc` целиком | ALL PASS | убран |
| `/proc` → `/proc/cpuinfo`, `/proc/filesystems` (записи из трассы) | ALL PASS | не нужны и они |
| `/sys/devices/system/cpu` | ALL PASS | убран |
| `/dev/urandom` | S5, F1, V1, N2 падают | оставлен |
| read-write `/dev/null` | ALL PASS | убран |
| `sock:/tmp` (при `rw:/tmp`) | S1-S5, F1, V1, N2 падают | оставлен |
| `rw:/tmp` (при `sock:/tmp`) | S1-S5, F1, V1, N2 падают, `OSL_PIPE_*` остаётся | оставлен (маршрут read-write) |

Второй проход, УЖЕ от итогового набора (`B-*` в evidence): убрать любую оставленную
запись — падает по содержимому (или по времени, или по виду JVM); это то, что держит
каждую оставленную запись. Итоговый набор целиком (одновременное удаление всех
«убранных») — `final-A2`, `ALL PASS`.

### veraPDF — итоговый набор

Read-write: `/dev/null` и ТОЛЬКО он (каталог задачи — неявно, `/tmp` не выдаётся: с
`JAVA_OPTS=-Djava.io.tmpdir=<job>/tmp -XX:-UsePerfData` `TMP-RESIDUE` после проверки —
none, `hsperfdata_*` нет; `-XX:-UsePerfData` нужен, потому что `hsperfdata` `java.io.tmpdir`
не двигает).

| Путь | Причина (что падает без него) |
|---|---|
| `/usr/lib` | JRE (`/usr/lib/jvm/java-17-openjdk-<arch>`) и библиотеки; V1 и V0 падают. Целиком, а не `/usr/lib/jvm` + `/usr/lib/<multiarch>`: имя multiarch-каталога зависит от архитектуры образа, а grant-список в коде — константа; сужение до архитектурных путей — вне этого плана (остаточная ширина: veraPDF видит и `/usr/lib/libreoffice`) |
| `/usr/bin/dash` | `#!/bin/sh` скрипта запуска `/opt/verapdf/verapdf` |
| `/usr/bin/dirname`, `/usr/bin/which` | скрипт запуска (`dirname "$0"`, `which java`); `which` — цепочка `/etc/alternatives/which` → `which.debianutils`, грант ложится на конечный файл |
| `/opt/verapdf` | jar и скрипт запуска; V1 падает |
| `/etc/java-17-openjdk` | резолвленная цель `conf/` JVM (Ловушки §7); V1 падает без вывода |
| `/proc/cgroups` | V0 и V0L: без него Java-часть JVM (`-XshowSettings:system`, `OperatingSystemMXBean`) отвечает «No metrics available for this platform»; куча и GC при этом прежние (70-08, candA). Называет его только трасса с `-XshowSettings` |
| `/proc/self/cgroup`, `/proc/self/mountinfo` | V0 и V0L: где смонтирован cgroup контейнера. Резолвятся helper'ом в его собственном pid при Apply; покрывают JVM, потому что скрипт запуска заканчивается `exec java` и helper сам делает exec (LAUNCHER: exec, 70-08) |
| `/sys/fs/cgroup` | V0: без него JVM видит процессоры хоста и лимиты Unlimited (см. ниже) |
| `/dev/random` | V2: без него V1 всё равно верный, но JVM засевает SecureRandom резервным потоковым генератором — 9.9 с вместо 1.6 с |

`/opt/verapdf/verapdf` (сам движок) добавляется в ro: отдельно, как движок в `forJobRW`.
`/usr/bin/java` НЕ выдаётся: симлинк `/etc/alternatives/java` резолвится в
`/usr/lib/jvm/...`, уже покрытый `/usr/lib`.

### veraPDF — таблица удаления (от широкого набора, по одной записи)

| Убрано | Результат | Решение |
|---|---|---|
| `/usr/bin` каталогом → `dash`, `dirname`, `uname`, `which`, `java` | ALL PASS | каталог не выдан |
| `/usr/bin/dash`, `dirname`, `which` | V1 падает | оставлены |
| `/usr/bin/uname`, `/usr/bin/java` | ALL PASS | убраны (`java` — внутри `/usr/lib`; `uname` не нужен) |
| `/lib` | ALL PASS | убран: дубль `/usr/lib` |
| `/opt/verapdf`, `/etc/java-17-openjdk` | V1 падает | оставлены |
| `/etc/host.conf`, `/etc/hosts`, `/etc/resolv.conf`, `/etc/nsswitch.conf`, `/etc/passwd`, `/etc/ld.so.cache`, `/etc/localtime` | ALL PASS, время V1 прежнее | убраны |
| `/proc` целиком (70-02) | V0 падает: JVM видит хост, а не контейнер | оставлен в 70-02, **заменён в 70-08** (строки ниже) |
| `/proc` → `cpuinfo`, `meminfo`, `stat`, `net/if_inet6`, `sys/vm/overcommit_memory` (70-02: записи трассы без `/proc/self/*`, «их путь зависит от pid, статически его не выдать») | V0 падает | оставлен `/proc` целиком в 70-02. 70-08: довод про pid не держится, потому что helper резолвит `/proc/self/*` в своём pid, а скрипт запуска делает exec |
| `/proc` → все записи трассы (`cgroups`, `cpuinfo`, `meminfo`, `stat`, `net/if_inet6`, `sys/vm/overcommit_memory`, `self/cgroup`, `self/mountinfo`, `self/coredump_filter`) (70-08, candB) | ALL PASS (14), V0, V0L, N3 проходят | отправная точка сужения |
| из candB: `/proc/cpuinfo`, `/proc/meminfo`, `/proc/stat`, `/proc/net/if_inet6`, `/proc/sys/vm/overcommit_memory`, `/proc/self/coredump_filter` (по одной) | ALL PASS у каждой | убраны |
| из candB: `/proc/cgroups`, `/proc/self/cgroup`, `/proc/self/mountinfo` (по одной) | V0 и V0L падают, V1/V2/N3 проходят | оставлены |
| candC (три оставленных, остальное убрано разом) | ALL PASS (14) | итоговый набор |
| `/sys/fs/cgroup` | V0 падает | оставлен |
| `/sys/devices/system/cpu`, `/sys/kernel/mm/transparent_hugepage` | V0 (метрики И эргономические значения) совпадает | убраны |
| `/dev/urandom` | ALL PASS | убран |
| `/dev/random` | V1 проходит, V2 падает (9.9 с против 1.6 с) | оставлен |
| read-write `/dev/null` | V1 падает (`cannot create /dev/null`) | оставлен |

### V0: почему JVM-записи судятся видом на контейнер, а не вердиктом

Без `/proc` или `/sys/fs/cgroup` veraPDF всё равно возвращает верный вердикт, но JVM
внутри контейнера на 1 GiB / 2 CPU перестаёт его видеть. Измерено (V0, `java
-XshowSettings:system -XX:+PrintFlagsFinal -version` без ruleset и под ним):

| | без ruleset / с итоговым набором | без `/proc` | без `/sys/fs/cgroup` |
|---|---|---|---|
| Operating System Metrics | cgroupv2, Effective CPU Count 2, Memory Limit 1.00G | «No metrics available for this platform» | cgroupv2, CPU Count 11, лимиты Unlimited |
| MaxHeapSize | 268435456 (256 МиБ) | 4206886912 (хост) | 4206886912 (хост) |
| GC | Serial, ParallelGCThreads 0 | G1, ParallelGCThreads 9 | G1, ParallelGCThreads 9 |

Куча в 4 ГиБ внутри контейнера на 1 ГиБ — путь к OOM-kill, а не к ошибке
валидации; поэтому эти пути оставлены на основании V0, а не V1. Итоговый набор даёт 26
совпавших строк.

**V0L (70-08).** V0 запускает `java` напрямую и не видит, форкает ли боевой скрипт
запуска JVM. V0L запускает ту же проверку через `/opt/verapdf/verapdf -f 2b --format xml`
(`JAVA_OPTS` — боевое значение плюс `-XshowSettings:system -XX:+PrintFlagsFinal`), без
ruleset и под ним, с теми же лимитами, и дополнительно требует `isCompliant="true"`.
Трасса: скрипт запуска завершается `exec "$JAVACMD" $JAVA_OPTS ...`, и strace показывает
два `execve` (скрипт, затем `/usr/bin/java`) в одном pid (`LAUNCHER: exec`), так что записи
`/proc/self/*` покрывают JVM. Свидетельства: `evidence/verapdf-proc-measure-70-08.txt`.

### /proc и другие процессы (WR-02, 70-08)

Ревью (WR-02) утверждало: JVM под `ro:/proc` читает `/proc/<ppid>/environ` воркера и
`/proc/1/environ`, то есть `DATABASE_URL` и ключи S3, и очистка окружения этого не
закрывает. Это было рассуждением. N3 в `scripts/document-sandbox-measure.sh` его
измеряет: в окружение контейнера кладётся метка-канарейка, её видят и PID 1, и
родитель helper'а (`timeout`); сначала тот же контейнер без ruleset доказывает, что оба
файла читаются и метку содержат (иначе отказ ничего не доказывает), затем под ruleset
veraPDF (движок `/usr/bin/dash`) — попытка открыть.

**Базовый прогон на прежнем гранте (`ro:/proc` целиком), 2026-09-30:**

| Открыть | Результат |
|---|---|
| `/proc/1/environ`, `/proc/<ppid>/environ` | **отказ** (без ruleset — метка читается) |
| `/proc/self/root/etc/hostname` | отказ |
| `/proc/1/cmdline`, `/proc/<ppid>/cmdline`, `/proc/<ppid>/status` | **открылись** |
| свои `/proc/self/environ`, `/proc/self/cmdline` (CONTROL) | открылись |

Вывод по измерению, не по рассуждению: чужой `environ` под тем грантом НЕ читался;
читались `cmdline` и `status` чужих процессов (argv воркера и tini, не среда). Отказ
не даёт файловый грант (`cmdline` того же процесса открывается), он наступает при
открытии самого `environ`; механизм — проверка ptrace в Landlock (процесс в домене не
получает ptrace-доступ к процессу вне его) — **выведен, не измерен**; измерен отказ.
Поэтому класс не должен на него опираться: на что он влияет — это его собственный
грант `/proc`.

**Запуск JVM.** Скрипт `/opt/verapdf/verapdf` заканчивается `exec "$JAVACMD" $JAVA_OPTS ...`;
strace показывает два `execve` (скрипт, затем `/usr/bin/java`) в одном pid, а остальные
файлы трассы — потоки этого pid (`LAUNCHER: exec`). Поэтому запись `/proc/self/...`,
которую helper резолвит в СВОЁМ pid при Apply, покрывает JVM — как уже покрывает
`/proc/self/auxv` и `/proc/self/status` в классе `image`. Доводов «путь зависит от pid,
статически его не выдать» из 70-02 это не оставляет.

**Исход: сужено.** Кандидат — все записи трассы: `/proc/cgroups`, `cpuinfo`, `meminfo`,
`stat`, `net/if_inet6`, `sys/vm/overcommit_memory`, `/proc/self/cgroup`,
`/proc/self/mountinfo`, `/proc/self/coredump_filter`; первый набор без `/proc/cgroups`
(его читает только Java-часть метрик, трасса без `-XshowSettings` его не показала)
уронил V0 и V0L. Убирая по одной записи, оставлены три, без каждой из которых
падают V0 и V0L: `/proc/cgroups`, `/proc/self/cgroup`, `/proc/self/mountinfo`.
Итоговый набор — `ALL PASS (14 checks)`, V0 и V0L совпали в 26 строках, V1 `isCompliant`,
V2 без замедления, N3: отказ на `/proc/1/environ`, родительском `environ` и
`/proc/self/root/etc/hostname`, и теперь `cmdline` и `status` чужих процессов тоже
отказ (PROC-OTHER). Честная оговорка: куча и GC в production держатся и без
`/proc/cgroups` (V0/V0L на нём падают только на блоке Operating System Metrics;
candA дал те же 256 МиБ и Serial); запись оставлена, потому что критерий — совпадение
вида JVM на контейнер, а метрики читает и `OperatingSystemMXBean`.

Прогоны — `evidence/verapdf-proc-measure-70-08.txt`: BASELINE, LAUNCHER, трасса, candA,
candB, девять прогонов удаления, candC, затем итоговый полный прогон драйвера.

### F1: что проверка по одному тексту не видит

Замена шрифтов меняет вёрстку, не текст. F1 конвертирует docx с Calibri, Cambria и
Arial и требует в PDF имена Carlito, Caladea и LiberationSans; без F1 записи
`/etc/fonts` и соседние нельзя было бы отличить от нужных. F1 проходит и без них —
по правилу они убраны.

### Действенность (эффективность)

- **N1, soffice на файле вне всех грантов** (идентичная копия фикстуры в `/outside`):
  вывода нет, текст фикстуры нигде не встречается. **SOFFICE-DENIAL: silent** (exit 0,
  файла вывода нет — по определению «silent» — exit 0). Ответ на Open Question 1:
  отказ тихий по коду выхода, но громкий по факту вывода; боевой код
  (`libreoffice.go`) превращает «exit 0 без файла» в терминальную ошибку `produced no
  output file`, однако preflight в 70-03 не может опираться на тип ошибки — только на
  отсутствие вывода с маркером.
- **N2, veraPDF на PDF/A вне грантов**: `<exceptionMessage>Exception: Couldn't parse
  stream caused by exception: /outside/in.pdf (Permission denied)</exceptionMessage>`,
  атрибута `isCompliant` нет. Код выхода 7 — тот же, что у вердикта «не соответствует»;
  поэтому код выхода veraPDF не является доказательством ни в одну сторону (D-11).
- **C1** доказывает, что сокет-грант и `rw:/tmp` не декоративны (см. маршрут).

### Что стоит preflight и минимальная фикстура

Preflight-последовательность (docx→odt; PDF/A-экспорт с валидацией; оба отрицательных
прогона) на итоговом наборе: S1 1.2 с + S5 1.2 с + V1 1.5 с + N1 1.0 с + N2 1.2 с ≈
6.1 с (5.9-6.3 с по прогонам; холодный старт JVM включён). Минимальный DOCX, который
LibreOffice 7.4 конвертирует с маркером `octoconv-preflight-0123456789abcdef` в
`content.xml` и чей PDF/A-экспорт veraPDF признаёт соответствующим: три части —
`[Content_Types].xml` (Default для `rels` и `xml`, Override для `/word/document.xml` с
типом WordprocessingML main), `_rels/.rels` (связь officeDocument на
`word/document.xml`) и `word/document.xml` с одним абзацем; 937 байт, `word/_rels/document.xml.rels`
не понадобился.

<!-- conf05:soffice:readwrite -->
/tmp
<!-- /conf05:soffice:readwrite -->
<!-- conf05:soffice:socket -->
/tmp
<!-- /conf05:soffice:socket -->
<!-- conf05:soffice:readonly -->
/usr/lib
/usr/bin/dash
/usr/bin/basename
/usr/bin/dirname
/usr/bin/ls
/usr/bin/sed
/etc/libreoffice
/usr/share/fonts
/dev/urandom
<!-- /conf05:soffice:readonly -->
<!-- conf05:soffice:engines -->
soffice
<!-- /conf05:soffice:engines -->

<!-- conf05:verapdf:readwrite -->
/dev/null
<!-- /conf05:verapdf:readwrite -->
<!-- conf05:verapdf:readonly -->
/usr/lib
/usr/bin/dash
/usr/bin/dirname
/usr/bin/which
/opt/verapdf
/etc/java-17-openjdk
/proc/cgroups
/proc/self/cgroup
/proc/self/mountinfo
/sys/fs/cgroup
/dev/random
<!-- /conf05:verapdf:readonly -->
<!-- conf05:verapdf:engines -->
verapdf
<!-- /conf05:verapdf:engines -->

## cad (cad-guard + cad-mesh + cad-render + cad-dxf + rsvg-convert) — замерено и доказано 2026-10-05 (фаза 73, план 03; render и повторный замер mesh — фаза 74, план 06; cad-dxf, rsvg-convert и повторный замер guard на DXF — фаза 75, план 08, 2026-10-11)

Две стадии одного образа и два ОТДЕЛЬНЫХ набора (D-12): guard первым читает
недоверенные байты и не должен иметь пути к OCP, поэтому `/opt/venv` в его наборе нет;
mesh запускает OCP. Инструмент — `scripts/cad-worker-sandbox-measure.sh`. Образ — БОЕВОЙ
engine-этап `Dockerfile.cad-worker` (id `sha256:566f27bbf72150d24a607fc9aabda98b9e229f8a7ac5cc10934d349982a3be30`,
Python 3.11.2, cadquery-ocp-novtk 7.9.3.1.1, `USER nobody`, нативный arm64 на arm64-сервере,
ядро 7.0.14-orbstack), а не образ замеров фазы 71 (VTK, OSMesa, шрифты, другой путь к
helper'у). Набор фазы 71 был только кандидатом: из него в плане 73-02 посеяны
Go-переменные, а этот прогон проверяет их заново на том, что поедет в продакшен, боевыми
helper'ами и боевым argv. Транскрипт —
`.planning/phases/73-cad-class-convert-slice-confined/evidence/73-03-landlock.txt`.

### Метод

Метод тот же, что в разделе «Метод, и почему одной фазы мало»; конкретно:

- **Матрица**, обе стадии на каждом входе: STEP `units_inch`, `LM8UU`, `nist_ctc_05`,
  `nist-d2mi-827-9999-906`, `V-Slot-20x20-100mm` (путь конечного бокса), `legacy_cp1251`,
  `assembly_nauo4`; IGES `surfaces_only`, `iges_no_newlines`, `freecad-festo-valve-5-2.igs`.
  Десять входов, двадцать эталонных прогонов.
- **argv и среда боевые.** guard: `/usr/local/bin/cad-guard --format step|iges --max-entities 754762
  --max-nauo 1518 /job/in.EXT`; mesh: `/usr/local/bin/cad-mesh --format step|iges /job/in.EXT /job/out.stl`.
  Среда ровно `HOME=/job`, `LANG=C.UTF-8`, `PATH` образа, `TMPDIR=/job`, `LP_NUM_THREADS=2`;
  `--network none --cpus 2 --memory 4g --memory-swap 4g --user nobody`; записываемый набор — один
  `/job`.
- **Кандидаты** — `scripts/sandbox-paths-measure.sh trace` по каждому входу каждой стадии
  (девять и одиннадцать записей после схлопывания), плюс руками то, чего `openat`-трасса не
  видит: собственный исполняемый файл стадии, интерпретатор, который ядро запускает по
  shebang (`/usr/bin/python3.11` — и для `/usr/bin/python3`, и через `/opt/venv/bin/python`),
  и каталог multiarch-библиотек образа.
- **Достаточность** судится по СОДЕРЖИМОМУ против прогона без ruleset: для guard — последняя
  строка `CADVERDICT` целиком (счётчики входят), для mesh — она же плюс sha256 STL. Код
  выхода не судится нигде: отказ Landlock — это rc=2 без текста, и по коду он неотличим от
  обычного отказа движка.
- **Действенность**: тот же `units_inch` вне всех грантов (`/outside`, смонтирован только для
  чтения). Проба читает канарейку `/etc/hostname` до ruleset и отказывается продолжать, если
  не прочла.
- **Удаление по одной записи** из широкого набора, каждый раз полная матрица стадии;
  `removable` — только если ВСЕ десять входов остались идентичными, иначе `load-bearing`
  с первым изменившимся входом. Набор — это load-bearing записи.
- **Отрицательный контроль** на каждой стадии: набор без одной load-bearing записи обязан
  провалить достаточность — доказательство, что судья умеет падать.

### Таблицы удаления

| Стадия | Запись | Итог |
|---|---|---|
| guard | `/etc/ld.so.cache` | removable |
| guard | `/usr/lib/locale` | removable |
| guard | `/usr/lib/python3` | removable |
| guard | `/usr/local/lib/python3.11` | removable |
| guard | `/usr/share/zoneinfo` | removable |
| guard | `/usr/bin/python3.11` | **load-bearing** (интерпретатор) |
| guard | `/usr/lib/python3.11` | **load-bearing** (стандартная библиотека) |
| guard | `/usr/local/bin/cad-guard` | **load-bearing** (движок) |
| guard | `/usr/lib/aarch64-linux-gnu` | **load-bearing** (multiarch) |
| mesh | `/etc/ld.so.cache` | removable |
| mesh | `/etc/nsswitch.conf` | removable |
| mesh | `/etc/passwd` | removable |
| mesh | `/sys/devices/system/cpu` | removable |
| mesh | `/usr/lib/locale` | removable |
| mesh | `/usr/share/zoneinfo` | removable |
| mesh | `/opt/venv` | **load-bearing** (OCP и shebang `/opt/venv/bin/python`) |
| mesh | `/usr/bin/python3.11` | **load-bearing** (интерпретатор) |
| mesh | `/usr/lib/python3.11` | **load-bearing** (стандартная библиотека) |
| mesh | `/usr/local/bin/cad-mesh` | **load-bearing** (движок) |
| mesh | `/usr/lib/aarch64-linux-gnu` | **load-bearing** (multiarch) |

Взаимодействия нет: набор из одних load-bearing записей даёт ту же матрицу (двадцать из
двадцати идентичны) — отдельная строка достаточности на каждую стадию в evidence. Ни
`/tmp`, ни `/proc`, ни `/sys` ни в одном из двух наборов нет: `/sys/devices/system/cpu`
mesh читает (трасса видит `/sys/devices/system/cpu/possible`), но без него результат тот
же, и запись убрана. Общего записываемого пути нет, `TMPDIR=/job`.

### Действенность и отрицательные контроли

- **guard вне грантов**: без ruleset ok; под набором вердикт `unreadable/cannot_count`, ok
  нет. **mesh вне грантов**: без ruleset ok и STL; под набором `unreadable/read_failed`,
  STL нет. Та же проверка выполнена и на широком кандидате, и на итоговом наборе.
- **guard не достаёт до OCP**: под итоговым набором guard читает `/opt/venv/pyvenv.cfg`
  — без ruleset файл читается, под ruleset `PermissionError`.
- **Отрицательные контроли**: guard без `/usr/lib/python3.11` и mesh без `/opt/venv` падают
  на первом входе (`missing`, вердикта нет) — `failed_as_expected`.
- **Достаточность** по двадцати парам стадия-вход на кандидате и на итоговом наборе:
  сорок проверок, ни одна не отличается. Повторный прогон `--recheck` (по одному STEP и
  одному IGES на стадию, без ruleset и с ним) заканчивается `RECHECK-OK`.

### Архитектура (R-05)

Каталог multiarch-библиотек в плоские наборы НЕ входит: он выбирается во время работы
по `runtime.GOARCH` через `cadMultiarchLib`, а архитектура, которой в карте нет, отказывает
строить ruleset (71 W17), а не пропускает запись. Замерено только на нативном arm64
(`/usr/lib/aarch64-linux-gnu`, load-bearing в обеих таблицах). amd64 здесь —
**доказательство сборки, а не замер Landlock** (эмулируемому процессу Landlock недоступен;
замер amd64 отложен по контексту фазы): тот же Dockerfile собран под `linux/amd64`,
в образе есть `/usr/lib/x86_64-linux-gnu`, на месте все пять записей наборов
(`/opt/venv`, `/usr/bin/python3.11`, `/usr/lib/python3.11` и оба движка), import-smoke
проходит, guard даёт ok, а mesh — двенадцать треугольников на `units_inch.step` (без
ruleset, в эмуляции). Строка amd64 в карте — выведена, не замерена.

### Render (cad-render) и повторный замер mesh — фаза 74, план 06

Третья стадия того же образа и ТРЕТИЙ отдельный набор. Инструмент и метод те же
(`scripts/cad-worker-sandbox-measure.sh`, кандидаты по `strace`, достаточность по содержимому,
таблица удаления, отрицательный контроль, действенность), образ — боевой engine-этап
`Dockerfile.cad-worker` уже с VTK 9.7.1 и OSMesa (id
`sha256:d1ee5a9ef804a300a8ba2344561a439ae6e99a641ef3cb561e3142ee107ac685`, Python 3.11.2,
cadquery-ocp-novtk 7.9.3.1.1, `USER nobody`, нативный arm64, ядро 7.0.14-orbstack). Транскрипт —
`.planning/phases/74-3d-render-measurements/evidence/74-06-landlock.txt`. До этого замера
`cadRenderReadOnly` был посеян равным набору mesh (план 74-04) и ничем не доказан.

- **Матрица render**: четырнадцать STL-входов — десять STL, которые сам боевой `cad-mesh` построил из десяти
  входов guard и mesh (STEP и IGES), плюс `stl_ascii_box`, `stl_binary_solid_header`,
  `nasa-hubble-all-pieces` и W15 (первые 4396 треугольников `nasa-sls-block1.stl` в ASCII, 1 МиБ,
  `stl-ascii-nasa-sls-block1-1MiB.stl` лестницы 71); на каждый вход три варианта argv:
  `iso 1280 opaque`, `top 2048 opaque`, `iso 1280 transparent`. Сорок два эталонных прогона;
  каждый эталон обязан быть кадром с вердиктом `ok` и PNG (иначе замер пуст).
- **argv**: `/usr/local/bin/cad-render --view V --size S --background B /job/in.stl /job/out.png`;
  среда и лимиты те же, что у guard и mesh.
- **Подпись прогона** — последняя строка `CADVERDICT` плюс sha256 PNG; код выхода не судится.
  Пиксели PNG побайтно совпали с неограниченным прогоном во всех сорока двух случаях и под
  кандидатом, и под итоговым набором.
- **Кандидат** — трасса по восемнадцати прогонам (все входы в `iso 1280 opaque`, а `units_inch` и
  `nasa-hubble` ещё и в двух других вариантах), плюс руками движок `/usr/local/bin/cad-render`,
  интерпретатор и каталог multiarch. `TMP_NEED none`.

Таблица удаления render (каждый раз полная матрица из сорока двух прогонов):

| Запись | Итог |
|---|---|
| `/etc/ld.so.cache` | removable |
| `/etc/nsswitch.conf` | removable |
| `/etc/passwd` | removable |
| `/proc/cpuinfo` | removable |
| `/sys/devices/system/cpu` | removable |
| `/usr/lib/locale` | removable |
| `/usr/share/zoneinfo` | removable |
| `/opt/venv` | **load-bearing** (VTK и shebang `/opt/venv/bin/python`) |
| `/usr/bin/python3.11` | **load-bearing** (интерпретатор) |
| `/usr/lib/python3.11` | **load-bearing** (стандартная библиотека) |
| `/usr/local/bin/cad-render` | **load-bearing** (движок) |
| `/usr/lib/aarch64-linux-gnu` | **load-bearing** (multiarch: OSMesa, LLVM) |

Итоговый набор render — те же пути, что у mesh: `/opt/venv`, `/usr/bin/python3.11`,
`/usr/lib/python3.11` плюс multiarch, и отличается он только грантом на исполнение
(`cad-render`, не `cad-mesh`). Набор из одних load-bearing записей даёт ту же матрицу (сорок два
из сорока двух идентичны). Ни `/tmp`, ни `/proc`, ни `/sys` в наборе нет: трасса видит
`/proc/cpuinfo` и `/sys/devices/system/cpu/*`, но без них результат тот же, и записи убраны;
общего записываемого пути нет, `TMPDIR=/job`.

- **Действенность**: STL `units_inch` вне всех грантов (`/outside`, только чтение) — без ruleset
  кадр `ok` и PNG есть, под набором вердикт без `ok` (`no_triangles`) и PNG нет, rc=2; проверено и
  на широком кандидате, и на итоговом наборе. Проба читает канарейку `/etc/hostname` до ruleset.
- **Отрицательный контроль**: render без `/opt/venv` падает на первом входе (`missing`, вердикта
  нет) — `failed_as_expected`.
- **Без OpenCascade — по импортам, а не по путям**: VTK и OCP живут в одном `/opt/venv`, так что набор
  путей не может отделить render от OCP. Это держит Go-тест, читающий `cmd/cad-worker/helper/cad-render`
  и падающий на любой import `OCP`/`cadquery`, и отдельный грант на исполнение. Узкий набор потребовал бы
  второго venv — это незамеренная работа.
- **Повторный замер mesh** (новый argv `--quality standard --measure /job/measure.json`, новые
  импорты OCP, STL-входы режима `cad-mesh stats`): тринадцать входов, из них три STL
  (`stl_ascii_box`, `stl_binary_solid_header`, `nasa-hubble`) идут через `stats`. Набор не изменился:
  `/opt/venv`, `/usr/bin/python3.11`, `/usr/lib/python3.11`; подпись mesh теперь включает sha256 STL и
  sha256 `measure.json`. Набор guard перемерен без изменений в argv — тот же результат.
- **Итого**: шестьдесят пять пар стадия-вход, сто тридцать проверок достаточности (кандидат и итоговый
  набор) — ни одна не отличается; семь строк действенности — все denied; три отрицательных
  контроля — все `failed_as_expected`; `--recheck` заканчивается `RECHECK-OK`.
- **amd64 для render не замерен.** Эмулируемому процессу Landlock недоступен, поэтому amd64 render
  отложен по контексту фазы и здесь только заявлен: строка amd64 в карте multiarch по-прежнему выведена,
  а не замерена, и эмулируемая сборка этим прогоном не повторялась (флаг `--amd64` повторяет сборку
  guard и mesh).

### cad-dxf и rsvg-convert — фаза 75, план 08

Две новые стадии того же образа и два ОТДЕЛЬНЫХ набора (D-01, D-14): `cad-dxf` (ezdxf) рисует
модельное пространство DXF в SVG, `rsvg-convert` превращает проверенный Go SVG в PDF или PNG. Инструмент
тот же — `scripts/cad-worker-sandbox-measure.sh`, теперь с `--stages guard,dxf,rsvg` (без флага он
повторяет прогон 74-06). Образ — боевой engine-этап `Dockerfile.cad-worker` с DXF-цепочкой (id
`sha256:687fe04acf5591e41fd33b38e6539babd4f32407146eda9eb4ff56878b7bd43c`, Python 3.11.2, ezdxf 1.4.4,
librsvg2-bin 2.54.7+dfsg-1~deb12u1, fonts-dejavu-core 2.37-6, `USER nobody`, нативный arm64 на arm64-сервере,
ядро 7.0.14-orbstack). Транскрипт — `.planning/phases/75-dxf-2d-pipeline/evidence/75-08-landlock.txt`.
До этого замера `cadDXFReadOnly` и `cadRsvgReadOnly` были посеяны в плане 75-06 из таблиц исследования
(прототип helper'а на образе исследования) и ничем на боевом образе не доказаны.

- **Матрица cad-dxf** — десять DXF: `text_cases`, `legacy_cp1251`, `legacy_latin1`, `columns_R2007`,
  `uncommon`, `never_resolved`, `layers_off_frozen`, `hatches_2`, `acad_table_with_blk_ref`,
  `dimension_in_nested_blocks`. Каждый эталон обязан дать вердикт `ok` и оба SVG.
- **argv cad-dxf боевой** (75-07): `/usr/local/bin/cad-dxf --max-drawn-entities 850155 --texts /job/texts.json
  --render-svg /job/draw.render.svg --measure /job/measure.json /job/in.dxf /job/draw.svg`.
- **Подпись cad-dxf** — последняя строка `CADVERDICT` плюс sha256 клиентского SVG, render-only SVG,
  `texts.json` и `measure.json` и число элементов `<text>` в клиентском SVG.
- **Матрица rsvg-convert** — render-only SVG тех же десяти рисунков, пропущенные через Go-валидатор
  (`scripts/cmd/cad-svg-check --out`, то есть ровно `clean.svg` воркера), по три argv на каждый:
  `-f pdf -b white -o /job/out.pdf /job/clean.svg`; PNG 1280 непрозрачный
  (`-b white -w SW -h SH --page-width 1280 --page-height 1280 --left L --top T -o /job/out.png /job/clean.svg`);
  PNG 2048 прозрачный (то же без `-b white`). Letterbox берётся из Go (`cad-pixel-judge --print-letterbox`).
  Тридцать эталонов.
- **Подпись rsvg-convert** — для PDF sha256 файла без `/CreationDate` плюс sha256 текста, который
  декодирует Go-судья PDF (`cad-pdf-check --print-text`); для PNG — sha256. Эталон без ruleset и прогон
  под набором совпадают по этой подписи во всех строках `SUFFICIENT` с PDF.
- **Среда — та, что строит Go** (`cadDrawStage`, 75-06): `PATH` образа, `TMPDIR=/job`,
  `HOME=/job/.home`, `XDG_CACHE_HOME=/job/.home/.cache`, `XDG_CONFIG_HOME=/job/.home/.config`, `LANG` и
  `LC_ALL` не заданы, каталоги `.home` созданы до запуска. Это не `HOME=/job` прошлых замеров:
  в контейнере воркера `HOME=/nonexistent`, и fontconfig у `rsvg-convert` пишет свой кэш в
  `$XDG_CACHE_HOME/fontconfig` внутри каталога задания. Лимиты и проба те же, что у guard и mesh.
- **Кандидаты** — семена из Go плюс грант на исполнение (`/usr/local/bin/cad-dxf` и интерпретатор
  `/usr/bin/python3.11`; `/usr/bin/rsvg-convert`), каталог multiarch и то, что добавила трасса:
  десять трасс cad-dxf и четырнадцать трасс rsvg-convert (каждый PDF, плюс оба PNG для `text_cases` и
  `uncommon`). `TMP_NEED none` для обеих стадий.

Таблица удаления cad-dxf (каждый раз полная матрица из десяти входов):

| Запись | Итог |
|---|---|
| `/etc/ld.so.cache` | removable |
| `/sys/devices/system/cpu` | removable |
| `/usr/lib/locale` | removable |
| `/usr/local/share` | removable |
| `/usr/share/zoneinfo` | removable |
| `/opt/venv` | **load-bearing** (ezdxf и shebang `/opt/venv/bin/python`) |
| `/usr/bin/python3.11` | **load-bearing** (интерпретатор) |
| `/usr/lib/python3.11` | **load-bearing** (стандартная библиотека) |
| `/usr/share/fonts` | **load-bearing** (DejaVu: без него текст не рисуется, первым меняется `text_cases`) |
| `/usr/local/bin/cad-dxf` | **load-bearing** (движок) |
| `/usr/lib/aarch64-linux-gnu` | **load-bearing** (multiarch) |

Таблица удаления rsvg-convert (каждый раз полная матрица из тридцати прогонов):

| Запись | Итог |
|---|---|
| `/etc/fonts` | removable (fontconfig берёт встроенную конфигурацию) |
| `/etc/ld.so.cache` | removable |
| `/proc/filesystems` | removable |
| `/proc/self/maps` | removable |
| `/usr/local/share` | removable |
| `/usr/share/fontconfig` | removable |
| `/usr/share/zoneinfo` | removable |
| `/var/cache/fontconfig` | removable (кэш строится в `$XDG_CACHE_HOME`) |
| `/usr/share/fonts` | **load-bearing** (без него текст выпадает из PDF, первым меняется `text_cases@pdf`) |
| `/usr/bin/rsvg-convert` | **load-bearing** (движок) |
| `/usr/lib/aarch64-linux-gnu` | **load-bearing** (multiarch: librsvg, cairo, pango) |

Итоговые наборы совпали с семенами 75-06: cad-dxf — `/opt/venv`, `/usr/bin/python3.11`,
`/usr/lib/python3.11`, `/usr/share/fonts`; rsvg-convert — `/usr/share/fonts`; у обеих плюс multiarch и
собственный грант на исполнение. Набор из одних load-bearing записей даёт ту же матрицу (сорок из сорока
идентичны). Ни `/tmp`, ни `/proc`, ни `/sys` ни в одном из двух наборов нет: трасса rsvg-convert видит
`/proc/filesystems` и `/proc/self/maps`, трасса cad-dxf — `/sys/devices/system/cpu/possible`, но без них
результат тот же, и записи убраны. Записываемый путь — один каталог задания.

- **Действенность**: `legacy_cp1251.dxf` вне всех грантов (`/outside`, только чтение) — без ruleset `ok` и
  SVG, под набором вердикт `unreadable` без SVG, rc=2; его проверенный SVG вне грантов — без ruleset PDF
  есть, под набором rsvg-convert печатает `Error opening file /outside/clean.svg` и PDF нет, rc=1. Обе
  проверки — и на кандидате, и на итоговом наборе. Проба читает канарейку `/etc/hostname` до ruleset.
- **Шрифтовой отрицательный контроль** (D-14, SC5): итоговый набор без `/usr/share/fonts`. cad-dxf — все
  десять входов с текстом дают `failed/font_unavailable` и ни одного SVG (helper падает громко, а не теряет
  текст молча). rsvg-convert — все десять PDF меняются, а текст, который декодирует Go-судья, пуст. Значит,
  шрифт читается именно под этим ruleset, и без него текст теряется.
- **Ссылки не разрешаются** (D-03, CADIN-04): трасса `never_resolved.dxf` без ruleset, `strace -f -e
  trace=%file` со всеми результатами (не только успешные открытия), ни одного обращения к путям, которые
  называют его сущности (`/etc/passwd`, `/etc/hostname`, `../../outside/secret.png`): `trace_hits=0` при
  3830 записанных вызовах и четырёх открытиях самого входа. helper пропустил и сосчитал `IMAGE` ×2,
  `OLE2FRAME`, `PDFUNDERLAY`, `XREF`. Прогон под итоговым набором идентичен прогону без него. Landlock здесь —
  вторая линия.
- **Повторный замер guard на DXF**: к десяти STEP и IGES добавлены пятьдесят три DXF (десять входов
  cad-dxf, пять остальных файлов корпуса 71 и тридцать восемь фикстур фазы) с боевым argv DXF-режима
  `--format dxf --max-expanded-inserts 170031 --max-expanded-entities 850155 --max-nesting 16
  --max-abs-coordinate 1e+12`. Пятнадцать фикстур guard отвергает (`too_complex`, `unreadable`); их трассы
  сняты через `sh -c '"$@"; exit 0'`, чтобы отказ движка не обрывал трассу, а судятся они, как и все, по
  строке вердикта. Набор guard не изменился: `/usr/bin/python3.11`, `/usr/lib/python3.11` плюс multiarch;
  `/opt/venv` guard по-прежнему не читает.
- **Итого**: сто три пары стадия-вход (63 guard, 10 cad-dxf, 30 rsvg-convert), двести шесть проверок
  достаточности — ни одна не отличается; семь строк действенности — все denied; три отрицательных
  контроля — все `failed_as_expected`; тридцать одна строка удаления; `--recheck` заканчивается `RECHECK-OK`.
  Транскрипт — третий прогон на закоммиченном коде (грязь — только `.planning/config.json`). Первый, на
  холодно собранном образе, остановился на `FAIL ... dxf_text_cases ... the trace recorded no path`: ручная
  трасса того же входа на том же образе пути видит, сбой не повторился. Второй остановил сторож диска VM
  (меньше 6 GiB свободно), не дойдя до трасс.
- **amd64 не замерен.** Эмулируемому процессу Landlock недоступен; наборы cad-dxf и rsvg-convert на amd64
  отложены, как и render, а строка amd64 в карте multiarch по-прежнему выведена.

<!-- conf05:cadguard:readonly -->
/usr/bin/python3.11
/usr/lib/python3.11
<!-- /conf05:cadguard:readonly -->
<!-- conf05:cadguard:engines -->
cad-guard
<!-- /conf05:cadguard:engines -->

<!-- conf05:cadmesh:readonly -->
/opt/venv
/usr/bin/python3.11
/usr/lib/python3.11
<!-- /conf05:cadmesh:readonly -->
<!-- conf05:cadmesh:engines -->
cad-mesh
<!-- /conf05:cadmesh:engines -->

<!-- conf05:cadrender:readonly -->
/opt/venv
/usr/bin/python3.11
/usr/lib/python3.11
<!-- /conf05:cadrender:readonly -->
<!-- conf05:cadrender:engines -->
cad-render
<!-- /conf05:cadrender:engines -->

<!-- conf05:caddxf:readonly -->
/opt/venv
/usr/bin/python3.11
/usr/lib/python3.11
/usr/share/fonts
<!-- /conf05:caddxf:readonly -->
<!-- conf05:caddxf:engines -->
cad-dxf
<!-- /conf05:caddxf:engines -->

<!-- conf05:cadrsvg:readonly -->
/usr/share/fonts
<!-- /conf05:cadrsvg:readonly -->
<!-- conf05:cadrsvg:engines -->
rsvg-convert
<!-- /conf05:cadrsvg:engines -->

<!-- conf05:cad:multiarch -->
amd64 x86_64-linux-gnu
arm64 aarch64-linux-gnu
<!-- /conf05:cad:multiarch -->

## Ещё не замерено

Ничего: все классы замерены на нативных образах. `document` (LibreOffice и
veraPDF) — в разделе выше. `cad` (guard, mesh, render, cad-dxf и rsvg-convert) замерен на нативном arm64;
amd64 для него — доказательство сборки (guard и mesh), а не замер, а для render, cad-dxf и rsvg-convert не
сделано и этого (раздел `cad` выше).
