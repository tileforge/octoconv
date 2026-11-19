package api

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

type boundFormCase struct {
	path     string
	filename string
	target   string
	data     func() []byte
	accepted int
}

func boundFormCases() []boundFormCase {
	return []boundFormCase{
		{"/v1/jobs", "in.md", "html", func() []byte { return paddedMarkdown(4096) }, http.StatusAccepted},
		{"/v1/inspect", "in.png", "", pngBytesFixture, http.StatusOK},
	}
}

func formWithUnknownField(t *testing.T, pad int, filename, target string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, err := w.CreateFormField("x_unknown")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(bytes.Repeat([]byte{'u'}, pad)); err != nil {
		t.Fatal(err)
	}
	fw, err = w.CreateFormFile(formFieldFile, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	if target != "" {
		if err := w.WriteField(formFieldTarget, target); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &b, w.FormDataContentType()
}

func formWithNonFileBytes(t *testing.T, nonFile int, filename, target string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	probe, _ := formWithUnknownField(t, 1, filename, target, data)
	body, ct := formWithUnknownField(t, 1+nonFile-(probe.Len()-len(data)), filename, target, data)
	if got := body.Len() - len(data); got != nonFile {
		t.Fatalf("the form carries %d bytes of non-file data, want exactly %d", got, nonFile)
	}
	return body, ct
}

type boundFormResult struct {
	rec   *httptest.ResponseRecorder
	read  int64
	repo  *fakeRepo
	store *fakeStorage
}

func postBoundForm(t *testing.T, path, contentType string, body io.Reader) boundFormResult {
	t.Helper()
	repo, store := &fakeRepo{}, &fakeStorage{}
	srv := NewServer(repo, store, &fakeQueue{}, defaultFakePresetRepo(), defaultFakePresetAdmin(), &fakeUsageRepo{}, &fakeLimitsRepo{}, newFakeResolver(), healthyDeps(), createIntakeConfig(64<<20))
	srv.SetCapabilityCatalog(pandocCatalog())
	counted := &countingBody{r: body}
	req := authed(httptest.NewRequest(http.MethodPost, path, counted))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return boundFormResult{rec: rec, read: counted.n.Load(), repo: repo, store: store}
}

func TestIntakeRefusesNonFileFormDataOverTheBound(t *testing.T) {
	for _, c := range boundFormCases() {
		t.Run(c.path+" at the acceptance line", func(t *testing.T) {
			body, ct := formWithNonFileBytes(t, maxFormNonFileBytes-multipartPeekBufferBytes, c.filename, c.target, c.data())
			res := postBoundForm(t, c.path, ct, body)
			if res.rec.Code != c.accepted {
				t.Errorf("%s: %d bytes of non-file data: status = %d body %s, want %d", c.path, maxFormNonFileBytes-multipartPeekBufferBytes, res.rec.Code, res.rec.Body.Bytes(), c.accepted)
			}
		})
		t.Run(c.path+" one byte over the bound", func(t *testing.T) {
			body, ct := formWithNonFileBytes(t, maxFormNonFileBytes+1, c.filename, c.target, c.data())
			res := postBoundForm(t, c.path, ct, body)
			if res.rec.Code != http.StatusRequestEntityTooLarge || errorMessage(t, res.rec.Body.Bytes()) != formFieldsTooLargeMessage {
				t.Errorf("%s: %d bytes of non-file data: status = %d body %s, want 413 %q", c.path, maxFormNonFileBytes+1, res.rec.Code, res.rec.Body.Bytes(), formFieldsTooLargeMessage)
			}
			if res.store.uploaded || res.repo.preAdmitCalls != 0 {
				t.Errorf("%s: uploaded %v, PreAdmit called %d times, want neither", c.path, res.store.uploaded, res.repo.preAdmitCalls)
			}
		})
		t.Run(c.path+" verifier probe", func(t *testing.T) {
			body, ct := formWithUnknownField(t, 10<<20-64<<10, c.filename, c.target, c.data())
			res := postBoundForm(t, c.path, ct, body)
			t.Logf("%s: status %d after %d bytes of the body were read; body %s", c.path, res.rec.Code, res.read, res.rec.Body.Bytes())
			if res.rec.Code != http.StatusRequestEntityTooLarge {
				t.Errorf("%s: status = %d, want 413", c.path, res.rec.Code)
			}
			assertErrorCode(t, res.rec.Body.Bytes(), "payload_too_large")
			if got := errorMessage(t, res.rec.Body.Bytes()); got != formFieldsTooLargeMessage {
				t.Errorf("%s: error = %q, want %q", c.path, got, formFieldsTooLargeMessage)
			}
			if res.read > maxFormNonFileBytes+64<<10 {
				t.Errorf("%s: %d bytes of the body were read, want at most %d", c.path, res.read, maxFormNonFileBytes+64<<10)
			}
			if res.store.uploaded || res.repo.preAdmitCalls != 0 {
				t.Errorf("%s: uploaded %v, PreAdmit called %d times, want neither", c.path, res.store.uploaded, res.repo.preAdmitCalls)
			}
		})
	}
}

func TestIntakeIgnoresUnknownFieldsWithinTheBound(t *testing.T) {
	for _, c := range boundFormCases() {
		t.Run(c.path, func(t *testing.T) {
			body, ct := formWithUnknownField(t, 1<<10, c.filename, c.target, c.data())
			res := postBoundForm(t, c.path, ct, body)
			if res.rec.Code != c.accepted {
				t.Errorf("%s: status = %d body %s, want %d", c.path, res.rec.Code, res.rec.Body.Bytes(), c.accepted)
			}
		})
	}
}

func TestIntakeRefusesANonMultipartBodyUnread(t *testing.T) {
	for _, c := range boundFormCases() {
		for _, contentType := range []string{"application/x-www-form-urlencoded", "application/x-www-form-urlencoded; boundary=intakeform"} {
			t.Run(c.path+" "+contentType, func(t *testing.T) {
				body := io.MultiReader(strings.NewReader("a="), strings.NewReader(strings.Repeat("b", 1<<20)))
				res := postBoundForm(t, c.path, contentType, body)
				t.Logf("%s %s: status %d after %d bytes of the body were read", c.path, contentType, res.rec.Code, res.read)
				if res.rec.Code != http.StatusBadRequest {
					t.Errorf("%s: status = %d, want 400", c.path, res.rec.Code)
				}
				assertErrorCode(t, res.rec.Body.Bytes(), "invalid_request")
				if got := errorMessage(t, res.rec.Body.Bytes()); got != "invalid multipart form" {
					t.Errorf("%s: error = %q, want %q", c.path, got, "invalid multipart form")
				}
				if res.read != 0 {
					t.Errorf("%s %s: %d bytes of a non-multipart body were read, want 0", c.path, contentType, res.read)
				}
			})
		}
	}
}

const hostileBoundary = "hostile0form0boundary0f3a9c"

type hostileForm struct {
	body      []byte
	fileBytes int64
	closeEnd  int64
}

func (h hostileForm) nonFile() int64 { return h.closeEnd - h.fileBytes }

type hostileFormBuilder struct {
	t         *testing.T
	b         bytes.Buffer
	fileBytes int64
	parts     int
}

func newHostileForm(t *testing.T) *hostileFormBuilder { return &hostileFormBuilder{t: t} }

func (f *hostileFormBuilder) delimiter() string {
	if f.parts == 0 {
		return "--" + hostileBoundary + "\r\n"
	}
	return "\r\n--" + hostileBoundary + "\r\n"
}

func (f *hostileFormBuilder) part(header string, content []byte, decoded int) {
	f.b.WriteString(f.delimiter() + header + "\r\n")
	f.b.Write(content)
	f.fileBytes += int64(decoded)
	f.parts++
}

func (f *hostileFormBuilder) field(name string, value []byte) {
	f.part(fmt.Sprintf("Content-Disposition: form-data; name=%q\r\n", name), value, 0)
}

func (f *hostileFormBuilder) file(name, filename, extraHeader string, content []byte) {
	f.part(fmt.Sprintf("Content-Disposition: form-data; name=%q; filename=%q\r\nContent-Type: application/octet-stream\r\n", name, filename)+extraHeader, content, len(content))
}

func (f *hostileFormBuilder) closedNonFile() int64 {
	return int64(f.b.Len()+len("\r\n--"+hostileBoundary+"--")) - f.fileBytes
}

func (f *hostileFormBuilder) padTo(target int64) {
	f.t.Helper()
	header := fmt.Sprintf("Content-Disposition: form-data; name=%q\r\n", "zpad")
	n := target - f.closedNonFile() - int64(len(f.delimiter()+header+"\r\n"))
	if n < 0 {
		f.t.Fatalf("the form already carries %d bytes of non-file data, more than the target %d", f.closedNonFile(), target)
	}
	f.field("zpad", bytes.Repeat([]byte{'z'}, int(n)))
}

func (f *hostileFormBuilder) close(epilogue []byte) hostileForm {
	f.b.WriteString("\r\n--" + hostileBoundary + "--")
	end := int64(f.b.Len())
	f.b.WriteString("\r\n")
	f.b.Write(epilogue)
	return hostileForm{body: f.b.Bytes(), fileBytes: f.fileBytes, closeEnd: end}
}

func (f *hostileFormBuilder) exactly(target int64, epilogue []byte) hostileForm {
	f.t.Helper()
	h := f.close(epilogue)
	if h.nonFile() != target {
		f.t.Fatalf("the form carries %d bytes of non-file data, want exactly %d", h.nonFile(), target)
	}
	return h
}

func hostileFileContent(n int, seed uint64) []byte {
	rng := rand.New(rand.NewPCG(seed, 78))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Uint32())
	}
	return b
}

