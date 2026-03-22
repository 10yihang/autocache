package clipboard

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

func TestNetcutMetricsRegistered(t *testing.T) {
	netcutHTTPRequestsTotal.WithLabelValues(http.MethodGet, "/health", "2xx").Add(0)
	netcutHTTPRequestDuration.WithLabelValues(http.MethodGet, "/health").Observe(0)
	netcutHTTPRequestErrorsTotal.WithLabelValues(http.MethodGet, "/health", "client_error").Add(0)
	netcutAccessTotal.WithLabelValues("paste_read").Add(0)
	netcutRateLimitedRequestsTotal.WithLabelValues("read").Add(0)

	wantMetrics := []string{
		"netcut_http_requests_total",
		"netcut_http_request_duration_seconds",
		"netcut_http_request_errors_total",
		"netcut_access_total",
		"netcut_rate_limited_requests_total",
	}

	got := gatherClipboardMetricNames(t)
	for _, want := range wantMetrics {
		if _, ok := got[want]; !ok {
			t.Fatalf("missing metric %q", want)
		}
	}
}

func TestNetcutHTTPMetricsObserveRequests(t *testing.T) {
	handler := instrumentHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}), "/api/paste")

	beforeRequests := testutil.ToFloat64(netcutHTTPRequestsTotal.WithLabelValues(http.MethodPost, "/api/paste", "2xx"))
	beforeErrors := testutil.ToFloat64(netcutHTTPRequestErrorsTotal.WithLabelValues(http.MethodPost, "/api/paste", "server_error"))
	beforeDuration := histogramSampleCount(t, "netcut_http_request_duration_seconds", "route", "/api/paste")

	req := httptest.NewRequest(http.MethodPost, "/api/paste", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	afterRequests := testutil.ToFloat64(netcutHTTPRequestsTotal.WithLabelValues(http.MethodPost, "/api/paste", "2xx"))
	afterErrors := testutil.ToFloat64(netcutHTTPRequestErrorsTotal.WithLabelValues(http.MethodPost, "/api/paste", "server_error"))
	afterDuration := histogramSampleCount(t, "netcut_http_request_duration_seconds", "route", "/api/paste")

	if afterRequests != beforeRequests+1 {
		t.Fatalf("request counter = %v, want %v", afterRequests, beforeRequests+1)
	}
	if afterErrors != beforeErrors {
		t.Fatalf("request error counter = %v, want %v", afterErrors, beforeErrors)
	}
	if afterDuration <= beforeDuration {
		t.Fatalf("request duration sample count = %d, want > %d", afterDuration, beforeDuration)
	}
}

func TestNetcutBusinessMetricsObserveAccessAndRateLimits(t *testing.T) {
	beforeCreate := testutil.ToFloat64(netcutAccessTotal.WithLabelValues("paste_create"))
	beforeRead := testutil.ToFloat64(netcutAccessTotal.WithLabelValues("paste_read"))
	beforeRateLimit := testutil.ToFloat64(netcutRateLimitedRequestsTotal.WithLabelValues("read"))

	recordNetcutAccess("paste_create")
	recordNetcutAccess("paste_read")
	recordNetcutRateLimit("read")

	afterCreate := testutil.ToFloat64(netcutAccessTotal.WithLabelValues("paste_create"))
	afterRead := testutil.ToFloat64(netcutAccessTotal.WithLabelValues("paste_read"))
	afterRateLimit := testutil.ToFloat64(netcutRateLimitedRequestsTotal.WithLabelValues("read"))

	if afterCreate != beforeCreate+1 {
		t.Fatalf("create access counter = %v, want %v", afterCreate, beforeCreate+1)
	}
	if afterRead != beforeRead+1 {
		t.Fatalf("read access counter = %v, want %v", afterRead, beforeRead+1)
	}
	if afterRateLimit != beforeRateLimit+1 {
		t.Fatalf("rate limit counter = %v, want %v", afterRateLimit, beforeRateLimit+1)
	}
}

func gatherClipboardMetricNames(t *testing.T) map[string]struct{} {
	t.Helper()

	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	names := make(map[string]struct{}, len(mfs))
	for _, mf := range mfs {
		names[mf.GetName()] = struct{}{}
	}

	return names
}

func histogramSampleCount(t *testing.T, metricName, labelName, labelValue string) uint64 {
	t.Helper()

	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, metric := range mf.GetMetric() {
			if hasLabel(metric, labelName, labelValue) {
				return metric.GetHistogram().GetSampleCount()
			}
		}
	}
	return 0
}

func hasLabel(metric *dto.Metric, name, value string) bool {
	for _, label := range metric.GetLabel() {
		if label.GetName() == name && label.GetValue() == value {
			return true
		}
	}
	return false
}
