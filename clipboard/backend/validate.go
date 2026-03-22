package clipboard

import (
	"errors"
	"slices"
	"strings"
)

const MaxContentBytes = 64 * 1024

var (
	ErrContentRequired         = errors.New("content is required")
	ErrInvalidTTL              = errors.New("ttl must be one of the allowed presets")
	ErrContentTooLarge         = errors.New("content exceeds max size")
	ErrConflictingAccessPolicy = errors.New("burn_after_read conflicts with max_views")
)

var allowedTTLs = []string{"5m", "30m", "1h", "6h", "1d", "7d"}

func AllowedTTLs() []string {
	return append([]string(nil), allowedTTLs...)
}

func ValidateCreateRequest(request CreatePasteRequest) error {
	if strings.TrimSpace(request.Content) == "" {
		return ErrContentRequired
	}

	if request.ContentBytes() > MaxContentBytes {
		return ErrContentTooLarge
	}

	if !slices.Contains(allowedTTLs, request.TTL) {
		return ErrInvalidTTL
	}

	if request.BurnAfterRead && request.HasViewLimit() {
		return ErrConflictingAccessPolicy
	}

	return nil
}
