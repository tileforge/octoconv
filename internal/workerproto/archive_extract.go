package workerproto

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/jobs"
	"github.com/tileforge/octoconv/internal/storage"
)

const extractionByteCeiling int64 = 100 << 20

var errBadArchive = errors.New("invalid archive member")

const archiveCapabilityID = "octoconv.archive.zip"

type extractionEntry struct {
	Name      string `json:"name"`
	Key       string `json:"key,omitempty"`
	Result    string `json:"result,omitempty"`
	ResultKey string `json:"result_key,omitempty"`
	Size      int64  `json:"size,omitempty"`
}

type archiveExtraction struct {
	Phase                 string            `json:"phase"`
	PairCapabilityID      string            `json:"pair_capability_id"`
	PairCapabilityVersion int               `json:"pair_capability_version"`
	PairEngine            string            `json:"pair_engine"`
	Source                string            `json:"source"`
	Target                string            `json:"target"`
	Operation             string            `json:"operation"`
	Opts                  map[string]any    `json:"opts"`
	Entries               []extractionEntry `json:"entries"`
	Next                  int               `json:"next"`
}

type jobInputRow struct {
	Ordinal     int
	Key         string
	Filename    string
	Format      string
	Size        int64
	ContentType string
}

func extractionResultName(name, target string) string {
	target = convert.NormalizeFormat(target)
	dir, base := path.Split(name)
	stem := strings.TrimSuffix(base, path.Ext(base))
	return dir + stem + "." + target
}

func parseExtraction(raw []byte) (archiveExtraction, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("{}")) {
		return archiveExtraction{}, false, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return archiveExtraction{}, false, fmt.Errorf("decode job options: %w", err)
	}
	body, ok := doc["archive_extraction"]
	if !ok {
		return archiveExtraction{}, false, nil
	}
	var ex archiveExtraction
	if err := json.Unmarshal(body, &ex); err != nil {
		return archiveExtraction{}, false, fmt.Errorf("decode archive extraction: %w", err)
	}
	if ex.Opts == nil {
		ex.Opts = map[string]any{}
	}
	if ex.Entries == nil {
		ex.Entries = []extractionEntry{}
	}
	return ex, true, nil
}

func optionsHavePaths(raw []byte) bool {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false
	}
	_, ok := doc["paths"]
	return ok
}

func packOutputsAreZip(jobEngine string, snapshotID *uuid.UUID, optsRaw []byte) bool {
	if snapshotID != nil || jobEngine != convert.EngineArchive {
		return false
	}
	_, extracting, err := parseExtraction(optsRaw)
	if err != nil || extracting {
		return false
	}
	return optionsHavePaths(optsRaw)
}

func pairParameters(ex archiveExtraction) []byte {
	if ex.Opts == nil {
		ex.Opts = map[string]any{}
	}
	raw, err := json.Marshal(ex.Opts)
	if err != nil || len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return []byte("{}")
	}
	return raw
}

func marshalExtraction(ex archiveExtraction) ([]byte, error) {
	if ex.Opts == nil {
		ex.Opts = map[string]any{}
	}
	if ex.Entries == nil {
		ex.Entries = []extractionEntry{}
	}
	raw, err := json.Marshal(map[string]archiveExtraction{"archive_extraction": ex})
	if err != nil {
		return nil, fmt.Errorf("encode archive extraction: %w", err)
	}
	return raw, nil
}

func zipFileMemberNames(r io.ReaderAt, size int64) ([]string, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("read zip: %w", errBadArchive)
	}
	names := make([]string, 0, len(zr.File))
	var declared uint64
	for _, file := range zr.File {
		if file.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("read zip: %w", errBadArchive)
		}
		if strings.HasSuffix(file.Name, "/") || file.Mode()&fs.ModeDir != 0 {
			if err := validateExtractionName(strings.TrimSuffix(file.Name, "/")); err != nil {
				return nil, fmt.Errorf("read zip: %w", errBadArchive)
			}
			continue
		}
		if err := validateExtractionName(file.Name); err != nil {
			return nil, fmt.Errorf("read zip: %w", errBadArchive)
		}
		if file.Flags&1 != 0 || (file.Method != zip.Store && file.Method != zip.Deflate) {
			return nil, fmt.Errorf("read zip: %w", errBadArchive)
		}
		if file.CompressedSize64 == 0 && file.UncompressedSize64 > 0 {
			return nil, fmt.Errorf("read zip: %w", errBadArchive)
		}
		if file.CompressedSize64 > 0 && file.CompressedSize64 <= math.MaxUint64/100 && file.UncompressedSize64 > 100*file.CompressedSize64 {
			return nil, fmt.Errorf("read zip: %w", errBadArchive)
		}
		if declared > math.MaxUint64-file.UncompressedSize64 {
			declared = math.MaxUint64
		} else {
			declared += file.UncompressedSize64
		}
		if declared > uint64(extractionByteCeiling) {
			return nil, fmt.Errorf("read zip: %w", errBadArchive)
		}
		names = append(names, file.Name)
	}
	if len(names) == 0 || len(names) > jobs.MaxInputParts {
		return nil, fmt.Errorf("read zip: %w", errBadArchive)
	}
	return names, nil
}

