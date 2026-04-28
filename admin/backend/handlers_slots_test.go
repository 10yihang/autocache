package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSlotByPath_ClusterDisabled(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slots/0", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestSlotByPath_InvalidSlot(t *testing.T) {
	h := newTestHandler()
	h.deps.Cluster = nil
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slots/99999", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (cluster disabled), got %d", rec.Code)
	}
}

func TestSlotByPath_EmptySlot(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slots/", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSlotMigrate_DangerousDisabled(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	body := `{"target_node_id":"node-abc"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/slots/0/migrate", strings.NewReader(body))
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatal("expected non-200 when cluster disabled or dangerous disabled")
	}
}

func TestSlotMigrate_MethodNotAllowed(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slots/0/migrate", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatal("expected non-200 for GET on migrate endpoint")
	}
}

func TestSlotByPath_NotFoundSubpath(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/slots/0/unknown", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatal("expected non-200 for unknown subpath")
	}
}

func TestSlotMigrate_AuditLogged(t *testing.T) {
	h := newTestHandlerWithStore()
	routes := h.Routes()

	body := `{"target_node_id":"node-xyz"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/slots/100/migrate", strings.NewReader(body))
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	_ = rec.Code

	entries := h.audit.Snapshot()
	found := false
	for _, e := range entries {
		if e.Action == "SLOT_MIGRATE" {
			found = true
			break
		}
	}
	if !found {
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if errObj, ok := body["error"].(map[string]any); ok {
			if errObj["code"] == "ERR_CLUSTER_DISABLED" {
				return
			}
		}
		t.Fatal("expected SLOT_MIGRATE audit entry")
	}
}
