package badger

import (
	"context"
	"os"
	"testing"

	"github.com/10yihang/autocache/internal/engine"
)

func TestStore_Set_PreservesEntryType(t *testing.T) {
	dir, err := os.MkdirTemp("", "badger-type-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	tests := []struct {
		name  string
		key   string
		value interface{}
		want  engine.ValueType
	}{
		{name: "string", key: "s", value: "value", want: engine.TypeString},
		{name: "hash", key: "h", value: map[string]string{"field": "value"}, want: engine.TypeHash},
		{name: "list", key: "l", value: []string{"a", "b"}, want: engine.TypeList},
		{name: "set", key: "set", value: map[string]struct{}{"x": {}}, want: engine.TypeSet},
		{name: "zset", key: "z", value: map[string]float64{"m": 1}, want: engine.TypeZSet},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.Set(ctx, tt.key, tt.value, 0); err != nil {
				t.Fatalf("Set failed: %v", err)
			}
			entry, err := store.Get(ctx, tt.key)
			if err != nil {
				t.Fatalf("Get failed: %v", err)
			}
			if entry.Type != tt.want {
				t.Fatalf("entry.Type = %v, want %v", entry.Type, tt.want)
			}
		})
	}
}
