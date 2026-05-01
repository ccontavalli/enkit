package blob

import (
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"strings"
	"sync"

	"github.com/ccontavalli/enkit/lib/config"
)

type storeWorkspace struct {
	mu     sync.Mutex
	store  Store
	closed bool
}

// NewWorkspace wraps a flat blob store and exposes namespace-scoped opens by
// prefixing keys.
//
// The returned workspace owns the wrapped store. Stores returned by Open are
// lightweight scoped views whose Close method is a no-op; closing the workspace
// closes the underlying root store.
func NewWorkspace(store Store) Workspace {
	return &storeWorkspace{store: store}
}

func (w *storeWorkspace) Open(name string, namespace ...string) (Store, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil, fs.ErrClosed
	}
	if w.store == nil {
		return nil, fmt.Errorf("blob store is required")
	}
	prefix, err := scopePrefix(name, namespace...)
	if err != nil {
		return nil, err
	}
	return &prefixedStore{store: w.store, prefix: prefix}, nil
}

func (w *storeWorkspace) Close() error {
	w.mu.Lock()
	store := w.store
	w.closed = true
	w.mu.Unlock()
	if store == nil {
		return nil
	}
	return store.Close()
}

type streamWorkspace struct {
	mu     sync.Mutex
	loader StreamLoader
	closed bool
}

// NewStreamWorkspace wraps a flat blob stream loader and exposes
// namespace-scoped opens by prefixing keys.
//
// The returned workspace owns the wrapped loader. Loaders returned by Open are
// lightweight scoped views whose Close method is a no-op; closing the workspace
// closes the underlying root loader.
func NewStreamWorkspace(loader StreamLoader) StreamWorkspace {
	return &streamWorkspace{loader: loader}
}

func (w *streamWorkspace) Open(name string, namespace ...string) (StreamLoader, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil, fs.ErrClosed
	}
	if w.loader == nil {
		return nil, fmt.Errorf("blob stream loader is required")
	}
	prefix, err := scopePrefix(name, namespace...)
	if err != nil {
		return nil, err
	}
	return &prefixedStreamLoader{loader: w.loader, prefix: prefix}, nil
}

func (w *streamWorkspace) Close() error {
	w.mu.Lock()
	loader := w.loader
	w.closed = true
	w.mu.Unlock()
	if loader == nil {
		return nil
	}
	return loader.Close()
}

type loaderWorkspaceAdapter struct {
	workspace config.LoaderWorkspace
}

// WrapLoaderWorkspace adapts a config loader workspace into a blob stream
// workspace.
//
// Each Open expects the resolved config loader to also implement
// blob.StreamLoader. This is true for file-backed loaders such as directory and
// memory, but not guaranteed for all config loader backends.
func WrapLoaderWorkspace(workspace config.LoaderWorkspace) StreamWorkspace {
	return &loaderWorkspaceAdapter{workspace: workspace}
}

func (w *loaderWorkspaceAdapter) Open(name string, namespace ...string) (StreamLoader, error) {
	if w.workspace == nil {
		return nil, fmt.Errorf("config loader workspace is required")
	}
	loader, err := w.workspace.Open(name, namespace...)
	if err != nil {
		return nil, err
	}
	stream, ok := loader.(StreamLoader)
	if !ok {
		_ = loader.Close()
		return nil, fmt.Errorf("loader for %s does not implement blob.StreamLoader", scopeName(name, namespace...))
	}
	return stream, nil
}

func (w *loaderWorkspaceAdapter) Close() error {
	if w.workspace == nil {
		return nil
	}
	return w.workspace.Close()
}

type prefixedStore struct {
	store  Store
	prefix string
}

func (s *prefixedStore) List() ([]Descriptor, error) {
	descs, err := s.store.List()
	if err != nil {
		return nil, err
	}
	out := make([]Descriptor, 0, len(descs))
	for _, desc := range descs {
		rel, ok := stripScopePrefix(s.prefix, desc.Key())
		if !ok || rel == "" {
			continue
		}
		out = append(out, Key(rel))
	}
	return out, nil
}

func (s *prefixedStore) DownloadURL(desc Descriptor, opts ...TransferOption) (*url.URL, error) {
	return s.store.DownloadURL(Key(s.prefix+desc.Key()), opts...)
}

func (s *prefixedStore) UploadURL(desc Descriptor, opts ...TransferOption) (*url.URL, error) {
	return s.store.UploadURL(Key(s.prefix+desc.Key()), opts...)
}

func (s *prefixedStore) Delete(desc Descriptor) error {
	return s.store.Delete(Key(s.prefix + desc.Key()))
}

func (s *prefixedStore) Close() error { return nil }

type prefixedStreamLoader struct {
	loader StreamLoader
	prefix string
}

func (l *prefixedStreamLoader) List(mods ...config.ListModifier) ([]string, error) {
	opts := &config.ListOptions{}
	if err := config.ListModifiers(mods).Apply(opts); err != nil {
		return nil, err
	}
	keys, err := l.loader.List()
	if err != nil {
		return nil, err
	}
	filtered := make([]string, 0, len(keys))
	for _, key := range keys {
		rel, ok := stripScopePrefix(l.prefix, key)
		if !ok || rel == "" {
			continue
		}
		filtered = append(filtered, rel)
	}
	return opts.FinalizeKeys(l, filtered, 0)
}

func (l *prefixedStreamLoader) Reader(name string) (io.ReadCloser, error) {
	return l.loader.Reader(l.prefix + name)
}

func (l *prefixedStreamLoader) Read(name string) ([]byte, error) {
	reader, err := l.Reader(name)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func (l *prefixedStreamLoader) Writer(name string) (io.WriteCloser, error) {
	return l.loader.Writer(l.prefix + name)
}

func (l *prefixedStreamLoader) Write(name string, data []byte) error {
	writer, err := l.Writer(name)
	if err != nil {
		return err
	}
	if _, err := writer.Write(data); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}

func (l *prefixedStreamLoader) Delete(name string) error {
	return l.loader.Delete(l.prefix + name)
}

func (l *prefixedStreamLoader) Close() error { return nil }

func scopePrefix(name string, namespace ...string) (string, error) {
	name = strings.Trim(name, "/")
	if name == "" {
		return "", fmt.Errorf("blob store name cannot be empty")
	}
	parts := []string{name}
	for _, ns := range namespace {
		ns = strings.Trim(ns, "/")
		if ns == "" {
			continue
		}
		parts = append(parts, ns)
	}
	return strings.Join(parts, "/") + "/", nil
}

func scopeName(name string, namespace ...string) string {
	prefix, err := scopePrefix(name, namespace...)
	if err != nil {
		return name
	}
	return strings.TrimSuffix(prefix, "/")
}

func stripScopePrefix(prefix string, key string) (string, bool) {
	if prefix == "" {
		return key, true
	}
	if !strings.HasPrefix(key, prefix) {
		return "", false
	}
	return strings.TrimPrefix(key, prefix), true
}
