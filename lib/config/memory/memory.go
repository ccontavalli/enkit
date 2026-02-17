// Package memory provides an in-memory loader suitable for tests and benchmarks.
package memory

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
)

// Loader is an in-memory implementation of config.Loader and blob.StreamLoader.
//
// Data is stored in memory only and is lost on process exit.
type Loader struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// New returns a new in-memory loader.
func New() *Loader {
	return &Loader{data: make(map[string][]byte)}
}

// List returns the stored keys in sorted order.
func (m *Loader) List() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]string, 0, len(m.data))
	for key := range m.data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

// Read returns a copy of the stored data.
func (m *Loader) Read(name string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.data[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

// Write stores a copy of the data.
func (m *Loader) Write(name string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[name] = append([]byte(nil), data...)
	return nil
}

// Delete removes the key if present.
func (m *Loader) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[name]; !ok {
		return os.ErrNotExist
	}
	delete(m.data, name)
	return nil
}

func (m *Loader) Close() error {
	return nil
}

// Reader returns a seekable reader for the stored data.
func (m *Loader) Reader(name string) (io.ReadCloser, error) {
	data, err := m.Read(name)
	if err != nil {
		return nil, err
	}
	return &readSeekerCloser{Reader: bytes.NewReader(data)}, nil
}

// Writer returns a write-closer that stores the written data on close.
func (m *Loader) Writer(name string) (io.WriteCloser, error) {
	return &memoryWriter{loader: m, key: name}, nil
}

type readSeekerCloser struct {
	*bytes.Reader
}

func (r *readSeekerCloser) Close() error {
	return nil
}

type memoryWriter struct {
	loader *Loader
	key    string
	buf    bytes.Buffer
}

func (w *memoryWriter) Write(p []byte) (int, error) {
	if w.loader == nil {
		return 0, fmt.Errorf("writer already closed")
	}
	return w.buf.Write(p)
}

func (w *memoryWriter) Close() error {
	if w.loader == nil {
		return nil
	}
	w.loader.Write(w.key, w.buf.Bytes())
	w.loader = nil
	return nil
}
