// Package api (this file): GET /v1/system/usage — the operator-facing
// combined usage + limits + remaining-budget endpoint (OPSV-01/OPSV-02,
// D-01/D-08). The route sits behind requireOperator in routes.go, so
// reaching this code already proves the caller is an authorized operator;
// handleSystemUsage does no second authorization check of its own.
//
// The `remaining` arithmetic below deliberately mirrors evaluate()
// (internal/jobs/admission.go:236-364) per D-05: concurrency remaining is
// in-flight only, daily-bytes remaining sums terminal bytes_in plus
// in-flight input bytes and NEVER touches bytes_out, and daily-jobs
// remaining sums terminal job_count plus in-flight job count. Any future
// change to evaluate()'s formula MUST be mirrored here, or this endpoint
// starts lying about what the admission gate will actually do.
//
// The arithmetic matching evaluate() is NOT the same claim as "the gate is
// actually enforcing" (plan 53-07, WR-02). remaining_matches_admission_gate
// distinguishes the two, and is true only when NONE of these three break
// the equivalence: (1) the resolved period is not periodToday — a multi-day
// window has no admission-gate analogue at all; (2) the service-wide
// QUOTA_ENFORCEMENT_DISABLED kill-switch is on — every verdict is computed
// and counted but nothing is rejected; (3) a given row's own
// enforcement_mode is "observe" — that row's own verdict is computed and
// counted but never enforced, even while (1) and (2) hold. Case (3) has no
// dedicated boolean (it is per-row, and enforcement_mode already reports
// it); it is instead named in remaining_note. quota_enforcement_disabled is
// a separate top-level field surfacing case (2) directly, since no per-row
// field can carry a service-wide fact.
package api

import (
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/clients"
	"github.com/apaderin/octoconv/internal/jobs"
)

// periodToday is the default ?period value (D-04) and the one value for
// which remaining can possibly equal what the admission gate computes right
// now (WR-02, IN-07b: collapses what used to be three separate bare-string
// literals of this same period name into one named constant).
const periodToday = "today"

// validUsagePeriods is the fail-closed allow-set an incoming ?period filter
// is validated against (D-03) — an unknown value never reaches either repo
// call, it is rejected 422 before any DB work, mirroring
// listJobsValidStatuses (jobs_list_handler.go). Arbitrary ?from=&to= ranges
// are deliberately out of scope for this phase.
var validUsagePeriods = map[string]struct{}{
	periodToday: {},
	"7d":        {},
	"30d":       {},
}

// resolveUsagePeriod turns a validated period string into an inclusive UTC
// day range [dayFrom, dayTo]. It is a package-level function (not a method)
// so it is directly unit-testable with a fixed clock. now is normalized to
// UTC FIRST — this is what keeps the returned boundaries aligned with
// client_usage_daily.day (written by Postgres as
// (now() AT TIME ZONE 'utc')::date) and with evaluate()'s own day filter;
// a local-timezone now would silently shift the window (RESEARCH Pitfall 4).
func resolveUsagePeriod(period string, now time.Time) (dayFrom, dayTo time.Time) {
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switch period {
	case "7d":
		return today.AddDate(0, 0, -6), today
	case "30d":
		return today.AddDate(0, 0, -29), today
	default: // periodToday
		return today, today
	}
}

// usedTerminal is the terminal (done/failed/canceled) portion of one
// usage row's consumption, reported for operator visibility.
type usedTerminal struct {
	JobCount int64 `json:"job_count"`
	BytesIn  int64 `json:"bytes_in"`
	BytesOut int64 `json:"bytes_out"`
}

// usedInFlight is the live (queued/active) portion of one usage row's
// consumption — an in-flight job has no outputs yet, so there is no
// bytes_out field here.
type usedInFlight struct {
	JobCount int64 `json:"job_count"`
	BytesIn  int64 `json:"bytes_in"`
}

// usageLimits mirrors clients.LimitWithMeta's three limit dimensions.
// Every field is a pointer so encoding/json emits null for "unlimited"
// rather than 0 — omitempty is never used here, because omitting the key
// would make "unlimited" indistinguishable from "field missing"
// (RESEARCH Pitfall 5).
type usageLimits struct {
	MaxConcurrent *int   `json:"max_concurrent"`
	MaxDailyBytes *int64 `json:"max_daily_bytes"`
	MaxDailyJobs  *int64 `json:"max_daily_jobs"`
}

