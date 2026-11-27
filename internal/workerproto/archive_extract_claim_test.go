package workerproto

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/storage"
)

func TestExtract_OnePngZipPacksWebpAndAcceptsOnlyOnPack(t *testing.T) {
	ln := startWorkerListener(t)
	clearArchiveClaimJobs(t, ln.pool)
	archiveLive := ln.hello(t, archiveClaimManifest(t))
	imageLive := ln.hello(t, imageLegacyManifest(t, "octoconv.convert.image", "png", "webp"))
	png := extractPNG()
	zipBytes := extractMemberZip(t, []namedMember{{name: "docs/photo.png", body: png}})
	clientID := insertClient(t, ln.pool)
	jobID := insertExtractionJob(t, ln.pool, archiveLive.store, clientID, zipBytes)

	status, resp := pullOnce(t, archiveLive, 1)
	if status != http.StatusOK {
		t.Fatalf("unpack pull %d: %s", status, resp)
	}
	unpack := decodeOneAssignment(t, resp)
	if got, err := jsonUUID(unpack["job_id"]); err != nil || got != jobID {
		t.Fatalf("unpack job %v %v", got, err)
	}
	if capID, err := jsonString(unpack["capability_id"]); err != nil || capID != "octoconv.archive.zip" {
		t.Fatalf("unpack capability %s %v", capID, err)
	}
	var outputs []fileLink
	if err := json.Unmarshal(unpack["outputs"], &outputs); err != nil {
		t.Fatalf("outputs: %v", err)
	}
	if len(outputs) != 1 || outputs[0].Role != "primary" || outputs[0].Ordinal != 0 || outputs[0].Format != "png" {
		t.Fatalf("unpack outputs %+v", outputs)
	}
	attemptID, err := jsonUUID(unpack["attempt_id"])
	if err != nil {
		t.Fatal(err)
	}
	token, err := jsonString(unpack["attempt_token"])
	if err != nil {
		t.Fatal(err)
	}
	archiveLive.store.put(storage.AttemptOutputKey(attemptID, 0), png)
	status, _, resp = postAttempt(t, archiveLive, attemptPaths(archiveLive.addr, attemptID)[3], token, completeBody(t, "png", convert.MIMEType("png"), png))
	if status != http.StatusOK {
		t.Fatalf("unpack complete %d: %s", status, resp)
	}
	if jobStatusOf(t, ln.pool, jobID) != "queued" {
		t.Fatalf("after unpack %s", jobStatusOf(t, ln.pool, jobID))
	}
	if outputCount(t, ln.pool, jobID) != 0 {
		t.Fatal("unpack inserted job_outputs")
	}

	status, resp = pullOnce(t, imageLive, 1)
	if status != http.StatusOK {
		t.Fatalf("pair pull %d: %s", status, resp)
	}
	pair := decodeOneAssignment(t, resp)
	if got, err := jsonUUID(pair["job_id"]); err != nil || got != jobID {
		t.Fatalf("pair job %v %v", got, err)
	}
	if capID, err := jsonString(pair["capability_id"]); err != nil || capID != "octoconv.convert.image" {
		t.Fatalf("pair capability %s %v", capID, err)
	}
	if raw := pair["parameters"]; bytes.Contains(raw, []byte("archive_extraction")) {
		t.Fatalf("pair parameters %s", raw)
	}
	var inputs []fileLink
	if err := json.Unmarshal(pair["inputs"], &inputs); err != nil {
		t.Fatalf("pair inputs: %v", err)
	}
	if len(inputs) != 1 || inputs[0].Ordinal != 0 || inputs[0].Format != "png" {
		t.Fatalf("pair inputs %+v", inputs)
	}
	member, ok := archiveLive.store.get(inputKeyOf(t, inputs[0].Href))
	if !ok || !bytes.Equal(member, png) {
		t.Fatal("pair input is not the png member")
	}
	attemptID, err = jsonUUID(pair["attempt_id"])
	if err != nil {
		t.Fatal(err)
	}
	token, err = jsonString(pair["attempt_token"])
	if err != nil {
		t.Fatal(err)
	}
	webp := []byte("webp-result")
	imageLive.store.put(storage.AttemptOutputKey(attemptID, 0), webp)
	status, _, resp = postAttempt(t, imageLive, attemptPaths(imageLive.addr, attemptID)[3], token, completeBody(t, "webp", convert.MIMEType("webp"), webp))
	if status != http.StatusOK {
		t.Fatalf("pair complete %d: %s", status, resp)
	}
	if jobStatusOf(t, ln.pool, jobID) != "queued" || outputCount(t, ln.pool, jobID) != 0 {
		t.Fatalf("after pair status %s outputs %d", jobStatusOf(t, ln.pool, jobID), outputCount(t, ln.pool, jobID))
	}

	status, resp = pullOnce(t, archiveLive, 1)
	if status != http.StatusOK {
		t.Fatalf("pack pull %d: %s", status, resp)
	}
	pack := decodeOneAssignment(t, resp)
	if got, err := jsonUUID(pack["job_id"]); err != nil || got != jobID {
		t.Fatalf("pack job %v %v", got, err)
	}
	if capID, err := jsonString(pack["capability_id"]); err != nil || capID != "octoconv.archive.zip" {
		t.Fatalf("pack capability %s %v", capID, err)
	}
	var paths struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(pack["parameters"], &paths); err != nil {
		t.Fatalf("pack parameters: %v body=%s", err, pack["parameters"])
	}
	if len(paths.Paths) != 1 || paths.Paths[0] != "docs/photo.webp" {
		t.Fatalf("paths %#v", paths.Paths)
	}
	if err := json.Unmarshal(pack["outputs"], &outputs); err != nil {
		t.Fatalf("pack outputs: %v", err)
	}
	if len(outputs) != 1 || outputs[0].Role != "primary" || outputs[0].Ordinal != 0 || outputs[0].Format != "zip" {
		t.Fatalf("pack outputs %+v", outputs)
	}
	var limits struct {
		MaxOutputBytes int `json:"max_output_bytes"`
	}
	if err := json.Unmarshal(pack["limits"], &limits); err != nil {
		t.Fatalf("limits: %v", err)
	}
	if limits.MaxOutputBytes != int((100<<20)+(1<<20)) {
		t.Fatalf("max output %d", limits.MaxOutputBytes)
	}
	attemptID, err = jsonUUID(pack["attempt_id"])
	if err != nil {
		t.Fatal(err)
	}
	token, err = jsonString(pack["attempt_token"])
	if err != nil {
		t.Fatal(err)
	}
	packed := []byte("zip-bytes")
	archiveLive.store.put(storage.AttemptOutputKey(attemptID, 0), packed)
	status, _, resp = postAttempt(t, archiveLive, attemptPaths(archiveLive.addr, attemptID)[3], token, completeBody(t, "zip", convert.MIMEType("zip"), packed))
	if status != http.StatusOK {
		t.Fatalf("pack complete %d: %s", status, resp)
	}
	if jobStatusOf(t, ln.pool, jobID) != "done" {
		t.Fatalf("after pack %s", jobStatusOf(t, ln.pool, jobID))
	}
	progress := jobProgress(t, ln.pool, jobID)
	if progress == nil || *progress != 100 {
		t.Fatalf("progress %v", progress)
	}
	var target, outFormat, filename string
	if err := ln.pool.QueryRow(context.Background(), `
		SELECT j.target_format, o.format, o.filename
		FROM jobs j
		JOIN job_outputs o ON o.job_id = j.id
		WHERE j.id = $1`, jobID).Scan(&target, &outFormat, &filename); err != nil {
		t.Fatal(err)
	}
	if target != "webp" || outFormat != "zip" || filename != "out.zip" {
		t.Fatalf("target %s output %s filename %s", target, outFormat, filename)
	}
}

