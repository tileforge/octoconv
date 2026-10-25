package api

import (
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

const refusalDrainGrace = 500 * time.Millisecond

type intakeReader struct {
	body        io.ReadCloser
	setDeadline func(time.Time) error
	start       time.Time
	window      time.Duration
	minRate     int64
	limit       int64
	received    int64
}

func (r *intakeReader) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if n > 0 {
		r.received += int64(n)
		if err == nil {
			_ = r.setDeadline(r.deadline())
		}
	}
	return n, err
}

func (r *intakeReader) Close() error {
	return r.body.Close()
}

func (r *intakeReader) deadline() time.Time {
	credited := min(r.received, max(r.limit, 0))
	return r.start.Add(r.window + time.Duration(credited/r.minRate)*time.Second + time.Duration(credited%r.minRate)*time.Second/time.Duration(r.minRate))
}

func (s *Server) armIntakeDeadline(w http.ResponseWriter) (*http.ResponseController, time.Time) {
	rc := http.NewResponseController(w)
	start := time.Now()
	_ = rc.SetReadDeadline(start.Add(s.intakeWindow))
	return rc, start
}

type bodyReadCounter struct {
	io.ReadCloser
	read atomic.Bool
}

func (b *bodyReadCounter) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.read.Store(true)
	}
	return n, err
}

type refusalWriter struct {
	http.ResponseWriter
	body     *bodyReadCounter
	answered bool
}

func (w *refusalWriter) WriteHeader(code int) {
	if !w.answered && code >= http.StatusBadRequest && !w.body.read.Load() {
		w.Header().Set("Connection", "close")
		_ = http.NewResponseController(w.ResponseWriter).SetReadDeadline(time.Now().Add(refusalDrainGrace))
	}
	if code >= http.StatusOK {
		w.answered = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *refusalWriter) Write(p []byte) (int, error) {
	w.answered = true
	return w.ResponseWriter.Write(p)
}

func (w *refusalWriter) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *refusalWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (s *Server) armBodyWindow(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.Body == http.NoBody {
			next.ServeHTTP(w, r)
			return
		}
		s.armIntakeDeadline(w)
		body := &bodyReadCounter{ReadCloser: r.Body}
		counted := r.WithContext(r.Context())
		counted.Body = body
		next.ServeHTTP(&refusalWriter{ResponseWriter: w, body: body}, counted)
	})
}

func (s *Server) newIntakeReader(w http.ResponseWriter, body io.ReadCloser, rc *http.ResponseController, start time.Time, limit int64) *intakeReader {
	return &intakeReader{
		body:        http.MaxBytesReader(w, body, limit),
		setDeadline: rc.SetReadDeadline,
		start:       start,
		window:      s.intakeWindow,
		minRate:     s.intakeMinRate,
		limit:       limit,
	}
}

func dropMultipartForm(r *http.Request) {
	if r.MultipartForm != nil {
		_ = r.MultipartForm.RemoveAll()
	}
	r.MultipartForm = nil
	r.Form = nil
	r.PostForm = nil
}
