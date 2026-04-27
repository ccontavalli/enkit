package enproxy

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"strings"

	"github.com/ccontavalli/enkit/lib/oauth"
	"github.com/ccontavalli/enkit/proxy/httpp"
	"github.com/ccontavalli/enkit/proxy/nasshp"
	"github.com/ccontavalli/enkit/proxy/utils"
)

type nasshModuleAdapter struct {
	defaultRelayHost string
}

func (nasshModuleAdapter) Kind() string {
	return "nassh"
}

func (nasshModuleAdapter) Matches(target Target) bool {
	return target.Nassh != nil
}

func (nasshModuleAdapter) ModuleNames(config *Config) []string {
	return moduleNamesFromMap(config.NasshModules)
}

func (adapter nasshModuleAdapter) NormalizeTarget(config *Config, ix int, mapping Mapping) (Target, error) {
	target, err := adapter.normalizeTarget(config, ix, mapping)
	if err != nil {
		return Target{}, err
	}
	return Target{Nassh: &target}, nil
}

func (adapter nasshModuleAdapter) Check(config *Config, ix int, mapping Mapping, warnings *Warnings) error {
	if _, err := resolveModule(adapter.Kind(), config.NasshModules, mapping.Module); err != nil {
		return err
	}

	path := strings.TrimSpace(mapping.From.Path)
	if path == "" {
		path = "/"
	}
	if path != "/" {
		return fmt.Errorf("nassh targets must be mounted on /")
	}
	if len(config.Tunnels) <= 0 {
		warnings.AddOnce("config file: empty whitelist for tunnels - no tunnel will be allowed!")
	}
	return nil
}

func (adapter nasshModuleAdapter) Build(build *moduleBuild, ix int, mapping Mapping) error {
	withoutAuthentication := build.ep.withoutAuthentication || build.ep.withoutNasshAuthentication
	if build.ep.authenticate == nil && !withoutAuthentication {
		return fmt.Errorf("error in mapping entry %d - nassh target requires authentication to be configured", ix)
	}

	moduleName := canonicalModuleName(mapping.Module)
	module, err := resolveModule(adapter.Kind(), build.config.NasshModules, mapping.Module)
	if err != nil {
		return fmt.Errorf("error in mapping entry %d - %w", ix, err)
	}

	nassh := mapping.Target.Nassh
	if nassh == nil {
		return fmt.Errorf("error in mapping entry %d - nassh target is missing", ix)
	}
	if strings.TrimSpace(nassh.RelayHost) == "" {
		return fmt.Errorf("error in mapping entry %d - nassh target is missing a relay host", ix)
	}

	key, err := jsonKey(module)
	if err != nil {
		return fmt.Errorf("error in mapping entry %d - %w", ix, err)
	}

	authenticate := build.ep.authenticate
	if withoutAuthentication {
		authenticate = nil
	}

	copy := *nassh
	return build.addRoute(ix, adapter.Kind(), moduleName, key, &nasshDesiredModule{
		id:           moduleID(adapter.Kind(), moduleName),
		key:          key,
		rng:          build.ep.rng,
		authenticate: authenticate,
		mods:         build.ep.nmods,
		whitelist:    build.ep.whitelist,
		patterns:     build.patterns,
	}, moduleTargetFromMapping(mapping, &copy))
}

func (adapter nasshModuleAdapter) normalizeTarget(config *Config, ix int, mapping Mapping) (NasshTarget, error) {
	module, err := resolveModule(adapter.Kind(), config.NasshModules, mapping.Module)
	if err != nil {
		return NasshTarget{}, fmt.Errorf("error in mapping entry %d - %w", ix, err)
	}

	nassh := mapping.Target.Nassh
	if nassh == nil {
		return NasshTarget{}, fmt.Errorf("error in mapping entry %d - nassh target is missing", ix)
	}

	normalized := *nassh
	normalized.RelayHost = resolveNasshRelayHost(adapter.defaultRelayHost, module, mapping)
	if normalized.RelayHost == "" {
		return NasshTarget{}, fmt.Errorf("error in mapping entry %d - nassh target is missing a relay host", ix)
	}

	return normalized, nil
}

