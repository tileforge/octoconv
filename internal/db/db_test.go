package db

import (
	"context"
	"os"
	"testing"
)

// skipUnlessDatabaseURLSet mirrors internal/jobs/repo_test.go's newTestRepo
// skip mechanics verbatim (58-03-PLAN.md Task 1's read_first requirement):
// both Connect and ConnectHealth Ping the server before returning (existing
// Connect behavior, preserved by this phase), so exercising either one for
// real requires a real Postgres reachable at DATABASE_URL.
func skipUnlessDatabaseURLSet(t *testing.T) {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
}

// TestConnect_MaxConnsFromEnv proves DB_MAX_CONNS sizes the main pool
// (D-06a): the whole point of an explicit, arithmetic-backed default is
// meaningless if the env override that arithmetic depends on doesn't
// actually take effect.
func TestConnect_MaxConnsFromEnv(t *testing.T) {
	skipUnlessDatabaseURLSet(t)
	t.Setenv("DB_MAX_CONNS", "7")

	pool, err := Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer pool.Close()

	if got := pool.Config().MaxConns; got != 7 {
		t.Fatalf("Config().MaxConns = %d, want 7 (DB_MAX_CONNS=7)", got)
	}
}

// TestConnect_MaxConnsDefaultWhenUnset proves the fallback is
// defaultMaxConns (10, documented arithmetic) rather than pgxpool's own
// max(4, runtime.NumCPU()) default -- the accidental, node-core-count-
// dependent sizing this phase replaces.
func TestConnect_MaxConnsDefaultWhenUnset(t *testing.T) {
	skipUnlessDatabaseURLSet(t)
	t.Setenv("DB_MAX_CONNS", "")

	pool, err := Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer pool.Close()

	if got := pool.Config().MaxConns; got != defaultMaxConns {
		t.Fatalf("Config().MaxConns = %d, want documented default %d", got, defaultMaxConns)
	}
}

// TestConnect_MaxConnsToleratesUnparseable proves DB_MAX_CONNS=abc does not
// fail startup -- the same tolerant-parsing semantics as cmd/api's
// envInt64/firstField, applied to this new knob.
func TestConnect_MaxConnsToleratesUnparseable(t *testing.T) {
	skipUnlessDatabaseURLSet(t)
	t.Setenv("DB_MAX_CONNS", "abc")

	pool, err := Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v (DB_MAX_CONNS=abc must not fail startup)", err)
	}
	defer pool.Close()

	if got := pool.Config().MaxConns; got != defaultMaxConns {
		t.Fatalf("Config().MaxConns = %d, want documented default %d when DB_MAX_CONNS is unparseable", got, defaultMaxConns)
	}
}

// TestConnectHealth_Sizing proves the dedicated health-ping pool is sized
// exactly 2/1 (MaxConns/MinConns), not left to any default.
func TestConnectHealth_Sizing(t *testing.T) {
	skipUnlessDatabaseURLSet(t)

	pool, err := ConnectHealth(context.Background())
	if err != nil {
		t.Fatalf("ConnectHealth: %v", err)
	}
	defer pool.Close()

	if got := pool.Config().MaxConns; got != healthPoolMaxConns {
		t.Fatalf("Config().MaxConns = %d, want %d", got, healthPoolMaxConns)
	}
	if got := pool.Config().MinConns; got != healthPoolMinConns {
		t.Fatalf("Config().MinConns = %d, want %d", got, healthPoolMinConns)
	}
}

// TestConnectHealth_DistinctFromConnect proves ConnectHealth returns a
// DIFFERENT *pgxpool.Pool than Connect -- the entire D-06a fix collapses to
// nothing if both calls end up sharing one pool under the hood.
func TestConnectHealth_DistinctFromConnect(t *testing.T) {
	skipUnlessDatabaseURLSet(t)

	mainPool, err := Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer mainPool.Close()

	healthPool, err := ConnectHealth(context.Background())
	if err != nil {
		t.Fatalf("ConnectHealth: %v", err)
	}
	defer healthPool.Close()

	if mainPool == healthPool {
		t.Fatalf("ConnectHealth returned the SAME *pgxpool.Pool as Connect -- D-06a requires two independent pools")
	}
}
