package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Exporter exposes metrics via HTTP
type Exporter struct {
	addr      string
	collector *Collector
	server    *http.Server
}

// NewExporter creates a metrics exporter
func NewExporter(addr string) *Exporter {
	collector := NewCollector()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	return &Exporter{
		addr:      addr,
		collector: collector,
		server: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
	}
}

// Start starts the exporter
func (e *Exporter) Start() error {
	// Start collector loop
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			e.collector.Collect()
		}
	}()

	return e.server.ListenAndServe()
}

// Stop stops the exporter
func (e *Exporter) Stop() error {
	return e.server.Close()
}
