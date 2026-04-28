package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"
)

type MetricsSnapshot struct {
	Timestamp   int64   `json:"timestamp"`
	Goroutines  int     `json:"goroutines"`
	AllocMB     float64 `json:"alloc_mb"`
	SysMB       float64 `json:"sys_mb"`
	NumGC       uint32  `json:"num_gc"`
	KeyCount    int64   `json:"key_count"`
	Hits        int64   `json:"hits"`
	Misses      int64   `json:"misses"`
	SetOps      int64   `json:"set_ops"`
	GetOps      int64   `json:"get_ops"`
	DelOps      int64   `json:"del_ops"`
	ExpiredKeys int64   `json:"expired_keys"`
	EvictedKeys int64   `json:"evicted_keys"`
}

func (h *HTTPHandler) handleMetricsStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "ERR_METHOD_NOT_ALLOWED", "method not allowed")
		return
	}

	current := h.sseConns.Add(1)
	defer h.sseConns.Add(-1)

	if int(current) > h.cfg.MaxSSEConns {
		jsonError(w, http.StatusServiceUnavailable, "ERR_SSE_LIMIT",
			fmt.Sprintf("SSE connection limit reached (%d)", h.cfg.MaxSSEConns))
		return
	}

	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		jsonError(w, http.StatusInternalServerError, "ERR_INTERNAL", "failed to configure SSE connection")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_ = rc.Flush()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap := h.collectMetrics()
			data, err := json.Marshal(snap)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}

func (h *HTTPHandler) collectMetrics() MetricsSnapshot {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	snap := MetricsSnapshot{
		Timestamp:  time.Now().Unix(),
		Goroutines: runtime.NumGoroutine(),
		AllocMB:    float64(mem.Alloc) / (1024 * 1024),
		SysMB:      float64(mem.Sys) / (1024 * 1024),
		NumGC:      mem.NumGC,
	}

	if h.deps.Store != nil {
		ctx := context.Background()
		keyCount, err := h.deps.Store.DBSize(ctx)
		if err == nil {
			snap.KeyCount = keyCount
		}
		stats := h.deps.Store.GetStats()
		snap.Hits = stats.Hits.Load()
		snap.Misses = stats.Misses.Load()
		snap.SetOps = stats.SetOps.Load()
		snap.GetOps = stats.GetOps.Load()
		snap.DelOps = stats.DelOps.Load()
		snap.ExpiredKeys = stats.ExpiredKeys.Load()
		snap.EvictedKeys = stats.EvictedKeys.Load()
	}

	return snap
}
