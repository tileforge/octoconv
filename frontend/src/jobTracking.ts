import { getJob, getJobWithRetry, isRetryableError, isTerminal, subscribeJobEvents } from './api';
import type { JobStatusResponse } from './api';

/** A response remains valid only until the next request or explicit invalidation. */
export function requestGeneration() {
  let version = 0;
  return {
    next() { const mine = ++version; return () => mine === version; },
    invalidate() { version++; },
  };
}

/** Owns one selected job. Cleanup invalidates every in-flight read and event. */
export function watchJob(params: {
  apiKey: string; jobId: string;
  onJob: (job: JobStatusResponse) => void;
  onError: (error: unknown) => void;
  fetchJob?: typeof getJob;
  finalize?: typeof getJob;
  subscribe?: typeof subscribeJobEvents;
  interval?: number;
}) {
  const controller = new AbortController();
  const fetchJob = params.fetchJob ?? getJob;
  let stopped = false;
  let terminal = false;
  let finishing = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let latest: JobStatusResponse | undefined;
  const identity = { apiKey: params.apiKey, jobId: params.jobId };
  function publish(job: JobStatusResponse) {
    if (stopped) return;
    latest = job;
    terminal = isTerminal(job.status);
    params.onJob(job);
  }
  function schedule() {
    if (!stopped && !terminal) timer = setTimeout(() => { void poll(); }, params.interval ?? 1800);
  }
  async function poll() {
    try { publish(await fetchJob(identity)); }
    catch (error) {
      if (!stopped) params.onError(error);
      if (!isRetryableError(error)) return;
    }
    schedule();
  }
  async function finalize() {
    if (finishing) return;
    finishing = true;
    try { publish(await (params.finalize ?? getJobWithRetry)(identity)); }
    catch (error) { if (!stopped) params.onError(error); }
  }
  async function start() {
    try { publish(await fetchJob(identity)); }
    catch (error) {
      if (!stopped) params.onError(error);
      if (isRetryableError(error)) schedule();
      return;
    }
    if (stopped || terminal) return;
    try {
      await (params.subscribe ?? subscribeJobEvents)({ ...identity, signal: controller.signal,
        onProgress(event) {
          if (stopped || terminal || !latest) return;
          publish({ ...latest, ...event });
          if (isTerminal(event.status)) void finalize();
        },
      });
    } catch (error) {
      if (!stopped && !isRetryableError(error)) { params.onError(error); return; }
    }
    // Both stream errors and an early normal EOF need a polling fallback.
    schedule();
  }
  void start();
  return () => { stopped = true; controller.abort(); if (timer !== undefined) clearTimeout(timer); };
}

export function downloadsForJob(job: JobStatusResponse | null) {
  if (!job || job.status !== 'done') return [];
  const links: Array<{ key: string; url: string; role?: string; format?: string; size?: number }> = [];
  for (const output of job.outputs ?? []) {
    if (!links.some(link => link.url === output.url)) links.push({ key: `${output.role}-${output.ordinal}`, url: output.url, role: output.role, format: output.format, size: output.size_bytes });
  }
  if (job.download_url && !links.some(link => link.url === job.download_url)) links.unshift({ key: 'result', url: job.download_url });
  return links;
}

export function retainedJobSelection(job: JobStatusResponse | null, selected: string) {
  return job?.job_id === selected ? job : null;
}
export function clearSubmittedFile(current: File | null, submitted: File) {
  return current === submitted ? null : current;
}
export function needsJobsRefresh(jobs: Array<{ status: import('./api').JobStatus }>) {
  return jobs.some(job => !isTerminal(job.status));
}
export function startJobsRefresh(params: { ready: () => boolean; refresh: () => void }) {
  const timer = setInterval(() => { if (params.ready()) params.refresh(); }, 10_000);
  return () => clearInterval(timer);
}