func hostileHeaderPadded(t *testing.T, target int64) hostileForm {
	t.Helper()
	build := func(pad int) *hostileFormBuilder {
		f := newHostileForm(t)
		f.field(formFieldTarget, []byte("glb"))
		f.file(formFieldFile, "in.stl", "X-Pad: "+strings.Repeat("p", pad)+"\r\n", hostileFileContent(1<<10, 1))
		return f
	}
	probe := build(1).close(nil)
	return build(1+int(target-probe.nonFile())).exactly(target, nil)
}

func hostileSmallValues(t *testing.T, target int64) hostileForm {
	t.Helper()
	f := newHostileForm(t)
	value := bytes.Repeat([]byte{'s'}, 16)
	for i := 1; ; i++ {
		next := newHostileForm(t)
		next.parts = 1
		next.field(fmt.Sprintf("f%04d", i), value)
		if f.closedNonFile()+int64(next.b.Len())+256 > target {
			break
		}
		f.field(fmt.Sprintf("f%04d", i), value)
	}
	f.padTo(target)
	return f.exactly(target, nil)
}

func hostileEmptyFileParts(t *testing.T, target int64) hostileForm {
	t.Helper()
	f := newHostileForm(t)
	for i := 1; ; i++ {
		next := newHostileForm(t)
		next.parts = 1
		next.file(fmt.Sprintf("e%04d", i), "a", "", nil)
		if f.closedNonFile()+int64(next.b.Len())+256 > target {
			break
		}
		f.file(fmt.Sprintf("e%04d", i), "a", "", nil)
	}
	f.padTo(target)
	return f.exactly(target, nil)
}

