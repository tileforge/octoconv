package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/inspect"
)

// manyEntriesDocxFixture is a REAL, valid docx-shaped zip (word/document.xml
// root part present, so convert.SniffContainer would resolve Format="docx"
// were it allowed to run to completion) plus n additional tiny junk
// entries. Used to exercise zipDirectoryExceedsStructuralBound's own entry-
// count ceiling with a genuinely parseable archive rather than a
// hand-crafted/inconsistent one -- n above inspectMaxZipDirEntries proves
// the bound trips BEFORE the docx root part would otherwise be recognized.
// Takes testing.TB (see mustWriteZipEntry's own doc comment, handlers_test.go)
// so FuzzZipDirectoryGateNeverUnderestimates can seed from
// zipDirectoryDifferentialCorpus directly, at *testing.F setup time.
func manyEntriesDocxFixture(t testing.TB, n int) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteZipEntry(t, zw, "word/document.xml", "root part content")
	for i := 0; i < n; i++ {
		mustWriteZipEntry(t, zw, fmt.Sprintf("junk/%05d.bin", i), "x")
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

func doInspect(t *testing.T, srv *Server, filename string, data []byte) (int, []byte) {
	t.Helper()
	body, ct := multipartBody(t, filename, "", data)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/inspect", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// isobmffTestBox is a size-accurate ISOBMFF box builder (size(4)+type(4)+
// payload), mirroring internal/inspect/media_test.go's own isobmffBox
// helper -- unexported to that package, so duplicated here rather than
// imported, the same cross-package-boundary discipline window.go's own doc
// comment already applies to readWindow. Accurate sizes matter here in a way
// mp4BytesFixture (handlers_test.go, Sniff-only) does not need: findMoov
// performs a REAL top-level box walk over these bytes during analysis, which
// validates every declared size against the buffer and fails closed on a
// mismatch -- unlike convert.Sniff's own fixed 16-byte magic-prefix peek.
func isobmffTestBox(boxType string, payload []byte) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint32(b[0:4], uint32(8+len(payload)))
	copy(b[4:8], boxType)
	return append(b, payload...)
}

// minimalValidMP4Fixture is a small, genuinely valid, fully-parseable MP4:
// ftyp (major brand "isom", matching convert.Sniff's own mp4VideoBrands) +
// moov{mvhd} (a real, well-formed mvhd v0 box declaring timescale=1000,
// duration=5000 -> 5.0s) + a small mdat. No trak/tkhd/stsd is included --
// width/height/declared_codec fall back to an honest malformed_input, which
// is irrelevant to what this fixture exists to prove (Coverage.Complete).
// This is PR9R9's own reproduction fixture: a moov placed directly after
// ftyp (head window covers it in ONE read), well within any default byte
// budget -- the exact shape the orchestrator used to reproduce
// `coverage.complete=false reason="work_ceiling_bytes"` against the real
// HTTP handler before this quick task's fix.
func minimalValidMP4Fixture() []byte {
	mvhdPayload := make([]byte, 20)
	binary.BigEndian.PutUint32(mvhdPayload[12:16], 1000) // timescale
	binary.BigEndian.PutUint32(mvhdPayload[16:20], 5000) // duration -> 5.0s
	mvhd := isobmffTestBox("mvhd", mvhdPayload)
	moov := isobmffTestBox("moov", mvhd)
	ftyp := isobmffTestBox("ftyp", []byte("isom\x00\x00\x02\x00isomiso2avc1mp41"))
	mdat := isobmffTestBox("mdat", make([]byte, 32))

	out := append([]byte{}, ftyp...)
	out = append(out, moov...)
	out = append(out, mdat...)
	return out
}

// wavBytesFixture is a small, genuinely valid, fully-parseable WAV: RIFF/
// WAVE header, a real 16-byte "fmt " chunk (PCM, 44.1kHz, mono, 16-bit), and
// a "data" chunk -- mirrors internal/inspect/media_test.go's own
// minimalWAVFixture (unexported to that package, duplicated here for the
// same reason isobmffTestBox above is).
func wavBytesFixture() []byte {
	const sampleRate, channels, bitsPerSample, dataSize = 44100, 1, 16, 4410 // 0.1s
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8

	fmtChunk := make([]byte, 16)
	binary.LittleEndian.PutUint16(fmtChunk[0:2], 1) // PCM
	binary.LittleEndian.PutUint16(fmtChunk[2:4], channels)
	binary.LittleEndian.PutUint32(fmtChunk[4:8], sampleRate)
	binary.LittleEndian.PutUint32(fmtChunk[8:12], uint32(byteRate))
	binary.LittleEndian.PutUint16(fmtChunk[12:14], uint16(blockAlign))
	binary.LittleEndian.PutUint16(fmtChunk[14:16], bitsPerSample)

	buf := []byte("RIFF")
	buf = append(buf, 0, 0, 0, 0) // RIFF size, unused by the analyzer
	buf = append(buf, []byte("WAVE")...)
	buf = append(buf, []byte("fmt ")...)
	fmtLen := make([]byte, 4)
	binary.LittleEndian.PutUint32(fmtLen, uint32(len(fmtChunk)))
	buf = append(buf, fmtLen...)
	buf = append(buf, fmtChunk...)
	buf = append(buf, []byte("data")...)
	dataLen := make([]byte, 4)
	binary.LittleEndian.PutUint32(dataLen, dataSize)
	buf = append(buf, dataLen...)
	buf = append(buf, make([]byte, dataSize)...)
	return buf
}

// TestInspectValidFilesWithinBudgetReportCompleteCoverage is PR9R9 task 1's
// central regression proof, through the SAME production path the
// orchestrator used to reproduce the defect (POST /v1/inspect, not
// AnalyzeMedia/BuildReport called directly): a small, genuinely valid,
// fully-parseable upload of EVERY family this endpoint dispatches to --
// mp4 (media, the family that actually regressed), pdf, docx (container),
// png (image), wav (media, the sibling audio path through the SAME
// findMoov-free wavCharacteristics code, included to prove the fix is not
// merely mp4-shaped) -- must report Coverage.Complete==true within the
// default work-ceiling budget. Before this fix, the mp4 case alone failed
// with the exact literal orchestrator reproduction:
// `coverage.complete=false reason="work_ceiling_bytes"`. The other four
// families were never affected by media.go's own double-charge (only
// findMoov ever called tr.Consume directly), but pinning them here closes
// the SAME class of regression for every family at once, per this task's
// own requirement not to prove this only for MP4.
func TestInspectValidFilesWithinBudgetReportCompleteCoverage(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		data     []byte
	}{
		{"mp4", "in.mp4", minimalValidMP4Fixture()},
		{"wav", "in.wav", wavBytesFixture()},
		{"pdf", "in.pdf", pdfBytesFixture()},
		{"docx", "in.docx", docxFixture(t)},
		{"png", "in.png", pngBytesFixture()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
			status, body := doInspect(t, srv, tc.filename, tc.data)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", status, body)
			}
			var resp inspectResponse
			if err := json.Unmarshal(body, &resp); err != nil {
				t.Fatalf("decode body: %v; body=%s", err, body)
			}
			if !resp.Coverage.Complete {
				t.Errorf("Coverage.Complete = false (reason=%q), want true -- a small, valid, fully-parsed %s upload well within the default work-ceiling budget must never report selective coverage; REPRODUCED (before this fix, mp4 case): coverage.complete=false reason=%q", resp.Coverage.Reason, tc.name, inspect.ReasonWorkCeilingBytes)
			}
			if resp.Coverage.Reason != "" {
				t.Errorf("Coverage.Reason = %q, want empty alongside Coverage.Complete=true", resp.Coverage.Reason)
			}
		})
	}
}

// TestDispatchInspectFamiliesChargesMediaBudgetExactlyOnce is PR9R9 task 1's
// own tracker-level proof (not merely reasoning, per this task's own
// acceptance criteria) that the shared byte budget is spent EXACTLY ONCE,
// by dispatchInspectFamilies alone, for a media (mp4) dispatch. budget is
// sized generously relative to the tiny fixture but tight enough that a
// SECOND charge -- media.go's own former findMoov->consumeActual call, on
// top of dispatchInspectFamilies's own tr.Consume(maxBytes) -- would push
// consumed past budget and flip the Tracker to exhausted. Remaining() == 0
// afterward additionally pins dispatchInspectFamilies's own documented
// "charge the full allowance" contract: exactly one Consume call, for the
// entire captured maxBytes, no more and no less.
func TestDispatchInspectFamiliesChargesMediaBudgetExactlyOnce(t *testing.T) {
	data := minimalValidMP4Fixture()
	f := memMultipartFile{bytes.NewReader(data)}

	const budget = 1 << 16 // 64 KiB
	tr := inspect.NewTracker(inspect.Budget{MaxBytes: budget, MaxDuration: time.Minute})

	_ = dispatchInspectFamilies("mp4", nil, nil, f, int64(len(data)), tr)

	if tr.Exhausted() {
		t.Fatalf("Tracker(MaxBytes=%d) exhausted after dispatching a %d-byte valid mp4 -- the shared budget was charged more than once (media.go's own findMoov reporting actual bytes read directly to tr, ON TOP OF dispatchInspectFamilies's own tr.Consume(maxBytes) charge); this is PR9R9's own live reproduction mechanism: coverage.complete=false reason=%q for a small valid mp4", int64(budget), len(data), inspect.ReasonWorkCeilingBytes)
	}
	if remaining := tr.Remaining(); remaining != 0 {
		t.Fatalf("Remaining() = %d, want 0 -- dispatchInspectFamilies always charges its FULL maxBytes allowance (tr.Remaining() at call time) exactly once, by design; any other value means a charge was skipped or made more than once", remaining)
	}
}

// TestInspectDetectsByMagicBytesNotExtension proves SC1: a PNG uploaded
// under a .jpg filename is reported as format=png (magic bytes), carries
// declared_format=jpg, and produces an extension_mismatch finding.
func TestInspectDetectsByMagicBytesNotExtension(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	status, body := doInspect(t, srv, "x.jpg", pngBytesFixture())
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}

	var resp inspectResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, body)
	}
	if resp.Format != "png" {
		t.Errorf("Format = %q, want png", resp.Format)
	}
	if resp.DeclaredFormat != "jpg" {
		t.Errorf("DeclaredFormat = %q, want jpg", resp.DeclaredFormat)
	}
	found := false
	for _, f := range resp.Findings {
		if f.Code == inspect.CodeExtensionMismatch {
			found = true
		}
	}
	if !found {
		t.Errorf("expected extension_mismatch finding; findings=%+v", resp.Findings)
	}
}

// TestInspectWritesNothing proves inspect never touches storage or the jobs
// repository -- both fake counters must stay at zero.
func TestInspectWritesNothing(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	status, body := doInspect(t, srv, "in.png", pngBytesFixture())
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	if store.uploadCalls != 0 {
		t.Errorf("storage.Upload called %d times, want 0", store.uploadCalls)
	}
	if repo.created != nil {
		t.Errorf("repo.Create was called: %+v", repo.created)
	}
}

// TestInspectUnrecognizedContentReturns200NotReject proves D-03: content
// matching no known signature is a normal 200 with an empty format and
// honest unknown statuses, never a rejection.
func TestInspectUnrecognizedContentReturns200NotReject(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	garbage := []byte("this is plain text, not a real image, matching no known magic-byte signature")
	status, body := doInspect(t, srv, "in.png", garbage)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}

	var resp inspectResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, body)
	}
	if resp.Format != "" {
		t.Errorf("Format = %q, want empty", resp.Format)
	}
	if len(resp.Operations) != 0 {
		t.Errorf("Operations = %+v, want empty for an unrecognized format", resp.Operations)
	}
	if err := inspect.ValidateReport(resp.Findings, resp.Checks); err != nil {
		t.Errorf("ValidateReport: %v", err)
	}
}

// TestInspectReportSatisfiesUnionInvariant proves a live call against the
// real handler (not just BuildReport in isolation) still satisfies
// ValidateReport, for a representative input from each family.
func TestInspectReportSatisfiesUnionInvariant(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		data     []byte
	}{
		{"png", "in.png", pngBytesFixture()},
		{"pdf", "in.pdf", pdfBytesFixture()},
		{"docx-with-macro", "in.docx", macroDocxFixture(t)},
		{"unrecognized", "in.bin", []byte("not a known format at all")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
			status, body := doInspect(t, srv, tc.filename, tc.data)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", status, body)
			}
			var resp inspectResponse
			if err := json.Unmarshal(body, &resp); err != nil {
				t.Fatalf("decode body: %v; body=%s", err, body)
			}
			if err := inspect.ValidateReport(resp.Findings, resp.Checks); err != nil {
				t.Fatalf("ValidateReport: %v", err)
			}
		})
	}
}

