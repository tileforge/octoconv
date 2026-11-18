package workerproto

import "testing"

func TestAssignmentParameters(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: ""},
		{name: "null", raw: "null", want: ""},
		{name: "object", raw: "{}", want: ""},
		{name: "whitespace", raw: "  {}  ", want: ""},
		{name: "page", raw: `{"page_size":"a4"}`, want: `{"page_size":"a4"}`},
		{name: "zero timecode", raw: `{"timecode":0}`, want: `{"timecode":0}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := assignmentParameters([]byte(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
	if _, err := assignmentParameters([]byte(`[]`)); err == nil {
		t.Fatal("array accepted")
	}
	if _, err := assignmentParameters([]byte(`"x"`)); err == nil {
		t.Fatal("string accepted")
	}
}

func TestPublishedParameterSchemas(t *testing.T) {
	cases := []struct {
		file string
		ok   map[string]any
		bad  map[string]any
	}{
		{
			file: "manifest-html-convert.json",
			ok:   map[string]any{"page_size": "a4", "margin_mm": float64(10), "landscape": true},
			bad:  map[string]any{"margin_mm": float64(51)},
		},
		{
			file: "manifest-audio-convert.json",
			ok:   map[string]any{"language": "ru", "translate": true},
			bad:  map[string]any{"language": "eng"},
		},
		{
			file: "manifest-av-convert.json",
			ok:   map[string]any{"timecode": float64(0), "resolution_height": float64(480), "codec": "h264"},
			bad:  map[string]any{"resolution_height": float64(240)},
		},
		{
			file: "manifest-ocr-convert.json",
			ok:   map[string]any{"language": "eng+rus"},
			bad:  map[string]any{"language": "deu"},
		},
		{
			file: "manifest-pandoc-convert.json",
			ok:   map[string]any{"standalone": true, "wrap": "preserve"},
			bad:  map[string]any{"wrap": "left"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			parsed := mustParseCadManifest(t, tc.file)
			if err := ValidateParameters(parsed.schema, map[string]any{}); err != nil {
				t.Fatalf("empty: %v", err)
			}
			if err := ValidateParameters(parsed.schema, tc.ok); err != nil {
				t.Fatalf("accepted instance: %v", err)
			}
			if err := ValidateParameters(parsed.schema, tc.bad); err == nil {
				t.Fatal("rejected instance was accepted")
			}
			if err := ValidateParameters(parsed.schema, map[string]any{"unknown": true}); err == nil {
				t.Fatal("unknown key was accepted")
			}
		})
	}
}
