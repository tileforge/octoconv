import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, listJobs } from './api';
import { requestGeneration, watchJob } from './jobTracking';
afterEach(() => vi.unstubAllGlobals());
describe('listJobs', () => {
  it('authenticates and encodes pagination and supported filters', async () => {
    let request: { url: string; init?: RequestInit } | undefined;
    vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
      request = { url, init };
      return new Response('{"jobs":[],"next_cursor":"next"}', { headers: { 'content-type': 'application/json' } });
    });
    expect(await listJobs({ apiKey: 'secret', status: 'done', engine: 'image', cursor: 'a+/=', limit: 20 })).toEqual({ jobs: [], next_cursor: 'next' });
    expect(Object.fromEntries(new URL(request!.url, 'http://local').searchParams)).toEqual({ status: 'done', engine: 'image', cursor: 'a+/=', limit: '20' });
    expect(request!.init!.headers).toEqual({ Authorization: 'ApiKey secret' });
  });
  it('preserves empty pages and nullable cursor', async () => {
    vi.stubGlobal('fetch', async () => new Response('{"jobs":[],"next_cursor":null}', { headers: { 'content-type': 'application/json' } }));
    expect(await listJobs({ apiKey: 'k' })).toEqual({ jobs: [], next_cursor: null });
  });
  it('propagates authorization codes', async () => {
    vi.stubGlobal('fetch', async () => new Response('{"error":"denied","code":"unauthorized"}', { status: 401, headers: { 'content-type': 'application/json' } }));
    await expect(listJobs({ apiKey: 'bad' })).rejects.toMatchObject({ status: 401, code: 'unauthorized' });
  });
});
it('rejects old responses even after returning to the same session key', async () => {
  const generation = requestGeneration();
  let resolve!: (value: string) => void;
  const pending = new Promise<string>(r => { resolve = r; });
  const current = generation.next();
  const committed: string[] = [];
  const task = pending.then(value => { if (current()) committed.push(value); });
  generation.next(); generation.next(); resolve('old download'); await task;
  expect(committed).toEqual([]);
});
describe('job tracking', () => {
  const job = { job_id: 'a', status: 'queued' as const, progress_pct: null, eta_seconds: null };
  it('retrieves final downloads after terminal events', async () => {
    const updates: unknown[] = [];
    const stop = watchJob({ apiKey: 'k', jobId: 'a', onJob: j => updates.push(j), onError: () => {}, fetchJob: async () => job,
      finalize: async () => ({ ...job, status: 'done', download_url: '/result' }),
      subscribe: async p => { p.onProgress({ status: 'done', progress_pct: 100, eta_seconds: 0 }); },
    });
    await vi.waitFor(() => expect(updates).toContainEqual(expect.objectContaining({ download_url: '/result' }))); stop();
  });
  it('polls after a clean stream close and stops at a terminal result', async () => {
    let calls = 0; const updates: unknown[] = [];
    const stop = watchJob({ apiKey: 'k', jobId: 'a', onJob: j => updates.push(j), onError: () => {}, interval: 1,
      fetchJob: async () => { calls++; return calls === 1 ? job : { ...job, status: 'done', download_url: '/ready' }; }, subscribe: async () => {},
    });
    await vi.waitFor(() => expect(updates).toContainEqual(expect.objectContaining({ download_url: '/ready' }))); stop(); expect(calls).toBe(2);
  });
  it('discards responses after selection cleanup', async () => {
    let resolve!: (v: typeof job) => void; const updates: unknown[] = [];
    const stop = watchJob({ apiKey: 'old', jobId: 'a', onJob: j => updates.push(j), onError: () => {}, fetchJob: () => new Promise(r => { resolve = r; }), subscribe: async () => {} });
    stop(); resolve(job); await Promise.resolve(); expect(updates).toEqual([]);
  });
  it('reports permanent errors without opening a stream', async () => {
    const errors: unknown[] = []; let opened = false;
    const stop = watchJob({ apiKey: 'bad', jobId: 'a', onJob: () => {}, onError: e => errors.push(e), fetchJob: async () => { throw new ApiError(401, 'denied'); }, subscribe: async () => { opened = true; } });
    await vi.waitFor(() => expect(errors).toHaveLength(1)); expect(opened).toBe(false); stop();
  });
});

import { downloadsForJob } from './jobTracking';
it('keeps the primary result and every distinct additional output', () => {
  const job = { job_id: 'a', status: 'done' as const, progress_pct: 100, eta_seconds: 0, download_url: '/main', outputs: [
    { role: 'main', ordinal: 0, format: 'pdf', size_bytes: 10, url: '/main' },
    { role: 'preview', ordinal: 1, format: 'png', size_bytes: 20, url: '/preview' },
  ] };
  expect(downloadsForJob(job).map(item => item.url)).toEqual(['/main', '/preview']);
  expect(downloadsForJob({ ...job, status: 'active' })).toEqual([]);
});
it('drops in-flight final download responses when the selected job is replaced', async () => {
  const job = { job_id: 'a', status: 'queued' as const, progress_pct: null, eta_seconds: null };
  let resolve!: (v: typeof job) => void;
  let finalStarted = false;
  const updates: unknown[] = [];
  const stop = watchJob({ apiKey: 'k', jobId: 'a', onJob: value => updates.push(value), onError: () => {}, fetchJob: async () => job,
    finalize: () => { finalStarted = true; return new Promise(r => { resolve = r; }); },
    subscribe: async p => { p.onProgress({ status: 'done', progress_pct: 100, eta_seconds: 0 }); },
  });
  await vi.waitFor(() => expect(finalStarted).toBe(true));
  stop(); const count = updates.length; resolve(job); await Promise.resolve();
  expect(updates).toHaveLength(count);
});

import { clearSubmittedFile, retainedJobSelection, needsJobsRefresh } from './jobTracking';
it('retains completed details when selecting the same job again', () => {
  const job = { job_id: 'a', status: 'done' as const, progress_pct: 100, eta_seconds: 0, download_url: '/result' };
  expect(retainedJobSelection(job, 'a')).toBe(job);
  expect(retainedJobSelection(job, 'b')).toBeNull();
});
it('clears only the file submitted, preserving a newer selection', () => {
  const submitted = new File(['a'], 'a.png');
  const newer = new File(['b'], 'b.png');
  expect(clearSubmittedFile(submitted, submitted)).toBeNull();
  expect(clearSubmittedFile(newer, submitted)).toBe(newer);
});
it('refreshes pages containing pending jobs, stopping for all-terminal or empty pages', () => {
  expect(needsJobsRefresh([{ status: 'active' }, { status: 'done' }])).toBe(true);
  expect(needsJobsRefresh([{ status: 'queued' }])).toBe(true);
  expect(needsJobsRefresh([{ status: 'awaiting_upload' }])).toBe(true);
  expect(needsJobsRefresh([{ status: 'done' }, { status: 'failed' }, { status: 'canceled' }])).toBe(false);
  expect(needsJobsRefresh([])).toBe(false);
});
import { startJobsRefresh } from './jobTracking';
it('refreshes at intervals only when ready and cancels its timer on cleanup', () => {
  vi.useFakeTimers();
  let ready = false; let count = 0;
  const stop = startJobsRefresh({ ready: () => ready, refresh: () => { count++; } });
  vi.advanceTimersByTime(10_000); expect(count).toBe(0);
  ready = true; vi.advanceTimersByTime(10_000); expect(count).toBe(1);
  stop(); vi.advanceTimersByTime(30_000); expect(count).toBe(1);
  vi.useRealTimers();
});
