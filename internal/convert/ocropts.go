// Package convert (this file): OCROpts is the closed, strictly-parsed set of
// client-requested OCR options for the OCR engine foundation (OCRO-01,
// 43-CONTEXT.md D-05). It mirrors AudioOpts (audioopts.go) verbatim -- a
// closed allowlist validated via the shared checkStrictObject helper
// (opts.go), never a raw client string reaching a subprocess argv.
package convert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ocrLanguageAllowlist is the closed set of accepted `language` values
// (OCRO-01, D-05). Only `eng` and `rus` are baked in day-one (Russian-first
// company, tessdata_fast, SHA-pinned) -- deliberately closed, extended per
// real client demand, never open-ended. This is the complete day-one set:
// no automatic page-rotation-detection capability is exposed here at all
// this phase (see the phase's decision record for the deferred capability
// and its rationale). A future extension of this allowlist MUST re-derive
// this same closed-map-lookup discipline, never accept a raw client string.
var ocrLanguageAllowlist = map[string]bool{
	"eng": true,
	"rus": true,
}

// OCROpts is the closed, strictly-parsed set of client-requested OCR
// options (OCRO-01). Language is validated against the closed
// ocrLanguageAllowlist above -- it is never passed as a raw client string
// into tesseract's -l argv flag. Combinable values (e.g. "eng+rus") are
// split on "+" and every component is independently allowlist-checked;
// the whole value is rejected if any component fails.
//
// Once validated, Language reaches tesseract ONLY as a discrete
// exec.Command argv slice element (e.g. `args = append(args, "-l",
// o.Language)`, wired in a later plan) -- runCommand (exec.go) never
// invokes a shell, so there is no shell-metacharacter injection surface at
// all. The allowlist closes the remaining risk: an unvalidated string
// reaching Language could otherwise smuggle an unsupported/malicious
// value (e.g. a path-traversal-shaped or flag-injection-shaped string)
// straight into tesseract's argv; the allowlist rejects it before any
// work begins, matching AudioOpts.Language's discipline verbatim.
//
// No page-segmentation-mode or engine-mode field exists here by design:
// those remain fixed server-side constants (D-04), never per-job
// client-controlled opts.
type OCROpts struct {
	Language string `json:"language,omitempty"`
}

// ParseOCROpts strict-decodes raw JSON into an OCROpts and validates
// Language against the closed ocrLanguageAllowlist (splitting combinable
// values on "+" and requiring every component to be listed). Strictness
// (exactly one top-level JSON object, no duplicate keys, no trailing
// bytes, no top-level null, unknown fields rejected) is enforced by the
// shared checkStrictObject helper (opts.go) -- reused verbatim, never
// duplicated. Empty/absent input (a zero-length raw or `{}`) is valid and
// yields a zero OCROpts (opts are optional).
func ParseOCROpts(raw []byte) (OCROpts, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return OCROpts{}, nil
	}
	if err := checkStrictObject(raw); err != nil {
		return OCROpts{}, err
	}
	var o OCROpts
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&o); err != nil {
		return OCROpts{}, fmt.Errorf("parse opts: %w", err)
	}
	if o.Language != "" {
		for _, lang := range strings.Split(o.Language, "+") {
			if !ocrLanguageAllowlist[lang] {
				return OCROpts{}, fmt.Errorf("unsupported language %q", o.Language)
			}
		}
	}
	return o, nil
}
