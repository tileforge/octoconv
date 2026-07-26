// Package convert (this file): ocrprogress.go implements a page-based
// progress percent + seconds/page ETA estimator for the streaming PDF OCR
// path (OCR-04, 43-CONTEXT.md). This is a NEW, page-shaped estimator,
// deliberately independent of progress.go's ffmpeg tick-based percent/ETA
// helpers: those parse a text stream of ffmpeg -progress "key=value"
// blocks, but OCR has no such stream -- its own progress signal is simply
// "one more page finished," so this estimator's currency is a
// completed-page count, never a parsed ffmpeg field. ocrpdf.go's streaming
// PDF loop (ConvertWithProgress, Plan 04 Task 3) is this estimator's only
// caller.
package convert

import "time"

// pctForPage converts a pages_done/total_pages pair into a clamped [0,100]
// percent -- mirrors progress.go's own duration-based percent helper's
// null-denominator discipline but PAGE-shaped rather than duration-shaped:
// ok is false when total <= 0, so a caller never divides by zero or reports
// a percent derived from an unknown page count. done is clamped into
// [0,total] before the division, so an out-of-range caller (done>total, or
// a negative done) can never yield a percent outside [0,100].
func pctForPage(done, total int) (pct int, ok bool) {
	if total <= 0 {
		return 0, false
	}
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}
	return done * 100 / total, true
}

// ocrPageProgress accumulates the streaming PDF loop's page-completion
// signal into a page-based ProgressUpdate stream: pagesDone/totalPages
// drive Pct (pctForPage), and elapsedTotal/pagesDone -- a plain running
// average over every completed page, deliberately NOT an EMA -- drives the
// ETA. recordPage takes each page's OWN elapsed processing duration as an
// explicit parameter, never an injected clock, so the whole estimator
// stays a pure, deterministic, unit-testable value type with no time.Now()
// call anywhere in this file.
type ocrPageProgress struct {
	totalPages   int
	pagesDone    int
	elapsedTotal time.Duration
}

// newOCRPageProgress constructs an estimator for a PDF whose page-count cap
// guard has already resolved totalPages (ocrPDFGuardStage, ocrpdf.go) --
// called once, before the per-page loop's first recordPage call.
func newOCRPageProgress(totalPages int) *ocrPageProgress {
	return &ocrPageProgress{totalPages: totalPages}
}

// recordPage folds one more completed page (and its elapsed processing
// time) into the estimator and returns the composed ProgressUpdate the PDF
// loop forwards to sink.Update. ETA stays nil until this has been called at
// least once -- the page-shaped analog of the ffmpeg estimator's warm-up
// window (progress.go), except a single completed page is already enough
// to derive a first estimate here: a page completion IS the OCR-native
// progress signal, unlike ffmpeg's noisy per-tick rate samples that need
// several ticks to smooth out.
func (p *ocrPageProgress) recordPage(elapsed time.Duration) ProgressUpdate {
	p.pagesDone++
	p.elapsedTotal += elapsed
	pct, _ := pctForPage(p.pagesDone, p.totalPages)
	return ProgressUpdate{Pct: pct, ETA: p.etaSeconds()}
}

// etaSeconds derives the remaining-seconds ETA as remaining_pages times the
// plain running average of seconds/page observed so far, or nil before at
// least one page has completed -- mirrors ProgressUpdate's own "nil until
// derivable" contract (progress.go) without reusing any of progress.go's
// ffmpeg-specific machinery.
func (p *ocrPageProgress) etaSeconds() *int {
	if p.pagesDone <= 0 {
		return nil
	}
	avgSecondsPerPage := p.elapsedTotal.Seconds() / float64(p.pagesDone)
	remaining := p.totalPages - p.pagesDone
	if remaining < 0 {
		remaining = 0
	}
	eta := int(avgSecondsPerPage * float64(remaining))
	if eta < 0 {
		eta = 0
	}
	return &eta
}
