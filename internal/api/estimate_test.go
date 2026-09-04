package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/errcode"
	"github.com/apaderin/octoconv/internal/jobs"
)

// fakeEngineQuotaRepo embeds fakeRepo (handlers_test.go) and overrides
// PreAdmit with a per-engine verdict table plus a per-engine call counter --
// exactly the shape TestAvailableOperationsCallsPreAdmitOncePerDistinctEngine
// needs to prove the dedup property, without touching fakeRepo's own
// counting fields (preAdmitCalls stays meaningful for every pre-existing
// handlers_test.go assertion).
type fakeEngineQuotaRepo struct {
	*fakeRepo
	verdicts map[string]error
	calls    map[string]int
}

func newFakeEngineQuotaRepo() *fakeEngineQuotaRepo {
	return &fakeEngineQuotaRepo{
		fakeRepo: &fakeRepo{},
		verdicts: map[string]error{},
		calls:    map[string]int{},
	}
}

func (f *fakeEngineQuotaRepo) PreAdmit(_ context.Context, _ uuid.UUID, engine string, _ int64) error {
	f.calls[engine]++
	return f.verdicts[engine]
}

// TestAvailableOperations proves the basic shape: for a format with at
// least one registered triple, availableOperations returns one offer per
// (operation, target, engine), each carrying a positive
// estimated_max_seconds, a declared estimate_source, and an allowed quota
// verdict when PreAdmit reports no error.
func TestAvailableOperations(t *testing.T) {
	repo := newFakeEngineQuotaRepo()
	srv, resolver := newTestServer(repo, &fakeStorage{}, &fakeQueue{})
	client, err := resolver.ResolveClient(context.Background(), testClientKey)
	if err != nil {
		t.Fatalf("ResolveClient: %v", err)
	}

	want := convert.Default.OperationClasses()
	var wantCount int
	for _, byEngine := range want {
		for _, pairs := range byEngine {
			for _, p := range pairs {
				if p.From == "png" {
					wantCount++
				}
			}
		}
	}
	if wantCount == 0 {
		t.Fatalf("test assumption broken: no registered triple has png as source")
	}

	offers := srv.availableOperations(context.Background(), client.ID, "png", 1024)
	if len(offers) != wantCount {
		t.Fatalf("len(offers) = %d, want %d", len(offers), wantCount)
	}
	for _, o := range offers {
		if o.Operation == "" || o.Target == "" || o.Engine == "" {
			t.Errorf("offer with an empty field: %+v", o)
		}
		if o.EstimatedMaxSeconds <= 0 {
			t.Errorf("offer %+v: EstimatedMaxSeconds <= 0", o)
		}
		if o.EstimateSource != EstimateSourceMeasured && o.EstimateSource != EstimateSourceUnmeasured {
			t.Errorf("offer %+v: unrecognized EstimateSource", o)
		}
		if o.Quota.Allowed == nil || !*o.Quota.Allowed {
			t.Errorf("offer %+v: expected Quota.Allowed == true", o)
		}
		if o.Quota.Reason != "" {
			t.Errorf("offer %+v: expected empty Quota.Reason on allowed verdict", o)
		}
	}
}

