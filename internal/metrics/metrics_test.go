package metrics

import (
	"testing"
	"time"
)

func TestMetricsRecording(t *testing.T) {
	// Reset/Clear metrics? Prometheus registry is global by default.
	// We can't easily reset, but we can check if values increment.

	// Record Command
	RecordCommand("get", 10*time.Millisecond, true)

	// Record Hit
	RecordCacheHit()

	// Record Miss
	RecordCacheMiss()

	// Record Connection
	RecordConnection(1)

	// Collector
	c := NewCollector()
	c.Collect()

	// Can't easily assert on global prometheus registry without parsing output,
	// but ensuring no panic is a good start for unit tests.
}
