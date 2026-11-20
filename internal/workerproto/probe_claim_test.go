package workerproto

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/jobs"
	"github.com/tileforge/octoconv/internal/storage"
)

func TestProbeClaimPresignsPrimaryAndReport(t *testing.T) {
	ln := startWorkerListener(t)
	clearProbeClaimJobs(t, ln.pool)
	live := ln.hello(t, probeClaimManifest(t))
	clientID := insertClient(t, live.pool)
	jobID := insertProbeClaimJob(t, ln.pool, clientID)

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
	if limits.MaxOutputBytes != int(protocolDefaultOutputBytes) {
		t.Fatalf("max_output_bytes %d", limits.MaxOutputBytes)
	}
	var inputs, outputs []fileLink
	if err := json.Unmarshal(assignment["inputs"], &inputs); err != nil {
		t.Fatalf("inputs: %v", err)
	}
	if err := json.Unmarshal(assignment["outputs"], &outputs); err != nil {
		t.Fatalf("outputs: %v", err)
	}
	if len(inputs) != 1 || len(outputs) != 2 {
		t.Fatalf("inputs %d outputs %d", len(inputs), len(outputs))
	}
	source := storage.InputKey(jobID, 0, "in.png")
	if inputs[0].Role != "source" || inputs[0].Ordinal != 0 || inputs[0].Format != "png" || inputs[0].Href != fileLinkURL("GET", source) {
		t.Fatalf("source %+v", inputs[0])
	}
	attemptID, err := jsonUUID(assignment["attempt_id"])
	if err != nil {
		t.Fatal(err)
	}
	primary := outputByRole(t, outputs, "primary")
	report := outputByRole(t, outputs, "report")
	if primary.Ordinal != 0 || primary.Format != "png" || primary.Href != fileLinkURL("PUT", storage.AttemptOutputKey(attemptID, 0)) {
		t.Fatalf("primary %+v", primary)
	}
	if report.Ordinal != 1 || report.Format != "json" || report.Href != fileLinkURL("PUT", storage.AttemptOutputKey(attemptID, 1)) {
		t.Fatalf("report %+v", report)
	}
	if primary.ExpiresAt == "" || primary.ExpiresAt != report.ExpiresAt || primary.ExpiresAt != inputs[0].ExpiresAt {
		t.Fatalf("expires primary %q report %q source %q", primary.ExpiresAt, report.ExpiresAt, inputs[0].ExpiresAt)
	}
}

func TestProbeCompleteAcceptsThePairInEitherOrder(t *testing.T) {
	for _, order := range []string{"primary-first", "report-first"} {
		t.Run(order, func(t *testing.T) {
			ln := startWorkerListener(t)
			clearProbeClaimJobs(t, ln.pool)
			live := ln.hello(t, probeClaimManifest(t))
			clientID := insertClient(t, live.pool)
			jobID := insertProbeClaimJob(t, ln.pool, clientID)
			status, resp := pullOnce(t, live, 1)
			if status != http.StatusOK {
				t.Fatalf("pull status %d", status)
			}
			assignment := decodeOneAssignment(t, resp)
			attemptID, err := jsonUUID(assignment["attempt_id"])
			if err != nil {
				t.Fatal(err)
			}
			token, err := jsonString(assignment["attempt_token"])
			if err != nil {
				t.Fatal(err)
			}
			primary := []byte("sanitized-png")
			report := []byte(`{"ok":true}`)
			live.store.put(storage.AttemptOutputKey(attemptID, 0), primary)
			live.store.put(storage.AttemptOutputKey(attemptID, 1), report)
			copies := live.store.copyCount()
			rows, usageJobs := usageSnapshot(t, live.pool, clientID)
			body := probeCompleteBody(t, primary, report, order == "report-first")
			status, _, resp = postAttempt(t, live, attemptPaths(live.addr, attemptID)[3], token, body)
			if status != http.StatusOK {
				t.Fatalf("complete status %d: %s", status, resp)
			}
			if live.store.copyCount() != copies+2 {
				t.Fatalf("copies %d", live.store.copyCount())
			}
			if jobStatusOf(t, live.pool, jobID) != jobs.StatusDone {
				t.Fatalf("status %s", jobStatusOf(t, live.pool, jobID))
			}
			assertProbeOutputs(t, live.pool, jobID, int64(len(primary)), int64(len(report)))
			gotRows, gotJobs := usageSnapshot(t, live.pool, clientID)
			if gotRows != rows+1 || gotJobs != usageJobs+1 {
				t.Fatalf("usage rows %d jobs %d, before %d %d", gotRows, gotJobs, rows, usageJobs)
			}
			var released bool
			if err := live.pool.QueryRow(context.Background(), `
				SELECT released_at IS NOT NULL FROM worker_slot_holds WHERE id = $1`, attemptID).Scan(&released); err != nil {
				t.Fatalf("hold: %s", redactDB(err))
			}
			if !released {
				t.Fatal("hold was not released")
			}
			if _, ok := live.store.get(storage.AttemptOutputKey(attemptID, 0)); ok {
				t.Fatal("primary staging remained")
			}
			if _, ok := live.store.get(storage.AttemptOutputKey(attemptID, 1)); ok {
				t.Fatal("report staging remained")
			}
		})
	}
}