// TestAvailableOperationsAgreesWithCreateJobEngineSizeGate is PR9R22
// (estimate.go:265), verbatim: "When an inspected file fits
// INSPECT_MAX_UPLOAD_BYTES but exceeds its selected engine's s.maxEngineBytes
// limit, this unconditionally adds the conversion to operations and can report
// quota.allowed: true, although handleCreateJob deterministically rejects the
// same file at its engine-size gate."
//
// The test is deliberately written against the GOAL rather than the field:
// what matters is not that an `available` flag exists, but that /v1/inspect and
// POST /v1/jobs cannot disagree about the same file. So it exercises both paths
// on ONE server with ONE size and requires the offer's verdict and the eventual
// HTTP status to match, carrying the same machine-readable code.
func TestAvailableOperationsAgreesWithCreateJobEngineSizeGate(t *testing.T) {
	const engineLimit = 100

	repo := newFakeEngineQuotaRepo()
	resolver := newFakeResolver()
	srv := NewServer(repo, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
		MaxUploadBytes: 10 << 20,
		MaxEngineBytes: map[string]int64{convert.EngineImage: engineLimit},
	})
	client, err := resolver.ResolveClient(context.Background(), testClientKey)
	if err != nil {
		t.Fatalf("ResolveClient: %v", err)
	}

	// The inspect side, for a file five times the engine's ceiling.
	oversized := paddedPNGFixture(engineLimit * 5)
	offers := srv.availableOperations(context.Background(), client.ID, "png", int64(len(oversized)))
	if len(offers) == 0 {
		t.Fatal("no offers for png: test assumption broken")
	}
	sawImageEngine := false
	for _, o := range offers {
		if o.Engine != convert.EngineImage {
			continue
		}
		sawImageEngine = true
		if o.Available {
			t.Errorf("offer %+v: Available = true for a file of %d bytes against an engine ceiling of %d -- POST /v1/jobs rejects it deterministically, so inspect must not call it available", o, len(oversized), engineLimit)
		}
		if o.UnavailableReason != errcode.PayloadTooLarge {
			t.Errorf("offer %+v: UnavailableReason = %q, want %q -- the offer and the eventual 413 must speak one vocabulary", o, o.UnavailableReason, errcode.PayloadTooLarge)
		}
		if o.MaxBytes != engineLimit {
			t.Errorf("offer %+v: MaxBytes = %d, want %d -- a client that knows the ceiling can split its input instead of guessing", o, o.MaxBytes, engineLimit)
		}
		// The quota verdict is a SEPARATE axis and must not be overwritten by
		// the size gate: this client is within quota, and saying otherwise
		// would send it looking for a quota problem it does not have.
		if o.Quota.Allowed == nil || !*o.Quota.Allowed {
			t.Errorf("offer %+v: Quota.Allowed must stay true -- deterministic unavailability is not a quota fact", o)
		}
	}
	if !sawImageEngine {
		t.Fatal("no png offer served by the image engine: test assumption broken")
	}

	// The create-job side, same server, same bytes: the offer's verdict has to
	// be what actually happens.
	body, ct := multipartBody(t, "in.png", "webp", oversized)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("POST /v1/jobs status = %d, want 413 -- the whole point of the offer's available:false is that this request is deterministically refused; body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec.Body.Bytes(), errcode.PayloadTooLarge)
}

// TestAvailableOperationsEngineSizeBoundaryMatchesCreateJob pins the boundary
// in the same direction handleCreateJob's own check pins it: the gate is `>`,
// so a file exactly AT the ceiling is accepted by both paths. An off-by-one
// here would make inspect refuse work the service would actually do.
func TestAvailableOperationsEngineSizeBoundaryMatchesCreateJob(t *testing.T) {
	const engineLimit = 4096

	repo := newFakeEngineQuotaRepo()
	resolver := newFakeResolver()
	srv := NewServer(repo, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
		MaxUploadBytes: 10 << 20,
		MaxEngineBytes: map[string]int64{convert.EngineImage: engineLimit},
	})
	client, _ := resolver.ResolveClient(context.Background(), testClientKey)

	for _, tc := range []struct {
		name          string
		size          int64
		wantAvailable bool
	}{
		{"one byte below the ceiling", engineLimit - 1, true},
		{"exactly at the ceiling", engineLimit, true},
		{"one byte above the ceiling", engineLimit + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, o := range srv.availableOperations(context.Background(), client.ID, "png", tc.size) {
				if o.Engine != convert.EngineImage {
					continue
				}
				if o.Available != tc.wantAvailable {
					t.Errorf("size %d: Available = %v, want %v (engine ceiling %d, gate is `>`)", tc.size, o.Available, tc.wantAvailable, engineLimit)
				}
			}
		})
	}
}

// TestAvailableOperationsUngatedEngineStaysAvailable pins the other half of
// s.maxEngineBytes' own documented behavior -- "the map gates known engines, it
// does not allowlist": an engine absent from the map has no ceiling to exceed,
// so its offers stay available and publish no max_bytes rather than defaulting
// to unavailable.
func TestAvailableOperationsUngatedEngineStaysAvailable(t *testing.T) {
	repo := newFakeEngineQuotaRepo()
	resolver := newFakeResolver()
	srv := NewServer(repo, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
		MaxUploadBytes: 10 << 20,
		MaxEngineBytes: map[string]int64{}, // gates nothing
	})
	client, _ := resolver.ResolveClient(context.Background(), testClientKey)

	offers := srv.availableOperations(context.Background(), client.ID, "png", 1<<30)
	if len(offers) == 0 {
		t.Fatal("no offers for png: test assumption broken")
	}
	for _, o := range offers {
		if !o.Available {
			t.Errorf("offer %+v: Available = false for an engine the map does not gate", o)
		}
		if o.UnavailableReason != "" {
			t.Errorf("offer %+v: UnavailableReason = %q, want empty", o, o.UnavailableReason)
		}
		if o.MaxBytes != 0 {
			t.Errorf("offer %+v: MaxBytes = %d, want 0 (absent) for an ungated engine", o, o.MaxBytes)
		}
	}
}

