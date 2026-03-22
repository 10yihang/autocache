package clipboard

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type MiddlewareOptions struct {
	AdminToken  string
	CreateLimit int
	ReadLimit   int
	Window      time.Duration
	Now         func() time.Time
}

type Middleware struct {
	adminToken  string
	createLimit int
	readLimit   int
	window      time.Duration
	now         func() time.Time

	mu      sync.Mutex
	buckets map[string]*rateBucket
	stats   RateLimitStatsDTO
}

type rateBucket struct {
	count       int
	windowStart time.Time
}

func NewMiddleware(options MiddlewareOptions) *Middleware {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	window := options.Window
	if window <= 0 {
		window = time.Minute
	}

	return &Middleware{
		adminToken:  options.AdminToken,
		createLimit: options.CreateLimit,
		readLimit:   options.ReadLimit,
		window:      window,
		now:         now,
		buckets:     make(map[string]*rateBucket),
	}
}

func (m *Middleware) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if m.adminToken == "" || token != m.adminToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) RequireJSONBody(next http.Handler, maxBytes int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType := r.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "application/json") {
			http.Error(w, "content type must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		if r.ContentLength > int64(maxBytes) {
			http.Error(w, "request body exceeds content limit", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, int64(maxBytes))
		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) RateLimit(kind string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := m.limitFor(kind)
		if limit <= 0 {
			next.ServeHTTP(w, r)
			return
		}

		ip := clientIP(r.RemoteAddr)
		allowed := m.allow(kind, ip, limit)
		if !allowed {
			http.Error(w, fmt.Sprintf("%s rate limit exceeded", kind), http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) Stats() RateLimitStatsDTO {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stats
}

func (m *Middleware) limitFor(kind string) int {
	switch kind {
	case "create":
		return m.createLimit
	case "read":
		return m.readLimit
	default:
		return 0
	}
}

func (m *Middleware) allow(kind, ip string, limit int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now().UTC()
	key := kind + ":" + ip
	bucket := m.buckets[key]
	if bucket == nil || now.Sub(bucket.windowStart) >= m.window {
		bucket = &rateBucket{windowStart: now}
		m.buckets[key] = bucket
	}
	if bucket.count >= limit {
		switch kind {
		case "create":
			m.stats.CreateRateLimitedTotal++
		case "read":
			m.stats.ReadRateLimitedTotal++
		}
		return false
	}

	bucket.count++
	return true
}

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}