type hostileFormCase struct {
	name    string
	build   func(t *testing.T) hostileForm
	refused bool
}

func hostileFormCases() []hostileFormCase {
	line := int64(maxFormNonFileBytes - multipartPeekBufferBytes)
	over := int64(maxFormNonFileBytes + 1)
	return []hostileFormCase{
		{"value at the admit line", func(t *testing.T) hostileForm {
			f := newHostileForm(t)
			f.padTo(line)
			return f.exactly(line, nil)
		}, false},
		{"value over the bound", func(t *testing.T) hostileForm {
			f := newHostileForm(t)
			f.padTo(over)
			return f.exactly(over, nil)
		}, true},
		{"small values at the admit line", func(t *testing.T) hostileForm { return hostileSmallValues(t, line) }, false},
		{"small values over the bound", func(t *testing.T) hostileForm { return hostileSmallValues(t, over) }, true},
		{"empty file parts at the admit line", func(t *testing.T) hostileForm { return hostileEmptyFileParts(t, line) }, false},
		{"file header at the admit line", func(t *testing.T) hostileForm { return hostileHeaderPadded(t, line) }, false},
		{"file header over the bound", func(t *testing.T) hostileForm { return hostileHeaderPadded(t, over) }, true},
		{"preamble over the bound", func(t *testing.T) hostileForm {
			f := newHostileForm(t)
			preamble := strings.Repeat(strings.Repeat("q", 62)+"\r\n", int(over)/64+1)
			f.b.WriteString(preamble)
			f.field(formFieldTarget, []byte("glb"))
			f.file(formFieldFile, "in.stl", "", hostileFileContent(1<<10, 2))
			return f.close(nil)
		}, true},
		{"nameless parts over the bound", func(t *testing.T) hostileForm {
			f := newHostileForm(t)
			for i := 0; int64(i*1000) <= over; i++ {
				f.part("Content-Disposition: form-data\r\n", bytes.Repeat([]byte{'n'}, 1000), 0)
			}
			f.field(formFieldTarget, []byte("glb"))
			return f.close(nil)
		}, true},
		{"epilogue after a small form", func(t *testing.T) hostileForm {
			f := newHostileForm(t)
			f.field(formFieldTarget, []byte("glb"))
			f.file(formFieldFile, "in.stl", "", hostileFileContent(4<<10, 3))
			return f.close(bytes.Repeat([]byte{'e'}, 1<<20))
		}, false},
		{"quoted-printable file part", func(t *testing.T) hostileForm {
			f := newHostileForm(t)
			var qp strings.Builder
			for i := 1; i <= 64<<10; i++ {
				qp.WriteString("=00")
				if i%25 == 0 {
					qp.WriteString("=\r\n")
				}
			}
			f.part(fmt.Sprintf("Content-Disposition: form-data; name=%q; filename=%q\r\nContent-Transfer-Encoding: quoted-printable\r\n", formFieldFile, "in.bin"), []byte(qp.String()), 64<<10)
			return f.close(nil)
		}, true},
		{"read-ahead interleave", func(t *testing.T) hostileForm {
			f := newHostileForm(t)
			for i := 0; i < 150; i++ {
				f.file(fmt.Sprintf("g%03d", i), "g.bin", "", hostileFileContent(3500, uint64(100+i)))
				f.field(fmt.Sprintf("h%03d", i), bytes.Repeat([]byte{'h'}, 500))
			}
			return f.close(nil)
		}, true},
		{"read-ahead resonance", func(t *testing.T) hostileForm {
			f := newHostileForm(t)
			f.field("lead", bytes.Repeat([]byte{'l'}, 1000))
			for i := 0; i < 150; i++ {
				f.file(fmt.Sprintf("g%03d", i), "g.bin", "", hostileFileContent(3338, uint64(400+i)))
				f.field(fmt.Sprintf("h%03d", i), bytes.Repeat([]byte{'h'}, 500))
			}
			return f.close(nil)
		}, true},
		{"file with fields at the admit line", func(t *testing.T) hostileForm {
			f := newHostileForm(t)
			f.file(formFieldFile, "in.stl", "", hostileFileContent(1<<20, 4))
			f.field(formFieldTarget, []byte("glb"))
			f.field(formFieldOperation, []byte("convert"))
			f.padTo(line)
			return f.exactly(line, nil)
		}, false},
		{"RFC 2231 file name", func(t *testing.T) hostileForm {
			f := newHostileForm(t)
			f.part(fmt.Sprintf("Content-Disposition: form-data; name=%q; filename*=UTF-8''%%D1%%84%%D0%%B0%%D0%%B9%%D0%%BB.stl\r\nContent-Type: model/stl\r\n", formFieldFile), hostileFileContent(1<<20, 5), 1<<20)
			f.field(formFieldTarget, []byte("glb"))
			return f.close(nil)
		}, false},
	}
}

