package blob

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"testing"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ Workspace = (*storeWorkspace)(nil)
var _ StreamWorkspace = (*streamWorkspace)(nil)
var _ StreamWorkspace = (*loaderWorkspaceAdapter)(nil)
var _ Store = (*prefixedStore)(nil)
var _ StreamLoader = (*prefixedStreamLoader)(nil)
var _ config.Loader = (*prefixedStreamLoader)(nil)

type workspaceTestStore struct {
	list       []Descriptor
	lastURLKey string
	lastDelKey string
	closeErr   error
	closeCount int
}

func (t *workspaceTestStore) List() ([]Descriptor, error) { return t.list, nil }

func (t *workspaceTestStore) DownloadURL(desc Descriptor, _ ...TransferOption) (*url.URL, error) {
	t.lastURLKey = desc.Key()
	return &url.URL{Path: "/" + desc.Key()}, nil
}

func (t *workspaceTestStore) UploadURL(desc Descriptor, _ ...TransferOption) (*url.URL, error) {
	t.lastURLKey = desc.Key()
	return &url.URL{Path: "/" + desc.Key()}, nil
}

func (t *workspaceTestStore) Delete(desc Descriptor) error {
	t.lastDelKey = desc.Key()
	return nil
}

func (t *workspaceTestStore) Close() error {
	t.closeCount++
	return t.closeErr
}

type loaderOnly struct{}

func (loaderOnly) List(mods ...config.ListModifier) ([]string, error) { return nil, nil }
func (loaderOnly) Read(name string) ([]byte, error)                   { return nil, nil }
func (loaderOnly) Write(name string, data []byte) error               { return nil }
func (loaderOnly) Delete(name string) error                           { return nil }
func (loaderOnly) Close() error                                       { return nil }

type loaderOnlyWorkspace struct{}

func (loaderOnlyWorkspace) Open(name string, namespace ...string) (config.Loader, error) {
	return loaderOnly{}, nil
}
func (loaderOnlyWorkspace) Explore(name string, namespace ...string) (config.Explorer, error) {
	return nil, errors.New("unexpected Explore call")
}
func (loaderOnlyWorkspace) ParsePath(path string) (config.ParsedPath, error) {
	return config.DefaultParsePath(path)
}
func (loaderOnlyWorkspace) Close() error { return nil }

type scopeOpenStore struct {
	closeErr   error
	closeCount int
	lastURLKey string
}

func (s *scopeOpenStore) List() ([]Descriptor, error) { return nil, nil }
func (s *scopeOpenStore) DownloadURL(desc Descriptor, _ ...TransferOption) (*url.URL, error) {
	s.lastURLKey = desc.Key()
	return &url.URL{Path: "/" + desc.Key()}, nil
}
func (s *scopeOpenStore) UploadURL(desc Descriptor, _ ...TransferOption) (*url.URL, error) {
	s.lastURLKey = desc.Key()
	return &url.URL{Path: "/" + desc.Key()}, nil
}
func (s *scopeOpenStore) Delete(desc Descriptor) error { return nil }
func (s *scopeOpenStore) Close() error {
	s.closeCount++
	return s.closeErr
}

type scopeWorkspace struct {
	store    Store
	openErr  error
	openName string
	openNS   []string
}

func (w *scopeWorkspace) Open(name string, namespace ...string) (Store, error) {
	w.openName = name
	w.openNS = append([]string(nil), namespace...)
	if w.openErr != nil {
		return nil, w.openErr
	}
	return w.store, nil
}
func (w *scopeWorkspace) Close() error { return nil }

func TestWorkspacePrefixesStoreKeys(t *testing.T) {
	root := &workspaceTestStore{
		list: []Descriptor{
			Key("profiles/1234/avatar"),
			Key("profiles/1234/docs/resume"),
			Key("profiles/other/avatar"),
			Key("misc/root"),
		},
	}
	ws := NewWorkspace(root)
	store, err := ws.Open("profiles", "1234")
	require.NoError(t, err)

	list, err := store.List()
	require.NoError(t, err)
	assert.Equal(t, []Descriptor{Key("avatar"), Key("docs/resume")}, list)

	_, err = store.DownloadURL(Key("avatar"))
	require.NoError(t, err)
	assert.Equal(t, "profiles/1234/avatar", root.lastURLKey)

	err = store.Delete(Key("docs/resume"))
	require.NoError(t, err)
	assert.Equal(t, "profiles/1234/docs/resume", root.lastDelKey)

	assert.NoError(t, store.Close())
	assert.Equal(t, 0, root.closeCount)
	assert.NoError(t, ws.Close())
	assert.Equal(t, 1, root.closeCount)
}

func TestStreamWorkspacePrefixesLoaderKeys(t *testing.T) {
	root := memory.Open()
	require.NoError(t, root.Write("profiles/1234/avatar", []byte("image")))
	require.NoError(t, root.Write("profiles/other/avatar", []byte("other")))

	ws := NewStreamWorkspace(root)
	loader, err := ws.Open("profiles", "1234")
	require.NoError(t, err)

	keys, err := loader.List()
	require.NoError(t, err)
	assert.Equal(t, []string{"avatar"}, keys)

	reader, err := loader.Reader("avatar")
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.NoError(t, reader.Close())
	assert.Equal(t, "image", string(data))

	writer, err := loader.Writer("notes")
	require.NoError(t, err)
	_, err = writer.Write([]byte("hello"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	got, err := root.Read("profiles/1234/notes")
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), got)

	assert.NoError(t, loader.Close())
	assert.NoError(t, ws.Close())
}

