package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// Both directions in one table, because a validator tested only on what it
// must reject is a validator that can reject everything and still pass.

func TestParseCorrelationID(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
		bad   bool
	}{
		// Accepted: every identifier shape anyone actually uses.
		{"a uuid", "0a001d7a-66fa-4e68-98dd-d2103a291e21", "0a001d7a-66fa-4e68-98dd-d2103a291e21", false},
		{"a ulid", "01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAV", false},
		{"base64url", "aB3-_x9Q", "aB3-_x9Q", false},
		{"a dotted key", "billing.invoice.4711", "billing.invoice.4711", false},
		{"a slashed key", "svc/checkout/req-99", "svc/checkout/req-99", false},
		{"exactly at the limit", strings.Repeat("a", maxCorrelationIDLen), strings.Repeat("a", maxCorrelationIDLen), false},
		{"absent", "", "", false},

		// Refused: anything that could end a log record or a JSON string early.
		{"a newline", "a\nb", "", true},
		{"a carriage return", "a\rb", "", true},
		{"a forged log record", "x\njob done job_id=FORGED status=done", "", true},
		{"a tab", "a\tb", "", true},
		{"a space", "a b", "", true},
		{"a NUL", "a\x00b", "", true},
		{"an ANSI escape", "a\x1b[31mb", "", true},
		{"non-ascii", "héllo", "", true},
		{"one over the limit", strings.Repeat("a", maxCorrelationIDLen+1), "", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := http.Header{}
			if c.value != "" {
				h.Set(CorrelationHeader, c.value)
			}
			got, err := parseCorrelationID(h)
			if c.bad {
				if err == nil {
					t.Fatalf("%q was accepted. It would reach a log record and a webhook body, "+
						"and this service would be carrying something a client composed.", c.value)
				}
				if !errors.Is(err, ErrCorrelationIDInvalid) {
					t.Fatalf("refused with an unexpected error: %v", err)
				}
				if got != "" {
					t.Fatalf("a refused value was still returned: %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("%q was refused: %v.\nRefusing a legitimate identifier is the more "+
					"expensive failure: the caller cannot correlate anything and has no way to "+
					"know why.", c.value, err)
			}
			if got != c.want {
				t.Fatalf("value = %q, want %q", got, c.want)
			}
		})
	}
}

// Absent must not become "generated". A value we invented is a second internal
// identifier next to job_id, which is exactly what this design does not have.
func TestAbsentCorrelationIDIsNotInvented(t *testing.T) {
	got, err := parseCorrelationID(http.Header{})
	if err != nil {
		t.Fatalf("an absent header is not an error: %v", err)
	}
	if got != "" {
		t.Fatalf("an id was generated for a caller that sent none: %q. job_id already identifies "+
			"the job internally; a second one drifts from the first.", got)
	}
}
