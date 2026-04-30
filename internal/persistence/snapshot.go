package persistence

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// SnapshotWriter handles RDB-style snapshot creation.
type SnapshotWriter struct {
	dataDir      string
	lastSaveUnix int64 // atomic
	mu           sync.Mutex
	saving       bool
}

// NewSnapshotWriter creates a snapshot writer.
func NewSnapshotWriter(dataDir string) *SnapshotWriter {
	return &SnapshotWriter{dataDir: dataDir}
}

// Save iterates all keys from the provided iterator and writes a snapshot file.
// The iterator function should return all key-value pairs to persist.
func (sw *SnapshotWriter) Save(iterateAll func() (map[string][]byte, error)) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	if sw.saving {
		return fmt.Errorf("a save is already in progress")
	}
	sw.saving = true
	defer func() { sw.saving = false }()

	data, err := iterateAll()
	if err != nil {
		return fmt.Errorf("iterate: %w", err)
	}

	tmpPath := filepath.Join(sw.dataDir, "dump.rdb.tmp")
	finalPath := filepath.Join(sw.dataDir, "dump.rdb")

	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create snapshot file: %w", err)
	}

	for key, val := range data {
		if _, err := fmt.Fprintf(f, "%d %s %d %s\n", len(key), key, len(val), val); err != nil {
			f.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("write snapshot: %w", err)
		}
	}

	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync snapshot: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close snapshot: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("rename snapshot: %w", err)
	}

	atomic.StoreInt64(&sw.lastSaveUnix, time.Now().Unix())
	log.Printf("Snapshot saved to %s (%d keys)", finalPath, len(data))
	return nil
}

// SaveAsync runs Save in a background goroutine.
func (sw *SnapshotWriter) SaveAsync(iterateAll func() (map[string][]byte, error)) {
	go func() {
		if err := sw.Save(iterateAll); err != nil {
			log.Printf("BGSAVE error: %v", err)
		}
	}()
}

// LastSaveUnix returns the Unix timestamp of the last successful save.
func (sw *SnapshotWriter) LastSaveUnix() int64 {
	return atomic.LoadInt64(&sw.lastSaveUnix)
}

// IsSaving returns whether a save is in progress.
func (sw *SnapshotWriter) IsSaving() bool {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.saving
}
