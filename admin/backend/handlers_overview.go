package admin

import (
	"context"
	"net/http"
	"os"
	"runtime"
	"time"
)

type OverviewResponse struct {
	Node    NodeSection   `json:"node"`
	Build   BuildSection  `json:"build"`
	Memory  MemorySection `json:"memory"`
	Store   StoreSection  `json:"store"`
	Cluster *ClusterBrief `json:"cluster,omitempty"`
}

type NodeSection struct {
	Hostname   string `json:"hostname"`
	PID        int    `json:"pid"`
	Uptime     string `json:"uptime"`
	Goroutines int    `json:"goroutines"`
}

type BuildSection struct {
	Version   string `json:"version"`
	GoVersion string `json:"go_version"`
}

type MemorySection struct {
	AllocMB      float64 `json:"alloc_mb"`
	TotalAllocMB float64 `json:"total_alloc_mb"`
	SysMB        float64 `json:"sys_mb"`
	NumGC        uint32  `json:"num_gc"`
}

type StoreSection struct {
	KeyCount    int64 `json:"key_count"`
	Hits        int64 `json:"hits"`
	Misses      int64 `json:"misses"`
	SetOps      int64 `json:"set_ops"`
	GetOps      int64 `json:"get_ops"`
	DelOps      int64 `json:"del_ops"`
	ExpiredKeys int64 `json:"expired_keys"`
	EvictedKeys int64 `json:"evicted_keys"`
}

type ClusterBrief struct {
	NodeID string `json:"node_id"`
	Role   string `json:"role"`
}

func (h *HTTPHandler) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "ERR_METHOD_NOT_ALLOWED", "method not allowed")
		return
	}

	hostname, _ := os.Hostname()

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	resp := OverviewResponse{
		Node: NodeSection{
			Hostname:   hostname,
			PID:        os.Getpid(),
			Uptime:     time.Since(h.deps.StartedAt).Truncate(time.Second).String(),
			Goroutines: runtime.NumGoroutine(),
		},
		Build: BuildSection{
			Version:   h.deps.Version,
			GoVersion: h.deps.GoVersion,
		},
		Memory: MemorySection{
			AllocMB:      float64(mem.Alloc) / (1024 * 1024),
			TotalAllocMB: float64(mem.TotalAlloc) / (1024 * 1024),
			SysMB:        float64(mem.Sys) / (1024 * 1024),
			NumGC:        mem.NumGC,
		},
	}

	if h.deps.Store != nil {
		ctx := context.Background()
		keyCount, err := h.deps.Store.DBSize(ctx)
		if err == nil {
			resp.Store.KeyCount = keyCount
		}
		stats := h.deps.Store.GetStats()
		resp.Store.Hits = stats.Hits.Load()
		resp.Store.Misses = stats.Misses.Load()
		resp.Store.SetOps = stats.SetOps.Load()
		resp.Store.GetOps = stats.GetOps.Load()
		resp.Store.DelOps = stats.DelOps.Load()
		resp.Store.ExpiredKeys = stats.ExpiredKeys.Load()
		resp.Store.EvictedKeys = stats.EvictedKeys.Load()
	}

	if h.deps.Cluster != nil {
		self := h.deps.Cluster.GetSelf()
		if self != nil {
			resp.Cluster = &ClusterBrief{
				NodeID: self.ID,
				Role:   self.Role.String(),
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}
