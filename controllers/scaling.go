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
	"net"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	cachev1alpha1 "github.com/10yihang/autocache/api/v1alpha1"
)

// ScaleManager manages scaling operations for AutoCache clusters
type ScaleManager struct {
	client            client.Client
	sendCommandFn     func(ctx context.Context, addr string, command string) error
	sendCommandArgsFn func(ctx context.Context, addr string, args ...string) error
	getNodeSlots      func(ctx context.Context, addr string) ([]int, error)
	getNodeID         func(ctx context.Context, addr string) (string, error)
	getKeysInSlot     func(ctx context.Context, addr string, slot int, count int) ([]string, error)
	sleepFn           func(time.Duration)
	totalSlots        int
}

// NewScaleManager creates a new ScaleManager
func NewScaleManager(c client.Client) *ScaleManager {
	return &ScaleManager{
		client:            c,
		sendCommandArgsFn: sendRESPCommandArgs,
		getNodeSlots:      getNodeSlotsAtAddr,
		getNodeID:         getNodeIDAtAddr,
		getKeysInSlot:     getKeysInSlotAtAddr,
		sleepFn:           time.Sleep,
		totalSlots:        16384,
	}
}

// HandleScaleUp handles scaling up the cluster
func (s *ScaleManager) HandleScaleUp(ctx context.Context, ac *cachev1alpha1.AutoCache, newReplicas, oldReplicas int32) error {
	logger := log.FromContext(ctx)
	logger.Info("Handling scale up", "from", oldReplicas, "to", newReplicas)

	// Wait for new pods to be ready
	for {
		pods := &corev1.PodList{}
		if err := s.client.List(ctx, pods,
			client.InNamespace(ac.Namespace),
			client.MatchingLabels(ac.GetSelectorLabels())); err != nil {
			return err
		}

		readyCount := int32(0)
		for _, pod := range pods.Items {
			if isPodReady(&pod) {
				readyCount++
			}
		}

		if readyCount >= newReplicas {
			break
		}

		logger.Info("Waiting for pods to be ready", "ready", readyCount, "target", newReplicas)
		s.sleepFn(5 * time.Second)
	}

	// Add new nodes to cluster
	if err := s.addNodesToCluster(ctx, ac, oldReplicas, newReplicas); err != nil {
		_ = s.updateMigrationStatus(ctx, ac, cachev1alpha1.MigrationPhaseFailed, -1, "", "", err.Error())
		return err
	}

	// Rebalance slots
	if err := s.rebalanceSlots(ctx, ac); err != nil {
		_ = s.updateMigrationStatus(ctx, ac, cachev1alpha1.MigrationPhaseFailed, -1, "", "", err.Error())
		return err
	}

	return nil
}

// HandleScaleDown handles scaling down the cluster
func (s *ScaleManager) HandleScaleDown(ctx context.Context, ac *cachev1alpha1.AutoCache, newReplicas, oldReplicas int32) error {
	logger := log.FromContext(ctx)
	logger.Info("Handling scale down", "from", oldReplicas, "to", newReplicas)

	// Migrate data from nodes that will be removed
	for i := oldReplicas - 1; i >= newReplicas; i-- {
		podName := fmt.Sprintf("%s-%d", ac.Name, i)
		if err := s.migrateDataFromNode(ctx, ac, podName, newReplicas); err != nil {
			_ = s.updateMigrationStatus(ctx, ac, cachev1alpha1.MigrationPhaseFailed, -1, podName, "", err.Error())
			return err
		}
		if err := s.removeNodeFromCluster(ctx, ac, podName); err != nil {
			_ = s.updateMigrationStatus(ctx, ac, cachev1alpha1.MigrationPhaseFailed, -1, podName, "", err.Error())
			return err
		}
	}

	return nil
}

