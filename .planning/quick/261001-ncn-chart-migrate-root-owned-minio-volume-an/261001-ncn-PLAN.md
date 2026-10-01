---
phase: quick-261001-ncn
plan: 01
wave: 1
depends_on: []
quick_id: 261001-ncn
type: execute
description: Два P1 от Codex на PR #31 — (1) chart сам переводит данные MinIO, записанные прежним root-образом, на uid 65532 init-контейнером до старта сервера; (2) minio.mcImage возвращён как необязательный override для каждого контейнера, запускающего mc, с откатом на minio.image
files_modified:
  - deploy/chart/octoconv/templates/minio.yaml
  - deploy/chart/octoconv/templates/job-createbucket.yaml
  - deploy/chart/octoconv/templates/_helpers.tpl
  - deploy/chart/octoconv/values.yaml
  - scripts/ci-helm-checks.sh
  - internal/e2e/minio_image_pin_test.go
  - README.md
  - .planning/quick/261001-ncn-chart-migrate-root-owned-minio-volume-an/evidence/dockerdev-before.txt
  - .planning/quick/261001-ncn-chart-migrate-root-owned-minio-volume-an/evidence/tool-measure.txt
  - .planning/quick/261001-ncn-chart-migrate-root-owned-minio-volume-an/evidence/ownership-probe.sh
  - .planning/quick/261001-ncn-chart-migrate-root-owned-minio-volume-an/evidence/ownership-probe.txt
  - .planning/quick/261001-ncn-chart-migrate-root-owned-minio-volume-an/evidence/guard-test.txt
autonomous: true
requirements:
  - CODEX-PR31-P1-minio-legacy-pvc-ownership
  - CODEX-PR31-P1-minio-mcimage-compat

estimate:
  tokens: 90000
  raw_tokens: 90000
  tasks: 3
  confidence: low

must_haves:
  truths:
    - "Том в форме local-path PVC, записанного прежним root-образом (корень 0777, КАЖДЫЙ элемент uid 0, бакет и объект созданы MinIO от root), валит закреплённый MinIO при старте (контроль: контейнер завершается, в логе `Unable to write to the backend`); после init-контейнера migrate-data-ownership, запущенного под docker с argv, пользователем, capabilities, read-only rootfs, no-new-privileges и лимитами, ВЗЯТЫМИ ИЗ helm-рендера, тот же MinIO отвечает `mc ready local` (команда healthcheck compose), отдаёт старый объект байт-в-байт и принимает новую запись"
    - "Повторный запуск init-контейнера на уже переведённом томе не меняет НИЧЕГО: снимок stat (uid:gid, mode, ctime с наносекундами, inode, путь) до и после побайтно равен; на свежем томе первый запуск меняет только корень тома, симлинк и элемент 0:65532, второй — ничего"
    - "init-контейнер работает от uid 0, все capabilities сброшены кроме CHOWN, allowPrivilegeEscalation false, readOnlyRootFilesystem true, лимиты 100m / 64Mi, exec-форма без шелла и без значений chart'а в argv; меняет только элементы с владельцем uid 0, не следует по симлинкам; на поддереве, которое root без DAC-capabilities прочитать не может, завершается с ненулевым кодом, а не рапортует успех"
    - "minio.mcImage снова есть в values.yaml, пустой по умолчанию; при пустом КАЖДЫЙ контейнер, запускающий mc (wait-for-minio и createbucket в Job, wait-for-bucket в каждом app-Deployment), рендерит minio.image; при заданном — ровно этот override, а Deployment minio (сервер и migrate-data-ownership) остаётся на minio.image; scripts/ci-helm-checks.sh проверяет оба случая на рендере, без пустых image:"
    - "TestMinioImagePinnedByDigest был красным на шаблонах без mcImage и стал зелёным: строки image: с .Values.minio.* в шаблонах — ровно одна из двух форм; каждое значение mcImage в values*.yaml пустое или закреплено по digest без тега; проверка, нашедшая ноль строк mcImage, падает"
    - "`GOTOOLCHAIN=go1.26.4 scripts/ci-local.sh helm gate` даёт helm PASS и gate PASS на закоммиченном HEAD без отслеживаемых изменений; контейнеров и томов octoconv-ncn-* после работы нет; набор контейнеров dockerdev совпадает с записанным до начала"
  artifacts:
    - path: deploy/chart/octoconv/templates/minio.yaml
      provides: "init-контейнер migrate-data-ownership в поде MinIO: chown -R -h --from=0 65532:65532 по тому данных, от root с одной capability CHOWN"
      contains: "migrate-data-ownership"
    - path: deploy/chart/octoconv/values.yaml
      provides: "minio.mcImage — необязательный образ для контейнеров с mc, пустой по умолчанию"
      contains: "mcImage: \"\""
    - path: deploy/chart/octoconv/templates/job-createbucket.yaml
      provides: "wait-for-minio и createbucket берут mcImage с откатом на minio.image"
      contains: "image: {{ .Values.minio.mcImage | default .Values.minio.image | quote }}"
    - path: deploy/chart/octoconv/templates/_helpers.tpl
      provides: "wait-for-bucket в octoconv.waitForDeps берёт mcImage с откатом на minio.image"
      contains: "image: {{ .Values.minio.mcImage | default .Values.minio.image | quote }}"
    - path: scripts/ci-helm-checks.sh
      provides: "Рендер-гейты: форма init-контейнера миграции; mc-контейнеры при пустом и заданном mcImage"
      contains: "minio.mcImage"
    - path: internal/e2e/minio_image_pin_test.go
      provides: "Гейт форм строк image: и закреплённости непустого mcImage"
      contains: "mcImage"
  key_links:
    - from: deploy/chart/octoconv/templates/minio.yaml (initContainer migrate-data-ownership)
      to: deploy/chart/octoconv/templates/minio.yaml (container minio)
      via: "тот же volume minio-data, тот же mountPath (последний аргумент chown = mountPath), тот же образ .Values.minio.image; держит раздел ci-helm-checks.sh"
      pattern: "migrate-data-ownership"
    - from: deploy/chart/octoconv/templates/job-createbucket.yaml
      to: deploy/chart/octoconv/values.yaml
      via: "mcImage с откатом на minio.image; держат TestMinioImagePinnedByDigest (форма строки) и ci-helm-checks.sh (значение в рендере)"
      pattern: "\\.Values\\.minio\\.mcImage \\| default \\.Values\\.minio\\.image"
    - from: deploy/chart/octoconv/templates/_helpers.tpl
      to: deploy/chart/octoconv/values.yaml
      via: "wait-for-bucket во всех app-Deployment'ах — тот же override"
      pattern: "\\.Values\\.minio\\.mcImage \\| default \\.Values\\.minio\\.image"
