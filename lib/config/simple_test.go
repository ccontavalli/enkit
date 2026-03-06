package config_test

import (
	"path/filepath"
	"testing"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/directory"
	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/stretchr/testify/assert"
)

type simpleTestConfig struct {
	Value string `toml:"value"`
}

type simpleListUnmarshalConfig struct {
	Name  string   `toml:"name"`
	Count int      `toml:"count,omitempty"`
	IDs   []string `toml:"ids,omitempty"`
}

func TestSimpleStoreEncodesKeys(t *testing.T) {
	base := t.TempDir()
	loader, err := directory.OpenDir(base)
	assert.NoError(t, err)

	store := config.OpenSimple(loader, marshal.Toml)
	key := "a/b%"
	err = store.Marshal(config.Key(key), simpleTestConfig{Value: "test"})
	assert.NoError(t, err)

	files, err := loader.List()
	assert.NoError(t, err)
	assert.Len(t, files, 1)
	assert.Equal(t, "a%2Fb%25.toml", files[0])

	var cfg simpleTestConfig
	_, err = store.Unmarshal(config.Key(key), &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "test", cfg.Value)

	paths, err := loader.List()
	assert.NoError(t, err)
	assert.Len(t, paths, 1)
	assert.Equal(t, filepath.Join(base, paths[0]), filepath.Join(base, "a%2Fb%25.toml"))
}

func TestDecodeKeyTolerance(t *testing.T) {
	assert.Equal(t, "a/b", config.DecodeKey("a%2Fb"))
	assert.Equal(t, "%zz", config.DecodeKey("%zz"))
	assert.Equal(t, "100%", config.DecodeKey("100%25"))
	assert.Equal(t, string([]byte{0}), config.DecodeKey("%00"))
}

func TestSimpleStoreKeyWithExtension(t *testing.T) {
	base := t.TempDir()
	loader, err := directory.OpenDir(base)
	assert.NoError(t, err)

	store := config.OpenSimple(loader, marshal.Toml)
	key := "foo.toml"
	err = store.Marshal(config.Key(key), simpleTestConfig{Value: "value"})
	assert.NoError(t, err)

	files, err := loader.List()
	assert.NoError(t, err)
	assert.Len(t, files, 1)
	assert.Equal(t, "foo.toml.toml", files[0])

	var cfg simpleTestConfig
	_, err = store.Unmarshal(config.Key(key), &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "value", cfg.Value)
}

func TestSimpleStoreKeyWithColon(t *testing.T) {
	base := t.TempDir()
	loader, err := directory.OpenDir(base)
	assert.NoError(t, err)

	store := config.OpenSimple(loader, marshal.Toml)
	key := "user:123"
	err = store.Marshal(config.Key(key), simpleTestConfig{Value: "value"})
	assert.NoError(t, err)

	files, err := loader.List()
	assert.NoError(t, err)
	assert.Len(t, files, 1)
	assert.Equal(t, "user:123.toml", files[0])

	var cfg simpleTestConfig
	_, err = store.Unmarshal(config.Key(key), &cfg)
	assert.NoError(t, err)
	assert.Equal(t, "value", cfg.Value)

	list, err := store.List()
	assert.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, key, list[0].Key())
}

func TestSimpleStoreListUnmarshalClearsMissingFields(t *testing.T) {
	base := t.TempDir()
	loader, err := directory.OpenDir(base)
	assert.NoError(t, err)

	store := config.OpenSimple(loader, marshal.Toml)
	err = store.Marshal(config.Key("a"), simpleListUnmarshalConfig{
		Name:  "first",
		Count: 2,
		IDs:   []string{"everyone-ho86l4nqzg"},
	})
	assert.NoError(t, err)
	err = store.Marshal(config.Key("b"), simpleListUnmarshalConfig{
		Name: "second",
	})
	assert.NoError(t, err)

	var current simpleListUnmarshalConfig
	items := []simpleListUnmarshalConfig{}
	_, err = store.List(config.Unmarshal(&current, func(_ config.Descriptor, value *simpleListUnmarshalConfig) error {
		items = append(items, *value)
		return nil
	}))
	assert.NoError(t, err)
	assert.Len(t, items, 2)
	assert.Equal(t, "first", items[0].Name)
	assert.Equal(t, 2, items[0].Count)
	assert.Equal(t, []string{"everyone-ho86l4nqzg"}, items[0].IDs)
	assert.Equal(t, "second", items[1].Name)
	assert.Equal(t, 0, items[1].Count)
	assert.Nil(t, items[1].IDs)
}