func resolveNasshRelayHost(defaultRelayHost string, module NasshModule, mapping Mapping) string {
	if mapping.Target.Nassh != nil {
		if relay := strings.TrimSpace(mapping.Target.Nassh.RelayHost); relay != "" {
			return relay
		}
	}
	if relay := strings.TrimSpace(module.RelayHost); relay != "" {
		return relay
	}
	if relay := strings.TrimSpace(mapping.From.Host); relay != "" {
		return relay
	}
	if relay := strings.TrimSpace(defaultRelayHost); relay != "" {
		return relay
	}
	return ""
}

type nasshDesiredModule struct {
	id           string
	key          string
	rng          *rand.Rand
	authenticate oauth.Authenticate
	mods         []nasshp.Modifier
	whitelist    *utils.ReplaceableWhitelist
	patterns     utils.PatternList
}

func (dm *nasshDesiredModule) ID() string {
	return dm.id
}

func (dm *nasshDesiredModule) Reconcile(previous runtimeModule) (runtimeModule, bool, error) {
	if existing, ok := previous.(*nasshRuntimeModule); ok && existing.key == dm.key && existing.whitelist == dm.whitelist {
		existing.patterns = dm.patterns
		return existing, true, nil
	}

	proxy, err := nasshp.New(dm.rng, dm.authenticate, dm.mods...)
	if err != nil {
		return nil, false, err
	}

	return &nasshRuntimeModule{
		id:        dm.id,
		key:       dm.key,
		proxy:     proxy,
		whitelist: dm.whitelist,
		patterns:  dm.patterns,
	}, false, nil
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

type nasshHostBinding struct {
	relayHost string
	explicit  bool
}

func (nm *nasshRuntimeModule) Plan() (modulePlan, error) {
	return &nasshPlan{
		module:           nm,
		boundHosts:       map[string]nasshHostBinding{},
		seenRelayDomains: map[string]bool{},
	}, nil
}

type nasshPlan struct {
	module           *nasshRuntimeModule
	domains          []string
	boundHosts       map[string]nasshHostBinding
	seenRelayDomains map[string]bool
}

func (np *nasshPlan) Map(target moduleTarget, register RouteRegistrar) error {
	nassh, ok := target.Config.(*NasshTarget)
	if !ok || nassh == nil {
		return fmt.Errorf("nassh target is missing")
	}

	registerHost := func(host, relayHost string) error {
		if err := register(&httpp.HostPath{Host: host, Path: "/cookie"}, "nasshp://cookie", np.module.proxy.ServeCookieForRelayHost(relayHost)); err != nil {
			return err
		}
		if err := register(&httpp.HostPath{Host: host, Path: "/proxy"}, "nasshp://proxy", http.HandlerFunc(np.module.proxy.ServeProxy)); err != nil {
			return err
		}
		if err := register(&httpp.HostPath{Host: host, Path: "/connect"}, "nasshp://connect", http.HandlerFunc(np.module.proxy.ServeConnect)); err != nil {
			return err
		}
		return nil
	}

	bindHost := func(host, relayHost string, explicit bool) error {
		host = utils.NormalizeHost(host)
		if existing, found := np.boundHosts[host]; found {
			if existing.relayHost != relayHost {
				return fmt.Errorf("duplicate route %q on host %q already defined for relay host %q", "/cookie", host, existing.relayHost)
			}
			if explicit && existing.explicit {
				return fmt.Errorf("duplicate route %q on host %q already defined", "/cookie", host)
			}
			if explicit && !existing.explicit {
				np.boundHosts[host] = nasshHostBinding{relayHost: relayHost, explicit: true}
			}
			return nil
		}
		if err := registerHost(host, relayHost); err != nil {
			return err
		}
		np.boundHosts[host] = nasshHostBinding{relayHost: relayHost, explicit: explicit}
		return nil
	}

	relayHost := utils.NormalizeHost(nassh.RelayHost)
	for _, host := range nasshBindingHosts(target.From.Host) {
		if err := bindHost(host, relayHost, true); err != nil {
			return err
		}
	}
	if relayHost == "" {
		return nil
	}
	if !np.seenRelayDomains[relayHost] {
		np.domains = append(np.domains, relayHost)
		np.seenRelayDomains[relayHost] = true
	}
	return bindHost(relayHost, relayHost, false)
}

func (np *nasshPlan) Domains() []string {
	return np.domains
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

func (np *nasshPlan) Commit() {
	nm := np.module
	if nm.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		nm.cancel = cancel
		go nm.proxy.Run(ctx)
	}
	nm.whitelist.Set(nm.patterns)
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
