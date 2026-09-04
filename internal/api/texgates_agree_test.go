package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/errcode"
	"github.com/apaderin/octoconv/internal/worker"
)

// TEX-03 deliberately puts ONE rule in TWO places: a cheap gate in cmd/api that
// may abstain, and an authoritative one in cmd/tectonic-worker that may not.
// That is the shape this project has been burned by repeatedly, so the test
// holding the two together is not optional -- its absence would be a plan
// failure, not a gap.
//
// 64-01's three-way test holds the declared list equal to the bundle and to the
// documentation. This file holds the two READERS equal to each other: for one
// package name, the API's verdict and the worker's must be the same verdict,
// with the same code, and the same package named in the message.
//
// It lives in package api rather than in worker because only this package can
// drive handleCreateJob end to end through its own fakes; internal/worker does
// not import internal/api, so the reverse direction would have been the one
// with a cycle in it.

// texGateCase is one package name run through both gates.
type texGateCase struct {
	pkg string
	// wantRefused is derived from the ONE source both gates are supposed to
	// consult, not restated by hand -- a hand-written expectation here would be
	// a third opinion, and three opinions cannot disagree two at a time.
	wantRefused bool
}

// apiVerdict runs a .tex loading pkg through POST /v1/jobs and reports whether
// it was refused, with what code and message.
func apiVerdict(t *testing.T, pkg string) (refused bool, code, message string) {
	t.Helper()
	repo := &fakeRepo{}
	srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})

	body, ct := multipartBody(t, "paper.tex", "pdf", texDoc(pkg))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code == http.StatusAccepted {
		return false, "", ""
	}
	var resp struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %d body: %v (%s)", rec.Code, err, rec.Body.String())
	}
	return true, resp.Code, resp.Error
}

// workerVerdict is the OTHER gate's answer for the same document, reached the
// way TectonicConverter.Convert reaches it.
//
// The two paths are not interchangeable, which is the whole point of round 2's
// finding. For a package the bundle LACKS, the verdict is the engine's own
// output parsed back. For a package the bundle HAS and this service refuses
// anyway, the engine has no verdict to give -- it loads the package and
// succeeds -- so the worker must scan the source itself.
func workerVerdict(pkg string) (terminal bool, code, message string) {
	var err error
	if denied := convert.TeXDeniedInSource([]byte(texDoc(pkg))); denied != "" {
		err = fmt.Errorf("convert: %w", fmt.Errorf("tectonic: %w",
			convert.TeXUnsupportedPackageError(denied)))
	} else {
		err = fmt.Errorf("convert: %w", fmt.Errorf("tectonic: %w: %s",
			convert.TeXUnsupportedPackageError(pkg),
			"! ! LaTeX Error: File `"+pkg+".sty' not found.."))
	}
	code, message = worker.TectonicDescriptor.FailureCode(err)
	return worker.TectonicDescriptor.IsTerminal(err), code, message
}

