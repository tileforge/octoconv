package workerproto

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tileforge/octoconv/internal/jobs"
)

func TestProtocolPassKeepsQueuedWork(t *testing.T) {
	pool := testPool(t)
	src, err := os.ReadFile("reconcile.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	for _, banned := range []string{"internal/reconciler", "asynq", "RequeueStale", "RequeueProtocol", "0x6F63746F"} {
		if strings.Contains(text, banned) {
			t.Fatalf("reconcile.go contains %s", banned)
		}
	}
	if !strings.Contains(text, "0x70726F74") {
		t.Fatal("reconcile.go is missing the protocol advisory key")
	}
	stepID := uuid.New()
	jobID := insertPullJob(t, pool, pullJob{status: "queued", stepID: &stepID})
	runProtocolPass(t, pool)
	snap := readProtocolSnap(t, pool, jobID)
	if snap.status != "queued" || snap.owner != "protocol" || snap.step != stepID || snap.notBefore != nil || holdCount(t, pool, jobID) != 0 {
		t.Fatalf("queued work changed: %+v holds %d", snap, holdCount(t, pool, jobID))
	}
}

func TestProtocolPassRearmsExpiredLease(t *testing.T) {
	pool := testPool(t)
	t.Run("requeue", func(t *testing.T) {
		jobID, stepID, holdID := insertExpiredLease(t, pool, -1, 120)
		if _, err := pool.Exec(context.Background(), `UPDATE jobs SET progress_pct = 40, eta_seconds = 5 WHERE id = $1`, jobID); err != nil {
			t.Fatal(err)
		}
		runProtocolPass(t, pool)
		snap := readProtocolSnap(t, pool, jobID)
		if snap.status != "queued" || snap.owner != "protocol" || snap.step != stepID || snap.notBefore == nil || snap.progress != nil || snap.eta != nil || snap.code != nil {
			t.Fatalf("requeue snap %+v", snap)
		}
		if holdState(t, pool, holdID) != "failed" || failedHolds(t, pool, stepID) != 1 || holdCount(t, pool, jobID) != 1 {
			t.Fatal("expired hold was not released as failed")
		}
	})
	t.Run("five failed", func(t *testing.T) {
		jobID, stepID, holdID := insertExpiredLease(t, pool, -1, 120)
		for i := 0; i < 5; i++ {
			insertSlotHold(t, pool, slotHold{
				owner: uuid.New().String(), sessionID: uuid.New(), jobID: &jobID, stepID: &stepID, state: "failed", released: true,
			})
		}
		runProtocolPass(t, pool)
		snap := readProtocolSnap(t, pool, jobID)
		if snap.status != "failed" || snap.code == nil || *snap.code != "reconciler_exhausted" || snap.step != stepID || holdState(t, pool, holdID) != "failed" {
			t.Fatalf("budget snap %+v hold %s", snap, holdState(t, pool, holdID))
		}
	})
	t.Run("deadline", func(t *testing.T) {
		jobID, stepID, holdID := insertExpiredLease(t, pool, -1, 120)
		if _, err := pool.Exec(context.Background(), `UPDATE jobs SET protocol_deadline = now() - interval '1 second' WHERE id = $1`, jobID); err != nil {
			t.Fatal(err)
		}
		runProtocolPass(t, pool)
		snap := readProtocolSnap(t, pool, jobID)
		if snap.status != "failed" || snap.code == nil || *snap.code != "reconciler_exhausted" || snap.step != stepID || holdState(t, pool, holdID) != "failed" {
			t.Fatalf("deadline snap %+v", snap)
		}
	})
	t.Run("stopped", func(t *testing.T) {
		jobID, stepID, holdID := insertExpiredLease(t, pool, -1, 120)
		stoppedID := insertSlotHold(t, pool, slotHold{
			owner: uuid.New().String(), sessionID: uuid.New(), jobID: &jobID, stepID: &stepID, state: "stopped", released: true,
		})
		runProtocolPass(t, pool)
		snap := readProtocolSnap(t, pool, jobID)
		if snap.status != "queued" || snap.notBefore == nil || snap.step != stepID || holdState(t, pool, holdID) != "failed" || holdState(t, pool, stoppedID) != "stopped" {
			t.Fatalf("stopped snap %+v", snap)
		}
	})
	t.Run("manual retry", func(t *testing.T) {
		jobID, stepID, holdID := insertExpiredLease(t, pool, -1, 120)
		for i := 0; i < 5; i++ {
			insertSlotHold(t, pool, slotHold{
				owner: uuid.New().String(), sessionID: uuid.New(), jobID: &jobID, stepID: &stepID, state: "failed", released: true,
			})
		}
		if _, err := pool.Exec(context.Background(), `
			UPDATE jobs SET manual_retry_at = now() WHERE id = $1`, jobID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(context.Background(), `
			UPDATE worker_slot_holds
			SET released_at = now() - interval '1 second'
			WHERE step_id = $1 AND state = 'failed' AND id <> $2`, stepID, holdID); err != nil {
			t.Fatal(err)
		}
		runProtocolPass(t, pool)
		snap := readProtocolSnap(t, pool, jobID)
		if snap.status != "queued" || snap.notBefore == nil || snap.step != stepID || holdState(t, pool, holdID) != "failed" || failedHolds(t, pool, stepID) != 6 {
			t.Fatalf("manual retry snap %+v failed %d", snap, failedHolds(t, pool, stepID))
		}
	})
}

func TestProtocolPassRearmsPastAttemptDeadline(t *testing.T) {
	pool := testPool(t)
	jobID, stepID, holdID := insertExpiredLease(t, pool, -10, -1)
	runProtocolPass(t, pool)
	snap := readProtocolSnap(t, pool, jobID)
	if snap.status != "queued" || snap.owner != "protocol" || snap.step != stepID || snap.notBefore == nil || holdState(t, pool, holdID) != "failed" || failedHolds(t, pool, stepID) != 1 {
		t.Fatalf("past attempt snap %+v hold %s", snap, holdState(t, pool, holdID))
	}
}

func TestProtocolDeadlineMarksFailed(t *testing.T) {
	pool := testPool(t)
	clientID := insertClient(t, pool)
	stepID := uuid.New()
	jobID := insertPullJob(t, pool, pullJob{clientID: &clientID, status: "queued", stepID: &stepID})
	if _, err := pool.Exec(context.Background(), `UPDATE jobs SET protocol_deadline = now() - interval '1 second' WHERE id = $1`, jobID); err != nil {
		t.Fatal(err)
	}
	holdID := insertOpenHold(t, pool, jobID, stepID, 60, 120)
	runProtocolPass(t, pool)
	snap := readProtocolSnap(t, pool, jobID)
	var released *time.Time
	if err := pool.QueryRow(context.Background(), `SELECT released_at FROM worker_slot_holds WHERE id = $1`, holdID).Scan(&released); err != nil {
		t.Fatal(err)
	}
	if snap.status != "failed" || snap.owner != "protocol" || snap.code == nil || *snap.code != "reconciler_exhausted" || holdState(t, pool, holdID) != "failed" || released == nil {
		t.Fatalf("deadline snap %+v released %v", snap, released)
	}
	rows, jobCount := usageSnapshot(t, pool, clientID)
	if rows != 1 || jobCount != 1 {
		t.Fatalf("usage rows=%d jobs=%d", rows, jobCount)
	}
	runProtocolPass(t, pool)
	if _, jobCount = usageSnapshot(t, pool, clientID); jobCount != 1 {
		t.Fatal("second pass recorded usage")
	}
	if readProtocolSnap(t, pool, jobID).status != "failed" {
		t.Fatal("second pass deleted the job")
	}
}

func TestProtocolPassDeletesOnlyStaleReservations(t *testing.T) {
	pool := testPool(t)
	sessionID := uuid.New()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM worker_request_dedup WHERE session_id = $1`, sessionID)
	})
	stale := uuid.New()
	answered := uuid.New()
	attempted := uuid.New()
	fresh := uuid.New()
	insertDedup(t, pool, sessionID, stale, nil, nil, true)
	body := []byte("ok")
	insertDedup(t, pool, sessionID, answered, body, nil, true)
	attemptID := uuid.New()
	insertDedup(t, pool, sessionID, attempted, nil, &attemptID, true)
	insertDedup(t, pool, sessionID, fresh, nil, nil, false)
	runProtocolPass(t, pool)
	if dedupExists(t, pool, sessionID, stale) {
		t.Fatal("stale reservation was kept")
	}
	if !dedupExists(t, pool, sessionID, answered) || !dedupExists(t, pool, sessionID, attempted) || !dedupExists(t, pool, sessionID, fresh) {
		t.Fatal("pass deleted a reservation that already had a response, an attempt, or a fresh clock")
	}
}

func runProtocolPass(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	restoreJitter(t)
	if err := protocolPass(context.Background(), pool, jobs.NewRepo(pool), newMemStore()); err != nil {
		t.Fatalf("protocol pass: %v", err)
	}
}

func insertExpiredLease(t *testing.T, pool *pgxpool.Pool, leaseSecs, deadlineSecs int) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	stepID := uuid.New()
	jobID := insertPullJob(t, pool, pullJob{status: "active", stepID: &stepID})
	holdID := insertOpenHold(t, pool, jobID, stepID, leaseSecs, deadlineSecs)
	return jobID, stepID, holdID
}

func insertOpenHold(t *testing.T, pool *pgxpool.Pool, jobID, stepID uuid.UUID, leaseSecs, deadlineSecs int) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO worker_slot_holds (
			id, owner_key, session_id, job_id, step_id, state,
			lease_expires_at, attempt_deadline
		) VALUES (
			$1, $2, $3, $4, $5, 'active',
			now() + make_interval(secs => $6),
			now() + make_interval(secs => $7)
		)`, id, uuid.New().String(), uuid.New(), jobID, stepID, leaseSecs, deadlineSecs)
	if err != nil {
		t.Fatalf("insert hold: %v", err)
	}
	return id
}

