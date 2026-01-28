package memory

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestDict_BasicOperations(t *testing.T) {
	d := NewDict(256)

	d.Set("key1", "value1", 0)
	entry, ok := d.Get("key1")
	if !ok {
		t.Fatal("expected key1 to exist")
	}
	if entry.Value.(string) != "value1" {
		t.Fatalf("expected value1, got %v", entry.Value)
	}

	d.Del("key1")
	_, ok = d.Get("key1")
	if ok {
		t.Fatal("expected key1 to be deleted")
	}
}

func TestDict_SetNX(t *testing.T) {
	d := NewDict(256)

	ok := d.SetNX("key1", "value1", 0)
	if !ok {
		t.Fatal("SetNX should succeed on new key")
	}

	ok = d.SetNX("key1", "value2", 0)
	if ok {
		t.Fatal("SetNX should fail on existing key")
	}

	entry, _ := d.Get("key1")
	if entry.Value.(string) != "value1" {
		t.Fatalf("expected value1, got %v", entry.Value)
	}
}

func TestDict_SetXX(t *testing.T) {
	d := NewDict(256)

	ok := d.SetXX("key1", "value1", 0)
	if ok {
		t.Fatal("SetXX should fail on non-existing key")
	}

	d.Set("key1", "value1", 0)
	ok = d.SetXX("key1", "value2", 0)
	if !ok {
		t.Fatal("SetXX should succeed on existing key")
	}

	entry, _ := d.Get("key1")
	if entry.Value.(string) != "value2" {
		t.Fatalf("expected value2, got %v", entry.Value)
	}
}

func TestDict_Expiration(t *testing.T) {
	d := NewDict(256)

	d.Set("key1", "value1", 50*time.Millisecond)
	_, ok := d.Get("key1")
	if !ok {
		t.Fatal("key1 should exist")
	}

	time.Sleep(100 * time.Millisecond)
	_, ok = d.Get("key1")
	if ok {
		t.Fatal("key1 should be expired")
	}
}

func TestDict_ConcurrentAccess(t *testing.T) {
	d := NewDict(256)
	var wg sync.WaitGroup
	numGoroutines := 100
	numOps := 1000

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				d.Set(key, "value", 0)
				d.Get(key)
				d.Del(key)
			}
		}(i)
	}

	wg.Wait()
}

func BenchmarkDict_Get(b *testing.B) {
	d := NewDict(256)
	for i := 0; i < 10000; i++ {
		d.Set(fmt.Sprintf("key%d", i), "value", 0)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			d.Get(fmt.Sprintf("key%d", i%10000))
			i++
		}
	})
}

func BenchmarkDict_Set(b *testing.B) {
	d := NewDict(256)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			d.Set(fmt.Sprintf("key%d", i), "value", 0)
			i++
		}
	})
}

func BenchmarkDict_SetNX(b *testing.B) {
	d := NewDict(256)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			d.SetNX(fmt.Sprintf("key%d", i), "value", 0)
			i++
		}
	})
}

func BenchmarkDict_GetShard(b *testing.B) {
	d := NewDict(256)
	keys := make([]string, 10000)
	for i := 0; i < 10000; i++ {
		keys[i] = fmt.Sprintf("key%d", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.getShard(keys[i%10000])
	}
}

func BenchmarkDict_MixedWorkload(b *testing.B) {
	d := NewDict(256)
	for i := 0; i < 10000; i++ {
		d.Set(fmt.Sprintf("key%d", i), "value", 0)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key%d", i%10000)
			if i%10 < 8 {
				d.Get(key)
			} else {
				d.Set(key, "newvalue", 0)
			}
			i++
		}
	})
}