// TestAvailableOperationsEmptyForUnregisteredFormat proves a format with no
// registered triple reports an empty, non-nil operations list.
func TestAvailableOperationsEmptyForUnregisteredFormat(t *testing.T) {
	repo := newFakeEngineQuotaRepo()
	srv, resolver := newTestServer(repo, &fakeStorage{}, &fakeQueue{})
	client, _ := resolver.ResolveClient(context.Background(), testClientKey)

	offers := srv.availableOperations(context.Background(), client.ID, "no-such-format-xyz", 1024)
	if offers == nil {
		t.Fatalf("availableOperations returned nil, want non-nil empty slice")
	}
	if len(offers) != 0 {
		t.Fatalf("len(offers) = %d, want 0", len(offers))
	}
}

// TestAvailableOperationsEmptyForBlankDetected proves an empty detected
// format (D-03's unrecognized-content case) also reports an empty,
// non-nil operations list, without calling PreAdmit at all.
func TestAvailableOperationsEmptyForBlankDetected(t *testing.T) {
	repo := newFakeEngineQuotaRepo()
	srv, resolver := newTestServer(repo, &fakeStorage{}, &fakeQueue{})
	client, _ := resolver.ResolveClient(context.Background(), testClientKey)

	offers := srv.availableOperations(context.Background(), client.ID, "", 1024)
	if offers == nil || len(offers) != 0 {
		t.Fatalf("availableOperations(\"\") = %+v, want empty non-nil slice", offers)
	}
	if len(repo.calls) != 0 {
		t.Fatalf("PreAdmit calls = %v, want none for a blank detected format", repo.calls)
	}
}

// TestAvailableOperationsCallsPreAdmitOncePerDistinctEngine is the plan's
// central dedup proof: png is the source of triples under at least two
// distinct engines (image via LibvipsConverter, ocr via OCRConverter) --
// PreAdmit must be called exactly once per distinct engine, never once per
// (operation, target) pair.
func TestAvailableOperationsCallsPreAdmitOncePerDistinctEngine(t *testing.T) {
	repo := newFakeEngineQuotaRepo()
	srv, resolver := newTestServer(repo, &fakeStorage{}, &fakeQueue{})
	client, _ := resolver.ResolveClient(context.Background(), testClientKey)

	wantEngines := map[string]bool{}
	for _, byEngine := range convert.Default.OperationClasses() {
		for engine, pairs := range byEngine {
			for _, p := range pairs {
				if p.From == "png" {
					wantEngines[engine] = true
				}
			}
		}
	}
	if len(wantEngines) < 2 {
		t.Fatalf("test assumption broken: expected png to be a source under >= 2 distinct engines, got %v", wantEngines)
	}

	_ = srv.availableOperations(context.Background(), client.ID, "png", 1024)

	if len(repo.calls) != len(wantEngines) {
		t.Fatalf("PreAdmit called for %d distinct engines %v, want exactly %d (%v)", len(repo.calls), repo.calls, len(wantEngines), wantEngines)
	}
	for engine, n := range repo.calls {
		if n != 1 {
			t.Errorf("engine %s: PreAdmit called %d times, want exactly 1", engine, n)
		}
		if !wantEngines[engine] {
			t.Errorf("PreAdmit called for unexpected engine %s", engine)
		}
	}
}

// TestQuotaUnknownIsNotAllowed proves незнание не разрешает (D-12): a
// non-quota PreAdmit error must report Quota.Allowed == nil (JSON null) and
// Quota.Reason == "unknown" -- NEVER true.
func TestQuotaUnknownIsNotAllowed(t *testing.T) {
	repo := newFakeEngineQuotaRepo()
	repo.verdicts[convert.EngineImage] = context.DeadlineExceeded // any non-quota error

	offer := quotaOfferFromPreAdmitError(repo.verdicts[convert.EngineImage])
	if offer.Allowed != nil {
		t.Fatalf("Allowed = %v, want nil", *offer.Allowed)
	}
	if offer.Reason != "unknown" {
		t.Fatalf("Reason = %q, want %q", offer.Reason, "unknown")
	}
}

