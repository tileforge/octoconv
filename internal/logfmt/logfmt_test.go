package logfmt

import (
	"strings"
	"testing"
)

// The one property that matters: a log record is ONE line, whatever arrived in
// it. Two of a job's fields come from outside -- the filename the client chose
// and the engine's stderr, which is the engine reacting to the client's
// document -- so "whatever arrived" includes anything the client felt like
// sending.

func TestJobLineIsAlwaysOneRecord(t *testing.T) {
	hostile := map[string]string{
		"a newline":           "before\nafter",
		"a carriage return":   "before\rafter",
		"CRLF":                "before\r\nafter",
		"a forged record":     "x\njob done job_id=00000000-0000-0000-0000-000000000000 status=done",
		"a quote":             `he said "hi"`,
		"an equals sign":      "a=b",
		"a tab":               "a\tb",
		"a NUL":               "a\x00b",
		"an escape character": "a\x1b[31mb",
		"a lone backslash":    `a\b`,
		// U+2028/U+2029 are line separators that some log viewers and JSON
		// parsers break on even though they are not \n or \r. The check
		// below originally looked only for \n and \r, so this case existed
		// and could not fail.
		"unicode line separator":      "a b",
		"unicode paragraph separator": "a b",
	}
	for name, value := range hostile {
		line := KV("job finished", "job_id", "abc", "filename", value)
		if strings.ContainsAny(line, "\n\r\u2028\u2029") {
			t.Errorf("%s: the record contains a line break, so it is not one record any more.\n"+
				"Anything reading these logs sees a line this service did not emit.\ngot: %q",
				name, line)
		}
		if !strings.HasPrefix(line, "job finished job_id=abc ") {
			t.Errorf("%s: the fields before the hostile one were disturbed: %q", name, line)
		}
	}
}

// The other direction, and it is not decoration: a formatter that quotes
// everything is safe and unreadable, and an unreadable log is one nobody
// greps. Ordinary values must stay bare.
func TestOrdinaryValuesAreNotQuoted(t *testing.T) {
	line := KV("job finished",
		"job_id", "0a001d7a-66fa-4e68-98dd-d2103a291e21",
		"engine", "tectonic",
		"status", "done",
		"duration_ms", "1234",
		"path", "uploads/abc/0-in.tex",
	)
	want := "job finished job_id=0a001d7a-66fa-4e68-98dd-d2103a291e21 engine=tectonic " +
		"status=done duration_ms=1234 path=uploads/abc/0-in.tex"
	if line != want {
		t.Errorf("ordinary values were mangled.\ngot:  %s\nwant: %s", line, want)
	}
}

// An empty value is rendered, not dropped: a key with nothing after it reads
// as a truncated record, and "nothing was captured here" is worth seeing.
func TestEmptyValueIsVisible(t *testing.T) {
	line := KV("job finished", "code", "", "next", "1")
	if !strings.Contains(line, `code="" next=1`) {
		t.Errorf("an empty value did not render as \"\": %s", line)
	}
}

// An odd number of key/value arguments is a caller bug. It must not produce a
// dangling key that reads as a field with an empty value -- that would hide
// the bug in something that looks like data.
func TestOddArgumentsDropTheDanglingKey(t *testing.T) {
	line := KV("job finished", "job_id", "abc", "orphan")
	if strings.Contains(line, "orphan") {
		t.Errorf("a key with no value was emitted: %q", line)
	}
}
