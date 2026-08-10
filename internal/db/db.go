// Package db provides the PostgreSQL connection pool and migration runner.
//
// Two pools, two purposes, two sizes (D-06a, Phase 58): Connect returns the
// MAIN pool that every jobs/presets/clients repository reads and writes
// through, including Repo.PreAdmit -- the quota pre-check a burst of
// synchronous POST /v1/inspect requests calls on every request (INSP-04).
// ConnectHealth returns a second, deliberately tiny pool used for EXACTLY
// ONE thing: the Postgres ping inside GET /healthz. Before this phase there
// was only one pool, and /healthz's ping shared it with every other
// Postgres consumer; a burst of concurrent inspect requests exhausting the
// main pool's connections/wait queue could delay that ping past its 3s
// timeout and fail liveness no matter which HTTP listener /healthz answers
// on (58-CONTEXT.md D-06a, 58-RESEARCH.md Pitfall 1). Moving /healthz's
// HTTP path onto its own listener (internal/api/routes.go HealthRoutes,
// Plan 58-03 Task 2) closes the ACCEPT-PATH half of that risk; giving its
// Postgres ping its own pool closes the DEPENDENCY half -- neither closes
// the other. See Connect's and ConnectHealth's own doc comments for the
// arithmetic and the caveats.
package db

import (
	"context"
	"embed"
	"fmt"
	"os"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// defaultMaxConns is Connect's fallback when DB_MAX_CONNS is unset or
// unparseable (D-06a). The arithmetic this number must satisfy: DB_MAX_CONNS
// must exceed the concurrent-inspect ceiling (INSPECT_MAX_CONCURRENT,
// introduced by Plan 58-06, default 4) with headroom left over for ordinary
// /v1/jobs traffic sharing the SAME pool -- 10 = 4 (inspect) + 6 (job
// create/list/get, preset CRUD, the reconciler, and everything else that
// reads/writes through this pool). What breaks if this arithmetic is
// violated: a burst of INSPECT_MAX_CONCURRENT concurrent inspect requests,
// each calling Repo.PreAdmit (internal/jobs/admission.go:596) against THIS
// pool, checks out up to INSPECT_MAX_CONCURRENT connections; if DB_MAX_CONNS
// is not comfortably above that number, every OTHER consumer of this pool
// queues behind the burst waiting for a connection and times out -- this is
// a real, measured contention path (58-RESEARCH.md Pitfall 1), not a
// hypothetical one. Before this phase MaxConns was never set at all, so
// pgxpool silently defaulted to max(4, runtime.NumCPU()) -- the NODE's core
// count, not a number anyone chose, and doubly meaningless because the api
// container has never carried a CPU resources.limits either (closed by
// Plan 58-03 Task 3, D-06b) -- so that inherited default was accidental
// sizing, never deliberate sizing.
const defaultMaxConns = 10

// Connect opens the MAIN pgx pool using the DATABASE_URL environment
// variable, sized by DB_MAX_CONNS (see defaultMaxConns's doc comment for
// the arithmetic that default must satisfy). This is the pool every
// jobs/presets/clients repository reads and writes through, INCLUDING
// Repo.PreAdmit. Do not also point api.HealthDeps.Postgres at this pool's
// return value -- that is exactly the pool-contention path D-06a closes;
// use ConnectHealth for that field instead.
func Connect(ctx context.Context) (*pgxpool.Pool, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL must be set")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}
	cfg.MaxConns = envInt32("DB_MAX_CONNS", defaultMaxConns)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

// healthPoolMaxConns / healthPoolMinConns size the dedicated health-ping
// pool ConnectHealth returns (D-06a). MinConns=1 keeps one connection warm
// so the ping never pays connection-establishment latency at exactly the
// moment the main pool is under pressure; MaxConns=2 gives headroom for one
// in-flight ping plus the pgxpool background health-check ping without
// either blocking the other. Two connections total, not sized against
// INSPECT_MAX_CONCURRENT at all -- this pool's whole point is that it is
// NEVER touched by inspect traffic.
const (
	healthPoolMaxConns = 2
	healthPoolMinConns = 1
)

// ConnectHealth opens a SECOND, small, dedicated pgx pool from the same
// DATABASE_URL, used for exactly one purpose: the Postgres ping inside
// GET /healthz (OBS-02, D-16/D-17 from earlier phases; D-06a this phase).
//
// This pool is a CORRECTION, not an enhancement: an earlier design
// discussion in this project assumed "moving /healthz onto its own HTTP
// listener isolates it completely." That claim was wrong. handleHealth
// pings Postgres (internal/api/handlers.go:94) through whatever
// *pgxpool.Pool sits in api.HealthDeps.Postgres. Before this pool existed,
// that field held the SAME pool Repo.PreAdmit reads through on every
// synchronous inspect request -- so a burst of concurrent inspect requests
// could exhaust that pool's connections/wait queue and delay the health
// ping past its 3s timeout, failing liveness, regardless of which port
// /healthz's HTTP handler answered on (58-RESEARCH.md Pitfall 1). Giving
// the ping its own pool removes the shared resource entirely: it does not
// rely on Connect's DB_MAX_CONNS sizing being generous enough, and
// Connect's sizing does not rely on this pool either -- D-06a requires
// closing this path explicitly, and both mechanisms ship together rather
// than choosing one.
//
// The Postgres ping stays inside the LIVENESS probe rather than moving to
// readiness (OBS-02, D-16 from an earlier phase) -- that is existing,
// unchanged behavior this phase does not revisit. This pool removes the
// reason the ping could FALSELY fail under inspect load; it does not
// change what the ping means or when it runs.
func ConnectHealth(ctx context.Context) (*pgxpool.Pool, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL must be set")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres config (health pool): %w", err)
	}
	cfg.MaxConns = healthPoolMaxConns
	cfg.MinConns = healthPoolMinConns
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres (health pool): %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres (health pool): %w", err)
	}
	return pool, nil
}

// envInt32 mirrors cmd/api's firstField/envInt64 tolerant-parsing semantics
// (this repo's project-wide convention for env-sourced integers) --
// duplicated here in miniature rather than imported, since cmd/api cannot
// be imported from internal/db without an import cycle
// (cmd/* -> internal/db). A missing or unparseable value returns def rather
// than failing startup: DB_MAX_CONNS is a tuning knob, not a required
// credential.
func envInt32(key string, def int32) int32 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	// Tolerate trailing inline comments / whitespace from .env files, same
	// as cmd/api's firstField helper.
	field := v
	for i := 0; i < len(v); i++ {
		if v[i] == ' ' || v[i] == '\t' {
			field = v[:i]
			break
		}
	}
	n, err := strconv.ParseInt(field, 10, 32)
	if err != nil {
		return def
	}
	return int32(n)
}

// Migrate applies all embedded SQL migrations in lexical order. Each migration
// runs once; applied versions are tracked in the schema_migrations table.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, name,
		).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if exists {
			continue
		}

		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`, name,
		); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}
	return nil
}
