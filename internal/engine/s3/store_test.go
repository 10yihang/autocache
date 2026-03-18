package s3

import "testing"

func TestNewStore_ReturnsExperimentalError(t *testing.T) {
	_, err := NewStore("http://127.0.0.1:9000", "autocache")
	if err == nil {
		t.Fatal("expected NewStore to fail for experimental cold tier")
	}
}
