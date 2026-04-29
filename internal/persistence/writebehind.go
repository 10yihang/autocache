// Package persistence provides async write-behind persistence to BadgerDB.
// Hot-path SET/DEL operations push to a channel; a background goroutine
// flushes batches to Badger using WriteBatch for high throughput.
package persistence

import (
	"bytes"
	"context"
	"encoding/gob"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/10yihang/autocache/internal/engine"
	"github.com/10yihang/autocache/internal/engine/memory"
	"github.com/dgraph-io/badger/v4"
)

func init() {
	gob.Register(map[string]string{})
	gob.Register([]string{})
	gob.Register(map[string]struct{}{})
	gob.Register(map[string]float64{})
}

type opType uint8

const (
	opSet opType = iota
	opDel
)

type writeOp struct {
	key   string
	entry *engine.Entry
	typ   opType
}

// Config holds write-behind configuration.
type Config struct {
	FlushInterval time.Duration
	BatchSize     int
	ChannelSize   int
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		FlushInterval: 100 * time.Millisecond,
		BatchSize:     512,
		ChannelSize:   65536,
	}
}

// WriteBehind buffers writes and asynchronously flushes them to Badger via WriteBatch.
type WriteBehind struct {
	db  *badger.DB
	mem *memory.Store

	cfg Config

	ch chan writeOp

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	flushed atomic.Int64
	dropped atomic.Int64
}

// NewWriteBehind creates a WriteBehind with the given Badger DB and memory store.
func NewWriteBehind(db *badger.DB, mem *memory.Store, cfg Config) *WriteBehind {
	ctx, cancel := context.WithCancel(context.Background())
	return &WriteBehind{
		db:     db,
		mem:    mem,
		cfg:    cfg,
		ch:     make(chan writeOp, cfg.ChannelSize),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start begins the background flush loop.
func (wb *WriteBehind) Start() {
	wb.wg.Add(1)
	go wb.flushLoop()
	log.Printf("[writebehind] Started, flush=%v batch=%d", wb.cfg.FlushInterval, wb.cfg.BatchSize)
}

// Stop gracefully shuts down the write-behind, draining pending writes.
func (wb *WriteBehind) Stop() {
	wb.cancel()
	wb.wg.Wait()
	wb.drain()
	log.Printf("[writebehind] Stopped, flushed=%d dropped=%d", wb.flushed.Load(), wb.dropped.Load())
}

// AppendSet enqueues a write for async flush. Non-blocking.
func (wb *WriteBehind) AppendSet(key string, entry *engine.Entry) {
	op := writeOp{key: key, entry: entry, typ: opSet}
	wb.push(op)
}

// AppendDel enqueues a delete for async flush. Non-blocking.
func (wb *WriteBehind) AppendDel(key string) {
	op := writeOp{key: key, typ: opDel}
	wb.push(op)
}

func (wb *WriteBehind) push(op writeOp) {
	select {
	case wb.ch <- op:
	default:
		wb.dropped.Add(1)
	}
}

func (wb *WriteBehind) flushLoop() {
	defer wb.wg.Done()

	batch := make([]writeOp, 0, wb.cfg.BatchSize)
	timer := time.NewTimer(wb.cfg.FlushInterval)
	defer timer.Stop()

	for {
		select {
		case <-wb.ctx.Done():
			return
		case op := <-wb.ch:
			batch = append(batch, op)
			if len(batch) >= wb.cfg.BatchSize {
				wb.flush(batch)
				batch = batch[:0]
				timer.Reset(wb.cfg.FlushInterval)
			}
		case <-timer.C:
			if len(batch) > 0 {
				wb.flush(batch)
				batch = batch[:0]
			}
			timer.Reset(wb.cfg.FlushInterval)
		}
	}
}

func (wb *WriteBehind) flush(batch []writeOp) {
	wbFlush := wb.db.NewWriteBatch()
	defer wbFlush.Cancel()

	for _, op := range batch {
		switch op.typ {
		case opSet:
			var buf bytes.Buffer
			if err := gob.NewEncoder(&buf).Encode(op.entry); err != nil {
				continue
			}
			e := badger.NewEntry([]byte(op.key), buf.Bytes())
			if !op.entry.ExpireAt.IsZero() {
				ttl := time.Until(op.entry.ExpireAt)
				if ttl > 0 {
					e = e.WithTTL(ttl)
				}
			}
			if err := wbFlush.SetEntry(e); err != nil {
				log.Printf("[writebehind] SetEntry error: %v", err)
			}
		case opDel:
			if err := wbFlush.Delete([]byte(op.key)); err != nil {
				log.Printf("[writebehind] Delete error: %v", err)
			}
		}
	}

	if err := wbFlush.Flush(); err != nil {
		log.Printf("[writebehind] Flush error: %v", err)
		return
	}
	wb.flushed.Add(int64(len(batch)))
}

func (wb *WriteBehind) drain() {
	for {
		select {
		case op := <-wb.ch:
			wb.flush([]writeOp{op})
		default:
			return
		}
	}
}

// LoadIntoMemory reads all entries from Badger and restores them into the memory store.
// Returns the number of keys loaded.
func (wb *WriteBehind) LoadIntoMemory(ctx context.Context) (int64, error) {
	var count int64

	err := wb.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := string(item.Key())

			if _, err := wb.mem.GetEntry(ctx, key); err == nil {
				continue
			}

			err := item.Value(func(val []byte) error {
				var entry engine.Entry
				if err := gob.NewDecoder(bytes.NewReader(val)).Decode(&entry); err != nil {
					return err
				}
				entry.Key = key

				payload, err := memory.SerializeEntryValue(&entry)
				if err != nil {
					return err
				}

				valueType := entry.Type
				payloadData := payload[1:]

				var ttl time.Duration
				if !entry.ExpireAt.IsZero() {
					ttl = time.Until(entry.ExpireAt)
					if ttl < 0 {
						return nil
					}
				}

				return wb.mem.RestoreEntry(ctx, key, valueType, payloadData, ttl)
			})
			if err != nil {
				continue
			}
			count++
		}
		return nil
	})

	log.Printf("[writebehind] Loaded %d keys from Badger into memory", count)
	return count, err
}

// Stats returns the number of flushed and dropped operations.
func (wb *WriteBehind) Stats() (flushed, dropped int64) {
	return wb.flushed.Load(), wb.dropped.Load()
}