// TestInspectBodyCeilingIsSeparateFromCreateJobCeiling proves
// InspectMaxUploadBytes is a SEPARATE, independently-configurable ceiling
// from MaxUploadBytes: a body between the two ceilings gives 413 on
// /v1/inspect and does NOT give 413 on /v1/jobs.
func TestInspectBodyCeilingIsSeparateFromCreateJobCeiling(t *testing.T) {
	resolver := newFakeResolver()
	srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
		MaxUploadBytes:        10 << 20, // 10 MiB: /v1/jobs' own ceiling
		InspectMaxUploadBytes: 1 << 10,  // 1 KiB: /v1/inspect's own, much smaller ceiling
	})

	// A body between the two ceilings (bigger than 1 KiB, smaller than
	// 10 MiB): must 413 on /v1/inspect...
	oversizedForInspect := paddedPNGFixture(2 << 10) // 2 KiB
	status, body := doInspect(t, srv, "in.png", oversizedForInspect)
	if status != http.StatusRequestEntityTooLarge {
		t.Fatalf("/v1/inspect status = %d, want 413; body=%s", status, body)
	}
	assertErrorCode(t, body, "payload_too_large")

	// ...but must NOT 413 on /v1/jobs, which is still well under its own
	// 10 MiB ceiling.
	jobStatus, jobBody := doPre58CreateJob(t, srv, "in.png", "webp", oversizedForInspect)
	if jobStatus == http.StatusRequestEntityTooLarge {
		t.Fatalf("/v1/jobs incorrectly 413'd on a body under its own ceiling; body=%s", jobBody)
	}
}

// TestInspectPinsNothingTOCTOU is the plan's central D-09 proof: inspecting
// one (benign) file first must NOT change the outcome of creating a job
// with a DIFFERENT (malicious) file afterward. The macro-rejection response
// after a preceding inspect call must be byte-identical to the pre-Phase-58
// golden fixture pinned before this phase touched a single line of
// production code.
func TestInspectPinsNothingTOCTOU(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	// Inspect a harmless PNG first -- same client, same server instance.
	inspectStatus, inspectBody := doInspect(t, srv, "harmless.png", pngBytesFixture())
	if inspectStatus != http.StatusOK {
		t.Fatalf("inspect status = %d, want 200; body=%s", inspectStatus, inspectBody)
	}

	// THEN create a job with a macro-carrying docx -- different bytes
	// entirely. If inspect pinned anything the job-creation path could read,
	// this would behave differently than the pre-Phase-58 golden snapshot.
	jobStatus, jobBody := doPre58CreateJob(t, srv, "in.docx", "pdf", macroDocxFixture(t))

	golden, err := os.ReadFile("testdata/pre58_createjob_rejections.golden.json")
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	var cases map[string]pre58Rejection
	if err := json.Unmarshal(golden, &cases); err != nil {
		t.Fatalf("decode golden fixture: %v", err)
	}
	want, ok := cases["macro_detected"]
	if !ok {
		t.Fatalf("golden fixture missing macro_detected case")
	}

	if jobStatus != want.Status {
		t.Fatalf("status = %d, want %d (golden macro_detected)", jobStatus, want.Status)
	}
	// Compact both sides before comparing: the golden fixture's per-case
	// "body" value is stored pretty-printed (assertGolden58's own
	// json.Encoder.SetIndent, applied to the WHOLE cases map at write time)
	// while writeJSON's live response is compact -- json.Compact
	// normalizes ONLY whitespace, never reordering or dropping a key, so
	// this stays a byte-for-byte proof of the actual JSON value, not a
	// weakened semantic comparison.
	gotCompact := &bytes.Buffer{}
	if err := json.Compact(gotCompact, jobBody); err != nil {
		t.Fatalf("compact live body: %v; body=%s", err, jobBody)
	}
	wantCompact := &bytes.Buffer{}
	if err := json.Compact(wantCompact, want.Body); err != nil {
		t.Fatalf("compact golden body: %v; body=%s", err, want.Body)
	}
	if !bytes.Equal(gotCompact.Bytes(), wantCompact.Bytes()) {
		t.Fatalf("body = %s, want %s (golden macro_detected) -- inspect must pin NOTHING that could change this outcome (D-09)", gotCompact.Bytes(), wantCompact.Bytes())
	}
}

// TestInspectBusyReturns429 proves the inspectConcurrencyGate is actually
// mounted ahead of handleInspect: a saturated semaphore returns 429
// inspect_busy, exactly like inspectgate_test.go's own middleware-level
// proof, but exercised through the real Routes() chain this time.
func TestInspectBusyReturns429(t *testing.T) {
	resolver := newFakeResolver()
	srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
		InspectMaxConcurrent: 1,
	})
	// Manually saturate the one available slot the same way
	// inspectgate_test.go does, without a real concurrent request.
	srv.inspectSem.sem <- struct{}{}
	defer func() { <-srv.inspectSem.sem }()

	status, body := doInspect(t, srv, "in.png", pngBytesFixture())
	if status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body=%s", status, body)
	}
	assertErrorCode(t, body, "inspect_busy")
}

// TestInspectNoAuthHeaderUnauthorized proves the route inherits the SAME
// auth.Middleware every other /v1 route uses.
func TestInspectNoAuthHeaderUnauthorized(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	body, ct := multipartBody(t, "in.png", "", pngBytesFixture())
	req := httptest.NewRequest(http.MethodPost, "/v1/inspect", body) // deliberately no Authorization header
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

// TestInspectZipDirectoryOverBoundReportsUnknownNot500 is T-SLF-01's central
// proof: a REAL, valid docx-shaped zip whose entry count exceeds
// inspectMaxZipDirEntries is reported as format=zip_directory_too_large
// (never the docx root part a full parse would have found), the three
// container-domain codes are unknown/work_ceiling_bytes (never clean, never
// 500), and Coverage is honestly marked incomplete.
func TestInspectZipDirectoryOverBoundReportsUnknownNot500(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	fixture := manyEntriesDocxFixture(t, inspectMaxZipDirEntries+1)
	status, body := doInspect(t, srv, "in.docx", fixture)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (never 500, never a rejection); body=%s", status, body)
	}

	var resp inspectResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, body)
	}
	if resp.Format != inspectZipTooLargeFormat {
		t.Errorf("Format = %q, want %q -- the archive must NOT be resolved to docx (that would mean the bound was never checked)", resp.Format, inspectZipTooLargeFormat)
	}
	if resp.Coverage.Complete {
		t.Errorf("Coverage.Complete = true, want false -- exceeding this handler's own structural ceiling must mark the report selective")
	}
	if resp.Coverage.Reason != inspect.ReasonWorkCeilingBytes {
		t.Errorf("Coverage.Reason = %q, want %q", resp.Coverage.Reason, inspect.ReasonWorkCeilingBytes)
	}

	wantUnknown := map[inspect.Code]bool{
		inspect.CodeOOXMLMacro:        true,
		inspect.CodeZipBombShape:      true,
		inspect.CodeExternalReference: true,
	}
	seen := map[inspect.Code]bool{}
	for _, c := range resp.Checks {
		if !wantUnknown[c.Code] {
			continue
		}
		seen[c.Code] = true
		if c.Status != inspect.StatusUnknown {
			t.Errorf("check %s: status = %q, want %q (never clean)", c.Code, c.Status, inspect.StatusUnknown)
		}
		if c.Reason != inspect.ReasonWorkCeilingBytes {
			t.Errorf("check %s: reason = %q, want %q", c.Code, c.Reason, inspect.ReasonWorkCeilingBytes)
		}
	}
	for code := range wantUnknown {
		if !seen[code] {
			t.Errorf("check %s missing from response entirely", code)
		}
	}
	if err := inspect.ValidateReport(resp.Findings, resp.Checks); err != nil {
		t.Errorf("ValidateReport: %v", err)
	}
}

// failingAfterPrefixContainerFile implements multipart.File (Read,
// ReadAt, Seek, Close), succeeding for a ReadAt exactly at offset 0
// requesting AT MOST the 4-byte PK\x03\x04 prefix detectInspectFormat's own
// ZIP-detection branch reads first, and failing every OTHER call (any
// larger read at offset 0, or any read at a nonzero offset) with failErr --
// codex PR #9 round 8's own reviewer scenario, quoted verbatim in this
// quick task's PLAN.md: "the first four bytes are readable but a later
// ReadAt fails while reading the EOCD or central directory". Go's real
// multipart.File (memory- or disk-backed) offers no portable, deterministic
// way to script a genuine mid-read i/o failure through an actual net/http
// request, so this test exercises detectInspectFormat/dispatchInspectFamilies
// directly -- the exact production functions handleInspect itself calls on
// its own sniff path -- rather than going through httptest's own multipart
// writer.
type failingAfterPrefixContainerFile struct {
	data    []byte
	failErr error
}

func (f *failingAfterPrefixContainerFile) ReadAt(p []byte, off int64) (int, error) {
	if off == 0 && len(p) <= 4 {
		return copy(p, f.data[:4]), nil
	}
	return 0, f.failErr
}
func (f *failingAfterPrefixContainerFile) Read(p []byte) (int, error) { return 0, f.failErr }
func (f *failingAfterPrefixContainerFile) Seek(int64, int) (int64, error) {
	return 0, f.failErr
}
func (f *failingAfterPrefixContainerFile) Close() error { return nil }

// TestInspectSniffReadFailureIsObservableAsReadFailed is PR9R8-F2/F3's own
// central proof: a ReaderAt that can deliver the 4-byte PK\x03\x04 prefix
// but fails on every read after it must surface as unknown/read_failed on
// the SAME production sniff path handleInspect actually runs
// (detectInspectFormat -> dispatchInspectFamilies -> inspect.BuildReport),
// never silently folded into unknown/format_unrecognized -- the exact
// defect the finding names: "detectInspectFormat consequently returns an
// empty format ... the HTTP report labels every check
// unknown/format_unrecognized rather than unknown/read_failed".
func TestInspectSniffReadFailureIsObservableAsReadFailed(t *testing.T) {
	data := macroDocxFixture(t) // real docx-shaped bytes; only the PK prefix is ever actually read through the failing wrapper below
	file := &failingAfterPrefixContainerFile{data: data, failErr: errors.New("simulated disk read failure")}

	detected, sniffed, sniffedZip := detectInspectFormat(file, int64(len(data)), "docx")
	if detected != inspectZipReadFailedFormat {
		t.Fatalf("detected = %q, want %q", detected, inspectZipReadFailedFormat)
	}
	if sniffed != nil {
		t.Fatalf("sniffed = %+v, want nil -- SniffContainer never succeeded", sniffed)
	}

	tr := inspect.NewTracker(inspect.Budget{MaxBytes: 1 << 20, MaxDuration: time.Minute})
	families := dispatchInspectFamilies(detected, sniffed, sniffedZip, file, int64(len(data)), tr)
	families = append(families, extensionMismatchFamilyResult(detected, "docx"))
	report := inspect.BuildReport(detected, "docx", int64(len(data)), tr, families...)

	if err := inspect.ValidateReport(report.Findings, report.Checks); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}

	wantReadFailed := map[inspect.Code]bool{
		inspect.CodeOOXMLMacro:        true,
		inspect.CodeZipBombShape:      true,
		inspect.CodeExternalReference: true,
		inspect.CodeExtensionMismatch: true,
	}
	seen := map[inspect.Code]bool{}
	for _, c := range report.Checks {
		if !wantReadFailed[c.Code] {
			continue
		}
		seen[c.Code] = true
		if c.Status != inspect.StatusUnknown || c.Reason != inspect.ReasonReadFailed {
			t.Errorf("check %s = %+v, want status=unknown reason=read_failed", c.Code, c)
		}
	}
	for code := range wantReadFailed {
		if !seen[code] {
			t.Errorf("check %s missing from report.Checks entirely", code)
		}
	}
	// The defect this test closes: BEFORE this fix, this identical scenario
	// produced format_unrecognized everywhere -- indistinguishable from a
	// genuinely unrecognized upload this handler never even tried to read.
	for _, c := range report.Checks {
		if c.Reason == inspect.ReasonFormatUnrecognized {
			t.Errorf("check %+v carries format_unrecognized -- a genuine ReadAt failure must never be reported the same way as an unrecognized format (PR9R8-F3)", c)
		}
	}
}

// rangeFailContainerFile implements multipart.File (Read, ReadAt, Seek,
// Close): a ReadAt call fails with failErr when its offset falls inside
// [failFrom, failTo) AND its requested length exceeds passThroughMaxLen --
// every other call (outside the offset range, OR inside it but short
// enough) delegates to the real underlying bytes. The multipart.File-shaped
// sibling of internal/convert/docsniff_test.go's own
// failingWithinRangeReaderAt, duplicated here for the identical
// cross-package reason failingAfterPrefixContainerFile above already
// states: this test exercises detectInspectFormat/dispatchInspectFamilies
// directly, since Go's real multipart.File offers no portable way to
// script a genuine mid-read i/o failure confined to one byte range through
// an actual net/http request.
//
// The length threshold exists because detectInspectFormat's OWN magic-byte
// detection already reads small prefixes AT OFFSET 0 -- convert.Sniff's own
// 12-byte peek, then a 4-byte PK\x03\x04 prefix check -- the SAME offset
// archive/zip's own local-header read for the "mimetype" entry (index 0,
// no leading bytes before it) ALSO starts at, since it is the file's first
// entry. Offset alone cannot distinguish "detection peek" from "the read
// this finding is actually about" -- but LENGTH can: Go's own fileHeaderLen
// is a fixed 30 bytes (reader.go), comfortably above both detection peeks
// and below passThroughMaxLen's own margin.
type rangeFailContainerFile struct {
	data              []byte
	failFrom, failTo  int64
	passThroughMaxLen int
	failErr           error
}

