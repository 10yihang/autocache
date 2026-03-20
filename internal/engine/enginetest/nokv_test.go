package enginetest

import (
	"testing"

	"github.com/10yihang/autocache/internal/engine"
	"github.com/10yihang/autocache/internal/engine/nokv"
)

func TestNoKVConformance(t *testing.T) {
	RunConformance(t, func(t *testing.T) engine.Engine {
		t.Helper()

		store, err := nokv.NewStore(t.TempDir())
		if err != nil {
			t.Fatalf("NewStore failed: %v", err)
		}

		return store
	})
}
