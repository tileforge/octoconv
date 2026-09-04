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
	//
	// Phase 57, D-08/D-09: Register now panics on a triple collision instead
	// of silently last-write-winning; cmd/av-worker's re-registration above
	// now goes through the new Registry.Replace, which performs this exact
	// overwrite deliberately and without panicking.
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
	//
	// Phase 57, D-08/D-09: Register now panics on a triple collision instead
	// of silently last-write-winning; cmd/ocr-worker's re-registration now
	// goes through the new Registry.Replace, which performs this exact
	// overwrite deliberately and without panicking.
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
	//
	// Phase 57, D-08/D-09: Register now panics on a triple collision instead
	// of silently last-write-winning; cmd/pandoc-worker's re-registration now
	// goes through the new Registry.Replace, which performs this exact
	// overwrite deliberately and without panicking.
	Default.Register(PandocConverter{})
	// Registered last (SANI-06, Phase 60). SanitizeConverter is the first
	// converter to declare an operation other than "convert", so its triples
	// live in a key space no existing registration can collide with: the
	// registry is keyed by (operation, from, to), and every pair here is an
	// identity pair under OperationSanitize. pdf->pdf under "sanitize" and
	// pdf->pdf under "convert" are different keys, and the second does not
	// exist. That is why this needs no disjointness test of the kind
	// TestPandocPairsDisjointFromOtherEngines exists for -- the separation is
	// structural rather than a property of the chosen format lists.
	Default.Register(SanitizeConverter{})
	// TectonicConverter is NOT registered here yet -- 64-04 does it, together
	// with the queue and the worker that consumes it. Registering it in 64-02
	// advertised latex -> pdf on GET /v1/formats with no queue behind it, which
	// is a job a client can create and nothing will ever run: the same defect
	// phase 60 shipped for sanitize. The converter type and its argv exist and
	// are tested; what is missing is the loop, and that is deliberate.
}
