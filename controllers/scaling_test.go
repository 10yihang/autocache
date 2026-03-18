package controllers

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	cachev1alpha1 "github.com/10yihang/autocache/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconcileStatefulSet_ReplicaChangeInvokesScaleManager(t *testing.T) {
	s := scheme.Scheme
	s.AddKnownTypes(cachev1alpha1.GroupVersion, &cachev1alpha1.AutoCache{})

	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"},
		Spec:       cachev1alpha1.AutoCacheSpec{Replicas: 4, Port: 6379, Image: "autocache:latest"},
		Status:     cachev1alpha1.AutoCacheStatus{Phase: cachev1alpha1.ClusterPhaseRunning},
	}
	sts := (&AutoCacheReconciler{Scheme: s}).buildStatefulSet(ac)
	three := int32(3)
	sts.Spec.Replicas = &three

	labels := ac.GetSelectorLabels()
	pods := []corev1.Pod{
		*makeReadyPod("test-cache-0", "default", "10.0.0.1", labels),
		*makeReadyPod("test-cache-1", "default", "10.0.0.2", labels),
		*makeReadyPod("test-cache-2", "default", "10.0.0.3", labels),
		*makeReadyPod("test-cache-3", "default", "10.0.0.4", labels),
	}

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ac, sts, &pods[0], &pods[1], &pods[2], &pods[3]).WithStatusSubresource(ac).Build()

	called := false
	r := &AutoCacheReconciler{
		Client: cl,
		Scheme: s,
		newScaleManagerFn: func(client.Client) *ScaleManager {
			m := NewScaleManager(cl)
			m.totalSlots = 8
			m.sleepFn = func(time.Duration) {}
			m.sendCommandArgsFn = func(ctx context.Context, addr string, args ...string) error { return nil }
			m.getNodeSlots = func(ctx context.Context, addr string) ([]int, error) {
				if addr == "10.0.0.1:6379" {
					return []int{0, 1, 2, 3, 4, 5, 6, 7}, nil
				}
				return nil, nil
			}
			m.getNodeID = func(ctx context.Context, addr string) (string, error) { return strings.ReplaceAll(addr, ":", "-"), nil }
			m.getKeysInSlot = func(ctx context.Context, addr string, slot int, count int) ([]string, error) {
				called = true
				return nil, nil
			}
			return m
		},
	}

	if err := r.reconcileStatefulSet(context.Background(), ac); err != nil {
		t.Fatalf("reconcileStatefulSet failed: %v", err)
	}
	if !called {
		t.Fatal("expected scale manager rebalance path to be invoked")
	}
}

func TestHandleScaleUp_AddsNodesAndRebalances(t *testing.T) {
	s := scheme.Scheme
	s.AddKnownTypes(cachev1alpha1.GroupVersion, &cachev1alpha1.AutoCache{})

	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"},
		Spec:       cachev1alpha1.AutoCacheSpec{Replicas: 4, Port: 6379},
		Status:     cachev1alpha1.AutoCacheStatus{Phase: cachev1alpha1.ClusterPhaseRunning},
	}
	labels := ac.GetSelectorLabels()
	pods := []corev1.Pod{
		*makeReadyPod("test-cache-0", "default", "10.0.0.1", labels),
		*makeReadyPod("test-cache-1", "default", "10.0.0.2", labels),
		*makeReadyPod("test-cache-2", "default", "10.0.0.3", labels),
		*makeReadyPod("test-cache-3", "default", "10.0.0.4", labels),
	}

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ac, &pods[0], &pods[1], &pods[2], &pods[3]).WithStatusSubresource(ac).Build()

	type sentCommand struct{ addr, command string }
	commands := make([]sentCommand, 0)
	slotCallCount := make(map[int]int)
	manager := NewScaleManager(cl)
	manager.totalSlots = 8
	manager.sleepFn = func(time.Duration) {}
	manager.sendCommandArgsFn = func(ctx context.Context, addr string, args ...string) error {
		commands = append(commands, sentCommand{addr: addr, command: strings.Join(args, "|")})
		return nil
	}
	manager.getNodeSlots = func(ctx context.Context, addr string) ([]int, error) {
		if addr == "10.0.0.1:6379" {
			return []int{0, 1, 2, 3, 4, 5, 6, 7}, nil
		}
		return nil, nil
	}
	manager.getNodeID = func(ctx context.Context, addr string) (string, error) {
		return strings.ReplaceAll(addr, ":", "-"), nil
	}
	manager.getKeysInSlot = func(ctx context.Context, addr string, slot int, count int) ([]string, error) {
		if addr != "10.0.0.1:6379" {
			return nil, nil
		}
		if slotCallCount[slot] > 0 {
			return nil, nil
		}
		slotCallCount[slot]++
		return []string{fmt.Sprintf("key-%d", slot)}, nil
	}

	if err := manager.HandleScaleUp(context.Background(), ac, 4, 3); err != nil {
		t.Fatalf("HandleScaleUp failed: %v", err)
	}

	meetCount := 0
	migrateCount := 0
	for _, cmd := range commands {
		if strings.HasPrefix(cmd.command, "CLUSTER|MEET") {
			meetCount++
		}
		if strings.HasPrefix(cmd.command, "MIGRATE|") {
			migrateCount++
		}
	}
	if meetCount != 1 {
		t.Fatalf("CLUSTER MEET count = %d, want 1", meetCount)
	}
	if migrateCount == 0 {
		t.Fatal("expected at least one MIGRATE command during rebalance")
	}

	updated := &cachev1alpha1.AutoCache{}
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(ac), updated); err != nil {
		t.Fatalf("failed to get updated AutoCache: %v", err)
	}
	if updated.Status.Migration == nil || updated.Status.Migration.Phase != cachev1alpha1.MigrationPhaseCompleted {
		t.Fatalf("migration status = %#v, want completed", updated.Status.Migration)
	}
}

