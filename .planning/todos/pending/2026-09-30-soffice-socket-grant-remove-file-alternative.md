---
created: 2026-09-30
area: sandbox
severity: low
origin: phase 70, gap closure -- user decision 4 (2026-09-30)
audit_acknowledged:
  milestone: v1.15
  at: 2026-09-30
---

# soffice: более узкий грант на сокет вместо `rw:/tmp` (`sock:` = MAKE_SOCK + REMOVE_FILE) — не замерен, не построен

## What

Сейчас `soffice` (класс `document`) держит `rw:/tmp` **и** `sock:/tmp`
(`sofficeReadWrite`, `sofficeSocket` в `internal/convert/documentsandbox.go`). Из-за `rw:/tmp`
на класс действуют правила фазы 69 для разделяемого writable-`/tmp`: `DOCUMENT_WORKER_CONCURRENCY`
ровно 1 (`sharedTmpGuard`), sweep `/tmp` перед каждой задачей, preflight. Это решение принято
и оставлено: **2026-09-30 пользователь выбрал оставить `rw:/tmp` с concurrency 1, guard и sweep**.

## Почему не сузили сразу

Гипотеза «нужен только сокет» проверена в 70-02 ПЕРВОЙ и не подтвердилась
(`.planning/research/SANDBOX-PATHS.md`, «Право на сокет и маршрут /tmp»):
`soffice.bin` создаёт AF_UNIX-сокет `/tmp/OSL_PIPE_<uid>_SingleOfficeIPC_<hash>` (каталог
выбирается только из `/tmp`, затем `/var/tmp`; `TMPDIR` и `-env:OSL_SOCKET_PATH` его не двигают),
первый процесс подключается к сокету и сразу делает `unlinkat` — `EACCES`, потому что для
unlink нужен `LANDLOCK_ACCESS_FS_REMOVE_FILE` на `/tmp`, а `sock:` его не несёт. Все конверсии
падали (S1-S5, F1, V1, N2 в `final-B2-socket-only`), а в `/tmp` оставался осиротевший `OSL_PIPE_*`.

## Альтернатива

`sock:<каталог>`, который несёт `MAKE_SOCK` **плюс** `REMOVE_FILE`. Тогда `rw:/tmp` для `soffice`
не нужен.

- Она меняет смысл общего формата (`Paths.Socket`, `accessMakeSocket`, аргумент `sock:` в
  `__sandbox-exec`): сейчас `sock:` — «сокет и ничего больше», и это держат
  `TestSocketGrantIsSocketOnly` и `TestNothingMayCreateSymlinksOrSpecialFiles`. REMOVE_FILE на
  всём `/tmp` — это право удалять чужие файлы в нём, то есть уже не «только сокет»; возможно,
  нужен отдельный вид гранта или удаление, ограниченное именем (Landlock так не умеет —
  ограничение только по иерархии).
- **Не замерена и не построена.** Нужен замер на том же образе (`scripts/document-sandbox-measure.sh`),
  не рассуждение: S1-S5, F1, V1, N2, плюс «pipe residue: absent» и проверка, что чужой файл в
  `/tmp` не удаляется (или честная запись, что удаляется).

## Что это даст, если замер выйдет хорошим

- У `soffice` нет `rw:/tmp`: класс `document` перестаёт быть разделяемым-writable-`/tmp`;
- `sharedTmpGuard` и sweep для `document` не нужны;
- `DOCUMENT_WORKER_CONCURRENCY` можно поднять выше 1 (после замера нагрузки, отдельно).

## Связи

- `.planning/research/SANDBOX-PATHS.md` «Право на сокет и маршрут /tmp (D-09)»,
  `evidence/soffice-ipc-spike.txt`, замер 70-02.
- `.planning/todos/pending/2026-09-30-sweep-tests-must-not-touch-host-tmp.md` — тот же sweep,
  та же причина; при сужении гранта этот todo исчезает вместе с sweep'ом.
