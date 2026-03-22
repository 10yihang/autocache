package clipboard

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const netcutNamespace = "netcut"

var (
	netcutHTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: netcutNamespace,
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests served by netcut",
		},
		[]string{"method", "route", "status_class"},
	)
	netcutHTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: netcutNamespace,
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request latency in seconds for netcut",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5},
		},
		[]string{"method", "route"},
	)
	netcutHTTPRequestErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: netcutNamespace,
			Name:      "http_request_errors_total",
			Help:      "Total number of HTTP requests resulting in an error outcome",
		},
		[]string{"method", "route", "error_type"},
	)
	netcutAccessTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: netcutNamespace,
			Name:      "access_total",
			Help:      "Total number of netcut business events",
		},
		[]string{"action"},
	)
	netcutRateLimitedRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: netcutNamespace,
			Name:      "rate_limited_requests_total",
			Help:      "Total number of rate limited requests",
		},
		[]string{"endpoint"},
	)
)

func instrumentHTTP(next http.Handler, route string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		statusClass := strconv.Itoa(recorder.status/100) + "xx"
		netcutHTTPRequestsTotal.WithLabelValues(r.Method, route, statusClass).Inc()
		netcutHTTPRequestDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())

		errorType := classifyHTTPError(recorder.status)
		if errorType != "none" {
			netcutHTTPRequestErrorsTotal.WithLabelValues(r.Method, route, errorType).Inc()
		}
	})
}

func recordNetcutAccess(action string) {
	netcutAccessTotal.WithLabelValues(action).Inc()
}

func recordNetcutRateLimit(endpoint string) {
	netcutRateLimitedRequestsTotal.WithLabelValues(endpoint).Inc()
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func classifyHTTPError(status int) string {
	switch {
	case status >= http.StatusInternalServerError:
		return "server_error"
	case status >= http.StatusBadRequest:
		return "client_error"
	default:
		return "none"
	}
}