// addNodesToCluster adds new nodes to the cluster
func (s *ScaleManager) addNodesToCluster(ctx context.Context, ac *cachev1alpha1.AutoCache, start, end int32) error {
	logger := log.FromContext(ctx)

	// Get seed node (first pod)
	firstPod := &corev1.Pod{}
	if err := s.client.Get(ctx, client.ObjectKey{
		Namespace: ac.Namespace,
		Name:      fmt.Sprintf("%s-0", ac.Name),
	}, firstPod); err != nil {
		return err
	}

	seedAddr := fmt.Sprintf("%s:%d", firstPod.Status.PodIP, ac.Spec.Port)

	// Make new nodes MEET the seed node
	for i := start; i < end; i++ {
		podName := fmt.Sprintf("%s-%d", ac.Name, i)
		pod := &corev1.Pod{}
		if err := s.client.Get(ctx, client.ObjectKey{
			Namespace: ac.Namespace,
			Name:      podName,
		}, pod); err != nil {
			return err
		}

		addr := fmt.Sprintf("%s:%d", pod.Status.PodIP, ac.Spec.Port)
		if err := s.sendCommand(ctx, addr, "CLUSTER", "MEET", firstPod.Status.PodIP, fmt.Sprintf("%d", ac.Spec.Port)); err != nil {
			return err
		}
		logger.Info("Adding node to cluster", "pod", podName, "seed", seedAddr)
	}

	return nil
}

// migrateDataFromNode migrates data from a node before removal
func (s *ScaleManager) migrateDataFromNode(ctx context.Context, ac *cachev1alpha1.AutoCache, podName string, targetReplicas int32) error {
	logger := log.FromContext(ctx)
	logger.Info("Migrating data from node", "pod", podName)

	readyPods, err := s.listReadyPods(ctx, ac)
	if err != nil {
		return err
	}
	retainedPods := make([]corev1.Pod, 0, len(readyPods))
	for _, pod := range readyPods {
		if pod.Name != podName {
			retainedPods = append(retainedPods, pod)
		}
	}
	if int32(len(retainedPods)) < targetReplicas {
		return fmt.Errorf("insufficient ready pods for scale down: have %d want %d", len(retainedPods), targetReplicas)
	}
	retainedPods = retainedPods[:targetReplicas]

	nodeAddrs := make(map[string]string, len(readyPods))
	currentOwnerBySlot := make(map[int]string)
	for _, pod := range readyPods {
		addr := fmt.Sprintf("%s:%d", pod.Status.PodIP, ac.Spec.Port)
		nodeAddrs[pod.Name] = addr
		slots, err := s.getNodeSlots(ctx, addr)
		if err != nil {
			return err
		}
		for _, slot := range slots {
			currentOwnerBySlot[slot] = pod.Name
		}
	}

	targetOwnerBySlot := s.buildTargetOwnerBySlot(retainedPods)
	sourceAddr := nodeAddrs[podName]
	for slot := 0; slot < s.totalSlots; slot++ {
		if currentOwnerBySlot[slot] != podName {
			continue
		}
		targetNode := targetOwnerBySlot[slot]
		if targetNode == "" {
			continue
		}
		if err := s.updateMigrationStatus(ctx, ac, cachev1alpha1.MigrationPhaseInProgress, slot, podName, targetNode, fmt.Sprintf("Draining slot %d from %s", slot, podName)); err != nil {
			return err
		}
		if err := s.moveSlot(ctx, sourceAddr, nodeAddrs[targetNode], slot); err != nil {
			return err
		}
	}

	return s.updateMigrationStatus(ctx, ac, cachev1alpha1.MigrationPhaseCompleted, -1, podName, "", fmt.Sprintf("Drained node %s", podName))
}

// removeNodeFromCluster removes a node from the cluster
func (s *ScaleManager) removeNodeFromCluster(ctx context.Context, ac *cachev1alpha1.AutoCache, podName string) error {
	logger := log.FromContext(ctx)
	logger.Info("Removing node from cluster", "pod", podName)

	readyPods, err := s.listReadyPods(ctx, ac)
	if err != nil {
		return err
	}

	var removedAddr string
	for _, pod := range readyPods {
		if pod.Name == podName {
			removedAddr = fmt.Sprintf("%s:%d", pod.Status.PodIP, ac.Spec.Port)
			break
		}
	}
	if removedAddr == "" {
		return nil
	}

	removedNodeID, err := s.getNodeID(ctx, removedAddr)
	if err != nil {
		return err
	}
	for _, pod := range readyPods {
		if pod.Name == podName {
			continue
		}
		addr := fmt.Sprintf("%s:%d", pod.Status.PodIP, ac.Spec.Port)
		if err := s.sendCommand(ctx, addr, "CLUSTER", "FORGET", removedNodeID); err != nil {
			return err
		}
	}
	return nil
}

