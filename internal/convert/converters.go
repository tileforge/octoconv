package convert

// init wires concrete converters into the Default registry. To add support for
// a new engine or format pair, register it here with a single line.
func init() {
	Default.Register(LibvipsConverter{})
	Default.Register(LibreOfficeConverter{})
	Default.Register(ChromiumConverter{})
	Default.Register(AudioConverter{})
	// Registered AFTER AudioConverter (D-08, Phase 35): Registry.Register is
	// a bare map assignment with silent last-write-wins on pair collision --
	// a later registration silently wins any shared (from, to) pair with no
	// error, panic, or log. This ordering is safe only because AVConverter's
	// target formats ({mp4,webm,mp3,wav,m4a,jpg,png,webp}) are disjoint from
	// AudioConverter's ({txt,srt,vtt,json}); the sole guard is
	// TestAVAudioPairDisjointness (pairs_disjoint_test.go) -- there is no
	// runtime check. Registering AVConverter without also wiring SniffVideo
	// into the upload detection chain (internal/api/handlers.go) in the same
	// change would ship an engine for formats (mkv/webm) the service cannot
	// recognize -- see that file for the paired change.
	//
	// This zero-value registration is a placeholder ceiling (4h/4320,
	// avMaxSourceDuration/avMaxSourceResolutionHeight): cmd/av-worker/main.go
	// re-registers a CONFIGURED AVConverter (MaxSourceDuration derived from
	// AV_MAX_DURATION_SECONDS) before it starts consuming tasks, using this
	// same documented last-write-wins semantics (Phase 36/D-09).
	Default.Register(AVConverter{})
	// Registered last (OCRP-01, Phase 44, D-01): OCRConverter's SOURCE set
	// ({jpg,png,tiff,webp,pdf}) is disjoint from every other converter's own
	// source set (LibvipsConverter's images convert to OTHER images, never
	// to txt/hocr/alto; AudioConverter's/AVConverter's sources are audio/
	// video containers, never jpg/png/tiff/webp/pdf) -- so no (from, to) Pair
	// key registered here can collide with an existing one, even though the
	// "txt" target string is shared with AudioConverter. The sole guard is
	// TestOCRPairsDisjointFromOtherEngines (pairs_disjoint_test.go, mirrors
	// the AV/Audio disjointness test above) -- there is no runtime check.
	// This zero-value registration is a placeholder ceiling
	// (MaxImagePixels/MaxPages, mirrors AVConverter's own zero-value
	// contract above): a future cmd/ocr-worker/main.go (Plan 02/03) may
	// re-register a CONFIGURED OCRConverter before it starts consuming
	// tasks, using this same documented last-write-wins semantics.
	Default.Register(OCRConverter{})
	// Registered last (PDCP-01, Phase 48, D-02, 48-CONTEXT.md): PandocConverter's
	// curated markup-family pairs (pandocPairTable, pandoc.go) are disjoint from
	// every other engine's own pairs -- it deliberately excludes every ->pdf
	// target (document owns docx->pdf, chromium owns html->pdf) and every
	// docx->odt-family target (LibreOffice's crossPairs own those), so no
	// (from, to) key registered here can collide with an existing one, even
	// though "md"/"html"/"docx" strings are shared as ENDPOINTS with other
	// converters. The sole guard is TestPandocPairsDisjointFromOtherEngines
	// (pairs_disjoint_test.go, mirrors the OCR/AV/Audio disjointness tests
	// above) -- there is no runtime check; Registry.Register is a bare map
	// assignment with silent last-write-wins on a duplicate (source, target)
	// pair. This zero-value registration is an uncapped placeholder
	// (MaxInputBytes, mirrors OCRConverter's/AVConverter's own zero-value
	// contract above): cmd/pandoc-worker (Phase 49) re-registers a CONFIGURED
	// PandocConverter before it starts consuming tasks, using this same
	// documented last-write-wins semantics.
	Default.Register(PandocConverter{})
}
