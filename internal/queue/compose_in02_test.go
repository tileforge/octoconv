package queue

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// IN-02, mechanically.
//
// Every process that constructs a queue.Client derives each engine's per-job
// asynq.Unique lock TTL from that engine's OWN timeout and retry budget
// (<Engine>UniqueTTL, computed in NewClient below). The derivation happens in
// the ENQUEUING process, so two services that disagree about
// TECTONIC_ENGINE_TIMEOUT do not produce an error anywhere -- they produce two
// different lock TTLs for the same job, and the shorter one lets a duplicate
// through. Duplicate processing is the failure, and it is silent.
//
// Seven compose services construct a client, each carrying the full set of
// engine vars by hand. Seven engine classes x two vars is fourteen values
// replicated seven times, maintained by copying: the exact shape where the
// next engine gets added to every service but one and the miss is found in
// production. This file is the mechanism that makes that impossible to ship,
// and it is deliberately DERIVED rather than listed -- the var names come from
// NewClient's own source, so an engine added there without touching compose
// fails here by name.

const (
	composePath = "../../docker-compose.yml"
	clientPath  = "client.go"
)

var (
	// The two forms NewClient reads: envInt("X_MAX_RETRY", ...) and
	// envDuration("X_ENGINE_TIMEOUT", ...).
	envReadRe   = regexp.MustCompile(`env(?:Int|Duration)\("([A-Z0-9_]+)"`)
	engineVarRe = regexp.MustCompile(`^([A-Z0-9]+_)?(ENGINE_TIMEOUT|MAX_RETRY)$`)
	// A compose service key: exactly two spaces of indent under `services:`.
	composeServiceRe = regexp.MustCompile(`^  ([a-z0-9][a-z0-9._-]*):\s*$`)
	// An environment entry inside a service: six spaces, KEY: value.
	composeEnvRe = regexp.MustCompile(`^      ([A-Z0-9_]+):`)
)

// engineVarsReadByNewClient extracts the per-engine var names NewClient reads,
// from client.go itself. Reading the source rather than restating the list is
// the whole point: a restated list is a second copy of the thing under test.
func engineVarsReadByNewClient(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile(clientPath)
	if err != nil {
		t.Fatalf("read %s: %v", clientPath, err)
	}
	seen := map[string]bool{}
	var vars []string
	for _, m := range envReadRe.FindAllStringSubmatch(string(src), -1) {
		name := m[1]
		if !engineVarRe.MatchString(name) || seen[name] {
			continue
		}
		seen[name] = true
		vars = append(vars, name)
	}
	sort.Strings(vars)
	// Vacuity guard. A regex that matched nothing would make every assertion
	// below pass while proving nothing -- the failure mode this project has
	// recorded more than once ("a scan of nothing is never clean"). Seven
	// engine classes x 2 vars is the floor, not an estimate.
	if len(vars) < 14 {
		t.Fatalf("only %d engine vars found in %s (%v) -- the extraction regex has drifted and every "+
			"assertion below would be vacuous", len(vars), clientPath, vars)
	}
	return vars
}

// composeServiceEnvKeys returns, per compose service, the set of environment
// KEYS it declares.
func composeServiceEnvKeys(t *testing.T) map[string]map[string]bool {
	t.Helper()
	b, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("read %s: %v", composePath, err)
	}
	out := map[string]map[string]bool{}
	current := ""
	for _, line := range strings.Split(string(b), "\n") {
		if m := composeServiceRe.FindStringSubmatch(line); m != nil {
			current = m[1]
			out[current] = map[string]bool{}
			continue
		}
		if current == "" {
			continue
		}
		if m := composeEnvRe.FindStringSubmatch(line); m != nil {
			out[current][m[1]] = true
		}
	}
	if len(out) < 10 {
		t.Fatalf("only %d services parsed out of %s -- the compose scan has drifted", len(out), composePath)
	}
	return out
}

// TestComposeReplicatesEveryEngineVarToEveryQueueClientService is the IN-02
// guard. It does not know which services construct a queue.Client -- it infers
// it: a service that declares ANY of NewClient's engine vars is one, and must
// therefore declare ALL of them.
//
// That inference is what makes the test survive the tenth engine. A hardcoded
// service list would have to be edited by the same person who forgot to edit
// compose.
func TestComposeReplicatesEveryEngineVarToEveryQueueClientService(t *testing.T) {
	vars := engineVarsReadByNewClient(t)
	services := composeServiceEnvKeys(t)

	enqueuing := 0
	for name, keys := range services {
		carries := 0
		for _, v := range vars {
			if keys[v] {
				carries++
			}
		}
		if carries == 0 {
			continue // not a queue.NewClient() service
		}
		enqueuing++
		var missing []string
		for _, v := range vars {
			if !keys[v] {
				missing = append(missing, v)
			}
		}
		if len(missing) > 0 {
			t.Errorf("compose service %q constructs a queue client (it declares %d of %d engine vars) but is "+
				"missing %v -- the unique-lock TTL is derived in the ENQUEUING process, so this does not error, "+
				"it silently produces duplicate processing", name, carries, len(vars), missing)
		}
	}
	if enqueuing < 7 {
		t.Fatalf("only %d compose services look like queue clients -- expected the full fleet; this test "+
			"would be checking almost nothing", enqueuing)
	}
}

// TestEveryEngineVarIsDocumentedInEnvExample pins the other half of the same
// invariant. .env.example is where an operator learns a knob exists at all; a
// var read by NewClient and absent there is a bound nobody knows they can set,
// and it is how TECTONIC_ENGINE_TIMEOUT would ship with an operator unable to
// find the number 64-05 is going to measure.
func TestEveryEngineVarIsDocumentedInEnvExample(t *testing.T) {
	vars := engineVarsReadByNewClient(t)
	b, err := os.ReadFile("../../.env.example")
	if err != nil {
		t.Fatalf("read .env.example: %v", err)
	}
	text := string(b)
	for _, v := range vars {
		if !strings.Contains(text, "\n"+v+"=") {
			t.Errorf(".env.example has no %s= line, but queue.NewClient reads it", v)
		}
	}
}
