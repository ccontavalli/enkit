package blob

import (
	"net/url"
	"testing"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testCodec struct{}

func (testCodec) Encode(s string) string { return "e-" + s }
func (testCodec) Decode(s string) string { return s[2:] }

func TestCodecStoreList(t *testing.T) {
	store := &testBlobStore{list: []Descriptor{Key("e-one"), Key("e-two")}}
	wrapped := WrapCodecStore(store, testCodec{})
	list, err := wrapped.List()
	require.NoError(t, err)
	assert.Equal(t, []Descriptor{Key("one"), Key("two")}, list)
}

func TestCodecStoreURL(t *testing.T) {
	store := &testBlobStore{}
	wrapped := WrapCodecStore(store, testCodec{})

	_, err := wrapped.DownloadURL(Key("file"))
	require.NoError(t, err)
	assert.Equal(t, "e-file", store.lastKey)
}

type testBlobStore struct {
	list    []Descriptor
	lastKey string
}

func (t *testBlobStore) List() ([]Descriptor, error) { return t.list, nil }

func (t *testBlobStore) DownloadURL(desc Descriptor, _ ...TransferOption) (*url.URL, error) {
	t.lastKey = desc.Key()
	return &url.URL{Path: "/"}, nil
}

func (t *testBlobStore) UploadURL(desc Descriptor, _ ...TransferOption) (*url.URL, error) {
	t.lastKey = desc.Key()
	return &url.URL{Path: "/"}, nil
}

func (t *testBlobStore) Delete(desc Descriptor) error {
	t.lastKey = desc.Key()
	return nil
}

func (t *testBlobStore) Close() error { return nil }

var _ Store = (*testBlobStore)(nil)
var _ config.KeyCodec = testCodec{}
