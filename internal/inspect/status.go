package inspect

import "fmt"

// CheckStatus is a closed, three-value classification of the outcome of one
// named check against one Code. Collapsing "unknown" and "not_applicable"
// in a client-side check of the shape `if status != "clean"` is exactly the
// defect INSP-03 exists to prevent -- and it is introduced by the RESPONSE
// SHAPE, not by any parser (Pitfall 4, 58-RESEARCH.md). The three values
// below are declared as separate literals for exactly that reason, and
// docs/FINDINGS.md obliges every consumer to carry three branches, not two.
type CheckStatus string

const (
	// StatusClean: the check applies to this format, ran to completion, and
	// found nothing.
	StatusClean = "clean"
	// StatusNotApplicable: the check does not apply to this format at all
	// (e.g. PDF-specific checks against a PNG upload). This is NOT "we
	// looked and found nothing" -- it is "there was nothing to look for".
	StatusNotApplicable = "not_applicable"
	// StatusUnknown: the check applies, but its result could not be
	// determined. This is NEITHER a synonym for clean NOR for
	// not_applicable.
	StatusUnknown = "unknown"
)

// AllStatuses returns the three CheckStatus values, in declaration order.
func AllStatuses() []CheckStatus {
	return []CheckStatus{StatusClean, StatusNotApplicable, StatusUnknown}
}

// Reason is a closed vocabulary explaining WHY a Check's status is not
// clean. Only ReasonFormatNotApplicable is a legitimate reason for
// StatusNotApplicable; the rest explain a StatusUnknown result.
//
// The line that matters most in this type (D-02a/INSP-03): the first seven
// reasons below are properties of the FILE or of this BUILD -- they
// produce an unknown status on one check, but never make the report itself
// partial. The last two are properties of THIS SERVICE'S OWN LOAD -- and
// ONLY those two may ever mark a Coverage incomplete (see MarkTruncated).
// This is the actual DoS-relevant distinction Phase 58 exists to make
// honest, and it cannot be folded into a single field without losing it.
type Reason string

const (
	// ReasonFormatNotApplicable is the only legitimate reason paired with
	// StatusNotApplicable.
	ReasonFormatNotApplicable = "format_not_applicable"
	// ReasonFormatUnrecognized: detection never claimed a format at all
	// (Report.Format is empty -- nothing in the sniffer chain recognized the
	// content). Deliberately DIFFERENT from ReasonFormatNotApplicable:
	// "not_applicable" is the honest claim "there was nothing HERE to look
	// for" and requires knowing what format IS being inspected; when the
	// format itself is unknown, applicability cannot be established either
	// way, so every code in the closed vocabulary reports unknown with this
	// reason instead (codex PR9 finding 6 / T-SLE-06).
	ReasonFormatUnrecognized = "format_unrecognized"
	// ReasonCompressedObjectStreams: structural blindness -- PDF 1.5+ hides
	// objects inside /ObjStm, which a byte scan cannot see (D-02a). The
	// primary trigger for StatusUnknown in this phase.
	ReasonCompressedObjectStreams = "compressed_object_streams"
	// ReasonPDFNameEscape: a PDF name used a hexadecimal escape ("#XX")
	// this scanner's literal-byte token tables cannot recognize (codex PR9
	// finding 1 / T-SLE-01). Deliberately a DIFFERENT reason from
	// ReasonCompressedObjectStreams: it is a different blind spot (an
	// escape this build chooses not to decode, not an object stream this
	// build cannot see into at all) -- collapsing the two would make it
	// impossible for a future reader to tell which one actually fired.
	ReasonPDFNameEscape = "pdf_name_escape"
	// ReasonPDFStreamBodyToken: a risk-token literal was found ONLY inside
	// a PDF stream body or comment, never in ordinary dictionary syntax
	// (codex PR9 finding 2 / T-SLE-02). Lexical filtering correctly refuses
	// to promote that occurrence to a Finding (it cannot verify the bytes
	// are really a PDF name rather than coincidental opaque payload
	// content), but the same filtering must never fall back to StatusClean
	// either -- this reason is the honest middle ground.
	ReasonPDFStreamBodyToken = "pdf_stream_body_token"
	// ReasonParserUnavailable: no parser has been written for this
	// characteristic/finding in this build (D-01: an honest answer, not a
	// failure).
	ReasonParserUnavailable = "parser_unavailable"
	// ReasonMalformedInput: structure is present but did not parse
	// (truncated/corrupted file).
	ReasonMalformedInput = "malformed_input"
	// ReasonWorkCeilingBytes: parsing stopped because THIS SERVICE'S OWN
	// byte ceiling was hit.
	ReasonWorkCeilingBytes = "work_ceiling_bytes"
	// ReasonWorkCeilingTime: parsing stopped because THIS SERVICE'S OWN
	// time ceiling was hit.
	ReasonWorkCeilingTime = "work_ceiling_time"
)

// Check is the three-value outcome of one named check against one Code.
type Check struct {
	Code   Code        `json:"code"`
	Status CheckStatus `json:"status"`
	Reason Reason      `json:"reason,omitempty"`
}

