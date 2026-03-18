package controllers

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"

	cachev1alpha1 "github.com/10yihang/autocache/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestUpdateStatus_RequiresRealClusterHealth(t *testing.T) {
	s := scheme.Scheme
	s.AddKnownTypes(cachev1alpha1.GroupVersion, &cachev1alpha1.AutoCache{})

	replicas := int32(3)
	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"},
		Spec:       cachev1alpha1.AutoCacheSpec{Replicas: replicas, Port: 6379},
	}
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"},
		Status: appsv1.StatefulSetStatus{
			Replicas:      replicas,
			ReadyReplicas: replicas,
		},
	}
	labels := ac.GetSelectorLabels()
	pods := []*corev1.Pod{
		makeReadyPod("test-cache-0", "default", "10.0.0.1", labels),
		makeReadyPod("test-cache-1", "default", "10.0.0.2", labels),
		makeReadyPod("test-cache-2", "default", "10.0.0.3", labels),
	}

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ac, sts, pods[0], pods[1], pods[2]).WithStatusSubresource(ac).Build()
	r := &AutoCacheReconciler{
		Client: cl,
		Scheme: s,
		getClusterHealthFn: func(context.Context, string) (*clusterHealth, error) {
			return &clusterHealth{Status: "fail", KnownNodes: 1, SlotsAssigned: 0}, nil
		},
	}

	if err := r.updateStatus(context.Background(), ac); err != nil {
		t.Fatalf("updateStatus failed: %v", err)
	}

	updated := &cachev1alpha1.AutoCache{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "test-cache", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get updated autocache: %v", err)
	}
	if updated.Status.Phase == cachev1alpha1.ClusterPhaseRunning {
		t.Fatalf("phase = %s, want not running when cluster health is degraded", updated.Status.Phase)
	}
	if updated.Status.ClusterStatus == "ok" {
		t.Fatalf("clusterStatus = %q, want non-ok when cluster health is degraded", updated.Status.ClusterStatus)
	}
}

func TestUpdateStatus_RejectsExtraStaleNodes(t *testing.T) {
	s := scheme.Scheme
	s.AddKnownTypes(cachev1alpha1.GroupVersion, &cachev1alpha1.AutoCache{})

	replicas := int32(3)
	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"},
		Spec:       cachev1alpha1.AutoCacheSpec{Replicas: replicas, Port: 6379},
	}
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"},
		Status: appsv1.StatefulSetStatus{
			Replicas:      replicas,
			ReadyReplicas: replicas,
		},
	}
	labels := ac.GetSelectorLabels()
	pods := []*corev1.Pod{
		makeReadyPod("test-cache-0", "default", "10.0.0.1", labels),
		makeReadyPod("test-cache-1", "default", "10.0.0.2", labels),
		makeReadyPod("test-cache-2", "default", "10.0.0.3", labels),
	}

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ac, sts, pods[0], pods[1], pods[2]).WithStatusSubresource(ac).Build()
	r := &AutoCacheReconciler{
		Client: cl,
		Scheme: s,
		getClusterHealthFn: func(context.Context, string) (*clusterHealth, error) {
			return &clusterHealth{Status: "ok", KnownNodes: 4, SlotsAssigned: 16384}, nil
		},
	}

	if err := r.updateStatus(context.Background(), ac); err != nil {
		t.Fatalf("updateStatus failed: %v", err)
	}
	updated := &cachev1alpha1.AutoCache{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "test-cache", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get updated autocache: %v", err)
	}
	if updated.Status.Phase == cachev1alpha1.ClusterPhaseRunning {
		t.Fatalf("phase = %s, want not running when cluster has stale extra nodes", updated.Status.Phase)
	}
}

