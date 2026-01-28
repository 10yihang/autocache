// Package migration provides slot migration functionality for Redis Cluster.
package migration

import (
	"context"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/10yihang/autocache/internal/cluster/hash"
	"github.com/10yihang/autocache/internal/protocol"
)

// SlotProvider provides slot state information for migration.
type SlotProvider interface {
	// GetMigratingSlots returns slots in MIGRATING state with target node info.
	GetMigratingSlots() map[uint16]MigrationInfo
	// GetNode returns node address by ID.
	GetNode(nodeID string) (addr string, ok bool)
}

// MigrationInfo contains information about a slot being migrated.
type MigrationInfo struct {
	TargetNodeID string
	TargetAddr   string
}

// Worker handles automatic slot migration in the background.
type Worker struct {
	engine   protocol.ProtocolEngine
	provider SlotProvider
	interval time.Duration
	timeout  time.Duration

	stopCh chan struct{}
	wg     sync.WaitGroup

	batchSize int
}

// Config configures the migration worker.
type Config struct {
	// Interval between migration checks (default: 100ms)
	Interval time.Duration
	// Timeout for network operations (default: 5s)
	Timeout time.Duration
	// BatchSize is max keys to migrate per tick (default: 10)
	BatchSize int
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Interval:  100 * time.Millisecond,
		Timeout:   5 * time.Second,
		BatchSize: 10,
	}
}

// NewWorker creates a new migration worker.
func NewWorker(engine protocol.ProtocolEngine, provider SlotProvider, cfg *Config) *Worker {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &Worker{
		engine:    engine,
		provider:  provider,
		interval:  cfg.Interval,
		timeout:   cfg.Timeout,
		batchSize: cfg.BatchSize,
		stopCh:    make(chan struct{}),
	}
}

// Start begins the migration worker loop.
func (w *Worker) Start() {
	w.wg.Add(1)
	go w.loop()
}

// Stop gracefully shuts down the migration worker.
func (w *Worker) Stop() {
	close(w.stopCh)
	w.wg.Wait()
}

func (w *Worker) loop() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.processMigratingSlots()
		case <-w.stopCh:
			return
		}
	}
}

func (w *Worker) processMigratingSlots() {
	migratingSlots := w.provider.GetMigratingSlots()
	if len(migratingSlots) == 0 {
		return
	}

	for slot, info := range migratingSlots {
		if err := w.migrateSlotKeys(slot, info); err != nil {
			log.Printf("migration: error migrating slot %d: %v", slot, err)
		}
	}
}

func (w *Worker) migrateSlotKeys(slot uint16, info MigrationInfo) error {
	ctx := context.Background()

	keys, err := w.getKeysForSlot(ctx, slot)
	if err != nil {
		return fmt.Errorf("scan keys for slot %d: %w", slot, err)
	}

	if len(keys) == 0 {
		return nil
	}

	if len(keys) > w.batchSize {
		keys = keys[:w.batchSize]
	}

	targetAddr := info.TargetAddr
	if targetAddr == "" {
		var ok bool
		targetAddr, ok = w.provider.GetNode(info.TargetNodeID)
		if !ok {
			return fmt.Errorf("target node %s not found", info.TargetNodeID)
		}
	}

	for _, key := range keys {
		if err := w.migrateKey(ctx, key, targetAddr); err != nil {
			log.Printf("migration: failed to migrate key %q to %s: %v", key, targetAddr, err)
			continue
		}
	}

	return nil
}

func (w *Worker) getKeysForSlot(ctx context.Context, slot uint16) ([]string, error) {
	var matchingKeys []string
	cursor := uint64(0)

	for {
		keys, nextCursor, err := w.engine.Scan(ctx, cursor, "*", 100)
		if err != nil {
			return nil, err
		}

		for _, key := range keys {
			if hash.KeySlot(key) == slot {
				matchingKeys = append(matchingKeys, key)
				if len(matchingKeys) >= w.batchSize*2 {
					return matchingKeys, nil
				}
			}
		}

		if nextCursor == 0 {
			break
		}
		cursor = nextCursor
	}

	return matchingKeys, nil
}

func (w *Worker) migrateKey(ctx context.Context, key, targetAddr string) error {
	entry, err := w.engine.GetEntry(ctx, key)
	if err != nil {
		return nil
	}

	var ttlMs int64
	if !entry.ExpireAt.IsZero() {
		ttlMs = time.Until(entry.ExpireAt).Milliseconds()
		if ttlMs < 0 {
			w.engine.Del(ctx, key)
			return nil
		}
	}

	var valueBytes []byte
	switch v := entry.Value.(type) {
	case []byte:
		valueBytes = v
	case string:
		valueBytes = []byte(v)
	default:
		return fmt.Errorf("unsupported value type: %T", entry.Value)
	}

	serialized := make([]byte, 1+len(valueBytes))
	serialized[0] = 0
	copy(serialized[1:], valueBytes)

	conn, err := net.DialTimeout("tcp", targetAddr, w.timeout)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", targetAddr, err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(w.timeout))

	if err := w.sendCommand(conn, "*1\r\n$6\r\nASKING\r\n"); err != nil {
		return fmt.Errorf("send ASKING: %w", err)
	}
	if err := w.readOK(conn); err != nil {
		return fmt.Errorf("ASKING response: %w", err)
	}

	ttlStr := strconv.FormatInt(ttlMs, 10)
	restoreCmd := fmt.Sprintf("*5\r\n$7\r\nRESTORE\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n$7\r\nREPLACE\r\n",
		len(key), key,
		len(ttlStr), ttlStr,
		len(serialized), serialized,
	)

	if err := w.sendCommand(conn, restoreCmd); err != nil {
		return fmt.Errorf("send RESTORE: %w", err)
	}
	if err := w.readOK(conn); err != nil {
		return fmt.Errorf("RESTORE response: %w", err)
	}

	w.engine.Del(ctx, key)

	return nil
}

func (w *Worker) sendCommand(conn net.Conn, cmd string) error {
	_, err := conn.Write([]byte(cmd))
	return err
}

func (w *Worker) readOK(conn net.Conn) error {
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return err
	}
	response := string(buf[:n])
	if response != "+OK\r\n" {
		return fmt.Errorf("unexpected response: %s", response)
	}
	return nil
}
