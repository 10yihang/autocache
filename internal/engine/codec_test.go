package engine

import (
	"bytes"
	"encoding/gob"
	"testing"
	"time"
)

func TestEntryGobRoundTripWithoutBackendImport(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
	}{
		{name: "hash", value: map[string]string{"field": "value"}},
		{name: "list", value: []string{"a", "b"}},
		{name: "set", value: map[string]struct{}{"x": {}}},
		{name: "zset", value: map[string]float64{"member": 1.5}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := Entry{
				Key:       tt.name,
				Value:     tt.value,
				ExpireAt:  time.Now().Add(time.Minute).Round(0),
				CreatedAt: time.Now().Round(0),
				UpdatedAt: time.Now().Round(0),
			}

			var buf bytes.Buffer
			if err := gob.NewEncoder(&buf).Encode(want); err != nil {
				t.Fatalf("Encode failed: %v", err)
			}

			var got Entry
			if err := gob.NewDecoder(&buf).Decode(&got); err != nil {
				t.Fatalf("Decode failed: %v", err)
			}
		})
	}
}
