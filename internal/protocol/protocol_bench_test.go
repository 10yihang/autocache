package protocol

import (
	"testing"
)

func BenchmarkToUpperInPlace(b *testing.B) {
	b.ReportAllocs()
	data := []byte("get")
	for i := 0; i < b.N; i++ {
		data[0], data[1], data[2] = 'g', 'e', 't'
		ToUpperInPlace(data)
	}
}

func BenchmarkHashBytes(b *testing.B) {
	b.ReportAllocs()
	data := []byte("GET")
	for i := 0; i < b.N; i++ {
		_ = HashBytes(data)
	}
}

func BenchmarkCmdMap_Lookup_GET(b *testing.B) {
	h := NewHandler(nil)
	cmd := []byte("GET")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.cmdMap.Lookup(cmd)
	}
}

func BenchmarkCmdMap_Lookup_SET(b *testing.B) {
	h := NewHandler(nil)
	cmd := []byte("SET")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.cmdMap.Lookup(cmd)
	}
}

func BenchmarkCmdMap_Lookup_INCR(b *testing.B) {
	h := NewHandler(nil)
	cmd := []byte("INCR")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.cmdMap.Lookup(cmd)
	}
}

func BenchmarkCmdMap_Lookup_Unknown(b *testing.B) {
	h := NewHandler(nil)
	cmd := []byte("UNKNOWN")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.cmdMap.Lookup(cmd)
	}
}

func BenchmarkIsClusterCmd_True(b *testing.B) {
	b.ReportAllocs()
	cmd := []byte("CLUSTER")
	for i := 0; i < b.N; i++ {
		_ = IsClusterCmd(cmd)
	}
}

func BenchmarkIsClusterCmd_False(b *testing.B) {
	b.ReportAllocs()
	cmd := []byte("GET")
	for i := 0; i < b.N; i++ {
		_ = IsClusterCmd(cmd)
	}
}

func BenchmarkEqualFold(b *testing.B) {
	b.ReportAllocs()
	a := []byte("GET")
	c := []byte("get")
	for i := 0; i < b.N; i++ {
		_ = EqualFold(a, c)
	}
}

func BenchmarkBytesEqual(b *testing.B) {
	b.ReportAllocs()
	a := []byte("GET")
	c := []byte("GET")
	for i := 0; i < b.N; i++ {
		_ = BytesEqual(a, c)
	}
}

func BenchmarkWriteCachedInt_Cached(b *testing.B) {
	b.ReportAllocs()
	buf := make([]byte, 0, 32)
	for i := 0; i < b.N; i++ {
		buf = FormatIntRESP(buf[:0], 42)
	}
}

func BenchmarkWriteCachedInt_Uncached(b *testing.B) {
	b.ReportAllocs()
	buf := make([]byte, 0, 32)
	for i := 0; i < b.N; i++ {
		buf = FormatIntRESP(buf[:0], 12345)
	}
}
