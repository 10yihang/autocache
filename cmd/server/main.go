package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/10yihang/autocache/internal/cluster"
	"github.com/10yihang/autocache/internal/cluster/hash"
	"github.com/10yihang/autocache/internal/cluster/state"
	"github.com/10yihang/autocache/internal/engine/memory"
	"github.com/10yihang/autocache/internal/engine/tiered"
	metrics2 "github.com/10yihang/autocache/internal/metrics"
	"github.com/10yihang/autocache/internal/protocol"
)

var (
	addr             = flag.String("addr", ":6379", "server address")
	clusterEnabled   = flag.Bool("cluster-enabled", false, "enable cluster mode")
	clusterPort      = flag.Int("cluster-port", 16379, "cluster communication port")
	nodeID           = flag.String("node-id", "", "node ID (auto-generated if empty)")
	bindAddr         = flag.String("bind", "127.0.0.1", "bind address for cluster")
	seeds            = flag.String("seeds", "", "comma-separated seed nodes (host:port)")
	dataDir          = flag.String("data-dir", "./data", "data directory for persistent state")
	configPath       = flag.String("config", "", "path to config file")
	quietConnections = flag.Bool("quiet-connections", false, "disable per-connection connect/disconnect logs")

	// Tiered storage flags
	tieredEnabled    = flag.Bool("tiered-enabled", false, "enable tiered storage")
	hotTierEnabled   = flag.Bool("hot-tier-enabled", true, "enable memory hot tier")
	warmTierEnabled  = flag.Bool("warm-tier-enabled", true, "enable disk warm tier")
	warmEngine       = flag.String("warm-engine", "badger", "warm tier storage engine (badger, nokv)")
	warmTierPath     = flag.String("warm-tier-path", "", "path for warm tier storage (default: <data-dir>/warm)")
	badgerPath       = flag.String("badger-path", "", "deprecated: path for badger warm tier (use --warm-tier-path)")
	coldTierEnabled  = flag.Bool("cold-tier-enabled", false, "enable cloud cold tier")
	coldTierEndpoint = flag.String("cold-tier-endpoint", "", "cold tier endpoint")
	coldTierBucket   = flag.String("cold-tier-bucket", "", "cold tier bucket")
	metricsAddr      = flag.String("metrics-addr", ":9121", "Prometheus metrics listen address")

	// CLI flags
	cliMode = flag.Bool("cli", false, "run in CLI mode")
	cliHost = flag.String("h", "127.0.0.1", "server host (CLI mode)")
	cliPort = flag.Int("p", 6379, "server port (CLI mode)")
)

func main() {
	flag.Parse()
	var fileConfig map[string]string
	if *configPath != "" {
		cfg, err := loadConfigFile(*configPath)
		if err != nil {
			log.Fatalf("Failed to load config file: %v", err)
		}
		fileConfig = cfg
		applyConfig(cfg)
	}

	if *cliMode {
		runCLI(*cliHost, *cliPort, flag.Args())
		return
	}

	var store *memory.Store
	if !*tieredEnabled || *hotTierEnabled {
		store = memory.NewStore(buildMemoryConfigFromConfig(fileConfig))
		store.SetSlotFunc(hash.KeySlot)
	}
	metrics2.InitInfo("dev", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	metricsExporter := metrics2.NewExporter(*metricsAddr)
	go func() {
		if err := metricsExporter.Start(); err != nil && err.Error() != "http: Server closed" {
			log.Printf("Metrics exporter stopped with error: %v", err)
		}
	}()

	var engine protocol.ProtocolEngine
	var tieredMgr *tiered.Manager

	if *tieredEnabled {
		if !*hotTierEnabled && !*warmTierEnabled && !*coldTierEnabled {
			log.Fatalf("Tiered storage requires at least one enabled tier")
		}
		warmPath := resolveWarmTierPath()
		if *badgerPath != "" && *warmTierPath == "" {
			log.Println("Flag --badger-path is deprecated; use --warm-tier-path instead")
		}

		tieredCfg := &tiered.Config{
			Enabled:            true,
			HotTierEnabled:     *hotTierEnabled,
			HotTierCapacity:    512 * 1024 * 1024, // 512MB
			WarmTierEnabled:    *warmTierEnabled,
			WarmTierEngine:     *warmEngine,
			WarmTierPath:       warmPath,
			ColdTierEnabled:    *coldTierEnabled,
			ColdTierEndpoint:   *coldTierEndpoint,
			ColdTierBucket:     *coldTierBucket,
			MigrationInterval:  time.Minute,
			MigrationBatchSize: 100,
			HotAccessThreshold: 10,
			HotIdleThreshold:   5 * time.Minute,
			ColdIdleThreshold:  2 * time.Hour,
		}

		var err error
		tieredMgr, err = tiered.NewManager(tieredCfg, store)
		if err != nil {
			log.Fatalf("Failed to create tiered manager: %v", err)
		}
		tieredMgr.Start()

		engine = protocol.NewTieredStoreAdapter(tieredMgr, store)
		log.Printf("Tiered storage enabled (hot=%t warm=%t cold=%t)", *hotTierEnabled, *warmTierEnabled, *coldTierEnabled)
		if *warmTierEnabled {
			log.Println("Warm tier path:", warmPath)
		}
	} else {
		if store == nil {
			log.Fatalf("Memory store must be enabled when tiered storage is disabled")
		}
		engine = protocol.NewMemoryStoreAdapter(store)
	}

	server := protocol.NewServer(*addr, engine)
	server.SetQuietConnections(*quietConnections)
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

	if tieredMgr != nil {
		tieredMgr.Stop()
	}
	if err := metricsExporter.Stop(); err != nil {
		log.Printf("Error stopping metrics exporter: %v", err)
	}

	if err := server.Stop(); err != nil {
		log.Printf("Error stopping server: %v", err)
	}
	if store != nil {
		if err := store.Close(); err != nil {
			log.Printf("Error closing store: %v", err)
		}
	}
}

func loadConfigFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	config := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		config[parts[0]] = strings.Join(parts[1:], " ")
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return config, nil
}

