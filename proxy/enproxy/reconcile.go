package enproxy

import (
	"fmt"
	"net/http"

	"github.com/ccontavalli/enkit/lib/logger"
	"github.com/ccontavalli/enkit/proxy/amux"
	"github.com/ccontavalli/enkit/proxy/httpp"
	"github.com/ccontavalli/enkit/proxy/nasshp"
	"github.com/ccontavalli/enkit/proxy/utils"
)

type desiredState struct {
	Warnings Warnings
	Domains  []string
	Modules  []desiredModule
	Routes   []proxyRoute
}

type desiredModule interface {
	ID() string
	Reconcile(previous runtimeModule) (runtimeModule, error)
}

type runtimeModule interface {
	ID() string
	Install(ctx *installContext) error
	Domains() []string
	Activate() error
	Close() error
}

type installContext struct {
	log   logger.Logger
	root  amux.Mux
	hosts map[string]amux.Mux
}

func newInstallContext(log logger.Logger, root amux.Mux) *installContext {
	if log == nil {
		log = logger.Nil
	}
	return &installContext{
		log:  log,
		root: root,
		hosts: map[string]amux.Mux{
			"": root,
		},
	}
}

func (ctx *installContext) Host(host string) amux.Mux {
	host = utils.NormalizeHost(host)
	hmux, found := ctx.hosts[host]
	if found {
		return hmux
	}
	hmux = ctx.root.Host(host)
	ctx.hosts[host] = hmux
	return hmux
}

func (ctx *installContext) InstallBinding(binding httpp.Binding) {
	t := "default transforms"
	if binding.Transform != nil {
		t = fmt.Sprintf("%+v", binding.Transform)
	}
	ctx.log.Infof("Mapping: %s%s to %s (%+v)", binding.Host, binding.Path, binding.To, t)
	ctx.Host(binding.Host).Handle(binding.Path, binding.Handler)
}

type proxyDesiredModule struct {
	id      string
	index   int
	key     string
	mapping httpp.Mapping
	builder *httpp.Proxy
}

func (dm *proxyDesiredModule) ID() string {
	return dm.id
}

func (dm *proxyDesiredModule) Reconcile(previous runtimeModule) (runtimeModule, error) {
	if existing, ok := previous.(*proxyRuntimeModule); ok && existing.key == dm.key {
		return existing, nil
	}

	handler, err := dm.builder.CreateHandler(dm.mapping)
	if err != nil {
		return nil, fmt.Errorf("error in mapping entry %d - %w", dm.index, err)
	}
	bindings, _ := httpp.BindingsForMapping(dm.mapping, handler)
	return &proxyRuntimeModule{
		id:       dm.id,
		key:      dm.key,
		handler:  handler,
		bindings: bindings,
	}, nil
}

type proxyRuntimeModule struct {
	id       string
	key      string
	handler  http.Handler
	bindings []httpp.Binding
}

func (pm *proxyRuntimeModule) ID() string {
	return pm.id
}

func (pm *proxyRuntimeModule) Install(ctx *installContext) error {
	return nil
}

func (pm *proxyRuntimeModule) Domains() []string {
	return nil
}

func (pm *proxyRuntimeModule) BindingsForHost(host string) []httpp.Binding {
	host = utils.NormalizeHost(host)
	bindings := append([]httpp.Binding{}, pm.bindings...)
	for ix := range bindings {
		bindings[ix].Host = host
	}
	return bindings
}

func (pm *proxyRuntimeModule) Activate() error {
	return nil
}

func (pm *proxyRuntimeModule) Close() error {
	return nil
}

type nasshDesiredModule struct {
	id        string
	proxy     *nasshp.NasshProxy
	whitelist *utils.ReplaceableWhitelist
	patterns  utils.PatternList
}

func (dm *nasshDesiredModule) ID() string {
	return dm.id
}

func (dm *nasshDesiredModule) Reconcile(previous runtimeModule) (runtimeModule, error) {
	if existing, ok := previous.(*nasshRuntimeModule); ok && existing.proxy == dm.proxy && existing.whitelist == dm.whitelist {
		existing.patterns = dm.patterns
		return existing, nil
	}

	return &nasshRuntimeModule{
		id:        dm.id,
		proxy:     dm.proxy,
		whitelist: dm.whitelist,
		patterns:  dm.patterns,
	}, nil
}

type nasshRuntimeModule struct {
	id        string
	proxy     *nasshp.NasshProxy
	whitelist *utils.ReplaceableWhitelist
	patterns  utils.PatternList
}

func (nm *nasshRuntimeModule) ID() string {
	return nm.id
}

func (nm *nasshRuntimeModule) Install(ctx *installContext) error {
	root := ctx.Host("")
	if rhost := nm.proxy.RelayHost(); rhost != "" {
		root = ctx.Host(rhost)
	}
	nm.proxy.Register(root.Handle)
	return nil
}

func (nm *nasshRuntimeModule) Domains() []string {
	return nil
}

func (nm *nasshRuntimeModule) Activate() error {
	nm.whitelist.Set(nm.patterns)
	return nil
}

func (nm *nasshRuntimeModule) Close() error {
	return nil
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

func compileDesiredState(builder *httpp.Proxy, nproxy *nasshp.NasshProxy, whitelist *utils.ReplaceableWhitelist, config Config, patterns utils.PatternList, warnings Warnings) (*desiredState, error) {
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
		Routes:   []proxyRoute{},
	}

	seenModules := map[string]bool{}
	for ix, mapping := range config.Mapping {
		key, err := httpp.ModuleKey(mapping)
		if err != nil {
			return nil, fmt.Errorf("error in mapping entry %d - %w", ix, err)
		}

		moduleID := fmt.Sprintf("proxy:%s", key)
		moduleMapping := mapping
		moduleMapping.From.Host = ""
		if !seenModules[moduleID] {
			state.Modules = append(state.Modules, &proxyDesiredModule{
				id:      moduleID,
				index:   ix,
				key:     key,
				mapping: moduleMapping,
				builder: builder,
			})
			seenModules[moduleID] = true
		}
		state.Routes = append(state.Routes, proxyRoute{
			ModuleID: moduleID,
			Host:     utils.NormalizeHost(mapping.From.Host),
			Index:    ix,
		})
	}

	if nproxy != nil {
		state.Modules = append(state.Modules, &nasshDesiredModule{
			id:        "nasshp:default",
			proxy:     nproxy,
			whitelist: whitelist,
			patterns:  patterns,
		})
	}

	return state, nil
}

type proxyRoute struct {
	ModuleID string
	Host     string
	Index    int
}
