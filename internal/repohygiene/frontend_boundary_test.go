package repohygiene_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestBackendGateRejectsFrontendStep(t *testing.T) {
	dir := t.TempDir()
	// Protect against execution before the old CLI has rejected the retired step.
	if err := os.WriteFile(filepath.Join(dir, "76-11-final-gate.txt"), []byte("sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "../../scripts/final-live-gate.sh", "--evidence-dir", dir, "--steps", "frontend")
	output, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 64 {
		t.Fatalf("expected rejected step (64), got %v: %s", err, output)
	}
	if !strings.Contains(string(output), "unknown step: frontend") {
		t.Fatalf("backend gate must reject the external UI step before running anything: %s", output)
	}
}

func TestBackendGateHelpHasNoRequiredFrontend(t *testing.T) {
	output, err := exec.Command("bash", "../../scripts/final-live-gate.sh").CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 64 {
		t.Fatalf("expected usage (64), got %v: %s", err, output)
	}
	match := regexp.MustCompile(`--steps ([a-z,-]+)`).FindStringSubmatch(string(output))
	if len(match) != 2 || strings.Contains(match[1], "frontend") {
		t.Fatalf("backend gate must advertise backend-only steps: %s", output)
	}
}
