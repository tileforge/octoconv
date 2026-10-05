package jobs

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func outputRow(ordinal int, role, name, format string, size int64) Output {
	return Output{
		Ordinal:     ordinal,
		ObjectKey:   "results/add-outputs/" + name,
		Filename:    name,
		Format:      format,
		SizeBytes:   size,
		ContentType: "application/octet-stream",
		Role:        role,
	}
}

func visibleRoles(t *testing.T, r *Repo, id uuid.UUID) []string {
	t.Helper()
	outs, err := r.Outputs(context.Background(), id)
	if err != nil {
		t.Fatalf("Outputs: %v", err)
	}
	roles := make([]string, 0, len(outs))
	for _, o := range outs {
		roles = append(roles, o.Role)
	}
	return roles
}

func TestAddOutputsWritesBothRowsInOneTransaction(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	id := createTestJob(t, r, createTestClient(t, r), "cad")
	if err := r.MarkActive(ctx, id); err != nil {
		t.Fatalf("MarkActive: %v", err)
	}

	err := r.AddOutputs(ctx, id, []Output{
		outputRow(0, OutputRolePrimary, "render.png", "png", 10),
		outputRow(1, OutputRoleMeasurements, "measurements.json", "json", 20),
	})
	if err != nil {
		t.Fatalf("AddOutputs: %v", err)
	}
	outs, err := r.Outputs(ctx, id)
	if err != nil {
		t.Fatalf("Outputs: %v", err)
	}
	if len(outs) != 2 || outs[0].Role != OutputRolePrimary || outs[0].Ordinal != 0 ||
		outs[1].Role != OutputRoleMeasurements || outs[1].Ordinal != 1 {
		t.Fatalf("outputs = %+v, want primary at 0 and measurements at 1", outs)
	}
	if err := r.MarkDone(ctx, id, 0); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}

	err = r.AddOutputs(ctx, id, []Output{
		outputRow(0, OutputRolePrimary, "render2.png", "png", 11),
		outputRow(1, OutputRoleMeasurements, "measurements2.json", "json", 21),
	})
	if err != nil {
		t.Fatalf("AddOutputs retry: %v", err)
	}
	outs, err = r.Outputs(ctx, id)
	if err != nil {
		t.Fatalf("Outputs: %v", err)
	}
	if len(outs) != 2 || outs[0].SizeBytes != 11 || outs[1].SizeBytes != 21 || outs[1].Role != OutputRoleMeasurements {
		t.Fatalf("retry did not upsert both rows: %+v", outs)
	}
}

func TestAddOutputsRollsBackWhenTheSecondRowFails(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	atomic := createTestJob(t, r, clientID, "cad")
	if err := r.MarkActive(ctx, atomic); err != nil {
		t.Fatalf("MarkActive: %v", err)
	}
	err := r.AddOutputs(ctx, atomic, []Output{
		outputRow(0, OutputRolePrimary, "render.png", "png", 10),
		outputRow(1, "not-a-role", "measurements.json", "json", 20),
	})
	if err == nil || !strings.Contains(err.Error(), "job_outputs_role_check") {
		t.Fatalf("AddOutputs error = %v, want the role CHECK failure", err)
	}
	if roles := visibleRoles(t, r, atomic); len(roles) != 0 {
		t.Fatalf("AddOutputs left %v visible after a failed second row, want none", roles)
	}
	err = r.MarkDone(ctx, atomic, 0)
	if err == nil || !strings.Contains(err.Error(), "0 primary outputs") {
		t.Fatalf("MarkDone error = %v, want a refusal naming 0 primary outputs", err)
	}
	j, err := r.Get(ctx, atomic)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if j.Status != StatusActive {
		t.Fatalf("status = %q, want %q", j.Status, StatusActive)
	}

	naive := createTestJob(t, r, clientID, "cad")
	if err := r.MarkActive(ctx, naive); err != nil {
		t.Fatalf("MarkActive: %v", err)
	}
	if err := r.AddOutputWithRole(ctx, naive, outputRow(0, OutputRolePrimary, "render.png", "png", 10)); err != nil {
		t.Fatalf("naive first row: %v", err)
	}
	if err := r.AddOutputWithRole(ctx, naive, outputRow(1, "not-a-role", "measurements.json", "json", 20)); err == nil {
		t.Fatal("naive second row: expected the CHECK failure, got nil")
	}
	if roles := visibleRoles(t, r, naive); len(roles) != 1 || roles[0] != OutputRolePrimary {
		t.Fatalf("naive sequence left %v visible, want exactly one primary (the control must show the half-written state)", roles)
	}
}

func TestAddOutputsRejectsEmptyInputWithoutWriting(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	id := createTestJob(t, r, createTestClient(t, r), "cad")

	if err := r.AddOutputs(ctx, id, nil); err == nil {
		t.Fatal("AddOutputs(nil) = nil, want an error")
	}
	err := r.AddOutputs(ctx, id, []Output{
		outputRow(0, OutputRolePrimary, "render.png", "png", 10),
		outputRow(1, "", "measurements.json", "json", 20),
	})
	if err == nil || !strings.Contains(err.Error(), "empty role") {
		t.Fatalf("AddOutputs with an empty role = %v, want an empty-role error", err)
	}
	if roles := visibleRoles(t, r, id); len(roles) != 0 {
		t.Fatalf("a refused call wrote %v", roles)
	}
}
