package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "steadily_requests_total",
			Help: "Total number of HTTP/TCP requests routed by Steadily",
		},
		[]string{"backend", "outcome"},
	)

	ActiveBackends = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "steadily_active_backends",
			Help: "Number of currently healthy backends",
		},
		[]string{"group"},
	)

	HealthCheckDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "steadily_health_check_duration_seconds",
			Help:    "Duration of health checks in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"backend", "status"},
	)

	InFlightRequests = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "steadily_in_flight_requests",
			Help: "Current in-flight requests per backend",
		},
		[]string{"backend"},
	)
)

func init() {
	prometheus.MustRegister(RequestsTotal)
	prometheus.MustRegister(ActiveBackends)
	prometheus.MustRegister(HealthCheckDuration)
	prometheus.MustRegister(InFlightRequests)
}

func Handler() http.Handler {
	return promhttp.Handler()
}