// TestQuotaOfferFromPreAdmitError_RecognizedQuotaError proves a recognized
// quota error reports allowed=false with the machine-readable reason, never
// unknown.
func TestQuotaOfferFromPreAdmitError_RecognizedQuotaError(t *testing.T) {
	offer := quotaOfferFromPreAdmitError(jobs.ErrConcurrencyLimit)
	if offer.Allowed == nil || *offer.Allowed {
		t.Fatalf("Allowed = %v, want false", offer.Allowed)
	}
	if offer.Reason != jobs.QuotaReasonConcurrency {
		t.Fatalf("Reason = %q, want %q", offer.Reason, jobs.QuotaReasonConcurrency)
	}
}

// TestEveryEngineHasEstimateSource proves engineEstimateSource declares a
// provenance for every engine convert.AllEngines() names -- no engine is
// silently left without a declared source.
func TestEveryEngineHasEstimateSource(t *testing.T) {
	for _, engine := range convert.AllEngines() {
		if _, ok := engineEstimateSource[engine]; !ok {
			t.Errorf("engine %s has no declared EstimateSource", engine)
		}
	}
	if len(engineEstimateSource) != len(convert.AllEngines()) {
		t.Errorf("engineEstimateSource has %d entries, convert.AllEngines() has %d -- must match exactly", len(engineEstimateSource), len(convert.AllEngines()))
	}
}

// envExampleEngineVar names, for each engine, the .env.example var this
// file's engineEstimateSource entry must be verified against.
var envExampleEngineVar = map[string]string{
	convert.EngineImage:    "ENGINE_TIMEOUT",
	convert.EngineDocument: "DOCUMENT_ENGINE_TIMEOUT",
	convert.EngineHTML:     "HTML_ENGINE_TIMEOUT",
	convert.EngineAudio:    "AUDIO_ENGINE_TIMEOUT",
	convert.EngineAV:       "AV_ENGINE_TIMEOUT",
	convert.EngineOCR:      "OCR_ENGINE_TIMEOUT",
	convert.EnginePandoc:   "PANDOC_ENGINE_TIMEOUT",
	convert.EngineProbe:    "PROBE_ENGINE_TIMEOUT",
	// The ninth engine joins this map on the day it is declared, not on the
	// day it is measured. An engine left out of it is an engine whose
	// .env.example provenance nothing compares against -- and the check below
	// would go on reporting success for the eight it does cover, which is the
	// shape of "a scan of nothing is clean".
	convert.EngineTectonic: "TECTONIC_ENGINE_TIMEOUT",
}

// envExampleClaimsMeasured decides what .env.example's own prose says about
// varName's provenance.
//
// Two substrings, read at two different SCOPES, and the difference is the
// whole content of this function:
//
//   - the placeholder disclaimer is read from the var's OWN assignment line.
//     Tectonic is why it is needed: its block says the number is a PLACEHOLDER
//     and that 64-05 MEASURES it, an honest disclaimer that a bare "measur"
//     test reads as the exact opposite of what it says.
//   - the measurement claim is read from the whole paragraph, because that is
//     where audio/av/ocr/pandoc/probe write their derivations.
//
// Reading the disclaimer at block scope instead was tried and was WRONG: av's
// and pandoc's paragraphs both contain the word "placeholder" describing a
// HISTORICAL one that a measurement replaced (14400s -> 90s, 90s -> 65s), and
// scoping the check to the block reported both engines as unmeasured. A
// paragraph may discuss a placeholder it no longer uses; a var's own line
// states what the var IS.
func envExampleClaimsMeasured(block, varName string) bool {
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, varName+"=") {
			continue
		}
		own := strings.ToLower(trimmed)
		if strings.Contains(own, "placeholder") || strings.Contains(own, "not measured") {
			return false
		}
	}
	return strings.Contains(strings.ToLower(block), "measur")
}

// envExampleBlockContaining returns the blank-line-delimited paragraph of
// .env.example that contains varName's own assignment line -- .env.example's
// measurement provenance for audio/av/ocr/pandoc lives in a MULTI-LINE block
// comment ABOVE the var's own line, not always in a trailing inline comment
// ON that line (verified by direct inspection of the real file), so the
// mechanical check must look at the whole paragraph, not just one line.
func envExampleBlockContaining(content, varName string) (string, bool) {
	blocks := strings.Split(content, "\n\n")
	for _, block := range blocks {
		for _, line := range strings.Split(block, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, varName+"=") {
				return block, true
			}
		}
	}
	return "", false
}

