package memory

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

func TestEntryHeader_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		header EntryHeader
	}{
		{
			name: "basic",
			header: EntryHeader{
				ExpireAt: time.Now().UnixNano(),
				Hash64:   0x123456789ABCDEF0,
				KeyLen:   10,
				ValLen:   100,
				ValCap:   128,
				Flags:    EntryTypeString,
			},
		},
		{
			name: "no expiry",
			header: EntryHeader{
				ExpireAt: 0,
				Hash64:   0xDEADBEEF,
				KeyLen:   5,
				ValLen:   50,
				ValCap:   64,
				Flags:    EntryTypeList,
			},
		},
		{
			name: "max values",
			header: EntryHeader{
				ExpireAt: 1<<63 - 1,
				Hash64:   1<<64 - 1,
				KeyLen:   65535,
				ValLen:   65535,
				ValCap:   65535,
				Flags:    65535,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, headerSize)
			tt.header.MarshalTo(buf)

			var decoded EntryHeader
			decoded.UnmarshalFrom(buf)

			if decoded != tt.header {
				t.Errorf("got %+v, want %+v", decoded, tt.header)
			}
		})
	}
}

func TestRingBuffer_WriteRead(t *testing.T) {
	rb := NewRingBuffer(minRingBufferSize)

	key := []byte("testkey")
	value := []byte("testvalue123")
	header := EntryHeader{
		ExpireAt: time.Now().Add(time.Hour).UnixNano(),
		Hash64:   0xABCD1234,
		KeyLen:   uint16(len(key)),
		ValLen:   uint16(len(value)),
		Flags:    EntryTypeString,
	}

	// Write
	offset, err := rb.Write(&header, key, value)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if offset != 0 {
		t.Errorf("expected offset 0, got %d", offset)
	}

	// Read back (zero-copy)
	readKey, readVal, readHeader, err := rb.ReadAt(offset)
	if err != nil {
		t.Fatalf("ReadAt failed: %v", err)
	}

	if !bytes.Equal(readKey, key) {
		t.Errorf("key mismatch: got %q, want %q", readKey, key)
	}
	if !bytes.Equal(readVal, value) {
		t.Errorf("value mismatch: got %q, want %q", readVal, value)
	}
	if readHeader.Hash64 != header.Hash64 {
		t.Errorf("hash mismatch: got %x, want %x", readHeader.Hash64, header.Hash64)
	}
}

func TestRingBuffer_MultipleEntries(t *testing.T) {
	rb := NewRingBuffer(minRingBufferSize)

	entries := []struct {
		key   string
		value string
	}{
		{"key1", "value1"},
		{"key2", "value2value2"},
		{"key3", "v3"},
		{"longerkey4", "this is a longer value for testing"},
	}

	offsets := make([]int64, len(entries))

	// Write all entries
	for i, e := range entries {
		key := []byte(e.key)
		val := []byte(e.value)
		header := EntryHeader{
			Hash64: uint64(i),
			KeyLen: uint16(len(key)),
			ValLen: uint16(len(val)),
		}
		offset, err := rb.Write(&header, key, val)
		if err != nil {
			t.Fatalf("Write %d failed: %v", i, err)
		}
		offsets[i] = offset
	}

	// Read all entries back
	for i, e := range entries {
		readKey, readVal, _, err := rb.ReadAt(offsets[i])
		if err != nil {
			t.Fatalf("ReadAt %d failed: %v", i, err)
		}
		if string(readKey) != e.key {
			t.Errorf("entry %d: key mismatch: got %q, want %q", i, readKey, e.key)
		}
		if string(readVal) != e.value {
			t.Errorf("entry %d: value mismatch: got %q, want %q", i, readVal, e.value)
		}
	}
}

