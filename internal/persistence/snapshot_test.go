package persistence

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotWriter_Save(t *testing.T) {
	dir := t.TempDir()
	sw := NewSnapshotWriter(dir)

	data := map[string][]byte{
		"key1": []byte("value1"),
		"key2": []byte("value2"),
	}

	err := sw.Save(func() (map[string][]byte, error) {
		return data, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "dump.rdb")); err != nil {
		t.Fatal("snapshot file not found:", err)
	}
	if sw.LastSaveUnix() == 0 {
		t.Error("expected non-zero last save time")
	}
}

func TestSnapshotWriter_ConcurrentSaveRejected(t *testing.T) {
	sw := NewSnapshotWriter(t.TempDir())
	sw.saving = true
	err := sw.Save(func() (map[string][]byte, error) {
		return nil, nil
	})
	if err == nil {
		t.Error("expected error when save already in progress")
	}
}

func TestSnapshotWriter_EmptyData(t *testing.T) {
	sw := NewSnapshotWriter(t.TempDir())
	err := sw.Save(func() (map[string][]byte, error) {
		return map[string][]byte{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if sw.LastSaveUnix() == 0 {
		t.Error("expected non-zero last save time even with empty data")
	}
}

func TestNewSnapshotWriter(t *testing.T) {
	sw := NewSnapshotWriter("/tmp/test")
	if sw == nil {
		t.Fatal("expected non-nil writer")
	}
	if sw.LastSaveUnix() != 0 {
		t.Error("expected zero initial last save time")
	}
	if sw.IsSaving() {
		t.Error("expected not saving initially")
	}
}