// TestEstimateSourceMatchesEnvExampleProvenance mechanically re-verifies
// every engineEstimateSource entry against .env.example's OWN comments: a
// paragraph containing a case-insensitive "measur" substring (matching
// "measured", "RTF-measured", "MEASURED", and script names like
// "av-rtf-measure.sh") means measured; its absence means unmeasured. This
// is the sync check the plan requires -- a future .env.example edit that
// changes an engine's provenance without updating engineEstimateSource to
// match will fail this test, not silently drift.
// TestEstimateSourceForMeasuredEngineNotOverridden proves a measured
// engine's provenance stays "measured" when its configured value equals
// EXACTLY the documented run's own figure.
func TestEstimateSourceForMeasuredEngineNotOverridden(t *testing.T) {
	got := estimateSourceFor(convert.EngineAudio, engineMeasuredDefaultSeconds[convert.EngineAudio])
	if got != EstimateSourceMeasured {
		t.Fatalf("estimateSourceFor(audio, %d) = %q, want %q", engineMeasuredDefaultSeconds[convert.EngineAudio], got, EstimateSourceMeasured)
	}
}

// TestEstimateSourceForMeasuredEngineOverride is the plan's own central
// proof (PR #9 finding at estimate.go:241): a measured engine whose
// configured value DIVERGES from the documented run's own figure must NOT
// report "measured" -- an operator override cannot masquerade as the
// exact number a supervised RTF run actually measured.
func TestEstimateSourceForMeasuredEngineOverride(t *testing.T) {
	overridden := engineMeasuredDefaultSeconds[convert.EngineAudio] + 100
	got := estimateSourceFor(convert.EngineAudio, overridden)
	if got != EstimateSourceUnmeasured {
		t.Fatalf("estimateSourceFor(audio, %d) = %q, want %q (overridden away from the measured default %d)", overridden, got, EstimateSourceUnmeasured, engineMeasuredDefaultSeconds[convert.EngineAudio])
	}
}

// TestEstimateSourceForUnmeasuredEngineAlwaysUnmeasured proves an engine
// with no measured provenance at all reports unmeasured regardless of its
// configured value -- there is no "measured" claim on the table for it to
// lose or keep.
func TestEstimateSourceForUnmeasuredEngineAlwaysUnmeasured(t *testing.T) {
	for _, secs := range []int{0, 120, 99999} {
		got := estimateSourceFor(convert.EngineImage, secs)
		if got != EstimateSourceUnmeasured {
			t.Errorf("estimateSourceFor(image, %d) = %q, want %q", secs, got, EstimateSourceUnmeasured)
		}
	}
}

// TestEstimateSourceForMatchesEnvExampleDefaults mechanically re-verifies
// engineMeasuredDefaultSeconds against .env.example's OWN numeric value for
// each measured engine's timeout var, exactly like
// TestEstimateSourceMatchesEnvExampleProvenance re-verifies the
// measured/unmeasured classification below -- a future .env.example edit
// that changes a measured value without updating this map fails here
// rather than silently drifting.
func TestEstimateSourceForMatchesEnvExampleDefaults(t *testing.T) {
	content, err := os.ReadFile("../../.env.example")
	if err != nil {
		t.Fatalf("read .env.example: %v", err)
	}
	text := string(content)

	for engine, wantSeconds := range engineMeasuredDefaultSeconds {
		varName, ok := envExampleEngineVar[engine]
		if !ok {
			t.Fatalf("engine %s has no envExampleEngineVar entry", engine)
		}
		block, found := envExampleBlockContaining(text, varName)
		if !found {
			t.Fatalf("var %s (engine %s) not found in .env.example", varName, engine)
		}
		var gotSeconds int
		if _, err := fmt.Sscanf(strings.TrimSpace(strings.Split(block, "\n")[0]), varName+"=%ds", &gotSeconds); err != nil {
			// The var's own assignment line is not necessarily the block's
			// first line for every engine -- fall back to a direct scan of
			// every line in the block.
			found := false
			for _, line := range strings.Split(block, "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, varName+"=") {
					if _, serr := fmt.Sscanf(trimmed, varName+"=%ds", &gotSeconds); serr == nil {
						found = true
						break
					}
				}
			}
			if !found {
				t.Fatalf("could not parse %s's numeric value from .env.example block:\n%s", varName, block)
			}
		}
		if gotSeconds != wantSeconds {
			t.Errorf("engine %s (%s): .env.example = %ds, engineMeasuredDefaultSeconds = %ds", engine, varName, gotSeconds, wantSeconds)
		}
	}
}

