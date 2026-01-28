package controllers

import (
	"bufio"
	"strings"
	"testing"

	cachev1alpha1 "github.com/10yihang/autocache/api/v1alpha1"
)

func TestParseSlotPlan(t *testing.T) {
	tests := []struct {
		name      string
		plan      map[string]string
		wantLen   int
		wantErr   bool
		checkFunc func([]SlotPlanEntry) bool
	}{
		{
			name: "single range",
			plan: map[string]string{
				"0-8191": "node-0",
			},
			wantLen: 1,
			wantErr: false,
			checkFunc: func(entries []SlotPlanEntry) bool {
				return entries[0].Range.Start == 0 &&
					entries[0].Range.End == 8191 &&
					entries[0].NodeName == "node-0"
			},
		},
		{
			name: "multiple ranges",
			plan: map[string]string{
				"0-5461":      "node-0",
				"5462-10922":  "node-1",
				"10923-16383": "node-2",
			},
			wantLen: 3,
			wantErr: false,
		},
		{
			name: "invalid format - no dash",
			plan: map[string]string{
				"08191": "node-0",
			},
			wantErr: true,
		},
		{
			name: "invalid format - not a number",
			plan: map[string]string{
				"abc-def": "node-0",
			},
			wantErr: true,
		},
		{
			name: "invalid range - negative start",
			plan: map[string]string{
				"-1-100": "node-0",
			},
			wantErr: true,
		},
		{
			name: "invalid range - exceeds max",
			plan: map[string]string{
				"0-16384": "node-0",
			},
			wantErr: true,
		},
		{
			name: "invalid range - start > end",
			plan: map[string]string{
				"100-50": "node-0",
			},
			wantErr: true,
		},
		{
			name:    "empty plan",
			plan:    map[string]string{},
			wantLen: 0,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := parseSlotPlan(tt.plan)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSlotPlan() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(entries) != tt.wantLen {
					t.Errorf("parseSlotPlan() len = %d, want %d", len(entries), tt.wantLen)
				}
				if tt.checkFunc != nil && !tt.checkFunc(entries) {
					t.Errorf("parseSlotPlan() checkFunc failed")
				}
			}
		})
	}
}

func TestCalculateMigrations(t *testing.T) {
	r := &AutoCacheReconciler{}

	tests := []struct {
		name         string
		plan         []SlotPlanEntry
		current      map[int]string
		ac           *cachev1alpha1.AutoCache
		wantMigCount int
	}{
		{
			name: "no migrations needed - empty current",
			plan: []SlotPlanEntry{
				{Range: SlotRange{Start: 0, End: 2}, NodeName: "node-0"},
			},
			current:      map[int]string{},
			ac:           makeTestAutoCache(),
			wantMigCount: 0,
		},
		{
			name: "no migrations needed - already correct",
			plan: []SlotPlanEntry{
				{Range: SlotRange{Start: 0, End: 2}, NodeName: "node-0"},
			},
			current: map[int]string{
				0: "node-0",
				1: "node-0",
				2: "node-0",
			},
			ac:           makeTestAutoCache(),
			wantMigCount: 0,
		},
		{
			name: "migrations needed - different owner",
			plan: []SlotPlanEntry{
				{Range: SlotRange{Start: 0, End: 2}, NodeName: "node-1"},
			},
			current: map[int]string{
				0: "node-0",
				1: "node-0",
				2: "node-0",
			},
			ac:           makeTestAutoCache(),
			wantMigCount: 3,
		},
		{
			name: "partial migrations needed",
			plan: []SlotPlanEntry{
				{Range: SlotRange{Start: 0, End: 2}, NodeName: "node-1"},
			},
			current: map[int]string{
				0: "node-0",
				1: "node-1",
				2: "node-0",
			},
			ac:           makeTestAutoCache(),
			wantMigCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tasks := r.calculateMigrations(tt.plan, tt.current, tt.ac)
			if len(tasks) != tt.wantMigCount {
				t.Errorf("calculateMigrations() = %d tasks, want %d", len(tasks), tt.wantMigCount)
			}
		})
	}
}

