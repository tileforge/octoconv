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
