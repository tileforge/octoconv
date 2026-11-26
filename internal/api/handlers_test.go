package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/auth"
	"github.com/tileforge/octoconv/internal/clients"
	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/errcode"
	"github.com/tileforge/octoconv/internal/jobs"
	"github.com/tileforge/octoconv/internal/presets"
	"github.com/tileforge/octoconv/internal/workerproto"
)

// testClientKey is the raw key fakeResolver accepts; requests present it via
// "Authorization: ApiKey testkey".
const testClientKey = "testkey"

// --- fakes ---

type fakeRepo struct {
	created *jobs.CreateParams
	// preAdmitBytes records what the advisory quota gate was charged, so a
	// test can hold it equal to what Create will charge.
	preAdmitBytes int64
	getJob        *jobs.Job
	getErr        error
	outputs       []jobs.Output
	createdID     uuid.UUID
	createErr     error
	// cancelErr is returned by Cancel; canceled records whether Cancel was
	// invoked at all, so tests can assert the already-terminal short-circuit
	// (D-02) never calls Cancel.
	cancelErr error
	canceled  bool
	// getJobAfterCancel, when non-nil, is returned by Get on any call AFTER
	// Cancel has been invoked once — simulating the D-02 terminal-race
	// (the job reached done/failed between the initial read and Cancel).
	getJobAfterCancel *jobs.Job
	// listResult/listNext/listErr configure List's return; the list* capture
	// fields record the args handleListJobs actually passed, so tests can
	// assert the caller's client.ID is the sole scope (T-40-04) and the
	// clamped limit reached the repo.
	listResult   []jobs.JobSummary
	listNext     *jobs.Cursor
	listErr      error
	listClientID uuid.UUID
	listFilter   jobs.ListFilter
	listCursor   *jobs.Cursor
	listLimit    int
	// preAdmitErr is returned by PreAdmit; preAdmitCalls counts invocations
	// so tests can assert the pre-upload quota gate ran exactly once (52-04).
	preAdmitErr   error
	preAdmitCalls int
}

func (f *fakeRepo) PreAdmit(_ context.Context, _ uuid.UUID, _ string, bytes int64) error {
	f.preAdmitBytes = bytes
	f.preAdmitCalls++
	return f.preAdmitErr
}

func (f *fakeRepo) Create(_ context.Context, p jobs.CreateParams) (uuid.UUID, error) {
	f.created = &p
	if f.createErr != nil {
		return uuid.Nil, f.createErr
	}
	if f.createdID == uuid.Nil {
		f.createdID = uuid.New()
	}
	return f.createdID, nil
}
func (f *fakeRepo) Get(_ context.Context, _ uuid.UUID) (*jobs.Job, error) {
	if f.canceled && f.getJobAfterCancel != nil {
		return f.getJobAfterCancel, nil
	}
	return f.getJob, f.getErr
}
func (f *fakeRepo) Outputs(_ context.Context, _ uuid.UUID) ([]jobs.Output, error) {
	return f.outputs, nil
}
func (f *fakeRepo) Cancel(_ context.Context, _ uuid.UUID) error {
	f.canceled = true
	return f.cancelErr
}
func (f *fakeRepo) List(_ context.Context, clientID uuid.UUID, filter jobs.ListFilter, cursor *jobs.Cursor, limit int) ([]jobs.JobSummary, *jobs.Cursor, error) {
	f.listClientID = clientID
	f.listFilter = filter
	f.listCursor = cursor
	f.listLimit = limit
	return f.listResult, f.listNext, f.listErr
}

// fakeUsageRepo backs api.UsageRepo in tests, copying fakeRepo's
// capture-args-and-configurable-returns shape (no mocking library). calls is
// load-bearing for plan 53-04: its validation tests assert 422/400 responses
// happen with calls == 0, i.e. before any DB work (D-03).
//
// today/todayErr/todayCalls back UTCToday (plan 53-07, WR-03). todayCalls is
// DELIBERATELY a separate counter from calls: calls keeps meaning "UsageSummary
// was called" for every existing assertion, and todayCalls means "UTCToday was
// called" — mixing them would silently change what every pre-existing
// zero-repo-call assertion actually proves.
type fakeUsageRepo struct {
	rows        []jobs.UsageRow
	err         error
	calls       int
	gotClientID *uuid.UUID
	gotDayFrom  time.Time
	gotDayTo    time.Time
	today       time.Time
	todayErr    error
	todayCalls  int
}

func (f *fakeUsageRepo) UsageSummary(_ context.Context, clientID *uuid.UUID, dayFrom, dayTo time.Time) ([]jobs.UsageRow, error) {
	f.calls++
	f.gotClientID = clientID
	f.gotDayFrom = dayFrom
	f.gotDayTo = dayTo
	return f.rows, f.err
}

// UTCToday returns f.today verbatim when set (frozen-clock tests); when
// f.today is the zero value it falls back to the real UTC-midnight of
// time.Now(), computed with the explicit time.Date(...) form rather than a
// duration-based truncation — that truncates against the Unix epoch, not
// calendar days, so it does not reliably land on UTC midnight for every
// input.
func (f *fakeUsageRepo) UTCToday(_ context.Context) (time.Time, error) {
	f.todayCalls++
	if f.todayErr != nil {
		return time.Time{}, f.todayErr
	}
	if f.today.IsZero() {
		n := time.Now().UTC()
		return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	return f.today, nil
}

// fakeLimitsRepo backs api.LimitsRepo in tests, mirroring fakeUsageRepo's
// shape.
type fakeLimitsRepo struct {
	rows        []clients.LimitWithMeta
	err         error
	calls       int
	gotClientID *uuid.UUID
}

func (f *fakeLimitsRepo) ListAllLimits(_ context.Context, clientID *uuid.UUID) ([]clients.LimitWithMeta, error) {
	f.calls++
	f.gotClientID = clientID
	return f.rows, f.err
}

type fakeStorage struct {
	uploaded    bool
	contentType string
	presigned   string
	// presignByKey (57-06, D-18) lets PresignGet return a DIFFERENT url per
	// object key, so a test can tell two outputs' presigned URLs apart --
	// something a single shared presigned field cannot do. Looked up first;
	// when the passed key has no entry, PresignGet falls back to the
	// pre-existing presigned field unchanged, so every test written before
	// this field existed keeps working without modification.
	presignByKey map[string]string
	// presignCalls/presignTTLs (57-06, D-12/T-57-27) count PresignGet
	// invocations and record the ttl argument of each, in call order --
	// used to prove presigning happens exactly once per output, only for a
	// terminal (done) job, and always with the server's configured
	// presignTTL, mirroring uploadCalls' existing "assert with an exact
	// integer" idiom below.
	presignCalls int
	presignTTLs  []time.Duration
	// uploadedBytes captures the full stream s.storage.Upload received, so
	// tests can assert byte-for-byte integrity (T-31-02: the SniffAudio
	// truncation trap must never drop the leading bytes of an audio upload).
	uploadedBytes []byte
	// uploadCalls counts Upload invocations so a quota test can assert "never
	// called" with an exact integer rather than inferring it from the boolean
	// (52-04).
	uploadCalls int
}

func (f *fakeStorage) Upload(_ context.Context, _ string, r io.Reader, _ int64, contentType string) error {
	b, _ := io.ReadAll(r)
	f.uploadedBytes = b
	f.uploaded = true
	f.uploadCalls++
	f.contentType = contentType
	return nil
}
func (f *fakeStorage) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	f.presignCalls++
	f.presignTTLs = append(f.presignTTLs, ttl)
	if url, ok := f.presignByKey[key]; ok {
		return url, nil
	}
	return f.presigned, nil
}

type fakeQueue struct {
	enqueuedImage    uuid.UUID
	enqueuedDocument uuid.UUID
	enqueuedHTML     uuid.UUID
	enqueuedAudio    uuid.UUID
	enqueuedAV       uuid.UUID
	enqueuedOCR      uuid.UUID
	enqueuedPandoc   uuid.UUID
	enqueuedProbe    uuid.UUID
	enqueuedTectonic uuid.UUID
	enqueuedCAD      uuid.UUID
	// enqueueCalls counts every Enqueue* invocation across all seven engine
	// methods, so a quota test can assert "never enqueued" with an exact
	// integer (52-04).
	enqueueCalls int
}

func (f *fakeQueue) EnqueueImageConvert(_ context.Context, id uuid.UUID) error {
	f.enqueuedImage = id
	f.enqueueCalls++
	return nil
}

func (f *fakeQueue) EnqueueDocumentConvert(_ context.Context, id uuid.UUID) error {
	f.enqueuedDocument = id
	f.enqueueCalls++
	return nil
}

func (f *fakeQueue) EnqueueAudioConvert(_ context.Context, id uuid.UUID) error {
	f.enqueuedAudio = id
	f.enqueueCalls++
	return nil
}

func (f *fakeQueue) EnqueueAVConvert(_ context.Context, id uuid.UUID) error {
	f.enqueuedAV = id
	f.enqueueCalls++
	return nil
}

func (f *fakeQueue) EnqueueOCRConvert(_ context.Context, id uuid.UUID) error {
	f.enqueuedOCR = id
	f.enqueueCalls++
	return nil
}

func (f *fakeQueue) EnqueuePandocConvert(_ context.Context, id uuid.UUID) error {
	f.enqueuedPandoc = id
	f.enqueueCalls++
	return nil
}

func (f *fakeQueue) EnqueueProbeSanitize(_ context.Context, id uuid.UUID) error {
	f.enqueuedProbe = id
	f.enqueueCalls++
	return nil
}

func (f *fakeQueue) EnqueueTectonicConvert(_ context.Context, id uuid.UUID) error {
	f.enqueuedTectonic = id
	f.enqueueCalls++
	return nil
}

func (f *fakeQueue) EnqueueCADJob(_ context.Context, id uuid.UUID) error {
	f.enqueuedCAD = id
	f.enqueueCalls++
	return nil
}

// fakePresetRepo implements api.PresetRepo. It supports a DIFFERENT result on
// the SECOND (and later) Resolve call so the pre-Create active re-check
// (Pitfall 8) can be simulated: resolve/resolveErr are the default result
// returned on every call; when recheckSet is true, the second-and-later call
// returns recheckResolve/recheckErr instead.
type fakePresetRepo struct {
	resolve    *presets.Preset
	resolveErr error

	recheckSet     bool
	recheckResolve *presets.Preset
	recheckErr     error

	calls int
}

func (f *fakePresetRepo) Resolve(_ context.Context, _ uuid.UUID, _ string) (*presets.Preset, error) {
	f.calls++
	if f.calls >= 2 && f.recheckSet {
		return f.recheckResolve, f.recheckErr
	}
	return f.resolve, f.resolveErr
}

// defaultFakePresetRepo returns presets.ErrNotFound on every call -- the
// no-preset default used by every existing (pre-18-03) test so their
// behavior is unaffected by the new NewServer positional argument.
func defaultFakePresetRepo() *fakePresetRepo {
	return &fakePresetRepo{resolveErr: presets.ErrNotFound}
}

// fakePresetAdmin implements api.PresetAdmin (D-08/Task 2). It supports
// settable return values per method plus a captured lastCreateParams so
// tests can assert the mass-assignment guard (T-20-01): a request body
// attempting to smuggle scope/client_id never reaches the repo layer with
// anything other than the ctx-derived values.
type fakePresetAdmin struct {
	createErr error

	updateVersion int
	updateErr     error

	deactivateErr error

	getResult *presets.Preset
	getErr    error

	listResult []presets.Preset
	listErr    error

	getForClientResult *presets.Preset
	getForClientErr    error

	listForClientResult []presets.Preset
	listForClientErr    error

	lastCreateParams *presets.CreateParams
}

func (f *fakePresetAdmin) Create(_ context.Context, p presets.CreateParams) (uuid.UUID, int, error) {
	f.lastCreateParams = &p
	if f.createErr != nil {
		return uuid.Nil, 0, f.createErr
	}
	return uuid.New(), 1, nil
}

func (f *fakePresetAdmin) Update(_ context.Context, _ string, _ *uuid.UUID, _, _ string, _ map[string]any, _ string) (int, error) {
	if f.updateErr != nil {
		return 0, f.updateErr
	}
	if f.updateVersion == 0 {
		return 2, nil
	}
	return f.updateVersion, nil
}

func (f *fakePresetAdmin) Deactivate(_ context.Context, _ string, _ *uuid.UUID, _ string) error {
	return f.deactivateErr
}

func (f *fakePresetAdmin) Get(_ context.Context, _ string, _ *uuid.UUID, _ string) (*presets.Preset, error) {
	return f.getResult, f.getErr
}

func (f *fakePresetAdmin) List(_ context.Context, _ string, _ *uuid.UUID, _ bool) ([]presets.Preset, error) {
	return f.listResult, f.listErr
}

func (f *fakePresetAdmin) ListForClient(_ context.Context, _ *uuid.UUID, _ bool) ([]presets.Preset, error) {
	return f.listForClientResult, f.listForClientErr
}

func (f *fakePresetAdmin) GetForClient(_ context.Context, _ *uuid.UUID, _ string) (*presets.Preset, error) {
	return f.getForClientResult, f.getForClientErr
}

// defaultFakePresetAdmin returns presets.ErrNotFound on every read -- the
// no-op default used by every existing (pre-20-01) test so their behavior is
// unaffected by the new NewServer positional argument.
func defaultFakePresetAdmin() *fakePresetAdmin {
	return &fakePresetAdmin{getErr: presets.ErrNotFound, getForClientErr: presets.ErrNotFound}
}

// fakePinger implements api.Pinger: returns err (nil = healthy) from Ping.
type fakePinger struct{ err error }

func (f fakePinger) Ping(_ context.Context) error { return f.err }

// healthyDeps is a HealthDeps with all three dependencies reachable.
func healthyDeps() HealthDeps {
	return HealthDeps{Postgres: fakePinger{}, Redis: fakePinger{}, S3: fakePinger{}}
}

// fakeResolver implements auth.ClientResolver: it resolves testClientKey to a
// fixed test client and rejects everything else with auth.ErrInvalidKey.
type fakeResolver struct {
	client *clients.Client
}

func newFakeResolver() *fakeResolver {
	return &fakeResolver{client: &clients.Client{ID: uuid.New(), Name: "test-client"}}
}

func (f *fakeResolver) ResolveClient(_ context.Context, rawKey string) (*clients.Client, error) {
	if rawKey != testClientKey {
		return nil, auth.ErrInvalidKey
	}
	return f.client, nil
}

// --- helpers ---

// pngBytesFixture returns a full 24-byte PNG signature+IHDR chunk (declaring
// a small, well-within-the-default-limit 100x100 image) so both convert.Sniff
// AND convert.Dimensions succeed on it — Dimensions needs the full IHDR
// (signature[8] + length[4] + type[4] + width[4] + height[4]), unlike a bare
// 16-byte Sniff-only prefix which stops mid-chunk.
func pngBytesFixture() []byte {
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // signature
		0x00, 0x00, 0x00, 0x0D, // chunk length = 13
		0x49, 0x48, 0x44, 0x52, // "IHDR"
		0x00, 0x00, 0x00, 0x64, // width = 100
		0x00, 0x00, 0x00, 0x64, // height = 100
	}
}

// oversizedPNGFixture returns a full PNG signature+IHDR chunk declaring
// width=height=20000 (400 megapixels), over the 100-megapixel default limit.
func oversizedPNGFixture() []byte {
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // signature
		0x00, 0x00, 0x00, 0x0D, // chunk length = 13
		0x49, 0x48, 0x44, 0x52, // "IHDR"
		0x00, 0x00, 0x4E, 0x20, // width = 20000
		0x00, 0x00, 0x4E, 0x20, // height = 20000
	}
}

// truncatedIHDRPNGFixture is a valid PNG signature (Sniff-passing) followed
// by a chunk whose type is NOT "IHDR" — convert.Dimensions cannot locate the
// declared dimensions and must fail closed (D-07).
func truncatedIHDRPNGFixture() []byte {
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // signature
		0x00, 0x00, 0x00, 0x0D, // chunk length = 13
		0x00, 0x00, 0x00, 0x00, // NOT "IHDR"
		0x00, 0x00, 0x00, 0x64,
		0x00, 0x00, 0x00, 0x64,
	}
}

// jpegBytesFixture returns the minimal magic-byte prefix that convert.Sniff
// detects as "jpg".
func jpegBytesFixture() []byte {
	return []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01}
}

// paddedPNGFixture returns pngBytesFixture() followed by size-24 zero bytes,
// giving D-07 ceiling tests (35-04) control over the PHYSICAL upload size
// (header.Size) independent of the small declared IHDR dimensions
// convert.Dimensions reads from the front of the stream -- padding never
// disturbs Sniff (fixed 12-byte window) or Dimensions (stops after the
// width/height fields). Returns pngBytesFixture() unchanged if size is not
// larger than it already is.
func paddedPNGFixture(size int) []byte {
	base := pngBytesFixture()
	if size <= len(base) {
		return base
	}
	out := make([]byte, size)
	copy(out, base)
	return out
}

// mp4BytesFixture returns the minimal ftyp+brand box matchMP4 (avsniff.go)
// detects as "mp4": a 4-byte box-size field (value unused by the matcher)
// followed by "ftyp" and the allowlisted major brand "isom".
func mp4BytesFixture() []byte {
	return []byte{
		0x00, 0x00, 0x00, 0x18, // box size (declared, ignored by matchMP4)
		0x66, 0x74, 0x79, 0x70, // "ftyp"
		0x69, 0x73, 0x6F, 0x6D, // "isom" -- mp4VideoBrands entry
	}
}

// paddedMP4Fixture mirrors paddedPNGFixture for the mp4 magic-byte prefix.
func paddedMP4Fixture(size int) []byte {
	base := mp4BytesFixture()
	if size <= len(base) {
		return base
	}
	out := make([]byte, size)
	copy(out, base)
	return out
}

// buildEBMLFixture constructs a byte-exact EBML header fixture for docType
// ("matroska" or "webm"), matching convert.SniffVideo's matchEBML parser
// exactly -- mirrors internal/convert/avsniff_test.go's own buildEBMLHeader
// helper (unexported to that package, so duplicated here rather than
// imported) so mkv/webm detection can be exercised end-to-end through
// handleCreateJob with a committed byte-slice literal, no binary fixture
// dependency (per the plan's stated preference). docType must be shorter
// than 16 bytes so its SIZE vint fits in a single byte (0x80|len).
func buildEBMLFixture(docType string) []byte {
	body := []byte{}
	body = append(body, 0x42, 0x86, 0x81, 0x01) // EBMLVersion = 1
	body = append(body, 0x42, 0xf7, 0x81, 0x01) // EBMLReadVersion = 1
	body = append(body, 0x42, 0xf2, 0x81, 0x04) // EBMLMaxIDLength = 4
	body = append(body, 0x42, 0xf3, 0x81, 0x08) // EBMLMaxSizeLength = 8
	body = append(body, 0x42, 0x82, byte(0x80|len(docType)))
	body = append(body, []byte(docType)...)
	body = append(body, 0x42, 0x87, 0x81, 0x04) // DocTypeVersion = 4
	body = append(body, 0x42, 0x85, 0x81, 0x02) // DocTypeReadVersion = 2

	header := []byte{0x1A, 0x45, 0xDF, 0xA3} // ebmlMagic (avsniff.go)
	header = append(header, byte(0x80|len(body)))
	header = append(header, body...)
	return header
}

// mkvBytesFixture returns a minimal, valid EBML header with DocType
// "matroska" -- convert.SniffVideo detects this as "mkv".
func mkvBytesFixture() []byte {
	return buildEBMLFixture("matroska")
}

// webmBytesFixture returns a minimal, valid EBML header with DocType
// "webm" -- convert.SniffVideo detects this as "webm".
func webmBytesFixture() []byte {
	return buildEBMLFixture("webm")
}

// avTestTarget is a synthetic, never-real target format string. It exists
// solely so fakeAVConverter's (mp4, avTestTarget) pair cannot collide with
// any real registration -- not AudioConverter's video-to-transcript pairs
// (D-04, mp4 -> txt/srt/vtt/json), and not the real AVConverter's own pairs
// (av.go), which is now registered into convert.Default (D-08, 35-06).
const avTestTarget = "avtestout"

// fakeAVConverter is a minimal convert.Converter registered ONLY so D-07
// ceiling/enqueue-switch tests (35-04) can exercise engine=="av" end-to-end
// through handleCreateJob via a collision-free synthetic pair, independent
// of whichever real (source, target) pairs the actual AVConverter happens to
// support.
type fakeAVConverter struct{}

func (fakeAVConverter) Pairs() []convert.Pair {
	return []convert.Pair{{From: "mp4", To: avTestTarget}}
}

func (fakeAVConverter) Convert(context.Context, string, string, map[string]any) error {
	return nil
}

func (fakeAVConverter) Engine() string { return convert.EngineAV }

