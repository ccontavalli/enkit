package enproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"

	"github.com/ccontavalli/enkit/lib/oauth"
	"github.com/ccontavalli/enkit/proxy/httpp"
	"github.com/ccontavalli/enkit/proxy/nasshp"
	"github.com/ccontavalli/enkit/proxy/utils"
)

type desiredState struct {
	Warnings Warnings
	Domains  []string
	Modules  []desiredModule
	Routes   []desiredRoute
}

type desiredModule interface {
	ID() string
	Reconcile(previous runtimeModule) (runtimeModule, error)
}

type moduleActivation interface {
	Commit()
	Rollback()
}

type runtimeModule interface {
	ID() string
	BindingsForMapping(mapping Mapping) ([]httpp.Binding, error)
	Domains() []string
	RegisterMetrics(metrics utils.MetricRegistry)
	PrepareActivate() (moduleActivation, error)
	Close() error
}

type noopActivation struct{}

func (noopActivation) Commit()   {}
func (noopActivation) Rollback() {}

type desiredRoute struct {
	ModuleID string
	Mapping  Mapping
	Index    int
}

func jsonKey(value interface{}) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

type proxyDesiredModule struct {
	id      string
	key     string
	module  ProxyModule
	builder *httpp.Proxy
}

func (dm *proxyDesiredModule) ID() string {
	return dm.id
}

func (dm *proxyDesiredModule) Reconcile(previous runtimeModule) (runtimeModule, error) {
	if existing, ok := previous.(*proxyRuntimeModule); ok && existing.key == dm.key {
		existing.module = dm.module
		existing.builder = dm.builder
		return existing, nil
	}

	return &proxyRuntimeModule{
		id:       dm.id,
		key:      dm.key,
		module:   dm.module,
		builder:  dm.builder,
		handlers: map[string]http.Handler{},
	}, nil
}

type proxyRuntimeModule struct {
	id       string
	key      string
	module   ProxyModule
	builder  *httpp.Proxy
	handlers map[string]http.Handler
}

func (pm *proxyRuntimeModule) ID() string {
	return pm.id
}

func (pm *proxyRuntimeModule) BindingsForMapping(mapping Mapping) ([]httpp.Binding, error) {
	effective, err := resolveProxyMapping(pm.module, mapping)
	if err != nil {
		return nil, err
	}

	key, err := httpp.ModuleKey(effective)
	if err != nil {
		return nil, err
	}

	handler := pm.handlers[key]
	if handler == nil {
		handler, err = pm.builder.CreateHandler(effective)
		if err != nil {
			return nil, err
		}
		pm.handlers[key] = handler
	}

	bindings, _ := httpp.BindingsForMapping(effective, handler)
	return bindings, nil
}

func (pm *proxyRuntimeModule) Domains() []string {
	return nil
}

func (pm *proxyRuntimeModule) RegisterMetrics(metrics utils.MetricRegistry) {
}

func (pm *proxyRuntimeModule) PrepareActivate() (moduleActivation, error) {
	return noopActivation{}, nil
}

func (pm *proxyRuntimeModule) Close() error {
	return nil
}

type nasshDesiredModule struct {
	id           string
	key          string
	relayHost    string
	rng          *rand.Rand
	authenticate oauth.Authenticate
	mods         []nasshp.Modifier
	whitelist    *utils.ReplaceableWhitelist
	patterns     utils.PatternList
}

func (dm *nasshDesiredModule) ID() string {
	return dm.id
}

func (dm *nasshDesiredModule) Reconcile(previous runtimeModule) (runtimeModule, error) {
	if existing, ok := previous.(*nasshRuntimeModule); ok && existing.key == dm.key && existing.whitelist == dm.whitelist {
		existing.patterns = dm.patterns
		return existing, nil
	}

	mods := append([]nasshp.Modifier{}, dm.mods...)
	if dm.relayHost != "" {
		mods = append(mods, nasshp.WithRelayHost(dm.relayHost))
	}
	proxy, err := nasshp.New(dm.rng, dm.authenticate, mods...)
	if err != nil {
		return nil, err
	}

	return &nasshRuntimeModule{
		id:        dm.id,
		key:       dm.key,
		proxy:     proxy,
		whitelist: dm.whitelist,
		patterns:  dm.patterns,
	}, nil
}

type nasshRuntimeModule struct {
	id        string
	key       string
	proxy     *nasshp.NasshProxy
	whitelist *utils.ReplaceableWhitelist
	patterns  utils.PatternList
	cancel    context.CancelFunc
}

func (nm *nasshRuntimeModule) ID() string {
	return nm.id
}

func (nm *nasshRuntimeModule) BindingsForMapping(mapping Mapping) ([]httpp.Binding, error) {
	bindings := []httpp.Binding{}
	for _, host := range nasshBindingHosts(mapping.From.Host, nm.proxy.RelayHost()) {
		bindings = append(bindings,
			httpp.Binding{Host: host, Path: "/cookie", To: "nasshp://cookie", Handler: http.HandlerFunc(nm.proxy.ServeCookie)},
			httpp.Binding{Host: host, Path: "/proxy", To: "nasshp://proxy", Handler: http.HandlerFunc(nm.proxy.ServeProxy)},
			httpp.Binding{Host: host, Path: "/connect", To: "nasshp://connect", Handler: http.HandlerFunc(nm.proxy.ServeConnect)},
		)
	}
	return bindings, nil
}

func (nm *nasshRuntimeModule) Domains() []string {
	relayHost := nm.proxy.RelayHost()
	if relayHost == "" {
		return nil
	}
	return []string{relayHost}
}

