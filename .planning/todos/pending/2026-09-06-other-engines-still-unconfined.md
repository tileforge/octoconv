---
created: 2026-09-06
area: convert
severity: low
origin: PR #21, while wiring Landlock into the tex engine
---

# The sandbox exists now, and only one of nine engines uses it

## What

`internal/sandbox` confines the tectonic engine's filesystem access. The other
eight engine classes — image (libvips), document (LibreOffice), html
(chromium), audio/av (ffmpeg, whisper), ocr (tesseract, ghostscript), pandoc,
probe — still run with read access to everything in their container.

## Why tectonic was first, and why this is not urgent for the rest

The tex engine is the only one whose input is a **program**. A `.tex` has
macros, control flow and `\openin`, so "read an arbitrary file and typeset it
into the output" is a feature of the input format, reachable in four lines and
measured (PR #19). Nothing similar is true of a PNG or an MP4: the others
process untrusted *data*, and reaching the filesystem from there requires an
exploitable bug in the engine rather than a documented directive.

So the severity is genuinely lower — but it is not zero, and the argument above
is about likelihood, not consequence. LibreOffice and chromium are both large
enough to have had exactly that class of bug, and both are given files a client
chose.

## Где это можно делать, а где нельзя — замерено 2026-09-19

Раньше здесь подразумевалось (и повторялось в пересказах), что arm64-хост
мешает конфайнить движки. **Это неверно.** Замер пробником, который вызывает
тот же `sandbox.Available()` и настоящий `sandbox.Apply()`, а затем пытается
прочитать `/etc/hostname`:

| Где | `Available()` | `Apply()` | чтение вне грантов |
|---|---|---|---|
| `debian:bookworm-slim`, нативный arm64 | `true` | ruleset установлен | **DENIED** |
| `debian:bookworm-slim`, `--platform linux/amd64` | `false` | `unavailable on this kernel` | — |
| `octoconv-document-worker:latest` (реальный образ, amd64) | `false` | `unavailable on this kernel` | — |
| `octoconv-tectonic-worker:latest` (реальный образ, arm64) | `true` | ruleset установлен | **DENIED** |

Ядро одно и то же — одна Docker-VM. Разница только в эмуляции:
`landlock_create_ruleset` из amd64-процесса не проходит.

Причина, следовательно, **не в архитектуре хоста**, а в том, что
`document-worker` — единственный из двенадцати сервисов, закреплённый на
`platform: linux/amd64` (`docker-compose.yml`), потому что `verapdf/cli:v1.30.2`
публикуется amd64-only. Остальные одиннадцать, включая chromium, ocr, pandoc,
av, audio, image и probe, идут нативно.

Что из этого следует для планирования фазы:

- **Семь из восьми неконфайненных классов разрабатываются и проверяются на этом
  хосте**, включая chromium — тот самый, который этот документ называет
  настоящей работой.
- Заблокирован ровно один, `document`, и снять блокировку можно, не трогая
  Landlock: либо amd64-машина/узел для живого гейта, либо arm64-сборка veraPDF,
  либо вынос veraPDF из этого образа.
- **В проде блокировки, вероятно, нет вовсе** — на amd64-узлах сервис идёт
  нативно. Это следствие, а НЕ замер: узлы не проверялись, и до проверки так и
  писать.

## Замеры путей

`.planning/research/SANDBOX-PATHS.md` — что каждому движку нужно читать, по
замеру, а не по рассуждению. Инструмент: `scripts/sandbox-paths-measure.sh`.
На 2026-09-20 замерены и доказаны `image`, `html`, `pandoc` и `ocr`; остальные три нативных ждут того же
прогона.

## What would be involved

Much less than the first one. `sandbox.Paths`, `sandbox.Apply` and the argv
wire format are engine-agnostic; the tex-specific parts are the helper mode in
`cmd/tectonic-worker` and `tectonicSandboxSpec`. Each other worker would need
its own equivalent of those two, and the per-engine question "what does this
binary actually need to read" answered — which for chromium and LibreOffice is
the real work, and is best answered by running them under a ruleset and reading
the denials rather than by reasoning.

The startup fail-closed and the retryable classification should be lifted with
it; both are engine-independent decisions that were argued once already in
`internal/worker/descriptors.go` and `cmd/tectonic-worker`.
