// Package protocolbuf provides buffer pooling utilities for the protocol layer.
//
// SlabPool implements a tiered slab allocator that reduces GC pressure by reusing
// byte slices across size classes. This is critical for high-performance protocol
// parsing where allocations dominate CPU time.
package protocolbuf

import (
	"sync"
	"sync/atomic"
)

// Size classes for the slab pool (powers of 4, roughly)
const (
	slab64B   = 64
	slab256B  = 256
	slab1KB   = 1024
	slab4KB   = 4096
	slab16KB  = 16384
	slab64KB  = 65536
	numSlabs  = 6
	maxPooled = slab64KB
)

// slabSizes are the available size classes in ascending order
var slabSizes = [numSlabs]int{slab64B, slab256B, slab1KB, slab4KB, slab16KB, slab64KB}

// SlabStats contains pool hit/miss statistics
type SlabStats struct {
	Hits   int64
	Misses int64
	Puts   int64
}

// slabPool is the global slab allocator instance
var slabPool = newSlabPool()

// slabPoolImpl implements a tiered slab allocator
type slabPoolImpl struct {
	pools [numSlabs]sync.Pool
	hits  atomic.Int64
	miss  atomic.Int64
	puts  atomic.Int64
}

func newSlabPool() *slabPoolImpl {
	sp := &slabPoolImpl{}
	for i := 0; i < numSlabs; i++ {
		size := slabSizes[i]
		sp.pools[i] = sync.Pool{
			New: func() any {
				return make([]byte, size)
			},
		}
	}
	return sp
}

// selectSlab returns the index of the smallest slab that can fit size bytes.
// Returns -1 if size exceeds maximum pooled size.
func selectSlab(size int) int {
	// Binary search would be overkill for 6 elements; linear is faster
	for i := 0; i < numSlabs; i++ {
		if size <= slabSizes[i] {
			return i
		}
	}
	return -1
}

// selectSlabByCap returns the slab index for a given capacity.
// Used when returning slices to the pool.
func selectSlabByCap(cap int) int {
	for i := 0; i < numSlabs; i++ {
		if cap == slabSizes[i] {
			return i
		}
	}
	return -1
}

// GetSlice returns a byte slice with at least the requested size.
// The slice is zeroed and ready for use. The returned slice's length
// equals the requested size, but its capacity may be larger.
//
// For sizes > 64KB, returns a freshly allocated slice (not pooled).
// Returns nil if size <= 0.
func GetSlice(size int) []byte {
	if size <= 0 {
		return nil
	}

	idx := selectSlab(size)
	if idx < 0 {
		// Too large to pool, allocate directly
		slabPool.miss.Add(1)
		return make([]byte, size)
	}

	slabPool.hits.Add(1)
	buf := slabPool.pools[idx].Get().([]byte)

	// Zero the slice for security and return with requested length
	clear(buf[:size])
	return buf[:size]
}

// PutSlice returns a byte slice to the pool for reuse.
// The slice is zeroed before being returned to the pool.
//
// Slices with capacity not matching a slab size are discarded.
// nil slices are safely ignored.
func PutSlice(b []byte) {
	if b == nil {
		return
	}

	c := cap(b)
	idx := selectSlabByCap(c)
	if idx < 0 {
		// Not from our pool (wrong capacity), discard
		return
	}

	// Zero for security before returning to pool
	clear(b[:c])

	slabPool.puts.Add(1)
	slabPool.pools[idx].Put(b[:c])
}

// SlabPoolStats returns current pool statistics.
func SlabPoolStats() SlabStats {
	return SlabStats{
		Hits:   slabPool.hits.Load(),
		Misses: slabPool.miss.Load(),
		Puts:   slabPool.puts.Load(),
	}
}

// ResetSlabStats resets pool statistics to zero.
func ResetSlabStats() {
	slabPool.hits.Store(0)
	slabPool.miss.Store(0)
	slabPool.puts.Store(0)
}