func init() {
	convert.Default.Register(fakeAVConverter{})
}

// --- document container fixture helpers (mirror internal/convert/docsniff_test.go) ---

// mustWriteZipEntry adds a single deflate-compressed entry to zw, failing
// the test on any write error. Takes testing.TB (not the concrete *testing.T)
// so fuzz targets can build these same fixtures as seed corpus entries at
// *testing.F setup time -- testing.TB is "the interface common to T, B, and
// F" (testing package doc), and every existing call site already passes a
// concrete *testing.T, which satisfies it without any change at the call
// site.
func mustWriteZipEntry(t testing.TB, zw *zip.Writer, name, content string) {
	t.Helper()
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
	if err != nil {
		t.Fatalf("CreateHeader(%q): %v", name, err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatalf("Write(%q): %v", name, err)
	}
}

// docxFixture returns a minimal, valid-shaped docx built via archive/zip.Writer
// (word/document.xml root part present) so convert.SniffContainer detects it.
func docxFixture(t testing.TB) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteZipEntry(t, zw, "[Content_Types].xml", "<Types/>")
	mustWriteZipEntry(t, zw, "word/document.xml", "root part content")
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// odtFixture returns a minimal odt: first entry "mimetype", Method=Store,
// payload "application/vnd.oasis.opendocument.text".
func odtFixture(t *testing.T) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatalf("CreateHeader(mimetype): %v", err)
	}
	if _, err := fw.Write([]byte("application/vnd.oasis.opendocument.text")); err != nil {
		t.Fatalf("Write(mimetype): %v", err)
	}
	mustWriteZipEntry(t, zw, "META-INF/manifest.xml", "<manifest/>")
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// zipBombDocxFixture returns a docx-shaped zip whose declared
// UncompressedSize64 equals declaredSize, written from real (highly
// compressible, all-zero) content so the physical multipart body stays tiny
// while the declared total exceeds a small test MaxDocumentUncompressedBytes.
func zipBombDocxFixture(t *testing.T, declaredSize uint64) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "word/document.xml", Method: zip.Deflate})
	if err != nil {
		t.Fatalf("CreateHeader: %v", err)
	}
	chunk := make([]byte, 1<<20) // 1 MiB of zeros, reused per write
	var written uint64
	for written < declaredSize {
		n := declaredSize - written
		if n > uint64(len(chunk)) {
			n = uint64(len(chunk))
		}
		if _, err := w.Write(chunk[:n]); err != nil {
			t.Fatalf("Write: %v", err)
		}
		written += n
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// macroDocxFixture is a valid docx PLUS a word/vbaProject.bin entry.
func macroDocxFixture(t *testing.T) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteZipEntry(t, zw, "word/document.xml", "root part content")
	mustWriteZipEntry(t, zw, "word/vbaProject.bin", "macro content")
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// duplicateRootPartDocxFixture is a zip with two entries named
// word/document.xml -- SniffContainer's fail-closed guard must leave this
// unrecognized rather than accept an ambiguous archive.
func duplicateRootPartDocxFixture(t *testing.T) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteZipEntry(t, zw, "word/document.xml", "first copy")
	mustWriteZipEntry(t, zw, "word/document.xml", "second copy, different content")
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// bareZipFixture is a plain .zip with no office root parts -- PK-prefixed
// but SniffContainer.Format stays "", so it must fall through to the
// existing unrecognized-content 422.
func bareZipFixture(t *testing.T) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteZipEntry(t, zw, "readme.txt", "hello")
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

func multipartBody(t *testing.T, filename, target string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	return multipartBodyWithOpts(t, filename, target, data, "")
}

// multipartBodyWithOpts is multipartBody plus an optional "opts" form field,
// written only when non-empty -- mirrors the callback_url optional-field
// discipline used by handleCreateJob itself.
func multipartBodyWithOpts(t *testing.T, filename, target string, data []byte, opts string) (*bytes.Buffer, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	if filename != "" {
		fw, err := w.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(data)
	}
	if target != "" {
		_ = w.WriteField("target", target)
	}
	if opts != "" {
		_ = w.WriteField("opts", opts)
	}
	_ = w.Close()
	return &b, w.FormDataContentType()
}

// multipartBodyWithOperation is multipartBody plus an optional "operation"
// form field, written only when non-empty -- lets 57-04's operation-field
// tests send an explicit value (or omit it entirely by passing "") without a
// new bespoke multipart builder per test.
func multipartBodyWithOperation(t *testing.T, filename, target, operation string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	if filename != "" {
		fw, err := w.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(data)
	}
	if target != "" {
		_ = w.WriteField("target", target)
	}
	if operation != "" {
		_ = w.WriteField("operation", operation)
	}
	_ = w.Close()
	return &b, w.FormDataContentType()
}

// multipartBodyWithCallback is multipartBody plus optional "callback_url" and
// "progress_webhook" form fields, each written only when non-empty -- used
// by the progress_webhook opt-in parse tests (PUSH-02/D-03).
func multipartBodyWithCallback(t *testing.T, filename, target string, data []byte, callbackURL, progressWebhook string) (*bytes.Buffer, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	if filename != "" {
		fw, err := w.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(data)
	}
	if target != "" {
		_ = w.WriteField("target", target)
	}
	if callbackURL != "" {
		_ = w.WriteField("callback_url", callbackURL)
	}
	if progressWebhook != "" {
		_ = w.WriteField("progress_webhook", progressWebhook)
	}
	_ = w.Close()
	return &b, w.FormDataContentType()
}

// multipartBodyWithPreset writes a "preset" form field instead of "target",
// and an optional "opts" form field -- used by the D-01 mutual-exclusivity
// tests (preset+target, preset+opts) and the preset-resolution tests.
func multipartBodyWithPreset(t *testing.T, filename, preset, target string, data []byte, opts string) (*bytes.Buffer, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	if filename != "" {
		fw, err := w.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(data)
	}
	if preset != "" {
		_ = w.WriteField("preset", preset)
	}
	if target != "" {
		_ = w.WriteField("target", target)
	}
	if opts != "" {
		_ = w.WriteField("opts", opts)
	}
	_ = w.Close()
	return &b, w.FormDataContentType()
}

// newTestServer wires a Server with a fakeResolver that accepts
// testClientKey; it returns the resolver too so tests can assert against its
// fixed client id. The PresetRepo slot defaults to ErrNotFound on every call
// so existing non-preset tests are unaffected (D-09/Task 3).
func newTestServer(repo Repo, store Storage, q Enqueuer) (*Server, *fakeResolver) {
	resolver := newFakeResolver()
	return NewServer(repo, store, q, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20}), resolver
}

// authed sets the Authorization header requests need to pass auth.Middleware.
func authed(req *http.Request) *http.Request {
	req.Header.Set("Authorization", "ApiKey "+testClientKey)
	return req
}

// --- tests ---

func TestCreateJob_OK(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, resolver := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(imageCatalog())

	body, ct := multipartBody(t, "in.png", "webp", pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if !store.uploaded {
		t.Error("expected upload to storage")
	}
	if store.contentType != "image/png" {
		t.Errorf("contentType = %q, want image/png (detected format, not client header, D-06)", store.contentType)
	}
	if repo.created == nil || repo.created.SourceFormat != "png" || repo.created.TargetFormat != "webp" {
		t.Errorf("unexpected create params: %+v", repo.created)
	}
	if repo.created == nil || repo.created.ClientID != resolver.client.ID {
		t.Errorf("expected CreateParams.ClientID = %s, got %+v", resolver.client.ID, repo.created)
	}
	if repo.created.DispatchOwner != "protocol" {
		t.Errorf("dispatch = %q, want protocol", repo.created.DispatchOwner)
	}
	if q.enqueuedImage != uuid.Nil {
		t.Errorf("enqueuedImage = %s, want none", q.enqueuedImage)
	}
	if q.enqueuedDocument != uuid.Nil {
		t.Errorf("enqueuedDocument = %s, want uuid.Nil (image upload must never touch the document queue)", q.enqueuedDocument)
	}
}

// TestCreateJob_OperationAbsentDefaultsToConvert pins D-04: omitting the
// operation field entirely must record jobs.operation = "convert", the same
// value an explicit operation=convert produces (proven by the next test).
func TestCreateJob_OperationAbsentDefaultsToConvert(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(imageCatalog())

	body, ct := multipartBodyWithOperation(t, "in.png", "webp", "", pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if repo.created == nil || repo.created.Operation != "convert" {
		t.Errorf("repo.created.Operation = %+v, want \"convert\"", repo.created)
	}
}

// TestCreateJob_OperationExplicitConvertIdenticalToAbsent pins D-04's core
// claim: below the normalization point, "field absent" and "field=convert"
// are indistinguishable. Proven two ways -- same recorded Operation, AND a
// byte-identical response body for a fixed createdID.
func TestCreateJob_OperationExplicitConvertIdenticalToAbsent(t *testing.T) {
	fixedID := uuid.MustParse("00000000-0000-0000-0000-0000000000aa")

	runOnce := func(operation string) (int, []byte, string) {
		repo := &fakeRepo{createdID: fixedID}
		store := &fakeStorage{}
		q := &fakeQueue{}
		srv, _ := newTestServer(repo, store, q)
		srv.SetCapabilityCatalog(imageCatalog())

		body, ct := multipartBodyWithOperation(t, "in.png", "webp", operation, pngBytesFixture())
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, req)

		if repo.created == nil || repo.created.Operation != "convert" {
			t.Fatalf("operation=%q: repo.created.Operation = %+v, want \"convert\"", operation, repo.created)
		}
		return rec.Code, rec.Body.Bytes(), operation
	}

	absentCode, absentBody, _ := runOnce("")
	explicitCode, explicitBody, _ := runOnce("convert")

	if absentCode != http.StatusAccepted || explicitCode != http.StatusAccepted {
		t.Fatalf("status codes = %d, %d, want both 202", absentCode, explicitCode)
	}
	if !bytes.Equal(absentBody, explicitBody) {
		t.Errorf("response bodies differ:\nabsent:   %s\nexplicit: %s", absentBody, explicitBody)
	}
}

// TestCreateJob_UnknownOperationRejectedBeforeStorageAndCreate is the direct
// proof of SC3/T-57-13/T-57-14: an operation with no registered triple at
// all must be rejected 422 unsupported_operation strictly before
// s.storage.Upload and s.repo.Create, with the queue untouched.
//
// "sanitize" left this list in Phase 60, "render" in Phase 74, and
// "archive" in Phase 129: each now has a registered triple, so using it
// here would test the pair gate rather than the operation gate. extract
// and inspect are still reserved in the jobs.operation CHECK constraint
// with no converter behind them, plus "bogus", which no build will ever
// register -- keeping both kinds means the test still distinguishes
// "reserved but unimplemented" from "not a value at all".
func TestCreateJob_UnknownOperationRejectedBeforeStorageAndCreate(t *testing.T) {
	for _, operation := range []string{"extract", "inspect", "bogus"} {
		t.Run(operation, func(t *testing.T) {
			repo := &fakeRepo{}
			store := &fakeStorage{}
			q := &fakeQueue{}
			srv, _ := newTestServer(repo, store, q)

			body, ct := multipartBodyWithOperation(t, "in.png", "webp", operation, pngBytesFixture())
			req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()

			srv.Routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
			}
			assertErrorCode(t, rec.Body.Bytes(), errcode.UnsupportedOperation)
			if store.uploadCalls != 0 {
				t.Errorf("storage.Upload called %d times, want 0", store.uploadCalls)
			}
			if repo.created != nil {
				t.Errorf("repo.created = %+v, want nil", repo.created)
			}
			if repo.preAdmitCalls != 0 {
				t.Errorf("repo.PreAdmit called %d times, want 0", repo.preAdmitCalls)
			}
			if q.enqueueCalls != 0 {
				t.Errorf("queue enqueue called %d times, want 0", q.enqueueCalls)
			}
		})
	}
}

// TestCreateJob_SanitizePassesTheOperationGateAndFailsThePairGate is the
// other half of what Phase 60 changed, and it is the reason the test above
// could not simply drop "sanitize" and move on.
//
// Lifting the 422 for sanitize must lift it at gate 1 only. A png -> webp
// request under sanitize is still refused, and refused as
// unsupported_conversion rather than unsupported_operation: the operation
// exists, that pair does not, because sanitize returns the format it was
// given. Without this, "the operation gate opened" and "every gate opened"
// produce the same passing test suite.
func TestCreateJob_SanitizePassesTheOperationGateAndFailsThePairGate(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)

	body, ct := multipartBodyWithOperation(t, "in.png", "webp", "sanitize", pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec.Body.Bytes(), errcode.UnsupportedConversion)
	if store.uploadCalls != 0 {
		t.Errorf("storage.Upload called %d times, want 0", store.uploadCalls)
	}
}

func TestCreateJob_SanitizeIdentityPairIsAccepted(t *testing.T) {
	t.Run("without a catalog", func(t *testing.T) {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		q := &fakeQueue{}
		srv, _ := newTestServer(repo, store, q)

		body, ct := multipartBodyWithOperation(t, "in.png", "png", "sanitize", pngBytesFixture())
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
		}
		assertErrorCode(t, rec.Body.Bytes(), errcode.UnsupportedConversion)
		if repo.created != nil || store.uploadCalls != 0 || q.enqueueCalls != 0 || q.enqueuedProbe != uuid.Nil {
			t.Fatalf("refused sanitize created=%v uploads=%d enqueueCalls=%d", repo.created != nil, store.uploadCalls, q.enqueueCalls)
		}
	})

	t.Run("with a catalog", func(t *testing.T) {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		q := &fakeQueue{}
		srv, _ := newTestServer(repo, store, q)
		srv.SetCapabilityCatalog(&fakeCatalog{found: true, match: probeMatch()})

		body, ct := multipartBodyWithOperation(t, "in.png", "png", "sanitize", pngBytesFixture())
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
		}
		if repo.created == nil {
			t.Fatal("repo.Create was never called")
		}
		if repo.created.Operation != convert.OperationSanitize {
			t.Errorf("created.Operation = %q, want %q", repo.created.Operation, convert.OperationSanitize)
		}
		if repo.created.Engine != convert.EngineProbe {
			t.Errorf("created.Engine = %q, want %q", repo.created.Engine, convert.EngineProbe)
		}
		if repo.created.DispatchOwner != "protocol" || repo.created.ProtocolAttemptBudget != 600*time.Second {
			t.Fatalf("created = %+v, want protocol probe at 600s", repo.created)
		}
		if repo.created.CapabilityID != "octoconv.sanitize.probe" || repo.created.CapabilityVersion != 1 {
			t.Fatalf("pin = %s v%d", repo.created.CapabilityID, repo.created.CapabilityVersion)
		}
		if q.enqueueCalls != 0 || q.enqueuedProbe != uuid.Nil {
			t.Fatalf("enqueueCalls = %d enqueuedProbe = %s", q.enqueueCalls, q.enqueuedProbe)
		}
	})
}

// TestCreateJob_KnownOperationUnsupportedPairRejected proves the split
// between the two 422 codes didn't break the pre-existing pair-rejection
// path: a KNOWN operation (convert) with an unregistered pair still gets
// unsupported_conversion, not unsupported_operation.
func TestCreateJob_KnownOperationUnsupportedPairRejected(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBodyWithOperation(t, "in.png", "mp3", "convert", pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec.Body.Bytes(), errcode.UnsupportedConversion)
	if store.uploadCalls != 0 {
		t.Errorf("storage.Upload called %d times, want 0", store.uploadCalls)
	}
	if repo.created != nil {
		t.Errorf("repo.created = %+v, want nil", repo.created)
	}
}

// TestCreateJob_OperationIsCaseSensitive pins T-57-15: no normalization
// beyond the D-04 empty-string default. "Convert" is a different string
// from "convert" and must be rejected as an unknown operation, not silently
// accepted.
func TestCreateJob_OperationIsCaseSensitive(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBodyWithOperation(t, "in.png", "webp", "Convert", pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec.Body.Bytes(), errcode.UnsupportedOperation)
}

// TestCreateJob_QuotaConcurrency_429BeforeUpload pins the SEQUENTIAL property
// of the pre-upload quota gate (QUOT-02, T-52-21): a client that already
// knows it is over quota gets its 429 without an upload. It does NOT and
// cannot speak to a simultaneous burst -- fakeRepo performs no serialization
// of its own -- the concurrent case is
// TestQuotaAdmission/ConcurrentBurstBoundedUploadWaste in plan 52-05, against
// a real repository.
func TestCreateJob_QuotaConcurrency_429BeforeUpload(t *testing.T) {
	repo := &fakeRepo{preAdmitErr: jobs.ErrConcurrencyLimit}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)

	body, ct := multipartBody(t, "in.png", "webp", pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Code == http.StatusForbidden {
		t.Fatalf("a quota rejection must never be 403 (QUOT-03)")
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp["code"] != "quota_exceeded" {
		t.Errorf("code = %q, want quota_exceeded", resp["code"])
	}
	if resp["error"] == "" {
		t.Error("expected a non-empty error message")
	}
	retryAfter, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || retryAfter != quotaConcurrencyRetryAfterSeconds {
		t.Errorf("Retry-After = %q, want %d", rec.Header().Get("Retry-After"), quotaConcurrencyRetryAfterSeconds)
	}
	if store.uploadCalls != 0 {
		t.Errorf("storage.Upload called %d times, want 0 (must reject before the S3 write)", store.uploadCalls)
	}
	if q.enqueueCalls != 0 {
		t.Errorf("enqueue called %d times, want 0", q.enqueueCalls)
	}
	if repo.preAdmitCalls != 1 {
		t.Errorf("PreAdmit called %d times, want 1", repo.preAdmitCalls)
	}
}

// TestCreateJob_QuotaDailyBytes_RetryAfterUntilMidnight proves Retry-After is
// DERIVED, not the concurrency constant, for a daily-limit rejection (D-11).
func TestCreateJob_QuotaDailyBytes_RetryAfterUntilMidnight(t *testing.T) {
	repo := &fakeRepo{preAdmitErr: jobs.ErrDailyBytesLimit}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBody(t, "in.png", "webp", pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp["code"] != "quota_exceeded" {
		t.Errorf("code = %q, want quota_exceeded", resp["code"])
	}
	retryAfter, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil {
		t.Fatalf("Retry-After not an integer: %q", rec.Header().Get("Retry-After"))
	}
	if retryAfter < 1 || retryAfter > 86400 {
		t.Errorf("Retry-After = %d, want in [1, 86400]", retryAfter)
	}
	if retryAfter == quotaConcurrencyRetryAfterSeconds {
		t.Errorf("Retry-After = %d, must not equal the concurrency constant (%d) -- proves it is derived", retryAfter, quotaConcurrencyRetryAfterSeconds)
	}
	if store.uploadCalls != 0 {
		t.Errorf("storage.Upload called %d times, want 0", store.uploadCalls)
	}
}

// TestCreateJob_QuotaDailyBytes_RetryAfterStraddlesMidnight is the
// end-to-end regression case
// .planning/todos/pending/2026-08-04-fix-retry-after-midnight-race.md asked
// for: a *jobs.DailyQuotaError whose EvaluatedAt is seconds before UTC
// midnight must produce a small Retry-After, even though the test process's
// own wall clock is nowhere near midnight. TestSecondsUntilUTCMidnight
// cannot reach this case -- it only varies the input to a pure function, not
// the DB-vs-API timing gap that caused the bug.
func TestCreateJob_QuotaDailyBytes_RetryAfterStraddlesMidnight(t *testing.T) {
	evaluatedAt := time.Date(2026, 8, 3, 23, 59, 59, 0, time.UTC)
	repo := &fakeRepo{preAdmitErr: &jobs.DailyQuotaError{Err: jobs.ErrDailyBytesLimit, EvaluatedAt: evaluatedAt}}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBody(t, "in.png", "webp", pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body=%s", rec.Code, rec.Body.String())
	}
	retryAfter, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil {
		t.Fatalf("Retry-After not an integer: %q", rec.Header().Get("Retry-After"))
	}
	if retryAfter >= 60 {
		t.Errorf("Retry-After = %d, want < 60 (derived from EvaluatedAt=%v, one second before UTC midnight) -- a value near 86400 means Retry-After regressed to using the process wall clock instead of the DB-evaluated instant", retryAfter, evaluatedAt)
	}
	if store.uploadCalls != 0 {
		t.Errorf("storage.Upload called %d times, want 0", store.uploadCalls)
	}
}

// TestCreateJob_QuotaFromCreate_429AfterUpload exercises the accepted
// post-upload race path (T-52-21): PreAdmit passed, but Create's
// authoritative admitLocked check lost the race. The upload already
// happened -- the object is left in place for TTL cleanup -- but the job is
// never created and never enqueued.
func TestCreateJob_QuotaFromCreate_429AfterUpload(t *testing.T) {
	repo := &fakeRepo{createErr: jobs.ErrDailyJobsLimit}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(imageCatalog())

	body, ct := multipartBody(t, "in.png", "webp", pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp["code"] != "quota_exceeded" {
		t.Errorf("code = %q, want quota_exceeded", resp["code"])
	}
	if store.uploadCalls != 1 {
		t.Errorf("storage.Upload called %d times, want 1 (accepted post-upload race path)", store.uploadCalls)
	}
	if q.enqueueCalls != 0 {
		t.Errorf("enqueue called %d times, want 0", q.enqueueCalls)
	}
}

// TestCreateJob_PreAdmitInfraErrorDoesNotBlock proves a non-quota PreAdmit
// failure neither fails open into a 500 nor rejects the client (T-52-26):
// Create's authoritative check still runs and admits the request normally.
func TestCreateJob_PreAdmitInfraErrorDoesNotBlock(t *testing.T) {
	repo := &fakeRepo{preAdmitErr: errors.New("boom")}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)
	srv.SetCapabilityCatalog(imageCatalog())

	body, ct := multipartBody(t, "in.png", "webp", pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (a non-quota PreAdmit error must not block or 500); body=%s", rec.Code, rec.Body.String())
	}
	if store.uploadCalls != 1 {
		t.Errorf("storage.Upload called %d times, want 1", store.uploadCalls)
	}
	if q.enqueueCalls != 0 {
		t.Errorf("enqueue called %d times, want 0", q.enqueueCalls)
	}
}

// TestCreateJob_NonQuotaErrorBodiesCarryClosedVocabularyCode supersedes the
// pre-56-03 D-10 boundary ("every other error body left byte-for-byte
// unchanged, no code field"): SEED-020/ERRC-01 widens writeError so every
// 4xx/422/413 now ALSO carries a code from the closed internal/errcode
// vocabulary, assigned by failure class (D-02), while the error message text
// itself stays byte-for-byte unchanged.
func TestCreateJob_NonQuotaErrorBodiesCarryClosedVocabularyCode(t *testing.T) {
	t.Run("422 unsupported pair", func(t *testing.T) {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		srv, _ := newTestServer(repo, store, &fakeQueue{})

		body, ct := multipartBody(t, "in.png", "mp3", pngBytesFixture())
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()

		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
		}
		assertErrorCode(t, rec.Body.Bytes(), errcode.UnsupportedConversion)
	})

	t.Run("413 engine size limit", func(t *testing.T) {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		resolver := newFakeResolver()
		srv := NewServer(repo, store, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
			MaxUploadBytes: 10 << 20,
			MaxEngineBytes: map[string]int64{convert.EngineImage: 100},
		})

		body, ct := multipartBody(t, "in.png", "webp", paddedPNGFixture(500))
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()

		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
		}
		assertErrorCode(t, rec.Body.Bytes(), errcode.PayloadTooLarge)
	})
}

