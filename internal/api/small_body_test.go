package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tileforge/octoconv/internal/presets"
)

func TestKeyedSmallBodyMassIsReleasedAtTheShortWindow(t *testing.T) {
	const n = 80
	window := smallBodyWindow(maxPresetBodyBytes)
	answerLimit := window + time.Second
	closeLimit := refusalDrainGrace + time.Second
	settle := max(2*time.Second, smallBodyWindow(smallBodyMaxBytes)+time.Second)
	marks := &readMarks{}
	ts := startShippedServer(t, marks.wrap(heapProbeServer().Routes()))
	head := headOnTheLine(strings.TrimSuffix(requestHead(http.MethodPost, "/v1/presets", keyHeader(testClientKey), "application/json", 4000), "\r\n"))
	burst := preAuthRefusals(t, ts.Listener.Addr().String(), head, n, 0, 0, 1, 0, settle)
	readers := marks.readers.Load()
	statuses := map[int]int{}
	var slowest, latest time.Duration
	for i, o := range burst.outcomes {
		if err := errors.Join(o.writeErr, o.err); err != nil {
			t.Errorf("request %d: %v", i, err)
			continue
		}
		statuses[o.status]++
		answer, closed := o.answered.Sub(o.headSent), o.closed.Sub(o.answered)
		slowest, latest = max(slowest, answer), max(latest, closed)
		if o.status != http.StatusBadRequest && o.status != http.StatusTooManyRequests {
			t.Errorf("request %d: status %d, want 400 or 429", i, o.status)
		}
		if answer > answerLimit {
			t.Errorf("request %d: answered %d %v after its head, want no later than smallBodyWindow(maxPresetBodyBytes) + 1 s = %v: the stalled JSON body held its request", i, o.status, answer, answerLimit)
		}
		if closed > closeLimit {
			t.Errorf("request %d: the server closed the connection %v after the answer, want no later than refusalDrainGrace + 1 s = %v", i, closed, closeLimit)
		}
	}
	if statuses[http.StatusBadRequest] == 0 || int64(statuses[http.StatusBadRequest]) != readers {
		t.Errorf("%d answers are 400 and %d requests read the body, want at least one 400 and every 400 a request that read the body", statuses[http.StatusBadRequest], readers)
	}
	bound := preAuthRequestHeapBytes()
	t.Logf("SMALLBODY | POST /v1/presets | n %d | statuses %v | readers %d | slowest answer %v | latest close %v | tail %d B (%.2f MiB) | bound %d B | window %v", n, statuses, readers, slowest.Round(time.Millisecond), latest.Round(time.Millisecond), burst.tail, mib(burst.tail), bound, window)
	if burst.tail > bound {
		t.Errorf("%d keyed POST /v1/presets with a stalled JSON body still hold %d bytes (%.1f MiB) of heap and stack %v after the last head, above preAuthRequestHeapBytes = %d (%.3f MiB)", n, burst.tail, mib(burst.tail), settle, bound, mib(bound))
	}
}

func probePresetAdmin() *fakePresetAdmin {
	admin := defaultFakePresetAdmin()
	admin.getForClientErr = nil
	admin.getForClientResult = &presets.Preset{Name: "probe", Version: 1, Scope: presets.ScopeUser, TargetFormat: "png", IsActive: true}
	return admin
}

func honestPresetBody() string {
	prefix, suffix := `{"name":"probe","target_format":"png","description":"`, `"}`
	return prefix + strings.Repeat("d", maxPresetBodyBytes-128-len(prefix)-len(suffix)) + suffix
}

func splitBody(body string, n int) []string {
	step := len(body) / n
	out := make([]string, 0, n)
	for i := range n {
		hi := (i + 1) * step
		if i == n-1 {
			hi = len(body)
		}
		out = append(out, body[i*step:hi])
	}
	return out
}

