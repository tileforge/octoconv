// This file proves the two sanitize routes the ceiling e2e tests never drove:
// OOXML (docm -> docx) and image (jpg -> jpg). Before Phase 69, no e2e test
// exercised these -- the ceiling tests only ever posted a PDF -- and the
// probe worker's independent verifier runs unzip and file(1) under the
// Landlock ruleset only on these two routes (sanitizeverify.go's
// verifyOOXML/verifyImage). A confinement grant that is wrong for pdfinfo/gs
// but happens to be right for unzip/file would pass every PDF-only e2e test
// and still deny the live worker mid-job on a real OOXML or image upload.
package e2e

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSanitizeOOXMLAndImageThroughTheConfinedWorkerE2E drives both routes
// through the LIVE pipeline: multipart upload -> poll to done -> presigned
// download -> magic-byte / structural check on the downloaded bytes.
func TestSanitizeOOXMLAndImageThroughTheConfinedWorkerE2E(t *testing.T) {
	cfg := e2eSetup(t)
	apiKey := provisionClient(t)

	t.Run("docm", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "convert", "testdata", "sanitize", "macro_enabled.docm"))
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}

		status, body := postSanitizeTo(t, cfg.baseURL, apiKey, "macro_enabled.docm", "docx", data)
		if status != 202 {
			t.Fatalf("status = %d, want 202; body=%v", status, body)
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

		resp, err := downloadClient().Get(downloadURL)
		if err != nil {
			t.Fatalf("GET download_url: %v", err)
		}
		defer resp.Body.Close()
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(resp.Body); err != nil {
			t.Fatalf("read download body: %v", err)
		}
		got := buf.Bytes()

		if resp.StatusCode != 200 {
			t.Fatalf("GET download_url status = %d, want 200; body=%s", resp.StatusCode, got)
		}
		if !bytes.HasPrefix(got, []byte("PK\x03\x04")) {
			t.Fatalf("downloaded sanitized docm does not start with the zip local-file-header magic PK\\x03\\x04; got %x", got[:min(8, len(got))])
		}

		zr, err := zip.NewReader(bytes.NewReader(got), int64(len(got)))
		if err != nil {
			t.Fatalf("open sanitized output as zip: %v", err)
		}
		for _, f := range zr.File {
			if strings.HasSuffix(f.Name, "vbaProject.bin") {
				t.Fatalf("sanitized output still contains %q -- the macro part was not removed", f.Name)
			}
		}
	})

	t.Run("jpg", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "convert", "testdata", "sanitize", "exif_gps.jpg"))
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}

		status, body := postSanitizeTo(t, cfg.baseURL, apiKey, "exif_gps.jpg", "jpg", data)
		if status != 202 {
			t.Fatalf("status = %d, want 202; body=%v", status, body)
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

		resp, err := downloadClient().Get(downloadURL)
		if err != nil {
			t.Fatalf("GET download_url: %v", err)
		}
		defer resp.Body.Close()
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(resp.Body); err != nil {
			t.Fatalf("read download body: %v", err)
		}
		got := buf.Bytes()

		if resp.StatusCode != 200 {
			t.Fatalf("GET download_url status = %d, want 200; body=%s", resp.StatusCode, got)
		}
		if !bytes.HasPrefix(got, []byte{0xFF, 0xD8, 0xFF}) {
			t.Fatalf("downloaded sanitized jpg does not start with FF D8 FF; got %x", got[:min(8, len(got))])
		}
	})
}