func TestHandleScaleDown_MigratesAndForgetsNode(t *testing.T) {
	s := scheme.Scheme
	s.AddKnownTypes(cachev1alpha1.GroupVersion, &cachev1alpha1.AutoCache{})

	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"},
		Spec:       cachev1alpha1.AutoCacheSpec{Replicas: 3, Port: 6379},
		Status:     cachev1alpha1.AutoCacheStatus{Phase: cachev1alpha1.ClusterPhaseRunning},
	}
	labels := ac.GetSelectorLabels()
	pods := []corev1.Pod{
		*makeReadyPod("test-cache-0", "default", "10.0.0.1", labels),
		*makeReadyPod("test-cache-1", "default", "10.0.0.2", labels),
		*makeReadyPod("test-cache-2", "default", "10.0.0.3", labels),
		*makeReadyPod("test-cache-3", "default", "10.0.0.4", labels),
	}

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ac, &pods[0], &pods[1], &pods[2], &pods[3]).WithStatusSubresource(ac).Build()

	type sentCommand struct{ addr, command string }
	commands := make([]sentCommand, 0)
	slotCallCount := make(map[int]int)
	manager := NewScaleManager(cl)
	manager.totalSlots = 8
	manager.sendCommandArgsFn = func(ctx context.Context, addr string, args ...string) error {
		commands = append(commands, sentCommand{addr: addr, command: strings.Join(args, "|")})
		return nil
	}
	manager.getNodeSlots = func(ctx context.Context, addr string) ([]int, error) {
		switch addr {
		case "10.0.0.1:6379":
			return []int{0, 1}, nil
		case "10.0.0.2:6379":
			return []int{2, 3}, nil
		case "10.0.0.3:6379":
			return []int{4, 5}, nil
		case "10.0.0.4:6379":
			return []int{6, 7}, nil
		default:
			return nil, nil
		}
	}
	manager.getNodeID = func(ctx context.Context, addr string) (string, error) {
		return strings.ReplaceAll(addr, ":", "-"), nil
	}
	manager.getKeysInSlot = func(ctx context.Context, addr string, slot int, count int) ([]string, error) {
		if addr != "10.0.0.4:6379" {
			return nil, nil
		}
		if slotCallCount[slot] > 0 {
			return nil, nil
		}
		slotCallCount[slot]++
		return []string{fmt.Sprintf("key-%d", slot)}, nil
	}

	if err := manager.HandleScaleDown(context.Background(), ac, 3, 4); err != nil {
		t.Fatalf("HandleScaleDown failed: %v", err)
	}

	forgetCount := 0
	migrateCount := 0
	for _, cmd := range commands {
		if strings.HasPrefix(cmd.command, "CLUSTER|FORGET") {
			forgetCount++
		}
		if strings.HasPrefix(cmd.command, "MIGRATE|") {
			migrateCount++
		}
	}
	if migrateCount != 2 {
		t.Fatalf("MIGRATE count = %d, want 2", migrateCount)
	}
	if forgetCount != 3 {
		t.Fatalf("CLUSTER FORGET count = %d, want 3", forgetCount)
	}

	updated := &cachev1alpha1.AutoCache{}
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(ac), updated); err != nil {
		t.Fatalf("failed to get updated AutoCache: %v", err)
	}
	if updated.Status.Migration == nil || updated.Status.Migration.Phase != cachev1alpha1.MigrationPhaseCompleted {
		t.Fatalf("migration status = %#v, want completed", updated.Status.Migration)
	}
}

