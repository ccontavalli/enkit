package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/directory"
	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/ccontavalli/enkit/lib/config/memory"
	"github.com/stretchr/testify/assert"
)

func testTempDir(t *testing.T) string {
	t.Helper()

	base := os.Getenv("TEST_TMPDIR")
	if base == "" {
		base = "/var/tmp"
	}
	dir, err := os.MkdirTemp(base, "config-path-root")
	if !assert.NoError(t, err) {
		return ""
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}

func TestDefaultParsePath(t *testing.T) {
	resolved, err := config.DefaultParsePath("enproxy/runtime/config")
	assert.NoError(t, err)
	assert.Equal(t, "enproxy", resolved.AppName)
	assert.Equal(t, []string{"runtime"}, resolved.Namespaces)
	assert.Equal(t, "config", resolved.Descriptor.Key())

	_, err = config.DefaultParsePath("config")
	assert.Error(t, err)

	_, err = config.DefaultParsePath("/etc/enproxy.yaml")
	assert.Error(t, err)
}

func TestDefaultParsePathDecodesEscapedKey(t *testing.T) {
	resolved, err := config.DefaultParsePath("enproxy/runtime/config%2Fprod")
	assert.NoError(t, err)
	assert.Equal(t, "enproxy", resolved.AppName)
	assert.Equal(t, []string{"runtime"}, resolved.Namespaces)
	assert.Equal(t, "config/prod", resolved.Descriptor.Key())
}

func TestResolvePathNativeUsesWorkspaceParser(t *testing.T) {
	ws := config.NewSimple(memory.NewMarshal(), marshal.Json)

	resolved, err := config.ResolvePathNative(ws, "enproxy/runtime/config")
	assert.NoError(t, err)
	assert.Equal(t, "enproxy", resolved.AppName)
	assert.Equal(t, []string{"runtime"}, resolved.Namespaces)
	assert.Equal(t, "config", resolved.Descriptor.Key())
}

func TestResolvePathWithinStore(t *testing.T) {
	root := config.StoreRoot{
		AppName:    "enproxy",
		Namespaces: []string{"runtime"},
	}

	resolved, err := config.ResolvePathWithinStore(root, "admin/config.yaml")
	assert.NoError(t, err)
	assert.Equal(t, "enproxy", resolved.AppName)
	assert.Equal(t, []string{"runtime", "admin"}, resolved.Namespaces)
	assert.Equal(t, "config", resolved.Descriptor.Key())

	hinted, ok := resolved.Descriptor.(config.RequestedFormatDescriptor)
	if assert.True(t, ok) {
		assert.Equal(t, "yaml", hinted.Format())
	}
}

func TestResolvePathWithinStoreDecodesEscapedKeyOnly(t *testing.T) {
	root := config.StoreRoot{
		AppName:    "enproxy",
		Namespaces: []string{"runtime"},
	}

	resolved, err := config.ResolvePathWithinStore(root, "admin/config%2Fprod.yaml")
	assert.NoError(t, err)
	assert.Equal(t, "enproxy", resolved.AppName)
	assert.Equal(t, []string{"runtime", "admin"}, resolved.Namespaces)
	assert.Equal(t, "config/prod", resolved.Descriptor.Key())

	hinted, ok := resolved.Descriptor.(config.RequestedFormatDescriptor)
	if assert.True(t, ok) {
		assert.Equal(t, "yaml", hinted.Format())
	}
}

func TestResolvePathWithinStoreRejectsAbsoluteAndEscape(t *testing.T) {
	root := config.StoreRoot{AppName: "enproxy", Namespaces: []string{"runtime"}}

	_, err := config.ResolvePathWithinStore(root, "/etc/enproxy.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "relative")

	_, err = config.ResolvePathWithinStore(root, "../enproxy.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "escapes")

	_, err = config.ResolvePathWithinStore(root, "~/enproxy.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "home-relative")
}

func TestBindUsesDescriptor(t *testing.T) {
	loader := memory.Open()
	store := config.OpenMulti(loader, marshal.Json, marshal.Toml)
	binding := config.Bind(store, config.FormatKey("quote", marshal.Json))

	type payload struct {
		Message string `json:"message"`
	}

	assert.NoError(t, binding.Marshal(payload{Message: "hello"}))

	data, err := loader.Read("quote.json")
	assert.NoError(t, err)
	assert.Contains(t, string(data), "\"message\":\"hello\"")

	_, err = loader.Read("quote.toml")
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestSimpleWorkspaceParsePathUsesDefaultParser(t *testing.T) {
	ws := config.NewSimple(memory.NewMarshal(), marshal.Json)

	resolved, err := ws.ParsePath("enproxy/config")
	assert.NoError(t, err)
	assert.Equal(t, "enproxy", resolved.AppName)
	assert.Empty(t, resolved.Namespaces)
	assert.Equal(t, "config", resolved.Descriptor.Key())
}

func TestDirectoryLoaderParsePathProvidesFormatHint(t *testing.T) {
	root := testTempDir(t)
	if root == "" {
		return
	}

	ws := directory.New(root)
	parsed, err := ws.ParsePath(filepath.Join(root, "etc", "enproxy.yaml"))
	assert.NoError(t, err)
	assert.Equal(t, "/", parsed.AppName)
	assert.Equal(t, []string{"etc"}, parsed.Namespaces)
	assert.Equal(t, "enproxy", parsed.Descriptor.Key())

	hinted, ok := parsed.Descriptor.(config.RequestedFormatDescriptor)
	if assert.True(t, ok) {
		assert.Equal(t, "yaml", hinted.Format())
	}
}

func TestDirectoryLoaderParsePathDecodesEscapedKey(t *testing.T) {
	root := testTempDir(t)
	if root == "" {
		return
	}

	ws := directory.New(root)
	parsed, err := ws.ParsePath(filepath.Join(root, "etc", "config%2Fprod.yaml"))
	assert.NoError(t, err)
	assert.Equal(t, "/", parsed.AppName)
	assert.Equal(t, []string{"etc"}, parsed.Namespaces)
	assert.Equal(t, "config/prod", parsed.Descriptor.Key())
}

func TestDirectoryLoaderParsePathAcceptsRelativePath(t *testing.T) {
	root := testTempDir(t)
	if root == "" {
		return
	}

	t.Chdir(root)

	ws := directory.New(root)
	parsed, err := ws.ParsePath(filepath.Join("etc", "enproxy.yaml"))
	assert.NoError(t, err)
	assert.Equal(t, "/", parsed.AppName)
	assert.Equal(t, []string{"etc"}, parsed.Namespaces)
	assert.Equal(t, "enproxy", parsed.Descriptor.Key())

	hinted, ok := parsed.Descriptor.(config.RequestedFormatDescriptor)
	if assert.True(t, ok) {
		assert.Equal(t, "yaml", hinted.Format())
	}
}

func TestDirectoryMultiWorkspaceParsePathAndBind(t *testing.T) {
	root := testTempDir(t)
	if root == "" {
		return
	}

	ws := config.NewMulti(directory.New(root), marshal.Known...)
	target := filepath.Join(root, "etc", "enproxy.yaml")

	resolved, err := ws.ParsePath(target)
	assert.NoError(t, err)
	assert.Equal(t, "/", resolved.AppName)
	assert.Equal(t, []string{"etc"}, resolved.Namespaces)
	assert.Equal(t, "enproxy", resolved.Descriptor.Key())

	store, err := resolved.OpenStore(ws)
	assert.NoError(t, err)
	defer store.Close()

	binding := resolved.Bind(store)
	type payload struct {
		Port int `yaml:"port"`
	}
	assert.NoError(t, binding.Marshal(payload{Port: 443}))

	data, err := os.ReadFile(target)
	assert.NoError(t, err)
	assert.Contains(t, string(data), "port: 443")
}

func TestDirectorySimpleWorkspaceParsePathRejectsExtensionMismatch(t *testing.T) {
	root := testTempDir(t)
	if root == "" {
		return
	}

	ws := config.NewSimple(directory.New(root), marshal.Toml)

	_, err := ws.ParsePath(filepath.Join(root, "etc", "enproxy.json"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), ".toml")
}

func TestDirectorySimpleWorkspaceParsePathRejectsUppercaseExtension(t *testing.T) {
	root := testTempDir(t)
	if root == "" {
		return
	}

	ws := config.NewSimple(directory.New(root), marshal.Yaml)

	_, err := ws.ParsePath(filepath.Join(root, "etc", "enproxy.YAML"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), ".yaml")
	assert.Contains(t, err.Error(), ".YAML")
}

func TestDirectorySimpleWorkspaceParsePathWithoutExtensionUsesStoreFormat(t *testing.T) {
	root := testTempDir(t)
	if root == "" {
		return
	}

	ws := config.NewSimple(directory.New(root), marshal.Toml)
	target := filepath.Join(root, "etc", "enproxy")

	parsed, err := ws.ParsePath(target)
	assert.NoError(t, err)
	assert.Equal(t, "/", parsed.AppName)
	assert.Equal(t, []string{"etc"}, parsed.Namespaces)
	assert.Equal(t, "enproxy", parsed.Descriptor.Key())

	store, err := parsed.OpenStore(ws)
	assert.NoError(t, err)
	defer store.Close()

	binding := parsed.Bind(store)
	type payload struct {
		Port int `toml:"port"`
	}
	assert.NoError(t, binding.Marshal(payload{Port: 443}))

	data, err := os.ReadFile(target + ".toml")
	assert.NoError(t, err)
	assert.Contains(t, string(data), "port = 443")
}

func TestResolvePathWithinStoreSimpleRejectsMismatchedFormatHint(t *testing.T) {
	ws := config.NewSimple(memory.NewMarshal(), marshal.Toml)
	root := config.StoreRoot{AppName: "enproxy"}
	parsed, err := config.ResolvePathWithinStore(root, "config.json")
	assert.NoError(t, err)

	store, err := parsed.OpenStore(ws)
	assert.NoError(t, err)
	defer store.Close()

	type payload struct {
		Port int `toml:"port"`
	}
	err = parsed.Bind(store).Marshal(payload{Port: 443})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), ".toml")
}

func TestResolvePathWithinStoreSimpleRejectsUppercaseExtension(t *testing.T) {
	ws := config.NewSimple(memory.NewMarshal(), marshal.Yaml)
	root := config.StoreRoot{AppName: "enproxy"}
	parsed, err := config.ResolvePathWithinStore(root, "config.YAML")
	assert.NoError(t, err)

	store, err := parsed.OpenStore(ws)
	assert.NoError(t, err)
	defer store.Close()

	type payload struct {
		Port int `yaml:"port"`
	}
	err = parsed.Bind(store).Marshal(payload{Port: 443})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), ".yaml")
	assert.Contains(t, err.Error(), ".YAML")
}

func TestResolvePathWithinStoreMultiUsesFormatHint(t *testing.T) {
	ws := config.NewMulti(memory.NewMarshal(), marshal.Json, marshal.Yaml)
	root := config.StoreRoot{AppName: "enproxy"}
	parsed, err := config.ResolvePathWithinStore(root, "config.yaml")
	assert.NoError(t, err)

	store, err := parsed.OpenStore(ws)
	assert.NoError(t, err)
	defer store.Close()

	type payload struct {
		Port int `yaml:"port"`
	}
	assert.NoError(t, parsed.Bind(store).Marshal(payload{Port: 443}))

	reopened, err := ws.Open("enproxy")
	assert.NoError(t, err)
	defer reopened.Close()

	descs, err := reopened.List()
	assert.NoError(t, err)
	if assert.Len(t, descs, 1) {
		assert.Equal(t, "config", descs[0].Key())
	}

	var out payload
	_, err = reopened.Unmarshal(config.FormatKey("config", marshal.Yaml), &out)
	assert.NoError(t, err)
	assert.Equal(t, 443, out.Port)
}

func TestResolvePathWithinStoreDirectoryKeepsPathsUnderSelectedStore(t *testing.T) {
	rootDir := testTempDir(t)
	if rootDir == "" {
		return
	}

	ws := config.NewMulti(directory.New(rootDir), marshal.Known...)
	root := config.StoreRoot{
		AppName:    "/",
		Namespaces: []string{"etc"},
	}
	parsed, err := config.ResolvePathWithinStore(root, "enproxy.yaml")
	assert.NoError(t, err)

	store, err := parsed.OpenStore(ws)
	assert.NoError(t, err)
	defer store.Close()

	type payload struct {
		Port int `yaml:"port"`
	}
	assert.NoError(t, parsed.Bind(store).Marshal(payload{Port: 443}))

	data, err := os.ReadFile(filepath.Join(rootDir, "etc", "enproxy.yaml"))
	assert.NoError(t, err)
	assert.Contains(t, string(data), "port: 443")
}

func TestDirectoryWorkspaceParsePathRejectsPathOutsideRoot(t *testing.T) {
	root := testTempDir(t)
	if root == "" {
		return
	}

	outside := testTempDir(t)
	if outside == "" {
		return
	}

	ws := config.NewMulti(directory.New(root), marshal.Known...)
	_, err := ws.ParsePath(filepath.Join(outside, "enproxy.yaml"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "outside directory root")
}

func ExampleResolvePathNative() {
	ws := config.NewSimple(memory.NewMarshal(), marshal.Json)
	parsed, err := config.ResolvePathNative(ws, "enproxy/runtime/config")
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(parsed.AppName)
	fmt.Println(strings.Join(parsed.Namespaces, "/"))
	fmt.Println(parsed.Descriptor.Key())
	// Output:
	// enproxy
	// runtime
	// config
}

func ExampleResolvePathWithinStore() {
	root := config.StoreRoot{
		AppName:    "enproxy",
		Namespaces: []string{"runtime"},
	}
	parsed, err := config.ResolvePathWithinStore(root, "admin/config.yaml")
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(parsed.AppName)
	fmt.Println(strings.Join(parsed.Namespaces, "/"))
	fmt.Println(parsed.Descriptor.Key())
	fmt.Println(parsed.Descriptor.(config.RequestedFormatDescriptor).Format())
	// Output:
	// enproxy
	// runtime/admin
	// config
	// yaml
}
