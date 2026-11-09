package workerproto

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestApproveWritesPorts(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("DATABASE_URL must be set")
	}
	pool := testPool(t)
	workerID := uuid.New()
	hexID := strings.ReplaceAll(workerID.String(), "-", "")
	capabilityID := "octoconv.test.a" + hexID
	operation := "op." + hexID
	from := "a" + hexID[:8]
	to := "b" + hexID[:8]
	body, err := json.Marshal(map[string]any{
		"capabilities": []any{
			map[string]any{
				"id":               capabilityID,
				"version":          1,
				"client_operation": operation,
				"inputs":           []any{map[string]any{"role": "source", "min": 1, "max": 1}},
				"outputs":          []any{map[string]any{"role": "primary", "min": 1, "max": 1}},
				"parameter_schema": map[string]any{},
				"formats":          []any{map[string]any{"from": from, "to": to}},
			},
		},
	})
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := NewRepo(pool).Approve(context.Background(), workerID, body); err != nil {
		t.Fatalf("approve: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM worker_capability_ports WHERE capability_id = $1`, capabilityID)
		_, _ = pool.Exec(ctx, `DELETE FROM worker_capability_formats WHERE capability_id = $1`, capabilityID)
		_, _ = pool.Exec(ctx, `DELETE FROM worker_capabilities WHERE worker_id = $1`, workerID)
		_, _ = pool.Exec(ctx, `DELETE FROM worker_identities WHERE worker_id = $1`, workerID)
	})
	rows, err := pool.Query(context.Background(), `
		SELECT direction, role, min_count, max_count, ordinal
		FROM worker_capability_ports
		WHERE capability_id = $1 AND capability_version = 1
		ORDER BY direction, ordinal`, capabilityID)
	if err != nil {
		t.Fatalf("read ports: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var direction, role string
		var min, max, ordinal int
		if err := rows.Scan(&direction, &role, &min, &max, &ordinal); err != nil {
			t.Fatalf("scan port: %v", err)
		}
		got = append(got, direction+" "+role)
		if min != 1 || max != 1 {
			t.Fatalf("counts %d %d", min, max)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("ports: %v", err)
	}
	if len(got) != 2 || got[0] != "input source" || got[1] != "output primary" {
		t.Fatalf("ports %v", got)
	}
}