type namedMember struct {
	name string
	body []byte
}

func extractPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D,
		0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x64,
		0x00, 0x00, 0x00, 0x64,
	}
}

func extractMemberZip(t *testing.T, members []namedMember) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	dir := &zip.FileHeader{Name: "docs/"}
	dir.SetMode(fs.ModeDir | 0o755)
	if _, err := zw.CreateHeader(dir); err != nil {
		t.Fatal(err)
	}
	for _, member := range members {
		w, err := zw.Create(member.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(member.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func insertExtractionJob(t *testing.T, pool *pgxpool.Pool, store *memStore, clientID uuid.UUID, zipBytes []byte) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO jobs (
			id, client_id, operation, engine, status, source_format, target_format,
			dispatch_owner, capability_id, capability_version, protocol_deadline,
			options, created_at
		) VALUES (
			$1, $2, 'convert', 'archive', 'queued', 'png', 'webp',
			'protocol', 'octoconv.archive.zip', 1, now() + interval '1 hour',
			$3::jsonb, $4
		)`, id, clientID, `{
			"archive_extraction": {
				"phase": "unpack",
				"pair_capability_id": "octoconv.convert.image",
				"pair_capability_version": 1,
				"pair_engine": "image",
				"source": "png",
				"target": "webp",
				"operation": "convert",
				"opts": {},
				"entries": [],
				"next": 0
			}
		}`, time.Now())
	if err != nil {
		t.Fatalf("insert job: %s", redactDB(err))
	}
	key := storage.InputKey(id, 0, "photo.zip")
	_, err = pool.Exec(context.Background(), `
		INSERT INTO job_inputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type)
		VALUES ($1, 0, $2, 'photo.zip', 'zip', $3, 'application/zip')`,
		id, key, len(zipBytes))
	if err != nil {
		t.Fatalf("insert input: %s", redactDB(err))
	}
	store.put(key, zipBytes)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM worker_slot_holds WHERE job_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM jobs WHERE id = $1`, id)
	})
	return id
}

