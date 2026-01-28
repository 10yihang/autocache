package tiered

import (
	"sync"
	"testing"
	"time"
)

func TestStatsCollector(t *testing.T) {
	sc := NewStatsCollector(1.0)
	defer sc.Stop()

	// Record Access
	sc.RecordAccess("key1", 100)
	sc.RecordAccess("key1", 100)
	sc.RecordWrite("key2", 200)

	// Check Stats
	stats1 := sc.GetStats("key1")
	if stats1.AccessCount != 2 {
		t.Errorf("AccessCount mismatch: got %d, want 2", stats1.AccessCount)
	}

	stats2 := sc.GetStats("key2")
	if stats2.Size != 200 {
		t.Errorf("Size mismatch: got %d, want 200", stats2.Size)
	}

	// Check Frequency
	freq := sc.GetFrequency("key1")
	if freq < 2 {
		t.Errorf("Frequency estimate low: got %d, want >= 2", freq)
	}
}

func TestStatsCollector_GetColdKeys(t *testing.T) {
	sc := NewStatsCollector(1.0)
	defer sc.Stop()

	sc.RecordWrite("cold", 100)
	sc.RecordWrite("hot", 100)

	// Simulate "hot" access
	sc.RecordAccess("hot", 100)
	sc.RecordAccess("hot", 100)
	sc.RecordAccess("hot", 100)
	sc.RecordAccess("hot", 100)
	sc.RecordAccess("hot", 100)

	// Update time manually? Can't easily mock time in current implementation.
	// But GetColdKeys logic uses LastAccessTime.
	// We can manually set LastAccessTime in the map for testing purposes
	// if we expose it or use reflection, but simpler is to rely on logic:
	// "cold" has 1 access (write). "hot" has 6 accesses.

	// Wait a bit to ensure non-zero duration
	time.Sleep(10 * time.Millisecond)

	// Thresholds: Idle > 1ms, Access < 5
	coldKeys := sc.GetColdKeys(1*time.Millisecond, 5)

	found := false
	for _, k := range coldKeys {
		if k == "cold" {
			found = true
		}
		if k == "hot" {
			t.Error("Hot key should not be returned as cold")
		}
	}

	if !found {
		t.Error("Cold key not found")
	}
}

func TestStatsCollector_Concurrency(t *testing.T) {
	sc := NewStatsCollector(1.0)
	defer sc.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sc.RecordAccess("key", 100)
		}(i)
	}
	wg.Wait()

	stats := sc.GetStats("key")
	if stats.AccessCount != 100 {
		t.Errorf("Concurrent access count mismatch: got %d, want 100", stats.AccessCount)
	}
}