func validateExtractionName(name string) error {
	if name == "" || strings.Contains(name, `\`) || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return errBadArchive
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return errBadArchive
		}
	}
	parts := strings.Split(name, "/")
	for i, part := range parts {
		if part == "" || part == "." || part == ".." || len(part) > 255 {
			return errBadArchive
		}
		if i == 0 && extractionDrivePrefix(part) {
			return errBadArchive
		}
	}
	return nil
}

func extractionDrivePrefix(part string) bool {
	if len(part) < 2 || part[1] != ':' {
		return false
	}
	c := part[0]
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func (h *Handler) archiveMemberNames(ctx context.Context, sources []storedJobInput) ([]string, error) {
	if len(sources) == 0 || sources[0].Key == "" {
		return nil, fmt.Errorf("read zip: missing input")
	}
	rc, err := h.store.Download(ctx, sources[0].Key)
	if err != nil {
		return nil, fmt.Errorf("read zip: %w", err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("read zip: %w", err)
	}
	return zipFileMemberNames(bytes.NewReader(body), int64(len(body)))
}

func (h *Handler) acceptUnpack(ctx context.Context, tx pgx.Tx, sess storedSession, jobID, attemptID uuid.UUID, key, bodyHash string, body *[]byte, ex archiveExtraction, arts []completeArtifact) (bool, error) {
	dsts, gotSizes, ok, err := h.copyUnpackArtifacts(ctx, tx, jobID, attemptID, ex, arts)
	if err != nil || !ok {
		return false, err
	}
	if _, err := lockReportingJob(ctx, tx, attemptID, sess.ID); err != nil {
		if rmErr := h.removeUnnamedCopies(ctx, tx, dsts...); rmErr != nil {
			return false, rmErr
		}
		return false, err
	}
	handled, err := takeStoredComplete(ctx, tx, sess.ID, key, bodyHash, body)
	if err != nil {
		if rmErr := h.removeUnnamedCopies(ctx, tx, dsts...); rmErr != nil {
			return false, rmErr
		}
		return false, err
	}
	if handled {
		return true, nil
	}
	var status string
	var clientID *uuid.UUID
	var admitted *int64
	var engine string
	if err := tx.QueryRow(ctx, `
		SELECT status, client_id, engine, admitted_input_bytes FROM jobs WHERE id = $1`, jobID).Scan(&status, &clientID, &engine, &admitted); err != nil {
		return false, fmt.Errorf("read job: %w", err)
	}
	if status != jobs.StatusActive {
		if err := h.removeUnnamedCopies(ctx, tx, dsts...); err != nil {
			return false, err
		}
		return false, &completeFailure{status: 409, code: "attempt_not_active"}
	}
	if err := h.attemptLiveNow(ctx, tx, attemptID, sess.ID, dsts...); err != nil {
		return false, err
	}
	seenResult := map[string]struct{}{}
	for i, dst := range dsts {
		detected, sniffErr := h.sniffCopied(ctx, dst, ex.Source, ex.Operation)
		if sniffErr != nil {
			if rmErr := h.removeUnnamedCopies(ctx, tx, dsts...); rmErr != nil {
				return false, rmErr
			}
			if message, refuse := copiedMemberRefusal(sniffErr, path.Base(ex.Entries[i].Name)); refuse {
				return false, h.jobsRepo.FailProtocolIn(ctx, tx, jobID, "engine_error", message)
			}
			return false, sniffErr
		}
		if detected != ex.Source {
			if rmErr := h.removeUnnamedCopies(ctx, tx, dsts...); rmErr != nil {
				return false, rmErr
			}
			return false, h.jobsRepo.FailProtocolIn(ctx, tx, jobID, "engine_error", "content does not match source")
		}
		if message, over := memberOverByteCeiling(ex, gotSizes[i]); over {
			if rmErr := h.removeUnnamedCopies(ctx, tx, dsts...); rmErr != nil {
				return false, rmErr
			}
			return false, h.jobsRepo.FailProtocolIn(ctx, tx, jobID, "engine_error", message)
		}
		result := extractionResultName(ex.Entries[i].Name, ex.Target)
		if _, exists := seenResult[result]; exists {
			if rmErr := h.removeUnnamedCopies(ctx, tx, dsts...); rmErr != nil {
				return false, rmErr
			}
			return false, h.jobsRepo.FailProtocolIn(ctx, tx, jobID, "engine_error", "result path collision")
		}
		seenResult[result] = struct{}{}
		ex.Entries[i].Key = dst
		ex.Entries[i].Size = gotSizes[i]
		ex.Entries[i].Result = result
	}
	ex.Phase = "convert"
	ex.Next = 0
	raw, err := marshalExtraction(ex)
	if err != nil {
		return false, err
	}
	filename := path.Base(ex.Entries[0].Name)
	pinned, err := pinExtractionAdmittedBytes(ctx, tx, jobID, admitted)
	if err != nil {
		return false, err
	}
	rows := []jobInputRow{{
		Ordinal:     0,
		Key:         dsts[0],
		Filename:    filename,
		Format:      ex.Source,
		Size:        gotSizes[0],
		ContentType: convert.MIMEType(ex.Source),
	}}
	return false, h.admitThenReplace(ctx, tx, jobID, attemptID, clientID, engine, ex.PairEngine, ex.PairCapabilityID, ex.PairCapabilityVersion, pinned, dsts, raw, rows)
}

func (h *Handler) copyUnpackArtifacts(ctx context.Context, tx pgx.Tx, jobID, attemptID uuid.UUID, ex archiveExtraction, arts []completeArtifact) ([]string, []int64, bool, error) {
	n := len(ex.Entries)
	if n == 0 || len(arts) != n {
		return nil, nil, false, &completeFailure{status: 409, code: "artifact_rejected"}
	}
	ordered := make([]completeArtifact, n)
	seen := make([]bool, n)
	var sum int64
	for _, art := range arts {
		if art.Role != "primary" || art.Format != ex.Source || art.ContentType != convert.MIMEType(art.Format) || !hex64.MatchString(art.SHA256) {
			return nil, nil, false, &completeFailure{status: 409, code: "artifact_rejected"}
		}
		ordinal, kind := integerToken(art.Ordinal)
		if kind != numOK || ordinal < 0 || int(ordinal) >= n || seen[ordinal] {
			return nil, nil, false, &completeFailure{status: 409, code: "artifact_rejected"}
		}
		size, kind := integerToken(art.ByteSize)
		if kind != numOK || size < 0 || sum > extractionByteCeiling-size {
			return nil, nil, false, &completeFailure{status: 409, code: "artifact_rejected"}
		}
		sum += size
		seen[ordinal] = true
		ordered[ordinal] = art
	}
	dsts := make([]string, n)
	gotSizes := make([]int64, n)
	for i, art := range ordered {
		size, _ := integerToken(art.ByteSize)
		src := storage.AttemptOutputKey(attemptID, i)
		dst := storage.AcceptedOutputKey(jobID, attemptID, i, art.SHA256)
		got, err := h.copyMatched(ctx, tx, src, dst, art.SHA256, size, extractionByteCeiling)
		if err != nil {
			if rmErr := h.removeUnnamedCopies(ctx, tx, dsts[:i]...); rmErr != nil {
				return nil, nil, false, rmErr
			}
			return nil, nil, false, err
		}
		dsts[i] = dst
		gotSizes[i] = got
	}
	return dsts, gotSizes, true, nil
}

func (h *Handler) sniffCopied(ctx context.Context, key, declared, operation string) (string, error) {
	rc, err := h.store.Download(ctx, key)
	if err != nil {
		return "", fmt.Errorf("read copied object: %w", err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("read copied object: %w", err)
	}
	detected, err := convert.SniffDeclared(bytes.NewReader(body), int64(len(body)), declared, operation, h.documentCeiling())
	if err != nil {
		return "", fmt.Errorf("sniff copied object: %w", err)
	}
	if err := h.rejectImagePixels(detected, body); err != nil {
		return "", err
	}
	return detected, nil
}

func (h *Handler) rejectImagePixels(format string, body []byte) error {
	if !convert.HasDimensionLimit(format) {
		return nil
	}
	width, height, _, err := convert.Dimensions(format, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("read copied object: %w", err)
	}
	if uint64(width)*uint64(height) > h.imagePixels() {
		return errDeclaredImagePixelLimit
	}
	return nil
}

var errDeclaredImagePixelLimit = errors.New("declared image dimensions exceed configured limit")

func copiedMemberRefusal(err error, filename string) (string, bool) {
	switch {
	case errors.Is(err, convert.ErrMacroCarryingDocument):
		return "macro-carrying documents are not accepted", true
	case errors.Is(err, convert.ErrDeclaredUncompressedLimit):
		return "declared uncompressed size exceeds configured limit", true
	case errors.Is(err, convert.ErrDimensionsUnknown):
		return "cannot determine declared image dimensions for " + filename, true
	case errors.Is(err, errDeclaredImagePixelLimit):
		return errDeclaredImagePixelLimit.Error(), true
	default:
		return "", false
	}
}

func memberOverByteCeiling(ex archiveExtraction, size int64) (string, bool) {
	limit, perFormat, gated := convert.InputByteCeiling(ex.PairEngine, ex.Source)
	if !gated || size <= limit {
		return "", false
	}
	return convert.ByteCeilingMessage(ex.Source, limit, perFormat), true
}

func (h *Handler) acceptConvertEntry(ctx context.Context, tx pgx.Tx, jobID, attemptID uuid.UUID, ex archiveExtraction, dsts []string, gotSizes []int64, clientID *uuid.UUID, engine string, admitted *int64) error {
	if engine == convert.EngineProbe {
		if len(dsts) != 2 || len(gotSizes) != 2 {
			return &completeFailure{status: 409, code: "artifact_rejected"}
		}
	} else if len(dsts) != 1 || len(gotSizes) != 1 {
		return &completeFailure{status: 409, code: "artifact_rejected"}
	}
	idx := ex.Next
	if idx < 0 || idx >= len(ex.Entries) {
		return &completeFailure{status: 409, code: "artifact_rejected"}
	}
	ex.Entries[idx].ResultKey = dsts[0]
	ex.Entries[idx].Size = gotSizes[0]
	if ex.Entries[idx].Result == "" {
		ex.Entries[idx].Result = extractionResultName(ex.Entries[idx].Name, ex.Target)
	}
	if ex.Next+1 < len(ex.Entries) {
		ex.Next++
		ex.Phase = "convert"
		next := ex.Entries[ex.Next]
		if next.Key == "" {
			return &completeFailure{status: 409, code: "artifact_rejected"}
		}
		raw, err := marshalExtraction(ex)
		if err != nil {
			return err
		}
		rows := []jobInputRow{{
			Ordinal:     0,
			Key:         next.Key,
			Filename:    path.Base(next.Name),
			Format:      ex.Source,
			Size:        next.Size,
			ContentType: convert.MIMEType(ex.Source),
		}}
		return h.admitThenReplace(ctx, tx, jobID, attemptID, clientID, engine, ex.PairEngine, ex.PairCapabilityID, ex.PairCapabilityVersion, admitted, dsts, raw, rows)
	}
	paths := make([]string, len(ex.Entries))
	rows := make([]jobInputRow, len(ex.Entries))
	for i, entry := range ex.Entries {
		name := entry.Result
		if name == "" {
			name = extractionResultName(entry.Name, ex.Target)
		}
		paths[i] = name
		key := entry.ResultKey
		size := entry.Size
		if i == idx {
			key = dsts[0]
			size = gotSizes[0]
		}
		if key == "" {
			return &completeFailure{status: 409, code: "artifact_rejected"}
		}
		rows[i] = jobInputRow{
			Ordinal:     i,
			Key:         key,
			Filename:    path.Base(name),
			Format:      ex.Target,
			Size:        size,
			ContentType: convert.MIMEType(ex.Target),
		}
	}
	raw, err := json.Marshal(map[string]any{"paths": paths})
	if err != nil {
		return fmt.Errorf("encode paths: %w", err)
	}
	return h.admitThenReplace(ctx, tx, jobID, attemptID, clientID, engine, convert.EngineArchive, archiveCapabilityID, 1, admitted, dsts, raw, rows)
}

func (h *Handler) admitThenReplace(ctx context.Context, tx pgx.Tx, jobID, attemptID uuid.UUID, clientID *uuid.UUID, engine, next, capabilityID string, capabilityVersion int, admitted *int64, dsts []string, options []byte, rows []jobInputRow) error {
	stopped, err := h.admitOrFail(ctx, tx, jobID, clientID, engine, next, admitted, dsts)
	if err != nil || stopped {
		return err
	}
	if err := replaceJobInputs(ctx, tx, jobID, rows); err != nil {
		return err
	}
	if engine == convert.EngineProbe && len(dsts) > 1 {
		if err := h.removeUnnamedCopies(ctx, tx, dsts[1:]...); err != nil {
			return err
		}
	}
	return requeueExtraction(ctx, tx, jobID, attemptID, next, capabilityID, capabilityVersion, options)
}

func (h *Handler) admitOrFail(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, clientID *uuid.UUID, engine, next string, admitted *int64, dsts []string) (bool, error) {
	quotaErr := h.admitChangedEngine(ctx, tx, clientID, engine, next, admitted)
	reason := jobs.QuotaReason(quotaErr)
	if quotaErr != nil && reason == "" {
		return false, quotaErr
	}
	if reason == "" {
		return false, nil
	}
	if err := h.removeUnnamedCopies(ctx, tx, dsts...); err != nil {
		return false, err
	}
	if err := h.jobsRepo.FailProtocolIn(ctx, tx, jobID, reason, "quota"); err != nil {
		return false, err
	}
	return true, nil
}

func (h *Handler) removeExtraAttemptOutputs(ctx context.Context, attemptID uuid.UUID, n int) {
	if h.store == nil {
		return
	}
	for i := 2; i < n; i++ {
		_ = h.store.Remove(ctx, storage.AttemptOutputKey(attemptID, i))
	}
}

func pinExtractionAdmittedBytes(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, admitted *int64) (*int64, error) {
	if admitted != nil {
		return admitted, nil
	}
	var n int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(sum(size_bytes), 0) FROM job_inputs WHERE job_id = $1`, jobID).Scan(&n); err != nil {
		return nil, fmt.Errorf("sum extraction inputs: %w", err)
	}
	var stored int64
	if err := tx.QueryRow(ctx, `
		UPDATE jobs
		SET admitted_input_bytes = COALESCE(admitted_input_bytes, $2)
		WHERE id = $1
		RETURNING admitted_input_bytes`, jobID, n).Scan(&stored); err != nil {
		return nil, fmt.Errorf("pin admitted bytes: %w", err)
	}
	return &stored, nil
}