---

<!-- planner-discipline-allow: TRACKED CHANGES NOT IN THIS COMMIT -->
<!-- planner-discipline-allow: Co-Authored-By -->
<!-- planner-discipline-allow: co-authored-by|generated with -->

<objective>
Закрыть два P1 от Codex на PR #31 (ветка quick-minio-chainguard уже выбрана; PR перевёл MinIO на `cgr.dev/chainguard/minio@sha256:fa23f6a6f62645654530ff94aa077d1cc0d0e44c8f1cce02ab039873612edc72` — дальше REF, работает от uid 65532, несёт mc):

1. minio.yaml:53 — на hostPath/local-path fsGroup не применяется, и PVC, заполненный прежним root-образом, валит non-root MinIO на root-владельце `.minio.sys`. Решение: init-контейнер `migrate-data-ownership` в поде MinIO.
2. job-createbucket.yaml:61 — операторы, задавшие `minio.mcImage`, молча потеряли его. Решение: вернуть `minio.mcImage` как необязательное значение (пустое по умолчанию) для КАЖДОГО контейнера, запускающего mc, с откатом на `minio.image`.

Решения планировщика (на его усмотрение, с замером 2026-10-01 на REF, локально, `--network none`):
- Инструмент. В REF есть `/usr/bin/chown` → coreutils, `chown (GNU coreutils) 9.11` с `--from` и `-h`; есть stat, ls, head, cut, tr, sort, bash 5.3 (`/bin/sh` → bash); НЕТ find, xargs, busybox, tar. find не нужен: `chown -R -h --from=0 65532:65532 /data` — один обход на C, который меняет владельца ТОЛЬКО у элементов с владельцем uid 0 и не делает ни одного системного вызова chown на остальных. Поэтому на переведённом томе он ничего не меняет (ctime не трогается), и не нужны ни разбор `ls`, ни отдельная проверка. Init берёт тот же `minio.image`: нового образа, реестра и пина нет, а кто зеркалирует minio.image, уже имеет и этот образ. busybox по digest отвергнут: chart получил бы третий образ, который каждому оператору с приватным зеркалом пришлось бы зеркалировать, — тот же класс несовместимости, что и P1 №2.
- Объём миграции — элементы с владельцем uid 0, а не «всё, что не 65532». Прежний образ работал от root, поэтому всё, что он записал, принадлежит uid 0. Новый образ пишет как 65532. Элемент третьего uid (например, оператор раньше переопределял minio.image на образ с другим uid) chart'у не принадлежит. Слепой chown всего дерева на 65532 сломал бы том такого оператора, а `--from=0` его не трогает. Это сужение буквы задания ради её цели — записать в SUMMARY как отклонение.
- Capabilities — только CHOWN (+ drop ALL). Всё, что записал прежний root-образ, принадлежит root, и root обходит это дерево как владелец без DAC-capabilities. Каталоги, которые создаёт MinIO от 65532, имеют режим 0755 или 0777, их root обходит как «прочие» (executor замеряет режимы на фикстуре). DAC_READ_SEARCH отвергнут: Pod Security Standard baseline его не разрешает (CHOWN разрешает), поэтому под MinIO в namespace с меткой baseline перестал бы создаваться. Прежний chart baseline проходил. FOWNER для chown(2) не нужен (capabilities(7)). Граница: поддерево, которое root без DAC-capabilities прочитать не может, роняет init ненулевым кодом. Executor это доказывает, и это записывается.
- Exec-форма `command: [chown, -R, -h, --from=0, "65532:65532", /data]` без шелла: нет зависимости от /bin/sh в образе, нет места, куда можно подставить значение chart'а. `"65532:65532"` в кавычках, чтобы YAML-парсер никогда не прочёл это как число в шестидесятеричной записи.
- mc-образ: строка `image: {{ .Values.minio.mcImage | default .Values.minio.image | quote }}` в трёх контейнерах (без нового named template — точную строку держит гейт-тест). `default` откатывается и на "", и на отсутствующий ключ.
- Комментарии: НОВЫХ комментариев не добавлять (глобальное правило пользователя). Существующий комментарий над securityContext пода MinIO уже называет внешнее ограничение (hostPath/local-path не применяют fsGroup). Править разрешено только те существующие фразы, которые эта работа делает ложными (перечислены в задачах). Обоснования идут в «PR body addendum» в SUMMARY.

Purpose: апгрейд chart'а поверх PVC от прежнего root-образа больше не оставляет MinIO в падении, а операторы с отдельным mc-образом снова получают его во всех mc-контейнерах.
Output: правки chart'а, рендер-гейты в scripts/ci-helm-checks.sh, расширенный гейт-тест, живая docker-проба с доказательствами, одна строка в README, зелёный `ci-local.sh helm gate`.
</objective>

<execution_context>
@~/.claude/gsd-core/workflows/execute-plan.md
@~/.claude/gsd-core/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@CLAUDE.md
@.planning/quick/261001-5ec-move-minio-images-off-docker-hub-to-cgr-/261001-5ec-SUMMARY.md
@.planning/quick/261001-5ec-move-minio-images-off-docker-hub-to-cgr-/evidence/rootvol-probe.txt
@deploy/chart/octoconv/templates/minio.yaml
@deploy/chart/octoconv/templates/job-createbucket.yaml
@internal/e2e/minio_image_pin_test.go