func TestHandleScaleUp_MigrateKeyWithSpaceUsesStructuredArgs(t *testing.T) {
	s := scheme.Scheme
	s.AddKnownTypes(cachev1alpha1.GroupVersion, &cachev1alpha1.AutoCache{})

	ac := &cachev1alpha1.AutoCache{ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"}, Spec: cachev1alpha1.AutoCacheSpec{Replicas: 4, Port: 6379}, Status: cachev1alpha1.AutoCacheStatus{Phase: cachev1alpha1.ClusterPhaseRunning}}
	labels := ac.GetSelectorLabels()
	pods := []corev1.Pod{
		*makeReadyPod("test-cache-0", "default", "10.0.0.1", labels),
		*makeReadyPod("test-cache-1", "default", "10.0.0.2", labels),
		*makeReadyPod("test-cache-2", "default", "10.0.0.3", labels),
		*makeReadyPod("test-cache-3", "default", "10.0.0.4", labels),
	}
	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ac, &pods[0], &pods[1], &pods[2], &pods[3]).WithStatusSubresource(ac).Build()

	manager := NewScaleManager(cl)
	manager.totalSlots = 8
	manager.sleepFn = func(time.Duration) {}
	var migrateArgs []string
	manager.sendCommandArgsFn = func(ctx context.Context, addr string, args ...string) error {
		if len(args) > 0 && args[0] == "MIGRATE" {
			migrateArgs = append([]string(nil), args...)
		}
		return nil
	}
	manager.getNodeSlots = func(ctx context.Context, addr string) ([]int, error) {
		if addr == "10.0.0.1:6379" {
			return []int{0, 1, 2, 3, 4, 5, 6, 7}, nil
		}
		return nil, nil
	}
	manager.getNodeID = func(ctx context.Context, addr string) (string, error) { return strings.ReplaceAll(addr, ":", "-"), nil }
	called := false
	manager.getKeysInSlot = func(ctx context.Context, addr string, slot int, count int) ([]string, error) {
		if called {
			return nil, nil
		}
		called = true
		return []string{"key with space"}, nil
	}

	if err := manager.HandleScaleUp(context.Background(), ac, 4, 3); err != nil {
		t.Fatalf("HandleScaleUp failed: %v", err)
	}
	if len(migrateArgs) == 0 || migrateArgs[3] != "key with space" {
		t.Fatalf("MIGRATE args = %v, want key with space preserved", migrateArgs)
	}
}

func TestHandleScaleUp_RollsBackAndMarksFailedOnMigrateError(t *testing.T) {
	s := scheme.Scheme
	s.AddKnownTypes(cachev1alpha1.GroupVersion, &cachev1alpha1.AutoCache{})
	ac := &cachev1alpha1.AutoCache{ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"}, Spec: cachev1alpha1.AutoCacheSpec{Replicas: 4, Port: 6379}, Status: cachev1alpha1.AutoCacheStatus{Phase: cachev1alpha1.ClusterPhaseRunning}}
	labels := ac.GetSelectorLabels()
	pods := []corev1.Pod{
		*makeReadyPod("test-cache-0", "default", "10.0.0.1", labels),
		*makeReadyPod("test-cache-1", "default", "10.0.0.2", labels),
		*makeReadyPod("test-cache-2", "default", "10.0.0.3", labels),
		*makeReadyPod("test-cache-3", "default", "10.0.0.4", labels),
	}
	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ac, &pods[0], &pods[1], &pods[2], &pods[3]).WithStatusSubresource(ac).Build()

	manager := NewScaleManager(cl)
	manager.totalSlots = 8
	manager.sleepFn = func(time.Duration) {}
	commands := make([]string, 0)
	manager.sendCommandArgsFn = func(ctx context.Context, addr string, args ...string) error {
		commands = append(commands, strings.Join(args, "|"))
		if len(args) > 0 && args[0] == "MIGRATE" {
			return fmt.Errorf("boom")
		}
		return nil
	}
	manager.getNodeSlots = func(ctx context.Context, addr string) ([]int, error) {
		if addr == "10.0.0.1:6379" {
			return []int{0, 1, 2, 3, 4, 5, 6, 7}, nil
		}
		return nil, nil
	}
	manager.getNodeID = func(ctx context.Context, addr string) (string, error) { return strings.ReplaceAll(addr, ":", "-"), nil }
	one := false
	manager.getKeysInSlot = func(ctx context.Context, addr string, slot int, count int) ([]string, error) {
		if one {
			return nil, nil
		}
		one = true
		return []string{"key"}, nil
	}

	if err := manager.HandleScaleUp(context.Background(), ac, 4, 3); err == nil {
		t.Fatal("expected HandleScaleUp to fail")
	}
	stableCount := 0
	for _, cmd := range commands {
		if strings.Contains(cmd, "|SETSLOT|") && strings.HasSuffix(cmd, "|STABLE") {
			stableCount++
		}
	}
	if stableCount != 2 {
		t.Fatalf("stable rollback count = %d, want 2", stableCount)
	}
	updated := &cachev1alpha1.AutoCache{}
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(ac), updated); err != nil {
		t.Fatalf("failed to get updated AutoCache: %v", err)
	}
	if updated.Status.Migration == nil || updated.Status.Migration.Phase != cachev1alpha1.MigrationPhaseFailed {
		t.Fatalf("migration status = %#v, want failed", updated.Status.Migration)
	}
}
