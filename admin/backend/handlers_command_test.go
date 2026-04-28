package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCommand_POST_Returns501(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/command", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error object")
	}
	if errObj["code"] != "ERR_NOT_IMPLEMENTED" {
		t.Fatalf("expected ERR_NOT_IMPLEMENTED, got %v", errObj["code"])
	}
}

func TestCommand_GET_Returns405(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/command", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestCommand_AuditLogged(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/command", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", rec.Code)
	}

	entries := h.audit.Snapshot()
	if len(entries) == 0 {
		t.Fatal("expected audit entry for command attempt")
	}

	found := false
	for _, e := range entries {
		if e.Action == "COMMAND_EXEC" && e.Target == "/api/v1/command" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected COMMAND_EXEC audit entry")
	}
}
