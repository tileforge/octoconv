package envcfg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const chartDir = "../../deploy/chart/octoconv"

// aliasScheme pulls the scheme out of every rendered `mc alias set` line.
var aliasScheme = regexp.MustCompile(`mc alias set local "(https?)://`)

func requireHelm(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not on PATH; this test compares a chart render against ParseBool and cannot run without it")
	}
}

// TestChartSchemeMatchesParseBool is a differential test between the two things
// that must agree about s3.useSSL: what the chart renders into every `mc alias
// set`, and what internal/envcfg.ParseBool tells storage.New.
//
// They must agree because the initContainers probe the endpoint the application
// will then dial. A probe on the wrong transport can never succeed, and a probe
// that can never succeed in an initContainer is worse than no probe at all: the
// pod sits in Init forever instead of starting.
//
// The chart no longer parses anything itself -- octoconv.s3Scheme uses sprig's
// `trim` (which IS strings.TrimSpace) and the same leading-segment cut on "#",
// so this checks that two callers of the same functions still line up rather
// than refereeing between two independent parsers. That is deliberate: the two
// POSIX-sh versions this replaced each shipped a divergence that review, not a
// test, had to find.
//
// Unrecognized values are expected to fail the RENDER, which is where
// ParseBool's `recognized == false` belongs once the value is known at template
// time -- storage.New would refuse to start on the same input anyway.
func TestChartSchemeMatchesParseBool(t *testing.T) {
	requireHelm(t)

	cases := []string{
		// ParseBool's own documented table.
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
		// Rows the doc comment does NOT list. These are the ones earning their
		// place: a table copied from the thing under test only proves the
		// author read the same doc twice. Internal whitespace is here because
		// the first shell version squeezed it out and read "t r u e" as true
		// while Go rejected it -- caught by review, not by this test's
		// predecessor.
		"t r u e",
		"tr ue",
		"f a l s e",
		"true false",
		" t r u e ",
		"\ttrue\t",
		"\ntrue\n",
		"\rtrue\r",
		"\v\ftrue",
		"  true  # reason with  spaces  ",
		" true",
		"true ",
		"true",
		// Hostile.
		"https://evil",
		"true;false",
		"true\nfalse",
		"$(echo true)",
		"`echo true`",
	}

	for _, raw := range cases {
		t.Run(strconv.Quote(raw), func(t *testing.T) {
			want, recognized := ParseBool(raw, false)
			wantScheme := "http"
			if want {
				wantScheme = "https"
			}

			values := filepath.Join(t.TempDir(), "useSSL.yaml")
			// strconv.Quote's escapes are a subset of YAML's double-quoted
			// style, so this round-trips control characters and NBSP without
			// pulling in a YAML library.
			if err := os.WriteFile(values, []byte(fmt.Sprintf("s3:\n  useSSL: %s\n", strconv.Quote(raw))), 0o600); err != nil {
				t.Fatal(err)
			}

			out, err := exec.Command("helm", "template", "octoconv", chartDir,
				"-f", values,
				"--set", "secrets.databaseUrl=x", "--set", "secrets.apiKeySalt=x",
				"--set", "secrets.webhookSigningSecret=x", "--set", "secrets.s3AccessKey=x",
				"--set", "secrets.s3SecretKey=x", "--set", "secrets.postgresPassword=x",
			).CombinedOutput()

			if !recognized {
				if err == nil {
					t.Fatalf("ParseBool rejects %q but the chart rendered anyway; every pod that render produced would fail at storage.New", raw)
				}
				if !strings.Contains(string(out), "s3.useSSL: unrecognized value") {
					t.Errorf("render failed for %q but not with the useSSL message, so the operator is told the wrong thing:\n%s", raw, out)
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseBool accepts %q but the chart refused to render it:\n%s", raw, out)
			}

			got := aliasScheme.FindAllStringSubmatch(string(out), -1)
			if len(got) == 0 {
				t.Fatalf("no `mc alias set` lines in the render; this case would pass vacuously")
			}
			for _, m := range got {
				if m[1] != wantScheme {
					t.Errorf("s3.useSSL=%q: chart renders %q, storage.New would use %q -- the probe and the application disagree about transport", raw, m[1], wantScheme)
				}
			}
		})
	}
}
