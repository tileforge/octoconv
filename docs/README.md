# OctoConv documentation

[Back to the project](../README.md).

## Get started

- [Local setup](GETTING-STARTED.md): prerequisites, infrastructure, workers and API keys.
- [Formats and operations](FORMATS.md): supported inputs, outputs and conversion engines.
- [HTTP API](API.md): endpoints, request limits, errors and job correlation.
- [OpenAPI 3.1](openapi.yaml): import into Yaak or use with your API tooling.
- [MCP integration](MCP.md): stdio and streamable HTTP clients.

## File processing

| Document | Contract |
|---|---|
| [CAD](CAD.md) | Formats, previews, options, ceilings and known limits |
| [Preview](PREVIEW.md) | Framing, sizes, backgrounds and output behavior |
| [Measurements](MEASUREMENTS.md) | Geometry JSON schema and validation rules |
| [Sanitization](SANITIZE.md) | What a same-format rebuild removes and retains |
| [Typesetting](TYPESETTING.md) | TeX bundle, cache, confinement and measured limits |
| [Inspection findings](FINDINGS.md) | Findings vocabulary and incomplete analysis |
| [Error codes](ERROR-CODES.md) | Machine-readable synchronous and job-failure codes |
| [Job search](JOB-SEARCH.md) | Filtering, pagination and supported search fields |
| [Worker Protocol](WORKER-PROTOCOL.md) | External workers, not the client API |

## Operate and maintain

- [Operations](OPERATIONS.md): architecture, quotas, recovery and configuration.
- [Worker handover](WORKER-HANDOVER.md): protocol version 1, certificates, and the image worker.
- [Sandbox paths](SANDBOX-PATHS.md): measured filesystem access for each engine.
- [Environment variables](../.env.example): configuration defaults and limit derivations.
- [Helm chart](../deploy/chart/octoconv): Kubernetes deployment and KEDA scaling.
- [Early milestones](HISTORY.md): historical record through v1.14.

The documentation's route set, response coverage, engine list, limits and several
format contracts are checked against code. OpenAPI response bodies and examples are
maintained by hand; see [HTTP API](API.md) for the boundary of those checks.

## Import into Yaak

Choose **Import → OpenAPI → `docs/openapi.yaml`**. The default base URL is
`http://localhost:8090`; change the `base_url` server variable for your deployment.
Every `/v1/*` request needs `Authorization: ApiKey <key>`. `GET /healthz` is public.
