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
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	cachev1alpha1 "github.com/10yihang/autocache/api/v1alpha1"
)

// ScaleManager manages scaling operations for AutoCache clusters
type ScaleManager struct {
	client client.Client
}

// NewScaleManager creates a new ScaleManager
func NewScaleManager(c client.Client) *ScaleManager {
	return &ScaleManager{client: c}
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
		time.Sleep(5 * time.Second)
	}

	// Add new nodes to cluster
	if err := s.addNodesToCluster(ctx, ac, oldReplicas, newReplicas); err != nil {
		return err
	}

	// Rebalance slots
	if err := s.rebalanceSlots(ctx, ac); err != nil {
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
		if err := s.migrateDataFromNode(ctx, ac, podName); err != nil {
			return err
		}
		if err := s.removeNodeFromCluster(ctx, ac, podName); err != nil {
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

	seedAddr := fmt.Sprintf("%s:%d", firstPod.Status.PodIP, ac.Spec.ClusterPort)

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

		// TODO: Execute CLUSTER MEET command via pod exec
		// redis-cli -h <new-pod-ip> -p <port> CLUSTER MEET <seed-ip> <seed-port>
		logger.Info("Adding node to cluster", "pod", podName, "seed", seedAddr)
	}

	return nil
}

// migrateDataFromNode migrates data from a node before removal
func (s *ScaleManager) migrateDataFromNode(ctx context.Context, ac *cachev1alpha1.AutoCache, podName string) error {
	logger := log.FromContext(ctx)
	logger.Info("Migrating data from node", "pod", podName)

	// TODO: Implement slot migration
	// 1. Get slots assigned to this node via CLUSTER SLOTS
	// 2. For each slot:
	//    a. Set slot state to MIGRATING on source
	//    b. Set slot state to IMPORTING on destination
	//    c. Migrate keys using MIGRATE command
	//    d. Set slot state to STABLE

	return nil
}

// removeNodeFromCluster removes a node from the cluster
func (s *ScaleManager) removeNodeFromCluster(ctx context.Context, ac *cachev1alpha1.AutoCache, podName string) error {
	logger := log.FromContext(ctx)
	logger.Info("Removing node from cluster", "pod", podName)

	// TODO: Execute CLUSTER FORGET command on all nodes
	// redis-cli -h <other-node> -p <port> CLUSTER FORGET <node-id>

	return nil
}

// rebalanceSlots redistributes slots across all nodes
func (s *ScaleManager) rebalanceSlots(ctx context.Context, ac *cachev1alpha1.AutoCache) error {
	logger := log.FromContext(ctx)
	logger.Info("Rebalancing slots")

	// TODO: Implement slot rebalancing
	// 1. Get current slot distribution via CLUSTER SLOTS
	// 2. Calculate target distribution (16384 / number of nodes)
	// 3. Migrate slots from nodes with excess to nodes with deficit

	return nil
}