type chunkedReader struct {
	r    io.Reader
	size func() int
}

func (c chunkedReader) Read(p []byte) (int, error) {
	if n := c.size(); len(p) > n {
		p = p[:n]
	}
	return c.r.Read(p)
}

type bodySlicing struct {
	name string
	wrap func(body []byte) io.Reader
}

func bodySlicings() []bodySlicing {
	fixed := func(n int) func([]byte) io.Reader {
		return func(body []byte) io.Reader {
			return chunkedReader{r: bytes.NewReader(body), size: func() int { return n }}
		}
	}
	return []bodySlicing{
		{"whole", func(body []byte) io.Reader { return bytes.NewReader(body) }},
		{"one byte", func(body []byte) io.Reader { return iotest.OneByteReader(bytes.NewReader(body)) }},
		{"4096-byte chunks", fixed(4096)},
		{"4097-byte chunks", fixed(4097)},
		{"random chunks", func(body []byte) io.Reader {
			rng := rand.New(rand.NewPCG(78, 9))
			return chunkedReader{r: bytes.NewReader(body), size: func() int { return 1 + rng.IntN(8192) }}
		}},
	}
}

func retainedNonFileBytes(form *multipart.Form) int64 {
	var n int64
	for name, values := range form.Value {
		for _, v := range values {
			n += int64(len(name) + len(v))
		}
	}
	for name, files := range form.File {
		for _, fh := range files {
			n += int64(len(name) + len(fh.Filename))
			for k, vs := range fh.Header {
				for _, v := range vs {
					n += int64(len(k) + len(v))
				}
			}
		}
	}
	return n
}

