---
created: 2026-09-29
area: deploy
severity: medium
origin: phase 70 D-05 -- the one follow-up for amd64 production across all eight confined classes
audit_acknowledged:
  milestone: v1.15
  at: 2026-09-30
---

# Первый деплой на amd64: проверить, что все восемь классов действительно стартуют под Landlock

## What

Все восемь классов, которые теперь идут под Landlock (`image`, `pandoc`, `ocr`, `av`,
`audio`, `html`, `probe`, `document`; `tectonic` — с PR #21, замерен раньше), замерены
и прогнаны через живой гейт (`scripts/sandbox-liveproof.sh`) только на **arm64**
(OrbStack, нативные образы). На amd64-узле не проверялся ни один из них.

## Почему это не гейт фазы 70 (D-05)

Гранты заданы на уровне директорий (`/usr/lib`, `/usr/share/fonts`, ...), поэтому
мультиархитектурные каталоги библиотек (`/usr/lib/aarch64-linux-gnu` против
`/usr/lib/x86_64-linux-gnu`) покрыты конструкцией, а не отдельным замером. Это
**рассуждение, а не замер**, и писать нужно именно так, пока узел не проверен.
Единственное, чего в этом рассуждении нет, — отдельных файлов вне этих директорий,
которые на amd64 называются иначе.

## Что сделать при первом amd64-деплое

1. В логе каждого воркера найти строку «sandbox proven» (`image` пишет только свою
   строку `landlock`) и убедиться, что ни один воркер не отказался стартовать.
2. Где воркер отказался — замерить на этом узле: `scripts/sandbox-paths-measure.sh`
   (для `document` — `scripts/document-sandbox-measure.sh`), внести разницу в
   `.planning/research/SANDBOX-PATHS.md` и в Go-набор одним коммитом, как делает
   `scripts/conf05-crosscheck.sh`.
3. Образ `document`, собранный под `linux/amd64` и запущенный эмулируемо (не на
   настоящем amd64-узле), по-прежнему отказывается стартовать и называет
   `DOCUMENT_SANDBOX_DISABLED` — это ожидаемо и не относится к этому todo.

## Связи

- Закрывает хвост `.planning/todos/completed/2026-09-06-other-engines-still-unconfined.md`
  («в проде блокировки, вероятно, нет вовсе» — следствие, не замер).
- Preflight каждого воркера (CONF-04) сам поймает недостающий грант на старте; todo нужен,
  чтобы это проверили один раз намеренно, а не узнали по отказавшему поду.
