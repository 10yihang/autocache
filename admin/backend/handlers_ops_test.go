package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReplicationStatus_NilReplication(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/replication/status", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp ReplicationStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Available {
		t.Fatal("expected available=false when replication is nil")
	}
}

func TestReplicationStatus_MethodNotAllowed(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/replication/status", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestTieredStats_NilTiered(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tiered/stats", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp TieredStatsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Available {
		t.Fatal("expected available=false when tiered is nil")
	}
}

func TestTieredStats_MethodNotAllowed(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tiered/stats", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestAudit_Empty(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp AuditResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(resp.Entries))
	}
}

func TestAudit_WithEntries(t *testing.T) {
	h := newTestHandler()

	h.audit.Record(AuditEntry{
		Timestamp:  time.Now(),
		RemoteAddr: "127.0.0.1",
		User:       "admin",
		Action:     "KEY_SET",
		Target:     "mykey",
		Result:     "200: ok",
	})
	h.audit.Record(AuditEntry{
		Timestamp:  time.Now(),
		RemoteAddr: "127.0.0.1",
		User:       "admin",
		Action:     "KEY_DELETE",
		Target:     "oldkey",
		Result:     "200: deleted",
	})

	routes := h.Routes()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp AuditResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(resp.Entries))
	}
}

func TestAudit_MethodNotAllowed(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/audit", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}