// rebalanceSlots redistributes slots across all nodes
func (s *ScaleManager) rebalanceSlots(ctx context.Context, ac *cachev1alpha1.AutoCache) error {
	logger := log.FromContext(ctx)
	logger.Info("Rebalancing slots")

	readyPods, err := s.listReadyPods(ctx, ac)
	if err != nil {
		return err
	}
	if len(readyPods) == 0 {
		return nil
	}

	nodeAddrs := make(map[string]string, len(readyPods))
	currentOwnerBySlot := make(map[int]string)
	for _, pod := range readyPods {
		addr := fmt.Sprintf("%s:%d", pod.Status.PodIP, ac.Spec.Port)
		nodeAddrs[pod.Name] = addr
		slots, err := s.getNodeSlots(ctx, addr)
		if err != nil {
			return err
		}
		for _, slot := range slots {
			currentOwnerBySlot[slot] = pod.Name
		}
	}

	targetOwnerBySlot := s.buildTargetOwnerBySlot(readyPods)

	for slot := 0; slot < s.totalSlots; slot++ {
		sourceNode := currentOwnerBySlot[slot]
		targetNode := targetOwnerBySlot[slot]
		if sourceNode == "" || sourceNode == targetNode {
			continue
		}

		sourceAddr := nodeAddrs[sourceNode]
		targetAddr := nodeAddrs[targetNode]
		if err := s.updateMigrationStatus(ctx, ac, cachev1alpha1.MigrationPhaseInProgress, slot, sourceNode, targetNode, fmt.Sprintf("Rebalancing slot %d", slot)); err != nil {
			return err
		}
		if err := s.moveSlot(ctx, sourceAddr, targetAddr, slot); err != nil {
			return err
		}
	}

	return s.updateMigrationStatus(ctx, ac, cachev1alpha1.MigrationPhaseCompleted, -1, "", "", "Rebalance completed")
}

func (s *ScaleManager) listReadyPods(ctx context.Context, ac *cachev1alpha1.AutoCache) ([]corev1.Pod, error) {
	pods := &corev1.PodList{}
	if err := s.client.List(ctx, pods,
		client.InNamespace(ac.Namespace),
		client.MatchingLabels(ac.GetSelectorLabels())); err != nil {
		return nil, err
	}
	readyPods := make([]corev1.Pod, 0, len(pods.Items))
	for _, pod := range pods.Items {
		if isPodReady(&pod) && pod.Status.PodIP != "" {
			readyPods = append(readyPods, pod)
		}
	}
	sort.Slice(readyPods, func(i, j int) bool { return readyPods[i].Name < readyPods[j].Name })
	return readyPods, nil
}

func (s *ScaleManager) buildTargetOwnerBySlot(pods []corev1.Pod) map[int]string {
	result := make(map[int]string, s.totalSlots)
	if len(pods) == 0 {
		return result
	}
	slotsPerPod := s.totalSlots / len(pods)
	remainder := s.totalSlots % len(pods)
	start := 0
	for i, pod := range pods {
		end := start + slotsPerPod
		if i < remainder {
			end++
		}
		for slot := start; slot < end; slot++ {
			result[slot] = pod.Name
		}
		start = end
	}
	return result
}

