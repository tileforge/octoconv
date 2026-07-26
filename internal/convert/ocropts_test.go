package convert

import (
	"strings"
	"testing"
)

func TestParseOCROpts(t *testing.T) {
	t.Run("empty object valid, zero opts", func(t *testing.T) {
		o, err := ParseOCROpts([]byte(`{}`))
		if err != nil {
			t.Fatalf("ParseOCROpts({}) unexpected error: %v", err)
		}
		if o != (OCROpts{}) {
			t.Errorf("ParseOCROpts({}) = %+v, want zero value", o)
		}
	})

	t.Run("empty input valid, zero opts", func(t *testing.T) {
		o, err := ParseOCROpts([]byte(``))
		if err != nil {
			t.Fatalf("ParseOCROpts('') unexpected error: %v", err)
		}
		if o != (OCROpts{}) {
			t.Errorf("ParseOCROpts('') = %+v, want zero value", o)
		}
	})

	t.Run("language eng accepted", func(t *testing.T) {
		o, err := ParseOCROpts([]byte(`{"language":"eng"}`))
		if err != nil {
			t.Fatalf("ParseOCROpts: unexpected error: %v", err)
		}
		if o.Language != "eng" {
			t.Errorf("ParseOCROpts = %+v, want language=eng", o)
		}
	})

	t.Run("language rus accepted", func(t *testing.T) {
		o, err := ParseOCROpts([]byte(`{"language":"rus"}`))
		if err != nil {
			t.Fatalf("ParseOCROpts: unexpected error: %v", err)
		}
		if o.Language != "rus" {
			t.Errorf("ParseOCROpts = %+v, want language=rus", o)
		}
	})

	t.Run("language eng+rus accepted (combinable)", func(t *testing.T) {
		o, err := ParseOCROpts([]byte(`{"language":"eng+rus"}`))
		if err != nil {
			t.Fatalf("ParseOCROpts: unexpected error: %v", err)
		}
		if o.Language != "eng+rus" {
			t.Errorf("ParseOCROpts = %+v, want language=eng+rus", o)
		}
	})

	t.Run("language rus+eng accepted (order-independent)", func(t *testing.T) {
		o, err := ParseOCROpts([]byte(`{"language":"rus+eng"}`))
		if err != nil {
			t.Fatalf("ParseOCROpts: unexpected error: %v", err)
		}
		if o.Language != "rus+eng" {
			t.Errorf("ParseOCROpts = %+v, want language=rus+eng", o)
		}
	})

	t.Run("language empty string valid, zero opts (absent == default)", func(t *testing.T) {
		o, err := ParseOCROpts([]byte(`{"language":""}`))
		if err != nil {
			t.Fatalf("ParseOCROpts: unexpected error: %v", err)
		}
		if o != (OCROpts{}) {
			t.Errorf("ParseOCROpts(language=\"\") = %+v, want zero value", o)
		}
	})

	t.Run("unlisted language chi_sim rejected", func(t *testing.T) {
		_, err := ParseOCROpts([]byte(`{"language":"chi_sim"}`))
		if err == nil {
			t.Fatal("ParseOCROpts with language=chi_sim = nil error, want error")
		}
		if !strings.Contains(err.Error(), "unsupported language") {
			t.Errorf("error = %q, want it to mention 'unsupported language'", err.Error())
		}
	})

	t.Run("combination with one unlisted component rejected entirely", func(t *testing.T) {
		_, err := ParseOCROpts([]byte(`{"language":"eng+chi_sim"}`))
		if err == nil {
			t.Fatal("ParseOCROpts with language=eng+chi_sim = nil error, want error")
		}
	})

	t.Run("language osd rejected (OSD deferred to v2, D-05a)", func(t *testing.T) {
		_, err := ParseOCROpts([]byte(`{"language":"osd"}`))
		if err == nil {
			t.Fatal("ParseOCROpts with language=osd = nil error, want error (OSD is deferred to v2)")
		}
	})

	t.Run("unknown field rejected", func(t *testing.T) {
		if _, err := ParseOCROpts([]byte(`{"language":"eng","psm":3}`)); err == nil {
			t.Error("ParseOCROpts with unknown field 'psm' = nil error, want error")
		}
	})

	t.Run("duplicate top-level key rejected", func(t *testing.T) {
		if _, err := ParseOCROpts([]byte(`{"language":"eng","language":"rus"}`)); err == nil {
			t.Error("ParseOCROpts with duplicate key = nil error, want error")
		}
	})

	t.Run("trailing bytes rejected", func(t *testing.T) {
		if _, err := ParseOCROpts([]byte(`{"language":"eng"}garbage`)); err == nil {
			t.Error("ParseOCROpts with trailing bytes = nil error, want error")
		}
	})

	t.Run("top-level null rejected", func(t *testing.T) {
		if _, err := ParseOCROpts([]byte(`null`)); err == nil {
			t.Error("ParseOCROpts with top-level null = nil error, want error")
		}
	})
}