type fileDigest struct {
	filename string
	size     int64
	header   map[string][]string
	sum      [sha256.Size]byte
}

func digestFiles(t *testing.T, form *multipart.Form) map[string][]fileDigest {
	t.Helper()
	out := make(map[string][]fileDigest, len(form.File))
	for name, files := range form.File {
		for _, fh := range files {
			f, err := fh.Open()
			if err != nil {
				t.Fatalf("open %s/%s: %v", name, fh.Filename, err)
			}
			h := sha256.New()
			_, err = io.Copy(h, f)
			f.Close()
			if err != nil {
				t.Fatalf("read %s/%s: %v", name, fh.Filename, err)
			}
			d := fileDigest{filename: fh.Filename, size: fh.Size, header: fh.Header}
			copy(d.sum[:], h.Sum(nil))
			out[name] = append(out[name], d)
		}
	}
	return out
}

func TestIntakeFormGateBoundsWhatReadFormHolds(t *testing.T) {
	line := int64(maxFormNonFileBytes - multipartPeekBufferBytes)
	for _, c := range hostileFormCases() {
		for _, s := range bodySlicings() {
			t.Run(c.name+" "+s.name, func(t *testing.T) {
				t.Setenv("TMPDIR", t.TempDir())
				form := c.build(t)
				req := httptest.NewRequest(http.MethodPost, "/v1/jobs", s.wrap(form.body))
				req.Header.Set("Content-Type", "multipart/form-data; boundary="+hostileBoundary)
				err := parseIntakeForm(req)
				defer dropMultipartForm(req)
				if errors.Is(err, errFormFieldsTooLarge) {
					if !c.refused {
						t.Errorf("refused a form the table admits: %d bytes of non-file data", form.nonFile())
					}
					if form.nonFile() <= line {
						t.Errorf("refused %d bytes of non-file data, at or below the admit line %d", form.nonFile(), line)
					}
					return
				}
				if err != nil {
					t.Fatalf("parseIntakeForm: %v (non-file data %d)", err, form.nonFile())
				}
				held := retainedNonFileBytes(req.MultipartForm)
				t.Logf("admitted: non-file data %d, held %d", form.nonFile(), held)
				if c.refused {
					t.Errorf("admitted a form the table refuses: %d bytes of non-file data, %d held", form.nonFile(), held)
				}
				if held > maxFormNonFileBytes {
					t.Errorf("the admitted form holds %d bytes of non-file data, above the bound %d", held, maxFormNonFileBytes)
				}
				plain, err := multipart.NewReader(bytes.NewReader(form.body), hostileBoundary).ReadForm(multipartInMemoryBudget)
				if err != nil {
					t.Fatalf("ReadForm without the gate: %v", err)
				}
				defer plain.RemoveAll()
				if !reflect.DeepEqual(req.MultipartForm.Value, plain.Value) {
					t.Errorf("values differ from ReadForm without the gate: %d keys gated, %d plain", len(req.MultipartForm.Value), len(plain.Value))
				}
				if gated, ref := digestFiles(t, req.MultipartForm), digestFiles(t, plain); !reflect.DeepEqual(gated, ref) {
					t.Errorf("files differ from ReadForm without the gate: %d names gated, %d plain", len(gated), len(ref))
				}
			})
		}
	}
}
