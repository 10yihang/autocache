package hotspot

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func runWindows(d *Detector, cfg Config, windows int, fn func()) {
	for tick := 0; tick < windows; tick++ {
		fn()
		time.Sleep(cfg.SampleInterval + 20*time.Millisecond)
	}
}

func TestDetectorBasic(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SampleInterval = 50 * time.Millisecond
	cfg.MinHotQPS = 10
	d := New(cfg)
	defer d.Stop()

	// Warmup: baseline traffic everywhere.
	runWindows(d, cfg, samplesWarmup, func() {
		for s := 0; s < 20; s++ {
			d.Record(uint16(s), 32)
		}
	})

	// Slot 100: 1000 req/window, others: ~50 req/window across 20 slots.
	runWindows(d, cfg, 4, func() {
		for i := 0; i < 1000; i++ {
			d.Record(100, 64)
		}
		for s := 0; s < 20; s++ {
			d.Record(uint16(s), 64)
		}
	})

	hot := d.HotSlots()
	found := false
	for _, h := range hot {
		t.Logf("hot slot %d: qps=%.0f avg=%.0f score=%.2f", h.Slot, h.QPS, h.AvgQPS, h.Score)
		if h.Slot == 100 {
			found = true
			if h.Score < hotMultiplier {
				t.Errorf("slot 100 score %.2f below multiplier %.2f", h.Score, hotMultiplier)
			}
		}
	}
	if !found {
		t.Fatal("slot 100 should be hot")
	}
}

func TestDetectorIdleDecay(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SampleInterval = 50 * time.Millisecond
	cfg.MinHotQPS = 10
	d := New(cfg)
	defer d.Stop()

	// Warmup with uniform traffic.
	for tick := 0; tick < samplesWarmup; tick++ {
		for s := 0; s < 20; s++ {
			d.Record(uint16(s), 32)
		}
		time.Sleep(60 * time.Millisecond)
	}

	// Slot 42: 3000 req/window, others: 20 req/window across 5 slots.
	for tick := 0; tick < 6; tick++ {
		for i := 0; i < 3000; i++ {
			d.Record(42, 32)
		}
		for s := 0; s < 5; s++ {
			d.Record(uint16(s), 32)
		}
		time.Sleep(60 * time.Millisecond)
	}

	hot := d.HotSlots()
	found := false
	for _, h := range hot {
		t.Logf("hot slot %d: qps=%.0f avg=%.0f score=%.2f", h.Slot, h.QPS, h.AvgQPS, h.Score)
		if h.Slot == 42 {
			found = true
		}
	}
	if !found {
		t.Fatal("slot 42 should be hot")
	}

	// Idle — slot should cool down.
	for tick := 0; tick < 25; tick++ {
		time.Sleep(60 * time.Millisecond)
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
	cfg.SampleInterval = 50 * time.Millisecond
	cfg.MinHotQPS = 10000
	d := New(cfg)
	defer d.Stop()

	runWindows(d, cfg, samplesWarmup+4, func() {
		for i := 0; i < 50; i++ {
			d.Record(1, 64)
		}
	})

	hot := d.HotSlots()
	if len(hot) != 0 {
		t.Errorf("expected no hot slots, got %d", len(hot))
	}
}

func TestDetectorMaxHotSlots(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SampleInterval = 50 * time.Millisecond
	cfg.MaxHotSlots = 5
	d := New(cfg)
	defer d.Stop()

	for tick := 0; tick < samplesWarmup; tick++ {
		for s := range slotCount {
			d.Record(uint16(s), 1)
		}
		time.Sleep(60 * time.Millisecond)
	}

	// 3 hot slots (5000 req), 17 completely idle.
	// avg = 5000*3/3 = 5000, but per-slot comparison: slot QPS 5000 vs avg 5000 = 1.0 (not hot)
	// Need a mix: hot slots much higher than active average.
	// Use: 3 hot at 5000, 17 cold but active at 10 req.
	for tick := 0; tick < 6; tick++ {
		for s := 0; s < 3; s++ {
			for i := 0; i < 5000; i++ {
				d.Record(uint16(s), 32)
			}
		}
		for s := 3; s < 20; s++ {
			for i := 0; i < 10; i++ {
				d.Record(uint16(s), 32)
			}
		}
		time.Sleep(60 * time.Millisecond)
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
	cfg.SampleInterval = 50 * time.Millisecond
	d := New(cfg)
	defer d.Stop()

	runWindows(d, cfg, samplesWarmup+2, func() {})

	hot := d.HotSlots()
	if len(hot) != 0 {
		t.Errorf("expected no hot slots, got %d", len(hot))
	}
}

func TestDetectorConcurrent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SampleInterval = 50 * time.Millisecond
	d := New(cfg)
	defer d.Stop()

	// Warmup.
	for tick := 0; tick < samplesWarmup; tick++ {
		for s := 0; s < 20; s++ {
			d.Record(uint16(s), 1)
		}
		time.Sleep(60 * time.Millisecond)
	}

	// Hot slot 50: 5000 req/window. Cold slots 0-19: 3 req/window.
	for tick := 0; tick < 5; tick++ {
		for i := 0; i < 5000; i++ {
			d.Record(50, 32)
		}
		for s := 0; s < 20; s++ {
			for i := 0; i < 3; i++ {
				d.Record(uint16(s), 32)
			}
		}
		time.Sleep(60 * time.Millisecond)
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

	// Verify concurrent safety: Record from multiple goroutines.
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
	// No assertion — just verifying no race/panic.
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
