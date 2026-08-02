import { describe, expect, it, vi } from 'vitest';
import { ApiError, getJobWithRetry, isRetryableError } from './api';
import type { JobStatusResponse } from './api';

describe('isRetryableError', () => {
  it('treats 5xx ApiErrors as retryable', () => {
    expect(isRetryableError(new ApiError(500, 'x'))).toBe(true);
    expect(isRetryableError(new ApiError(503, 'x'))).toBe(true);
  });

  it('treats 429 and 408 ApiErrors as retryable', () => {
    expect(isRetryableError(new ApiError(429, 'x'))).toBe(true);
    expect(isRetryableError(new ApiError(408, 'x'))).toBe(true);
  });

  it('treats permanent client-error ApiErrors as non-retryable', () => {
    expect(isRetryableError(new ApiError(401, 'x'))).toBe(false);
    expect(isRetryableError(new ApiError(403, 'x'))).toBe(false);
    expect(isRetryableError(new ApiError(404, 'x'))).toBe(false);
    expect(isRetryableError(new ApiError(422, 'x'))).toBe(false);
  });

  it('never retries an aborted request', () => {
    expect(isRetryableError(new DOMException('aborted', 'AbortError'))).toBe(false);
  });

  it('treats a network-layer TypeError as retryable', () => {
    expect(isRetryableError(new TypeError('Failed to fetch'))).toBe(true);
  });
});

function makeJob(overrides: Partial<JobStatusResponse> = {}): JobStatusResponse {
  return {
    job_id: 'job-1',
    status: 'done',
    progress_pct: 100,
    eta_seconds: null,
    ...overrides,
  };
}

describe('getJobWithRetry', () => {
  it('resolves on the first successful call with no retry', async () => {
    const job = makeJob();
    const fetchJob = vi.fn().mockResolvedValue(job);
    const sleep = vi.fn().mockResolvedValue(undefined);

    const result = await getJobWithRetry({ apiKey: 'k', jobId: 'job-1', fetchJob, sleep });

    expect(result).toBe(job);
    expect(fetchJob).toHaveBeenCalledTimes(1);
    expect(sleep).toHaveBeenCalledTimes(0);
  });

  it('retries a retryable failure and resolves once it succeeds, backing off in order', async () => {
    const job = makeJob();
    const fetchJob = vi
      .fn()
      .mockRejectedValueOnce(new ApiError(503, 'x'))
      .mockRejectedValueOnce(new ApiError(503, 'x'))
      .mockResolvedValueOnce(job);
    const sleep = vi.fn().mockResolvedValue(undefined);

    const result = await getJobWithRetry({ apiKey: 'k', jobId: 'job-1', fetchJob, sleep });

    expect(result).toBe(job);
    expect(fetchJob).toHaveBeenCalledTimes(3);
    expect(sleep).toHaveBeenNthCalledWith(1, 400);
    expect(sleep).toHaveBeenNthCalledWith(2, 1200);
  });

  it('rejects with the last error once the default attempt budget is exhausted', async () => {
    const errors = [new ApiError(503, 'a'), new ApiError(503, 'b'), new ApiError(503, 'c')];
    const fetchJob = vi
      .fn()
      .mockRejectedValueOnce(errors[0])
      .mockRejectedValueOnce(errors[1])
      .mockRejectedValueOnce(errors[2]);
    const sleep = vi.fn().mockResolvedValue(undefined);

    await expect(getJobWithRetry({ apiKey: 'k', jobId: 'job-1', fetchJob, sleep })).rejects.toBe(
      errors[2],
    );
    expect(fetchJob).toHaveBeenCalledTimes(3);
    expect(sleep).toHaveBeenCalledTimes(2);
  });

  it('fails fast on a permanent error without retrying', async () => {
    const err = new ApiError(401, 'x');
    const fetchJob = vi.fn().mockRejectedValue(err);
    const sleep = vi.fn().mockResolvedValue(undefined);

    await expect(getJobWithRetry({ apiKey: 'k', jobId: 'job-1', fetchJob, sleep })).rejects.toBe(
      err,
    );
    expect(fetchJob).toHaveBeenCalledTimes(1);
    expect(sleep).toHaveBeenCalledTimes(0);
  });

  it('respects attempts: 1 by never retrying even a retryable error', async () => {
    const err = new ApiError(503, 'x');
    const fetchJob = vi.fn().mockRejectedValue(err);
    const sleep = vi.fn().mockResolvedValue(undefined);

    await expect(
      getJobWithRetry({ apiKey: 'k', jobId: 'job-1', attempts: 1, fetchJob, sleep }),
    ).rejects.toBe(err);
    expect(fetchJob).toHaveBeenCalledTimes(1);
    expect(sleep).toHaveBeenCalledTimes(0);
  });
});
