package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tileforge/octoconv/internal/db"
)

func TestPublishStoresGrants(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	cap := insertCapability(t, pool, capabilitySpec{})
	clientA := insertClient(t, pool)
	clientB := insertClient(t, pool)
	name := uniqueName()
	got, err := store.Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{clientB, clientA}, Steps: twoSteps(cap.id), Budgets: imageBudget(),
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got.Version != 1 || len(got.ClientIDs) != 2 {
		t.Fatalf("stored %+v", got)
	}
	assertNoStepID(t, got.Document)
	var grants int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM workflow_grants WHERE name = $1 AND version = 1`, name).Scan(&grants); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if grants != 2 {
		t.Fatalf("grants %d", grants)
	}
}

func TestPublishRejectsNinthStep(t *testing.T) {
	pool := testPool(t)
	cap := insertCapability(t, pool, capabilitySpec{})
	name := uniqueName()
	steps := twoSteps(cap.id)
	for len(steps) < 9 {
		next := steps[len(steps)-1]
		if next.TargetFormat == "png" {
			next.SourceFormat, next.TargetFormat = "png", "jpg"
		} else {
			next.SourceFormat, next.TargetFormat = "jpg", "png"
		}
		steps = append(steps, next)
	}
	_, err := NewStore(pool).Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{insertClient(t, pool)}, Steps: steps, Budgets: imageBudget(),
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err %v", err)
	}
	assertNothing(t, pool, name)
}

func TestPublishRejectsOneStep(t *testing.T) {
	pool := testPool(t)
	cap := insertCapability(t, pool, capabilitySpec{})
	name := uniqueName()
	_, err := NewStore(pool).Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{insertClient(t, pool)}, Steps: twoSteps(cap.id)[:1], Budgets: imageBudget(),
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err %v", err)
	}
	assertNothing(t, pool, name)
}

func TestPublishRejectsEmptyClients(t *testing.T) {
	pool := testPool(t)
	cap := insertCapability(t, pool, capabilitySpec{})
	name := uniqueName()
	_, err := NewStore(pool).Publish(context.Background(), PublishInput{
		Name: name, Version: 1, Steps: twoSteps(cap.id), Budgets: imageBudget(),
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err %v", err)
	}
	assertNothing(t, pool, name)
}

func TestPublishRejectsUnknownClient(t *testing.T) {
	pool := testPool(t)
	cap := insertCapability(t, pool, capabilitySpec{})
	name := uniqueName()
	_, err := NewStore(pool).Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{uuid.New()}, Steps: twoSteps(cap.id), Budgets: imageBudget(),
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err %v", err)
	}
	assertNothing(t, pool, name)
}

func TestPublishRejectsOutputMaxCount(t *testing.T) {
	pool := testPool(t)
	cap := insertCapability(t, pool, capabilitySpec{outputMax: 2})
	name := uniqueName()
	_, err := NewStore(pool).Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{insertClient(t, pool)}, Steps: twoSteps(cap.id), Budgets: imageBudget(),
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err %v", err)
	}
	assertNothing(t, pool, name)
}

func TestPublishRejectsPreviewRole(t *testing.T) {
	pool := testPool(t)
	cap := insertCapability(t, pool, capabilitySpec{outputRole: "preview"})
	name := uniqueName()
	steps := twoSteps(cap.id)
	for i := range steps {
		steps[i].OutputRole = "preview"
	}
	_, err := NewStore(pool).Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{insertClient(t, pool)}, Steps: steps, Budgets: imageBudget(),
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err %v", err)
	}
	assertNothing(t, pool, name)
}

func TestPublishRejectsSecondPort(t *testing.T) {
	pool := testPool(t)
	cap := insertCapability(t, pool, capabilitySpec{secondOutput: true})
	name := uniqueName()
	_, err := NewStore(pool).Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{insertClient(t, pool)}, Steps: twoSteps(cap.id), Budgets: imageBudget(),
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err %v", err)
	}
	assertNothing(t, pool, name)
}

func TestPublishRejectsRequiredParameter(t *testing.T) {
	pool := testPool(t)
	cap := insertCapability(t, pool, capabilitySpec{schema: `{"type":"object","required":["x"]}`})
	name := uniqueName()
	_, err := NewStore(pool).Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{insertClient(t, pool)}, Steps: twoSteps(cap.id), Budgets: imageBudget(),
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err %v", err)
	}
	assertNothing(t, pool, name)
}

func TestPublishRejectsImagePDF(t *testing.T) {
	pool := testPool(t)
	cap := insertCapability(t, pool, capabilitySpec{id: imagepdfCapability})
	name := uniqueName()
	_, err := NewStore(pool).Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{insertClient(t, pool)}, Steps: twoSteps(cap.id), Budgets: imageBudget(),
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err %v", err)
	}
	assertNothing(t, pool, name)
}

func TestPublishRejectsAdjacency(t *testing.T) {
	pool := testPool(t)
	cap := insertCapability(t, pool, capabilitySpec{})
	name := uniqueName()
	steps := twoSteps(cap.id)
	steps[1].SourceFormat = "jpg"
	steps[1].TargetFormat = "png"
	_, err := NewStore(pool).Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{insertClient(t, pool)}, Steps: steps, Budgets: imageBudget(),
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err %v", err)
	}
	assertNothing(t, pool, name)
}

func TestPublishConflict(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	cap := insertCapability(t, pool, capabilitySpec{})
	name := uniqueName()
	in := PublishInput{Name: name, Version: 1, ClientIDs: []uuid.UUID{insertClient(t, pool)}, Steps: twoSteps(cap.id), Budgets: imageBudget()}
	if _, err := store.Publish(context.Background(), in); err != nil {
		t.Fatalf("publish: %v", err)
	}
	_, err := store.Publish(context.Background(), in)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err %v", err)
	}
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM workflow_definitions WHERE name = $1 AND version = 1`, name).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("rows %d", n)
	}
}

