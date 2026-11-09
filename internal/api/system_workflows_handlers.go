package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/errcode"
	"github.com/tileforge/octoconv/internal/workflow"
)

func (s *Server) handleSystemWorkflows(w http.ResponseWriter, r *http.Request) {
	if s.workflows == nil {
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to list workflows")
		return
	}
	defs, err := s.workflows.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to list workflows")
		return
	}
	if defs == nil {
		defs = []workflow.Definition{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflows": defs})
}

func (s *Server) handleSystemWorkflowPublish(w http.ResponseWriter, r *http.Request) {
	if s.workflows == nil {
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to publish workflow")
		return
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var body struct {
		Name      string      `json:"name"`
		Version   int         `json:"version"`
		ClientIDs []uuid.UUID `json:"client_ids"`
		Steps     []struct {
			CapabilityID      string          `json:"capability_id"`
			CapabilityVersion int             `json:"capability_version"`
			SourceFormat      string          `json:"source_format"`
			TargetFormat      string          `json:"target_format"`
			InputRole         string          `json:"input_role"`
			OutputRole        string          `json:"output_role"`
			Parameters        json.RawMessage `json:"parameters"`
		} `json:"steps"`
	}
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, errcode.InvalidRequest, "invalid JSON body")
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, errcode.InvalidRequest, "invalid JSON body")
		return
	}
	steps := make([]workflow.Step, 0, len(body.Steps))
	for _, step := range body.Steps {
		params, err := workflowParameters(step.Parameters)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest, "invalid workflow")
			return
		}
		steps = append(steps, workflow.Step{
			CapabilityID:      step.CapabilityID,
			CapabilityVersion: step.CapabilityVersion,
			SourceFormat:      step.SourceFormat,
			TargetFormat:      step.TargetFormat,
			InputRole:         step.InputRole,
			OutputRole:        step.OutputRole,
			Parameters:        params,
		})
	}
	stored, err := s.workflows.Publish(r.Context(), workflow.PublishInput{
		Name:      body.Name,
		Version:   body.Version,
		ClientIDs: body.ClientIDs,
		Steps:     steps,
		Budgets:   s.engineTimeouts,
	})
	if errors.Is(err, workflow.ErrRejected) {
		writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest, "invalid workflow")
		return
	}
	if errors.Is(err, workflow.ErrConflict) {
		writeError(w, http.StatusConflict, errcode.AlreadyExists, "workflow already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to publish workflow")
		return
	}
	writeJSON(w, http.StatusCreated, stored)
}

func workflowParameters(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var params map[string]any
	if err := json.Unmarshal(raw, &params); err != nil || params == nil {
		return nil, errors.New("parameters")
	}
	return params, nil
}
