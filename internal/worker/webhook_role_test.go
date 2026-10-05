package worker

// Regression coverage for codex PR #7 finding #1: the terminal webhook's
// download_url must be selected by jobs.OutputRolePrimary, never by ordinal
// or slice position. Offline -- no Postgres, no S3, no live Handler; a stub
// presign closure stands in for storage.Client.PresignGet, matching this
// project's precedent for testing pure helpers factored out of
// HandleWebhookDeliver (see setWebhookDownloadURL's doc comment for why the
// handler itself cannot be unit-tested).

import (
	"context"
	"errors"
	"testing"

	"github.com/apaderin/octoconv/internal/jobs"
)

// stubPresign returns a deterministic fake URL per key and records every
// call it receives, so tests can assert both the returned download_url and
// that presign was invoked exactly once with the expected object key.
func stubPresign(calls *[]string) func(context.Context, string) (string, error) {
	return func(_ context.Context, key string) (string, error) {
		*calls = append(*calls, key)
		return "https://s3.test/" + key, nil
	}
}

func TestSetWebhookDownloadURL_ReverseOrderSelectsPrimary(t *testing.T) {
	outs := []jobs.Output{
		{Ordinal: 0, ObjectKey: "results/J/0-report.json", Role: "report"},
		{Ordinal: 1, ObjectKey: "results/J/1-out.png", Role: jobs.OutputRolePrimary},
	}
	var calls []string
	body := map[string]any{}

	if err := setWebhookDownloadURL(context.Background(), body, outs, stubPresign(&calls)); err != nil {
		t.Fatalf("setWebhookDownloadURL returned error: %v", err)
	}

	const wantURL = "https://s3.test/results/J/1-out.png"
	if got, _ := body["download_url"].(string); got != wantURL {
		t.Fatalf("download_url = %q, want %q (must select the PRIMARY output, not ordinal 0)", got, wantURL)
	}
	if len(calls) != 1 {
		t.Fatalf("presign called %d times, want exactly 1", len(calls))
	}
	if calls[0] != "results/J/1-out.png" {
		t.Fatalf("presign called with key %q, want the primary output's key", calls[0])
	}
}

func TestSetWebhookDownloadURL_SinglePrimaryUnchanged(t *testing.T) {
	outs := []jobs.Output{
		{Ordinal: 0, ObjectKey: "results/J/0-out.png", Role: jobs.OutputRolePrimary},
	}
	var calls []string
	body := map[string]any{}

	if err := setWebhookDownloadURL(context.Background(), body, outs, stubPresign(&calls)); err != nil {
		t.Fatalf("setWebhookDownloadURL returned error: %v", err)
	}

	const wantURL = "https://s3.test/results/J/0-out.png"
	if got, _ := body["download_url"].(string); got != wantURL {
		t.Fatalf("download_url = %q, want %q (pre-change behavior for the single-output case, D-13)", got, wantURL)
	}
	if len(calls) != 1 {
		t.Fatalf("presign called %d times, want exactly 1", len(calls))
	}
}

func TestSetWebhookDownloadURL_NoPrimaryOmitsKey(t *testing.T) {
	outs := []jobs.Output{
		{Ordinal: 0, ObjectKey: "results/J/0-report.json", Role: "report"},
	}
	var calls []string
	body := map[string]any{}

	if err := setWebhookDownloadURL(context.Background(), body, outs, stubPresign(&calls)); err != nil {
		t.Fatalf("setWebhookDownloadURL returned error: %v", err)
	}

	if _, ok := body["download_url"]; ok {
		t.Fatalf("download_url present in body, want it absent when no primary output exists (unreachable under D-13, never guessed at)")
	}
	if len(calls) != 0 {
		t.Fatalf("presign called %d times, want 0 (no primary output to presign)", len(calls))
	}
}

func TestSetWebhookDownloadURL_NeverSelectsMeasurements(t *testing.T) {
	t.Run("measurements_sorts_before_primary", func(t *testing.T) {
		outs := []jobs.Output{
			{Ordinal: 0, ObjectKey: "results/J/0-measurements.json", Role: jobs.OutputRoleMeasurements},
			{Ordinal: 1, ObjectKey: "results/J/1-out.png", Role: jobs.OutputRolePrimary},
		}
		var calls []string
		body := map[string]any{}

		if err := setWebhookDownloadURL(context.Background(), body, outs, stubPresign(&calls)); err != nil {
			t.Fatalf("setWebhookDownloadURL returned error: %v", err)
		}

		const wantURL = "https://s3.test/results/J/1-out.png"
		if got, _ := body["download_url"].(string); got != wantURL {
			t.Fatalf("download_url = %q, want %q (must select the PRIMARY output, not the measurements row)", got, wantURL)
		}
		if len(calls) != 1 || calls[0] != "results/J/1-out.png" {
			t.Fatalf("presign calls = %v, want exactly one call with the primary output's key", calls)
		}
	})

	t.Run("measurements_after_primary", func(t *testing.T) {
		outs := []jobs.Output{
			{Ordinal: 0, ObjectKey: "results/J/0-out.png", Role: jobs.OutputRolePrimary},
			{Ordinal: 1, ObjectKey: "results/J/1-measurements.json", Role: jobs.OutputRoleMeasurements},
		}
		var calls []string
		body := map[string]any{}

		if err := setWebhookDownloadURL(context.Background(), body, outs, stubPresign(&calls)); err != nil {
			t.Fatalf("setWebhookDownloadURL returned error: %v", err)
		}

		const wantURL = "https://s3.test/results/J/0-out.png"
		if got, _ := body["download_url"].(string); got != wantURL {
			t.Fatalf("download_url = %q, want %q", got, wantURL)
		}
		if len(calls) != 1 || calls[0] != "results/J/0-out.png" {
			t.Fatalf("presign calls = %v, want exactly one call with the primary output's key", calls)
		}
	})

	t.Run("only_measurements_omits_key", func(t *testing.T) {
		outs := []jobs.Output{
			{Ordinal: 0, ObjectKey: "results/J/0-measurements.json", Role: jobs.OutputRoleMeasurements},
		}
		var calls []string
		body := map[string]any{}

		if err := setWebhookDownloadURL(context.Background(), body, outs, stubPresign(&calls)); err != nil {
			t.Fatalf("setWebhookDownloadURL returned error: %v", err)
		}

		if _, ok := body["download_url"]; ok {
			t.Fatalf("download_url present in body, want it absent when the only output is measurements")
		}
		if len(calls) != 0 {
			t.Fatalf("presign called %d times, want 0", len(calls))
		}
	})
}

func TestSetWebhookDownloadURL_PresignErrorPropagates(t *testing.T) {
	outs := []jobs.Output{
		{Ordinal: 0, ObjectKey: "results/J/0-out.png", Role: jobs.OutputRolePrimary},
	}
	wantErr := errors.New("presign boom")
	body := map[string]any{}

	err := setWebhookDownloadURL(context.Background(), body, outs, func(context.Context, string) (string, error) {
		return "", wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("setWebhookDownloadURL error = %v, want %v propagated unchanged", err, wantErr)
	}
	if _, ok := body["download_url"]; ok {
		t.Fatalf("download_url present in body after a presign error, want it absent")
	}
}
