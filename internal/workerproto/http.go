package workerproto

import "net/http"

// Handler serves the worker session routes.
type Handler struct {
	repo *Repo
}

// NewHandler returns a handler bound to the catalog.
func NewHandler(repo *Repo) *Handler {
	return &Handler{repo: repo}
}

// Routes returns the worker HTTP routes.
func (h *Handler) Routes() http.Handler {
	return http.NewServeMux()
}
