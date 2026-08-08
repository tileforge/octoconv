package errcode

import (
	"regexp"
	"testing"
)

// machineSafePattern matches the lowercase-snake shape every sync error code
// must follow: `^[a-z][a-z0-9_]*$`, matching the existing `quota_exceeded` /
// `rate_limited` / `engine_error` style.
var machineSafePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func TestAllCodesUniqueAndNonEmpty(t *testing.T) {
	seen := make(map[string]bool, len(all))
	for _, code := range All() {
		if code == "" {
			t.Fatal("All() contains an empty-string code")
		}
		if seen[code] {
			t.Fatalf("All() contains duplicate code %q", code)
		}
		seen[code] = true
	}
}

func TestAllCodesMatchMachineSafePattern(t *testing.T) {
	for _, code := range All() {
		if !machineSafePattern.MatchString(code) {
			t.Fatalf("code %q does not match machine-safe pattern %s", code, machineSafePattern.String())
		}
	}
}

// TestWireValuesFrozen asserts the literal strings already shipping on the
// wire (ERRC-V2-01) against the constants -- these two must never be
// renamed, since internal/mcpserver/client.go's 429 handling and the
// frontend both match on the literal string values, not on this package.
func TestWireValuesFrozen(t *testing.T) {
	if QuotaExceeded != "quota_exceeded" {
		t.Fatalf("QuotaExceeded = %q, want %q (already on the wire, ERRC-V2-01)", QuotaExceeded, "quota_exceeded")
	}
	if RateLimited != "rate_limited" {
		t.Fatalf("RateLimited = %q, want %q (already on the wire, ERRC-V2-01)", RateLimited, "rate_limited")
	}
}

func TestIsKnown(t *testing.T) {
	if IsKnown("no_such_code") {
		t.Fatal(`IsKnown("no_such_code") = true, want false`)
	}
	if !IsKnown(NotFound) {
		t.Fatal("IsKnown(NotFound) = false, want true")
	}
}

// TestAllReturnsCopy asserts mutating the slice returned by All() does not
// affect a subsequent call -- callers must not be able to corrupt the
// package's closed vocabulary via the slice they were handed.
func TestAllReturnsCopy(t *testing.T) {
	first := All()
	if len(first) == 0 {
		t.Fatal("All() returned an empty slice")
	}
	original := first[0]
	first[0] = "mutated"

	second := All()
	if second[0] != original {
		t.Fatalf("All() second call returned %q, want unmutated %q -- All() must return a copy", second[0], original)
	}
}
