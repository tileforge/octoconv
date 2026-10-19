package worker

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/db"
	"github.com/tileforge/octoconv/internal/jobs"
)

type routeConverter struct{ op, from, to string }

func (c routeConverter) Pairs() []convert.Pair { return []convert.Pair{{From: c.from, To: c.to}} }

func (routeConverter) Engine() string { return convert.EngineCAD }

func (routeConverter) Convert(context.Context, string, string, map[string]any) error {
	return errors.New("plain Convert must not run")
}

func (c routeConverter) ConvertWithStages(_ context.Context, _, _ string, _ map[string]any, sink convert.StageSink) error {
	route, ok := convert.CADStages(c.op, c.from, c.to)
	if !ok {
		return errors.New("no route")
	}
	for i, stage := range route {
		sink.Stage(convert.StageUpdate{Stage: stage, Index: i + 1, Count: len(route)})
	}
	return nil
}

func TestStageBranchWritesNoPercent(t *testing.T) {
	repo := &spyProgressRepo{}
	conv := routeConverter{convert.OperationConvert, "step", "stl"}
	if err := runConvertWithOptionalProgress(context.Background(), conv, "in.step", "out.stl", nil, repo, uuid.New(), "", 0, 0, false, "", nil, nil, 0); err != nil {
		t.Fatalf("runConvertWithOptionalProgress: %v", err)
	}
	if len(repo.updates) != 0 {
		t.Errorf("UpdateProgress calls = %+v, want none on the stage branch", repo.updates)
	}
	want := []convert.StageUpdate{{Stage: "guard", Index: 1, Count: 3}, {Stage: "mesh", Index: 2, Count: 3}, {Stage: "judge", Index: 3, Count: 3}}
	if !slices.Equal(repo.stages, want) {
		t.Errorf("stages = %+v, want %+v", repo.stages, want)
	}
}

func TestCADStageReachesTheJobRow(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx)
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	var clientID uuid.UUID
	if err := pool.QueryRow(ctx, "INSERT INTO clients (name) VALUES ($1) RETURNING id", "worker-stage-test-client").Scan(&clientID); err != nil {
		t.Fatalf("create test client: %v", err)
	}
	repo := jobs.NewRepo(pool)
	id, err := repo.Create(ctx, jobs.CreateParams{
		ClientID:     clientID,
		Operation:    convert.OperationConvert,
		Engine:       convert.EngineCAD,
		SourceFormat: "step",
		TargetFormat: "stl",
		Input:        jobs.Input{ObjectKey: "uploads/stage-tracer/0-in.step", Filename: "in.step", Format: "step"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.MarkActive(ctx, id); err != nil {
		t.Fatalf("MarkActive: %v", err)
	}
	conv := routeConverter{convert.OperationConvert, "step", "stl"}
	if err := runConvertWithOptionalProgress(ctx, conv, "in.step", "out.stl", nil, repo, id, "", 0, 0, false, "", nil, nil, 0); err != nil {
		t.Fatalf("runConvertWithOptionalProgress: %v", err)
	}
	job, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if job.Stage == nil || *job.Stage != "judge" {
		t.Errorf("Stage = %v, want judge", job.Stage)
	}
	if job.StageIndex == nil || *job.StageIndex != 3 {
		t.Errorf("StageIndex = %v, want 3", job.StageIndex)
	}
	if job.StageCount == nil || *job.StageCount != 3 {
		t.Errorf("StageCount = %v, want 3", job.StageCount)
	}
	if job.ProgressPct != nil {
		t.Errorf("ProgressPct = %d, want null", *job.ProgressPct)
	}
}