// assertErrorCode decodes body and asserts it has exactly the two flat keys
// D-01 mandates (error, code), that error is non-empty, and that code
// equals wantCode exactly.
func assertErrorCode(t *testing.T, body []byte, wantCode string) {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(resp) != 2 {
		t.Errorf("body has %d keys, want exactly 2 (error, code); body=%s", len(resp), body)
	}
	if errMsg, ok := resp["error"]; !ok || errMsg == "" {
		t.Errorf("body missing non-empty error key; body=%s", body)
	}
	if code, ok := resp["code"]; !ok || code != wantCode {
		t.Errorf("code = %v, want %q; body=%s", code, wantCode, body)
	}
}

// TestSecondsUntilUTCMidnight is a pure unit test over fixed time.Time
// inputs, per D-11's Retry-After derivation for a daily quota rejection.
func TestSecondsUntilUTCMidnight(t *testing.T) {
	cases := []struct {
		name string
		in   time.Time
		want int
	}{
		{"one second before midnight", time.Date(2026, 8, 3, 23, 59, 59, 0, time.UTC), 1},
		{"exactly midnight", time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC), 86400},
		{"midday", time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC), 43200},
		{"ten and a half seconds before midnight", time.Date(2026, 8, 3, 23, 59, 49, 500000000, time.UTC), 11},
		{"half a second past midday", time.Date(2026, 8, 3, 12, 0, 0, 500000000, time.UTC), 43200},
		{"one nanosecond past midnight", time.Date(2026, 8, 3, 0, 0, 0, 1, time.UTC), 86400},
		{"tenth of a second before midnight", time.Date(2026, 8, 3, 23, 59, 59, 900000000, time.UTC), 1},
		{"one nanosecond before midnight", time.Date(2026, 8, 3, 23, 59, 59, 999999999, time.UTC), 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := secondsUntilUTCMidnight(c.in)
			if got != c.want {
				t.Errorf("secondsUntilUTCMidnight(%v) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

// TestQuotaRejection_RetryAfterUsesDBEvaluationInstantNotProcessClock is a
// pure unit test on quotaRejection (no HTTP, no DB), pinning the fix for
// .planning/todos/pending/2026-08-04-fix-retry-after-midnight-race.md: a
// daily rejection's Retry-After must derive from a wrapped
// *jobs.DailyQuotaError's EvaluatedAt, never from the process's own wall
// clock at response-write time.
func TestQuotaRejection_RetryAfterUsesDBEvaluationInstantNotProcessClock(t *testing.T) {
	t.Run("daily bytes, EvaluatedAt one second before midnight", func(t *testing.T) {
		err := &jobs.DailyQuotaError{Err: jobs.ErrDailyBytesLimit, EvaluatedAt: time.Date(2026, 8, 3, 23, 59, 59, 0, time.UTC)}
		_, retryAfter, ok := quotaRejection(err)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if retryAfter != 1 {
			t.Errorf("Retry-After = %d, want exactly 1", retryAfter)
		}
	})

	t.Run("daily jobs, EvaluatedAt one second before midnight", func(t *testing.T) {
		err := &jobs.DailyQuotaError{Err: jobs.ErrDailyJobsLimit, EvaluatedAt: time.Date(2026, 8, 3, 23, 59, 59, 0, time.UTC)}
		_, retryAfter, ok := quotaRejection(err)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if retryAfter != 1 {
			t.Errorf("Retry-After = %d, want exactly 1", retryAfter)
		}
	})

	t.Run("daily bytes, EvaluatedAt at midday", func(t *testing.T) {
		err := &jobs.DailyQuotaError{Err: jobs.ErrDailyBytesLimit, EvaluatedAt: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)}
		_, retryAfter, ok := quotaRejection(err)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if retryAfter != 43200 {
			t.Errorf("Retry-After = %d, want exactly 43200", retryAfter)
		}
	})

	t.Run("bare sentinel fallback (no wrapper) still returns a valid Retry-After", func(t *testing.T) {
		_, retryAfter, ok := quotaRejection(jobs.ErrDailyBytesLimit)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if retryAfter < 1 {
			t.Errorf("Retry-After = %d, want >= 1 -- a bare sentinel must never regress to a zero/missing header", retryAfter)
		}
	})

	t.Run("concurrency rejection returns the fixed constant, unaffected by DailyQuotaError handling", func(t *testing.T) {
		_, retryAfter, ok := quotaRejection(jobs.ErrConcurrencyLimit)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if retryAfter != quotaConcurrencyRetryAfterSeconds {
			t.Errorf("Retry-After = %d, want %d", retryAfter, quotaConcurrencyRetryAfterSeconds)
		}
	})

	t.Run("non-quota error returns ok=false", func(t *testing.T) {
		_, _, ok := quotaRejection(errors.New("boom"))
		if ok {
			t.Error("ok = true, want false for a non-quota error")
		}
	})
}

// TestCreateJob_ContentMismatch verifies D-01/D-04/D-05: a filename claiming
// one format but carrying magic bytes of a different (also-supported) format
// is rejected 422 with a detailed message naming both formats, before any
// storage write or job creation.
// TestCreateJob_ProgressWebhook covers PUSH-02/D-03: progress_webhook is
// parsed as a top-level form flag (not opts) and silently normalized to
// false when there is no callback_url to deliver to (Assumption A4) -- never
// a 400.
func TestCreateJob_ProgressWebhook(t *testing.T) {
	cases := []struct {
		name            string
		callbackURL     string
		progressWebhook string
		want            bool
	}{
		{"true with callback_url", "https://8.8.8.8/hook", "true", true},
		{"true without callback_url normalizes to false", "", "true", false},
		{"absent defaults to false", "https://8.8.8.8/hook", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeRepo{}
			store := &fakeStorage{}
			srv, _ := newTestServer(repo, store, &fakeQueue{})
			srv.SetCapabilityCatalog(imageCatalog())

			body, ct := multipartBodyWithCallback(t, "in.png", "webp", pngBytesFixture(), c.callbackURL, c.progressWebhook)
			req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()

			srv.Routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
			}
			if repo.created == nil {
				t.Fatal("expected job to be created")
			}
			if repo.created.ProgressWebhook != c.want {
				t.Errorf("CreateParams.ProgressWebhook = %v, want %v", repo.created.ProgressWebhook, c.want)
			}
		})
	}
}

func TestCreateJob_ContentMismatch(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBody(t, "in.jpg", "webp", pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !strings.Contains(resp["error"], "jpg") || !strings.Contains(resp["error"], "png") {
		t.Errorf("error = %q, want a message naming both declared (jpg) and detected (png)", resp["error"])
	}
	if store.uploaded {
		t.Error("must not upload before content validation")
	}
	if repo.created != nil {
		t.Error("must not create job for mismatched content")
	}
}

// TestCreateJob_UnrecognizedContent verifies D-02: content matching no known
// signature is rejected 422 before storage/create, regardless of extension.
func TestCreateJob_UnrecognizedContent(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBody(t, "in.png", "webp", []byte("notanimage"))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload unrecognized content")
	}
	if repo.created != nil {
		t.Error("must not create job for unrecognized content")
	}
}

// TestCreateJob_OLECFBRejected verifies SAFE-01/D-05/Pitfall 11: a
// synthetic OLE-CFB-magic file whose directory does not decode to any known
// stream name classifies as convert.CFBUnknown and therefore still falls
// through to the ORIGINAL combined 422 message unchanged (fail-closed
// compat) — this is the CFBUnknown case of the 22-cfb-classification D-06
// three-way split; see TestCreateJob_CFB below for the CFBEncrypted/
// CFBLegacy cases over real fixtures.
func TestCreateJob_OLECFBRejected(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	cfb := append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, []byte("legacy sector padding")...)
	body, ct := multipartBody(t, "in.doc", "pdf", cfb)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload an OLE-CFB document before rejection")
	}
	if repo.created != nil {
		t.Error("must not create job for an OLE-CFB document")
	}
	if !strings.Contains(rec.Body.String(), "password") {
		t.Errorf("body = %s, want a message mentioning password/remedy", rec.Body.String())
	}
}

// TestCreateJob_CFB verifies the 22-cfb-classification D-06 three-way split
// over the real fixtures (internal/api/testdata/, copied from
// internal/e2e/testdata/): a password-protected OOXML .docx classifies
// CFBEncrypted and gets its own distinct 422, and a legacy binary .doc
// classifies CFBLegacy and gets a DIFFERENT distinct 422 that must not
// mention "password" -- proving the two messages are actually
// distinguishable, not just both non-empty. Both must reject before any
// storage write or job creation (Pitfall 11 fail-closed).
func TestCreateJob_CFB(t *testing.T) {
	tests := []struct {
		name       string
		fixture    string
		wantSubstr []string
		wantAbsent []string
	}{
		{
			name:       "CFBEncrypted",
			fixture:    "encrypted.docx",
			wantSubstr: []string{"remove the password", "password-protected"},
		},
		{
			name:       "CFBLegacy",
			fixture:    "legacy.doc",
			wantSubstr: []string{"legacy binary", ".doc/.xls/.ppt"},
			wantAbsent: []string{"password"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + tc.fixture)
			if err != nil {
				t.Fatalf("read fixture %s: %v", tc.fixture, err)
			}

			repo := &fakeRepo{}
			store := &fakeStorage{}
			srv, _ := newTestServer(repo, store, &fakeQueue{})

			ext := tc.fixture[strings.LastIndex(tc.fixture, "."):]
			body, ct := multipartBody(t, "in"+ext, "pdf", data)
			req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()

			srv.Routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
			}
			if store.uploaded {
				t.Errorf("must not upload %s before rejection", tc.fixture)
			}
			if repo.created != nil {
				t.Errorf("must not create job for %s", tc.fixture)
			}
			got := rec.Body.String()
			for _, want := range tc.wantSubstr {
				if !strings.Contains(got, want) {
					t.Errorf("body = %s, want substring %q", got, want)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(got, absent) {
					t.Errorf("body = %s, must NOT contain %q (distinct-message proof)", got, absent)
				}
			}
		})
	}
}

func TestCreateJob_NoAuthHeader_Unauthorized(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBody(t, "in.png", "webp", []byte("fakepng"))
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if repo.created != nil {
		t.Error("must not create job when unauthenticated")
	}
}

func TestCreateJob_UnsupportedPair(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBody(t, "in.png", "mp3", pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if store.uploaded {
		t.Error("must not upload before validating the pair")
	}
	if repo.created != nil {
		t.Error("must not create job for unsupported pair")
	}
}

// TestCreateJob_DimensionLimitExceeded verifies VALID-03/D-04/D-06: an upload
// whose declared pixel dimensions exceed the configured MAX_IMAGE_PIXELS
// limit is rejected 422 before any storage write or job creation.
func TestCreateJob_DimensionLimitExceeded(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	resolver := newFakeResolver()
	srv := NewServer(repo, store, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20, MaxImagePixels: 1_000_000})

	body, ct := multipartBody(t, "in.png", "webp", oversizedPNGFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload a decompression-bomb-shaped upload before the dimension check")
	}
	if repo.created != nil {
		t.Error("must not create job for an oversized declared-dimension upload")
	}
}

// TestCreateJob_DimensionsUnknown verifies D-07: a Sniff-passing upload whose
// declared dimensions cannot be located within the bounded window fails
// closed with 422, not a fallback accept.
func TestCreateJob_DimensionsUnknown(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBody(t, "in.png", "webp", truncatedIHDRPNGFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !strings.Contains(resp["error"], "cannot determine declared image dimensions") {
		t.Errorf("error = %q, want a message about undeterminable declared dimensions", resp["error"])
	}
	if store.uploaded {
		t.Error("must not upload when declared dimensions cannot be determined")
	}
	if repo.created != nil {
		t.Error("must not create job when declared dimensions cannot be determined")
	}
}

// TestCreateJob_DocumentDetectedAndAccepted verifies a well-formed docx is
// structurally detected before any storage write, passes the pair-check, and
// is admitted on the protocol path with Engine="document".
func TestCreateJob_DocumentDetectedAndAccepted(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	queue := &fakeQueue{}
	srv, _ := newTestServer(repo, store, queue)
	srv.SetCapabilityCatalog(documentCatalog())

	body, ct := multipartBody(t, "in.docx", "pdf", docxFixture(t))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if !store.uploaded {
		t.Error("must upload a supported document pair")
	}
	if store.contentType != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Errorf("contentType = %q, want the canonical docx MIME type (D-06 parity with image jobs)", store.contentType)
	}
	if repo.created == nil {
		t.Fatal("must create job for a supported document pair")
	}
	if repo.created.SourceFormat != "docx" || repo.created.TargetFormat != "pdf" {
		t.Errorf("job format = %s -> %s, want docx -> pdf", repo.created.SourceFormat, repo.created.TargetFormat)
	}
	if repo.created.Engine != "document" {
		t.Errorf("job Engine = %q, want document", repo.created.Engine)
	}
	if repo.created.DispatchOwner != "protocol" {
		t.Errorf("dispatch = %q, want protocol", repo.created.DispatchOwner)
	}
	if queue.enqueuedDocument != uuid.Nil {
		t.Errorf("enqueuedDocument = %s, want uuid.Nil", queue.enqueuedDocument)
	}
	if queue.enqueuedImage != uuid.Nil {
		t.Errorf("enqueuedImage = %s, want uuid.Nil (document upload must never touch the image queue)", queue.enqueuedImage)
	}
}

// TestCreateJob_ODFDetectedAndAccepted proves the ODF disambiguation path
// (index-0 mimetype check) is reached and produces the same
// detected-and-accepted outcome as OOXML.
func TestCreateJob_ODFDetectedAndAccepted(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	queue := &fakeQueue{}
	srv, _ := newTestServer(repo, store, queue)
	srv.SetCapabilityCatalog(&fakeCatalog{
		allow: func(operation, source, target string) (workerproto.Match, bool) {
			if operation == convert.OperationConvert && source == "odt" && target == "pdf" {
				return workerproto.Match{
					CapabilityID:      "octoconv.convert.document",
					CapabilityVersion: 1,
					ClientOperation:   convert.OperationConvert,
					LegacyOperation:   convert.OperationConvert,
					LegacyEngine:      convert.EngineDocument,
					ParameterSchema:   []byte(`{}`),
				}, true
			}
			return workerproto.Match{}, false
		},
	})

	body, ct := multipartBody(t, "in.odt", "pdf", odtFixture(t))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if !store.uploaded {
		t.Error("must upload a supported document pair")
	}
	if store.contentType != "application/vnd.oasis.opendocument.text" {
		t.Errorf("contentType = %q, want the canonical odt MIME type (D-06 parity with image jobs)", store.contentType)
	}
	if repo.created == nil {
		t.Fatal("must create job for a supported document pair")
	}
	if repo.created.SourceFormat != "odt" || repo.created.TargetFormat != "pdf" {
		t.Errorf("job format = %s -> %s, want odt -> pdf", repo.created.SourceFormat, repo.created.TargetFormat)
	}
	if repo.created.Engine != "document" {
		t.Errorf("job Engine = %q, want document", repo.created.Engine)
	}
	if repo.created.DispatchOwner != "protocol" {
		t.Errorf("dispatch = %q, want protocol", repo.created.DispatchOwner)
	}
	if queue.enqueuedDocument != uuid.Nil {
		t.Errorf("enqueuedDocument = %s, want uuid.Nil", queue.enqueuedDocument)
	}
	if queue.enqueuedImage != uuid.Nil {
		t.Errorf("enqueuedImage = %s, want uuid.Nil (document upload must never touch the image queue)", queue.enqueuedImage)
	}
}

// TestCreateJob_DocumentSkipsDimensionCheck verifies D-06: HasDimensionLimit
// scopes the pixel-dimension check to image formats only, so a document
// upload is accepted even under a MaxImagePixels limit that would reject any
// real image (proving the image-only check is never reached for documents).
func TestCreateJob_DocumentSkipsDimensionCheck(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	resolver := newFakeResolver()
	srv := NewServer(repo, store, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20, MaxImagePixels: 1})
	srv.SetCapabilityCatalog(documentCatalog())

	body, ct := multipartBody(t, "in.docx", "pdf", docxFixture(t))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (documents must skip the pixel-dimension check); body=%s", rec.Code, rec.Body.String())
	}
}

// TestCreateJob_ZipBombRejected verifies DOC-02/D-04: a docx-shaped upload
// whose declared uncompressed total exceeds the configured
// MaxDocumentUncompressedBytes limit is rejected 422 before any storage
// write, even though the actual compressed bytes transmitted are tiny.
func TestCreateJob_ZipBombRejected(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	resolver := newFakeResolver()
	srv := NewServer(repo, store, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
		MaxUploadBytes:               1 << 20,
		MaxDocumentUncompressedBytes: 1 << 20, // 1 MiB test limit
	})

	body, ct := multipartBody(t, "in.docx", "pdf", zipBombDocxFixture(t, 2<<20)) // declares 2 MiB uncompressed
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload a zip-bomb-shaped document before the size check")
	}
	if repo.created != nil {
		t.Error("must not create job for a zip-bomb-shaped document")
	}
}

// TestCreateJob_MacroRejected verifies DOC-03/D-05: a docx containing a
// macro part is rejected 422, unconditionally, before any storage write.
func TestCreateJob_MacroRejected(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBody(t, "in.docx", "pdf", macroDocxFixture(t))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload a macro-carrying document")
	}
	if repo.created != nil {
		t.Error("must not create job for a macro-carrying document")
	}
}

// TestCreateJob_DuplicateRootPartRejected verifies the fail-closed guard: a
// docx-shaped zip with two word/document.xml entries is NOT accepted as
// docx -- it falls through to the existing unrecognized-content 422.
func TestCreateJob_DuplicateRootPartRejected(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBody(t, "in.docx", "pdf", duplicateRootPartDocxFixture(t))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload a duplicate-root-part document")
	}
	if repo.created != nil {
		t.Error("must not create job for a duplicate-root-part document")
	}
}

