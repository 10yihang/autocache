package clipboard

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClipboardRoutes_CreateReadDeleteHealth(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	service, cleanup := newTestPasteService(t, ctx)
	defer cleanup()

	handler := NewHTTPHandler(service, NewMiddleware(MiddlewareOptions{}), HTTPHandlerOptions{})
	server := httptest.NewServer(handler.Routes())
	defer server.Close()

	body := bytes.NewBufferString(`{"content":"hello defense","ttl":"1h"}`)
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/paste", body)
	if err != nil {
		t.Fatalf("NewRequest(create): %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do(create): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	var created CreatePasteResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("Decode(create): %v", err)
	}
	if created.Code == "" {
		t.Fatal("created code is empty")
	}

	resp, err = http.Get(server.URL + "/api/paste/" + created.Code)
	if err != nil {
		t.Fatalf("Get(read): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var readResp ReadPasteResponse
	if err := json.NewDecoder(resp.Body).Decode(&readResp); err != nil {
		t.Fatalf("Decode(read): %v", err)
	}
	if readResp.Paste.Content != "hello defense" {
		t.Fatalf("read content = %q, want %q", readResp.Paste.Content, "hello defense")
	}

	resp, err = http.Get(server.URL + "/raw/" + created.Code)
	if err != nil {
		t.Fatalf("Get(raw): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("raw status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll(raw): %v", err)
	}
	if string(rawBody) != "hello defense" {
		t.Fatalf("raw body = %q, want %q", rawBody, "hello defense")
	}

	resp, err = http.Get(server.URL + "/p/" + created.Code)
	if err != nil {
		t.Fatalf("Get(share page): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("share page status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	shareBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll(share page): %v", err)
	}
	if !bytes.Contains(shareBody, []byte("id=\"root\"")) {
		t.Fatalf("share page body missing SPA root marker: %q", shareBody)
	}

	req, err = http.NewRequest(http.MethodDelete, server.URL+"/api/paste/"+created.Code, nil)
	if err != nil {
		t.Fatalf("NewRequest(delete): %v", err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do(delete): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	resp, err = http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("Get(health): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestClipboardRoutes_ErrorMappingAndEmptyCode(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	service, cleanup := newTestPasteService(t, ctx)
	defer cleanup()

	handler := NewHTTPHandler(service, NewMiddleware(MiddlewareOptions{}), HTTPHandlerOptions{})
	server := httptest.NewServer(handler.Routes())
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/paste", bytes.NewBufferString(`{"content":"","ttl":"999d"}`))
	if err != nil {
		t.Fatalf("NewRequest(invalid create): %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do(invalid create): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid create status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	resp, err = http.Get(server.URL + "/api/paste/missing123")
	if err != nil {
		t.Fatalf("Get(missing): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	resp, err = http.Get(server.URL + "/api/paste/")
	if err != nil {
		t.Fatalf("Get(empty code): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty code status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	var apiError map[string]map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&apiError); err != nil {
		t.Fatalf("Decode(empty code error): %v", err)
	}
	if apiError["error"]["code"] == "" {
		t.Fatalf("error code should not be empty: %#v", apiError)
	}
}

func newTestPasteService(t *testing.T, ctx context.Context) (*PasteService, func()) {
	t.Helper()

	addr := freeLoopbackAddr(t)
	metricsAddr := freeLoopbackAddr(t)
	serverBinary := buildAutoCacheServerBinary(t)
	serverCmd := startAutoCacheServer(t, ctx, serverBinary, addr, metricsAddr)

	client := newRedisClient(addr)
	if err := waitForPing(ctx, client); err != nil {
		_ = client.Close()
		stopCommand(t, serverCmd)
		t.Fatalf("wait for ping: %v", err)
	}

	service := NewPasteService(client, PasteServiceOptions{
		BaseURL:           "http://clipboard.test",
		ShortcodeReader:   bytes.NewReader([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}),
		ShortcodeAttempts: 4,
	})

	cleanup := func() {
		if err := client.Close(); err != nil {
			t.Fatalf("close redis client: %v", err)
		}
		stopCommand(t, serverCmd)
	}

	return service, cleanup
}
