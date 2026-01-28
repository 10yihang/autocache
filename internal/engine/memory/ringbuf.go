package memory

import (
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
)

const (
	// headerSize is the fixed size of EntryHeader in bytes
	headerSize = 24

	// minRingBufferSize is the minimum capacity for a ring buffer
	minRingBufferSize = 1 << 20 // 1MB

	// defaultRingBufferSize is the default capacity
	defaultRingBufferSize = 64 << 20 // 64MB
)

var (
	// ErrBufferFull is returned when the ring buffer cannot accommodate the entry
	ErrBufferFull = errors.New("ring buffer full")

	// ErrInvalidOffset is returned when reading from an invalid offset
	ErrInvalidOffset = errors.New("invalid offset")

	// ErrEntryTooLarge is returned when an entry exceeds maximum size
	ErrEntryTooLarge = errors.New("entry too large for buffer")
)

type EvictedEntry struct {
	Hash   uint64
	Offset int64
}

// EntryHeader is a 24-byte fixed header for each entry in the ring buffer
// Layout:
//
//	[0:8]   ExpireAt  - Unix nanoseconds (0 = no expiry)
//	[8:16]  Hash64    - 64-bit hash for collision detection
//	[16:18] KeyLen    - Length of key (max 65535)
//	[18:20] ValLen    - Length of value (max 65535)
//	[20:22] ValCap    - Pre-allocated capacity (for future APPEND)
//	[22:24] Flags     - Type flags (string=0, list=1, set=2, etc.)
type EntryHeader struct {
	ExpireAt int64  // 8 bytes
	Hash64   uint64 // 8 bytes
	KeyLen   uint16 // 2 bytes
	ValLen   uint16 // 2 bytes
	ValCap   uint16 // 2 bytes
	Flags    uint16 // 2 bytes
}

// EntryType constants for Flags field
const (
	EntryTypeString uint16 = iota
	EntryTypeList
	EntryTypeSet
	EntryTypeHash
	EntryTypeZSet
)

// MarshalTo writes the header to a byte slice (must be at least 24 bytes)
func (h *EntryHeader) MarshalTo(buf []byte) {
	binary.LittleEndian.PutUint64(buf[0:8], uint64(h.ExpireAt))
	binary.LittleEndian.PutUint64(buf[8:16], h.Hash64)
	binary.LittleEndian.PutUint16(buf[16:18], h.KeyLen)
	binary.LittleEndian.PutUint16(buf[18:20], h.ValLen)
	binary.LittleEndian.PutUint16(buf[20:22], h.ValCap)
	binary.LittleEndian.PutUint16(buf[22:24], h.Flags)
}

// UnmarshalFrom reads the header from a byte slice
func (h *EntryHeader) UnmarshalFrom(buf []byte) {
	h.ExpireAt = int64(binary.LittleEndian.Uint64(buf[0:8]))
	h.Hash64 = binary.LittleEndian.Uint64(buf[8:16])
	h.KeyLen = binary.LittleEndian.Uint16(buf[16:18])
	h.ValLen = binary.LittleEndian.Uint16(buf[18:20])
	h.ValCap = binary.LittleEndian.Uint16(buf[20:22])
	h.Flags = binary.LittleEndian.Uint16(buf[22:24])
}

// RingBuffer is a high-performance circular buffer for storing serialized entries
// Design goals:
// - Zero-copy reads: return slices directly into the buffer
// - Append-only writes: no fragmentation, simple offset tracking
// - Lock-free reads where possible (atomic tail tracking)
//
// Memory layout:
// [Header1][Key1][Value1][Header2][Key2][Value2]...
//
// When buffer wraps, we reset to beginning (FIFO eviction)
type RingBuffer struct {
	data     []byte
	capacity int64

	// Write position (only written by writer, read by readers)
	tail atomic.Int64

	// Read position / eviction frontier
	head atomic.Int64

	// Used bytes in buffer
	used atomic.Int64

	// Write lock (single writer)
	mu sync.Mutex

	// Statistics
	writeCount atomic.Int64
	evictCount atomic.Int64
}

// NewRingBuffer creates a new ring buffer with the specified capacity
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity < minRingBufferSize {
		capacity = minRingBufferSize
	}

	return &RingBuffer{
		data:     make([]byte, capacity),
		capacity: int64(capacity),
	}
}

// Write appends an entry to the buffer and returns its offset
// Thread-safe: uses mutex for write serialization
//
// Returns:
// - offset: position where entry was written
// - error: ErrEntryTooLarge if entry exceeds buffer capacity
func (rb *RingBuffer) Write(header *EntryHeader, key, value []byte) (int64, error) {
	offset, _, err := rb.WriteWithEvict(header, key, value)
	return offset, err
}

