/*
Copyright 2024 The AutoCache Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cachev1alpha1 "github.com/10yihang/autocache/api/v1alpha1"
)

func TestBuildHPA_BasicDefaults(t *testing.T) {
	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: cachev1alpha1.AutoCacheSpec{
			HPA: &cachev1alpha1.AutoscalingSpec{
				Enabled:     true,
				MinReplicas: 2,
				MaxReplicas: 10,
			},
		},
	}
	reconciler := &AutoCacheReconciler{}
	hpa := reconciler.buildHPA(ac)
	if hpa.Name != "test" {
		t.Errorf("expected name 'test', got %q", hpa.Name)
	}
	if *hpa.Spec.MinReplicas != 2 {
		t.Errorf("expected minReplicas 2, got %d", *hpa.Spec.MinReplicas)
	}
	if hpa.Spec.MaxReplicas != 10 {
		t.Errorf("expected maxReplicas 10, got %d", hpa.Spec.MaxReplicas)
	}
	if len(hpa.Spec.Metrics) != 1 {
		t.Fatalf("expected 1 default metric, got %d", len(hpa.Spec.Metrics))
	}
	if hpa.Spec.Metrics[0].Resource.Name != corev1.ResourceCPU {
		t.Errorf("expected CPU metric, got %s", hpa.Spec.Metrics[0].Resource.Name)
	}
}

func TestBuildHPA_WithCPUMetric(t *testing.T) {
	cpuTarget := int32(70)
	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: cachev1alpha1.AutoCacheSpec{
			HPA: &cachev1alpha1.AutoscalingSpec{
				Enabled:                        true,
				MinReplicas:                    1,
				MaxReplicas:                    5,
				TargetCPUUtilizationPercentage: &cpuTarget,
			},
		},
	}
	reconciler := &AutoCacheReconciler{}
	hpa := reconciler.buildHPA(ac)
	if len(hpa.Spec.Metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(hpa.Spec.Metrics))
	}
	if *hpa.Spec.Metrics[0].Resource.Target.AverageUtilization != 70 {
		t.Errorf("expected CPU target 70, got %d", *hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
	}
}

func TestBuildHPA_WithBothMetrics(t *testing.T) {
	cpuTarget := int32(60)
	memTarget := int32(80)
	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: cachev1alpha1.AutoCacheSpec{
			HPA: &cachev1alpha1.AutoscalingSpec{
				Enabled:                           true,
				MinReplicas:                       1,
				MaxReplicas:                       5,
				TargetCPUUtilizationPercentage:    &cpuTarget,
				TargetMemoryUtilizationPercentage: &memTarget,
			},
		},
	}
	reconciler := &AutoCacheReconciler{}
	hpa := reconciler.buildHPA(ac)
	if len(hpa.Spec.Metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(hpa.Spec.Metrics))
	}
}

func TestBuildHPA_MinReplicasDefaultsTo1(t *testing.T) {
	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: cachev1alpha1.AutoCacheSpec{
			HPA: &cachev1alpha1.AutoscalingSpec{
				Enabled:     true,
				MinReplicas: 0,
				MaxReplicas: 5,
			},
		},
	}
	reconciler := &AutoCacheReconciler{}
	hpa := reconciler.buildHPA(ac)
	if *hpa.Spec.MinReplicas != 1 {
		t.Errorf("expected minReplicas default 1, got %d", *hpa.Spec.MinReplicas)
	}
}

func TestBuildHPA_WithBehavior(t *testing.T) {
	stabilizationSeconds := int32(300)
	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: cachev1alpha1.AutoCacheSpec{
			HPA: &cachev1alpha1.AutoscalingSpec{
				Enabled:     true,
				MinReplicas: 1,
				MaxReplicas: 5,
				Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleDown: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: &stabilizationSeconds,
					},
				},
			},
		},
	}
	reconciler := &AutoCacheReconciler{}
	hpa := reconciler.buildHPA(ac)
	if hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds == nil {
		t.Fatal("expected behavior to be set")
	}
	if *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds != 300 {
		t.Errorf("expected stabilization 300, got %d", *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds)
	}
}
