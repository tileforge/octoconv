import { FormEvent, useEffect, useMemo, useState } from 'react';
import {
  ApiError,
  FormatsResponse,
  JobStatusResponse,
  cancelJob,
  createJob,
  getJob,
  getJobWithRetry,
  isTerminal,
  listFormats,
  subscribeJobEvents,
} from './api';
import {
  operationTable,
  operationsOf,
  sourceFormatOfFilename,
  targetFormatsForOperation,
} from './formats';

const apiKeyStorageKey = 'octoconv.apiKey';
const pollIntervalMs = 1800;
const defaultOperation = 'convert';

// describeError selects the user-facing message primarily from the
// machine-readable error.code (docs/ERROR-CODES.md) -- never by matching
// error.message text. The code list here is deliberately an open string,
// not a closed TypeScript union: docs/ERROR-CODES.md explicitly warns that a
// closed enum turns every future additive code into a breaking change here
// (PITFALLS.md Pitfall 12).
export function describeError(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.code) {
      case 'payload_too_large':
        return 'The selected file exceeds the upload size limit.';
      case 'unauthorized':
        return 'API key is missing, invalid, or revoked.';
      case 'rate_limited':
        return 'Too many requests. Please wait and try again.';
      case 'quota_exceeded':
        return 'Daily quota exceeded for this conversion type. Please try again later.';
      case 'unsupported_conversion':
        return error.message || 'This source/target conversion is not supported.';
      case 'invalid_options':
        return error.message || 'The provided options are invalid for this conversion.';
      case 'opts_not_applicable':
        return error.message || 'The provided options do not apply to this conversion.';
      case 'unknown_preset':
        return error.message || 'The requested preset could not be found.';
      case 'missing_file':
        return 'Choose a file to convert.';
      case 'not_found':
        return error.message || 'The requested job or preset could not be found.';
      case 'unsupported_operation':
        return error.message || 'The requested operation is not supported by this server.';
      default:
        // Unrecognized (or absent) code: degrade to the status-derived
        // message below rather than throwing or rendering nothing. A
        // future additive code (docs/ERROR-CODES.md) MUST land here safely.
        break;
    }

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
  const [operation, setOperation] = useState<string>(defaultOperation);
  const [formatsResp, setFormatsResp] = useState<FormatsResponse | null>(null);
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
      setFormatsResp(null);
      return undefined;
    }

    let cancelled = false;

    (async () => {
      try {
        const resp = await listFormats({ apiKey: trimmedKey });
        if (cancelled) {
          return;
        }
        setFormatsResp(resp);
        setFormatsNotice(null);
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

  // F3: targets are derived from the selected file's normalized source format
  // once a file is chosen (targetFormatsFor), falling back to the global
  // union (targetFormatsOf) before a file is picked. This replaces the
  // previous alphabetical-first default, which could land on an
  // engine-incompatible target (e.g. `alto` for a .docx upload).
  const formatRows = useMemo(() => (formatsResp ? operationTable(formatsResp) : []), [formatsResp]);

  const availableOperations = useMemo(() => (formatsResp ? operationsOf(formatsResp) : []), [formatsResp]);

  useEffect(() => {
    setOperation((prev) =>
      availableOperations.length === 0 || availableOperations.includes(prev)
        ? prev
        : availableOperations.includes(defaultOperation)
          ? defaultOperation
          : availableOperations[0],
    );
  }, [availableOperations]);

  const operationGroups = useMemo(() => {
    const groups = new Map<string, typeof formatRows>();
    for (const row of formatRows) {
      groups.set(row.operation, [...(groups.get(row.operation) ?? []), row]);
    }
    return Array.from(groups.entries());
  }, [formatRows]);

  const availableTargets = useMemo(() => {
    if (!formatsResp) {
      return [];
    }
    if (file) {
      return targetFormatsForOperation(formatsResp, operation, sourceFormatOfFilename(file.name));
    }
    return Array.from(new Set(formatRows.filter((row) => row.operation === operation).map((row) => row.to))).sort();
  }, [formatsResp, formatRows, file, operation]);

  useEffect(() => {
    setTarget((prev) => (prev && availableTargets.includes(prev) ? prev : (availableTargets[0] ?? '')));
  }, [availableTargets]);

  const jobId = job?.job_id ?? null;

  // Live progress via SSE, falling back to polling GET /v1/jobs/{id} on stream failure.
  //
  // F1: the job's status field MUST NOT be in this dependency array. A
  // terminal SSE frame flips that status, which used to change these deps, run the
  // cleanup, and set `cancelled = true` before the in-flight `finalize()` GET
  // it just launched could resolve — throwing away the only fetch that would
  // ever have carried `download_url` (terminal SSE frames deliberately carry
  // none). Keying the effect on `jobId` alone means the terminal transition
  // no longer tears the effect down out from under its own finalize call.
  // Dropping the `isTerminal` guard on entry is safe: a job only ever enters
  // this component as `queued`, `subscribeJobEvents` returns on the terminal
  // frame, the server closes the stream after it, and the polling fallback
  // clears its own interval on `isTerminal`.
  useEffect(() => {
    if (!jobId) {
      return undefined;
    }

    const trimmedKey = apiKey.trim();
    const controller = new AbortController();
    let cancelled = false;
    let finalized = false;
    let pollTimer: number | undefined;

    async function finalize() {
      if (finalized) {
        return;
      }
      finalized = true;
      try {
        const finalJob = await getJobWithRetry({ apiKey: trimmedKey, jobId: jobId! });
        // Identity-gated, not `cancelled`-gated: the correctness question is
        // "is this still the job on screen", which survives the effect
        // cleanup that the terminal status transition itself triggers. A
        // response for a superseded job (user started a new upload) is
        // still dropped.
        setJob((prev) => (prev && prev.job_id === jobId ? finalJob : prev));
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
          const nextJob = await getJob({ apiKey: trimmedKey, jobId: jobId! });
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
        setJob((prev) => (prev && prev.job_id === jobId ? { ...prev, ...evt } : prev));
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
  }, [apiKey, jobId]);

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
      const createdJob = await createJob({
        apiKey: apiKey.trim(),
        file,
        target,
        opts,
        operation: operation === defaultOperation ? undefined : operation,
      });
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
              <span>Operation</span>
              <select value={operation} onChange={(event) => setOperation(event.target.value)}>
                {availableOperations.length === 0 && <option value={operation}>{operation}</option>}
                {availableOperations.map((op) => (
                  <option key={op} value={op}>
                    {op}
                  </option>
                ))}
              </select>
            </label>

            <label className="field">
              <span>Target format</span>
              <select value={target} onChange={(event) => setTarget(event.target.value)}>
                {availableTargets.length === 0 && (
                  <option value="">
                    {!formatsResp
                      ? 'Enter API key to load formats'
                      : file
                        ? 'No conversions available for this file'
                        : 'Enter API key to load formats'}
                  </option>
                )}
                {availableTargets.map((format) => (
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

              {job.status === 'done' && job.outputs && job.outputs.length > 0 && (
                <div className="result-row">
                  <span>Outputs</span>
                  <ul className="outputs">
                    {job.outputs.map((output) => (
                      <li key={`${output.role}-${output.ordinal}`}>
                        <strong>{output.role}</strong> {output.format}, {output.size_bytes} bytes{' '}
                        <a href={output.url} target="_blank" rel="noopener noreferrer">
                          download
                        </a>
                      </li>
                    ))}
                  </ul>
                </div>
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

      {operationGroups.length > 0 && (
        <section className="card" aria-label="Supported conversions">
          <h2>Supported conversions</h2>
          {operationGroups.map(([op, rows]) => (
            <div key={op} className="operation-group">
              <h3>{op}</h3>
              <table className="formats-table">
                <thead>
                  <tr>
                    <th>engine</th>
                    <th>from</th>
                    <th>to</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((row) => (
                    <tr key={`${row.engine}-${row.from}-${row.to}`}>
                      <td>{row.engine}</td>
                      <td>{row.from}</td>
                      <td>{row.to}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ))}
        </section>
      )}
    </main>
  );
}
