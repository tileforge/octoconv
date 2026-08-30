// This file assembles the final Report handed back to a client of POST
// /v1/inspect (Plan 58-07) from the FamilyResult values every family
// analyzer (pdf.go, container.go, media.go, image.go) already produces.
// Nothing here re-derives a finding or a characteristic -- it only merges,
// completes the closed-vocabulary section, and computes the report-wide
// Coverage claim, which is a DIFFERENT thing from any one Check's own
// three-value status (status.go's own doc comment on Coverage explains why
// the two must not collapse into one field).
package inspect

import (
	"sort"
	"strings"
)

// Report is the JSON shape POST /v1/inspect returns for the part this
// package owns: the detected format, the applicable-only characteristics
// bag, the closed findings-vocabulary section, and the report-wide
// completeness claim. It deliberately has NO "operations" field -- Plan
// 58-07's Task 2/3 adds that as a sibling field on a wrapper type declared
// in internal/api, not here, because enumerating (operation, target, engine)
// triples with a live quota check requires reading convert.Default and
// calling jobs.Repo.PreAdmit -- neither of which this package may import
// without internal/inspect suddenly knowing about engines and Postgres,
// which is exactly the layering this package's own doc comment (findings.go)
// exists to keep clean: this package answers "what was found in the file",
// nothing about what the service can DO with it.
type Report struct {
	// Format is the magic-byte-detected format (SC1) -- empty when nothing
	// in the sniffer chain recognized the content at all (D-03: this is a
	// normal, complete report, never a rejection).
	Format string `json:"format"`
	// DeclaredFormat is the normalized filename-extension-derived format,
	// carried purely for the extension_mismatch finding and for client
	// display -- it NEVER selects which family analyzer ran (SC1's own
	// prohibition on choosing behavior by extension).
	DeclaredFormat string `json:"declared_format"`
	// SizeBytes is the uploaded file's total size, taken from the
	// multipart part header, not from any bounded read this package
	// performed.
	SizeBytes int64 `json:"size_bytes"`
	// Characteristics is the union of every family analyzer's own
	// characteristics bag, merged by mergeCharacteristics below.
	Characteristics Characteristics `json:"characteristics"`
	// Findings is the union of every family analyzer's own findings,
	// sorted by Code for a reproducible response body.
	Findings []Finding `json:"findings"`
	// Checks is the CLOSED, complete section: exactly one Check per code
	// AllCodes() declares that did not produce a Finding, in AllCodes()'s
	// own declaration order.
	Checks []Check `json:"checks"`
	// Coverage is the report-WIDE completeness claim -- see Coverage's own
	// doc comment (status.go) for why this is a separate claim from any one
	// Check's status.
	Coverage Coverage `json:"coverage"`
}

// BuildReport merges every family analyzer's FamilyResult into one Report,
// completes the closed findings-vocabulary section for every code no family
// claimed, and computes the report-wide Coverage claim. tr may be nil (a
// caller that never wired a Tracker at all, e.g. a unit test exercising a
// single family in isolation) -- BuildReport treats a nil tr exactly like an
// unexhausted one: Coverage stays Complete unless the per-Check loop below
// finds a ceiling reason on some individual Check.
func BuildReport(format, declaredFormat string, size int64, tr *Tracker, families ...FamilyResult) Report {
	merged := mergeCharacteristics(families)
	findings := mergeFindings(families)
	checks := buildChecks(format, families, findings)
	coverage := buildCoverage(merged, checks, tr)
	coverage = degradeCoverageOnInvariantViolation(findings, checks, coverage)

	return Report{
		Format:          format,
		DeclaredFormat:  declaredFormat,
		SizeBytes:       size,
		Characteristics: merged,
		Findings:        findings,
		Checks:          checks,
		Coverage:        coverage,
	}
}

