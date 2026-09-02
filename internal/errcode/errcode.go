// Package errcode defines the sync (HTTP 4xx/429) machine-readable error-code
// vocabulary this service returns in every {"error": msg, "code": ...} body.
// Async job-failure codes (jobs.error_code) are a SEPARATE, already-structured
// vocabulary that lives beside EngineDescriptor in internal/worker — this
// package intentionally does not re-declare them (D-04/D-06, 56-CONTEXT.md).
//
// InvalidOptions intentionally shares its string value with the async
// invalid_options code because the service has ONE code space (D-03) — a
// client-actionable "your opts were rejected" failure looks the same whether
// it happened synchronously at request time or asynchronously in a worker.
// TooManyPages is the second such code, added by Phase 63 for the same reason
// and pinned by the same test.
package errcode

// Sync error codes, one per class of 4xx/429 rejection (D-02). Granularity is
// by failure CLASS, not by call site — do not add a new constant per
// writeError message string; only per distinguishable client-actionable
// class. The list is closed at seventeen codes.
const (
	InvalidRequest        = "invalid_request"        // generic request/form validation: missing target, preset+target conflict, unrecognized source format, invalid callback_url, bad job id, invalid status/engine filter, invalid client_id, invalid period filter, invalid JSON body, invalid preset name
	UnsupportedConversion = "unsupported_conversion" // the (source,target) pair is not served by any engine
	// UnsupportedOperation is a DIFFERENT failure class than
	// UnsupportedConversion (57-CONTEXT.md D-03): the requested operation
	// itself has no registered triple in convert.Default AT ALL -- not one
	// pair is registered under it -- either a client typo or an engine that
	// simply is not compiled into this build. UnsupportedConversion, by
	// contrast, means the operation exists but this specific (from, to)
	// pair under it does not. Declared right after UnsupportedConversion so
	// the two read in order: "no such operation" -> "operation exists, no
	// such pair under it".
	UnsupportedOperation = "unsupported_operation"
	InvalidOptions       = "invalid_options"     // opts present but malformed/invalid for the chosen conversion — same name as the async code (D-03)
	OptsNotApplicable    = "opts_not_applicable" // opts are well-formed but not meaningful for this conversion
	MissingFile          = "missing_file"        // no file part in the multipart body
	UnknownPreset        = "unknown_preset"      // named preset does not exist / is not active
	PayloadTooLarge      = "payload_too_large"   // 413: body or opts field exceeds its cap
	NotFound             = "not_found"           // 404: job or preset does not exist for this client
	InvalidCursor        = "invalid_cursor"      // 400: opaque list cursor failed to decode
	AlreadyExists        = "already_exists"      // 409: preset name collision
	Unauthorized         = "unauthorized"        // 401: missing/malformed Authorization header, or invalid api key
	QuotaExceeded        = "quota_exceeded"      // 429: v1.12 quota gate — value already on the wire, must not change
	RateLimited          = "rate_limited"        // 429: internal/ratelimit — value already on the wire, must not change
	// InspectBusy is a THIRD, distinct 429 class (58-CONTEXT.md D-06,
	// mechanism 2): the service has exhausted its bounded concurrency budget
	// for synchronous inspect parsing right now and refuses to accept
	// another one IMMEDIATELY — it neither queues the request nor begins
	// parsing. This is a different client-actionable class than either
	// neighboring 429: RateLimited means "you exceeded your own request
	// rate"; QuotaExceeded means "you exhausted your own daily/concurrent
	// job budget"; InspectBusy means "the service as a whole is at its
	// parallel-parsing ceiling right now — your budget is not the issue at
	// all." Declared directly after RateLimited so the three 429 codes read
	// together in one place.
	InspectBusy = "inspect_busy"

	// TooManyPages is the sanitize admission ceiling (PROBE-01, 63-CONTEXT.md
	// D-04): the uploaded PDF declares more pages than PROBE_MAX_PAGES, and
	// the request is refused BEFORE any S3 byte is spent.
	//
	// Same string as the async code (D-03), exactly like InvalidOptions above
	// and for the identical reason: a client should not have to learn that
	// "this document has too many pages" is spelled one way when the service
	// noticed at admission and another way when a worker noticed. The async
	// half is worker.CodeTooManyPages, and
	// TestSyncAndAsyncShareTooManyPages pins the two equal.
	//
	// Why a code of its own rather than InvalidRequest: the client action is
	// different and specific -- split the document, or ask the operator to
	// raise the ceiling -- where InvalidRequest's action is always "the
	// message names the field you got wrong". Nothing about this request is
	// malformed.
	TooManyPages = "too_many_pages"

	InternalError = "internal_error" // 5xx: anything the client cannot act on
)

// all is the single place this vocabulary is enumerated; All() and IsKnown()
// are both built from it.
var all = []string{
	InvalidRequest,
	UnsupportedConversion,
	UnsupportedOperation,
	InvalidOptions,
	OptsNotApplicable,
	MissingFile,
	UnknownPreset,
	PayloadTooLarge,
	NotFound,
	InvalidCursor,
	AlreadyExists,
	Unauthorized,
	QuotaExceeded,
	RateLimited,
	InspectBusy,
	TooManyPages,
	InternalError,
}

// All returns every declared sync error code exactly once, in declaration
// order. The returned slice is a fresh copy on every call, so callers cannot
// mutate the package's closed vocabulary through it.
func All() []string {
	out := make([]string, len(all))
	copy(out, all)
	return out
}

// IsKnown reports whether code is one of the closed set of sync error codes
// declared in this package.
func IsKnown(code string) bool {
	for _, c := range all {
		if c == code {
			return true
		}
	}
	return false
}
