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
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/jobs"
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

type extractionPair struct {
	source     string
	target     string
	pairID     string
	pairEngine string
	operation  string
}

func insertExtractionJob(t *testing.T, pool *pgxpool.Pool, store *memStore, clientID uuid.UUID, zipBytes []byte) uuid.UUID {
	t.Helper()
	return insertExtractionJobFor(t, pool, store, clientID, zipBytes, extractionPair{
		source: "png", target: "webp", pairID: "octoconv.convert.image", pairEngine: "image",
	})
}

func insertExtractionJobFor(t *testing.T, pool *pgxpool.Pool, store *memStore, clientID uuid.UUID, zipBytes []byte, pair extractionPair) uuid.UUID {
	t.Helper()
	id := uuid.New()
	operation := pair.operation
	if operation == "" {
		operation = "convert"
	}
	opts, err := json.Marshal(map[string]any{
		"archive_extraction": map[string]any{
			"phase":                   "unpack",
			"pair_capability_id":      pair.pairID,
			"pair_capability_version": 1,
			"pair_engine":             pair.pairEngine,
			"source":                  pair.source,
			"target":                  pair.target,
			"operation":               operation,
			"opts":                    map[string]any{},
			"entries":                 []any{},
			"next":                    0,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(context.Background(), `
		INSERT INTO jobs (
			id, client_id, operation, engine, status, source_format, target_format,
			dispatch_owner, capability_id, capability_version, protocol_deadline,
			options, created_at
		) VALUES (
			$1, $2, $3, 'archive', 'queued', $4, $5,
			'protocol', 'octoconv.archive.zip', 1, now() + interval '1 hour',
			$6::jsonb, $7
		)`, id, clientID, operation, pair.source, pair.target, opts, time.Now())
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

func TestExtract_SniffMismatchFailsWholeJob(t *testing.T) {
	png := extractPNG()
	t.Run("text member", func(t *testing.T) {
		raw := extractMemberZip(t, []namedMember{
			{name: "docs/photo.png", body: png},
			{name: "docs/note.txt", body: []byte("hello")},
		})
		expectUnpackFailed(t, raw, pngPair, [][]byte{png, []byte("hello")})
	})
	t.Run("plain zip member", func(t *testing.T) {
		inner := bareStoredZip(t, "inside.txt", []byte("in"))
		raw := extractMemberZip(t, []namedMember{{name: "docs/nested.zip", body: inner}})
		expectUnpackFailed(t, raw, pngPair, [][]byte{inner})
	})
	t.Run("docx source", func(t *testing.T) {
		docx := officeDocx(t)
		raw := extractMemberZip(t, []namedMember{{name: "docs/file.docx", body: docx}})
		jobID := expectUnpackSettled(t, raw, extractionPair{
			source: "docx", target: "pdf", pairID: "octoconv.convert.document", pairEngine: "document",
		}, [][]byte{docx}, "queued")
		if jobStatusOf(t, jobID.pool, jobID.id) == "failed" {
			t.Fatal("docx member failed")
		}
	})
}

func TestExtract_DSStoreIsNotSkipped(t *testing.T) {
	png := extractPNG()
	raw := extractMemberZip(t, []namedMember{
		{name: "docs/photo.png", body: png},
		{name: ".DS_Store", body: []byte("junk")},
	})
	expectUnpackFailed(t, raw, pngPair, [][]byte{png, []byte("junk")})
}

func TestExtract_ResultPathCollisionFailsBeforePack(t *testing.T) {
	png := extractPNG()
	other := append([]byte(nil), png...)
	other[len(other)-1] = 0x65
	t.Run("same result name", func(t *testing.T) {
		raw := extractMemberZip(t, []namedMember{
			{name: "docs/a.png", body: png},
			{name: "docs/a.jpg", body: other},
		})
		settled := expectUnpackSettled(t, raw, pngPair, [][]byte{png, other}, "failed")
		var opts string
		if err := settled.pool.QueryRow(context.Background(), `SELECT options::text FROM jobs WHERE id = $1`, settled.id).Scan(&opts); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(opts, `"paths"`) {
			t.Fatalf("options %s", opts)
		}
		status, resp := pullOnce(t, settled.image, 1)
		if status != http.StatusOK {
			t.Fatalf("pair pull %d: %s", status, resp)
		}
		assertEmptyPull(t, resp)
	})
	t.Run("case distinct", func(t *testing.T) {
		raw := extractMemberZip(t, []namedMember{
			{name: "docs/A.png", body: png},
			{name: "docs/a.jpg", body: other},
		})
		expectUnpackSettled(t, raw, pngPair, [][]byte{png, other}, "queued")
	})
}

func TestExtract_NoFileMembersFails(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		expectClaimRefusal(t, emptyZip(t))
	})
	t.Run("directory only", func(t *testing.T) {
		expectClaimRefusal(t, directoryZip(t, "docs/"))
		expectClaimRefusal(t, directoryZip(t, "photo.png/"))
	})
	t.Run("too many", func(t *testing.T) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for i := 0; i < 33; i++ {
			w, err := zw.Create("f" + strings.Repeat("x", 0) + string(rune('a'+i%26)) + ".txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write([]byte("x")); err != nil {
				t.Fatal(err)
			}
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		expectClaimRefusal(t, buf.Bytes())
	})
	t.Run("declared sum", func(t *testing.T) {
		expectClaimRefusal(t, rawZip(t, &zip.FileHeader{
			Name:               "a.bin",
			Method:             zip.Store,
			CompressedSize64:   1,
			UncompressedSize64: (100 << 20) + 1,
		}, []byte{0}))
	})
	t.Run("hostile name", func(t *testing.T) {
		expectClaimRefusal(t, storedZip(t, "../x", []byte("x")))
		expectClaimRefusal(t, dirPlusFile(t, "//", "a.txt", []byte("a")))
	})
	t.Run("symlink", func(t *testing.T) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		h := &zip.FileHeader{Name: "link", Method: zip.Store}
		h.SetMode(fs.ModeSymlink | 0o777)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("target")); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		expectClaimRefusal(t, buf.Bytes())
	})
	t.Run("encrypted", func(t *testing.T) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		h := &zip.FileHeader{Name: "a.txt", Method: zip.Store, Flags: 1}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("secret")); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		expectClaimRefusal(t, buf.Bytes())
	})
	t.Run("ratio", func(t *testing.T) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, err := zw.Create("zeros.bin")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(bytes.Repeat([]byte{0}, 64<<10)); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		expectClaimRefusal(t, buf.Bytes())
	})
	t.Run("zero compressed", func(t *testing.T) {
		expectClaimRefusal(t, rawZip(t, &zip.FileHeader{
			Name:               "a.txt",
			Method:             zip.Store,
			CRC32:              1,
			CompressedSize64:   0,
			UncompressedSize64: 10,
		}, nil))
	})
}

