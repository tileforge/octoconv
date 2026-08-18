---
task: 4 (image.go instance)
title: image.go — обрыв обхода не выдаётся за пустой обход
status: complete
files_modified:
  - internal/inspect/image.go
  - internal/inspect/image_test.go
commit: 566da90
---

# Задача 4 (продолжение) — image.go

`260811-r5p-SUMMARY-task4-sweep.md` (родительский проход) назвал дефект в
`scanJPEGMetadata`/`scanPNGMetadata`, но сам проход был выполнен по
`internal/inspect/container.go`, `media.go`, `pdf.go` и
`internal/api/inspect_handlers.go` — `image.go` в его файловый список не
входил. Эта задача закрывает дефект в `image.go` отдельным коммитом и
расширяет аудит на `scanRIFFChunkMetadata` (WebP) и TIFF-ветку, которые
имеют ТОЧНО ту же форму, но не были названы ни одним кругом ревью и ни
родительским проходом.

## Дефект

`scanJPEGMetadata` и `scanPNGMetadata` при структурной проблеме возвращали
ровно то же, что при полностью пройденном обходе без находок:

- **PNG**: `len(b) < 8`; объявленная длина чанка, при которой
  `dataStart+length+4 > len(b)`; выход из цикла с остатком байтов, не
  образующим заголовок чанка.
- **JPEG**: отсутствие валидного SOI; `i+4 > len(b)`;
  `segLen < 2 || i+2+segLen > len(b)`; выход из цикла без достижения EOI/SOS.

`scanImageMetadata` сворачивала это в единственный булев `truncated`, а
`AnalyzeImage` при `found==false && truncated==false` докладывала
`metadata_present: clean`. PNG, второй чанк которого объявляет длину за
пределы файла, с настоящим EXIF после него, объявлялся чистым — сканер не
смотрел и заявил, что смотрел.

**Аудит этим проходом нашёл ту же форму ещё в двух местах, которых не
назвал ни родительский проход, ни ревьюер:**

- `scanRIFFChunkMetadata` (WebP) — объявленный `size` за пределами буфера
  и остаток байт, не образующий заголовок чанка, возвращали то же самое
  `"", false, false`, что и честный полный обход.
- TIFF-ветка (инлайн в `scanImageMetadata`) — `len(buf) < 8` ИЛИ байты не
  начинаются с `II`/`MM` возвращали `"", false, windowTruncated`
  одинаково, было ли окно короче 8 байт (наш предел) или окно покрывало
  ВЕСЬ файл, а сигнатура всё равно не сошлась (факт о файле). Второй
  случай ложно схлопывался с `windowTruncated=false` → `clean`.

## Правило исправления

То же правило, что задача 2 применила к XML в `container.go`: докладывать
`clean` имеет право только выход, дошедший до собственного естественного
конца формата.

| Формат | Естественный конец |
|---|---|
| JPEG | EOI (`0xD9`) или SOS (`0xDA`) |
| PNG | обход чанков потребляет буфер РОВНО (`pos == len(b)`) |
| WebP (RIFF) | обход чанков потребляет буфер РОВНО, то же правило, что PNG |
| TIFF | валидный заголовок байт-порядка при ПОЛНОМ (не усечённом) окне |
| HEIC | не применимо — см. «Раскрытое ограничение» ниже |

Любой другой выход — не `clean`, с сохранением различия причин:
`work_ceiling_bytes` — НАШ предел (байтовое окно ИЛИ собственный
итерационный потолок сканера), `malformed_input` — факт о файле. При
пересечении обеих причин (окно усечено И структура сломана) побеждает
`work_ceiling_bytes`: отличить настоящую поломку от собственного обрезания
нельзя, честнее признать свой предел.

## Форма исправления