func TestRingBuffer_WrapAround(t *testing.T) {
	// Small buffer to force wrap
	capacity := 1 << 20 // 1MB minimum
	rb := NewRingBuffer(capacity)

	// Write entries until we wrap
	key := []byte("key")
	value := make([]byte, 10000) // 10KB value
	for i := range value {
		value[i] = byte(i % 256)
	}

	header := EntryHeader{
		KeyLen: uint16(len(key)),
		ValLen: uint16(len(value)),
	}

	entrySize := int64(headerSize + len(key) + len(value))
	entriesBeforeWrap := int64(capacity) / entrySize

	var wrapDetected bool
	var prevOffset int64 = -1
	var lastOffset int64

	for i := int64(0); i < entriesBeforeWrap+10; i++ {
		offset, err := rb.Write(&header, key, value)
		if err != nil {
			t.Fatalf("Write %d failed: %v", i, err)
		}

		// Detect wrap: offset goes backwards
		if prevOffset >= 0 && offset < prevOffset {
			wrapDetected = true
		}
		prevOffset = offset
		lastOffset = offset
	}

	if !wrapDetected {
		t.Error("expected buffer to wrap around, but it didn't")
	}

	// Verify we can still read the last entry
	readKey, readVal, _, err := rb.ReadAt(lastOffset)
	if err != nil {
		t.Fatalf("ReadAt after wrap failed: %v", err)
	}
	if !bytes.Equal(readKey, key) {
		t.Error("key mismatch after wrap")
	}
	if !bytes.Equal(readVal, value) {
		t.Error("value mismatch after wrap")
	}

	// Verify evict count increased
	_, evicts := rb.Stats()
	if evicts == 0 {
		t.Error("expected evict count > 0 after wrap")
	}
}

func TestRingBuffer_ConcurrentWrite(t *testing.T) {
	rb := NewRingBuffer(8 << 20) // 8MB

	const goroutines = 10
	const writesPerGoroutine = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()

			key := []byte("key")
			value := make([]byte, 100)

			header := EntryHeader{
				Hash64: uint64(id),
				KeyLen: uint16(len(key)),
				ValLen: uint16(len(value)),
			}

			for i := 0; i < writesPerGoroutine; i++ {
				_, err := rb.Write(&header, key, value)
				if err != nil {
					t.Errorf("goroutine %d, write %d failed: %v", id, i, err)
					return
				}
			}
		}(g)
	}

	wg.Wait()

	writes, _ := rb.Stats()
	expectedWrites := int64(goroutines * writesPerGoroutine)
	if writes != expectedWrites {
		t.Errorf("expected %d writes, got %d", expectedWrites, writes)
	}
}

func TestRingBuffer_EntryTooLarge(t *testing.T) {
	rb := NewRingBuffer(minRingBufferSize)

	// Entry larger than buffer
	key := []byte("key")
	value := make([]byte, minRingBufferSize) // Same as capacity

	header := EntryHeader{
		KeyLen: uint16(len(key)),
		ValLen: uint16(len(value)),
	}

	_, err := rb.Write(&header, key, value)
	if err != ErrEntryTooLarge {
		t.Errorf("expected ErrEntryTooLarge, got %v", err)
	}
}

func TestRingBuffer_InvalidOffset(t *testing.T) {
	rb := NewRingBuffer(minRingBufferSize)

	tests := []int64{-1, -100, rb.Capacity() + 1, rb.Capacity() * 2}

	for _, offset := range tests {
		_, _, _, err := rb.ReadAt(offset)
		if err != ErrInvalidOffset {
			t.Errorf("offset %d: expected ErrInvalidOffset, got %v", offset, err)
		}
	}
}

func TestRingBuffer_ReadAtCopy(t *testing.T) {
	rb := NewRingBuffer(minRingBufferSize)

	key := []byte("testkey")
	value := []byte("testvalue")
	header := EntryHeader{
		KeyLen: uint16(len(key)),
		ValLen: uint16(len(value)),
	}

	offset, _ := rb.Write(&header, key, value)

	// Read with copy
	readKey, readVal, _, err := rb.ReadAtCopy(offset)
	if err != nil {
		t.Fatalf("ReadAtCopy failed: %v", err)
	}

	// Modify original to verify we got a copy
	readKey[0] = 'X'
	readVal[0] = 'Y'

	// Read again and verify original is unchanged
	origKey, origVal, _, _ := rb.ReadAt(offset)
	if origKey[0] == 'X' {
		t.Error("ReadAtCopy should return a copy, not a view")
	}
	if origVal[0] == 'Y' {
		t.Error("ReadAtCopy should return a copy, not a view")
	}
}

