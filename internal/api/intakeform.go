package api

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
)

var errFormFieldsTooLarge = errors.New("multipart form: non-file data above its bound")

type intakeFormGate struct {
	src        io.Reader
	shadow     *multipart.Reader
	part       *multipart.Part
	partIsFile bool
	queue      []byte
	scratch    []byte
	raw        int64
	fileBytes  int64
	limit      int64
	srcErr     error
	shadowErr  error
	exceeded   bool
	done       bool
}

type intakeFormSource struct{ g *intakeFormGate }

func (s intakeFormSource) Read(p []byte) (int, error) {
	g := s.g
	n, err := g.src.Read(p)
	g.raw += int64(n)
	if g.raw-g.fileBytes > g.limit {
		g.exceeded = true
		return 0, errFormFieldsTooLarge
	}
	g.queue = append(g.queue, p[:n]...)
	if err != nil && g.srcErr == nil {
		g.srcErr = err
	}
	return n, err
}

func (g *intakeFormGate) step() {
	if g.part == nil {
		p, err := g.shadow.NextPart()
		if err != nil {
			g.done = true
			g.shadowErr = err
			return
		}
		g.part = p
		g.partIsFile = p.FormName() != "" && p.FileName() != ""
		return
	}
	n, err := g.part.Read(g.scratch)
	if g.partIsFile {
		g.fileBytes += int64(n)
	}
	switch {
	case err == io.EOF:
		g.part = nil
	case err != nil:
		g.done = true
		g.shadowErr = err
	}
}

func (g *intakeFormGate) Read(p []byte) (int, error) {
	for len(g.queue) == 0 && !g.done {
		g.step()
	}
	if len(g.queue) > 0 {
		n := copy(p, g.queue)
		g.queue = g.queue[:copy(g.queue, g.queue[n:])]
		return n, nil
	}
	switch {
	case g.exceeded:
		return 0, errFormFieldsTooLarge
	case g.srcErr != nil && g.srcErr != io.EOF:
		return 0, g.srcErr
	case g.shadowErr != nil && g.shadowErr != io.EOF:
		return 0, g.shadowErr
	}
	return 0, io.EOF
}

func parseIntakeForm(r *http.Request) error {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return http.ErrNotMultipart
	}
	if r.Body == nil {
		return r.ParseMultipartForm(multipartInMemoryBudget)
	}
	body := r.Body
	g := &intakeFormGate{src: body, scratch: make([]byte, 4096), limit: maxFormNonFileBytes}
	g.shadow = multipart.NewReader(intakeFormSource{g}, params["boundary"])
	r.Body = io.NopCloser(g)
	err = r.ParseMultipartForm(multipartInMemoryBudget)
	r.Body = body
	if g.exceeded {
		dropMultipartForm(r)
		return errFormFieldsTooLarge
	}
	return err
}