// usageRemaining is the per-dimension remaining budget, each nil exactly
// when the matching limit is nil, and NOT clamped at zero — a signed
// negative is the informative answer for OPSV-02's "set limits from
// observed data".
type usageRemaining struct {
	Concurrent *int   `json:"concurrent"`
	DailyBytes *int64 `json:"daily_bytes"`
	DailyJobs  *int64 `json:"daily_jobs"`
}

// systemUsageRow is one (client, engine) row of the response envelope's
// `usage` array.
type systemUsageRow struct {
	ClientID        uuid.UUID      `json:"client_id"`
	ClientName      string         `json:"client_name"`
	Engine          string         `json:"engine"`
	EnforcementMode string         `json:"enforcement_mode"`
	UsedTerminal    usedTerminal   `json:"used_terminal"`
	UsedInFlight    usedInFlight   `json:"used_in_flight"`
	Limits          usageLimits    `json:"limits"`
	Remaining       usageRemaining `json:"remaining"`
}

// systemUsageResponse is the top-level envelope for GET /v1/system/usage,
// mirroring handleListJobs's {"jobs": [...]} wrapper rather than a bare
// array.
type systemUsageResponse struct {
	Period  string `json:"period"`
	DayFrom string `json:"day_from"`
	DayTo   string `json:"day_to"`
	// QuotaEnforcementDisabled reports the service-wide QUOTA_ENFORCEMENT_DISABLED
	// kill-switch (plan 53-07, WR-02) — always emitted, no omitempty, since
	// this is the ONLY way an operator can learn this state; no per-row
	// field can carry it.
	QuotaEnforcementDisabled      bool             `json:"quota_enforcement_disabled"`
	RemainingMatchesAdmissionGate bool             `json:"remaining_matches_admission_gate"`
	RemainingSemantics            string           `json:"remaining_semantics"`
	RemainingNote                 string           `json:"remaining_note"`
	Usage                         []systemUsageRow `json:"usage"`
}

// clientMeta carries a client's identity fields, shared across every
// (client, engine) row emitted for that client.
type clientMeta struct {
	name            string
	enforcementMode string
}

