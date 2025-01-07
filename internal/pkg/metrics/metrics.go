package metrics

import (
	"fmt"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	methodMetricArg = "method"
	partnerNameArg  = "name"
	successArg      = "success"
	targetTypeArg   = "target_type"
	rpcErrorArg     = "rpc_error"
	endpointArg     = "endpoint"
	chainArg        = "chain"
	hostArg         = "host"
)

// See the NewMetrics func for proper descriptions and prometheus names!
// In case you add a metric here later, make sure to include it in the
// MetricsList method or you'll going to have a bad time.
var (
	metrics struct {
		// Gauge
		startTime            prometheus.Gauge
		websocketConnections *prometheus.GaugeVec

		// Counter
		httpResponsesTotal *prometheus.CounterVec
		partnersNodeUsage  *prometheus.CounterVec
		rpcErrors          *prometheus.CounterVec

		// Histogram
		executionTime    *prometheus.HistogramVec
		nodeResponseTime *prometheus.HistogramVec
		nodeAttempts     *prometheus.HistogramVec

		externalRequests *prometheus.HistogramVec
	}
)

// Creates and populates a new Metrics struct
// This is where all the prometheus metrics, names and labels are specified
func init() {
	// Gauge
	initMetric(&metrics.startTime, newGauge("start_time", "api start time"))
	initMetric(&metrics.websocketConnections, newGaugeVec("websocket_connections", "current connection number by chain", []string{chainArg}))

	basicArgs := []string{chainArg, methodMetricArg, successArg}

	// Counter
	initMetric(&metrics.httpResponsesTotal, newCounterVec("http_responses_total", "", []string{chainArg, targetTypeArg, methodMetricArg, successArg}))
	initMetric(&metrics.partnersNodeUsage, newCounterVec("partners_node_usage", "", []string{partnerNameArg, successArg}))
	initMetric(&metrics.rpcErrors, newCounterVec("rpc_errors", "", []string{rpcErrorArg, endpointArg, methodMetricArg}))

	// Histogram
	buckets := []float64{1, 5, 10, 25, 50, 100, 500, 800, 1000, 2000, 4000, 8000, 10000, 15000, 20000, 30000, 50000, 100000, 200000}
	initMetric(&metrics.executionTime, newHistogram("execution_time", "total request execution time", basicArgs, buckets))
	initMetric(&metrics.nodeResponseTime, newHistogram("node_response_time", "the time it took to fetch data from node", basicArgs, buckets))
	initMetric(&metrics.nodeAttempts, newHistogram("node_attempts", "attempts to fetch data from node", basicArgs, []float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}))

	initMetric(&metrics.externalRequests, newHistogram("external_requests", "requests to external services", []string{chainArg, hostArg, methodMetricArg, successArg}, buckets))

}

func initMetric[T prometheus.Collector](dest *T, metric T) {
	*dest = metric
	prometheus.MustRegister(metric)
}

func InitStartTime() {
	metrics.startTime.Set(float64(time.Now().UTC().Unix()))
}

func ObserveExecutionTime(chain, method string, success bool, d time.Duration) {
	l := prometheus.Labels{
		chainArg:        chain,
		methodMetricArg: method,
		successArg:      strconv.FormatBool(success),
	}
	metrics.executionTime.With(l).Observe(float64(d.Milliseconds()))
}

func ObserveNodeResponseTime(chain, method string, success bool, d int64) {
	l := prometheus.Labels{
		chainArg:        chain,
		methodMetricArg: method,
		successArg:      strconv.FormatBool(success),
	}
	metrics.nodeResponseTime.With(l).Observe(float64(d))
}

func ObserveNodeAttempts(chain, method string, success bool, attempts int) {
	l := prometheus.Labels{
		chainArg:        chain,
		methodMetricArg: method,
		successArg:      strconv.FormatBool(success),
	}
	metrics.nodeAttempts.With(l).Observe(float64(attempts))
}

func IncHTTPResponsesTotalCnt(chain, method string, success bool, targetType string) {
	l := prometheus.Labels{
		chainArg:        chain,
		methodMetricArg: method,
		successArg:      strconv.FormatBool(success),
		targetTypeArg:   targetType,
	}
	metrics.httpResponsesTotal.With(l).Inc()
}

func IncPartnerNodeUsage(partnerName string, success bool) {
	l := prometheus.Labels{
		partnerNameArg: partnerName,
		successArg:     strconv.FormatBool(success),
	}
	metrics.partnersNodeUsage.With(l).Inc()
}

func IncWebsocketConnections(chain string) {
	metrics.websocketConnections.With(prometheus.Labels{chainArg: chain}).Inc()
}
func DecWebsocketConnections(chain string) {
	metrics.websocketConnections.With(prometheus.Labels{chainArg: chain}).Dec()
}

func IncRPCErrors(rpcErr int, endpoint, method string) {
	l := prometheus.Labels{
		rpcErrorArg:     fmt.Sprintf("%d", rpcErr),
		endpointArg:     endpoint,
		methodMetricArg: method,
	}
	metrics.rpcErrors.With(l).Inc()
}

func ObserveExternalRequests(chain, host, method string, success bool, d time.Duration) {
	l := prometheus.Labels{
		chainArg:        chain,
		hostArg:         host,
		methodMetricArg: method,
		successArg:      strconv.FormatBool(success),
	}
	metrics.externalRequests.With(l).Observe(float64(d.Milliseconds()))
}
