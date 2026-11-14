package inspect

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
)

const stlCensusTriangles = 1 << 15

var stlVertex = regexp.MustCompile(`(?im)^[ \t]*vertex[ \t]+(\S+)[ \t]+(\S+)[ \t]+(\S+)[ \t]*\r?$`)

var stlCodes = []Code{CodeSTLOpen, CodeSTLNonManifold, CodeSTLUnoriented}

var errSTLCeiling = errors.New("stl census ceiling")

// AnalyzeSTL reports stl_open, stl_non_manifold, and stl_unoriented for one complete STL.
func AnalyzeSTL(r io.ReaderAt, size, maxBytes int64, tr *Tracker) FamilyResult {
	if tr != nil && tr.Exhausted() {
		return stlUnknown(tr.ceilingReason())
	}
	if size > maxBytes {
		tr.MarkCeilingHit()
		return stlUnknown(ReasonWorkCeilingBytes)
	}
	if r == nil || size <= 0 {
		return stlUnknown(ReasonMalformedInput)
	}
	buf := make([]byte, size)
	n, err := r.ReadAt(buf, 0)
	if int64(n) != size {
		if err != nil && !errors.Is(err, io.EOF) {
			return stlUnknown(ReasonReadFailed)
		}
		return stlUnknown(ReasonMalformedInput)
	}
	codes, err := classifySTL(buf)
	if err != nil {
		if errors.Is(err, errSTLCeiling) {
			tr.MarkCeilingHit()
			return stlUnknown(ReasonWorkCeilingBytes)
		}
		return stlUnknown(ReasonMalformedInput)
	}
	return stlResult(codes)
}

func stlResult(found []Code) FamilyResult {
	present := make(map[Code]bool, len(found))
	res := FamilyResult{}
	for _, code := range found {
		sev, _ := SeverityOf(code)
		res.Findings = append(res.Findings, Finding{Code: code, Severity: sev})
		present[code] = true
	}
	for _, code := range stlCodes {
		if present[code] {
			continue
		}
		res.Checks = append(res.Checks, Check{Code: code, Status: StatusClean})
	}
	return res
}

func stlUnknown(reason Reason) FamilyResult {
	res := FamilyResult{}
	for _, code := range stlCodes {
		res.Checks = append(res.Checks, Check{Code: code, Status: StatusUnknown, Reason: reason})
	}
	return res
}

func classifySTL(buf []byte) ([]Code, error) {
	if count, ok := binaryCount(buf); ok {
		if int(count) > stlCensusTriangles {
			return nil, errSTLCeiling
		}
		return census(weld(cornersFromBinary(buf, count))), nil
	}
	corners, err := asciiCorners(buf)
	if err != nil {
		return nil, err
	}
	return census(weld(corners)), nil
}

func binaryCount(buf []byte) (uint32, bool) {
	if len(buf) < 84 {
		return 0, false
	}
	count := binary.LittleEndian.Uint32(buf[80:84])
	if int64(len(buf)) != 84+50*int64(count) {
		return 0, false
	}
	return count, true
}

func cornersFromBinary(buf []byte, count uint32) []stlCorner {
	corners := make([]stlCorner, 3*int(count))
	var seq int32
	for i := uint32(0); i < count; i++ {
		rec := buf[84+50*int64(i):]
		for v := 0; v < 3; v++ {
			var p [3]float64
			for axis := 0; axis < 3; axis++ {
				off := 12 + 4*(3*v+axis)
				p[axis] = float64(math.Float32frombits(binary.LittleEndian.Uint32(rec[off:])))
			}
			corners[seq] = cornerOf(p, seq)
			seq++
		}
	}
	return corners
}

func asciiCorners(buf []byte) ([]stlCorner, error) {
	n, ok := countASCIIVertices(buf)
	if !ok {
		return nil, errSTLCeiling
	}
	if n == 0 {
		if looksLikeASCIISTL(buf) {
			return []stlCorner{}, nil
		}
		return nil, errSTLMalformed
	}
	if n%3 != 0 || n/3 > stlCensusTriangles {
		if n/3 > stlCensusTriangles {
			return nil, errSTLCeiling
		}
		return nil, errSTLMalformed
	}
	corners := make([]stlCorner, n)
	rest := buf
	for seq := 0; seq < n; seq++ {
		loc := stlVertex.FindSubmatchIndex(rest)
		if loc == nil {
			return nil, errSTLMalformed
		}
		var p [3]float64
		for axis := 0; axis < 3; axis++ {
			raw := rest[loc[2+2*axis]:loc[3+2*axis]]
			f, err := strconv.ParseFloat(string(raw), 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				return nil, errSTLMalformed
			}
			p[axis] = f
		}
		corners[seq] = cornerOf(p, int32(seq))
		rest = rest[loc[1]:]
	}
	return corners, nil
}

