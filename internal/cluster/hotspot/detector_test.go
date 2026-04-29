package hotspot

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDetectorBasic(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SampleInterval = 100 * time.Millisecond
	cfg.MinHotQPS = 10
	d := New(cfg)
	defer d.Stop()

	// Warmup.
	for tick := 0; tick < samplesWarmup+2; tick++ {
		for s := 0; s < 50; s++ {
			d.Record(uint16(s), 1)
		}
		time.Sleep(120 * time.Millisecond)
	}

	// Slot 100: 3000 req/window. Others: 10 req/window across 50 slots.
	for tick := 0; tick < 6; tick++ {
		for i := 0; i < 3000; i++ {
			d.Record(100, 64)
		}
		for s := 0; s < 50; s++ {
			for i := 0; i < 10; i++ {
				d.Record(uint16(s), 64)
			}
		}
		time.Sleep(120 * time.Millisecond)
	}

	hot := d.HotSlots()
	found := false
	for _, h := range hot {
		t.Logf("hot slot %d: qps=%.0f avg=%.0f score=%.2f", h.Slot, h.QPS, h.AvgQPS, h.Score)
		if h.Slot == 100 {
			found = true
		}
	}
	if !found {
		t.Fatal("slot 100 should be hot")
	}
}

func TestDetectorIdleDecay(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SampleInterval = 100 * time.Millisecond
	cfg.MinHotQPS = 10
	d := New(cfg)
	defer d.Stop()

	for tick := 0; tick < samplesWarmup+2; tick++ {
		for s := 0; s < 50; s++ {
			d.Record(uint16(s), 1)
		}
		time.Sleep(120 * time.Millisecond)
	}

	for tick := 0; tick < 6; tick++ {
		for i := 0; i < 5000; i++ {
			d.Record(42, 32)
		}
		for s := 0; s < 5; s++ {
			for i := 0; i < 10; i++ {
				d.Record(uint16(s), 32)
			}
		}
		time.Sleep(120 * time.Millisecond)
	}

	hot := d.HotSlots()
	found := false
	for _, h := range hot {
		t.Logf("idle: hot slot %d: qps=%.0f avg=%.0f score=%.2f", h.Slot, h.QPS, h.AvgQPS, h.Score)
		if h.Slot == 42 {
			found = true
		}
	}
	if !found {
		t.Fatal("slot 42 should be hot")
	}

	for tick := 0; tick < 25; tick++ {
		time.Sleep(120 * time.Millisecond)
	}

	hot = d.HotSlots()
	for _, h := range hot {
		if h.Slot == 42 {
			t.Error("slot 42 should not be hot after idle")
		}
	}
}

func TestDetectorExcludesBelowMinQPS(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SampleInterval = 100 * time.Millisecond
	cfg.MinHotQPS = 10000
	d := New(cfg)
	defer d.Stop()

	for tick := 0; tick < samplesWarmup+5; tick++ {
		for i := 0; i < 50; i++ {
			d.Record(1, 64)
		}
		time.Sleep(120 * time.Millisecond)
	}

	hot := d.HotSlots()
	if len(hot) != 0 {
		t.Errorf("expected no hot slots, got %d", len(hot))
	}
}

func TestDetectorMaxHotSlots(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SampleInterval = 100 * time.Millisecond
	cfg.MaxHotSlots = 3
	d := New(cfg)
	defer d.Stop()

	for tick := 0; tick < samplesWarmup+2; tick++ {
		for s := 0; s < 50; s++ {
			d.Record(uint16(s), 1)
		}
		time.Sleep(120 * time.Millisecond)
	}

	// 5 hot slots with 4000 req each, 45 cold with 10 req each.
	for tick := 0; tick < 6; tick++ {
		for s := 0; s < 5; s++ {
			for i := 0; i < 4000; i++ {
				d.Record(uint16(s), 32)
			}
		}
		for s := 5; s < 50; s++ {
			for i := 0; i < 10; i++ {
				d.Record(uint16(s), 32)
			}
		}
		time.Sleep(120 * time.Millisecond)
	}

	hot := d.HotSlots()
	for _, h := range hot {
		t.Logf("hot slot %d: qps=%.0f avg=%.0f score=%.2f", h.Slot, h.QPS, h.AvgQPS, h.Score)
	}
	if len(hot) > cfg.MaxHotSlots {
		t.Errorf("got %d, max %d", len(hot), cfg.MaxHotSlots)
	}
	for i := 1; i < len(hot); i++ {
		if hot[i].Score > hot[i-1].Score {
			t.Error("not sorted descending")
		}
	}
}

func TestDetectorOOB(t *testing.T) {
	d := New(DefaultConfig())
	defer d.Stop()
	d.Record(slotCount, 64)
	d.Record(slotCount+1, 64)
}

func TestDetectorEmptyCluster(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SampleInterval = 100 * time.Millisecond
	d := New(cfg)
	defer d.Stop()

	time.Sleep(time.Duration(samplesWarmup+3) * 120 * time.Millisecond)

	hot := d.HotSlots()
	if len(hot) != 0 {
		t.Errorf("expected no hot slots, got %d", len(hot))
	}
}

func TestDetectorConcurrent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SampleInterval = 100 * time.Millisecond
	d := New(cfg)
	defer d.Stop()

	for tick := 0; tick < samplesWarmup+2; tick++ {
		for s := 0; s < 50; s++ {
			d.Record(uint16(s), 1)
		}
		time.Sleep(120 * time.Millisecond)
	}

	// Hot slot 50: 4000 req/window vs cold slots 0-19: 3 req/window.
	for tick := 0; tick < 5; tick++ {
		for i := 0; i < 4000; i++ {
			d.Record(50, 32)
		}
		for s := 0; s < 20; s++ {
			for i := 0; i < 3; i++ {
				d.Record(uint16(s), 32)
			}
		}
		time.Sleep(120 * time.Millisecond)
	}

	hot := d.HotSlots()
	t.Logf("detected %d hot slots", len(hot))
	for _, h := range hot {
		t.Logf("  slot %d: qps=%.0f avg=%.0f score=%.2f", h.Slot, h.QPS, h.AvgQPS, h.Score)
	}
	found := false
	for _, h := range hot {
		if h.Slot == 50 {
			found = true
		}
	}
	if !found {
		t.Fatal("slot 50 should be hot")
	}

	// Concurrent safety: Record from multiple goroutines verifies no race.
	var wg sync.WaitGroup
	var ops atomic.Uint64
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(slot uint16) {
			defer wg.Done()
			for i := 0; i < 100000; i++ {
				d.Record(slot, 32)
				ops.Add(1)
			}
		}(uint16(g))
	}
	wg.Wait()
	t.Logf("concurrent records: %d", ops.Load())
}

func BenchmarkRecord(b *testing.B) {
	d := New(DefaultConfig())
	defer d.Stop()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Record(uint16(i%slotCount), 64)
	}
}

func BenchmarkRecordParallel(b *testing.B) {
	d := New(DefaultConfig())
	defer d.Stop()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			d.Record(uint16(i%slotCount), 64)
			i++
		}
	})
}