func (f *rangeFailContainerFile) ReadAt(p []byte, off int64) (int, error) {
	if off >= f.failFrom && off < f.failTo && len(p) > f.passThroughMaxLen {
		return 0, f.failErr
	}
	if off < 0 || off >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(p, f.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
func (f *rangeFailContainerFile) Read(p []byte) (int, error)     { return 0, f.failErr }
func (f *rangeFailContainerFile) Seek(int64, int) (int64, error) { return 0, f.failErr }
func (f *rangeFailContainerFile) Close() error                   { return nil }

// odfZipFixtureWithFiller mirrors internal/convert/docsniff_test.go's own
// helper of the same name (duplicated here for the same reason
// rangeFailContainerFile above is): mimetype-first ODF shape, plus n
// Deflate-compressed filler entries, solely to push the central directory
// past 1024 bytes -- see that helper's own doc comment (docsniff_test.go)
// for why this matters: archive/zip's own EOCD-search read (reader.go)
// reads the last 1024 bytes of the file FIRST, which starts at offset 0 for
// any file smaller than that, making it impossible to fail ONLY the
// mimetype entry's own read by offset range alone.
func odfZipFixtureWithFiller(t testing.TB, mimetype string, n int) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatalf("CreateHeader(mimetype): %v", err)
	}
	if _, err := fw.Write([]byte(mimetype)); err != nil {
		t.Fatalf("Write(mimetype): %v", err)
	}
	for i := 0; i < n; i++ {
		mustWriteZipEntry(t, zw, fmt.Sprintf("filler/%05d.txt", i), "x")
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// TestInspectMimetypeReadFailureIsObservableAsReadFailed is PR9R9 task 2's
// own end-to-end proof, through the SAME production functions handleInspect
// itself calls (detectInspectFormat -> dispatchInspectFamilies ->
// inspect.BuildReport): a ReadAt failure confined to the "mimetype" entry's
// own local header+data -- AFTER convert.SniffContainer's own zip.NewReader
// call has already read the central directory successfully -- must surface
// as unknown/read_failed, never unknown/format_unrecognized. Before this
// fix, SniffContainer silently discarded readBounded's own error and
// returned (ContainerResult{}, nil): detected stayed "" (no ODF match,
// Format=="", cerr==nil), so detectInspectFormat's own zip branch fell all
// the way through to "leaves detected empty intentionally" -- the SAME
// outcome a genuinely unrecognized, fully-read upload produces.
func TestInspectMimetypeReadFailureIsObservableAsReadFailed(t *testing.T) {
	const failTo = 512
	data := odfZipFixtureWithFiller(t, "application/vnd.oasis.opendocument.text", 40)
	if int64(len(data)) <= 1024 {
		t.Fatalf("test fixture assumption broken: fixture is %d bytes, want > 1024 so archive/zip's own EOCD search window (last 1024 bytes) never reaches offset 0", len(data))
	}
	if searchStart := int64(len(data)) - 1024; searchStart < failTo {
		t.Fatalf("test fixture assumption broken: archive/zip's own EOCD-search read would start at offset %d, INSIDE this test's own failFrom..failTo=[0,%d) range -- add more filler entries", searchStart, failTo)
	}
	file := &rangeFailContainerFile{data: data, failFrom: 0, failTo: failTo, passThroughMaxLen: 20, failErr: errors.New("simulated disk read failure")}

	detected, sniffed, sniffedZip := detectInspectFormat(file, int64(len(data)), "odt")
	if detected != inspectZipReadFailedFormat {
		t.Fatalf("detected = %q, want %q -- a mimetype-entry read failure, observed AFTER the central directory itself was read successfully, must not be silently folded into an unrecognized/empty format", detected, inspectZipReadFailedFormat)
	}
	if sniffed != nil {
		t.Fatalf("sniffed = %+v, want nil -- SniffContainer never returned a usable result", sniffed)
	}

	tr := inspect.NewTracker(inspect.Budget{MaxBytes: 1 << 20, MaxDuration: time.Minute})
	families := dispatchInspectFamilies(detected, sniffed, sniffedZip, file, int64(len(data)), tr)
	families = append(families, extensionMismatchFamilyResult(detected, "odt"))
	report := inspect.BuildReport(detected, "odt", int64(len(data)), tr, families...)

	if err := inspect.ValidateReport(report.Findings, report.Checks); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}

	wantReadFailed := map[inspect.Code]bool{
		inspect.CodeOOXMLMacro:        true,
		inspect.CodeZipBombShape:      true,
		inspect.CodeExternalReference: true,
		inspect.CodeExtensionMismatch: true,
	}
	seen := map[inspect.Code]bool{}
	for _, c := range report.Checks {
		if !wantReadFailed[c.Code] {
			continue
		}
		seen[c.Code] = true
		if c.Status != inspect.StatusUnknown || c.Reason != inspect.ReasonReadFailed {
			t.Errorf("check %s = %+v, want status=unknown reason=read_failed", c.Code, c)
		}
	}
	for code := range wantReadFailed {
		if !seen[code] {
			t.Errorf("check %s missing from report.Checks entirely", code)
		}
	}
	// The defect this test closes: BEFORE this fix, this identical scenario
	// produced format_unrecognized everywhere -- indistinguishable from a
	// genuinely unrecognized upload this handler never even tried to read.
	for _, c := range report.Checks {
		if c.Reason == inspect.ReasonFormatUnrecognized {
			t.Errorf("check %+v carries format_unrecognized -- a genuine mimetype-entry read failure must never be reported the same way as an unrecognized format (PR9R9)", c)
		}
	}
}

// reviewerScenarioZipFixture builds the SMALLEST possible upload that
// reproduces codex's round-2 review scenario end-to-end through the real
// HTTP handler: a PK\x03\x04 local-file-header signature (so
// detectInspectFormat's ZIP branch is entered at all) immediately followed
// by a 22-byte EOCD record declaring entries=1 (65537 mod 65536) and
// dirBytes=65537*46=3,014,702 -- the exact wraparound arithmetic the
// finding gave. No actual central-directory bytes are present: this
// handler's own structural bound must reject the upload from the EOCD's
// declared fields alone, before archive/zip.NewReader (or anything that
// would notice the directory is not really there) ever runs.
func reviewerScenarioZipFixture() []byte {
	wrappedEntries := uint16(65537 % 65536) // == 1
	trueDirBytes := uint32(65537 * zipCentralDirMinRecordLen)

	buf := make([]byte, 4+22)
	buf[0], buf[1], buf[2], buf[3] = 'P', 'K', 3, 4 // local-file-header magic, gates detectInspectFormat's ZIP branch
	eocd := buf[4:]
	eocd[0], eocd[1], eocd[2], eocd[3] = 'P', 'K', 0x05, 0x06
	binary.LittleEndian.PutUint16(eocd[10:12], wrappedEntries)
	binary.LittleEndian.PutUint32(eocd[12:16], trueDirBytes)
	// comment length (last 2 bytes) left at zero.
	return buf
}

// TestInspectReviewerScenarioReportsUnknownNot500 is the plan's central
// PR9R2-F5-EOCD-UINT16-WRAP proof, exercised through the real handler
// end-to-end (not just the bound-check function in isolation): an upload
// whose declared EOCD entry count wraps to 1 while its declared directory
// size matches 65,537 real minimal records must still be reported as
// format=zip_directory_too_large -- never resolved as a real container
// format, never clean, never 500 -- exactly like
// TestInspectZipDirectoryOverBoundReportsUnknownNot500 above proves for the
// round-1 (non-wrapping) over-bound case.
func TestInspectReviewerScenarioReportsUnknownNot500(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	status, body := doInspect(t, srv, "in.zip", reviewerScenarioZipFixture())
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (never 500, never a rejection); body=%s", status, body)
	}

	var resp inspectResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, body)
	}
	// PR9R13 reclassified this fixture, deliberately. It is a bare 22-byte
	// EOCD declaring a 2.9 MiB directory -- a directory larger than the
	// whole upload, which no legal archive has and no ceiling increase can
	// make exist. "We refused to spend the work" was the wrong reason for
	// it; "this archive is impossible" is the right one, and a client
	// reading the first would go looking for a limit to raise.
	//
	// What this test actually guards is unchanged and still asserted below:
	// the wrapped entry count does not get the upload past the bound, the
	// response is 200 rather than a 500 or a rejection, nothing is reported
	// clean, and -- the part that must never regress -- the coverage is NOT
	// complete, because this archive's contents were never parsed.
	if resp.Format != inspectZipMalformedFormat {
		t.Errorf("Format = %q, want %q -- entries=1 (wrapped) must NOT let this upload past the structural bound", resp.Format, inspectZipMalformedFormat)
	}
	if resp.Coverage.Complete {
		t.Errorf("Coverage.Complete = true, want false -- a refusal to parse must never publish as a full inspection, whatever reason the checks carry")
	}
	if resp.Coverage.Reason != inspect.ReasonWorkCeilingBytes {
		t.Errorf("Coverage.Reason = %q, want %q -- the CHECKS say malformed_input (a fact about the archive) while the COVERAGE says we declined to read it; the two are separate claims", resp.Coverage.Reason, inspect.ReasonWorkCeilingBytes)
	}
	if err := inspect.ValidateReport(resp.Findings, resp.Checks); err != nil {
		t.Errorf("ValidateReport: %v", err)
	}
}

// TestInspectZipDirectoryWithinBoundUnaffected proves normal-sized ZIP
// office uploads are completely unaffected by the new structural ceiling:
// the SAME docx-shaped fixture, with an entry count comfortably under
// inspectMaxZipDirEntries, resolves to format=docx exactly as before.
func TestInspectZipDirectoryWithinBoundUnaffected(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	status, body := doInspect(t, srv, "in.docx", docxFixture(t))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	var resp inspectResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, body)
	}
	if resp.Format != "docx" {
		t.Errorf("Format = %q, want docx -- a normal-sized office upload must not be affected by the new structural ceiling", resp.Format)
	}
}

// TestZipDirectoryExceedsStructuralBoundNeverParsesOversizedArchive proves
// zipDirectoryExceedsStructuralBound itself trips on a real archive whose
// entry count crosses the ceiling (exceeds=true) and stays untripped for a
// real small archive (exceeds=false) -- exercised directly, independent of
// the HTTP handler, against real archive/zip.Writer output. Since round 2
// of PR #9 review this passes not because the entry COUNT was compared
// against a ceiling, but because that many real (non-minimal, named)
// central-directory records inevitably declare a dirBytes value past
// inspectMaxZipDirBytes -- see TestZipDirectoryExceedsStructuralBoundIgnoresWrappedEntryCount
// below for the direct proof that entries itself is never consulted.
func TestZipDirectoryExceedsStructuralBoundNeverParsesOversizedArchive(t *testing.T) {
	small := docxFixture(t)
	r := bytes.NewReader(small)
	if outcome, _, _ := zipDirectoryExceedsStructuralBound(r, int64(len(small))); outcome.exceeds() {
		t.Errorf("small docx fixture: exceeds = true, want false")
	}

	junkEntries := inspectMaxZipDirEntries + 1
	wantEntries := junkEntries + 1 // +1 for the fixture's own word/document.xml root part
	big := manyEntriesDocxFixture(t, junkEntries)
	r2 := bytes.NewReader(big)
	outcome, entries, dirBytes := zipDirectoryExceedsStructuralBound(r2, int64(len(big)))
	if !outcome.exceeds() {
		t.Fatalf("exceeds = false, want true for %d entries (bound is %d)", wantEntries, inspectMaxZipDirEntries)
	}
	if int(entries) != wantEntries {
		t.Errorf("entries = %d, want %d", entries, wantEntries)
	}
	if dirBytes <= inspectMaxZipDirBytes {
		t.Errorf("dirBytes = %d, want > %d -- this test's own exceeds=true only proves the fix works if the byte bound, not the entry bound, is what actually tripped", dirBytes, inspectMaxZipDirBytes)
	}
}

// TestInspectZipDirectoryWithinBoundToleratesRealisticOfficeArchive proves a
// legitimate multi-part archive -- several dozen parts, the shape a real
// docx/xlsx/pptx/odt/ods/odp/epub upload actually has, not the two-entry toy
// fixture TestInspectZipDirectoryWithinBoundUnaffected uses -- still resolves
// to its real format under the new byte-derived bound (inspectMaxZipDirBytes,
// ~184 KiB). This is the fixture-backed proof the plan requires, not
// reasoning about the arithmetic: a real central directory this size is
// nowhere near the ~184 KiB ceiling.
func TestInspectZipDirectoryWithinBoundToleratesRealisticOfficeArchive(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	fixture := manyEntriesDocxFixture(t, 50) // 51 parts total with the root
	status, body := doInspect(t, srv, "in.docx", fixture)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	var resp inspectResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, body)
	}
	if resp.Format != "docx" {
		t.Errorf("Format = %q, want docx -- a realistic multi-part office archive must not be rejected by the byte-derived structural ceiling", resp.Format)
	}
}

