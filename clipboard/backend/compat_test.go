package clipboard

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
)

func TestAutoCacheGoRedisCompatibility(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	addr := freeLoopbackAddr(t)
	metricsAddr := freeLoopbackAddr(t)
	serverBinary := buildAutoCacheServerBinary(t)
	serverCmd := startAutoCacheServer(t, ctx, serverBinary, addr, metricsAddr)
	defer stopCommand(t, serverCmd)

	client := newRedisClient(addr)
	defer func() {
		if err := client.Close(); err != nil {
			t.Fatalf("close redis client: %v", err)
		}
	}()

	if err := waitForPing(ctx, client); err != nil {
		t.Fatalf("wait for ping: %v", err)
	}

	key := "clipboard:compat:string"
	hashKey := "clipboard:compat:hash"
	setNXKey := "clipboard:compat:setnx"
	counterKey := "clipboard:compat:counter"

	if err := client.Set(ctx, key, "value", 0).Err(); err != nil {
		t.Fatalf("SET: %v", err)
	}

	got, err := client.Get(ctx, key).Result()
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	if got != "value" {
		t.Fatalf("GET returned %q, want %q", got, "value")
	}

	setNXResult, err := client.Do(ctx, "SETNX", setNXKey, "first").Int()
	if err != nil {
		t.Fatalf("SETNX first: %v", err)
	}
	if setNXResult != 1 {
		t.Fatalf("SETNX first returned %d, want 1", setNXResult)
	}

	setNXResult, err = client.Do(ctx, "SETNX", setNXKey, "second").Int()
	if err != nil {
		t.Fatalf("SETNX second: %v", err)
	}
	if setNXResult != 0 {
		t.Fatalf("SETNX second returned %d, want 0", setNXResult)
	}

	exists, err := client.Exists(ctx, key, setNXKey).Result()
	if err != nil {
		t.Fatalf("EXISTS: %v", err)
	}
	if exists != 2 {
		t.Fatalf("EXISTS returned %d, want 2", exists)
	}

	if err := client.HSet(ctx, hashKey, "content", "hello", "ttl", "5m").Err(); err != nil {
		t.Fatalf("HSET: %v", err)
	}

	hashValue, err := client.HGet(ctx, hashKey, "content").Result()
	if err != nil {
		t.Fatalf("HGET: %v", err)
	}
	if hashValue != "hello" {
		t.Fatalf("HGET returned %q, want %q", hashValue, "hello")
	}

	hashAll, err := client.HGetAll(ctx, hashKey).Result()
	if err != nil {
		t.Fatalf("HGETALL: %v", err)
	}
	if len(hashAll) != 2 || hashAll["content"] != "hello" || hashAll["ttl"] != "5m" {
		t.Fatalf("HGETALL returned %#v, want content+ttl fields", hashAll)
	}

	if err := client.Expire(ctx, key, 10*time.Second).Err(); err != nil {
		t.Fatalf("EXPIRE: %v", err)
	}

	ttl, err := client.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 {
		t.Fatalf("TTL returned %v, want positive duration", ttl)
	}

	incrValue, err := client.Incr(ctx, counterKey).Result()
	if err != nil {
		t.Fatalf("INCR: %v", err)
	}
	if incrValue != 1 {
		t.Fatalf("INCR returned %d, want 1", incrValue)
	}

	decrValue, err := client.Decr(ctx, counterKey).Result()
	if err != nil {
		t.Fatalf("DECR: %v", err)
	}
	if decrValue != 0 {
		t.Fatalf("DECR returned %d, want 0", decrValue)
	}

	decrByValue, err := client.DecrBy(ctx, counterKey, 2).Result()
	if err != nil {
		t.Fatalf("DECRBY: %v", err)
	}
	if decrByValue != -2 {
		t.Fatalf("DECRBY returned %d, want -2", decrByValue)
	}

	if deleted, err := client.Del(ctx, key, hashKey, setNXKey, counterKey).Result(); err != nil {
		t.Fatalf("DEL: %v", err)
	} else if deleted != 4 {
		t.Fatalf("DEL removed %d keys, want 4", deleted)
	}

	exists, err = client.Exists(ctx, key, hashKey, setNXKey, counterKey).Result()
	if err != nil {
		t.Fatalf("EXISTS after DEL: %v", err)
	}
	if exists != 0 {
		t.Fatalf("EXISTS after DEL returned %d, want 0", exists)
	}
}

func newRedisClient(addr string) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:            addr,
		Protocol:        2,
		DisableIdentity: true,
	})
}

func waitForPing(ctx context.Context, client *redis.Client) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := client.Ping(ctx).Err(); err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func buildAutoCacheServerBinary(t *testing.T) string {
	t.Helper()

	root := repoRoot(t)
	binaryPath := filepath.Join(t.TempDir(), "autocache-server")
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/server")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build autocache server: %v\n%s", err, output)
	}

	return binaryPath
}

func startAutoCacheServer(t *testing.T, ctx context.Context, binaryPath, addr, metricsAddr string) *exec.Cmd {
	t.Helper()

	cmd := exec.CommandContext(ctx, binaryPath, "-addr", addr, "-metrics-addr", metricsAddr, "-cluster-enabled=false", "-quiet-connections=true")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Start(); err != nil {
		t.Fatalf("start autocache server: %v", err)
	}

	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("autocache server output:\n%s", output.String())
		}
	})

	return cmd
}

func stopCommand(t *testing.T, cmd *exec.Cmd) {
	t.Helper()

	if cmd.Process == nil {
		return
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil && !isExitedProcess(err) {
		if killErr := cmd.Process.Kill(); killErr != nil && !isExitedProcess(killErr) {
			t.Fatalf("stop process: interrupt=%v kill=%v", err, killErr)
		}
	}

	if err := cmd.Wait(); err != nil && !isExitedProcess(err) {
		t.Fatalf("wait process: %v", err)
	}
}

func isExitedProcess(err error) bool {
	var exitErr *exec.ExitError
	return err == nil || errors.As(err, &exitErr)
}

func repoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("stat repo root go.mod: %v", err)
	}

	return root
}

func freeLoopbackAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen loopback: %v", err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Fatalf("close loopback listener: %v", err)
		}
	}()

	return listener.Addr().String()
}
