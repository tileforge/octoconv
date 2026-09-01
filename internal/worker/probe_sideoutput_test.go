package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/jobs"
)

func requireSanitizeBinaries(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"gs", "unzip", "file"} {
		if _, err := exec.LookPath(bin); err != nil {
			if os.Getenv("OCTOCONV_REQUIRE_BINS") == "1" {
				t.Fatalf("%s not on PATH and OCTOCONV_REQUIRE_BINS=1: a skipped test is not a passed test", bin)
			}
			t.Skipf("%s not found on PATH, skipping real-binary test", bin)
		}
	}
}

// TestProbeSideOutputsCarriesTheReport proves the seam between the converter
// and the worker: the converter writes the report beside its output, and this
// hook is what turns that file into the job's role=report output.
//
// Written because the report was a deliverable with nothing delivering it --
// Plan 60-05 built the report and Plan 60-06's task list did not include
// emitting it, so without this wiring the phase would have ended with a
// report shape that no job ever produces.
func TestProbeSideOutputsCarriesTheReport(t *testing.T) {
	requireSanitizeBinaries(t)

	dir := t.TempDir()
	in := filepath.Join(dir, "in.pdf")
	blob, err := os.ReadFile(filepath.Join("..", "convert", "testdata", "sanitize", "threat_all_classes.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(in, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.pdf")

	if err := (convert.SanitizeConverter{}).Convert(context.Background(), in, out, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	extra, err := probeSideOutputs(context.Background(), &jobs.Job{}, out)
	if err != nil {
		t.Fatalf("probeSideOutputs: %v", err)
	}
	if len(extra) != 1 {
		t.Fatalf("got %d side outputs, want exactly 1", len(extra))
	}
	so := extra[0]

	if so.Role != jobs.OutputRoleReport {
		t.Errorf("Role = %q, want %q -- a client selects the report BY ROLE, never by ordinal",
			so.Role, jobs.OutputRoleReport)
	}
	if so.ContentType != "application/json" || so.Format != "json" {
		t.Errorf("ContentType/Format = %q/%q, want application/json/json", so.ContentType, so.Format)
	}

	var rep convert.SanitizeReport
	if err := json.Unmarshal(so.Body, &rep); err != nil {
		t.Fatalf("report body does not parse as a SanitizeReport: %v\nbody=%s", err, so.Body)
	}
	if len(rep.Removed) == 0 {
		t.Error("report carries an empty removed list after a successful sanitize")
	}
	if len(rep.Verified) == 0 {
		t.Error("report carries an empty verified list -- the removal ran and nothing confirmed it")
	}
	if rep.GuaranteeScope == "" {
		t.Error("report's guarantee_scope is empty: it points nowhere")
	}
}

// TestProbeSideOutputsFailsWhenTheReportIsMissing pins the fail-closed half.
// A missing sidecar must not produce a job that succeeds with a sanitized file
// and no statement of what was removed from it.
func TestProbeSideOutputsFailsWhenTheReportIsMissing(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.pdf")
	if err := os.WriteFile(out, []byte("%PDF-1.4\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := probeSideOutputs(context.Background(), &jobs.Job{}, out); err == nil {
		t.Fatal("a missing report sidecar produced no error -- the job would go green with " +
			"a sanitized file and no report")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("error does not name the missing file: %v", err)
	}
}

// TestOnlyProbeDeclaresSideOutputs keeps the hook from spreading by accident.
// Seven engines have nothing to report alongside their result, and the day one
// of them grows a side output should be a deliberate edit here.
func TestOnlyProbeDeclaresSideOutputs(t *testing.T) {
	for _, d := range []struct {
		name string
		desc EngineDescriptor
	}{
		{"image", ImageDescriptor},
		{"document", DocumentDescriptor},
		{"html", HTMLDescriptor},
		{"audio", AudioDescriptor},
		{"av", AVDescriptor},
		{"ocr", OCRDescriptor},
		{"pandoc", PandocDescriptor},
	} {
		if d.desc.SideOutputs != nil {
			t.Errorf("%s declares SideOutputs; only probe should", d.name)
		}
	}
	if ProbeDescriptor.SideOutputs == nil {
		t.Error("ProbeDescriptor declares no SideOutputs -- the sanitize report would never be stored")
	}
}

// TestNewRunnerInstallsSideOutputs closes the gap between declaring the hook
// and it actually running. ProbeDescriptor.SideOutputs being non-nil proves
// nothing on its own: NewRunner is what copies it onto the Handler that
// process() reads, and dropping that one line would leave every test above
// green while no report was ever stored.
func TestNewRunnerInstallsSideOutputs(t *testing.T) {
	h := &Handler{}
	NewRunner(h, ProbeDescriptor)
	if h.sideOutputs == nil {
		t.Fatal("NewRunner did not install ProbeDescriptor.SideOutputs onto the Handler -- " +
			"the descriptor declares the hook and nothing would call it")
	}

	other := &Handler{}
	NewRunner(other, PandocDescriptor)
	if other.sideOutputs != nil {
		t.Error("NewRunner installed a side-output hook for pandoc, which declares none")
	}
}

// TestProbeTerminalClassification pins the branch-by-branch argument in
// ProbeDescriptor's doc comment. It is the classification most likely to be
// "simplified" into a copy of a neighbour's, and the polarity split is the
// whole content of the decision.
func TestProbeTerminalClassification(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		terminal bool
		why      string
	}{
		{"residue", convert.ErrSanitizeResidue, true,
			"deterministic on the output, which is deterministic on the input"},
		{"unverifiable", convert.ErrSanitizeUnverifiable, true,
			"the verifier will reach the same verdict on retry"},
		{"timeout", context.DeadlineExceeded, false,
			"depends on load, not on the file -- the opposite of isPandocTerminal"},
		{"malformed input", convert.ErrSanitizeMalformedInput, true,
			"the engine cannot parse this file now and will not in twenty seconds"},
		// The wrapped forms the sanitizer actually produces, because
		// errors.Is has to reach through them.
		{"wrapped malformed input", fmt.Errorf("sanitize pdf: %w: ps2write pass: exit 1",
			convert.ErrSanitizeMalformedInput), true,
			"classification must survive wrapping"},
		{"wrapped timeout", fmt.Errorf("sanitize pdf: pdfwrite pass: %w",
			context.DeadlineExceeded), false,
			"a wrapped deadline stays transient -- classifyGSFailure keeps it out of the " +
				"malformed branch"},
	}
	for _, tc := range cases {
		if got := isProbeTerminal(tc.err); got != tc.terminal {
			t.Errorf("isProbeTerminal(%s) = %v, want %v -- %s", tc.name, got, tc.terminal, tc.why)
		}
	}
	if isProbeTerminal(nil) {
		t.Error("isProbeTerminal(nil) = true")
	}

	// The two verification outcomes are both terminal and must still be
	// distinguishable to a client: one says the service is at fault, the
	// other says the service refuses to make a claim.
	_, residueMsg := probeFailureCode(convert.ErrSanitizeResidue)
	_, unverifiedMsg := probeFailureCode(convert.ErrSanitizeUnverifiable)
	_, malformedMsg := probeFailureCode(convert.ErrSanitizeMalformedInput)
	if malformedMsg == residueMsg || malformedMsg == unverifiedMsg {
		t.Errorf("a malformed INPUT reports the same message as a verification outcome (%q) -- "+
			"one says the client's file is broken, the others say ours is", malformedMsg)
	}
	if residueMsg == unverifiedMsg {
		t.Errorf("both verification outcomes report the same message %q -- a client cannot tell "+
			"\"still contained something\" from \"could not be checked\"", residueMsg)
	}
}
