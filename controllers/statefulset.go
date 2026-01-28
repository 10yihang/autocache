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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	cachev1alpha1 "github.com/10yihang/autocache/api/v1alpha1"
)

// reconcileStatefulSet reconciles the StatefulSet for the AutoCache cluster
func (r *AutoCacheReconciler) reconcileStatefulSet(ctx context.Context, ac *cachev1alpha1.AutoCache) error {
	sts := r.buildStatefulSet(ac)

	if err := controllerutil.SetControllerReference(ac, sts, r.Scheme); err != nil {
		return err
	}

	existingSts := &appsv1.StatefulSet{}
	err := r.Get(ctx, types.NamespacedName{Name: sts.Name, Namespace: sts.Namespace}, existingSts)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create StatefulSet
			if err := r.Create(ctx, sts); err != nil {
				return err
			}
			// Update status to Creating
			ac.Status.Phase = cachev1alpha1.ClusterPhaseCreating
			return r.Status().Update(ctx, ac)
		}
		return err
	}

	// Check if update is needed
	needUpdate := false

	// Replica count change
	if *existingSts.Spec.Replicas != ac.Spec.Replicas {
		existingSts.Spec.Replicas = &ac.Spec.Replicas
		needUpdate = true
		ac.Status.Phase = cachev1alpha1.ClusterPhaseScaling
	}

	// Image change
	if len(existingSts.Spec.Template.Spec.Containers) > 0 {
		if existingSts.Spec.Template.Spec.Containers[0].Image != ac.Spec.Image {
			existingSts.Spec.Template.Spec.Containers[0].Image = ac.Spec.Image
			needUpdate = true
			ac.Status.Phase = cachev1alpha1.ClusterPhaseUpdating
		}
	}

	if needUpdate {
		if err := r.Update(ctx, existingSts); err != nil {
			return err
		}
		return r.Status().Update(ctx, ac)
	}

	return nil
}

// buildStatefulSet builds the StatefulSet for the AutoCache cluster
func (r *AutoCacheReconciler) buildStatefulSet(ac *cachev1alpha1.AutoCache) *appsv1.StatefulSet {
	labels := ac.GetLabels()
	selectorLabels := ac.GetSelectorLabels()

	// Merge pod annotations
	podAnnotations := map[string]string{
		"prometheus.io/scrape": "true",
		"prometheus.io/port":   "9121",
	}
	for k, v := range ac.Spec.PodAnnotations {
		podAnnotations[k] = v
	}

	// Build container
	container := r.buildContainer(ac)

	// Build init container
	initContainer := r.buildInitContainer(ac)

	// Build volumes
	volumes := r.buildVolumes(ac)

	// Build VolumeClaimTemplates
	var volumeClaimTemplates []corev1.PersistentVolumeClaim
	if ac.Spec.Storage != nil && ac.Spec.Storage.Enabled {
		volumeClaimTemplates = r.buildVolumeClaimTemplates(ac)
	}

	replicas := ac.Spec.Replicas
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ac.Name,
			Namespace: ac.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: ac.Name + "-headless",
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
				},
			},
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{initContainer},
					Containers:     []corev1.Container{container},
					Volumes:        volumes,
					Affinity:       r.buildAffinity(ac),
					Tolerations:    ac.Spec.Tolerations,
					NodeSelector:   ac.Spec.NodeSelector,
				},
			},
			VolumeClaimTemplates: volumeClaimTemplates,
		},
	}

	return sts
}

// buildContainer builds the main container for the AutoCache pod
func (r *AutoCacheReconciler) buildContainer(ac *cachev1alpha1.AutoCache) corev1.Container {
	// Environment variables
	env := []corev1.EnvVar{
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		{
			Name: "POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
		},
		{
			Name:  "CLUSTER_ANNOUNCE_PORT",
			Value: fmt.Sprintf("%d", ac.Spec.Port),
		},
		{
			Name:  "CLUSTER_ANNOUNCE_BUS_PORT",
			Value: fmt.Sprintf("%d", ac.Spec.ClusterPort),
		},
	}

	// Ports
	ports := []corev1.ContainerPort{
		{
			Name:          "redis",
			ContainerPort: ac.Spec.Port,
			Protocol:      corev1.ProtocolTCP,
		},
		{
			Name:          "cluster",
			ContainerPort: ac.Spec.ClusterPort,
			Protocol:      corev1.ProtocolTCP,
		},
		{
			Name:          "metrics",
			ContainerPort: 9121,
			Protocol:      corev1.ProtocolTCP,
		},
	}

	// Volume mounts
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "config",
			MountPath: "/etc/autocache",
		},
	}

	if ac.Spec.Storage != nil && ac.Spec.Storage.Enabled {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "data",
			MountPath: "/data",
		})
	}

	// Health checks
	livenessProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt(int(ac.Spec.Port)),
			},
		},
		InitialDelaySeconds: 30,
		PeriodSeconds:       10,
		TimeoutSeconds:      5,
		FailureThreshold:    3,
	}

	readinessProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt(int(ac.Spec.Port)),
			},
		},
		InitialDelaySeconds: 5,
		PeriodSeconds:       5,
		TimeoutSeconds:      3,
		FailureThreshold:    3,
	}

	return corev1.Container{
		Name:            "autocache",
		Image:           ac.Spec.Image,
		ImagePullPolicy: ac.Spec.ImagePullPolicy,
		Ports:           ports,
		Env:             env,
		VolumeMounts:    volumeMounts,
		Resources:       ac.Spec.Resources,
		LivenessProbe:   livenessProbe,
		ReadinessProbe:  readinessProbe,
		Command: []string{
			"/autocache",
			"--cluster-enabled",
			"--bind", "$(POD_IP)",
		},
	}
}

