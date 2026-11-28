package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/tileforge/octoconv/internal/auth"
	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/errcode"
	"github.com/tileforge/octoconv/internal/jobs"
)

var repeatSlots = make(chan struct{}, 2)

type repeatStorage interface {
	archiveDownloader
	ObjectExists(context.Context, string) (bool, error)
}

type repeatSource struct {
	job    *jobs.Job
	inputs []jobs.Input
	store  repeatStorage
	paths  []string
}

func (s *Server) loadRepeatSource(w http.ResponseWriter, r *http.Request) (repeatSource, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, errcode.InvalidRequest, "invalid job id")
		return repeatSource{}, false
	}
	job, err := s.repo.Get(r.Context(), id)
	client, _ := auth.ClientFromContext(r.Context())
	if errors.Is(err, jobs.ErrNotFound) || (err == nil && job != nil && job.ClientID != client.ID) {
		writeError(w, 404, errcode.NotFound, "job not found")
		return repeatSource{}, false
	}
	if err != nil || job == nil {
		writeError(w, 500, errcode.InternalError, "failed to load job")
		return repeatSource{}, false
	}
	if job.Status != jobs.StatusDone && job.Status != jobs.StatusFailed && job.Status != jobs.StatusCanceled {
		writeError(w, 409, errcode.InvalidRequest, "only terminal jobs can be repeated")
		return repeatSource{}, false
	}
	repo, ok := s.repo.(archiveInputs)
	store, storageOK := s.storage.(repeatStorage)
	if !ok || !storageOK {
		writeError(w, 501, errcode.InvalidRequest, "repeat unavailable")
		return repeatSource{}, false
	}
	inputs, err := repo.Inputs(r.Context(), id)
	if err != nil {
		writeError(w, 500, errcode.InternalError, "failed to load inputs")
		return repeatSource{}, false
	}
	if len(inputs) == 0 || len(inputs) > 100 {
		writeError(w, 409, errcode.InvalidRequest, "original input set is unavailable")
		return repeatSource{}, false
	}
	return repeatSource{job: job, inputs: inputs, store: store}, true
}
func acquireRepeat(w http.ResponseWriter) bool {
	select {
	case repeatSlots <- struct{}{}:
		return true
	default:
		w.Header().Set("Retry-After", "5")
		writeError(w, 429, errcode.RateLimited, "repeat preparation busy")
		return false
	}
}
func (s *Server) handleRepeatMetadata(w http.ResponseWriter, r *http.Request) {
	if !acquireRepeat(w) {
		return
	}
	defer func() { <-repeatSlots }()
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	source, ok := s.loadRepeatSource(w, r)
	if !ok {
		return
	}
	type inputInfo struct {
		Ordinal   int    `json:"ordinal"`
		Filename  string `json:"filename"`
		Format    string `json:"format"`
		Size      int64  `json:"size_bytes"`
		Available bool   `json:"available"`
	}
	inputs := make([]inputInfo, 0, len(source.inputs))
	for _, in := range source.inputs {
		available, err := source.store.ObjectExists(ctx, in.ObjectKey)
		if err != nil {
			writeError(w, 503, errcode.InternalError, "source storage temporarily unavailable")
			return
		}
		inputs = append(inputs, inputInfo{in.Ordinal, in.Filename, in.Format, in.SizeBytes, available})
	}
	w.Header().Set("Cache-Control", "no-store")
	opts := source.job.Opts
	if opts == nil {
		opts = map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"job_id": source.job.ID, "operation": source.job.Operation, "source_format": source.job.SourceFormat, "target_format": source.job.TargetFormat, "opts": opts, "inputs": inputs})
}
func (s *Server) handleRepeat(w http.ResponseWriter, r *http.Request) {
	r.Body = smallBodyReader(w, r.Body, smallBodyMaxBytes)
	var request struct {
		Operation string          `json:"operation"`
		Target    string          `json:"target"`
		Opts      json.RawMessage `json:"opts"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, 400, errcode.InvalidRequest, "invalid JSON body")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, 400, errcode.InvalidRequest, "invalid JSON body")
		return
	}
	if !acquireRepeat(w) {
		return
	}
	defer func() { <-repeatSlots }()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	r = r.WithContext(ctx)
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Minute))
	defer http.NewResponseController(w).SetWriteDeadline(time.Time{})
	source, ok := s.loadRepeatSource(w, r)
	if !ok {
		return
	}
	target := convert.NormalizeFormat(request.Target)
	engine, ok := convert.Default.EngineForOp(request.Operation, source.job.SourceFormat, target)
	if !ok {
		writeError(w, 422, errcode.UnsupportedConversion, "unsupported conversion")
		return
	}
	forwardOpts := request.Opts
	if engine == convert.EngineArchive {
		paths, ok := archiveStoredPaths(source.job.Opts, len(source.inputs))
		if !ok {
			writeError(w, 422, errcode.InvalidRequest, "invalid archive path")
			return
		}
		if message := archiveRepeatOptsRejection(request.Opts, paths); message != "" {
			writeError(w, 422, errcode.InvalidRequest, message)
			return
		}
		source.paths = paths
		forwardOpts = nil
	} else if len(source.inputs) > 1 && !convert.EngineTakesMultipleInputs(engine) {
		writeError(w, 422, errcode.InvalidRequest, "conversion cannot consume original attachments")
		return
	}
	var total int64
	for index, in := range source.inputs {
		if in.Ordinal != index || in.SizeBytes < 0 || in.SizeBytes > min(s.maxUploadByte, 100<<20)-total || len(in.Filename) > maxInputFilenameBytes || strings.ContainsAny(in.Filename, "\r\n") {
			writeError(w, 413, errcode.PayloadTooLarge, "original inputs exceed repeat limits; upload replacements")
			return
		}
		total += in.SizeBytes
		available, err := source.store.ObjectExists(ctx, in.ObjectKey)
		if err != nil {
			writeError(w, 503, errcode.InternalError, "source storage temporarily unavailable")
			return
		}
		if !available {
			writeError(w, 409, errcode.InvalidRequest, "original inputs expired; upload replacements")
			return
		}
	}
	client, _ := auth.ClientFromContext(ctx)
	if err := s.repo.PreAdmit(ctx, client.ID, engine, total); err != nil {
		if message, after, ok := quotaRejection(err); ok {
			writeQuotaError(w, message, after)
		} else {
			writeError(w, 500, errcode.InternalError, "failed to check quota")
		}
		return
	}
	dir, err := os.MkdirTemp("", "octoconv-repeat-")
	if err != nil {
		writeError(w, 500, errcode.InternalError, "failed to prepare sources")
		return
	}
	defer os.RemoveAll(dir)
	var files []*os.File
	defer func() {
		for _, file := range files {
			_ = file.Close()
		}
	}()
	for _, in := range source.inputs {
		reader, err := source.store.Download(ctx, in.ObjectKey)
		if err != nil {
			writeRepeatReadFailure(w, ctx, source.store, in.ObjectKey)
			return
		}
		file, err := os.CreateTemp(dir, "input-")
		if err != nil {
			_ = reader.Close()
			writeError(w, 500, errcode.InternalError, "failed to prepare sources")
			return
		}
		files = append(files, file)
		n, copyErr := io.Copy(file, io.LimitReader(repeatContextReader{ctx, reader}, in.SizeBytes+1))
		_ = reader.Close()
		if copyErr != nil {
			writeRepeatReadFailure(w, ctx, source.store, in.ObjectKey)
			return
		}
		if n != in.SizeBytes {
			writeError(w, 409, errcode.InvalidRequest, "original input changed; upload replacements")
			return
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			writeError(w, 500, errcode.InternalError, "failed to prepare sources")
			return
		}
	}
	body, contentType, err := repeatMultipart(source, files, request.Operation, target, forwardOpts)
	if err != nil {
		writeError(w, 500, errcode.InternalError, "failed to prepare request")
		return
	}
	created, err := http.NewRequestWithContext(ctx, http.MethodPost, "/v1/jobs", body)
	if err != nil {
		writeError(w, 500, errcode.InternalError, "failed to prepare request")
		return
	}
	created.Header.Set("Content-Type", contentType)
	// Fresh request inherits caller context but no notification/correlation headers or fields.
	s.handleCreateJob(w, created)
}

// Both lazy object-open and streaming-read failures can race lifecycle deletion.
func writeRepeatReadFailure(w http.ResponseWriter, ctx context.Context, store repeatStorage, key string) {
	if ctx.Err() == nil {
		available, err := store.ObjectExists(ctx, key)
		if err == nil && !available {
			writeError(w, 409, errcode.InvalidRequest, "original inputs expired; upload replacements")
			return
		}
	}
	writeError(w, 503, errcode.InternalError, "source storage temporarily unavailable")
}

type repeatContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r repeatContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func repeatForwardedSource(job *jobs.Job) string {
	if job == nil || job.Opts == nil {
		return ""
	}
	if _, ok := job.Opts["archive_extraction"]; !ok {
		return ""
	}
	return job.SourceFormat
}

// repeatMultipart streams staged source files through ordinary create validation.
func repeatMultipart(source repeatSource, files []*os.File, operation, target string, opts json.RawMessage) (io.Reader, string, error) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for _, field := range [][2]string{{"operation", operation}, {"source", repeatForwardedSource(source.job)}, {"target", target}, {"opts", string(opts)}} {
		if field[1] != "" {
			if err := writer.WriteField(field[0], field[1]); err != nil {
				return nil, "", err
			}
		}
	}
	var parts []io.Reader
	for index, file := range files {
		if index < len(source.paths) {
			if err := writer.WriteField(formFieldPath, source.paths[index]); err != nil {
				return nil, "", err
			}
		}
		if _, err := writer.CreateFormFile("file", source.inputs[index].Filename); err != nil {
			return nil, "", err
		}
		parts = append(parts, bytes.NewReader(bytes.Clone(buffer.Bytes())), file)
		buffer.Reset()
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	parts = append(parts, bytes.NewReader(bytes.Clone(buffer.Bytes())))
	return io.MultiReader(parts...), writer.FormDataContentType(), nil
}

func archiveStoredPaths(opts map[string]any, count int) ([]string, bool) {
	if opts == nil {
		return nil, true
	}
	raw, exists := opts["paths"]
	if !exists {
		return nil, true
	}
	switch values := raw.(type) {
	case []string:
		if len(values) != count {
			return nil, false
		}
		return values, true
	case []any:
		if len(values) != count {
			return nil, false
		}
		paths := make([]string, count)
		for i, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, false
			}
			paths[i] = text
		}
		return paths, true
	default:
		return nil, false
	}
}

func archiveRepeatOptsRejection(raw json.RawMessage, stored []string) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("{}")) {
		return ""
	}
	var opts map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &opts); err != nil || opts == nil {
		return "archive jobs do not accept preset or opts"
	}
	if len(opts) == 0 {
		return ""
	}
	pathsRaw, onlyPaths := opts["paths"]
	if !onlyPaths || len(opts) != 1 {
		return "archive jobs do not accept preset or opts"
	}
	var got []string
	if err := json.Unmarshal(pathsRaw, &got); err != nil || !slices.Equal(got, stored) {
		return "invalid archive path"
	}
	return ""
}
