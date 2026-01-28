package memory

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// === COUNT-MIN SKETCH TESTS ===

func TestCountMinSketch_IncrementEstimate(t *testing.T) {
	cms := NewCountMinSketch(1 << 10)

	hash := uint64(0x12345678)

	if est := cms.Estimate(hash); est != 0 {
		t.Errorf("expected 0, got %d", est)
	}

	for i := 0; i < 10; i++ {
		cms.Increment(hash)
	}

	est := cms.Estimate(hash)
	if est < 9 || est > 11 {
		t.Errorf("expected ~10, got %d", est)
	}
}

func TestCountMinSketch_Saturation(t *testing.T) {
	cms := NewCountMinSketch(1 << 10)
	hash := uint64(0xABCDEF)

	for i := 0; i < 100; i++ {
		cms.Increment(hash)
	}

	est := cms.Estimate(hash)
	if est > counterMax {
		t.Errorf("counter should saturate at %d, got %d", counterMax, est)
	}
}

func TestCountMinSketch_Clear(t *testing.T) {
	cms := NewCountMinSketch(1 << 10)
	hash := uint64(0x12345)

	cms.Increment(hash)
	cms.Increment(hash)

	if cms.Estimate(hash) == 0 {
		t.Error("expected non-zero before clear")
	}

	cms.Clear()

	if cms.Estimate(hash) != 0 {
		t.Error("expected zero after clear")
	}
}

// === TINYLFU TESTS ===

func TestTinyLFU_Admit(t *testing.T) {
	lfu := NewTinyLFU(1 << 10)

	hotKey := uint64(0x1111)
	coldKey := uint64(0x2222)

	for i := 0; i < 10; i++ {
		lfu.RecordAccess(hotKey)
	}
	lfu.RecordAccess(coldKey)

	if !lfu.Admit(hotKey, coldKey) {
		t.Error("hot key should be admitted over cold key")
	}

	if lfu.Admit(coldKey, hotKey) {
		t.Error("cold key should not be admitted over hot key")
	}
}

func TestTinyLFU_Frequency(t *testing.T) {
	lfu := NewTinyLFU(1 << 10)
	hash := uint64(0xDEADBEEF)

	for i := 0; i < 5; i++ {
		lfu.RecordAccess(hash)
	}

	freq := lfu.Frequency(hash)
	if freq < 4 || freq > 6 {
		t.Errorf("expected ~5, got %d", freq)
	}
}

// === SHARDED CACHE TESTS ===