func inputKeyOf(t *testing.T, href string) string {
	t.Helper()
	parsed, err := url.Parse(href)
	if err != nil {
		t.Fatal(err)
	}
	key := parsed.Query().Get("key")
	if key == "" {
		t.Fatalf("href %s", href)
	}
	return key
}

func TestExtractionResultName_OneExtension(t *testing.T) {
	if got := extractionResultName("docs/photo.png", "webp"); got != "docs/photo.webp" {
		t.Fatalf("png stem %s", got)
	}
	if got := extractionResultName("docs/photo.tar.gz", "webp"); got != "docs/photo.tar.webp" {
		t.Fatalf("tar.gz stem %s", got)
	}
}

func TestExtract_TwoEntriesSerialAndProgressKept(t *testing.T) {
	ln := startWorkerListener(t)
	clearArchiveClaimJobs(t, ln.pool)
	archiveLive := ln.hello(t, archiveClaimManifest(t))
	imageLive := ln.hello(t, imageLegacyManifest(t, "octoconv.convert.image", "png", "webp"))
	photo := extractPNG()
	scan := extractPNG()
	scan[19] = 0x65
	zipBytes := extractMemberZip(t, []namedMember{
		{name: "docs/photo.png", body: photo},
		{name: "docs/scan.png", body: scan},
	})
	clientID := insertClient(t, ln.pool)
	jobID := insertExtractionJob(t, ln.pool, archiveLive.store, clientID, zipBytes)

	status, resp := pullOnce(t, archiveLive, 1)
	if status != http.StatusOK {
		t.Fatalf("unpack pull %d: %s", status, resp)
	}
	unpack := decodeOneAssignment(t, resp)
	if got, err := jsonUUID(unpack["job_id"]); err != nil || got != jobID {
		t.Fatalf("unpack job %v %v", got, err)
	}
	var outputs []fileLink
	if err := json.Unmarshal(unpack["outputs"], &outputs); err != nil {
		t.Fatalf("outputs: %v", err)
	}
	if len(outputs) != 2 {
		t.Fatalf("unpack outputs %+v", outputs)
	}
	for i, out := range outputs {
		if out.Role != "primary" || out.Ordinal != i || out.Format != "png" {
			t.Fatalf("unpack output %d %+v", i, out)
		}
	}
	attemptID, err := jsonUUID(unpack["attempt_id"])
	if err != nil {
		t.Fatal(err)
	}
	token, err := jsonString(unpack["attempt_token"])
	if err != nil {
		t.Fatal(err)
	}
	members := [][]byte{photo, scan}
	for i, body := range members {
		archiveLive.store.put(storage.AttemptOutputKey(attemptID, i), body)
	}
	status, _, resp = postAttempt(t, archiveLive, attemptPaths(archiveLive.addr, attemptID)[3], token, completeMembers(t, "png", convert.MIMEType("png"), members))
	if status != http.StatusOK {
		t.Fatalf("unpack complete %d: %s", status, resp)
	}
	if jobStatusOf(t, ln.pool, jobID) != "queued" || outputCount(t, ln.pool, jobID) != 0 {
		t.Fatalf("after unpack status %s outputs %d", jobStatusOf(t, ln.pool, jobID), outputCount(t, ln.pool, jobID))
	}

	first := claimPairMember(t, imageLive, archiveLive.store, jobID, photo)
	webpA := []byte("webp-a")
	imageLive.store.put(storage.AttemptOutputKey(first.attemptID, 0), webpA)
	status, _, resp = postAttempt(t, imageLive, attemptPaths(imageLive.addr, first.attemptID)[3], first.token, completeBody(t, "webp", convert.MIMEType("webp"), webpA))
	if status != http.StatusOK {
		t.Fatalf("first pair complete %d: %s", status, resp)
	}
	if jobStatusOf(t, ln.pool, jobID) != "queued" || outputCount(t, ln.pool, jobID) != 0 {
		t.Fatalf("after first pair status %s outputs %d", jobStatusOf(t, ln.pool, jobID), outputCount(t, ln.pool, jobID))
	}
	tag, err := ln.pool.Exec(context.Background(), `UPDATE jobs SET progress_pct = 40 WHERE id = $1 AND status = 'queued'`, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("progress update affected %d", tag.RowsAffected())
	}

	second := claimPairMember(t, imageLive, archiveLive.store, jobID, scan)
	progress := jobProgress(t, ln.pool, jobID)
	if progress == nil || *progress != 40 {
		t.Fatalf("progress after second claim %v", progress)
	}
	webpB := []byte("webp-b")
	imageLive.store.put(storage.AttemptOutputKey(second.attemptID, 0), webpB)
	status, _, resp = postAttempt(t, imageLive, attemptPaths(imageLive.addr, second.attemptID)[3], second.token, completeBody(t, "webp", convert.MIMEType("webp"), webpB))
	if status != http.StatusOK {
		t.Fatalf("second pair complete %d: %s", status, resp)
	}
	if jobStatusOf(t, ln.pool, jobID) != "queued" || outputCount(t, ln.pool, jobID) != 0 {
		t.Fatalf("after second pair status %s outputs %d", jobStatusOf(t, ln.pool, jobID), outputCount(t, ln.pool, jobID))
	}
	progress = jobProgress(t, ln.pool, jobID)
	if progress == nil || *progress != 40 {
		t.Fatalf("progress after second complete %v", progress)
	}
	if got := doneUsageCount(t, ln.pool, clientID); got != 0 {
		t.Fatalf("usage before pack %d", got)
	}

	status, resp = pullOnce(t, archiveLive, 1)
	if status != http.StatusOK {
		t.Fatalf("pack pull %d: %s", status, resp)
	}
	pack := decodeOneAssignment(t, resp)
	if got, err := jsonUUID(pack["job_id"]); err != nil || got != jobID {
		t.Fatalf("pack job %v %v", got, err)
	}
	if capID, err := jsonString(pack["capability_id"]); err != nil || capID != "octoconv.archive.zip" {
		t.Fatalf("pack capability %s %v", capID, err)
	}
	var paths struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(pack["parameters"], &paths); err != nil {
		t.Fatalf("pack parameters: %v body=%s", err, pack["parameters"])
	}
	if len(paths.Paths) != 2 || paths.Paths[0] != "docs/photo.webp" || paths.Paths[1] != "docs/scan.webp" {
		t.Fatalf("paths %#v", paths.Paths)
	}
	if err := json.Unmarshal(pack["outputs"], &outputs); err != nil {
		t.Fatalf("pack outputs: %v", err)
	}
	if len(outputs) != 1 || outputs[0].Role != "primary" || outputs[0].Ordinal != 0 || outputs[0].Format != "zip" {
		t.Fatalf("pack outputs %+v", outputs)
	}
	attemptID, err = jsonUUID(pack["attempt_id"])
	if err != nil {
		t.Fatal(err)
	}
	token, err = jsonString(pack["attempt_token"])
	if err != nil {
		t.Fatal(err)
	}
	packed := []byte("zip-bytes")
	archiveLive.store.put(storage.AttemptOutputKey(attemptID, 0), packed)
	status, _, resp = postAttempt(t, archiveLive, attemptPaths(archiveLive.addr, attemptID)[3], token, completeBody(t, "zip", convert.MIMEType("zip"), packed))
	if status != http.StatusOK {
		t.Fatalf("pack complete %d: %s", status, resp)
	}
	if jobStatusOf(t, ln.pool, jobID) != "done" {
		t.Fatalf("after pack %s", jobStatusOf(t, ln.pool, jobID))
	}
	progress = jobProgress(t, ln.pool, jobID)
	if progress == nil || *progress != 100 {
		t.Fatalf("progress %v", progress)
	}
	var target string
	if err := ln.pool.QueryRow(context.Background(), `SELECT target_format FROM jobs WHERE id = $1`, jobID).Scan(&target); err != nil {
		t.Fatal(err)
	}
	if target != "webp" {
		t.Fatalf("target %s", target)
	}
	if got := doneUsageCount(t, ln.pool, clientID); got != 1 {
		t.Fatalf("usage %d", got)
	}
}

