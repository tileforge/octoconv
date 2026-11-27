package workerproto

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tileforge/octoconv/internal/storage"
)

func TestArchiveClaimPresignsEveryStoredInput(t *testing.T) {
	ln := startWorkerListener(t)
	clearArchiveClaimJobs(t, ln.pool)
	live := ln.hello(t, archiveClaimManifest(t))
	jobID := insertArchiveClaimJob(t, ln.pool, []claimInput{
		{ordinal: 0, filename: "a.txt", format: "txt"},
		{ordinal: 1, filename: "b.txt", format: "txt"},
	})

	status, resp := pullOnce(t, live, 1)
	if status != http.StatusOK {
		t.Fatalf("pull status %d", status)
	}
	assignment := decodeOneAssignment(t, resp)
	if got, err := jsonUUID(assignment["job_id"]); err != nil || got != jobID {
		t.Fatalf("assigned job %v %v", got, err)
	}
	var inputs []fileLink
	if err := json.Unmarshal(assignment["inputs"], &inputs); err != nil {
		t.Fatalf("inputs: %v", err)
	}
	if len(inputs) != 2 {
		t.Fatalf("inputs %d", len(inputs))
	}
	first := storage.InputKey(jobID, 0, "a.txt")
	second := storage.InputKey(jobID, 1, "b.txt")
	if inputs[0].Role != "source" || inputs[0].Ordinal != 0 ||
		inputs[0].Format != "txt" || inputs[0].Href != fileLinkURL("GET", first) {
		t.Fatalf("source zero %+v", inputs[0])
	}
	if inputs[1].Role != "source" || inputs[1].Ordinal != 1 ||
		inputs[1].Format != "txt" || inputs[1].Href != fileLinkURL("GET", second) {
		t.Fatalf("source one %+v", inputs[1])
	}
	var params struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(assignment["parameters"], &params); err != nil {
		t.Fatalf("parameters: %v body=%s", err, assignment["parameters"])
	}
	if len(params.Paths) != 2 || params.Paths[0] != "docs/a.txt" || params.Paths[1] != "b.txt" {
		t.Fatalf("paths %#v", params.Paths)
	}
}

func archiveClaimManifest(t *testing.T) []byte {
	t.Helper()
	return mustJSON(t, map[string]any{
		"capabilities": []any{
			map[string]any{
				"id":               "octoconv.archive.zip",
				"version":          1,
				"legacy":           map[string]any{"operation": "archive", "engine": "archive"},
				"client_operation": "archive",
				"inputs":           []any{map[string]any{"role": "source", "min": 1, "max": 32}},
				"outputs":          []any{map[string]any{"role": "primary", "min": 1, "max": 1}},
				"parameter_schema": map[string]any{},
				"formats":          []any{map[string]any{"from": "files", "to": "zip"}},
			},
		},
	})
}

func insertArchiveClaimJob(t *testing.T, pool *pgxpool.Pool, parts []claimInput) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO jobs (
			id, operation, engine, status, source_format, target_format,
			dispatch_owner, capability_id, capability_version, protocol_deadline,
			options, created_at
		) VALUES (
			$1, 'archive', 'archive', 'queued', 'files', 'zip',
			'protocol', 'octoconv.archive.zip', 1, now() + interval '1 hour',
			'{"paths":["docs/a.txt","b.txt"]}', $2
		)`, id, time.Now())
	if err != nil {
		t.Fatalf("insert job: %s", redactDB(err))
	}
	for _, part := range parts {
		_, err = pool.Exec(context.Background(), `
			INSERT INTO job_inputs (job_id, ordinal, object_key, filename, format, size_bytes)
			VALUES ($1, $2, $3, $4, $5, 4)`,
			id, part.ordinal, storage.InputKey(id, part.ordinal, part.filename), part.filename, part.format)
		if err != nil {
			t.Fatalf("insert input: %s", redactDB(err))
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM worker_slot_holds WHERE job_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM jobs WHERE id = $1`, id)
	})
	return id
}

func clearArchiveClaimJobs(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		DELETE FROM worker_slot_holds
		WHERE job_id IN (SELECT id FROM jobs WHERE capability_id = 'octoconv.archive.zip')`)
	if err != nil {
		t.Fatalf("clear holds: %s", redactDB(err))
	}
	_, err = pool.Exec(context.Background(), `DELETE FROM jobs WHERE capability_id = 'octoconv.archive.zip'`)
	if err != nil {
		t.Fatalf("clear jobs: %s", redactDB(err))
	}
}
