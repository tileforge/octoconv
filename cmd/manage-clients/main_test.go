package main

import (
	"os"
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

// TestCLIIssuesNoDirectJobMutation is the guard that keeps this command
// honest, and it guards against a future edit rather than against today's
// code.
//
// 52 WR-01's real cost was never the inconvenience of writing SQL by hand. It
// is that a hand-written status update skips jobs.Repo.Cancel's transaction:
// the job stops being in-flight, so the concurrency slot frees, while its
// contribution to the daily rollup is never booked by recordUsage. The quota
// is repaired and its accounting is silently broken in the same stroke — a
// worse state than the stuck job, because nothing looks wrong afterwards.
//
// So this file may never grow its own job mutation. The plausible way it would
// is somebody removing the internal/jobs dependency "to keep the CLI light",
// which reads as a simplification and is a correctness regression.
//
// Reading the source rather than exercising behaviour is deliberate: the
// behaviour that must not exist cannot be observed by calling anything. It is
// the same shape as internal/inspect's TestAllReasonsCoversEveryDeclaredConstant,
// which also reads a Go file to assert a property no call can reveal.
func TestCLIIssuesNoDirectJobMutation(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	// Verbs that would mutate or remove job rows outside the guarded
	// transition. Deliberately narrow: broad SQL matching would fire on
	// unrelated prose and get the test weakened rather than the code fixed.
	for _, forbidden := range []string{
		"UPDATE jobs",
		"DELETE FROM jobs",
		"INSERT INTO jobs",
		"UPDATE client_usage_daily",
	} {
		if strings.Contains(string(src), forbidden) {
			t.Errorf("cmd/manage-clients/main.go contains %q.\n\n"+
				"Job state must change ONLY through jobs.Repo, whose transition runs the status update and the "+
				"recordUsage rollup in one transaction. A direct statement here frees the concurrency slot while "+
				"never booking the job's daily contribution — it repairs the quota and breaks its accounting at the "+
				"same time, which is the defect 52 WR-01 describes, not the cure for it.\n\n"+
				"If this fired because a COMMENT mentions the statement, reword the comment: the SQL this test "+
				"forbids is deliberately never spelled out in that file.", forbidden)
		}
	}
}

// TestCancelGoesThroughTheRepo is the positive half. The test above can be
// satisfied by a file that cancels nothing at all; this one pins that the
// command still reaches the one path that releases quota correctly.
func TestCancelGoesThroughTheRepo(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(src), ".Cancel(ctx,") {
		t.Error("cmd/manage-clients/main.go no longer calls jobs.Repo.Cancel — the cancel-job command must delegate " +
			"to the guarded transition, not reimplement it")
	}
}

// TestUsageListsOperatorJobCommands closes the gap that made an operator reach
// for raw SQL in the first place. A command nobody is told about is, from the
// operator's side, a command that does not exist — and this CLI's usage string
// is the only place they will look.
func TestUsageListsOperatorJobCommands(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	usageStart := strings.Index(string(src), "func usage()")
	if usageStart < 0 {
		t.Fatal("usage() not found")
	}
	usageBody := string(src)[usageStart:]

	for _, cmd := range []string{"list-inflight", "cancel-job"} {
		if !strings.Contains(usageBody, cmd) {
			t.Errorf("usage() does not mention %q -- an operator in an incident reads this string and nothing else, "+
				"so an unlisted remedy sends them back to a psql prompt", cmd)
		}
	}
}
