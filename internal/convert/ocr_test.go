package convert

import "testing"

func TestOCRConverterPairs(t *testing.T) {
	pairs := OCRConverter{}.Pairs()
	want := len(ocrSourceFormats) * len(ocrTargetFormats)
	if len(pairs) != want {
		t.Fatalf("Pairs() returned %d pairs, want %d", len(pairs), want)
	}
	mustHave := []Pair{
		{From: "pdf", To: "txt"},
		{From: "png", To: "hocr"},
		{From: "jpg", To: "alto"},
	}
	for _, wp := range mustHave {
		found := false
		for _, p := range pairs {
			if p == wp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Pairs() missing %+v", wp)
		}
	}
}

func TestOCRConverterEngine(t *testing.T) {
	if got := (OCRConverter{}).Engine(); got != EngineOCR {
		t.Fatalf("Engine() = %q, want %q", got, EngineOCR)
	}
}