func TestWrapLoaderWorkspaceAdaptsConfigLoaderWorkspace(t *testing.T) {
	raw := memory.NewMarshal()
	ws := WrapLoaderWorkspace(raw)

	scope := NewStreamScope(ws, "profiles", "1234")
	err := scope.Run(func(loader StreamLoader) error {
		writer, err := loader.Writer("avatar")
		if err != nil {
			return err
		}
		if _, err := writer.Write([]byte("image")); err != nil {
			return err
		}
		return writer.Close()
	})
	require.NoError(t, err)

	loader, err := raw.Open("profiles", "1234")
	require.NoError(t, err)
	stream, ok := loader.(StreamLoader)
	require.True(t, ok)

	reader, err := stream.Reader("avatar")
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.NoError(t, reader.Close())
	assert.Equal(t, []byte("image"), data)
}

func TestWrapLoaderWorkspaceRejectsNonStreamLoaders(t *testing.T) {
	ws := WrapLoaderWorkspace(loaderOnlyWorkspace{})
	_, err := ws.Open("profiles", "1234")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not implement blob.StreamLoader")
}

func TestStoreScopeRunOpensAndClosesStore(t *testing.T) {
	store := &scopeOpenStore{}
	ws := &scopeWorkspace{store: store}
	scope := NewStoreScope(ws, "profiles", "by-id").Child("1234")

	err := scope.Run(func(store Store) error {
		_, err := store.DownloadURL(Key("avatar"))
		return err
	})
	require.NoError(t, err)
	assert.Equal(t, "profiles", ws.openName)
	assert.Equal(t, []string{"by-id", "1234"}, ws.openNS)
	assert.Equal(t, "avatar", store.lastURLKey)
	assert.Equal(t, 1, store.closeCount)
}

func TestStreamScopeRunReturnsValue(t *testing.T) {
	root := memory.Open()
	ws := NewStreamWorkspace(root)
	scope := NewStreamScope(ws, "profiles", "1234")

	value, err := UseStream(scope, func(loader StreamLoader) (string, error) {
		writer, err := loader.Writer("doc")
		if err != nil {
			return "", err
		}
		if _, err := writer.Write([]byte("hello")); err != nil {
			return "", err
		}
		if err := writer.Close(); err != nil {
			return "", err
		}
		reader, err := loader.Reader("doc")
		if err != nil {
			return "", err
		}
		defer reader.Close()
		data, err := io.ReadAll(reader)
		if err != nil {
			return "", err
		}
		return string(data), nil
	})

	require.NoError(t, err)
	assert.Equal(t, "hello", value)
}

func TestStoreScopeRunReturnsCallbackAndCloseErrors(t *testing.T) {
	runErr := errors.New("run failed")
	closeErr := errors.New("close failed")
	store := &scopeOpenStore{closeErr: closeErr}
	ws := &scopeWorkspace{store: store}
	scope := NewStoreScope(ws, "profiles", "1234")

	err := scope.Run(func(store Store) error {
		return runErr
	})

	assert.ErrorIs(t, err, runErr)
	assert.ErrorIs(t, err, closeErr)
}

func TestStoreScopeChildAndRoot(t *testing.T) {
	scope := NewStoreScope(nil, "profiles", "by-id")
	child := scope.Child("1234", "photos")

	assert.Equal(t, "profiles", scope.Name())
	assert.Equal(t, []string{"by-id"}, scope.Namespace())
	assert.Equal(t, "profiles", child.Name())
	assert.Equal(t, []string{"by-id", "1234", "photos"}, child.Namespace())
	assert.Equal(t, StoreRoot{
		Name:       "profiles",
		Namespaces: []string{"by-id", "1234", "photos"},
	}, child.Root())
}

func TestWorkspaceOpenRejectsEmptyName(t *testing.T) {
	ws := NewWorkspace(&workspaceTestStore{})
	_, err := ws.Open("", "1234")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

func TestWorkspaceCloseRejectsFurtherOpen(t *testing.T) {
	ws := NewWorkspace(&workspaceTestStore{})
	require.NoError(t, ws.Close())
	_, err := ws.Open("profiles", "1234")
	require.Error(t, err)
	assert.ErrorIs(t, err, fs.ErrClosed)
}

func TestNewStreamWorkspaceRoundTripBytes(t *testing.T) {
	root := memory.Open()
	ws := NewStreamWorkspace(root)
	loader, err := ws.Open("profiles", "1234")
	require.NoError(t, err)
	writer, err := loader.Writer("hello.txt")
	require.NoError(t, err)
	_, err = io.Copy(writer, bytes.NewBufferString("world"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	data, err := root.Read("profiles/1234/hello.txt")
	require.NoError(t, err)
	assert.Equal(t, []byte("world"), data)
}
