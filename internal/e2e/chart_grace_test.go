package e2e

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Kubernetes must outlive asynq, on every worker.
//
// Each engine worker sets asynq ShutdownTimeout = <ENGINE>_ENGINE_TIMEOUT + 10s
// so a job in flight at SIGTERM finishes instead of being aborted and requeued.
// That only means anything if the POD survives that long: the grace period is
// when Kubernetes sends SIGKILL, and a grace equal to ShutdownTimeout kills the
// process at the instant asynq is still allowed to be finishing -- with nothing
// left for signal delivery, the final MarkDone, or the metrics server's own
// shutdown. The job is then abandoned mid-flight and waits for the reconciler.
//
// The fleet's older six workers all carry ShutdownTimeout + 20s. Phase 63's
// probe worker and phase 64's tectonic worker were both written with grace
// EQUAL to ShutdownTimeout -- the same slip twice, found in review (PR #16,
// codex round 2) on the newer of the two. This test is why there will not be a
// third: every worker is checked, and a worker missing from the table below
// fails rather than being skipped.
func TestChartGraceOutlivesAsynqShutdown(t *testing.T) {
	// chart values key -> the ConfigMap var its ShutdownTimeout derives from.
	// Listed rather than derived because the two names genuinely do not share a
	// stem (worker/ENGINE_TIMEOUT), but the LIST is checked for completeness
	// against values.yaml below.
	workers := map[string]string{
		"worker":         "ENGINE_TIMEOUT",
		"pandocWorker":   "PANDOC_ENGINE_TIMEOUT",
		"probeWorker":    "PROBE_ENGINE_TIMEOUT",
		"tectonicWorker": "TECTONIC_ENGINE_TIMEOUT",
	}
	// Not engine workers: they run no conversion and derive no ShutdownTimeout
	// from an engine timeout.
	exempt := map[string]bool{"api": true, "mcpHttp": true, "webhookWorker": true, "reconciler": true}

	// asynq's own margin, set identically by every cmd/*-worker.
	const asynqMargin = 10
	// What Kubernetes needs on top: signal delivery, the final status write and
	// the metrics server's shutdown. 10s is the fleet's own stated derivation --
	// pandocWorker's comment spells it out ("+ 10s Docker/k8s SIGKILL margin")
	// and every compose stop_grace_period uses it. The older six workers carry
	// 20s, which also passes; what must never be zero is the margin itself.
	const podMargin = 10

	grace := parseGracePeriods(t)
	timeouts := parseConfigMapTimeouts(t)

	for key := range grace {
		if _, ok := workers[key]; !ok && !exempt[key] {
			t.Errorf("values.yaml has a terminationGracePeriodSeconds for %q that this test does not "+
				"know about: add it to the table (with its engine timeout var) or to the exempt list, "+
				"because an unchecked worker is how this defect shipped twice", key)
		}
	}

	for key, envVar := range workers {
		got, ok := grace[key]
		if !ok {
			t.Errorf("no terminationGracePeriodSeconds for %s in values.yaml", key)
			continue
		}
		timeout, ok := timeouts[envVar]
		if !ok {
			t.Errorf("no %s in the ConfigMap template", envVar)
			continue
		}
		want := timeout + asynqMargin + podMargin
		if got < want {
			t.Errorf("%s.terminationGracePeriodSeconds = %d, want at least %d (%s=%ds + %ds asynq "+
				"ShutdownTimeout margin + %ds for signal delivery, the final status write and the "+
				"metrics server). At %d Kubernetes can SIGKILL while asynq is still finishing a job",
				key, got, want, envVar, timeout, asynqMargin, podMargin, got)
		}
	}
}

var (
	graceKeyRe   = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9]*):\s*$`)
	graceValueRe = regexp.MustCompile(`^  terminationGracePeriodSeconds:\s*(\d+)\s*$`)
	cmTimeoutRe  = regexp.MustCompile(`^  ([A-Z0-9_]*ENGINE_TIMEOUT):\s*"(\d+)s"\s*$`)
)

func parseGracePeriods(t *testing.T) map[string]int {
	t.Helper()
	raw, err := os.ReadFile("../../deploy/chart/octoconv/values.yaml")
	if err != nil {
		t.Fatalf("read values.yaml: %v", err)
	}
	out, current := map[string]int{}, ""
	for _, line := range strings.Split(string(raw), "\n") {
		if m := graceKeyRe.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		if m := graceValueRe.FindStringSubmatch(line); m != nil && current != "" {
			n, _ := strconv.Atoi(m[1])
			out[current] = n
		}
	}
	if len(out) < 5 {
		t.Fatalf("parsed only %d grace periods from values.yaml -- the parser is wrong", len(out))
	}
	return out
}

func parseConfigMapTimeouts(t *testing.T) map[string]int {
	t.Helper()
	raw, err := os.ReadFile("../../deploy/chart/octoconv/templates/configmap.yaml")
	if err != nil {
		t.Fatalf("read configmap.yaml: %v", err)
	}
	out := map[string]int{}
	for _, line := range strings.Split(string(raw), "\n") {
		if m := cmTimeoutRe.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[2])
			out[m[1]] = n
		}
	}
	if len(out) < 5 {
		t.Fatalf("parsed only %d engine timeouts from the ConfigMap -- the parser is wrong", len(out))
	}
	return out
}
