package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestVersionFlagPrintsVersionAndExits(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("go", "run", ".", "-v")
	cmd.Dir = "/Users/huangyihang/Code/autocache/cmd/server"
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . -v failed: %v\noutput:\n%s", err, out)
	}

	got := strings.TrimSpace(string(out))
	if got == "" {
		t.Fatal("version output is empty")
	}
	if strings.Contains(got, "AutoCache server starting") {
		t.Fatalf("version flag started the server instead of exiting, output:\n%s", out)
	}
	if strings.Contains(got, "Usage of") {
		t.Fatalf("version flag printed usage instead of version, output:\n%s", out)
	}
	if !strings.Contains(got, "dev") {
		t.Fatalf("version output = %q, want it to include the version string", got)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("version output = %q, want a single line", got)
	}
	if got != "dev" {
		t.Fatalf("version output = %q, want %q", got, "dev")
	}
}

func TestVersionFlagRespectsInjectedVersion(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("go", "run", "-ldflags", "-X github.com/10yihang/autocache/internal/version.Version=v1.2.3-test", ".", "-v")
	cmd.Dir = "/Users/huangyihang/Code/autocache/cmd/server"
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run with injected version failed: %v\noutput:\n%s", err, out)
	}

	got := strings.TrimSpace(string(out))
	if got != "v1.2.3-test" {
		t.Fatalf("injected version output = %q, want %q", got, "v1.2.3-test")
	}
}
