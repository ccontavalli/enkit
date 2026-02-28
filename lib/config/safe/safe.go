// Package safe provides best-effort cleanup helpers for config stores.
//
// The safe wrapper registers opened stores/workspaces in a global registry so a
// process can close them on shutdown signals. This is a best-effort safety net
// to reduce leaks when callers forget Close or the process exits via SIGINT or
// SIGTERM.
//
// Usage:
//
//	func main() {
//	    cleanup := safe.Install()
//	    defer cleanup()
//
//	    ws, _ := factory.NewStore(factory.FromFlags(flags))
//	    ws = safe.WrapWorkspace(ws)
//
//	    store, _ := ws.Open("myapp", "prod")
//	    defer store.Close()
//	}
//
// Tracing can wrap either order. A common pattern is:
//
//	ws = safe.WrapWorkspace(trace.WrapWorkspace(ws))
//
// Typed stores work with the safe wrapper:
//
//	store, _ := typed.OpenAs[MyConfig](safe.WrapWorkspace(ws), "myapp", "prod")
package safe

import (
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/ccontavalli/enkit/lib/config"
)

type entryKind int

const (
	kindStore entryKind = iota
	kindWorkspace
)

type entry struct {
	closer io.Closer
	kind   entryKind
}

// Registry tracks open stores/workspaces for best-effort cleanup.
type Registry struct {
	mu      sync.RWMutex
	nextID  uint64
	entries map[uint64]entry
}

// DefaultRegistry is used by the package-level helpers.
var DefaultRegistry = NewRegistry()

// NewRegistry creates a new registry.
func NewRegistry() *Registry {
	return &Registry{entries: make(map[uint64]entry)}
}

// Install installs signal handlers that close all tracked resources.
// It returns a cleanup function that stops the handlers and closes all entries.
func Install() func() {
	return DefaultRegistry.Install()
}

// WrapWorkspace wraps a StoreWorkspace with safety tracking.
func WrapWorkspace(ws config.StoreWorkspace) config.StoreWorkspace {
	return DefaultRegistry.WrapWorkspace(ws)
}

// Install installs signal handlers that close all tracked resources.
func (r *Registry) Install() func() {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	stopped := int32(0)

	go func() {
		<-sigc
		if atomic.CompareAndSwapInt32(&stopped, 0, 1) {
			r.CloseAll()
		}
	}()

	return func() {
		if atomic.CompareAndSwapInt32(&stopped, 0, 1) {
			signal.Stop(sigc)
			close(sigc)
			r.CloseAll()
		}
	}
}

// WrapWorkspace wraps a StoreWorkspace with safety tracking.
func (r *Registry) WrapWorkspace(ws config.StoreWorkspace) config.StoreWorkspace {
	if ws == nil {
		return nil
	}
	id := r.register(kindWorkspace, ws)
	return &workspace{
		StoreWorkspace: ws,
		reg:            r,
		id:             id,
	}
}

// CloseAll closes all tracked stores and workspaces.
func (r *Registry) CloseAll() {
	stores, workspaces := r.snapshot()
	for _, store := range stores {
		_ = store.Close()
	}
	for _, ws := range workspaces {
		_ = ws.Close()
	}
}

func (r *Registry) register(kind entryKind, closer io.Closer) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.nextID
	r.nextID++
	r.entries[id] = entry{closer: closer, kind: kind}
	return id
}

func (r *Registry) unregister(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, id)
}

func (r *Registry) snapshot() ([]config.Store, []config.StoreWorkspace) {
	r.mu.RLock()
	entries := make(map[uint64]entry, len(r.entries))
	for id, ent := range r.entries {
		entries[id] = ent
	}
	r.mu.RUnlock()

	r.mu.Lock()
	for id := range entries {
		delete(r.entries, id)
	}
	r.mu.Unlock()

	stores := make([]config.Store, 0)
	workspaces := make([]config.StoreWorkspace, 0)
	for _, ent := range entries {
		switch ent.kind {
		case kindStore:
			if store, ok := ent.closer.(config.Store); ok {
				stores = append(stores, store)
			}
		case kindWorkspace:
			if ws, ok := ent.closer.(config.StoreWorkspace); ok {
				workspaces = append(workspaces, ws)
			}
		}
	}
	return stores, workspaces
}

type workspace struct {
	config.StoreWorkspace
	reg *Registry
	id  uint64
}

func (w *workspace) Open(app string, namespace ...string) (config.Store, error) {
	store, err := w.StoreWorkspace.Open(app, namespace...)
	if err != nil {
		return nil, err
	}
	id := w.reg.register(kindStore, store)
	return &storeWrap{
		Store: store,
		reg:   w.reg,
		id:    id,
	}, nil
}

func (w *workspace) Close() error {
	w.reg.unregister(w.id)
	return w.StoreWorkspace.Close()
}

type storeWrap struct {
	config.Store
	reg *Registry
	id  uint64
}

func (s *storeWrap) Close() error {
	s.reg.unregister(s.id)
	return s.Store.Close()
}
