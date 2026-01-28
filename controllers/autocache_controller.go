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
	"sort"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	cachev1alpha1 "github.com/10yihang/autocache/api/v1alpha1"
)

const (
	finalizerName = "cache.autocache.io/finalizer"

	// Requeue intervals
	requeueAfterSuccess = 30 * time.Second
	requeueAfterError   = 5 * time.Second
)

// AutoCacheReconciler reconciles a AutoCache object
type AutoCacheReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	sendCommandFn func(ctx context.Context, addr string, command string) error
}

// +kubebuilder:rbac:groups=cache.autocache.io,resources=autocaches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cache.autocache.io,resources=autocaches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cache.autocache.io,resources=autocaches/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

// Reconcile is the main reconciliation loop
func (r *AutoCacheReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling AutoCache", "namespace", req.Namespace, "name", req.Name)

	// Fetch the AutoCache resource
	ac := &cachev1alpha1.AutoCache{}
	if err := r.Get(ctx, req.NamespacedName, ac); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("AutoCache not found, possibly deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get AutoCache")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !ac.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, ac)
	}

	// Add finalizer
	if !controllerutil.ContainsFinalizer(ac, finalizerName) {
		controllerutil.AddFinalizer(ac, finalizerName)
		if err := r.Update(ctx, ac); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Initialize status
	if ac.Status.Phase == "" {
		ac.Status.Phase = cachev1alpha1.ClusterPhasePending
		if err := r.Status().Update(ctx, ac); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Reconcile ConfigMap
	if err := r.reconcileConfigMap(ctx, ac); err != nil {
		logger.Error(err, "Failed to reconcile ConfigMap")
		return ctrl.Result{RequeueAfter: requeueAfterError}, err
	}

	// Reconcile Headless Service
	if err := r.reconcileHeadlessService(ctx, ac); err != nil {
		logger.Error(err, "Failed to reconcile Headless Service")
		return ctrl.Result{RequeueAfter: requeueAfterError}, err
	}

	// Reconcile Client Service
	if err := r.reconcileClientService(ctx, ac); err != nil {
		logger.Error(err, "Failed to reconcile Client Service")
		return ctrl.Result{RequeueAfter: requeueAfterError}, err
	}

	// Reconcile StatefulSet
	if err := r.reconcileStatefulSet(ctx, ac); err != nil {
		logger.Error(err, "Failed to reconcile StatefulSet")
		return ctrl.Result{RequeueAfter: requeueAfterError}, err
	}

	// Update status
	if err := r.updateStatus(ctx, ac); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{RequeueAfter: requeueAfterError}, err
	}

	// Check if cluster needs initialization
	if ac.Status.Phase == cachev1alpha1.ClusterPhaseRunning && ac.Status.SlotsAssigned == 0 {
		if err := r.initializeCluster(ctx, ac); err != nil {
			logger.Error(err, "Failed to initialize cluster")
			return ctrl.Result{RequeueAfter: requeueAfterError}, err
		}
	}

	if ac.Status.Phase == cachev1alpha1.ClusterPhaseRunning && ac.Spec.SlotPlan != nil {
		if err := r.reconcileMigration(ctx, ac); err != nil {
			logger.Error(err, "Failed to reconcile migration")
			return ctrl.Result{RequeueAfter: requeueAfterError}, err
		}
	}

	logger.Info("Reconcile completed", "phase", ac.Status.Phase, "ready", ac.Status.ReadyReplicas)
	return ctrl.Result{RequeueAfter: requeueAfterSuccess}, nil
}

// handleDeletion handles the deletion of an AutoCache resource
func (r *AutoCacheReconciler) handleDeletion(ctx context.Context, ac *cachev1alpha1.AutoCache) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Handling deletion")

	// Perform cleanup logic
	// TODO: Clean up external resources if any

	// Remove finalizer
	controllerutil.RemoveFinalizer(ac, finalizerName)
	if err := r.Update(ctx, ac); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcileConfigMap reconciles the ConfigMap for the AutoCache cluster
func (r *AutoCacheReconciler) reconcileConfigMap(ctx context.Context, ac *cachev1alpha1.AutoCache) error {
	cm := r.buildConfigMap(ac)

	// Set OwnerReference
	if err := controllerutil.SetControllerReference(ac, cm, r.Scheme); err != nil {
		return err
	}

	// Create or update
	existingCM := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, existingCM)
	if err != nil {
		if errors.IsNotFound(err) {
			return r.Create(ctx, cm)
		}
		return err
	}

	// Update
	existingCM.Data = cm.Data
	return r.Update(ctx, existingCM)
}