func TestHonestSmallBodyPassesWithinTheShortWindow(t *testing.T) {
	window := smallBodyWindow(maxPresetBodyBytes)
	body := honestPresetBody()
	if len(body) < maxPresetBodyBytes-256 || len(body) > maxPresetBodyBytes {
		t.Fatalf("honest body is %d bytes, want between maxPresetBodyBytes - 256 and maxPresetBodyBytes = %d", len(body), maxPresetBodyBytes)
	}
	head := requestHead(http.MethodPost, "/v1/presets", keyHeader(testClientKey), "application/json", int64(len(body)))

	t.Run("kept connection", func(t *testing.T) {
		var news atomic.Int64
		srv := heapProbeServer()
		srv.presetAdmin = probePresetAdmin()
		ts := startShippedServer(t, srv.Routes(), countNewConns(&news))
		k := dialKept(t, ts)
		short := `{"name":"probe","target_format":"png"}` + "\n"
		for _, step := range []struct {
			name   string
			head   string
			pieces []string
			gap    time.Duration
		}{
			{"whole", head, []string{body}, 0},
			{"eight pieces, the last at half the window", head, splitBody(body, 8), window / 16},
			{"JSON with a trailing newline in one write", requestHead(http.MethodPost, "/v1/presets", keyHeader(testClientKey), "application/json", int64(len(short))), []string{short}, 0},
			{"whole again", head, []string{body}, 0},
		} {
			r := k.earlyExchange(step.head, step.pieces, step.gap, 10*time.Second)
			t.Logf("HONEST | %s | status %d | close %v | after %v | err %v", step.name, r.status, r.close, r.elapsed.Round(time.Millisecond), r.err)
			if r.err != nil || r.status != http.StatusCreated || r.close {
				t.Fatalf("%s: status %d, close %v, err %v, want 201 on the kept connection; body %.200s", step.name, r.status, r.close, r.err, r.body)
			}
		}
		if got := news.Load(); got != 1 {
			t.Errorf("the honest client opened %d connections, want exactly 1", got)
		}
	})

	t.Run("handler outlives the window", func(t *testing.T) {
		srv := heapProbeServer()
		admin := &slowPresetAdmin{fakePresetAdmin: probePresetAdmin(), delay: 2 * window}
		srv.presetAdmin = admin
		ts := startShippedServer(t, srv.Routes())
		r := dialKept(t, ts).earlyExchange(head, []string{body}, 0, 2*window+10*time.Second)
		admin.mu.Lock()
		called, ctxErr := admin.called, admin.ctxErr
		admin.mu.Unlock()
		t.Logf("HONEST | Create sleeps %v | status %d | close %v | after %v | called %v | ctx.Err() %v | err %v", admin.delay, r.status, r.close, r.elapsed.Round(time.Millisecond), called, ctxErr, r.err)
		if r.err != nil || r.status != http.StatusCreated {
			t.Fatalf("status %d, err %v, want 201; body %.200s", r.status, r.err, r.body)
		}
		if !called || ctxErr != nil {
			t.Fatalf("Create called=%v with ctx.Err()=%v, want a live context after the whole body", called, ctxErr)
		}
	})

	t.Run("body past the window", func(t *testing.T) {
		srv := heapProbeServer()
		srv.presetAdmin = probePresetAdmin()
		ts := startShippedServer(t, srv.Routes())
		r := dialKept(t, ts).earlyExchange(head, splitBody(body, 8), window/4, 10*time.Second)
		t.Logf("HONEST | eight pieces, the last at two windows | status %d | close %v | after %v | err %v | %.200s", r.status, r.close, r.elapsed.Round(time.Millisecond), r.err, r.body)
		if r.err != nil || r.status != http.StatusBadRequest {
			t.Fatalf("status %d, err %v, want 400 for a body still incomplete at smallBodyWindow(maxPresetBodyBytes) = %v", r.status, r.err, window)
		}
		var answer struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(r.body, &answer); err != nil || answer.Code != "invalid_request" || answer.Error != "invalid JSON body" {
			t.Errorf("answer %q (decode error %v), want code invalid_request and error \"invalid JSON body\"", r.body, err)
		}
		if !r.close {
			t.Errorf("the 400 for a body past the window keeps the connection, want Connection: close")
		}
	})
}

func TestSmallBodyWindowFollowsTheHeadRate(t *testing.T) {
	var prev time.Duration
	for _, limit := range []int64{1, 512, 1024, 4096, maxPresetBodyBytes, smallBodyMaxBytes} {
		w := smallBodyWindow(limit)
		t.Logf("WINDOW | limit %d | window %v", limit, w)
		if w < refusalDrainGrace {
			t.Errorf("limit %d: window %v is shorter than refusalDrainGrace %v", limit, w, refusalDrainGrace)
		}
		if int64(w)*apiMaxHeaderBytes < limit*int64(apiReadHeaderTimeout) {
			t.Errorf("limit %d: window %v asks for more than the head rate apiMaxHeaderBytes %d per apiReadHeaderTimeout %v", limit, w, apiMaxHeaderBytes, apiReadHeaderTimeout)
		}
		if ceil := max(refusalDrainGrace, time.Duration(limit)*apiReadHeaderTimeout/apiMaxHeaderBytes) + time.Millisecond; w > ceil {
			t.Errorf("limit %d: window %v is longer than the head rate allows, want at most %v", limit, w, ceil)
		}
		if w < prev {
			t.Errorf("limit %d: window %v is shorter than %v of a smaller limit", limit, w, prev)
		}
		prev = w
	}
	if w := smallBodyWindow(smallBodyMaxBytes); w >= intakeReadDeadline {
		t.Errorf("smallBodyWindow(smallBodyMaxBytes) = %v, want shorter than intakeReadDeadline %v", w, intakeReadDeadline)
	}
	if maxPresetBodyBytes > smallBodyMaxBytes {
		t.Errorf("maxPresetBodyBytes %d is above smallBodyMaxBytes %d", maxPresetBodyBytes, smallBodyMaxBytes)
	}
}
