// Package logfmt renders one log record as key=value pairs, escaping anything
// that would end the record early.
//
// Its own package because BOTH sides need it and neither may import the other:
// internal/api logs the admission record that joins the HTTP request id to the
// job id, and internal/worker logs everything about that job afterwards. A
// copy in each would be two escapings to keep in step, which is the thing the
// escaping exists to avoid.
package logfmt

import (
	"strconv"
	"strings"
)

// The contract, and why it is worth a package:
//
// # WHY THIS EXISTS RATHER THAN log.Printf AT EACH SITE
//
// Two of the values in a job's log line come from OUTSIDE: the filename the
// client chose, and the engine's own stderr — which is the engine's reaction
// to a document the client wrote. A newline inside either one SPLITS THE LOG
// RECORD, and what follows the split is a line the client composed. Anything
// reading these logs — a human grepping, or a shipper parsing key=value — then
// sees a record that no part of this service emitted.
//
// That is the same defect class phase 64 kept finding in other places, so the
// escaping lives in one function that every call site must go through, rather
// than in a convention every call site must remember.
//
// # WHY key=value AND NOT SOMETHING STRUCTURED
//
// internal/api already logs in exactly this shape (client_id=… reason=…).
// CLAUDE.md says internal/* does not log at all; that convention holds
// everywhere except there, where it was broken deliberately. Matching the
// existing exception is better than inventing a third style, and switching the
// whole service to slog is a decision with its own blast radius, not a
// side effect of wanting job ids in the log.
// KV builds one record: an event name followed by key=value pairs.
func KV(event string, kv ...string) string {
	var b strings.Builder
	b.WriteString(event)
	for i := 0; i+1 < len(kv); i += 2 {
		b.WriteByte(' ')
		b.WriteString(kv[i])
		b.WriteByte('=')
		b.WriteString(value(kv[i+1]))
	}
	return b.String()
}

// logValue renders one value so that it cannot end the record early.
//
// Quoted whenever it is not a plain token, and strconv.Quote is what does the
// escaping: it turns a newline into the two characters \ and n, a quote into
// \", and any other control byte into an escape — so the record stays one
// line whatever arrived.
//
// An empty value is rendered as "" rather than omitted: a key with nothing
// after it reads as a truncated record, and "nothing was captured" is itself
// worth seeing.
func value(s string) string {
	if s == "" {
		return `""`
	}
	if needsQuoting(s) {
		return strconv.Quote(s)
	}
	return s
}

// needsQuoting reports whether s contains anything that would make the record
// ambiguous: whitespace of any kind (including the newline that would split
// it), a quote, an equals sign, or a control byte.
//
// Deliberately an ALLOWLIST of safe bytes rather than a denylist of dangerous
// ones. A denylist has to enumerate every byte that could hurt, and the ones
// nobody thought of are exactly the ones that arrive from a client.
func needsQuoting(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		safe := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '/' || c == ':' || c == '+'
		if !safe {
			return true
		}
	}
	return false
}

// MS renders a duration in whole milliseconds for a log field.
func MS(d int64) string { return strconv.FormatInt(d, 10) }
