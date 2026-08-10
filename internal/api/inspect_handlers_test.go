package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
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
	if resp.Format != inspectZipTooLargeFormat {
		t.Errorf("Format = %q, want %q -- entries=1 (wrapped) must NOT let this upload past the structural bound", resp.Format, inspectZipTooLargeFormat)
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
	if exceeds, _, _ := zipDirectoryExceedsStructuralBound(r, int64(len(small))); exceeds {
		t.Errorf("small docx fixture: exceeds = true, want false")
	}

	junkEntries := inspectMaxZipDirEntries + 1
	wantEntries := junkEntries + 1 // +1 for the fixture's own word/document.xml root part
	big := manyEntriesDocxFixture(t, junkEntries)
	r2 := bytes.NewReader(big)
	exceeds, entries, dirBytes := zipDirectoryExceedsStructuralBound(r2, int64(len(big)))
	if !exceeds {
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

	exceeds, gotEntries, gotDirBytes := zipDirectoryExceedsStructuralBound(bytes.NewReader(eocd), int64(len(eocd)))
	if gotEntries != wrappedEntries {
		t.Fatalf("entries = %d, want %d (the wrapped value the reviewer's scenario declares)", gotEntries, wrappedEntries)
	}
	if gotDirBytes != trueDirBytes {
		t.Fatalf("dirBytes = %d, want %d", gotDirBytes, trueDirBytes)
	}
	if !exceeds {
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

	exceeds, _, dirBytes := zipDirectoryExceedsStructuralBound(bytes.NewReader(eocd), int64(len(eocd)))
	if dirBytes != 0xFFFFFFFF {
		t.Fatalf("dirBytes = %#x, want 0xffffffff", dirBytes)
	}
	if !exceeds {
		t.Fatalf("exceeds = false, want true -- the ZIP64 sentinel must never be read as a small-enough declared size")
	}
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
