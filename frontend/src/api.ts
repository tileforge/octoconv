export type JobStatus = 'queued' | 'active' | 'done' | 'failed' | 'canceled';

export const terminalStatuses = new Set<JobStatus>(['done', 'failed', 'canceled']);

export function isTerminal(status: JobStatus): boolean {
  return terminalStatuses.has(status);
}

export type FormatsResponse = {
  engines: Record<string, { pairs: [string, string][] }>;
};

export type CreateJobResponse = {
  job_id: string;
  status: JobStatus;
  opts?: Record<string, unknown>;
};

export type JobStatusResponse = {
  job_id: string;
  status: JobStatus;
  progress_pct: number | null;
  eta_seconds: number | null;
  opts?: Record<string, unknown>;
  download_url?: string;
  error_code?: string;
  error_message?: string;
};

export type ProgressEvent = {
  status: JobStatus;
  progress_pct: number | null;
  eta_seconds: number | null;
};

export class ApiError extends Error {
  readonly status: number;
  // Machine-readable error code from the response body's flat {"error","code"}
  // envelope (docs/ERROR-CODES.md, D-01). Undefined when the body carries no
  // code at all (non-JSON error, SSE frame, legacy/proxy error) -- describeError
  // in App.tsx falls back to the status-derived message in that case.
  readonly code?: string;

