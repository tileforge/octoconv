---
created: 2026-09-30
area: dev
severity: medium
origin: phase 70 deferred-items.md (found in 70-01) -- user decision 5 (2026-09-30)
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
