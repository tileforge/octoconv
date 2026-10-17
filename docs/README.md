# OctoConv API docs

`openapi.yaml` is an OpenAPI 3.1 spec describing the OctoConv HTTP API
exactly as implemented in `internal/api/`.

`ERROR-CODES.md` is the frozen, versioned list of every machine-readable
error code the service can emit — both the synchronous 4xx/429 half and the
asynchronous job-failure half. It is the migration target for the frontend
and both MCP servers; publishing this list into `openapi.yaml` as an enum
(ERRC-V2-02) is a separate, deferred step.

`CAD.md` is the client entry point of the `cad` class: formats and aliases, operations and pairs, render
options and where they apply, the two outputs, size ceilings, time limits, error codes and known limits.
Tests in `internal/convert` and `internal/worker` hold every table in it equal to the code.

`PREVIEW.md` is the preview contract: what a preview is (operation, format, sizes, background, framing,
views, outputs) as fixed by the `cad` class, what happens on input that cannot be previewed, and the
questions CAD did not decide, marked "not fixed" for the next class. A test holds the fixed values equal to
the code.

`MEASUREMENTS.md` is the contract for the measurements JSON of a CAD job (schema, closed vocabularies,
validation rules). The `cad` engine's `render` operation emits it beside the PNG as the output with role
`measurements`, and a test holds the document equal to the validator.

## Import into Yaak

Yaak -> **Import** -> **OpenAPI** -> select `docs/openapi.yaml`.

## Auth

Every `/v1/*` route requires an API key: `Authorization: ApiKey <key>`
(the literal "ApiKey " prefix, then the raw key). `GET /healthz` is public.

## Base URL

The spec defines a `base_url` server variable, defaulting to
`http://localhost:8090` (the local docker-compose deployment's `API_ADDR`).
Override it in Yaak's environment settings to point at another deployment.
