# Phase 71: Evaluation Gate - Pattern Map

**Mapped:** 2026-10-03
**Files analyzed:** 14 (все новые; существующие файлы не меняются, критерий 5)
**Analogs found:** 11 / 14

Все аналоги проверены как tracked (`git ls-files`): scripts/*.sh, Dockerfile.probe-measure, scripts/sanitize-corpus.py, PROVENANCE.md, scripts/cmd/sandbox-probe/main.go.

## File Classification

| Новый файл | Role | Data Flow | Closest Analog | Match |
|---|---|---|---|---|
| `scripts/cad-corpus-gen.py` | генератор корпуса | batch / transform | `scripts/sanitize-corpus.py` | exact |
| `internal/convert/testdata/cad/PROVENANCE.md` (путь на выбор планировщика) | doc / provenance | — | `internal/inspect/testdata/pdf/PROVENANCE.md`, `internal/convert/testdata/sanitize/PROVENANCE.md` | exact |
| `scripts/cad-corpus-fetch.sh` | утилита загрузки | file-I/O | `scripts/tectonic-bundle-fetch.sh` | role-match |
| `Dockerfile.cad-measure` | измерительный образ | — | `Dockerfile.probe-measure` | role-match (см. отличия) |
| `scripts/cad-rss-time-measure.sh` | замер | batch, контейнер на ячейку | `scripts/probe-timeout-measure.sh` (+ `audio-rtf-measure.sh` для `memory.peak`) | role-match |
| `scripts/cad-render-measure.sh` | замер/гейт | batch | `scripts/probe-timeout-measure.sh` (каркас), `sanitize-corpus.py` (самопроверка по содержимому) | partial |
| `scripts/cad-measure/{cad_mesh,cad_render,cad_dxf}.py` (прототипы) | helper | request-response (argv -> файл) | нет в Go-кодовой базе | нет |
| `scripts/cad-measure/pixelcheck.py`, `contact_sheet.py` | утилита | transform | `scripts/fixtures/render_evidence.py` (только каркас CLI; CONTEXT: не подходит по смыслу) | partial |
| `scripts/cad-measure/vtk_keeplist.py` | генератор | transform | нет | нет |
| `scripts/cad-measure/cad-requirements.txt` (хеши) | config | — | нет (pip-хеши новые) | нет |
| `scripts/cad-sandbox-measure.sh` (если нужна обёртка) | замер | request-response | `scripts/sandbox-paths-measure.sh` (вызывать, не копировать) | exact |
| `.planning/phases/71-evaluation-gate/71-DECISION.md` | запись решения | — | `.planning/milestones/v1.15-phases/70-document-on-amd64/*` (формат evidence/SUMMARY) | role-match |
| `.planning/phases/71-evaluation-gate/evidence/*.txt` | evidence | — | вывод `probe-timeout-measure.sh` (`tee "$OUT"`) | exact |
| `docs/` или `71-LICENSES.md` (инженерная запись EVAL-04) | doc | — | `docs/FINDINGS.md` как соседний формат | partial |

## Pattern Assignments

### `scripts/cad-corpus-gen.py` (generator, deterministic)

**Analog:** `scripts/sanitize-corpus.py` (832 строки). Копировать структуру: docstring "WHY / WHERE THE PROVENANCE LIVES / DETERMINISM / SELF-CHECK / Usage", `DEFAULT_OUT`, `build_*() -> bytes`, таблица `EXPECTED = {name: [markers]}`, `check(out_dir) -> int`, `main()` с `--out-dir` и `--check-only`.

**Каркас main/check** (lines 750-832):
```python
def check(out_dir: str) -> int:
    failures = []
    for name, markers in EXPECTED.items():
        path = os.path.join(out_dir, name)
        if not os.path.exists(path):
            failures.append(f"{name}: missing"); continue
        blob = open(path, "rb").read()
        for m in markers:
            if m not in blob:
                failures.append(f"{name}: marker {m!r} absent from raw bytes")
    ...
    if failures:
        print("SELF-CHECK FAILED:", file=sys.stderr)
        ...
        return 1
    print("self-check passed: every fixture carries the markers it exists to carry")
    return 0

def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--out-dir", default=DEFAULT_OUT)
    ap.add_argument("--check-only", action="store_true", ...)
    ...
    return check(args.out_dir)

if __name__ == "__main__":
    sys.exit(main())
```
**Детерминизм** (lines 46-55): никаких timestamps/random/host paths; фиксированные даты (`ZIP_DATE = (2020,1,1,0,0,0)`).
**Структурные проверки сверх grep** (lines 676-732, `check_jpeg_structure`): marker-grep недостаточен, файл должен быть проходим как свой формат. Для CAD: независимо парсить текст STEP (`INCH`/`CONVERSION_BASED_UNIT`), DXF `$DWGCODEPAGE` и реальные 8-битные байты cp1251 без `\X2\`, IGES D-секция по типам сущностей (RESEARCH Pitfall 5, V15: нельзя доверять `FileUnits`).
**Протокол:** генератор и его выход идут в ПЕРВЫЙ коммит фазы, до кода, который ими проверяется (docstring lines 1-20). Для STEP/IGES генератору нужен OCP, значит запускается в измерительном образе, а не на macOS: `--check-only` должен работать без OCP.

### `PROVENANCE.md` для корпуса

**Analog:** `internal/inspect/testdata/pdf/PROVENANCE.md` (lines 1-40): преамбула "inputs to tests, never output", секция на файл: `## \`name\` — N bytes, reference: **X**`, команда воспроизведения в code-fence, **Proves:**, **If replaced:**. Добавить на файл: источник (URL + путь), автор, лицензия (PD/CC0/CC-BY/MIT/BSD, D-04), sha256, атрибуция CC-BY. Производные (масштабированные): "производная от X, коэффициент N, генератор G" (RESEARCH, Корпус).
Расположение: файлы, читаемые Go-тестами, в `internal/<pkg>/testdata/` с PROVENANCE.md рядом; фикстуры shell-гейтов в `scripts/fixtures/` + README.md (`sanitize-corpus.py` lines 14-27, `scripts/fixtures/README.md`). Для CAD выбрать `internal/convert/testdata/cad/` (CONTEXT).

### `scripts/cad-corpus-fetch.sh` (file-I/O)

**Analog:** `scripts/tectonic-bundle-fetch.sh`. Копировать: `set -euo pipefail`, `REPO_ROOT=...; cd`, `digest_of() { shasum -a 256 "$1" 2>/dev/null | cut -d' ' -f1; }`, режим `--check`, при несовпадении `rm -f "$DEST"; exit 1` (lines 50-96):
```bash
GOT="$(digest_of "$DEST")"
if [ "$GOT" != "$EXPECTED" ]; then
  echo "error: downloaded ... does not match the pin" >&2
  echo "  have $GOT" >&2
  echo "  want $EXPECTED" >&2
  rm -f "$DEST"
  exit 1
fi
```
**Отличия:** источник `curl -fsSL --retry 3` по URL (не `gh release`); кэш вне репо (путь на выбор). Принцип "пин читается из одного места" (lines 38-46): хеши брать из PROVENANCE.md/манифеста, не дублировать во втором месте. Код-пример из RESEARCH (`[ -f "$dest" ] || curl ... && mv`) имеет приоритетную ошибку в связке `||`/`&&`; писать через явный `if`.

### `Dockerfile.cad-measure`

**Analog:** `Dockerfile.probe-measure` (19 строк): шапка-комментарий "зачем образ: измеряем то, что едет", `RUN apt-get update && apt-get install -y --no-install-recommends ... && rm -rf /var/lib/apt/lists/*`.
**Отличия:** база `debian:bookworm-slim` по digest (`debian@sha256:3783cc01769c7b2b1b83a5c5ad96c815348e28ed7da68e2e3687004faa906251`, arm64), не `golang:*`; apt: `python3 python3-venv libosmesa6 libgl1 libx11-6 librsvg2-bin fonts-dejavu-core`; venv + `pip install --require-hashes -r cad-requirements.txt`, `pip install --no-deps --require-hashes vtk==9.7.1`; `USER nobody` (как продакшен-воркеры: см. `Dockerfile.worker` в CLAUDE.md). Корпус монтируется, не COPY. Go-toolchain не нужен (`sandbox-probe` собирается на хосте скриптом).

### `scripts/cad-rss-time-measure.sh` (batch, контейнер на ячейку)

**Analog:** `scripts/probe-timeout-measure.sh`. Копировать:
- шапку WHY/WHAT/WHERE/FAIL-CLOSED/USAGE (lines 1-55);
- `set -euo pipefail`, usage с `exit 64`, `exit 69` при отсутствии docker, `REPO_ROOT` (lines 57-78);
- сборку образа и "WHERE" с реальными лимитами: `docker run --rm --cpus 2.0 --memory 1g -v ... ` (lines 106-125). Здесь `--memory=<N>` перебирается по лестнице 1/2/4g;
- построчный `tee "$RAW" | grep --line-buffered -E '^(ROW|SKIP|BUDGET|SUMMARY),'` (комментарий lines 108-113: молчащий гейт неотличим от зависшего);
- EXCEEDED-блок (lines 131-141): ячейки, не уложившиеся в бюджет, перечисляются отдельно, а не выбрасываются; в нашем случае OOM (rc=137 / `oom_kill>0`) это строка результата, обязательная для D-05;
- fail-closed: ненулевой выход без рекомендации, если часть корпуса не обработана (lines 143-151);
- распределение min/median/p95/max nearest-rank (lines 156-176), формулу печатать вместе с результатом (lines 178-190), вывод `{ ... } | tee "$OUT"` и "commit it alongside the value it justifies".
**memory.peak:** прецедент `scripts/audio-rtf-measure.sh:129-135` читает `docker exec cat /sys/fs/cgroup/memory.peak` с fallback "unavailable". Но он кумулятивен на контейнер, поэтому здесь один `docker run --rm` на (файл, операция, повтор) и обёртка из RESEARCH Pattern 1 (`ru_maxrss` + `memory.peak` + `memory.events`).
**Отличия:** в evidence не печатать имена файлов приватного каталога (D-01); при счёте файлов = 0 в приватном каталоге скрипт печатает "реальный поток не замерен" (D-02, по счёту, а не по отсутствию ошибок).

### `scripts/cad-render-measure.sh` + `pixelcheck.py` + `contact_sheet.py`

**Analog:** каркас как у `probe-timeout-measure.sh`. Пиксельные критерии судят PNG (маска `(img<250).any(axis=2)`, поле, тёмные рёбра, различие ракурсов): код из RESEARCH "Проверка маски рендера", пороги калибровать (A6). Принцип "судить по содержимому, не по коду возврата" взят из `sandbox-paths-measure.sh` (комментарий lines 24-33 про `/usr/bin/vips` и lines 40-46 про пустой skeleton gs). Контактный лист в Pillow внутри образа; `scripts/fixtures/render_evidence.py` использовать только как образец CLI-каркаса (CONTEXT: графики CSV, по смыслу не подходит). D-11 ступень 2 это `checkpoint:human-verify` в плане, не скрипт.

### Матрица Landlock (кандидатные наборы путей для cad_mesh / cad_render / cad_dxf / rsvg-convert)

**Analog:** `scripts/sandbox-paths-measure.sh` (151 строка), вызывать как есть. Использование:
```bash
scripts/sandbox-paths-measure.sh trace  <image> <fixture-dir> <cmd...>
scripts/sandbox-paths-measure.sh verify <image> <fixture-dir> <ro-paths> <cmd...>
```
Важные свойства, которые план не должен обходить:
- `trace` ставит strace в одноразовый слой, запускает через `setpriv --reuid "$PROBE_USER"` под пользователем образа (lines 63-77); значит `Dockerfile.cad-measure` обязан иметь `USER nobody` (иначе `image_user=root`);
- в трассу не попадает execve: интерпретатор и цели симлинков venv (`/usr/bin/python3.11`) добавляются руками (lines 8-17, 111-113 "NOT shown");
- `verify` строит `sandbox-probe` под архитектуру образа (`CGO_ENABLED=0 GOOS=linux GOARCH="$image_arch" go build ./scripts/cmd/sandbox-probe`, lines 130-133) и при расхождении архитектур печатает note (lines 120-128); эмулированный amd64 считается отсутствием замера;
- env `PROBE_RW`, `PROBE_SOCK`, `PROBE_CANARY` пробрасываются (lines 138-141); `PROBE_CANARY` по умолчанию `/etc/hostname` и должен быть читаем до `Apply`;
- критерий достаточности для рендера/DXF: содержимое (пиксельные критерии, sha256 против unconfined), не код возврата (RESEARCH Pattern 4). Скрипт сам судит по коду возврата, поэтому поверх него нужен контент-чек; прецедент "driving the production helper and judging by content": `scripts/document-sandbox-measure.sh` (упомянут в комментарии lines 37-40; в этой выборке не читался).
- Наборы записываются как "кандидат для фазы 73", не как грант.

### `71-DECISION.md` и `evidence/*.txt`

**Analog:** вывод `probe-timeout-measure.sh` (lines 178-205): блок "measured in / host arch / версии движков / формула с подставленными числами / оговорка 'recommendation, not a decision' / per-file rows", и evidence лежит в `.planning/phases/<NN>-<slug>/evidence/*.txt` и коммитится (CONTEXT, Established Patterns). Прецедент каталога evidence: `.planning/milestones/v1.15-phases/70-document-on-amd64/evidence`, и `.planning/milestones/v1.13-phases/60-sanitize-eighth-engine-class/evidence/probe-timeout-20260901-repo-fixtures.txt`. В каждом транскрипте писать `uname -m`, версии движков и digest образа (RESEARCH Pitfall 7). `.planning/` в `.gitignore`, файлы force-add.

## Shared Patterns

### Шапка измерительного скрипта
**Source:** `scripts/probe-timeout-measure.sh` lines 1-80. WHY (антипаттерн, который закрывает скрипт), WHAT, WHERE (образ и лимиты), FAIL-CLOSED, USAGE. Коды выхода: 64 (usage), 69 (нет зависимости), 1 (замер не удался). **Apply to:** все `scripts/cad-*.sh`.

### Лимиты контейнера как у развёртывания
**Source:** `scripts/probe-timeout-measure.sh:108` (`--cpus 2.0 --memory 1g`), `deploy/chart/octoconv/values.yaml` (cpu "2", memory "1Gi", один воркер 2Gi; комментарий "measured timeout valid only at these limits", lines ~141-144). **Apply to:** все замеры RSS/времени. Лимит перебирается 1/2/4g по D-06, но всегда выставляется на контейнере, не на хосте.

### Fail-closed на корпусе
**Source:** `probe-timeout-measure.sh` lines 143-151 и `sanitize-corpus.py` `check()`. Нулевой счёт обработанных файлов или пропавший маркер = ненулевой выход, рекомендация не печатается. **Apply to:** генератор, фетчер, замеры, пиксельный гейт.

### Хеш-пин, несовпадение = ошибка
**Source:** `scripts/tectonic-bundle-fetch.sh` lines 50-96. **Apply to:** фетчер корпуса, сборка образа (`--require-hashes`, база по digest).

### Распределения
**Source:** `probe-timeout-measure.sh` lines 156-176 (nearest-rank p95, одинаковое определение во всех timing-скриптах: audio/av/ocr/pandoc). **Apply to:** повторы замера времени (минимум 3, min/median/max).

### Провенанс
**Source:** `internal/inspect/testdata/pdf/PROVENANCE.md`; два соглашения о расположении (`sanitize-corpus.py` lines 14-27). **Apply to:** корпус, производные и синтетика.

### Сборка probe
**Source:** `sandbox-paths-measure.sh` lines 130-133. Образцы Go-версии: отметить в evidence (локально go1.27.1, go.mod требует 1.26.4; память "pinned toolchain").

## No Analog Found

| Файл | Role | Data Flow | Причина |
|---|---|---|---|
| `scripts/cad-measure/cad_mesh.py`, `cad_render.py`, `cad_dxf.py` | helper | argv -> файл | В репо нет Python-хелперов движков (прод-хелперы Go/shell); использовать RESEARCH Standard Stack/Pattern 1-3, V9/V10 как рабочие образцы. Пометить как одноразовые прототипы, не embed. |
| `scripts/cad-measure/vtk_keeplist.py` | generator | transform | Нет аналога; алгоритм в RESEARCH Pattern 3 (`/proc/self/maps` union). |
| `cad-requirements.txt` с хешами | config | — | Нет pip-lock в репо; генерировать `pip-compile --generate-hashes` внутри bookworm 3.11, один `checkpoint:human-verify` на lock (RESEARCH Package Legitimacy). |
| Измерение cold pull (`registry:2`) | — | — | Нет прецедента; RESEARCH "Образ и cold pull". |

## Замечания для планировщика

- Файлы, которые фаза НЕ трогает: `internal/`, `cmd/`, `deploy/`, `docker-compose.yml`, `AllEngines()`, `/v1/formats` (критерий 5). Исключение: `.gitignore`/`.dockerignore` проверять только по необходимости (корпус монтируется).
- Корпус и PROVENANCE первым коммитом, измерительные скрипты после (протокол sanitize).
- Commit подписи сейчас заблокирован; PATTERNS.md не коммитился.
- Глобальная инструкция владельца: комментарии в коде не добавлять по умолчанию; существующие аналоги (длинные WHY-шапки) это конвенция этого репо для скриптов измерения, но решение "как комментировать" вынести в PR-описание, оставив только внешние ограничения (квирки strace/cgroup/OrbStack).

## Metadata

**Analog search scope:** `scripts/`, `scripts/fixtures/`, `scripts/cmd/`, `Dockerfile*`, `docs/`, `internal/*/testdata/**/PROVENANCE.md`, `deploy/`
**Files read:** sandbox-paths-measure.sh, probe-timeout-measure.sh, tectonic-bundle-fetch.sh, Dockerfile.probe-measure, sanitize-corpus.py (head + check/main), PDF PROVENANCE.md, scripts/fixtures/README.md
**Pattern extraction date:** 2026-10-03
