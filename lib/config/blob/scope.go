package blob

import (
	"fmt"

	"github.com/ccontavalli/enkit/lib/multierror"
)

// StoreRoot identifies the workspace root of a blob store.
type StoreRoot struct {
	Name       string
	Namespaces []string
}

// StoreScope identifies a blob store namespace without keeping the store open.
type StoreScope struct {
	workspace Workspace
	name      string
	namespace []string
}

// NewStoreScope returns a scope rooted at name/namespace within workspace.
func NewStoreScope(workspace Workspace, name string, namespace ...string) StoreScope {
	return StoreScope{
		workspace: workspace,
		name:      name,
		namespace: append([]string(nil), namespace...),
	}
}

// Child returns a new scope rooted under the receiver namespace.
func (s StoreScope) Child(namespace ...string) StoreScope {
	child := append(append([]string(nil), s.namespace...), namespace...)
	return StoreScope{
		workspace: s.workspace,
		name:      s.name,
		namespace: child,
	}
}

// Root returns the logical root identified by the scope.
func (s StoreScope) Root() StoreRoot {
	return StoreRoot{
		Name:       s.name,
		Namespaces: append([]string(nil), s.namespace...),
	}
}

// Name returns the scope root name.
func (s StoreScope) Name() string {
	return s.name
}

// Namespace returns a copy of the scope namespace path.
func (s StoreScope) Namespace() []string {
	return append([]string(nil), s.namespace...)
}

// Run opens the scoped store, runs fn, and closes the store before returning.
func (s StoreScope) Run(fn func(Store) error) error {
	if fn == nil {
		return fmt.Errorf("blob store callback cannot be nil")
	}
	_, err := UseStore(s, func(store Store) (struct{}, error) {
		return struct{}{}, fn(store)
	})
	return err
}

// UseStore opens the scoped store, runs fn, and closes the store before
// returning fn's value.
func UseStore[T any](s StoreScope, fn func(Store) (T, error)) (T, error) {
	var zero T
	if s.workspace == nil {
		return zero, fmt.Errorf("blob workspace cannot be nil")
	}
	if fn == nil {
		return zero, fmt.Errorf("blob store callback cannot be nil")
	}

	store, err := s.workspace.Open(s.name, s.namespace...)
	if err != nil {
		return zero, err
	}

	value, runErr := fn(store)
	closeErr := store.Close()
	if runErr != nil || closeErr != nil {
		return value, multierror.New([]error{runErr, closeErr})
	}
	return value, nil
}

// StreamScope identifies a blob stream-loader namespace without keeping the
// loader open.
type StreamScope struct {
	workspace StreamWorkspace
	name      string
	namespace []string
}

// NewStreamScope returns a scope rooted at name/namespace within workspace.
func NewStreamScope(workspace StreamWorkspace, name string, namespace ...string) StreamScope {
	return StreamScope{
		workspace: workspace,
		name:      name,
		namespace: append([]string(nil), namespace...),
	}
}

// Child returns a new scope rooted under the receiver namespace.
func (s StreamScope) Child(namespace ...string) StreamScope {
	child := append(append([]string(nil), s.namespace...), namespace...)
	return StreamScope{
		workspace: s.workspace,
		name:      s.name,
		namespace: child,
	}
}

// Root returns the logical root identified by the scope.
func (s StreamScope) Root() StoreRoot {
	return StoreRoot{
		Name:       s.name,
		Namespaces: append([]string(nil), s.namespace...),
	}
}

// Name returns the scope root name.
func (s StreamScope) Name() string {
	return s.name
}

// Namespace returns a copy of the scope namespace path.
func (s StreamScope) Namespace() []string {
	return append([]string(nil), s.namespace...)
}

// Run opens the scoped loader, runs fn, and closes the loader before returning.
func (s StreamScope) Run(fn func(StreamLoader) error) error {
	if fn == nil {
		return fmt.Errorf("blob stream callback cannot be nil")
	}
	_, err := UseStream(s, func(loader StreamLoader) (struct{}, error) {
		return struct{}{}, fn(loader)
	})
	return err
}

// UseStream opens the scoped loader, runs fn, and closes the loader before
// returning fn's value.
func UseStream[T any](s StreamScope, fn func(StreamLoader) (T, error)) (T, error) {
	var zero T
	if s.workspace == nil {
		return zero, fmt.Errorf("blob stream workspace cannot be nil")
	}
	if fn == nil {
		return zero, fmt.Errorf("blob stream callback cannot be nil")
	}

	loader, err := s.workspace.Open(s.name, s.namespace...)
	if err != nil {
		return zero, err
	}

	value, runErr := fn(loader)
	closeErr := loader.Close()
	if runErr != nil || closeErr != nil {
		return value, multierror.New([]error{runErr, closeErr})
	}
	return value, nil
}
