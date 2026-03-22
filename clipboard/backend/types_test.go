package clipboard

import "testing"

func TestCreatePasteRequestHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		request       CreatePasteRequest
		wantBytes     int
		wantViewLimit bool
		wantViews     int
	}{
		{
			name: "plain text without limit",
			request: CreatePasteRequest{
				Content: "hello",
			},
			wantBytes:     5,
			wantViewLimit: false,
			wantViews:     0,
		},
		{
			name: "unicode content counts utf8 bytes",
			request: CreatePasteRequest{
				Content: "你好",
			},
			wantBytes:     6,
			wantViewLimit: false,
			wantViews:     0,
		},
		{
			name: "positive max views enables limit",
			request: CreatePasteRequest{
				Content:  "hello",
				MaxViews: 3,
			},
			wantBytes:     5,
			wantViewLimit: true,
			wantViews:     3,
		},
		{
			name: "negative max views normalizes to zero",
			request: CreatePasteRequest{
				Content:  "hello",
				MaxViews: -2,
			},
			wantBytes:     5,
			wantViewLimit: false,
			wantViews:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.request.ContentBytes(); got != tt.wantBytes {
				t.Fatalf("ContentBytes()=%d, want %d", got, tt.wantBytes)
			}

			if got := tt.request.HasViewLimit(); got != tt.wantViewLimit {
				t.Fatalf("HasViewLimit()=%v, want %v", got, tt.wantViewLimit)
			}

			if got := tt.request.NormalizedMaxViews(); got != tt.wantViews {
				t.Fatalf("NormalizedMaxViews()=%d, want %d", got, tt.wantViews)
			}
		})
	}
}

func TestPasteMetadataHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		metadata      PasteMetadata
		wantHasLimit  bool
		wantRemaining int
	}{
		{
			name:          "zero remaining views means unlimited by default",
			metadata:      PasteMetadata{},
			wantHasLimit:  false,
			wantRemaining: 0,
		},
		{
			name: "positive remaining views exposed",
			metadata: PasteMetadata{
				RemainingViews: 2,
			},
			wantHasLimit:  true,
			wantRemaining: 2,
		},
		{
			name: "negative remaining views clamp to zero",
			metadata: PasteMetadata{
				RemainingViews: -1,
			},
			wantHasLimit:  false,
			wantRemaining: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.metadata.HasRemainingViews(); got != tt.wantHasLimit {
				t.Fatalf("HasRemainingViews()=%v, want %v", got, tt.wantHasLimit)
			}

			if got := tt.metadata.NormalizedRemainingViews(); got != tt.wantRemaining {
				t.Fatalf("NormalizedRemainingViews()=%d, want %d", got, tt.wantRemaining)
			}
		})
	}
}