func TestMigrationTask(t *testing.T) {
	task := MigrationTask{
		Slot:       100,
		SourceNode: "node-0",
		SourceAddr: "10.0.0.1:6379",
		TargetNode: "node-1",
		TargetAddr: "10.0.0.2:6379",
	}

	if task.Slot != 100 {
		t.Errorf("MigrationTask.Slot = %d, want 100", task.Slot)
	}
	if task.SourceNode != "node-0" {
		t.Errorf("MigrationTask.SourceNode = %s, want node-0", task.SourceNode)
	}
	if task.TargetNode != "node-1" {
		t.Errorf("MigrationTask.TargetNode = %s, want node-1", task.TargetNode)
	}
	if task.SourceAddr != "10.0.0.1:6379" {
		t.Errorf("MigrationTask.SourceAddr = %s, want 10.0.0.1:6379", task.SourceAddr)
	}
	if task.TargetAddr != "10.0.0.2:6379" {
		t.Errorf("MigrationTask.TargetAddr = %s, want 10.0.0.2:6379", task.TargetAddr)
	}
}

func TestSlotRange(t *testing.T) {
	sr := SlotRange{Start: 0, End: 8191}
	if sr.Start != 0 {
		t.Errorf("SlotRange.Start = %d, want 0", sr.Start)
	}
	if sr.End != 8191 {
		t.Errorf("SlotRange.End = %d, want 8191", sr.End)
	}
}

func TestGetNodeAddr(t *testing.T) {
	r := &AutoCacheReconciler{}
	ac := makeTestAutoCache()

	tests := []struct {
		name     string
		nodeName string
		wantAddr string
	}{
		{
			name:     "existing node",
			nodeName: "node-0",
			wantAddr: "10.0.0.1:6379",
		},
		{
			name:     "existing node 2",
			nodeName: "node-1",
			wantAddr: "10.0.0.2:6379",
		},
		{
			name:     "non-existing node",
			nodeName: "node-999",
			wantAddr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := r.getNodeAddr(ac, tt.nodeName)
			if addr != tt.wantAddr {
				t.Errorf("getNodeAddr() = %s, want %s", addr, tt.wantAddr)
			}
		})
	}
}

func TestGetNodeSlots_ParsesValidResponse(t *testing.T) {
	resp := "*2\r\n" +
		"*3\r\n" +
		":0\r\n" +
		":5460\r\n" +
		"*3\r\n" +
		"$9\r\n127.0.0.1\r\n" +
		":6379\r\n" +
		"$6\r\nnode-0\r\n" +
		"*3\r\n" +
		":5461\r\n" +
		":10922\r\n" +
		"*3\r\n" +
		"$9\r\n127.0.0.2\r\n" +
		":6380\r\n" +
		"$6\r\nnode-1\r\n"

	slots, err := parseNodeSlots(bufio.NewReader(strings.NewReader(resp)))
	if err != nil {
		t.Fatalf("parseNodeSlots() error = %v", err)
	}

	if len(slots) != 10923 {
		t.Fatalf("parseNodeSlots() len = %d, want %d", len(slots), 10923)
	}

	seen := make(map[int]struct{}, len(slots))
	for _, slot := range slots {
		seen[slot] = struct{}{}
	}
	if len(seen) != len(slots) {
		t.Fatalf("parseNodeSlots() has duplicate slots")
	}
	for slot := 0; slot <= 10922; slot++ {
		if _, ok := seen[slot]; !ok {
			t.Fatalf("parseNodeSlots() missing slot %d", slot)
		}
	}
}

func TestGetNodeSlots_ErrorResponse(t *testing.T) {
	resp := "-ERR cluster not ready\r\n"

	_, err := parseNodeSlots(bufio.NewReader(strings.NewReader(resp)))
	if err == nil {
		t.Fatalf("parseNodeSlots() error = nil, want error")
	}
}

func TestGetNodeSlots_MalformedResponse(t *testing.T) {
	resp := "*1\r\n:foo\r\n"

	_, err := parseNodeSlots(bufio.NewReader(strings.NewReader(resp)))
	if err == nil {
		t.Fatalf("parseNodeSlots() error = nil, want error")
	}
}

func makeTestAutoCache() *cachev1alpha1.AutoCache {
	return &cachev1alpha1.AutoCache{
		Spec: cachev1alpha1.AutoCacheSpec{
			Port: 6379,
		},
		Status: cachev1alpha1.AutoCacheStatus{
			Nodes: []cachev1alpha1.NodeStatus{
				{Name: "node-0", PodIP: "10.0.0.1"},
				{Name: "node-1", PodIP: "10.0.0.2"},
				{Name: "node-2", PodIP: "10.0.0.3"},
			},
		},
	}
}