func (rb *RingBuffer) WriteWithEvict(header *EntryHeader, key, value []byte) (int64, []EvictedEntry, error) {
	entrySize := int64(headerSize + len(key) + len(value))
	if entrySize > rb.capacity {
		return -1, nil, ErrEntryTooLarge
	}

	rb.mu.Lock()
	defer rb.mu.Unlock()

	head := rb.head.Load()
	tail := rb.tail.Load()
	used := rb.used.Load()

	evicted := make([]EvictedEntry, 0)

	for entrySize > rb.capacity-used {
		h, err := rb.readHeaderAt(head)
		if err != nil {
			head = 0
			tail = 0
			used = 0
			evicted = evicted[:0]
			break
		}

		itemSize := headerEntrySize(h)
		if itemSize <= 0 || itemSize > rb.capacity {
			head = 0
			tail = 0
			used = 0
			evicted = evicted[:0]
			break
		}

		evicted = append(evicted, EvictedEntry{Hash: h.Hash64, Offset: head})
		head += itemSize
		if head >= rb.capacity {
			head = 0
		}
		used -= itemSize
		rb.evictCount.Add(1)
	}

	if tail+entrySize > rb.capacity {
		tail = 0
		for used > 0 && entrySize > head {
			h, err := rb.readHeaderAt(head)
			if err != nil {
				head = 0
				tail = 0
				used = 0
				evicted = evicted[:0]
				break
			}

			itemSize := headerEntrySize(h)
			if itemSize <= 0 || itemSize > rb.capacity {
				head = 0
				tail = 0
				used = 0
				evicted = evicted[:0]
				break
			}

			evicted = append(evicted, EvictedEntry{Hash: h.Hash64, Offset: head})
			head += itemSize
			if head >= rb.capacity {
				head = 0
			}
			used -= itemSize
			rb.evictCount.Add(1)
		}
	}

	offset := tail
	header.MarshalTo(rb.data[offset:])
	keyStart := offset + headerSize
	copy(rb.data[keyStart:], key)
	valStart := keyStart + int64(len(key))
	copy(rb.data[valStart:], value)

	tail = offset + entrySize
	used += entrySize

	rb.head.Store(head)
	rb.tail.Store(tail)
	rb.used.Store(used)
	rb.writeCount.Add(1)

	return offset, evicted, nil
}

// ReadAt returns zero-copy views of key and value at the given offset
// Thread-safe: uses atomic tail check for validity
//
// IMPORTANT: The returned slices are views into the buffer.
// They are only valid until the next write that wraps past this offset.
// Caller must copy if long-term storage is needed.
func (rb *RingBuffer) ReadAt(offset int64) (key, value []byte, header EntryHeader, err error) {
	if offset < 0 || offset+headerSize > rb.capacity {
		return nil, nil, header, ErrInvalidOffset
	}

	// Read header
	header.UnmarshalFrom(rb.data[offset:])

	// Calculate positions
	keyStart := offset + headerSize
	keyEnd := keyStart + int64(header.KeyLen)
	valEnd := keyEnd + int64(header.ValLen)

	// Bounds check
	if valEnd > rb.capacity {
		return nil, nil, header, ErrInvalidOffset
	}

	// Return zero-copy slices
	key = rb.data[keyStart:keyEnd]
	value = rb.data[keyEnd:valEnd]

	return key, value, header, nil
}

// ReadAtCopy returns copies of key and value (safe for long-term storage)
func (rb *RingBuffer) ReadAtCopy(offset int64) (key, value []byte, header EntryHeader, err error) {
	k, v, h, err := rb.ReadAt(offset)
	if err != nil {
		return nil, nil, h, err
	}

	// Make copies
	key = make([]byte, len(k))
	copy(key, k)

	value = make([]byte, len(v))
	copy(value, v)

	return key, value, h, nil
}

// Tail returns the current write position
func (rb *RingBuffer) Tail() int64 {
	return rb.tail.Load()
}

// Head returns the current read/eviction position
func (rb *RingBuffer) Head() int64 {
	return rb.head.Load()
}

// Capacity returns the buffer capacity
func (rb *RingBuffer) Capacity() int64 {
	return rb.capacity
}

// Used returns the approximate bytes used (may be inaccurate during wrap)
func (rb *RingBuffer) Used() int64 {
	return rb.used.Load()
}

// Stats returns buffer statistics
func (rb *RingBuffer) Stats() (writes, evicts int64) {
	return rb.writeCount.Load(), rb.evictCount.Load()
}

// Reset clears the buffer
func (rb *RingBuffer) Reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.tail.Store(0)
	rb.head.Store(0)
	rb.used.Store(0)
	rb.writeCount.Store(0)
	rb.evictCount.Store(0)
}

func (rb *RingBuffer) readHeaderAt(offset int64) (EntryHeader, error) {
	var header EntryHeader
	if offset < 0 || offset+headerSize > rb.capacity {
		return header, ErrInvalidOffset
	}
	header.UnmarshalFrom(rb.data[offset:])
	return header, nil
}

func headerEntrySize(header EntryHeader) int64 {
	return int64(headerSize) + int64(header.KeyLen) + int64(header.ValLen)
}