func nasshBindingHosts(hosts ...string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, host := range hosts {
		normalized := utils.NormalizeHost(host)
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}

type nasshActivation struct {
	module *nasshRuntimeModule
}

func (na *nasshActivation) Commit() {
	nm := na.module
	if nm.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		nm.cancel = cancel
		go nm.proxy.Run(ctx)
	}
	nm.whitelist.Set(nm.patterns)
}

func (na *nasshActivation) Rollback() {
}

func (nm *nasshRuntimeModule) PrepareActivate() (moduleActivation, error) {
	return &nasshActivation{module: nm}, nil
}

func (nm *nasshRuntimeModule) Close() error {
	if nm.cancel != nil {
		nm.cancel()
		nm.cancel = nil
	}
	return nil
}

func (nm *nasshRuntimeModule) RegisterMetrics(metrics utils.MetricRegistry) {
	nm.proxy.RegisterMetrics(metrics)
}

func reconcileModules(desired []desiredModule, current map[string]runtimeModule) (map[string]runtimeModule, []runtimeModule, error) {
	next := map[string]runtimeModule{}
	seen := map[string]bool{}
	for _, module := range desired {
		id := module.ID()
		if seen[id] {
			return nil, nil, fmt.Errorf("duplicate module id %q", id)
		}
		seen[id] = true

		reconciled, err := module.Reconcile(current[id])
		if err != nil {
			return nil, nil, err
		}
		next[id] = reconciled
	}

	stale := []runtimeModule{}
	for id, module := range current {
		if next[id] != module {
			stale = append(stale, module)
		}
	}
	return next, stale, nil
}

func compileDesiredState(builder *httpp.Proxy, ep *Enproxy, config Config, patterns utils.PatternList, warnings Warnings) (*desiredState, error) {
	domains := []string{}
	for _, domain := range config.Domains {
		normalized := utils.NormalizeHost(domain)
		if normalized != "" {
			domains = append(domains, normalized)
		}
	}

	state := &desiredState{
		Warnings: warnings,
		Domains:  domains,
		Modules:  []desiredModule{},
		Routes:   []desiredRoute{},
	}

	seenProxyModules := map[string]string{}
	seenNasshHosts := map[string]string{}
	for ix, mapping := range config.Mapping {
		switch {
		case mapping.Target.Proxy != nil:
			moduleName := canonicalModuleName(mapping.Module)
			module, err := resolveProxyModule(config.ProxyModules, mapping.Module)
			if err != nil {
				return nil, fmt.Errorf("error in mapping entry %d - %w", ix, err)
			}

			moduleID := "proxy:" + moduleName
			key, err := jsonKey(module)
			if err != nil {
				return nil, fmt.Errorf("error in mapping entry %d - %w", ix, err)
			}
			if previous, found := seenProxyModules[moduleID]; !found {
				state.Modules = append(state.Modules, &proxyDesiredModule{
					id:      moduleID,
					key:     key,
					module:  module,
					builder: builder,
				})
				seenProxyModules[moduleID] = key
			} else if previous != key {
				return nil, fmt.Errorf("error in mapping entry %d - proxy module %q was defined with conflicting settings", ix, moduleName)
			}

			state.Routes = append(state.Routes, desiredRoute{
				ModuleID: moduleID,
				Mapping:  mapping,
				Index:    ix,
			})

		case mapping.Target.Nassh != nil:
			if ep.authenticate == nil && !ep.withoutNasshAuthentication {
				return nil, fmt.Errorf("error in mapping entry %d - nassh target requires authentication to be configured", ix)
			}

			moduleName := canonicalModuleName(mapping.Module)
			module, err := resolveNasshModule(config.NasshModules, mapping.Module)
			if err != nil {
				return nil, fmt.Errorf("error in mapping entry %d - %w", ix, err)
			}

			moduleID := "nassh:" + moduleName
			relayHost := resolveNasshRelayHost(ep.defaultNasshRelayHost, module, mapping)
			if relayHost == "" {
				return nil, fmt.Errorf("error in mapping entry %d - nassh target is missing a relay host", ix)
			}
			host := utils.NormalizeHost(mapping.From.Host)
			if previous, found := seenNasshHosts[moduleID]; found && previous != host {
				return nil, fmt.Errorf("error in mapping entry %d - nassh module %q cannot be mounted on multiple hosts", ix, moduleName)
			}
			seenNasshHosts[moduleID] = host

			key, err := jsonKey(struct {
				Module    NasshModule
				RelayHost string
			}{
				Module:    module,
				RelayHost: relayHost,
			})
			if err != nil {
				return nil, fmt.Errorf("error in mapping entry %d - %w", ix, err)
			}
			if _, found := seenProxyModules[moduleID]; !found {
				state.Modules = append(state.Modules, &nasshDesiredModule{
					id:           moduleID,
					key:          key,
					relayHost:    relayHost,
					rng:          ep.rng,
					authenticate: ep.authenticate,
					mods:         ep.nmods,
					whitelist:    ep.whitelist,
					patterns:     patterns,
				})
				seenProxyModules[moduleID] = key
			} else if seenProxyModules[moduleID] != key {
				return nil, fmt.Errorf("error in mapping entry %d - nassh module %q was defined with conflicting settings", ix, moduleName)
			}

			state.Routes = append(state.Routes, desiredRoute{
				ModuleID: moduleID,
				Mapping:  mapping,
				Index:    ix,
			})
		}
	}

	return state, nil
}
