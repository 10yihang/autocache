package protocol

import (
	"testing"
)

func TestFormatInt(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{9, "9"},
		{10, "10"},
		{42, "42"},
		{99, "99"},
		{100, "100"},
		{12345, "12345"},
		{-1, "-1"},
		{-42, "-42"},
		{-12345, "-12345"},
		{9223372036854775807, "9223372036854775807"},
		{-9223372036854775808, "-9223372036854775808"},
	}

	for _, tt := range tests {
		got := string(FormatInt(nil, tt.n))
		if got != tt.want {
			t.Errorf("FormatInt(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestFormatIntRESP(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, ":0\r\n"},
		{1, ":1\r\n"},
		{-1, ":-1\r\n"},
		{42, ":42\r\n"},
		{12345, ":12345\r\n"},
	}

	for _, tt := range tests {
		got := string(FormatIntRESP(nil, tt.n))
		if got != tt.want {
			t.Errorf("FormatIntRESP(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestFormatBulkString(t *testing.T) {
	tests := []struct {
		s    string
		want string
	}{
		{"", "$0\r\n\r\n"},
		{"hello", "$5\r\nhello\r\n"},
		{"OK", "$2\r\nOK\r\n"},
	}

	for _, tt := range tests {
		got := string(FormatBulkString(nil, []byte(tt.s)))
		if got != tt.want {
			t.Errorf("FormatBulkString(%q) = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestFormatArrayLen(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "*0\r\n"},
		{1, "*1\r\n"},
		{10, "*10\r\n"},
		{100, "*100\r\n"},
	}

	for _, tt := range tests {
		got := string(FormatArrayLen(nil, tt.n))
		if got != tt.want {
			t.Errorf("FormatArrayLen(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestFormatSimpleString(t *testing.T) {
	got := string(FormatSimpleString(nil, "OK"))
	want := "+OK\r\n"
	if got != want {
		t.Errorf("FormatSimpleString(\"OK\") = %q, want %q", got, want)
	}
}

func BenchmarkFormatInt_Small(b *testing.B) {
	b.ReportAllocs()
	buf := make([]byte, 0, 32)
	for i := 0; i < b.N; i++ {
		buf = FormatInt(buf[:0], 42)
	}
}

func BenchmarkFormatInt_Large(b *testing.B) {
	b.ReportAllocs()
	buf := make([]byte, 0, 32)
	for i := 0; i < b.N; i++ {
		buf = FormatInt(buf[:0], 9223372036854775807)
	}
}

func BenchmarkFormatInt_Negative(b *testing.B) {
	b.ReportAllocs()
	buf := make([]byte, 0, 32)
	for i := 0; i < b.N; i++ {
		buf = FormatInt(buf[:0], -12345)
	}
}

func BenchmarkFormatIntRESP(b *testing.B) {
	b.ReportAllocs()
	buf := make([]byte, 0, 32)
	for i := 0; i < b.N; i++ {
		buf = FormatIntRESP(buf[:0], int64(i%1000))
	}
}

func BenchmarkFormatBulkString(b *testing.B) {
	b.ReportAllocs()
	buf := make([]byte, 0, 64)
	data := []byte("hello world")
	for i := 0; i < b.N; i++ {
		buf = FormatBulkString(buf[:0], data)
	}
}
