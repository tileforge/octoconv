package main

import (
	"testing"
	"time"

	"github.com/tileforge/octoconv/internal/convert"
)

// TestEngineTimeoutsFromEnvCoversEveryEngine walks convert.AllEngines() -- the
// one list that grows with every engine class -- and requires this process to
// read a real, non-zero upper bound for each.
//
// api.NewServer merges defaultEngineTimeouts into whatever this call site
// supplies, so a missing engine no longer publishes estimated_max_seconds: 0
// (63-02 closed that structurally after PR #12 shipped it). What a missing
// engine still does is publish the API's OWN default while the matching worker
// runs on the operator's TECTONIC_ENGINE_TIMEOUT/OCR_ENGINE_TIMEOUT/... --
// the same divergence, one layer quieter, and invisible to every test at the
// api package layer because the merge makes the map look complete there.
func TestEngineTimeoutsFromEnvCoversEveryEngine(t *testing.T) {
	got := engineTimeoutsFromEnv()
	for _, engine := range convert.AllEngines() {
		d, ok := got[engine]
		if !ok {
			t.Errorf("engine %q is not read by cmd/api: /v1/inspect would publish api's own default for it "+
				"while %s's worker uses the operator's env value", engine, engine)
			continue
		}
		if d <= 0 {
			t.Errorf("engine %q: cmd/api reads %v, want a positive upper bound", engine, d)
		}
	}
	if len(got) != len(convert.AllEngines()) {
		t.Errorf("engineTimeoutsFromEnv() has %d entries, convert.AllEngines() has %d -- an entry for an "+
			"engine class that does not exist is as wrong as a missing one", len(got), len(convert.AllEngines()))
	}
}

// TestTectonicEngineTimeoutIsRead proves the tex ceiling this process
// publishes comes from TECTONIC_ENGINE_TIMEOUT. The API reads it as the
// protocol attempt budget.
func TestTectonicEngineTimeoutIsRead(t *testing.T) {
	if d := engineTimeoutsFromEnv()[convert.EngineTectonic]; d != 271*time.Second {
		t.Fatalf("default TECTONIC_ENGINE_TIMEOUT = %v, want 271s", d)
	}

	t.Setenv("TECTONIC_ENGINE_TIMEOUT", "900s")
	if d := engineTimeoutsFromEnv()[convert.EngineTectonic]; d != 900*time.Second {
		t.Fatalf("with TECTONIC_ENGINE_TIMEOUT=900s the API reads %v -- the operator's ceiling never "+
			"reaches the published estimate", d)
	}
}
