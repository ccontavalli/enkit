package config

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/ccontavalli/enkit/lib/config/directory"
	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/stretchr/testify/assert"
)

type InnerTestConfig struct {
	Wisdom string
}

type TestConfig struct {
	Key   string
	Value string
	Inner InnerTestConfig
}

func TestMulti(t *testing.T) {
	td, err := ioutil.TempDir("", "test-multi")
	assert.Nil(t, err)

	hd, err := directory.OpenDir(filepath.Join(td, "test"))
	assert.Nil(t, err)

	data := TestConfig{
		Key:   "Despair",
		Value: "is typical of those who do not understand the causes of evil, see no way out, and are incapable of struggle.",
		Inner: InnerTestConfig{
			Wisdom: "We shouldn't be looking for heroes, we should be looking for good ideas.",
		},
	}

	m := NewMulti(hd)

	found, err := m.List()
	assert.Nil(t, err)
	assert.Equal(t, 0, len(found))

	var read TestConfig
	_, err = m.Unmarshal(Key("quote"), &read)
	assert.True(t, os.IsNotExist(err))

	err = m.Delete(Key("quote"))
	assert.True(t, os.IsNotExist(err), "%v", err)
	err = m.Delete(Key("quote.toml"))
	assert.True(t, os.IsNotExist(err))

	err = m.Marshal(Key("quote"), data)
	assert.Nil(t, err)

	desc, err := m.Unmarshal(Key("quote"), &read)
	assert.Nil(t, err)
	assert.Equal(t, "quote", desc.Key())
	assert.Equal(t, marshal.Toml, desc.(*multiDescriptor).m)
	assert.Equal(t, data, read)

	data2 := TestConfig{
		Key: "If we don't believe in freedom of expression for people we despise, we don't believe in it at all.",
	}
	data3 := TestConfig{
		Key: "If you assume that there is no hope, you guarantee that there will be no hope.",
	}

	err = m.Marshal(FormatKey("quote", marshal.Json), data2)
	assert.Nil(t, err)

	// Despite writing a quote.json file, the preferred quote is the toml one.
	desc, err = m.Unmarshal(Key("quote"), &read)
	assert.Nil(t, err)
	assert.Equal(t, "quote", desc.Key())
	assert.Equal(t, marshal.Toml, desc.(*multiDescriptor).m)
	assert.Equal(t, data, read)

	// And writing it affects the toml, but not the json.
	err = m.Marshal(Key("quote"), data3)
	assert.Nil(t, err)

	desc, err = m.Unmarshal(FormatKey("quote", marshal.Json), &read)
	assert.Nil(t, err)
	assert.Equal(t, "quote", desc.Key())
	assert.Equal(t, marshal.Json, desc.(*multiDescriptor).m)
	assert.Equal(t, data2, read)

	// Marshalling via descriptor affects the correct file.
	err = m.Marshal(desc, data)
	assert.Nil(t, err)

	// Now we add a 3rd format, just so we can delete a file later.
	err = m.Marshal(FormatKey("quote", marshal.Yaml), data2)
	assert.Nil(t, err)

	found, err = m.List()
	assert.Nil(t, err)
	assert.ElementsMatch(t, []string{"quote.json", "quote.toml", "quote.yaml"}, descriptorPaths(found))

	// Let's delete a specific file.
	err = m.Delete(desc)
	assert.Nil(t, err)

	// Check that only one file was deleted.
	found, err = m.List()
	assert.Nil(t, err)
	assert.ElementsMatch(t, []string{"quote.toml", "quote.yaml"}, descriptorPaths(found))

	// Let's delete the whole key.
	err = m.Delete(Key("quote"))
	assert.Nil(t, err)

	// No quote anymore.
	found, err = m.List()
	assert.Nil(t, err)
	assert.Empty(t, found)
}

func descriptorPaths(descs []Descriptor) []string {
	paths := make([]string, 0, len(descs))
	for _, desc := range descs {
		if d, ok := desc.(*multiDescriptor); ok {
			paths = append(paths, d.k+"."+d.m.Extension())
		}
	}
	return paths
}

func TestMultiKeyWithExtension(t *testing.T) {
	td, err := ioutil.TempDir("", "test-multi")
	assert.Nil(t, err)

	hd, err := directory.OpenDir(filepath.Join(td, "test"))
	assert.Nil(t, err)

	m := NewMulti(hd, marshal.Toml)
	data := TestConfig{Key: "k", Value: "v"}
	key := "foo.toml"
	err = m.Marshal(Key(key), data)
	assert.Nil(t, err)

	var read TestConfig
	_, err = m.Unmarshal(Key(key), &read)
	assert.Nil(t, err)
	assert.Equal(t, data, read)

	files, err := hd.List()
	assert.Nil(t, err)
	assert.Len(t, files, 1)
	assert.Equal(t, "foo.toml.toml", files[0])
}
