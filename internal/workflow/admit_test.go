package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAdmitRejectsUngrantedClient(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	cap := insertCapability(t, pool, capabilitySpec{})
	owner := insertClient(t, pool)
	other := insertClient(t, pool)
	name := uniqueName()
	if _, err := store.Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{owner}, Steps: twoSteps(cap.id), Budgets: imageBudget(),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	beforeJobs, beforeSnaps := ownedCounts(t, pool, other, name)
	_, err := store.Admit(context.Background(), AdmitInput{
		ClientID: other, Name: name, Version: 1, SourceFormat: "jpg",
		Deadline: time.Now().Add(10 * time.Minute), Budgets: imageBudget(),
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err %v", err)
	}
	afterJobs, afterSnaps := ownedCounts(t, pool, other, name)
	if afterJobs != beforeJobs || afterSnaps != beforeSnaps {
		t.Fatalf("jobs %d->%d snapshots %d->%d", beforeJobs, afterJobs, beforeSnaps, afterSnaps)
	}
}

func TestAdmitRejectsPortMismatch(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	cap := insertCapability(t, pool, capabilitySpec{})
	clientID := insertClient(t, pool)
	name := uniqueName()
	if _, err := store.Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{clientID}, Steps: twoSteps(cap.id), Budgets: imageBudget(),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE worker_capability_ports
		SET max_count = 2
		WHERE capability_id = $1 AND direction = 'output'`, cap.id); err != nil {
		t.Fatalf("update port: %v", err)
	}
	beforeJobs, beforeSnaps := ownedCounts(t, pool, clientID, name)
	_, err := store.Admit(context.Background(), AdmitInput{
		ClientID: clientID, Name: name, Version: 1, SourceFormat: "jpg",
		Deadline: time.Now().Add(10 * time.Minute), Budgets: imageBudget(),
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err %v", err)
	}
	afterJobs, afterSnaps := ownedCounts(t, pool, clientID, name)
	if afterJobs != beforeJobs || afterSnaps != beforeSnaps {
		t.Fatalf("jobs %d->%d snapshots %d->%d", beforeJobs, afterJobs, beforeSnaps, afterSnaps)
	}
}

func TestAdmitRejectsRequiredParameter(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	cap := insertCapability(t, pool, capabilitySpec{})
	clientID := insertClient(t, pool)
	name := uniqueName()
	if _, err := store.Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{clientID}, Steps: twoSteps(cap.id), Budgets: imageBudget(),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE worker_capabilities
		SET parameter_schema = '{"type":"object","required":["x"]}'::jsonb
		WHERE id = $1`, cap.id); err != nil {
		t.Fatalf("update schema: %v", err)
	}
	beforeJobs, beforeSnaps := ownedCounts(t, pool, clientID, name)
	_, err := store.Admit(context.Background(), AdmitInput{
		ClientID: clientID, Name: name, Version: 1, SourceFormat: "jpg",
		Deadline: time.Now().Add(10 * time.Minute), Budgets: imageBudget(),
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err %v", err)
	}
	afterJobs, afterSnaps := ownedCounts(t, pool, clientID, name)
	if afterJobs != beforeJobs || afterSnaps != beforeSnaps {
		t.Fatalf("jobs %d->%d snapshots %d->%d", beforeJobs, afterJobs, beforeSnaps, afterSnaps)
	}
}

func TestAdmitRejectsImagePDF(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	cap := insertCapability(t, pool, capabilitySpec{id: imagepdfCapability})
	clientID := insertClient(t, pool)
	name := uniqueName()
	doc := []byte(`{"steps":[{"capability_id":"` + cap.id + `","capability_version":1,"source_format":"jpg","target_format":"png","input_role":"source","output_role":"primary","parameters":{},"legacy_engine":"image"},{"capability_id":"` + cap.id + `","capability_version":1,"source_format":"png","target_format":"jpg","input_role":"source","output_role":"primary","parameters":{},"legacy_engine":"image"}]}`)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO workflow_definitions (name, version, document) VALUES ($1, 1, $2::jsonb)`, name, doc); err != nil {
		t.Fatalf("insert definition: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO workflow_grants (name, version, client_id) VALUES ($1, 1, $2)`, name, clientID); err != nil {
		t.Fatalf("insert grant: %v", err)
	}
	beforeJobs, beforeSnaps := ownedCounts(t, pool, clientID, name)
	_, err := store.Admit(context.Background(), AdmitInput{
		ClientID: clientID, Name: name, Version: 1, SourceFormat: "jpg",
		Deadline: time.Now().Add(10 * time.Minute), Budgets: imageBudget(),
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err %v", err)
	}
	afterJobs, afterSnaps := ownedCounts(t, pool, clientID, name)
	if afterJobs != beforeJobs || afterSnaps != beforeSnaps {
		t.Fatalf("jobs %d->%d snapshots %d->%d", beforeJobs, afterJobs, beforeSnaps, afterSnaps)
	}
}

func TestAdmitRejectsSourceFormat(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	cap := insertCapability(t, pool, capabilitySpec{})
	clientID := insertClient(t, pool)
	name := uniqueName()
	if _, err := store.Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{clientID}, Steps: twoSteps(cap.id), Budgets: imageBudget(),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	beforeJobs, beforeSnaps := ownedCounts(t, pool, clientID, name)
	_, err := store.Admit(context.Background(), AdmitInput{
		ClientID: clientID, Name: name, Version: 1, SourceFormat: "png",
		Deadline: time.Now().Add(10 * time.Minute), Budgets: imageBudget(),
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err %v", err)
	}
	afterJobs, afterSnaps := ownedCounts(t, pool, clientID, name)
	if afterJobs != beforeJobs || afterSnaps != beforeSnaps {
		t.Fatalf("jobs %d->%d snapshots %d->%d", beforeJobs, afterJobs, beforeSnaps, afterSnaps)
	}
}

func TestAdmitRejectsDeadline(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	cap := insertCapability(t, pool, capabilitySpec{})
	clientID := insertClient(t, pool)
	name := uniqueName()
	if _, err := store.Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{clientID}, Steps: twoSteps(cap.id), Budgets: imageBudget(),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	beforeJobs, beforeSnaps := ownedCounts(t, pool, clientID, name)
	_, err := store.Admit(context.Background(), AdmitInput{
		ClientID: clientID, Name: name, Version: 1, SourceFormat: "jpg",
		Deadline: time.Now().Add(time.Second), Budgets: imageBudget(),
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err %v", err)
	}
	afterJobs, afterSnaps := ownedCounts(t, pool, clientID, name)
	if afterJobs != beforeJobs || afterSnaps != beforeSnaps {
		t.Fatalf("jobs %d->%d snapshots %d->%d", beforeJobs, afterJobs, beforeSnaps, afterSnaps)
	}
}

func TestAdmitRejectsMissingBudget(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	cap := insertCapability(t, pool, capabilitySpec{})
	clientID := insertClient(t, pool)
	name := uniqueName()
	if _, err := store.Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{clientID}, Steps: twoSteps(cap.id), Budgets: imageBudget(),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	beforeJobs, beforeSnaps := ownedCounts(t, pool, clientID, name)
	_, err := store.Admit(context.Background(), AdmitInput{
		ClientID: clientID, Name: name, Version: 1, SourceFormat: "jpg",
		Deadline: time.Now().Add(10 * time.Minute), Budgets: map[string]time.Duration{"document": time.Minute},
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err %v", err)
	}
	afterJobs, afterSnaps := ownedCounts(t, pool, clientID, name)
	if afterJobs != beforeJobs || afterSnaps != beforeSnaps {
		t.Fatalf("jobs %d->%d snapshots %d->%d", beforeJobs, afterJobs, beforeSnaps, afterSnaps)
	}
}

func TestAdmitRejectsNullEngine(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	cap := insertCapability(t, pool, capabilitySpec{})
	clientID := insertClient(t, pool)
	name := uniqueName()
	if _, err := store.Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{clientID}, Steps: twoSteps(cap.id), Budgets: imageBudget(),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE worker_capabilities
		SET legacy_operation = NULL, legacy_engine = NULL
		WHERE id = $1`, cap.id); err != nil {
		t.Fatalf("clear engine: %v", err)
	}
	beforeJobs, beforeSnaps := ownedCounts(t, pool, clientID, name)
	_, err := store.Admit(context.Background(), AdmitInput{
		ClientID: clientID, Name: name, Version: 1, SourceFormat: "jpg",
		Deadline: time.Now().Add(10 * time.Minute), Budgets: imageBudget(),
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err %v", err)
	}
	afterJobs, afterSnaps := ownedCounts(t, pool, clientID, name)
	if afterJobs != beforeJobs || afterSnaps != beforeSnaps {
		t.Fatalf("jobs %d->%d snapshots %d->%d", beforeJobs, afterJobs, beforeSnaps, afterSnaps)
	}
}

func TestAdmitStoresOneJob(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	cap := insertCapability(t, pool, capabilitySpec{})
	clientID := insertClient(t, pool)
	name := uniqueName()
	if _, err := store.Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{clientID}, Steps: twoSteps(cap.id), Budgets: imageBudget(),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	jobID, err := store.Admit(context.Background(), AdmitInput{
		ClientID: clientID, Name: name, Version: 1, SourceFormat: "jpeg",
		Deadline: time.Now().Add(10 * time.Minute), Budgets: imageBudget(),
	})
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	var status, engine, owner, source, target string
	var attempts, version int
	var capability string
	var snapshot *uuid.UUID
	if err := pool.QueryRow(context.Background(), `
		SELECT status, engine, dispatch_owner, attempts, source_format, target_format,
		       capability_id, capability_version, workflow_snapshot_id
		FROM jobs WHERE id = $1`, jobID).Scan(
		&status, &engine, &owner, &attempts, &source, &target, &capability, &version, &snapshot,
	); err != nil {
		t.Fatalf("read job: %v", err)
	}
	if status != "queued" || engine != "image" || owner != "protocol" || attempts != 0 {
		t.Fatalf("job %s %s %s attempts %d", status, engine, owner, attempts)
	}
	if source != "jpg" || target != "jpg" || capability != cap.id || version != 1 || snapshot == nil || *snapshot != jobID {
		t.Fatalf("shape source %s target %s capability %s version %d snapshot %v", source, target, capability, version, snapshot)
	}
	var holds, creds int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM worker_slot_holds WHERE job_id = $1`, jobID).Scan(&holds); err != nil {
		t.Fatalf("count holds: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM worker_attempt_credentials c
		WHERE c.attempt_id IN (SELECT id FROM worker_slot_holds WHERE job_id = $1)`, jobID).Scan(&creds); err != nil {
		t.Fatalf("count credentials: %v", err)
	}
	if holds != 0 || creds != 0 {
		t.Fatalf("holds %d credentials %d", holds, creds)
	}
	view, err := store.Status(context.Background(), clientID, jobID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if view.Status != "queued" || len(view.Steps) != 2 {
		t.Fatalf("view %+v", view)
	}
	if _, err := uuid.Parse(view.Steps[0].StepID.String()); err != nil || view.Steps[0].StepID == uuid.Nil {
		t.Fatal("missing step id")
	}
}
