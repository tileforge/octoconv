// This file backs INSP-04: for a magic-byte-detected format, enumerate
// every registered (operation, target) triple available for it, each
// carrying the engine that would serve it, an honest UPPER-BOUND duration
// estimate with its own provenance (measured vs unmeasured), and a live,
// read-only quota verdict.
//
// Why an upper bound, not a real estimate (D-10): this project has no
// historical duration data to derive a real estimate from.
// octoconv_job_duration_seconds (internal/metrics/metrics.go) is a
// Prometheus histogram that is only WRITTEN by a worker on job completion --
// nothing in this codebase ever READS it back. client_usage_daily
// (internal/jobs, QUOT-02) is a byte/job-count ledger with no per-attempt
// duration column at all. Absent that data, the honest answer is the upper
// bound this service already enforces on itself -- the per-engine
// ENGINE_TIMEOUT family already read at worker startup -- not an invented
// number. This is the same discipline PROG-V2-02 records elsewhere in this
// project ("null is an honest answer, a guess is not"), applied here to a
// ceiling instead of a null.
package api

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/errcode"
	"github.com/apaderin/octoconv/internal/jobs"
)

// EstimateSource is a closed, two-value classification of WHERE an engine's
// upper-bound duration came from -- D-11. A client that only ever sees the
// number without this flag cannot tell "we measured this" from "we copied a
// starting point and have not measured it yet"; making the flag itself part
// of the wire contract is what keeps that distinction from silently
// disappearing the way progress_pct/eta_seconds nearly did (Phase 57 PR #7
// review).
type EstimateSource string

const (
	// EstimateSourceMeasured: the underlying per-engine timeout was derived
	// from a real RTF (real-time-factor) measurement run against the actual
	// worker image under its deployed cgroup limits -- see
	// engineEstimateSource's own per-engine comment for the specific script
	// and .env.example line each measurement lives at.
	EstimateSourceMeasured EstimateSource = "measured"
	// EstimateSourceUnmeasured: the underlying timeout is a starting point
	// -- either an inherited placeholder or an expectation ("expected to be
	// faster than X") -- with no RTF measurement behind it yet. Reporting
	// this honestly is a deliberate side benefit: it makes the calibration
	// debt for that engine class VISIBLE on the wire instead of letting it
	// hide behind a number that looks exactly as authoritative as a
	// measured one. Copying a neighboring engine's measured number to make
	// an unmeasured one LOOK measured is this project's own documented
	// anti-pattern (see .env.example's OCR_ENGINE_TIMEOUT/PANDOC_ENGINE_TIMEOUT
	// comments on why a value is never copied across engine classes) --
	// this flag exists so an unmeasured engine is never allowed to
	// masquerade as a measured one instead.
	EstimateSourceUnmeasured EstimateSource = "unmeasured"
)

