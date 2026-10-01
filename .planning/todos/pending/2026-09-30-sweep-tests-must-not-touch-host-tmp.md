---
created: 2026-09-30
area: testing
severity: low
origin: phase 70, plan 70-07 (Deviation 3) -- recorded at 70-09 at the user's request
audit_acknowledged:
  milestone: v1.15
  at: 2026-09-30
---

# Тест, который строит `ConfinedDocument`/`ConfinedChromium` и доходит до `Convert`, чистит настоящий `/tmp` хоста

## What

`ConfinedDocument` и `ConfinedChromium` подключают sweep к настоящему `/tmp`
(`sharedTmp = sharedTmpDir`, `internal/convert/documentsandbox.go`, `chromium.go`). Если тест
построил конвертер ими и код дошёл до `Convert` (в 70-07 — `DocumentPreflight`), то
`sweepSharedTmp("/tmp", ...)` запускается на самом хосте разработчика.

Случилось в 70-07: RED-тест остановился на первой же записи (`/private/tmp/0618-6745-6b69-40e3`,
принадлежит root, sticky bit отказал) с `permission denied`. Удалено ничего не было, но только
потому, что эта запись сортируется первой и её не снять; на другом `/tmp` тот же прогон мог
бы опустошить каталог (в нём лежал scratchpad сессии). См. `70-07-SUMMARY.md`, Deviation 3.

## Что сделано и чего нет

В 70-07 сам тест исправлен: он строит `LibreOfficeConverter{}.WithSandbox(helper)` напрямую и
ставит `sharedTmp = t.TempDir()`, с комментарием почему. **Класс ошибок остался**: следующий
тест, который возьмёт `ConfinedDocument`/`ConfinedChromium`, повторит её молча.

## Что сделать

Один из вариантов (выбрать при взятии):

1. Тест в `internal/convert`, который находит каждый вызов `ConfinedDocument(`/`ConfinedChromium(`
   в `*_test.go` и требует рядом присвоения `sharedTmp` (по образцу существующих исходных
   проверок вроде `TestConfinedClassesExecOnlyThroughTheSandbox`);
2. Либо запретить sweep вне `t.TempDir()`: `sweepSharedTmp` отказывается работать, если
   запущен тестовым бинарником и путь равен настоящему `/tmp` (`testing.Testing()`).

## Связи

- Исчезает вместе с причиной, если будет взят
  `.planning/todos/pending/2026-09-30-soffice-socket-grant-remove-file-alternative.md`
  (без `rw:/tmp` у `document` нет и sweep'а), но `html` держит `rw:/tmp` и без него не
  обойтись — для chromium todo остаётся.
- `.planning/phases/70-document-on-amd64/70-07-SUMMARY.md` (Deviation 3, key-decisions).
