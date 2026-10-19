package jobs

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func createCADStageJob(t *testing.T, r *Repo, name string) uuid.UUID {
	t.Helper()
	id, err := r.Create(context.Background(), CreateParams{
		ClientID:  createTestClient(t, r),
		Operation: "render", Engine: "cad", SourceFormat: "step", TargetFormat: "png",
		Input: Input{ObjectKey: "uploads/stage-" + name + "/0-in.step", Filename: "in.step", Format: "step"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return id
}

func stageOf(j *Job) string {
	part := func(n *int) string {
		if n == nil {
			return "nil"
		}
		return strconv.Itoa(*n)
	}
	s := "nil"
	if j.Stage != nil {
		s = *j.Stage
	}
	return s + " " + part(j.StageIndex) + "/" + part(j.StageCount)
}

func assertStage(t *testing.T, r *Repo, id uuid.UUID, stage string, index, count int) {
	t.Helper()
	got, err := r.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Stage == nil || got.StageIndex == nil || got.StageCount == nil {
		t.Fatalf("stage = %s, want %s %d/%d", stageOf(got), stage, index, count)
	}
	if *got.Stage != stage || *got.StageIndex != index || *got.StageCount != count {
		t.Fatalf("stage = %s %d/%d, want %s %d/%d", *got.Stage, *got.StageIndex, *got.StageCount, stage, index, count)
	}
}

func assertNoStage(t *testing.T, r *Repo, id uuid.UUID) {
	t.Helper()
	got, err := r.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Stage != nil || got.StageIndex != nil || got.StageCount != nil {
		t.Fatalf("stage = %s, want all nil", stageOf(got))
	}
}

func TestUpdateStage(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	id := createCADStageJob(t, r, "update")

	if err := r.UpdateStage(ctx, id, "guard", 1, 4); err != nil {
		t.Fatalf("UpdateStage on queued job: %v", err)
	}
	assertNoStage(t, r, id)

	if err := r.MarkActive(ctx, id); err != nil {
		t.Fatalf("MarkActive: %v", err)
	}
	eventsBefore := countJobEvents(t, r, id)

	if err := r.UpdateStage(ctx, id, "guard", 1, 4); err != nil {
		t.Fatalf("UpdateStage(guard 1/4): %v", err)
	}
	assertStage(t, r, id, "guard", 1, 4)

	if err := r.UpdateStage(ctx, id, "mesh", 2, 4); err != nil {
		t.Fatalf("UpdateStage(mesh 2/4): %v", err)
	}
	assertStage(t, r, id, "mesh", 2, 4)

	if err := r.UpdateStage(ctx, id, "render", 2, 4); err != nil {
		t.Fatalf("UpdateStage(render 2/4): %v", err)
	}
	assertStage(t, r, id, "mesh", 2, 4)

	if err := r.UpdateStage(ctx, id, "guard", 1, 4); err != nil {
		t.Fatalf("UpdateStage(guard 1/4) after mesh: %v", err)
	}
	assertStage(t, r, id, "mesh", 2, 4)

	if after := countJobEvents(t, r, id); after != eventsBefore {
		t.Fatalf("job_events count changed from %d to %d", eventsBefore, after)
	}
}

func TestUpdateStageRejectsWhatTheChecksForbid(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	id := createCADStageJob(t, r, "checks")
	if err := r.MarkActive(ctx, id); err != nil {
		t.Fatalf("MarkActive: %v", err)
	}

	cases := []struct {
		name       string
		stage      string
		index      int
		constraint string
	}{
		{"unknown_word", "cad-dxf", 1, "jobs_stage_check"},
		{"index_zero", "guard", 0, "jobs_stage_shape_check"},
		{"index_past_count", "judge", 5, "jobs_stage_shape_check"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := r.UpdateStage(ctx, id, tc.stage, tc.index, 4)
			if err == nil || !strings.Contains(err.Error(), tc.constraint) {
				t.Fatalf("UpdateStage(%q, %d, 4) err = %v, want %s", tc.stage, tc.index, err, tc.constraint)
			}
			assertNoStage(t, r, id)
		})
	}

	t.Run("partial_triple", func(t *testing.T) {
		_, err := r.pool.Exec(ctx, `UPDATE jobs SET stage = 'mesh' WHERE id = $1`, id)
		if err == nil || !strings.Contains(err.Error(), "jobs_stage_shape_check") {
			t.Fatalf("partial UPDATE err = %v, want jobs_stage_shape_check", err)
		}
		assertNoStage(t, r, id)
	})
}

func TestMarkActiveResetsTheStage(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	id := createCADStageJob(t, r, "retry")
	if err := r.MarkActive(ctx, id); err != nil {
		t.Fatalf("MarkActive: %v", err)
	}
	if err := r.UpdateStage(ctx, id, "mesh", 2, 4); err != nil {
		t.Fatalf("UpdateStage(mesh 2/4): %v", err)
	}
	assertStage(t, r, id, "mesh", 2, 4)

	if err := r.MarkActive(ctx, id); err != nil {
		t.Fatalf("MarkActive retry: %v", err)
	}
	var attempts int
	if err := r.pool.QueryRow(ctx, `SELECT attempts FROM jobs WHERE id = $1`, id).Scan(&attempts); err != nil {
		t.Fatalf("read attempts: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	assertNoStage(t, r, id)

	if err := r.UpdateStage(ctx, id, "guard", 1, 4); err != nil {
		t.Fatalf("UpdateStage(guard 1/4) after retry: %v", err)
	}
	assertStage(t, r, id, "guard", 1, 4)
}

func TestRequeueStaleResetsTheStage(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	id := createCADStageJob(t, r, "requeue")
	if err := r.MarkActive(ctx, id); err != nil {
		t.Fatalf("MarkActive: %v", err)
	}
	if err := r.UpdateStage(ctx, id, "mesh", 2, 4); err != nil {
		t.Fatalf("UpdateStage(mesh 2/4): %v", err)
	}

	if err := r.RequeueStale(ctx, id, "stale_active"); err != nil {
		t.Fatalf("RequeueStale: %v", err)
	}
	got, err := r.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusQueued {
		t.Fatalf("Status = %s, want %s", got.Status, StatusQueued)
	}
	assertNoStage(t, r, id)
}

func TestStageFreezesOnTerminal(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	t.Run("done", func(t *testing.T) {
		id := createCADStageJob(t, r, "done")
		if err := r.MarkActive(ctx, id); err != nil {
			t.Fatalf("MarkActive: %v", err)
		}
		if err := r.UpdateStage(ctx, id, "judge", 4, 4); err != nil {
			t.Fatalf("UpdateStage(judge 4/4): %v", err)
		}
		addPrimaryOutput(t, r, id)
		if err := r.MarkDone(ctx, id, 0); err != nil {
			t.Fatalf("MarkDone: %v", err)
		}
		assertStage(t, r, id, "judge", 4, 4)
	})

	t.Run("failed", func(t *testing.T) {
		id := createCADStageJob(t, r, "failed")
		if err := r.MarkActive(ctx, id); err != nil {
			t.Fatalf("MarkActive: %v", err)
		}
		if err := r.UpdateStage(ctx, id, "mesh", 2, 4); err != nil {
			t.Fatalf("UpdateStage(mesh 2/4): %v", err)
		}
		if err := r.MarkFailed(ctx, id, "engine_error", "boom", nil, 0); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}
		assertStage(t, r, id, "mesh", 2, 4)
		if err := r.UpdateStage(ctx, id, "render", 3, 4); err != nil {
			t.Fatalf("UpdateStage after failed: %v", err)
		}
		assertStage(t, r, id, "mesh", 2, 4)
	})

	t.Run("canceled", func(t *testing.T) {
		id := createCADStageJob(t, r, "canceled")
		if err := r.MarkActive(ctx, id); err != nil {
			t.Fatalf("MarkActive: %v", err)
		}
		if err := r.UpdateStage(ctx, id, "guard", 1, 4); err != nil {
			t.Fatalf("UpdateStage(guard 1/4): %v", err)
		}
		if err := r.Cancel(ctx, id); err != nil {
			t.Fatalf("Cancel: %v", err)
		}
		assertStage(t, r, id, "guard", 1, 4)
		if err := r.UpdateStage(ctx, id, "mesh", 2, 4); err != nil {
			t.Fatalf("UpdateStage after canceled: %v", err)
		}
		assertStage(t, r, id, "guard", 1, 4)
	})
}
