package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// createTestJob is a small list_test.go-local helper that creates a job for
// clientID with the given engine/status, returning its id. Status defaults
// to queued (Create's only status); callers that need active/done/failed
// transition afterward via the Repo's existing Mark* methods.
func createTestJob(t *testing.T, r *Repo, clientID uuid.UUID, engine string) uuid.UUID {
	t.Helper()
	id, err := r.Create(context.Background(), CreateParams{
		ClientID:     clientID,
		Operation:    "convert",
		Engine:       engine,
		SourceFormat: "png",
		TargetFormat: "webp",
		Input: Input{
			ObjectKey: "uploads/list-" + uuid.New().String() + "/0-in.png",
			Filename:  "in.png",
			Format:    "png",
		},
	})
	if err != nil {
		t.Fatalf("createTestJob: %v", err)
	}
	return id
}

// TestListClientScopeLeak pins JLIST-01's load-bearing cross-client-isolation
// guarantee at the SQL layer: client A's List call must never return any of
// client B's jobs, and vice versa. This is the REQUIRED two-client leak test
// (T-40-01) — proven, not inferred from the single-job 404 test.
func TestListClientScopeLeak(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	clientA := createTestClient(t, r)
	clientB := createTestClient(t, r)

	var aIDs, bIDs []uuid.UUID
	for i := 0; i < 3; i++ {
		aIDs = append(aIDs, createTestJob(t, r, clientA, "image"))
	}
	for i := 0; i < 3; i++ {
		bIDs = append(bIDs, createTestJob(t, r, clientB, "image"))
	}

	aPage, _, err := r.List(ctx, clientA, ListFilter{}, nil, 100)
	if err != nil {
		t.Fatalf("List(clientA): %v", err)
	}
	bPage, _, err := r.List(ctx, clientB, ListFilter{}, nil, 100)
	if err != nil {
		t.Fatalf("List(clientB): %v", err)
	}

	aSeen := map[uuid.UUID]bool{}
	for _, j := range aPage {
		aSeen[j.ID] = true
	}
	bSeen := map[uuid.UUID]bool{}
	for _, j := range bPage {
		bSeen[j.ID] = true
	}

	for _, id := range aIDs {
		if !aSeen[id] {
			t.Fatalf("client A's List is missing its own job %s", id)
		}
	}
	for _, id := range bIDs {
		if !bSeen[id] {
			t.Fatalf("client B's List is missing its own job %s", id)
		}
	}
	for _, id := range bIDs {
		if aSeen[id] {
			t.Fatalf("LEAK: client A's List returned client B's job %s", id)
		}
	}
	for _, id := range aIDs {
		if bSeen[id] {
			t.Fatalf("LEAK: client B's List returned client A's job %s", id)
		}
	}
}

// TestListKeysetStableUnderInsert proves paging is keyset-stable (not
// offset-based): fetching page 2 via the cursor returned by page 1 must not
// duplicate or skip any row, even when a NEW job for the same client is
// inserted between the two fetches.
func TestListKeysetStableUnderInsert(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	const limit = 3
	var created []uuid.UUID
	for i := 0; i < limit*2; i++ {
		created = append(created, createTestJob(t, r, clientID, "image"))
		time.Sleep(time.Millisecond) // ensure distinct created_at ordering
	}

	page1, cursor1, err := r.List(ctx, clientID, ListFilter{}, nil, limit)
	if err != nil {
		t.Fatalf("List page1: %v", err)
	}
	if len(page1) != limit {
		t.Fatalf("len(page1) = %d, want %d", len(page1), limit)
	}
	if cursor1 == nil {
		t.Fatalf("expected non-nil next cursor after page1 (more rows exist)")
	}

	// Insert a fresh job for the same client between page fetches.
	inserted := createTestJob(t, r, clientID, "image")

	page2, _, err := r.List(ctx, clientID, ListFilter{}, cursor1, 100)
	if err != nil {
		t.Fatalf("List page2: %v", err)
	}

	seen := map[uuid.UUID]int{}
	for _, j := range page1 {
		seen[j.ID]++
	}
	for _, j := range page2 {
		seen[j.ID]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Fatalf("job %s appeared %d times across page1+page2 (duplicate)", id, n)
		}
	}
	for _, id := range created {
		if seen[id] == 0 {
			t.Fatalf("job %s (created before paging started) missing from both pages (gap)", id)
		}
	}
	// The newly-inserted job (created strictly AFTER page1 was fetched) is
	// newer than everything in page1, so it must never appear in page2 — a
	// keyset page never grows to include rows newer than the cursor position.
	if seen[inserted] != 0 {
		t.Fatalf("job %s inserted after page1 must not appear in page2 (keyset instability)", inserted)
	}
}

