package workerproto

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/storage"
)

func TestTectonicClaimPresignsEveryStoredInput(t *testing.T) {
	ln := startWorkerListener(t)
	clearTectonicClaimJobs(t, ln.pool)
	live := ln.hello(t, tectonicClaimManifest(t))
	jobID := insertTectonicClaimJob(t, ln.pool, time.Now(), []claimInput{
		{ordinal: 0, filename: "paper.tex", format: "latex"},
		{ordinal: 1, filename: "refs.bib", format: ""},
	})

	status, resp := pullOnce(t, live, 1)
	if status != http.StatusOK {
		t.Fatalf("pull status %d", status)
	}
	assignment := decodeOneAssignment(t, resp)
	if got, err := jsonUUID(assignment["job_id"]); err != nil || got != jobID {
		t.Fatalf("assigned job %v %v", got, err)
	}
	var limits struct {
		MaxOutputBytes int `json:"max_output_bytes"`
	}
	if err := json.Unmarshal(assignment["limits"], &limits); err != nil {
		t.Fatalf("limits: %v", err)
	}
	if limits.MaxOutputBytes != int(convert.DefaultTectonicMaxOutputBytes) {
		t.Fatalf("max_output_bytes %d", limits.MaxOutputBytes)
	}
	var inputs []fileLink
	if err := json.Unmarshal(assignment["inputs"], &inputs); err != nil {
		t.Fatalf("inputs: %v", err)
	}
	if len(inputs) != 2 {
		t.Fatalf("inputs %d", len(inputs))
	}
	paper := storage.InputKey(jobID, 0, "paper.tex")
	bib := storage.InputKey(jobID, 1, "refs.bib")
	if inputs[0].Role != "source" || inputs[0].Ordinal != 0 || inputs[0].Format != "latex" || inputs[0].Href != fileLinkURL("GET", paper) {
		t.Fatalf("source zero %+v", inputs[0])
	}
	if inputs[1].Role != "source" || inputs[1].Ordinal != 1 || inputs[1].Format != "bib" || inputs[1].Href != fileLinkURL("GET", bib) {
		t.Fatalf("source one %+v", inputs[1])
	}
}

func TestTectonicClaimMissingOrdinalStaysQueued(t *testing.T) {
	ln := startWorkerListener(t)
	clearTectonicClaimJobs(t, ln.pool)
	live := ln.hello(t, tectonicClaimManifest(t))
	missing := insertTectonicClaimJob(t, ln.pool, time.Now().Add(-time.Minute), []claimInput{
		{ordinal: 1, filename: "refs.bib", format: ""},
	})
	ready := insertTectonicClaimJob(t, ln.pool, time.Now(), []claimInput{
		{ordinal: 0, filename: "paper.tex", format: "latex"},
		{ordinal: 1, filename: "refs.bib", format: ""},
	})

	status, resp := pullOnce(t, live, 1)
	if status != http.StatusOK {
		t.Fatalf("pull status %d", status)
	}
	assignment := decodeOneAssignment(t, resp)
	if got, err := jsonUUID(assignment["job_id"]); err != nil || got != ready {
		t.Fatalf("assigned job %v %v", got, err)
	}
	if got := tectonicJobStatus(t, ln.pool, missing); got != "queued" {
		t.Fatalf("missing ordinal zero status %s", got)
	}
	if got := tectonicJobStatus(t, ln.pool, ready); got != "active" {
		t.Fatalf("ready status %s", got)
	}
}

func tectonicClaimManifest(t *testing.T) []byte {
	t.Helper()
	return mustJSON(t, map[string]any{
		"capabilities": []any{
			map[string]any{
				"id":               "octoconv.convert.tectonic",
				"version":          1,
				"legacy":           map[string]any{"operation": "convert", "engine": "tectonic"},
				"client_operation": "convert",
				"inputs":           []any{map[string]any{"role": "source", "min": 1, "max": 32}},
				"outputs":          []any{map[string]any{"role": "primary", "min": 1, "max": 1}},
				"parameter_schema": map[string]any{},
				"formats":          []any{map[string]any{"from": "latex", "to": "pdf"}},
			},
		},
	})
}

type claimInput struct {
	ordinal  int
	filename string
	format   string
}

func insertTectonicClaimJob(t *testing.T, pool *pgxpool.Pool, created time.Time, parts []claimInput) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO jobs (
			id, operation, engine, status, source_format, target_format,
			dispatch_owner, capability_id, capability_version, protocol_deadline,
			options, created_at
		) VALUES (
			$1, 'convert', 'tectonic', 'queued', 'latex', 'pdf',
			'protocol', 'octoconv.convert.tectonic', 1, now() + interval '1 hour',
			'{}', $2
		)`, id, created)
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

func clearTectonicClaimJobs(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		DELETE FROM worker_slot_holds
		WHERE job_id IN (SELECT id FROM jobs WHERE capability_id = 'octoconv.convert.tectonic')`)
	if err != nil {
		t.Fatalf("clear holds: %s", redactDB(err))
	}
	_, err = pool.Exec(context.Background(), `DELETE FROM jobs WHERE capability_id = 'octoconv.convert.tectonic'`)
	if err != nil {
		t.Fatalf("clear jobs: %s", redactDB(err))
	}
}

func tectonicJobStatus(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM jobs WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatalf("read status: %s", redactDB(err))
	}
	return status
}

func redactDB(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	if strings.Contains(msg, "://") || strings.Contains(lower, "password") || strings.Contains(lower, "postgres") {
		return "database error"
	}
	return msg
}
