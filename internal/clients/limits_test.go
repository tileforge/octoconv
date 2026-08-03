package clients_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/apaderin/octoconv/internal/clients"
	"github.com/apaderin/octoconv/internal/db"
	"github.com/google/uuid"
)

// newLimitsTestClient inserts a bare client fixture with no real API key —
// limits tests only need a valid client_id to key rows on, matching the
// createTestClient shape used by internal/jobs/repo_test.go.
func newLimitsTestClient(t *testing.T, r *clients.Repo) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id, err := r.Create(ctx, "test-limits-client-"+uuid.NewString(), "unused-hash-"+uuid.NewString())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return id
}

func intPtr(v int) *int       { return &v }
func int64Ptr(v int64) *int64 { return &v }

func TestLimitAbsentRowIsUnlimited(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	id := newLimitsTestClient(t, r)

	got, err := r.GetLimit(ctx, id, "image")
	if err != nil {
		t.Fatalf("GetLimit: %v", err)
	}
	if got != nil {
		t.Fatalf("GetLimit on absent row = %+v, want nil", got)
	}
}

func TestLimitSetAllNilRoundTrips(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	id := newLimitsTestClient(t, r)

	if err := r.SetLimit(ctx, id, clients.Limit{Engine: "image"}); err != nil {
		t.Fatalf("SetLimit: %v", err)
	}

	got, err := r.GetLimit(ctx, id, "image")
	if err != nil {
		t.Fatalf("GetLimit: %v", err)
	}
	if got == nil {
		t.Fatal("GetLimit after SetLimit(all nil) = nil, want a row with all-nil columns")
	}
	if got.MaxConcurrent != nil || got.MaxDailyBytes != nil || got.MaxDailyJobs != nil {
		t.Fatalf("GetLimit = %+v, want all three columns nil", got)
	}
}

func TestLimitMaxConcurrentZeroIsNotNil(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	id := newLimitsTestClient(t, r)

	if err := r.SetLimit(ctx, id, clients.Limit{Engine: "image", MaxConcurrent: intPtr(0)}); err != nil {
		t.Fatalf("SetLimit: %v", err)
	}

	got, err := r.GetLimit(ctx, id, "image")
	if err != nil {
		t.Fatalf("GetLimit: %v", err)
	}
	if got == nil || got.MaxConcurrent == nil {
		t.Fatalf("GetLimit = %+v, want non-nil MaxConcurrent", got)
	}
	if *got.MaxConcurrent != 0 {
		t.Fatalf("MaxConcurrent = %d, want 0", *got.MaxConcurrent)
	}
}

func TestLimitSetTwiceReplacesNotMerges(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	id := newLimitsTestClient(t, r)

	if err := r.SetLimit(ctx, id, clients.Limit{
		Engine:        "image",
		MaxConcurrent: intPtr(5),
		MaxDailyBytes: int64Ptr(1000),
		MaxDailyJobs:  int64Ptr(50),
	}); err != nil {
		t.Fatalf("SetLimit (1): %v", err)
	}

	// Second call omits MaxDailyBytes/MaxDailyJobs (nil) — a merge would
	// leave the old values in place; a full-row replace clears them.
	if err := r.SetLimit(ctx, id, clients.Limit{
		Engine:        "image",
		MaxConcurrent: intPtr(2),
	}); err != nil {
		t.Fatalf("SetLimit (2): %v", err)
	}

	got, err := r.GetLimit(ctx, id, "image")
	if err != nil {
		t.Fatalf("GetLimit: %v", err)
	}
	if got == nil {
		t.Fatal("GetLimit = nil after second SetLimit")
	}
	if got.MaxConcurrent == nil || *got.MaxConcurrent != 2 {
		t.Fatalf("MaxConcurrent = %v, want pointer to 2", got.MaxConcurrent)
	}
	if got.MaxDailyBytes != nil {
		t.Fatalf("MaxDailyBytes = %v, want nil (second call should have cleared it)", got.MaxDailyBytes)
	}
	if got.MaxDailyJobs != nil {
		t.Fatalf("MaxDailyJobs = %v, want nil (second call should have cleared it)", got.MaxDailyJobs)
	}
}

func TestClearLimit(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	id := newLimitsTestClient(t, r)

	if err := r.SetLimit(ctx, id, clients.Limit{Engine: "image", MaxConcurrent: intPtr(3)}); err != nil {
		t.Fatalf("SetLimit: %v", err)
	}
	if err := r.ClearLimit(ctx, id, "image"); err != nil {
		t.Fatalf("ClearLimit: %v", err)
	}

	got, err := r.GetLimit(ctx, id, "image")
	if err != nil {
		t.Fatalf("GetLimit: %v", err)
	}
	if got != nil {
		t.Fatalf("GetLimit after ClearLimit = %+v, want nil", got)
	}
}

func TestListLimits(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	id := newLimitsTestClient(t, r)

	if err := r.SetLimit(ctx, id, clients.Limit{Engine: "image", MaxConcurrent: intPtr(3)}); err != nil {
		t.Fatalf("SetLimit(image): %v", err)
	}
	if err := r.SetLimit(ctx, id, clients.Limit{Engine: "document", MaxDailyJobs: int64Ptr(10)}); err != nil {
		t.Fatalf("SetLimit(document): %v", err)
	}

	got, err := r.ListLimits(ctx, id)
	if err != nil {
		t.Fatalf("ListLimits: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListLimits returned %d rows, want 2", len(got))
	}
	// ORDER BY engine: "document" < "image" lexically.
	if got[0].Engine != "document" || got[1].Engine != "image" {
		t.Fatalf("ListLimits engines = [%s, %s], want [document, image]", got[0].Engine, got[1].Engine)
	}
}

func TestSetEnforcementMode(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	r := newTestRepo(t)
	ctx := context.Background()
	id := newLimitsTestClient(t, r)

	if err := r.SetEnforcementMode(ctx, id, "enforce"); err != nil {
		t.Fatalf("SetEnforcementMode(enforce): %v", err)
	}

	if err := r.SetEnforcementMode(ctx, id, "bogus"); err == nil {
		t.Fatal("SetEnforcementMode(bogus) = nil error, want a validation error")
	}

	// Stored value must be unchanged after the rejected call. This package
	// has no getter for enforcement_mode (it's not part of the Limit
	// interface), so read it back directly via a second pool connection —
	// legitimate here since this is a DATABASE_URL-gated integration test,
	// not production code.
	pool, err := db.Connect(ctx)
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}
	defer pool.Close()

	var mode string
	if err := pool.QueryRow(ctx, `SELECT enforcement_mode FROM clients WHERE id = $1`, id).Scan(&mode); err != nil {
		t.Fatalf("read back enforcement_mode: %v", err)
	}
	if mode != "enforce" {
		t.Fatalf("enforcement_mode = %q after rejected SetEnforcementMode(bogus), want %q (unchanged)", mode, "enforce")
	}
}

func TestSetEnforcementModeUnknownClient(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	err := r.SetEnforcementMode(ctx, uuid.New(), "enforce")
	if !errors.Is(err, clients.ErrNotFound) {
		t.Fatalf("SetEnforcementMode(random uuid) = %v, want ErrNotFound", err)
	}
}