Где что лежит (не читать файлы целиком без нужды):
- deploy/chart/octoconv/templates/minio.yaml: pod `spec:` — строки 47-101; существующий комментарий + `securityContext:` (fsGroup 65532, fsGroupChangePolicy OnRootMismatch) — 48-58; `containers:` — 59; volumeMount `minio-data` → `/data` — 78-80; args `server /data --console-address ":9001"`.
- deploy/chart/octoconv/templates/job-createbucket.yaml: комментарий над initContainers — 52-58 (фраза «from the same pinned image as MinIO itself -- mc is in it» станет ложной); `image:` wait-for-minio — 61, createbucket — 72.
- deploy/chart/octoconv/templates/_helpers.tpl: блок-комментарий octoconv.waitForDeps — 220-276 (фраза в 266-268 «the digest-pinned MinIO image for mc -- it carries mc, and the createbucket Job uses it too» станет неполной); `image:` wait-for-bucket — 310; лимиты wait-контейнеров cpu "100m", memory "64Mi" — 305-308.
- deploy/chart/octoconv/values.yaml: блок `minio:` — 268-280; фраза «so the MinIO Deployment, the createbucket Job and the wait-for-bucket init container of every app Deployment all read this one value» станет ложной; `image.pullPolicy` — строка 14.
- internal/e2e/minio_image_pin_test.go: doc-комментарий 12-28 (фраза 16-18 «use that single image for the server, the createbucket job and the chart's wait-for-bucket init container» станет ложной); `minioSepMCKeyRe` и два правила, ЗАПРЕЩАЮЩИЕ mcImage (151-154, 169-179), — теперь неверны по смыслу; счётчик шаблонных строк `tplLines < 4` — 166.
- scripts/ci-helm-checks.sh: `CHART`, `SECRETS_SET`; разделы `echo "### ..."`; yq (mikefarah v4.54.1) уже используется; `set -euo pipefail` — каждый `grep -c` с `|| true` (см. существующие комментарии о SIGPIPE/pipefail).
- README.md:155-171 — `> [!NOTE]` про образ MinIO и ручной chown тома compose.

Замеры планировщика 2026-10-01 (executor ПЕРЕМЕРЯЕТ и пишет свои числа):
- REF локально: Id sha256:8c32a7e0f535…; User 65532; Entrypoint /usr/bin/minio; /data в образе `drwxrwxrwx root root` и НЕ пуст (docker копирует его содержимое в пустой именованный том при первом монтировании на /data — поэтому фикстура «прежний root-образ» засевается через монтирование на /seed, где копирования нет).
- chown → coreutils, GNU 9.11, `--from` и `--no-dereference` есть; find, xargs, busybox, tar отсутствуют; cut, tr, sort есть; bash 5.3.0.
- Проба 261001-5ec (rootvol-probe.txt): non-root MinIO на томе, засеянном root, падает: `unable to rename (/data/.minio.sys/tmp -> …) file access denied` → `FATAL Unable to initialize backend: Unable to write to the backend`; `mc ready` зависает, если сервер умер внутри того же контейнера, — поэтому сервер здесь запускается PID 1 в `docker run -d`, и его смерть останавливает контейнер вместе с `docker exec`.
- yq-выражение, классифицирующее контейнеры рендера (проверено на дефолтном рендере, 13 mc-контейнеров: 11 wait-for-bucket + wait-for-minio + createbucket; сервер minio — false): `select(.kind=="Deployment" or .kind=="Job") | .metadata.name as $d | .spec.template.spec | ((.initContainers // []) + .containers)[] | $d + " " + .name + " " + .image + " " + (((.command // []) + (.args // [])) | join(" ") | test("(^|[^a-z])mc (alias|ready|mb|ls)") | tostring)`, затем `grep -v '^---$'`.
- Пространства имён chart'а меток pod-security.kubernetes.io не несут; ни у одного пода, кроме MinIO (fsGroup), securityContext нет.
- OrbStack Kubernetes выключен: chart только рендерится; миграция доказывается docker-запуском ОТРЕНДЕРЕННОГО контейнера.

ПРАВИЛА БЕЗОПАСНОСТИ ХОСТА (на весь план):
- Стек `dockerdev-*` работает: никогда не останавливать/удалять его контейнеры, образы, тома; никаких `prune` и `docker compose -p dockerdev`.
- Создавать только контейнеры и тома с префиксом `octoconv-ncn-` и удалять их только по точным именам. Перед созданием убедиться, что таких имён нет; если есть — остановиться и сообщить, не удалять. Образы не тянуть и не удалять: REF уже локальный, новых образов план не вводит; никаких `docker pull`, `--platform`, `docker rmi`.
- Ни одного опубликованного порта: пробы работают с `--network none` (у MinIO внутри есть loopback для `mc ready local`). Compose-стек octoconv не поднимать (compose не меняется, e2e-уровень не нужен).
- Ожидание — только внутри контейнеров (ограниченные циклы с sleep внутри `docker exec … sh -c`), без sleep на хосте; macOS/BSD-шелл, бинаря `timeout` нет, в grep `[[:space:]]`.
- Go-команды — с `GOTOOLCHAIN=go1.26.4`.
- Коммиты: явные пути (никакого `git add -A`: в дереве неотслеживаемые .gsd/, .vscode/, octoconv.code-workspace), без трейлера Co-Authored-By и без любой атрибуции агента; не пушить. `.planning/` в .gitignore — доказательства и SUMMARY коммитит оркестратор (`git add -f`).
</context>

<tasks>

<task type="tracer">
  <name>Task 1: Сквозной путь миграции — init-контейнер в chart → helm-рендер → docker-запуск отрендеренного контейнера на томе от прежнего root-образа → закреплённый MinIO ready, данные целы; повтор ничего не меняет</name>
  <files>deploy/chart/octoconv/templates/minio.yaml, scripts/ci-helm-checks.sh, .planning/quick/261001-ncn-chart-migrate-root-owned-minio-volume-an/evidence/dockerdev-before.txt, .planning/quick/261001-ncn-chart-migrate-root-owned-minio-volume-an/evidence/tool-measure.txt, .planning/quick/261001-ncn-chart-migrate-root-owned-minio-volume-an/evidence/ownership-probe.sh, .planning/quick/261001-ncn-chart-migrate-root-owned-minio-volume-an/evidence/ownership-probe.txt</files>
  <precondition>`git rev-parse --abbrev-ref HEAD` = quick-minio-chainguard и `git status --porcelain --untracked-files=no` пуст; `docker image inspect cgr.dev/chainguard/minio@sha256:fa23f6a6f62645654530ff94aa077d1cc0d0e44c8f1cce02ab039873612edc72` резолвится локально; `docker ps -aq --filter name=octoconv-ncn` и `docker volume ls -q --filter name=octoconv-ncn` пусты. Иначе остановиться и сообщить.</precondition>
  <reversibility rating="reversible">Правка шаблона и скрипта проверок; тома и контейнеры пробы одноразовые, удаляются по точным именам.</reversibility>
  <action>
Каталог доказательств: .planning/quick/261001-ncn-chart-migrate-root-owned-minio-volume-an/evidence/ (дальше EV). Писать сырые выводы команд, не пересказ.

1. Базовая линия: `docker ps --filter name=dockerdev --format '{{.Names}}' | sort` → EV/dockerdev-before.txt.

2. Перемерить инструменты в REF → EV/tool-measure.txt (`docker run --rm --network none --entrypoint /bin/sh REF -c '…'`): `command -v` для chown, find, stat, ls, xargs, busybox, ln, mkdir, touch, chmod; `chown --version | head -1`; есть ли `--from=` и `--no-dereference` в `chown --help` (сопоставление через case в шелле — grep в образе нет); `echo "$PATH"`. Если `--from` нет — остановиться и сообщить: весь дизайн из objective стоит на нём.

3. deploy/chart/octoconv/templates/minio.yaml — в pod spec между существующим `securityContext:` и `containers:` добавить `initContainers:` с одним контейнером (без комментариев):
   - name `migrate-data-ownership`; image `{{ .Values.minio.image | quote }}` (ровно та же строка, что у контейнера minio — её держит гейт-тест); imagePullPolicy `{{ .Values.image.pullPolicy }}`.
   - command — exec-форма, по элементу на строку: `chown`, `-R`, `-h`, `--from=0`, `"65532:65532"` (в кавычках), `/data`. Без шелла, без значений .Values в argv.
   - securityContext: runAsUser 0; allowPrivilegeEscalation false; readOnlyRootFilesystem true; capabilities drop `["ALL"]`, add `["CHOWN"]`.
   - resources.limits: cpu "100m", memory "64Mi" (как у wait-контейнеров в _helpers.tpl).
   - volumeMounts: name minio-data, mountPath /data.
   Существующий комментарий над securityContext, fsGroup/fsGroupChangePolicy, readinessProbe и всё прочее не трогать.

4. Скрипт пробы EV/ownership-probe.sh (bash, `set -euo pipefail`, запуск из корня репозитория; печатает заголовки шагов и сырые выводы; завершается 0 и последней строкой `RESULT: PASS` только если прошли ВСЕ утверждения ниже; при любом провале — ненулевой код с указанием шага). Требования:
   a. Тот же объект, что в chart: REF = `yq '.minio.image' deploy/chart/octoconv/values.yaml`; рендер `helm template octoconv deploy/chart/octoconv -f deploy/chart/octoconv/values-local.yaml --show-only templates/minio.yaml`; из Deployment yq'ом взять init-контейнер, монтирующий minio-data, и из НЕГО построить флаги docker: `-u` из runAsUser; `--cap-drop ALL` и `--cap-add` на каждый элемент capabilities.add; `--security-opt no-new-privileges`, если allowPrivilegeEscalation false; `--read-only`, если readOnlyRootFilesystem true; `--memory` из limits.memory (64Mi → 64m); `--cpus` из limits.cpu (100m → 0.1); `-v VOL:<mountPath>`; `--network none`; `--entrypoint <command[0]>` REF `<command[1:]…>`. Ни одного флага руками — иначе проба доказывает не то, что отрендерено. Напечатать собранную команду.
   b. Защита имён: в начале проверить, что контейнеров octoconv-ncn-minio, octoconv-ncn-control и томов octoconv-ncn-legacy, octoconv-ncn-fresh, octoconv-ncn-boundary не существует (иначе выход без удаления). trap на EXIT удаляет ровно эти имена (`docker rm -f` контейнеров, `docker volume rm` томов, ошибки «нет такого» глушить).
   c. Снимок тома (функция): `docker run --rm --network none -u 0 -v VOL:/data --entrypoint /bin/bash REF -c 'shopt -s globstar dotglob nullglob; stat -c "%u:%g %a %z %i %n" /data /data/**'` (stat без -L описывает симлинк, а не цель).
   d. Фикстура «прежний root-образ» (том octoconv-ncn-legacy, засеять через монтирование на /seed, чтобы docker НЕ скопировал в том содержимое /data образа): одним `docker run --rm --network none -u 0 -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin -v octoconv-ncn-legacy:/seed --entrypoint /bin/bash REF -c '…'` — `chmod 0777 /seed` (форма каталога local-path); `minio server /seed` в фоне; ограниченный цикл `mc ready local` (до 30 попыток по 1 с, перед каждой — `kill -0` сервера); `mc alias set p http://localhost:9000 minioadmin minioadmin`; `mc mb p/probe`; положить объект `probe/f.txt` с известным содержимым; остановить сервер (kill, wait). Снимок: каждый элемент, кроме корня тома, — uid 0 (утвердить); записать режимы каталогов.
   e. Контроль (дефект воспроизводится): `docker run -d --name octoconv-ncn-control --network none -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin -v octoconv-ncn-legacy:/data REF server /data --console-address :9001`; `docker exec octoconv-ncn-control sh -c '<ограниченный цикл mc ready local, до 30 с>'` должен НЕ дойти до ready (контейнер умирает → exec падает); затем `docker inspect -f '{{.State.Running}} {{.State.ExitCode}}'` → `false` и ненулевой код, хвост `docker logs` содержит `Unable to write to the backend`. Если контрольный MinIO стал ready — выход с ошибкой «фикстура не воспроизводит дефект» (положительный результат тогда ничего не значит). Удалить octoconv-ncn-control.
   f. Миграция: снимок «до» → запуск отрендеренного init (пункт a) на octoconv-ncn-legacy, код 0 → снимок «после». Утвердить: в «после» нет ни одного элемента с uid 0; каждый элемент, бывший uid 0, теперь 65532:65532 с тем же mode и inode; элементы, которые уже не были uid 0 (например, tmp-old от контрольного запуска), не изменились ни в uid:gid, ни в ctime.
   g. Сервер на переведённом томе, как в compose: `docker run -d --name octoconv-ncn-minio --network none -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin -v octoconv-ncn-legacy:/data REF server /data --console-address :9001`; `docker exec` с ограниченным циклом `mc ready local` (команда healthcheck compose) → ready; `docker exec` → `mc alias set p http://localhost:9000 minioadmin minioadmin`, `mc cat p/probe/f.txt` = исходное содержимое, запись нового объекта (`mc pipe` или `mc cp`) с кодом 0, `mc ls p/probe` показывает оба; `docker exec octoconv-ncn-minio id -u` → 65532. Удалить octoconv-ncn-minio (после этого на томе ничего не работает).
   h. Идемпотентность: снимок → второй запуск отрендеренного init → снимок; `diff` двух снимков пуст (ни владелец, ни ctime, ни inode не изменились — значит, chown на этих элементах не вызывался).
   i. Свежий том (octoconv-ncn-fresh; первое монтирование на /data, docker копирует /data образа): root-контейнер без пользовательских флагов добавляет `ln -s /etc/passwd /data/link` и файл `/data/regrouped` с владельцем 0:65532 (форма того, что kubelet оставляет после перегруппировки fsGroup); снимок → init → снимок: изменились ровно корень тома (если он был uid 0), `link` (сам симлинк, lstat) и `regrouped` — все стали 65532:65532, init завершился с кодом 0 при read-only rootfs (то есть `/etc/passwd` в контейнере не трогался); второй запуск init → diff снимков пуст.
   j. Граница (octoconv-ncn-boundary, засеять через /seed от root): каталог `locked` mode 0700 владелец 1234:1234, внутри файл с владельцем 0:0. Отрендеренный init должен завершиться с НЕНУЛЕВЫМ кодом и сообщением о Permission denied/cannot read directory. Это доказательство, что поддерево, которое он не смог прочитать, не превращается в рапорт об успехе. Записать как замеренную границу.
   k. Если в f, g или i отрендеренный init с одной CHOWN падает (вопреки модели из objective), добавлять capabilities по одной: сначала FOWNER, потом DAC_READ_SEARCH (последний конфликтует с PSS baseline — записать это). Записывать сообщение об ошибке на каждом шаге и оставить в шаблоне минимальный набор, при котором проходят f/g/i. Ожидаемый исход — одна CHOWN.
   Запустить: `bash EV/ownership-probe.sh 2>&1 | tee EV/ownership-probe.txt`; последняя строка — `RESULT: PASS`.

5. scripts/ci-helm-checks.sh — новый раздел после проверки «every Deployment that gets the app secret must wait for BOTH startup deps» (только строки `echo "### …"` / `echo "OK: …"` / `echo "FAIL: …"`, НИ ОДНОЙ новой строки-комментария `#`): `echo "### the MinIO pod hands root-owned data to uid 65532 before the server starts"`. Одним проходом yq по уже существующему `$DEFAULT_RENDER` построить строку-сводку по Deployment minio и сравнить её с ожидаемой ТОЧНО (при несовпадении напечатать обе и exit 1): число init-контейнеров, монтирующих minio-data (=1); образ init равен образу контейнера minio; mountPath init равен mountPath minio-data контейнера minio; последний аргумент command равен этому mountPath; runAsUser; allowPrivilegeEscalation; readOnlyRootFilesystem; drop; add; аргумент владельца в command; fsGroup пода. Ожидаемая строка вида `inits=1 image=same mount=/data target=/data user=0 escalation=false rorf=true drop=ALL add=CHOWN owner=65532:65532 fsGroup=65532` (точный формат выбирает executor; набор add — из пункта 4k). Если Deployment minio в рендере не найден — FAIL (иначе проверка проходит на пустом месте). Проба инверсии (не коммитить): временно убрать `add` или поменять mountPath init в minio.yaml → раздел падает; откат через `git checkout -- deploy/chart/octoconv/templates/minio.yaml`, затем вернуть правку. Если правка ещё не закоммичена — делать инверсию после коммита из пункта 6 и откатывать через git. Вывод инверсии дописать в EV/ownership-probe.txt.

6. `helm lint deploy/chart/octoconv -f deploy/chart/octoconv/values-local.yaml` и `scripts/ci-helm-checks.sh` зелёные; `GOTOOLCHAIN=go1.26.4 go test -count=1 -run TestMinioImage ./internal/e2e/` зелёный (существующий гейт уже пропускает вторую строку `image: {{ .Values.minio.image | quote }}`). Коммит двух файлов (deploy/chart/octoconv/templates/minio.yaml, scripts/ci-helm-checks.sh): `fix(chart): hand MinIO data written by the old root image to uid 65532 before the server starts`, без трейлеров и атрибуции.
  </action>
  <verify>
    <automated>EV=.planning/quick/261001-ncn-chart-migrate-root-owned-minio-volume-an/evidence; bash "$EV/ownership-probe.sh" > "$EV/ownership-probe.verify.txt" 2>&1 && test "$(tail -1 "$EV/ownership-probe.verify.txt")" = 'RESULT: PASS' && test -z "$(docker ps -aq --filter name=octoconv-ncn)" && test -z "$(docker volume ls -q --filter name=octoconv-ncn)" && M="$(helm template octoconv deploy/chart/octoconv -f deploy/chart/octoconv/values-local.yaml --show-only templates/minio.yaml)" && test "$(printf '%s\n' "$M" | yq 'select(.kind=="Deployment") | .spec.template.spec.initContainers[] | select(.name=="migrate-data-ownership") | .securityContext.capabilities.drop | join(",")')" = ALL && test "$(printf '%s\n' "$M" | yq 'select(.kind=="Deployment") | .spec.template.spec.initContainers[] | select(.name=="migrate-data-ownership") | .command | join(" ")')" = 'chown -R -h --from=0 65532:65532 /data' && helm lint deploy/chart/octoconv -f deploy/chart/octoconv/values-local.yaml && scripts/ci-helm-checks.sh && GOTOOLCHAIN=go1.26.4 go test -count=1 -run TestMinioImage ./internal/e2e/ && docker ps --filter name=dockerdev --format '{{.Names}}' | sort | diff - "$EV/dockerdev-before.txt"</automated>
  </verify>
  <done>Отрендеренный init-контейнер, запущенный под docker с флагами из рендера: на томе от прежнего root-образа (контроль падает с `Unable to write to the backend`) переводит uid 0 на 65532:65532, после чего закреплённый MinIO ready, отдаёт старый объект и пишет новый; повтор ничего не меняет (ctime-снимки равны); симлинк не разыменован; нечитаемое поддерево даёт ненулевой код; раздел ci-helm-checks.sh держит форму init-контейнера и падает на инверсии; два файла закоммичены; проба за собой убрала; dockerdev не тронут.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: minio.mcImage — необязательный override для каждого mc-контейнера с откатом на minio.image; гейт-тест и рендер-гейт обоих случаев</name>
  <files>internal/e2e/minio_image_pin_test.go, deploy/chart/octoconv/values.yaml, deploy/chart/octoconv/templates/job-createbucket.yaml, deploy/chart/octoconv/templates/_helpers.tpl, scripts/ci-helm-checks.sh, .planning/quick/261001-ncn-chart-migrate-root-owned-minio-volume-an/evidence/guard-test.txt</files>
  <behavior>
    - TestMinioImagePinnedByDigest, шаблоны: каждая строка (после TrimSpace) с префиксом `image:`, ссылающаяся на `.Values.minio.`, равна ровно одной из двух форм — серверной `image: {{ .Values.minio.image | quote }}` или mc-формы `image: {{ .Values.minio.mcImage | default .Values.minio.image | quote }}`; серверных строк не меньше 2 (контейнер minio и migrate-data-ownership), mc-строк не меньше 3 (wait-for-minio, createbucket, wait-for-bucket). Сообщение называет файл:строку. До правки шаблонов (после Task 1) тест ПАДАЕТ на счётчике mc-строк (0 < 3) — это RED.
    - TestMinioImagePinnedByDigest, values: каждая некомментарийная строка `mcImage:` в каждом deploy/chart/octoconv/values*.yaml (кавычки и хвостовой ` #…` снимаются) — пустая или проходит mcImageRefProblem; строк mcImage найдено не меньше одной (иначе тест падает: проверка ноля строк — провал, а не успех). До правки values.yaml — RED.
    - Прежние правила, запрещавшие отдельный mc-ключ (`minioSepMCKeyRe` и два его использования), удалены — они противоречат восстановленному значению. Правила для image: в compose и values (≥2 / ≥1, одна закреплённая строка, равенство) не меняются.
    - mcImageRefProblem(ref string) string (новый unexported helper, без doc-комментария): "" для пустой строки и для `<имя>@sha256:<64 строчных hex>`, где имя непустое и в его последнем сегменте пути (после последнего `/`) нет `:` (тег перед digest запрещён — D-02; порт реестра в первом сегменте разрешён); иначе — причина. Таблица TestMcImageRefRejectsUnpinnedForms: принимает "", REF, `registry.example:5000/mirror/mc@sha256:` + 64 hex; отвергает `minio/mc:latest`, `registry.example:5000/mirror/mc:RELEASE.2025-08-13T08-35-41Z@sha256:` + 64 hex, digest из 8 hex, digest в верхнем регистре, `@sha256:` + 64 hex без имени.
  </behavior>
  <action>
1. RED. Изменить internal/e2e/minio_image_pin_test.go по behavior (только stdlib; без новых комментариев). В doc-комментарии файла поправить ровно ту фразу, которая станет ложной («…use that single image for the server, the createbucket job and the chart's wait-for-bucket init container»): mc-контейнеры chart'а берут minio.mcImage, если оно задано, иначе этот же образ. Остальной текст комментария не трогать. Запустить `GOTOOLCHAIN=go1.26.4 go test -count=1 -run 'TestMinioImage|TestMcImage' ./internal/e2e/`: TestMinioImagePinnedByDigest падает на счётчике mc-строк и на отсутствии строк mcImage, обе таблицы проходят. Вывод → EV/guard-test.txt (EV = .planning/quick/261001-ncn-chart-migrate-root-owned-minio-volume-an/evidence).

2. deploy/chart/octoconv/values.yaml, блок minio: сразу после строки `image:` добавить `mcImage: ""`. Фразу «so the MinIO Deployment, the createbucket Job and the wait-for-bucket init container of every app Deployment all read this one value» переписать по смыслу: Deployment MinIO читает это значение, а контейнеры, запускающие mc (Job createbucket, wait-for-bucket в каждом app-Deployment), читают mcImage или это значение, если mcImage пусто. Других строк комментария не добавлять; image, storage, rootUser, rootPassword не трогать.

3. deploy/chart/octoconv/templates/job-createbucket.yaml: обе строки `image:` (wait-for-minio, createbucket) → mc-форма `image: {{ .Values.minio.mcImage | default .Values.minio.image | quote }}`. В комментарии над initContainers фразу «from the same pinned image as MinIO itself -- mc is in it» поправить так, чтобы она оставалась правдой: из minio.mcImage, а если оно пустое — из закреплённого образа MinIO, в котором mc есть. Команды, env, hook-аннотации не трогать.

4. deploy/chart/octoconv/templates/_helpers.tpl, wait-for-bucket в `octoconv.waitForDeps`: `image:` → та же mc-форма. Во фразе блок-комментария (строки 266-268) mc-образ назвать так: minio.mcImage, а по умолчанию — закреплённый образ MinIO, который несёт mc. Остальное не трогать (команда, env, лимиты).

5. scripts/ci-helm-checks.sh — новый раздел после раздела «every mc invocation must use the transport s3.useSSL asks for» (без строк-комментариев `#`): `echo "### every container that runs mc uses minio.mcImage when set, minio.image when it is empty"`. MINIO_IMAGE = `yq '.minio.image' "$CHART/values.yaml"` (значение из values, не литерал); OVERRIDE = `registry.example.invalid/mirror/mc@sha256:` + 64 символа `a`. Два рендера с `$SECRETS_SET`: без override и с `--set minio.mcImage=$OVERRIDE`. Для каждого — строки «workload container image runsMc» тем yq-выражением, что в context. Утверждения (каждое с FAIL-сообщением и exit 1):
   - в обоих рендерах нет контейнера с пустым image;
   - классификатор не слеп: каждый контейнер с именем wait-for-minio, createbucket или wait-for-bucket помечен runsMc=true, и mc-контейнеров больше нуля;
   - множество пар «workload container» с runsMc=true одинаково в обоих рендерах;
   - без override: образ каждого mc-контейнера = MINIO_IMAGE;
   - с override: образ каждого mc-контейнера = OVERRIDE; ни один контейнер с runsMc=false не несёт OVERRIDE; оба контейнера Deployment minio (minio, migrate-data-ownership) = MINIO_IMAGE.
   `echo "OK: …"` с числом mc-контейнеров. Каждый `grep -c` с `|| true` (pipefail).

6. GREEN: тест из шага 1 проходит; `gofmt -l internal/e2e` пуст; `GOTOOLCHAIN=go1.26.4 go vet ./internal/e2e/` чист; проверки из verify зелёные. Проверка, что новые строки-комментарии не появились: `git diff -U0 HEAD -- scripts/ci-helm-checks.sh deploy/chart/octoconv internal/e2e | grep -E '^\+[[:space:]]*(#|//|\{\{/\*)'` должна показывать только правки уже существующих фраз из шагов 1-4 — записать вывод в EV/guard-test.txt.

7. Коммит пяти файлов (internal/e2e/minio_image_pin_test.go, deploy/chart/octoconv/values.yaml, deploy/chart/octoconv/templates/job-createbucket.yaml, deploy/chart/octoconv/templates/_helpers.tpl, scripts/ci-helm-checks.sh): `fix(chart): restore minio.mcImage as an optional override for every mc container`, без трейлеров и атрибуции.

8. Пробы гейтов ПОСЛЕ коммита (откат через git безопасен), выводы → EV/guard-test.txt: (a) в values.yaml временно `mcImage: "minio/mc:latest"` → TestMinioImagePinnedByDigest падает с указанием values.yaml:строка; (b) в _helpers.tpl временно вернуть wait-for-bucket на серверную форму → раздел ci-helm-checks.sh падает (в рендере с override wait-for-bucket не несёт OVERRIDE), а Go-тест падает на счётчике mc-строк (2 < 3); (c) в job-createbucket.yaml временно убрать `| default .Values.minio.image` у одной строки → Go-тест падает (форма строки), и рендер без override даёт пустой image (раздел ловит). После каждой пробы `git checkout --` того файла; в конце `git status --porcelain --untracked-files=no` пуст, тест и скрипт снова зелёные.
  </action>
  <verify>
    <automated>REF="$(yq '.minio.image' deploy/chart/octoconv/values.yaml)"; O=registry.example.invalid/mirror/mc@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa; test "$REF" = cgr.dev/chainguard/minio@sha256:fa23f6a6f62645654530ff94aa077d1cc0d0e44c8f1cce02ab039873612edc72 && test -z "$(yq '.minio.mcImage' deploy/chart/octoconv/values.yaml)" && GOTOOLCHAIN=go1.26.4 go test -count=1 -run 'TestMinioImage|TestMcImage' ./internal/e2e/ && test -z "$(gofmt -l internal/e2e)" && GOTOOLCHAIN=go1.26.4 go vet ./internal/e2e/ && R="$(helm template octoconv deploy/chart/octoconv -f deploy/chart/octoconv/values-local.yaml)" && S="$(helm template octoconv deploy/chart/octoconv -f deploy/chart/octoconv/values-local.yaml --set minio.mcImage=$O)" && test -z "$(printf '%s\n%s\n' "$R" "$S" | grep -nE '^[[:space:]]*image:[[:space:]]*("")?[[:space:]]*$')" && W="$(printf '%s\n' "$R" | grep -A1 -e '- name: wait-for-bucket' | grep -E '^[[:space:]]*image:')" && test "$(printf '%s\n' "$W" | grep -cF "\"$REF\"")" -ge 11 && test -z "$(printf '%s\n' "$W" | grep -vF "\"$REF\"")" && WS="$(printf '%s\n' "$S" | grep -A1 -e '- name: wait-for-bucket' | grep -E '^[[:space:]]*image:')" && test "$(printf '%s\n' "$WS" | grep -cF "\"$O\"")" -ge 11 && test -z "$(printf '%s\n' "$WS" | grep -vF "\"$O\"")" && J="$(helm template octoconv deploy/chart/octoconv -f deploy/chart/octoconv/values-local.yaml --show-only templates/job-createbucket.yaml)" && test "$(printf '%s\n' "$J" | grep -E '^[[:space:]]*image:' | grep -cF "\"$REF\"")" -ge 2 && test -z "$(printf '%s\n' "$J" | grep -E '^[[:space:]]*image:' | grep -vF "\"$REF\"")" && JS="$(helm template octoconv deploy/chart/octoconv -f deploy/chart/octoconv/values-local.yaml --set minio.mcImage=$O --show-only templates/job-createbucket.yaml)" && test "$(printf '%s\n' "$JS" | grep -E '^[[:space:]]*image:' | grep -cF "\"$O\"")" -ge 2 && test -z "$(printf '%s\n' "$JS" | grep -E '^[[:space:]]*image:' | grep -vF "\"$O\"")" && MS="$(helm template octoconv deploy/chart/octoconv -f deploy/chart/octoconv/values-local.yaml --set minio.mcImage=$O --show-only templates/minio.yaml)" && test "$(printf '%s\n' "$MS" | grep -E '^[[:space:]]*image:' | grep -cF "\"$REF\"")" -ge 2 && test -z "$(printf '%s\n' "$MS" | grep -E '^[[:space:]]*image:' | grep -vF "\"$REF\"")" && helm lint deploy/chart/octoconv -f deploy/chart/octoconv/values-local.yaml && scripts/ci-helm-checks.sh && test -z "$(git status --porcelain --untracked-files=no)"</automated>
  </verify>
  <done>Гейт-тест был красным на шаблонах без mcImage и стал зелёным, и падает на тег-только mcImage и на неверной форме строки; рендер: без override все mc-контейнеры (2 в Job + каждый wait-for-bucket) несут REF, с override — ровно его, под MinIO (сервер и migrate-data-ownership) остаётся на REF, пустых image: нет; раздел ci-helm-checks.sh держит оба случая и падает на пробе инверсии; пять файлов закоммичены, дерево чистое.</done>
</task>

<task type="auto">
  <name>Task 3: Строка в README, полный локальный гейт helm+gate, итог и текст для PR</name>
  <files>README.md</files>
  <precondition>Task 1 и Task 2 закоммичены; `git status --porcelain --untracked-files=no` пуст.</precondition>
  <action>
1. README.md, блок `> [!NOTE]` про MinIO (строки ~155-171): после строки «or drop the dev objects with `docker volume rm octoconv_minio_data`.» добавить внутри того же блока пустую строку `>` и одно предложение по-английски, например: «The Helm chart needs no manual step: an init container on the MinIO pod hands root-owned files to uid 65532 before the server starts.» Остальной README не трогать (compose не менялся, ручной chown для тома compose по-прежнему верен). Коммит README.md: `docs: the Helm chart migrates MinIO data ownership itself`, без трейлеров и атрибуции.

2. Полный гейт: `GOTOOLCHAIN=go1.26.4 scripts/ci-local.sh helm gate` (compose не менялся — e2e-уровень не нужен). Долгий прогон — run_in_background или Bash с timeout до 600000 мс; без foreground-sleep. Читать только строки `commit:`, `WARNING`, `### tier …` и хвост последнего `.planning/ci-local/ci-local-*.log`. Нужны `### tier helm: PASS` и `### tier gate: PASS`, коммит транскрипта = HEAD. Предупреждение о версии helm (локальная 4.3.0 против закреплённой в ci.yml) уже было до этой работы, его записать как есть. Любой другой отказ — разобрать, в своей зоне чинить и прогнать заново, постороннее — сообщить как есть.

3. После гейта: `docker ps -aq --filter name=octoconv-ncn` и `docker volume ls -q --filter name=octoconv-ncn` пусты; список dockerdev совпадает с EV/dockerdev-before.txt (EV = .planning/quick/261001-ncn-chart-migrate-root-owned-minio-volume-an/evidence).

4. В SUMMARY (.planning/quick/261001-ncn-chart-migrate-root-owned-minio-volume-an/261001-ncn-SUMMARY.md, по шаблону) кроме обычного: (a) раздел «Deviations» с двумя осознанными сужениями из objective: миграция только элементов с владельцем uid 0 (почему: прежний образ — root, а третий uid не принадлежит chart'у, и слепой chown сломал бы том оператора с другим uid) и capabilities только CHOWN (почему: PSS baseline не разрешает DAC_READ_SEARCH; граница замерена в пункте 4j Task 1); (b) раздел «Not proven»: chart на живом кластере (k8s выключен; fsGroup + init на настоящем local-path PVC не запускались), образы-override без GNU chown `--from` (init тогда падает громко — записать как остаточный риск для оператора, переопределяющего minio.image), amd64 в работе; (c) раздел «PR body addendum» — короткий английский текст для описания PR #31 в стиле существующего body: что делает init-контейнер и почему так (`chown -R -h --from=0`, нет find и нового образа, ничего не меняет на переведённом томе, только CHOWN), что `minio.mcImage` вернулся как необязательное значение для всех mc-контейнеров, чем это проверено (docker-проба отрендеренного контейнера с контролем, идемпотентность по ctime, рендер-гейты обоих случаев mcImage, `ci-local.sh helm gate`), что не проверено. Без атрибуции агента. Обоснования, которые по правилу пользователя не идут в комментарии кода, должны быть здесь.

5. Не пушить, PR не редактировать — это делает оркестратор. Доказательства и SUMMARY не коммитить (их забирает финальный docs-коммит оркестратора через `git add -f`).
  </action>
  <verify>
    <automated>T="$(ls -t .planning/ci-local/ci-local-*.log | head -1)"; C="$(sed -n 's/^commit: *\([0-9a-f]*\).*/\1/p' "$T")"; grep -q '^### tier helm: PASS$' "$T" && grep -q '^### tier gate: PASS$' "$T" && ! grep -q 'TRACKED CHANGES NOT IN THIS COMMIT' "$T" && git diff --quiet "$C" HEAD -- . ':(exclude).planning' && grep -q 'init container on the MinIO pod' README.md && test -z "$(docker ps -aq --filter name=octoconv-ncn)" && test -z "$(docker volume ls -q --filter name=octoconv-ncn)" && docker ps --filter name=dockerdev --format '{{.Names}}' | sort | diff - .planning/quick/261001-ncn-chart-migrate-root-owned-minio-volume-an/evidence/dockerdev-before.txt && L="$(git log --format=%B 90d465a..HEAD)" && ! printf '%s\n' "$L" | grep -qiE 'co-authored-by|generated with' && test "$(git rev-list --count 90d465a..HEAD)" -eq 3</automated>
  </verify>
  <done>README называет автоматическую миграцию в chart; `ci-local.sh helm gate` — helm PASS и gate PASS на HEAD; ровно три новых коммита после 90d465a, без атрибуции; следов пробы нет, dockerdev не тронут; в SUMMARY отклонения, недоказанное и текст для описания PR.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| PVC/том данных MinIO → init-контейнер от root | Содержимое тома (имена, симлинки, режимы) записано процессами, которым init не доверяет, а сам init работает от uid 0 |
| values оператора → шаблоны chart'а | minio.image и minio.mcImage задаёт оператор, они определяют, какие бинарники запускаются в каждом mc-контейнере и в init-контейнере |
| реестр → узел | образы MinIO/mc тянутся по ссылкам из values |

## STRIDE Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation Plan |
|-----------|----------|-----------|----------|-------------|-----------------|
| T-261001-ncn-SC | Tampering | ссылки на образы в values.yaml | high | mitigate | Новых образов нет: init берёт уже закреплённый по digest minio.image; непустое значение mcImage по умолчанию в любом values*.yaml обязано быть `имя@sha256:` + 64 hex без тега (TestMinioImagePinnedByDigest + таблица TestMcImageRefRejectsUnpinnedForms) |
| T-261001-ncn-01 | Elevation of Privilege | init migrate-data-ownership (uid 0) | high | mitigate | drop ALL + add только CHOWN, allowPrivilegeEscalation false, readOnlyRootFilesystem true, лимиты 100m/64Mi, exec-форма без шелла и без значений chart'а в argv; форма держится разделом ci-helm-checks.sh с пробой инверсии |
| T-261001-ncn-02 | Tampering | симлинк в томе, указывающий за его пределы | high | mitigate | `chown -R -h`: симлинки не разыменовываются и не обходятся (-R по умолчанию -P); Task 1 4i доказывает, что симлинк на /etc/passwd меняет только сам себя, а init проходит при read-only rootfs |
| T-261001-ncn-03 | Tampering | файлы третьего uid в томе | medium | mitigate | `--from=0` меняет только элементы с владельцем root; чужие элементы и уже переведённые не трогаются (Task 1 4f/4h: снимки uid:gid/ctime/inode) |
| T-261001-ncn-04 | Denial of Service | init на поддереве, которое root без DAC-capabilities прочитать не может | low | accept | Init громко падает ненулевым кодом (Task 1 4j), под остаётся в Init с понятным сообщением chown; у такого расклада нет источника ни в прежнем, ни в новом образе; DAC_READ_SEARCH не добавляется из-за PSS baseline |
| T-261001-ncn-05 | Denial of Service | оператор переопределил minio.image на образ без GNU chown `--from` | medium | accept | Init падает громко; записано в SUMMARY «Not proven» и в тексте для PR; образ по умолчанию замерен (GNU coreutils 9.11) |
| T-261001-ncn-06 | Denial of Service | mc-контейнеры при заданном mcImage | high | mitigate | Все mc-контейнеры (Job и wait-for-bucket в каждом app-Deployment) берут override с откатом на minio.image; рендер-гейт проверяет оба случая, отсутствие пустых image: и неслепоту классификатора |
| T-261001-ncn-07 | Information Disclosure | логи init-контейнера | low | mitigate | chown запускается без -v/-c, имена объектов (имена пользовательских файлов) в лог пода не попадают |
</threat_model>

<verification>
- Task 1: отрендеренный init под docker, флаги — из рендера: контроль падает → миграция → MinIO ready, старый объект цел, новая запись проходит; повтор и второй запуск на свежем томе ничего не меняют (ctime-снимки равны); симлинк не разыменован; нечитаемое поддерево даёт ненулевой код; раздел ci-helm-checks.sh держит форму init-контейнера и падает на инверсии.
- Task 2: гейт-тест RED→GREEN и падает на пробах; рендер mc-контейнеров: без override — REF, с override — override, под MinIO — REF; пустых image: нет; ≥11 wait-for-bucket в обоих случаях.
- Task 3: `ci-local.sh helm gate` — два PASS на HEAD; ровно три коммита после 90d465a, без атрибуции; следов пробы нет.
- Не доказано (записать в SUMMARY): chart на живом кластере (fsGroup, init и PSS-admission на настоящем PVC); образы-override без GNU chown; amd64 в работе.
</verification>

<success_criteria>
- Апгрейд chart'а поверх PVC от прежнего root-образа на hostPath/local-path больше не оставляет MinIO в падении, и это доказано запуском именно отрендеренного контейнера на томе, где без него MinIO падает.
- Init ничего не меняет на томе, который уже переведён, и не трогает элементы, не принадлежащие root.
- Оператор с заданным minio.mcImage снова получает его во всех mc-контейнерах; без него всё работает на minio.image; оба случая держат рендер-гейт и Go-тест.
- `scripts/ci-local.sh helm gate` зелёный на HEAD.
</success_criteria>

<output>
Создать `.planning/quick/261001-ncn-chart-migrate-root-owned-minio-volume-an/261001-ncn-SUMMARY.md` по завершении.
</output>