func TestOCROptsFromMap(t *testing.T) {
	t.Run("nil map yields zero opts", func(t *testing.T) {
		o, err := OCROptsFromMap(nil)
		if err != nil {
			t.Fatalf("OCROptsFromMap(nil) unexpected error: %v", err)
		}
		if o != (OCROpts{}) {
			t.Errorf("OCROptsFromMap(nil) = %+v, want zero value", o)
		}
	})

	t.Run("empty map yields zero opts", func(t *testing.T) {
		o, err := OCROptsFromMap(map[string]any{})
		if err != nil {
			t.Fatalf("OCROptsFromMap({}) unexpected error: %v", err)
		}
		if o != (OCROpts{}) {
			t.Errorf("OCROptsFromMap({}) = %+v, want zero value", o)
		}
	})

	t.Run("round trip valid map eng+rus", func(t *testing.T) {
		m := map[string]any{"language": "eng+rus"}
		o, err := OCROptsFromMap(m)
		if err != nil {
			t.Fatalf("OCROptsFromMap: unexpected error: %v", err)
		}
		if o.Language != "eng+rus" {
			t.Errorf("OCROptsFromMap = %+v, want language=eng+rus", o)
		}
	})

	t.Run("corrupt persisted map applies same strictness (invalid language)", func(t *testing.T) {
		m := map[string]any{"language": "chi_sim"}
		if _, err := OCROptsFromMap(m); err == nil {
			t.Error("OCROptsFromMap with invalid language = nil error, want error")
		}
	})

	t.Run("unknown field in persisted map rejected", func(t *testing.T) {
		m := map[string]any{"language": "eng", "psm": 3}
		if _, err := OCROptsFromMap(m); err == nil {
			t.Error("OCROptsFromMap with unknown field = nil error, want error")
		}
	})
}

func TestOCRLanguageArgs(t *testing.T) {
	t.Run("language produces discrete -l argv element", func(t *testing.T) {
		args := ocrLanguageArgs(OCROpts{Language: "eng+rus"})
		if len(args) != 2 || args[0] != "-l" || args[1] != "eng+rus" {
			t.Errorf("ocrLanguageArgs = %v, want [\"-l\", \"eng+rus\"]", args)
		}
	})

	t.Run("empty language yields empty argv", func(t *testing.T) {
		args := ocrLanguageArgs(OCROpts{})
		if len(args) != 0 {
			t.Errorf("ocrLanguageArgs(zero opts) = %v, want empty slice", args)
		}
	})
}

// TestOCROptsInjection is the OCRO-01 required proof: client bytes carrying
// shell metacharacters or path-traversal-shaped values never reach
// tesseract's -l argv unvalidated.
//
// Part 1: ParseOCROpts/OCROptsFromMap reject every metacharacter/traversal
// payload via the closed ocrLanguageAllowlist -- none of these strings are
// in the allowlist, so they never pass validation regardless of content.
//
// Part 2: ocrLanguageArgs only ever emits an allowlist-validated Language
// as ONE discrete argv slice element (never a concatenated/interpolated
// string) -- an injection payload can never reach it because ParseOCROpts
// rejects it first.
func TestOCROptsInjection(t *testing.T) {
	payloads := []string{
		"eng; rm -rf /",
		"eng$(whoami)",
		"eng`whoami`",
		"../../etc/passwd",
		"--user-words=/etc/passwd",
	}

	t.Run("allowlist rejects every metacharacter/traversal payload via ParseOCROpts", func(t *testing.T) {
		for _, p := range payloads {
			raw := []byte(`{"language":"` + strings.ReplaceAll(p, `"`, `\"`) + `"}`)
			if _, err := ParseOCROpts(raw); err == nil {
				t.Errorf("ParseOCROpts(language=%q) = nil error, want rejection by ocrLanguageAllowlist", p)
			}
		}
	})

	t.Run("allowlist rejects every payload via OCROptsFromMap (read path)", func(t *testing.T) {
		for _, p := range payloads {
			m := map[string]any{"language": p}
			if _, err := OCROptsFromMap(m); err == nil {
				t.Errorf("OCROptsFromMap(language=%q) = nil error, want rejection by ocrLanguageAllowlist", p)
			}
		}
	})

	t.Run("ocrLanguageArgs only ever emits a discrete validated slice element", func(t *testing.T) {
		// A hand-constructed bypass value (never reachable via
		// ParseOCROpts/OCROptsFromMap) still cannot leak into a shell string:
		// ocrLanguageArgs only ever appends the field verbatim as ONE argv
		// slice element, and runCommand (exec.go) never invokes a shell.
		o := OCROpts{Language: "; rm -rf /"}
		args := ocrLanguageArgs(o)
		if len(args) != 2 || args[0] != "-l" || args[1] != o.Language {
			t.Fatalf("ocrLanguageArgs(%+v) = %v, want [\"-l\", %q] as a discrete slice element", o, args, o.Language)
		}
	})
}
