package api

import (
	"io"
	"net/http"
	"time"
)

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