Три сканера (`scanJPEGMetadata`, `scanPNGMetadata`, `scanRIFFChunkMetadata`)
теперь возвращают общий тип `imageMetadataStatus{location, ceiling,
malformed}` — зеркало `container.go`'s `externalRefStatus`, то есть та же
дисциплина, которую задача 2 уже применила к внешним ссылкам ZIP-контейнеров,
перенесённая на другой формат этим кругом. `scanImageMetadata` и новая
функция `composeMetadataStatus` применяют правило приоритета (потолок
побеждает малформед) в ОДНОМ месте для всех трёх сканеров. TIFF-ветка
вынесена в отдельную функцию `scanTIFFMetadata` с тем же трёхзначным
возвратом (`ceilingHit`/`malformedHit` как булевы, а не структура — форма,
которую сам JPEG/PNG/WebP-кластер не разделяет, потому что TIFF не ходит
по чанкам вообще).

`AnalyzeImage` получил третью ветку `case malformedHit` в дополнение к уже
существующей `case ceilingHit` — сигналы больше не совмещены.

## Раскрытое ограничение (не дефект, но не молчание)

`scanISOBMFFMetadataTokens` (HEIC) — рассмотрен явно и оставлен без
изменений. Обоснование записано прямо в doc-комментарии функции: эта
функция не совершает структурного обхода, который можно оборвать — это
безусловный `bytes.Contains` по всему окну, каждый раз. У неё нет
объявленной длины, которую можно превысить, нет заголовка с magic-байтами,
который может не пройти проверку, нет границы чанка, которая оставляет
неучтённый хвост. Единственный источник неполноты — само байтовое окно
(уже отражается как `ceilingHit` через `windowTruncated`, как и у любого
другого формата) плюс собственный риск ложноотрицательного результата на
не-непрерывной сериализации — свойство ВЫБРАННОГО ПРИБЛИЖЕНИЯ, а не
какого-либо выхода этой функции. Присвоение этому риску отдельного
`unknown`/`malformed_input` означало бы, что КАЖДАЯ HEIC-загрузка без
метаданных отчитывается как unknown вместо clean — изменение поведения
куда шире, чем замена неверно классифицированного обрыва честным, и это
уже было зафиксировано родительским проходом
(`260811-r5p-SUMMARY-task4-sweep.md`, раздел «Раскрытые ограничения»,
пункт 1) как сознательно не тронутое.

## Тесты

Добавлены в `internal/inspect/image_test.go` (список, помощник
`assertCheckReason` для проверки `Check.Reason`):

- `TestAnalyzeImageJPEGReachingEOIWithNoMetadataIsClean` — позитив: JPEG,
  дошедший до EOI без метаданных, — честный `clean`.
- `TestAnalyzeImageJPEGMissingSOIIsMalformedNotClean` — worked example
  дефекта: буфер без валидного SOI → `unknown`/`malformed_input`.
- `TestAnalyzeImageJPEGSegmentLengthPastBufferIsMalformedNotClean` —
  именной тест выхода `segLen < 2 || i+2+segLen > len(b)`.
- `TestAnalyzeImageJPEGTruncatedMarkerHeaderIsMalformedNotClean` —
  именной тест выхода `i+4 > len(b)`.
- `TestAnalyzeImagePNGWithIENDIsCleanNaturalEnd` — позитив: PNG с IEND —
  честный `clean`.
- `TestAnalyzeImagePNGDeclaredLengthPastBufferIsMalformedNotClean` —
  дословный worked example из дефекта.
- `TestAnalyzeImagePNGLeftoverBytesAreMalformedNotClean` — именной тест
  правила «обход потребляет буфер ровно».
- `TestAnalyzeImageWebPBufferConsumedExactlyIsCleanNaturalEnd` — позитив
  для WebP.
- `TestAnalyzeImageWebPDeclaredSizePastBufferIsMalformedNotClean` —
  именной тест для WebP, дефект, которого не было ни в одном ревью-круге.
- `TestAnalyzeImageWebPLeftoverBytesAreMalformedNotClean` — зеркало PNG.
- `TestAnalyzeImageTIFFValidHeaderIsMetadataPresent` — регресс-проверка,
  что находка не сломана.