  constructor(status: number, message: string, code?: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

async function parseResponse<T>(response: Response): Promise<T> {
  const contentType = response.headers.get('content-type') ?? '';
  const body = contentType.includes('application/json')
    ? await response.json()
    : await response.text();

  if (!response.ok) {
    const message =
      typeof body === 'object' && body !== null && 'error' in body
        ? String(body.error)
        : response.statusText || 'Request failed';
    const code =
      typeof body === 'object' && body !== null && 'code' in body ? String(body.code) : undefined;
    throw new ApiError(response.status, message, code);
  }

  return body as T;
}

function authHeaders(apiKey: string): HeadersInit {
  return {
    Authorization: `ApiKey ${apiKey}`,
  };
}

export async function listFormats(params: { apiKey: string }): Promise<FormatsResponse> {
  const response = await fetch('/v1/formats', {
    headers: authHeaders(params.apiKey),
  });

  return parseResponse<FormatsResponse>(response);
}

export async function createJob(params: {
  apiKey: string;
  file: File;
  target: string;
  opts?: string;
}): Promise<CreateJobResponse> {
  const form = new FormData();
  form.append('file', params.file);
  form.append('target', params.target);

  const trimmedOpts = params.opts?.trim();
  if (trimmedOpts) {
    form.append('opts', trimmedOpts);
  }

  const response = await fetch('/v1/jobs', {
    method: 'POST',
    headers: authHeaders(params.apiKey),
    body: form,
  });

  return parseResponse<CreateJobResponse>(response);
}

export async function getJob(params: {
  apiKey: string;
  jobId: string;
}): Promise<JobStatusResponse> {
  const response = await fetch(`/v1/jobs/${encodeURIComponent(params.jobId)}`, {
    headers: authHeaders(params.apiKey),
  });

  return parseResponse<JobStatusResponse>(response);
}

/**
 * isRetryableError classifies whether a failed request is worth retrying.
 * Retrying a 401/403/404/422 cannot succeed — the request is permanently
 * rejected — so retrying it only burns the retry budget for nothing. An
 * aborted request (the caller cancelled it) must never be retried either.
 * A 5xx, 429 (rate limited) or 408 (request timeout), and any other
 * unrecognised failure (notably fetch's own `TypeError` on a network blip),
 * ARE retryable — that transient-failure case is exactly what F2 exists for.
 */
export function isRetryableError(error: unknown): boolean {
  if (error instanceof ApiError) {
    return error.status >= 500 || error.status === 429 || error.status === 408;
  }
  if (error instanceof DOMException && error.name === 'AbortError') {
    return false;
  }
  return true;
}

/**
 * getJobWithRetry wraps `getJob` with a bounded retry budget for the
 * post-terminal finalize GET (F2): at most `attempts` requests total and a
 * fixed backoff between them, so a transient blip does not permanently hide
 * a completed job's result, while a permanent error (401/403/404/422) or an
 * abort fails fast without wasting the budget. `fetchJob` and `sleep` are
 * injectable seams for testing the retry semantics without a network or
 * real timers; callers in the app never need to pass them.
 */
export async function getJobWithRetry(params: {
  apiKey: string;
  jobId: string;
  attempts?: number;
  delaysMs?: number[];
  fetchJob?: (p: { apiKey: string; jobId: string }) => Promise<JobStatusResponse>;
  sleep?: (ms: number) => Promise<void>;
}): Promise<JobStatusResponse> {
  const attempts = params.attempts ?? 3;
  const delaysMs = params.delaysMs ?? [400, 1200];
  const fetchJob = params.fetchJob ?? getJob;
  const sleep = params.sleep ?? ((ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms)));

  let lastError: unknown;
  for (let attempt = 1; attempt <= attempts; attempt += 1) {
    try {
      return await fetchJob({ apiKey: params.apiKey, jobId: params.jobId });
    } catch (err) {
      lastError = err;
      if (!isRetryableError(err) || attempt === attempts) {
        throw err;
      }
      await sleep(delaysMs[attempt - 1] ?? delaysMs[delaysMs.length - 1] ?? 0);
    }
  }
  // Unreachable: the loop always returns or throws. Satisfies the compiler.
  throw lastError;
}

export async function cancelJob(params: {
  apiKey: string;
  jobId: string;
}): Promise<{ job_id: string; status: JobStatus }> {
  const response = await fetch(`/v1/jobs/${encodeURIComponent(params.jobId)}/cancel`, {
    method: 'POST',
    headers: authHeaders(params.apiKey),
  });

  return parseResponse<{ job_id: string; status: JobStatus }>(response);
}

/**
 * subscribeJobEvents opens the SSE stream at GET /v1/jobs/{id}/events via
 * fetch()+ReadableStream (browser EventSource cannot set the Authorization
 * header). Frames are `event: progress\ndata: {json}\n\n`; `:`-prefixed
 * keepalive comment lines are ignored. The read loop stops once a terminal
 * status is observed. Rejects (non-abort) so callers can fall back to polling.
 */
export async function subscribeJobEvents(params: {
  apiKey: string;
  jobId: string;
  signal: AbortSignal;
  onProgress: (event: ProgressEvent) => void;
}): Promise<void> {
  const response = await fetch(`/v1/jobs/${encodeURIComponent(params.jobId)}/events`, {
    headers: authHeaders(params.apiKey),
    signal: params.signal,
  });

  if (!response.ok || !response.body) {
    throw new ApiError(response.status, response.statusText || 'SSE connection failed');
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) {
        return;
      }

      buffer += decoder.decode(value, { stream: true });

      let separatorIndex: number;
      while ((separatorIndex = buffer.indexOf('\n\n')) !== -1) {
        const frame = buffer.slice(0, separatorIndex);
        buffer = buffer.slice(separatorIndex + 2);

        const dataLines: string[] = [];
        for (const line of frame.split('\n')) {
          if (line.startsWith(':')) {
            continue; // keepalive comment
          }
          if (line.startsWith('data:')) {
            dataLines.push(line.slice(5).replace(/^ /, ''));
          }
        }

        if (dataLines.length === 0) {
          continue;
        }

        const parsed = JSON.parse(dataLines.join('\n')) as ProgressEvent;
        params.onProgress(parsed);

        if (isTerminal(parsed.status)) {
          await reader.cancel();
          return;
        }
      }
    }
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') {
      return;
    }
    throw err;
  } finally {
    reader.releaseLock();
  }
}