// TestCreateJob_BareZipUnrecognized verifies T-08-07: a plain .zip with no
// office root parts is PK-prefixed but must still 422 as unrecognized
// content, never silently accepted.
func TestCreateJob_BareZipUnrecognized(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBody(t, "in.zip", "pdf", bareZipFixture(t))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload a bare zip with no office root parts")
	}
	if repo.created != nil {
		t.Error("must not create job for a bare zip with no office root parts")
	}
}

func TestCreateJob_TooLarge(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	big := make([]byte, (1<<20)+1024) // exceed 1 MiB test limit
	body, ct := multipartBody(t, "in.png", "webp", big)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestHealthz_NoAuthRequired(t *testing.T) {
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil) // deliberately no Authorization header
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (D-09: /healthz must stay reachable without a key)", rec.Code)
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "ok" || resp["postgres"] != "ok" || resp["redis"] != "ok" || resp["s3"] != "ok" {
		t.Fatalf("unexpected healthy body: %v", resp)
	}
}

// TestHealthz_Degraded verifies OBS-02/D-16/D-17: a failing dependency ping
// causes a 503 with per-dependency detail, while the other two dependencies
// still report "ok".
func TestHealthz_Degraded(t *testing.T) {
	cases := []struct {
		name   string
		health HealthDeps
		failed string
	}{
		{
			name:   "s3 unreachable",
			health: HealthDeps{Postgres: fakePinger{}, Redis: fakePinger{}, S3: fakePinger{err: errors.New("boom")}},
			failed: "s3",
		},
		{
			name:   "redis unreachable",
			health: HealthDeps{Postgres: fakePinger{}, Redis: fakePinger{err: errors.New("boom")}, S3: fakePinger{}},
			failed: "redis",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolver := newFakeResolver()
			srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, tc.health, Config{MaxUploadBytes: 1 << 20})

			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
			}
			var resp map[string]string
			_ = json.Unmarshal(rec.Body.Bytes(), &resp)
			if resp["status"] != "degraded" {
				t.Errorf("status field = %q, want degraded", resp["status"])
			}
			if resp[tc.failed] == "ok" {
				t.Errorf("%s = %q, want non-ok", tc.failed, resp[tc.failed])
			}
			for _, dep := range []string{"postgres", "redis", "s3"} {
				if dep == tc.failed {
					continue
				}
				if resp[dep] != "ok" {
					t.Errorf("%s = %q, want ok", dep, resp[dep])
				}
			}
		})
	}
}

func TestGetJob_DonePresigned(t *testing.T) {
	id := uuid.New()
	resolver := newFakeResolver()
	repo := &fakeRepo{
		getJob: &jobs.Job{ID: id, ClientID: resolver.client.ID, Status: jobs.StatusDone},
		// Role: "primary" (57-06): this fixture predates jobs.Output.Role
		// (added by plan 57-03); handleGetJob now selects download_url by
		// role, not by outs[0], so this pre-existing test needs the same
		// fixture fix as the golden-pinning suite's exception #2 to keep
		// testing what it claims (that a done job's single output is
		// presigned into download_url).
		outputs: []jobs.Output{{Ordinal: 0, ObjectKey: "results/x/0-out.webp", Role: "primary"}},
	}
	store := &fakeStorage{presigned: "https://example/download"}
	srv := NewServer(repo, store, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id.String(), nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != jobs.StatusDone || resp["download_url"] != "https://example/download" {
		t.Fatalf("unexpected response: %v", resp)
	}
}

// TestGetJob_DownloadURLResolvesToPrimaryNotOrdinalZero is the core D-18/
// ROLE-02 proof: two outputs in a deliberately "wrong" order -- a report at
// ordinal 0, the primary result at ordinal 1 -- so a bug that picks outs[0]
// instead of the role='primary' output is caught by a POSITIVE assertion
// (download_url equals the primary output's own URL), not merely by
// asserting it isn't the report's URL. T-57-24 in the plan's threat register
// names exactly this defect class.
func TestGetJob_DownloadURLResolvesToPrimaryNotOrdinalZero(t *testing.T) {
	id := uuid.New()
	resolver := newFakeResolver()
	reportKey := "results/x/0-report.json"
	primaryKey := "results/x/1-out.webp"
	repo := &fakeRepo{
		getJob: &jobs.Job{ID: id, ClientID: resolver.client.ID, Status: jobs.StatusDone},
		outputs: []jobs.Output{
			{Ordinal: 0, ObjectKey: reportKey, Format: "json", SizeBytes: 111, Role: "report"},
			{Ordinal: 1, ObjectKey: primaryKey, Format: "webp", SizeBytes: 222, Role: "primary"},
		},
	}
	store := &fakeStorage{
		presignByKey: map[string]string{
			reportKey:  "https://example/report",
			primaryKey: "https://example/primary",
		},
	}
	srv := NewServer(repo, store, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id.String(), nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, rec.Body.String())
	}

	// Positive assertion: download_url must equal the PRIMARY output's own
	// presigned URL -- not merely "not the report's URL".
	if resp["download_url"] != "https://example/primary" {
		t.Fatalf("download_url = %v, want the primary output's url (https://example/primary)", resp["download_url"])
	}

	rawOutputs, ok := resp["outputs"]
	if !ok {
		t.Fatalf("response missing outputs key; body=%s", rec.Body.String())
	}
	outputsList, ok := rawOutputs.([]any)
	if !ok || len(outputsList) != 2 {
		t.Fatalf("outputs = %v, want a 2-element list", rawOutputs)
	}

	var sawReport, sawPrimary bool
	for _, raw := range outputsList {
		item, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("outputs element is not an object: %v", raw)
		}
		if url, _ := item["url"].(string); url == "" {
			t.Fatalf("outputs element missing url: %v", item)
		}
		if format, _ := item["format"].(string); format == "" {
			t.Fatalf("outputs element missing format: %v", item)
		}
		if _, ok := item["size_bytes"]; !ok {
			t.Fatalf("outputs element missing size_bytes: %v", item)
		}
		switch item["role"] {
		case "report":
			sawReport = true
			if item["ordinal"] != float64(0) {
				t.Errorf("report ordinal = %v, want 0", item["ordinal"])
			}
		case "primary":
			sawPrimary = true
			if item["ordinal"] != float64(1) {
				t.Errorf("primary ordinal = %v, want 1", item["ordinal"])
			}
		default:
			t.Fatalf("unexpected role in outputs element: %v", item)
		}
	}
	if !sawReport || !sawPrimary {
		t.Fatalf("outputs missing one of report/primary roles: %v", outputsList)
	}
}

func TestGetJob_DownloadURLNeverMeasurements(t *testing.T) {
	get := func(t *testing.T, outputs []jobs.Output, urls map[string]string) map[string]any {
		t.Helper()
		id := uuid.New()
		resolver := newFakeResolver()
		repo := &fakeRepo{
			getJob:  &jobs.Job{ID: id, ClientID: resolver.client.ID, Status: jobs.StatusDone},
			outputs: outputs,
		}
		store := &fakeStorage{presignByKey: urls}
		srv := NewServer(repo, store, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})

		req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id.String(), nil))
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v; body=%s", err, rec.Body.String())
		}
		return resp
	}

	t.Run("measurements_sorts_before_primary", func(t *testing.T) {
		measKey := "results/x/0-measurements.json"
		primaryKey := "results/x/1-out.png"
		resp := get(t, []jobs.Output{
			{Ordinal: 0, ObjectKey: measKey, Format: "json", SizeBytes: 111, Role: jobs.OutputRoleMeasurements},
			{Ordinal: 1, ObjectKey: primaryKey, Format: "png", SizeBytes: 222, Role: jobs.OutputRolePrimary},
		}, map[string]string{
			measKey:    "https://example/measurements",
			primaryKey: "https://example/primary",
		})

		if resp["download_url"] != "https://example/primary" {
			t.Fatalf("download_url = %v, want the primary output's url (https://example/primary)", resp["download_url"])
		}
		outputsList, ok := resp["outputs"].([]any)
		if !ok || len(outputsList) != 2 {
			t.Fatalf("outputs = %v, want a 2-element list", resp["outputs"])
		}
		first, ok := outputsList[0].(map[string]any)
		if !ok {
			t.Fatalf("outputs[0] is not an object: %v", outputsList[0])
		}
		if first["role"] != jobs.OutputRoleMeasurements {
			t.Errorf("outputs[0].role = %v, want %q", first["role"], jobs.OutputRoleMeasurements)
		}
		if first["ordinal"] != float64(0) {
			t.Errorf("outputs[0].ordinal = %v, want 0", first["ordinal"])
		}
		if first["url"] != "https://example/measurements" {
			t.Errorf("outputs[0].url = %v, want the measurements output's own url", first["url"])
		}
	})

	t.Run("measurements_after_primary", func(t *testing.T) {
		primaryKey := "results/x/0-out.png"
		measKey := "results/x/1-measurements.json"
		resp := get(t, []jobs.Output{
			{Ordinal: 0, ObjectKey: primaryKey, Format: "png", SizeBytes: 222, Role: jobs.OutputRolePrimary},
			{Ordinal: 1, ObjectKey: measKey, Format: "json", SizeBytes: 111, Role: jobs.OutputRoleMeasurements},
		}, map[string]string{
			measKey:    "https://example/measurements",
			primaryKey: "https://example/primary",
		})

		if resp["download_url"] != "https://example/primary" {
			t.Fatalf("download_url = %v, want the primary output's url (https://example/primary)", resp["download_url"])
		}
		outputsList, ok := resp["outputs"].([]any)
		if !ok || len(outputsList) != 2 {
			t.Fatalf("outputs = %v, want a 2-element list", resp["outputs"])
		}
		second, _ := outputsList[1].(map[string]any)
		if second["role"] != jobs.OutputRoleMeasurements || second["ordinal"] != float64(1) {
			t.Errorf("outputs[1] = %v, want the measurements row at ordinal 1", second)
		}
	})

	t.Run("only_measurements_has_no_download_url", func(t *testing.T) {
		measKey := "results/x/0-measurements.json"
		resp := get(t, []jobs.Output{
			{Ordinal: 0, ObjectKey: measKey, Format: "json", SizeBytes: 111, Role: jobs.OutputRoleMeasurements},
		}, map[string]string{measKey: "https://example/measurements"})

		if v, ok := resp["download_url"]; ok {
			t.Fatalf("download_url = %v, want the key absent when the only output is measurements", v)
		}
	})
}

// TestGetJob_SingleOutputHasNoOutputsKey pins SC4: a done job with exactly
// one output has no "outputs" key at all (not an empty array), keeping a
// single-output convert job's response byte-shape unchanged from
// pre-Phase-57 -- while download_url is still present via the role-driven
// selection.
func TestGetJob_SingleOutputHasNoOutputsKey(t *testing.T) {
	id := uuid.New()
	resolver := newFakeResolver()
	repo := &fakeRepo{
		getJob:  &jobs.Job{ID: id, ClientID: resolver.client.ID, Status: jobs.StatusDone},
		outputs: []jobs.Output{{Ordinal: 0, ObjectKey: "results/x/0-out.webp", Role: "primary"}},
	}
	store := &fakeStorage{presigned: "https://example/download"}
	srv := NewServer(repo, store, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id.String(), nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, rec.Body.String())
	}
	if _, ok := resp["outputs"]; ok {
		t.Fatalf("response has an outputs key for a single-output job; body=%s", rec.Body.String())
	}
	if resp["download_url"] != "https://example/download" {
		t.Fatalf("download_url = %v, want https://example/download", resp["download_url"])
	}
}

// TestGetJob_AllOutputsPresignedWithSameTTL pins D-12: every output of a
// done job is presigned eager, in one round trip per output, all with the
// server's configured presignTTL -- never a per-role or per-ordinal
// different TTL.
func TestGetJob_AllOutputsPresignedWithSameTTL(t *testing.T) {
	id := uuid.New()
	resolver := newFakeResolver()
	repo := &fakeRepo{
		getJob: &jobs.Job{ID: id, ClientID: resolver.client.ID, Status: jobs.StatusDone},
		outputs: []jobs.Output{
			{Ordinal: 0, ObjectKey: "results/x/0-out.webp", Role: "primary"},
			{Ordinal: 1, ObjectKey: "results/x/1-report.json", Role: "report"},
			{Ordinal: 2, ObjectKey: "results/x/2-preview.webp", Role: "preview"},
		},
	}
	store := &fakeStorage{presigned: "https://example/download"}
	wantTTL := 7 * time.Minute
	srv := NewServer(repo, store, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20, PresignTTL: wantTTL})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id.String(), nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if store.presignCalls != 3 {
		t.Fatalf("PresignGet called %d times, want 3 (one per output)", store.presignCalls)
	}
	for i, ttl := range store.presignTTLs {
		if ttl != wantTTL {
			t.Errorf("PresignGet call %d ttl = %v, want %v (server's configured presignTTL)", i, ttl, wantTTL)
		}
	}
}

// TestGetJob_ActiveJobPresignsNothing pins D-12's other half: polling an
// active (non-terminal) job makes zero PresignGet calls -- presigning lives
// exclusively inside case jobs.StatusDone.
func TestGetJob_ActiveJobPresignsNothing(t *testing.T) {
	id := uuid.New()
	resolver := newFakeResolver()
	repo := &fakeRepo{
		getJob: &jobs.Job{ID: id, ClientID: resolver.client.ID, Status: jobs.StatusActive},
	}
	store := &fakeStorage{}
	srv := NewServer(repo, store, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id.String(), nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if store.presignCalls != 0 {
		t.Fatalf("PresignGet called %d times, want 0 (an active job must never presign, D-12)", store.presignCalls)
	}
}

// TestGetJob_ProgressSurfaced pins PROG-01/PROG-04: an active av job with a
// non-nil ProgressPct/EtaSeconds surfaces the real integers.
func TestGetJob_ProgressSurfaced(t *testing.T) {
	id := uuid.New()
	resolver := newFakeResolver()
	pct, eta := 42, 17
	repo := &fakeRepo{getJob: &jobs.Job{
		ID: id, ClientID: resolver.client.ID, Status: jobs.StatusActive,
		Engine: convert.EngineAV, ProgressPct: &pct, EtaSeconds: &eta,
	}}
	srv := NewServer(repo, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id.String(), nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if got, ok := resp["progress_pct"].(float64); !ok || int(got) != 42 {
		t.Fatalf("progress_pct = %v, want 42", resp["progress_pct"])
	}
	if got, ok := resp["eta_seconds"].(float64); !ok || int(got) != 17 {
		t.Fatalf("eta_seconds = %v, want 17", resp["eta_seconds"])
	}
}

// TestGetJob_ProgressNullForNonProgressEngine pins the "never fabricated"
// domain boundary: a job with nil ProgressPct/EtaSeconds (every non-av
// engine, or an av job before its first tick) surfaces a real JSON null for
// both fields — never a fabricated 0 or absent key.
func TestGetJob_ProgressNullForNonProgressEngine(t *testing.T) {
	id := uuid.New()
	resolver := newFakeResolver()
	repo := &fakeRepo{getJob: &jobs.Job{
		ID: id, ClientID: resolver.client.ID, Status: jobs.StatusActive, Engine: convert.EngineImage,
	}}
	srv := NewServer(repo, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id.String(), nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	pctVal, pctPresent := resp["progress_pct"]
	if !pctPresent || pctVal != nil {
		t.Fatalf("progress_pct = %v (present=%v), want present and null", pctVal, pctPresent)
	}
	etaVal, etaPresent := resp["eta_seconds"]
	if !etaPresent || etaVal != nil {
		t.Fatalf("eta_seconds = %v (present=%v), want present and null", etaVal, etaPresent)
	}
}

// TestGetJob_ProgressFrozenOnCancel pins D-06: a canceled-mid-stream job
// surfaces its frozen last progress value — not null, not 100.
func TestGetJob_ProgressFrozenOnCancel(t *testing.T) {
	id := uuid.New()
	resolver := newFakeResolver()
	pct := 63
	repo := &fakeRepo{getJob: &jobs.Job{
		ID: id, ClientID: resolver.client.ID, Status: jobs.StatusCanceled,
		Engine: convert.EngineAV, ProgressPct: &pct,
	}}
	srv := NewServer(repo, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id.String(), nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	got, ok := resp["progress_pct"].(float64)
	if !ok || int(got) != 63 {
		t.Fatalf("progress_pct = %v, want frozen 63 (not null, not 100)", resp["progress_pct"])
	}
}

func TestGetJob_NotFound(t *testing.T) {
	repo := &fakeRepo{getErr: jobs.ErrNotFound}
	srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+uuid.New().String(), nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGetJob_CrossClient_NotFound(t *testing.T) {
	id := uuid.New()
	otherClientID := uuid.New()
	repo := &fakeRepo{getJob: &jobs.Job{ID: id, ClientID: otherClientID, Status: jobs.StatusQueued}}
	srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id.String(), nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["error"] != "job not found" {
		t.Fatalf(`error = %q, want "job not found" (identical to true-not-found)`, resp["error"])
	}
}

// --- cancel tests (CNCL-01/CNCL-02, D-02/D-09, T-39-01/T-39-02) ---

// assertNeverForbiddenOrConflict is a shared guard for every cancel test:
// the handler must never return 403 or 409 (D-02/D-09) — cross-client is
// 404-not-403, and any race/idempotency case is 200, never a conflict.
func assertNeverForbiddenOrConflict(t *testing.T, code int) {
	t.Helper()
	if code == http.StatusForbidden || code == http.StatusConflict {
		t.Fatalf("status = %d, must never be 403 or 409", code)
	}
}

func TestCancelJob_QueuedOK(t *testing.T) {
	id := uuid.New()
	resolver := newFakeResolver()
	repo := &fakeRepo{getJob: &jobs.Job{ID: id, ClientID: resolver.client.ID, Status: jobs.StatusQueued}}
	srv := NewServer(repo, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs/"+id.String()+"/cancel", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	assertNeverForbiddenOrConflict(t, rec.Code)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != jobs.StatusCanceled {
		t.Fatalf("status field = %v, want %q", resp["status"], jobs.StatusCanceled)
	}
	if !repo.canceled {
		t.Error("expected Repo.Cancel to be called for a queued job")
	}
}

func TestCancelJob_ActiveOK(t *testing.T) {
	id := uuid.New()
	resolver := newFakeResolver()
	repo := &fakeRepo{getJob: &jobs.Job{ID: id, ClientID: resolver.client.ID, Status: jobs.StatusActive}}
	srv := NewServer(repo, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs/"+id.String()+"/cancel", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	assertNeverForbiddenOrConflict(t, rec.Code)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != jobs.StatusCanceled {
		t.Fatalf("status field = %v, want %q", resp["status"], jobs.StatusCanceled)
	}
	if !repo.canceled {
		t.Error("expected Repo.Cancel to be called for an active job")
	}
}

// TestCancelJob_CrossClient_NotFound verifies T-39-01: canceling a job owned
// by another client returns 404 with the identical message as true-not-found
// — never 403.
func TestCancelJob_CrossClient_NotFound(t *testing.T) {
	id := uuid.New()
	otherClientID := uuid.New()
	repo := &fakeRepo{getJob: &jobs.Job{ID: id, ClientID: otherClientID, Status: jobs.StatusQueued}}
	srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})

	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs/"+id.String()+"/cancel", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	assertNeverForbiddenOrConflict(t, rec.Code)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["error"] != "job not found" {
		t.Fatalf(`error = %q, want "job not found" (identical to true-not-found)`, resp["error"])
	}
	if repo.canceled {
		t.Error("expected Repo.Cancel to NOT be called for a cross-client job")
	}
}

func TestCancelJob_UnknownID_NotFound(t *testing.T) {
	repo := &fakeRepo{getErr: jobs.ErrNotFound}
	srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})

	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs/"+uuid.New().String()+"/cancel", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	assertNeverForbiddenOrConflict(t, rec.Code)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestCancelJob_InvalidID_BadRequest(t *testing.T) {
	repo := &fakeRepo{}
	srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})

	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs/not-a-uuid/cancel", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	assertNeverForbiddenOrConflict(t, rec.Code)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestCancelJob_AlreadyTerminal_Idempotent verifies D-02: an already-done
// job returns 200 with its ORIGINAL status and Repo.Cancel is NEVER called
// (no re-transition attempted).
func TestCancelJob_AlreadyTerminal_Idempotent(t *testing.T) {
	id := uuid.New()
	resolver := newFakeResolver()
	repo := &fakeRepo{getJob: &jobs.Job{ID: id, ClientID: resolver.client.ID, Status: jobs.StatusDone}}
	srv := NewServer(repo, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs/"+id.String()+"/cancel", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	assertNeverForbiddenOrConflict(t, rec.Code)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != jobs.StatusDone {
		t.Fatalf("status field = %v, want %q (original status, unchanged)", resp["status"], jobs.StatusDone)
	}
	if repo.canceled {
		t.Error("expected Repo.Cancel to NOT be called for an already-terminal job")
	}
}

