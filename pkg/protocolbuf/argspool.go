package protocolbuf

import (
	"sync"
)

// Arguments pool size classes
const (
	argsSmall  = 8  // Most commands have <= 8 args
	argsMedium = 16 // Moderate commands
	argsLarge  = 32 // Large commands (MSET, etc.)
	numArgPool = 3
)

var argsSizes = [numArgPool]int{argsSmall, argsMedium, argsLarge}

// argsPoolImpl manages pools of [][]byte slices
type argsPoolImpl struct {
	pools [numArgPool]sync.Pool
}

var argsPool = newArgsPool()

func newArgsPool() *argsPoolImpl {
	ap := &argsPoolImpl{}
	for i := 0; i < numArgPool; i++ {
		cap := argsSizes[i]
		ap.pools[i] = sync.Pool{
			New: func() any {
				return make([][]byte, 0, cap)
			},
		}
	}
	return ap
}

// selectArgsPool returns the index of the smallest pool that can fit minCap.
// Returns -1 if minCap exceeds maximum pooled size.
func selectArgsPool(minCap int) int {
	for i := 0; i < numArgPool; i++ {
		if minCap <= argsSizes[i] {
			return i
		}
	}
	return -1
}

// selectArgsPoolByCap returns the pool index for a given capacity.
func selectArgsPoolByCap(cap int) int {
	for i := 0; i < numArgPool; i++ {
		if cap == argsSizes[i] {
			return i
		}
	}
	return -1
}

// GetArgs returns a [][]byte slice with at least the requested capacity.
// The returned slice has length 0.
//
// For minCap > 32, returns a freshly allocated slice (not pooled).
func GetArgs(minCap int) [][]byte {
	if minCap <= 0 {
		minCap = argsSmall // Default to 8
	}

	idx := selectArgsPool(minCap)
	if idx < 0 {
		// Too large to pool, allocate directly
		return make([][]byte, 0, minCap)
	}

	args := argsPool.pools[idx].Get().([][]byte)
	return args[:0]
}

// PutArgs returns a [][]byte slice to the pool for reuse.
// The slice is cleared before being returned to prevent data leakage.
//
// Slices with capacity not matching a pool size are discarded.
// nil slices are safely ignored.
func PutArgs(args [][]byte) {
	if args == nil {
		return
	}

	c := cap(args)
	idx := selectArgsPoolByCap(c)
	if idx < 0 {
		// Not from our pool (wrong capacity), discard
		return
	}

	// Clear all elements to prevent data leakage
	fullSlice := args[:c]
	for i := range fullSlice {
		fullSlice[i] = nil
	}

	argsPool.pools[idx].Put(fullSlice[:0])
}
