package factory

import (
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/cryptstore"
	"github.com/ccontavalli/enkit/lib/config/directory"
	"github.com/ccontavalli/enkit/lib/config/sqlite"
	"github.com/stretchr/testify/assert"
)

func testRng() *rand.Rand {
	return rand.New(rand.NewSource(1234))
}

func TestDefaultFlags(t *testing.T) {
	flags := DefaultFlags()
	assert.NotEmpty(t, flags.StoreType)
	assert.NotNil(t, flags.Crypt)
	// On non-GCE environment (CI/local), likely "directory".
}

func TestDefaultConfigFileFlags(t *testing.T) {
	flags := DefaultConfigFileFlags()
	assert.Equal(t, "directory:multi", flags.StoreType)
	if assert.NotNil(t, flags.Directory) {
		assert.Equal(t, "/", flags.Directory.Path)
	}
	assert.NotNil(t, flags.Crypt)
}

func TestDefaultAppConfigFlags(t *testing.T) {
	flags := DefaultAppConfigFlags()
	assert.Equal(t, "directory:multi", flags.StoreType)
	if assert.NotNil(t, flags.Directory) {
		assert.Equal(t, "", flags.Directory.Path)
	}
	assert.NotNil(t, flags.Crypt)
}

func TestNewDirectoryStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-factory-test")
	assert.Nil(t, err)
	defer os.RemoveAll(tmpDir)

	flags := &Flags{
		StoreType: "directory:toml",
		Directory: &directory.Flags{Path: tmpDir},
	}

	workspace, err := NewStore(testRng(), FromFlags(flags))
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
	_, err := NewStore(testRng(), FromFlags(flags))
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

	workspace, err := NewStore(testRng(), FromFlags(flags))
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

	workspace, err := NewStore(testRng(), FromFlags(flags))
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

	workspace, err := NewStore(testRng(), FromFlags(flags))
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

	explorer, err := NewStore(testRng(), FromFlags(flags))
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

func TestNewDirectoryStoreWithCryptoPrefix(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-factory-crypto-store-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	flags := &Flags{
		StoreType: "crypto:directory:json",
		Directory: &directory.Flags{Path: tmpDir},
		Crypt: &cryptstore.Flags{
			ValueEncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		},
	}

	workspace, err := NewStore(testRng(), FromFlags(flags))
	assert.NoError(t, err)
	assert.NotNil(t, workspace)

	store, err := workspace.Open("myapp", "testns")
	assert.NoError(t, err)
	assert.NotNil(t, store)

	type TestConfig struct {
		Value string `json:"value"`
	}
	assert.NoError(t, store.Marshal(config.Key("test-key"), &TestConfig{Value: "secret"}))

	rawPath := filepath.Join(tmpDir, "myapp", "testns", "test-key.json")
	rawBytes, err := os.ReadFile(rawPath)
	assert.NoError(t, err)
	assert.NotContains(t, string(rawBytes), "secret")

	var loaded TestConfig
	_, err = store.Unmarshal(config.Key("test-key"), &loaded)
	assert.NoError(t, err)
	assert.Equal(t, "secret", loaded.Value)
}

func TestNewDirectoryLoaderWithCryptoPrefixAndDeterministicKeys(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-factory-crypto-loader-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	flags := &Flags{
		StoreType: "crypto:directory",
		Directory: &directory.Flags{Path: tmpDir},
		Crypt: &cryptstore.Flags{
			KeyMode:            cryptstore.KeyModeDeterministicToken,
			KeyEncryptionKey:   []byte("abcdef0123456789abcdef0123456789"),
			KeyEncryptionNonce: []byte("123456789012"),
			ValueEncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		},
	}

	workspace, err := NewLoader(testRng(), FromFlags(flags))
	assert.NoError(t, err)
	assert.NotNil(t, workspace)

	loader, err := workspace.Open("myapp", "testns")
	assert.NoError(t, err)
	assert.NotNil(t, loader)

	assert.NoError(t, loader.Write("test-key", []byte("secret")))

	dirEntries, err := os.ReadDir(filepath.Join(tmpDir, "myapp", "testns"))
	assert.NoError(t, err)
	if assert.Len(t, dirEntries, 1) {
		assert.NotEqual(t, "test-key", dirEntries[0].Name())
		assert.False(t, strings.Contains(dirEntries[0].Name(), "test-key"))
	}

	keys, err := loader.List()
	assert.NoError(t, err)
	assert.Equal(t, []string{"test-key"}, keys)

	got, err := loader.Read("test-key")
	assert.NoError(t, err)
	assert.Equal(t, []byte("secret"), got)
}

func TestNewStoreRejectsCryptoDatastore(t *testing.T) {
	flags := &Flags{
		StoreType: "crypto:datastore",
		Crypt: &cryptstore.Flags{
			ValueEncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		},
	}

	_, err := NewStore(testRng(), FromFlags(flags))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "crypto wrapper requires a loader-backed config store")
}

func TestNewStoreRejectsCryptoMemoryRaw(t *testing.T) {
	flags := &Flags{
		StoreType: "crypto:memory:raw",
		Crypt: &cryptstore.Flags{
			ValueEncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		},
	}

	_, err := NewStore(testRng(), FromFlags(flags))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "crypto wrapper does not support raw config stores")
}

func TestNewStoreRejectsIncompleteCryptoConfiguration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-factory-crypto-config-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	flags := &Flags{
		StoreType: "crypto:directory:json",
		Directory: &directory.Flags{Path: tmpDir},
		Crypt:     cryptstore.DefaultFlags(),
	}

	_, err = NewStore(testRng(), FromFlags(flags))
	assert.EqualError(t, err, "value encryption key is required")
}
