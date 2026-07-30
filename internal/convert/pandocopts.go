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
