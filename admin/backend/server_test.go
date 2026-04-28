package admin

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestNew_ReturnsNonNil(t *testing.T) {
	deps := Deps{
		Version:   "test",
		GoVersion: "go1.24.0",
		StartedAt: time.Now(),
	}
	s := New(deps, Config{})
	if s == nil {
		t.Fatal("expected non-nil server")
	}
	if s.AuditLog() == nil {
		t.Fatal("expected non-nil audit log")
	}
}

func TestServer_StartAndShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	deps := Deps{
		Version:   "test",
		GoVersion: "go1.24.0",
		StartedAt: time.Now(),
	}
	s := New(deps, Config{Addr: addr})

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Start()
	}()

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("healthz request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	startErr := <-errCh
	if startErr != nil && startErr != http.ErrServerClosed {
		t.Fatalf("unexpected start error: %v", startErr)
	}
}

func TestServer_DefaultAddr(t *testing.T) {
	deps := Deps{
		Version:   "test",
		GoVersion: "go1.24.0",
		StartedAt: time.Now(),
	}
	s := New(deps, Config{})
	if s.Addr() != "127.0.0.1:8080" {
		t.Fatalf("expected default addr 127.0.0.1:8080, got %s", s.Addr())
	}
}