// TestCancelJob_TerminalRace verifies D-02: when Cancel returns an illegal-
// transition-style error because the job reached done between the read and
// the transition, the handler re-reads and returns 200 with the now-current
// status — never a 409/500.
func TestCancelJob_TerminalRace(t *testing.T) {
	id := uuid.New()
	resolver := newFakeResolver()
	repo := &fakeRepo{
		getJob:            &jobs.Job{ID: id, ClientID: resolver.client.ID, Status: jobs.StatusActive},
		cancelErr:         errors.New("illegal transition active -> canceled for job " + id.String()),
		getJobAfterCancel: &jobs.Job{ID: id, ClientID: resolver.client.ID, Status: jobs.StatusDone},
	}
	srv := NewServer(repo, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs/"+id.String()+"/cancel", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	assertNeverForbiddenOrConflict(t, rec.Code)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != jobs.StatusDone {
		t.Fatalf("status field = %v, want %q (the now-current status)", resp["status"], jobs.StatusDone)
	}
	if !repo.canceled {
		t.Error("expected Repo.Cancel to have been called")
	}
}

// TestCancelJob_NoAuthHeader_Unauthorized mirrors
// TestCreateJob_NoAuthHeader_Unauthorized: the cancel route requires auth
// like every other /v1 route.
func TestCancelJob_NoAuthHeader_Unauthorized(t *testing.T) {
	repo := &fakeRepo{}
	srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})

	req := httptest.NewRequest(http.MethodPost, "/v1/jobs/"+uuid.New().String()+"/cancel", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	assertNeverForbiddenOrConflict(t, rec.Code)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if repo.canceled {
		t.Error("must not cancel when unauthenticated")
	}
}

// fakeCancelPublisher records Publish calls for TestCancelJob_PublishesOnSuccess.
type fakeCancelPublisher struct {
	published uuid.UUID
	err       error
}

func (f *fakeCancelPublisher) Publish(_ context.Context, jobID uuid.UUID) error {
	f.published = jobID
	return f.err
}

// TestCancelJob_PublishesOnSuccess verifies the nil-safe CancelPublisher
// seam: when set via SetCancelPublisher, a successful cancel best-effort
// publishes the job id (D-09, Plan 03's speed-layer seam).
func TestCancelJob_PublishesOnSuccess(t *testing.T) {
	id := uuid.New()
	resolver := newFakeResolver()
	repo := &fakeRepo{getJob: &jobs.Job{ID: id, ClientID: resolver.client.ID, Status: jobs.StatusQueued}}
	srv := NewServer(repo, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})
	pub := &fakeCancelPublisher{}
	srv.SetCancelPublisher(pub)

	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs/"+id.String()+"/cancel", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if pub.published != id {
		t.Fatalf("published = %s, want %s", pub.published, id)
	}
}

// --- opts tests (OPTS-01/OPTS-02, D-02/D-03/D-04/D-08/D-09) ---

// TestCreateJob_DocumentOptsRefused verifies pdf_profile on docx->pdf is
// refused before upload. Protocol v1 cannot carry the option.
func TestCreateJob_DocumentOptsRefused(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	queue := &fakeQueue{}
	srv, _ := newTestServer(repo, store, queue)

	body, ct := multipartBodyWithOpts(t, "in.docx", "pdf", docxFixture(t), `{"pdf_profile":"pdf/a-2b"}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec.Body.Bytes(), errcode.OptsNotApplicable)
	if store.uploaded || repo.created != nil || queue.enqueueCalls != 0 {
		t.Fatalf("refused opts uploaded=%v created=%v enqueueCalls=%d", store.uploaded, repo.created != nil, queue.enqueueCalls)
	}
}

// TestCreateJob_OptsUnknownKeyRejected verifies an opts value with an
// unrecognized key is rejected 422 before any storage write (T-14-01).
func TestCreateJob_OptsUnknownKeyRejected(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBodyWithOpts(t, "in.docx", "pdf", docxFixture(t), `{"EncryptFile":true}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload before opts validation")
	}
	if repo.created != nil {
		t.Error("must not create job for an unknown-key opts value")
	}
}

// TestCreateJob_OptsInapplicableImage verifies pdf_profile requested on an
// image conversion (png->webp) is rejected 422 (inapplicable).
func TestCreateJob_OptsInapplicableImage(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBodyWithOpts(t, "in.png", "webp", pngBytesFixture(), `{"pdf_profile":"pdf/a-2b"}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload before opts applicability validation")
	}
	if repo.created != nil {
		t.Error("must not create job for inapplicable opts on an image conversion")
	}
}

// TestCreateJob_OptsInapplicableTarget verifies pdf_profile requested on a
// docx->odt request (non-pdf target) is rejected 422 (inapplicable).
func TestCreateJob_OptsInapplicableTarget(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBodyWithOpts(t, "in.docx", "odt", docxFixture(t), `{"pdf_profile":"pdf/a-2b"}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload before opts applicability validation")
	}
	if repo.created != nil {
		t.Error("must not create job for pdf_profile on a non-pdf target")
	}
}

// TestCreateJob_OptsOversizeRejected verifies an opts field larger than the
// size cap is rejected 422 before parsing (T-14-02). The payload is padded
// with JSON whitespace around a fully VALID pdf_profile, so ONLY the size
// cap can reject it — if the len(rawOpts) > maxOptsBytes guard were deleted,
// the opts would parse and validate cleanly and this test would fail
// (review WR-05: the previous payload also failed the enum allow-list,
// silently masking removal of the cap).
func TestCreateJob_OptsOversizeRejected(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	oversized := `{` + strings.Repeat(" ", 5000) + `"pdf_profile":"pdf/a-2b"}`
	body, ct := multipartBodyWithOpts(t, "in.docx", "pdf", docxFixture(t), oversized)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload an oversized opts field")
	}
	if repo.created != nil {
		t.Error("must not create job for an oversized opts field")
	}
}

// TestCreateJob_OptsInjectionAttempt verifies an API-level attempt to smuggle
// a second filter property inside the pdf_profile value itself is rejected
// 422 -- the closed allow-list check in ParseDocOpts rejects anything other
// than the exact "pdf/a-2b" string, proving adversarial bytes never reach
// storage or the CreateParams.Opts persisted value (T-14-01).
func TestCreateJob_OptsInjectionAttempt(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	injection := `{"pdf_profile":"pdf/a-2b\",\"EncryptFile\":true"}`
	body, ct := multipartBodyWithOpts(t, "in.docx", "pdf", docxFixture(t), injection)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload before opts validation rejects the injection attempt")
	}
	if repo.created != nil {
		t.Error("must not create job for an opts injection attempt")
	}
}

// TestCreateJob_NoOptsResponseUnchanged is the D-09 regression: a no-opts
// POST response contains no "opts" key and CreateParams.Opts stays nil.
func TestCreateJob_NoOptsResponseUnchanged(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})
	srv.SetCapabilityCatalog(imageCatalog())

	body, ct := multipartBody(t, "in.png", "webp", pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if repo.created == nil || repo.created.Opts != nil {
		t.Errorf("CreateParams.Opts = %+v, want nil (no opts field supplied)", repo.created)
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if _, present := resp["opts"]; present {
		t.Errorf("response = %v, want no \"opts\" key when no opts were supplied", resp)
	}
}

// TestCreateJob_EmptyOptsObjectTreatedAsNoOpts verifies the literal "{}"
// opts value is treated identically to an absent opts field (D-09).
func TestCreateJob_EmptyOptsObjectTreatedAsNoOpts(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})
	srv.SetCapabilityCatalog(imageCatalog())

	body, ct := multipartBodyWithOpts(t, "in.png", "webp", pngBytesFixture(), "{}")
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if _, present := resp["opts"]; present {
		t.Errorf("response = %v, want no \"opts\" key for opts={}", resp)
	}
}