// buildInitContainer builds the init container for config initialization
func (r *AutoCacheReconciler) buildInitContainer(ac *cachev1alpha1.AutoCache) corev1.Container {
	return corev1.Container{
		Name:  "init-config",
		Image: "rancher/mirrored-library-busybox:1.36.1", // Use local image to avoid pull issues
		Command: []string{
			"sh", "-c",
			"echo 'Initializing AutoCache...' && " +
				"cp /config-template/* /config/ && " +
				"echo 'Done'",
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "config-template",
				MountPath: "/config-template",
			},
			{
				Name:      "config",
				MountPath: "/config",
			},
		},
	}
}

// buildVolumes builds the volumes for the pod
func (r *AutoCacheReconciler) buildVolumes(ac *cachev1alpha1.AutoCache) []corev1.Volume {
	volumes := []corev1.Volume{
		{
			Name: "config-template",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: ac.Name + "-config",
					},
				},
			},
		},
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	return volumes
}

// buildVolumeClaimTemplates builds the PVC templates for persistent storage
func (r *AutoCacheReconciler) buildVolumeClaimTemplates(ac *cachev1alpha1.AutoCache) []corev1.PersistentVolumeClaim {
	storage := ac.Spec.Storage

	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "data",
			Labels: ac.GetLabels(),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storage.Size,
				},
			},
		},
	}

	if storage.StorageClassName != nil {
		pvc.Spec.StorageClassName = storage.StorageClassName
	}

	return []corev1.PersistentVolumeClaim{pvc}
}

// buildAffinity builds the affinity configuration for the pod
func (r *AutoCacheReconciler) buildAffinity(ac *cachev1alpha1.AutoCache) *corev1.Affinity {
	// If user customized affinity, use their configuration
	if ac.Spec.Affinity != nil {
		return ac.Spec.Affinity
	}

	// Default: Pod anti-affinity to spread across nodes
	return &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
				{
					Weight: 100,
					PodAffinityTerm: corev1.PodAffinityTerm{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: ac.GetSelectorLabels(),
						},
						TopologyKey: "kubernetes.io/hostname",
					},
				},
			},
		},
	}
}

// updateStatus updates the AutoCache status based on the StatefulSet and Pods
func (r *AutoCacheReconciler) updateStatus(ctx context.Context, ac *cachev1alpha1.AutoCache) error {
	// Get StatefulSet
	sts := &appsv1.StatefulSet{}
	err := r.Get(ctx, types.NamespacedName{Name: ac.Name, Namespace: ac.Namespace}, sts)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil // StatefulSet not created yet
		}
		return err
	}

	// Get Pods
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods,
		client.InNamespace(ac.Namespace),
		client.MatchingLabels(ac.GetSelectorLabels())); err != nil {
		return err
	}

	// Update replica counts
	ac.Status.Replicas = sts.Status.Replicas
	ac.Status.ReadyReplicas = sts.Status.ReadyReplicas

	// Update node information
	ac.Status.Nodes = make([]cachev1alpha1.NodeStatus, 0, len(pods.Items))
	for _, pod := range pods.Items {
		node := cachev1alpha1.NodeStatus{
			Name:  pod.Name,
			PodIP: pod.Status.PodIP,
			Ready: isPodReady(&pod),
			Phase: pod.Status.Phase,
			Role:  "master", // TODO: Get actual role from cluster
		}
		ac.Status.Nodes = append(ac.Status.Nodes, node)
	}

	// Update phase
	if ac.Status.ReadyReplicas == ac.Spec.Replicas && ac.Status.Replicas == ac.Spec.Replicas {
		ac.Status.Phase = cachev1alpha1.ClusterPhaseRunning
		ac.Status.ClusterStatus = "ok"
	}

	// Update timestamp
	now := metav1.Now()
	ac.Status.LastUpdateTime = &now
	ac.Status.ObservedGeneration = ac.Generation

	return r.Status().Update(ctx, ac)
}

// isPodReady checks if a pod is ready
func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
