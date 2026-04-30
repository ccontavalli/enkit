package config

import (
	"fmt"

	"github.com/ccontavalli/enkit/lib/multierror"
)

// StoreScope identifies a store namespace without keeping the store open.
//
// StoreScope values are immutable and cheap to copy. Use Child to derive a
// deeper namespace and Run or UseStore to open the store for one operation.
type StoreScope struct {
	workspace StoreWorkspace
	app       string
	namespace []string
}

// NewStoreScope returns a scope rooted at app/namespace within workspace.
func NewStoreScope(workspace StoreWorkspace, app string, namespace ...string) StoreScope {
	return StoreScope{
		workspace: workspace,
		app:       app,
		namespace: append([]string(nil), namespace...),
	}
}

// Child returns a new scope rooted under the receiver namespace.
func (s StoreScope) Child(namespace ...string) StoreScope {
	child := append(append([]string(nil), s.namespace...), namespace...)
	return StoreScope{
		workspace: s.workspace,
		app:       s.app,
		namespace: child,
	}
}

// Root returns the logical store root identified by the scope.
func (s StoreScope) Root() StoreRoot {
	return StoreRoot{
		AppName:    s.app,
		Namespaces: append([]string(nil), s.namespace...),
	}
}

// App returns the scope app name.
func (s StoreScope) App() string {
	return s.app
}

// Namespace returns a copy of the scope namespace path.
func (s StoreScope) Namespace() []string {
	return append([]string(nil), s.namespace...)
}

// Run opens the scoped store, runs fn, and closes the store before returning.
func (s StoreScope) Run(fn func(Store) error) error {
	if fn == nil {
		return fmt.Errorf("store callback cannot be nil")
	}
	_, err := UseStore(s, func(store Store) (struct{}, error) {
		return struct{}{}, fn(store)
	})
	return err
}

// UseStore opens the scoped store, runs fn, and closes the store before
// returning fn's value.
func UseStore[T any](s StoreScope, fn func(Store) (T, error)) (value T, err error) {
	if s.workspace == nil {
		return value, fmt.Errorf("store workspace cannot be nil")
	}
	if fn == nil {
		return value, fmt.Errorf("store callback cannot be nil")
	}

	store, err := s.workspace.Open(s.app, s.namespace...)
	if err != nil {
		return value, err
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			err = multierror.New([]error{err, closeErr})
		}
	}()

	return fn(store)
}

// LoaderScope identifies a loader namespace without keeping the loader open.
//
// LoaderScope values are immutable and cheap to copy. Use Child to derive a
// deeper namespace and Run or UseLoader to open the loader for one operation.
type LoaderScope struct {
	workspace LoaderWorkspace
	app       string
	namespace []string
}

// NewLoaderScope returns a scope rooted at app/namespace within workspace.
func NewLoaderScope(workspace LoaderWorkspace, app string, namespace ...string) LoaderScope {
	return LoaderScope{
		workspace: workspace,
		app:       app,
		namespace: append([]string(nil), namespace...),
	}
}

// Child returns a new scope rooted under the receiver namespace.
func (s LoaderScope) Child(namespace ...string) LoaderScope {
	child := append(append([]string(nil), s.namespace...), namespace...)
	return LoaderScope{
		workspace: s.workspace,
		app:       s.app,
		namespace: child,
	}
}

// Root returns the logical store root identified by the scope.
func (s LoaderScope) Root() StoreRoot {
	return StoreRoot{
		AppName:    s.app,
		Namespaces: append([]string(nil), s.namespace...),
	}
}

// App returns the scope app name.
func (s LoaderScope) App() string {
	return s.app
}

// Namespace returns a copy of the scope namespace path.
func (s LoaderScope) Namespace() []string {
	return append([]string(nil), s.namespace...)
}

// Run opens the scoped loader, runs fn, and closes the loader before returning.
func (s LoaderScope) Run(fn func(Loader) error) error {
	if fn == nil {
		return fmt.Errorf("loader callback cannot be nil")
	}
	_, err := UseLoader(s, func(loader Loader) (struct{}, error) {
		return struct{}{}, fn(loader)
	})
	return err
}

// UseLoader opens the scoped loader, runs fn, and closes the loader before
// returning fn's value.
func UseLoader[T any](s LoaderScope, fn func(Loader) (T, error)) (value T, err error) {
	if s.workspace == nil {
		return value, fmt.Errorf("loader workspace cannot be nil")
	}
	if fn == nil {
		return value, fmt.Errorf("loader callback cannot be nil")
	}

	loader, err := s.workspace.Open(s.app, s.namespace...)
	if err != nil {
		return value, err
	}
	defer func() {
		if closeErr := loader.Close(); closeErr != nil {
			err = multierror.New([]error{err, closeErr})
		}
	}()

	return fn(loader)
}
