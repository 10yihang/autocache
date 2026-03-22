package clipboard

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestPasteService_CreateAndGet(t *testing.T) {
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

	now := time.Date(2026, time.March, 22, 18, 30, 0, 0, time.UTC)
	service := NewPasteService(client, PasteServiceOptions{
		BaseURL:           "https://clip.example",
		ShortcodeReader:   bytes.NewReader([]byte{0, 1, 2, 3, 4, 5, 6, 7}),
		Now:               func() time.Time { return now },
		ShortcodeAttempts: 2,
	})

	created, err := service.Create(ctx, CreatePasteRequest{
		Content:  "hello defense",
		TTL:      "1h",
		MaxViews: 3,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if created.Code == "" {
		t.Fatalf("Create() returned empty code")
	}
	if created.ShareURL == "" {
		t.Fatalf("Create() returned empty share URL")
	}
	if created.RawURL == "" {
		t.Fatalf("Create() returned empty raw URL")
	}

	got, err := service.Get(ctx, created.Code)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.Paste.Content != "hello defense" {
		t.Fatalf("Get().Paste.Content = %q, want %q", got.Paste.Content, "hello defense")
	}
	if got.Paste.Metadata.TTL != "1h" {
		t.Fatalf("Get().Paste.Metadata.TTL = %q, want %q", got.Paste.Metadata.TTL, "1h")
	}
	if got.Paste.Metadata.RemainingViews != 3 {
		t.Fatalf("Get().Paste.Metadata.RemainingViews = %d, want 3", got.Paste.Metadata.RemainingViews)
	}
	if got.Paste.Metadata.CreatedAt != now {
		t.Fatalf("Get().Paste.Metadata.CreatedAt = %v, want %v", got.Paste.Metadata.CreatedAt, now)
	}
}

func TestPasteService_ExpiredOrMissing(t *testing.T) {
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

	service := NewPasteService(client, PasteServiceOptions{})

	if _, err := service.Get(ctx, "missing123"); !errors.Is(err, ErrPasteNotFound) {
		t.Fatalf("Get(missing) error = %v, want %v", err, ErrPasteNotFound)
	}

	if err := client.HSet(ctx, "paste:expired123", "content", "soon gone", "ttl", "1s", "created_at", time.Now().UTC().Format(time.RFC3339Nano), "expires_at", time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano)).Err(); err != nil {
		t.Fatalf("HSet(expired): %v", err)
	}
	if err := client.Expire(ctx, "paste:expired123", time.Second).Err(); err != nil {
		t.Fatalf("Expire(expired): %v", err)
	}

	time.Sleep(1200 * time.Millisecond)

	if _, err := service.Get(ctx, "expired123"); !errors.Is(err, ErrPasteNotFound) {
		t.Fatalf("Get(expired) error = %v, want %v", err, ErrPasteNotFound)
	}
}