// buildConfigMap builds the ConfigMap for the AutoCache cluster
func (r *AutoCacheReconciler) buildConfigMap(ac *cachev1alpha1.AutoCache) *corev1.ConfigMap {
	data := map[string]string{
		"autocache.conf": r.generateConfig(ac),
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ac.Name + "-config",
			Namespace: ac.Namespace,
			Labels:    ac.GetLabels(),
		},
		Data: data,
	}
}

// generateConfig generates the configuration file content
func (r *AutoCacheReconciler) generateConfig(ac *cachev1alpha1.AutoCache) string {
	config := ac.Spec.Config

	var builder strings.Builder
	fmt.Fprintf(&builder, `# AutoCache Configuration
port %d
cluster-port %d
cluster-enabled yes
cluster-config-file nodes.conf
cluster-node-timeout 15000
`, ac.Spec.Port, ac.Spec.ClusterPort)

	if config != nil {
		if config.MaxMemory != "" {
			fmt.Fprintf(&builder, "maxmemory %s\n", config.MaxMemory)
		}
		if config.MaxMemoryPolicy != "" {
			fmt.Fprintf(&builder, "maxmemory-policy %s\n", config.MaxMemoryPolicy)
		}
		for k, v := range config.Custom {
			fmt.Fprintf(&builder, "%s %s\n", k, v)
		}
	}

	return builder.String()
}

// reconcileHeadlessService reconciles the Headless Service
func (r *AutoCacheReconciler) reconcileHeadlessService(ctx context.Context, ac *cachev1alpha1.AutoCache) error {
	svc := r.buildHeadlessService(ac)

	if err := controllerutil.SetControllerReference(ac, svc, r.Scheme); err != nil {
		return err
	}

	existingSvc := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, existingSvc)
	if err != nil {
		if errors.IsNotFound(err) {
			return r.Create(ctx, svc)
		}
		return err
	}

	return nil
}

// buildHeadlessService builds the Headless Service
func (r *AutoCacheReconciler) buildHeadlessService(ac *cachev1alpha1.AutoCache) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ac.Name + "-headless",
			Namespace: ac.Namespace,
			Labels:    ac.GetLabels(),
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: corev1.ClusterIPNone,
			Selector:  ac.GetSelectorLabels(),
			Ports: []corev1.ServicePort{
				{
					Name:     "redis",
					Port:     ac.Spec.Port,
					Protocol: corev1.ProtocolTCP,
				},
				{
					Name:     "cluster",
					Port:     ac.Spec.ClusterPort,
					Protocol: corev1.ProtocolTCP,
				},
			},
			PublishNotReadyAddresses: true,
		},
	}
}

// reconcileClientService reconciles the Client Service
func (r *AutoCacheReconciler) reconcileClientService(ctx context.Context, ac *cachev1alpha1.AutoCache) error {
	svc := r.buildClientService(ac)

	if err := controllerutil.SetControllerReference(ac, svc, r.Scheme); err != nil {
		return err
	}

	existingSvc := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, existingSvc)
	if err != nil {
		if errors.IsNotFound(err) {
			return r.Create(ctx, svc)
		}
		return err
	}

	return nil
}

// buildClientService builds the Client Service
func (r *AutoCacheReconciler) buildClientService(ac *cachev1alpha1.AutoCache) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        ac.Name,
			Namespace:   ac.Namespace,
			Labels:      ac.GetLabels(),
			Annotations: ac.Spec.ServiceAnnotations,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: ac.GetSelectorLabels(),
			Ports: []corev1.ServicePort{
				{
					Name:     "redis",
					Port:     ac.Spec.Port,
					Protocol: corev1.ProtocolTCP,
				},
			},
		},
	}
}