var pngPair = extractionPair{source: "png", target: "webp", pairID: "octoconv.convert.image", pairEngine: "image"}

type settledJob struct {
	id    uuid.UUID
	pool  *pgxpool.Pool
	image liveSession
}

func expectClaimRefusal(t *testing.T, zipBytes []byte) {
	t.Helper()
	ln := startWorkerListener(t)
	clearArchiveClaimJobs(t, ln.pool)
	archiveLive := ln.hello(t, archiveClaimManifest(t))
	clientID := insertClient(t, ln.pool)
	jobID := insertExtractionJob(t, ln.pool, archiveLive.store, clientID, zipBytes)
	beforeGet, beforePut := archiveLive.store.signerCounts()
	status, resp := pullOnce(t, archiveLive, 1)
	if status != http.StatusOK {
		t.Fatalf("pull %d: %s", status, resp)
	}
	assertEmptyPull(t, resp)
	if jobStatusOf(t, ln.pool, jobID) != "failed" || jobErrorCode(t, ln.pool, jobID) != "engine_error" {
		t.Fatalf("status %s code %s", jobStatusOf(t, ln.pool, jobID), jobErrorCode(t, ln.pool, jobID))
	}
	if outputCount(t, ln.pool, jobID) != 0 {
		t.Fatal("outputs")
	}
	progress := jobProgress(t, ln.pool, jobID)
	if progress != nil && *progress == 100 {
		t.Fatalf("progress %d", *progress)
	}
	gets, puts := archiveLive.store.signerCounts()
	if gets != beforeGet || puts != beforePut {
		t.Fatalf("signer %d/%d -> %d/%d", beforeGet, beforePut, gets, puts)
	}
}