func TestProbeCompleteRejectsAPartialPairBeforeCopy(t *testing.T) {
	ln := startWorkerListener(t)
	clearProbeClaimJobs(t, ln.pool)
	live := ln.hello(t, probeClaimManifest(t))
	clientID := insertClient(t, live.pool)
	primary := []byte("sanitized-png")
	report := []byte(`{"ok":true}`)
	primarySum := sha256.Sum256(primary)
	reportSum := sha256.Sum256(report)
	primarySHA := hex.EncodeToString(primarySum[:])
	reportSHA := hex.EncodeToString(reportSum[:])
	goodPrimary := primaryArtifact("png", "image/png", primarySHA, 0, len(primary))
	sidecar := map[string]any{
		"role": "sidecar", "ordinal": 1, "format": "json",
		"content_type": "application/json", "sha256": reportSHA, "byte_size": len(report),
	}
	badReport := map[string]any{
		"role": "report", "ordinal": 1, "format": "txt",
		"content_type": "text/plain", "sha256": reportSHA, "byte_size": len(report),
	}
	cases := []struct {
		name string
		body []byte
	}{
		{name: "one artifact", body: completeArtifacts(t, goodPrimary)},
		{name: "sidecar", body: completeArtifacts(t, goodPrimary, sidecar)},
		{name: "non-json report", body: completeArtifacts(t, goodPrimary, badReport)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jobID := insertProbeClaimJob(t, ln.pool, clientID)
			status, resp := pullOnce(t, live, 1)
			if status != http.StatusOK {
				t.Fatalf("pull status %d", status)
			}
			assignment := decodeOneAssignment(t, resp)
			attemptID, err := jsonUUID(assignment["attempt_id"])
			if err != nil {
				t.Fatal(err)
			}
			token, err := jsonString(assignment["attempt_token"])
			if err != nil {
				t.Fatal(err)
			}
			live.store.put(storage.AttemptOutputKey(attemptID, 0), primary)
			live.store.put(storage.AttemptOutputKey(attemptID, 1), report)
			copies := live.store.copyCount()
			status, hdr, resp := postAttempt(t, live, attemptPaths(live.addr, attemptID)[3], token, tc.body)
			assertProtocolError(t, status, hdr, resp, http.StatusConflict, "artifact_rejected")
			if outputCount(t, live.pool, jobID) != 0 || jobStatusOf(t, live.pool, jobID) != jobs.StatusActive {
				t.Fatal("rejected complete published an output")
			}
			if live.store.copyCount() != copies {
				t.Fatalf("copies %d", live.store.copyCount())
			}
		})
	}
}

func probeClaimManifest(t *testing.T) []byte {
	t.Helper()
	return mustJSON(t, map[string]any{
		"capabilities": []any{
			map[string]any{
				"id":               "octoconv.sanitize.probe",
				"version":          1,
				"legacy":           map[string]any{"operation": "sanitize", "engine": "probe"},
				"client_operation": "sanitize",
				"inputs":           []any{map[string]any{"role": "source", "min": 1, "max": 1}},
				"outputs": []any{
					map[string]any{"role": "primary", "min": 1, "max": 1},
					map[string]any{"role": "report", "min": 1, "max": 1},
				},
				"parameter_schema": map[string]any{},
				"formats":          []any{map[string]any{"from": "png", "to": "png"}},
			},
		},
	})
}

