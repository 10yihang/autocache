package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestHandler() *HTTPHandler {
	deps := Deps{
		Version:   "test-v0.0.1",
		GoVersion: "go1.24.0",
		StartedAt: time.Now(),
	}
	cfg := Config{}
	cfg.applyDefaults()
	audit := NewAuditLog(64)
	return NewHTTPHandler(deps, cfg, audit)
}

func TestHealthz_ReturnsOK(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %s", body["status"])
	}
}

func TestOverview_RouteWired(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("expected application/json, got %s", ct)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	for _, key := range []string{"node", "build", "memory", "store"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("expected key %q in overview response, got %v", key, body)
		}
	}
}

func TestSPA_Root_ReturnsHTML(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html content type, got %s", ct)
	}
}

func TestSPA_Keys_ReturnsHTML(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/keys", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html, got %s", ct)
	}
}

func TestSPA_ClusterTopology_ReturnsHTML(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/cluster/topology", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html, got %s", ct)
	}
}

func TestSPA_UnknownDeepPath_Returns404(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/unknown-deep-path", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestAssets_Missing_Returns404(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestSPA_POST_Returns405(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("expected application/json content type, got %s", ct)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error object in response")
	}
	if errObj["code"] != "ERR_METHOD_NOT_ALLOWED" {
		t.Fatalf("expected ERR_METHOD_NOT_ALLOWED, got %v", errObj["code"])
	}
}

func TestSPA_AdminSubRoute_ReturnsHTML(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	for _, path := range []string{"/admin", "/admin/settings", "/admin/audit"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		routes.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("path %s: expected 200, got %d", path, rec.Code)
		}
		ct := rec.Header().Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Fatalf("path %s: expected text/html, got %s", path, ct)
		}
	}
}
