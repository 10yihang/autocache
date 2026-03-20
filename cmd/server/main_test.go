package main

import "testing"

func TestWarmTierFlagsDefaultValues(t *testing.T) {
	if *warmEngine != "badger" {
		t.Fatalf("warmEngine = %q, want %q", *warmEngine, "badger")
	}
	if *warmTierPath != "" {
		t.Fatalf("warmTierPath = %q, want empty string", *warmTierPath)
	}
}

func TestApplyConfigSetsWarmTierValues(t *testing.T) {
	*hotTierEnabled = true
	*warmTierEnabled = true
	*coldTierEnabled = false
	*coldTierEndpoint = ""
	*coldTierBucket = ""
	*warmEngine = "badger"
	*warmTierPath = ""

	applyConfig(map[string]string{
		"warm-engine":    "nokv",
		"warm-tier-path": "/tmp/warm",
	})

	if *warmEngine != "nokv" {
		t.Fatalf("warmEngine = %q, want %q", *warmEngine, "nokv")
	}
	if *warmTierPath != "/tmp/warm" {
		t.Fatalf("warmTierPath = %q, want %q", *warmTierPath, "/tmp/warm")
	}

	applyConfig(map[string]string{
		"hot-tier-enabled":   "false",
		"warm-tier-enabled":  "true",
		"cold-tier-enabled":  "true",
		"cold-tier-endpoint": "http://minio:9000",
		"cold-tier-bucket":   "autocache",
	})
	if *hotTierEnabled {
		t.Fatal("hotTierEnabled = true, want false")
	}
	if !*warmTierEnabled {
		t.Fatal("warmTierEnabled = false, want true")
	}
	if !*coldTierEnabled {
		t.Fatal("coldTierEnabled = false, want true")
	}
	if *coldTierEndpoint != "http://minio:9000" {
		t.Fatalf("coldTierEndpoint = %q, want %q", *coldTierEndpoint, "http://minio:9000")
	}
	if *coldTierBucket != "autocache" {
		t.Fatalf("coldTierBucket = %q, want %q", *coldTierBucket, "autocache")
	}
	*warmEngine = "badger"
	*warmTierPath = ""
	*badgerPath = ""
	*dataDir = "./data"
	if got := resolveWarmTierPath(); got != "data/warm" {
		t.Fatalf("resolveWarmTierPath() = %q, want %q", got, "data/warm")
	}

	*badgerPath = "/tmp/legacy"
	if got := resolveWarmTierPath(); got != "/tmp/legacy" {
		t.Fatalf("resolveWarmTierPath() with badgerPath = %q, want %q", got, "/tmp/legacy")
	}

	*warmTierPath = "/tmp/new"
	if got := resolveWarmTierPath(); got != "/tmp/new" {
		t.Fatalf("resolveWarmTierPath() with warmTierPath = %q, want %q", got, "/tmp/new")
	}
}