// TestListFilters covers status and engine filters, singly and combined.
func TestListFilters(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	queuedImage := createTestJob(t, r, clientID, "image")

	activeImage := createTestJob(t, r, clientID, "image")
	if err := r.MarkActive(ctx, activeImage); err != nil {
		t.Fatalf("MarkActive activeImage: %v", err)
	}

	queuedDoc := createTestJob(t, r, clientID, "document")

	// status=queued -> queuedImage, queuedDoc (not activeImage)
	got, _, err := r.List(ctx, clientID, ListFilter{Status: StatusQueued}, nil, 100)
	if err != nil {
		t.Fatalf("List status=queued: %v", err)
	}
	gotIDs := idSet(got)
	if !gotIDs[queuedImage] || !gotIDs[queuedDoc] || gotIDs[activeImage] {
		t.Fatalf("status=queued filter wrong: got %+v", gotIDs)
	}

	// engine=image -> queuedImage, activeImage (not queuedDoc)
	got, _, err = r.List(ctx, clientID, ListFilter{Engine: "image"}, nil, 100)
	if err != nil {
		t.Fatalf("List engine=image: %v", err)
	}
	gotIDs = idSet(got)
	if !gotIDs[queuedImage] || !gotIDs[activeImage] || gotIDs[queuedDoc] {
		t.Fatalf("engine=image filter wrong: got %+v", gotIDs)
	}

	// status=queued AND engine=image -> only queuedImage
	got, _, err = r.List(ctx, clientID, ListFilter{Status: StatusQueued, Engine: "image"}, nil, 100)
	if err != nil {
		t.Fatalf("List status=queued engine=image: %v", err)
	}
	gotIDs = idSet(got)
	if len(gotIDs) != 1 || !gotIDs[queuedImage] {
		t.Fatalf("combined status+engine filter wrong: got %+v, want only %v", gotIDs, queuedImage)
	}
}

// TestListLimitProbe covers the limit+1 next-cursor probe: exactly `limit`
// matching rows yields a nil next cursor; limit+1 yields a non-nil one.
func TestListLimitProbe(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	const limit = 2
	for i := 0; i < limit; i++ {
		createTestJob(t, r, clientID, "image")
	}

	got, cursor, err := r.List(ctx, clientID, ListFilter{}, nil, limit)
	if err != nil {
		t.Fatalf("List (exact limit): %v", err)
	}
	if len(got) != limit {
		t.Fatalf("len(got) = %d, want %d", len(got), limit)
	}
	if cursor != nil {
		t.Fatalf("expected nil next cursor with exactly limit rows, got %+v", cursor)
	}

	// One more job pushes the total past limit.
	createTestJob(t, r, clientID, "image")

	got, cursor, err = r.List(ctx, clientID, ListFilter{}, nil, limit)
	if err != nil {
		t.Fatalf("List (limit+1): %v", err)
	}
	if len(got) != limit {
		t.Fatalf("len(got) = %d, want %d (probe row must be dropped)", len(got), limit)
	}
	if cursor == nil {
		t.Fatalf("expected non-nil next cursor with limit+1 rows")
	}
}

// TestListProgressNull covers progress_pct scanning as nil for freshly-
// created jobs (the column stays NULL until Phase 41 populates it).
func TestListProgressNull(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	id := createTestJob(t, r, clientID, "image")

	got, _, err := r.List(ctx, clientID, ListFilter{}, nil, 100)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found *JobSummary
	for i := range got {
		if got[i].ID == id {
			found = &got[i]
		}
	}
	if found == nil {
		t.Fatalf("job %s not found in listing", id)
	}
	if found.ProgressPct != nil {
		t.Fatalf("ProgressPct = %v, want nil for a freshly-created job", *found.ProgressPct)
	}
}

func idSet(js []JobSummary) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	for _, j := range js {
		out[j.ID] = true
	}
	return out
}
