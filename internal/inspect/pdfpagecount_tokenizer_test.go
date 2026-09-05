package inspect

import (
	"strconv"
	"testing"
)

// page_count is derived by a TOKENIZER now, not by a byte scan near a marker.
//
// The scan this replaces found a "/Type /Pages" marker and took the first
// "/Count" within 512 bytes after it. Four ways that is wrong were reported
// against it and a fifth was found while replacing it. Every row below was
// REPRODUCED against the old code before anything changed -- each returned 300
// (or an object number) for a two-page document -- and the "was" column is what
// it returned.
//
// They are one class: a token recognised by its bytes without regard to the
// lexical context it sits in. pdflex.go's own header already listed the same
// class one key name over ("/Length matched inside /Length1", "a /Length inside
// [ ... ] read as a key of the enclosing dictionary"), which is why the fix is
// the tokenizer rather than a fifth comparison.
func TestPageCountReadsDictionaryKeysNotBytes(t *testing.T) {
	const twoPages = 2

	cases := []struct {
		name string
		body string
		was  string
	}{
		{
			name: "a Pages dict inside a content stream",
			body: "%PDF-1.4\n1 0 obj\n<< /Type /Pages /Count 2 >>\nendobj\n" +
				"2 0 obj\n<< /Length 30 >>\nstream\n<< /Type /Pages /Count 300 >>\nendstream\nendobj\n",
			was: "300 -- payload bytes read as structure",
		},
		{
			name: "a /Count in the NEXT dictionary",
			body: "%PDF-1.4\n1 0 obj\n<< /Count 2 /Type /Pages >>\nendobj\n" +
				"2 0 obj\n<< /Outlines 5 0 R /Count 300 >>\nendobj\n",
			was: "300 -- the window ran past this dictionary's own end",
		},
		{
			name: "a key that merely starts with Count",
			body: "%PDF-1.4\n1 0 obj\n<< /Type /Pages /Count300 0 /Count 2 >>\nendobj\n",
			was:  "300 -- /Count300 matched the key /Count by prefix",
		},
		{
			name: "a /Count inside an array value",
			body: "%PDF-1.4\n1 0 obj\n<< /Type /Pages /Extra [ /Count 300 ] /Count 2 >>\nendobj\n",
			was:  "300 -- an array element read as a direct key",
		},
		{
			name: "an indirect /Count",
			body: "%PDF-1.4\n1 0 obj\n<< /Type /Pages /Count 3 0 R >>\nendobj\n3 0 obj\n2\nendobj\n",
			was:  "3 -- an OBJECT NUMBER reported as a page count",
		},
		{
			name: "the ordinary case",
			body: "%PDF-1.4\n1 0 obj\n<< /Type /Pages /Count 2 >>\nendobj\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := extractPDFPageCount([]byte(tc.body), false, false, false, &Tracker{})
			if reason != "" {
				t.Fatalf("reason = %q, want a value: this document is legible", reason)
			}
			if got == nil || *got != twoPages {
				val := "nil"
				if got != nil {
					val = strconv.Itoa(*got)
				}
				msg := "page_count = %s, want %d"
				if tc.was != "" {
					msg += "\n  the byte scan returned " + tc.was
				}
				t.Fatalf(msg, val, twoPages)
			}
		})
	}
}
