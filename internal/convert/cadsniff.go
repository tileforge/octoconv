package convert

import (
	"bytes"
	"io"
)

type CADRejection string

const CADRejectNone CADRejection = ""

type CADResult struct {
	Format    string
	Rejection CADRejection
}

const (
	CADHeadBytes = 4096
	CADTailBytes = 4096
)

var (
	stepMagic     = []byte("ISO-10303-21;")
	stepHeaderTag = []byte("HEADER;")
	stepEndTag    = []byte("END-ISO-10303-21;")
)

func IsCADDeclared(format string) bool {
	switch format {
	case "step", "iges", "stl", "dxf", "dwg":
		return true
	}
	return false
}

func ReadCADTail(r io.ReaderAt, size int64) ([]byte, error) {
	if size <= 0 {
		return nil, nil
	}
	offset := size - CADTailBytes
	if offset < 0 {
		offset = 0
	}
	buf := make([]byte, size-offset)
	n, err := r.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:n], nil
}

func ReadCADWindows(r io.ReaderAt, size int64) (head, tail []byte, err error) {
	if size <= 0 {
		return nil, nil, nil
	}
	n := size
	if n > CADHeadBytes {
		n = CADHeadBytes
	}
	buf := make([]byte, n)
	read, err := r.ReadAt(buf, 0)
	if err != nil && err != io.EOF {
		return nil, nil, err
	}
	tail, err = ReadCADTail(r, size)
	if err != nil {
		return nil, nil, err
	}
	return buf[:read], tail, nil
}

func isASCIISpace(b byte) bool {
	switch b {
	case ' ', '\t', '\r', '\n', '\f', '\v':
		return true
	}
	return false
}

func trimLeftASCIISpace(b []byte) []byte {
	i := 0
	for i < len(b) && isASCIISpace(b[i]) {
		i++
	}
	return b[i:]
}

func trimRightASCIISpace(b []byte) []byte {
	n := len(b)
	for n > 0 && isASCIISpace(b[n-1]) {
		n--
	}
	return b[:n]
}

func SniffCADWindows(head, tail []byte, size int64, declared string) CADResult {
	lead := trimLeftASCIISpace(head)
	if bytes.HasPrefix(lead, stepMagic) &&
		bytes.Contains(head, stepHeaderTag) &&
		bytes.HasSuffix(trimRightASCIISpace(tail), stepEndTag) {
		return CADResult{Format: "step"}
	}
	return CADResult{}
}