// Coverage is a SEPARATE claim from any individual Check's status: it says
// whether THIS REPORT, as a whole, is complete. A structurally unknown
// check (parser blind, format inapplicable, malformed input) does not, by
// itself, make a report incomplete -- the checker did everything it could
// and honestly reported what it couldn't determine. Only exhausting the
// service's OWN work ceiling (bytes or time) makes the report selective,
// because that is the one case where MORE would have been found had the
// service kept going. Folding these two different claims into one status
// value is exactly the schema-level regression Pitfall 4 warns about.
type Coverage struct {
	Complete        bool   `json:"complete"`
	Reason          Reason `json:"reason,omitempty"`
	TruncatedChecks []Code `json:"truncated_checks,omitempty"`
}

// MarkTruncated records that code's check was cut short by r. Only the two
// ceiling reasons (ReasonWorkCeilingBytes, ReasonWorkCeilingTime) are
// accepted -- any other Reason is a fail-safe no-op that leaves Coverage
// unchanged. This project forbids panic for control flow in internal/
// (CLAUDE.md); the correct fail-safe here is silently refusing an invalid
// call, never letting a structural/format reason accidentally flip
// Complete to false.
//
// The FIRST call's reason is kept (later calls do not overwrite it): it
// names where the budget actually ran out, and that is the fact worth
// keeping when several checks are truncated by the same exhausted budget.
func (c *Coverage) MarkTruncated(r Reason, code Code) {
	if r != ReasonWorkCeilingBytes && r != ReasonWorkCeilingTime {
		return
	}
	c.Complete = false
	if c.Reason == "" {
		c.Reason = r
	}
	for _, tc := range c.TruncatedChecks {
		if tc == code {
			return
		}
	}
	c.TruncatedChecks = append(c.TruncatedChecks, code)
}

// MarkTruncatedByCharacteristic records that some CHARACTERISTIC (not a
// Check) was cut short by r -- report.go's buildCoverage calls this when an
// analyzer's own "<name>_reason" characteristic key carries one of the two
// service-ceiling reasons even though no accompanying Check exists for it
// (codex PR9 finding 5 / T-SLE-05: e.g. media.go can report
// duration_seconds_reason=work_ceiling_bytes with zero Checks at all, and a
// Coverage that only ever inspected checks would stay Complete:true while
// admitting a truncation in the very same response body). Same
// ceiling-reason gate and first-reason-wins rule as MarkTruncated;
// TruncatedChecks is deliberately left untouched here, because it is a
// Code-keyed list and a bare characteristic key does not name one.
func (c *Coverage) MarkTruncatedByCharacteristic(r Reason) {
	if r != ReasonWorkCeilingBytes && r != ReasonWorkCeilingTime {
		return
	}
	c.Complete = false
	if c.Reason == "" {
		c.Reason = r
	}
}

// ValidateReport is the single place the report's UNION invariant is
// recorded: for every code AllCodes() declares, exactly ONE of two things
// must be true -- it appears at least once in findings, or it appears
// exactly once in checks carrying a three-value status. Never both, never
// neither.
//
// Why this invariant exists: it is the mechanical form of "every DECLARED
// check carries a three-value status" -- every code in the closed
// vocabulary counts as declared. A positive finding is not a statement
// about absence, so it lives in findings, not in the three-value scale;
// but every code that did NOT produce a finding must be accounted for by
// exactly one Check, or the report is silently incomplete about that code
// without saying so.
func ValidateReport(findings []Finding, checks []Check) error {
	findingCodes := make(map[Code]bool, len(findings))
	for _, f := range findings {
		findingCodes[f.Code] = true
	}
	checkCounts := make(map[Code]int, len(checks))
	for _, c := range checks {
		checkCounts[c.Code]++
	}

	var both, missing, duplicated []Code
	for _, code := range AllCodes() {
		inFindings := findingCodes[code]
		count := checkCounts[code]
		switch {
		case inFindings && count > 0:
			both = append(both, code)
		case !inFindings && count == 0:
			missing = append(missing, code)
		case count > 1:
			duplicated = append(duplicated, code)
		}
	}

	if len(both) > 0 || len(missing) > 0 || len(duplicated) > 0 {
		return fmt.Errorf("report invariant violated: in both findings and checks: %v; missing from both: %v; duplicated in checks: %v", both, missing, duplicated)
	}
	return nil
}

// Characteristics is a per-format-applicable-only bag of extracted facts
// about a file (page count, duration, pixel dimensions, ...). Only keys
// APPLICABLE to the detected format are ever present (INSP-01). A key
// whose VALUE is unknown is present with a nil value (serializes to JSON
// null -- an honest "we don't know", the same PROG-V2-02 discipline this
// project already applies elsewhere); a key that does not apply to this
// format is simply absent. This distinction -- present-but-null versus
// absent -- cannot be expressed by a flat struct with zero values in every
// inapplicable field, which is exactly what SC1 rules out.
//
// Declared as a type ALIAS (not a new named type) so map literals assemble
// without a wrapper conversion at every call site.
type Characteristics = map[string]any

// FamilyResult is the single return shape EVERY family analyzer (PDF,
// containers, media, images) must produce -- declared here, not inside any
// one analyzer's own plan, because plans 58-04 and 58-05 write analyzers in
// the SAME wave: whichever declared it first, the other would receive it
// as an already-shipped fact rather than a shared agreement. No analyzer
// may declare its own return type instead of this one.
type FamilyResult struct {
	Characteristics Characteristics `json:"characteristics"`
	Findings        []Finding       `json:"findings"`
	Checks          []Check         `json:"checks"`
}
