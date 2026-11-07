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
	onRemove   func(string) error
	sizes      map[string]int64
	downloads  int
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
	if size, overridden := m.sizes[key]; overridden {
		return size, nil
	}
	return int64(len(payload)), nil
}

func (m *memStore) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	m.downloads++
	payload, ok := m.objects[key]
	if ok {
		payload = append([]byte(nil), payload...)
	}
	m.mu.Unlock()
	if !ok {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(payload)), nil
}

func (m *memStore) Remove(ctx context.Context, key string) error {
	m.mu.Lock()
	m.removes = append(m.removes, key)
	hook := m.onRemove
	err := m.removeErr
	m.mu.Unlock()
	if err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.objects, key)
	m.mu.Unlock()
	if hook != nil {
		return hook(key)
	}
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

func (m *memStore) downloadCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.downloads
}

func (m *memStore) removedKeys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.removes...)
}

func (m *memStore) resetRemoves() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removes = nil
}

func (m *memStore) setOnRemove(fn func(string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onRemove = fn
}

func (m *memStore) setRemoveErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeErr = err
}

func (m *memStore) setCopyErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.copyErr = err
}

func (m *memStore) setSize(key string, size int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sizes == nil {
		m.sizes = map[string]int64{}
	}
	m.sizes[key] = size
}

func (m *memStore) signerCounts() (int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.presignGet, m.presignPut
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
