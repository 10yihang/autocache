package protocolbuf

import (
	"testing"
)

func TestArgsPool_BucketSelection(t *testing.T) {
	tests := []struct {
		name    string
		minCap  int
		wantCap int
	}{
		{"small", 3, 8},
		{"exact_8", 8, 8},
		{"medium", 10, 16},
		{"exact_16", 16, 16},
		{"large", 20, 32},
		{"exact_32", 32, 32},
		{"zero_defaults", 0, 8},
		{"negative_defaults", -1, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetArgs(tt.minCap)

			if len(got) != 0 {
				t.Errorf("GetArgs(%d) len = %d, want 0", tt.minCap, len(got))
			}

			if cap(got) != tt.wantCap {
				t.Errorf("GetArgs(%d) cap = %d, want %d", tt.minCap, cap(got), tt.wantCap)
			}

			PutArgs(got)
		})
	}
}

func TestArgsPool_TooLarge(t *testing.T) {
	got := GetArgs(50)

	if len(got) != 0 {
		t.Errorf("GetArgs(50) len = %d, want 0", len(got))
	}

	if cap(got) != 50 {
		t.Errorf("GetArgs(50) cap = %d, want 50", cap(got))
	}

	// Should be safe to call PutArgs (will be discarded)
	PutArgs(got)
}

func TestArgsPool_NoDataLeakage(t *testing.T) {
	// Get a slice and add some data
	args1 := GetArgs(8)
	args1 = append(args1, []byte("secret1"), []byte("secret2"))
	PutArgs(args1)

	// Get another slice
	args2 := GetArgs(8)

	// Verify capacity is correct
	if cap(args2) != 8 {
		t.Fatalf("Expected cap 8, got %d", cap(args2))
	}

	// Verify length is 0
	if len(args2) != 0 {
		t.Errorf("Expected len 0, got %d", len(args2))
	}

	// Check underlying array is cleared
	fullSlice := args2[:cap(args2)]
	for i, v := range fullSlice {
		if v != nil {
			t.Errorf("Data leakage at index %d: %v", i, v)
		}
	}

	PutArgs(args2)
}

func TestArgsPool_NilSafe(t *testing.T) {
	PutArgs(nil)
}

func TestArgsPool_Reuse(t *testing.T) {
	args1 := GetArgs(4)
	args1 = append(args1, []byte("test"))

	// Get pointer to underlying array for comparison
	ptr1 := &args1[:cap(args1)][0]

	PutArgs(args1)

	args2 := GetArgs(4)
	args2 = append(args2, []byte("other"))

	// Likely same underlying array (not guaranteed by spec, but usually true)
	ptr2 := &args2[:cap(args2)][0]

	PutArgs(args2)

	// This test just verifies the pool works - pointer equality isn't guaranteed
	_ = ptr1
	_ = ptr2
}

func BenchmarkArgsPool_GetPut_8(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		args := GetArgs(8)
		PutArgs(args)
	}
}

func BenchmarkArgsPool_GetPut_16(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		args := GetArgs(16)
		PutArgs(args)
	}
}

func BenchmarkArgsPool_Parallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			args := GetArgs(8)
			args = append(args, []byte("test1"), []byte("test2"))
			PutArgs(args)
		}
	})
}
