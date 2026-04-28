package admin

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	memstore "github.com/10yihang/autocache/internal/engine/memory"
)

func TestMetricsStream_ReceivesData(t *testing.T) {
	store := memstore.NewStore(nil)
	deps := Deps{
		Store:     store,
		Version:   "test",
		GoVersion: "go1.24.0",
		StartedAt: time.Now(),
	}
	cfg := Config{MaxSSEConns: 4}
	cfg.applyDefaults()
	h := NewHTTPHandler(deps, cfg, NewAuditLog(64))

	// Use a real HTTP server because httptest.NewRecorder does not support
	// http.NewResponseController / Flush.
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/metrics/stream", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected text/event-stream, got %s", ct)
	}

	scanner := bufio.NewScanner(resp.Body)
	got := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			got = true
			break
		}
	}
	if !got {
		t.Fatal("did not receive any SSE data event")
	}
}

func TestMetricsStream_MethodNotAllowed(t *testing.T) {
	h := newTestHandler()
	routes := h.Routes()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/metrics/stream", nil)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestMetricsStream_ConcurrencyCap(t *testing.T) {
	store := memstore.NewStore(nil)
	deps := Deps{
		Store:     store,
		Version:   "test",
		GoVersion: "go1.24.0",
		StartedAt: time.Now(),
	}
	// Allow only 1 SSE connection.
	cfg := Config{MaxSSEConns: 1}
	cfg.applyDefaults()
	h := NewHTTPHandler(deps, cfg, NewAuditLog(64))

	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	ctx1, cancel1 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel1()

	req1, _ := http.NewRequestWithContext(ctx1, http.MethodGet, srv.URL+"/api/v1/metrics/stream", nil)
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("first conn: %v", err)
	}
	defer func() { _ = resp1.Body.Close() }()

	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first conn: expected 200, got %d", resp1.StatusCode)
	}

	time.Sleep(100 * time.Millisecond)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()

	req2, _ := http.NewRequestWithContext(ctx2, http.MethodGet, srv.URL+"/api/v1/metrics/stream", nil)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("second conn: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()

	if resp2.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("second conn: expected 503, got %d", resp2.StatusCode)
	}
}
