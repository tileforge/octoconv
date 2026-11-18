# Worker handover

[Back to the documentation index](README.md).

This page points an author at Worker Protocol version 2 and tells an operator where the image worker is configured. The published contract is the specification, the schema, and the examples. Do not change the core to add a worker.

## For an author

Every message uses protocol_version 2. The schema is [docs/worker-protocol/v1.schema.json](worker-protocol/v1.schema.json). Message examples are under [docs/worker-protocol/examples/](worker-protocol/examples/). Follow the scenarios in [docs/worker-protocol/conformance.md](worker-protocol/conformance.md). Do not invent a private scenario list.

This repository does not publish a Python worker. png to pdf stays 422.

## Class repository

Image stays in this repository. Any other engine class uses `tileforge/octoconv-worker-<engine>`, where `<engine>` is the legacy engine id. Cad uses `tileforge/octoconv-worker-cad`.

That process reads `<ENGINE>_PROTOCOL_CORE_URL`, `<ENGINE>_PROTOCOL_CLIENT_CERT`, `<ENGINE>_PROTOCOL_CLIENT_KEY`, `<ENGINE>_PROTOCOL_CLIENT_CA`, `<ENGINE>_PROTOCOL_WORKER_ID`, and `<ENGINE>_PROTOCOL_MANIFEST_PATH`. Cad uses the prefix `CAD_PROTOCOL_`.

The process has no `DATABASE_URL` and no `REDIS_ADDR`. One manifest holds one capability. Cad publishes `manifest-cad-convert.json` (`octoconv.convert.cad`) and `manifest-cad-render.json` (`octoconv.render.cad`) for STEP, IGES, and STL, and `manifest-caddraw-convert.json` and `manifest-caddraw-render.json` for DXF and DWG. The model image sets `CAD_STACK` to `model`. The drawing image sets `CAD_STACK` to `drawing`. `legacy.engine` stays `cad`. The document fixture is `manifest-document-convert.json` (`octoconv.convert.document`). It names the twelve LibreOffice pairs. An approved document job with no options and no progress webhook goes to the external worker when its attempt budget fits the file-link window. Any other document job is refused before upload. The html fixture is `manifest-html-convert.json` (`octoconv.convert.html`). It names html to pdf. Its `parameter_schema` closes `page_size`, `margin_mm`, `landscape`, and `print_background`. An approved html job goes to the external worker when its attempt budget fits the file-link window, and a non-empty normalized option object is the assignment `parameters`. An html job with a progress webhook, a longer budget, or no approved manifest stays on asynq. An html job already queued there finishes on the built-in process. The audio fixture is `manifest-audio-convert.json` (`octoconv.convert.audio`). It names the thirty-six audio pairs. Its `parameter_schema` closes `language` and `translate`. An approved audio job, including language, translate, and a progress webhook, goes to the external worker when its attempt budget fits the file-link window, and a non-empty normalized option object is the assignment `parameters`. A longer budget, or no approved manifest, is refused before upload. A stranded audio row stays queued. The in-repo process does not re-enqueue it and does not fail it. This change does not approve the manifest. The av fixture is `manifest-av-convert.json` (`octoconv.convert.av`). It names the thirty-five av pairs. Its `parameter_schema` closes `timecode` (including explicit 0), `resolution_height`, and `codec`. An approved av job, including timecode, resolution_height, codec, and a progress webhook, goes to the external worker when its attempt budget fits the file-link window, and a non-empty normalized option object is the assignment `parameters`. A nonempty codec on webm is refused before upload. A longer budget, or no approved manifest, is refused before upload. A stranded av row stays queued. The in-repo process does not re-enqueue it and does not fail it. This change does not approve the manifest. The ocr fixture is `manifest-ocr-convert.json` (`octoconv.convert.ocr`). It names the fifteen ocr pairs. Its `parameter_schema` closes `language` (`eng`, `rus`, or a `+` combination). An approved ocr job goes to the external worker when its attempt budget fits the file-link window, and a non-empty normalized option object is the assignment `parameters`. An ocr job with a progress webhook, a longer budget, or no approved manifest stays on asynq. An ocr job already queued there finishes on `cmd/ocr-worker`. The pandoc fixture is `manifest-pandoc-convert.json` (`octoconv.convert.pandoc`). It names the thirty-two pandoc pairs. Its `parameter_schema` closes `standalone`, `toc`, `number_sections`, `section_divs`, and `wrap`. An approved pandoc job goes to the external worker when its attempt budget fits the file-link window, and a non-empty normalized option object is the assignment `parameters`. A pandoc job with a progress webhook, a longer budget, or no approved manifest stays on asynq. A pandoc job already queued there finishes on `cmd/pandoc-worker`.

## For an operator

A client certificate is required. There is no server flag to allow plaintext. Approve the manifest before HELLO.

The image protocol worker reads `IMAGE_PROTOCOL_CORE_URL`, `IMAGE_PROTOCOL_CLIENT_CERT`, `IMAGE_PROTOCOL_CLIENT_KEY`, `IMAGE_PROTOCOL_CLIENT_CA`, `IMAGE_PROTOCOL_WORKER_ID`, and `IMAGE_PROTOCOL_MANIFEST_PATH`.

The lease stays 30 seconds. expires_at is the minimum of issued_at plus file_link_max_ttl, the attempt deadline, and the job deadline. It is not capped by lease_expires_at.

The image scaler query is `sum(octoconv_queue_depth{queue="image", state=~"pending|active|retry"})`. imagepdf does not get a ScaledObject.

Rollout and rollback for the image worker are in [Operations](OPERATIONS.md).