func TestInitializeCluster_UsesObservedHealthBeforeSettingSlotsAssigned(t *testing.T) {
	s := scheme.Scheme
	s.AddKnownTypes(cachev1alpha1.GroupVersion, &cachev1alpha1.AutoCache{})

	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"},
		Spec:       cachev1alpha1.AutoCacheSpec{Replicas: 3, Port: 6379},
	}
	labels := ac.GetSelectorLabels()
	pods := []*corev1.Pod{
		makeReadyPod("test-cache-0", "default", "10.0.0.1", labels),
		makeReadyPod("test-cache-1", "default", "10.0.0.2", labels),
		makeReadyPod("test-cache-2", "default", "10.0.0.3", labels),
	}

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ac, pods[0], pods[1], pods[2]).WithStatusSubresource(ac).Build()
	r := &AutoCacheReconciler{
		Client:        cl,
		Scheme:        s,
		sendCommandFn: func(context.Context, string, string) error { return nil },
		getClusterHealthFn: func(context.Context, string) (*clusterHealth, error) {
			return &clusterHealth{Status: "fail", KnownNodes: 1, SlotsAssigned: 0}, nil
		},
	}

	if err := r.initializeCluster(context.Background(), ac); err != nil {
		t.Fatalf("initializeCluster failed: %v", err)
	}
	updated := &cachev1alpha1.AutoCache{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "test-cache", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get updated autocache: %v", err)
	}
	if updated.Status.SlotsAssigned != 0 {
		t.Fatalf("slotsAssigned = %d, want 0 until real cluster health confirms slots", updated.Status.SlotsAssigned)
	}
}

func TestInitializeCluster_ForgetsStaleFailedNodes(t *testing.T) {
	s := scheme.Scheme
	s.AddKnownTypes(cachev1alpha1.GroupVersion, &cachev1alpha1.AutoCache{})

	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"},
		Spec:       cachev1alpha1.AutoCacheSpec{Replicas: 3, Port: 6379},
	}
	labels := ac.GetSelectorLabels()
	pods := []*corev1.Pod{
		makeReadyPod("test-cache-0", "default", "10.0.0.1", labels),
		makeReadyPod("test-cache-1", "default", "10.0.0.2", labels),
		makeReadyPod("test-cache-2", "default", "10.0.0.3", labels),
	}

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ac, pods[0], pods[1], pods[2]).WithStatusSubresource(ac).Build()
	var commands []string
	r := &AutoCacheReconciler{
		Client: cl,
		Scheme: s,
		sendCommandFn: func(_ context.Context, addr string, command string) error {
			commands = append(commands, addr+"|"+command)
			return nil
		},
		getClusterNodesFn: func(context.Context, string) ([]clusterNode, error) {
			return []clusterNode{
				{ID: "stale-node", Addr: "10.0.0.99:6379@16379", Flags: "master,fail?"},
			}, nil
		},
		getClusterHealthFn: func(context.Context, string) (*clusterHealth, error) {
			return &clusterHealth{Status: "ok", KnownNodes: 3, SlotsAssigned: 16384}, nil
		},
	}

	if err := r.initializeCluster(context.Background(), ac); err != nil {
		t.Fatalf("initializeCluster failed: %v", err)
	}
	forgetCount := 0
	for _, cmd := range commands {
		if strings.Contains(cmd, "CLUSTER FORGET stale-node") {
			forgetCount++
		}
	}
	if forgetCount != 3 {
		t.Fatalf("forgetCount = %d, want 3", forgetCount)
	}
}

