package controllers

import (
	"strings"
	"testing"

	cachev1alpha1 "github.com/10yihang/autocache/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestBuildStatefulSet_UsesTopologySpreadConstraints(t *testing.T) {
	r := &AutoCacheReconciler{Scheme: scheme.Scheme}
	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"},
		Spec: cachev1alpha1.AutoCacheSpec{
			Replicas:          3,
			Image:             "autocache:latest",
			SchedulerName:     "autocache-scheduler",
			PriorityClassName: "cache-critical",
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				{
					MaxSkew:           1,
					TopologyKey:       "topology.kubernetes.io/zone",
					WhenUnsatisfiable: corev1.ScheduleAnyway,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app.kubernetes.io/name": "autocache"},
					},
				},
			},
		},
	}

	sts := r.buildStatefulSet(ac)
	constraints := sts.Spec.Template.Spec.TopologySpreadConstraints
	if len(constraints) != 1 {
		t.Fatalf("topology spread constraints count = %d, want 1", len(constraints))
	}
	if constraints[0].TopologyKey != "topology.kubernetes.io/zone" {
		t.Fatalf("topology key = %q, want zone", constraints[0].TopologyKey)
	}
	if sts.Spec.Template.Spec.SchedulerName != "autocache-scheduler" {
		t.Fatalf("schedulerName = %q, want autocache-scheduler", sts.Spec.Template.Spec.SchedulerName)
	}
	if sts.Spec.Template.Spec.PriorityClassName != "cache-critical" {
		t.Fatalf("priorityClassName = %q, want cache-critical", sts.Spec.Template.Spec.PriorityClassName)
	}

	container := sts.Spec.Template.Spec.Containers[0]
	dataMountFound := false
	for _, mount := range container.VolumeMounts {
		if mount.Name == "data" && mount.MountPath == "/data" {
			dataMountFound = true
			break
		}
	}
	if !dataMountFound {
		t.Fatal("expected data volume mount at /data")
	}
	joined := strings.Join(container.Command, " ")
	if !strings.Contains(joined, "--data-dir /data") {
		t.Fatalf("container command %q missing --data-dir /data", joined)
	}
}
