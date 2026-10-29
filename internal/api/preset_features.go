package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/errcode"
	"github.com/tileforge/octoconv/internal/presets"
)

const presetFeaturesHeader = "X-Octoconv-Presets"
const presetFeatures = "operations,version-preconditions"

var errPresetFeaturesUnsupported = errors.New("versioned presets unavailable")

type versionedPresetAdmin interface {
	UpdateWithVersion(context.Context, presets.MutationParams) (int, error)
	DeactivateWithVersion(context.Context, string, *uuid.UUID, string, *int) error
}

// Separate write routes fail closed on old servers, including mixed-version deployments.
func (s *Server) requirePresetManagement(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.presetAdmin.(versionedPresetAdmin); !ok {
			writeError(w, 501, errcode.InvalidRequest, "versioned presets unavailable")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) advertisePresetFeatures(w http.ResponseWriter) {
	if _, ok := s.presetAdmin.(versionedPresetAdmin); ok {
		w.Header().Set(presetFeaturesHeader, presetFeatures)
	}
}
func (s *Server) validatePresetFeatures(w http.ResponseWriter, r *http.Request, p presetRequest) bool {
	if p.Operation != nil && !convert.Default.IsKnownOperation(*p.Operation) {
		writeError(w, 422, errcode.UnsupportedOperation, "unsupported preset operation")
		return false
	}
	if strings.HasPrefix(r.URL.Path, "/v1/preset-settings") && r.Method == "PUT" && p.ExpectedVersion == nil {
		writeError(w, 400, errcode.InvalidRequest, "expected version required")
		return false
	}
	if p.ExpectedVersion != nil && (*p.ExpectedVersion < 1 || r.Method != "PUT") {
		writeError(w, 400, errcode.InvalidRequest, "invalid expected version")
		return false
	}
	if p.Operation != nil || p.ExpectedVersion != nil {
		if _, ok := s.presetAdmin.(versionedPresetAdmin); !ok {
			writeError(w, 501, errcode.InvalidRequest, "versioned presets unavailable")
			return false
		}
	}
	return true
}
func presetOperation(p presetRequest) string {
	if p.Operation != nil {
		return *p.Operation
	}
	return ""
}
func presetPathName(r *http.Request) string {
	name := chi.URLParam(r, "name")
	if r.URL.RawPath != "" {
		if value, err := url.PathUnescape(name); err == nil {
			return value
		}
	}
	return name
}
func (s *Server) updatePreset(ctx context.Context, scope string, clientID *uuid.UUID, name string, p presetRequest) (int, error) {
	if p.Operation != nil || p.ExpectedVersion != nil {
		admin, ok := s.presetAdmin.(versionedPresetAdmin)
		if !ok {
			return 0, errPresetFeaturesUnsupported
		}
		return admin.UpdateWithVersion(ctx, presets.MutationParams{Scope: scope, ClientID: clientID, Name: name, Operation: p.Operation, ExpectedVersion: p.ExpectedVersion, TargetFormat: p.TargetFormat, Options: p.Options, Description: p.Description})
	}
	return s.presetAdmin.Update(ctx, scope, clientID, name, p.TargetFormat, p.Options, p.Description)
}
func expectedPresetVersion(w http.ResponseWriter, r *http.Request) (*int, bool) {
	raw := strings.TrimSpace(r.Header.Get("If-Match"))
	if raw == "" {
		if strings.HasPrefix(r.URL.Path, "/v1/preset-settings") {
			writeError(w, 400, errcode.InvalidRequest, "expected version required")
			return nil, false
		}
		return nil, true
	}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		raw = raw[1 : len(raw)-1]
	}
	version, err := strconv.Atoi(raw)
	if err != nil || version < 1 {
		writeError(w, 400, errcode.InvalidRequest, "invalid expected version")
		return nil, false
	}
	return &version, true
}
func (s *Server) deactivatePreset(ctx context.Context, scope string, clientID *uuid.UUID, name string, expected *int) error {
	if expected != nil {
		admin, ok := s.presetAdmin.(versionedPresetAdmin)
		if !ok {
			return errPresetFeaturesUnsupported
		}
		return admin.DeactivateWithVersion(ctx, scope, clientID, name, expected)
	}
	return s.presetAdmin.Deactivate(ctx, scope, clientID, name)
}
func presetMutationFailure(w http.ResponseWriter, err error) bool {
	if errors.Is(err, presets.ErrVersionConflict) {
		writeError(w, 409, errcode.InvalidRequest, "preset version changed; reload or save under another name")
		return true
	}
	if errors.Is(err, errPresetFeaturesUnsupported) {
		writeError(w, 501, errcode.InvalidRequest, "versioned presets unavailable")
		return true
	}
	return false
}
