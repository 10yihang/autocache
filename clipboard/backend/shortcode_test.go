package clipboard

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"testing"
)

func TestGenerateShortcode(t *testing.T) {
	t.Parallel()

	code, err := GenerateShortcode(bytes.NewReader([]byte{0, 1, 2, 3, 4, 5, 6, 7}))
	if err != nil {
		t.Fatalf("GenerateShortcode() error = %v", err)
	}

	if matched := regexp.MustCompile(`^[a-zA-Z0-9]{8}$`).MatchString(code); !matched {
		t.Fatalf("GenerateShortcode() = %q, want base62 8-char code", code)
	}
}

func TestGenerateShortcodeCollisionRetry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := bytes.NewReader([]byte{
		0, 1, 2, 3, 4, 5, 6, 7,
		8, 9, 10, 11, 12, 13, 14, 15,
	})
	firstCollision := true

	code, err := GenerateUniqueShortcode(ctx, reader, func(_ context.Context, candidate string) (bool, error) {
		if firstCollision {
			firstCollision = false
			return true, nil
		}
		return false, nil
	}, 3)
	if err != nil {
		t.Fatalf("GenerateUniqueShortcode() error = %v", err)
	}

	if code == "abcdefgh" {
		t.Fatalf("GenerateUniqueShortcode() = %q, want retry result", code)
	}
	if len(code) != shortcodeLength {
		t.Fatalf("GenerateUniqueShortcode() len = %d, want %d", len(code), shortcodeLength)
	}
}

func TestGenerateShortcodeCollisionRetryExhausted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := bytes.NewReader(bytes.Repeat([]byte{1}, shortcodeLength*2))

	_, err := GenerateUniqueShortcode(ctx, reader, func(_ context.Context, _ string) (bool, error) {
		return true, nil
	}, 2)
	if !errors.Is(err, ErrShortcodeCollision) {
		t.Fatalf("GenerateUniqueShortcode() error = %v, want %v", err, ErrShortcodeCollision)
	}
}
