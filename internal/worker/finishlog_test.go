package worker

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/jobs"
)

// The log record is written AFTER the commit, and says what the commit did.
//
// Writing it first claimed a terminal state that MarkFailed might then fail to
// reach -- a Postgres error, or a concurrent transition that already took the
// job elsewhere. Everything downstream treats such a job as NOT having reached
// the status, and terminal callers discard finish's error and return
// SkipRetry, so the log would have been the only record of the outcome and it
// would have been wrong (codex, PR #23).
func TestFinishRecordReflectsWhetherTheCommitLanded(t *testing.T) {
	tests := []struct {
		name        string
		commitErr   error
		wantEvent   string
		absentEvent string
	}{
		{
			// The terminal record itself now comes from jobs.Repo.transition
			// (see internal/jobs/transitionlog_test.go). What this asserts is
			// that finish stays SILENT when the commit landed -- two records
			// for one outcome is its own defect.
			name:        "the commit landed",
			commitErr:   nil,
			wantEvent:   "",
			absentEvent: "job finish uncommitted",
		},
		{
			// A job that could not be marked failed is exactly what an
			// operator needs to see, so this is not silent either -- it is a
			// DIFFERENT event, because the row and the log genuinely disagree.
			name:        "the commit did not land",
			commitErr:   errors.New("mark failed: connection reset"),
			wantEvent:   "job finish uncommitted",
			absentEvent: "job failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			prev := log.Writer()
			log.SetOutput(&buf)
			t.Cleanup(func() { log.SetOutput(prev) })

			jobID, clientID := uuid.New(), uuid.New()
			job := &jobs.Job{ID: jobID, ClientID: clientID, Engine: "image", TargetFormat: "webp"}
			calls := []string{}
			repo := &pinRepo{job: job, calls: &calls, markFailedErr: tc.commitErr}
			h := newPinHandler(t, repo, &pinEnqueuer{calls: &calls})
			r := NewRunner(h, DocumentDescriptor)

			_ = r.finish(context.Background(), job, jobs.StatusFailed, CodeEngineError, "boom", nil, 0)

			out := buf.String()
			if tc.wantEvent != "" && !strings.Contains(out, tc.wantEvent) {
				t.Fatalf("no %q record.\nlog:\n%s", tc.wantEvent, out)
			}
			if strings.Contains(out, tc.absentEvent) {
				t.Fatalf("the log says %q, which is not what happened.\nlog:\n%s", tc.absentEvent, out)
			}
			if tc.wantEvent != "" && !strings.Contains(out, jobID.String()) {
				t.Fatalf("the record does not carry the job id, so it joins nothing:\n%s", out)
			}
		})
	}
}