// TestZipDirectoryExceedsStructuralBoundIgnoresWrappedEntryCount reproduces
// the EXACT scenario codex's round-2 review gave, arithmetic included: an
// archive with 65,537 minimal 46-byte central-directory records declares
// entries=1 (65537 mod 65536, the uint16 wraparound) while its true
// directory size is 65537*46 = 3,014,702 bytes (~2.9 MiB) -- a value round
// 1's flat 4 MiB ceiling would have let straight through. Constructing only
// the 22-byte EOCD record itself (no actual 3 MiB of central-directory
// bytes behind it) is sufficient: zipDirectoryExceedsStructuralBound never
// reads the directory itself, only the EOCD's own declared fields, so this
// is a direct, minimal proof that dirBytes alone -- not entries -- drives
// the decision.
//
// D-16 inversion, performed by hand while writing this fix and reverted
// before this commit (see this quick task's SUMMARY.md for the literal
// before/after `go test` output): temporarily changing the exceeds
// assignment inside zipDirectoryExceedsStructuralBound back to
// round-1's `entries > inspectMaxZipDirEntries || dirBytes > inspectMaxZipDirBytes`
// makes this test fail (entries=1 is not > inspectMaxZipDirEntries=4096,
// and dirBytes=3,014,702 is not > round-1's 4 MiB, so exceeds stays false)
// -- reproducing the exact bypass PR9R2-F5-EOCD-UINT16-WRAP reported.
func TestZipDirectoryExceedsStructuralBoundIgnoresWrappedEntryCount(t *testing.T) {
	const trueRecordCount = 65537
	wrappedEntries := uint16(trueRecordCount % 65536)                   // == 1
	trueDirBytes := uint32(trueRecordCount * zipCentralDirMinRecordLen) // 3,014,702

	// A minimal, self-consistent 22-byte EOCD record (no comment, no real
	// central-directory bytes behind it -- this function never reads them).
	eocd := make([]byte, 22)
	eocd[0], eocd[1], eocd[2], eocd[3] = 'P', 'K', 0x05, 0x06
	binary.LittleEndian.PutUint16(eocd[10:12], wrappedEntries)
	binary.LittleEndian.PutUint32(eocd[12:16], trueDirBytes)
	// comment length (offset 20:22) left at zero.

	outcome, gotEntries, gotDirBytes := zipDirectoryExceedsStructuralBound(bytes.NewReader(eocd), int64(len(eocd)))
	if gotEntries != wrappedEntries {
		t.Fatalf("entries = %d, want %d (the wrapped value the reviewer's scenario declares)", gotEntries, wrappedEntries)
	}
	if gotDirBytes != trueDirBytes {
		t.Fatalf("dirBytes = %d, want %d", gotDirBytes, trueDirBytes)
	}
	if !outcome.exceeds() {
		t.Fatalf("exceeds = false, want true -- entries=%d (wrapped, looks tiny) must NOT be able to wave a %d-byte declared directory (~2.9 MiB, %d real minimal records) through the structural bound", gotEntries, gotDirBytes, trueRecordCount)
	}
}

// TestZipDirectoryExceedsStructuralBoundTreatsZip64SentinelAsExceeding
// proves dirBytes == 0xFFFFFFFF -- the ZIP64 "the real size is in the
// ZIP64 end-of-central-directory record, not this field" sentinel -- is
// treated as exceeding the bound. entries is deliberately left at the
// wrapped value 1 from the round-2 scenario above, so this test also proves
// the sentinel check is not merely riding along on a coincidentally large
// entries value.
func TestZipDirectoryExceedsStructuralBoundTreatsZip64SentinelAsExceeding(t *testing.T) {
	eocd := make([]byte, 22)
	eocd[0], eocd[1], eocd[2], eocd[3] = 'P', 'K', 0x05, 0x06
	eocd[10], eocd[11] = 1, 0 // entries = 1
	eocd[12], eocd[13], eocd[14], eocd[15] = 0xFF, 0xFF, 0xFF, 0xFF

	outcome, _, dirBytes := zipDirectoryExceedsStructuralBound(bytes.NewReader(eocd), int64(len(eocd)))
	if dirBytes != 0xFFFFFFFF {
		t.Fatalf("dirBytes = %#x, want 0xffffffff", dirBytes)
	}
	if !outcome.exceeds() {
		t.Fatalf("exceeds = false, want true -- the ZIP64 sentinel must never be read as a small-enough declared size")
	}
}

// realZipCentralDirectoryFixture builds a raw byte stream shaped like a real
// ZIP upload: a PK\x03\x04 local-file-header magic (gates
// detectInspectFormat's ZIP branch for the HTTP-level tests below; harmless
// for direct zipDirectoryExceedsStructuralBound calls), n REAL, valid
// central-directory records (PK\x01\x02, each carrying a nameLen-byte name
// whose CONTENT is immaterial -- only the declared length affects the
// walk's own record-length arithmetic), and a trailing EOCD whose
// directoryOffset points at the real directory and whose entries/dirBytes
// fields are whatever the caller declares -- letting tests build both
// honest and lying EOCDs from one builder.
func realZipCentralDirectoryFixture(n, nameLen int, declaredEntries uint16, declaredDirBytes uint32) []byte {
	recordLen := zipCentralDirMinRecordLen + nameLen
	dir := make([]byte, n*recordLen)
	for i := 0; i < n; i++ {
		rec := dir[i*recordLen : (i+1)*recordLen]
		rec[0], rec[1], rec[2], rec[3] = 'P', 'K', 0x01, 0x02
		binary.LittleEndian.PutUint16(rec[28:30], uint16(nameLen))
	}
	buf := make([]byte, 4+len(dir)+22)
	buf[0], buf[1], buf[2], buf[3] = 'P', 'K', 3, 4
	copy(buf[4:], dir)
	eocd := buf[4+len(dir):]
	eocd[0], eocd[1], eocd[2], eocd[3] = 'P', 'K', 0x05, 0x06
	binary.LittleEndian.PutUint16(eocd[10:12], declaredEntries)
	binary.LittleEndian.PutUint32(eocd[12:16], declaredDirBytes)
	binary.LittleEndian.PutUint32(eocd[16:20], 4) // directoryOffset points at the real directory, right after the 4-byte local-header magic
	// comment length (last 2 bytes) left at zero.
	return buf
}

// round4RealDirectoryZipFixture reproduces the FOURTH round of review
// verbatim: "an archive with 65,537 valid 46-byte central-directory
// headers, entries=1, directorySize=46, and directoryOffset pointing to the
// real directory". Unlike round 2's own fixture (a bare 22-byte EOCD
// claiming a directory that was never actually there), this one contains
// 65,537 REAL central-directory records at the offset its EOCD honestly
// points to -- archive/zip.NewReader really would walk all of them -- but
// LIES about the directory's declared byte size (46, not the true
// 3,014,702), exactly the value round 2's dirBytes-only comparison would
// have waved through unchanged.
func round4RealDirectoryZipFixture() []byte {
	const trueRecordCount = 65537
	wrappedEntries := uint16(trueRecordCount % 65536) // == 1
	return realZipCentralDirectoryFixture(trueRecordCount, 0, wrappedEntries, zipCentralDirMinRecordLen)
}

// TestZipDirectoryExceedsStructuralBoundCountsRealRecordsBehindALyingDeclaredSize
// is the central round-4 proof: zipDirectoryExceedsStructuralBound must
// reject round4RealDirectoryZipFixture even though NEITHER declared EOCD
// field (entries=1, dirBytes=46) is itself large enough to trip a
// comparison against any ceiling -- the previous two fixes both only ever
// compared a declared field against a ceiling, so both would pass this
// fixture. Only counting the REAL records behind the lying declaration
// catches it.
func TestZipDirectoryExceedsStructuralBoundCountsRealRecordsBehindALyingDeclaredSize(t *testing.T) {
	data := round4RealDirectoryZipFixture()
	outcome, gotEntries, gotDirBytes := zipDirectoryExceedsStructuralBound(bytes.NewReader(data), int64(len(data)))
	if gotEntries != 1 {
		t.Fatalf("entries = %d, want 1 (65537 mod 65536, read verbatim from the EOCD's own field)", gotEntries)
	}
	if gotDirBytes != zipCentralDirMinRecordLen {
		t.Fatalf("dirBytes = %d, want %d (the lying declared size)", gotDirBytes, zipCentralDirMinRecordLen)
	}
	if !outcome.exceeds() {
		t.Fatalf("exceeds = false, want true -- 65,537 REAL central-directory records sit behind a 46-byte lying declared size; a bound that only reads declared fields (round 1's entries, round 2's dirBytes) cannot catch this")
	}
}

// TestInspectRound4ScenarioReportsUnknownNot500 is
// TestZipDirectoryExceedsStructuralBoundCountsRealRecordsBehindALyingDeclaredSize's
// end-to-end sibling, exercised through the real HTTP handler exactly like
// TestInspectReviewerScenarioReportsUnknownNot500 proves for round 2's own
// scenario: the upload must be reported as format=zip_directory_too_large,
// never resolved as docx, never clean, never 500.
func TestInspectRound4ScenarioReportsUnknownNot500(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	status, body := doInspect(t, srv, "in.zip", round4RealDirectoryZipFixture())
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (never 500, never a rejection); body=%s", status, body)
	}

	var resp inspectResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, body)
	}
	if resp.Format != inspectZipTooLargeFormat {
		t.Errorf("Format = %q, want %q -- a 46-byte lying declared directory size must not let 65,537 real records past the structural bound", resp.Format, inspectZipTooLargeFormat)
	}
	if resp.Coverage.Complete {
		t.Errorf("Coverage.Complete = true, want false")
	}
	if resp.Coverage.Reason != inspect.ReasonWorkCeilingBytes {
		t.Errorf("Coverage.Reason = %q, want %q", resp.Coverage.Reason, inspect.ReasonWorkCeilingBytes)
	}
	if err := inspect.ValidateReport(resp.Findings, resp.Checks); err != nil {
		t.Errorf("ValidateReport: %v", err)
	}
}

// TestZipDirectoryExceedsStructuralBoundWindowExhaustionIsExceeding proves
// the SECOND shape round 4 requires: a directory whose entry count would
// stay safely under inspectMaxZipDirEntries, but whose real records are
// large enough that reading inspectMaxZipDirBytes of them does not reach
// the directory's own (structurally located, not declared) end. The plan's
// own words: "the window ran out, and the directory did not end -- an
// exceeded bound, not a pass: you do not know what follows, and guessing is
// what got the previous two versions bypassed." Every EOCD field in this
// fixture is completely HONEST (matches the real bytes on disk exactly) --
// proving this is the fixed-size window itself doing the rejecting, not
// merely a lying-declared-field detector (that is what the round-4 test
// above already proves).
func TestZipDirectoryExceedsStructuralBoundWindowExhaustionIsExceeding(t *testing.T) {
	const n, nameLen = 100, 2000 // 100 * (46+2000) = 204,600 bytes > inspectMaxZipDirBytes (188,416); 100 records is nowhere near the 4096 entry ceiling
	recordLen := uint32(zipCentralDirMinRecordLen + nameLen)
	data := realZipCentralDirectoryFixture(n, nameLen, uint16(n), uint32(n)*recordLen)

	outcome, gotEntries, gotDirBytes := zipDirectoryExceedsStructuralBound(bytes.NewReader(data), int64(len(data)))
	if gotEntries != n || gotDirBytes != uint32(n)*recordLen {
		t.Fatalf("entries/dirBytes = %d/%d, want %d/%d -- this test's own EOCD is completely honest", gotEntries, gotDirBytes, n, uint32(n)*recordLen)
	}
	if !outcome.exceeds() {
		t.Fatalf("exceeds = false, want true -- the %d-byte window ran out before reaching the real, located end of a %d-record, honestly-declared directory (entries=%d is nowhere near the %d ceiling); not knowing what lies past the window must never pass", inspectMaxZipDirBytes, n, n, inspectMaxZipDirEntries)
	}
}

// virtualHostileZipReader synthesizes an arbitrarily large ZIP-shaped byte
// stream ON DEMAND -- it never materializes more than the single slice a
// caller's ReadAt asks for, however large `size` claims to be. Every byte
// from directoryOffset onward (up to a real EOCD at the true tail) is a
// valid, minimal (zipCentralDirMinRecordLen-byte) central-directory record;
// the EOCD itself lies about the directory's byte size (46) exactly like
// round4RealDirectoryZipFixture. Used to prove
// zipDirectoryExceedsStructuralBound's own total read volume, for a single
// call, is bounded by a FIXED CONSTANT -- never by size or anything the
// "archive" declares -- without the test itself allocating anywhere near
// `size` bytes to prove it.
type virtualHostileZipReader struct {
	size             int64
	directoryOffset  int64
	maxSingleReadLen int64 // high-water mark across every ReadAt call; read by the test after the call under test returns
}

func (v *virtualHostileZipReader) ReadAt(p []byte, off int64) (int, error) {
	if int64(len(p)) > v.maxSingleReadLen {
		v.maxSingleReadLen = int64(len(p))
	}
	eocdOffset := v.size - zipEOCDFixedLen
	for i := range p {
		abs := off + int64(i)
		switch {
		case abs >= eocdOffset && abs < v.size:
			switch abs - eocdOffset {
			case 0:
				p[i] = 'P'
			case 1:
				p[i] = 'K'
			case 2:
				p[i] = 0x05
			case 3:
				p[i] = 0x06
			case 12:
				p[i] = zipCentralDirMinRecordLen // dirBytes lies: declares 46, not the true (enormous) span
			case 16:
				p[i] = byte(v.directoryOffset)
			case 17:
				p[i] = byte(v.directoryOffset >> 8)
			default:
				p[i] = 0
			}
		case abs >= v.directoryOffset && abs < eocdOffset:
			switch (abs - v.directoryOffset) % zipCentralDirMinRecordLen {
			case 0:
				p[i] = 'P'
			case 1:
				p[i] = 'K'
			case 2:
				p[i] = 0x01
			case 3:
				p[i] = 0x02
			default:
				p[i] = 0
			}
		default:
			p[i] = 0
		}
	}
	return len(p), nil
}

