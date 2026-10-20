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

func TestBackendGateToolCheckWithoutNode(t *testing.T) {
	source, err := os.ReadFile("../../scripts/final-live-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	// Execute the actual availability check in isolation: the rest of preflight
	// probes live Docker/Kubernetes services and does not belong in this test.
	loop := regexp.MustCompile(`(?s)\tfor t in [^\n]+; do\n.*?\n\tdone`).Find(source)
	if len(loop) == 0 {
		t.Fatal("required-tool check not found")
	}
	for _, missing := range []string{"", "docker"} {
		t.Run("missing_"+missing, func(t *testing.T) {
			dir := t.TempDir()
			for _, tool := range []string{"docker", "kubectl", "lsof", "jq", "perl", "shasum", "curl", "python3", "go", "git"} {
				if tool != missing {
					if err := os.WriteFile(filepath.Join(dir, tool), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			cmd := exec.Command("bash", "-c", "pf() { printf '%s\\n' \"$*\"; };\n"+string(loop))
			cmd.Env = []string{"PATH=" + dir}
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("tool check failed: %v: %s", err, output)
			}
			want := ""
			if missing != "" {
				want = "tool fail missing=" + missing
			}
			if got := strings.TrimSpace(string(output)); got != want {
				t.Fatalf("unexpected preflight result without Node/npm: got %q, want %q", got, want)
			}
		})
	}
}
