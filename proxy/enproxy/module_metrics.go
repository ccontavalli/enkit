package enproxy

import (
	"fmt"
	"net/http"

	"github.com/ccontavalli/enkit/lib/oauth"
	"github.com/ccontavalli/enkit/proxy/httpp"
	"github.com/ccontavalli/enkit/proxy/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type metricsModuleAdapter struct{}

func (metricsModuleAdapter) Kind() string {
	return "metrics"
}

func (metricsModuleAdapter) Matches(target Target) bool {
	return target.Metrics != nil
}

func (metricsModuleAdapter) ModuleNames(config *Config) []string {
	return moduleNamesFromMap(config.MetricsModules)
}

func (adapter metricsModuleAdapter) Check(config *Config, ix int, mapping Mapping, warnings *Warnings) error {
	_, err := resolveModule(adapter.Kind(), config.MetricsModules, mapping.Module)
	return err
}

func (metricsModuleAdapter) NormalizeTarget(config *Config, ix int, mapping Mapping) (Target, error) {
	metrics := mapping.Target.Metrics
	if metrics == nil {
		return Target{}, fmt.Errorf("metrics target is missing")
	}
	copy := *metrics
	return Target{Metrics: &copy}, nil
}

func (adapter metricsModuleAdapter) Build(build *moduleBuild, ix int, mapping Mapping) error {
	moduleName := canonicalModuleName(mapping.Module)
	module, err := resolveModule(adapter.Kind(), build.config.MetricsModules, mapping.Module)
	if err != nil {
		return fmt.Errorf("error in mapping entry %d - %w", ix, err)
	}

	key, err := jsonKey(module)
	if err != nil {
		return fmt.Errorf("error in mapping entry %d - %w", ix, err)
	}

	metrics := mapping.Target.Metrics
	if metrics == nil {
		return fmt.Errorf("error in mapping entry %d - metrics target is missing", ix)
	}
	copy := *metrics

	return build.addRoute(ix, adapter.Kind(), moduleName, key, &metricsDesiredModule{
		id:           moduleID(adapter.Kind(), moduleName),
		key:          key,
		module:       module,
		gatherer:     build.gatherer,
		authenticate: build.ep.authenticate,
	}, moduleTargetFromMapping(mapping, &copy))
}

type metricsDesiredModule struct {
	id           string
	key          string
	module       MetricsModule
	gatherer     prometheus.Gatherer
	authenticate oauth.Authenticate
}

func (dm *metricsDesiredModule) ID() string {
	return dm.id
}

func (dm *metricsDesiredModule) Reconcile(previous runtimeModule) (runtimeModule, bool, error) {
	if existing, ok := previous.(*metricsRuntimeModule); ok && existing.key == dm.key {
		return &metricsRuntimeModule{
			id:           dm.id,
			key:          dm.key,
			module:       dm.module,
			gatherer:     dm.gatherer,
			authenticate: dm.authenticate,
		}, true, nil
	}

	return &metricsRuntimeModule{
		id:           dm.id,
		key:          dm.key,
		module:       dm.module,
		gatherer:     dm.gatherer,
		authenticate: dm.authenticate,
	}, false, nil
}

type metricsRuntimeModule struct {
	id           string
	key          string
	module       MetricsModule
	gatherer     prometheus.Gatherer
	authenticate oauth.Authenticate
}

func (mm *metricsRuntimeModule) ID() string {
	return mm.id
}

func (mm *metricsRuntimeModule) Plan() (modulePlan, error) {
	return &metricsPlan{module: mm}, nil
}

type metricsPlan struct {
	module *metricsRuntimeModule
}

func (mp *metricsPlan) Map(target moduleTarget, register RouteRegistrar) error {
	metrics, ok := target.Config.(*MetricsTarget)
	if !ok || metrics == nil {
		return fmt.Errorf("metrics target is missing")
	}
	if mp.module.gatherer == nil {
		return fmt.Errorf("metrics target requires a prometheus gatherer")
	}

	handler := promhttp.HandlerFor(mp.module.gatherer, promhttp.HandlerOpts{})
	if target.Auth != httpp.MappingPublic {
		if mp.module.authenticate == nil {
			return fmt.Errorf("metrics target requires authentication to be configured")
		}
		handler = authenticateMetricsHandler(handler, mp.module.authenticate)
	}

	return register(nil, "metrics://prometheus", handler)
}

func authenticateMetricsHandler(handler http.Handler, authenticate oauth.Authenticate) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		creds, err := authenticate(w, r, oauth.CreateRedirectURL(r))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if creds == nil {
			return
		}
		handler.ServeHTTP(w, r.WithContext(oauth.SetCredentials(r.Context(), creds)))
	})
}

func (mp *metricsPlan) Domains() []string {
	return nil
}

func (mp *metricsPlan) Commit() {
}

func (mm *metricsRuntimeModule) RegisterMetrics(metrics utils.MetricRegistry) {
}

func (mm *metricsRuntimeModule) Close() error {
	return nil
}