func expectUnpackFailed(t *testing.T, zipBytes []byte, pair extractionPair, bodies [][]byte) {
	t.Helper()
	expectUnpackSettled(t, zipBytes, pair, bodies, "failed")
}

func expectUnpackSettled(t *testing.T, zipBytes []byte, pair extractionPair, bodies [][]byte, want string) settledJob {
	t.Helper()
	ln := startWorkerListener(t)
	clearArchiveClaimJobs(t, ln.pool)
	archiveLive := ln.hello(t, archiveClaimManifest(t))
	var image liveSession
	if pair.pairEngine == "image" {
		image = ln.hello(t, imageLegacyManifest(t, pair.pairID, pair.source, pair.target))
	}
	clientID := insertClient(t, ln.pool)
	jobID := insertExtractionJobFor(t, ln.pool, archiveLive.store, clientID, zipBytes, pair)
	status, resp := pullOnce(t, archiveLive, 1)
	if status != http.StatusOK {
		t.Fatalf("unpack pull %d: %s", status, resp)
	}
	unpack := decodeOneAssignment(t, resp)
	attemptID, err := jsonUUID(unpack["attempt_id"])
	if err != nil {
		t.Fatal(err)
	}
	token, err := jsonString(unpack["attempt_token"])
	if err != nil {
		t.Fatal(err)
	}
	for i, body := range bodies {
		archiveLive.store.put(storage.AttemptOutputKey(attemptID, i), body)
	}
	status, _, resp = postAttempt(t, archiveLive, attemptPaths(archiveLive.addr, attemptID)[3], token, completeMembers(t, pair.source, convert.MIMEType(pair.source), bodies))
	if status != http.StatusOK {
		t.Fatalf("unpack complete %d: %s", status, resp)
	}
	if jobStatusOf(t, ln.pool, jobID) != want {
		t.Fatalf("status %s want %s", jobStatusOf(t, ln.pool, jobID), want)
	}
	if outputCount(t, ln.pool, jobID) != 0 {
		t.Fatal("outputs")
	}
	progress := jobProgress(t, ln.pool, jobID)
	if progress != nil && *progress == 100 {
		t.Fatalf("progress %d", *progress)
	}
	return settledJob{id: jobID, pool: ln.pool, image: image}
}

func emptyZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func directoryZip(t *testing.T, name string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: name}
	h.SetMode(fs.ModeDir | 0o755)
	if _, err := zw.CreateHeader(h); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func storedZip(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func bareStoredZip(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	return storedZip(t, name, body)
}

func dirPlusFile(t *testing.T, dirName, fileName string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: dirName}
	h.SetMode(fs.ModeDir | 0o755)
	if _, err := zw.CreateHeader(h); err != nil {
		t.Fatal(err)
	}
	w, err := zw.Create(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func rawZip(t *testing.T, h *zip.FileHeader, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateRaw(h)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > 0 {
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func officeDocx(t *testing.T) []byte {
	t.Helper()
	return storedZip(t, "word/document.xml", []byte(`<?xml version="1.0"?><document/>`))
}

func TestExtract_FailDoesNotStore100(t *testing.T) {
	t.Run("fail", func(t *testing.T) {
		ln, archiveLive, jobID, attemptID, token := claimUnpackAttempt(t)
		if _, err := ln.pool.Exec(context.Background(), `UPDATE jobs SET progress_pct = 40 WHERE id = $1 AND status = 'active'`, jobID); err != nil {
			t.Fatal(err)
		}
		status, _, resp := postAttempt(t, archiveLive, attemptPaths(archiveLive.addr, attemptID)[2], token, []byte(`{"protocol_version":2,"engine_code":"engine_error","message":"engine failed"}`))
		if status != http.StatusOK {
			t.Fatalf("fail %d: %s", status, resp)
		}
		assertControlOnly(t, resp)
		if bytes.HasPrefix(resp, []byte("PK")) {
			t.Fatal("fail returned an archive")
		}
		assertTerminalUnpublished(t, ln.pool, jobID, "failed")
		if jobErrorCode(t, ln.pool, jobID) != "engine_error" {
			t.Fatalf("code %s", jobErrorCode(t, ln.pool, jobID))
		}
		progress := jobProgress(t, ln.pool, jobID)
		if progress == nil || *progress != 40 {
			t.Fatalf("progress %v", progress)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		ln, archiveLive, jobID, attemptID, token := claimUnpackAttempt(t)
		if _, err := ln.pool.Exec(context.Background(), `UPDATE jobs SET progress_pct = 40 WHERE id = $1 AND status = 'active'`, jobID); err != nil {
			t.Fatal(err)
		}
		if err := jobs.NewRepo(ln.pool).Cancel(context.Background(), jobID); err != nil {
			t.Fatal(err)
		}
		assertTerminalUnpublished(t, ln.pool, jobID, "canceled")
		if holdState(t, ln.pool, attemptID) != "canceled" {
			t.Fatalf("hold %s", holdState(t, ln.pool, attemptID))
		}
		status, _, resp := postAttempt(t, archiveLive, attemptPaths(archiveLive.addr, attemptID)[4], token, []byte(`{"protocol_version":2,"reason":"cancel"}`))
		if status != http.StatusOK {
			t.Fatalf("stopped %d: %s", status, resp)
		}
		assertControlOnly(t, resp)
		if bytes.HasPrefix(resp, []byte("PK")) {
			t.Fatal("cancel returned an archive")
		}
		assertTerminalUnpublished(t, ln.pool, jobID, "canceled")
		progress := jobProgress(t, ln.pool, jobID)
		if progress == nil || *progress != 40 {
			t.Fatalf("progress %v", progress)
		}
		if holdState(t, ln.pool, attemptID) != "stopped" {
			t.Fatalf("hold after ack %s", holdState(t, ln.pool, attemptID))
		}
	})
}

func claimUnpackAttempt(t *testing.T) (workerListener, liveSession, uuid.UUID, uuid.UUID, string) {
	t.Helper()
	ln := startWorkerListener(t)
	clearArchiveClaimJobs(t, ln.pool)
	archiveLive := ln.hello(t, archiveClaimManifest(t))
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
	attemptID, err := jsonUUID(unpack["attempt_id"])
	if err != nil {
		t.Fatal(err)
	}
	token, err := jsonString(unpack["attempt_token"])
	if err != nil {
		t.Fatal(err)
	}
	if jobStatusOf(t, ln.pool, jobID) != "active" {
		t.Fatalf("claimed status %s", jobStatusOf(t, ln.pool, jobID))
	}
	return ln, archiveLive, jobID, attemptID, token
}

func assertTerminalUnpublished(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID, want string) {
	t.Helper()
	if jobStatusOf(t, pool, jobID) != want {
		t.Fatalf("status %s want %s", jobStatusOf(t, pool, jobID), want)
	}
	progress := jobProgress(t, pool, jobID)
	if progress != nil && *progress == 100 {
		t.Fatalf("progress %d", *progress)
	}
	var primary int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM job_outputs WHERE job_id = $1 AND role = 'primary'`, jobID).Scan(&primary); err != nil {
		t.Fatal(err)
	}
	if primary != 0 || outputCount(t, pool, jobID) != 0 {
		t.Fatalf("primary %d outputs %d", primary, outputCount(t, pool, jobID))
	}
}

func TestExtract_SanitizePairDropsReport(t *testing.T) {
	ln := startWorkerListener(t)
	clearArchiveClaimJobs(t, ln.pool)
	archiveLive := ln.hello(t, archiveClaimManifest(t))
	probeLive := ln.hello(t, probeClaimManifest(t))
	png := extractPNG()
	zipBytes := extractMemberZip(t, []namedMember{{name: "docs/photo.png", body: png}})
	clientID := insertClient(t, ln.pool)
	jobID := insertExtractionJobFor(t, ln.pool, archiveLive.store, clientID, zipBytes, extractionPair{
		source: "png", target: "png", pairID: "octoconv.sanitize.probe", pairEngine: "probe", operation: "sanitize",
	})

	status, resp := pullOnce(t, archiveLive, 1)
	if status != http.StatusOK {
		t.Fatalf("unpack pull %d: %s", status, resp)
	}
	unpack := decodeOneAssignment(t, resp)
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

	status, resp = pullOnce(t, probeLive, 1)
	if status != http.StatusOK {
		t.Fatalf("pair pull %d: %s", status, resp)
	}
	pair := decodeOneAssignment(t, resp)
	if capID, err := jsonString(pair["capability_id"]); err != nil || capID != "octoconv.sanitize.probe" {
		t.Fatalf("pair capability %s %v", capID, err)
	}
	var outputs []fileLink
	if err := json.Unmarshal(pair["outputs"], &outputs); err != nil {
		t.Fatalf("outputs: %v", err)
	}
	if len(outputs) != 2 || outputs[0].Role != "primary" || outputs[0].Format != "png" || outputs[1].Role != "report" || outputs[1].Ordinal != 1 || outputs[1].Format != "json" {
		t.Fatalf("pair outputs %+v", outputs)
	}
	attemptID, err = jsonUUID(pair["attempt_id"])
	if err != nil {
		t.Fatal(err)
	}
	token, err = jsonString(pair["attempt_token"])
	if err != nil {
		t.Fatal(err)
	}
	report := []byte(`{"ok":true}`)
	probeLive.store.put(storage.AttemptOutputKey(attemptID, 0), png)
	probeLive.store.put(storage.AttemptOutputKey(attemptID, 1), report)
	status, _, resp = postAttempt(t, probeLive, attemptPaths(probeLive.addr, attemptID)[3], token, probeCompleteBody(t, png, report, false))
	if status != http.StatusOK {
		t.Fatalf("pair complete %d: %s", status, resp)
	}
	if jobStatusOf(t, ln.pool, jobID) != "queued" || outputCount(t, ln.pool, jobID) != 0 {
		t.Fatalf("after pair status %s outputs %d", jobStatusOf(t, ln.pool, jobID), outputCount(t, ln.pool, jobID))
	}
	reportSum := sha256.Sum256(report)
	reportKey := storage.AcceptedOutputKey(jobID, attemptID, 1, hex.EncodeToString(reportSum[:]))
	if _, ok := probeLive.store.get(reportKey); ok {
		t.Fatal("report copy stayed")
	}
	primarySum := sha256.Sum256(png)
	primaryKey := storage.AcceptedOutputKey(jobID, attemptID, 0, hex.EncodeToString(primarySum[:]))
	if _, ok := probeLive.store.get(primaryKey); !ok {
		t.Fatal("primary copy missing")
	}

	status, resp = pullOnce(t, archiveLive, 1)
	if status != http.StatusOK {
		t.Fatalf("pack pull %d: %s", status, resp)
	}
	pack := decodeOneAssignment(t, resp)
	var paths struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(pack["parameters"], &paths); err != nil {
		t.Fatalf("pack parameters: %v", err)
	}
	if len(paths.Paths) != 1 || paths.Paths[0] != "docs/photo.png" {
		t.Fatalf("paths %#v", paths.Paths)
	}
	if err := json.Unmarshal(pack["outputs"], &outputs); err != nil {
		t.Fatalf("pack outputs: %v", err)
	}
	if len(outputs) != 1 || outputs[0].Format != "zip" {
		t.Fatalf("pack outputs %+v", outputs)
	}
}

func TestExtract_QuotaFailureCommits(t *testing.T) {
	t.Run("unpack", func(t *testing.T) {
		ln := startWorkerListener(t)
		clearArchiveClaimJobs(t, ln.pool)
		archiveLive := ln.hello(t, archiveClaimManifest(t))
		png := extractPNG()
		zipBytes := extractMemberZip(t, []namedMember{{name: "docs/photo.png", body: png}})
		clientID := insertClient(t, ln.pool)
		jobID := insertExtractionJob(t, ln.pool, archiveLive.store, clientID, zipBytes)
		limitDailyJobs(t, ln.pool, clientID, "image", 0)

		status, resp := pullOnce(t, archiveLive, 1)
		if status != http.StatusOK {
			t.Fatalf("unpack pull %d: %s", status, resp)
		}
		unpack := decodeOneAssignment(t, resp)
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
		if holdState(t, ln.pool, attemptID) != "failed" {
			t.Fatalf("hold %s", holdState(t, ln.pool, attemptID))
		}
		assertQuotaFailed(t, ln.pool, jobID, clientID, "archive")
	})
	t.Run("pack", func(t *testing.T) {
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
		limitDailyJobs(t, ln.pool, clientID, "archive", 0)

		pair := claimPairMember(t, imageLive, archiveLive.store, jobID, png)
		webp := []byte("webp-a")
		imageLive.store.put(storage.AttemptOutputKey(pair.attemptID, 0), webp)
		status, _, resp = postAttempt(t, imageLive, attemptPaths(imageLive.addr, pair.attemptID)[3], pair.token, completeBody(t, "webp", convert.MIMEType("webp"), webp))
		if status != http.StatusOK {
			t.Fatalf("pair complete %d: %s", status, resp)
		}
		if holdState(t, ln.pool, pair.attemptID) != "failed" {
			t.Fatalf("hold %s", holdState(t, ln.pool, pair.attemptID))
		}
		assertQuotaFailed(t, ln.pool, jobID, clientID, "image")
	})
}

func limitDailyJobs(t *testing.T, pool *pgxpool.Pool, clientID uuid.UUID, engine string, n int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `UPDATE clients SET enforcement_mode = 'enforce' WHERE id = $1`, clientID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO client_limits (client_id, engine, max_daily_jobs)
		VALUES ($1, $2, $3)`, clientID, engine, n); err != nil {
		t.Fatal(err)
	}
}

func assertQuotaFailed(t *testing.T, pool *pgxpool.Pool, jobID, clientID uuid.UUID, engine string) {
	t.Helper()
	assertTerminalUnpublished(t, pool, jobID, "failed")
	var gotEngine, code, message string
	if err := pool.QueryRow(context.Background(), `
		SELECT engine, error_code, error_message FROM jobs WHERE id = $1`, jobID).Scan(&gotEngine, &code, &message); err != nil {
		t.Fatal(err)
	}
	if gotEngine != engine || code != jobs.QuotaReasonDailyJobs || message != "quota" {
		t.Fatalf("engine %s code %s message %s", gotEngine, code, message)
	}
	var status string
	var n int
	if err := pool.QueryRow(context.Background(), `
		SELECT status, job_count FROM client_usage_daily WHERE client_id = $1`, clientID).Scan(&status, &n); err != nil {
		t.Fatalf("usage: %v", err)
	}
	if status != "failed" || n != 1 {
		t.Fatalf("usage %s %d", status, n)
	}
}
