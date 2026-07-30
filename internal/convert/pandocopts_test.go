package convert

import "testing"

func TestParsePandocOpts(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    PandocOpts
		wantErr bool
	}{
		{
			name: "empty raw yields zero value",
			raw:  "",
			want: PandocOpts{},
		},
		{
			name: "empty object yields zero value",
			raw:  `{}`,
			want: PandocOpts{},
		},
		{
			name: "every field set",
			raw:  `{"standalone":true,"toc":true,"number_sections":true,"wrap":"none","section_divs":true}`,
			want: PandocOpts{
				Standalone:     true,
				Toc:            true,
				NumberSections: true,
				Wrap:           "none",
				SectionDivs:    true,
			},
		},
		{
			name:    "unknown key rejected",
			raw:     `{"pdf_engine":"xelatex"}`,
			wantErr: true,
		},
		{
			name:    "out-of-enum wrap rejected",
			raw:     `{"wrap":"tight"}`,
			wantErr: true,
		},
		{
			name:    "duplicate top-level key rejected",
			raw:     `{"standalone":true,"standalone":false}`,
			wantErr: true,
		},
		{
			name:    "trailing bytes after object rejected",
			raw:     `{"standalone":true}{}`,
			wantErr: true,
		},
		{
			name:    "top-level null rejected",
			raw:     `null`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePandocOpts([]byte(tc.raw))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParsePandocOpts(%q) = %+v, nil; want error", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePandocOpts(%q) unexpected error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("ParsePandocOpts(%q) = %+v; want %+v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestPandocOptsFromMap(t *testing.T) {
	t.Run("nil map yields zero value", func(t *testing.T) {
		got, err := PandocOptsFromMap(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != (PandocOpts{}) {
			t.Fatalf("got %+v; want zero value", got)
		}
	})

	t.Run("round-trips a persisted map through the same allowlist", func(t *testing.T) {
		m := map[string]any{
			"standalone": true,
			"wrap":       "preserve",
		}
		got, err := PandocOptsFromMap(m)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := PandocOpts{Standalone: true, Wrap: "preserve"}
		if got != want {
			t.Fatalf("got %+v; want %+v", got, want)
		}
	})

	t.Run("corrupt persisted wrap value rejected exactly as at parse time", func(t *testing.T) {
		m := map[string]any{"wrap": "smuggled"}
		if _, err := PandocOptsFromMap(m); err == nil {
			t.Fatalf("expected error for corrupt persisted wrap value, got nil")
		}
	})

	t.Run("corrupt persisted unknown key rejected exactly as at parse time", func(t *testing.T) {
		m := map[string]any{"pdf_engine": "xelatex"}
		if _, err := PandocOptsFromMap(m); err == nil {
			t.Fatalf("expected error for unknown persisted key, got nil")
		}
	})
}
