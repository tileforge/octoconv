package jobs

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestListGlobalSearchAndSort(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	client := createTestClient(t, r)
	other := createTestClient(t, r)
	wanted := make([]uuid.UUID, 0, 13)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 25; i++ {
		name := fmt.Sprintf("other-%02d.png", i)
		if i%2 == 0 {
			name = fmt.Sprintf(`Report_%%_\-%02d.PNG`, i)
		}
		id, err := r.Create(ctx, CreateParams{ClientID: client, Operation: "convert", Engine: "image", SourceFormat: "png", TargetFormat: "webp", Input: Input{Filename: name, ObjectKey: "test-search", Format: "png"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.pool.Exec(ctx, "UPDATE jobs SET created_at=$1 WHERE id=$2", base.Add(time.Duration(i)*time.Second), id); err != nil {
			t.Fatal(err)
		}
		if i%2 == 0 {
			wanted = append(wanted, id)
		}
	}
	_ = createTestJob(t, r, other, "image")
	for _, sort := range []string{"newest", "oldest"} {
		t.Run(sort, func(t *testing.T) {
			f := ListFilter{Query: `%_\`, Sort: sort}
			var cursor *Cursor
			seen := make([]uuid.UUID, 0, len(wanted))
			for {
				page, next, err := r.List(ctx, client, f, cursor, 3)
				if err != nil {
					t.Fatal(err)
				}
				for _, job := range page {
					if job.SourceFilename == "" {
						t.Fatal("missing historical filename")
					}
					seen = append(seen, job.ID)
				}
				if next == nil {
					break
				}
				decoded, err := DecodeCursor(EncodeCursor(*next))
				if err != nil {
					t.Fatal(err)
				}
				cursor = &decoded
				if len(seen) > 25 {
					t.Fatal("cursor repeated rows")
				}
			}
			if len(seen) != len(wanted) {
				t.Fatalf("got %d jobs, want %d", len(seen), len(wanted))
			}
			for i, id := range seen {
				j := i
				if sort == "newest" {
					j = len(wanted) - 1 - i
				}
				if id != wanted[j] {
					t.Fatalf("wrong order at %d", i)
				}
			}
		})
	}
	page, next, err := r.List(ctx, client, ListFilter{Query: "REPORT"}, nil, 2)
	if err != nil || len(page) != 2 || next == nil {
		t.Fatalf("case search: %v %v", page, err)
	}
	if _, _, err := r.List(ctx, client, ListFilter{Query: "other"}, next, 2); err == nil {
		t.Fatal("cursor query mismatch accepted")
	}
	if _, _, err := r.List(ctx, client, ListFilter{Query: "REPORT", Sort: "oldest"}, next, 2); err == nil {
		t.Fatal("cursor sort mismatch accepted")
	}
	id := wanted[0]
	got, _, err := r.List(ctx, client, ListFilter{Query: id.String()}, nil, 20)
	if err != nil || len(got) != 1 || got[0].ID != id {
		t.Fatalf("ID search: %+v %v", got, err)
	}
	foreign, _, err := r.List(ctx, other, ListFilter{Query: id.String()}, nil, 20)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("cross-client leak: %+v %v", foreign, err)
	}
}
func TestListSearchMissingDuplicateInputsAndTieOrder(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	client := createTestClient(t, r)
	a := createTestJob(t, r, client, "image")
	b := createTestJob(t, r, client, "image")
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := r.pool.Exec(ctx, "UPDATE jobs SET created_at=$1 WHERE client_id=$2", at, client); err != nil {
		t.Fatal(err)
	}
	if _, err := r.pool.Exec(ctx, "INSERT INTO job_inputs(job_id,ordinal,filename,object_key) VALUES($1,0,'duplicate.png','dup')", a); err != nil {
		t.Fatal(err)
	}
	if _, err := r.pool.Exec(ctx, "DELETE FROM job_inputs WHERE job_id=$1", b); err != nil {
		t.Fatal(err)
	}
	for _, sort := range []string{"newest", "oldest"} {
		first, next, err := r.List(ctx, client, ListFilter{Sort: sort}, nil, 1)
		if err != nil || len(first) != 1 || next == nil {
			t.Fatalf("first: %+v %v", first, err)
		}
		second, end, err := r.List(ctx, client, ListFilter{Sort: sort}, next, 1)
		if err != nil || len(second) != 1 || end != nil {
			t.Fatalf("second: %+v %v", second, err)
		}
		if first[0].ID == second[0].ID {
			t.Fatal("duplicate row from primary-input join")
		}
		if (sort == "oldest") != (first[0].ID.String() < second[0].ID.String()) {
			t.Fatal("incorrect tie order")
		}
	}
}
