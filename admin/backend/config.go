package admin

import "time"

type Config struct {
	Addr            string
	RedisAddr       string // RESP port for console command dispatch (default: 127.0.0.1:6379)
	User            string
	Password        string
	AllowDangerous  bool
	MaxSSEConns     int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	MaxRequestBytes int64
	AuditLogSize    int
}

func (c *Config) applyDefaults() {
	if c.Addr == "" {
		c.Addr = "127.0.0.1:8080"
	}
	if c.RedisAddr == "" {
		c.RedisAddr = "127.0.0.1:6379"
	}
	if c.MaxSSEConns <= 0 {
		c.MaxSSEConns = 32
	}
	if c.ReadTimeout <= 0 {
		c.ReadTimeout = 15 * time.Second
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = 30 * time.Second
	}
	if c.IdleTimeout <= 0 {
		c.IdleTimeout = 60 * time.Second
	}
	if c.MaxRequestBytes <= 0 {
		c.MaxRequestBytes = 256 * 1024
	}
	if c.AuditLogSize <= 0 {
		c.AuditLogSize = 1024
	}
}
