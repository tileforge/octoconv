package api

import (
	"io"
	"time"
)

type inspectIntakeReader struct {
	body        io.ReadCloser
	setDeadline func(time.Time) error
	start       time.Time
	window      time.Duration
	minRate     int64
	limit       int64
	received    int64
}

func (r *inspectIntakeReader) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if n > 0 {
		r.received += int64(n)
		_ = r.setDeadline(r.deadline())
	}
	return n, err
}

func (r *inspectIntakeReader) Close() error {
	return r.body.Close()
}

func (r *inspectIntakeReader) deadline() time.Time {
	credited := min(r.received, max(r.limit, 0))
	return r.start.Add(r.window + time.Duration(credited/r.minRate)*time.Second + time.Duration(credited%r.minRate)*time.Second/time.Duration(r.minRate))
}
