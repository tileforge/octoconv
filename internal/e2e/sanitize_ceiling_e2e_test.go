// This file proves PROBE-01 and PROBE-02 on the wire, against a running stack,
// because everything else that proves them proves them against doubles.
//
// 63-02's unit tests assert that s.storage.Upload is never called by using a
// storage double that fails the test from inside Upload. That is the right test
// and it is not the same claim: it says the handler does not call the method,
// not that a client sending a real over-long PDF to a real API gets a 422 with
// a code they can act on. SC4 is the second sentence.
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

// postSanitize submits one sanitize job and returns the raw response, without
// asserting a status -- the point of two of the tests below is the status.
func postSanitize(t *testing.T, baseURL, apiKey, filename string, data []byte) (int, map[string]any) {
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
	_ = w.WriteField("target", "pdf")
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

// TestSanitizeCeilingRejectsAtAdmissionE2E is SC4 on the wire: a document over
// PROBE_MAX_PAGES is refused with a machine-readable code, and no job exists to
// poll afterwards.
func TestSanitizeCeilingRejectsAtAdmissionE2E(t *testing.T) {
	cfg := e2eSetup(t)
	apiKey := provisionClient(t)

	max := 200
	if v := os.Getenv("PROBE_MAX_PAGES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			max = n
		}
	}

	status, body := postSanitize(t, cfg.baseURL, apiKey, "over.pdf", multiPagePDF(max+1))

	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 for a %d-page document against a %d-page ceiling; body=%v",
			status, max+1, max, body)
	}
	if code, _ := body["code"].(string); code != "too_many_pages" {
		t.Fatalf("code = %q, want \"too_many_pages\" -- the client must be able to tell this apart "+
			"from a generic rejection; body=%v", code, body)
	}
	// job_id, which is what the accepted response actually carries. The first
	// version of this checked "id" and therefore asserted nothing at all: the
	// key is never present under that name, so the branch could not fire. Caught
	// by the sibling test below failing on the same wrong key.
	if _, hasID := body["job_id"]; hasID {
		t.Fatalf("the rejection carried a job id (%v) -- a refused upload must not have created a "+
			"job row, let alone stored bytes", body["job_id"])
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

// TestSanitizeCeilingIsTerminalInTheWorkerE2E is PROBE-02 on the wire, and the
// half no unit test can claim: a document the CHEAP gate cannot judge is
// admitted, and the worker refuses it terminally with the same code.
//
// The construction is the one 63-CONTEXT.md D-04 designed for. Appending an
// /ObjStm marker makes the API's bounded walk abstain -- an object hidden in a
// compressed object stream is structurally invisible to it, so it cannot claim
// the count it can see is the whole truth -- while pdfinfo in the worker reads
// the page tree perfectly well. Same file, two gates, opposite verdicts, and
// that difference is the entire reason the second gate exists.
func TestSanitizeCeilingIsTerminalInTheWorkerE2E(t *testing.T) {
	cfg := e2eSetup(t)
	apiKey := provisionClient(t)

	max := 200
	if v := os.Getenv("PROBE_MAX_PAGES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			max = n
		}
	}

	blind := append(multiPagePDF(max+1), []byte("\n<< /Type /ObjStm >>\n")...)

	status, body := postSanitize(t, cfg.baseURL, apiKey, "blind.pdf", blind)
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 -- a count the cheap gate cannot establish must be ADMITTED, "+
			"not refused on a heuristic's blind spot; body=%v", status, body)
	}
	jobID, _ := body["job_id"].(string)
	if jobID == "" {
		t.Fatalf("no job id in the accepted response: %v", body)
	}

	failed := pollUntilFailed(t, cfg.baseURL, apiKey, jobID, 3*time.Minute)
	if code, _ := failed["error_code"].(string); code != "too_many_pages" {
		t.Fatalf("error_code = %q, want \"too_many_pages\" -- the two gates report the SAME code for "+
			"the same condition, which is the whole of D-04's one-code-space rule; body=%v",
			code, failed)
	}
}