// TestGetJob_OptsEcho verifies a job with stored opts echoes them in the GET
// response (D-09).
func TestGetJob_OptsEcho(t *testing.T) {
	id := uuid.New()
	resolver := newFakeResolver()
	repo := &fakeRepo{getJob: &jobs.Job{
		ID:       id,
		ClientID: resolver.client.ID,
		Status:   jobs.StatusQueued,
		Opts:     map[string]any{"pdf_profile": "pdf/a-2b"},
	}}
	srv := NewServer(repo, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id.String(), nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	optsResp, ok := resp["opts"].(map[string]any)
	if !ok || optsResp["pdf_profile"] != "pdf/a-2b" {
		t.Errorf("response opts = %v, want echoed pdf_profile=pdf/a-2b", resp["opts"])
	}
}

// TestGetJob_NoOptsOmitted verifies a job without stored opts omits the
// "opts" key entirely (D-09 omitempty; existing no-opts responses unchanged).
func TestGetJob_NoOptsOmitted(t *testing.T) {
	id := uuid.New()
	resolver := newFakeResolver()
	repo := &fakeRepo{getJob: &jobs.Job{ID: id, ClientID: resolver.client.ID, Status: jobs.StatusQueued}}
	srv := NewServer(repo, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id.String(), nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if _, present := resp["opts"]; present {
		t.Errorf("response = %v, want no \"opts\" key for a job without stored opts", resp)
	}
}

func TestGetJob_SameClient_OK(t *testing.T) {
	id := uuid.New()
	resolver := newFakeResolver()
	repo := &fakeRepo{getJob: &jobs.Job{ID: id, ClientID: resolver.client.ID, Status: jobs.StatusQueued}}
	srv := NewServer(repo, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id.String(), nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// --- html engine tests (Plan 03: HTML-01/02/03) ---

// htmlFixture returns a small, genuinely-HTML document (valid UTF-8, no NUL,
// starts with <!doctype html> after trimming) that convert.LooksLikeHTML
// accepts.
func htmlFixture() []byte {
	return []byte("<!doctype html><html><head></head><body><p>hello</p></body></html>")
}

// TestCreateJob_HTMLDetectedAndAccepted proves a valid .html upload targeting
// pdf is detected and refused before upload when no html manifest is approved.
func TestCreateJob_HTMLDetectedAndAccepted(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	queue := &fakeQueue{}
	srv, _ := newTestServer(repo, store, queue)

	body, ct := multipartBody(t, "in.html", "pdf", htmlFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "html -> pdf") {
		t.Fatalf("body = %s, want html -> pdf", rec.Body.String())
	}
	if store.uploaded || repo.created != nil || queue.enqueueCalls != 0 || queue.enqueuedHTML != uuid.Nil {
		t.Fatalf("refused html uploaded=%v created=%v enqueueCalls=%d", store.uploaded, repo.created != nil, queue.enqueueCalls)
	}
}

// TestCreateJob_HTMExtensionAliasAccepted proves the htm->html NormalizeFormat
// alias (Plan 01) lets a .htm-named upload with genuine HTML content route
// the same way a .html-named upload does.
func TestCreateJob_HTMExtensionAliasAccepted(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	queue := &fakeQueue{}
	srv, _ := newTestServer(repo, store, queue)

	body, ct := multipartBody(t, "in.htm", "pdf", htmlFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "html -> pdf") {
		t.Fatalf("body = %s, want html -> pdf (htm alias detected as html)", rec.Body.String())
	}
	if store.uploaded || repo.created != nil || queue.enqueueCalls != 0 {
		t.Fatalf("refused htm uploaded=%v created=%v enqueueCalls=%d", store.uploaded, repo.created != nil, queue.enqueueCalls)
	}
}

// TestCreateJob_HTMLContentRejected proves a .html-named upload whose content
// fails LooksLikeHTML (binary/non-HTML content masquerading as html, T-15-09)
// is rejected 422 BEFORE any storage write -- fail-closed, D-07.
func TestCreateJob_HTMLContentRejected(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	// Binary garbage, NOT valid HTML text (contains NUL bytes).
	garbage := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0x00, 0x00}
	body, ct := multipartBody(t, "in.html", "pdf", garbage)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload content that fails the HTML content check")
	}
	if repo.created != nil {
		t.Error("must not create job for content that fails the HTML content check")
	}
}

// TestCreateJob_HTMLOptsAccepted verifies a valid page_size/margin_mm/
// landscape/print_background opts payload is validated, persisted
// (normalized), and echoed in the response (HTML-03).
func TestCreateJob_HTMLOptsAccepted(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	queue := &fakeQueue{}
	srv, _ := newTestServer(repo, store, queue)
	srv.SetCapabilityCatalog(htmlCatalog())

	body, ct := multipartBodyWithOpts(t, "in.html", "pdf", htmlFixture(), `{"page_size":"a4","margin_mm":10,"landscape":true,"print_background":true}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if repo.created == nil {
		t.Fatal("must create job for a valid html opts payload")
	}
	if repo.created.Opts["page_size"] != "a4" {
		t.Errorf("CreateParams.Opts = %+v, want page_size=a4", repo.created.Opts)
	}
	if repo.created.DispatchOwner != "protocol" || queue.enqueueCalls != 0 {
		t.Fatalf("created = %+v enqueueCalls=%d, want protocol and no enqueue", repo.created, queue.enqueueCalls)
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	optsResp, ok := resp["opts"].(map[string]any)
	if !ok || optsResp["page_size"] != "a4" {
		t.Errorf("response opts = %v, want echoed page_size=a4", resp["opts"])
	}
}

// TestCreateJob_HTMLOptsUnknownFieldRejected verifies an unknown opts field
// on an html job is rejected 422 before storage.
func TestCreateJob_HTMLOptsUnknownFieldRejected(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBodyWithOpts(t, "in.html", "pdf", htmlFixture(), `{"unknown_field":true}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload before opts validation")
	}
	if repo.created != nil {
		t.Error("must not create job for an unknown-key html opts value")
	}
}

// TestCreateJob_HTMLOptsMarginOutOfRangeRejected verifies an out-of-range
// margin_mm value on an html job is rejected 422 before storage.
func TestCreateJob_HTMLOptsMarginOutOfRangeRejected(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBodyWithOpts(t, "in.html", "pdf", htmlFixture(), `{"margin_mm":9999}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload before opts validation")
	}
	if repo.created != nil {
		t.Error("must not create job for an out-of-range margin_mm value")
	}
}

// TestCreateJob_DocumentOptsOnHTMLRejected verifies a document-only opts
// value (pdf_profile) requested on an html job is rejected 422 -- proves the
// engine-keyed opts dispatch does not accidentally accept DocOpts fields on
// an html conversion (HTML-03).
func TestCreateJob_DocumentOptsOnHTMLRejected(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBodyWithOpts(t, "in.html", "pdf", htmlFixture(), `{"pdf_profile":"pdf/a-2b"}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload before opts validation")
	}
	if repo.created != nil {
		t.Error("must not create job for a document opts value (pdf_profile) on an html job")
	}
}

// --- preset resolution tests (Plan 18-03: PRST-02/03/04, D-01/D-03/D-06/D-07/D-08, Pitfall 8) ---

// TestCreateJob_PresetResolvedImage verifies PRST-02/D-07/D-08: preset=<name>
// (no target) resolves to the preset's target_format, converts, and persists
// provenance into CreateParams.
func TestCreateJob_PresetResolvedImage(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	resolver := newFakeResolver()
	presetRepo := &fakePresetRepo{resolve: &presets.Preset{
		ID: uuid.New(), Name: "thumb", Version: 2, TargetFormat: "webp",
	}}
	srv := NewServer(repo, store, q, presetRepo, defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})
	srv.SetCapabilityCatalog(imageCatalog())

	body, ct := multipartBodyWithPreset(t, "in.png", "thumb", "", pngBytesFixture(), "")
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if repo.created == nil {
		t.Fatal("expected job to be created")
	}
	if repo.created.TargetFormat != "webp" {
		t.Errorf("TargetFormat = %q, want webp (preset-resolved)", repo.created.TargetFormat)
	}
	if repo.created.PresetName != "thumb" || repo.created.PresetVersion != 2 {
		t.Errorf("provenance = %+v, want PresetName=thumb PresetVersion=2", repo.created)
	}
	if q.enqueuedImage != uuid.Nil {
		t.Errorf("enqueuedImage = %s, want none", q.enqueuedImage)
	}
}

// TestCreateJob_PresetAndTargetMutuallyExclusive verifies D-01: supplying
// preset together with an explicit target is rejected 422, before any
// upload or preset lookup.
func TestCreateJob_PresetAndTargetMutuallyExclusive(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	presetRepo := &fakePresetRepo{resolve: &presets.Preset{ID: uuid.New(), Name: "thumb", Version: 1, TargetFormat: "webp"}}
	resolver := newFakeResolver()
	srv := NewServer(repo, store, &fakeQueue{}, presetRepo, defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	body, ct := multipartBodyWithPreset(t, "in.png", "thumb", "webp", pngBytesFixture(), "")
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload for preset+target")
	}
	if repo.created != nil {
		t.Error("must not create job for preset+target")
	}
	if presetRepo.calls != 0 {
		t.Errorf("presetRepo.calls = %d, want 0 (mutual-exclusivity gate runs before any preset lookup)", presetRepo.calls)
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !strings.Contains(resp["error"], "either preset or target") {
		t.Errorf("error = %q, want a mutual-exclusivity message", resp["error"])
	}
}

// TestCreateJob_PresetAndOptsMutuallyExclusive verifies D-01: supplying
// preset together with a non-empty opts field is rejected 422.
func TestCreateJob_PresetAndOptsMutuallyExclusive(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	presetRepo := &fakePresetRepo{resolve: &presets.Preset{ID: uuid.New(), Name: "thumb", Version: 1, TargetFormat: "webp"}}
	resolver := newFakeResolver()
	srv := NewServer(repo, store, &fakeQueue{}, presetRepo, defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	body, ct := multipartBodyWithPreset(t, "in.png", "thumb", "", pngBytesFixture(), `{"pdf_profile":"pdf/a-2b"}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload for preset+opts")
	}
	if repo.created != nil {
		t.Error("must not create job for preset+opts")
	}
}

// TestCreateJob_UnknownPreset422NoLeak verifies D-03/Pitfall 12: a
// nonexistent preset AND a (simulated) cross-client/inactive preset both
// collapse to ErrNotFound inside Resolve and return the IDENTICAL 422 body
// -- byte-for-byte, not just the same status code.
func TestCreateJob_UnknownPreset422NoLeak(t *testing.T) {
	cases := []struct {
		name       string
		presetRepo *fakePresetRepo
	}{
		{name: "nonexistent", presetRepo: &fakePresetRepo{resolveErr: presets.ErrNotFound}},
		{name: "cross-client-or-inactive", presetRepo: &fakePresetRepo{resolveErr: presets.ErrNotFound}},
	}

	bodies := make([][]byte, 0, len(cases))
	for _, tc := range cases {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		resolver := newFakeResolver()
		srv := NewServer(repo, store, &fakeQueue{}, tc.presetRepo, defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})

		body, ct := multipartBodyWithPreset(t, "in.png", "ghost", "", pngBytesFixture(), "")
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()

		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status = %d, want 422; body=%s", tc.name, rec.Code, rec.Body.String())
		}
		var resp map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["error"] != errUnknownPreset {
			t.Errorf("%s: error = %q, want %q", tc.name, resp["error"], errUnknownPreset)
		}
		if repo.created != nil {
			t.Errorf("%s: must not create job for an unresolvable preset", tc.name)
		}
		bodies = append(bodies, rec.Body.Bytes())
	}
	if !bytes.Equal(bodies[0], bodies[1]) {
		t.Errorf("nonexistent vs cross-client/inactive response bodies differ: %q vs %q (D-03/Pitfall 12 no-leak violation)", bodies[0], bodies[1])
	}
}

// TestCreateJob_PresetOptsRevalidated verifies D-06: a preset's stored opts
// (containing a field the current allowlist rejects) are re-run through
// ParseDocOpts at USE time, not trusted -- a stale preset fails job creation.
func TestCreateJob_PresetOptsRevalidated(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	presetRepo := &fakePresetRepo{resolve: &presets.Preset{
		ID: uuid.New(), Name: "stale", Version: 1, TargetFormat: "pdf",
		Options: map[string]any{"EncryptFile": true}, // unknown key, since-invalidated
	}}
	resolver := newFakeResolver()
	srv := NewServer(repo, store, &fakeQueue{}, presetRepo, defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})

	body, ct := multipartBodyWithPreset(t, "in.docx", "stale", "", docxFixture(t), "")
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload before opts re-validation rejects a stale preset")
	}
	if repo.created != nil {
		t.Error("must not create job for a preset with since-invalidated opts")
	}
}

// TestCreateJob_PresetDeactivatedDuringCreate verifies Pitfall 8: the preset
// resolves successfully on the FIRST Resolve (so resolution, opts
// validation, and upload all succeed), but the pre-Create re-check
// (SECOND Resolve) reports ErrNotFound -- simulating deactivation in the
// resolve-to-insert window. The job must be rejected 422 and repo.Create
// must NEVER be invoked; the already-uploaded object is left in place
// (mirrors the existing repo.Create-failure no-cleanup path).
func TestCreateJob_PresetDeactivatedDuringCreate(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	presetRepo := &fakePresetRepo{
		resolve:    &presets.Preset{ID: uuid.New(), Name: "thumb", Version: 1, TargetFormat: "webp"},
		recheckSet: true,
		recheckErr: presets.ErrNotFound,
	}
	resolver := newFakeResolver()
	srv := NewServer(repo, store, &fakeQueue{}, presetRepo, defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})
	srv.SetCapabilityCatalog(imageCatalog())

	body, ct := multipartBodyWithPreset(t, "in.png", "thumb", "", pngBytesFixture(), "")
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["error"] != errUnknownPreset {
		t.Errorf("error = %q, want %q", resp["error"], errUnknownPreset)
	}
	if repo.created != nil {
		t.Error("repo.Create must NEVER be invoked when the preset is deactivated between resolve and insert")
	}
	if !store.uploaded {
		t.Error("upload happens before the pre-Create re-check; the object is left uploaded for TTL cleanup")
	}
	if presetRepo.calls < 2 {
		t.Errorf("presetRepo.calls = %d, want >= 2 (resolution + pre-Create re-check)", presetRepo.calls)
	}
}

// TestCreateJob_PresetHTMLOptsResolved proves the engine-keyed opts dispatch
// handles preset-sourced HTML opts identically to ad-hoc HTML opts.
func TestCreateJob_PresetHTMLOptsResolved(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	presetRepo := &fakePresetRepo{resolve: &presets.Preset{
		ID: uuid.New(), Name: "a4pdf", Version: 1, TargetFormat: "pdf",
		Options: map[string]any{"page_size": "a4"},
	}}
	resolver := newFakeResolver()
	srv := NewServer(repo, store, q, presetRepo, defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{MaxUploadBytes: 1 << 20})
	srv.SetCapabilityCatalog(htmlCatalog())

	body, ct := multipartBodyWithPreset(t, "in.html", "a4pdf", "", htmlFixture(), "")
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if repo.created == nil || repo.created.Opts["page_size"] != "a4" || repo.created.DispatchOwner != "protocol" {
		t.Errorf("CreateParams = %+v, want protocol html with page_size=a4", repo.created)
	}
	if q.enqueueCalls != 0 || q.enqueuedHTML != uuid.Nil {
		t.Errorf("enqueueCalls=%d enqueuedHTML=%s, want no enqueue", q.enqueueCalls, q.enqueuedHTML)
	}
}

// --- audio engine tests (Plan 03: AUD-05) ---

// audioMP3Fixture reads the real fixture with a large ID3v2 tag, shared with
// internal/convert's own SniffAudio tests. Its whole size sits comfortably
// under mp3PeekLen (512 KiB) AND the 1 MiB newTestServer MaxUploadBytes
// default, so the entire file is read by SniffAudio's io.ReadFull peek in
// one shot.
func audioMP3Fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../convert/testdata/audio/sample-id3.mp3")
	if err != nil {
		t.Fatalf("read audio fixture: %v", err)
	}
	return data
}

// TestCreateJob_AudioDetectedAndAccepted proves an mp3 upload with a large
// ID3v2 tag is detected by SniffAudio, routed to engine "audio", enqueued via
// EnqueueAudioConvert (and NONE of the other three queues), and -- the
// regression a naive "content validation passed" assertion would NOT catch
// (T-31-02) -- the object handed to storage.Upload is byte-identical to the
// uploaded file, proving SniffAudio was chained off `rest` and not `file`
// (which would have silently dropped the first sniffLen=12 bytes).
func TestCreateJob_AudioDetectedAndAccepted(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	queue := &fakeQueue{}
	srv, _ := newTestServer(repo, store, queue)
	srv.SetCapabilityCatalog(audioCatalog())

	data := audioMP3Fixture(t)
	body, ct := multipartBody(t, "in.mp3", "txt", data)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if !store.uploaded {
		t.Fatal("must upload a valid mp3 upload")
	}
	if store.contentType != "audio/mpeg" {
		t.Errorf("contentType = %q, want audio/mpeg", store.contentType)
	}
	if !bytes.Equal(store.uploadedBytes, data) {
		t.Fatalf("stored object (%d bytes) != uploaded bytes (%d bytes) -- SniffAudio must chain off `rest`, not `file` (T-31-02 truncation trap)", len(store.uploadedBytes), len(data))
	}
	if repo.created == nil {
		t.Fatal("must create job for a valid mp3 -> txt conversion")
	}
	if repo.created.SourceFormat != "mp3" || repo.created.TargetFormat != "txt" {
		t.Errorf("job format = %s -> %s, want mp3 -> txt", repo.created.SourceFormat, repo.created.TargetFormat)
	}
	if repo.created.Engine != "audio" {
		t.Errorf("job Engine = %q, want audio", repo.created.Engine)
	}
	if repo.created.DispatchOwner != "protocol" || queue.enqueueCalls != 0 || queue.enqueuedAudio != uuid.Nil {
		t.Errorf("dispatch=%q enqueueCalls=%d enqueuedAudio=%s, want protocol and no enqueue", repo.created.DispatchOwner, queue.enqueueCalls, queue.enqueuedAudio)
	}
	if queue.enqueuedHTML != uuid.Nil {
		t.Errorf("enqueuedHTML = %s, want uuid.Nil (mp3 upload must never touch the html queue)", queue.enqueuedHTML)
	}
	if queue.enqueuedDocument != uuid.Nil {
		t.Errorf("enqueuedDocument = %s, want uuid.Nil (mp3 upload must never touch the document queue)", queue.enqueuedDocument)
	}
	if queue.enqueuedImage != uuid.Nil {
		t.Errorf("enqueuedImage = %s, want uuid.Nil (mp3 upload must never touch the image queue)", queue.enqueuedImage)
	}
}

// TestCreateJob_AudioOptsAccepted proves an audio job carrying
// {"language":"ru"} opts is accepted via the dedicated EngineAudio opts case
// (ParseAudioOpts) rather than being rejected as an invalid DocOpts (T-31-04)
// -- the confirmed opts mis-routing bug this plan closes.
func TestCreateJob_AudioOptsAccepted(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	queue := &fakeQueue{}
	srv, _ := newTestServer(repo, store, queue)
	srv.SetCapabilityCatalog(audioCatalog())

	body, ct := multipartBodyWithOpts(t, "in.mp3", "txt", audioMP3Fixture(t), `{"language":"ru"}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (audio opts must be validated via ParseAudioOpts, not rejected as DocOpts); body=%s", rec.Code, rec.Body.String())
	}
	if repo.created == nil || repo.created.Opts["language"] != "ru" || repo.created.DispatchOwner != "protocol" {
		t.Errorf("CreateParams = %+v, want protocol audio with language=ru", repo.created)
	}
	if queue.enqueueCalls != 0 || queue.enqueuedAudio != uuid.Nil {
		t.Errorf("enqueueCalls=%d enqueuedAudio=%s, want no enqueue", queue.enqueueCalls, queue.enqueuedAudio)
	}
}

// TestCreateJob_AudioOptsRejectedForWrongLanguage proves ParseAudioOpts's
// closed language allowlist is actually enforced through the API path (not
// silently bypassed), mirroring the HTML/Doc opts-applicability tests'
// discipline.
func TestCreateJob_AudioOptsRejectedForWrongLanguage(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBodyWithOpts(t, "in.mp3", "txt", audioMP3Fixture(t), `{"language":"zz"}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if repo.created != nil {
		t.Error("repo.Create must never run for an unsupported language")
	}
}

// --- av engine detection/routing tests (Plan 06: D-08 SniffVideo wiring, AVE-03/AVT-01) ---

// TestCreateJob_MKVDetectedAndAccepted proves an mkv upload (undetectable
// before this plan, since SniffVideo had zero non-test callers) is now
// detected via the D-08 SniffVideo chain link, routed to the REAL
// convert.Default-registered AVConverter's "av" engine (transcode mkv->mp4
// is one of AVConverter's locked Pairs, av.go), enqueued via
// EnqueueAVConvert, and -- proving the rest-rebinding fix did not truncate
// the upload -- the object handed to storage.Upload is byte-identical to the
// uploaded file.
func TestCreateJob_MKVDetectedAndAccepted(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	queue := &fakeQueue{}
	srv, _ := newTestServer(repo, store, queue)
	srv.SetCapabilityCatalog(avCatalog())

	data := mkvBytesFixture()
	body, ct := multipartBody(t, "in.mkv", "mp4", data)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (mkv must be detected via SniffVideo, not rejected as unrecognized content); body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(store.uploadedBytes, data) {
		t.Fatalf("stored object (%d bytes) != uploaded bytes (%d bytes) -- SniffVideo's rest must be rebound even on empty video-detection lookahead by SniffAudio, and preserved on a genuine mkv match", len(store.uploadedBytes), len(data))
	}
	if repo.created == nil {
		t.Fatal("must create job for a valid mkv -> mp4 conversion")
	}
	if repo.created.SourceFormat != "mkv" || repo.created.TargetFormat != "mp4" {
		t.Errorf("job format = %s -> %s, want mkv -> mp4", repo.created.SourceFormat, repo.created.TargetFormat)
	}
	if repo.created.Engine != convert.EngineAV || repo.created.DispatchOwner != "protocol" {
		t.Errorf("job = %+v, want protocol av", repo.created)
	}
	if queue.enqueueCalls != 0 || queue.enqueuedAV != uuid.Nil {
		t.Errorf("enqueueCalls=%d enqueuedAV=%s, want no enqueue", queue.enqueueCalls, queue.enqueuedAV)
	}
	if queue.enqueuedAudio != uuid.Nil {
		t.Errorf("enqueuedAudio = %s, want uuid.Nil (mkv->mp4 transcode must never touch the audio queue)", queue.enqueuedAudio)
	}
}

// TestCreateJob_WebMDetectedAndAccepted mirrors
// TestCreateJob_MKVDetectedAndAccepted for the webm DocType -- both mkv and
// webm share the identical EBML magic and can only be disambiguated by the
// DocType element matchEBML walks to (avsniff.go).
func TestCreateJob_WebMDetectedAndAccepted(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	queue := &fakeQueue{}
	srv, _ := newTestServer(repo, store, queue)
	srv.SetCapabilityCatalog(avCatalog())

	data := webmBytesFixture()
	body, ct := multipartBody(t, "in.webm", "mp4", data)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (webm must be detected via SniffVideo, not rejected as unrecognized content); body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(store.uploadedBytes, data) {
		t.Fatalf("stored object (%d bytes) != uploaded bytes (%d bytes)", len(store.uploadedBytes), len(data))
	}
	if repo.created == nil {
		t.Fatal("must create job for a valid webm -> mp4 conversion")
	}
	if repo.created.SourceFormat != "webm" || repo.created.TargetFormat != "mp4" {
		t.Errorf("job format = %s -> %s, want webm -> mp4", repo.created.SourceFormat, repo.created.TargetFormat)
	}
	if repo.created.Engine != convert.EngineAV || repo.created.DispatchOwner != "protocol" {
		t.Errorf("job = %+v, want protocol av", repo.created)
	}
	if queue.enqueueCalls != 0 || queue.enqueuedAV != uuid.Nil {
		t.Errorf("enqueueCalls=%d enqueuedAV=%s, want no enqueue", queue.enqueueCalls, queue.enqueuedAV)
	}
}

// TestCreateJob_MP4StillDetectedBySniffPrefixTable proves the D-08 SniffVideo
// wiring does not disturb mp4 detection: mp4 is caught by Sniff()'s
// fixed-12-byte-window signatures table (matchMP4, avsniff.go) earlier in
// the chain, so the `detected == ""` gate guarding the new SniffVideo call
// is already false by the time execution reaches it -- SniffVideo never
// runs for mp4. Routed through the audio-extract pair (mp4 -> mp3, one of
// AVConverter's locked Pairs) so this exercises the REAL registered
// converter, not the test-only fakeAVConverter's synthetic pair.
func TestCreateJob_MP4StillDetectedBySniffPrefixTable(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	queue := &fakeQueue{}
	srv, _ := newTestServer(repo, store, queue)
	srv.SetCapabilityCatalog(avCatalog())

	data := mp4BytesFixture()
	body, ct := multipartBody(t, "in.mp4", "mp3", data)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if repo.created == nil || repo.created.SourceFormat != "mp4" {
		t.Fatalf("job SourceFormat = %+v, want mp4 (Sniff's prefix table must still win, not SniffVideo)", repo.created)
	}
	if repo.created.Engine != convert.EngineAV || repo.created.DispatchOwner != "protocol" {
		t.Errorf("job = %+v, want protocol av", repo.created)
	}
	if queue.enqueueCalls != 0 || queue.enqueuedAV != uuid.Nil {
		t.Errorf("enqueueCalls=%d enqueuedAV=%s, want no enqueue", queue.enqueueCalls, queue.enqueuedAV)
	}
}

// --- av opts-dispatch tests (Plan 06: EngineAV case, AVE-03) ---

// TestCreateJob_AVOptsAccepted proves a valid AV opts payload (a thumbnail
// timecode on a thumbnail target) is accepted via the dedicated EngineAV
// opts case (ParseAVOpts/ValidateAVApplicability), normalized, and persisted
// -- mirrors TestCreateJob_AudioOptsAccepted's discipline for the av engine.
func TestCreateJob_AVOptsAccepted(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	queue := &fakeQueue{}
	srv, _ := newTestServer(repo, store, queue)
	srv.SetCapabilityCatalog(avCatalog())

	body, ct := multipartBodyWithOpts(t, "in.mp4", "jpg", mp4BytesFixture(), `{"timecode":2.5}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (valid av opts must be accepted via ParseAVOpts, not rejected as DocOpts); body=%s", rec.Code, rec.Body.String())
	}
	if repo.created == nil || repo.created.Opts["timecode"] != 2.5 || repo.created.DispatchOwner != "protocol" {
		t.Errorf("CreateParams = %+v, want protocol av with timecode=2.5", repo.created)
	}
	if repo.created.Engine != convert.EngineAV {
		t.Errorf("job Engine = %q, want av", repo.created.Engine)
	}
	if queue.enqueueCalls != 0 || queue.enqueuedAV != uuid.Nil {
		t.Errorf("enqueueCalls=%d enqueuedAV=%s, want no enqueue", queue.enqueueCalls, queue.enqueuedAV)
	}
}

// TestCreateJob_AVOptsAbsentAccepted proves an av-engine conversion with no
// opts field at all is accepted -- the fourth opts-dispatch behavior bullet
// (opts absent -> accepted), same discipline as every other engine's opts
// being entirely optional (D-02, opts.go doc comment mirrored across
// engines).
func TestCreateJob_AVOptsAbsentAccepted(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	queue := &fakeQueue{}
	srv, _ := newTestServer(repo, store, queue)
	srv.SetCapabilityCatalog(avCatalog())

	body, ct := multipartBody(t, "in.mp4", "jpg", mp4BytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (absent opts must be accepted for the av engine); body=%s", rec.Code, rec.Body.String())
	}
	if repo.created == nil || repo.created.DispatchOwner != "protocol" || queue.enqueueCalls != 0 || queue.enqueuedAV != uuid.Nil {
		t.Errorf("created=%+v enqueueCalls=%d enqueuedAV=%s, want protocol av and no enqueue", repo.created, queue.enqueueCalls, queue.enqueuedAV)
	}
}

// TestCreateJob_AVOptsRejectedForMalformedJSON proves ParseAVOpts's strict
// decode (checkStrictObject, opts.go) actually runs on the API path: a
// value of the wrong type for a known field is rejected with 422, not
// silently coerced or passed through to the converter.
func TestCreateJob_AVOptsRejectedForMalformedJSON(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBodyWithOpts(t, "in.mp4", "jpg", mp4BytesFixture(), `{"timecode":"not-a-number"}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if repo.created != nil {
		t.Error("repo.Create must never run for malformed av opts JSON")
	}
}

// TestCreateJob_AVOptsRejectedForInapplicablePair proves
// ValidateAVApplicability's engine/target gating is actually enforced
// through the API path: a Timecode (thumbnail-only) requested against a
// transcode target is rejected with 422, mirroring the D-09/CR-01 "never
// silently retarget a client's request" discipline the plan calls out.
func TestCreateJob_AVOptsRejectedForInapplicablePair(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBodyWithOpts(t, "in.mkv", "mp4", mkvBytesFixture(), `{"timecode":2.5}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (timecode is thumbnail-only, mkv->mp4 is a transcode target); body=%s", rec.Code, rec.Body.String())
	}
	if repo.created != nil {
		t.Error("repo.Create must never run for opts inapplicable to the requested conversion")
	}
}

// --- D-06 API enqueue-switch completeness guard (Plan 06, third and final seam) ---

// TestCreateJobRoutesEveryEngineToItsQueue is the D-06 completeness guard for
// the API's enqueue switch (35-CONTEXT.md): D-06 explicitly declined
// refactoring the API's (and the reconciler's) routing switch into a shared
// queueForEngine helper for this phase, so the risk of a future engine
// constant silently missing an enqueue case is closed by this test instead
// of a structural refactor -- the sanctioned structural replacement for that
// declined refactor, and the THIRD and final D-06 seam this phase closes
// (the queue-depth collector list was closed in plan 04's derived-collector
// wiring, the reconciler routing switch in plan 05's
// TestSweepRoutesEveryEngineConstant). The API switch at least fails closed
// at runtime already (a missing case returns 500, never silently drops the
// job) -- the queue-depth collector's variadic call site did NOT fail closed
// (Pitfall 2, 35-RESEARCH.md), which is why all three seams get their own
// test rather than relying on any one switch's fail-closed default to stand
// in for the others.
//
// The table is driven directly off the convert.Engine* constants: a future
// sixth engine class added to convert.go without a matching case in
// handleCreateJob's enqueue switch fails this test (via the missing table
// row/accessor) rather than staying invisible except for a 500 in
// production.
// TestCreateJob_TectonicInputCeilingEnforced proves the 2 MiB ceiling is
// actually enforced at admission, not merely declared in a map.
//
// It is the half of 64-05's decision that is easy to lose: the timeout is
// visible in five files and an operator would notice it changing, while the
// ceiling is one integer in defaultMaxEngineBytes. If it silently reverted to
// the 100 MiB this engine inherited, nothing about the timeout would look
// wrong -- and a 40 MiB .tex would be admitted, run for ten times the
// reconciler cap, and be re-enqueued underneath itself.
func TestCreateJob_TectonicInputCeilingEnforced(t *testing.T) {
	store := &failOnUploadStorage{t: t}

	// Just over the ceiling, built out of a legal preamble plus padding so the
	// rejection cannot be blamed on the content check.
	var b strings.Builder
	b.WriteString("\\documentclass{article}\n\\begin{document}\n")
	for b.Len() < (2<<20)+4096 {
		b.WriteString("padding that is entirely valid LaTeX body text.\n")
	}
	b.WriteString("\\end{document}\n")

	body, ct := multipartBody(t, "book.tex", "pdf", []byte(b.String()))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	// MaxUploadBytes is raised well above the upload on purpose: the server's
	// own global cap must not be what refuses this, or the test would pass
	// with no per-engine ceiling existing at all.
	srv := NewServer(&fakeRepo{}, store, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(),
		&fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(),
		Config{MaxUploadBytes: 64 << 20})
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 -- a .tex above the 2 MiB tex-engine ceiling must be refused "+
			"before the upload; body=%s", rec.Code, rec.Body.String())
	}
}

// --- TEX-03: the tex-package admission gate --------------------------------

// failOnUploadStorage fails the test if Upload is called AT ALL. A boolean
// checked afterwards would prove the same thing one step later; this proves it
// at the moment of the call, with the stack still pointing at the line that
// made it.
type failOnUploadStorage struct {
	fakeStorage
	t *testing.T
}

func (f *failOnUploadStorage) Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	f.t.Fatalf("storage.Upload(%q) was called: the tex-package gate must refuse BEFORE any object is "+
		"written, or a document this deployment cannot typeset costs S3 bytes and a quota slot to be "+
		"told so", key)
	return nil
}

// texDoc builds a .tex fixture loading exactly the given packages.
func texDoc(packages ...string) []byte {
	src := "\\documentclass{article}\n"
	for _, p := range packages {
		src += "\\usepackage{" + p + "}\n"
	}
	return []byte(src + "\\begin{document}\nhello\n\\end{document}\n")
}

