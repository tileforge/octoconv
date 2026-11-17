# Worker handover

[Back to the documentation index](README.md).

This page points an author at Worker Protocol version 1 and tells an operator where the image worker is configured. The published contract is the specification, the schema, and the examples. Do not change the core to add a worker.

## For an author

Every message uses protocol_version 1. The schema is [docs/worker-protocol/v1.schema.json](worker-protocol/v1.schema.json). Message examples are under [docs/worker-protocol/examples/](worker-protocol/examples/). Follow the scenarios in [docs/worker-protocol/conformance.md](worker-protocol/conformance.md). Do not invent a private scenario list.

This repository does not publish a Python worker. png to pdf stays 422.

## Class repository

Image stays in this repository. Any other engine class uses `tileforge/octoconv-worker-<engine>`, where `<engine>` is the legacy engine id. Cad uses `tileforge/octoconv-worker-cad`.

That process reads `<ENGINE>_PROTOCOL_CORE_URL`, `<ENGINE>_PROTOCOL_CLIENT_CERT`, `<ENGINE>_PROTOCOL_CLIENT_KEY`, `<ENGINE>_PROTOCOL_CLIENT_CA`, `<ENGINE>_PROTOCOL_WORKER_ID`, and `<ENGINE>_PROTOCOL_MANIFEST_PATH`. Cad uses the prefix `CAD_PROTOCOL_`.

The process has no `DATABASE_URL` and no `REDIS_ADDR`. One manifest holds one capability. Cad publishes `manifest-cad-convert.json` (`octoconv.convert.cad`) and `manifest-cad-render.json` (`octoconv.render.cad`) for STEP, IGES, and STL, and `manifest-caddraw-convert.json` and `manifest-caddraw-render.json` for DXF and DWG. The model image sets `CAD_STACK` to `model`. The drawing image sets `CAD_STACK` to `drawing`. `legacy.engine` stays `cad`. The document fixture is `manifest-document-convert.json` (`octoconv.convert.document`). It names the twelve LibreOffice pairs. An approved document job with no options and no progress webhook goes to the external worker when its attempt budget fits the file-link window. Any other document job is refused before upload. The html fixture is `manifest-html-convert.json` (`octoconv.convert.html`). It names html to pdf. An approved html job with no print options and no progress webhook goes to the external worker when its attempt budget fits the file-link window. An html job with a print option, a progress webhook, a longer budget, or no approved manifest stays on asynq. An html job already queued there finishes on the built-in process. The audio fixture is `manifest-audio-convert.json` (`octoconv.convert.audio`). It names the thirty-six audio pairs. An approved audio job with no language or translate and no progress webhook goes to the external worker when its attempt budget fits the file-link window. An audio job with `language` or `translate`, a progress webhook, a longer budget, or no approved manifest stays on asynq. An audio job already queued there finishes on `cmd/audio-worker`. The av fixture is `manifest-av-convert.json` (`octoconv.convert.av`). It names the thirty-five av pairs. An approved av job with no timecode, resolution_height, or codec and no progress webhook goes to the external worker when its attempt budget fits the file-link window. An av job with `timecode` (including explicit 0), `resolution_height`, or `codec`, a progress webhook, a longer budget, or no approved manifest stays on asynq. An av job already queued there finishes on `cmd/av-worker`. The ocr fixture is `manifest-ocr-convert.json` (`octoconv.convert.ocr`). It names the fifteen ocr pairs. An approved ocr job with no language and no progress webhook goes to the external worker when its attempt budget fits the file-link window. An ocr job with `language` (`eng`, `rus`, or a `+` combination), a progress webhook, a longer budget, or no approved manifest stays on asynq. An ocr job already queued there finishes on `cmd/ocr-worker`.

## For an operator

A client certificate is required. There is no server flag to allow plaintext. Approve the manifest before HELLO.

The image protocol worker reads `IMAGE_PROTOCOL_CORE_URL`, `IMAGE_PROTOCOL_CLIENT_CERT`, `IMAGE_PROTOCOL_CLIENT_KEY`, `IMAGE_PROTOCOL_CLIENT_CA`, `IMAGE_PROTOCOL_WORKER_ID`, and `IMAGE_PROTOCOL_MANIFEST_PATH`.

The lease stays 30 seconds. expires_at is the minimum of issued_at plus file_link_max_ttl, the attempt deadline, and the job deadline. It is not capped by lease_expires_at.

The image scaler query is `sum(octoconv_queue_depth{queue="image", state=~"pending|active|retry"})`. imagepdf does not get a ScaledObject.

Rollout and rollback for the image worker are in [Operations](OPERATIONS.md).
