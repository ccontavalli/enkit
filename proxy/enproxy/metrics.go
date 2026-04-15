package enproxy

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ccontavalli/enkit/proxy/utils"
	"github.com/prometheus/client_golang/prometheus"
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

type moduleMetricsActivation struct {
	manager      *moduleMetricsManager
	next         map[string]*registeredModuleMetrics
	toUnregister []*registeredModuleMetrics
	toRegister   []*registeredModuleMetrics
}

func newModuleMetricsManager(register prometheus.Registerer) *moduleMetricsManager {
	return &moduleMetricsManager{
		register: register,
		active:   map[string]*registeredModuleMetrics{},
	}
}

func (m *moduleMetricsManager) Prepare(modules map[string]runtimeModule) *moduleMetricsActivation {
	activation := &moduleMetricsActivation{}
	if m == nil || m.register == nil {
		return activation
	}

	activation.manager = m
	activation.next = map[string]*registeredModuleMetrics{}

	for _, id := range sortedModuleIDs(modules) {
		module := modules[id]
		current := m.active[id]
		if current != nil && current.module == module {
			activation.next[id] = current
			continue
		}
		if current != nil {
			activation.toUnregister = append(activation.toUnregister, current)
		}

		registered := m.metricsForModule(module)
		if registered == nil {
			continue
		}
		activation.next[id] = registered
		activation.toRegister = append(activation.toRegister, registered)
	}

	for _, id := range sortedRegisteredMetricIDs(m.active) {
		if _, found := modules[id]; !found {
			activation.toUnregister = append(activation.toUnregister, m.active[id])
		}
	}
	return activation
}

func (m *moduleMetricsManager) metricsForModule(module runtimeModule) *registeredModuleMetrics {
	var metrics utils.CounterMetrics
	module.RegisterMetrics(&metrics)
	collector := metrics.Collector()
	if collector == nil {
		return nil
	}

	id := module.ID()
	register := prometheus.WrapRegistererWith(prometheus.Labels{
		"module": moduleMetricLabel(id),
	}, m.register)
	return &registeredModuleMetrics{
		id:        id,
		module:    module,
		register:  register,
		collector: collector,
	}
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

func (a *moduleMetricsActivation) Commit() error {
	if a == nil || a.manager == nil {
		return nil
	}

	unregistered := []*registeredModuleMetrics{}
	for _, registered := range a.toUnregister {
		registered.unregister()
		unregistered = append(unregistered, registered)
	}

	registered := []*registeredModuleMetrics{}
	for _, next := range a.toRegister {
		if err := next.registerMetric(); err != nil {
			err = fmt.Errorf("register metrics for module %q: %w", next.id, err)
			rollbackRegisteredMetrics(registered)
			return errors.Join(err, restoreRegisteredMetrics(unregistered))
		}
		registered = append(registered, next)
	}

	a.manager.active = a.next
	return nil
}

func (m *moduleMetricsManager) Close() {
	if m == nil {
		return
	}
	for _, id := range sortedRegisteredMetricIDs(m.active) {
		m.active[id].unregister()
	}
	m.active = map[string]*registeredModuleMetrics{}
}

func (m *registeredModuleMetrics) registerMetric() error {
	return m.register.Register(m.collector)
}

func (m *registeredModuleMetrics) unregister() {
	m.register.Unregister(m.collector)
}

func rollbackRegisteredMetrics(metrics []*registeredModuleMetrics) {
	for i := len(metrics) - 1; i >= 0; i-- {
		metrics[i].unregister()
	}
}

func restoreRegisteredMetrics(metrics []*registeredModuleMetrics) error {
	var errs []error
	for i := len(metrics) - 1; i >= 0; i-- {
		if err := metrics[i].registerMetric(); err != nil {
			errs = append(errs, fmt.Errorf("restore metrics for module %q: %w", metrics[i].id, err))
		}
	}
	return errors.Join(errs...)
}
