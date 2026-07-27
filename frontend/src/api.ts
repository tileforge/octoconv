export type JobStatus = 'queued' | 'active' | 'done' | 'failed' | 'canceled';

export const terminalStatuses = new Set<JobStatus>(['done', 'failed', 'canceled']);

export function isTerminal(status: JobStatus): boolean {
  return terminalStatuses.has(status);
}

export type FormatsResponse = {
  engines: Record<string, { pairs: [string, string][] }>;
};

/**
 * targetFormatsOf returns the deduped, alphabetically sorted union of every
 * engine class's target formats (pair[1]), so all engine classes reported by
 * GET /v1/formats are reachable from a single select box.
 */
export function targetFormatsOf(resp: FormatsResponse): string[] {
  const targets = new Set<string>();
  for (const engine of Object.values(resp.engines)) {
    for (const pair of engine.pairs) {
      targets.add(pair[1]);
    }
  }
  return Array.from(targets).sort();
}

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

  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
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
    throw new ApiError(response.status, message);
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