type protocolSnap struct {
	status    string
	owner     string
	notBefore *time.Time
	step      uuid.UUID
	progress  *int16
	eta       *int
	code      *string
}

func readProtocolSnap(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) protocolSnap {
	t.Helper()
	var snap protocolSnap
	err := pool.QueryRow(context.Background(), `
		SELECT status, dispatch_owner, not_before, step_id, progress_pct, eta_seconds, error_code
		FROM jobs WHERE id = $1`, id).Scan(
		&snap.status, &snap.owner, &snap.notBefore, &snap.step, &snap.progress, &snap.eta, &snap.code)
	if err != nil {
		t.Fatalf("read job: %v", err)
	}
	return snap
}

func insertDedup(t *testing.T, pool *pgxpool.Pool, sessionID, key uuid.UUID, body []byte, attemptID *uuid.UUID, stale bool) {
	t.Helper()
	age := "now()"
	if stale {
		age = "now() - interval '26 seconds'"
	}
	_, err := pool.Exec(context.Background(), `
		INSERT INTO worker_request_dedup (session_id, dedup_key, response_body, attempt_id, created_at)
		VALUES ($1, $2, $3, $4, `+age+`)`, sessionID, key.String(), body, attemptID)
	if err != nil {
		t.Fatalf("insert dedup: %v", err)
	}
}

func dedupExists(t *testing.T, pool *pgxpool.Pool, sessionID, key uuid.UUID) bool {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM worker_request_dedup
		WHERE session_id = $1 AND dedup_key = $2`, sessionID, key.String()).Scan(&n); err != nil {
		t.Fatalf("read dedup: %v", err)
	}
	return n == 1
}
