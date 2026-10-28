package api

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/tileforge/octoconv/internal/auth"
	"github.com/tileforge/octoconv/internal/errcode"
	"github.com/tileforge/octoconv/internal/jobs"
)

const archiveMaxBytes int64 = 256 << 20

// Bound open object readers and concurrent streams across API servers in this process.
var archiveSlots = make(chan struct{}, 2)

type archiveDownloader interface {
	Download(context.Context, string) (io.ReadCloser, error)
}
type archiveInputs interface {
	Inputs(context.Context, uuid.UUID) ([]jobs.Input, error)
}
type archiveEntry struct {
	name   string
	output jobs.Output
	reader io.ReadCloser
}

func archiveComponent(raw string) string {
	raw = path.Base(strings.ReplaceAll(raw, "\\", "/"))
	var b strings.Builder
	for _, c := range raw {
		if b.Len() >= 80 {
			break
		}
		if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '-' || c == '_' {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "result"
	}
	return b.String()
}

func (s *Server) handleArchive(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var request struct {
		IDs []uuid.UUID `json:"job_ids"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&request); err != nil {
		writeError(w, 400, errcode.InvalidRequest, "invalid archive request")
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF || len(request.IDs) == 0 || len(request.IDs) > 20 {
		writeError(w, 400, errcode.InvalidRequest, "select 1 to 20 unique jobs")
		return
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range request.IDs {
		if id == uuid.Nil || seen[id] {
			writeError(w, 400, errcode.InvalidRequest, "select unique valid jobs")
			return
		}
		seen[id] = true
	}
	select {
	case archiveSlots <- struct{}{}:
		defer func() { <-archiveSlots }()
	default:
		w.Header().Set("Retry-After", "5")
		writeError(w, 429, errcode.RateLimited, "archive capacity exhausted; try again")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	// A streaming failure must cut the HTTP body short rather than finalize a partial ZIP.
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Now().Add(2 * time.Minute))
	defer controller.SetWriteDeadline(time.Time{})
	client, _ := auth.ClientFromContext(ctx)
	// Authorize the entire selection before metadata or object reads.
	for _, id := range request.IDs {
		job, err := s.repo.Get(ctx, id)
		if errors.Is(err, jobs.ErrNotFound) || (err == nil && job != nil && job.ClientID != client.ID) {
			writeError(w, 404, errcode.NotFound, "job not found")
			return
		}
		if err != nil || job == nil {
			writeError(w, 500, errcode.InternalError, "failed to load job")
			return
		}
		if job.Status != jobs.StatusDone {
			writeError(w, 409, errcode.InvalidRequest, "all selected jobs must be completed")
			return
		}
	}
	downloader, ok := s.storage.(archiveDownloader)
	if !ok {
		writeError(w, 501, errcode.InvalidRequest, "archive download unavailable")
		return
	}
	var entries []archiveEntry
	var total int64
	for _, id := range request.IDs {
		outputs, err := s.repo.Outputs(ctx, id)
		if err != nil {
			writeError(w, 500, errcode.InternalError, "failed to load outputs")
			return
		}
		if len(outputs) == 0 {
			writeError(w, 409, errcode.InvalidRequest, "selected result is unavailable")
			return
		}
		folder := id.String()
		if repo, ok := s.repo.(archiveInputs); ok {
			inputs, err := repo.Inputs(ctx, id)
			if err != nil {
				writeError(w, 500, errcode.InternalError, "failed to load source name")
				return
			}
			if len(inputs) > 0 {
				folder = archiveComponent(inputs[0].Filename) + "-" + folder
			}
		}
		for index, output := range outputs {
			if output.SizeBytes < 0 || output.SizeBytes > archiveMaxBytes-total || len(entries) >= 100 {
				writeError(w, 413, errcode.PayloadTooLarge, "archive exceeds 100 outputs or 256 MiB")
				return
			}
			total += output.SizeBytes
			name := fmt.Sprintf("%s/%03d-%s-%d.%s", folder, index, archiveComponent(output.Role), output.Ordinal, archiveComponent(output.Format))
			entries = append(entries, archiveEntry{name: name, output: output})
		}
	}
	defer func() {
		for _, entry := range entries {
			if entry.reader != nil {
				_ = entry.reader.Close()
			}
		}
	}()
	for index := range entries {
		reader, err := downloader.Download(ctx, entries[index].output.ObjectKey)
		if err != nil {
			writeError(w, 409, errcode.InvalidRequest, "selected result is unavailable")
			return
		}
		entries[index].reader = reader
	}
	if ctx.Err() != nil {
		writeError(w, 504, errcode.InternalError, "archive preparation timed out")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="results.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	zw := zip.NewWriter(w)
	for _, entry := range entries {
		if ctx.Err() != nil {
			panic(http.ErrAbortHandler)
		}
		target, err := zw.CreateHeader(&zip.FileHeader{Name: entry.name, Method: zip.Store})
		if err != nil {
			panic(http.ErrAbortHandler)
		}
		n, err := io.CopyN(target, entry.reader, entry.output.SizeBytes)
		if err != nil || n != entry.output.SizeBytes {
			panic(http.ErrAbortHandler)
		}
		var extra [1]byte
		nExtra, err := io.ReadFull(entry.reader, extra[:])
		if nExtra != 0 || err != io.EOF {
			panic(http.ErrAbortHandler)
		}
		_ = entry.reader.Close()
	}
	if ctx.Err() != nil {
		panic(http.ErrAbortHandler)
	}
	if err := zw.Close(); err != nil {
		panic(http.ErrAbortHandler)
	}
}
