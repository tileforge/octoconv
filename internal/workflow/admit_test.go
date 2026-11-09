package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tileforge/octoconv/internal/jobs"
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

func TestAdmitRejectsEnforcedZeroConcurrency(t *testing.T) {
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
	denyImage(t, pool, clientID)
	beforeJobs, beforeSnaps := ownedCounts(t, pool, clientID, name)
	_, err := store.Admit(context.Background(), AdmitInput{
		ClientID: clientID, Name: name, Version: 1, SourceFormat: "jpg",
		Deadline: time.Now().Add(10 * time.Minute), Budgets: imageBudget(),
		InputBytes: 10,
	})
	if !errors.Is(err, jobs.ErrConcurrencyLimit) {
		t.Fatalf("err %v", err)
	}
	afterJobs, afterSnaps := ownedCounts(t, pool, clientID, name)
	if afterJobs != beforeJobs || afterSnaps != beforeSnaps {
		t.Fatalf("jobs %d->%d snapshots %d->%d", beforeJobs, afterJobs, beforeSnaps, afterSnaps)
	}
}

func TestAdmitDisabledQuotaStillStores(t *testing.T) {
	pool := testPool(t)
	repo := jobs.NewRepo(pool)
	repo.SetQuotaEnforcementDisabled(true)
	store := NewStore(pool)
	store.SetJobs(repo)
	cap := insertCapability(t, pool, capabilitySpec{})
	clientID := insertClient(t, pool)
	name := uniqueName()
	if _, err := store.Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{clientID}, Steps: twoSteps(cap.id), Budgets: imageBudget(),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	denyImage(t, pool, clientID)
	jobID, err := store.Admit(context.Background(), AdmitInput{
		ClientID: clientID, Name: name, Version: 1, SourceFormat: "jpg",
		Deadline: time.Now().Add(10 * time.Minute), Budgets: imageBudget(),
	})
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if jobID == uuid.Nil {
		t.Fatal("missing job")
	}
}

func TestPurgeDeletesWorkflowSnapshot(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	var def string
	if err := pool.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conname = 'workflow_snapshots_job_id_fkey'`).Scan(&def); err != nil {
		t.Fatalf("read constraint: %v", err)
	}
	if !strings.Contains(def, "ON DELETE CASCADE") {
		t.Fatalf("constraint %s", def)
	}
	clientID := insertClient(t, pool)
	jobID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO jobs (
			id, client_id, operation, engine, status, source_format, target_format, options, created_at
		) VALUES (
			$1, $2, 'convert', 'image', 'done', 'jpg', 'png', '{}', '-infinity'
		)`, jobID, clientID); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO workflow_snapshots (job_id, definition_name, definition_version, document)
		VALUES ($1, 'purge-snap', 1, '{"steps":[]}'::jsonb)`, jobID); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET workflow_snapshot_id = id WHERE id = $1`, jobID); err != nil {
		t.Fatalf("attach snapshot: %v", err)
	}
	if _, err := jobs.NewRepo(pool).PurgeJobs(ctx, 90*24*time.Hour, 1); err != nil {
		t.Fatalf("purge: %v", err)
	}
	var jobsLeft, snapsLeft int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE id = $1`, jobID).Scan(&jobsLeft); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_snapshots WHERE job_id = $1`, jobID).Scan(&snapsLeft); err != nil {
		t.Fatalf("count snapshots: %v", err)
	}
	if jobsLeft != 0 || snapsLeft != 0 {
		t.Fatalf("jobs %d snapshots %d", jobsLeft, snapsLeft)
	}
}

func TestAdmitDailyBytesSeesPriorWorkflowJob(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	repo := jobs.NewRepo(pool)
	cap := insertCapability(t, pool, capabilitySpec{})
	clientID := insertClient(t, pool)
	name := uniqueName()
	if _, err := store.Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{clientID}, Steps: twoSteps(cap.id), Budgets: imageBudget(),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	limitImageBytes(t, pool, clientID, 100)
	first, err := store.Admit(context.Background(), AdmitInput{
		ClientID: clientID, Name: name, Version: 1, SourceFormat: "jpg",
		Deadline: time.Now().Add(10 * time.Minute), Budgets: imageBudget(),
		InputBytes: 60,
	})
	if err != nil {
		t.Fatalf("first admit: %v", err)
	}
	var stored int64
	if err := pool.QueryRow(context.Background(), `SELECT admitted_input_bytes FROM jobs WHERE id = $1`, first).Scan(&stored); err != nil {
		t.Fatalf("read bytes: %v", err)
	}
	if stored != 60 {
		t.Fatalf("admitted bytes %d", stored)
	}
	_, err = store.Admit(context.Background(), AdmitInput{
		ClientID: clientID, Name: name, Version: 1, SourceFormat: "jpg",
		Deadline: time.Now().Add(10 * time.Minute), Budgets: imageBudget(),
		InputBytes: 60,
	})
	if !errors.Is(err, jobs.ErrDailyBytesLimit) {
		t.Fatalf("second admit err %v", err)
	}
	if n, snaps := ownedCounts(t, pool, clientID, name); n != 1 || snaps != 1 {
		t.Fatalf("jobs %d snapshots %d", n, snaps)
	}
	if err := repo.MarkFailed(context.Background(), first, "engine_error", "boom", nil, 0); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	var rolled int64
	if err := pool.QueryRow(context.Background(), `
		SELECT COALESCE(sum(bytes_in), 0) FROM client_usage_daily
		WHERE client_id = $1 AND engine = 'image' AND day = (now() AT TIME ZONE 'utc')::date`, clientID).Scan(&rolled); err != nil {
		t.Fatalf("read usage: %v", err)
	}
	if rolled != 60 {
		t.Fatalf("bytes_in %d", rolled)
	}
	_, err = store.Admit(context.Background(), AdmitInput{
		ClientID: clientID, Name: name, Version: 1, SourceFormat: "jpg",
		Deadline: time.Now().Add(10 * time.Minute), Budgets: imageBudget(),
		InputBytes: 60,
	})
	if !errors.Is(err, jobs.ErrDailyBytesLimit) {
		t.Fatalf("admit after rollup err %v", err)
	}
	if n, snaps := ownedCounts(t, pool, clientID, name); n != 1 || snaps != 1 {
		t.Fatalf("after rollup jobs %d snapshots %d", n, snaps)
	}
}

func limitImageBytes(t *testing.T, pool *pgxpool.Pool, clientID uuid.UUID, maxBytes int64) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE clients SET enforcement_mode = 'enforce' WHERE id = $1`, clientID); err != nil {
		t.Fatalf("enforce: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO client_limits (client_id, engine, max_daily_bytes)
		VALUES ($1, 'image', $2)`, clientID, maxBytes); err != nil {
		t.Fatalf("limit: %v", err)
	}
}

func denyImage(t *testing.T, pool *pgxpool.Pool, clientID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE clients SET enforcement_mode = 'enforce' WHERE id = $1`, clientID); err != nil {
		t.Fatalf("enforce: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO client_limits (client_id, engine, max_concurrent)
		VALUES ($1, 'image', 0)`, clientID); err != nil {
		t.Fatalf("limit: %v", err)
	}
}
