package factory

import (
	"os"
	"path/filepath"
	"testing"

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
		StoreType:     "directory",
		DirectoryPath: tmpDir,
	}

	opener, err := New(FromFlags(flags))
	assert.Nil(t, err)
	assert.NotNil(t, opener)

	store, err := opener("myapp", "testns")
	assert.Nil(t, err)
	assert.NotNil(t, store)

	// Verify we can write and read
	type TestConfig struct {
		Value string
	}
	err = store.Marshal("test-key", &TestConfig{Value: "foo"})
	assert.Nil(t, err)

	// Check file exists
	// The path logic in factory: Join(DirectoryPath, name, namespace...)
	// name="myapp", namespace="testns"
	expectedPath := filepath.Join(tmpDir, "myapp", "testns", "test-key.toml") // default format toml
	_, err = os.Stat(expectedPath)
	assert.Nil(t, err)

	var loaded TestConfig
	_, err = store.Unmarshal("test-key", &loaded)
	assert.Nil(t, err)
	assert.Equal(t, "foo", loaded.Value)
}

func TestNewUnknownStore(t *testing.T) {
	flags := &Flags{
		StoreType: "unknown-type",
	}
	_, err := New(FromFlags(flags))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown config store type")
}

func TestNewSQLiteStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-factory-sqlite-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "config.db")
	flags := &Flags{
		StoreType: "sqlite",
		SQLite: &sqlite.Flags{
			Path: dbPath,
		},
	}

	opener, err := New(FromFlags(flags))
	assert.NoError(t, err)
	assert.NotNil(t, opener)

	store, err := opener("myapp", "testns")
	assert.NoError(t, err)
	assert.NotNil(t, store)

	type TestConfig struct {
		Value string
	}
	err = store.Marshal("test-key", &TestConfig{Value: "bar"})
	assert.NoError(t, err)

	var loaded TestConfig
	_, err = store.Unmarshal("test-key", &loaded)
	assert.NoError(t, err)
	assert.Equal(t, "bar", loaded.Value)
}
