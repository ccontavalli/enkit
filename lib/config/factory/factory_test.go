package factory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/directory"
	"github.com/ccontavalli/enkit/lib/config/sqlite"
	"github.com/stretchr/testify/assert"
)

func TestDefaultFlags(t *testing.T) {
	flags := DefaultFlags()
	assert.NotEmpty(t, flags.StoreType)
	// On non-GCE environment (CI/local), likely "directory".
}

func TestNewDirectoryStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-factory-test")
	assert.Nil(t, err)
	defer os.RemoveAll(tmpDir)

	flags := &Flags{
		StoreType: "directory:toml",
		Directory: &directory.Flags{Path: tmpDir},
	}

	workspace, err := NewStore(FromFlags(flags))
	assert.Nil(t, err)
	assert.NotNil(t, workspace)

	store, err := workspace.Open("myapp", "testns")
	assert.Nil(t, err)
	assert.NotNil(t, store)

	// Verify we can write and read
	type TestConfig struct {
		Value string
	}
	err = store.Marshal(config.Key("test-key"), &TestConfig{Value: "foo"})
	assert.Nil(t, err)

	// Check file exists
	// The path logic in factory: Join(DirectoryPath, name, namespace...)
	// name="myapp", namespace="testns"
	expectedPath := filepath.Join(tmpDir, "myapp", "testns", "test-key.toml")
	_, err = os.Stat(expectedPath)
	assert.Nil(t, err)

	var loaded TestConfig
	_, err = store.Unmarshal(config.Key("test-key"), &loaded)
	assert.Nil(t, err)
	assert.Equal(t, "foo", loaded.Value)
}

func TestNewUnknownStore(t *testing.T) {
	flags := &Flags{
		StoreType: "unknown-type",
	}
	_, err := NewStore(FromFlags(flags))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown config store type")
}

func TestNewSQLiteStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-factory-sqlite-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "config.db")
	flags := &Flags{
		StoreType: "sqlite:json",
		SQLite: &sqlite.Flags{
			Path: dbPath,
		},
	}

	workspace, err := NewStore(FromFlags(flags))
	assert.NoError(t, err)
	assert.NotNil(t, workspace)

	store, err := workspace.Open("myapp", "testns")
	assert.NoError(t, err)
	assert.NotNil(t, store)

	type TestConfig struct {
		Value string
	}
	err = store.Marshal(config.Key("test-key"), &TestConfig{Value: "bar"})
	assert.NoError(t, err)

	var loaded TestConfig
	_, err = store.Unmarshal(config.Key("test-key"), &loaded)
	assert.NoError(t, err)
	assert.Equal(t, "bar", loaded.Value)
}
func TestDirectorySimpleWithFormat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-factory-test")
	assert.Nil(t, err)
	defer os.RemoveAll(tmpDir)

	flags := &Flags{
		StoreType: "directory:json",
		Directory: &directory.Flags{Path: tmpDir},
	}

	workspace, err := NewStore(FromFlags(flags))
	assert.Nil(t, err)
	assert.NotNil(t, workspace)

	store, err := workspace.Open("myapp", "testns")
	assert.Nil(t, err)
	assert.NotNil(t, store)

	type TestConfig struct {
		Value string
	}
	err = store.Marshal(config.Key("test-key"), &TestConfig{Value: "baz"})
	assert.Nil(t, err)

	var loaded TestConfig
	_, err = store.Unmarshal(config.Key("test-key"), &loaded)
	assert.Nil(t, err)
	assert.Equal(t, "baz", loaded.Value)
}

func TestNewMemoryRawStore(t *testing.T) {
	flags := &Flags{
		StoreType: "memory:raw",
	}

	workspace, err := NewStore(FromFlags(flags))
	assert.NoError(t, err)
	assert.NotNil(t, workspace)

	store, err := workspace.Open("myapp", "testns")
	assert.NoError(t, err)
	assert.NotNil(t, store)

	type TestConfig struct {
		Value string
	}
	err = store.Marshal(config.Key("test-key"), &TestConfig{Value: "mem"})
	assert.NoError(t, err)

	var loaded TestConfig
	_, err = store.Unmarshal(config.Key("test-key"), &loaded)
	assert.NoError(t, err)
	assert.Equal(t, "mem", loaded.Value)
}

func TestNewDirectoryExplorer(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-factory-explorer-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	flags := &Flags{
		StoreType: "directory:toml",
		Directory: &directory.Flags{Path: tmpDir},
	}

	explorer, err := NewStore(FromFlags(flags))
	assert.NoError(t, err)
	assert.NotNil(t, explorer)

	store, err := explorer.Open("myapp", "ns1")
	assert.NoError(t, err)
	assert.NotNil(t, store)
	type TestConfig struct {
		Value string
	}
	assert.NoError(t, store.Marshal(config.Key("k"), TestConfig{Value: "v"}))
	assert.NoError(t, store.Close())

	expl, err := explorer.Explore("myapp")
	assert.NoError(t, err)
	descs, err := expl.List()
	assert.NoError(t, err)
	if assert.Len(t, descs, 1) {
		assert.Equal(t, "ns1", descs[0].Key())
	}
	assert.NoError(t, expl.Close())
}
