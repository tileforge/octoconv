package api

import (
	"errors"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/auth"
	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/errcode"
	"github.com/tileforge/octoconv/internal/jobs"
	"github.com/tileforge/octoconv/internal/workflow"
)

func (s *Server) admitWorkflow(w http.ResponseWriter, r *http.Request, operation, correlationID string) {
	if operation != operationConv || r.FormValue(formFieldPreset) != "" || r.FormValue(formFieldTarget) != "" {
		writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest, "workflow cannot be combined with preset or target")
		return
	}
	name, version, ok := parseWorkflowRef(r.FormValue(formFieldWorkflow))
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest, "invalid workflow")
		return
	}
	deadline, err := time.Parse(time.RFC3339, r.FormValue(formFieldDeadline))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest, "invalid deadline")
		return
	}
	if s.workflows == nil {
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to create job")
		return
	}
	file, header, err := r.FormFile(formFieldFile)
	if err != nil {
		writeError(w, http.StatusBadRequest, errcode.MissingFile, "missing file")
		return
	}
	defer file.Close()
	if r.MultipartForm != nil && len(r.MultipartForm.File[formFieldFile]) > 1 {
		writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest, "this conversion accepts a single file")
		return
	}
	source := convert.NormalizeFormat(strings.TrimPrefix(path.Ext(path.Base(header.Filename)), "."))
	if source == "" {
		writeError(w, http.StatusBadRequest, errcode.InvalidRequest, "cannot determine source format from filename")
		return
	}
	callbackURL := r.FormValue(formFieldCallbackURL)
	if callbackURL != "" {
		if err := validateCallbackURL(callbackURL); err != nil {
			writeError(w, http.StatusBadRequest, errcode.InvalidRequest, "invalid callback_url")
			return
		}
	}
	client, _ := auth.ClientFromContext(r.Context())
	jobID, err := s.workflows.Admit(r.Context(), workflow.AdmitInput{
		ClientID:        client.ID,
		Name:            name,
		Version:         version,
		SourceFormat:    source,
		Deadline:        deadline,
		Budgets:         s.engineTimeouts,
		InputBytes:      header.Size,
		CallbackURL:     callbackURL,
		ProgressWebhook: callbackURL != "" && r.FormValue(formFieldProgressWebhook) == "true",
		CorrelationID:   correlationID,
	})
	if errors.Is(err, workflow.ErrRejected) || errors.Is(err, workflow.ErrConflict) {
		writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest, "invalid workflow")
		return
	}
	if msg, retryAfter, ok := quotaRejection(err); ok {
		writeQuotaError(w, msg, retryAfter)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to create job")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"job_id": jobID,
		"status": jobs.StatusQueued,
	})
}

func parseWorkflowRef(raw string) (string, int, bool) {
	name, ver, ok := strings.Cut(raw, "@")
	if !ok || name == "" || strings.Contains(ver, "@") {
		return "", 0, false
	}
	n, err := strconv.Atoi(ver)
	if err != nil || n < 1 || strconv.Itoa(n) != ver {
		return "", 0, false
	}
	return name, n, true
}

func (s *Server) handleJobWorkflow(w http.ResponseWriter, r *http.Request) {
	if s.workflows == nil {
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to load job")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errcode.InvalidRequest, "invalid job id")
		return
	}
	client, _ := auth.ClientFromContext(r.Context())
	view, err := s.workflows.Status(r.Context(), client.ID, id)
	if errors.Is(err, workflow.ErrNotFound) {
		writeError(w, http.StatusNotFound, errcode.NotFound, "job not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to load job")
		return
	}
	steps := make([]map[string]any, 0, len(view.Steps))
	for _, step := range view.Steps {
		steps = append(steps, map[string]any{
			"step_id":            step.StepID,
			"capability_id":      step.CapabilityID,
			"capability_version": step.CapabilityVersion,
			"source_format":      step.SourceFormat,
			"target_format":      step.TargetFormat,
			"progress_pct":       nil,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       view.Status,
		"current_step": nil,
		"steps":        steps,
	})
}
