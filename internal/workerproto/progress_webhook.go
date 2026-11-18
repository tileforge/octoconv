package workerproto

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/webhook"
)

type progressNotice struct {
	fire          bool
	jobID         uuid.UUID
	callbackURL   string
	correlationID string
	percent       int
	eta           *int
}

func (h *Handler) fireProgress(n progressNotice) {
	if !n.fire || h.progressDeliverer == nil || len(h.progressSecret) == 0 || h.progressTimeout <= 0 {
		return
	}
	payload := map[string]any{
		"job_id":       n.jobID,
		"event":        "progress",
		"progress_pct": n.percent,
		"eta_seconds":  n.eta,
	}
	if n.correlationID != "" {
		payload["correlation_id"] = n.correlationID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	url := n.callbackURL
	secret := append([]byte(nil), h.progressSecret...)
	timeout := h.progressTimeout
	deliv := h.progressDeliverer
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		ts := time.Now().Unix()
		sig := webhook.SignPayload(secret, ts, body)
		_, _ = deliv.Deliver(ctx, url, body, ts, sig)
	}()
}
