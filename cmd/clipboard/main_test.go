package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestClipboardBinaryBuildsAndServes(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	root := repoRoot(t)
	backendAddr := freeLoopbackAddr(t)
	backendMetricsAddr := freeLoopbackAddr(t)
	clipboardAddr := freeLoopbackAddr(t)
	clipboardMetricsAddr := freeLoopbackAddr(t)

	serverBinary := buildBinary(t, root, filepath.Join(t.TempDir(), "autocache-server"), "./cmd/server")
	backendCmd := exec.CommandContext(ctx, serverBinary, "-addr", backendAddr, "-metrics-addr", backendMetricsAddr, "-cluster-enabled=false", "-quiet-connections=true")
	var backendOutput bytes.Buffer
	backendCmd.Stdout = &backendOutput
	backendCmd.Stderr = &backendOutput
	if err := backendCmd.Start(); err != nil {
		t.Fatalf("start backend server: %v", err)
	}
	defer stopCmd(t, backendCmd, &backendOutput)

	clipboardBinary := buildBinary(t, root, filepath.Join(t.TempDir(), "clipboard"), "./cmd/clipboard")
	clipboardCmd := exec.CommandContext(ctx, clipboardBinary,
		"-addr", clipboardAddr,
		"-backend-addr", backendAddr,
		"-metrics-addr", clipboardMetricsAddr,
		"-admin-token", "test-token",
	)
	var clipboardOutput bytes.Buffer
	clipboardCmd.Stdout = &clipboardOutput
	clipboardCmd.Stderr = &clipboardOutput
	if err := clipboardCmd.Start(); err != nil {
		t.Fatalf("start clipboard binary: %v", err)
	}
	defer stopCmd(t, clipboardCmd, &clipboardOutput)

	waitForHTTPStatus(t, ctx, "http://"+clipboardAddr+"/health", http.StatusOK)
	waitForHTTPStatus(t, ctx, "http://"+clipboardAddr+"/", http.StatusOK)

	resp, err := http.Get("http://" + clipboardAddr + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read index body: %v", err)
	}
	if !bytes.Contains(body, []byte("id=\"root\"")) {
		t.Fatalf("index body missing SPA root marker: %q", body)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("stat go.mod: %v", err)
	}
	return root
}

func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Fatalf("close listener: %v", err)
		}
	}()
	return listener.Addr().String()
}

func buildBinary(t *testing.T, root, outputPath, pkg string) string {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", outputPath, pkg)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, output)
	}
	return outputPath
}

func stopCmd(t *testing.T, cmd *exec.Cmd, output *bytes.Buffer) {
	t.Helper()
	if cmd.Process == nil {
		return
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
	if t.Failed() && output != nil {
		t.Logf("process output:\n%s", output.String())
	}
}

func waitForHTTPStatus(t *testing.T, ctx context.Context, url string, want int) {
	t.Helper()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == want {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for %s status %d: %v", url, want, ctx.Err())
		case <-ticker.C:
		}
	}
}
