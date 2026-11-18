package convert

import "testing"

func TestChromiumConverterPairs(t *testing.T) {
	got := ChromiumConverter{}.Pairs()
	want := []Pair{{From: "html", To: "pdf"}}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("Pairs() = %+v, want %+v", got, want)
	}
}

func TestChromiumConverterEngine(t *testing.T) {
	if got := (ChromiumConverter{}).Engine(); got != EngineHTML {
		t.Errorf("Engine() = %q, want %q", got, EngineHTML)
	}
}

func TestChromiumRegisteredInDefault(t *testing.T) {
	engine, ok := Default.EngineFor("html", "pdf")
	if !ok {
		t.Fatal("EngineFor(html, pdf) ok=false, want true after ChromiumConverter registration")
	}
	if engine != EngineHTML {
		t.Errorf("EngineFor(html, pdf) engine = %q, want %q", engine, EngineHTML)
	}
}

func TestChromiumConvertRefuses(t *testing.T) {
	err := ChromiumConverter{}.Convert(t.Context(), "in.html", "out.pdf", nil)
	if err == nil || err.Error() != "chromium: the in-repo engine is retired" {
		t.Fatalf("Convert() = %v", err)
	}
}
