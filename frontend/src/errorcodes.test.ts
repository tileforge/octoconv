import { describe, expect, it, vi } from 'vitest';
import { ApiError } from './api';
import { describeError } from './App';

// These tests drive the REAL parseResponse/ApiError/describeError pipeline,
// not a copy, proving the frontend selects behavior from the machine-readable
// `code` (docs/ERROR-CODES.md) rather than from status or message text alone
// (ERRC-03, D-08 unknown-code fallback fixture).

describe('describeError code-driven behavior', () => {
  it('maps code=unsupported_conversion (422) to a conversion-specific message', () => {
    const err = new ApiError(422, 'pair not supported', 'unsupported_conversion');
    expect(describeError(err)).toBe('pair not supported');
  });

  it('maps code=payload_too_large (413) to the size-limit message', () => {
    const err = new ApiError(413, 'too big', 'payload_too_large');
    expect(describeError(err)).toBe('The selected file exceeds the upload size limit.');
  });

  it('distinguishes code=rate_limited from code=quota_exceeded on the same 429 status', () => {
    const rateLimited = new ApiError(429, 'slow down', 'rate_limited');
    const quotaExceeded = new ApiError(429, 'quota gone', 'quota_exceeded');

    const rateLimitedMessage = describeError(rateLimited);
    const quotaExceededMessage = describeError(quotaExceeded);

    expect(rateLimitedMessage).not.toBe(quotaExceededMessage);
    expect(rateLimitedMessage).toBe('Too many requests. Please wait and try again.');
    expect(quotaExceededMessage).toBe(
      'Daily quota exceeded for this conversion type. Please try again later.',
    );
  });

  it('falls back safely on an unrecognized code (unknown-code fallback fixture, D-08)', () => {
    const err = new ApiError(422, 'something new happened', 'totally_new_code_v99');
    expect(() => describeError(err)).not.toThrow();
    const message = describeError(err);
    expect(message).toBeTruthy();
    expect(message).toBe('something new happened');
  });

  it('produces the pre-existing status-derived message when the body has no code at all', () => {
    const err = new ApiError(401, 'no auth header', undefined);
    expect(describeError(err)).toBe('API key is missing, invalid, or revoked.');
  });

  it('maps code=unsupported_operation (422) to the server message, not a status-derived fallback (OPRN-01)', () => {
    const err = new ApiError(422, 'operation "sanitize" is not supported', 'unsupported_operation');
    expect(describeError(err)).toBe('operation "sanitize" is not supported');
  });

  it('falls back to its own neutral text when unsupported_operation carries no server message', () => {
    const err = new ApiError(422, '', 'unsupported_operation');
    expect(describeError(err)).toBe('The requested operation is not supported by this server.');
  });
});

describe('ApiError.code end-to-end through parseResponse', () => {
  it('parses body.code from a 422 JSON error body via createJob-shaped call', async () => {
    const response = new Response(
      JSON.stringify({ error: 'unsupported conversion', code: 'unsupported_conversion' }),
      { status: 422, headers: { 'Content-Type': 'application/json' } },
    );

    // Exercise the same fetch+parseResponse pipeline the real API module
    // uses, via a minimal fetch stub, to prove code flows end-to-end rather
    // than being fabricated only inside this test file.
    const fetchStub = vi.fn().mockResolvedValue(response);
    vi.stubGlobal('fetch', fetchStub);

    const { getJob } = await import('./api');
    await expect(getJob({ apiKey: 'k', jobId: 'job-1' })).rejects.toMatchObject({
      code: 'unsupported_conversion',
    });

    vi.unstubAllGlobals();
  });
});
