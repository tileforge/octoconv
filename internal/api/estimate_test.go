package api

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/convert"
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
		measured := strings.Contains(strings.ToLower(block), "measur")
		wantMeasured := engineEstimateSource[engine] == EstimateSourceMeasured
		if measured != wantMeasured {
			t.Errorf("engine %s (%s): .env.example measured=%v, engineEstimateSource says measured=%v", engine, varName, measured, wantMeasured)
		}
	}
}
