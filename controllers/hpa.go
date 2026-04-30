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
	"context"
	"fmt"
	"reflect"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	cachev1alpha1 "github.com/10yihang/autocache/api/v1alpha1"
)

func (r *AutoCacheReconciler) reconcileHPA(ctx context.Context, ac *cachev1alpha1.AutoCache) error {
	logger := log.FromContext(ctx)

	if ac.Spec.HPA == nil || !ac.Spec.HPA.Enabled {
		ac.Status.HPAActive = false
		ac.Status.HPAReason = "HPA disabled"
		existingHPA := &autoscalingv2.HorizontalPodAutoscaler{}
		err := r.Get(ctx, types.NamespacedName{Name: ac.Name, Namespace: ac.Namespace}, existingHPA)
		if err == nil {
			logger.Info("Deleting HPA (autoscaling disabled)")
			return r.Delete(ctx, existingHPA)
		}
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	hpa := r.buildHPA(ac)
	if err := controllerutil.SetControllerReference(ac, hpa, r.Scheme); err != nil {
		ac.Status.HPAActive = false
		ac.Status.HPAReason = fmt.Sprintf("Failed to set owner reference: %v", err)
		return err
	}

	existingHPA := &autoscalingv2.HorizontalPodAutoscaler{}
	err := r.Get(ctx, types.NamespacedName{Name: hpa.Name, Namespace: hpa.Namespace}, existingHPA)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating HPA", "name", hpa.Name, "min", *hpa.Spec.MinReplicas, "max", hpa.Spec.MaxReplicas)
			if createErr := r.Create(ctx, hpa); createErr != nil {
				ac.Status.HPAActive = false
				ac.Status.HPAReason = fmt.Sprintf("Failed to create HPA: %v", createErr)
				return createErr
			}
			ac.Status.HPAActive = true
			ac.Status.HPAReason = fmt.Sprintf("HPA active, min=%d, max=%d", *hpa.Spec.MinReplicas, hpa.Spec.MaxReplicas)
			return nil
		}
		ac.Status.HPAActive = false
		ac.Status.HPAReason = fmt.Sprintf("Failed to get HPA: %v", err)
		return err
	}

	if !reflect.DeepEqual(existingHPA.Spec, hpa.Spec) {
		existingHPA.Spec = hpa.Spec
		logger.Info("Updating HPA", "name", hpa.Name)
		if updateErr := r.Update(ctx, existingHPA); updateErr != nil {
			ac.Status.HPAActive = false
			ac.Status.HPAReason = fmt.Sprintf("Failed to update HPA: %v", updateErr)
			return updateErr
		}
	}

	ac.Status.HPAActive = true
	ac.Status.HPAReason = fmt.Sprintf("HPA active, min=%d, max=%d", *hpa.Spec.MinReplicas, hpa.Spec.MaxReplicas)
	return nil
}

func (r *AutoCacheReconciler) buildHPA(ac *cachev1alpha1.AutoCache) *autoscalingv2.HorizontalPodAutoscaler {
	hpaSpec := ac.Spec.HPA

	minReplicas := hpaSpec.MinReplicas
	if minReplicas < 1 {
		minReplicas = 1
	}

	var metrics []autoscalingv2.MetricSpec

	if hpaSpec.TargetCPUUtilizationPercentage != nil {
		metrics = append(metrics, autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceCPU,
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: hpaSpec.TargetCPUUtilizationPercentage,
				},
			},
		})
	}

	if hpaSpec.TargetMemoryUtilizationPercentage != nil {
		metrics = append(metrics, autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceMemory,
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: hpaSpec.TargetMemoryUtilizationPercentage,
				},
			},
		})
	}

	if len(metrics) == 0 {
		defaultCPU := int32(80)
		metrics = []autoscalingv2.MetricSpec{
			{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: corev1.ResourceCPU,
					Target: autoscalingv2.MetricTarget{
						Type:               autoscalingv2.UtilizationMetricType,
						AverageUtilization: &defaultCPU,
					},
				},
			},
		}
	}

	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ac.Name,
			Namespace: ac.Namespace,
			Labels:    ac.GetLabels(),
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "StatefulSet",
				Name:       ac.Name,
			},
			MinReplicas: &minReplicas,
			MaxReplicas: hpaSpec.MaxReplicas,
			Metrics:     metrics,
			Behavior:    hpaSpec.Behavior,
		},
	}
}
