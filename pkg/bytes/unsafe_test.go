package bytes

import (
	"testing"
)

func TestStringToBytes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []byte
	}{
		{"empty", "", nil},
		{"simple", "hello", []byte("hello")},
		{"unicode", "日本語", []byte("日本語")},
		{"with spaces", "hello world", []byte("hello world")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StringToBytes(tt.input)
			if tt.input == "" {
				if got != nil {
					t.Errorf("StringToBytes(%q) = %v, want nil", tt.input, got)
				}
				return
			}
			if string(got) != tt.input {
				t.Errorf("StringToBytes(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBytesToString(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{"nil", nil, ""},
		{"empty", []byte{}, ""},
		{"simple", []byte("hello"), "hello"},
		{"unicode", []byte("日本語"), "日本語"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BytesToString(tt.input)
			if got != tt.want {
				t.Errorf("BytesToString(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	original := "test string for round trip"
	bytes := StringToBytes(original)
	result := BytesToString(bytes)
	if result != original {
		t.Errorf("round trip failed: got %q, want %q", result, original)
	}
}

func BenchmarkStringToBytes(b *testing.B) {
	s := "benchmark test string"
	for i := 0; i < b.N; i++ {
		_ = StringToBytes(s)
	}
}

func BenchmarkBytesToString(b *testing.B) {
	bs := []byte("benchmark test string")
	for i := 0; i < b.N; i++ {
		_ = BytesToString(bs)
	}
}
