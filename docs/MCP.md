# MCP integration

[Back to OctoConv](../README.md) · [Documentation](README.md)

Both expose the same tools — `convert_file`, `get_job_status`, `download_result`,
`list_supported_formats`, `list_presets` — to any [Model Context Protocol][mcp] client. Each
is a zero-privilege HTTP client of the public API: it holds a client API key, never a
database, S3 or Redis credential.

**`cmd/mcp-server`** speaks stdio, for a local agent:

```bash
go build -o octoconv-mcp ./cmd/mcp-server
```

```json
{
  "mcpServers": {
    "octoconv": {
      "command": "/path/to/octoconv-mcp",
      "env": {
        "OCTOCONV_BASE_URL": "http://localhost:8090",
        "OCTOCONV_API_KEY": "<raw-key-from-manage-clients>"
      }
    }
  }
}
```

Also reads `OCTOCONV_OUTPUT_DIR`, `OCTOCONV_CONVERT_TIMEOUT`, `OCTOCONV_POLL_INTERVAL`, and
`OCTOCONV_S3_DIAL_ADDR` for the operator-only case of redialing presigned downloads.

**`cmd/mcp-http`** speaks streamable HTTP, for in-cluster deployment
(`deploy/chart/octoconv/templates/deployment-mcp-http.yaml`, gated by `mcpHttp.enabled`):

- **Auth is per-request, not per-process.** The pod holds no API key. Every request carries
  `Authorization: ApiKey <key>`, rejected with `401` before any JSON-RPC code runs, and a
  session-key binding rejects (`403`) a request whose `Mcp-Session-Id` doesn't match the key
  that created it.
- **Results are presigned-only** — HTTP mode never writes to the pod's filesystem.
- **Config is `OCTOCONV_BASE_URL` + `MCP_HTTP_ADDR` only** (default `:8070`).
- **ClusterIP only**, behind a NetworkPolicy that default-denies ingress except `:8070` from
  within the release namespace. Reach it with
  `kubectl -n octoconv port-forward svc/mcp-http 8070:8070`.

[mcp]: https://modelcontextprotocol.io
