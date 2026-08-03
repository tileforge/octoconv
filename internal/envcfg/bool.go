// Package envcfg parses environment-variable values into typed configuration
// with strict, explicitly-documented semantics. It deliberately never logs:
// surfacing an unrecognized value is left to the caller, because callers in
// internal/ return errors rather than log (see CLAUDE.md), and some of them
// are read on a per-request path, where a log line would flood on a single
// operator typo.
package envcfg

import (
	"os"
	"strings"
)

// ParseBool interprets raw as a strict boolean flag. It returns def together
// with recognized == false when raw is non-empty but is not exactly "true" or
// "false" after stripping a trailing inline "#" comment and surrounding
// whitespace.
//
// Behavior table:
//
//	raw value                    | value      | recognized
//	------------------------------+-----------+-----------
//	""  (or var absent)          | def        | true
//	"true"                       | true       | true
//	"false"                      | false      | true
//	"true   # reason"            | true       | true
//	"false # reason"             | false      | true
//	"true " (trailing space)     | true       | true
//	"  true" (leading space)     | true       | true
//	"true anything"              | def        | false
//	"TRUE", "1", "yes", "on"     | def        | false
//	"# only a comment"           | def        | true (strips to empty)
//
// This deliberately does NOT reuse cmd/api's firstField: firstField splits at
// the first space, so it would accept "true anything" as "true" — leniency on
// a value that gates a safety control (QUOTA_ENFORCEMENT_DISABLED) is exactly
// the defect this parser exists to close. Do not "simplify" the two back
// together.
func ParseBool(raw string, def bool) (bool, bool) {
	if idx := strings.IndexByte(raw, '#'); idx >= 0 {
		raw = raw[:idx]
	}
	raw = strings.TrimSpace(raw)

	switch raw {
	case "":
		return def, true
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return def, false
	}
}

// Bool reads key from the process environment and parses it with ParseBool.
func Bool(key string, def bool) (bool, bool) {
	return ParseBool(os.Getenv(key), def)
}
