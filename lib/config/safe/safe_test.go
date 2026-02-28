package safe_test

import (
	"testing"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/memory"
	"github.com/ccontavalli/enkit/lib/config/safe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testCloser struct{ closed *int }

func (t testCloser) Close() error {
	*t.closed = *t.closed + 1
	return nil
}

func TestRegistryCloseAll(t *testing.T) {
	r := safe.NewRegistry()
	count := 0
	r.WrapWorkspace(testWorkspace{closer: testCloser{closed: &count}})
	r.CloseAll()
	assert.Equal(t, 1, count)
}

type testWorkspace struct {
	closer testCloser
}

func (t testWorkspace) Open(app string, namespace ...string) (config.Store, error) {
	return testStore{closer: t.closer}, nil
}

func (t testWorkspace) Explore(app string, namespace ...string) (config.Explorer, error) {
	return testExplorer{closer: t.closer}, nil
}

func (t testWorkspace) Close() error {
	return t.closer.Close()
}

func TestWrapWorkspaceStoreClose(t *testing.T) {
	ws := safe.WrapWorkspace(memory.NewRaw())
	store, err := ws.Open("app", "ns")
	require.NoError(t, err)
	require.NoError(t, store.Close())
}

type testStore struct {
	closer testCloser
}

func (t testStore) List(mods ...config.ListModifier) ([]config.Descriptor, error) {
	return nil, nil
}

func (t testStore) Marshal(desc config.Descriptor, value interface{}) error {
	return nil
}

func (t testStore) Unmarshal(desc config.Descriptor, value interface{}) (config.Descriptor, error) {
	return desc, nil
}

func (t testStore) Delete(desc config.Descriptor) error {
	return nil
}

func (t testStore) Close() error {
	return t.closer.Close()
}

type testExplorer struct {
	closer testCloser
}

func (t testExplorer) List(mods ...config.ListModifier) ([]config.Descriptor, error) {
	return nil, nil
}

func (t testExplorer) Delete(desc config.Descriptor) error {
	return nil
}

func (t testExplorer) Close() error {
	return t.closer.Close()
}
