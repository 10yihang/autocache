package protocol

import (
	"context"
	"log"
	"net"
	"sync"

	"github.com/tidwall/redcon"

	"github.com/10yihang/autocache/internal/cluster"
)

type Server struct {
	addr     string
	engine   ProtocolEngine
	handler  *Handler
	server   *redcon.Server
	listener net.Listener

	mu      sync.RWMutex
	clients map[redcon.Conn]*Client
}

type Client struct {
	conn          redcon.Conn
	db            int
	name          string
	authenticated bool
}

func NewServer(addr string, engine ProtocolEngine) *Server {
	s := &Server{
		addr:    addr,
		engine:  engine,
		clients: make(map[redcon.Conn]*Client),
	}
	s.handler = NewHandler(engine)
	return s
}

func (s *Server) SetCluster(c *cluster.Cluster) {
	s.handler.SetCluster(c)
}

func (s *Server) Start() error {
	log.Printf("AutoCache server starting on %s", s.addr)

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}

	srv := redcon.NewServer(s.addr,
		s.handleCommand,
		s.handleAccept,
		s.handleClose,
	)

	s.mu.Lock()
	s.listener = ln
	s.server = srv
	s.mu.Unlock()

	return srv.Serve(ln)
}

func (s *Server) Stop() error {
	s.mu.RLock()
	srv := s.server
	s.mu.RUnlock()
	if srv == nil {
		return nil
	}
	return srv.Close()
}

func (s *Server) Addr() string {
	s.mu.RLock()
	ln := s.listener
	s.mu.RUnlock()
	if ln != nil {
		return ln.Addr().String()
	}
	return s.addr
}

func (s *Server) handleAccept(conn redcon.Conn) bool {
	s.mu.Lock()
	s.clients[conn] = &Client{
		conn: conn,
		db:   0,
	}
	s.mu.Unlock()

	log.Printf("Client connected: %s", conn.RemoteAddr())
	return true
}

func (s *Server) handleClose(conn redcon.Conn, err error) {
	s.mu.Lock()
	delete(s.clients, conn)
	s.mu.Unlock()

	log.Printf("Client disconnected: %s", conn.RemoteAddr())
}

func (s *Server) handleCommand(conn redcon.Conn, cmd redcon.Command) {
	if len(cmd.Args) == 0 {
		conn.WriteError("ERR empty command")
		return
	}

	ctx := context.Background()
	s.handler.ExecuteBytes(ctx, conn, cmd.Args[0], cmd.Args[1:])

	pipeline := conn.ReadPipeline()
	if len(pipeline) == 0 {
		return
	}

	for _, p := range pipeline {
		if len(p.Args) == 0 {
			continue
		}
		s.handler.ExecuteBytes(ctx, conn, p.Args[0], p.Args[1:])
	}
}
