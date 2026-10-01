---
created: 2026-09-30
area: dev
severity: medium
origin: phase 70 deferred-items.md (found in 70-01) -- user decision 5 (2026-09-30)
completed: 2026-10-01
resolution: compose and chart run MinIO and mc from cgr.dev/chainguard/minio pinned by digest; local aliases removed; quick task 261001-5ec
audit_acknowledged:
  milestone: v1.15
  at: 2026-09-30
---

# Docker Hub больше не отдаёт `minio/minio` и `minio/mc`: перевести compose и chart на образ chainguard

## What

`minio/minio:latest` и `minio/mc:latest` больше не отдаются с Docker Hub
(`pull access denied`), а локальные копии на чистой машине не появятся. На них ссылаются
`docker-compose.yml` (сервисы `minio` и `createbucket`) и chart в `deploy/`. `docker compose up`
и `scripts/ci-local.sh e2e` на хосте без этих образов падают на pull.

Этот хост работает на локальных алиасах: `docker tag cgr.dev/chainguard/minio:latest
minio/minio:latest` и `... minio/mc:latest` (образ chainguard содержит minio, mc и sh).
Это не в репозитории; в фазе 70 ни один отслеживаемый файл не менялся.

## Что сделать

Быстрая задача (`/gsd-quick`) **после милстоуна**: перевести compose и chart на
`cgr.dev/chainguard/minio` (другой проект пользователя уже на нём). Проверить: `minio` и
`createbucket` (у chainguard нет отдельного `mc`-образа — команда и entrypoint createbucket
подгоняются под один образ), healthcheck MinIO, `scripts/ci-local.sh gate helm e2e`, убрать
локальные алиасы.

## Почему todo, а не только строка в deferred-items.md

`deferred-items.md` лежит в каталоге фазы, которая архивируется при закрытии милстоуна.
Закрытие милстоуна поднимает быстрые задачи, seeds и todo (STATE.md «Deferred Items»), а не
`deferred-items.md` одной фазы — значит, без todo пункт исчез бы из виду вместе с архивом.

## Связи

- `.planning/phases/70-document-on-amd64/deferred-items.md` (первый пункт) — там описание находки;
  этот todo — её трекер.
- Решение пользователя 5 (2026-09-30): вне фазы 70, `docker-compose.yml` и chart не менять.

## Закрыто (2026-10-01)

Быстрая задача 261001-5ec, ветка quick-minio-chainguard.

- Ссылка: `cgr.dev/chainguard/minio@sha256:fa23f6a6f62645654530ff94aa077d1cc0d0e44c8f1cce02ab039873612edc72`
  (digest мультиархитектурного индекса, форма без `:latest` — D-02). В compose (minio и
  createbucket) и в chart (`minio.image`, единственный источник для Deployment minio, Job
  createbucket и init-контейнера wait-for-bucket всех 11 app-Deployment'ов; отдельного mc-ключа больше нет).
- Коммиты: fd9784e (compose), c5618ec (chart + `internal/e2e/minio_image_pin_test.go`), d93424a (README, CLAUDE.md).
- Гейт: `.planning/ci-local/ci-local-20261001T012020Z.log` (commit d93424a) — helm PASS, gate PASS, e2e PASS.
- Локальные алиасы `minio/minio:latest` и `minio/mc:latest` удалены ДО живого прогона (без `-f`,
  два Untagged, ноль Deleted); все прогоны шли без них.
- Замерено: compose живьём (minio healthy, uid 65532, createbucket вышел 0 и создал бакет в свежем томе);
  команды wait-for-bucket при лимитах 64Mi / 0.1 CPU против живого MinIO; реестр отдаёт оба
  платформенных манифеста, config и все слои пина (200/307 через анонимный токен);
  гейт-тест падает на испорченном digest и на тег-только форме.
- НЕ замерено: chart на живом кластере (OrbStack Kubernetes выключен, только `helm template`/lint,
  включая fsGroup 65532); скачивание пина в чистое хранилище Docker (доказано через API реестра, а
  не `docker pull`: слои уже лежали локально); amd64 вживую (индекс содержит amd64, прогон — arm64).
- Наблюдение: `:latest` уже ушёл с этого digest (индекс sha256:4692462f…). Пин оставлен на fa23 —
  на нём прошёл e2e 2026-09-30 и работает dockerdev-minio-1. Процедура обновления — комментарий над
  сервисом `minio` в `docker-compose.yml`.
- Проба тома, записанного root: non-root MinIO на нём НЕ стартует (`unable to rename
  (/data/.minio.sys/tmp -> ...) file access denied`, затем `FATAL Unable to initialize backend:
  Unable to write to the backend`). Лечение `chown -R 65532:65532 /data` одноразовым root-контейнером
  проверено: после него ready и запись проходят. Команда записана в README.
  Доказательство: `.planning/quick/261001-5ec-move-minio-images-off-docker-hub-to-cgr-/evidence/rootvol-probe.txt`.
