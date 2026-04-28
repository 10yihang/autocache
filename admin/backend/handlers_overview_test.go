package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	memstore "github.com/10yihang/autocache/internal/engine/memory"
)

func newTestHandlerWithStore() *HTTPHandler {
	store := memstore.NewStore(nil)
	deps := Deps{
		Store:     store,
		Version:   "test-v0.0.1",
		GoVersion: "go1.24.0",
		StartedAt: time.Now(),
	}
	cfg := Config{}
	cfg.applyDefaults()
	audit := NewAuditLog(64)
	return NewHTTPHandler(deps, cfg, audit)
}

func TestOverview_ReturnsOK(t *testing.T) {
	h := newTestHandlerWithStore()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp OverviewResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Build.Version != "test-v0.0.1" {
		t.Fatalf("expected version test-v0.0.1, got %s", resp.Build.Version)
	}
	if resp.Build.GoVersion != "go1.24.0" {
		t.Fatalf("expected go version go1.24.0, got %s", resp.Build.GoVersion)
	}
	if resp.Node.PID == 0 {
		t.Fatal("expected non-zero PID")
	}
	if resp.Node.Goroutines == 0 {
		t.Fatal("expected non-zero goroutines")
	}
	if resp.Memory.SysMB == 0 {
		t.Fatal("expected non-zero sys_mb")
	}
}

func TestOverview_NilStore(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestOverview_MethodNotAllowed(t *testing.T) {
	h := newTestHandlerWithStore()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/overview", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestOverview_NilCluster(t *testing.T) {
	h := newTestHandlerWithStore()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	var resp OverviewResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Cluster != nil {
		t.Fatal("expected nil cluster brief when cluster is disabled")
	}
}

func TestOverview_StoreStats(t *testing.T) {
	store := memstore.NewStore(nil)
	_ = store.Set(nil, "key1", "val1", 0)
	_ = store.Set(nil, "key2", "val2", 0)

	deps := Deps{
		Store:     store,
		Version:   "v1",
		GoVersion: "go1.24.0",
		StartedAt: time.Now(),
	}
	cfg := Config{}
	cfg.applyDefaults()
	h := NewHTTPHandler(deps, cfg, NewAuditLog(64))
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	var resp OverviewResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Store.KeyCount != 2 {
		t.Fatalf("expected 2 keys, got %d", resp.Store.KeyCount)
	}
	if resp.Store.SetOps < 2 {
		t.Fatalf("expected at least 2 set ops, got %d", resp.Store.SetOps)
	}
}