// handleSystemUsage serves GET /v1/system/usage (OPSV-01/OPSV-02): per-
// client, per-engine usage for the resolved period, merged with each
// client's configured limits and remaining budget, in one round trip.
func (s *Server) handleSystemUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	// 1. Resolve ?period. Empty means periodToday (D-04); anything not in
	// the allow-set is 422 BEFORE any repo call (D-03).
	period := q.Get("period")
	if period == "" {
		period = periodToday
	}
	if _, ok := validUsagePeriods[period]; !ok {
		writeError(w, http.StatusUnprocessableEntity, "invalid period filter")
		return
	}

	// 2. Resolve the optional ?client_id. Never interpolated anywhere — it
	// is passed as a bound parameter by the repo layer.
	var clientID *uuid.UUID
	if raw := q.Get("client_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid client_id")
			return
		}
		clientID = &id
	}

	// 3. Both validations above complete before either repo call, so an
	// invalid request costs zero DB work (D-03, mirroring handleListJobs) —
	// including the UTCToday read below, which sits strictly after this
	// point.

	// 4. Resolve the current day from the SAME clock evaluate() reads
	// (WR-03): Postgres's own now(), via s.usageRepo.UTCToday, never this
	// API process's own wall clock. Deriving it here in the API process
	// instead would let a UTC-midnight crossing or NTP skew make this
	// endpoint query one day while the admission gate enforces another —
	// while the response still claimed remaining_matches_admission_gate:
	// true.
	today, err := s.usageRepo.UTCToday(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve current day")
		return
	}
	dayFrom, dayTo := resolveUsagePeriod(period, today)

	usageRows, err := s.usageRepo.UsageSummary(ctx, clientID, dayFrom, dayTo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load usage")
		return
	}

	limitRows, err := s.limitsRepo.ListAllLimits(ctx, clientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load limits")
		return
	}

	// 7. Merge. Per-client metadata (name, enforcement mode) from
	// ListAllLimits — every row for a client carries the same values.
	meta := make(map[uuid.UUID]clientMeta, len(limitRows))
	// limitsByKey holds only rows whose Engine is non-nil (a real
	// client_limits row); a nil-Engine row is metadata-only.
	limitsByKey := make(map[usageMergeKey]clients.LimitWithMeta)
	for _, lm := range limitRows {
		meta[lm.ClientID] = clientMeta{name: lm.ClientName, enforcementMode: lm.EnforcementMode}
		if lm.Engine != nil {
			limitsByKey[usageMergeKey{clientID: lm.ClientID, engine: *lm.Engine}] = lm
		}
	}

	usageByKey := make(map[usageMergeKey]jobs.UsageRow, len(usageRows))
	for _, ur := range usageRows {
		usageByKey[usageMergeKey{clientID: ur.ClientID, engine: ur.Engine}] = ur
	}

	// The emitted row key set is a THREE-way union (ROADMAP SC2 / OPSV-02,
	// 53-VERIFICATION.md Gap #1 / 53-REVIEW.md CR-01):
	//   1. usageByKey — clients with usage in the period but no configured
	//      limit appear with all-null limits.
	//   2. limitsByKey (non-nil-Engine only) — clients with a configured
	//      limit but zero usage appear with zero usage and remaining equal
	//      to the full limit.
	//   3. every remaining ClientID in meta — clients.LimitWithMeta rows
	//      with a nil Engine are the documented signal for "this client has
	//      no client_limits row at all" and MUST NOT be discarded by the
	//      caller (internal/clients/limits.go:116-119). Before this third
	//      term, such a client contributed to neither map above, so a
	//      client with no limits row, no usage in the period and no
	//      in-flight jobs emitted ZERO rows — making a real, idle,
	//      unlimited client byte-identical to a client_id matching no
	//      client at all, which is exactly the question OPSV-02 poses.
	keySet := make(map[usageMergeKey]struct{}, len(usageByKey)+len(limitsByKey))
	for k := range usageByKey {
		keySet[k] = struct{}{}
	}
	for k := range limitsByKey {
		keySet[k] = struct{}{}
	}
	clientsWithRows := make(map[uuid.UUID]struct{}, len(keySet))
	for k := range keySet {
		clientsWithRows[k.clientID] = struct{}{}
	}
	for id := range meta {
		if _, ok := clientsWithRows[id]; !ok {
			keySet[usageMergeKey{clientID: id, engine: usageEngineNone}] = struct{}{}
		}
	}

	rows := make([]systemUsageRow, 0, len(keySet))
	for k := range keySet {
		ur := usageByKey[k] // zero value if absent: all used_* fields zero
		lm, hasLimit := limitsByKey[k]

		// A usage row's client should always have a metadata entry (every
		// client_usage_daily row's client_id has a FK to clients, ON DELETE
		// CASCADE) — this lookup is defensive only. If it is somehow absent,
		// still emit the row with empty name/enforcement_mode rather than
		// dropping it, because dropping would hide real usage from the
		// operator.
		cm := meta[k.clientID]

		row := systemUsageRow{
			ClientID:        k.clientID,
			ClientName:      cm.name,
			Engine:          k.engine,
			EnforcementMode: cm.enforcementMode,
			UsedTerminal: usedTerminal{
				JobCount: ur.TerminalJobCount,
				BytesIn:  ur.TerminalBytesIn,
				BytesOut: ur.TerminalBytesOut,
			},
			UsedInFlight: usedInFlight{
				JobCount: ur.InFlightJobCount,
				BytesIn:  ur.InFlightBytesIn,
			},
		}

		if hasLimit {
			row.Limits = usageLimits{
				MaxConcurrent: lm.MaxConcurrent,
				MaxDailyBytes: lm.MaxDailyBytes,
				MaxDailyJobs:  lm.MaxDailyJobs,
			}
			// 8. Compute remaining per dimension, each nil exactly when its
			// limit is nil, never clamped at zero.
			if lm.MaxConcurrent != nil {
				// concurrent: in-flight ONLY — evaluate()'s concurrency check
				// counts queued+active jobs and never consults the rollup.
				v := *lm.MaxConcurrent - int(ur.InFlightJobCount)
				row.Remaining.Concurrent = &v
			}
			if lm.MaxDailyBytes != nil {
				// daily_bytes: TerminalBytesOut is deliberately NOT part of
				// this sum — evaluate() compares max_daily_bytes against
				// sum(bytes_in) plus in-flight input sizes only, never
				// against bytes_out.
				v := *lm.MaxDailyBytes - (ur.TerminalBytesIn + ur.InFlightBytesIn)
				row.Remaining.DailyBytes = &v
			}
			if lm.MaxDailyJobs != nil {
				v := *lm.MaxDailyJobs - (ur.TerminalJobCount + ur.InFlightJobCount)
				row.Remaining.DailyJobs = &v
			}
		}
		// hasLimit == false: row.Limits and row.Remaining stay at their zero
		// values, i.e. every field nil.

		rows = append(rows, row)
	}

	// 9. Sort by consumption descending (D-02): total = TerminalBytesIn +
	// TerminalBytesOut summed across ALL of that client's engines — bytes
	// rather than job count, because job count treats a 1 KB job and a 2 GB
	// video identically. Ties break by ClientID string ascending for
	// determinism. Within a client, engine ascending so a client's engines
	// stay contiguous and stable.
	// A synthetic (usageEngineNone) row contributes 0 to its client's
	// consumption total, so such clients sort to the bottom via the
	// existing ClientID-string tiebreak below — already a total order,
	// already deterministic, no special-casing needed.
	consumption := make(map[uuid.UUID]int64, len(meta))
	for _, row := range rows {
		consumption[row.ClientID] += row.UsedTerminal.BytesIn + row.UsedTerminal.BytesOut
	}
	sortSystemUsageRows(rows, consumption)

	// 10. Build the envelope. Locked precedence order (53-07-PLAN.md
	// <design_resolution>, WR-02): multi-day period wins over the
	// kill-switch (a multi-day figure has no admission-gate analogue
	// regardless of whether enforcement is live), then the service-wide
	// kill-switch, then the live case.
	resp := systemUsageResponse{
		Period:                        period,
		DayFrom:                       dayFrom.Format("2006-01-02"),
		DayTo:                         dayTo.Format("2006-01-02"),
		QuotaEnforcementDisabled:      s.quotaEnforcementDisabled,
		RemainingMatchesAdmissionGate: period == periodToday && !s.quotaEnforcementDisabled,
		Usage:                         rows,
	}
	switch {
	case period != periodToday:
		resp.RemainingSemantics = "hypothetical_single_day_equivalent"
		resp.RemainingNote = "max_daily_bytes/max_daily_jobs are DAILY caps that reset at UTC midnight. For a multi-day period, remaining compares a multi-day sum against a one-day cap — it has no admission-gate analogue and will legitimately go deeply negative for healthy clients."
	case s.quotaEnforcementDisabled:
		resp.RemainingSemantics = "computed_but_not_enforced"
		resp.RemainingNote = "period=today: the arithmetic is exactly what the admission gate (evaluate()) computes, but QUOTA_ENFORCEMENT_DISABLED is set service-wide right now — no request is being rejected. Verdicts are still computed and counted, never enforced, until the kill-switch is turned off."
	default:
		resp.RemainingSemantics = "live_admission_gate"
		resp.RemainingNote = "period=today: remaining is exactly what the admission gate (evaluate()) computes right now. A row whose own enforcement_mode is \"observe\" has its verdict computed and counted here too, but that row is never actually enforced — a negative remaining on such a row does not mean requests are being rejected for it."
	}

	// 11. writeJSON never logs (internal/* never logs).
	writeJSON(w, http.StatusOK, resp)
}

