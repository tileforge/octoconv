package convert

import "testing"

// TestAVAudioPairDisjointness proves AVConverter.Pairs() and
// AudioConverter.Pairs() share no (from, to) pair -- the regression guard
// Pitfall 7 (35-RESEARCH.md) and D-06 (35-CONTEXT.md) require.
//
// The hazard this test guards against: Registry.Register (convert.go:74-80)
// is a bare map assignment with NO collision check -- a later
// Default.Register(AVConverter{}) after AudioConverter would silently win
// any shared (from, to) pair, with no error, panic, or log. The only
// observable symptom would be a job silently routed to the wrong
// engine-class queue. THIS TEST IS THE ONLY GUARD; there is no runtime one.
//
// Why disjointness currently holds by construction: AVConverter's target
// formats are {mp4,webm,mp3,wav,m4a,jpg,png,webp} (av.go); AudioConverter's
// target formats are always {txt,srt,vtt,json} (whisper.go), UNCHANGED by
// D-04's expansion of audioSourceFormats. These two target sets are
// disjoint, so no (from, to) pair can collide even though both converters
// now share five SOURCE formats (mp4/mov/avi/mkv/webm, after D-04). A
// future edit that adds an overlapping TARGET format to either converter
// would silently break this invariant without this test.
// Pairs are compared in NORMALIZED form (NormalizeFormat on both From and To),
// because that is exactly the key Registry.Register indexes on (convert.go:78).
// Comparing raw pairs would miss an alias-level collision -- e.g. one converter
// claiming target "jpeg" and the other "jpg" map to the same registry slot even
// though the raw Pair values differ (35-SECURITY.md non-blocking observation 1).
// All current formats are already canonical, so this changes nothing today; it
// closes the gap against a future alias-form pair being added.
func TestAVAudioPairDisjointness(t *testing.T) {
	norm := func(p Pair) Pair {
		return Pair{From: NormalizeFormat(p.From), To: NormalizeFormat(p.To)}
	}
	avPairs := make([]Pair, 0)
	for _, p := range (AVConverter{}).Pairs() {
		avPairs = append(avPairs, norm(p))
	}
	audioPairs := make([]Pair, 0)
	for _, p := range (AudioConverter{}).Pairs() {
		audioPairs = append(audioPairs, norm(p))
	}

	for _, p := range avPairs {
		for _, q := range audioPairs {
			if p == q {
				t.Fatalf("normalized pair %+v registered by both AVConverter and AudioConverter, want disjoint", p)
			}
		}
	}

	// Second assertion: the union's length must equal the sum of the two
	// converters' individual pair counts -- catches an overlap by COUNT as
	// well as by identity, so a future change that introduces a duplicate
	// pair is caught even if the identity loop above were ever weakened.
	union := make(map[Pair]bool, len(avPairs)+len(audioPairs))
	for _, p := range avPairs {
		union[p] = true
	}
	for _, p := range audioPairs {
		union[p] = true
	}
	if want := len(avPairs) + len(audioPairs); len(union) != want {
		t.Fatalf("len(union of AVConverter.Pairs() and AudioConverter.Pairs()) = %d, want %d (len(avPairs)=%d + len(audioPairs)=%d) -- a collision was masked by set semantics",
			len(union), want, len(avPairs), len(audioPairs))
	}
}