// TestZipDirectoryExceedsStructuralBoundReadVolumeBoundedByConstant is the
// plan's own explicit acceptance criterion: "Объём чтения при любом входе
// ограничен константой, не зависящей от содержимого архива". size here is
// 1 TiB and the (virtual, never-materialized) real directory is comparably
// enormous -- if this function's own read volume depended on anything the
// archive declares or actually contains, this call would attempt to read
// gigabytes. It must not: every ReadAt this function issues is capped by
// either zipEOCDRecordMaxLen (the tail scan) or inspectMaxZipDirBytes (the
// record-counting window), both fixed package constants.
func TestZipDirectoryExceedsStructuralBoundReadVolumeBoundedByConstant(t *testing.T) {
	v := &virtualHostileZipReader{size: 1 << 40, directoryOffset: 4} // 1 TiB, never actually allocated
	outcome, _, _ := zipDirectoryExceedsStructuralBound(v, v.size)
	if !outcome.exceeds() {
		t.Fatalf("exceeds = false, want true -- a directory this large can never be verified within a bounded window")
	}
	wantMax := int64(inspectMaxZipDirBytes) // the larger of this function's two own per-call ceilings (zipEOCDRecordMaxLen < inspectMaxZipDirBytes)
	if v.maxSingleReadLen == 0 {
		t.Fatalf("ReadAt was never called -- test fixture broken")
	}
	if v.maxSingleReadLen > wantMax {
		t.Fatalf("largest single ReadAt request = %d bytes, want <= %d -- read volume must be bounded by a fixed constant, not by size (%d) or anything the archive declares", v.maxSingleReadLen, wantMax, v.size)
	}
}

// zipEOCDFixture is round 5's own generic corpus builder. Unlike
// realZipCentralDirectoryFixture above (which always places the real
// directory immediately after a 4-byte local-header magic and always
// declares directoryOffset==4), this lets a test vary every axis round 5's
// own divergence table needs independently: WHERE the real directory
// physically sits (realDirStart), WHAT the EOCD declares about it
// (declaredEntries/declaredDirBytes/declaredDirOffset -- three
// INDEPENDENTLY liable fields, per this function's own doc comment), and
// how the EOCD's own comment is terminated (commentLen, trailingGarbage).
type zipEOCDFixture struct {
	realDirStart      int64 // absolute offset where the real directory records begin
	realRecordCount   int   // how many REAL PK\x01\x02 records physically exist at realDirStart
	nameLen           int   // per-record declared name length (content immaterial)
	declaredEntries   uint16
	declaredDirBytes  uint32
	declaredDirOffset uint32
	commentLen        uint16 // EOCD's own declared comment length
	trailingGarbage   int    // extra bytes appended AFTER the declared comment end
}

func (f zipEOCDFixture) build() []byte {
	recordLen := zipCentralDirMinRecordLen + f.nameLen
	dirLen := int64(f.realRecordCount) * int64(recordLen)
	eocdOff := f.realDirStart + dirLen
	total := eocdOff + zipEOCDFixedLen + int64(f.commentLen) + int64(f.trailingGarbage)
	buf := make([]byte, total)
	buf[0], buf[1], buf[2], buf[3] = 'P', 'K', 3, 4
	for i := 0; i < f.realRecordCount; i++ {
		off := f.realDirStart + int64(i)*int64(recordLen)
		rec := buf[off : off+int64(recordLen)]
		rec[0], rec[1], rec[2], rec[3] = 'P', 'K', 0x01, 0x02
		binary.LittleEndian.PutUint16(rec[28:30], uint16(f.nameLen))
	}
	eocd := buf[eocdOff : eocdOff+zipEOCDFixedLen+int64(f.commentLen)]
	eocd[0], eocd[1], eocd[2], eocd[3] = 'P', 'K', 0x05, 0x06
	binary.LittleEndian.PutUint16(eocd[10:12], f.declaredEntries)
	binary.LittleEndian.PutUint32(eocd[12:16], f.declaredDirBytes)
	binary.LittleEndian.PutUint32(eocd[16:20], f.declaredDirOffset)
	binary.LittleEndian.PutUint16(eocd[20:22], f.commentLen)
	return buf
}

// round5ReviewerScenarioZipFixture reproduces the FIFTH round of review
// verbatim (inspect_handlers.go:530, quoted in full in this quick task's
// PLAN.md): an HONEST full dirBytes (matching the real 65,537-record
// directory's true size), a wrapped entries=1, and a directoryOffset field
// pointing only at the directory's OWN final 100 records -- not its real
// start. Round 4's own walk starts exactly at directoryOffset, finds only
// ~100 real records, reaches eocdOffset, and returns exceeds=false.
// archive/zip does not: it computes
// baseOffset = eocdOffset - dirBytes - directoryOffset -- sharply negative,
// since dirBytes describes the FULL span but directoryOffset points deep
// into it -- and reads from baseOffset+directoryOffset instead: the TRUE
// start of the real directory, walking all 65,537 records.
func round5ReviewerScenarioZipFixture() []byte {
	const trueRecordCount = 65537
	const suffixRecordCount = 100
	wrappedEntries := uint16(trueRecordCount % 65536) // == 1
	trueDirBytes := uint32(trueRecordCount * zipCentralDirMinRecordLen)
	f := zipEOCDFixture{
		realDirStart:      4,
		realRecordCount:   trueRecordCount,
		declaredEntries:   wrappedEntries,
		declaredDirBytes:  trueDirBytes, // HONEST: matches the real, full span
		declaredDirOffset: uint32(4 + (trueRecordCount-suffixRecordCount)*zipCentralDirMinRecordLen),
	}
	return f.build()
}

// TestZipDirectoryExceedsStructuralBoundRound5ScenarioCatchesNegativeBaseOffset
// is the central round-5 proof, reproducing the reviewer's own scenario
// literally (see round5ReviewerScenarioZipFixture's own doc comment). It
// first confirms, against REAL archive/zip.NewReader, that this fixture
// really does exhibit the reported behavior (archive/zip parses all 65,537
// records via its own negative-baseOffset correction) -- so a future change
// that made this fixture stop reproducing the scenario would fail loudly
// here, rather than this test silently passing for the wrong reason.
func TestZipDirectoryExceedsStructuralBoundRound5ScenarioCatchesNegativeBaseOffset(t *testing.T) {
	data := round5ReviewerScenarioZipFixture()

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("archive/zip.NewReader: %v -- this fixture must reproduce a real archive/zip.NewReader success (the round-5 scenario is that archive/zip DOES parse this successfully via its own negative baseOffset)", err)
	}
	if len(zr.File) <= inspectMaxZipDirEntries {
		t.Fatalf("archive/zip.NewReader parsed %d records, want > %d -- this fixture must reproduce archive/zip walking the FULL 65,537-record directory via its own baseOffset correction, not the short suffix directoryOffset points at directly", len(zr.File), inspectMaxZipDirEntries)
	}

	outcome, gotEntries, gotDirBytes := zipDirectoryExceedsStructuralBound(bytes.NewReader(data), int64(len(data)))
	if gotEntries != 1 {
		t.Fatalf("entries = %d, want 1 (65537 mod 65536, read verbatim from the EOCD's own field)", gotEntries)
	}
	if gotDirBytes != uint32(65537*zipCentralDirMinRecordLen) {
		t.Fatalf("dirBytes = %d, want %d (the HONEST full declared size)", gotDirBytes, uint32(65537*zipCentralDirMinRecordLen))
	}
	if !outcome.exceeds() {
		t.Fatalf("exceeds = false, want true -- an honest full dirBytes with a directoryOffset field pointing only at the directory's own final 100 records must not let archive/zip's own negative-baseOffset walk of all 65,537 real records past this bound (round 5's own reviewer scenario)")
	}
}

// TestInspectRound5ScenarioReportsUnknownNot500 is
// TestZipDirectoryExceedsStructuralBoundRound5ScenarioCatchesNegativeBaseOffset's
// end-to-end sibling, exercised through the real HTTP handler exactly like
// TestInspectRound4ScenarioReportsUnknownNot500 proves for round 4's own
// scenario: the upload must be reported as format=zip_directory_too_large,
// never resolved as docx, never clean, never 500.
func TestInspectRound5ScenarioReportsUnknownNot500(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	status, body := doInspect(t, srv, "in.zip", round5ReviewerScenarioZipFixture())
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (never 500, never a rejection); body=%s", status, body)
	}

	var resp inspectResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, body)
	}
	if resp.Format != inspectZipTooLargeFormat {
		t.Errorf("Format = %q, want %q -- an honest dirBytes with a directoryOffset pointing only at the final 100 records must not let archive/zip's own negative-baseOffset walk of all 65,537 records past the structural bound", resp.Format, inspectZipTooLargeFormat)
	}
	if resp.Coverage.Complete {
		t.Errorf("Coverage.Complete = true, want false")
	}
	if resp.Coverage.Reason != inspect.ReasonWorkCeilingBytes {
		t.Errorf("Coverage.Reason = %q, want %q", resp.Coverage.Reason, inspect.ReasonWorkCeilingBytes)
	}
	if err := inspect.ValidateReport(resp.Findings, resp.Checks); err != nil {
		t.Errorf("ValidateReport: %v", err)
	}
}

// TestZipDirectoryExceedsStructuralBoundTruncatedCommentDoesNotBypassOverCeilingDirectory
// is divergence row #2 (and #3) of zipDirectoryExceedsStructuralBound's own
// doc comment: an EOCD whose declared comment (here, zero bytes) ends
// BEFORE the file's own end -- archive/zip's own findSignatureInBlock
// tolerates this explicitly (n+22+i<=len(b), not ==) -- must not let an
// over-ceiling real directory through merely because rounds 1-4's own
// exact-match-to-file-end rule would have scanned straight past this EOCD
// looking (in vain) for a different one.
func TestZipDirectoryExceedsStructuralBoundTruncatedCommentDoesNotBypassOverCeilingDirectory(t *testing.T) {
	const over = inspectMaxZipDirEntries + 1
	data := zipEOCDFixture{
		realDirStart:      4,
		realRecordCount:   over,
		declaredEntries:   uint16(over % 65536),
		declaredDirBytes:  uint32(over) * zipCentralDirMinRecordLen,
		declaredDirOffset: 4,
		commentLen:        0,
		trailingGarbage:   1000, // the (zero-byte) comment ends 1000 bytes before the file's own end
	}.build()

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("archive/zip.NewReader: %v -- this fixture must be one archive/zip itself accepts (a short/truncated comment is explicitly tolerated)", err)
	}
	if len(zr.File) <= inspectMaxZipDirEntries {
		t.Fatalf("archive/zip.NewReader parsed %d records, want > %d", len(zr.File), inspectMaxZipDirEntries)
	}

	outcome, _, _ := zipDirectoryExceedsStructuralBound(bytes.NewReader(data), int64(len(data)))
	if !outcome.exceeds() {
		t.Fatalf("exceeds = false, want true -- an EOCD whose comment ends before the file's own end must not let an over-ceiling real directory through just because an exact-match-to-file-end locator would have scanned past it looking for another signature")
	}
}

// TestZipDirectoryExceedsStructuralBoundDirBytesFFFFTreatedAsZip64Sentinel is
// divergence row #4: archive/zip's own ZIP64 "look elsewhere" trigger
// (reader.go:608) compares directorySize against 0xffff (65,535), NOT
// 0xffffffff -- a value small enough to have sailed straight through every
// prior round's own dirBytes-vs-ceiling comparison as an "ordinary", if odd,
// declared size.
func TestZipDirectoryExceedsStructuralBoundDirBytesFFFFTreatedAsZip64Sentinel(t *testing.T) {
	eocd := make([]byte, 22)
	eocd[0], eocd[1], eocd[2], eocd[3] = 'P', 'K', 0x05, 0x06
	eocd[10], eocd[11] = 1, 0 // entries = 1
	binary.LittleEndian.PutUint32(eocd[12:16], 0xFFFF)

	outcome, _, dirBytes := zipDirectoryExceedsStructuralBound(bytes.NewReader(eocd), int64(len(eocd)))
	if dirBytes != 0xFFFF {
		t.Fatalf("dirBytes = %#x, want 0xffff", dirBytes)
	}
	if !outcome.exceeds() {
		t.Fatalf("exceeds = false, want true -- dirBytes == 0xFFFF is archive/zip's own ZIP64 'look elsewhere' trigger value (directorySize==0xffff, reader.go:608), and must not be read as an ordinary small declared size")
	}
}

// zipDifferentialCase is one corpus entry for
// TestZipDirectoryExceedsStructuralBoundDifferentialAgainstArchiveZip.
// describe is written verbatim into any failure message so a future
// round's own audit can see exactly which input, if any, exposed a new
// divergence -- the plan's own explicit requirement for this test.
type zipDifferentialCase struct {
	name     string
	data     []byte
	describe string
}

