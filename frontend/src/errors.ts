import { ApiError } from './api';

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

