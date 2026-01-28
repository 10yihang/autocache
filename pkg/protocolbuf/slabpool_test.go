package protocolbuf

import (
	"testing"
)

func TestSlabPool_BucketSelection(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		wantCap  int
		wantPool bool
	}{
		{"tiny", 10, 64, true},
		{"exact_64", 64, 64, true},
		{"small", 100, 256, true},
		{"exact_256", 256, 256, true},
		{"medium", 500, 1024, true},
		{"exact_1KB", 1024, 1024, true},
		{"large", 2000, 4096, true},
		{"exact_4KB", 4096, 4096, true},
		{"xlarge", 10000, 16384, true},
		{"exact_16KB", 16384, 16384, true},
		{"xxlarge", 50000, 65536, true},
		{"exact_64KB", 65536, 65536, true},
		{"too_large", 100000, 100000, false},
		{"zero", 0, 0, false},
		{"negative", -1, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResetSlabStats()

			got := GetSlice(tt.size)

			if tt.size <= 0 {
				if got != nil {
					t.Errorf("GetSlice(%d) = non-nil, want nil", tt.size)
				}
				return
			}

			if len(got) != tt.size {
				t.Errorf("GetSlice(%d) len = %d, want %d", tt.size, len(got), tt.size)
			}

			if cap(got) < tt.size {
				t.Errorf("GetSlice(%d) cap = %d, want >= %d", tt.size, cap(got), tt.size)
			}

			if tt.wantPool && cap(got) != tt.wantCap {
				t.Errorf("GetSlice(%d) cap = %d, want %d", tt.size, cap(got), tt.wantCap)
			}

			// Verify zeroed
			for i := 0; i < len(got); i++ {
				if got[i] != 0 {
					t.Errorf("GetSlice(%d)[%d] = %d, want 0", tt.size, i, got[i])
					break
				}
			}

			// Return to pool if poolable
			PutSlice(got)
		})
	}
}

func TestSlabPool_Reuse(t *testing.T) {
	ResetSlabStats()

	// Get a slice, write data, return it
	s1 := GetSlice(50)
	if s1 == nil {
		t.Fatal("GetSlice(50) returned nil")
	}

	// Write some data
	for i := range s1 {
		s1[i] = byte(i)
	}

	// Extend to full capacity before putting back
	fullSlice := s1[:cap(s1)]
	PutSlice(fullSlice)

	// Get another slice of same size class
	s2 := GetSlice(50)
	if s2 == nil {
		t.Fatal("Second GetSlice(50) returned nil")
	}

	// Verify it's zeroed (security check)
	for i := 0; i < len(s2); i++ {
		if s2[i] != 0 {
			t.Errorf("Reused slice not zeroed at index %d: got %d", i, s2[i])
			break
		}
	}

	PutSlice(s2)

	// Check stats
	stats := SlabPoolStats()
	if stats.Hits < 2 {
		t.Errorf("Expected at least 2 hits, got %d", stats.Hits)
	}
	if stats.Puts < 2 {
		t.Errorf("Expected at least 2 puts, got %d", stats.Puts)
	}
}

func TestSlabPool_NilSafe(t *testing.T) {
	// PutSlice should not panic with nil
	PutSlice(nil)
}

func TestSlabPool_WrongCapacity(t *testing.T) {
	// Create slice with non-standard capacity
	s := make([]byte, 100)
	initialPuts := SlabPoolStats().Puts

	PutSlice(s)

	// Should be discarded (not pooled)
	finalPuts := SlabPoolStats().Puts
	if finalPuts != initialPuts {
		t.Error("Slice with wrong capacity should be discarded")
	}
}

func BenchmarkSlabPool_GetPut_64B(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s := GetSlice(64)
		PutSlice(s)
	}
}

func BenchmarkSlabPool_GetPut_1KB(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s := GetSlice(1024)
		PutSlice(s)
	}
}

func BenchmarkSlabPool_GetPut_4KB(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s := GetSlice(4096)
		PutSlice(s)
	}
}

func BenchmarkSlabPool_Get_TooLarge(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s := GetSlice(100000)
		_ = s
	}
}

func BenchmarkSlabPool_Parallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s := GetSlice(256)
			PutSlice(s)
		}
	})
}
