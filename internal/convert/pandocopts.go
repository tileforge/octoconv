// Package convert (this file): PandocOpts is the closed, strictly-parsed set
// of client-requested pandoc options for the pandoc engine foundation
// (PDCO-01, 47-CONTEXT.md D-04). It mirrors OCROpts/AudioOpts verbatim -- a
// closed allowlist validated via the shared checkStrictObject helper
// (opts.go), never a raw client string reaching a subprocess argv.
package convert

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// pandocWrapAllowlist is the closed set of accepted `wrap` values (PDCO-01,
// D-04) -- pandoc's own --wrap enum. Deliberately a map lookup (mirrors
// audioLanguageAllowlist/ocrLanguageAllowlist), never a raw client string:
// an unlisted value is rejected before any work begins, matching every
// other engine's opts discipline.
var pandocWrapAllowlist = map[string]bool{
	"auto":     true,
	"none":     true,
	"preserve": true,
}

// PandocOpts is the closed, strictly-parsed set of client-requested pandoc
// options (PDCO-01, D-04). Wrap is validated against the closed
// pandocWrapAllowlist above -- it is never passed as a raw client string
// into pandoc's --wrap argv flag.
//
// Once validated, every field reaches pandoc ONLY as a discrete
// exec.Command argv slice element via pandocArgs (this file, Task 2) --
// runCommand (exec.go) never invokes a shell, so there is no
// shell-metacharacter injection surface at all. The allowlist closes the
// remaining risk: an unvalidated Wrap string could otherwise smuggle an
// unsupported/malicious value (e.g. a flag-injection-shaped string) straight
// into pandoc's argv; the allowlist rejects it before any work begins,
// matching OCROpts.Language's/AudioOpts.Language's discipline verbatim.
type PandocOpts struct {
	Standalone     bool   `json:"standalone,omitempty"`
	Toc            bool   `json:"toc,omitempty"`
	NumberSections bool   `json:"number_sections,omitempty"`
	Wrap           string `json:"wrap,omitempty"`
	SectionDivs    bool   `json:"section_divs,omitempty"`
}

// ParsePandocOpts strict-decodes raw JSON into a PandocOpts and validates
// Wrap against the closed pandocWrapAllowlist. Strictness (exactly one
// top-level JSON object, no duplicate keys, no trailing bytes, no
// top-level null, unknown fields rejected) is enforced by the shared
// checkStrictObject helper (opts.go) -- reused verbatim, never duplicated.
// Empty/absent input (a zero-length raw or `{}`) is valid and yields a zero
// PandocOpts (opts are optional).
func ParsePandocOpts(raw []byte) (PandocOpts, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return PandocOpts{}, nil
	}
	if err := checkStrictObject(raw); err != nil {
		return PandocOpts{}, err
	}
	var o PandocOpts
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&o); err != nil {
		return PandocOpts{}, fmt.Errorf("parse opts: %w", err)
	}
	if o.Wrap != "" && !pandocWrapAllowlist[o.Wrap] {
		return PandocOpts{}, fmt.Errorf("unsupported wrap %q", o.Wrap)
	}
	return o, nil
}

// PandocOptsFromMap round-trips a persisted map[string]any (job.Opts,
// already unmarshaled from the jobs.options jsonb column by internal/jobs)
// through ParsePandocOpts -- the same strictness (DisallowUnknownFields +
// closed allowlist) applied on the worker/converter read path as on the API
// write path, mirroring OCROptsFromMap/AudioOptsFromMap. A nil/empty map
// yields a zero PandocOpts, no error. A corrupt persisted map (e.g. an
// unlisted wrap value smuggled in by a prior write path bug) is rejected
// here exactly as it would be at parse time -- it can never smuggle an
// unlisted value into a later argv-building step.
func PandocOptsFromMap(m map[string]any) (PandocOpts, error) {
	if len(m) == 0 {
		return PandocOpts{}, nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return PandocOpts{}, fmt.Errorf("marshal opts: %w", err)
	}
	return ParsePandocOpts(raw)
}

// ValidatePandocApplicability rejects opts that do not apply to the given
// (engine, source, target) conversion. Empty opts always apply (nothing
// was requested). A non-empty PandocOpts applies only to pandoc-engine
// conversions. Deliberately its OWN function scoped to EnginePandoc, not
// merged into the shared ValidateApplicability (opts.go) -- mirrors
// ValidateOCRApplicability's/ValidateAudioApplicability's engine-scoped
// shape.
func ValidatePandocApplicability(engine, source, target string, o PandocOpts) error {
	if isZeroPandocOpts(o) {
		return nil
	}
	if engine != EnginePandoc {
		return fmt.Errorf("pandoc options are only valid for pandoc-engine conversions")
	}
	return nil
}

// isZeroPandocOpts reports whether o carries no client-requested options at
// all -- the same "empty opts always apply" shortcut ValidateApplicability
// uses for DocOpts/HTMLOpts (opts.go, htmlopts.go), for PandocOpts's 5
// fields.
func isZeroPandocOpts(o PandocOpts) bool {
	return o == PandocOpts{}
}

// pandocArgs builds pandoc's discrete argv fragment from an
// already-validated PandocOpts (PDCO-01). o.Wrap MUST already have passed
// ParsePandocOpts/PandocOptsFromMap -- this function performs NO
// validation of its own, it only shapes the argv: every set bool becomes
// its own discrete slice element, and Wrap (when non-empty) becomes a
// single "--wrap=<value>" element whose value is the already-enum-gated
// string. The returned slice never concatenates or interpolates raw client
// bytes into a single string beyond this one enum-constrained form -- so a
// caller appending these to an exec.Command argv (runCommand, exec.go)
// never invokes a shell and has no shell-metacharacter injection surface.
func pandocArgs(o PandocOpts) []string {
	var args []string
	if o.Standalone {
		args = append(args, "--standalone")
	}
	if o.Toc {
		args = append(args, "--toc")
	}
	if o.NumberSections {
		args = append(args, "--number-sections")
	}
	if o.SectionDivs {
		args = append(args, "--section-divs")
	}
	if o.Wrap != "" {
		args = append(args, "--wrap="+o.Wrap)
	}
	return args
}
