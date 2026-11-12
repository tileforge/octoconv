package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/db"
	"github.com/tileforge/octoconv/internal/jobs"
	"github.com/tileforge/octoconv/internal/workerproto"
)

func TestCadProtocolWorkerCompletesEightPairs(t *testing.T) {
	if os.Getenv("CAD_PROTOCOL_PROOF") == "" {
		t.Skip("CAD_PROTOCOL_PROOF unset")
	}
	rawURL := os.Getenv("DATABASE_URL")
	if rawURL == "" {
		t.Fatal("DATABASE_URL is empty")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Port() == "15432" {
		t.Fatal("DATABASE_URL")
	}

	workerBin := os.Getenv("CAD_PROTOCOL_WORKER_BIN")
	fixtureDir := os.Getenv("CAD_FIXTURE_DIR")
	manifestDir := os.Getenv("CAD_MANIFEST_DIR")
	if workerBin == "" || fixtureDir == "" || manifestDir == "" {
		t.Fatal("proof paths")
	}

	step := readProof(t, filepath.Join(fixtureDir, "units_inch.step"))
	dxf := readProof(t, filepath.Join(fixtureDir, "legacy_cp1251.dxf"))
	stl := readProof(t, filepath.Join(fixtureDir, "stl_ascii_box.stl"))
	iges := proofIGES(t, filepath.Join(fixtureDir, "units_inch.step"))

	ctx := context.Background()
	pool, err := db.Connect(ctx)
	if err != nil {
		t.Fatal("postgres")
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal("migrate")
	}
	resetProtocolTables(t, pool)
	clientID := insertProtocolClient(t, pool)

	convertManifest := readProof(t, filepath.Join(manifestDir, "manifest-cad-convert.json"))
	renderManifest := readProof(t, filepath.Join(manifestDir, "manifest-cad-render.json"))
	repo := workerproto.NewRepo(pool)
	ca, caKey := mintCA(t)
	convertID := uuid.New()
	renderID := uuid.New()
	convertCert := mintClient(t, ca, caKey, convertID)
	renderCert := mintClient(t, ca, caKey, renderID)
	serverCert := mintIPServer(t, ca, caKey)
	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	writeCertPEM(t, caFile, [][]byte{ca.Raw})
	convertFiles := writeClientMaterial(t, dir, "convert", convertCert, convertManifest)
	renderFiles := writeClientMaterial(t, dir, "render", renderCert, renderManifest)
	if err := repo.Approve(ctx, convertID, convertManifest); err != nil {
		t.Fatal(err)
	}
	if err := repo.Approve(ctx, renderID, renderManifest); err != nil {
		t.Fatal(err)
	}

	store := newObjectBytes()
	fileAddr := serveTLS(t, fileServerConfig(serverCert), store, true)
	store.setBase("https://" + fileAddr)
	coreCfg, err := workerproto.ServerTLSConfig(repo)
	if err != nil {
		t.Fatal(err)
	}
	poolCerts := x509.NewCertPool()
	poolCerts.AddCert(ca)
	coreCfg.ClientCAs = poolCerts
	coreCfg.Certificates = []tls.Certificate{serverCert}
	coreCfg.NextProtos = []string{"h2", "http/1.1"}
	coreAddr := serveTLS(t, coreCfg, workerproto.NewHandler(repo, jobs.NewRepo(pool), store, 3*time.Minute).Routes(), true)

	jobRepo := jobs.NewRepo(pool)
	convertJobs := []proofJob{
		{op: convert.OperationConvert, from: "step", to: "stl", name: "part.step", body: step, cap: "octoconv.convert.cad"},
		{op: convert.OperationConvert, from: "iges", to: "stl", name: "part.iges", body: iges, cap: "octoconv.convert.cad"},
		{op: convert.OperationConvert, from: "dxf", to: "svg", name: "part.dxf", body: dxf, cap: "octoconv.convert.cad"},
		{op: convert.OperationConvert, from: "dxf", to: "pdf", name: "part.dxf", body: dxf, cap: "octoconv.convert.cad"},
	}
	renderJobs := []proofJob{
		{op: convert.OperationRender, from: "step", to: "png", name: "part.step", body: step, cap: "octoconv.render.cad"},
		{op: convert.OperationRender, from: "iges", to: "png", name: "part.iges", body: iges, cap: "octoconv.render.cad"},
		{op: convert.OperationRender, from: "stl", to: "png", name: "part.stl", body: stl, cap: "octoconv.render.cad"},
		{op: convert.OperationRender, from: "dxf", to: "png", name: "part.dxf", body: dxf, cap: "octoconv.render.cad"},
	}
	runProofWorker(t, pool, workerBin, "https://"+coreAddr, convertID, convertFiles, caFile, insertProofJobs(t, jobRepo, store, clientID, convertJobs))
	runProofWorker(t, pool, workerBin, "https://"+coreAddr, renderID, renderFiles, caFile, insertProofJobs(t, jobRepo, store, clientID, renderJobs))
}

type proofJob struct {
	op, from, to, name, cap string
	body                    []byte
}

type proofMaterial struct {
	cert     string
	key      string
	manifest string
}

func writeClientMaterial(t *testing.T, dir, name string, cert tls.Certificate, manifest []byte) proofMaterial {
	t.Helper()
	out := proofMaterial{
		cert:     filepath.Join(dir, name+".pem"),
		key:      filepath.Join(dir, name+".key"),
		manifest: filepath.Join(dir, name+".json"),
	}
	writeCertPEM(t, out.cert, cert.Certificate)
	writeKeyPEM(t, out.key, cert.PrivateKey)
	if err := os.WriteFile(out.manifest, manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	return out
}

func insertProofJobs(t *testing.T, repo *jobs.Repo, store *objectBytes, clientID uuid.UUID, batch []proofJob) []uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, 0, len(batch))
	for _, job := range batch {
		id := uuid.New()
		key := "uploads/" + id.String() + "/0-" + job.name
		if err := store.Upload(context.Background(), key, bytes.NewReader(job.body), int64(len(job.body)), "application/octet-stream"); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Create(context.Background(), jobs.CreateParams{
			ID:                    id,
			ClientID:              clientID,
			Operation:             job.op,
			Engine:                convert.EngineCAD,
			SourceFormat:          job.from,
			TargetFormat:          job.to,
			Input:                 jobs.Input{Ordinal: 0, ObjectKey: key, Filename: job.name, Format: job.from, SizeBytes: int64(len(job.body)), ContentType: "application/octet-stream"},
			CapabilityID:          job.cap,
			CapabilityVersion:     1,
			DispatchOwner:         "protocol",
			ProtocolAttemptBudget: 3 * time.Minute,
		}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func runProofWorker(t *testing.T, pool *pgxpool.Pool, bin, core string, workerID uuid.UUID, mat proofMaterial, caFile string, ids []uuid.UUID) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = []string{
		"PATH=/opt/venv/bin:/usr/local/bin:/usr/bin:/bin",
		"HOME=/tmp",
		"CAD_PROTOCOL_CORE_URL=" + core,
		"CAD_PROTOCOL_CLIENT_CERT=" + mat.cert,
		"CAD_PROTOCOL_CLIENT_KEY=" + mat.key,
		"CAD_PROTOCOL_CLIENT_CA=" + caFile,
		"CAD_PROTOCOL_WORKER_ID=" + workerID.String(),
		"CAD_PROTOCOL_MANIFEST_PATH=" + mat.manifest,
		"CAD_WORKER_CONCURRENCY=1",
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- cmd.Wait() }()
	environ, err := os.ReadFile("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/environ")
	if err != nil {
		cancel()
		<-errCh
		t.Fatal(err)
	}
	for _, field := range bytes.Split(environ, []byte{0}) {
		if bytes.HasPrefix(field, []byte("DATABASE_URL=")) || bytes.HasPrefix(field, []byte("REDIS_ADDR=")) {
			cancel()
			<-errCh
			t.Fatal("worker environment has a store address")
		}
	}
	deadline := time.Now().Add(8 * time.Minute)
	for {
		if time.Now().After(deadline) {
			cancel()
			<-errCh
			t.Fatalf("jobs unfinished\n%s", output.String())
		}
		select {
		case waitErr := <-errCh:
			t.Fatalf("worker exited: %v\n%s", waitErr, output.String())
		default:
		}
		finished := true
		for _, id := range ids {
			var status, engine, owner, code, message string
			if err := pool.QueryRow(context.Background(), `
				SELECT status, engine, dispatch_owner, COALESCE(error_code, ''), COALESCE(error_message, '')
				FROM jobs WHERE id = $1`, id).Scan(&status, &engine, &owner, &code, &message); err != nil {
				cancel()
				<-errCh
				t.Fatal(err)
			}
			if status == "failed" {
				cancel()
				<-errCh
				t.Fatalf("job %s failed %s %s\n%s", id, code, message, output.String())
			}
			if status != "done" || engine != convert.EngineCAD || owner != "protocol" {
				finished = false
			}
		}
		if finished {
			cancel()
			<-errCh
			return
		}
		time.Sleep(time.Second)
	}
}

func readProof(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func proofIGES(t *testing.T, stepPath string) []byte {
	t.Helper()
	out := filepath.Join(t.TempDir(), "part.iges")
	cmd := exec.Command("/opt/venv/bin/python", "-I", "-B", "-c", proofIGESProgram, stepPath, out)
	cmd.Env = []string{"HOME=/tmp", "PATH=/opt/venv/bin:/usr/bin:/bin"}
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("iges export: %v\n%s", err, raw)
	}
	return readProof(t, out)
}

const proofIGESProgram = `
import sys
from OCP.STEPControl import STEPControl_Reader
from OCP.IGESControl import IGESControl_Writer
from OCP.IFSelect import IFSelect_RetDone
reader = STEPControl_Reader()
if reader.ReadFile(sys.argv[1]) != IFSelect_RetDone:
    raise SystemExit("step read")
reader.TransferRoots()
writer = IGESControl_Writer()
writer.AddShape(reader.OneShape())
if not writer.Write(sys.argv[2]):
    raise SystemExit("iges write")
`