// === UNSAFE TESTS ===

func TestStringToBytes(t *testing.T) {
	tests := []string{
		"",
		"hello",
		"hello world",
		"你好世界",
		"a",
	}

	for _, s := range tests {
		b := StringToBytes(s)
		if string(b) != s {
			t.Errorf("StringToBytes(%q) = %q", s, b)
		}
	}
}

func TestBytesToString(t *testing.T) {
	tests := [][]byte{
		nil,
		{},
		[]byte("hello"),
		[]byte("hello world"),
		[]byte("你好世界"),
	}

	for _, b := range tests {
		s := BytesToString(b)
		if s != string(b) {
			t.Errorf("BytesToString(%q) = %q", b, s)
		}
	}
}

// === BENCHMARKS ===

func BenchmarkEntryHeader_Marshal(b *testing.B) {
	header := EntryHeader{
		ExpireAt: time.Now().UnixNano(),
		Hash64:   0x123456789ABCDEF0,
		KeyLen:   10,
		ValLen:   100,
	}
	buf := make([]byte, headerSize)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		header.MarshalTo(buf)
	}
}

func BenchmarkEntryHeader_Unmarshal(b *testing.B) {
	header := EntryHeader{
		ExpireAt: time.Now().UnixNano(),
		Hash64:   0x123456789ABCDEF0,
		KeyLen:   10,
		ValLen:   100,
	}
	buf := make([]byte, headerSize)
	header.MarshalTo(buf)

	var decoded EntryHeader

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		decoded.UnmarshalFrom(buf)
	}
	_ = decoded
}

func BenchmarkRingBuffer_Write(b *testing.B) {
	rb := NewRingBuffer(64 << 20) // 64MB

	key := []byte("testkey")
	value := []byte("testvalue12345678901234567890")
	header := EntryHeader{
		KeyLen: uint16(len(key)),
		ValLen: uint16(len(value)),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		rb.Write(&header, key, value)
	}
}

func BenchmarkRingBuffer_ReadAt(b *testing.B) {
	rb := NewRingBuffer(64 << 20) // 64MB

	key := []byte("testkey")
	value := []byte("testvalue12345678901234567890")
	header := EntryHeader{
		KeyLen: uint16(len(key)),
		ValLen: uint16(len(value)),
	}

	offset, _ := rb.Write(&header, key, value)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		rb.ReadAt(offset)
	}
}

func BenchmarkRingBuffer_WriteRead(b *testing.B) {
	rb := NewRingBuffer(64 << 20) // 64MB

	key := []byte("testkey")
	value := []byte("testvalue12345678901234567890")
	header := EntryHeader{
		KeyLen: uint16(len(key)),
		ValLen: uint16(len(value)),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		offset, _ := rb.Write(&header, key, value)
		rb.ReadAt(offset)
	}
}

func BenchmarkStringToBytes(b *testing.B) {
	s := "this is a test string for benchmarking"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = StringToBytes(s)
	}
}

func BenchmarkBytesToString(b *testing.B) {
	data := []byte("this is a test string for benchmarking")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = BytesToString(data)
	}
}

// Compare with standard conversion
func BenchmarkStringToBytes_Standard(b *testing.B) {
	s := "this is a test string for benchmarking"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = []byte(s)
	}
}

func BenchmarkBytesToString_Standard(b *testing.B) {
	data := []byte("this is a test string for benchmarking")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = string(data)
	}
}

// Parallel benchmarks
func BenchmarkRingBuffer_Write_Parallel(b *testing.B) {
	rb := NewRingBuffer(128 << 20) // 128MB

	key := []byte("testkey")
	value := []byte("testvalue12345678901234567890")
	header := EntryHeader{
		KeyLen: uint16(len(key)),
		ValLen: uint16(len(value)),
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rb.Write(&header, key, value)
		}
	})
}
