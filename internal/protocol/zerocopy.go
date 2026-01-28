package protocol

// Zero-allocation string manipulation utilities for command parsing.

// upperTable is a lookup table for ASCII uppercase conversion.
// Non-ASCII bytes are passed through unchanged.
var upperTable [256]byte

func init() {
	for i := 0; i < 256; i++ {
		if i >= 'a' && i <= 'z' {
			upperTable[i] = byte(i - 32)
		} else {
			upperTable[i] = byte(i)
		}
	}
}

// ToUpperInPlace converts ASCII lowercase letters to uppercase in place.
// Non-ASCII bytes are left unchanged.
// This is safe to call on command name bytes from the RESP parser.
func ToUpperInPlace(b []byte) {
	for i := range b {
		b[i] = upperTable[b[i]]
	}
}

// ToUpperCopy returns a new byte slice with ASCII lowercase converted to uppercase.
// This allocates a new slice and does not modify the input.
func ToUpperCopy(b []byte) []byte {
	result := make([]byte, len(b))
	for i := range b {
		result[i] = upperTable[b[i]]
	}
	return result
}

// EqualFold compares two byte slices for equality, ignoring ASCII case.
// This is optimized for short command names (3-10 bytes).
func EqualFold(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if upperTable[a[i]] != upperTable[b[i]] {
			return false
		}
	}
	return true
}

// BytesEqual compares two byte slices for exact equality.
// Optimized for short slices with early exit.
func BytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// HashBytes computes a simple hash of the byte slice for use in fast command lookup.
// Uses FNV-1a variant for good distribution on short strings.
func HashBytes(b []byte) uint32 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)
	h := uint32(offset32)
	for _, c := range b {
		h ^= uint32(upperTable[c])
		h *= prime32
	}
	return h
}