// TestCreateJob_UnresolvablePackageRejectedBeforeUpload is TEX-03's central
// assertion: the refusal happens before the storage write, carries the shared
// code, and NAMES the package.
//
// pgfplots is not a hypothetical -- it was compiled against the real bundle in
// the container and reported `! LaTeX Error: File \`pgfplots.sty' not found.`
// (evidence/64-03-package-resolution.txt).
func TestCreateJob_UnresolvablePackageRejectedBeforeUpload(t *testing.T) {
	repo := &fakeRepo{}
	store := &failOnUploadStorage{t: t}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBody(t, "paper.tex", "pdf", texDoc("amsmath", "pgfplots"))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v (%s)", err, rec.Body.String())
	}
	if resp.Code != errcode.UnsupportedPackage {
		t.Errorf("code = %q, want %q", resp.Code, errcode.UnsupportedPackage)
	}
	if !strings.Contains(resp.Error, "pgfplots") {
		t.Errorf("message = %q, does not name the package -- the client is left bisecting their own "+
			"preamble, one upload per attempt", resp.Error)
	}
	if repo.created != nil {
		t.Error("a job row was created for a document that was refused")
	}
}

// TestCreateJob_ResolvablePackagesOutsideTheDeclaredSetAreAdmitted is the half
// that keeps the gate honest. All five packages below are OUTSIDE
// convert.TeXPackages and all five compile against the shipped bundle; five of
// the twelve measured are in pandoc's default LaTeX template, and fontenc is in
// the paper 64-04 ran end to end.
//
// Gating on the declared set would fail HERE, which is the point: without this
// test, "the gate works" and "the gate refuses valid documents" look identical.
func TestCreateJob_ResolvablePackagesOutsideTheDeclaredSetAreAdmitted(t *testing.T) {
	pandocTemplatePackages := []string{"lmodern", "fontenc", "inputenc", "textcomp", "microtype"}
	for _, pkg := range pandocTemplatePackages {
		if convert.TeXPackageSupported(pkg) {
			t.Fatalf("%s is now in the DECLARED set -- this test no longer proves what it claims", pkg)
		}
	}

	repo := &fakeRepo{}
	store := &fakeStorage{}
	queue := &fakeQueue{}
	srv, _ := newTestServer(repo, store, queue)
	srv.SetCapabilityCatalog(&fakeCatalog{found: true, match: tectonicMatch()})

	body, ct := multipartBody(t, "paper.tex", "pdf", texDoc(pandocTemplatePackages...))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 -- these five packages compile against the shipped bundle; "+
			"body=%s", rec.Code, rec.Body.String())
	}
	if queue.enqueueCalls != 0 || queue.enqueuedTectonic != uuid.Nil {
		t.Errorf("enqueueCalls = %d enqueuedTectonic = %s, want no enqueue", queue.enqueueCalls, queue.enqueuedTectonic)
	}
	if repo.created == nil || repo.created.DispatchOwner != "protocol" {
		t.Errorf("dispatch = %+v, want protocol", repo.created)
	}
}

// TestCreateJob_PreambleBeyondTheWindowIsAdmitted pins the ABSTENTION. A
// preamble longer than the scan window is admitted, on purpose: the scan saw
// only part of it, so the absence of a violation says nothing, and the worker
// is the authority. 63-02's page-count gate is the precedent, and the failure
// it exists to avoid is refusing a document on the strength of a partial read.
func TestCreateJob_PreambleBeyondTheWindowIsAdmitted(t *testing.T) {
	var b strings.Builder
	b.WriteString("\\documentclass{article}\n")
	// Comment lines only -- no package is named, so nothing is hidden from the
	// scanner except the \begin{document} that would have made it complete.
	for b.Len() < convert.SniffWindowBytes+1024 {
		b.WriteString("% padding so the preamble runs past the sniff window\n")
	}
	b.WriteString("\\usepackage{pgfplots}\n\\begin{document}\nx\n\\end{document}\n")

	repo := &fakeRepo{}
	store := &fakeStorage{}
	queue := &fakeQueue{}
	resolver := newFakeResolver()
	// A document whose preamble outruns the 1 MiB sniff window is, by
	// construction, larger than newTestServer's own 1 MiB MaxUploadBytes --
	// which would refuse it 413 for a reason that has nothing to do with this
	// gate. Raised here so the 202 below is the abstention and not an accident
	// of two limits happening to be the same number.
	srv := NewServer(repo, store, queue, defaultFakePresetRepo(), defaultFakePresetAdmin(),
		&fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(),
		Config{MaxUploadBytes: 8 << 20})
	srv.SetCapabilityCatalog(&fakeCatalog{found: true, match: tectonicMatch()})

	body, ct := multipartBody(t, "paper.tex", "pdf", []byte(b.String()))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: the gate must ABSTAIN when the preamble runs past its window, "+
			"not refuse on a partial read; body=%s", rec.Code, rec.Body.String())
	}
	if queue.enqueueCalls != 0 || queue.enqueuedTectonic != uuid.Nil {
		t.Errorf("enqueueCalls = %d enqueuedTectonic = %s, want no enqueue", queue.enqueueCalls, queue.enqueuedTectonic)
	}
	if repo.created == nil || repo.created.DispatchOwner != "protocol" {
		t.Errorf("dispatch = %+v, want protocol", repo.created)
	}
}

// TestEveryEngineHasAnEnqueueRoute walks convert.AllEngines() and asserts
// none is routed to an asynq queue. A new class that grows the list is
// covered without a fixture.
func TestEveryEngineHasAnEnqueueRoute(t *testing.T) {
	engines := convert.AllEngines()
	if len(engines) == 0 {
		t.Fatal("convert.AllEngines() is empty -- this test would pass vacuously")
	}
	srv, _ := newTestServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{})
	for _, engine := range engines {
		routed, err := srv.enqueueForEngine(context.Background(), engine, uuid.New())
		if routed {
			t.Errorf("engine %s is still routed to an asynq queue", engine)
		}
		if err != nil {
			t.Errorf("engine %s: %v", engine, err)
		}
	}
	if routed, _ := srv.enqueueForEngine(context.Background(), "nonsense", uuid.New()); routed {
		t.Error("engine \"nonsense\" was routed")
	}
}

func TestCreateJobCADRejectsOtherEnginesOptions(t *testing.T) {
	for _, opts := range []string{`{"scale":2}`, `{"pdf_profile":"pdf/a-2b"}`} {
		t.Run(opts, func(t *testing.T) {
			repo, store, queue := &fakeRepo{}, &fakeStorage{}, &fakeQueue{}
			srv, _ := newTestServer(repo, store, queue)

			body, ct := multipartBodyWithOpts(t, "in.step", "stl", readCADFixture(t, "public/LM8UU.step"), opts)
			req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
			}
			if store.uploaded || store.uploadCalls != 0 {
				t.Error("rejected opts must not reach storage.Upload")
			}
			if queue.enqueueCalls != 0 || queue.enqueuedCAD != uuid.Nil {
				t.Errorf("rejected opts must not enqueue: enqueueCalls=%d enqueuedCAD=%s", queue.enqueueCalls, queue.enqueuedCAD)
			}
		})
	}
}

func TestCreateJobRoutesEveryEngineToItsQueue(t *testing.T) {
	t.Run("tectonic_without_a_catalog_is_refused", func(t *testing.T) {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		queue := &fakeQueue{}
		srv, _ := newTestServer(repo, store, queue)

		body, ct := multipartBody(t, "in.tex", "pdf", latexFixture())
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
		}
		assertErrorCode(t, rec.Body.Bytes(), errcode.UnsupportedConversion)
		if repo.created != nil || store.uploaded || queue.enqueueCalls != 0 || queue.enqueuedTectonic != uuid.Nil {
			t.Fatalf("refused tectonic created=%v uploaded=%v enqueueCalls=%d", repo.created != nil, store.uploaded, queue.enqueueCalls)
		}
	})

	t.Run("tectonic_with_a_catalog_is_protocol", func(t *testing.T) {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		queue := &fakeQueue{}
		srv, _ := newTestServer(repo, store, queue)
		srv.SetCapabilityCatalog(&fakeCatalog{found: true, match: tectonicMatch()})

		body, ct := multipartBody(t, "in.tex", "pdf", latexFixture())
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
		}
		if repo.created == nil || repo.created.Engine != convert.EngineTectonic || repo.created.DispatchOwner != "protocol" || repo.created.ProtocolAttemptBudget != 271*time.Second {
			t.Fatalf("created = %+v, want protocol tectonic at 271s", repo.created)
		}
		if repo.created.CapabilityID != "octoconv.convert.tectonic" || repo.created.CapabilityVersion != 1 {
			t.Fatalf("pin = %s v%d", repo.created.CapabilityID, repo.created.CapabilityVersion)
		}
		if queue.enqueueCalls != 0 || queue.enqueuedTectonic != uuid.Nil {
			t.Fatalf("enqueueCalls = %d enqueuedTectonic = %s, want no enqueue", queue.enqueueCalls, queue.enqueuedTectonic)
		}
	})

	t.Run("image_without_a_catalog_is_refused", func(t *testing.T) {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		queue := &fakeQueue{}
		srv, _ := newTestServer(repo, store, queue)

		body, ct := multipartBody(t, "in.png", "webp", pngBytesFixture())
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()

		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
		}
		assertErrorCode(t, rec.Body.Bytes(), errcode.UnsupportedConversion)
		if !strings.Contains(rec.Body.String(), "png -> webp") {
			t.Fatalf("body = %s, want png -> webp", rec.Body.String())
		}
		if repo.created != nil || store.uploaded || queue.enqueueCalls != 0 || queue.enqueuedImage != uuid.Nil {
			t.Fatalf("refused image created=%v uploaded=%v enqueueCalls=%d", repo.created != nil, store.uploaded, queue.enqueueCalls)
		}
	})

	t.Run("audio_without_a_catalog_is_refused", func(t *testing.T) {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		queue := &fakeQueue{}
		srv, _ := newTestServer(repo, store, queue)

		body, ct := multipartBody(t, "in.mp3", "txt", audioMP3Fixture(t))
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()

		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
		}
		assertErrorCode(t, rec.Body.Bytes(), errcode.UnsupportedConversion)
		if !strings.Contains(rec.Body.String(), "mp3 -> txt") {
			t.Fatalf("body = %s, want mp3 -> txt", rec.Body.String())
		}
		if repo.created != nil || store.uploaded || queue.enqueueCalls != 0 || queue.enqueuedAudio != uuid.Nil {
			t.Fatalf("refused audio created=%v uploaded=%v enqueueCalls=%d", repo.created != nil, store.uploaded, queue.enqueueCalls)
		}
	})

	t.Run("av_without_a_catalog_is_refused", func(t *testing.T) {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		queue := &fakeQueue{}
		srv, _ := newTestServer(repo, store, queue)

		body, ct := multipartBody(t, "in.mp4", "mp3", mp4BytesFixture())
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()

		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
		}
		assertErrorCode(t, rec.Body.Bytes(), errcode.UnsupportedConversion)
		if !strings.Contains(rec.Body.String(), "mp4 -> mp3") {
			t.Fatalf("body = %s, want mp4 -> mp3", rec.Body.String())
		}
		if repo.created != nil || store.uploaded || queue.enqueueCalls != 0 || queue.enqueuedAV != uuid.Nil {
			t.Fatalf("refused av created=%v uploaded=%v enqueueCalls=%d", repo.created != nil, store.uploaded, queue.enqueueCalls)
		}
	})

	t.Run("ocr_without_a_catalog_is_refused", func(t *testing.T) {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		queue := &fakeQueue{}
		srv, _ := newTestServer(repo, store, queue)

		body, ct := multipartBody(t, "scan.pdf", "txt", pdfBytesFixture())
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()

		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
		}
		assertErrorCode(t, rec.Body.Bytes(), errcode.UnsupportedConversion)
		if !strings.Contains(rec.Body.String(), "pdf -> txt") {
			t.Fatalf("body = %s, want pdf -> txt", rec.Body.String())
		}
		if repo.created != nil || store.uploaded || queue.enqueueCalls != 0 || queue.enqueuedOCR != uuid.Nil {
			t.Fatalf("refused ocr created=%v uploaded=%v enqueueCalls=%d", repo.created != nil, store.uploaded, queue.enqueueCalls)
		}
	})

	t.Run("pandoc_without_a_catalog_is_refused", func(t *testing.T) {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		queue := &fakeQueue{}
		srv, _ := newTestServer(repo, store, queue)

		body, ct := multipartBody(t, "in.md", "html", markdownFixture())
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()

		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
		}
		assertErrorCode(t, rec.Body.Bytes(), errcode.UnsupportedConversion)
		if !strings.Contains(rec.Body.String(), "md -> html") {
			t.Fatalf("body = %s, want md -> html", rec.Body.String())
		}
		if repo.created != nil || store.uploaded || queue.enqueueCalls != 0 || queue.enqueuedPandoc != uuid.Nil {
			t.Fatalf("refused pandoc created=%v uploaded=%v enqueueCalls=%d", repo.created != nil, store.uploaded, queue.enqueueCalls)
		}
	})

	t.Run("html_without_a_catalog_is_refused", func(t *testing.T) {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		queue := &fakeQueue{}
		srv, _ := newTestServer(repo, store, queue)

		body, ct := multipartBody(t, "in.html", "pdf", htmlFixture())
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()

		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
		}
		assertErrorCode(t, rec.Body.Bytes(), errcode.UnsupportedConversion)
		if !strings.Contains(rec.Body.String(), "html -> pdf") {
			t.Fatalf("body = %s, want html -> pdf", rec.Body.String())
		}
		if repo.created != nil || store.uploaded || queue.enqueueCalls != 0 || queue.enqueuedHTML != uuid.Nil {
			t.Fatalf("refused html created=%v uploaded=%v enqueueCalls=%d", repo.created != nil, store.uploaded, queue.enqueueCalls)
		}
	})

	t.Run("document_without_a_catalog_is_refused", func(t *testing.T) {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		queue := &fakeQueue{}
		srv, _ := newTestServer(repo, store, queue)

		body, ct := multipartBody(t, "in.docx", "pdf", docxFixture(t))
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()

		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
		}
		assertErrorCode(t, rec.Body.Bytes(), errcode.UnsupportedConversion)
		if repo.created != nil || store.uploaded || queue.enqueueCalls != 0 || queue.enqueuedDocument != uuid.Nil {
			t.Fatalf("refused document created=%v uploaded=%v enqueueCalls=%d", repo.created != nil, store.uploaded, queue.enqueueCalls)
		}
	})

	t.Run("cad_without_a_catalog_is_refused", func(t *testing.T) {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		queue := &fakeQueue{}
		srv, _ := newTestServer(repo, store, queue)

		body, ct := multipartBody(t, "in.step", "stl", readCADFixture(t, "public/LM8UU.step"))
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()

		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
		}
		assertErrorCode(t, rec.Body.Bytes(), errcode.UnsupportedConversion)
		if repo.created != nil || store.uploaded || queue.enqueueCalls != 0 || queue.enqueuedCAD != uuid.Nil {
			t.Fatalf("refused cad created=%v uploaded=%v enqueueCalls=%d", repo.created != nil, store.uploaded, queue.enqueueCalls)
		}
	})

	// AVT-01 routing split (ROADMAP success criterion 2): the SAME source
	// format (mp4) resolves to two DIFFERENT engines depending on target --
	// mp4 -> srt (transcript) rides the existing audio queue/worker
	// (video-to-transcript, D-04), while mp4 -> webm (transcode) is the real
	// AVConverter's own pair. Reuses the "one queue and NONE of the others"
	// negative-assertion style already established by
	// TestCreateJob_AudioDetectedAndAccepted (handlers_test.go).
	t.Run("mp4_to_srt_routes_audio_not_av", func(t *testing.T) {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		queue := &fakeQueue{}
		srv, _ := newTestServer(repo, store, queue)
		srv.SetCapabilityCatalog(&fakeCatalog{
			allow: func(operation, source, target string) (workerproto.Match, bool) {
				if operation == convert.OperationConvert && source == "mp4" && target == "srt" {
					return workerproto.Match{
						CapabilityID:      "octoconv.convert.audio",
						CapabilityVersion: 1,
						ClientOperation:   convert.OperationConvert,
						LegacyOperation:   convert.OperationConvert,
						LegacyEngine:      convert.EngineAudio,
						ParameterSchema:   []byte(`{}`),
					}, true
				}
				return workerproto.Match{}, false
			},
		})

		body, ct := multipartBody(t, "in.mp4", "srt", mp4BytesFixture())
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()

		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
		}
		if repo.created == nil || repo.created.Engine != convert.EngineAudio || repo.created.DispatchOwner != "protocol" {
			t.Fatalf("job = %+v, want protocol audio", repo.created)
		}
		if queue.enqueueCalls != 0 || queue.enqueuedAudio != uuid.Nil {
			t.Errorf("enqueueCalls=%d enqueuedAudio=%s, want no enqueue", queue.enqueueCalls, queue.enqueuedAudio)
		}
		if queue.enqueuedAV != uuid.Nil {
			t.Errorf("enqueuedAV = %s, want uuid.Nil (mp4 -> srt must never touch the av queue)", queue.enqueuedAV)
		}
	})

	t.Run("mp4_to_webm_routes_av_not_audio", func(t *testing.T) {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		queue := &fakeQueue{}
		srv, _ := newTestServer(repo, store, queue)
		srv.SetCapabilityCatalog(avCatalog())

		body, ct := multipartBody(t, "in.mp4", "webm", mp4BytesFixture())
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()

		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
		}
		if repo.created == nil || repo.created.Engine != convert.EngineAV || repo.created.DispatchOwner != "protocol" {
			t.Fatalf("job = %+v, want protocol av", repo.created)
		}
		if queue.enqueueCalls != 0 || queue.enqueuedAV != uuid.Nil {
			t.Errorf("enqueueCalls=%d enqueuedAV=%s, want no enqueue", queue.enqueueCalls, queue.enqueuedAV)
		}
		if queue.enqueuedAudio != uuid.Nil {
			t.Errorf("enqueuedAudio = %s, want uuid.Nil (mp4 -> webm must never touch the audio queue)", queue.enqueuedAudio)
		}
	})
}

// --- D-07 two-tier upload ceiling tests (35-04) ---

// TestNewServer_MaxEngineBytesDefaults proves NewServer's zero-value
// Config.MaxEngineBytes defaulting yields a non-nil map covering all seven
// engines: image/document/html/audio/ocr/pandoc at their 100 MiB ceiling (ocr
// per D-03, Phase 44; pandoc per PR#2 finding-1 -- an unset
// Config.MaxPandocInputBytes must never change the default), av at the
// raised 2 GiB allowance.
func TestNewServer_MaxEngineBytesDefaults(t *testing.T) {
	srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), Config{})

	if srv.maxEngineBytes == nil {
		t.Fatal("maxEngineBytes must be non-nil after NewServer")
	}
	for _, engine := range []string{convert.EngineImage, convert.EngineDocument, convert.EngineHTML, convert.EngineAudio, convert.EngineOCR, convert.EnginePandoc} {
		if got := srv.maxEngineBytes[engine]; got != 100<<20 {
			t.Errorf("maxEngineBytes[%s] = %d, want 100 MiB (100<<20)", engine, got)
		}
	}
	if got := srv.maxEngineBytes[convert.EngineAV]; got != 2<<30 {
		t.Errorf("maxEngineBytes[av] = %d, want 2 GiB (2<<30)", got)
	}
}

// TestCreateJob_EngineSizeLimitRejectsOversizedImage verifies D-07's second
// tier: an image upload whose physical size exceeds ITS engine's ceiling is
// rejected 413 -- before any storage.Upload call and before repo.Create --
// even though it is comfortably under the (much larger) global
// MaxUploadBytes pre-parse bound. Uses a scaled-down MaxEngineBytes value
// (mirrors TestCreateJob_DimensionLimitExceeded's scaled-down MaxImagePixels)
// rather than an actual 200 MiB payload.
func TestCreateJob_EngineSizeLimitRejectsOversizedImage(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	resolver := newFakeResolver()
	srv := NewServer(repo, store, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
		MaxUploadBytes: 10 << 20,
		MaxEngineBytes: map[string]int64{convert.EngineImage: 100},
	})

	body, ct := multipartBody(t, "in.png", "webp", paddedPNGFixture(500))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not call storage.Upload for an upload exceeding its engine's size ceiling")
	}
	if repo.created != nil {
		t.Error("must not create a job for an upload exceeding its engine's size ceiling")
	}
}

