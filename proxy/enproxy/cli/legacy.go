package main

import (
	"strings"

	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/ccontavalli/enkit/lib/khttp"
	"github.com/ccontavalli/enkit/proxy/enproxy"
	"github.com/ccontavalli/enkit/proxy/httpp"
)

// legacyConfig is the pre-module enproxy config format.
type legacyConfig struct {
	Mapping []httpp.Mapping
	Domains []string
	Tunnels []string
}

func (cfg legacyConfig) looksLegacy() bool {
	if len(cfg.Mapping) == 0 {
		return false
	}

	for _, mapping := range cfg.Mapping {
		if strings.TrimSpace(mapping.To) != "" {
			return true
		}
		if mapping.Transform != nil {
			return true
		}
	}
	return false
}

func (cfg legacyConfig) upgrade(relayHost string) (enproxy.Config, error) {
	upgraded := enproxy.Config{
		Mapping: make([]enproxy.Mapping, 0, len(cfg.Mapping)+1),
		Domains: append([]string(nil), cfg.Domains...),
		Tunnels: append([]string(nil), cfg.Tunnels...),
	}

	for _, mapping := range cfg.Mapping {
		upgraded.Mapping = append(upgraded.Mapping, enproxy.Mapping{
			Name:   mapping.Name,
			From:   mapping.From,
			Auth:   mapping.Auth,
			Target: enproxy.Target{Proxy: &enproxy.ProxyTarget{To: mapping.To, Transform: mapping.Transform}},
		})
	}

	if len(cfg.Tunnels) > 0 {
		mapping, err := legacyNasshMapping(relayHost)
		if err != nil {
			return enproxy.Config{}, err
		}
		upgraded.Mapping = append(upgraded.Mapping, mapping)
	}

	return upgraded, nil
}

func legacyNasshMapping(relayHost string) (enproxy.Mapping, error) {
	relayHost = strings.TrimSpace(relayHost)
	if relayHost == "" {
		return enproxy.Mapping{}, kflags.NewUsageErrorf("legacy config contains tunnels; pass --host-port to set the NASSH relay host")
	}

	routeHost := strings.TrimSpace(khttp.LooselyGetHost(relayHost))
	if routeHost == "" {
		return enproxy.Mapping{}, kflags.NewUsageErrorf("legacy NASSH relay host %q does not contain a routable host", relayHost)
	}

	target := &enproxy.NasshTarget{}
	if routeHost != relayHost {
		target.RelayHost = relayHost
	}
	return enproxy.Mapping{
		Name: "nassh",
		From: httpp.HostPath{
			Host: routeHost,
			Path: "/",
		},
		Target: enproxy.Target{Nassh: target},
	}, nil
}
