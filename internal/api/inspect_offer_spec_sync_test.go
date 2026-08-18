// This file keeps docs/openapi.yaml's InspectOperation schema honest about the
// deterministic-availability half of an offer, the same way
// internal/errcode/openapi_sync_test.go keeps the Error.code enum honest: by
// mechanical comparison against the Go code, not by review discipline.
//
// PR9R22 (estimate.go:265) was a case of inspect promising something POST
// /v1/jobs would refuse. The fix added `available` / `unavailable_reason` /
// `max_bytes` to the offer, and a documented contract that drifts from the
// code it documents recreates the same class of lie one layer up -- an SDK
// generated from a spec that omits `available` cannot even read the verdict.
package api

import (
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/apaderin/octoconv/internal/errcode"
)

// inspectOfferSpecPath is relative to this package's own directory
// (internal/api/), so it resolves to <repo-root>/docs/openapi.yaml.
const inspectOfferSpecPath = "../../docs/openapi.yaml"

// offerUnavailableReasons is every value availableOperations can put in an
// offer's unavailable_reason field. It is the Go-side source of truth for the
// test below; extend it in the same commit that teaches availableOperations a
// new deterministic refusal.
var offerUnavailableReasons = []string{errcode.PayloadTooLarge}

// inspectOperationSchema returns the lines of docs/openapi.yaml's
// InspectOperation schema block: from its own four-space key to the next
// sibling schema at the same indentation.
func inspectOperationSchema(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(inspectOfferSpecPath)
	if err != nil {
		t.Fatalf("read %s: %v -- docs/openapi.yaml must exist at the repo root", inspectOfferSpecPath, err)
	}
	lines := strings.Split(string(raw), "\n")
	start := -1
	for i, l := range lines {
		if l == "    InspectOperation:" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("no `    InspectOperation:` schema key in %s -- the schema was renamed or reindented; fix this parser in the same commit", inspectOfferSpecPath)
	}
	for i := start; i < len(lines); i++ {
		l := lines[i]
		if strings.HasPrefix(l, "    ") && !strings.HasPrefix(l, "     ") && strings.TrimSpace(l) != "" {
			return lines[start:i] // next sibling schema
		}
	}
	return lines[start:]
}

// TestInspectOperationSpecDeclaresDeterministicAvailability proves the spec
// declares `available` as REQUIRED. Optional would be worse than absent: a
// generated client would treat a missing field as "unknown", and the whole
// point of this axis is that it is never unknown.
func TestInspectOperationSpecDeclaresDeterministicAvailability(t *testing.T) {
	block := inspectOperationSchema(t)

	var requiredLine string
	for _, l := range block {
		if strings.HasPrefix(strings.TrimSpace(l), "required:") {
			requiredLine = l
		}
	}
	if requiredLine == "" {
		t.Fatal("InspectOperation declares no `required:` list at all")
	}
	for _, field := range []string{"operation", "target", "engine", "available", "estimated_max_seconds", "estimate_source", "quota"} {
		if !strings.Contains(requiredLine, field) {
			t.Errorf("InspectOperation.required omits %q (line: %s) -- every field availableOperations always emits must be required, or a generated client cannot rely on it", field, strings.TrimSpace(requiredLine))
		}
	}
	for _, prop := range []string{"available:", "unavailable_reason:", "max_bytes:"} {
		found := false
		for _, l := range block {
			if strings.TrimSpace(l) == prop {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("InspectOperation declares no %s property -- operationOffer emits it, so the spec must document it", prop)
		}
	}
}

// TestInspectOperationSpecUnavailableReasonEnumMatchesGo is the bidirectional
// half: the spec's unavailable_reason enum may not omit a value the Go code can
// emit, and may not advertise one it cannot. Both directions mislead a client
// that generates code from this document.
func TestInspectOperationSpecUnavailableReasonEnumMatchesGo(t *testing.T) {
	block := inspectOperationSchema(t)

	// The enum line belonging to unavailable_reason: the first `enum: [...]`
	// after that property's own key, within this schema block.
	specEnum := ""
	inProp := false
	for _, l := range block {
		trimmed := strings.TrimSpace(l)
		switch {
		case trimmed == "unavailable_reason:":
			inProp = true
		case inProp && strings.HasPrefix(trimmed, "enum:"):
			specEnum = trimmed
			inProp = false
		case inProp && strings.HasSuffix(trimmed, ":") && !strings.HasPrefix(trimmed, "description") && trimmed != "unavailable_reason:":
			inProp = false // left the property without finding an enum
		}
		if specEnum != "" {
			break
		}
	}
	if specEnum == "" {
		t.Fatal("InspectOperation.unavailable_reason declares no enum -- an open string field lets any value pass review; keep it closed")
	}

	got := parseInlineEnum(specEnum)
	want := append([]string(nil), offerUnavailableReasons...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("drift between the spec's unavailable_reason enum %v and what availableOperations can emit %v -- fix whichever side is wrong (the Go code is the source of truth, D-06); never relax this test", got, want)
	}

	// Every advertised reason must also be a real error code, so the offer and
	// the eventual HTTP error keep speaking one vocabulary.
	known := map[string]bool{}
	for _, c := range errcode.All() {
		known[c] = true
	}
	for _, r := range got {
		if !known[r] {
			t.Errorf("unavailable_reason %q is not a code errcode.All() declares -- an offer must never invent a vocabulary the error path does not use", r)
		}
	}
}

// parseInlineEnum extracts the values from a flow-style `enum: [a, b]` line.
func parseInlineEnum(line string) []string {
	open := strings.Index(line, "[")
	closeIdx := strings.LastIndex(line, "]")
	if open < 0 || closeIdx < open {
		return nil
	}
	var out []string
	for _, part := range strings.Split(line[open+1:closeIdx], ",") {
		if v := strings.Trim(strings.TrimSpace(part), `"'`); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// TestInspectSentinelFormatsMatchTheContract keeps the two halves of PR9R26's
// fix from drifting apart: a sentinel that reaches the wire must be documented,
// and a sentinel that must not reach the wire must actually not.
//
// The failure this guards against is not hypothetical -- it is what the round
// found: a fourth sentinel was added for routing and silently became a public
// "format" value the contract does not describe.
func TestInspectSentinelFormatsMatchTheContract(t *testing.T) {
	raw, err := os.ReadFile(inspectOfferSpecPath)
	if err != nil {
		t.Fatalf("read %s: %v", inspectOfferSpecPath, err)
	}
	spec := string(raw)

	for _, public := range inspectPublicSentinelFormats {
		if !strings.Contains(spec, public) {
			t.Errorf("sentinel %q reaches Report.Format but docs/openapi.yaml never mentions it -- a client cannot handle a value the contract does not describe", public)
		}
		if got := reportFormat(public); got != public {
			t.Errorf("reportFormat(%q) = %q, want it unchanged -- this sentinel is documented as a public value", public, got)
		}
	}

	for _, internal := range inspectInternalOnlyFormats {
		if got := reportFormat(internal); got != "" {
			t.Errorf("reportFormat(%q) = %q, want \"\" -- an internal routing sentinel must never be published as a detected format", internal, got)
		}
		if strings.Contains(spec, internal) {
			t.Errorf("sentinel %q is internal-only, yet docs/openapi.yaml mentions it -- either it is public (move it to inspectPublicSentinelFormats) or the doc is wrong", internal)
		}
	}
}
