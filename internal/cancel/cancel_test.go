package cancel

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// TestRegistryCancelInvokesRegisteredFunc asserts that Register(id, fn)
// followed by Cancel(id) invokes fn exactly once.
func TestRegistryCancelInvokesRegisteredFunc(t *testing.T) {
	r := NewRegistry()
	id := uuid.New()
	calls := 0
	r.Register(id, func() { calls++ })

	r.Cancel(id)

	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

// TestRegistryCancelUnregisteredIsNoop asserts that Cancel on an id that was
// never registered does not panic and has no effect.
func TestRegistryCancelUnregisteredIsNoop(t *testing.T) {
	r := NewRegistry()
	id := uuid.New()

	r.Cancel(id) // must not panic
}

// TestRegistryDeregisterThenCancelIsNoop asserts that once an id is
// deregistered, Cancel no longer invokes its (now-removed) CancelFunc.
func TestRegistryDeregisterThenCancelIsNoop(t *testing.T) {
	r := NewRegistry()
	id := uuid.New()
	calls := 0
	r.Register(id, func() { calls++ })

	r.Deregister(id)
	r.Cancel(id)

	if calls != 0 {
		t.Errorf("calls = %d, want 0 (fn should not fire after Deregister)", calls)
	}
}

// TestRegistryConcurrentAccessIsRaceFree spins many goroutines doing
// Register/Cancel/Deregister against both distinct and shared ids
// concurrently. Run with -race to confirm the mutex actually guards m.
func TestRegistryConcurrentAccessIsRaceFree(t *testing.T) {
	r := NewRegistry()
	sharedID := uuid.New()

	var wg sync.WaitGroup
	const n = 100
	for i := 0; i < n; i++ {
		wg.Add(3)
		go func(i int) {
			defer wg.Done()
			id := uuid.New()
			r.Register(id, func() {})
			r.Cancel(id)
			r.Deregister(id)
		}(i)
		go func() {
			defer wg.Done()
			r.Register(sharedID, func() {})
		}()
		go func() {
			defer wg.Done()
			r.Cancel(sharedID)
		}()
	}
	wg.Wait()
}

// TestDefaultIsUsableRegistry asserts the package singleton behaves like any
// other Registry instance (Default must not be a nil map / zero-value
// footgun).
func TestDefaultIsUsableRegistry(t *testing.T) {
	id := uuid.New()
	calls := 0
	Default.Register(id, func() { calls++ })
	defer Default.Deregister(id)

	Default.Cancel(id)

	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

// TestPublishSubscribeRoundTrip is an integration test that publishes a job
// id over a real Redis job:cancel channel and asserts a Subscriber running
// against a shared Registry cancels the matching entry, then asserts Run
// returns promptly once its ctx is canceled (no leaked goroutine, T-39-04).
// Requires a live Redis (REDIS_ADDR); skipped otherwise, mirroring
// internal/queue/queue_test.go's REDIS_ADDR skip.
func TestPublishSubscribeRoundTrip(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set; skipping integration test")
	}

	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()

	reg := NewRegistry()
	id := uuid.New()
	canceled := make(chan struct{})
	reg.Register(id, func() { close(canceled) })
	defer reg.Deregister(id)

	sub := NewSubscriber(rdb, reg)
	subCtx, subCancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		sub.Run(subCtx)
		close(done)
	}()

	// Give the subscription time to establish before publishing -- a
	// publish before Subscribe's SUBSCRIBE reaches Redis would be missed
	// (fire-and-forget pub/sub has no backlog), same caveat this package's
	// doc comment documents for the real system.
	time.Sleep(200 * time.Millisecond)

	pub := NewPublisher(rdb)
	if err := pub.Publish(context.Background(), id); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("CancelFunc was not invoked within timeout after Publish")
	}

	subCancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Subscriber.Run did not return within timeout after ctx cancellation")
	}
}
