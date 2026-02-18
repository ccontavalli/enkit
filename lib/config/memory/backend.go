package memory

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/ccontavalli/enkit/lib/config"
)

// Backend provides Open and Explore over in-memory stores.
type Backend struct {
	mu     sync.RWMutex
	stores map[string]*Store
}

// NewBackend returns a new in-memory backend.
func NewBackend() *Backend {
	return &Backend{stores: make(map[string]*Store)}
}

// Open returns a Store for the specified namespace path.
func (b *Backend) Open(app string, namespace ...string) (config.Store, error) {
	key := namespaceKey(app, namespace...)
	b.mu.Lock()
	defer b.mu.Unlock()
	store, ok := b.stores[key]
	if !ok {
		store = NewStore()
		b.stores[key] = store
	}
	return store, nil
}

// Explore returns a Store that lists child namespaces under the provided path.
func (b *Backend) Explore(app string, namespace ...string) (config.Explorator, error) {
	return &explorator{backend: b, app: app, base: append([]string(nil), namespace...)}, nil
}

type explorator struct {
	backend *Backend
	app     string
	base    []string
}

func (s *explorator) List(mods ...config.ListModifier) ([]config.Descriptor, error) {
	opts := &config.ListOptions{}
	if err := config.ListModifiers(mods).Apply(opts); err != nil {
		return nil, err
	}
	if opts.Unmarshal != nil {
		return nil, fmt.Errorf("namespace list does not support unmarshal")
	}

	prefix := namespaceKey(s.app, s.base...)
	if prefix != "" {
		prefix += "/"
	}
	childSet := map[string]struct{}{}

	s.backend.mu.RLock()
	for key := range s.backend.stores {
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
	s.backend.mu.RUnlock()

	descs := config.SortedNamespaceDescriptors(s.base, config.KeysFromSet(childSet))

	return opts.Apply(descs, 0), nil
}

func (s *explorator) Delete(desc config.Descriptor) error {
	path := config.NamespacePathFromDescriptor(s.base, desc)
	target := namespaceKey(s.app, path...)
	prefix := target + "/"

	s.backend.mu.Lock()
	defer s.backend.mu.Unlock()
	deleted := false
	for key := range s.backend.stores {
		if key == target || strings.HasPrefix(key, prefix) {
			delete(s.backend.stores, key)
			deleted = true
		}
	}
	if !deleted {
		return os.ErrNotExist
	}
	return nil
}

func (s *explorator) Close() error { return nil }

func namespaceKey(app string, namespace ...string) string {
	parts := append([]string{app}, namespace...)
	return strings.Join(parts, "/")
}