// TestBothTeXGatesAgree is the drift test.
func TestBothTeXGatesAgree(t *testing.T) {
	cases := []texGateCase{
		// In the bundle AND declared.
		{pkg: "amsmath"},
		{pkg: "hyperref"},
		// In the bundle, NOT declared -- the twelve measured in 64-03. Both
		// gates must admit these, and the API's admission is the half that
		// would break if the gate were re-pointed at convert.TeXPackages.
		{pkg: "fontenc"},
		{pkg: "microtype"},
		{pkg: "tikz"},
		// Not in the bundle. Measured MISSING in the container.
		{pkg: "pgfplots", wantRefused: true},
		{pkg: "siunitx", wantRefused: true},
		{pkg: "minted", wantRefused: true},
		// IN the bundle and refused anyway (TEX-07). These are the cases this
		// test did not have when the deny list was added in round 1, and their
		// absence is why round 2 found the deny list enforced on one side only:
		// the API refused fontspec while the worker typeset it. All three names
		// are one package -- fontspec.sty loads fontspec-xetex.sty -- so a list
		// naming only the public one was a one-line bypass.
		{pkg: "fontspec", wantRefused: true},
		{pkg: "fontspec-xetex", wantRefused: true},
		{pkg: "fontspec-luatex", wantRefused: true},
	}

	for i := range cases {
		// Derived, not asserted: whatever convert.TeXPackageResolvable says is
		// what BOTH gates must do.
		cases[i].wantRefused = !convert.TeXPackageResolvable(cases[i].pkg)
	}

	refusedCount := 0
	for _, tc := range cases {
		t.Run(tc.pkg, func(t *testing.T) {
			refused, code, message := apiVerdict(t, tc.pkg)
			if refused != tc.wantRefused {
				t.Fatalf("API refused = %v, want %v (convert.TeXPackageResolvable(%q) = %v)",
					refused, tc.wantRefused, tc.pkg, convert.TeXPackageResolvable(tc.pkg))
			}
			if !refused {
				return
			}
			refusedCount++

			if code != errcode.UnsupportedPackage {
				t.Fatalf("API code = %q, want %q", code, errcode.UnsupportedPackage)
			}

			// The same package, arriving at the other gate because the API's
			// bounded scan did not see it -- which happens whenever the
			// preamble outruns the window, or the \usepackage is behind a
			// macro or an \input.
			terminal, wCode, wMessage := workerVerdict(tc.pkg)
			if !terminal {
				t.Errorf("worker classified %q as transient while the API refuses it outright -- the "+
					"same document would be retried three times and then fail anyway", tc.pkg)
			}
			if wCode != code {
				t.Errorf("the two gates disagree on the CODE for %q: API %q, worker %q. A client's "+
					"error code would then depend on where \\begin{document} falls in their own file",
					tc.pkg, code, wCode)
			}
			if message != wMessage {
				t.Errorf("the two gates disagree on the MESSAGE for %q:\n  API:    %q\n  worker: %q",
					tc.pkg, message, wMessage)
			}
		})
	}

	// Vacuity guard. If the bundle ever grew to contain all three refused
	// packages, every assertion above would pass while proving nothing about
	// refusal at all.
	if refusedCount == 0 {
		t.Fatal("no case was refused by either gate -- this test proved only that admission works")
	}
}

// TestBothTeXGatesReadTheSameSource is the structural half. The test above
// compares OUTCOMES, which two gates could match today while consulting two
// different lists that happen to agree; this asserts the sources themselves.
//
// convert.TeXPackageResolvable is the one authority. The API calls it directly.
// For a package the bundle LACKS, the worker does not call it at all -- it
// reads the ENGINE's verdict, which is the same bundle answering the same
// question at run time, and that is the stronger form: the worker cannot drift
// from the bundle because it IS the bundle.
//
// That sentence used to be the whole story, and round 1's deny list quietly
// falsified it: a DENIED package is in the bundle, so the engine's verdict is
// "loaded fine" and there is nothing to read back. The worker now scans for
// those itself (convert.TeXDeniedInSource), which is the one place it consults
// a list rather than the engine, and TestBothTeXGatesAgree covers all three
// denied names.
//
// What must not happen is either side quietly switching to convert.TeXPackages,
// the declared/documented set, which is measurably smaller.
func TestBothTeXGatesReadTheSameSource(t *testing.T) {
	// A package in the bundle but NOT declared. If either gate consulted the
	// declared set, this is the case that breaks -- and it is not exotic:
	// fontenc is in pandoc's default template and in the paper 64-04 ran.
	const pkg = "fontenc"
	if convert.TeXPackageSupported(pkg) {
		t.Fatalf("%s is now in the declared set; pick another bundle-only package or this test proves "+
			"nothing", pkg)
	}
	if !convert.TeXPackageResolvable(pkg) {
		t.Fatalf("%s is not in the admission set -- the bundle changed and 64-03's measurement needs "+
			"re-running", pkg)
	}

	if refused, code, _ := apiVerdict(t, pkg); refused {
		t.Fatalf("the API refused \\usepackage{%s} with code %q -- it is consulting the DECLARED set, "+
			"not the bundle, and would reject documents the worker typesets", pkg, code)
	}

	// The worker's half: it must classify by the sentinel, not by whether the
	// name happens to be in the declared set.
	err := convert.TeXUnsupportedPackageError(pkg)
	if !errors.Is(err, convert.ErrTeXUnsupportedPackage) {
		t.Fatal("the sentinel no longer round-trips")
	}
	if code, _ := worker.TectonicDescriptor.FailureCode(err); code != errcode.UnsupportedPackage {
		t.Fatalf("worker code for a sentinel-wrapped %q = %q, want %q -- the worker is deciding by "+
			"package identity rather than by the engine's verdict", pkg, code, errcode.UnsupportedPackage)
	}
}
