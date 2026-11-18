package workerproto

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/jobs"
	"github.com/tileforge/octoconv/internal/webhook"
)

type progressDelivery struct {
	url  string
	body []byte
	ts   int64
	sig  string
}

type progressSpy struct {
	ch chan progressDelivery
}

func (s *progressSpy) Deliver(_ context.Context, url string, body []byte, timestamp int64, signature string) (int, error) {
	copied := append([]byte(nil), body...)
	s.ch <- progressDelivery{url: url, body: copied, ts: timestamp, sig: signature}
	return 204, nil
}

func waitDeliveries(t *testing.T, ch <-chan progressDelivery, n int) []progressDelivery {
	t.Helper()
	got := make([]progressDelivery, 0, n)
	deadline := time.After(2 * time.Second)
	for len(got) < n {
		select {
		case d := <-ch:
			got = append(got, d)
		case <-deadline:
			t.Fatalf("deliveries = %d, want %d", len(got), n)
		}
	}
	return got
}

func assertNoDelivery(t *testing.T, ch <-chan progressDelivery) {
	t.Helper()
	select {
	case d := <-ch:
		t.Fatalf("unexpected delivery to %s body %s", d.url, d.body)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestFireProgressPostsOneSignedBody(t *testing.T) {
	secret := []byte("progress-secret")
	spy := &progressSpy{ch: make(chan progressDelivery, 4)}
	h := NewHandler(nil, nil, nil, time.Second)
	h.SetProgressWebhook(spy, secret, time.Second)
	jobID := uuid.New()
	eta := 7
	h.fireProgress(progressNotice{
		fire:          true,
		jobID:         jobID,
		callbackURL:   "https://client.example/hook",
		correlationID: "corr-1",
		percent:       40,
		eta:           &eta,
	})
	got := waitDeliveries(t, spy.ch, 1)
	assertNoDelivery(t, spy.ch)
	if got[0].url != "https://client.example/hook" {
		t.Fatalf("url = %s", got[0].url)
	}
	if got[0].sig != webhook.SignPayload(secret, got[0].ts, got[0].body) {
		t.Fatalf("signature %s does not match the body", got[0].sig)
	}
	if delta := time.Since(time.Unix(got[0].ts, 0)); delta < -2*time.Second || delta > 5*time.Second {
		t.Fatalf("timestamp %d is not current", got[0].ts)
	}
	var payload map[string]any
	if err := json.Unmarshal(got[0].body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["job_id"] != jobID.String() || payload["event"] != "progress" || payload["progress_pct"] != float64(40) || payload["eta_seconds"] != float64(7) || payload["correlation_id"] != "corr-1" {
		t.Fatalf("body = %s", got[0].body)
	}
}

func TestFireProgressOmitsEmptyCorrelationAndNullETA(t *testing.T) {
	spy := &progressSpy{ch: make(chan progressDelivery, 2)}
	h := NewHandler(nil, nil, nil, time.Second)
	h.SetProgressWebhook(spy, []byte("s"), time.Second)
	h.fireProgress(progressNotice{fire: true, jobID: uuid.New(), callbackURL: "https://client.example/hook", percent: 0})
	got := waitDeliveries(t, spy.ch, 1)
	if strings.Contains(string(got[0].body), "correlation_id") {
		t.Fatalf("body = %s", got[0].body)
	}
	var payload struct {
		ETA *int `json:"eta_seconds"`
	}
	if err := json.Unmarshal(got[0].body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ETA != nil {
		t.Fatalf("eta = %v, want null", *payload.ETA)
	}
}

func TestFireProgressStaysQuiet(t *testing.T) {
	notice := progressNotice{fire: true, jobID: uuid.New(), callbackURL: "https://client.example/hook", percent: 1}
	cases := []struct {
		name string
		h    *Handler
		n    progressNotice
	}{
		{"nil deliverer", func() *Handler {
			h := NewHandler(nil, nil, nil, time.Second)
			h.SetProgressWebhook(nil, []byte("s"), time.Second)
			return h
		}(), notice},
		{"empty secret", func() *Handler {
			h := NewHandler(nil, nil, nil, time.Second)
			spy := &progressSpy{ch: make(chan progressDelivery, 1)}
			h.SetProgressWebhook(spy, nil, time.Second)
			return h
		}(), notice},
		{"non-positive timeout", func() *Handler {
			h := NewHandler(nil, nil, nil, time.Second)
			spy := &progressSpy{ch: make(chan progressDelivery, 1)}
			h.SetProgressWebhook(spy, []byte("s"), 0)
			return h
		}(), notice},
		{"not earned", func() *Handler {
			h := NewHandler(nil, nil, nil, time.Second)
			spy := &progressSpy{ch: make(chan progressDelivery, 1)}
			h.SetProgressWebhook(spy, []byte("s"), time.Second)
			return h
		}(), progressNotice{callbackURL: "https://client.example/hook", percent: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.h.fireProgress(tc.n)
			if d := tc.h.progressDeliverer; d != nil {
				if spy, ok := d.(*progressSpy); ok {
					assertNoDelivery(t, spy.ch)
				}
			}
		})
	}
}

func TestStoreProgressEarnsAClientWebhook(t *testing.T) {
	live := openHello(t)
	attemptID, jobID := armProgressJob(t, live, true, "https://client.example/hook", "corr-9")
	h := NewHandler(NewRepo(live.pool), jobs.NewRepo(live.pool), newMemStore(), 120*time.Second)
	eta := 12
	limited, notice, err := h.storeProgress(context.Background(), live.sessionID, attemptID, parsedProgress{percent: 15, eta: &eta})
	if err != nil || limited || !notice.fire {
		t.Fatalf("limited=%v notice=%+v err=%v", limited, notice, err)
	}
	if notice.jobID != jobID || notice.callbackURL != "https://client.example/hook" || notice.correlationID != "corr-9" || notice.percent != 15 || notice.eta == nil || *notice.eta != 12 {
		t.Fatalf("notice = %+v", notice)
	}

	_, quiet, err := h.storeProgress(context.Background(), live.sessionID, attemptID, parsedProgress{percent: 10})
	if err != nil || quiet.fire {
		t.Fatalf("lower percent notice=%+v err=%v", quiet, err)
	}
}

func TestStoreProgressSkipsWebhookWhenTheJobDoesNotAdvance(t *testing.T) {
	live := openHello(t)
	attemptID, jobID := armProgressJob(t, live, true, "https://client.example/hook", "")
	if _, err := live.pool.Exec(context.Background(), `UPDATE jobs SET progress_pct = 80 WHERE id = $1`, jobID); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(NewRepo(live.pool), jobs.NewRepo(live.pool), newMemStore(), 120*time.Second)
	_, notice, err := h.storeProgress(context.Background(), live.sessionID, attemptID, parsedProgress{percent: 10})
	if err != nil || notice.fire {
		t.Fatalf("notice=%+v err=%v", notice, err)
	}
}

func TestStoreProgressSkipsWebhookWithoutAClientTarget(t *testing.T) {
	live := openHello(t)
	h := NewHandler(NewRepo(live.pool), jobs.NewRepo(live.pool), newMemStore(), 120*time.Second)
	attemptID, _ := armProgressJob(t, live, false, "https://client.example/hook", "")
	_, notice, err := h.storeProgress(context.Background(), live.sessionID, attemptID, parsedProgress{percent: 10})
	if err != nil || notice.fire {
		t.Fatalf("webhook off notice=%+v err=%v", notice, err)
	}
	attemptID, _ = armProgressJob(t, live, true, "", "corr")
	_, notice, err = h.storeProgress(context.Background(), live.sessionID, attemptID, parsedProgress{percent: 10})
	if err != nil || notice.fire {
		t.Fatalf("empty callback notice=%+v err=%v", notice, err)
	}
}

func TestStoreProgressRateLimitDoesNotEarnAWebhook(t *testing.T) {
	live := openHello(t)
	attemptID, _ := armProgressJob(t, live, true, "https://client.example/hook", "")
	h := NewHandler(NewRepo(live.pool), jobs.NewRepo(live.pool), newMemStore(), 120*time.Second)
	for i := 1; i <= 4; i++ {
		limited, notice, err := h.storeProgress(context.Background(), live.sessionID, attemptID, parsedProgress{percent: i})
		if err != nil || limited || !notice.fire {
			t.Fatalf("progress %d limited=%v notice=%+v err=%v", i, limited, notice, err)
		}
	}
	limited, notice, err := h.storeProgress(context.Background(), live.sessionID, attemptID, parsedProgress{percent: 5})
	if err != nil || !limited || notice.fire {
		t.Fatalf("fifth limited=%v notice=%+v err=%v", limited, notice, err)
	}
}

func armProgressJob(t *testing.T, live liveSession, hook bool, callback, correlation string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	_, attemptID, jobID := insertActiveAttempt(t, live, 60, 120)
	if _, err := live.pool.Exec(context.Background(), `
		UPDATE jobs
		SET progress_webhook = $2,
		    callback_url = NULLIF($3, ''),
		    correlation_id = NULLIF($4, '')
		WHERE id = $1`, jobID, hook, callback, correlation); err != nil {
		t.Fatalf("arm job: %v", err)
	}
	return attemptID, jobID
}