// engineEstimateSource is the closed, per-engine provenance table this
// file's entire honesty claim rests on. Every entry below was
// mechanically re-verified against .env.example's OWN inline/block
// comments immediately before being written here (not assumed from this
// plan's own prose, which pre-dated one of these measurements) --
// TestEstimateSourceMatchesEnvExampleProvenance re-runs that same
// verification on every test run so a future .env.example edit that
// changes an engine's provenance cannot silently drift from this map.
//
//   - image (ENGINE_TIMEOUT=120s): no measurement comment in .env.example at
//     all -- unmeasured.
//   - document (DOCUMENT_ENGINE_TIMEOUT=300s): "Phase 9 D-01", no RTF
//     measurement -- unmeasured.
//   - html (HTML_ENGINE_TIMEOUT=60s): "expected to be faster than a
//     LibreOffice conversion" -- an EXPECTATION, not a measurement --
//     unmeasured.
//   - audio (AUDIO_ENGINE_TIMEOUT=742s): "Phase 32 AUD-07: RTF-measured
//     (scripts/audio-rtf-measure.sh, p95 RTF=0.205853, N=10, ...)" --
//     measured.
//   - av (AV_ENGINE_TIMEOUT=753s): "RTF-FINALIZED by Phase 36 Plan 04's
//     supervised scripts/av-rtf-measure.sh matrix run ... p95 RTF worst
//     BOUNDED cell = hevc@1080 = 4.179133s" -- measured.
//   - ocr (OCR_ENGINE_TIMEOUT=744s): "MEASURED (Phase 45 Plan 03's
//     supervised scripts/ocr-timing-measure.sh run, ... p95 sec/page)" --
//     measured.
//   - pandoc (PANDOC_ENGINE_TIMEOUT=65s): "PANDOC_ENGINE_TIMEOUT below is
//     now MEASURED (Phase 49 Plan 03's supervised
//     scripts/pandoc-timing-measure.sh run, ... p95=32.318s)" -- measured.
//
// Deviation from this plan's own action text, recorded per D-16's own
// instruction ("если комментарий противоречит этому списку -- верен
// комментарий, а не план"): 58-07-PLAN.md's action text describes pandoc's
// timeout as "90с... unmeasured fallback", carried over from an EARLIER
// state of .env.example. Phase 49 Plan 03 has since RTF-measured pandoc and
// lowered the value to 65s -- .env.example (the actual source of truth) now
// documents pandoc as measured, exactly like audio/av/ocr. This map follows
// .env.example, not the plan's stale prose.
var engineEstimateSource = map[string]EstimateSource{
	convert.EngineImage:    EstimateSourceUnmeasured,
	convert.EngineDocument: EstimateSourceUnmeasured,
	convert.EngineHTML:     EstimateSourceUnmeasured,
	convert.EngineAudio:    EstimateSourceMeasured,
	convert.EngineAV:       EstimateSourceMeasured,
	convert.EngineOCR:      EstimateSourceMeasured,
	convert.EnginePandoc:   EstimateSourceMeasured,
	// probe (PROBE_ENGINE_TIMEOUT=180s): a PLACEHOLDER pending Phase 60's
	// operator-run scripts/probe-timeout-measure.sh on a representative
	// corpus -- unmeasured, and it must keep saying so until that run
	// happens. This is the same provisional-then-measured path OCR walked
	// (300s -> 744s) and pandoc after it (90s -> 65s); the entry flips to
	// measured in the same commit as the number, never before.
	convert.EngineProbe: EstimateSourceUnmeasured,
}

// engineMeasuredDefaultSeconds records the EXACT upper-bound value each
// measured engine's provenance in engineEstimateSource was measured AT --
// the .env.example figure named in that map's own per-engine comment above
// (Phase 32/36/45/49's supervised RTF-measurement runs). This is D-11's own
// 2026-08-10 correction (58-CONTEXT.md), closing a PR #9 finding
// (estimate.go:241, dated): engineEstimateSource answers "does THIS ENGINE
// have a documented measurement run at all", a property of the engine's
// HISTORY that does not change from one request to the next; it says
// nothing about whether the number THIS request is about to report is the
// exact value that run measured. An operator who overrides
// Config.EngineTimeouts for a measured engine (api.go) produces a
// DIFFERENT number than the one the documented run measured -- reporting
// "measured" for that arbitrary replacement would be exactly the
// mislabeling the finding calls out. estimateSourceFor below is the one
// place that closes it.
var engineMeasuredDefaultSeconds = map[string]int{
	convert.EngineAudio:  742, // AUDIO_ENGINE_TIMEOUT=742s (.env.example): Phase 32 AUD-07, RTF-measured
	convert.EngineAV:     753, // AV_ENGINE_TIMEOUT=753s (.env.example): Phase 36 Plan 04, RTF-measured
	convert.EngineOCR:    744, // OCR_ENGINE_TIMEOUT=744s (.env.example): Phase 45 Plan 03, measured
	convert.EnginePandoc: 65,  // PANDOC_ENGINE_TIMEOUT=65s (.env.example): Phase 49 Plan 03, measured
}

// estimateSourceFor is the provenance THIS SPECIFIC offer carries -- not
// "does this engine have a measured history in the abstract"
// (engineEstimateSource answers that alone), but "does actualSeconds --
// the number THIS request is about to report -- equal the exact figure the
// documented run measured". An engine with no measured provenance at all
// (image/document/html) is always unmeasured regardless of its configured
// value: there is no "measured" claim on the table for those to lose. An
// engine WITH measured provenance whose runtime-configurable
// Config.EngineTimeouts value diverges from engineMeasuredDefaultSeconds is
// downgraded to unmeasured HERE -- chosen side (б) of the plan's own
// two-option choice (conservative relabel over threading provenance
// through Config as a new field): a mislabeled operator override is
// exactly the failure mode this closes, and erring toward "unmeasured" is
// the safe direction to be wrong in, mirroring PROG-V2-02's "an honest
// unknown beats a confident guess" discipline elsewhere in this project.
func estimateSourceFor(engine string, actualSeconds int) EstimateSource {
	base := engineEstimateSource[engine]
	if base != EstimateSourceMeasured {
		return base
	}
	if want, ok := engineMeasuredDefaultSeconds[engine]; ok && want == actualSeconds {
		return EstimateSourceMeasured
	}
	return EstimateSourceUnmeasured
}