func TestShardedCache_BasicOperations(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	cfg.SampledEvict = 0
	cache := NewShardedCache(cfg)

	key := "testkey"
	value := []byte("testvalue")

	if err := cache.Set(key, value, 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got, ok := cache.Get(key)
	if !ok {
		t.Fatal("Get returned not ok")
	}
	if string(got) != string(value) {
		t.Errorf("got %q, want %q", got, value)
	}

	if !cache.Exists(key) {
		t.Error("Exists returned false for existing key")
	}

	if cache.Delete(key) != true {
		t.Error("Delete returned false for existing key")
	}

	if _, ok := cache.Get(key); ok {
		t.Error("Get returned ok after Delete")
	}
}

func TestShardedCache_Expiration(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	cfg.SampledEvict = 0
	cache := NewShardedCache(cfg)

	key := "expiring"
	value := []byte("value")

	if err := cache.Set(key, value, 50*time.Millisecond); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	if _, ok := cache.Get(key); !ok {
		t.Error("key should exist before expiry")
	}

	time.Sleep(100 * time.Millisecond)

	if _, ok := cache.Get(key); ok {
		t.Error("key should not exist after expiry")
	}
}

func TestShardedCache_SetNX(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	cfg.SampledEvict = 0
	cache := NewShardedCache(cfg)

	key := "setnx"
	value1 := []byte("first")
	value2 := []byte("second")

	ok, err := cache.SetNX(key, value1, 0)
	if err != nil {
		t.Fatalf("SetNX failed: %v", err)
	}
	if !ok {
		t.Error("first SetNX should succeed")
	}

	ok, err = cache.SetNX(key, value2, 0)
	if err != nil {
		t.Fatalf("SetNX failed: %v", err)
	}
	if ok {
		t.Error("second SetNX should fail")
	}

	got, _ := cache.Get(key)
	if string(got) != string(value1) {
		t.Errorf("got %q, want %q", got, value1)
	}
}

func TestShardedCache_SetNX_Expired(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	cfg.SampledEvict = 0
	cache := NewShardedCache(cfg)

	key := "setnx-exp"
	value1 := []byte("first")
	value2 := []byte("second")

	cache.SetNX(key, value1, 50*time.Millisecond)
	time.Sleep(100 * time.Millisecond)

	ok, _ := cache.SetNX(key, value2, 0)
	if !ok {
		t.Error("SetNX should succeed after expiry")
	}

	got, _ := cache.Get(key)
	if string(got) != string(value2) {
		t.Errorf("got %q, want %q", got, value2)
	}
}

func TestShardedCache_SetXX(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	cfg.SampledEvict = 0
	cache := NewShardedCache(cfg)

	key := "setxx"
	value1 := []byte("first")
	value2 := []byte("second")

	ok, err := cache.SetXX(key, value1, 0)
	if err != nil {
		t.Fatalf("SetXX failed: %v", err)
	}
	if ok {
		t.Error("SetXX should fail when key does not exist")
	}

	if err := cache.Set(key, value1, 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	ok, err = cache.SetXX(key, value2, 0)
	if err != nil {
		t.Fatalf("SetXX failed: %v", err)
	}
	if !ok {
		t.Error("SetXX should succeed when key exists")
	}

	got, _ := cache.Get(key)
	if string(got) != string(value2) {
		t.Errorf("got %q, want %q", got, value2)
	}
}

func TestShardedCache_GetCopy(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	cfg.SampledEvict = 0
	cache := NewShardedCache(cfg)

	key := "copy"
	value := []byte("original")

	cache.Set(key, value, 0)

	got, ok := cache.GetCopy(key)
	if !ok {
		t.Fatal("GetCopy returned not ok")
	}

	got[0] = 'X'

	original, _ := cache.Get(key)
	if original[0] == 'X' {
		t.Error("GetCopy should return independent copy")
	}
}

func TestShardedCache_TTL(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	cfg.SampledEvict = 0
	cache := NewShardedCache(cfg)

	key := "ttl"
	value := []byte("value")

	if ttl, ok := cache.TTL(key); ok || ttl != -2*time.Second {
		t.Errorf("expected missing key ttl -2s, got %v ok=%v", ttl, ok)
	}

	cache.Set(key, value, 0)
	if ttl, ok := cache.TTL(key); !ok || ttl != -1 {
		t.Errorf("expected ttl -1 for no expiry, got %v ok=%v", ttl, ok)
	}

	cache.Set(key, value, 100*time.Millisecond)
	if ttl, ok := cache.TTL(key); !ok || ttl <= 0 {
		t.Errorf("expected positive ttl, got %v ok=%v", ttl, ok)
	}

	time.Sleep(150 * time.Millisecond)
	if ttl, ok := cache.TTL(key); ok || ttl != -2*time.Second {
		t.Errorf("expected expired ttl -2s, got %v ok=%v", ttl, ok)
	}
}

func TestShardedCache_Len(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	cfg.SampledEvict = 0
	cache := NewShardedCache(cfg)

	for i := 0; i < 100; i++ {
		cache.Set(fmt.Sprintf("key%d", i), []byte("value"), 0)
	}

	if cache.Len() != 100 {
		t.Errorf("expected 100, got %d", cache.Len())
	}
}

func TestShardedCache_Clear(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	cfg.SampledEvict = 0
	cache := NewShardedCache(cfg)

	for i := 0; i < 100; i++ {
		cache.Set(fmt.Sprintf("key%d", i), []byte("value"), 0)
	}

	cache.Clear()

	if cache.Len() != 0 {
		t.Errorf("expected 0 after clear, got %d", cache.Len())
	}
}

func TestShardedCache_KeysAndRandomKeys(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	cfg.SampledEvict = 0
	cache := NewShardedCache(cfg)

	keys := []string{"alpha", "beta", "gamma"}
	for _, k := range keys {
		cache.Set(k, []byte("value"), 0)
	}

	all := cache.Keys("*")
	if len(all) != len(keys) {
		t.Errorf("expected %d keys, got %d", len(keys), len(all))
	}

	random := cache.RandomKeys(2)
	if len(random) != 2 {
		t.Errorf("expected 2 random keys, got %d", len(random))
	}
}

func TestShardedCache_ConcurrentAccess(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	cfg.SampledEvict = 0
	cache := NewShardedCache(cfg)

	const goroutines = 10
	const opsPerGoroutine = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := fmt.Sprintf("key-%d-%d", id, i)
				cache.Set(key, []byte("value"), 0)
			}
		}(g)

		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := fmt.Sprintf("key-%d-%d", id, i)
				cache.Get(key)
			}
		}(g)
	}

	wg.Wait()
}

