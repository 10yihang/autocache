package admin

import (
	"sync"
	"time"
)

type AuditEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	RemoteAddr string    `json:"remote_addr"`
	User       string    `json:"user"`
	Action     string    `json:"action"`
	Target     string    `json:"target"`
	Result     string    `json:"result"`
}

type AuditLog struct {
	mu   sync.Mutex
	buf  []AuditEntry
	head int
	size int
}

func NewAuditLog(size int) *AuditLog {
	if size <= 0 {
		size = 1024
	}
	return &AuditLog{
		buf: make([]AuditEntry, size),
	}
}

func (a *AuditLog) Record(e AuditEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.buf[a.head%len(a.buf)] = e
	a.head++
	if a.size < len(a.buf) {
		a.size++
	}
}

func (a *AuditLog) Snapshot() []AuditEntry {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.size == 0 {
		return nil
	}

	result := make([]AuditEntry, a.size)
	if a.size < len(a.buf) {
		copy(result, a.buf[:a.size])
	} else {
		start := a.head % len(a.buf)
		n := copy(result, a.buf[start:])
		copy(result[n:], a.buf[:start])
	}
	return result
}