// usageMergeKey identifies one (client, engine) group while merging
// UsageSummary and ListAllLimits results.
type usageMergeKey struct {
	clientID uuid.UUID
	engine   string
}

// usageEngineNone is the engine label carried by a synthetic "exists,
// unlimited, idle" row: a client that produced neither a usage key nor a
// configured-limit key (ROADMAP SC2 / OPSV-02, 53-VERIFICATION.md Gap #1 /
// 53-REVIEW.md CR-01). The empty string is deliberate, not a placeholder —
// engine stays a required JSON string in the OpenAPI contract
// (docs/openapi.yaml), so this must decode identically to a real engine
// label, never as null or an omitted key.
const usageEngineNone = ""

// sortSystemUsageRows orders rows by each client's total consumption
// descending (ties broken by ClientID string ascending for determinism),
// and within a client by engine ascending. The comparator is a total order
// (client total, then ClientID string, then Engine), so sort.Slice's
// non-stable guarantee never matters here.
func sortSystemUsageRows(rows []systemUsageRow, consumption map[uuid.UUID]int64) {
	sort.Slice(rows, func(i, j int) bool {
		ci, cj := consumption[rows[i].ClientID], consumption[rows[j].ClientID]
		if ci != cj {
			return ci > cj
		}
		si, sj := rows[i].ClientID.String(), rows[j].ClientID.String()
		if si != sj {
			return si < sj
		}
		return rows[i].Engine < rows[j].Engine
	})
}
