package clipboard

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateCreateRequest_AllowsPresetTTL(t *testing.T) {
	t.Parallel()

	for _, ttl := range AllowedTTLs() {
		t.Run(ttl, func(t *testing.T) {
			err := ValidateCreateRequest(CreatePasteRequest{
				Content: "hello",
				TTL:     ttl,
			})
			if err != nil {
				t.Fatalf("ValidateCreateRequest(%q) error = %v", ttl, err)
			}
		})
	}
}

func TestValidateCreateRequest_RejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request CreatePasteRequest
		wantErr error
	}{
		{
			name: "empty content",
			request: CreatePasteRequest{
				TTL: "1h",
			},
			wantErr: ErrContentRequired,
		},
		{
			name: "unsupported ttl",
			request: CreatePasteRequest{
				Content: "hello",
				TTL:     "999d",
			},
			wantErr: ErrInvalidTTL,
		},
		{
			name: "oversized content",
			request: CreatePasteRequest{
				Content: strings.Repeat("a", MaxContentBytes+1),
				TTL:     "1h",
			},
			wantErr: ErrContentTooLarge,
		},
		{
			name: "burn after read conflicts with max views",
			request: CreatePasteRequest{
				Content:       "hello",
				TTL:           "1h",
				MaxViews:      1,
				BurnAfterRead: true,
			},
			wantErr: ErrConflictingAccessPolicy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCreateRequest(tt.request)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateCreateRequest() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