func TestReconcile_InitializesClusterWhenPodsReadyButHealthDegraded(t *testing.T) {
	s := scheme.Scheme
	s.AddKnownTypes(cachev1alpha1.GroupVersion, &cachev1alpha1.AutoCache{})

	replicas := int32(3)
	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default", Finalizers: []string{finalizerName}},
		Spec:       cachev1alpha1.AutoCacheSpec{Replicas: replicas, Port: 6379, Image: "autocache:latest"},
		Status:     cachev1alpha1.AutoCacheStatus{Phase: cachev1alpha1.ClusterPhasePending, ObservedGeneration: 1},
	}
	sts := (&AutoCacheReconciler{Scheme: s}).buildStatefulSet(ac)
	sts.Status.Replicas = replicas
	sts.Status.ReadyReplicas = replicas
	labels := ac.GetSelectorLabels()
	pods := []*corev1.Pod{
		makeReadyPod("test-cache-0", "default", "10.0.0.1", labels),
		makeReadyPod("test-cache-1", "default", "10.0.0.2", labels),
		makeReadyPod("test-cache-2", "default", "10.0.0.3", labels),
	}

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(ac, sts, pods[0], pods[1], pods[2]).WithStatusSubresource(ac).Build()
	called := 0
	r := &AutoCacheReconciler{
		Client: cl,
		Scheme: s,
		sendCommandFn: func(context.Context, string, string) error {
			called++
			return nil
		},
		getClusterHealthFn: func(context.Context, string) (*clusterHealth, error) {
			return &clusterHealth{Status: "fail", KnownNodes: 1, SlotsAssigned: 0}, nil
		},
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: "test-cache", Namespace: "default"}})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if called == 0 {
		t.Fatal("expected reconcile to trigger cluster initialization commands when pods are ready but health is degraded")
	}
}

func TestAutoCacheReconciler_Reconcile(t *testing.T) {
	// Register types
	s := scheme.Scheme
	s.AddKnownTypes(cachev1alpha1.GroupVersion, &cachev1alpha1.AutoCache{})

	// Create a AutoCache object
	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cache",
			Namespace: "default",
		},
		Spec: cachev1alpha1.AutoCacheSpec{
			Image:    "autocache:latest",
			Replicas: 3,
		},
	}

	// Create fake client
	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(ac).
		WithStatusSubresource(ac).
		Build()

	// Create Reconciler
	r := &AutoCacheReconciler{
		Client: cl,
		Scheme: s,
	}

	// Reconcile request
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-cache",
			Namespace: "default",
		},
	}

	// Reconcile 1: Add Finalizer
	res, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("Reconcile 1 failed: %v", err)
	}
	if !res.Requeue {
		t.Log("Reconcile 1 did not requeue, continuing check...")
	}

	// Reconcile 2: Initialize Status (adds Requeue: true)
	res, err = r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("Reconcile 2 failed: %v", err)
	}

	// Reconcile 3: Create Resources
	res, err = r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("Reconcile 3 failed: %v", err)
	}

	// Verify StatefulSet created
	sts := &appsv1.StatefulSet{}
	err = cl.Get(context.Background(), types.NamespacedName{Name: "test-cache", Namespace: "default"}, sts)
	if err != nil {
		t.Fatalf("Failed to get StatefulSet: %v", err)
	}

	if *sts.Spec.Replicas != 3 {
		t.Errorf("Expected 3 replicas, got %d", *sts.Spec.Replicas)
	}

	// Verify Service created
	svc := &corev1.Service{}
	err = cl.Get(context.Background(), types.NamespacedName{Name: "test-cache", Namespace: "default"}, svc)
	if err != nil {
		t.Fatalf("Failed to get Service: %v", err)
	}

	metricsSvc := &corev1.Service{}
	err = cl.Get(context.Background(), types.NamespacedName{Name: "test-cache-metrics", Namespace: "default"}, metricsSvc)
	if err != nil {
		t.Fatalf("Failed to get Metrics Service: %v", err)
	}
}