// operationOffer is one row of the "operations" section POST /v1/inspect's
// response carries (INSP-04) -- one (operation, target) triple available
// for the detected format, the engine that serves it, its upper-bound
// duration with provenance, and a live quota verdict.
type operationOffer struct {
	Operation           string         `json:"operation"`
	Target              string         `json:"target"`
	Engine              string         `json:"engine"`
	EstimatedMaxSeconds int            `json:"estimated_max_seconds"`
	EstimateSource      EstimateSource `json:"estimate_source"`
	Quota               quotaOffer     `json:"quota"`
	// Available is the DETERMINISTIC half of "could I actually submit this",
	// deliberately separate from Quota: it is computed locally from this
	// file's size against the engine's own post-detection ceiling
	// (s.maxEngineBytes, D-07), so unlike a quota verdict it is never
	// "as of this request" and never unknown -- which is exactly why it is a
	// plain bool while quotaOffer.Allowed is a pointer.
	//
	// PR9R22 (estimate.go:265), verbatim: "When an inspected file fits
	// INSPECT_MAX_UPLOAD_BYTES but exceeds its selected engine's
	// s.maxEngineBytes limit, this unconditionally adds the conversion to
	// operations and can report quota.allowed: true, although handleCreateJob
	// deterministically rejects the same file at its engine-size gate."
	//
	// available:false does NOT fold in the quota verdict: a client must be
	// able to tell "this service will refuse this file at any quota" from
	// "your quota is exhausted right now". Both can appear on one offer.
	Available bool `json:"available"`
	// UnavailableReason is empty when Available is true, and otherwise carries
	// the SAME machine-readable code the POST /v1/jobs rejection would carry
	// (errcode.PayloadTooLarge) -- so an agent reading this offer and an agent
	// reading the eventual 413 are looking at one vocabulary, not two.
	UnavailableReason string `json:"unavailable_reason,omitempty"`
	// MaxBytes is the engine's own post-detection ceiling when it has one,
	// present whether or not this file fits it: a client that knows the limit
	// can split or downscale its input instead of guessing. Absent for an
	// engine s.maxEngineBytes does not gate (the map gates known engines, it
	// does not allowlist).
	MaxBytes int64 `json:"max_bytes,omitempty"`
}

