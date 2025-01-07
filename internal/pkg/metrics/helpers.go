package metrics

import "github.com/prometheus/client_golang/prometheus"

func newGauge(name, description string) prometheus.Gauge {
	return prometheus.NewGauge(prometheus.GaugeOpts{
		Name: name,
		Help: description,
	})
}

func newGaugeVec(name, description string, labels []string) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: name,
		Help: description,
	}, labels)
}

func newCounter(name, description string) prometheus.Counter {
	return prometheus.NewCounter(prometheus.CounterOpts{
		Name: name,
		Help: description,
	})
}

func newCounterVec(name, description string, labels []string) *prometheus.CounterVec {
	return prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: name,
		Help: description,
	}, labels)
}

func newHistogram(name, description string, labels []string, buckets []float64) *prometheus.HistogramVec {
	return prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    name,
		Help:    description,
		Buckets: buckets,
	}, labels)
}