// TestOCRPairsDisjointFromOtherEngines proves OCRConverter.Pairs() (ocr.go)
// shares no (from, to) pair with LibvipsConverter, AudioConverter, or
// AVConverter -- mirroring TestAVAudioPairDisjointness's rationale (OCRP-01,
// Phase 44, D-01). Registry.Register is still the same bare, uncollision-
// checked map assignment; this is the only guard against OCRConverter's
// converters.go init() registration silently stealing or losing a pair.
//
// Why disjointness holds by construction: OCRConverter's SOURCE formats are
// {jpg,png,tiff,webp,pdf} (ocr.go) with TARGET formats always {txt,hocr,
// alto}. LibvipsConverter's pairs are image->image (never txt/hocr/alto
// targets); AudioConverter's/AVConverter's SOURCE formats are audio/video
// containers, never jpg/png/tiff/webp/pdf -- so even though OCRConverter and
// AudioConverter both target "txt", no (from, to) key can collide because
// their FROM sets never intersect.
func TestOCRPairsDisjointFromOtherEngines(t *testing.T) {
	norm := func(p Pair) Pair {
		return Pair{From: NormalizeFormat(p.From), To: NormalizeFormat(p.To)}
	}
	normAll := func(pairs []Pair) []Pair {
		out := make([]Pair, 0, len(pairs))
		for _, p := range pairs {
			out = append(out, norm(p))
		}
		return out
	}

	ocrPairs := normAll((OCRConverter{}).Pairs())
	others := map[string][]Pair{
		"LibvipsConverter": normAll((LibvipsConverter{}).Pairs()),
		"AudioConverter":   normAll((AudioConverter{}).Pairs()),
		"AVConverter":      normAll((AVConverter{}).Pairs()),
	}

	for name, otherPairs := range others {
		for _, p := range ocrPairs {
			for _, q := range otherPairs {
				if p == q {
					t.Fatalf("normalized pair %+v registered by both OCRConverter and %s, want disjoint", p, name)
				}
			}
		}
		union := make(map[Pair]bool, len(ocrPairs)+len(otherPairs))
		for _, p := range ocrPairs {
			union[p] = true
		}
		for _, p := range otherPairs {
			union[p] = true
		}
		if want := len(ocrPairs) + len(otherPairs); len(union) != want {
			t.Fatalf("len(union of OCRConverter.Pairs() and %s.Pairs()) = %d, want %d -- a collision was masked by set semantics",
				name, len(union), want)
		}
	}
}

// TestPandocPairsDisjointFromOtherEngines proves PandocConverter.Pairs()
// (pandoc.go) shares no (from, to) pair with any other registered converter
// -- mirroring TestOCRPairsDisjointFromOtherEngines's rationale (PDCP-01,
// Phase 48, D-02, 48-CONTEXT.md). Registry.Register is still the same bare,
// uncollision-checked map assignment; this is the only guard against
// PandocConverter's converters.go init() registration silently stealing or
// losing a pair.
//
// Why disjointness holds by construction: PandocConverter's curated
// markup-family table (pandocPairTable, pandoc.go) deliberately excludes
// every ->pdf target (LibreOfficeConverter owns docx/odt/xlsx/ods/pptx/odp
// ->pdf, ChromiumConverter owns html->pdf) and every docx<->odt-family
// target (LibreOfficeConverter's crossPairs own those) -- so even though
// "md"/"html"/"docx" strings are shared ENDPOINTS with LibvipsConverter (N/A,
// image-only), LibreOfficeConverter, ChromiumConverter, AudioConverter, and
// AVConverter, no (from, to) KEY registered by PandocConverter can collide
// with one already claimed by another engine.
func TestPandocPairsDisjointFromOtherEngines(t *testing.T) {
	norm := func(p Pair) Pair {
		return Pair{From: NormalizeFormat(p.From), To: NormalizeFormat(p.To)}
	}
	normAll := func(pairs []Pair) []Pair {
		out := make([]Pair, 0, len(pairs))
		for _, p := range pairs {
			out = append(out, norm(p))
		}
		return out
	}

	pandocPairs := normAll((PandocConverter{}).Pairs())
	others := map[string][]Pair{
		"LibvipsConverter":     normAll((LibvipsConverter{}).Pairs()),
		"LibreOfficeConverter": normAll((LibreOfficeConverter{}).Pairs()),
		"ChromiumConverter":    normAll((ChromiumConverter{}).Pairs()),
		"AudioConverter":       normAll((AudioConverter{}).Pairs()),
		"AVConverter":          normAll((AVConverter{}).Pairs()),
		"OCRConverter":         normAll((OCRConverter{}).Pairs()),
	}

	for name, otherPairs := range others {
		for _, p := range pandocPairs {
			for _, q := range otherPairs {
				if p == q {
					t.Fatalf("normalized pair %+v registered by both PandocConverter and %s, want disjoint", p, name)
				}
			}
		}
		union := make(map[Pair]bool, len(pandocPairs)+len(otherPairs))
		for _, p := range pandocPairs {
			union[p] = true
		}
		for _, p := range otherPairs {
			union[p] = true
		}
		if want := len(pandocPairs) + len(otherPairs); len(union) != want {
			t.Fatalf("len(union of PandocConverter.Pairs() and %s.Pairs()) = %d, want %d -- a collision was masked by set semantics",
				name, len(union), want)
		}
	}
}
