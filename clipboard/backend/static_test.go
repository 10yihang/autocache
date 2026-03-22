package clipboard

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedStaticAssets(t *testing.T) {
	t.Parallel()

	indexHTML, err := fs.ReadFile(StaticFS(), "embed/index.html")
	if err != nil {
		t.Fatalf("ReadFile(index.html) error = %v", err)
	}

	content := string(indexHTML)
	checks := []string{
		`id="root"`,
		`assets/index-`,
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Fatalf("index.html missing %q", check)
		}
	}
}