// TestEveryEngineHasAnEnvExampleVar keeps the map above complete. Without it,
// an engine added to convert.AllEngines() and forgotten here is simply not
// checked by TestEstimateSourceMatchesEnvExampleProvenance, which then reports
// success over a strictly smaller set than it claims.
func TestEveryEngineHasAnEnvExampleVar(t *testing.T) {
	for _, engine := range convert.AllEngines() {
		if _, ok := envExampleEngineVar[engine]; !ok {
			t.Errorf("engine %q has no envExampleEngineVar entry, so its .env.example provenance is "+
				"compared against nothing", engine)
		}
	}
}

func TestEstimateSourceMatchesEnvExampleProvenance(t *testing.T) {
	content, err := os.ReadFile("../../.env.example")
	if err != nil {
		t.Fatalf("read .env.example: %v", err)
	}
	text := string(content)

	for engine, varName := range envExampleEngineVar {
		block, found := envExampleBlockContaining(text, varName)
		if !found {
			t.Fatalf("var %s (engine %s) not found in .env.example", varName, engine)
		}
		measured := envExampleClaimsMeasured(block, varName)
		wantMeasured := engineEstimateSource[engine] == EstimateSourceMeasured
		if measured != wantMeasured {
			t.Errorf("engine %s (%s): .env.example measured=%v, engineEstimateSource says measured=%v", engine, varName, measured, wantMeasured)
		}
	}
}

// TestEveryEngineHasADefaultCeilingAndTimeout is the completeness guard for
// the two per-engine default maps, added after PR #12 found the eighth engine
// missing from both.
//
// Neither omission could fail a test at the time: MaxEngineBytes silently left
// probe bounded only by the 2 GiB global request limit, in front of a 1 GiB
// worker, and EngineTimeouts silently published estimated_max_seconds 0
// because cmd/api supplies a non-nil map that the all-or-nothing default never
// touched. Both were found by reading, which is not a mechanism.
//
// Driven off convert.AllEngines() rather than a list, so a ninth class fails
// here the moment it is declared.
func TestEveryEngineHasADefaultCeilingAndTimeout(t *testing.T) {
	for _, engine := range convert.AllEngines() {
		if _, ok := defaultMaxEngineBytes[engine]; !ok {
			t.Errorf("engine %q has no defaultMaxEngineBytes entry -- its uploads would be "+
				"bounded only by the global request limit, in front of a worker that is not",
				engine)
		}
		if _, ok := defaultEngineTimeouts[engine]; !ok {
			t.Errorf("engine %q has no defaultEngineTimeouts entry -- POST /v1/inspect would "+
				"publish estimated_max_seconds 0 for it", engine)
		}
	}
	if len(defaultMaxEngineBytes) != len(convert.AllEngines()) {
		t.Errorf("defaultMaxEngineBytes has %d entries, convert.AllEngines() has %d",
			len(defaultMaxEngineBytes), len(convert.AllEngines()))
	}
	if len(defaultEngineTimeouts) != len(convert.AllEngines()) {
		t.Errorf("defaultEngineTimeouts has %d entries, convert.AllEngines() has %d",
			len(defaultEngineTimeouts), len(convert.AllEngines()))
	}
}

// TestPartialEngineTimeoutsAreFilledIn pins the merge itself. cmd/api supplies
// a non-nil map; a caller that forgets one engine must not silently publish a
// zero upper bound for it.
func TestPartialEngineTimeoutsAreFilledIn(t *testing.T) {
	srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{},
		defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{},
		newFakeResolver(), healthyDeps(), Config{
			MaxUploadBytes: 1 << 20,
			EngineTimeouts: map[string]time.Duration{
				convert.EngineImage: 99 * time.Second,
			},
		})
	if got := srv.engineTimeouts[convert.EngineImage]; got != 99*time.Second {
		t.Errorf("caller's own value was overwritten: image = %v, want 99s", got)
	}
	for _, engine := range convert.AllEngines() {
		if srv.engineTimeouts[engine] == 0 {
			t.Errorf("engine %q was omitted by the caller and left at zero", engine)
		}
	}
}
