package api

import (
	"errors"
	"net/http"
)

// CorrelationHeader is the caller's own identifier for the operation that led
// to this job.
//
// No X- prefix: RFC 6648 deprecated it, and this is an internal API we own.
//
// A HEADER and not a form field, because the other parameters (target, opts,
// preset, callback_url) describe WHAT TO CONVERT, while this describes the
// call. Mixing it into the multipart body would put metadata about the request
// inside the description of the work.
const CorrelationHeader = "Correlation-Id"

// maxCorrelationIDLen bounds the value. Room for a UUID (36), a ULID (26), or
// any sane internal key, with margin -- and far below anything that would make
// a log record unreadable or a webhook body meaningfully larger.
const maxCorrelationIDLen = 128

// ErrCorrelationIDInvalid is returned for a value this service will not carry.
var ErrCorrelationIDInvalid = errors.New("correlation id is too long or contains characters that are not allowed")

// parseCorrelationID reads and validates the header.
//
// # WHY THIS IS VALIDATED AT ALL
//
// The value ends up in two places this service is responsible for: its own log
// records, and the JSON body of a webhook POSTed to a client-controlled URL. A
// newline inside it would split a log record, and what followed the split
// would be a line the CLIENT composed -- indistinguishable, to anything
// reading the logs, from something this service emitted.
//
// # WHY REFUSED AND NOT TRUNCATED
//
// A truncated correlation id is worse than an absent one: it will not match
// anything on the caller's side, and nobody will be able to tell why. The
// caller gets 422 and can fix it; silently carrying a mangled value would make
// the feature quietly useless exactly when it is needed.
//
// # WHY AN ALLOWLIST
//
// A denylist has to enumerate every byte that could hurt -- and the ones
// nobody thought of are the ones that arrive. Printable ASCII except space is
// enough for every identifier format anyone actually uses (hex, base64url,
// UUID, ULID, dotted, slashed).
func parseCorrelationID(h http.Header) (string, error) {
	v := h.Get(CorrelationHeader)
	if v == "" {
		// Absent is not an error, and no id is generated to replace it: that
		// would be a second internal identifier alongside job_id, which is the
		// thing this design deliberately does not have.
		return "", nil
	}
	if len(v) > maxCorrelationIDLen {
		return "", ErrCorrelationIDInvalid
	}
	for i := 0; i < len(v); i++ {
		if c := v[i]; c < '!' || c > '~' {
			// Below '!' catches space, tab, CR, LF, NUL and every other
			// control byte; above '~' catches everything non-ASCII, including
			// U+2028/U+2029 once encoded.
			return "", ErrCorrelationIDInvalid
		}
	}
	return v, nil
}