// mergeCharacteristics unions every family's Characteristics bag with a
// deterministic "first family wins" rule on a key collision. A collision
// between two families that could ever legitimately run for the SAME
// detected format would be a programming error (two families claiming to
// know the same fact about one file) -- but it is not this function's job
// to detect that; TestBuildReportFamilyCharacteristicKeysDoNotCollide
// documents and proves the actual, closed set of INTENTIONAL cross-family
// key reuses (page_count between pdf.go/container.go, width/height between
// image.go/media.go -- both pairs of families are format-mutually-exclusive
// by construction, so the two members of a pair are never both present in
// the SAME families slice a real dispatcher builds). This function stays a
// simple, panic-free merge regardless: "first wins" is a safe, silent
// degradation for the pathological case, never a crash.
func mergeCharacteristics(families []FamilyResult) Characteristics {
	merged := Characteristics{}
	for _, f := range families {
		for k, v := range f.Characteristics {
			if _, exists := merged[k]; !exists {
				merged[k] = v
			}
		}
	}
	return merged
}

// mergeFindings unions every family's Findings, sorted by Code so the
// response body is reproducible across runs of the same input (INSP-02).
func mergeFindings(families []FamilyResult) []Finding {
	var findings []Finding
	for _, f := range families {
		findings = append(findings, f.Findings...)
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].Code < findings[j].Code })
	return findings
}

// buildChecks completes the closed findings-vocabulary section: for every
// code AllCodes() declares that did not produce a Finding, take the FIRST
// family's own Check for that code if one exists, else fall back to
// StatusNotApplicable/ReasonFormatNotApplicable -- UNLESS format is empty
// (codex PR9 finding 6 / T-SLE-06), in which case the fallback becomes
// StatusUnknown/ReasonFormatUnrecognized instead. "not_applicable" is the
// honest claim "there was nothing HERE to look for", which requires
// knowing what format IS being inspected; when detection never claimed a
// format at all (Report.Format's own doc comment: empty means nothing in
// the sniffer chain recognized the content), applicability cannot be
// established either way, so the whole vocabulary must say "unknown", not
// "not applicable" -- a client currently receiving a complete
// not_applicable report for unrecognized content would wrongly conclude
// every risk check was irrelevant.
//
// This completion lives HERE, not inside any one family analyzer, because
// a single analyzer only knows its OWN domain's codes and has no standing
// to declare a code from a DIFFERENT family's domain "not applicable" --
// only the assembler, which sees every family's result at once, can safely
// make that claim for the codes nobody else claimed. Iterating AllCodes()
// in its own declaration order also means the returned slice is already
// sorted the way the closed vocabulary is declared -- no separate sort
// needed.
func buildChecks(format string, families []FamilyResult, findings []Finding) []Check {
	findingCodes := make(map[Code]bool, len(findings))
	for _, f := range findings {
		findingCodes[f.Code] = true
	}

	checkByCode := make(map[Code]Check)
	for _, f := range families {
		for _, c := range f.Checks {
			if _, exists := checkByCode[c.Code]; !exists {
				checkByCode[c.Code] = c
			}
		}
	}

	unrecognizedFormat := format == ""

	checks := make([]Check, 0, len(allCodes))
	for _, code := range AllCodes() {
		if findingCodes[code] {
			continue
		}
		if c, ok := checkByCode[code]; ok {
			checks = append(checks, c)
			continue
		}
		if unrecognizedFormat {
			checks = append(checks, Check{Code: code, Status: StatusUnknown, Reason: ReasonFormatUnrecognized})
			continue
		}
		checks = append(checks, Check{Code: code, Status: StatusNotApplicable, Reason: ReasonFormatNotApplicable})
	}
	return checks
}

