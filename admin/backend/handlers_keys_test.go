package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	memstore "github.com/10yihang/autocache/internal/engine/memory"
)

func newDangerousHandlerWithStore() *HTTPHandler {
	store := memstore.NewStore(nil)
	deps := Deps{
		Store:     store,
		Version:   "test-v0.0.1",
		GoVersion: "go1.24.0",
		StartedAt: time.Now(),
	}
	cfg := Config{AllowDangerous: true}
	cfg.applyDefaults()
	audit := NewAuditLog(64)
	return NewHTTPHandler(deps, cfg, audit)
}

func TestKeysList_Empty(t *testing.T) {
	h := newTestHandlerWithStore()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp KeyListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 0 {
		t.Fatalf("expected 0 keys, got %d", resp.Count)
	}
}

func TestKeysList_WithKeys(t *testing.T) {
	h := newDangerousHandlerWithStore()
	_ = h.deps.Store.Set(nil, "foo", "bar", 0)
	_ = h.deps.Store.Set(nil, "baz", "qux", 0)
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys?match=*&count=100", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp KeyListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count < 2 {
		t.Fatalf("expected at least 2 keys, got %d", resp.Count)
	}
}

func TestKeysList_MethodNotAllowed(t *testing.T) {
	h := newTestHandlerWithStore()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/keys", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestKeysList_NilStore(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestKeyGet_Found(t *testing.T) {
	h := newDangerousHandlerWithStore()
	_ = h.deps.Store.Set(nil, "testkey", "testval", 0)
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/testkey", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp KeyDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Key != "testkey" {
		t.Fatalf("expected key testkey, got %s", resp.Key)
	}
	if resp.Value != "testval" {
		t.Fatalf("expected value testval, got %s", resp.Value)
	}
	if resp.Type != "string" {
		t.Fatalf("expected type string, got %s", resp.Type)
	}
}

func TestKeyGet_NotFound(t *testing.T) {
	h := newTestHandlerWithStore()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/nonexistent", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestKeyGet_EmptyKeyName(t *testing.T) {
	h := newTestHandlerWithStore()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestKeySet_Success(t *testing.T) {
	h := newDangerousHandlerWithStore()
	routes := h.Routes()

	body := `{"value":"hello","ttl_seconds":60}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/keys/mykey", strings.NewReader(body))
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestKeySet_DangerousDisabled(t *testing.T) {
	h := newTestHandlerWithStore()
	routes := h.Routes()

	body := `{"value":"hello"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/keys/mykey", strings.NewReader(body))
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestKeyDelete_Success(t *testing.T) {
	h := newDangerousHandlerWithStore()
	_ = h.deps.Store.Set(nil, "delme", "val", 0)
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/keys/delme", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestKeyDelete_NotFound(t *testing.T) {
	h := newDangerousHandlerWithStore()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/keys/ghost", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestKeyDelete_DangerousDisabled(t *testing.T) {
	h := newTestHandlerWithStore()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/keys/anything", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestKeyByPath_MethodNotAllowed(t *testing.T) {
	h := newTestHandlerWithStore()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/keys/foo", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}
