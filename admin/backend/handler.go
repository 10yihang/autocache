package admin

import (
	"io/fs"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/10yihang/autocache/internal/cluster"
)

type HTTPHandler struct {
	deps      Deps
	cfg       Config
	audit     *AuditLog
	staticFS  fs.FS
	indexPath string
	sseConns  atomic.Int32
}

func NewHTTPHandler(deps Deps, cfg Config, audit *AuditLog) *HTTPHandler {
	assetsFS, indexPath := resolveStaticFS(StaticFS())
	return &HTTPHandler{
		deps:      deps,
		cfg:       cfg,
		audit:     audit,
		staticFS:  assetsFS,
		indexPath: indexPath,
	}
}

func (h *HTTPHandler) Routes() http.Handler {
	mux := http.NewServeMux()
	chain := func(handler http.Handler) http.Handler {
		return recoverPanic(logRequest(limitBody(h.cfg.MaxRequestBytes, basicAuth(h.cfg, handler))))
	}

	mux.Handle("/healthz", chain(http.HandlerFunc(h.handleHealthz)))

	mux.Handle("/api/v1/overview", chain(http.HandlerFunc(h.handleOverview)))
	mux.Handle("/api/v1/cluster/info", chain(http.HandlerFunc(h.handleClusterInfo)))
	mux.Handle("/api/v1/cluster/nodes", chain(http.HandlerFunc(h.handleClusterNodes)))
	mux.Handle("/api/v1/cluster/slots", chain(http.HandlerFunc(h.handleClusterSlots)))
	mux.Handle("/api/v1/cluster/overview", chain(http.HandlerFunc(h.handleClusterOverview)))
	mux.Handle("/api/v1/keys", chain(http.HandlerFunc(h.handleKeysList)))
	mux.Handle("/api/v1/keys/", chain(http.HandlerFunc(h.handleKeyByPath)))
	mux.Handle("/api/v1/slots/", chain(http.HandlerFunc(h.handleSlotByPath)))
	mux.Handle("/api/v1/command", chain(http.HandlerFunc(h.handleCommand)))
	mux.Handle("/api/v1/replication/status", chain(http.HandlerFunc(h.handleReplicationStatus)))
	mux.Handle("/api/v1/rebalance/status", chain(http.HandlerFunc(h.handleRebalanceStatus)))
	mux.Handle("/api/v1/tiered/stats", chain(http.HandlerFunc(h.handleTieredStats)))
	mux.Handle("/api/v1/hotspots", chain(http.HandlerFunc(h.handleHotspots)))
	mux.Handle("/api/v1/audit", chain(http.HandlerFunc(h.handleAudit)))
	mux.Handle("/api/v1/metrics/stream", chain(http.HandlerFunc(h.handleMetricsStream)))

	mux.Handle("/assets/", chain(http.StripPrefix("/", http.FileServer(http.FS(h.staticFS)))))
	mux.Handle("/", chain(http.HandlerFunc(h.handleSPA)))

	return mux
}

func (h *HTTPHandler) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *HTTPHandler) handleHotspots(w http.ResponseWriter, _ *http.Request) {
	if h.deps.Hotspot == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"enabled":     false,
			"hot_slots":   []interface{}{},
			"sample_rate": "1s",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":     true,
		"hot_slots":   h.deps.Hotspot.HotSlots(),
		"sample_rate": "1s",
	})
}

func (h *HTTPHandler) handleRebalanceStatus(w http.ResponseWriter, _ *http.Request) {
	var nodes []map[string]interface{}
	if h.deps.Cluster != nil {
		for _, n := range h.deps.Cluster.GetNodes() {
			if n.Role == cluster.NodeRoleMaster && n.State == cluster.NodeStateConnected {
				nodes = append(nodes, map[string]interface{}{
					"id":        n.ID,
					"addr":      n.Addr(),
					"total_qps": n.Load.TotalQPS,
					"hot_slots": n.Load.HotSlotCount,
				})
			}
		}
	}
	hot := h.deps.Hotspot.HotSlots()
	response := map[string]interface{}{
		"hot_slots": hot,
		"peers":     nodes,
	}
	if len(hot) > 0 {
		response["rebalance_candidates"] = len(hot)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *HTTPHandler) handleNotImplemented(w http.ResponseWriter, _ *http.Request) {
	jsonError(w, http.StatusNotImplemented, "ERR_NOT_IMPLEMENTED", "this endpoint is not yet implemented")
}

func (h *HTTPHandler) handleSPA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "ERR_METHOD_NOT_ALLOWED", "method not allowed")
		return
	}

	if !isSPARoute(r.URL.Path) {
		jsonError(w, http.StatusNotFound, "ERR_NOT_FOUND", "not found")
		return
	}

	data, err := fs.ReadFile(h.staticFS, h.indexPath)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "ERR_INTERNAL", "index not available")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func isSPARoute(path string) bool {
	if path == "/" {
		return true
	}

	prefixes := []string{"/admin", "/overview", "/keys", "/cluster", "/metrics", "/console", "/slots", "/ops"}
	for _, prefix := range prefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}

	return false
}

func resolveStaticFS(staticFiles fs.FS) (fs.FS, string) {
	if _, err := fs.Stat(staticFiles, "embed/index.html"); err == nil {
		embedFS, subErr := fs.Sub(staticFiles, "embed")
		if subErr == nil {
			return embedFS, "index.html"
		}
	}

	return staticFiles, "index.html"
}
