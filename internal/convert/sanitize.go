package convert

// This file (sanitize.go): the shared shape of the sanitize operation --
// the dispatcher and the result type. Per-format work lives beside it
// (sanitizepdf.go, sanitizeooxml.go, sanitizeimage.go), mirroring the
// ocr.go / ocrpdf.go split.
//
// WHAT SANITIZE IS, AND WHAT IT IS NOT
//
// It is Content Disarm and Reconstruction: the enumerated carriers in
// findings.go are removed and their absence is then proven by an
// implementation that did not do the removing (Plan 60-04). It is NOT an
// antivirus, it does not decide whether anything was malicious, and it makes
// no claim about content outside those enumerated classes. See findings.go's
// header for why that restriction is mechanical rather than advisory.

import (
	"context"
	"fmt"
	"os"
)

// SanitizeResult is what one sanitize run reports about itself.
//
// Removed and LostCapabilities are two structurally separate lists on purpose
// (D-02). A client reading "this document lost its fillable form fields"
// inside a list of removed THREATS would reasonably conclude the form was an
// attack. It was not: it is the price of rebuilding the document through an
// interpreter that draws marks and has no concept of a form. Filing both under
// one heading tells a client something false about both.
//
// Verified is narrower than Removed and deliberately so: "we removed
// JavaScript" and "we confirmed no JavaScript is present in the output" are
// different claims, and SANI-04 requires the second. Plan 60-04 fills it in
// from the independent verifier; a class in Removed but absent from Verified
// means the removal ran and nothing checked it.
type SanitizeResult struct {
	// Removed lists the threat classes found and removed from this document.
	Removed []ThreatClass
	// LostCapabilities lists document features the sanitization method
	// destroys as a side effect -- fillable forms, bookmarks, tagged
	// accessibility structure. Not threats; costs.
	LostCapabilities []string
	// Verified lists the classes an independent implementation confirmed
	// absent from the OUTPUT bytes. Filled by Plan 60-04.
	Verified []ThreatClass
}

// SanitizeFormats is the closed set of source formats the sanitize operation
// accepts, mapped to the function that handles each. Output format always
// equals input format: sanitize is not a conversion, and a client asking for
// a safe copy of a PDF expects a PDF back.
//
// Populated per-format by the plans that implement each path, so an
// unimplemented format is a missing map entry rather than a stub that returns
// a plausible-looking empty result.
var sanitizeFormats = map[string]func(context.Context, string, string) (SanitizeResult, error){}

// SanitizeFile sanitizes inPath into outPath, dispatching on the NORMALIZED
// source format.
//
// The format is normalized through NormalizeFormat, the same entry point the
// registry uses, so "JPEG"/"jpeg"/"jpg" reach one handler rather than three
// map misses. An unknown format is an error, never a pass-through copy: a
// sanitizer that silently returns the input unchanged for a format it does not
// handle is the worst available outcome, because the job goes green and the
// client believes the file was disarmed.
func SanitizeFile(ctx context.Context, format, inPath, outPath string) (SanitizeResult, error) {
	norm := NormalizeFormat(format)
	fn, ok := sanitizeFormats[norm]
	if !ok {
		return SanitizeResult{}, fmt.Errorf("sanitize: unsupported format %q", format)
	}
	res, err := fn(ctx, inPath, outPath)
	if err != nil {
		return SanitizeResult{}, err
	}

	// Verification is INSIDE this function, not left to the caller, because a
	// caller that forgets it produces a file that went through an operation
	// named sanitize and was never checked -- and nothing downstream can tell
	// that file from a verified one. There is no skip flag for the same
	// reason (SANI-04, D-07).
	verified, verr := VerifySanitized(ctx, norm, outPath)
	if verr != nil {
		// Delete the output before returning. The requirement is that
		// nothing unverified reaches S3; the strongest form of that is that
		// unverified bytes stop existing, rather than existing next to a
		// promise that no one will upload them.
		_ = os.Remove(outPath)
		return SanitizeResult{}, verr
	}
	res.Verified = verified
	return res, nil
}

// SanitizeSupports reports whether format has a registered sanitize handler.
// Used by the registry wiring in Plan 60-06; kept here so the closed set has
// exactly one reader and one writer.
func SanitizeSupports(format string) bool {
	_, ok := sanitizeFormats[NormalizeFormat(format)]
	return ok
}
