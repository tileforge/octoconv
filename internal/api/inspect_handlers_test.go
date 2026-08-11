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
	exceeds, gotEntries, gotDirBytes := zipDirectoryExceedsStructuralBound(bytes.NewReader(data), int64(len(data)))
	if gotEntries != 1 {
		t.Fatalf("entries = %d, want 1 (65537 mod 65536, read verbatim from the EOCD's own field)", gotEntries)
	}
	if gotDirBytes != zipCentralDirMinRecordLen {
		t.Fatalf("dirBytes = %d, want %d (the lying declared size)", gotDirBytes, zipCentralDirMinRecordLen)
	}
	if !exceeds {
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

	exceeds, gotEntries, gotDirBytes := zipDirectoryExceedsStructuralBound(bytes.NewReader(data), int64(len(data)))
	if gotEntries != n || gotDirBytes != uint32(n)*recordLen {
		t.Fatalf("entries/dirBytes = %d/%d, want %d/%d -- this test's own EOCD is completely honest", gotEntries, gotDirBytes, n, uint32(n)*recordLen)
	}
	if !exceeds {
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
	exceeds, _, _ := zipDirectoryExceedsStructuralBound(v, v.size)
	if !exceeds {
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

	exceeds, gotEntries, gotDirBytes := zipDirectoryExceedsStructuralBound(bytes.NewReader(data), int64(len(data)))
	if gotEntries != 1 {
		t.Fatalf("entries = %d, want 1 (65537 mod 65536, read verbatim from the EOCD's own field)", gotEntries)
	}
	if gotDirBytes != uint32(65537*zipCentralDirMinRecordLen) {
		t.Fatalf("dirBytes = %d, want %d (the HONEST full declared size)", gotDirBytes, uint32(65537*zipCentralDirMinRecordLen))
	}
	if !exceeds {
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

	exceeds, _, _ := zipDirectoryExceedsStructuralBound(bytes.NewReader(data), int64(len(data)))
	if !exceeds {
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

	exceeds, _, dirBytes := zipDirectoryExceedsStructuralBound(bytes.NewReader(eocd), int64(len(eocd)))
	if dirBytes != 0xFFFF {
		t.Fatalf("dirBytes = %#x, want 0xffff", dirBytes)
	}
	if !exceeds {
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
func zipDirectoryDifferentialCorpus(t *testing.T) []zipDifferentialCase {
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

			exceeds, _, _ := zipDirectoryExceedsStructuralBound(bytes.NewReader(data), int64(len(data)))

			if realCount > inspectMaxZipDirEntries && !exceeds {
				t.Fatalf("FORBIDDEN COMBINATION on corpus case %q: archive/zip.NewReader parsed %d real records (> ceiling %d) but zipDirectoryExceedsStructuralBound said exceeds=false.\nInput: %s", tc.name, realCount, inspectMaxZipDirEntries, tc.describe)
			}
		})
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

	_ = dispatchInspectFamilies("xlsx", nil, f, int64(len(data)), tr)

	if !tr.Exhausted() {
		t.Fatal("dispatchInspectFamilies returned without marking the Tracker it was given -- an analyzer hit its own internal ceiling (xl/workbook.xml over maxRelsPartBytes) and that fact did not reach the tracker, so BuildReport would report complete coverage over an analysis that stopped early. The usual cause is a dispatch site passing nil instead of tr.")
	}
}
