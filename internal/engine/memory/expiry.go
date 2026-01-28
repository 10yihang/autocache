package memory

import (
	"sync"
	"time"
)

const (
	expireScanInterval = 100 * time.Millisecond
	expireScanCount    = 20
	expireThreshold    = 0.25
)

type ExpiryManager struct {
	cache  *ShardedCache
	stopCh chan struct{}
	wg     sync.WaitGroup
}

func NewExpiryManager(cache *ShardedCache) *ExpiryManager {
	return &ExpiryManager{
		cache:  cache,
		stopCh: make(chan struct{}),
	}
}

func (m *ExpiryManager) Start() {
	m.wg.Add(1)
	go m.activeExpireLoop()
}

func (m *ExpiryManager) Stop() {
	close(m.stopCh)
	m.wg.Wait()
}

func (m *ExpiryManager) activeExpireLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(expireScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.activeExpireCycle()
		}
	}
}

func (m *ExpiryManager) activeExpireCycle() {
	for {
		keys := m.cache.RandomKeys(expireScanCount)
		if len(keys) == 0 {
			return
		}

		expired := 0
		for _, key := range keys {
			ttl, ok := m.cache.TTL(key)
			if !ok {
				continue
			}
			if ttl == -1 {
				continue
			}
			if ttl <= 0 {
				m.cache.Delete(key)
				expired++
			}
		}

		if float64(expired)/float64(len(keys)) < expireThreshold {
			return
		}
	}
}