// initializeCluster initializes the cluster by assigning slots
func (r *AutoCacheReconciler) initializeCluster(ctx context.Context, ac *cachev1alpha1.AutoCache) error {
	logger := log.FromContext(ctx)

	// Get all Ready pods
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods,
		client.InNamespace(ac.Namespace),
		client.MatchingLabels(ac.GetSelectorLabels())); err != nil {
		return err
	}

	var readyPods []corev1.Pod
	for _, pod := range pods.Items {
		if isPodReady(&pod) && pod.Status.PodIP != "" {
			readyPods = append(readyPods, pod)
		}
	}

	if len(readyPods) < int(ac.Spec.Replicas) {
		logger.Info("Waiting for pods to be ready", "ready", len(readyPods), "target", ac.Spec.Replicas)
		return nil // Wait for more pods to be ready
	}

	if len(readyPods) == 0 {
		logger.Info("No ready pods for cluster initialization")
		return nil
	}

	sort.Slice(readyPods, func(i, j int) bool {
		return readyPods[i].Name < readyPods[j].Name
	})

	port := int(ac.Spec.Port)
	if port == 0 {
		port = 6379
	}

	seedPod := readyPods[0]
	seedIP := seedPod.Status.PodIP
	seedAddr := fmt.Sprintf("%s:%d", seedIP, port)
	logger.Info("Initializing cluster", "seed", seedPod.Name, "seedAddr", seedAddr, "nodes", len(readyPods))

	sendCommand := r.sendCommand
	if r.sendCommandFn != nil {
		sendCommand = r.sendCommandFn
	}

	const (
		totalSlots         = 16384
		slotsPerCommand    = 256
		commandTimeoutSecs = 5 * time.Second
	)

	hadErrors := false

	for i := 1; i < len(readyPods); i++ {
		pod := readyPods[i]
		addr := fmt.Sprintf("%s:%d", pod.Status.PodIP, port)
		cmd := fmt.Sprintf("CLUSTER MEET %s %d", seedIP, port)
		cmdCtx, cancel := context.WithTimeout(ctx, commandTimeoutSecs)
		err := sendCommand(cmdCtx, addr, cmd)
		cancel()
		if err != nil {
			if isNonFatalClusterError(err) {
				logger.Info("Non-fatal cluster meet error", "pod", pod.Name, "addr", addr, "error", err)
			} else {
				logger.Error(err, "Failed to send cluster meet", "pod", pod.Name, "addr", addr)
				hadErrors = true
			}
		}
	}

	podsCount := len(readyPods)
	slotsPerPod := totalSlots / podsCount
	remainder := totalSlots % podsCount

	for i, pod := range readyPods {
		start := i*slotsPerPod + min(i, remainder)
		end := (i+1)*slotsPerPod + min(i+1, remainder)

		addr := fmt.Sprintf("%s:%d", pod.Status.PodIP, port)
		for batchStart := start; batchStart < end; batchStart += slotsPerCommand {
			batchEnd := min(batchStart+slotsPerCommand, end)

			args := make([]string, 0, 2+(batchEnd-batchStart))
			args = append(args, "CLUSTER", "ADDSLOTS")
			for slot := batchStart; slot < batchEnd; slot++ {
				args = append(args, strconv.Itoa(slot))
			}

			cmd := strings.Join(args, " ")
			cmdCtx, cancel := context.WithTimeout(ctx, commandTimeoutSecs)
			err := sendCommand(cmdCtx, addr, cmd)
			cancel()
			if err != nil {
				if isNonFatalClusterError(err) {
					logger.Info("Non-fatal cluster addslots error", "pod", pod.Name, "addr", addr, "error", err)
				} else {
					logger.Error(err, "Failed to assign slots", "pod", pod.Name, "addr", addr, "start", batchStart, "end", batchEnd-1)
					hadErrors = true
				}
			}
		}
	}

	if hadErrors {
		logger.Info("Cluster initialization completed with errors; will retry", "readyPods", len(readyPods))
		return nil
	}

	if ac.Status.SlotsAssigned != int32(totalSlots) {
		ac.Status.SlotsAssigned = int32(totalSlots)
		if err := r.Status().Update(ctx, ac); err != nil {
			return err
		}
	}

	logger.Info("Cluster initialization completed", "readyPods", len(readyPods))
	return nil
}

func isNonFatalClusterError(err error) bool {
	if err == nil {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "already") {
		return true
	}
	if strings.Contains(msg, "slot already") || strings.Contains(msg, "already assigned") {
		return true
	}
	return false
}

// SetupWithManager sets up the controller with the Manager
func (r *AutoCacheReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cachev1alpha1.AutoCache{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}
