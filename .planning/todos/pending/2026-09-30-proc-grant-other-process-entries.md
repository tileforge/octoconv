---
created: 2026-09-30
area: sandbox
severity: medium
origin: phase 70 WR-02 (70-REVIEW.md), plan 70-08
audit_acknowledged:
  milestone: v1.15
  at: 2026-09-30
---

# html держит `/proc` целиком: чужие `cmdline` и `status` читаются, `environ` нет (замерено)

## What

После 70-08 целиком `/proc` выдаёт ровно один класс — `html` (`htmlReadOnly`,
`internal/convert/htmlsandbox.go`). Остальные выдают поимённые записи:
`veraPDF` — `/proc/cgroups`, `/proc/self/cgroup`, `/proc/self/mountinfo` (сужено в 70-08);
`image`, `av`, `audio` — свои несколько файлов; `soffice` и `tectonic` (`tectonicSystemReadOnly`)
— ни одной.

Замер (N3, 70-08, `evidence/verapdf-proc-measure-70-08.txt`, раздел HTML; канарейка в
окружении PID 1 и родителя helper'а доказана без ruleset) под грантом html:

| Открыть | Результат |
|---|---|
| `/proc/1/environ`, `/proc/<ppid>/environ` | отказ |
| `/proc/self/root/etc/hostname` | отказ |
| `/proc/1/cmdline`, `/proc/<ppid>/cmdline`, `/proc/<ppid>/status` | **открылись** |

То есть ни одна строка `environ` не открылась — поэтому severity medium, а не high. Но
отказ на `environ` не обеспечивает файловый грант (`cmdline` тех же процессов читается);
механизм — проверка ptrace в Landlock — выведен, не измерен. Опираться на него как на
барьер нельзя, а `cmdline` и `status` чужих процессов (argv воркера и tini) html-движок
видит.

veraPDF под тем же замером (до сужения) давал те же строки; его остаток закрыт в 70-08:
после сужения `cmdline` и `status` чужих процессов тоже отказ (PROC-OTHER: denied).

## Почему html нельзя сузить так же

Для veraPDF сужение держится на том, что скрипт запуска делает `exec java`, и helper
резолвит `/proc/self/...` в своём pid, который и есть pid JVM (`LAUNCHER: exec`). chromium
пере-`exec`ает себя для дочерних процессов (zygote, renderer, GPU; Ловушки §1 в
`SANDBOX-PATHS.md`): у каждого свой pid, и запись `/proc/self/...`, резолвленная в pid
helper'а, ни одного из них не покроет. Чтобы сузить, нужна обработка по процессам
(набор записей, не зависящих от pid, либо выдача на каждый дочерний процесс) — и сначала
трасса того, что chromium читает в `/proc` вообще; в 70-08 она не снималась (решение
пользователя 3: не перемерять каждый класс).

## Не гейт фазы 70

Решение пользователя 3 в 70-08: сузить `veraPDF` по замеру, для остальных — только
записать положение. Деплой фазы 70 это не блокирует: секреты воркера (`environ`) не
открылись ни в одном замере.

## Что сделать

1. Снять трассу `/proc`-записей chromium (`scripts/sandbox-paths-measure.sh trace`,
   `-ff` по процессам) для одного документа.
2. Проверить, хватает ли записей, не зависящих от pid, плюс `/proc/self/...` главного
   процесса; если дочерним процессам нужны свои, решить, стоит ли `/proc` целиком
   оставить с N3 как постоянной проверкой отказа.
3. Перемерять N3 на ядре деплоя (ptrace-проверка Landlock — свойство ядра).

## Связи

- `.planning/phases/70-document-on-amd64/70-REVIEW.md` (WR-02),
  `.planning/research/SANDBOX-PATHS.md` (раздел «/proc и другие процессы»).
