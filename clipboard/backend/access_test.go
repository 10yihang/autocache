package clipboard

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPasteService_MaxViews(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	addr := freeLoopbackAddr(t)
	metricsAddr := freeLoopbackAddr(t)
	serverBinary := buildAutoCacheServerBinary(t)
	serverCmd := startAutoCacheServer(t, ctx, serverBinary, addr, metricsAddr)
	defer stopCommand(t, serverCmd)

	client := newRedisClient(addr)
	defer func() {
		if err := client.Close(); err != nil {
			t.Fatalf("close redis client: %v", err)
		}
	}()

	if err := waitForPing(ctx, client); err != nil {
		t.Fatalf("wait for ping: %v", err)
	}

	service := NewPasteService(client, PasteServiceOptions{
		ShortcodeReader:   bytes.NewReader([]byte{0, 1, 2, 3, 4, 5, 6, 7}),
		ShortcodeAttempts: 2,
	})

	created, err := service.Create(ctx, CreatePasteRequest{
		Content:  "limited",
		TTL:      "1h",
		MaxViews: 2,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	for i := range 2 {
		got, err := service.Get(ctx, created.Code)
		if err != nil {
			t.Fatalf("Get() #%d error = %v", i+1, err)
		}
		if got.Paste.Content != "limited" {
			t.Fatalf("Get() #%d content = %q, want %q", i+1, got.Paste.Content, "limited")
		}
	}

	if _, err := service.Get(ctx, created.Code); !errors.Is(err, ErrPasteNotFound) {
		t.Fatalf("Get() after limit error = %v, want %v", err, ErrPasteNotFound)
	}
}

func TestPasteService_BurnAfterReadConcurrent(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	addr := freeLoopbackAddr(t)
	metricsAddr := freeLoopbackAddr(t)
	serverBinary := buildAutoCacheServerBinary(t)
	serverCmd := startAutoCacheServer(t, ctx, serverBinary, addr, metricsAddr)
	defer stopCommand(t, serverCmd)

	client := newRedisClient(addr)
	defer func() {
		if err := client.Close(); err != nil {
			t.Fatalf("close redis client: %v", err)
		}
	}()

	if err := waitForPing(ctx, client); err != nil {
		t.Fatalf("wait for ping: %v", err)
	}

	service := NewPasteService(client, PasteServiceOptions{
		ShortcodeReader:   bytes.NewReader([]byte{8, 9, 10, 11, 12, 13, 14, 15}),
		ShortcodeAttempts: 2,
	})

	created, err := service.Create(ctx, CreatePasteRequest{
		Content:       "single use",
		TTL:           "1h",
		BurnAfterRead: true,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	var successCount atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.Get(ctx, created.Code); err == nil {
				successCount.Add(1)
			} else if !errors.Is(err, ErrPasteNotFound) {
				t.Errorf("Get() concurrent error = %v, want nil or %v", err, ErrPasteNotFound)
			}
		}()
	}
	wg.Wait()

	if got := successCount.Load(); got != 1 {
		t.Fatalf("successful concurrent reads = %d, want 1", got)
	}
}
