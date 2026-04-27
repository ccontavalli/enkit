package enproxy

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ccontavalli/enkit/proxy/httpp"
	"github.com/ccontavalli/enkit/proxy/utils"
)

type desiredState struct {
	Warnings Warnings
	Domains  []string
	Modules  []desiredModule
	Routes   []desiredRoute
}

// desiredModule is the module-level state requested by one config load.
//
// moduleKind.Build creates desiredModule values while compiling config. A
// desiredModule does not serve traffic and must not hold live resources that
// need cleanup if the config load is rejected before commit.
type desiredModule interface {
	// ID returns the stable runtime identity for this module.
	//
	// IDs are normally moduleID(kind, name). The ID selects the previous
	// runtimeModule passed to Reconcile and associates routes with the resulting
	// module plan.
	ID() string

	// Reconcile creates the runtime module for this config load.
	//
	// previous is the currently active runtime module with the same ID, if any.
	// Reconcile may return a new runtimeModule that shares durable resources
	// with previous, but it must not mutate any state that affects the active
	// config. Active state may still need to keep serving if a later Plan, Map,
	// metrics registration, or validation step fails.
	//
	// The bool return must be true only when the returned runtimeModule reuses
	// resources owned by previous and previous must therefore not be closed as a
	// stale module after commit. Return false when previous is incompatible or no
	// resources are shared; an incompatible previous module will be closed after
	// the new config is published.
	Reconcile(previous runtimeModule) (runtimeModule, bool, error)
}

// runtimeModule is the live module object selected for a config generation.
//
// Runtime modules may be newly created or copy-on-write wrappers around durable
// state from the previous generation. They should expose no externally visible
// changes until their modulePlan.Commit method is called.
type runtimeModule interface {
	// ID returns the same stable identity as the desiredModule that produced it.
	ID() string

	// Plan starts one transactional apply for this module.
	//
	// Plan is called once per runtimeModule after all desired modules have been
	// reconciled. The returned modulePlan receives every target mapped to this
	// module. Plan may allocate per-apply scratch state, but it must not mutate
	// active serving state; the current config must remain valid if a later step
	// fails and Commit is never called.
	Plan() (modulePlan, error)

	// RegisterMetrics describes this module's metrics to the provided registry.
	//
	// RegisterMetrics is called during metrics activation, after planning and
	// mapping have succeeded but before modulePlan.Commit. Implementations must
	// only populate the provided registry object; they must not register directly
	// with Prometheus or mutate serving state.
	RegisterMetrics(metrics utils.MetricRegistry)

	// Close releases resources owned only by this runtime module.
	//
	// Close is called after a replacement config has been published for modules
	// that were removed or not reused, and during Enproxy shutdown. It is not
	// called for a previous runtime module when Reconcile reported that its
	// resources were reused by the new runtime module.
	Close() error
}

// modulePlan is the per-apply transaction for one runtime module.
//
// The call order is Plan, then Map for each route using the module, then
// Domains, then Commit after every module has mapped successfully and metrics
// activation has succeeded. If any step before Commit fails, Commit is not
// called and the previous config must keep serving unchanged.
type modulePlan interface {
	// Map attaches one target to this module for the pending config.
	//
	// Map may create handlers and record tentative state in the plan. It should
	// call register for each route it wants installed. Map must not publish
	// changes to shared runtime state; put those changes in the plan and apply
	// them from Commit.
	Map(target moduleTarget, register RouteRegistrar) error

	// Domains returns extra listener domains required by targets mapped so far.
	//
	// Domains is called once after all Map calls for this module. It should
	// return normalized or normalizable host names only; collectDomains handles
	// final normalization and de-duplication.
	Domains() []string

	// Commit publishes the plan's pending runtime changes.
	//
	// Commit is called only after all modules have planned and mapped
	// successfully and metrics activation has committed. It must not fail. Any
	// operation that can fail must happen earlier in Plan or Map so the active
	// config can remain untouched on error.
	Commit()
}