func TestInitializeCluster_Bootstrap(t *testing.T) {
	s := scheme.Scheme
	s.AddKnownTypes(cachev1alpha1.GroupVersion, &cachev1alpha1.AutoCache{})

	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cache",
			Namespace: "default",
		},
		Spec: cachev1alpha1.AutoCacheSpec{
			Replicas: 3,
			Port:     6379,
		},
	}

	labels := ac.GetSelectorLabels()
	pods := []*corev1.Pod{
		makeReadyPod("test-cache-0", "default", "10.0.0.1", labels),
		makeReadyPod("test-cache-1", "default", "10.0.0.2", labels),
		makeReadyPod("test-cache-2", "default", "10.0.0.3", labels),
	}

	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(ac, pods[0], pods[1], pods[2]).
		WithStatusSubresource(ac).
		Build()

	type sentCommand struct {
		addr    string
		command string
	}
	var commands []sentCommand

	r := &AutoCacheReconciler{
		Client: cl,
		Scheme: s,
		sendCommandFn: func(ctx context.Context, addr string, command string) error {
			commands = append(commands, sentCommand{addr: addr, command: command})
			return nil
		},
	}

	if err := r.initializeCluster(context.Background(), ac); err != nil {
		t.Fatalf("initializeCluster failed: %v", err)
	}

	readyPods := []corev1.Pod{*pods[0], *pods[1], *pods[2]}
	sort.Slice(readyPods, func(i, j int) bool {
		return readyPods[i].Name < readyPods[j].Name
	})

	port := int(ac.Spec.Port)
	if port == 0 {
		port = 6379
	}

	seedIP := readyPods[0].Status.PodIP
	expectedMeetTargets := make(map[string]struct{})
	for _, pod := range readyPods[1:] {
		addr := fmt.Sprintf("%s:%d", pod.Status.PodIP, port)
		expectedMeetTargets[addr] = struct{}{}
	}

	meetTargets := make(map[string]struct{})
	addSlotsTargets := make(map[string]struct{})
	slotSet := make(map[int]struct{})
	totalSlotArgs := 0

	for _, cmd := range commands {
		fields := strings.Fields(cmd.command)
		if len(fields) < 2 {
			continue
		}
		if strings.ToUpper(fields[0]) != "CLUSTER" {
			continue
		}
		switch strings.ToUpper(fields[1]) {
		case "MEET":
			if len(fields) < 4 {
				t.Fatalf("CLUSTER MEET missing args: %q", cmd.command)
			}
			meetTargets[cmd.addr] = struct{}{}
			if fields[2] != seedIP {
				t.Errorf("CLUSTER MEET seed ip = %s, want %s", fields[2], seedIP)
			}
			if fields[3] != strconv.Itoa(port) {
				t.Errorf("CLUSTER MEET seed port = %s, want %d", fields[3], port)
			}
		case "ADDSLOTS":
			addSlotsTargets[cmd.addr] = struct{}{}
			for _, slotStr := range fields[2:] {
				slot, err := strconv.Atoi(slotStr)
				if err != nil {
					t.Fatalf("invalid slot %q: %v", slotStr, err)
				}
				if slot < 0 || slot >= 16384 {
					t.Fatalf("slot out of range: %d", slot)
				}
				slotSet[slot] = struct{}{}
				totalSlotArgs++
			}
		}
	}

	if len(meetTargets) != len(expectedMeetTargets) {
		t.Fatalf("CLUSTER MEET targets = %d, want %d", len(meetTargets), len(expectedMeetTargets))
	}
	for addr := range expectedMeetTargets {
		if _, ok := meetTargets[addr]; !ok {
			t.Errorf("missing CLUSTER MEET for %s", addr)
		}
	}

	for _, pod := range readyPods {
		addr := fmt.Sprintf("%s:%d", pod.Status.PodIP, port)
		if _, ok := addSlotsTargets[addr]; !ok {
			t.Errorf("missing CLUSTER ADDSLOTS for %s", addr)
		}
	}

	if totalSlotArgs != 16384 {
		t.Fatalf("total slots assigned = %d, want %d", totalSlotArgs, 16384)
	}
	if len(slotSet) != 16384 {
		t.Fatalf("unique slots assigned = %d, want %d", len(slotSet), 16384)
	}
	for slot := range 16384 {
		if _, ok := slotSet[slot]; !ok {
			t.Fatalf("missing slot %d", slot)
		}
	}
}

func makeReadyPod(name, namespace, ip string, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Status: corev1.PodStatus{
			PodIP: ip,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
}