func applyConfig(cfg map[string]string) {
	if v, ok := cfg["port"]; ok && v != "" {
		*addr = ":" + v
	}
	if v, ok := cfg["cluster-port"]; ok && v != "" {
		*clusterPort = parseInt(v, *clusterPort)
	}
	if v, ok := cfg["cluster-enabled"]; ok {
		*clusterEnabled = strings.EqualFold(v, "yes") || strings.EqualFold(v, "true")
	}
	if v, ok := cfg["warm-engine"]; ok && v != "" {
		*warmEngine = v
	}
	if v, ok := cfg["hot-tier-enabled"]; ok {
		*hotTierEnabled = strings.EqualFold(v, "yes") || strings.EqualFold(v, "true")
	}
	if v, ok := cfg["warm-tier-enabled"]; ok {
		*warmTierEnabled = strings.EqualFold(v, "yes") || strings.EqualFold(v, "true")
	}
	if v, ok := cfg["warm-tier-path"]; ok && v != "" {
		*warmTierPath = v
	}
	if v, ok := cfg["cold-tier-enabled"]; ok {
		*coldTierEnabled = strings.EqualFold(v, "yes") || strings.EqualFold(v, "true")
	}
	if v, ok := cfg["cold-tier-endpoint"]; ok && v != "" {
		*coldTierEndpoint = v
	}
	if v, ok := cfg["cold-tier-bucket"]; ok && v != "" {
		*coldTierBucket = v
	}
}

func resolveWarmTierPath() string {
	if *warmTierPath != "" {
		return *warmTierPath
	}
	if *badgerPath != "" {
		return *badgerPath
	}
	return filepath.Join(*dataDir, "warm")
}

func buildMemoryConfigFromConfig(cfg map[string]string) *memory.Config {
	memCfg := memory.DefaultConfig()
	if cfg == nil {
		return memCfg
	}
	if v, ok := cfg["maxmemory"]; ok && v != "" {
		if bytes, err := parseMemoryBytes(v); err == nil {
			memCfg.MaxMemory = bytes
		}
	}
	if v, ok := cfg["maxmemory-policy"]; ok && v != "" {
		memCfg.EvictPolicy = v
	}
	return memCfg
}

func parseMemoryBytes(value string) (int64, error) {
	v := strings.TrimSpace(strings.ToLower(value))
	multiplier := int64(1)
	for _, suffix := range []struct {
		suffix     string
		multiplier int64
	}{
		{"gb", 1024 * 1024 * 1024},
		{"g", 1024 * 1024 * 1024},
		{"mb", 1024 * 1024},
		{"m", 1024 * 1024},
		{"kb", 1024},
		{"k", 1024},
		{"b", 1},
	} {
		if strings.HasSuffix(v, suffix.suffix) {
			multiplier = suffix.multiplier
			v = strings.TrimSpace(strings.TrimSuffix(v, suffix.suffix))
			break
		}
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, err
	}
	return n * multiplier, nil
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

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		fmt.Printf("Error connecting to %s: %v\n", addr, err)
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
