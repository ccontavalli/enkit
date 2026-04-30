package config_test

import (
	"errors"
	"testing"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/stretchr/testify/assert"
)

type scopeTestStore struct {
	closeErr error
	closed   int
}

func (s *scopeTestStore) List(mods ...config.ListModifier) ([]config.Descriptor, error) {
	return nil, nil
}

func (s *scopeTestStore) Marshal(desc config.Descriptor, value interface{}) error {
	return nil
}

func (s *scopeTestStore) Unmarshal(desc config.Descriptor, value interface{}) (config.Descriptor, error) {
	return desc, nil
}

func (s *scopeTestStore) Delete(desc config.Descriptor) error {
	return nil
}

func (s *scopeTestStore) Close() error {
	s.closed++
	return s.closeErr
}

type scopeTestLoader struct {
	closeErr error
	closed   int
}

func (l *scopeTestLoader) List(mods ...config.ListModifier) ([]string, error) {
	return nil, nil
}

func (l *scopeTestLoader) Read(name string) ([]byte, error) {
	return nil, nil
}

func (l *scopeTestLoader) Write(name string, data []byte) error {
	return nil
}

func (l *scopeTestLoader) Delete(name string) error {
	return nil
}

func (l *scopeTestLoader) Close() error {
	l.closed++
	return l.closeErr
}

type scopeStoreWorkspace struct {
	store      config.Store
	openErr    error
	apps       []string
	namespaces [][]string
	openCount  int
}

func (w *scopeStoreWorkspace) Open(app string, namespace ...string) (config.Store, error) {
	w.openCount++
	w.apps = append(w.apps, app)
	w.namespaces = append(w.namespaces, append([]string(nil), namespace...))
	if w.openErr != nil {
		return nil, w.openErr
	}
	return w.store, nil
}

func (w *scopeStoreWorkspace) Explore(app string, namespace ...string) (config.Explorer, error) {
	return nil, errors.New("unexpected Explore call")
}

func (w *scopeStoreWorkspace) ParsePath(path string) (config.ParsedPath, error) {
	return config.DefaultParsePath(path)
}

func (w *scopeStoreWorkspace) Close() error {
	return nil
}

type scopeLoaderWorkspace struct {
	loader     config.Loader
	openErr    error
	apps       []string
	namespaces [][]string
	openCount  int
}

func (w *scopeLoaderWorkspace) Open(app string, namespace ...string) (config.Loader, error) {
	w.openCount++
	w.apps = append(w.apps, app)
	w.namespaces = append(w.namespaces, append([]string(nil), namespace...))
	if w.openErr != nil {
		return nil, w.openErr
	}
	return w.loader, nil
}

func (w *scopeLoaderWorkspace) Explore(app string, namespace ...string) (config.Explorer, error) {
	return nil, errors.New("unexpected Explore call")
}

func (w *scopeLoaderWorkspace) ParsePath(path string) (config.ParsedPath, error) {
	return config.DefaultParsePath(path)
}

func (w *scopeLoaderWorkspace) Close() error {
	return nil
}

func TestStoreScopeChildAndRoot(t *testing.T) {
	scope := config.NewStoreScope(nil, "profiles", "by-id")
	child := scope.Child("1234", "settings")

	assert.Equal(t, "profiles", scope.App())
	assert.Equal(t, []string{"by-id"}, scope.Namespace())
	assert.Equal(t, "profiles", child.App())
	assert.Equal(t, []string{"by-id", "1234", "settings"}, child.Namespace())

	root := child.Root()
	assert.Equal(t, "profiles", root.AppName)
	assert.Equal(t, []string{"by-id", "1234", "settings"}, root.Namespaces)
}