// buildCoverage computes the report-wide Coverage claim from the already-
// completed checks slice, the merged Characteristics bag, and tr's own
// exhaustion state.
//
// The per-Check loop below is the PRIMARY mechanism and is intentionally
// narrow: it marks Coverage incomplete for EXACTLY the codes whose own
// Check carries a reason status.go's makesReportIncomplete admits -- never
// for a code that is merely unknown for a structural reason
// (ReasonCompressedObjectStreams, ReasonParserUnavailable,
// ReasonMalformedInput) or genuinely not applicable
// (ReasonFormatNotApplicable). This is D-02a's central distinction, applied
// mechanically: structural blindness about the FILE never makes the REPORT
// selective; input this service never actually examined does.
//
// The membership test is DELEGATED, not repeated here (Phase 59, D-11). An
// earlier version of this loop compared against ReasonWorkCeilingBytes and
// ReasonWorkCeilingTime inline, so the set was declared in three places --
// here, in MarkTruncated, and in MarkTruncatedByCharacteristic -- and
// widening it meant remembering all three. The two Mark* methods gate
// themselves anyway; this filter exists only to avoid calling them for
// reasons that are obviously not candidates, and it must therefore use the
// SAME predicate they do or it becomes a second, quieter gate.
//
// The characteristics loop below it closes codex PR9 finding 5 / T-SLE-05:
// a Check is not the only place a coverage-losing reason can appear. Since
// Phase 59 this is also the path by which pdf.go's own text_layer_reason
// characteristic (Plan 59-05) reaches Coverage -- the "_reason" suffix is
// load-bearing naming, not decoration. A media
// analyzer can report a characteristic-level "<name>_reason" key (e.g.
// duration_seconds_reason) carrying ReasonWorkCeilingBytes/
// ReasonWorkCeilingTime with NO accompanying Check at all -- the per-Check
// loop above would never see it, and a Coverage that only ever examined
// checks would say Complete:true in the same response body that admits a
// characteristic was truncated by this service's own ceiling, which is a
// self-contradictory report. merged's keys are walked in SORTED order so
// that, when more than one characteristic admits a ceiling truncation,
// which one supplies Coverage.Reason (the first one seen) is deterministic
// across runs of the same input, not dependent on Go's randomized map
// iteration order.
//
// The tr-wide fallback below both loops is a SEPARATE, narrower safety net:
// if the shared Tracker for the whole operation is exhausted but neither
// loop above happened to see a ceiling reason yet (e.g. an analyzer was
// never even dispatched to before the shared budget ran dry, so it fell
// through to the generic not_applicable/format_not_applicable branch above
// with no way for this function to know that was actually a starved
// analyzer rather than a genuinely inapplicable format), Coverage.Complete
// is still flipped to false and Reason is filled in -- but ONLY when
// nothing above already left Reason non-empty, and WITHOUT touching
// TruncatedChecks. Guessing which additional codes to list there would risk
// incorrectly reclassifying a code that really is not_applicable to this
// format as if the operation's own budget were the reason it is missing,
// which is worse than an honest "the report as a whole is incomplete, but
// this package will not name individual codes it cannot actually prove
// were affected."
func buildCoverage(merged Characteristics, checks []Check, tr *Tracker) Coverage {
	coverage := Coverage{Complete: true}
	for _, c := range checks {
		if c.Reason.makesReportIncomplete() {
			coverage.MarkTruncated(c.Reason, c.Code)
		}
	}

	characteristicKeys := make([]string, 0, len(merged))
	for k := range merged {
		characteristicKeys = append(characteristicKeys, k)
	}
	sort.Strings(characteristicKeys)
	for _, k := range characteristicKeys {
		if !strings.HasSuffix(k, "_reason") {
			continue
		}
		if s, ok := merged[k].(string); ok {
			coverage.MarkTruncatedByCharacteristic(Reason(s))
		}
	}

	if tr != nil && tr.Exhausted() {
		coverage.Complete = false
		if coverage.Reason == "" {
			coverage.Reason = tr.ExhaustedReason()
		}
	}
	return coverage
}

// degradeCoverageOnInvariantViolation is STEP 5 (D-02a's own "unknown is a
// correct answer, a silent lie is not" discipline, applied to this
// package's OWN invariant rather than to a parser's result): ValidateReport
// is the single mechanical proof that every declared code is accounted for
// exactly once. A violation here is a DEFECT in this assembly, not a fact
// about the file -- the only honest response is to say the report is
// incomplete, never to silently return findings/checks that do not actually
// satisfy the union invariant this whole package promises callers. This
// project forbids panic for control flow in internal/ (CLAUDE.md);
// degrading Coverage is the fail-safe equivalent. Extracted as its own
// function (rather than inlined in BuildReport) so
// TestBuildReportSelfCheckDegradesOnInvariantViolation can exercise this
// exact tail directly against a manufactured, adversarial findings/checks
// pair -- BuildReport's own merge logic (buildChecks) structurally prevents
// a real violation from ever reaching here in practice, which is precisely
// why this path needs a test that does not rely on reproducing that defect
// through the normal merge path.
func degradeCoverageOnInvariantViolation(findings []Finding, checks []Check, coverage Coverage) Coverage {
	if err := ValidateReport(findings, checks); err != nil {
		coverage.Complete = false
		coverage.Reason = ReasonMalformedInput
	}
	return coverage
}
