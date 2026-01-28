package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/10yihang/autocache/internal/cluster"
	"github.com/10yihang/autocache/internal/cluster/state"
	"github.com/10yihang/autocache/internal/engine/memory"
	"github.com/10yihang/autocache/internal/protocol"
)

var (
	addr           = flag.String("addr", ":6379", "server address")
	clusterEnabled = flag.Bool("cluster-enabled", false, "enable cluster mode")
	clusterPort    = flag.Int("cluster-port", 16379, "cluster communication port")
	nodeID         = flag.String("node-id", "", "node ID (auto-generated if empty)")
	bindAddr       = flag.String("bind", "127.0.0.1", "bind address for cluster")
	seeds          = flag.String("seeds", "", "comma-separated seed nodes (host:port)")
	dataDir        = flag.String("data-dir", "./data", "data directory for persistent state")

	// CLI flags
	cliMode = flag.Bool("cli", false, "run in CLI mode")
	cliHost = flag.String("h", "127.0.0.1", "server host (CLI mode)")
	cliPort = flag.Int("p", 6379, "server port (CLI mode)")
)

func main() {
	flag.Parse()

	if *cliMode {
		runCLI(*cliHost, *cliPort, flag.Args())
		return
	}

	store := memory.NewStore(memory.DefaultConfig())
	adapter := protocol.NewMemoryStoreAdapter(store)
	server := protocol.NewServer(*addr, adapter)

	var clusterInstance *cluster.Cluster
	var stateManager *state.StateManager

	if *clusterEnabled {
		port := 6379
		if len(*addr) > 1 && (*addr)[0] == ':' {
			port = parseInt((*addr)[1:], 6379)
		}

		var err error
		stateManager, err = state.NewStateManager(*dataDir)
		if err != nil {
			log.Fatalf("Failed to create state manager: %v", err)
		}

		cfg := &cluster.Config{
			NodeID:      *nodeID,
			BindAddr:    *bindAddr,
			Port:        port,
			ClusterPort: *clusterPort,
		}

		clusterInstance, err = cluster.NewCluster(cfg, stateManager)
		if err != nil {
			log.Fatalf("Failed to create cluster: %v", err)
		}

		if err := stateManager.Load(); err != nil {
			log.Printf("Warning: failed to load state: %v", err)
		}

		server.SetCluster(clusterInstance)

		var seedList []string
		if *seeds != "" {
			seedList = strings.Split(*seeds, ",")
		}

		if err := clusterInstance.Start(seedList); err != nil {
			log.Fatalf("Failed to start cluster: %v", err)
		}

		log.Printf("Cluster mode enabled, node ID: %s", clusterInstance.GetSelf().ID)
	}

	go func() {
		if err := server.Start(); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")

	if stateManager != nil {
		if err := stateManager.Close(); err != nil {
			log.Printf("Error closing state manager: %v", err)
		}
	}

	if clusterInstance != nil {
		if err := clusterInstance.Stop(); err != nil {
			log.Printf("Error stopping cluster: %v", err)
		}
	}

	if err := server.Stop(); err != nil {
		log.Printf("Error stopping server: %v", err)
	}
	if err := store.Close(); err != nil {
		log.Printf("Error closing store: %v", err)
	}
}

func parseInt(s string, defaultVal int) int {
	val := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			val = val*10 + int(c-'0')
		} else {
			return defaultVal
		}
	}
	if val == 0 {
		return defaultVal
	}
	return val
}

func runCLI(host string, port int, args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: autocache -cli -h <host> -p <port> <command> [args...]")
		os.Exit(1)
	}

	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		fmt.Printf("Error connecting to %s:%d: %v\n", host, port, err)
		os.Exit(1)
	}
	defer conn.Close()

	// Build RESP request
	var req strings.Builder
	req.WriteString(fmt.Sprintf("*%d\r\n", len(args)))
	for _, arg := range args {
		req.WriteString(fmt.Sprintf("$%d\r\n%s\r\n", len(arg), arg))
	}

	if _, err := conn.Write([]byte(req.String())); err != nil {
		fmt.Printf("Error sending request: %v\n", err)
		os.Exit(1)
	}

	// Read RESP response (simple implementation)
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil && err != io.EOF {
		fmt.Printf("Error reading response: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(string(buf[:n]))
}
