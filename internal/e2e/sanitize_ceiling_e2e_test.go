// This file proves the sanitize page ceiling on the wire, against a running
// stack, because everything else that proves it proves it against doubles.
//
// PROBE-01's admission gate was removed (63-CONTEXT.md D-04, reversed), so
// there is no 422 to assert here any more. What is left is the claim that
// matters and always did: an over-long document is refused, terminally, with a
// code the client can act on -- and a document UNDER the ceiling still gets
// sanitized, which a ceiling that refused everything would also satisfy.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// multiPagePDF builds a structurally valid PDF with the requested page count,
// xref offsets included, so the worker's pdfinfo and the API's own walk both
// read it as a real document.
//
// Built rather than committed for the reason 63-02's unit fixtures give: the
// property under test is the page COUNT, and a 201-page corpus file would be a
// binary whose only content is padding.
func multiPagePDF(pages int) []byte {
	objs := []string{"<< /Type /Catalog /Pages 2 0 R >>"}
	kids := ""
	for i := 0; i < pages; i++ {
		kids += strconv.Itoa(3+i) + " 0 R "
	}
	objs = append(objs, "<< /Type /Pages /Kids ["+kids+"] /Count "+strconv.Itoa(pages)+" >>")
	for i := 0; i < pages; i++ {
		objs = append(objs, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << >> >>")
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs)+1)
	for i, o := range objs {
		offsets[i+1] = b.Len()
		b.WriteString(strconv.Itoa(i+1) + " 0 obj\n" + o + "\nendobj\n")
	}
	xref := b.Len()
	b.WriteString("xref\n0 " + strconv.Itoa(len(objs)+1) + "\n0000000000 65535 f \n")
	for i := 1; i <= len(objs); i++ {
		b.WriteString(fmt.Sprintf("%010d 00000 n \n", offsets[i]))
	}
	b.WriteString("trailer\n<< /Size " + strconv.Itoa(len(objs)+1) + " /Root 1 0 R >>\nstartxref\n" +
		strconv.Itoa(xref) + "\n%%EOF\n")
	return b.Bytes()
}

// postSanitize submits one sanitize job with target "pdf" and returns the
// raw response, without asserting a status -- the point of two of the tests
// below is the status. A thin wrapper over postSanitizeTo (69-03): every
// pre-69-03 caller sanitizes a PDF, so this keeps their call sites unchanged.
func postSanitize(t *testing.T, baseURL, apiKey, filename string, data []byte) (int, map[string]any) {
	t.Helper()
	return postSanitizeTo(t, baseURL, apiKey, filename, "pdf", data)
}

// postSanitizeTo submits one sanitize job with the given target and returns
// the raw response, without asserting a status -- callers that need a
// specific status assert it themselves. Added in 69-03 so the new OOXML/image
// sanitize routes (target "docx", target "jpg") can share this helper with
// the PDF ceiling tests instead of duplicating the multipart-build/POST
// sequence a third time.
func postSanitizeTo(t *testing.T, baseURL, apiKey, filename, target string, data []byte) (int, map[string]any) {
	t.Helper()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	_ = w.WriteField("target", target)
	_ = w.WriteField("operation", "sanitize")
	_ = w.Close()

	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/jobs", &body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "ApiKey "+apiKey)

	resp, err := e2eHTTP.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/jobs: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	_ = json.Unmarshal(raw, &parsed)
	if parsed == nil {
		t.Fatalf("POST /v1/jobs returned unparseable body: %s", raw)
	}
	return resp.StatusCode, parsed
}

// TestSanitizeCeilingIsTerminalE2E is the ceiling on the wire: an over-long
// document is ADMITTED -- there is no longer an admission gate -- and then
// refused by the worker on its FIRST attempt, with no retries spent.
func TestSanitizeCeilingIsTerminalE2E(t *testing.T) {
	cfg := e2eSetup(t)
	apiKey := provisionClient(t)

	max := 200
	if v := os.Getenv("PROBE_MAX_PAGES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			max = n
		}
	}

	status, body := postSanitize(t, cfg.baseURL, apiKey, "over.pdf", multiPagePDF(max+1))
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 -- the admission gate is gone, so an over-long document is "+
			"accepted and refused by the worker; body=%v", status, body)
	}
	jobID, _ := body["job_id"].(string)
	if jobID == "" {
		t.Fatalf("no job id in the accepted response: %v", body)
	}

	failed := pollUntilFailed(t, cfg.baseURL, apiKey, jobID, 3*time.Minute)
	if code, _ := failed["error_code"].(string); code != "too_many_pages" {
		t.Fatalf("error_code = %q, want \"too_many_pages\" -- the client must be able to tell this "+
			"apart from a generic engine failure; body=%v", code, failed)
	}
}

// TestSanitizeUnderCeilingStillSanitizesE2E is the other half, and the reason
// the test above is not sufficient on its own: a ceiling that refuses
// everything would pass it.
func TestSanitizeUnderCeilingStillSanitizesE2E(t *testing.T) {
	cfg := e2eSetup(t)
	apiKey := provisionClient(t)

	data, err := os.ReadFile(filepath.Join("..", "convert", "testdata", "sanitize", "threat_all_classes.pdf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	// Deliberately the THREAT fixture rather than a blank page. The first
	// version of the worker gate counted pages through pdfcpu's validator,
	// which refuses several of the constructs sanitize exists to remove -- so
	// with a ceiling configured, every threat-carrying PDF failed terminally as
	// malformed input. That defect passed the entire unit suite; this is the
	// shape of test that would have caught it on the wire.
	status, body := postSanitize(t, cfg.baseURL, apiKey, "threat.pdf", data)
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 for a one-page threat-carrying PDF; body=%v", status, body)
	}
	jobID, _ := body["job_id"].(string)
	if jobID == "" {
		t.Fatalf("no job id in the accepted response: %v", body)
	}

	done := pollUntilDone(t, cfg.baseURL, apiKey, jobID, 3*time.Minute)
	downloadURL, _ := done["download_url"].(string)
	if downloadURL == "" {
		t.Fatalf("job %s done but download_url missing: %v", jobID, done)
	}
	assertDownloadIsPDF(t, downloadURL)
}
