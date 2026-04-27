package main

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/ccontavalli/enkit/proxy/enproxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runRoot(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	root := NewRoot(rand.New(rand.NewSource(1)))
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

func decodePrintedJSONConfig(t *testing.T, output string) enproxy.Config {
	t.Helper()

	var cfg enproxy.Config
	require.NoError(t, marshal.UnmarshalDefault("config.json", []byte(output), marshal.Json, &cfg))
	return cfg
}

func TestConfigCheckUsesParsedSnapshotForLoadability(t *testing.T) {
	path := writeTempConfig(t, `
mapping:
  - from:
      host: example.com
      path: /
    auth: public
    target:
      proxy:
        to: https://backend.example.com
`)

	flags := enproxy.DefaultFlags()
	flags.ConfigPath = path
	rng := rand.New(rand.NewSource(1))
	normalizer, err := enproxy.NewConfigNormalizer("", flags.DisabledAuthentication, flags.UnsafeIgnoreAuthentication)
	require.NoError(t, err)

	workspace, store, binding, _, err := enproxy.OpenConfigBinding(rng, flags)
	require.NoError(t, err)
	defer workspace.Close()
	defer store.Close()

	current, warnings, err := currentConfigOrRejectLegacy(binding, normalizer)
	require.NoError(t, err)
	assert.Empty(t, warnings)

	require.NoError(t, os.WriteFile(path, []byte(`
mapping:
  - from:
      path: /
    target:
      nassh: {}
tunnels:
  - corp/*
`), 0o660))

	require.NoError(t, loadableCurrentConfig(rand.New(rand.NewSource(2)), flags, current))
}

func TestConfigCheckAcceptsPublicOnlyCurrentConfigWithSharedOAuthDefaults(t *testing.T) {
	path := writeTempConfig(t, `
mapping:
  - from:
      host: example.com
      path: /
    auth: public
    target:
      proxy:
        to: https://backend.example.com
`)

	stdout, stderr, err := runRoot(t,
		"--without-authentication",
		"--auth-url", "https://auth.example.test/login",
		"config", "check",
		"--config", path,
	)
	require.NoError(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestConfigCheckRejectsProtectedCurrentConfigWhenAuthenticationDisabled(t *testing.T) {
	path := writeTempConfig(t, `
mapping:
  - from:
      host: example.com
      path: /
    auth: required
    target:
      proxy:
        to: https://backend.example.com
`)

	stdout, stderr, err := runRoot(t,
		"--without-authentication",
		"config", "check",
		"--config", path,
	)
	require.Error(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
	assert.Contains(t, err.Error(), "selected config is not loadable")
	assert.Contains(t, err.Error(), "--unsafe-ignore-authentication")
}

func TestConfigCheckAllowsProtectedCurrentConfigWhenUnsafeAuthenticationIgnored(t *testing.T) {
	path := writeTempConfig(t, `
mapping:
  - from:
      host: example.com
      path: /
    auth: required
    target:
      proxy:
        to: https://backend.example.com
`)

	stdout, stderr, err := runRoot(t,
		"--without-authentication",
		"--unsafe-ignore-authentication",
		"config", "check",
		"--config", path,
	)
	require.NoError(t, err)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "being treated as public")
	assert.Contains(t, stderr, "--unsafe-ignore-authentication")
}

func TestConfigCheckRejectsUnsafeIgnoreAuthenticationWithoutGlobalDisable(t *testing.T) {
	path := writeTempConfig(t, `
mapping:
  - from:
      host: example.com
      path: /
    auth: public
    target:
      proxy:
        to: https://backend.example.com
`)

	stdout, stderr, err := runRoot(t,
		"--unsafe-ignore-authentication",
		"config", "check",
		"--config", path,
	)
	require.Error(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
	assert.Contains(t, err.Error(), "--unsafe-ignore-authentication requires --without-authentication")
}

func TestConfigCheckRejectsLegacyConfig(t *testing.T) {
	path := writeTempConfig(t, `
mapping:
  - from:
      host: example.com
      path: /
    to: https://backend.example.com
`)

	stdout, stderr, err := runRoot(t, "config", "check", "--config", path)
	require.Error(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
	assert.Contains(t, err.Error(), "legacy enproxy format")
}

func TestConfigCheckRejectsCurrentConfigThatDoesNotLoad(t *testing.T) {
	path := writeTempConfig(t, `
mapping:
  - from:
      path: /
    target:
      nassh: {}
tunnels:
  - corp/*
`)

	stdout, stderr, err := runRoot(t,
		"--without-authentication",
		"config", "check",
		"--config", path,
	)
	require.Error(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
	assert.Contains(t, err.Error(), "selected config is not loadable")
	assert.Contains(t, err.Error(), "nassh target is missing a relay host")
}

func TestConfigCheckAcceptsCurrentConfigWithRelayHostDefault(t *testing.T) {
	path := writeTempConfig(t, `
mapping:
  - from:
      path: /
    target:
      nassh: {}
tunnels:
  - corp/*
`)

	stdout, stderr, err := runRoot(t,
		"--without-authentication",
		"--host-port", "relay.example.com:443",
		"config", "check",
		"--config", path,
	)
	require.NoError(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestConfigPrintRejectsLegacyConfig(t *testing.T) {
	path := writeTempConfig(t, `
mapping:
  - from:
      host: example.com
      path: /
    to: https://backend.example.com
`)

	stdout, stderr, err := runRoot(t, "config", "print", "--config", path)
	require.Error(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
	assert.Contains(t, err.Error(), "legacy enproxy format")
}

func TestConfigPrintShowsEffectiveCurrentConfig(t *testing.T) {
	path := writeTempConfig(t, `
mapping:
  - from:
      path: /
    target:
      nassh: {}
tunnels:
  - corp/*
`)

	stdout, stderr, err := runRoot(t,
		"--host-port", "relay.example.com:443",
		"config", "print",
		"--config", path,
		"--format", "json",
	)
	require.NoError(t, err)
	assert.Empty(t, stderr)

	cfg := decodePrintedJSONConfig(t, stdout)
	if assert.Len(t, cfg.Mapping, 1) && assert.NotNil(t, cfg.Mapping[0].Target.Nassh) {
		assert.Equal(t, "relay.example.com:443", cfg.Mapping[0].Target.Nassh.RelayHost)
	}
}

func TestConfigPrintRejectsProtectedCurrentConfigWhenAuthenticationDisabled(t *testing.T) {
	path := writeTempConfig(t, `
mapping:
  - from:
      host: example.com
      path: /
    auth: required
    target:
      proxy:
        to: https://backend.example.com
`)

	stdout, stderr, err := runRoot(t,
		"--without-authentication",
		"config", "print",
		"--config", path,
	)
	require.Error(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
	assert.Contains(t, err.Error(), "--unsafe-ignore-authentication")
}

func TestConfigPrintDowngradesProtectedCurrentConfigWhenUnsafeAuthenticationIgnored(t *testing.T) {
	path := writeTempConfig(t, `
mapping:
  - from:
      host: example.com
      path: /
    auth: required
    target:
      proxy:
        to: https://backend.example.com
`)

	stdout, stderr, err := runRoot(t,
		"--without-authentication",
		"--unsafe-ignore-authentication",
		"config", "print",
		"--config", path,
		"--format", "json",
	)
	require.NoError(t, err)
	assert.Contains(t, stderr, "being treated as public")
	assert.Contains(t, stderr, "--unsafe-ignore-authentication")

	cfg := decodePrintedJSONConfig(t, stdout)
	if assert.Len(t, cfg.Mapping, 1) {
		assert.Equal(t, "public", string(cfg.Mapping[0].Auth))
	}
}

func TestConfigUpdateRewritesLegacyConfig(t *testing.T) {
	path := writeTempConfig(t, `
mapping:
  - from:
      host: example.com
      path: /
    auth: public
    to: https://backend.example.com
tunnels:
  - corp/*
`)

	_, _, err := runRoot(t,
		"--host-port", "relay.example.com:443",
		"config", "update",
		"--config", path,
	)
	require.NoError(t, err)

	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(updated), "target:")
	assert.Contains(t, string(updated), "proxy:")
	assert.Contains(t, string(updated), "nassh:")
	assert.Contains(t, string(updated), "relay.example.com:443")

	stdout, stderr, err := runRoot(t,
		"--without-authentication",
		"config", "check",
		"--config", path,
	)
	require.NoError(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)

	stdout, stderr, err = runRoot(t,
		"config", "print",
		"--config", path,
		"--format", "json",
	)
	require.NoError(t, err)
	assert.Empty(t, stderr)
	cfg := decodePrintedJSONConfig(t, stdout)
	if assert.Len(t, cfg.Mapping, 2) && assert.NotNil(t, cfg.Mapping[1].Target.Nassh) {
		assert.Equal(t, "relay.example.com:443", cfg.Mapping[1].Target.Nassh.RelayHost)
	}
}
