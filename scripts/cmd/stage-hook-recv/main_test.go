package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/tileforge/octoconv/internal/webhook"
)

type hookLine struct {
	ReceivedNS int64           `json:"received_ns"`
	SigOK      bool            `json:"sig_ok"`
	Body       json.RawMessage `json:"body"`
}

func post(t *testing.T, h http.Handler, body, ts, sig string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/hook", strings.NewReader(body))
	req.Header.Set("X-OctoConv-Timestamp", ts)
	req.Header.Set("X-OctoConv-Signature", sig)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func readLines(t *testing.T, out *bytes.Buffer) []hookLine {
	t.Helper()
	var lines []hookLine
	for _, raw := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if raw == "" {
			continue
		}
		var l hookLine
		if err := json.Unmarshal([]byte(raw), &l); err != nil {
			t.Fatalf("line %q is not JSON: %v", raw, err)
		}
		lines = append(lines, l)
	}
	return lines
}

func TestSignedDeliveryIsRecordedWithSigOK(t *testing.T) {
	secret := []byte("s3cret")
	var out bytes.Buffer
	h := newHandler(secret, &out)
	body := `{"job_id":"j1","event":"progress","stage":"guard","stage_index":1}`
	var ts int64 = 1760000000
	rec := post(t, h, body, strconv.FormatInt(ts, 10), webhook.SignPayload(secret, ts, []byte(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	lines := readLines(t, &out)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1: %q", len(lines), out.String())
	}
	if !lines[0].SigOK {
		t.Fatalf("sig_ok false for a correctly signed delivery")
	}
	if string(lines[0].Body) != body {
		t.Fatalf("body %s, want %s", lines[0].Body, body)
	}
	if lines[0].ReceivedNS <= 0 {
		t.Fatalf("received_ns %d, want a positive timestamp", lines[0].ReceivedNS)
	}
}

func TestWrongSignatureIsRecordedWithoutSigOK(t *testing.T) {
	secret := []byte("s3cret")
	var out bytes.Buffer
	h := newHandler(secret, &out)
	body := `{"job_id":"j1","event":"completed"}`
	var ts int64 = 1760000000
	rec := post(t, h, body, strconv.FormatInt(ts, 10), webhook.SignPayload([]byte("other"), ts, []byte(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	lines := readLines(t, &out)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1: %q", len(lines), out.String())
	}
	if lines[0].SigOK {
		t.Fatalf("sig_ok true for a delivery signed with another secret")
	}
	if string(lines[0].Body) != body {
		t.Fatalf("body %s, want %s", lines[0].Body, body)
	}

	out.Reset()
	rec = post(t, h, body, "not-a-number", webhook.SignPayload(secret, ts, []byte(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	lines = readLines(t, &out)
	if len(lines) != 1 || lines[0].SigOK {
		t.Fatalf("an unparseable timestamp must give one line with sig_ok false, got %q", out.String())
	}
}

func TestNonPostIsRejectedWithoutALine(t *testing.T) {
	var out bytes.Buffer
	h := newHandler([]byte("s3cret"), &out)
	req := httptest.NewRequest(http.MethodGet, "/hook", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", rec.Code)
	}
	if out.Len() != 0 {
		t.Fatalf("a non-POST wrote %q, want nothing", out.String())
	}
}
