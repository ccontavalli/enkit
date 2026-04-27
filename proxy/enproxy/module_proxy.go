package enproxy

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/ccontavalli/enkit/proxy/httpp"
	"github.com/ccontavalli/enkit/proxy/utils"
)

type proxyModuleAdapter struct{}

func (proxyModuleAdapter) Kind() string {
	return "proxy"
}

func (proxyModuleAdapter) Matches(target Target) bool {
	return target.Proxy != nil
}

func (proxyModuleAdapter) ModuleNames(config *Config) []string {
	return moduleNamesFromMap(config.ProxyModules)
}

func (adapter proxyModuleAdapter) Check(config *Config, ix int, mapping Mapping, warnings *Warnings) error {
	module, err := resolveModule(adapter.Kind(), config.ProxyModules, mapping.Module)
	if err != nil {
		return err
	}
	_, err = resolveProxyTarget(module, moduleTargetFromMapping(mapping, mapping.Target.Proxy))
	return err
}

func (proxyModuleAdapter) NormalizeTarget(config *Config, ix int, mapping Mapping) (Target, error) {
	proxy := mapping.Target.Proxy
	if proxy == nil {
		return Target{}, fmt.Errorf("proxy target is missing")
	}
	copy := *proxy
	return Target{Proxy: &copy}, nil
}

func (adapter proxyModuleAdapter) Build(build *moduleBuild, ix int, mapping Mapping) error {
	moduleName := canonicalModuleName(mapping.Module)
	module, err := resolveModule(adapter.Kind(), build.config.ProxyModules, mapping.Module)
	if err != nil {
		return fmt.Errorf("error in mapping entry %d - %w", ix, err)
	}

	key, err := jsonKey(module)
	if err != nil {
		return fmt.Errorf("error in mapping entry %d - %w", ix, err)
	}

	proxy := mapping.Target.Proxy
	if proxy == nil {
		return fmt.Errorf("error in mapping entry %d - proxy target is missing", ix)
	}
	copy := *proxy

	return build.addRoute(ix, adapter.Kind(), moduleName, key, &proxyDesiredModule{
		id:      moduleID(adapter.Kind(), moduleName),
		key:     key,
		module:  module,
		builder: build.builder,
	}, moduleTargetFromMapping(mapping, &copy))
}

func resolveProxyTarget(module ProxyModule, target moduleTarget) (httpp.Mapping, error) {
	proxy, ok := target.Config.(*ProxyTarget)
	if !ok || proxy == nil {
		return httpp.Mapping{}, fmt.Errorf("proxy target is missing")
	}

	to := strings.TrimSpace(module.To)
	if targetTo := strings.TrimSpace(proxy.To); targetTo != "" {
		to = targetTo
	}
	if to == "" {
		return httpp.Mapping{}, fmt.Errorf("proxy target is missing a backend address")
	}

	transform := module.Transform
	if proxy.Transform != nil {
		transform = proxy.Transform
	}

	return httpp.Mapping{
		Name: target.Name,
		From: target.From,
		Auth: target.Auth,
		To:   to,

		Transform: transform,
	}, nil
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

func (dm *proxyDesiredModule) Reconcile(previous runtimeModule) (runtimeModule, bool, error) {
	if existing, ok := previous.(*proxyRuntimeModule); ok && existing.key == dm.key {
		return &proxyRuntimeModule{
			id:       dm.id,
			key:      dm.key,
			module:   dm.module,
			builder:  dm.builder,
			handlers: existing.handlers,
		}, true, nil
	}

	return &proxyRuntimeModule{
		id:       dm.id,
		key:      dm.key,
		module:   dm.module,
		builder:  dm.builder,
		handlers: map[string]http.Handler{},
	}, false, nil
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

func (pm *proxyRuntimeModule) Plan() (modulePlan, error) {
	return &proxyPlan{
		module:   pm,
		handlers: map[string]http.Handler{},
	}, nil
}

type proxyPlan struct {
	module   *proxyRuntimeModule
	handlers map[string]http.Handler
}

func (pp *proxyPlan) Map(target moduleTarget, register RouteRegistrar) error {
	effective, err := resolveProxyTarget(pp.module.module, target)
	if err != nil {
		return err
	}

	key, err := httpp.ModuleKey(effective)
	if err != nil {
		return err
	}

	handler := pp.module.handlers[key]
	if handler == nil {
		handler = pp.handlers[key]
	}
	if handler == nil {
		handler, err = pp.module.builder.CreateHandler(effective)
		if err != nil {
			return err
		}
		pp.handlers[key] = handler
	}

	return register(nil, effective.To, handler)
}

func (pp *proxyPlan) Domains() []string {
	return nil
}

func (pp *proxyPlan) Commit() {
	for key, handler := range pp.handlers {
		pp.module.handlers[key] = handler
	}
}

func (pm *proxyRuntimeModule) RegisterMetrics(metrics utils.MetricRegistry) {
}

func (pm *proxyRuntimeModule) Close() error {
	return nil
}