type claimedMember struct {
	attemptID uuid.UUID
	token     string
}

func claimPairMember(t *testing.T, imageLive liveSession, store *memStore, jobID uuid.UUID, want []byte) claimedMember {
	t.Helper()
	status, resp := pullOnce(t, imageLive, 1)
	if status != http.StatusOK {
		t.Fatalf("pair pull %d: %s", status, resp)
	}
	pair := decodeOneAssignment(t, resp)
	if got, err := jsonUUID(pair["job_id"]); err != nil || got != jobID {
		t.Fatalf("pair job %v %v", got, err)
	}
	if capID, err := jsonString(pair["capability_id"]); err != nil || capID != "octoconv.convert.image" {
		t.Fatalf("pair capability %s %v", capID, err)
	}
	if raw := pair["parameters"]; bytes.Contains(raw, []byte("archive_extraction")) {
		t.Fatalf("pair parameters %s", raw)
	}
	var inputs []fileLink
	if err := json.Unmarshal(pair["inputs"], &inputs); err != nil {
		t.Fatalf("pair inputs: %v", err)
	}
	if len(inputs) != 1 || inputs[0].Ordinal != 0 || inputs[0].Format != "png" {
		t.Fatalf("pair inputs %+v", inputs)
	}
	member, ok := store.get(inputKeyOf(t, inputs[0].Href))
	if !ok || !bytes.Equal(member, want) {
		t.Fatal("pair input bytes")
	}
	attemptID, err := jsonUUID(pair["attempt_id"])
	if err != nil {
		t.Fatal(err)
	}
	token, err := jsonString(pair["attempt_token"])
	if err != nil {
		t.Fatal(err)
	}
	return claimedMember{attemptID: attemptID, token: token}
}

func completeMembers(t *testing.T, format, contentType string, payloads [][]byte) []byte {
	t.Helper()
	arts := make([]any, len(payloads))
	for i, payload := range payloads {
		sum := sha256.Sum256(payload)
		arts[i] = map[string]any{
			"role":         "primary",
			"ordinal":      i,
			"format":       format,
			"content_type": contentType,
			"sha256":       hex.EncodeToString(sum[:]),
			"byte_size":    len(payload),
		}
	}
	return mustJSON(t, map[string]any{
		"protocol_version": 2,
		"artifacts":        arts,
	})
}