func TestStoreScopeRunOpensAndClosesStore(t *testing.T) {
	store := &scopeTestStore{}
	workspace := &scopeStoreWorkspace{store: store}
	scope := config.NewStoreScope(workspace, "profiles", "by-id").Child("1234")

	called := false
	err := scope.Run(func(got config.Store) error {
		called = true
		assert.Same(t, store, got)
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, 1, workspace.openCount)
	assert.Equal(t, []string{"profiles"}, workspace.apps)
	assert.Equal(t, [][]string{{"by-id", "1234"}}, workspace.namespaces)
	assert.Equal(t, 1, store.closed)
}

func TestUseStoreReturnsValue(t *testing.T) {
	store := &scopeTestStore{}
	workspace := &scopeStoreWorkspace{store: store}
	scope := config.NewStoreScope(workspace, "profiles", "1234")

	value, err := config.UseStore(scope, func(got config.Store) (string, error) {
		assert.Same(t, store, got)
		return "ok", nil
	})

	assert.NoError(t, err)
	assert.Equal(t, "ok", value)
	assert.Equal(t, 1, store.closed)
}

func TestStoreScopeRunReturnsCallbackAndCloseErrors(t *testing.T) {
	runErr := errors.New("run failed")
	closeErr := errors.New("close failed")
	store := &scopeTestStore{closeErr: closeErr}
	workspace := &scopeStoreWorkspace{store: store}
	scope := config.NewStoreScope(workspace, "profiles", "1234")

	err := scope.Run(func(store config.Store) error {
		return runErr
	})

	assert.ErrorIs(t, err, runErr)
	assert.ErrorIs(t, err, closeErr)
	assert.Equal(t, 1, store.closed)
}

func TestLoaderScopeRunOpensAndClosesLoader(t *testing.T) {
	loader := &scopeTestLoader{}
	workspace := &scopeLoaderWorkspace{loader: loader}
	scope := config.NewLoaderScope(workspace, "profiles", "by-id").Child("1234")

	called := false
	err := scope.Run(func(got config.Loader) error {
		called = true
		assert.Same(t, loader, got)
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, 1, workspace.openCount)
	assert.Equal(t, []string{"profiles"}, workspace.apps)
	assert.Equal(t, [][]string{{"by-id", "1234"}}, workspace.namespaces)
	assert.Equal(t, 1, loader.closed)
}

func TestStoreScopeRootInteropsWithResolvePathWithinStore(t *testing.T) {
	scope := config.NewStoreScope(nil, "profiles", "by-id", "1234")
	parsed, err := config.ResolvePathWithinStore(scope.Root(), "admin/config.yaml")

	assert.NoError(t, err)
	assert.Equal(t, "profiles", parsed.AppName)
	assert.Equal(t, []string{"by-id", "1234", "admin"}, parsed.Namespaces)
	assert.Equal(t, "config", parsed.Descriptor.Key())

	hinted, ok := parsed.Descriptor.(config.RequestedFormatDescriptor)
	if assert.True(t, ok) {
		assert.Equal(t, "yaml", hinted.Format())
	}
}

func TestStoreScopeRunRejectsNilWorkspaceAndCallback(t *testing.T) {
	scope := config.NewStoreScope(nil, "profiles", "1234")

	err := scope.Run(func(store config.Store) error { return nil })
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "workspace")

	workspace := &scopeStoreWorkspace{store: &scopeTestStore{}}
	err = config.NewStoreScope(workspace, "profiles", "1234").Run(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "callback")
}

func TestUseStoreClosesOnPanic(t *testing.T) {
	store := &scopeTestStore{}
	workspace := &scopeStoreWorkspace{store: store}
	scope := config.NewStoreScope(workspace, "profiles", "1234")

	assert.PanicsWithValue(t, "boom", func() {
		_, _ = config.UseStore(scope, func(got config.Store) (string, error) {
			assert.Same(t, store, got)
			panic("boom")
		})
	})
	assert.Equal(t, 1, store.closed)
}

func TestUseLoaderClosesOnPanic(t *testing.T) {
	loader := &scopeTestLoader{}
	workspace := &scopeLoaderWorkspace{loader: loader}
	scope := config.NewLoaderScope(workspace, "profiles", "1234")

	assert.PanicsWithValue(t, "boom", func() {
		_, _ = config.UseLoader(scope, func(got config.Loader) (string, error) {
			assert.Same(t, loader, got)
			panic("boom")
		})
	})
	assert.Equal(t, 1, loader.closed)
}
