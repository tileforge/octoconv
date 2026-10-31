# Early milestones

[Back to OctoConv](../README.md) · [Documentation](README.md)

This is a historical record through v1.14, not the current feature list. See the
[repository releases](https://github.com/tileforge/octoconv/releases) and
[commit history](https://github.com/tileforge/octoconv/commits/main) for later work.

| Milestone | What landed |
|---|---|
| v1.0 Hardening MVP | Mandatory auth + rate limiting, signed webhooks with retry/dead-letter, retry classification, stranded-job reconciler, magic-byte validation, storage TTL, Prometheus + health |
| v1.1 Tech Debt Cleanup | Opt-in RFC1918 webhooks, reconciler coverage for dropped deliveries, wall-clock soak test, image decompression-bomb guard |
| v1.2 Document Engine | Office → PDF via LibreOffice, structural zip-bomb/macro validation, dedicated `document-worker` |
| v1.3 Document Class v2 | Cross-format document pairs, OLE-CFB rejection, HTML/Chromium class, dedicated `webhook-worker` |
| v1.4 CI, Presets & Debt | First CI workflow, named presets, registry-derived `GET /v1/formats` |
| v1.5 MCP & Fidelity | stdio MCP server, veraPDF ISO-19005 validation, encrypted-vs-legacy OLE-CFB classification |
| v1.6 Kubernetes & KEDA | Helm chart, KEDA queue-depth autoscaling, MCP over HTTP, operator-gated system presets |
| v1.7 Audio Engine | Transcription via whisper.cpp |
| v1.8 AV Engine | Transcode, audio extraction, thumbnails via ffmpeg |
| v1.9–v1.12 | OCR and pandoc engine classes, SSE progress streaming, per-client quotas and fairness, job listing, retention |
| v1.13 Inspect & Operations | Synchronous `POST /v1/inspect`, structured error codes, JobRunner consolidation, born-digital PDF routing, `sanitize` operation, `/v1/system/usage` |
| v1.14 Typesetting & Debt | LaTeX → PDF via tectonic, multi-file projects, Landlock confinement of the engine's file reads |
