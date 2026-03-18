package protocol

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/10yihang/autocache/internal/engine/memory"
	"github.com/10yihang/autocache/internal/engine/tiered"
	metrics2 "github.com/10yihang/autocache/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

func TestProtocol_RecordsCommandMetrics(t *testing.T) {
	store := memory.NewStore(memory.DefaultConfig())
	adapter := NewMemoryStoreAdapter(store)
	server := NewServer(":0", adapter)

	go func() { _ = server.Start() }()
	defer server.Stop()

	addr := waitForServer(t, server, 2*time.Second)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	beforeSet := testutil.ToFloat64(metrics2.CommandsTotal.WithLabelValues("SET", "success"))
	beforeUnknown := testutil.ToFloat64(metrics2.CommandsTotal.WithLabelValues("UNKNOWN", "error"))
	beforeGetHist := histogramSampleCount(t, "autocache_command_duration_seconds", "cmd", "GET")

	buf := make([]byte, 1024)
	writeAndRead := func(cmd string) {
		t.Helper()
		if _, err := conn.Write([]byte(cmd)); err != nil {
			t.Fatalf("failed to write command: %v", err)
		}
		if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatalf("failed to set deadline: %v", err)
		}
		if _, err := conn.Read(buf); err != nil {
			t.Fatalf("failed to read response: %v", err)
		}
	}

	writeAndRead("*3\r\n$3\r\nSET\r\n$1\r\na\r\n$1\r\n1\r\n")
	writeAndRead("*2\r\n$3\r\nGET\r\n$1\r\na\r\n")
	writeAndRead("*1\r\n$7\r\nUNKNOWN\r\n")

	afterSet := testutil.ToFloat64(metrics2.CommandsTotal.WithLabelValues("SET", "success"))
	afterUnknown := testutil.ToFloat64(metrics2.CommandsTotal.WithLabelValues("UNKNOWN", "error"))
	afterGetHist := histogramSampleCount(t, "autocache_command_duration_seconds", "cmd", "GET")

	if afterSet != beforeSet+1 {
		t.Fatalf("SET success counter = %v, want %v", afterSet, beforeSet+1)
	}
	if afterUnknown != beforeUnknown+1 {
		t.Fatalf("UNKNOWN error counter = %v, want %v", afterUnknown, beforeUnknown+1)
	}
	if afterGetHist <= beforeGetHist {
		t.Fatalf("GET histogram observations did not increase: before=%d after=%d", beforeGetHist, afterGetHist)
	}
}

func TestProtocol_RecordsConnectionMetrics(t *testing.T) {
	store := memory.NewStore(memory.DefaultConfig())
	adapter := NewMemoryStoreAdapter(store)
	server := NewServer(":0", adapter)

	go func() { _ = server.Start() }()
	defer server.Stop()

	addr := waitForServer(t, server, 2*time.Second)
	before := testutil.ToFloat64(metrics2.ConnectionsTotal)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	connected := testutil.ToFloat64(metrics2.ConnectionsTotal)
	if connected < before+1 {
		t.Fatalf("connections gauge after connect = %v, want at least %v", connected, before+1)
	}

	_ = conn.Close()
	time.Sleep(20 * time.Millisecond)
	afterClose := testutil.ToFloat64(metrics2.ConnectionsTotal)
	if afterClose > connected-1+0.001 {
		t.Fatalf("connections gauge after close = %v, want drop from %v", afterClose, connected)
	}
}

func TestTieredAdapter_WarmStringCommandsRemainCorrect(t *testing.T) {
	dir, err := os.MkdirTemp("", "tiered-adapter-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	hotTier := memory.NewStore(memory.DefaultConfig())
	defer hotTier.Close()

	cfg := tiered.DefaultConfig()
	cfg.WarmTierEnabled = true
	cfg.ColdTierEnabled = false
	cfg.WarmTierPath = dir
	cfg.HotIdleThreshold = 50 * time.Millisecond
	cfg.MigrationInterval = 50 * time.Millisecond

	manager, err := tiered.NewManager(cfg, hotTier)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	manager.Start()
	defer manager.Stop()

	ctx := context.Background()
	if err := manager.Set(ctx, "warm-key", "value", 2*time.Second); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := hotTier.Get(ctx, "warm-key"); err == nil {
		t.Fatal("expected key to be demoted out of hot tier")
	}

	adapter := NewTieredStoreAdapter(manager, hotTier)
	if ttl, err := adapter.TTL(ctx, "warm-key"); err != nil || ttl <= 0 {
		t.Fatalf("TTL = (%v,%v), want positive TTL", ttl, err)
	}
	if strlen, err := adapter.Strlen(ctx, "warm-key"); err != nil || strlen != int64(len("value")) {
		t.Fatalf("Strlen = (%d,%v), want (%d,nil)", strlen, err, len("value"))
	}
	if deleted, err := adapter.Del(ctx, "warm-key"); err != nil || deleted != 1 {
		t.Fatalf("Del = (%d,%v), want (1,nil)", deleted, err)
	}
	if exists, err := adapter.Exists(ctx, "warm-key"); err != nil || exists != 0 {
		t.Fatalf("Exists = (%d,%v), want (0,nil)", exists, err)
	}
}

func histogramSampleCount(t *testing.T, metricName, labelName, labelValue string) uint64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, metric := range mf.GetMetric() {
			if hasLabel(metric, labelName, labelValue) {
				return metric.GetHistogram().GetSampleCount()
			}
		}
	}
	return 0
}

func hasLabel(metric *dto.Metric, name, value string) bool {
	for _, label := range metric.GetLabel() {
		if label.GetName() == name && label.GetValue() == value {
			return true
		}
	}
	return false
}
