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

// filterByClient narrows an all-clients ListAllLimits result down to the
// rows belonging to id. The shared test database carries rows from other
// tests, so every all-clients assertion in this file filters down to its
// own fixture client(s) first rather than asserting on len(got) directly.
func filterByClient(rows []clients.LimitWithMeta, id uuid.UUID) []clients.LimitWithMeta {
	var out []clients.LimitWithMeta
	for _, row := range rows {
		if row.ClientID == id {
			out = append(out, row)
		}
	}
	return out
}

func TestListAllLimitsIncludesClientWithNoLimitsRow(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	id := newLimitsTestClient(t, r)

	all, err := r.ListAllLimits(ctx, &id)
	if err != nil {
		t.Fatalf("ListAllLimits: %v", err)
	}
	got := filterByClient(all, id)
	if len(got) != 1 {
		t.Fatalf("ListAllLimits(narrowed to client with no limits row) returned %d rows, want 1 -- dropping this row would make the operator endpoint silently omit unlimited clients, which is precisely the state OPSV-02 asks about", len(got))
	}
	if got[0].Engine != nil {
		t.Fatalf("Engine = %v, want nil for a client with no client_limits row", *got[0].Engine)
	}
	if got[0].ClientName == "" {
		t.Fatal("ClientName is empty, want the name set at Create")
	}
	if got[0].EnforcementMode != "observe" {
		t.Fatalf("EnforcementMode = %q, want %q (the 0013 column default)", got[0].EnforcementMode, "observe")
	}
}

func TestListAllLimitsReturnsOneRowPerEngine(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	id := newLimitsTestClient(t, r)

	if err := r.SetLimit(ctx, id, clients.Limit{Engine: "image", MaxConcurrent: intPtr(3)}); err != nil {
		t.Fatalf("SetLimit(image): %v", err)
	}
	if err := r.SetLimit(ctx, id, clients.Limit{Engine: "document", MaxDailyJobs: int64Ptr(10)}); err != nil {
		t.Fatalf("SetLimit(document): %v", err)
	}

	all, err := r.ListAllLimits(ctx, &id)
	if err != nil {
		t.Fatalf("ListAllLimits: %v", err)
	}
	got := filterByClient(all, id)
	if len(got) != 2 {
		t.Fatalf("ListAllLimits returned %d rows for client with 2 engines configured, want 2", len(got))
	}
	for _, row := range got {
		if row.Engine == nil {
			t.Fatal("Engine = nil, want non-nil for a configured (client, engine) row")
		}
	}
	// ORDER BY l.engine: "document" < "image" lexically.
	if *got[0].Engine != "document" || *got[1].Engine != "image" {
		t.Fatalf("ListAllLimits engines = [%s, %s], want [document, image]", *got[0].Engine, *got[1].Engine)
	}
}

func TestListAllLimitsPreservesNullVersusExplicitZero(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	id := newLimitsTestClient(t, r)

	if err := r.SetLimit(ctx, id, clients.Limit{
		Engine:        "image",
		MaxConcurrent: intPtr(0),
		MaxDailyBytes: nil,
		MaxDailyJobs:  int64Ptr(5),
	}); err != nil {
		t.Fatalf("SetLimit: %v", err)
	}

	all, err := r.ListAllLimits(ctx, &id)
	if err != nil {
		t.Fatalf("ListAllLimits: %v", err)
	}
	got := filterByClient(all, id)
	if len(got) != 1 {
		t.Fatalf("ListAllLimits returned %d rows, want 1", len(got))
	}
	row := got[0]
	if row.MaxConcurrent == nil {
		t.Fatal("MaxConcurrent = nil, want a non-nil pointer to 0 -- collapsing nil and 0 turns \"unlimited\" into \"denied\" (RESEARCH Pitfall 5)")
	}
	if *row.MaxConcurrent != 0 {
		t.Fatalf("MaxConcurrent = %d, want 0", *row.MaxConcurrent)
	}
	if row.MaxDailyBytes != nil {
		t.Fatalf("MaxDailyBytes = %v, want nil (unset)", *row.MaxDailyBytes)
	}
	if row.MaxDailyJobs == nil || *row.MaxDailyJobs != 5 {
		t.Fatalf("MaxDailyJobs = %v, want pointer to 5", row.MaxDailyJobs)
	}
}

