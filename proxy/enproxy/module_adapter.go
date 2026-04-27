package enproxy

import (
	"fmt"

	"github.com/ccontavalli/enkit/proxy/httpp"
	"github.com/ccontavalli/enkit/proxy/utils"
	"github.com/prometheus/client_golang/prometheus"
)

// moduleKind is the static adapter for one Target variant.
//
// A moduleKind bridges the public configuration structs and the reload
// lifecycle in reconcile.go. Implementations may carry immutable default
// configuration, but all per-load state belongs in moduleBuild, desiredModule,
// runtimeModule, or modulePlan.
//
// Reload flow for a mapping is:
//   - ConfigNormalizer.NormalizeConfig calls Matches and NormalizeTarget to
//     materialize representable defaults and policy rewrites.
//   - Config.Parse calls Matches and Check to validate the normalized mapping.
//   - ApplyConfigStruct calls Matches and Build after Parse succeeds.
//   - Build records a desiredModule plus one desiredRoute for the mapping.
//
// Build must not mutate live runtime state. Runtime changes happen later via
// desiredModule.Reconcile, modulePlan.Map, and modulePlan.Commit.
type moduleKind interface {
	// Kind returns the stable kind name used in module IDs and errors.
	//
	// The value is part of runtime identity through moduleID(kind, name); avoid
	// changing it for existing module kinds.
	Kind() string

	// Matches reports whether this adapter owns target.
	//
	// Exactly one registered moduleKind must match each Target. Matches should
	// only inspect which target-specific field is set; semantic validation
	// belongs in Check.
	Matches(target Target) bool

	// ModuleNames returns all configured module names for this kind.
	//
	// Config.Parse uses this once per config load to validate names before any
	// mappings are checked. Return only names explicitly present in the config;
	// the implicit default module is handled by resolveModule.
	ModuleNames(config *Config) []string

	// Check validates one mapping during Config.Parse.
	//
	// Check is called before any runtime objects are built. It may validate the
	// referenced module, target-specific fields, and cross-field constraints, and
	// may append warnings. It must not depend on Enproxy runtime state, allocate
	// handlers, request Prometheus, or mutate config.
	Check(config *Config, ix int, mapping Mapping, warnings *Warnings) error

	// Build records the desired state for one already-validated mapping.
	//
	// Build is called once per mapping during ApplyConfigStruct after Parse has
	// succeeded. ApplyConfigStruct passes the normalized config produced by the
	// ConfigNormalizer, so Build should consume explicit, already-materialized
	// config defaults and focus on runtime planning: compute a stable key for
	// conflict/reuse detection, create a desiredModule, and call build.addRoute.
	// If this module kind exposes the shared Prometheus gatherer, use
	// build.gatherer.
	//
	// Build must remain transactional: it may construct desired objects, but it
	// must not alter existing runtime modules or externally visible state.
	Build(build *moduleBuild, ix int, mapping Mapping) error

	// NormalizeTarget materializes target defaults that can be represented in
	// Config.
	//
	// ConfigNormalizer.NormalizeConfig calls this once per mapping before Parse
	// or Build so CLI inspection and runtime reload see the same explicit target
	// configuration. Return a detached copy of the target and apply only pure
	// config-to-config normalization. Do not allocate runtime objects or mutate
	// config or mapping.
	NormalizeTarget(config *Config, ix int, mapping Mapping) (Target, error)
}

// moduleBuild contains the per-ApplyConfigStruct state shared by moduleKind
// implementations while converting mappings into desired state.
//
// Implementations should treat these fields as read-only except through the
// helper methods below. addRoute is the normal way to append desired modules and
// routes; build.gatherer exposes the shared Prometheus gatherer when needed.
type moduleBuild struct {
	builder     *httpp.Proxy
	ep          *Enproxy
	config      Config
	patterns    utils.PatternList
	gatherer    prometheus.Gatherer
	state       *desiredState
	seenModules map[string]string
}

// addRoute adds or reuses one desired module and attaches one mapping to it.
//
// key must uniquely describe the module-level configuration. If two mappings
// refer to the same kind/name but compute different keys, the config is
// rejected as conflicting. Per-target configuration should live in target; it
// must not be included in key unless changing it requires replacing the module.
func (build *moduleBuild) addRoute(ix int, kind, name, key string, module desiredModule, target moduleTarget) error {
	id := moduleID(kind, name)
	if err := addDesiredModule(build.state, build.seenModules, ix, kind, name, id, key, module); err != nil {
		return err
	}
	build.state.Routes = append(build.state.Routes, desiredRoute{
		ModuleID: id,
		Target:   target,
		Index:    ix,
	})
	return nil
}

func moduleID(kind, name string) string {
	return kind + ":" + name
}

func newModuleKinds(defaultNasshRelayHost string) []moduleKind {
	return []moduleKind{
		proxyModuleAdapter{},
		nasshModuleAdapter{defaultRelayHost: defaultNasshRelayHost},
		metricsModuleAdapter{},
	}
}

var moduleKinds = newModuleKinds("")

func moduleKindForTarget(kinds []moduleKind, target Target) (moduleKind, error) {
	var matched moduleKind
	count := 0
	for _, kind := range kinds {
		if kind.Matches(target) {
			matched = kind
			count++
		}
	}
	if count != 1 {
		return nil, fmt.Errorf("must define exactly one target kind")
	}
	return matched, nil
}

func moduleTargetFromMapping(mapping Mapping, config any) moduleTarget {
	return moduleTarget{
		Name:   mapping.Name,
		From:   mapping.From,
		Auth:   mapping.Auth,
		Config: config,
	}
}

func moduleNamesFromMap[T any](modules map[string]T) []string {
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	return names
}