func replaceJobInputs(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, rows []jobInputRow) error {
	if _, err := tx.Exec(ctx, `DELETE FROM job_inputs WHERE job_id = $1`, jobID); err != nil {
		return fmt.Errorf("delete job inputs: %w", err)
	}
	for _, row := range rows {
		if _, err := tx.Exec(ctx, `
			INSERT INTO job_inputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			jobID, row.Ordinal, row.Key, row.Filename, row.Format, row.Size, row.ContentType); err != nil {
			return fmt.Errorf("insert job input: %w", err)
		}
	}
	return nil
}

func requeueExtraction(ctx context.Context, tx pgx.Tx, jobID, attemptID uuid.UUID, engine, capabilityID string, capabilityVersion int, options []byte) error {
	tag, err := tx.Exec(ctx, `
		UPDATE worker_slot_holds
		SET released_at = now()
		WHERE id = $1 AND job_id = $2 AND state = 'active' AND released_at IS NULL`,
		attemptID, jobID)
	if err != nil {
		return fmt.Errorf("release hold: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("release attempt %s: no active hold", attemptID)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE jobs
		SET status = 'queued',
		    step_id = NULL,
		    engine = $2,
		    capability_id = $3,
		    capability_version = $4,
		    options = $5::jsonb,
		    not_before = NULL,
		    eta_seconds = NULL,
		    stage = NULL,
		    stage_index = NULL,
		    stage_count = NULL
		WHERE id = $1`,
		jobID, engine, capabilityID, capabilityVersion, string(options)); err != nil {
		return fmt.Errorf("requeue extraction: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO job_events (job_id, from_status, to_status, detail)
		VALUES ($1, $2, $3, NULL)`, jobID, jobs.StatusActive, jobs.StatusQueued); err != nil {
		return fmt.Errorf("insert job_event: %w", err)
	}
	return nil
}