func TestListAllLimitsCarriesEnforcementMode(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	id := newLimitsTestClient(t, r)

	if err := r.SetLimit(ctx, id, clients.Limit{Engine: "image", MaxConcurrent: intPtr(3)}); err != nil {
		t.Fatalf("SetLimit: %v", err)
	}
	if err := r.SetEnforcementMode(ctx, id, "enforce"); err != nil {
		t.Fatalf("SetEnforcementMode: %v", err)
	}

	all, err := r.ListAllLimits(ctx, &id)
	if err != nil {
		t.Fatalf("ListAllLimits: %v", err)
	}
	got := filterByClient(all, id)
	if len(got) == 0 {
		t.Fatal("ListAllLimits returned no rows for client")
	}
	for _, row := range got {
		if row.EnforcementMode != "enforce" {
			t.Fatalf("EnforcementMode = %q, want %q", row.EnforcementMode, "enforce")
		}
	}
}

func TestListAllLimitsNarrowingMatchesUnnarrowed(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	idA := newLimitsTestClient(t, r)
	idB := newLimitsTestClient(t, r)

	if err := r.SetLimit(ctx, idA, clients.Limit{Engine: "image", MaxConcurrent: intPtr(7)}); err != nil {
		t.Fatalf("SetLimit(A): %v", err)
	}
	if err := r.SetLimit(ctx, idB, clients.Limit{Engine: "document", MaxDailyJobs: int64Ptr(20)}); err != nil {
		t.Fatalf("SetLimit(B): %v", err)
	}

	narrowed, err := r.ListAllLimits(ctx, &idA)
	if err != nil {
		t.Fatalf("ListAllLimits(narrowed): %v", err)
	}
	for _, row := range narrowed {
		if row.ClientID != idA {
			t.Fatalf("narrowed ListAllLimits(&idA) returned row for ClientID %s, want only %s", row.ClientID, idA)
		}
	}

	unnarrowed, err := r.ListAllLimits(ctx, nil)
	if err != nil {
		t.Fatalf("ListAllLimits(nil): %v", err)
	}
	gotA := filterByClient(unnarrowed, idA)
	gotB := filterByClient(unnarrowed, idB)
	if len(gotA) == 0 {
		t.Fatal("unnarrowed ListAllLimits(nil) is missing rows for client A")
	}
	if len(gotB) == 0 {
		t.Fatal("unnarrowed ListAllLimits(nil) is missing rows for client B")
	}

	// A's rows must be field-for-field identical (pointer VALUES, not
	// identity) between the narrowed and unnarrowed calls -- this pins
	// D-01's "same query, one optional bind" contract.
	if len(narrowed) != len(gotA) {
		t.Fatalf("narrowed call returned %d rows for A, unnarrowed call returned %d rows for A -- want identical counts", len(narrowed), len(gotA))
	}
	for i := range narrowed {
		n, u := narrowed[i], gotA[i]
		if n.ClientID != u.ClientID || n.ClientName != u.ClientName || n.EnforcementMode != u.EnforcementMode {
			t.Fatalf("row %d differs between narrowed and unnarrowed: %+v vs %+v", i, n, u)
		}
		if (n.Engine == nil) != (u.Engine == nil) || (n.Engine != nil && *n.Engine != *u.Engine) {
			t.Fatalf("row %d Engine differs between narrowed and unnarrowed: %v vs %v", i, n.Engine, u.Engine)
		}
		if (n.MaxConcurrent == nil) != (u.MaxConcurrent == nil) || (n.MaxConcurrent != nil && *n.MaxConcurrent != *u.MaxConcurrent) {
			t.Fatalf("row %d MaxConcurrent differs between narrowed and unnarrowed: %v vs %v", i, n.MaxConcurrent, u.MaxConcurrent)
		}
		if (n.MaxDailyBytes == nil) != (u.MaxDailyBytes == nil) || (n.MaxDailyBytes != nil && *n.MaxDailyBytes != *u.MaxDailyBytes) {
			t.Fatalf("row %d MaxDailyBytes differs between narrowed and unnarrowed: %v vs %v", i, n.MaxDailyBytes, u.MaxDailyBytes)
		}
		if (n.MaxDailyJobs == nil) != (u.MaxDailyJobs == nil) || (n.MaxDailyJobs != nil && *n.MaxDailyJobs != *u.MaxDailyJobs) {
			t.Fatalf("row %d MaxDailyJobs differs between narrowed and unnarrowed: %v vs %v", i, n.MaxDailyJobs, u.MaxDailyJobs)
		}
	}
}
