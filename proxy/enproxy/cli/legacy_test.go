package main

import (
	"testing"

	"github.com/ccontavalli/enkit/proxy/httpp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyConfigUpgrade(t *testing.T) {
	legacy := legacyConfig{
		Mapping: []httpp.Mapping{{
			Name: "api",
			From: httpp.HostPath{Host: "example.com", Path: "/api/"},
			To:   "https://backend.example.com",
			Auth: httpp.MappingAuth("required"),
			Transform: &httpp.Transform{
				Maintain: true,
			},
		}},
		Domains: []string{"extra.example.com"},
		Tunnels: []string{"corp/*"},
	}

	upgraded, err := legacy.upgrade("relay.example.com:443")
	require.NoError(t, err)
	_, warnings, err := (&upgraded).Parse()

	assert.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, []string{"extra.example.com"}, upgraded.Domains)
	assert.Equal(t, []string{"corp/*"}, upgraded.Tunnels)
	if assert.Len(t, upgraded.Mapping, 2) {
		assert.Equal(t, "api", upgraded.Mapping[0].Name)
		assert.Equal(t, httpp.HostPath{Host: "example.com", Path: "/api/"}, upgraded.Mapping[0].From)
		assert.Equal(t, httpp.MappingAuth("required"), upgraded.Mapping[0].Auth)
		if assert.NotNil(t, upgraded.Mapping[0].Target.Proxy) {
			assert.Equal(t, "https://backend.example.com", upgraded.Mapping[0].Target.Proxy.To)
			if assert.NotNil(t, upgraded.Mapping[0].Target.Proxy.Transform) {
				assert.True(t, upgraded.Mapping[0].Target.Proxy.Transform.Maintain)
			}
		}
		assert.Equal(t, "nassh", upgraded.Mapping[1].Name)
		assert.Equal(t, httpp.HostPath{Host: "relay.example.com", Path: "/"}, upgraded.Mapping[1].From)
		if assert.NotNil(t, upgraded.Mapping[1].Target.Nassh) {
			assert.Equal(t, "relay.example.com:443", upgraded.Mapping[1].Target.Nassh.RelayHost)
		}
	}
}

func TestLegacyConfigUpgradeOmitsRedundantRelayHost(t *testing.T) {
	legacy := legacyConfig{
		Mapping: []httpp.Mapping{{
			From: httpp.HostPath{Host: "example.com", Path: "/"},
			To:   "https://backend.example.com",
		}},
		Tunnels: []string{"corp/*"},
	}

	upgraded, err := legacy.upgrade("relay.example.com")
	require.NoError(t, err)

	if assert.Len(t, upgraded.Mapping, 2) {
		assert.Equal(t, httpp.HostPath{Host: "relay.example.com", Path: "/"}, upgraded.Mapping[1].From)
		if assert.NotNil(t, upgraded.Mapping[1].Target.Nassh) {
			assert.Empty(t, upgraded.Mapping[1].Target.Nassh.RelayHost)
		}
	}
}

func TestLegacyConfigUpgradeRequiresRelayHostForTunnels(t *testing.T) {
	legacy := legacyConfig{
		Mapping: []httpp.Mapping{{
			From: httpp.HostPath{Host: "example.com", Path: "/"},
			To:   "https://backend.example.com",
		}},
		Tunnels: []string{"corp/*"},
	}

	_, err := legacy.upgrade("")
	assert.ErrorContains(t, err, "--host-port")
}

func TestLegacyConfigLooksLegacy(t *testing.T) {
	assert.False(t, (legacyConfig{}).looksLegacy())
	assert.False(t, legacyConfig{Mapping: []httpp.Mapping{{From: httpp.HostPath{Host: "example.com", Path: "/"}}}}.looksLegacy())
	assert.True(t, legacyConfig{Mapping: []httpp.Mapping{{To: "https://backend.example.com"}}}.looksLegacy())
}
