package main

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/ccontavalli/enkit/lib/srand"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runRoot(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	root := NewRoot(rand.New(srand.Source))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args)

	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func writeTempConfig(t *testing.T, contents string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o660))
	return path
}

func TestConfigCheckAcceptsCurrentConfig(t *testing.T) {
	path := writeTempConfig(t, `
mapping:
  - from:
      host: example.com
      path: /
    target:
      proxy:
        to: https://backend.example.com
`)

	stdout, stderr, err := runRoot(t, "config", "check", "--config", path)
	require.NoError(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestConfigPrintUpgradesLegacyConfig(t *testing.T) {
	path := writeTempConfig(t, `
mapping:
  - from:
      host: example.com
      path: /
    to: https://backend.example.com
tunnels:
  - corp/*
`)

	stdout, stderr, err := runRoot(t, "--host-port", "relay.example.com:443", "config", "print", "--config", path, "--format", "json")
	require.NoError(t, err)
	assert.Contains(t, stderr, "legacy enproxy format")

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(stdout), &decoded))
	mapping := decoded["Mapping"].([]interface{})
	target := mapping[0].(map[string]interface{})["Target"].(map[string]interface{})
	proxy := target["Proxy"].(map[string]interface{})
	assert.Equal(t, "https://backend.example.com", proxy["To"])
	nasshMapping := mapping[1].(map[string]interface{})
	nasshFrom := nasshMapping["From"].(map[string]interface{})
	assert.Equal(t, "relay.example.com", nasshFrom["Host"])
	nasshTarget := nasshMapping["Target"].(map[string]interface{})["Nassh"].(map[string]interface{})
	assert.Equal(t, "relay.example.com:443", nasshTarget["RelayHost"])
}

func TestConfigUpdateRewritesLegacyConfig(t *testing.T) {
	path := writeTempConfig(t, `
mapping:
  - from:
      host: example.com
      path: /
    to: https://backend.example.com
tunnels:
  - corp/*
`)

	_, _, err := runRoot(t, "--host-port", "relay.example.com:443", "config", "update", "--config", path)
	require.NoError(t, err)

	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(updated), "target:")
	assert.Contains(t, string(updated), "proxy:")
	assert.Contains(t, string(updated), "nassh:")
	assert.Contains(t, string(updated), "relay.example.com:443")

	_, stderr, err := runRoot(t, "config", "check", "--config", path)
	require.NoError(t, err)
	assert.NotContains(t, stderr, "legacy enproxy format")
}