// zipDirectoryDifferentialCorpus builds the corpus
// TestZipDirectoryExceedsStructuralBoundDifferentialAgainstArchiveZip runs
// the forbidden-combination property over: reproductions of rounds 1, 2, 4,
// and 5's own bypass scenarios, an EOCD comment ending before the file's
// own end, two EOCD signatures in one file, dirBytes==0xFFFF, a legitimate
// (JAR-style) positive baseOffset, and legitimate small/realistic office
// archives -- built by a generator (zipEOCDFixture/manyEntriesDocxFixture/
// docxFixture), not binary fixtures, per the plan's own requirement.
//
// Takes testing.TB (see mustWriteZipEntry's own doc comment,
// handlers_test.go), not the concrete *testing.T, so
// FuzzZipDirectoryGateNeverUnderestimates can call this directly with its
// own *testing.F to seed the fuzz corpus with every entry here -- the known
// scenarios stay covered by the fuzzer's own corpus, not only by
// TestZipDirectoryExceedsStructuralBoundDifferentialAgainstArchiveZip below.
func zipDirectoryDifferentialCorpus(t testing.TB) []zipDifferentialCase {
	t.Helper()
	const over = inspectMaxZipDirEntries + 1

	commentEndsBeforeEOF := zipEOCDFixture{
		realDirStart:      4,
		realRecordCount:   over,
		declaredEntries:   uint16(over % 65536),
		declaredDirBytes:  uint32(over) * zipCentralDirMinRecordLen,
		declaredDirOffset: 4,
		trailingGarbage:   1000,
	}.build()

	dualSignature := func() []byte {
		data := zipEOCDFixture{
			realDirStart:      4,
			realRecordCount:   over,
			declaredEntries:   uint16(over % 65536),
			declaredDirBytes:  uint32(over) * zipCentralDirMinRecordLen,
			declaredDirOffset: 4,
			commentLen:        40,
		}.build()
		// A second 'PK\x05\x06' byte sequence embedded inside the real
		// EOCD's own comment bytes -- closer to the file's own end than the
		// real record data, the classic "signature ambiguity" case
		// findSignatureInBlock's own hard-fail-on-first-candidate rule
		// exists to settle one way or the other, consistently with
		// archive/zip's own choice.
		commentStart := len(data) - 40
		data[commentStart+10], data[commentStart+11], data[commentStart+12], data[commentStart+13] = 'P', 'K', 5, 6
		return data
	}()

	dirBytesFFFF := zipEOCDFixture{
		realDirStart:      4,
		realRecordCount:   over,
		declaredEntries:   uint16(over % 65536),
		declaredDirBytes:  0xFFFF,
		declaredDirOffset: 4,
	}.build()

	positiveBaseOffsetJARStyle := func() []byte {
		const prefixLen = 1000
		data := zipEOCDFixture{
			realDirStart:      prefixLen + 4,
			realRecordCount:   over,
			declaredEntries:   uint16(over % 65536),
			declaredDirBytes:  uint32(over) * zipCentralDirMinRecordLen,
			declaredDirOffset: 4, // relative to the ZIP's own start, not the file's
		}.build()
		// zipEOCDFixture.build unconditionally writes the local-header magic
		// at byte 0 -- clear it and place a fake, non-zip script prefix (all
		// zero bytes here; content is immaterial) with the REAL zip magic
		// at prefixLen instead, modeling a JAR-style "bash script + zip"
		// concatenation.
		data[0], data[1], data[2], data[3] = 0, 0, 0, 0
		copy(data[prefixLen:prefixLen+4], []byte{'P', 'K', 3, 4})
		return data
	}()

	return []zipDifferentialCase{
		{"round1_real_overflowing_docx", manyEntriesDocxFixture(t, over), "a real, valid docx-shaped archive with inspectMaxZipDirEntries+1 real entries -- round 1's own repro shape"},
		{"round2_wrapped_entries_lying_dirbytes_no_real_directory", reviewerScenarioZipFixture(), "a bare 22-byte EOCD declaring entries=1 (wrapped) and dirBytes=65537*46, with no real directory bytes behind it at all -- round 2's own reviewer scenario"},
		{"round4_real_directory_behind_lying_dirbytes", round4RealDirectoryZipFixture(), "65,537 REAL central-directory records behind a lying dirBytes=46 -- round 4's own reviewer scenario"},
		{"round5_honest_dirbytes_directoryoffset_at_final_suffix", round5ReviewerScenarioZipFixture(), "an honest full dirBytes describing all 65,537 real records, with directoryOffset pointing only at the final 100 of them -- round 5's own reviewer scenario, the negative-baseOffset bypass"},
		{"eocd_comment_ends_before_file_end", commentEndsBeforeEOF, "an over-ceiling real directory whose EOCD declares a 0-byte comment that ends 1000 bytes before the file's own end"},
		{"dual_eocd_signature_in_real_comment", dualSignature, "an over-ceiling real directory whose EOCD's own 40-byte comment contains a second 'PK\\x05\\x06' byte sequence"},
		{"dirbytes_0xffff_zip64_trigger", dirBytesFFFF, "dirBytes == 0xFFFF (65,535), archive/zip's own ZIP64 trigger value, behind an over-ceiling real directory"},
		{"positive_baseoffset_jar_style_prefix", positiveBaseOffsetJARStyle, "a 1000-byte non-zip script prefix before an over-ceiling real directory, directoryOffset declared relative to the ZIP's own start -- a legitimate JAR-style positive baseOffset"},
		{"legit_small_docx", docxFixture(t), "a real, tiny, legitimate docx (2 entries) -- must never be a false positive"},
		{"legit_realistic_office_archive", manyEntriesDocxFixture(t, 50), "a realistic 51-part office archive, comfortably under the ceiling"},
	}
}

// TestZipDirectoryExceedsStructuralBoundDifferentialAgainstArchiveZip is the
// plan's own central artifact (see this quick task's PLAN.md,
// <the_recurring_one_read_this_first>): for every input in the corpus
// above, the forbidden combination "archive/zip.NewReader parsed more than
// inspectMaxZipDirEntries real records" AND "zipDirectoryExceedsStructuralBound
// said exceeds=false" must never occur. This is a PROPERTY, not a list of
// scenarios -- it does not require guessing the next field a sixth round of
// review might find; it is checked directly against the real
// archive/zip package this function exists to predict correctly.
func TestZipDirectoryExceedsStructuralBoundDifferentialAgainstArchiveZip(t *testing.T) {
	for _, tc := range zipDirectoryDifferentialCorpus(t) {
		t.Run(tc.name, func(t *testing.T) {
			data := tc.data

			realCount := -1
			if zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data))); err == nil || errors.Is(err, zip.ErrInsecurePath) {
				realCount = len(zr.File)
			}

			outcome, _, _ := zipDirectoryExceedsStructuralBound(bytes.NewReader(data), int64(len(data)))

			if realCount > inspectMaxZipDirEntries && !outcome.exceeds() {
				t.Fatalf("FORBIDDEN COMBINATION on corpus case %q: archive/zip.NewReader parsed %d real records (> ceiling %d) but zipDirectoryExceedsStructuralBound said exceeds=false.\nInput: %s", tc.name, realCount, inspectMaxZipDirEntries, tc.describe)
			}
		})
	}
}

// zipDirectoryGateFuzzMaxInputLen bounds every input
// FuzzZipDirectoryGateNeverUnderestimates actually exercises: large enough
// to hold the round-4/5 seed corpus (65,537 real 46-byte records, ~3 MiB)
// with headroom, small enough that a fuzzer-mutated length can never make
// the REFERENCE call below (archive/zip.NewReader) walk an unbounded
// number of records. This is deliberately NOT about
// zipDirectoryExceedsStructuralBound's own read volume -- that is already
// bounded by a fixed constant regardless of input size
// (TestZipDirectoryExceedsStructuralBoundReadVolumeBoundedByConstant proves
// this directly, with a 1 TiB virtual input) -- it is about archive/zip's
// own reader, which has no such ceiling and will happily parse however many
// real records a mutated input contains. Anything the fuzzer or corpus
// proposes above this is skipped (t.Skip), never allocated.
const zipDirectoryGateFuzzMaxInputLen = 8 << 20 // 8 MiB

// FuzzZipDirectoryGateNeverUnderestimates is
// TestZipDirectoryExceedsStructuralBoundDifferentialAgainstArchiveZip's own
// property, run against FUZZER-GENERATED inputs rather than only a
// human-enumerated corpus. A fixed table proves the property on cases
// someone thought of, and every one of this bound's four prior bypasses
// (see zipDirectoryExceedsStructuralBound's own doc comment) was an input
// nobody enumerated -- the exact gap a fixed corpus cannot close on its
// own.
//
// Seeded with every entry of zipDirectoryDifferentialCorpus (round 1/2/4/5
// reproductions, the truncated-comment/dual-signature/0xFFFF/positive-
// baseOffset cases, and the two legitimate office archives), via f.Add, so
// the known scenarios stay covered by the fuzzer's own corpus too, not only
// by the table test above. zipDirectoryDifferentialCorpus takes
// testing.TB, not *testing.T, specifically so this *testing.F can call it
// directly (see mustWriteZipEntry's own doc comment, handlers_test.go).
//
// The fuzz body asserts EXACTLY the one property the table test asserts,
// nothing more: legitimate-file assertions would make this flaky, since a
// fuzzer mutating raw bytes overwhelmingly produces garbage that is not,
// and was never meant to be, a valid archive of any kind -- the plan's own
// discipline against enumerating scenarios extends to not enumerating
// EXPECTED OUTCOMES either; only the one forbidden combination is checked.
func FuzzZipDirectoryGateNeverUnderestimates(f *testing.F) {
	for _, tc := range zipDirectoryDifferentialCorpus(f) {
		f.Add(tc.data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > zipDirectoryGateFuzzMaxInputLen {
			// Bound the cost of the reference call below, not this
			// function's own gate (already proven bounded independent of
			// input size) -- skip rather than allocate/parse an arbitrarily
			// large mutated input. None of the seed corpus entries are
			// anywhere near this cap (the largest, round 4/5's 65,537-record
			// fixtures, are ~3 MiB), so no seed is ever skipped.
			t.Skip("input exceeds zipDirectoryGateFuzzMaxInputLen")
		}

		realCount := -1
		if zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data))); err == nil || errors.Is(err, zip.ErrInsecurePath) {
			realCount = len(zr.File)
		}

		outcome, _, _ := zipDirectoryExceedsStructuralBound(bytes.NewReader(data), int64(len(data)))

		if realCount > inspectMaxZipDirEntries && !outcome.exceeds() {
			t.Fatalf("FORBIDDEN COMBINATION: archive/zip.NewReader parsed %d real records (> ceiling %d) but zipDirectoryExceedsStructuralBound said exceeds=false.\nInput (%d bytes, hex): %x", realCount, inspectMaxZipDirEntries, len(data), data)
		}
	})
}

// incompleteMultipartPrefix builds a real, well-formed multipart body up to
// (and including) the file field's own content, but deliberately never
// calls (*multipart.Writer).Close -- the closing boundary
// ("--<boundary>--\r\n") is never written. Returned alongside the
// boundary string a caller needs for the request's Content-Type header.
func incompleteMultipartPrefix(t *testing.T, filename string, data []byte) (prefix []byte, boundary string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	boundary = w.Boundary()
	fw, err := w.CreateFormFile(formFieldFile, filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return b.Bytes(), boundary
}

// blockingReader serves prefix immediately (real bytes, non-blocking), then
// blocks on block for every subsequent Read -- modeling a client that has
// sent SOME of its body and is now trickling the rest arbitrarily slowly.
// Read never returns once prefix is exhausted, until the test closes
// block.
type blockingReader struct {
	prefix []byte
	pos    int
	block  chan struct{}
}

func (r *blockingReader) Read(p []byte) (int, error) {
	if r.pos < len(r.prefix) {
		n := copy(p, r.prefix[r.pos:])
		r.pos += n
		return n, nil
	}
	<-r.block
	return 0, io.EOF
}

// TestInspectSlowBodyDoesNotHoldConcurrencySlot is T-SLF-02's central proof:
// more concurrent slow-body clients than InspectMaxConcurrent must NOT
// exhaust the concurrency gate, because none of them have finished reading
// their own request body yet -- the slot is acquired AFTER body intake, not
// before. Before this fix, the same scenario would have every slot held by
// a slow-body client doing no parsing at all, and the legitimate request
// below would receive 429 inspect_busy.
func TestInspectSlowBodyDoesNotHoldConcurrencySlot(t *testing.T) {
	resolver := newFakeResolver()
	srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
		InspectMaxConcurrent: 2,
	})

	const slowClients = 3 // exceeds InspectMaxConcurrent (2)
	var wg sync.WaitGroup
	blockers := make([]chan struct{}, slowClients)
	for i := range blockers {
		blockers[i] = make(chan struct{})
		prefix, boundary := incompleteMultipartPrefix(t, "in.png", pngBytesFixture())
		body := &blockingReader{prefix: prefix, block: blockers[i]}
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/inspect", body))
		req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.Routes().ServeHTTP(httptest.NewRecorder(), req)
		}()
	}
	t.Cleanup(func() {
		for _, ch := range blockers {
			close(ch)
		}
		wg.Wait()
	})

	// Give the slow clients a moment to reach (and block inside)
	// ParseMultipartForm -- long enough that, if the OLD code still
	// acquired the slot before reading the body, all three would already
	// hold (or be rejected for) a slot by now.
	time.Sleep(150 * time.Millisecond)

	status, body := doInspect(t, srv, "legit.png", pngBytesFixture())
	if status != http.StatusOK {
		t.Fatalf("legit request status = %d, want 200 -- must not be blocked by %d slow-body clients holding no slot; body=%s", status, slowClients, body)
	}
}

