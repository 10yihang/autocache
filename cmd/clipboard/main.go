package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	redis "github.com/redis/go-redis/v9"

	clipboard "github.com/10yihang/autocache/clipboard/backend"
)

var (
	addr               = flag.String("addr", "127.0.0.1:8080", "clipboard HTTP listen address")
	backendAddr        = flag.String("backend-addr", "127.0.0.1:6379", "AutoCache RESP backend address")
	metricsAddr        = flag.String("metrics-addr", "127.0.0.1:9122", "clipboard metrics listen address")
	adminToken         = flag.String("admin-token", "", "admin bearer token")
	baseURL            = flag.String("base-url", "", "public base URL for generated paste links")
	createRateLimit    = flag.Int("create-rate-limit", 30, "create requests per IP per window")
	readRateLimit      = flag.Int("read-rate-limit", 120, "read requests per IP per window")
	rateLimitWindowSec = flag.Int("rate-limit-window-sec", 60, "rate limit window in seconds")
)

func main() {
	flag.Parse()

	client := redis.NewClient(&redis.Options{
		Addr:            *backendAddr,
		Protocol:        2,
		DisableIdentity: true,
	})
	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("close redis client: %v", err)
		}
	}()

	service := clipboard.NewPasteService(client, clipboard.PasteServiceOptions{
		BaseURL: *baseURL,
	})
	middleware := clipboard.NewMiddleware(clipboard.MiddlewareOptions{
		AdminToken:  *adminToken,
		CreateLimit: *createRateLimit,
		ReadLimit:   *readRateLimit,
		Window:      time.Duration(*rateLimitWindowSec) * time.Second,
	})
	handler := clipboard.NewHTTPHandler(service, middleware, clipboard.HTTPHandlerOptions{})

	httpServer := &http.Server{
		Addr:    *addr,
		Handler: handler.Routes(),
	}
	metricsServer := &http.Server{
		Addr:    *metricsAddr,
		Handler: promhttp.Handler(),
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("clipboard server failed: %v", err)
		}
	}()
	go func() {
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("metrics server failed: %v", err)
		}
	}()

	log.Printf("clipboard server listening on %s (backend %s)", *addr, *backendAddr)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown metrics server: %v", err)
	}
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown clipboard server: %v", err)
	}
}
