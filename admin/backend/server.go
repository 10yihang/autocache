package admin

import (
	"context"
	"net/http"
)

type Server struct {
	cfg    Config
	server *http.Server
	audit  *AuditLog
}

func New(deps Deps, cfg Config) *Server {
	cfg.applyDefaults()
	audit := NewAuditLog(cfg.AuditLogSize)
	handler := NewHTTPHandler(deps, cfg, audit)

	return &Server{
		cfg:   cfg,
		audit: audit,
		server: &http.Server{
			Addr:         cfg.Addr,
			Handler:      handler.Routes(),
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,
			IdleTimeout:  cfg.IdleTimeout,
		},
	}
}

func (s *Server) Start() error {
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) Addr() string {
	return s.cfg.Addr
}

func (s *Server) AuditLog() *AuditLog {
	return s.audit
}
