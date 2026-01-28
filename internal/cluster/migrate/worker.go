package migrate

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/10yihang/autocache/internal/cluster/hash"
	"github.com/10yihang/autocache/internal/engine"
)

type MigrationEngine interface {
	Scan(ctx context.Context, cursor uint64, pattern string, count int) ([]string, uint64, error)
	GetEntry(ctx context.Context, key string) (*engine.Entry, error)
	Del(ctx context.Context, keys ...string) (int64, error)
}

type MigrationStatus int

const (
	MigrationStatusPending MigrationStatus = iota
	MigrationStatusRunning
	MigrationStatusCompleted
	MigrationStatusFailed
)

type MigrationProgress struct {
	Slot         uint16
	TargetAddr   string
	TotalKeys    int
	MigratedKeys int
	FailedKeys   int
	Status       MigrationStatus
	LastError    string
	StartTime    time.Time
	EndTime      time.Time
}

type Worker struct {
	engine     MigrationEngine
	timeout    time.Duration
	maxRetries int
	batchSize  int

	mu       sync.Mutex
	progress map[uint16]*MigrationProgress
	stopCh   chan struct{}
}

func NewWorker(engine MigrationEngine) *Worker {
	return &Worker{
		engine:     engine,
		timeout:    5 * time.Second,
		maxRetries: 3,
		batchSize:  100,
		progress:   make(map[uint16]*MigrationProgress),
		stopCh:     make(chan struct{}),
	}
}

func (w *Worker) SetTimeout(timeout time.Duration) {
	w.timeout = timeout
}

func (w *Worker) SetMaxRetries(retries int) {
	w.maxRetries = retries
}

func (w *Worker) SetBatchSize(size int) {
	w.batchSize = size
}

func (w *Worker) MigrateSlot(ctx context.Context, slot uint16, targetAddr string, replace bool) error {
	w.mu.Lock()
	w.progress[slot] = &MigrationProgress{
		Slot:       slot,
		TargetAddr: targetAddr,
		Status:     MigrationStatusRunning,
		StartTime:  time.Now(),
	}
	progress := w.progress[slot]
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		progress.EndTime = time.Now()
		w.mu.Unlock()
	}()

	keys, err := w.getKeysInSlot(ctx, slot)
	if err != nil {
		w.mu.Lock()
		progress.Status = MigrationStatusFailed
		progress.LastError = err.Error()
		w.mu.Unlock()
		return err
	}

	w.mu.Lock()
	progress.TotalKeys = len(keys)
	w.mu.Unlock()

	for _, key := range keys {
		select {
		case <-ctx.Done():
			w.mu.Lock()
			progress.Status = MigrationStatusFailed
			progress.LastError = "context cancelled"
			w.mu.Unlock()
			return ctx.Err()
		case <-w.stopCh:
			w.mu.Lock()
			progress.Status = MigrationStatusFailed
			progress.LastError = "worker stopped"
			w.mu.Unlock()
			return fmt.Errorf("migration stopped")
		default:
		}

		err := w.migrateKey(ctx, key, targetAddr, replace)
		if err != nil {
			w.mu.Lock()
			progress.FailedKeys++
			progress.LastError = err.Error()
			w.mu.Unlock()
			continue
		}

		w.mu.Lock()
		progress.MigratedKeys++
		w.mu.Unlock()
	}

	w.mu.Lock()
	if progress.FailedKeys == 0 {
		progress.Status = MigrationStatusCompleted
	} else {
		progress.Status = MigrationStatusFailed
	}
	w.mu.Unlock()

	if progress.FailedKeys > 0 {
		return fmt.Errorf("migration completed with %d failed keys", progress.FailedKeys)
	}
	return nil
}

func (w *Worker) getKeysInSlot(ctx context.Context, slot uint16) ([]string, error) {
	var result []string
	var cursor uint64

	for {
		keys, nextCursor, err := w.engine.Scan(ctx, cursor, "*", w.batchSize)
		if err != nil {
			return nil, err
		}

		for _, key := range keys {
			if hash.KeySlot(key) == slot {
				result = append(result, key)
			}
		}

		if nextCursor == 0 {
			break
		}
		cursor = nextCursor
	}

	return result, nil
}

func (w *Worker) migrateKey(ctx context.Context, key string, targetAddr string, replace bool) error {
	var lastErr error

	for attempt := 0; attempt < w.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(100<<uint(attempt-1)) * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		err := w.doMigrate(ctx, key, targetAddr, replace)
		if err == nil {
			return nil
		}
		lastErr = err

		if strings.Contains(err.Error(), "BUSYKEY") && !replace {
			return err
		}
	}

	return lastErr
}

func (w *Worker) doMigrate(ctx context.Context, key string, targetAddr string, replace bool) error {
	entry, err := w.engine.GetEntry(ctx, key)
	if err != nil {
		return fmt.Errorf("get entry: %w", err)
	}

	var ttlMs int64
	if !entry.ExpireAt.IsZero() {
		ttlMs = time.Until(entry.ExpireAt).Milliseconds()
		if ttlMs < 0 {
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
		return fmt.Errorf("unsupported value type")
	}

	serialized := make([]byte, 1+len(valueBytes))
	serialized[0] = 0
	copy(serialized[1:], valueBytes)

	conn, err := net.DialTimeout("tcp", targetAddr, w.timeout)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(w.timeout))

	_, err = conn.Write([]byte("*1\r\n$6\r\nASKING\r\n"))
	if err != nil {
		return fmt.Errorf("write ASKING: %w", err)
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("read ASKING response: %w", err)
	}
	if string(buf[:n]) != "+OK\r\n" {
		return fmt.Errorf("ASKING failed: %s", strings.TrimSpace(string(buf[:n])))
	}

	keyLen := len(key)
	ttlStr := strconv.FormatInt(ttlMs, 10)
	serializedLen := len(serialized)

	var restoreCmd string
	if replace {
		restoreCmd = fmt.Sprintf("*5\r\n$7\r\nRESTORE\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n$7\r\nREPLACE\r\n",
			keyLen, key, len(ttlStr), ttlStr, serializedLen, serialized)
	} else {
		restoreCmd = fmt.Sprintf("*4\r\n$7\r\nRESTORE\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n",
			keyLen, key, len(ttlStr), ttlStr, serializedLen, serialized)
	}

	_, err = conn.Write([]byte(restoreCmd))
	if err != nil {
		return fmt.Errorf("write RESTORE: %w", err)
	}

	n, err = conn.Read(buf)
	if err != nil {
		return fmt.Errorf("read RESTORE response: %w", err)
	}

	response := string(buf[:n])
	if response != "+OK\r\n" {
		return fmt.Errorf("RESTORE failed: %s", strings.TrimSpace(response))
	}

	_, err = w.engine.Del(ctx, key)
	if err != nil {
		return fmt.Errorf("del local key: %w", err)
	}

	return nil
}

func (w *Worker) GetProgress(slot uint16) *MigrationProgress {
	w.mu.Lock()
	defer w.mu.Unlock()
	if p, ok := w.progress[slot]; ok {
		copy := *p
		return &copy
	}
	return nil
}

func (w *Worker) Stop() {
	close(w.stopCh)
}
