package memory

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/ccontavalli/enkit/lib/config"
)

// Workspace opens in-memory loaders and explorators.
type Workspace struct {
	mu    sync.RWMutex
	paths map[string]*Loader
}

// NewMarshal returns a loader workspace backed by in-memory loaders.
func NewMarshal() *Workspace {
	return &Workspace{paths: make(map[string]*Loader)}
}

// Open returns a loader for the provided namespace.
func (m *Workspace) Open(name string, namespace ...string) (config.Loader, error) {
	key := namespaceKey(name, namespace...)
	m.mu.Lock()
	loader, ok := m.paths[key]
	if !ok {
		loader = Open()
		m.paths[key] = loader
	}
	m.mu.Unlock()
	return loader, nil
}

// Explore returns an explorator for the provided namespace.
func (m *Workspace) Explore(name string, namespace ...string) (config.Explorer, error) {
	return &simpleExplorator{explorer: m, app: name, base: append([]string(nil), namespace...)}, nil
}

type simpleExplorator struct {
	explorer *Workspace
	app      string
	base     []string
}

func (e *simpleExplorator) List(mods ...config.ListModifier) ([]config.Descriptor, error) {
	opts := &config.ListOptions{}
	if err := config.ListModifiers(mods).Apply(opts); err != nil {
		return nil, err
	}
	if opts.Unmarshal != nil {
		return nil, fmt.Errorf("namespace list does not support unmarshal")
	}
	prefix := namespaceKey(e.app, e.base...)
	if prefix != "" {
		prefix += "/"
	}
	childSet := map[string]struct{}{}

	e.explorer.mu.RLock()
	for key := range e.explorer.paths {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rel := strings.TrimPrefix(key, prefix)
		parts := strings.Split(rel, "/")
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		childSet[parts[0]] = struct{}{}
	}
	e.explorer.mu.RUnlock()

	descs := config.SortedNamespaceDescriptors(e.base, config.KeysFromSet(childSet))
	return opts.Apply(descs, 0), nil
}

func (e *simpleExplorator) Delete(desc config.Descriptor) error {
	path := config.NamespacePathFromDescriptor(e.base, desc)
	target := namespaceKey(e.app, path...)
	prefix := target + "/"
	deleted := false

	e.explorer.mu.Lock()
	for key := range e.explorer.paths {
		if key == target || strings.HasPrefix(key, prefix) {
			delete(e.explorer.paths, key)
			deleted = true
		}
	}
	e.explorer.mu.Unlock()

	if !deleted {
		return os.ErrNotExist
	}
	return nil
}

func (e *simpleExplorator) Close() error { return nil }
