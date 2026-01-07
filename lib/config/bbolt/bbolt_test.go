package bbolt

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/stretchr/testify/assert"
)

func TestBoltStoreRoundTrip(t *testing.T) {
	tmp, err := os.CreateTemp("", "config-bbolt-*.db")
	assert.NoError(t, err)
	path := tmp.Name()
	assert.NoError(t, tmp.Close())
	defer os.Remove(path)

	db, err := New(WithPath(path))
	assert.NoError(t, err)
	defer db.Close()

	store, err := db.Open("myapp", "testns")
	assert.NoError(t, err)

	type TestConfig struct {
		Value string
	}

	err = store.Marshal(config.Key("config"), &TestConfig{Value: "hello"})
	assert.NoError(t, err)

	var loaded TestConfig
	_, err = store.Unmarshal("config", &loaded)
	assert.NoError(t, err)
	assert.Equal(t, "hello", loaded.Value)

	descs, err := store.List()
	assert.NoError(t, err)
	assert.True(t, descriptorListContains(descs, "config"))

	err = store.Delete(config.Key("config"))
	assert.NoError(t, err)

	_, err = store.Unmarshal("config", &loaded)
	assert.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

func TestBoltStoreJSON(t *testing.T) {
	tmp, err := os.CreateTemp("", "config-bbolt-json-*.db")
	assert.NoError(t, err)
	path := tmp.Name()
	assert.NoError(t, tmp.Close())
	defer os.Remove(path)

	db, err := New(WithPath(path))
	assert.NoError(t, err)
	defer db.Close()

	store, err := db.Open("myapp", "json")
	assert.NoError(t, err)

	type TestConfig struct {
		Value string `json:"value"`
	}

	err = store.Marshal(config.Key("config"), TestConfig{Value: "hello"})
	assert.NoError(t, err)

	boltStore, ok := store.(*BoltStore)
	assert.True(t, ok)

	data, err := boltStore.loader.Read("config")
	assert.NoError(t, err)

	expected, err := json.Marshal(TestConfig{Value: "hello"})
	assert.NoError(t, err)
	assert.Equal(t, expected, data)

	err = boltStore.loader.Write("bad", []byte("{"))
	assert.NoError(t, err)

	var loaded TestConfig
	_, err = store.Unmarshal("bad", &loaded)
	assert.Error(t, err)
}

func descriptorListContains(descs []config.Descriptor, name string) bool {
	for _, desc := range descs {
		if desc != nil && desc.Key() == name {
			return true
		}
	}
	return false
}
