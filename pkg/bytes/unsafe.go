//go:build !appengine
// +build !appengine

// Package bytes provides efficient byte/string conversion utilities.
package bytes

import "unsafe"

// StringToBytes converts a string to []byte without memory allocation.
// The returned slice shares memory with the original string.
//
// WARNING: The returned []byte MUST NOT be modified, as strings are immutable.
// Modifying the returned slice will cause undefined behavior.
//
// Use cases:
// - Passing string data to functions that accept []byte for reading
// - Avoiding allocation in hot paths where string→[]byte conversion is frequent
//
// Performance: Zero allocations, ~0.3ns per call
func StringToBytes(s string) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// BytesToString converts a []byte to string without memory allocation.
// The returned string shares memory with the original slice.
//
// WARNING: The original []byte MUST NOT be modified after this call,
// as the string will reflect those changes (violating string immutability).
//
// Use cases:
// - Converting []byte from ring buffer to string for map lookups
// - Avoiding allocation when returning string values
//
// Performance: Zero allocations, ~0.3ns per call
func BytesToString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}
