package workerproto

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"sync"
	"time"

	"github.com/tileforge/octoconv/internal/storage"
)

type copyPause struct {
	started chan struct{}
	release chan struct{}
}

type memStore struct {
	mu         sync.Mutex
	objects    map[string][]byte
	presignGet int
	presignPut int
	copies     int
	removes    []string
	copyErr    error
	removeErr  error
	pause      *copyPause
}

func newMemStore() *memStore {
	return &memStore{objects: map[string][]byte{}}
}

var listenerStores sync.Map

func rememberListenerStore(addr string, store *memStore) {
	listenerStores.Store(addr, store)
}

func listenerStore(addr string) *memStore {
	value, ok := listenerStores.Load(addr)
	if !ok {
		return newMemStore()
	}
	return value.(*memStore)
}

func fileLinkURL(method, key string) string {
	return "https://files.test/object?method=" + method + "&key=" + url.QueryEscape(key)
}

func (m *memStore) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	m.mu.Lock()
	m.presignGet++
	m.mu.Unlock()
	return fileLinkURL("GET", key), nil
}

func (m *memStore) PresignPut(ctx context.Context, key string, ttl time.Duration) (string, error) {
	m.mu.Lock()
	m.presignPut++
	m.mu.Unlock()
	return fileLinkURL("PUT", key), nil
}

func (m *memStore) Copy(ctx context.Context, srcKey, dstKey string) error {
	m.mu.Lock()
	if m.copyErr != nil {
		err := m.copyErr
		m.mu.Unlock()
		return err
	}
	payload, ok := m.objects[srcKey]
	if !ok {
		m.mu.Unlock()
		return storage.ErrNotFound
	}
	m.objects[dstKey] = append([]byte(nil), payload...)
	m.copies++
	pause := m.pause
	m.pause = nil
	m.mu.Unlock()
	if pause != nil {
		pause.started <- struct{}{}
		select {
		case <-pause.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (m *memStore) ObjectSize(ctx context.Context, key string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	payload, ok := m.objects[key]
	if !ok {
		return 0, storage.ErrNotFound
	}
	return int64(len(payload)), nil
}

func (m *memStore) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	payload, ok := m.get(key)
	if !ok {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(payload)), nil
}

func (m *memStore) Remove(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removes = append(m.removes, key)
	if m.removeErr != nil {
		return m.removeErr
	}
	delete(m.objects, key)
	return nil
}

func (m *memStore) put(key string, payload []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = append([]byte(nil), payload...)
}

func (m *memStore) get(key string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	payload, ok := m.objects[key]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), payload...), true
}

func (m *memStore) copyCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.copies
}

func (m *memStore) pauseNextCopy() (<-chan struct{}, func()) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once
	m.mu.Lock()
	m.pause = &copyPause{started: started, release: release}
	m.mu.Unlock()
	return started, func() { once.Do(func() { close(release) }) }
}
