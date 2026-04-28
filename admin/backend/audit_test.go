package admin

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestAuditLog_WrapAround(t *testing.T) {
	size := 1024
	al := NewAuditLog(size)

	for i := 0; i < 1500; i++ {
		al.Record(AuditEntry{
			Timestamp: time.Now(),
			Action:    fmt.Sprintf("action-%04d", i),
		})
	}

	snap := al.Snapshot()
	if len(snap) != size {
		t.Fatalf("expected %d entries, got %d", size, len(snap))
	}

	if snap[0].Action != "action-0476" {
		t.Errorf("expected first entry action-0476, got %s", snap[0].Action)
	}
	if snap[size-1].Action != "action-1499" {
		t.Errorf("expected last entry action-1499, got %s", snap[size-1].Action)
	}

	for i := 1; i < len(snap); i++ {
		prev := snap[i-1].Action
		curr := snap[i].Action
		if prev >= curr {
			t.Fatalf("entries not in order at index %d: %s >= %s", i, prev, curr)
		}
	}
}

func TestAuditLog_UnderCapacity(t *testing.T) {
	al := NewAuditLog(100)

	for i := 0; i < 10; i++ {
		al.Record(AuditEntry{Action: fmt.Sprintf("a-%d", i)})
	}

	snap := al.Snapshot()
	if len(snap) != 10 {
		t.Fatalf("expected 10, got %d", len(snap))
	}
	if snap[0].Action != "a-0" {
		t.Errorf("expected a-0, got %s", snap[0].Action)
	}
	if snap[9].Action != "a-9" {
		t.Errorf("expected a-9, got %s", snap[9].Action)
	}
}

func TestAuditLog_Empty(t *testing.T) {
	al := NewAuditLog(10)
	snap := al.Snapshot()
	if snap != nil {
		t.Fatalf("expected nil for empty log, got %v", snap)
	}
}

func TestAuditLog_ConcurrentRecord(t *testing.T) {
	al := NewAuditLog(256)

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				al.Record(AuditEntry{
					Action: fmt.Sprintf("g%d-i%d", id, i),
				})
			}
		}(g)
	}
	wg.Wait()

	snap := al.Snapshot()
	if len(snap) != 256 {
		t.Fatalf("expected 256, got %d", len(snap))
	}
}

func TestAuditLog_SnapshotDeepCopy(t *testing.T) {
	al := NewAuditLog(10)
	al.Record(AuditEntry{Action: "original"})

	snap := al.Snapshot()
	snap[0].Action = "mutated"

	snap2 := al.Snapshot()
	if snap2[0].Action != "original" {
		t.Fatalf("snapshot was not a deep copy: got %s", snap2[0].Action)
	}
}
