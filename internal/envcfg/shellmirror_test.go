package envcfg

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// helpersTpl is the chart helper that carries the shell mirror of ParseBool.
const helpersTpl = "../../deploy/chart/octoconv/templates/_helpers.tpl"

const (
	preludeOpen  = `{{- define "octoconv.mcAliasPrelude" -}}`
	preludeClose = `{{- end }}`
)

// TestChartShellMirrorsParseBool is a differential test, not a reading of the
// shell by eye.
//
// The chart's mc-using containers must pick http:// or https:// exactly as
// storage.New does, because they probe the endpoint the application will then
// dial. That means a second implementation of ParseBool, written in POSIX sh,
// living in a Helm template -- and a second implementation of a parser is
// something to test against the first, never to reason about. This project has
// already paid for the reasoning version of that mistake four times over one
// bound (see .planning/RETROSPECTIVE.md).
//
// So: extract the snippet the chart actually renders, run it under /bin/sh for
// every row of ParseBool's own documented behaviour table, and compare. If
// someone "simplifies" either side, the table diverges here rather than in a
// cluster where every pod sits in Init.
func TestChartShellMirrorsParseBool(t *testing.T) {
	raw, err := os.ReadFile(helpersTpl)
	if err != nil {
		t.Fatalf("read chart helper: %v", err)
	}
	body := string(raw)

	i := strings.Index(body, preludeOpen)
	if i < 0 {
		t.Fatalf("%s no longer defines octoconv.mcAliasPrelude -- if the shell mirror moved, move this test with it", helpersTpl)
	}
	rest := body[i+len(preludeOpen):]
	j := strings.Index(rest, preludeClose)
	if j < 0 {
		t.Fatalf("octoconv.mcAliasPrelude is not terminated by %q", preludeClose)
	}
	snippet := rest[:j]

	// A snippet that renders no branches would pass every case below by
	// accident. Refuse to run against one.
	for _, want := range []string{"S3_SCHEME=https", "S3_SCHEME=http", "S3_USE_SSL"} {
		if !strings.Contains(snippet, want) {
			t.Fatalf("extracted snippet does not contain %q; this test would prove nothing:\n%s", want, snippet)
		}
	}
	if strings.Contains(snippet, "{{") {
		t.Fatalf("extracted snippet contains a Helm action, so what runs here is not what the chart renders:\n%s", snippet)
	}

	// ParseBool's own doc-comment table, verbatim, plus the two-scheme mapping
	// storage.New applies to the result.
	cases := []string{
		"",
		"true",
		"false",
		"true   # reason",
		"false # reason",
		"true ",
		"  true",
		"true anything",
		"TRUE",
		"1",
		"yes",
		"on",
		"# only a comment",
		"https://evil",
		"true;false",
	}

	for _, raw := range cases {
		t.Run("S3_USE_SSL="+raw, func(t *testing.T) {
			wantScheme, recognized := ParseBool(raw, false)
			goResult := "ERROR"
			if recognized {
				goResult = "http"
				if wantScheme {
					goResult = "https"
				}
			}

			cmd := exec.Command("/bin/sh", "-c", snippet+"\nprintf '%s' \"$S3_SCHEME\"")
			cmd.Env = append(os.Environ(), "S3_USE_SSL="+raw)
			out, err := cmd.Output()
			shResult := strings.TrimSpace(string(out))
			if err != nil {
				shResult = "ERROR"
			}

			if shResult != goResult {
				t.Errorf("S3_USE_SSL=%q: chart shell says %q, envcfg.ParseBool says %q -- the probe would use a different transport than storage.New", raw, shResult, goResult)
			}
		})
	}
}