func TestShardedCache_Stats(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	cfg.SampledEvict = 0
	cache := NewShardedCache(cfg)

	for i := 0; i < 100; i++ {
		cache.Set(fmt.Sprintf("key%d", i), []byte("value"), 0)
	}

	stats := cache.Stats()
	if stats.KeyCount != 100 {
		t.Errorf("expected KeyCount=100, got %d", stats.KeyCount)
	}
	if stats.TotalWrites != 100 {
		t.Errorf("expected TotalWrites=100, got %d", stats.TotalWrites)
	}
}

func TestShardedCache_Frequency(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	cfg.SampledEvict = 0
	cache := NewShardedCache(cfg)

	key := "hotkey"
	cache.Set(key, []byte("value"), 0)

	for i := 0; i < 10; i++ {
		cache.Get(key)
	}

	freq := cache.Frequency(key)
	if freq < 5 {
		t.Errorf("expected frequency >= 5, got %d", freq)
	}
}

func TestShardedCache_SampledEvict(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	cfg.SampledEvict = 0
	cache := NewShardedCache(cfg)

	for i := 0; i < 10; i++ {
		cache.Set(fmt.Sprintf("key%d", i), []byte("value"), 0)
	}

	before := cache.Len()
	if before == 0 {
		t.Fatal("expected keys before eviction")
	}

	hash := cache.hash("key0")
	cache.SampledEvict(hash)

	after := cache.Len()
	if after >= before {
		t.Error("expected key count to decrease after eviction")
	}
}

func TestShardedCache_SampledEvictVolatile(t *testing.T) {
	cfg := DefaultShardedCacheConfig()
	cfg.SampledEvict = 0
	cache := NewShardedCache(cfg)

	cache.Set("persist", []byte("value"), 0)
	cache.Set("temp", []byte("value"), 50*time.Millisecond)

	before := cache.Len()
	if before != 2 {
		t.Fatalf("expected 2 keys, got %d", before)
	}

	hash := cache.hash("temp")
	cache.SampledEvictVolatile(hash)

	after := cache.Len()
	if after >= before {
		t.Error("expected key count to decrease after volatile eviction")
	}
}

// === BENCHMARKS ===

func BenchmarkCountMinSketch_Increment(b *testing.B) {
	cms := NewCountMinSketch(1 << 16)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		cms.Increment(uint64(i))
	}
}

func BenchmarkCountMinSketch_Estimate(b *testing.B) {
	cms := NewCountMinSketch(1 << 16)
	for i := 0; i < 10000; i++ {
		cms.Increment(uint64(i))
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		cms.Estimate(uint64(i % 10000))
	}
}

func BenchmarkShardedCache_Get(b *testing.B) {
	cache := NewShardedCache(DefaultShardedCacheConfig())
	key := "benchkey"
	cache.Set(key, []byte("benchvalue"), 0)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		cache.Get(key)
	}
}

func BenchmarkShardedCache_Set(b *testing.B) {
	cache := NewShardedCache(DefaultShardedCacheConfig())
	value := []byte("benchvalue12345678901234567890")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		cache.Set("key", value, 0)
	}
}

func BenchmarkShardedCache_GetSet_Mixed(b *testing.B) {
	cache := NewShardedCache(DefaultShardedCacheConfig())
	value := []byte("benchvalue")

	for i := 0; i < 1000; i++ {
		cache.Set(fmt.Sprintf("key%d", i), value, 0)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if i%5 == 0 {
			cache.Set(fmt.Sprintf("key%d", i%1000), value, 0)
		} else {
			cache.Get(fmt.Sprintf("key%d", i%1000))
		}
	}
}

func BenchmarkShardedCache_Get_Parallel(b *testing.B) {
	cache := NewShardedCache(DefaultShardedCacheConfig())

	for i := 0; i < 1000; i++ {
		cache.Set(fmt.Sprintf("key%d", i), []byte("value"), 0)
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Get(fmt.Sprintf("key%d", i%1000))
			i++
		}
	})
}

func BenchmarkShardedCache_Set_Parallel(b *testing.B) {
	cache := NewShardedCache(DefaultShardedCacheConfig())
	value := []byte("benchvalue12345678901234567890")

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Set(fmt.Sprintf("key%d", i%1000), value, 0)
			i++
		}
	})
}

// Compare with original Dict
func BenchmarkDict_Get_Compare(b *testing.B) {
	dict := NewDict(256)
	key := "benchkey"
	dict.Set(key, "benchvalue", 0)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		dict.Get(key)
	}
}

func BenchmarkDict_Set_Compare(b *testing.B) {
	dict := NewDict(256)
	value := "benchvalue12345678901234567890"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		dict.Set("key", value, 0)
	}
}