func insertProbeClaimJob(t *testing.T, pool *pgxpool.Pool, clientID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	var client any
	if clientID != uuid.Nil {
		client = clientID
	}
	_, err := pool.Exec(context.Background(), `
		INSERT INTO jobs (
			id, client_id, operation, engine, status, source_format, target_format,
			dispatch_owner, capability_id, capability_version, protocol_deadline,
			options, created_at
		) VALUES (
			$1, $2, 'sanitize', 'probe', 'queued', 'png', 'png',
			'protocol', 'octoconv.sanitize.probe', 1, now() + interval '1 hour',
			'{}', $3
		)`, id, client, time.Now())
	if err != nil {
		t.Fatalf("insert job: %s", redactDB(err))
	}
	_, err = pool.Exec(context.Background(), `
		INSERT INTO job_inputs (job_id, ordinal, object_key, filename, format, size_bytes)
		VALUES ($1, 0, $2, 'in.png', 'png', 4)`, id, storage.InputKey(id, 0, "in.png"))
	if err != nil {
		t.Fatalf("insert input: %s", redactDB(err))
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM job_outputs WHERE job_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM job_events WHERE job_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM worker_slot_holds WHERE job_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM jobs WHERE id = $1`, id)
	})
	return id
}

func clearProbeClaimJobs(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		DELETE FROM worker_slot_holds
		WHERE job_id IN (SELECT id FROM jobs WHERE capability_id = 'octoconv.sanitize.probe')`)
	if err != nil {
		t.Fatalf("clear holds: %s", redactDB(err))
	}
	_, err = pool.Exec(context.Background(), `DELETE FROM jobs WHERE capability_id = 'octoconv.sanitize.probe'`)
	if err != nil {
		t.Fatalf("clear jobs: %s", redactDB(err))
	}
}

func outputByRole(t *testing.T, links []fileLink, role string) fileLink {
	t.Helper()
	for _, link := range links {
		if link.Role == role {
			return link
		}
	}
	t.Fatalf("no %s output", role)
	return fileLink{}
}

func probeCompleteBody(t *testing.T, primary, report []byte, reportFirst bool) []byte {
	t.Helper()
	primarySum := sha256.Sum256(primary)
	reportSum := sha256.Sum256(report)
	primaryArt := primaryArtifact("png", convert.MIMEType("png"), hex.EncodeToString(primarySum[:]), 0, len(primary))
	reportArt := map[string]any{
		"role":         "report",
		"ordinal":      1,
		"format":       "json",
		"content_type": convert.MIMEType("json"),
		"sha256":       hex.EncodeToString(reportSum[:]),
		"byte_size":    len(report),
	}
	if reportFirst {
		return completeArtifacts(t, reportArt, primaryArt)
	}
	return completeArtifacts(t, primaryArt, reportArt)
}

func assertProbeOutputs(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID, primarySize, reportSize int64) {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT ordinal, filename, format, content_type, role, size_bytes
		FROM job_outputs WHERE job_id = $1 ORDER BY ordinal`, jobID)
	if err != nil {
		t.Fatalf("outputs: %s", redactDB(err))
	}
	defer rows.Close()
	type outRow struct {
		ordinal     int
		filename    string
		format      string
		contentType string
		role        string
		size        int64
	}
	var got []outRow
	for rows.Next() {
		var row outRow
		if err := rows.Scan(&row.ordinal, &row.filename, &row.format, &row.contentType, &row.role, &row.size); err != nil {
			t.Fatalf("scan output: %s", redactDB(err))
		}
		got = append(got, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("outputs: %s", redactDB(err))
	}
	if len(got) != 2 {
		t.Fatalf("outputs %d", len(got))
	}
	if got[0].ordinal != 0 || got[0].filename != "out.png" || got[0].format != "png" || got[0].role != jobs.OutputRolePrimary || got[0].contentType != convert.MIMEType("png") || got[0].size != primarySize {
		t.Fatalf("primary %+v", got[0])
	}
	if got[1].ordinal != 1 || got[1].filename != "report.json" || got[1].format != "json" || got[1].role != jobs.OutputRoleReport || got[1].contentType != convert.MIMEType("json") || got[1].size != reportSize {
		t.Fatalf("report %+v", got[1])
	}
}