// quotaOffer is the live, read-only quota verdict for one engine, "as of
// this request" (D-12) -- never a promise the eventual POST /v1/jobs will
// succeed. Allowed is a pointer so a non-quota PreAdmit error can encode
// "we do not know" as JSON null, distinct from both true and false --
// незнание не разрешает: an unknown verdict is NEVER reported as allowed.
type quotaOffer struct {
	Allowed *bool  `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

// quotaOfferFromPreAdmitError classifies one Repo.PreAdmit call's result
// into the three-way quotaOffer shape: nil error -> allowed; a recognized
// quota error (jobs.QuotaReason returns non-empty) -> not allowed, with the
// machine-readable reason; any OTHER error -> unknown, reason "unknown",
// Allowed left nil. The third case is the one this function exists to get
// right: a non-quota PreAdmit failure means the pre-admission check itself
// could not run, not that the client is clear to proceed -- незнание не
// разрешает (unknowing is never permission), so allowed is NEVER true here.
func quotaOfferFromPreAdmitError(err error) quotaOffer {
	if err == nil {
		allowed := true
		return quotaOffer{Allowed: &allowed}
	}
	if reason := jobs.QuotaReason(err); reason != "" {
		allowed := false
		return quotaOffer{Allowed: &allowed, Reason: reason}
	}
	return quotaOffer{Allowed: nil, Reason: "unknown"}
}

// engineTimeoutSeconds converts d to a whole-second upper bound, rounding
// UP (never down -- a rounded-DOWN ceiling could understate the real
// timeout by up to a second, which is the wrong direction for an upper
// bound to be wrong in). d <= 0 (an engine absent from
// Server.engineTimeouts) reports 0 rather than a negative or nonsensical
// value.
func engineTimeoutSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(math.Ceil(d.Seconds()))
}

// availableOperations enumerates every registered (operation, target)
// triple whose source format equals detected, each with its engine's
// upper-bound duration/provenance and a live quota verdict. It ALWAYS
// returns a non-nil, possibly-empty slice (a format with zero registered
// triples reports operations: [], never null -- json.Marshal on a nil
// []operationOffer would otherwise emit null, which a client's "loop over
// operations" code could choke on where an empty array would not).
//
// PreAdmit is called EXACTLY ONCE per DISTINCT engine in the matched triple
// set, never once per (operation, target) pair -- this is a PROPERTY this
// function guarantees, not an incidental optimization. Without this
// deduplication, one inspect call against a format with a dozen registered
// pairs under one engine (e.g. every image<->image libvips pair) would make
// a dozen round trips to the shared *pgxpool.Pool per request; the held
// inspectSemaphore slot (internal/api/inspectgate.go, D-06a) already bounds
// how many CONCURRENT inspect requests may be doing this, but it does
// nothing about how much POOL work a SINGLE inspect request generates --
// that is this function's own job to bound, and it is exactly the T-58-40
// mitigation the plan's threat register names.
//
// PreAdmit's own two documented caveats (internal/jobs/admission.go) apply
// unchanged here and are recorded once, at this call site, rather than only
// in planning docs: (a) it is not линеаризуем (not linearizable) under a
// concurrent burst -- several simultaneous inspect calls for the same
// client can each read the same below-cap count before any of them would
// have moved it, so this verdict is honestly "as of this request", never a
// guarantee the eventual POST /v1/jobs will succeed; (b) a rejection while
// enforcement is live fires the same quota-would-reject обсервер (observer)
// metric callback admitLocked's own rejection path fires -- calling this
// function from POST /v1/inspect is therefore not perfectly silent
// telemetry-wise, and that is expected, not a regression to "fix".
func (s *Server) availableOperations(ctx context.Context, clientID uuid.UUID, detected string, sizeBytes int64) []operationOffer {
	offers := []operationOffer{}
	if detected == "" {
		return offers
	}

	type pendingOffer struct {
		operation, target, engine string
	}
	var pending []pendingOffer
	engines := make(map[string]bool)

	for operation, byEngine := range convert.Default.OperationClasses() {
		for engine, pairs := range byEngine {
			for _, p := range pairs {
				if p.From != detected {
					continue
				}
				pending = append(pending, pendingOffer{operation: operation, target: p.To, engine: engine})
				engines[engine] = true
			}
		}
	}
	if len(pending) == 0 {
		return offers
	}

	// The dedup this function's own doc comment above promises: one
	// PreAdmit call per DISTINCT engine, regardless of how many
	// (operation, target) pairs share it.
	quotaByEngine := make(map[string]quotaOffer, len(engines))
	for engine := range engines {
		err := s.repo.PreAdmit(ctx, clientID, engine, sizeBytes)
		quotaByEngine[engine] = quotaOfferFromPreAdmitError(err)
	}

	for _, p := range pending {
		secs := engineTimeoutSeconds(s.engineTimeouts[p.engine])
		// The engine-size gate, read from the SAME map handleCreateJob's own
		// D-07 check reads (api.go's maxEngineBytes, handlers.go around line
		// 470) rather than from a second copy of the rule -- one declaration,
		// two readers, the same discipline D-03 applies to the finding
		// vocabulary. The comparison is `>` here because it is `>` there:
		// a file exactly at the limit is accepted by both.
		limit, gated := s.maxEngineBytes[p.engine]
		available := true
		unavailableReason := ""
		if gated && sizeBytes > limit {
			available = false
			unavailableReason = errcode.PayloadTooLarge
		}
		maxBytes := int64(0)
		if gated {
			maxBytes = limit
		}
		offers = append(offers, operationOffer{
			Operation:           p.operation,
			Target:              p.target,
			Engine:              p.engine,
			Available:           available,
			UnavailableReason:   unavailableReason,
			MaxBytes:            maxBytes,
			EstimatedMaxSeconds: secs,
			// estimateSourceFor (not the fixed engineEstimateSource table
			// directly) is D-11's own 2026-08-10 correction: it downgrades
			// to unmeasured whenever secs diverges from the exact value
			// the documented run measured, closing PR #9's finding that an
			// operator-overridden timeout could otherwise masquerade as
			// measured.
			EstimateSource: estimateSourceFor(p.engine, secs),
			Quota:          quotaByEngine[p.engine],
		})
	}
	sort.Slice(offers, func(i, j int) bool {
		if offers[i].Operation != offers[j].Operation {
			return offers[i].Operation < offers[j].Operation
		}
		if offers[i].Target != offers[j].Target {
			return offers[i].Target < offers[j].Target
		}
		return offers[i].Engine < offers[j].Engine
	})
	return offers
}
