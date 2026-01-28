// Package errors defines sentinel errors used across the AutoCache project.
package errors

import "errors"

// Sentinel errors for key operations.
var (
	// ErrKeyNotFound indicates that the requested key does not exist.
	ErrKeyNotFound = errors.New("key not found")

	// ErrWrongType indicates a type mismatch for the value stored under a key.
	ErrWrongType = errors.New("WRONGTYPE Operation against a key holding the wrong kind of value")

	// ErrNotInteger indicates the value is not a valid integer.
	ErrNotInteger = errors.New("value is not an integer or out of range")

	// ErrNotFloat indicates the value is not a valid float.
	ErrNotFloat = errors.New("value is not a valid float")

	// ErrKeyExpired indicates the key has expired.
	ErrKeyExpired = errors.New("key expired")
)

// Sentinel errors for cluster operations.
var (
	// ErrClusterDown indicates the cluster is not available.
	ErrClusterDown = errors.New("CLUSTERDOWN The cluster is down")

	// ErrMoved indicates the key belongs to a different node.
	ErrMoved = errors.New("MOVED")

	// ErrAsk indicates the key is being migrated to a different node.
	ErrAsk = errors.New("ASK")

	// ErrCrossSlot indicates keys belong to different slots.
	ErrCrossSlot = errors.New("CROSSSLOT Keys in request don't hash to the same slot")
)

// Sentinel errors for connection/protocol.
var (
	// ErrClosed indicates the resource has been closed.
	ErrClosed = errors.New("resource is closed")

	// ErrTimeout indicates an operation timed out.
	ErrTimeout = errors.New("operation timed out")

	// ErrNoAuth indicates authentication is required.
	ErrNoAuth = errors.New("NOAUTH Authentication required")

	// ErrInvalidArgs indicates wrong number of arguments.
	ErrInvalidArgs = errors.New("wrong number of arguments")
)

// Sentinel errors for memory/eviction.
var (
	// ErrOOM indicates out of memory when maxmemory is reached.
	ErrOOM = errors.New("OOM command not allowed when used memory > 'maxmemory'")
)
