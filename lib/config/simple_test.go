package config

import (
	"path/filepath"
	"testing"

	"github.com/ccontavalli/enkit/lib/config/directory"
	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/stretchr/testify/assert"
)

type simpleTestConfig struct {
	Value string `toml:"value"`
}

func TestSimpleStoreEncodesKeys(t *testing.T) {
	base := t.TempDir()
	loader, err := directory.OpenDir(base)
	assert.NoError(t, err)

	store := NewSimple(loader, marshal.Toml)
	key := "a/b%"
	err = store.Marshal(Key(key), simpleTestConfig{Value: "test"})
	assert.NoError(t, err)

	files, err := loader.List()
	assert.NoError(t, err)
	assert.Len(t, files, 1)
	assert.Equal(t, "a%2Fb%25.toml", files[0])

	var cfg simpleTestConfig
	_, err = store.Unmarshal(Key(key), &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "test", cfg.Value)

	paths, err := loader.List()
	assert.NoError(t, err)
	assert.Len(t, paths, 1)
	assert.Equal(t, filepath.Join(base, paths[0]), filepath.Join(base, "a%2Fb%25.toml"))
}

func TestDecodeKeyTolerance(t *testing.T) {
	assert.Equal(t, "a/b", DecodeKey("a%2Fb"))
	assert.Equal(t, "%zz", DecodeKey("%zz"))
	assert.Equal(t, "100%", DecodeKey("100%25"))
	assert.Equal(t, string([]byte{0}), DecodeKey("%00"))
}

func TestSimpleStoreKeyWithExtension(t *testing.T) {
	base := t.TempDir()
	loader, err := directory.OpenDir(base)
	assert.NoError(t, err)

	store := NewSimple(loader, marshal.Toml)
	key := "foo.toml"
	err = store.Marshal(Key(key), simpleTestConfig{Value: "value"})
	assert.NoError(t, err)

	files, err := loader.List()
	assert.NoError(t, err)
	assert.Len(t, files, 1)
	assert.Equal(t, "foo.toml.toml", files[0])

	var cfg simpleTestConfig
	_, err = store.Unmarshal(Key(key), &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "value", cfg.Value)
}
