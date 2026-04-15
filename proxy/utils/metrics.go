package utils

import "github.com/prometheus/client_golang/prometheus"

// MetricRegistry collects metric descriptions and their backing counters.
type MetricRegistry interface {
	Counter(desc *prometheus.Desc, counter *Counter)
}

type CounterMetric struct {
	Desc    *prometheus.Desc
	Counter *Counter
}

type CounterMetrics []CounterMetric

func (m *CounterMetrics) Counter(desc *prometheus.Desc, counter *Counter) {
	if desc == nil || counter == nil {
		return
	}
	*m = append(*m, CounterMetric{
		Desc:    desc,
		Counter: counter,
	})
}

func (m CounterMetrics) Collector() prometheus.Collector {
	if len(m) == 0 {
		return nil
	}
	return counterMetricsCollector(m)
}

type counterMetricsCollector []CounterMetric

func (c counterMetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, metric := range c {
		ch <- metric.Desc
	}
}

func (c counterMetricsCollector) Collect(ch chan<- prometheus.Metric) {
	for _, metric := range c {
		ch <- prometheus.MustNewConstMetric(
			metric.Desc,
			prometheus.CounterValue,
			float64(metric.Counter.Get()),
		)
	}
}