// inspectIntakeConcurrencyResult carries one slow-body request's own outcome
// back to the main goroutine through a channel -- rec.Code is read in the
// SAME goroutine that just wrote it (immediately after ServeHTTP returns),
// then handed off via the channel send/receive itself, which is what
// establishes the happens-before edge -race requires; reading rec.Code
// directly from the test goroutine after a bare time.Sleep, with no such
// edge, would be a genuine data race against the handler goroutine's own
// write inside http.ResponseWriter.WriteHeader.
type inspectIntakeConcurrencyResult struct {
	idx  int
	code int
}

// TestInspectIntakeConcurrencyIsBounded is PR9R8-F1-INTAKE-BOUND's central
// proof: MORE concurrent slow-body clients than inspectIntakeMaxConcurrent
// must NOT all be admitted into ParseMultipartForm -- exactly the excess
// beyond that constant must be rejected immediately with 429 inspect_busy,
// WITHOUT ever reading a byte of their own body (a saturated intake stage
// is not a queue). Before this fix, every one of them would have entered
// ParseMultipartForm unconditionally, each buffering up to
// multipartInMemoryBudget bytes in the process heap with nothing to stop
// arbitrarily many from doing so at once -- the exact scenario the finding
// names (roughly 30 concurrent requests times 32 MiB against the pod's own
// 1 GiB memory limit).
func TestInspectIntakeConcurrencyIsBounded(t *testing.T) {
	resolver := newFakeResolver()
	srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{})

	const overflow = 3 // exceeds inspectIntakeMaxConcurrent by this many
	total := inspectIntakeMaxConcurrent + overflow

	var wg sync.WaitGroup
	blockers := make([]chan struct{}, total)
	results := make(chan inspectIntakeConcurrencyResult, total)
	for i := 0; i < total; i++ {
		blockers[i] = make(chan struct{})
		prefix, boundary := incompleteMultipartPrefix(t, "in.png", pngBytesFixture())
		body := &blockingReader{prefix: prefix, block: blockers[i]}
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/inspect", body))
		req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
		wg.Add(1)
		go func(i int, req *http.Request) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, req)
			results <- inspectIntakeConcurrencyResult{idx: i, code: rec.Code}
		}(i, req)
	}
	t.Cleanup(func() {
		for _, ch := range blockers {
			close(ch)
		}
		wg.Wait()
	})

	// Only the `overflow` requests rejected for being over
	// inspectIntakeMaxConcurrent can possibly finish before their own
	// blockers are closed -- the accepted ones are, by construction, stuck
	// inside ParseMultipartForm reading blockingReader, which never returns
	// until t.Cleanup runs. A bounded wait, not a fixed sleep, drains
	// exactly the results that arrive.
	rejected := 0
	deadline := time.After(2 * time.Second)
drain:
	for range total {
		select {
		case res := <-results:
			if res.code != http.StatusTooManyRequests {
				t.Fatalf("request %d completed with status %d before its blocker was closed -- want either 429 (rejected at intake) or no completion at all yet", res.idx, res.code)
			}
			rejected++
		case <-time.After(300 * time.Millisecond):
			// No further result arrived within this window -- the
			// remaining requests are (as expected) still blocked inside
			// ParseMultipartForm, holding an intake slot each.
			break drain
		case <-deadline:
			t.Fatalf("timed out waiting for the %d expected intake rejections; got %d so far", overflow, rejected)
		}
	}
	if rejected != overflow {
		t.Fatalf("rejected = %d, want exactly %d -- concurrent buffering must be bounded to inspectIntakeMaxConcurrent (%d), not admitted unconditionally", rejected, overflow, inspectIntakeMaxConcurrent)
	}
}

// fakeContextAwarePreAdmitRepo mimics a property a REAL jobs.Repo.PreAdmit
// has (a DB call made with an ALREADY-EXPIRED context fails immediately,
// exactly like pgx would) that fakeRepo's own PreAdmit does not: fakeRepo
// ignores ctx entirely and always succeeds, which could never catch the
// T-SLF-03 regression this file's own test below proves fixed.
type fakeContextAwarePreAdmitRepo struct {
	*fakeRepo
}

func (f *fakeContextAwarePreAdmitRepo) PreAdmit(ctx context.Context, _ uuid.UUID, _ string, _ int64) error {
	return ctx.Err()
}

// delayedReader sleeps once, for delay, before its FIRST Read, then
// delegates every read (including that first one) to r -- modeling a
// legitimately slow-but-complete upload: real bytes, arriving late, never
// truncated.
type delayedReader struct {
	r     io.Reader
	delay time.Duration
	slept bool
}

func (d *delayedReader) Read(p []byte) (int, error) {
	if !d.slept {
		d.slept = true
		time.Sleep(d.delay)
	}
	return d.r.Read(p)
}

// TestInspectPreAdmitGetsFreshContextAfterSlowBody is T-SLF-03's central
// proof: a body whose intake takes LONGER than InspectMaxWorkDuration must
// still reach Repo.PreAdmit with a live (non-expired) context -- the
// work-ceiling context is created AFTER body intake, not at handler entry.
// Before this fix, the same scenario would create a 2s-shorter-than-the-
// actual-delay context BEFORE ParseMultipartForm, so by the time PreAdmit
// ran it would already be expired, and every quota verdict would report
// Allowed == nil ("unknown") regardless of the client's real quota state.
func TestInspectPreAdmitGetsFreshContextAfterSlowBody(t *testing.T) {
	resolver := newFakeResolver()
	repo := &fakeContextAwarePreAdmitRepo{fakeRepo: &fakeRepo{}}
	srv := NewServer(repo, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
		InspectMaxWorkDuration: 50 * time.Millisecond,
	})

	full, ct := multipartBody(t, "in.png", "", pngBytesFixture())
	slow := &delayedReader{r: bytes.NewReader(full.Bytes()), delay: 200 * time.Millisecond}
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/inspect", slow))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp inspectResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
	}
	if len(resp.Operations) == 0 {
		t.Fatalf("Operations empty -- test assumption broken: png must have at least one registered (operation, target)")
	}
	for _, op := range resp.Operations {
		if op.Quota.Allowed == nil {
			t.Errorf("operation %+v: Quota.Allowed = nil (unknown) after a slow-but-legitimate body read -- the work-ceiling context must not start before body intake", op)
		}
	}
}

// memMultipartFile adapts a byte slice to multipart.File so a test can call
// dispatchInspectFamilies directly, without going through an HTTP request.
type memMultipartFile struct{ *bytes.Reader }

func (memMultipartFile) Close() error { return nil }

// oversizedWorkbookXLSXFixture is a structurally valid xlsx whose
// xl/workbook.xml is larger than maxRelsPartBytes, so extractXLSXSheetCount
// refuses to read it and marks the Tracker's ceiling receptacle instead of
// reporting a sheet count.
func oversizedWorkbookXLSXFixture(t *testing.T) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteZipEntry(t, zw, "[Content_Types].xml", "<Types/>")
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "xl/workbook.xml", Method: zip.Deflate})
	if err != nil {
		t.Fatalf("CreateHeader: %v", err)
	}
	// 512 KiB of a repeating, highly compressible sheet element -- twice the
	// 256 KiB maxRelsPartBytes ceiling, so the part is refused whichever way
	// the ceiling is detected (declared size or read overrun).
	chunk := bytes.Repeat([]byte(`<sheet name="s"/>`), 1<<15)
	if _, err := w.Write(chunk); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// TestDispatchInspectFamiliesMarksTheTrackerItWasGiven pins the one thing the
// exhaustion receptacle cannot enforce about itself: that the production path
// actually hands the analyzers a live Tracker.
//
// The receptacle was introduced (quick task 260811-r3s) because exhaustion had
// been an ordinary return value that callers could silently drop, and three
// consecutive review rounds each found another dropped one. Recording it on a
// per-operation Tracker removed the need to thread anything -- but it replaced
// "every caller must remember to pass the flag up" with "the one entry point
// must remember to pass a non-nil Tracker down", and a *Tracker is a pointer,
// so nil is expressible and MarkCeilingHit on nil is a deliberate no-op.
//
// That residue was not hypothetical: replacing tr with nil at all five
// dispatch sites left the ENTIRE repository test suite green while every
// report silently claimed complete coverage. This test is what turns that
// substitution red. It asserts the property (the tracker this dispatch was
// given comes back marked) rather than the mechanism, so it keeps holding if
// the analyzers are re-plumbed again.
func TestDispatchInspectFamiliesMarksTheTrackerItWasGiven(t *testing.T) {
	data := oversizedWorkbookXLSXFixture(t)
	f := memMultipartFile{bytes.NewReader(data)}

	tr := inspect.NewTracker(inspect.Budget{
		MaxBytes:    int64(len(data)) * 4,
		MaxDuration: time.Minute,
	})
	if tr.Exhausted() {
		t.Fatal("precondition: a fresh Tracker with a generous budget must not start exhausted")
	}

	_ = dispatchInspectFamilies("xlsx", nil, nil, f, int64(len(data)), tr)

	if !tr.Exhausted() {
		t.Fatal("dispatchInspectFamilies returned without marking the Tracker it was given -- an analyzer hit its own internal ceiling (xl/workbook.xml over maxRelsPartBytes) and that fact did not reach the tracker, so BuildReport would report complete coverage over an analysis that stopped early. The usual cause is a dispatch site passing nil instead of tr.")
	}
}

// failAfterNthReadAt serves the first allow ReadAt calls from inner and
// fails every one after that with a real, non-EOF error. The EOCD tail scan
// is the first read this bound performs and the central-directory window is
// the next, so allow=1 puts the failure exactly where PR9R11 describes it:
// the EOCD is readable, the directory behind it is not.
type failAfterNthReadAt struct {
	inner *bytes.Reader
	allow int
	seen  int
}

func (f *failAfterNthReadAt) ReadAt(p []byte, off int64) (int, error) {
	f.seen++
	if f.seen > f.allow {
		return 0, errors.New("simulated disk read failure")
	}
	return f.inner.ReadAt(p, off)
}

// TestInspectZipCentralDirectoryReadFailureIsReadFailedNotTooLarge is
// PR9R11 (inspect_handlers.go:871). The structural bound refuses in both
// cases and should -- but "the directory is bigger than we will parse" and
// "we could not read the bytes" say opposite things about whose fault it
// is, and reporting the second as work_ceiling_bytes claims a ceiling was
// exceeded when nothing was ever measured.
func TestInspectZipCentralDirectoryReadFailureIsReadFailedNotTooLarge(t *testing.T) {
	data := docxFixture(t)
	r := &failAfterNthReadAt{inner: bytes.NewReader(data), allow: 1}

	outcome, _, _ := zipDirectoryExceedsStructuralBound(r, int64(len(data)))
	if !outcome.exceeds() {
		t.Fatal("outcome = zipBoundOK -- an unreadable central directory must still be refused; nothing about it was verified")
	}
	if outcome != zipBoundReadFailed {
		t.Fatalf("outcome = %v, want zipBoundReadFailed -- the refusal came from a non-EOF ReadAt error, not from any ceiling this archive exceeded", outcome)
	}
}

// TestInspectZipCorruptDirectorySignatureIsMalformedNotTooLarge is PR9R12
// (inspect_handlers.go:912): a tiny archive whose EOCD is readable and
// self-consistent but whose central directory does not start with a real
// record signature crosses NO configured ceiling. Reporting it as
// zip_directory_too_large / work_ceiling_bytes asserts a measurement that
// never happened, about a file whose actual problem is structural.
func TestInspectZipCorruptDirectorySignatureIsMalformedNotTooLarge(t *testing.T) {
	data := docxFixture(t)
	// Corrupt the FIRST central-directory record's signature in place. The
	// EOCD (and therefore every offset the bound reads) stays untouched, so
	// the bound reaches its record walk and finds bytes that are not a
	// record where one must be.
	idx := bytes.Index(data, []byte{'P', 'K', 0x01, 0x02})
	if idx < 0 {
		t.Fatal("fixture has no central-directory record to corrupt")
	}
	corrupt := append([]byte{}, data...)
	corrupt[idx+2], corrupt[idx+3] = 0x09, 0x09

	outcome, _, _ := zipDirectoryExceedsStructuralBound(bytes.NewReader(corrupt), int64(len(corrupt)))
	if !outcome.exceeds() {
		t.Fatal("outcome = zipBoundOK -- a directory that does not begin with a record signature was never verified and must be refused")
	}
	if outcome != zipBoundMalformed {
		t.Fatalf("outcome = %v, want zipBoundMalformed -- this archive is a few hundred bytes and crosses no ceiling this service configured; its problem is its own structure", outcome)
	}
}

