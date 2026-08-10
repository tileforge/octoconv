package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/apaderin/octoconv/internal/inspect"
)

// manyEntriesDocxFixture is a REAL, valid docx-shaped zip (word/document.xml
// root part present, so convert.SniffContainer would resolve Format="docx"
// were it allowed to run to completion) plus n additional tiny junk
// entries. Used to exercise zipDirectoryExceedsStructuralBound's own entry-
// count ceiling with a genuinely parseable archive rather than a
// hand-crafted/inconsistent one -- n above inspectMaxZipDirEntries proves
// the bound trips BEFORE the docx root part would otherwise be recognized.
func manyEntriesDocxFixture(t *testing.T, n int) []byte {
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
// zipDirectoryExceedsStructuralBound itself answers the entries/dirBytes
// question from the EOCD record alone, for both a real small archive
// (exceeds=false) and a real archive whose entry count crosses the ceiling
// (exceeds=true) -- exercised directly, independent of the HTTP handler,
// against real archive/zip.Writer output.
func TestZipDirectoryExceedsStructuralBoundNeverParsesOversizedArchive(t *testing.T) {
	small := docxFixture(t)
	r := bytes.NewReader(small)
	if exceeds, _, _ := zipDirectoryExceedsStructuralBound(r, int64(len(small))); exceeds {
		t.Errorf("small docx fixture: exceeds = true, want false")
	}

	junkEntries := inspectMaxZipDirEntries + 1
	wantEntries := junkEntries + 1 // +1 for the fixture's own word/document.xml root part
	big := manyEntriesDocxFixture(t, junkEntries)
	r2 := bytes.NewReader(big)
	exceeds, entries, _ := zipDirectoryExceedsStructuralBound(r2, int64(len(big)))
	if !exceeds {
		t.Fatalf("exceeds = false, want true for %d entries (bound is %d)", wantEntries, inspectMaxZipDirEntries)
	}
	if int(entries) != wantEntries {
		t.Errorf("entries = %d, want %d", entries, wantEntries)
	}
}

// TestInspectZipDirectoryBoundInversion is D-16's own inversion discipline,
// applied here directly at the unit level rather than by temporarily
// editing production code: manually constructing an EOCD-only buffer that
// declares an over-bound entry count but is NOT preceded by the PK\x03\x04
// detectInspectFormat gates on proves the bound-check FUNCTION itself is
// what produces exceeds=true (not some other accidental signal) -- and,
// documented here in prose per D-16, temporarily commenting out the
// zipDirectoryExceedsStructuralBound call in detectInspectFormat's own ZIP
// branch (restoring the pre-fix code exactly) makes
// TestInspectZipDirectoryOverBoundReportsUnknownNot500 above fail: Format
// becomes "docx" (the full, unbounded parse succeeds) and every
// container-domain check reports clean instead of unknown/work_ceiling_bytes
// -- verified by hand while writing this fix, reverted before this commit.
func TestInspectZipDirectoryBoundInversion(t *testing.T) {
	// A minimal, self-consistent 22-byte EOCD record (no comment, no real
	// central directory behind it) declaring entries=inspectMaxZipDirEntries+1.
	eocd := make([]byte, 22)
	eocd[0], eocd[1], eocd[2], eocd[3] = 'P', 'K', 0x05, 0x06
	entries := uint16(inspectMaxZipDirEntries + 1)
	eocd[10] = byte(entries)
	eocd[11] = byte(entries >> 8)
	// comment length (offset 20:22) left at zero.

	exceeds, gotEntries, _ := zipDirectoryExceedsStructuralBound(bytes.NewReader(eocd), int64(len(eocd)))
	if !exceeds {
		t.Fatalf("exceeds = false, want true")
	}
	if gotEntries != entries {
		t.Fatalf("entries = %d, want %d", gotEntries, entries)
	}
}
