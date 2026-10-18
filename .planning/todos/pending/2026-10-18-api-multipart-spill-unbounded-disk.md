---
created: 2026-10-18
area: deploy
severity: medium
origin: quick task 261018-bmr research
---

# The API pod's multipart spill has no disk bound

## What

`handleCreateJob` and `handleInspect` call `ParseMultipartForm(multipartInMemoryBudget)` (32 MiB,
`internal/api/handlers.go`), so every file part beyond 32 MiB is spilled through `os.TempDir()` to
`/tmp` in the api container's writable layer (`Dockerfile.api` sets no `TMPDIR`) and removed by
`MultipartForm.RemoveAll` when the handler returns. The chart declares no `emptyDir`, `sizeLimit` or
`ephemeral-storage` request or limit for any pod, and the compose api service has no `tmpfs`.

Per api pod the transient spill is bounded only by: concurrent creates times `MAX_UPLOAD_BYTES`
(nothing bounds the number of concurrent creates except `RATE_LIMIT_IP_RPM` and
`RATE_LIMIT_CLIENT_RPM`), plus 12 times `INSPECT_MAX_UPLOAD_BYTES` (8 intakes,
`inspectIntakeMaxConcurrent`, plus 4 analyses, `INSPECT_MAX_CONCURRENT`, each holding its spilled
file). A burst fills the node's disk, and kubelet's DiskPressure eviction then evicts pods without
requests first, which in this chart is every pod.

This predates 261018-bmr. That task raised the chart's `MAX_UPLOAD_BYTES` and
`INSPECT_MAX_UPLOAD_BYTES` from 100 MiB to 183 MiB, which scales the chart's bound 1.83x (inspect
alone: about 2.1 GiB per pod at 12 x 183 MiB). In compose `MAX_UPLOAD_BYTES` is 2 GiB with the same
mechanism.

## Idea

- Mount an `emptyDir` with a `sizeLimit` at a directory the api reads as `TMPDIR`, and give the api
  container an `ephemeral-storage` request and limit derived from the caps and the intake
  concurrency, so a burst evicts the api pod instead of its neighbours.
- Or bound the create path's intake the way inspect's is bounded: a semaphore around
  `ParseMultipartForm` sized so that slots x `MAX_UPLOAD_BYTES` fits the volume.

## Constraints

- `multipartInMemoryBudget`, `inspectIntakeMaxConcurrent` and `inspectIntakeReadDeadline` bound the
  heap; any change here must keep that bound (8 x 32 MiB of the 1Gi limit).
- A create-path semaphore turns a disk failure into a 429/503 under load; the answer code needs a
  decision, as inspect's `inspect_busy` did.
