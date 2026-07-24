package cancel

import (
	"sync"
	"testing"

	"github.com/google/uuid"
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