// TestInspectZipRecordOvershootIsMalformedNotTooLarge is PR9R14
// (inspect_handlers.go:968): a central-directory record whose declared
// name/extra/comment lengths carry the walk PAST the located EOCD claims to
// extend beyond the directory containing it. Every byte proving that was
// already read, and no ceiling increase repairs it.
func TestInspectZipRecordOvershootIsMalformedNotTooLarge(t *testing.T) {
	data := docxFixture(t)
	idx := bytes.Index(data, []byte{'P', 'K', 0x01, 0x02})
	if idx < 0 {
		t.Fatal("fixture has no central-directory record to corrupt")
	}
	corrupt := append([]byte{}, data...)
	// Inflate the first record's declared extra-field length so the walk
	// steps clean over the EOCD. The signature stays valid, so this is a
	// different path from the corrupt-signature case.
	binary.LittleEndian.PutUint16(corrupt[idx+30:idx+32], 0xFFF0)

	outcome, _, _ := zipDirectoryExceedsStructuralBound(bytes.NewReader(corrupt), int64(len(corrupt)))
	if !outcome.exceeds() {
		t.Fatal("outcome = zipBoundOK -- a record that overruns its own directory was never verified and must be refused")
	}
	if outcome != zipBoundMalformed {
		t.Fatalf("outcome = %v, want zipBoundMalformed -- this archive is a few hundred bytes; raising any ceiling cannot make a record fit inside a directory it overruns", outcome)
	}
}

// TestInspectCFBWalkStaysInsideTheWorkAllowance is the largest instance of
// PR9R23's class and the one the finding did not name: convert.ClassifyCFB
// takes no byte allowance of its own and its bounded directory walk can read up
// to ~16 MiB (cfbMaxDirSectors x a 4096-byte sector), while
// dispatchInspectFamilies charges the operation's allowance -- 8 MiB by default
// -- and reports it as enforced.
//
// With the allowance set below one sector, the walk cannot complete, and the
// point of the test is WHICH answer that produces: not an ole_cfb_unclassified
// finding (a structural verdict about bytes nobody read), but all three codes
// unknown with work_ceiling_bytes, and a report whose coverage says partial.
func TestInspectCFBWalkStaysInsideTheWorkAllowance(t *testing.T) {
	resolver := newFakeResolver()
	srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
		InspectMaxWorkBytes: 64, // far below one 512-byte CFB sector
	})

	cfb := append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, make([]byte, 4096)...)
	status, body := doInspect(t, srv, "legacy.doc", cfb)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}

	var resp inspectResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, body)
	}

	for _, f := range resp.Findings {
		switch f.Code {
		case inspect.CodeOLECFBLegacy, inspect.CodeOLECFBEncrypted, inspect.CodeOLECFBUnclassified:
			t.Errorf("got Finding %+v -- the directory walk was cut short by our own byte ceiling, so no CFB class was established; publishing one states a conclusion drawn from bytes that were never read", f)
		}
	}
	for _, code := range []inspect.Code{inspect.CodeOLECFBLegacy, inspect.CodeOLECFBEncrypted, inspect.CodeOLECFBUnclassified} {
		var found *inspect.Check
		for i := range resp.Checks {
			if resp.Checks[i].Code == code {
				found = &resp.Checks[i]
			}
		}
		if found == nil {
			t.Errorf("no Check for %s", code)
			continue
		}
		if found.Status != inspect.StatusUnknown || found.Reason != inspect.ReasonWorkCeilingBytes {
			t.Errorf("Check(%s) = %+v, want unknown/work_ceiling_bytes", code, *found)
		}
	}
	if resp.Coverage.Complete {
		t.Error("Coverage.Complete = true -- the CFB walk was truncated by our ceiling, so this inspection is not complete")
	}
}

// alwaysFailingFile is a multipart.File whose every read fails -- the
// disk-backed temp file that has gone away, or a device error. Unlike
// failingAfterPrefixContainerFile above it does not even deliver a magic
// prefix, so nothing in the detector chain can recognize it.
type alwaysFailingFile struct {
	size    int64
	failErr error
}

func (f *alwaysFailingFile) ReadAt([]byte, int64) (int, error) { return 0, f.failErr }
func (f *alwaysFailingFile) Read([]byte) (int, error)          { return 0, f.failErr }
func (f *alwaysFailingFile) Seek(int64, int) (int64, error)    { return 0, f.failErr }
func (f *alwaysFailingFile) Close() error                      { return nil }

// TestInspectInitialSniffReadFailureIsReadFailedNotUnrecognized is PR9R25
// (inspect_handlers.go:509), verbatim: "When a disk-backed multipart file
// returns a non-EOF error during the initial 12-byte magic sniff, this branch
// discards the error and continues as though no signature matched... the
// response consequently reports an empty format with format_unrecognized checks
// instead of read_failed, conflating an operational temp-file failure with
// genuinely unknown content."
//
// The two answers are about different subjects -- this service, and the upload
// -- and a client acts on them differently: format_unrecognized says "your file
// is something I do not handle" (do not retry), read_failed says "I could not
// read it" (retry is reasonable). PR9R8-F3 closed this for the ZIP sniff; the
// read that runs BEFORE it still had it. Measured on the pre-fix code, this
// exact input reported detected="" with every check unknown/format_unrecognized.
func TestInspectInitialSniffReadFailureIsReadFailedNotUnrecognized(t *testing.T) {
	file := &alwaysFailingFile{size: 4096, failErr: errors.New("simulated temp-file read failure")}

	detected, sniffed, sniffedZip := detectInspectFormat(file, file.size, "png")
	if detected != inspectSniffReadFailedFormat {
		t.Fatalf("detected = %q, want %q -- an unreadable upload is not an unrecognized one", detected, inspectSniffReadFailedFormat)
	}

	tr := inspect.NewTracker(inspect.Budget{MaxBytes: 1 << 20, MaxDuration: time.Minute})
	families := dispatchInspectFamilies(detected, sniffed, sniffedZip, file, file.size, tr)
	families = append(families, extensionMismatchFamilyResult(detected, "png"))
	// reportFormat, exactly as handleInspect does it: the sentinel routes the
	// dispatch above and must not survive into the response (PR9R26).
	report := inspect.BuildReport(reportFormat(detected), "png", file.size, tr, families...)

	if report.Format != "" {
		t.Errorf("report.Format = %q, want empty -- nothing about this upload was recognized, and the contract says an unrecognized upload reports an empty format; publishing an internal routing sentinel invents a format that does not exist", report.Format)
	}

	if err := inspect.ValidateReport(report.Findings, report.Checks); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}
	if len(report.Findings) != 0 {
		t.Errorf("findings = %+v, want none -- nothing was read, so nothing can be claimed", report.Findings)
	}
	// EVERY code, not just the container three: the failure predates any format
	// claim, so no code may be called not_applicable to a format we never
	// established, and none may say format_unrecognized either.
	for _, c := range report.Checks {
		if c.Status != inspect.StatusUnknown || c.Reason != inspect.ReasonReadFailed {
			t.Errorf("check %s = %+v, want unknown/read_failed -- reporting our own read failure as a fact about the upload is the conflation this closes", c.Code, c)
		}
	}
	if len(report.Checks) != len(inspect.AllCodes()) {
		t.Errorf("report carries %d checks, want %d (the whole closed vocabulary)", len(report.Checks), len(inspect.AllCodes()))
	}
}

// TestInspectSniffReadFailureDoesNotSuppressALaterDetection is the
// over-rejection guard on the fix above: a failed magic peek does not prove the
// later detectors will fail too, so the sentinel must only decide the outcome
// when the whole chain came up empty. Trading a false format_unrecognized for a
// false read_failed would be no improvement.
func TestInspectSniffReadFailureDoesNotSuppressALaterDetection(t *testing.T) {
	data := macroDocxFixture(t)
	// Streaming reads (convert.Sniff's own path) fail; positional reads, which
	// the ZIP detector uses, succeed.
	file := &failingStreamOnlyFile{data: data}

	detected, _, _ := detectInspectFormat(file, int64(len(data)), "docx")
	if detected != "docx" {
		t.Errorf("detected = %q, want docx -- the ZIP detector succeeded on this upload, and a failed magic peek must not override a detection that actually happened", detected)
	}
}

// failingStreamOnlyFile fails every streaming Read (so convert.Sniff's peek
// errors) while serving positional ReadAt calls normally.
type failingStreamOnlyFile struct {
	data []byte
}

func (f *failingStreamOnlyFile) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(p, f.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
func (f *failingStreamOnlyFile) Read([]byte) (int, error) {
	return 0, errors.New("simulated streaming read failure")
}
func (f *failingStreamOnlyFile) Seek(int64, int) (int64, error) {
	return 0, errors.New("simulated seek failure")
}
func (f *failingStreamOnlyFile) Close() error { return nil }

// failAfterNthReadAtFile serves the first failAt positional reads normally and
// fails every one after that -- a temp file that goes away mid-detection.
//
// ReadAt, not Read, is the axis that matters: detectInspectFormat hands every
// sniffer a fresh io.NewSectionReader over the file, so the whole chain reads
// positionally. With failAt=1 the initial magic peek succeeds and every later
// sniffer fails, which isolates exactly what this test is about -- the LATER
// sniffers' discarded errors.
type failAfterNthReadAtFile struct {
	data    []byte
	off     int64
	reads   int
	failAt  int
	failErr error
}

func (f *failAfterNthReadAtFile) ReadAt(p []byte, off int64) (int, error) {
	f.reads++
	if f.reads > f.failAt {
		return 0, f.failErr
	}
	if off < 0 || off >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(p, f.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (f *failAfterNthReadAtFile) Read(p []byte) (int, error) {
	n, err := f.ReadAt(p, f.off)
	f.off += int64(n)
	return n, err
}

func (f *failAfterNthReadAtFile) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		f.off = off
	case io.SeekCurrent:
		f.off += off
	case io.SeekEnd:
		f.off = int64(len(f.data)) + off
	}
	return f.off, nil
}
func (f *failAfterNthReadAtFile) Close() error { return nil }

// TestInspectLaterSnifferReadFailureIsReadFailedNotUnrecognized is PR9R27
// (inspect_handlers.go:657), verbatim: "When the initial 12-byte sniff succeeds
// without recognizing the file, but a later range of the multipart temp file
// returns a non-EOF I/O error, this branch discards `aerr` and leaves `detected`
// empty; the video branch immediately above does the same with `verr`."
//
// This is the previous round's own fix left half-applied: the initial peek was
// taught to carry its failure and its two siblings were not. All three share
// one contract -- an unrecognized upload is ("", rest, nil) -- so a non-nil
// error from any of them is always a read failure, never a verdict.
func TestInspectLaterSnifferReadFailureIsReadFailedNotUnrecognized(t *testing.T) {
	// Bytes no detector recognizes, so the chain runs all the way to the audio
	// sniffer. The first streaming read (convert.Sniff's peek) succeeds; the
	// later ones fail.
	unrecognized := bytes.Repeat([]byte{0x7A}, 4096)
	file := &failAfterNthReadAtFile{
		data:    unrecognized,
		failAt:  1, // the initial magic peek succeeds; everything after it fails
		failErr: errors.New("simulated mid-detection temp-file failure"),
	}

	detected, _, _ := detectInspectFormat(file, int64(len(unrecognized)), "bin")
	if detected != inspectSniffReadFailedFormat {
		t.Fatalf("detected = %q, want %q -- a sniffer that failed to READ must not be reported as one that did not RECOGNIZE", detected, inspectSniffReadFailedFormat)
	}
}

// TestExtensionMismatchUnrecognizedUploadIsUnknownNotNotApplicable is PR9R27
// (inspect_handlers.go:1393): for an unrecognized upload carrying a filename
// extension, this check alone said not_applicable while every other code in the
// vocabulary said unknown/format_unrecognized. not_applicable claims "there was
// nothing here to look for", which requires knowing what the file IS -- and a
// client reading it would conclude the extension is fine when nothing was ever
// compared against it.
func TestExtensionMismatchUnrecognizedUploadIsUnknownNotNotApplicable(t *testing.T) {
	t.Run("unrecognized upload with an extension", func(t *testing.T) {
		res := extensionMismatchFamilyResult("", "png")
		if len(res.Checks) != 1 {
			t.Fatalf("checks = %+v, want exactly one", res.Checks)
		}
		c := res.Checks[0]
		if c.Status != inspect.StatusUnknown || c.Reason != inspect.ReasonFormatUnrecognized {
			t.Errorf("check = %+v, want unknown/format_unrecognized -- applicability was never established, so calling the check irrelevant overclaims", c)
		}
	})

	t.Run("a filename with no extension stays not_applicable", func(t *testing.T) {
		// The genuinely inapplicable case, and the over-rejection guard on the
		// change above: both sides were established here and one of them is
		// simply absent, so nothing is unknown.
		res := extensionMismatchFamilyResult("png", "")
		if len(res.Checks) != 1 {
			t.Fatalf("checks = %+v, want exactly one", res.Checks)
		}
		c := res.Checks[0]
		if c.Status != inspect.StatusNotApplicable || c.Reason != inspect.ReasonFormatNotApplicable {
			t.Errorf("check = %+v, want not_applicable/format_not_applicable", c)
		}
	})
}