// RouteRegistrar records an HTTP handler for one route produced by modulePlan.Map.
//
// If from is nil, the mapping's configured From value is used. A non-nil from
// lets a module expand one mapping into multiple concrete routes, such as
// module-owned paths or relay host aliases. label is used for routing metadata
// and diagnostics; handler is the HTTP handler to install if the config commits.
type RouteRegistrar func(from *httpp.HostPath, label string, handler http.Handler) error

type desiredRoute struct {
	ModuleID string
	Target   moduleTarget
	Index    int
}

// moduleTarget is the target-level configuration passed from moduleKind.Build
// to modulePlan.Map.
//
// Config contains the concrete target config for the module kind, for example
// *ProxyTarget, *NasshTarget, or *MetricsTarget. Module-level config should have
// already been parsed into the desired/runtime module; only per-target settings
// belong here.
type moduleTarget struct {
	Name   string
	From   httpp.HostPath
	Auth   httpp.MappingAuth
	Config any
}

func jsonKey(value interface{}) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

type plannedModule struct {
	id   string
	plan modulePlan
}

func planModules(desired []desiredModule, modules map[string]runtimeModule) ([]plannedModule, map[string]modulePlan, error) {
	ordered := []plannedModule{}
	byID := map[string]modulePlan{}
	for _, desiredModule := range desired {
		id := desiredModule.ID()
		plan, err := modules[id].Plan()
		if err != nil {
			return nil, nil, err
		}
		planned := plannedModule{
			id:   id,
			plan: plan,
		}
		ordered = append(ordered, planned)
		byID[id] = plan
	}
	return ordered, byID, nil
}

func commitModulePlans(plans []plannedModule) {
	for _, planned := range plans {
		planned.plan.Commit()
	}
}

func addDesiredModule(state *desiredState, seen map[string]string, ix int, kind, name, id, key string, module desiredModule) error {
	if previous, found := seen[id]; found {
		if previous != key {
			return fmt.Errorf("error in mapping entry %d - %s module %q was defined with conflicting settings", ix, kind, name)
		}
		return nil
	}

	state.Modules = append(state.Modules, module)
	seen[id] = key
	return nil
}

func reconcileModules(desired []desiredModule, current map[string]runtimeModule) (map[string]runtimeModule, []runtimeModule, error) {
	next := map[string]runtimeModule{}
	seen := map[string]bool{}
	reused := map[string]bool{}
	for _, module := range desired {
		id := module.ID()
		if seen[id] {
			return nil, nil, fmt.Errorf("duplicate module id %q", id)
		}
		seen[id] = true

		previous := current[id]
		reconciled, reusedPrevious, err := module.Reconcile(previous)
		if err != nil {
			return nil, nil, err
		}
		if previous != nil && reusedPrevious {
			reused[id] = true
		}
		next[id] = reconciled
	}

	stale := []runtimeModule{}
	for id, module := range current {
		if _, found := next[id]; !found {
			stale = append(stale, module)
			continue
		}
		if next[id] != module && !reused[id] {
			stale = append(stale, module)
		}
	}
	return next, stale, nil
}

func compileDesiredState(builder *httpp.Proxy, ep *Enproxy, config Config, patterns utils.PatternList, warnings Warnings) (*desiredState, error) {
	domains := append([]string{}, config.Domains...)

	state := &desiredState{
		Warnings: warnings,
		Domains:  domains,
		Modules:  []desiredModule{},
		Routes:   []desiredRoute{},
	}

	build := moduleBuild{
		builder:     builder,
		ep:          ep,
		config:      config,
		patterns:    patterns,
		gatherer:    ep.gatherer,
		state:       state,
		seenModules: map[string]string{},
	}
	for ix, mapping := range config.Mapping {
		kind, err := moduleKindForTarget(moduleKinds, mapping.Target)
		if err != nil {
			return nil, fmt.Errorf("error in mapping entry %d - %w", ix, err)
		}
		if err := kind.Build(&build, ix, mapping); err != nil {
			return nil, err
		}
	}

	return state, nil
}
