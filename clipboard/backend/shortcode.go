package clipboard

import (
	"context"
	"errors"
	"fmt"
	"io"
)

const (
	shortcodeAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	shortcodeLength   = 8
)

var ErrShortcodeCollision = errors.New("shortcode collision")

func GenerateShortcode(reader io.Reader) (string, error) {
	buf := make([]byte, shortcodeLength)
	if _, err := io.ReadFull(reader, buf); err != nil {
		return "", fmt.Errorf("read shortcode entropy: %w", err)
	}

	code := make([]byte, shortcodeLength)
	for i, b := range buf {
		code[i] = shortcodeAlphabet[int(b)%len(shortcodeAlphabet)]
	}

	return string(code), nil
}

func GenerateUniqueShortcode(
	ctx context.Context,
	reader io.Reader,
	exists func(context.Context, string) (bool, error),
	maxAttempts int,
) (string, error) {
	if maxAttempts <= 0 {
		return "", ErrShortcodeCollision
	}

	for range maxAttempts {
		code, err := GenerateShortcode(reader)
		if err != nil {
			return "", err
		}

		collision, err := exists(ctx, code)
		if err != nil {
			return "", fmt.Errorf("check shortcode collision: %w", err)
		}
		if !collision {
			return code, nil
		}
	}

	return "", ErrShortcodeCollision
}