func TestSnapshot(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	cap := insertCapability(t, pool, capabilitySpec{})
	clientID := insertClient(t, pool)
	name := uniqueName()
	published, err := store.Publish(context.Background(), PublishInput{
		Name: name, Version: 1, ClientIDs: []uuid.UUID{clientID}, Steps: twoSteps(cap.id), Budgets: imageBudget(),
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	assertNoStepID(t, published.Document)
	jobID := insertPlainJob(t, pool, clientID)
	if err := store.Snapshot(context.Background(), jobID, name, 1); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	first := snapshotText(t, pool, jobID)
	ids := stepIDs(t, []byte(first))
	if len(ids) != 2 {
		t.Fatalf("step ids %v", ids)
	}
	for _, id := range ids {
		if _, err := uuid.Parse(id); err != nil {
			t.Fatalf("step id %s", id)
		}
	}
	if _, err := store.Publish(context.Background(), PublishInput{
		Name: name, Version: 2, ClientIDs: []uuid.UUID{clientID}, Steps: twoSteps(cap.id), Budgets: imageBudget(),
	}); err != nil {
		t.Fatalf("publish v2: %v", err)
	}
	second := snapshotText(t, pool, jobID)
	if second != first {
		t.Fatalf("snapshot changed\n%s\n%s", first, second)
	}
	if stepIDs(t, []byte(second))[0] != ids[0] || stepIDs(t, []byte(second))[1] != ids[1] {
		t.Fatal("step id changed")
	}
	var old []byte
	if err := pool.QueryRow(context.Background(), `SELECT document FROM workflow_definitions WHERE name = $1 AND version = 1`, name).Scan(&old); err != nil {
		t.Fatalf("read v1: %v", err)
	}
	assertNoStepID(t, old)
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("DATABASE_URL must be set")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx)
	if err != nil {
		t.Fatal("db.Connect failed")
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	return pool
}

func imageBudget() map[string]time.Duration {
	return map[string]time.Duration{"image": 120 * time.Second}
}

func uniqueName() string {
	return "wf-" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func twoSteps(id string) []Step {
	return []Step{
		{CapabilityID: id, CapabilityVersion: 1, SourceFormat: "jpg", TargetFormat: "png", InputRole: "source", OutputRole: "primary"},
		{CapabilityID: id, CapabilityVersion: 1, SourceFormat: "png", TargetFormat: "jpg", InputRole: "source", OutputRole: "primary"},
	}
}

type capabilitySpec struct {
	id           string
	schema       string
	outputMax    int
	outputRole   string
	secondOutput bool
	nullEngine   bool
}

type insertedCapability struct {
	id       string
	workerID uuid.UUID
}

func insertCapability(t *testing.T, pool *pgxpool.Pool, spec capabilitySpec) insertedCapability {
	t.Helper()
	workerID := uuid.New()
	hexID := strings.ReplaceAll(workerID.String(), "-", "")
	id := spec.id
	if id == "" {
		id = "octoconv.test.a" + hexID
	}
	schema := spec.schema
	if schema == "" {
		schema = `{}`
	}
	outputMax := spec.outputMax
	if outputMax == 0 {
		outputMax = 1
	}
	outputRole := spec.outputRole
	if outputRole == "" {
		outputRole = "primary"
	}
	ctx := context.Background()
	sum := strings.Repeat("ab", 32)
	if _, err := pool.Exec(ctx, `
		INSERT INTO worker_identities (worker_id, manifest_sha256, manifest_bytes)
		VALUES ($1, $2, $3)`, workerID, sum, []byte("manifest")); err != nil {
		t.Fatalf("insert identity: %v", err)
	}
	var engine any
	var operation any
	if !spec.nullEngine {
		engine = "image"
		operation = "convert"
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO worker_capabilities (
			id, version, worker_id, client_operation, legacy_operation, legacy_engine, parameter_schema
		) VALUES ($1, 1, $2, $3, $4, $5, $6::jsonb)`,
		id, workerID, "op."+hexID, operation, engine, schema); err != nil {
		t.Fatalf("insert capability: %v", err)
	}
	for _, pair := range [][2]string{{"jpg", "png"}, {"png", "jpg"}} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO worker_capability_formats (
				capability_id, capability_version, source_format, target_format, client_operation
			) VALUES ($1, 1, $2, $3, $4)`, id, pair[0], pair[1], "op."+hexID); err != nil {
			t.Fatalf("insert format: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO worker_capability_ports (
			capability_id, capability_version, direction, role, min_count, max_count, ordinal
		) VALUES ($1, 1, 'input', 'source', 1, 1, 0)`, id); err != nil {
		t.Fatalf("insert input port: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO worker_capability_ports (
			capability_id, capability_version, direction, role, min_count, max_count, ordinal
		) VALUES ($1, 1, 'output', $2, 1, $3, 0)`, id, outputRole, outputMax); err != nil {
		t.Fatalf("insert output port: %v", err)
	}
	if spec.secondOutput {
		if _, err := pool.Exec(ctx, `
			INSERT INTO worker_capability_ports (
				capability_id, capability_version, direction, role, min_count, max_count, ordinal
			) VALUES ($1, 1, 'output', 'preview', 1, 1, 1)`, id); err != nil {
			t.Fatalf("insert preview port: %v", err)
		}
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM workflow_grants WHERE name IN (SELECT name FROM workflow_definitions WHERE document::text LIKE $1)`, "%"+id+"%")
		_, _ = pool.Exec(ctx, `DELETE FROM workflow_definitions WHERE document::text LIKE $1`, "%"+id+"%")
		_, _ = pool.Exec(ctx, `DELETE FROM worker_capability_ports WHERE capability_id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM worker_capability_formats WHERE capability_id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM worker_capabilities WHERE id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM worker_identities WHERE worker_id = $1`, workerID)
	})
	return insertedCapability{id: id, workerID: workerID}
}

func insertClient(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(context.Background(), `INSERT INTO clients (id, name) VALUES ($1, $2)`, id, "wf-"+id.String()); err != nil {
		t.Fatalf("insert client: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `UPDATE jobs SET workflow_snapshot_id = NULL WHERE client_id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM workflow_snapshots WHERE job_id IN (SELECT id FROM jobs WHERE client_id = $1)`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM jobs WHERE client_id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM workflow_grants WHERE client_id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM clients WHERE id = $1`, id)
	})
	return id
}

func insertPlainJob(t *testing.T, pool *pgxpool.Pool, clientID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO jobs (id, client_id, operation, engine, status, source_format, target_format, options)
		VALUES ($1, $2, 'convert', 'image', 'queued', 'jpg', 'jpg', '{}')`, id, clientID); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	return id
}

func assertNothing(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()
	var defs, grants int
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_definitions WHERE name = $1`, name).Scan(&defs); err != nil {
		t.Fatalf("count definitions: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_grants WHERE name = $1`, name).Scan(&grants); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if defs != 0 || grants != 0 {
		t.Fatalf("definitions %d grants %d", defs, grants)
	}
}

func assertNoStepID(t *testing.T, raw []byte) {
	t.Helper()
	var doc struct {
		Steps []map[string]json.RawMessage `json:"steps"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode document: %v", err)
	}
	if len(doc.Steps) == 0 {
		t.Fatal("document has no steps")
	}
	for _, step := range doc.Steps {
		if _, ok := step["step_id"]; ok {
			t.Fatal("published document has step_id")
		}
	}
}

func snapshotText(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID) string {
	t.Helper()
	var text string
	if err := pool.QueryRow(context.Background(), `SELECT document::text FROM workflow_snapshots WHERE job_id = $1`, jobID).Scan(&text); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	return text
}

func stepIDs(t *testing.T, raw []byte) []string {
	t.Helper()
	var doc struct {
		Steps []struct {
			StepID string `json:"step_id"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	out := make([]string, 0, len(doc.Steps))
	for _, step := range doc.Steps {
		out = append(out, step.StepID)
	}
	return out
}

func ownedCounts(t *testing.T, pool *pgxpool.Pool, clientID uuid.UUID, name string) (int, int) {
	t.Helper()
	var jobs, snaps int
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID).Scan(&jobs); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_snapshots WHERE definition_name = $1`, name).Scan(&snaps); err != nil {
		t.Fatalf("count snapshots: %v", err)
	}
	return jobs, snaps
}
