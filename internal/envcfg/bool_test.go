package envcfg

import "testing"

func TestParseBool(t *testing.T) {
	tests := []struct {
		name           string
		raw            string
		def            bool
		wantValue      bool
		wantRecognized bool
	}{
		{"empty with default true", "", true, true, true},
		{"empty with default false", "", false, false, true},
		{"true", "true", false, true, true},
		{"false", "false", true, false, true},
		{"true with inline comment", "true   # for staging", false, true, true},
		{"false with inline comment", "false # off", true, false, true},
		{"true trailing space", "true ", false, true, true},
		{"true leading space", "  true", false, true, true},
		{"true anything is unrecognized", "true anything", false, false, false},
		{"TRUE is unrecognized", "TRUE", false, false, false},
		{"1 is unrecognized", "1", false, false, false},
		{"yes is unrecognized", "yes", false, false, false},
		{"True is unrecognized", "True", false, false, false},
		{"on is unrecognized", "on", false, false, false},
		{"only a comment strips to empty", "# only a comment", true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, gotRecognized := ParseBool(tt.raw, tt.def)
			if gotValue != tt.wantValue || gotRecognized != tt.wantRecognized {
				t.Errorf("ParseBool(%q, %v) = (%v, %v), want (%v, %v)",
					tt.raw, tt.def, gotValue, gotRecognized, tt.wantValue, tt.wantRecognized)
			}
		})
	}
}

func TestBoolReadsEnv(t *testing.T) {
	t.Run("var unset returns default with recognized true", func(t *testing.T) {
		value, recognized := Bool("ENVCFG_TEST_UNSET_VAR", true)
		if !value || !recognized {
			t.Errorf("Bool(unset, true) = (%v, %v), want (true, true)", value, recognized)
		}
	})

	t.Run("var set to true with inline comment", func(t *testing.T) {
		t.Setenv("ENVCFG_TEST_VAR", "true   # staging")
		value, recognized := Bool("ENVCFG_TEST_VAR", false)
		if !value || !recognized {
			t.Errorf("Bool(%q, false) = (%v, %v), want (true, true)", "true   # staging", value, recognized)
		}
	})

	t.Run("var set to TRUE is unrecognized", func(t *testing.T) {
		t.Setenv("ENVCFG_TEST_VAR", "TRUE")
		value, recognized := Bool("ENVCFG_TEST_VAR", false)
		if value != false || recognized {
			t.Errorf("Bool(%q, false) = (%v, %v), want (false, false)", "TRUE", value, recognized)
		}
	})
}
