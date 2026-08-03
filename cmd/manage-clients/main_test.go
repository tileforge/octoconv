package main

import (
	"strings"
	"testing"

	"github.com/apaderin/octoconv/internal/convert"
)

// TestValidateEngine requires no database and no API_KEY_SALT -- it only
// exercises validateEngine, which is pure. Every value convert.AllEngines()
// returns must be accepted; a typo, case variant, empty string, or
// whitespace-padded value must all be rejected (52-07/WR-08: an operator
// typo in `manage-clients set-limit` must never silently produce a limit
// that is never enforced).
func TestValidateEngine(t *testing.T) {
	for _, e := range convert.AllEngines() {
		if err := validateEngine(e); err != nil {
			t.Errorf("validateEngine(%q) = %v, want nil", e, err)
		}
	}

	for _, bad := range []string{"images", "Image", "IMAGE", "", "*", " image", "image "} {
		err := validateEngine(bad)
		if err == nil {
			t.Errorf("validateEngine(%q) = nil, want error", bad)
		}
	}

	// The rejected token and at least one known engine name must both be
	// present in the error text, so the operator's next action is obvious
	// from the message alone.
	err := validateEngine("images")
	if err == nil {
		t.Fatal("validateEngine(\"images\") = nil, want error")
	}
	if !strings.Contains(err.Error(), "images") {
		t.Errorf("validateEngine(\"images\") error %q does not contain the rejected token %q", err.Error(), "images")
	}
	foundKnown := false
	for _, e := range convert.AllEngines() {
		if strings.Contains(err.Error(), e) {
			foundKnown = true
			break
		}
	}
	if !foundKnown {
		t.Errorf("validateEngine(\"images\") error %q does not name any known engine class", err.Error())
	}
}

// TestValidateEngineAcceptsExactlyKnownSet ties the CLI's accepted set to
// convert.AllEngines() directly (the single source of truth) rather than to
// a copy: every element of AllEngines() is accepted, and the count of
// accepted values equals len(AllEngines()).
func TestValidateEngineAcceptsExactlyKnownSet(t *testing.T) {
	engines := convert.AllEngines()
	accepted := 0
	for _, e := range engines {
		if err := validateEngine(e); err == nil {
			accepted++
		}
	}
	if accepted != len(engines) {
		t.Errorf("validateEngine accepted %d of %d values from convert.AllEngines()", accepted, len(engines))
	}
}