func (s *ScaleManager) moveSlot(ctx context.Context, sourceAddr, targetAddr string, slot int) error {
	targetNodeID, err := s.getNodeID(ctx, targetAddr)
	if err != nil {
		return err
	}
	sourceNodeID, err := s.getNodeID(ctx, sourceAddr)
	if err != nil {
		return err
	}

	if err := s.sendCommand(ctx, targetAddr, "CLUSTER", "SETSLOT", fmt.Sprintf("%d", slot), "IMPORTING", sourceNodeID); err != nil {
		return err
	}
	if err := s.sendCommand(ctx, sourceAddr, "CLUSTER", "SETSLOT", fmt.Sprintf("%d", slot), "MIGRATING", targetNodeID); err != nil {
		_ = s.rollbackSlotMove(ctx, sourceAddr, targetAddr, slot)
		return err
	}

	host, portStr, err := net.SplitHostPort(targetAddr)
	if err != nil {
		return err
	}
	port := 0
	fmt.Sscanf(portStr, "%d", &port)

	for {
		keys, err := s.getKeysInSlot(ctx, sourceAddr, slot, 100)
		if err != nil {
			_ = s.rollbackSlotMove(ctx, sourceAddr, targetAddr, slot)
			return err
		}
		if len(keys) == 0 {
			break
		}
		for _, key := range keys {
			if err := s.sendCommand(ctx, sourceAddr, "MIGRATE", host, fmt.Sprintf("%d", port), key, "0", "5000", "REPLACE"); err != nil {
				_ = s.rollbackSlotMove(ctx, sourceAddr, targetAddr, slot)
				return err
			}
		}
	}

	if err := s.sendCommand(ctx, targetAddr, "CLUSTER", "SETSLOT", fmt.Sprintf("%d", slot), "NODE", targetNodeID); err != nil {
		_ = s.rollbackSlotMove(ctx, sourceAddr, targetAddr, slot)
		return err
	}
	if err := s.sendCommand(ctx, sourceAddr, "CLUSTER", "SETSLOT", fmt.Sprintf("%d", slot), "NODE", targetNodeID); err != nil {
		_ = s.rollbackSlotMove(ctx, sourceAddr, targetAddr, slot)
		return err
	}
	return nil
}

func (s *ScaleManager) rollbackSlotMove(ctx context.Context, sourceAddr, targetAddr string, slot int) error {
	var rollbackErrs []string
	if err := s.sendCommand(ctx, sourceAddr, "CLUSTER", "SETSLOT", fmt.Sprintf("%d", slot), "STABLE"); err != nil {
		rollbackErrs = append(rollbackErrs, err.Error())
	}
	if err := s.sendCommand(ctx, targetAddr, "CLUSTER", "SETSLOT", fmt.Sprintf("%d", slot), "STABLE"); err != nil {
		rollbackErrs = append(rollbackErrs, err.Error())
	}
	if len(rollbackErrs) > 0 {
		return fmt.Errorf("rollback slot %d: %s", slot, strings.Join(rollbackErrs, "; "))
	}
	return nil
}

func (s *ScaleManager) sendCommand(ctx context.Context, addr string, args ...string) error {
	if s.sendCommandArgsFn != nil {
		return s.sendCommandArgsFn(ctx, addr, args...)
	}
	if s.sendCommandFn != nil {
		return s.sendCommandFn(ctx, addr, strings.Join(args, " "))
	}
	return sendRESPCommandArgs(ctx, addr, args...)
}

func (s *ScaleManager) updateMigrationStatus(ctx context.Context, ac *cachev1alpha1.AutoCache, phase cachev1alpha1.MigrationPhase, slot int, sourceNode, targetNode, message string) error {
	status := &cachev1alpha1.MigrationStatus{
		Phase:   phase,
		Message: message,
	}
	if slot >= 0 {
		s := int32(slot)
		status.CurrentSlot = &s
	}
	if sourceNode != "" {
		status.SourceNode = sourceNode
	}
	if targetNode != "" {
		status.TargetNode = targetNode
	}
	if phase == cachev1alpha1.MigrationPhaseInProgress {
		now := metav1.Now()
		status.StartTime = &now
	}
	if phase == cachev1alpha1.MigrationPhaseCompleted {
		now := metav1.Now()
		status.CompletionTime = &now
	}
	ac.Status.Migration = status
	return s.client.Status().Update(ctx, ac)
}
