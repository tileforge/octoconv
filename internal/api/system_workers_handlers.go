package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/errcode"
	"github.com/tileforge/octoconv/internal/workerproto"
)

func (s *Server) handleSystemWorkers(w http.ResponseWriter, r *http.Request) {
	if s.workers == nil {
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to list workers")
		return
	}
	workers, err := s.workers.ListWorkers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to list workers")
		return
	}
	if workers == nil {
		workers = []workerproto.WorkerView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"workers": workers})
}

func (s *Server) handleSystemWorkerDrain(w http.ResponseWriter, r *http.Request) {
	if s.workers == nil {
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to drain worker")
		return
	}
	workerID, err := uuid.Parse(chi.URLParam(r, "worker_id"))
	if err != nil {
		writeError(w, http.StatusNotFound, errcode.NotFound, "worker not found")
		return
	}
	view, err := s.workers.DrainWorker(r.Context(), workerID)
	if errors.Is(err, workerproto.ErrWorkerNotFound) {
		writeError(w, http.StatusNotFound, errcode.NotFound, "worker not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to drain worker")
		return
	}
	writeJSON(w, http.StatusOK, view)
}