// TestCreateJob_EngineSizeLimitAcceptsLargeAVUpload proves the OTHER half of
// D-07's two-tier design: an av-engine upload that would exceed the four
// existing engine classes' 100 MiB ceiling is accepted, routed with
// Engine="av", and enqueued via EnqueueAVConvert -- because av gets its own,
// much larger per-engine ceiling. Exercises engine=="av" through the fake
// (mp4, avTestTarget) collision-free synthetic pair registered above, kept
// independent of the real AVConverter's own pair set (now also registered,
// D-08, 35-06) so this test does not need to track it.
func TestCreateJob_EngineSizeLimitAcceptsLargeAVUpload(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	queue := &fakeQueue{}
	resolver := newFakeResolver()
	srv := NewServer(repo, store, queue, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
		MaxUploadBytes: 10 << 20,
		MaxEngineBytes: map[string]int64{convert.EngineImage: 1, convert.EngineAV: 5 << 20},
	})
	srv.SetCapabilityCatalog(&fakeCatalog{
		allow: func(operation, source, target string) (workerproto.Match, bool) {
			if operation == convert.OperationConvert && source == "mp4" && target == avTestTarget {
				return workerproto.Match{
					CapabilityID:      "octoconv.convert.av",
					CapabilityVersion: 1,
					ClientOperation:   convert.OperationConvert,
					LegacyOperation:   convert.OperationConvert,
					LegacyEngine:      convert.EngineAV,
					ParameterSchema:   []byte(`{}`),
				}, true
			}
			return workerproto.Match{}, false
		},
	})

	body, ct := multipartBody(t, "in.mp4", avTestTarget, paddedMP4Fixture(1<<20))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if !store.uploaded {
		t.Error("must upload an av upload within the av engine's ceiling")
	}
	if repo.created == nil {
		t.Fatal("must create a job for an av upload within the av engine's ceiling")
	}
	if repo.created.Engine != convert.EngineAV || repo.created.DispatchOwner != "protocol" {
		t.Errorf("job = %+v, want protocol av", repo.created)
	}
	if queue.enqueueCalls != 0 || queue.enqueuedAV != uuid.Nil {
		t.Errorf("enqueueCalls=%d enqueuedAV=%s, want no enqueue", queue.enqueueCalls, queue.enqueuedAV)
	}
}

// TestCreateJob_EngineAbsentFromSizeMapNotRejected verifies the fifth D-07
// behavior bullet: s.maxEngineBytes GATES known engines, it does not
// ALLOWLIST them. An engine deliberately omitted from a custom
// MaxEngineBytes map must never be rejected on size grounds, regardless of
// how large its upload is (bounded here only by MaxUploadBytes).
func TestCreateJob_EngineAbsentFromSizeMapNotRejected(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	resolver := newFakeResolver()
	srv := NewServer(repo, store, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
		MaxUploadBytes: 10 << 20,
		// EngineImage is deliberately absent -- it must not be gated.
		MaxEngineBytes: map[string]int64{convert.EngineDocument: 1},
	})
	srv.SetCapabilityCatalog(imageCatalog())

	body, ct := multipartBody(t, "in.png", "webp", paddedPNGFixture(500))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (engine absent from MaxEngineBytes must not be rejected); body=%s", rec.Code, rec.Body.String())
	}
	if !store.uploaded {
		t.Error("must upload when the detected engine has no configured size ceiling")
	}
}

// TestCreateJob_PandocOversizedRejectedBeforeUpload is the PR#2 finding-1
// regression guard: the API must honour the SAME PANDOC_MAX_INPUT_BYTES the
// worker's enforcePandocInputSize enforces, so an upload the worker would
// certainly reject with input_too_large is rejected 413 at the upload gate
// instead -- no S3 write, no job row, no queue slot burned.
func TestCreateJob_PandocOversizedRejectedBeforeUpload(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	resolver := newFakeResolver()
	srv := NewServer(repo, store, q, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
		MaxUploadBytes:      10 << 20,
		MaxPandocInputBytes: 100,
		// MaxEngineBytes deliberately left nil: NewServer's own D-07
		// defaulting must fold the scalar into the map.
	})

	content := append(append([]byte{}, markdownFixture()...), bytes.Repeat([]byte("a"), 200)...)
	body, ct := multipartBody(t, "in.md", "html", content)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not call storage.Upload for a pandoc upload exceeding PANDOC_MAX_INPUT_BYTES")
	}
	if repo.created != nil {
		t.Error("must not create a job for a pandoc upload exceeding PANDOC_MAX_INPUT_BYTES")
	}
	if q.enqueuedPandoc != uuid.Nil {
		t.Error("must not enqueue a pandoc upload exceeding PANDOC_MAX_INPUT_BYTES")
	}
}

// TestCreateJob_PandocWithinConfiguredCapAccepted proves the gate is not a
// blanket pandoc block: an upload comfortably under the configured
// MaxPandocInputBytes, with an approved manifest, still reaches 202 and is
// not enqueued.
func TestCreateJob_PandocWithinConfiguredCapAccepted(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	resolver := newFakeResolver()
	srv := NewServer(repo, store, q, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
		MaxUploadBytes:      10 << 20,
		MaxPandocInputBytes: 10_000,
	})
	srv.SetCapabilityCatalog(pandocCatalog())

	body, ct := multipartBody(t, "in.md", "html", markdownFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if !store.uploaded || repo.created == nil || repo.created.DispatchOwner != "protocol" {
		t.Fatalf("uploaded=%v created=%+v, want a protocol pandoc job", store.uploaded, repo.created)
	}
	if q.enqueueCalls != 0 || q.enqueuedPandoc != uuid.Nil {
		t.Fatalf("enqueue calls=%d pandoc=%s, want no enqueue", q.enqueueCalls, q.enqueuedPandoc)
	}
}

// TestNewServer_MaxPandocInputBytesOverridesDefault is the "single source of
// truth preserved" assertion: setting Config.MaxPandocInputBytes overrides
// ONLY the pandoc entry NewServer's own D-07 default map produces -- the six
// other per-engine ceilings are untouched.
func TestNewServer_MaxPandocInputBytesOverridesDefault(t *testing.T) {
	srv := NewServer(&fakeRepo{}, &fakeStorage{}, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), Config{
		MaxPandocInputBytes: 3145728,
	})

	if got := srv.maxEngineBytes[convert.EnginePandoc]; got != 3145728 {
		t.Errorf("maxEngineBytes[pandoc] = %d, want 3145728", got)
	}
	for _, engine := range []string{convert.EngineImage, convert.EngineDocument, convert.EngineHTML, convert.EngineAudio, convert.EngineOCR} {
		if got := srv.maxEngineBytes[engine]; got != 100<<20 {
			t.Errorf("maxEngineBytes[%s] = %d, want 100 MiB (100<<20)", engine, got)
		}
	}
	if got := srv.maxEngineBytes[convert.EngineAV]; got != 2<<30 {
		t.Errorf("maxEngineBytes[av] = %d, want 2 GiB (2<<30)", got)
	}
}

// --- OCR routing tests (Phase 44, OCRE-01/D-03) ---

// pdfBytesFixture returns the minimal %PDF- magic-byte prefix convert.Sniff
// (matchPDF, sniff.go) detects as "pdf".
func pdfBytesFixture() []byte {
	return []byte("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")
}

// TestCreateJob_OCRPDFDeclaredAsImageMismatchRejected proves OCRE-01's
// fail-closed declared/detected mismatch: a %PDF- body declared with a jpg
// extension (source=jpg) targeting an OCR output is rejected 422 via the
// EXISTING detected!=source path (handlers.go, no new OCR-specific
// validation branch) -- before any storage write, proven via the storage
// spy's Upload never being invoked.
func TestCreateJob_OCRPDFDeclaredAsImageMismatchRejected(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBody(t, "in.jpg", "txt", pdfBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !strings.Contains(resp["error"], "jpg") || !strings.Contains(resp["error"], "pdf") {
		t.Errorf("error = %q, want a message naming both declared (jpg) and detected (pdf)", resp["error"])
	}
	if store.uploaded {
		t.Error("must not upload before content validation (OCRE-01 fail-closed)")
	}
	if repo.created != nil {
		t.Error("must not create job for mismatched content")
	}
}

// TestCreateJob_ImageDeclaredAsPDFMismatchRejected is the mirror case: a PNG
// body declared with a pdf extension (source=pdf) is likewise rejected 422
// via the same existing mismatch path, before any storage write.
func TestCreateJob_ImageDeclaredAsPDFMismatchRejected(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBody(t, "in.pdf", "txt", pngBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !strings.Contains(resp["error"], "pdf") || !strings.Contains(resp["error"], "png") {
		t.Errorf("error = %q, want a message naming both declared (pdf) and detected (png)", resp["error"])
	}
	if store.uploaded {
		t.Error("must not upload before content validation (OCRE-01 fail-closed)")
	}
	if repo.created != nil {
		t.Error("must not create job for mismatched content")
	}
}

// TestCreateJob_OCRPDFWithoutACatalogIsRefused proves a valid pdf to txt
// upload with no approved manifest is refused before upload.
func TestCreateJob_OCRPDFWithoutACatalogIsRefused(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)

	body, ct := multipartBody(t, "scan.pdf", "txt", pdfBytesFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec.Body.Bytes(), errcode.UnsupportedConversion)
	if !strings.Contains(rec.Body.String(), "pdf -> txt") {
		t.Fatalf("body = %s, want pdf -> txt", rec.Body.String())
	}
	if repo.created != nil || store.uploaded || q.enqueueCalls != 0 || q.enqueuedOCR != uuid.Nil {
		t.Fatalf("refused ocr created=%v uploaded=%v enqueueCalls=%d", repo.created != nil, store.uploaded, q.enqueueCalls)
	}
}

// TestCreateJob_OCREngineSizeLimitRejectsOversizedUpload proves D-03: an
// OCR-routed upload (png->txt, EngineFor disambiguates by target to "ocr"
// now that OCRConverter is registered) whose physical size exceeds the
// EngineOCR ceiling is rejected 413 before any storage write -- mirrors
// TestCreateJob_EngineSizeLimitRejectsOversizedImage's scaled-down
// MaxEngineBytes pattern.
func TestCreateJob_OCREngineSizeLimitRejectsOversizedUpload(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	resolver := newFakeResolver()
	srv := NewServer(repo, store, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, resolver, healthyDeps(), Config{
		MaxUploadBytes: 10 << 20,
		MaxEngineBytes: map[string]int64{convert.EngineOCR: 100},
	})

	body, ct := multipartBody(t, "in.png", "txt", paddedPNGFixture(500))
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not call storage.Upload for an OCR upload exceeding its engine's size ceiling")
	}
	if repo.created != nil {
		t.Error("must not create a job for an OCR upload exceeding its engine's size ceiling")
	}
}

// --- pandoc engine tests (Plan 48-02: PDCP-01/D-02) ---

// markdownFixture returns a small, genuinely-plaintext markdown document
// (valid UTF-8, no NUL, no binary-control bytes) that convert.LooksLikeText
// accepts -- the fail-closed content check the API's detected=="" chain
// trusts for the md/rst/org text family (D-01, 47-CONTEXT.md).
func markdownFixture() []byte {
	return []byte("# Hello\n\nThis is a small markdown document.\n")
}

// latexFixture returns a small, genuinely-LaTeX document (valid UTF-8, no
// NUL, begins with a \documentclass marker) that convert.LooksLikeLaTeX
// accepts.
func latexFixture() []byte {
	return []byte("\\documentclass{article}\n\\begin{document}\nhello\n\\end{document}\n")
}

// TestCreateJob_PandocMarkdownWithoutACatalogIsRefused proves a valid
// markdown to html upload with no approved manifest is refused before upload.
func TestCreateJob_PandocMarkdownWithoutACatalogIsRefused(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	q := &fakeQueue{}
	srv, _ := newTestServer(repo, store, q)

	body, ct := multipartBody(t, "in.md", "html", markdownFixture())
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec.Body.Bytes(), errcode.UnsupportedConversion)
	if !strings.Contains(rec.Body.String(), "md -> html") {
		t.Fatalf("body = %s, want md -> html", rec.Body.String())
	}
	if repo.created != nil || store.uploaded || q.enqueueCalls != 0 || q.enqueuedPandoc != uuid.Nil {
		t.Fatalf("refused pandoc created=%v uploaded=%v enqueueCalls=%d", repo.created != nil, store.uploaded, q.enqueueCalls)
	}
}

// TestCreateJob_PandocInvalidOptsRejectedBeforeUpload proves the pandoc
// opts-dispatch case runs ParsePandocOpts (closed wrap allowlist,
// pandocopts.go) BEFORE any storage write: a latex->html upload carrying an
// unsupported wrap value is rejected 422 with storage.Upload never called
// (fail-closed, D-02/T-48-05).
func TestCreateJob_PandocInvalidOptsRejectedBeforeUpload(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	body, ct := multipartBodyWithOpts(t, "in.latex", "html", latexFixture(), `{"wrap":"bogus"}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload before pandoc opts validation (fail-closed)")
	}
	if repo.created != nil {
		t.Error("must not create job for invalid pandoc opts")
	}
}

// TestCreateJob_PandocBinaryContentDeclaredAsMarkdownRejected proves a
// .md-named upload whose content is binary (fails LooksLikeText -- a NUL
// byte, no recognizable markdown magic) is rejected 422 BEFORE any storage
// write, and never silently accepted just because the extension claims md
// (D-02/T-48-03).
func TestCreateJob_PandocBinaryContentDeclaredAsMarkdownRejected(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStorage{}
	srv, _ := newTestServer(repo, store, &fakeQueue{})

	binary := append([]byte{0x00, 0x01, 0x02, 0x03}, bytes.Repeat([]byte{0xFF}, 32)...)
	body, ct := multipartBody(t, "in.md", "html", binary)
	req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if store.uploaded {
		t.Error("must not upload before content validation (fail-closed)")
	}
	if repo.created != nil {
		t.Error("must not create job for unrecognized content declared as markdown")
	}
}

// --- GET /v1/jobs (handleListJobs) ---

func TestListJobs_Envelope(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeRepo{listResult: []jobs.JobSummary{
		{
			ID: uuid.New(), Status: jobs.StatusFailed, Engine: convert.EngineImage,
			SourceFormat: "png", TargetFormat: "webp", CreatedAt: now, ErrorCode: "engine_error",
		},
		{
			ID: uuid.New(), Status: jobs.StatusDone, Engine: convert.EngineImage,
			SourceFormat: "png", TargetFormat: "webp", CreatedAt: now,
		},
	}}
	srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Jobs       []map[string]any `json:"jobs"`
		NextCursor *string          `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rec.Body.String())
	}
	if len(resp.Jobs) != 2 {
		t.Fatalf("len(jobs) = %d, want 2", len(resp.Jobs))
	}
	if resp.NextCursor != nil {
		t.Fatalf("next_cursor = %v, want nil", resp.NextCursor)
	}

	failed, done := resp.Jobs[0], resp.Jobs[1]
	for _, key := range []string{"job_id", "status", "engine", "source_format", "target_format", "progress_pct", "created_at", "started_at", "finished_at"} {
		if _, ok := failed[key]; !ok {
			t.Errorf("failed item missing key %q", key)
		}
	}
	if failed["error_code"] != "engine_error" {
		t.Errorf("failed item error_code = %v, want engine_error", failed["error_code"])
	}
	if _, ok := done["error_code"]; ok {
		t.Error("done item must NOT carry error_code")
	}
	for _, item := range resp.Jobs {
		if _, ok := item["download_url"]; ok {
			t.Error("list item must NOT carry download_url")
		}
		if _, ok := item["opts"]; ok {
			t.Error("list item must NOT carry opts")
		}
	}
}

func TestListJobs_ScopedToCaller(t *testing.T) {
	repo := &fakeRepo{}
	srv, resolver := newTestServer(repo, &fakeStorage{}, &fakeQueue{})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if repo.listClientID != resolver.client.ID {
		t.Fatalf("Repo.List clientID = %v, want caller's own client.ID %v", repo.listClientID, resolver.client.ID)
	}
}

func TestListJobs_UnknownStatus(t *testing.T) {
	repo := &fakeRepo{}
	srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs?status=bogus", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestListJobs_UnknownEngine(t *testing.T) {
	repo := &fakeRepo{}
	srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs?engine=bogus", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestListJobs_LimitClampedToMax(t *testing.T) {
	repo := &fakeRepo{}
	srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs?limit=500", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if repo.listLimit != 100 {
		t.Fatalf("listLimit = %d, want 100 (clamped to MAX)", repo.listLimit)
	}
}

func TestListJobs_LimitDefault(t *testing.T) {
	repo := &fakeRepo{}
	srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if repo.listLimit != 20 {
		t.Fatalf("listLimit = %d, want 20 (default)", repo.listLimit)
	}
}

func TestListJobs_MalformedCursor(t *testing.T) {
	repo := &fakeRepo{}
	srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs?cursor=not-valid-base64!!", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestListJobs_NextCursor(t *testing.T) {
	next := jobs.Cursor{CreatedAt: time.Now().UTC(), ID: uuid.New()}
	repo := &fakeRepo{
		listResult: []jobs.JobSummary{{ID: uuid.New(), Status: jobs.StatusQueued, Engine: convert.EngineImage, SourceFormat: "png", TargetFormat: "webp", CreatedAt: time.Now().UTC()}},
		listNext:   &next,
	}
	srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		NextCursor *string `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.NextCursor == nil || *resp.NextCursor == "" {
		t.Fatal("next_cursor = nil/empty, want a non-empty opaque token")
	}
	if _, err := jobs.DecodeCursor(*resp.NextCursor); err != nil {
		t.Fatalf("next_cursor did not decode: %v", err)
	}
}

func TestListJobs_EmptyResult(t *testing.T) {
	repo := &fakeRepo{}
	srv, _ := newTestServer(repo, &fakeStorage{}, &fakeQueue{})

	req := authed(httptest.NewRequest(http.MethodGet, "/v1/jobs", nil))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"jobs":[],"next_cursor":null}` {
		t.Fatalf("body = %q, want {\"jobs\":[],\"next_cursor\":null}", got)
	}
}

// TestCreateJob_MacroPackageReachesSanitizeButNothingElse is the regression
// proof for the seventh finding of PR #12.
//
// The macro rejection in handleCreateJob was unconditional, and its stated
// reason -- "macro code never executes as part of producing a PDF output" --
// was written when convert was the only operation. operation=sanitize exists
// to remove those macros, so the rejection made the entire OOXML
// macro-removal path unreachable through the API: registered, advertised in
// GET /v1/formats, and impossible to invoke.
//
// The test asserts BOTH halves, because opening the gate for sanitize is only
// correct if it stays shut for everything else -- and that other half is
// pinned byte-for-byte by the pre-Phase-58 golden.
func TestCreateJob_MacroPackageReachesSanitizeButNothingElse(t *testing.T) {
	t.Run("sanitize is admitted and routed to probe", func(t *testing.T) {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		q := &fakeQueue{}
		srv, _ := newTestServer(repo, store, q)
		srv.SetCapabilityCatalog(&fakeCatalog{found: true, match: probeMatch()})

		body, ct := multipartBodyWithOperation(t, "in.docm", "docx", "sanitize", macroDocxFixture(t))
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202 -- a macro-carrying package is exactly what "+
				"sanitize exists for; body=%s", rec.Code, rec.Body.String())
		}
		if repo.created == nil {
			t.Fatal("repo.Create was never called")
		}
		if repo.created.SourceFormat != "docm" {
			t.Errorf("created.SourceFormat = %q, want \"docm\" -- SniffContainer reports the "+
				"macro-free base format, and reporting that would make the declared/detected "+
				"check reject every macro upload", repo.created.SourceFormat)
		}
		if repo.created.TargetFormat != "docx" {
			t.Errorf("created.TargetFormat = %q, want \"docx\"", repo.created.TargetFormat)
		}
		if repo.created.Engine != convert.EngineProbe {
			t.Errorf("created.Engine = %q, want %q", repo.created.Engine, convert.EngineProbe)
		}
		if repo.created.DispatchOwner != "protocol" || repo.created.ProtocolAttemptBudget != 600*time.Second {
			t.Fatalf("created = %+v, want protocol probe at 600s", repo.created)
		}
		if q.enqueueCalls != 0 || q.enqueuedProbe != uuid.Nil {
			t.Fatalf("enqueueCalls = %d enqueuedProbe = %s", q.enqueueCalls, q.enqueuedProbe)
		}
	})

	t.Run("convert is still rejected", func(t *testing.T) {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		q := &fakeQueue{}
		srv, _ := newTestServer(repo, store, q)

		body, ct := multipartBody(t, "in.docx", "pdf", macroDocxFixture(t))
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422 -- opening the gate for sanitize must not open it "+
				"for convert; body=%s", rec.Code, rec.Body.String())
		}
		if store.uploadCalls != 0 {
			t.Errorf("storage.Upload called %d times, want 0", store.uploadCalls)
		}
	})

	t.Run("a mislabelled macro package is still a mismatch", func(t *testing.T) {
		repo := &fakeRepo{}
		store := &fakeStorage{}
		q := &fakeQueue{}
		srv, _ := newTestServer(repo, store, q)

		// Declared .docx, actually macro-carrying. The extension check must
		// still fire: admitting macros for sanitize is not a licence to stop
		// checking that the filename tells the truth.
		body, ct := multipartBodyWithOperation(t, "in.docx", "docx", "sanitize", macroDocxFixture(t))
		req := authed(httptest.NewRequest(http.MethodPost, "/v1/jobs", body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte("does not match detected content")) {
			t.Errorf("body does not report a declared/detected mismatch: %s", rec.Body.String())
		}
	})
}
