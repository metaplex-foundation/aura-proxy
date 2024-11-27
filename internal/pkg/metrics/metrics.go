package metrics

import (
	"fmt"
	"strconv"
	"time"

	"github.com/labstack/echo-contrib/prometheus"
	prom "github.com/prometheus/client_golang/prometheus"
)

const (
	methodMetricArg = "method"
	partnerNameArg  = "name"
	successArg      = "success"
	targetTypeArg   = "target_type"
	rpcErrorArg     = "rpc_error"
	endpointArg     = "endpoint"
	chainArg        = "chain"
)

// See the NewMetrics func for proper descriptions and prometheus names!
// In case you add a metric here later, make sure to include it in the
// MetricsList method or you'll going to have a bad time.
var (
	metrics struct {
		// Gauge
		startTime            *prometheus.Metric
		websocketConnections *prometheus.Metric

		// Counter
		httpResponsesTotal *prometheus.Metric
		partnersNodeUsage  *prometheus.Metric
		rpcErrors          *prometheus.Metric

		// Histogram
		executionTime    *prometheus.Metric
		nodeResponseTime *prometheus.Metric
		nodeAttempts     *prometheus.Metric
	}

	metricList []*prometheus.Metric
)

// Creates and populates a new Metrics struct
// This is where all the prometheus metrics, names and labels are specified
func init() {
	// Gauge
	initMetric(&metrics.startTime, newGauge(
		"startTime",
		"start_time",
		"api start time",
	))
	initMetric(&metrics.websocketConnections, newGaugeVec(
		"websocketConnections",
		"websocket_connections",
		"current connection number by chain",
		[]string{chainArg},
	))

	basicArgs := []string{chainArg, methodMetricArg, successArg}

	// Counter
	initMetric(&metrics.httpResponsesTotal, newCounterVec(
		"httpResponsesTotal",
		"http_responses_total",
		"",
		[]string{chainArg, targetTypeArg, methodMetricArg, successArg},
	))
	initMetric(&metrics.partnersNodeUsage, newCounterVec(
		"partnersNodeUsage",
		"partners_node_usage",
		"",
		[]string{partnerNameArg, successArg},
	))
	initMetric(&metrics.rpcErrors, newCounterVec(
		"rpcErrors",
		"rpc_errors",
		"",
		[]string{rpcErrorArg, endpointArg, methodMetricArg},
	))

	// Histogram
	initMetric(&metrics.executionTime, newHistogram(
		"executionTime",
		"execution_time",
		"total request execution time",
		basicArgs,
		[]float64{1, 5, 10, 25, 50, 100, 500, 800, 1000, 2000, 4000, 8000, 10000, 15000, 20000, 30000, 50000, 100000, 200000},
	))
	initMetric(&metrics.nodeResponseTime, newHistogram(
		"nodeResponseTime",
		"node_response_time",
		"the time it took to fetch data from node",
		basicArgs,
		[]float64{1, 5, 10, 25, 50, 100, 500, 800, 1000, 2000, 4000, 8000, 10000, 15000, 20000, 30000, 50000, 100000, 200000},
	))
	initMetric(&metrics.nodeAttempts, newHistogram(
		"nodeAttempts",
		"node_attempts",
		"attempts to fetch data from node",
		basicArgs,
		[]float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	))
}

func initMetric(dest **prometheus.Metric, metric *prometheus.Metric) {
	*dest = metric
	metricList = append(metricList, metric)
}

// Needed by echo-contrib so echo can register and collect these metrics
func MetricList() []*prometheus.Metric {
	return metricList
}

func InitStartTime() {
	metrics.startTime.MetricCollector.(prom.Gauge).Set(float64(time.Now().UTC().Unix()))
}

func ObserveExecutionTime(chain, method string, success bool, d time.Duration) {
	l := prom.Labels{
		chainArg:        chain,
		methodMetricArg: method,
		successArg:      strconv.FormatBool(success),
	}
	metrics.executionTime.MetricCollector.(*prom.HistogramVec).With(l).Observe(float64(d.Milliseconds()))
}

func ObserveNodeResponseTime(chain, method string, success bool, d int64) {
	l := prom.Labels{
		chainArg:        chain,
		methodMetricArg: method,
		successArg:      strconv.FormatBool(success),
	}
	metrics.nodeResponseTime.MetricCollector.(*prom.HistogramVec).With(l).Observe(float64(d))
}

func ObserveNodeAttempts(chain, method string, success bool, attempts int) {
	l := prom.Labels{
		chainArg:        chain,
		methodMetricArg: method,
		successArg:      strconv.FormatBool(success),
	}
	metrics.nodeAttempts.MetricCollector.(*prom.HistogramVec).With(l).Observe(float64(attempts))
}

func IncHTTPResponsesTotalCnt(chain, method string, success bool, targetType string) {
	l := prom.Labels{
		chainArg:        chain,
		methodMetricArg: method,
		successArg:      strconv.FormatBool(success),
		targetTypeArg:   targetType,
	}
	metrics.httpResponsesTotal.MetricCollector.(*prom.CounterVec).With(l).Inc()
}

func IncPartnerNodeUsage(partnerName string, success bool) {
	l := prom.Labels{
		partnerNameArg: partnerName,
		successArg:     strconv.FormatBool(success),
	}
	metrics.partnersNodeUsage.MetricCollector.(*prom.CounterVec).With(l).Inc()
}

func IncWebsocketConnections(chain string) {
	metrics.websocketConnections.MetricCollector.(*prom.GaugeVec).With(prom.Labels{chainArg: chain}).Inc()
}
func DecWebsocketConnections(chain string) {
	metrics.websocketConnections.MetricCollector.(*prom.GaugeVec).With(prom.Labels{chainArg: chain}).Dec()
}

func IncRPCErrors(rpcErr int, endpoint, method string) {
	l := prom.Labels{
		rpcErrorArg:     fmt.Sprintf("%d", rpcErr),
		endpointArg:     endpoint,
		methodMetricArg: method,
	}
	metrics.rpcErrors.MetricCollector.(*prom.CounterVec).With(l).Inc()
}