- `TestAnalyzeImageTIFFInvalidHeaderWithFullWindowIsMalformedNotClean` —
  именной тест для TIFF: полное окно + неверная сигнатура → `malformed`.
- `TestAnalyzeImageTIFFShortWindowIsCeilingNotMalformed` — контраст: то же
  «сигнатура не проверена», но из-за короткого окна → `work_ceiling_bytes`.
- `TestAnalyzeImageHEICMissWithFullWindowStaysClean` — документирует
  сознательно не тронутое поведение HEIC.

Все существующие тесты `image_test.go` зелёные БЕЗ правок — только
добавлен `assertCheckReason` и новые функции ниже существующего кода.
Ни одна старая тестовая функция не изменена.

## Инверсии (дословный вывод)

Протокол: `cp` фиксированной версии в `/private/tmp/...`, точечная правка
одного выхода обратно на старое поведение, прогон ИМЕННО названного
теста, фиксация провала, восстановление через `cp` (никогда
`git checkout`).

### Инверсия 1 — JPEG, `segLen`-переполнение

Правка: `segLen < 2 || i+2+segLen > len(b)` → `return
imageMetadataStatus{}` вместо `{malformed: true}`.

```
=== RUN   TestAnalyzeImageJPEGSegmentLengthPastBufferIsMalformedNotClean
    image_test.go:401: check metadata_present status = clean, want unknown
--- FAIL: TestAnalyzeImageJPEGSegmentLengthPastBufferIsMalformedNotClean (0.00s)
FAIL
FAIL	github.com/apaderin/octoconv/internal/inspect	0.645s
FAIL
```

### Инверсия 2 — PNG, остаток байт после обхода

Правка: безусловный `return imageMetadataStatus{}` вместо ветвления
`pos == len(b)` / остаток.

```
=== RUN   TestAnalyzeImagePNGLeftoverBytesAreMalformedNotClean
    image_test.go:456: check metadata_present status = clean, want unknown
--- FAIL: TestAnalyzeImagePNGLeftoverBytesAreMalformedNotClean (0.00s)
FAIL
FAIL	github.com/apaderin/octoconv/internal/inspect	0.650s
FAIL
```

### Инверсия 3 — WebP, `size`-переполнение

Правка: `size < 0 || uint64(dataStart)+uint64(size) > uint64(len(b))` →
`return imageMetadataStatus{}` вместо `{malformed: true}`.

```
=== RUN   TestAnalyzeImageWebPDeclaredSizePastBufferIsMalformedNotClean
    image_test.go:488: check metadata_present status = clean, want unknown
--- FAIL: TestAnalyzeImageWebPDeclaredSizePastBufferIsMalformedNotClean (0.00s)
FAIL
FAIL	github.com/apaderin/octoconv/internal/inspect	0.639s
FAIL
```

### Инверсия 4 — TIFF, полное окно с неверной сигнатурой

Правка: `return "", false, false, true` (malformed) →
`return "", false, windowTruncated, false` (старое поведение — совмещает с
ceiling).

```
=== RUN   TestAnalyzeImageTIFFInvalidHeaderWithFullWindowIsMalformedNotClean
    image_test.go:526: check metadata_present status = clean, want unknown
--- FAIL: TestAnalyzeImageTIFFInvalidHeaderWithFullWindowIsMalformedNotClean (0.00s)
FAIL
FAIL	github.com/apaderin/octoconv/internal/inspect	0.904s
FAIL
```

После каждой инверсии файл восстановлен `cp` из зафиксированной копии
(md5 `ce08ad64f0eadc1aadc617067ae52685`, сверен после каждого восстановления),
`git checkout` не использован ни разу.

## Верификация

```
$ gofmt -l internal/inspect
(пусто)
$ go build ./...
(чисто)
$ go vet ./...
(чисто)
$ go test ./... -count=1 -race
ok  	github.com/apaderin/octoconv/... (все пакеты)
```

## Self-Check

- `internal/inspect/image.go` — FOUND
- `internal/inspect/image_test.go` — FOUND
- Коммит `566da90` — FOUND в `git log --oneline --all`
