package main

// This suite proves cmd/api's health-pool wiring (D-06a, 58-CONTEXT.md) by
// scanning main.go's source rather than exercising behavior, mirroring
// internal/errcode/openapi_sync_test.go's doc-sync technique. The reason a
// unit test cannot do this job: whether the health-ping pool and the jobs
// repository land on the CORRECT sides of api.HealthDeps{...} and
// jobs.NewRepo(...) is entirely a fact about which Go identifier was typed
// into which struct-literal field in main() -- there is no exported
// behavior to assert against, only source structure. A regression here
// (Postgres: pool instead of Postgres: healthPool) silently re-opens the
// exact pool-contention path D-06a exists to close, and the second
// /healthz listener (internal/api/routes.go HealthRoutes) would not catch
// it -- HTTP-accept isolation and Postgres-pool isolation are independent
// claims, proven independently.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// mainSourcePath is relative to this package's own directory (cmd/api/),
// so it resolves regardless of the working directory `go test` is invoked
// from.
const mainSourcePath = "main.go"

// readMainSource reads cmd/api/main.go, failing loudly if it is missing --
// a missing source file is a broken build, not a skippable test.
func readMainSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(mainSourcePath)
	if err != nil {
		t.Fatalf("read %s: %v", mainSourcePath, err)
	}
	return string(b)
}

// TestConnectHealthCalledExactlyOnce proves cmd/api constructs the
// dedicated health-ping pool (db.ConnectHealth, D-06a) exactly once. Zero
// occurrences means the pool never exists at all; more than one means a
// second, redundant pool was introduced somewhere -- neither is the
// intended wiring.
func TestConnectHealthCalledExactlyOnce(t *testing.T) {
	src := readMainSource(t)
	got := strings.Count(src, "db.ConnectHealth(")
	if got != 1 {
		t.Fatalf("db.ConnectHealth( occurs %d times in %s, want exactly 1 -- D-06a requires the dedicated health-ping pool be constructed exactly once", got, mainSourcePath)
	}
}

// healthDepsPostgresFieldPattern matches the Postgres: field inside the
// api.HealthDeps{...} literal, capturing the identifier assigned to it.
var healthDepsPostgresFieldPattern = regexp.MustCompile(`Postgres:\s*(\w+)`)

// TestHealthDepsPostgresIsHealthPool proves the api.HealthDeps{...} literal
// wires Postgres: healthPool, never Postgres: pool. Returning pool here is
// EXACTLY the regression D-06a exists to prevent: it silently re-opens the
// pool-contention path a burst of inspect requests can exploit to fail
// /healthz's liveness ping, regardless of which listener answers the HTTP
// request (58-CONTEXT.md D-06a, 58-RESEARCH.md Pitfall 1).
func TestHealthDepsPostgresIsHealthPool(t *testing.T) {
	src := readMainSource(t)
	m := healthDepsPostgresFieldPattern.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("no `Postgres: <ident>` field found in %s inside the api.HealthDeps{...} literal", mainSourcePath)
	}
	if m[1] != "healthPool" {
		t.Fatalf("api.HealthDeps{Postgres: %s, ...} in %s, want Postgres: healthPool (D-06a) -- Postgres: pool reopens the pool-contention path a burst of inspect requests can exploit to fail /healthz's liveness ping regardless of which listener answers the HTTP request", m[1], mainSourcePath)
	}
}

// jobsNewRepoPattern matches the jobs.NewRepo(...) constructor call,
// capturing the identifier passed as its pool argument.
var jobsNewRepoPattern = regexp.MustCompile(`jobs\.NewRepo\((\w+)\)`)

// TestJobsRepoUsesMainPool proves jobs.NewRepo (and therefore
// Repo.PreAdmit, the quota check every synchronous inspect request calls,
// INSP-04) is constructed from the MAIN pool, never the small dedicated
// health pool. The health pool exists to serve exactly one caller (the
// /healthz Postgres ping); routing the jobs repository through it instead
// would starve it of the very headroom its MaxConns=2 sizing assumes it
// has to itself, and would leave the jobs repository sharing a 2-connection
// pool with every synchronous inspect request -- worse than the pre-Phase-58
// state this plan exists to fix.
func TestJobsRepoUsesMainPool(t *testing.T) {
	src := readMainSource(t)
	m := jobsNewRepoPattern.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("no jobs.NewRepo(<ident>) call found in %s", mainSourcePath)
	}
	if m[1] != "pool" {
		t.Fatalf("jobs.NewRepo(%s) in %s, want jobs.NewRepo(pool) -- the jobs repository (and Repo.PreAdmit, INSP-04's quota check) must stay on the MAIN pool, never the dedicated health pool (D-06a)", m[1], mainSourcePath)
	}
}

func TestEveryAPIListenerIsBuiltByTheConstructor(t *testing.T) {
	src := readMainSource(t)
	literals := strings.Count(src, "&http.Server{")
	calls := strings.Count(src, "api.NewHTTPServer(")
	if literals != 0 || calls != 4 {
		t.Fatalf("%s has %d &http.Server{ literals and %d api.NewHTTPServer( calls, want 0 and 4: every listener of the api process (API_ADDR, METRICS_ADDR, HEALTH_ADDR, and the worker listener) must carry the header and idle bounds of api.NewHTTPServer (D-25)", mainSourcePath, literals, calls)
	}
}
