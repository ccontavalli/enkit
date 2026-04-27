package enproxy

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/ccontavalli/enkit/proxy/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type moduleMetricsManager struct {
	register prometheus.Registerer
	active   map[string]*registeredModuleMetrics
}

type registeredModuleMetrics struct {
	id        string
	module    runtimeModule
	register  prometheus.Registerer
	collector prometheus.Collector
}

func newModuleMetricsManager(register prometheus.Registerer) *moduleMetricsManager {
	return &moduleMetricsManager{
		register: register,
		active:   map[string]*registeredModuleMetrics{},
	}
}

func resolvePrometheus(gatherer prometheus.Gatherer, register prometheus.Registerer) (prometheus.Gatherer, prometheus.Registerer, error) {
	if gatherer == nil && register == nil {
		registry := prometheus.NewRegistry()
		return prometheus.Gatherers{registry, prometheus.DefaultGatherer}, registry, nil
	}
	if gatherer == nil || register == nil {
		return nil, nil, fmt.Errorf("prometheus gatherer and registerer must both be set or both be nil")
	}
	return gatherer, register, nil
}

func (ep *Enproxy) RunMetrics() error {
	if ep.gatherer == nil {
		return fmt.Errorf("metrics listener requires a prometheus gatherer")
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(ep.gatherer, promhttp.HandlerOpts{}))
	return ep.metrics(ep.log.Infof, mux)
}

func (m *moduleMetricsManager) Apply(modules map[string]runtimeModule) error {
	if m == nil || m.register == nil {
		return nil
	}

	next := map[string]*registeredModuleMetrics{}

	for _, id := range sortedModuleIDs(modules) {
		module := modules[id]
		current := m.active[id]
		if current != nil && current.module == module {
			next[id] = current
			continue
		}

		var metrics utils.CounterMetrics
		module.RegisterMetrics(&metrics)
		collector := metrics.Collector()
		if collector == nil {
			continue
		}

		next[id] = &registeredModuleMetrics{
			id:     id,
			module: module,
			register: prometheus.WrapRegistererWith(prometheus.Labels{
				"module": moduleMetricLabel(id),
			}, m.register),
			collector: collector,
		}
	}

	current := m.active
	unregistered := []*registeredModuleMetrics{}
	for _, id := range sortedRegisteredMetricIDs(current) {
		previous := current[id]
		if previous == next[id] {
			continue
		}
		previous.register.Unregister(previous.collector)
		unregistered = append(unregistered, previous)
	}

	registered := []*registeredModuleMetrics{}
	for _, id := range sortedRegisteredMetricIDs(next) {
		registeredMetrics := next[id]
		if registeredMetrics == current[id] {
			continue
		}
		if err := registeredMetrics.register.Register(registeredMetrics.collector); err != nil {
			errs := []error{fmt.Errorf("register metrics for module %q: %w", registeredMetrics.id, err)}
			for i := len(registered) - 1; i >= 0; i-- {
				registered[i].register.Unregister(registered[i].collector)
			}
			for i := len(unregistered) - 1; i >= 0; i-- {
				if err := unregistered[i].register.Register(unregistered[i].collector); err != nil {
					errs = append(errs, fmt.Errorf("restore metrics for module %q: %w", unregistered[i].id, err))
				}
			}
			return errors.Join(errs...)
		}
		registered = append(registered, registeredMetrics)
	}

	m.active = next
	return nil
}

func moduleMetricLabel(id string) string {
	_, name, found := strings.Cut(id, ":")
	if !found {
		name = id
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return defaultModuleName
	}
	return name
}

func sortedModuleIDs(modules map[string]runtimeModule) []string {
	ids := make([]string, 0, len(modules))
	for id := range modules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sortedRegisteredMetricIDs(metrics map[string]*registeredModuleMetrics) []string {
	ids := make([]string, 0, len(metrics))
	for id := range metrics {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (m *moduleMetricsManager) Close() {
	if m == nil {
		return
	}
	for _, id := range sortedRegisteredMetricIDs(m.active) {
		registered := m.active[id]
		registered.register.Unregister(registered.collector)
	}
	m.active = map[string]*registeredModuleMetrics{}
}
