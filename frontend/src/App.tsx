import { FormEvent, useEffect, useMemo, useState } from 'react';
import {
  ApiError,
  JobStatusResponse,
  cancelJob,
  createJob,
  getJob,
  isTerminal,
  listFormats,
  subscribeJobEvents,
  targetFormatsOf,
} from './api';

const apiKeyStorageKey = 'octoconv.apiKey';
const pollIntervalMs = 1800;

function describeError(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 401:
        return 'API key is missing, invalid, or revoked.';
      case 413:
        return 'The selected file exceeds the upload size limit.';
      case 422:
        return error.message || 'This source/target conversion is not supported.';
      case 429:
        return 'Too many requests. Please wait and try again.';
      default:
        return error.message || `Request failed with status ${error.status}.`;
    }
  }

  if (error instanceof Error) {
    return error.message;
  }

  return 'Unexpected error.';
}

export default function App() {
  const [apiKey, setApiKey] = useState(() => localStorage.getItem(apiKeyStorageKey) ?? '');
  const [rememberApiKey, setRememberApiKey] = useState(() =>
    Boolean(localStorage.getItem(apiKeyStorageKey)),
  );
  const [file, setFile] = useState<File | null>(null);
  const [target, setTarget] = useState<string>('');
  const [formats, setFormats] = useState<string[]>([]);
  const [formatsNotice, setFormatsNotice] = useState<string | null>(null);
  const [opts, setOpts] = useState('');
  const [job, setJob] = useState<JobStatusResponse | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isCanceling, setIsCanceling] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const canSubmit = useMemo(
    () => apiKey.trim().length > 0 && file !== null && target !== '' && !isSubmitting,
    [apiKey, file, target, isSubmitting],
  );

  useEffect(() => {
    if (rememberApiKey && apiKey.trim()) {
      localStorage.setItem(apiKeyStorageKey, apiKey.trim());
      return;
    }

    localStorage.removeItem(apiKeyStorageKey);
  }, [apiKey, rememberApiKey]);

  useEffect(() => {
    const trimmedKey = apiKey.trim();
    if (!trimmedKey) {
      setFormats([]);
      return undefined;
    }

    let cancelled = false;

    (async () => {
      try {
        const resp = await listFormats({ apiKey: trimmedKey });
        if (cancelled) {
          return;
        }
        const nextFormats = targetFormatsOf(resp);
        setFormats(nextFormats);
        setFormatsNotice(null);
        setTarget((prev) => (prev && nextFormats.includes(prev) ? prev : (nextFormats[0] ?? '')));
      } catch (formatsError) {
        if (!cancelled) {
          setFormatsNotice(describeError(formatsError));
        }
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [apiKey]);

  // Live progress via SSE, falling back to polling GET /v1/jobs/{id} on stream failure.
  useEffect(() => {
    if (!job || isTerminal(job.status)) {
      return undefined;
    }

    const trimmedKey = apiKey.trim();
    const jobId = job.job_id;
    const controller = new AbortController();
    let cancelled = false;
    let pollTimer: number | undefined;

    async function finalize() {
      try {
        const finalJob = await getJob({ apiKey: trimmedKey, jobId });
        if (!cancelled) {
          setJob(finalJob);
        }
      } catch (finalizeError) {
        if (!cancelled) {
          setError(describeError(finalizeError));
        }
      }
    }

    function startPolling() {
      if (pollTimer !== undefined) {
        return;
      }
      pollTimer = window.setInterval(async () => {
        try {
          const nextJob = await getJob({ apiKey: trimmedKey, jobId });
          if (!cancelled) {
            setJob(nextJob);
            setError(null);
            if (isTerminal(nextJob.status)) {
              window.clearInterval(pollTimer);
              pollTimer = undefined;
            }
          }
        } catch (pollError) {
          if (!cancelled) {
            setError(describeError(pollError));
          }
        }
      }, pollIntervalMs);
    }

    subscribeJobEvents({
      apiKey: trimmedKey,
      jobId,
      signal: controller.signal,
      onProgress: (evt) => {
        if (cancelled) {
          return;
        }
        setJob((prev) => (prev ? { ...prev, ...evt } : prev));
        if (isTerminal(evt.status)) {
          void finalize();
        }
      },
    }).catch((sseError) => {
      if (cancelled || controller.signal.aborted) {
        return;
      }
      startPolling();
      void sseError;
    });

    return () => {
      cancelled = true;
      controller.abort();
      if (pollTimer !== undefined) {
        window.clearInterval(pollTimer);
      }
    };
  }, [apiKey, job?.job_id, job?.status]);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!file) {
      setError('Choose a file to convert.');
      return;
    }
    if (!target) {
      setError('Choose a target format.');
      return;
    }

    setIsSubmitting(true);
    setError(null);
    setJob(null);

    try {
      const createdJob = await createJob({ apiKey: apiKey.trim(), file, target, opts });
      setJob({
        job_id: createdJob.job_id,
        status: createdJob.status,
        progress_pct: null,
        eta_seconds: null,
        opts: createdJob.opts,
      });
    } catch (submitError) {
      setError(describeError(submitError));
    } finally {
      setIsSubmitting(false);
    }
  }

  async function handleCancel() {
    if (!job || isCanceling) {
      return;
    }
    setIsCanceling(true);
    try {
      const result = await cancelJob({ apiKey: apiKey.trim(), jobId: job.job_id });
      setJob((prev) => (prev ? { ...prev, ...result } : prev));
    } catch (cancelError) {
      setError(describeError(cancelError));
    } finally {
      setIsCanceling(false);
    }
  }

  return (
    <main className="shell">
      <section className="hero" aria-labelledby="page-title">
        <p className="eyebrow">OctoConv</p>
        <h1 id="page-title">Convert files through the REST API</h1>
        <p className="lede">
          Upload a file, choose the target format, and download the converted result when the
          worker finishes processing it — with live progress for lengthy jobs.
        </p>
      </section>

      <section className="card" aria-label="Upload form">
        <form onSubmit={handleSubmit} className="form">
          <label className="field">
            <span>API key</span>
            <input
              type="password"
              value={apiKey}
              placeholder="ApiKey value generated by manage-clients"
              autoComplete="off"
              onChange={(event) => setApiKey(event.target.value)}
            />
          </label>

          <label className="checkbox">
            <input
              type="checkbox"
              checked={rememberApiKey}
              onChange={(event) => setRememberApiKey(event.target.checked)}
            />
            <span>Remember this key in this browser</span>
          </label>

          <div className="grid">
            <label className="field">
              <span>File</span>
              <input type="file" onChange={(event) => setFile(event.target.files?.[0] ?? null)} />
            </label>

            <label className="field">
              <span>Target format</span>
              <select value={target} onChange={(event) => setTarget(event.target.value)}>
                {formats.length === 0 && <option value="">Enter API key to load formats</option>}
                {formats.map((format) => (
                  <option key={format} value={format}>
                    {format.toUpperCase()}
                  </option>
                ))}
              </select>
            </label>
          </div>

          <label className="field">
            <span>Options (JSON, optional)</span>
            <input
              type="text"
              value={opts}
              placeholder='{"language":"eng"}'
              onChange={(event) => setOpts(event.target.value)}
            />
          </label>

          {formatsNotice && <p className="hint">{formatsNotice}</p>}

          <button type="submit" disabled={!canSubmit}>
            {isSubmitting ? 'Uploading…' : 'Convert'}
          </button>
        </form>
      </section>

      {(job || error) && (
        <section className="card result" aria-live="polite">
          {job && (
            <>
              <div className="result-row">
                <span>Job ID</span>
                <code>{job.job_id}</code>
              </div>
              <div className="result-row">
                <span>Status</span>
                <strong className={`status status-${job.status}`}>{job.status}</strong>
              </div>

              {!isTerminal(job.status) && typeof job.progress_pct === 'number' && (
                <div className="result-row">
                  <span>Progress</span>
                  <div>
                    <div className="progress">
                      <div className="progress-bar" style={{ width: `${job.progress_pct}%` }} />
                    </div>
                    <p className="hint">
                      {job.progress_pct}%
                      {job.eta_seconds != null ? ` — ETA ~${job.eta_seconds}s` : ''}
                    </p>
                  </div>
                </div>
              )}

              {!isTerminal(job.status) && job.progress_pct == null && (
                <div className="result-row">
                  <span>Progress</span>
                  <p className="hint">processing…</p>
                </div>
              )}

              {!isTerminal(job.status) && (
                <button
                  type="button"
                  className="cancel"
                  disabled={isCanceling}
                  onClick={handleCancel}
                >
                  {isCanceling ? 'Canceling…' : 'Cancel job'}
                </button>
              )}

              {job.download_url && (
                <>
                  <a className="download" href={job.download_url} rel="noreferrer">
                    Download converted file
                  </a>
                  <p className="hint">
                    Local presigned URLs point at <code>minio:9000</code>, which is unreachable
                    from your browser — rewrite the host to <code>localhost:9100</code> to
                    download locally.
                  </p>
                </>
              )}

              {job.status === 'failed' && (
                <p className="error">
                  {job.error_message || job.error_code || 'Conversion failed.'}
                </p>
              )}
            </>
          )}
          {error && <p className="error">{error}</p>}
        </section>
      )}
    </main>
  );
}
