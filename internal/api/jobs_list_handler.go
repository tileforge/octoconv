package api

import (
	"net/http"
	"strconv"

	"github.com/tileforge/octoconv/internal/auth"
	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/errcode"
	"github.com/tileforge/octoconv/internal/jobs"
)

// listJobsValidStatuses is the fail-closed allow-set an incoming ?status
// filter is validated against (D-05) — an unknown value never reaches
// Repo.List, it is rejected 422 before any DB work.
var listJobsValidStatuses = map[string]struct{}{
	jobs.StatusAwaitingUpload: {},
	jobs.StatusQueued:         {},
	jobs.StatusActive:         {},
	jobs.StatusDone:           {},
	jobs.StatusFailed:         {},
	jobs.StatusCanceled:       {},
}

// handleListJobs returns the caller's own jobs, keyset-paginated (JLIST-01/
// 02). Order matters: every validation step below runs BEFORE any DB work
// (fail-closed, mirrors the upload path's 422-before-work idiom) — the
// caller's client.ID from auth.ClientFromContext is the ONLY client scope
// ever passed to Repo.List (T-40-04); a client-supplied id is never honored.
func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	client, _ := auth.ClientFromContext(ctx)

	q := r.URL.Query()

	status := q.Get("status")
	if status != "" {
		if _, ok := listJobsValidStatuses[status]; !ok {
			writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest, "invalid status filter")
			return
		}
	}

	engine := q.Get("engine")
	if engine != "" {
		if _, ok := convert.Default.Classes()[engine]; !ok {
			writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest, "invalid engine filter")
			return
		}
	}

	limit := s.jobsListDefaultPageSize
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > s.jobsListMaxPageSize {
		limit = s.jobsListMaxPageSize // D-06: above-MAX clamps, never a 422
	}

	filter, err := jobs.NormalizeListFilter(jobs.ListFilter{Status: status, Engine: engine, Query: q.Get("q"), Sort: q.Get("sort")})
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, errcode.InvalidRequest, err.Error())
		return
	}
	var cursor *jobs.Cursor
	if v := q.Get("cursor"); v != "" {
		c, err := jobs.DecodeCursor(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, errcode.InvalidCursor, "invalid cursor")
			return
		}
		if !c.Matches(filter) {
			writeError(w, http.StatusBadRequest, errcode.InvalidCursor, "cursor does not match query or sort")
			return
		}
		cursor = &c
	}

	summaries, next, err := s.repo.List(ctx, client.ID, filter, cursor, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errcode.InternalError, "failed to list jobs")
		return
	}

	// Non-nil so an empty result marshals to [] rather than null (D-02).
	items := make([]map[string]any, 0, len(summaries))
	for _, j := range summaries {
		item := map[string]any{
			"job_id":        j.ID,
			"status":        j.Status,
			"engine":        j.Engine,
			"source_format": j.SourceFormat,
			"target_format": j.TargetFormat,
			"progress_pct":  j.ProgressPct,
			"created_at":    j.CreatedAt,
			"started_at":    j.StartedAt,
			"finished_at":   j.FinishedAt,
		}
		if j.SourceFilename != "" {
			item["source_filename"] = j.SourceFilename
		}
		if j.Status == jobs.StatusFailed && j.ErrorCode != "" {
			item["error_code"] = j.ErrorCode
		}
		items = append(items, item)
	}

	resp := map[string]any{"jobs": items, "next_cursor": nil}
	if q.Has("q") || q.Has("sort") {
		resp["query"] = map[string]string{"q": filter.Query, "sort": filter.Sort}
	}
	if next != nil {
		resp["next_cursor"] = jobs.EncodeCursor(*next)
	}

	writeJSON(w, http.StatusOK, resp)
}
