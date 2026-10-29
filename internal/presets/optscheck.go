package presets

import (
	"encoding/json"
	"fmt"

	"github.com/tileforge/octoconv/internal/convert"
)

// ValidateOptsJSON is the D-11 write-time convenience check for
// cmd/manage-presets create/update: it gives operators a fast, fail-early
// rejection of malformed opts at write time. It is deliberately NOT the
// trust boundary -- the engine that a preset's opts will actually be applied
// to is only known at job-creation time (derived from detected source +
// target), so this helper accepts opts that pass AT LEAST ONE of the current
// allowlist parsers (the current engine parsers). The real enforcement
// point is D-06: handleCreateJob re-runs the SAME strict parsers, engine-
// scoped, on every use of a resolved preset -- stored opts are never trusted
// (Pitfall 9).
//
// A nil or empty map is always valid (mirrors DocOptsFromMap/HTMLOptsFromMap's
// len==0 short-circuit).
func ValidateOptsJSON(options map[string]any) error {
	if len(options) == 0 {
		return nil
	}

	raw, err := json.Marshal(options)
	if err != nil {
		return fmt.Errorf("marshal opts: %w", err)
	}

	if len(raw) > 4096 {
		return fmt.Errorf("opts exceed 4096 bytes")
	}
	if _, err := convert.ParseDocOpts(raw); err == nil {
		return nil
	}
	if _, err := convert.ParseHTMLOpts(raw); err == nil {
		return nil
	}

	if _, err := convert.ParseAudioOpts(raw); err == nil {
		return nil
	}
	if _, err := convert.ParseAVOpts(raw); err == nil {
		return nil
	}
	if _, err := convert.ParseOCROpts(raw); err == nil {
		return nil
	}
	if _, err := convert.ParsePandocOpts(raw); err == nil {
		return nil
	}
	if _, err := convert.ParseCADOpts(raw); err == nil {
		return nil
	}

	return fmt.Errorf("opts match no known engine schema")
}
