package clipboard

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdminAuthMiddleware(t *testing.T) {
	t.Parallel()

	m := NewMiddleware(MiddlewareOptions{AdminToken: "test-token"})
	handler := m.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/stats", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", res.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/stats", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("authorized status = %d, want %d", res.Code, http.StatusOK)
	}
}

func TestPayloadGuardRejectsInvalidContentTypeAndOversize(t *testing.T) {
	t.Parallel()

	m := NewMiddleware(MiddlewareOptions{})
	handler := m.RequireJSONBody(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), MaxContentBytes)

	req := httptest.NewRequest(http.MethodPost, "/api/paste", strings.NewReader(`{"content":"hello","ttl":"1h"}`))
	req.Header.Set("Content-Type", "text/plain")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("invalid content type status = %d, want %d", res.Code, http.StatusUnsupportedMediaType)
	}

	oversized := strings.NewReader(`{"content":"` + strings.Repeat("a", MaxContentBytes+1) + `","ttl":"1h"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/paste", oversized)
	req.Header.Set("Content-Type", "application/json")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized status = %d, want %d", res.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	t.Parallel()

	m := NewMiddleware(MiddlewareOptions{
		CreateLimit: 2,
		ReadLimit:   2,
		Window:      time.Minute,
		Now: func() time.Time {
			return time.Date(2026, time.March, 22, 19, 0, 0, 0, time.UTC)
		},
	})

	handler := m.RateLimit("create", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := range 2 {
		req := httptest.NewRequest(http.MethodPost, "/api/paste", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want %d", i+1, res.Code, http.StatusOK)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/paste", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusTooManyRequests {
		t.Fatalf("limited status = %d, want %d", res.Code, http.StatusTooManyRequests)
	}
	if body, _ := io.ReadAll(res.Result().Body); !strings.Contains(string(body), "rate limit") {
		t.Fatalf("limited body = %q, want rate limit message", body)
	}
}