func countASCIIVertices(buf []byte) (int, bool) {
	n := 0
	rest := buf
	for {
		loc := stlVertex.FindIndex(rest)
		if loc == nil {
			return n, true
		}
		n++
		if n > stlCensusTriangles*3 {
			return n, false
		}
		rest = rest[loc[1]:]
	}
}

var errSTLMalformed = errors.New("stl malformed")

type stlCorner struct {
	bits [3]uint64
	seq  int32
	nan  bool
}

func cornerOf(p [3]float64, seq int32) stlCorner {
	if hasNaN(p) {
		return stlCorner{seq: seq, nan: true}
	}
	return stlCorner{bits: [3]uint64{canonBits(p[0]), canonBits(p[1]), canonBits(p[2])}, seq: seq}
}

func weld(corners []stlCorner) []int32 {
	sort.Slice(corners, func(i, j int) bool {
		a, b := corners[i], corners[j]
		if a.nan != b.nan {
			return !a.nan
		}
		if a.nan {
			return a.seq < b.seq
		}
		if a.bits[0] != b.bits[0] {
			return a.bits[0] < b.bits[0]
		}
		if a.bits[1] != b.bits[1] {
			return a.bits[1] < b.bits[1]
		}
		if a.bits[2] != b.bits[2] {
			return a.bits[2] < b.bits[2]
		}
		return a.seq < b.seq
	})
	ids := make([]int32, len(corners))
	var id int32
	for i := range corners {
		if i > 0 && !sameCorner(corners[i], corners[i-1]) {
			id++
		}
		ids[corners[i].seq] = id
	}
	return ids
}

func sameCorner(a, b stlCorner) bool {
	return !a.nan && !b.nan && a.bits == b.bits
}

type stlEdge struct {
	lo, hi int32
	dir    int8
}

func census(ids []int32) []Code {
	edges := make([]stlEdge, 0, len(ids))
	for t := 0; t+2 < len(ids); t += 3 {
		a, b, c := ids[t], ids[t+1], ids[t+2]
		if a == b || b == c || c == a {
			continue
		}
		edges = append(edges, edgeOf(a, b), edgeOf(b, c), edgeOf(c, a))
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].lo != edges[j].lo {
			return edges[i].lo < edges[j].lo
		}
		return edges[i].hi < edges[j].hi
	})
	var opened, nonmanifold, unoriented bool
	for i := 0; i < len(edges); {
		j := i + 1
		for j < len(edges) && edges[j].lo == edges[i].lo && edges[j].hi == edges[i].hi {
			j++
		}
		switch count := j - i; {
		case count == 1:
			opened = true
		case count > 2:
			nonmanifold = true
		case edges[i].dir == edges[i+1].dir:
			unoriented = true
		}
		i = j
	}
	var found []Code
	if opened {
		found = append(found, CodeSTLOpen)
	}
	if nonmanifold {
		found = append(found, CodeSTLNonManifold)
	}
	if unoriented {
		found = append(found, CodeSTLUnoriented)
	}
	return found
}

func edgeOf(a, b int32) stlEdge {
	if b < a {
		return stlEdge{lo: b, hi: a, dir: -1}
	}
	return stlEdge{lo: a, hi: b, dir: 1}
}

func looksLikeASCIISTL(buf []byte) bool {
	head := bytes.TrimLeft(buf, " \t\r\n")
	if len(head) < len("solid") || !bytes.EqualFold(head[:len("solid")], []byte("solid")) {
		return false
	}
	tail := bytes.TrimRight(buf, " \t\r\n")
	line := tail[bytes.LastIndexByte(tail, '\n')+1:]
	line = bytes.TrimLeft(line, " \t")
	return len(line) >= len("endsolid") && bytes.EqualFold(line[:len("endsolid")], []byte("endsolid")) && bytes.IndexByte(line, 0) < 0
}

func canonBits(f float64) uint64 {
	if f == 0 {
		return math.Float64bits(0)
	}
	return math.Float64bits(f)
}

func hasNaN(v [3]float64) bool {
	return math.IsNaN(v[0]) || math.IsNaN(v[1]) || math.IsNaN(v[2])
}
