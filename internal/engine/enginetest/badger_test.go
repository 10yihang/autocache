package enginetest

import (
	"testing"

	"github.com/10yihang/autocache/internal/engine"
	"github.com/10yihang/autocache/internal/engine/badger"
)

func TestBadgerConformance(t *testing.T) {
	RunConformance(t, func(t *testing.T) engine.Engine {
		t.Helper()

		store, err := badger.NewStore(t.TempDir())
		if err != nil {
			t.Fatalf("NewStore failed: %v", err)
		}

		return store
	})
}
