package clipboard

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdminStatsRoute(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	service, cleanup := newTestPasteService(t, ctx)
	defer cleanup()

	_, err := service.Create(ctx, CreatePasteRequest{Content: "admin-visible", TTL: "1h"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	middleware := NewMiddleware(MiddlewareOptions{AdminToken: "test-token"})
	handler := NewHTTPHandler(service, middleware, HTTPHandlerOptions{})
	server := httptest.NewServer(handler.Routes())
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/admin/stats", nil)
	if err != nil {
		t.Fatalf("NewRequest(admin stats): %v", err)
	}
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do(admin stats): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin stats status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var stats AdminStatsDTO
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("Decode(admin stats): %v", err)
	}
	if stats.Usage.PastesCreatedTotal < 1 {
		t.Fatalf("pastes_created_total = %d, want >= 1", stats.Usage.PastesCreatedTotal)
	}
}

func TestAdminDashboardAuth(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	service, cleanup := newTestPasteService(t, ctx)
	defer cleanup()

	middleware := NewMiddleware(MiddlewareOptions{AdminToken: "test-token"})
	handler := NewHTTPHandler(service, middleware, HTTPHandlerOptions{})
	server := httptest.NewServer(handler.Routes())
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/admin", bytes.NewBuffer(nil))
	if err != nil {
		t.Fatalf("NewRequest(admin): %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do(admin route): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin route status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll(admin): %v", err)
	}
	if !strings.Contains(string(body), "id=\"root\"") {
		t.Fatalf("admin page body missing root marker: %q", body)
	}

	resp, err = http.Get(server.URL + "/admin/pastes")
	if err != nil {
		t.Fatalf("Get(unauthorized admin list): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized admin list status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestAdminStatsDoNotConsumePasteState(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	service, cleanup := newTestPasteService(t, ctx)
	defer cleanup()

	created, err := service.Create(ctx, CreatePasteRequest{Content: "keep me", TTL: "1h", MaxViews: 2})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	middleware := NewMiddleware(MiddlewareOptions{AdminToken: "test-token"})
	handler := NewHTTPHandler(service, middleware, HTTPHandlerOptions{})
	server := httptest.NewServer(handler.Routes())
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/admin/stats", nil)
	if err != nil {
		t.Fatalf("NewRequest(admin stats): %v", err)
	}
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do(admin stats): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin stats status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	readResp, err := service.Get(ctx, created.Code)
	if err != nil {
		t.Fatalf("Get() after admin stats error = %v", err)
	}
	if readResp.Paste.Metadata.RemainingViews != 2 {
		t.Fatalf("remaining views after admin stats = %d, want 2", readResp.Paste.Metadata.RemainingViews)
	}
}
