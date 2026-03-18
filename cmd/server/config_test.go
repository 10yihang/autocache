package main

import (
	"os"
	"testing"
)

func TestLoadConfigFile(t *testing.T) {
	file, err := os.CreateTemp("", "autocache-config-*.conf")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(file.Name())

	content := "port 6380\ncluster-port 16380\ncluster-enabled yes\nmaxmemory 1gb\nmaxmemory-policy allkeys-lru\n"
	if _, err := file.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfigFile(file.Name())
	if err != nil {
		t.Fatalf("loadConfigFile failed: %v", err)
	}
	if cfg["port"] != "6380" {
		t.Fatalf("port = %q, want 6380", cfg["port"])
	}
	if cfg["cluster-port"] != "16380" {
		t.Fatalf("cluster-port = %q, want 16380", cfg["cluster-port"])
	}
	if cfg["cluster-enabled"] != "yes" {
		t.Fatalf("cluster-enabled = %q, want yes", cfg["cluster-enabled"])
	}
	if cfg["maxmemory-policy"] != "allkeys-lru" {
		t.Fatalf("maxmemory-policy = %q, want allkeys-lru", cfg["maxmemory-policy"])
	}
}

func TestBuildMemoryConfigFromConfig(t *testing.T) {
	cfg := buildMemoryConfigFromConfig(map[string]string{
		"maxmemory":        "1gb",
		"maxmemory-policy": "allkeys-lru",
	})
	if cfg.MaxMemory != 1024*1024*1024 {
		t.Fatalf("MaxMemory = %d, want %d", cfg.MaxMemory, 1024*1024*1024)
	}
	if cfg.EvictPolicy != "allkeys-lru" {
		t.Fatalf("EvictPolicy = %q, want allkeys-lru", cfg.EvictPolicy)
	}
}
