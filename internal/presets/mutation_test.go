package presets

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestVersionedMutationPreservesOperationAndRejectsStaleWrites(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	client := createTestClient(t, r)
	name := uniqueName(t, "render")
	_, version, err := r.Create(ctx, CreateParams{Name: name, Scope: ScopeUser, ClientID: &client, Operation: "render", TargetFormat: "png", Options: map[string]any{"size": 512}})
	if err != nil {
		t.Fatal(err)
	}
	next, err := r.UpdateWithVersion(ctx, MutationParams{Scope: ScopeUser, ClientID: &client, Name: name, TargetFormat: "png", Options: map[string]any{"size": 1024}, ExpectedVersion: &version})
	if err != nil || next != version+1 {
		t.Fatalf("version %d err %v", next, err)
	}
	if _, err := r.UpdateWithVersion(ctx, MutationParams{Scope: ScopeUser, ClientID: &client, Name: name, TargetFormat: "png", ExpectedVersion: &version}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale update %v", err)
	}
	if err := r.DeactivateWithVersion(ctx, ScopeUser, &client, name, &version); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale delete %v", err)
	}
	if _, err := r.Update(ctx, ScopeUser, &client, name, "png", nil, ""); err != nil {
		t.Fatal(err)
	}
	saved, err := r.Get(ctx, ScopeUser, &client, name)
	if err != nil || saved.Operation != "render" {
		t.Fatalf("saved %+v err %v", saved, err)
	}
	other := uuid.New()
	if err := r.DeactivateWithVersion(ctx, ScopeUser, &other, name, &next); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign %v", err)
	}
}
func TestConcurrentPresetVersionedWriters(t *testing.T) {
	for _, deleteSecond := range []bool{false, true} {
		t.Run(map[bool]string{false: "update update", true: "update delete"}[deleteSecond], func(t *testing.T) {
			r := newTestRepo(t)
			ctx := context.Background()
			client := createTestClient(t, r)
			name := uniqueName(t, "concurrent")
			_, version, err := r.Create(ctx, CreateParams{Name: name, Scope: ScopeUser, ClientID: &client, TargetFormat: "pdf"})
			if err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			out := make(chan error, 2)
			var ready sync.WaitGroup
			ready.Add(2)
			for index := 0; index < 2; index++ {
				go func(index int) {
					ready.Done()
					<-start
					if deleteSecond && index == 1 {
						out <- r.DeactivateWithVersion(ctx, ScopeUser, &client, name, &version)
					} else {
						_, err := r.UpdateWithVersion(ctx, MutationParams{Scope: ScopeUser, ClientID: &client, Name: name, TargetFormat: "pdf", ExpectedVersion: &version})
						out <- err
					}
				}(index)
			}
			ready.Wait()
			close(start)
			success, conflicts := 0, 0
			for index := 0; index < 2; index++ {
				err := <-out
				if err == nil {
					success++
				} else if errors.Is(err, ErrVersionConflict) {
					conflicts++
				} else {
					t.Fatalf("unexpected concurrent error %v", err)
				}
			}
			if success != 1 || conflicts != 1 {
				t.Fatalf("success %d conflicts %d", success, conflicts)
			}
		})
	}
}

func TestVersionedWriterWaitingOnLegacyActiveRowConflicts(t *testing.T) {
	r := newTestRepo(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := createTestClient(t, r)
	name := uniqueName(t, "legacy-waiter")
	_, version, err := r.Create(ctx, CreateParams{Name: name, Scope: ScopeUser, ClientID: &client, TargetFormat: "pdf"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM presets WHERE name=$1 AND client_id=$2 AND is_active FOR UPDATE`, name, client).Scan(&id); err != nil {
		t.Fatal(err)
	}
	out := make(chan error, 1)
	go func() {
		_, err := r.UpdateWithVersion(ctx, MutationParams{Scope: ScopeUser, ClientID: &client, Name: name, TargetFormat: "pdf", ExpectedVersion: &version})
		out <- err
	}()
	for {
		var waiting bool
		err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT version,operation FROM presets%')`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("writer did not block on old row")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE presets SET is_active=false WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO presets(name,version,scope,client_id,operation,target_format) VALUES($1,2,'user',$2,'convert','pdf')`, name, client); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-out; !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("legacy waiter returned %v", err)
	}
}
