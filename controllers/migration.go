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
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	cachev1alpha1 "github.com/10yihang/autocache/api/v1alpha1"
)

type SlotRange struct {
	Start int
	End   int
}

type SlotPlanEntry struct {
	Range    SlotRange
	NodeName string
}

func parseSlotPlan(plan map[string]string) ([]SlotPlanEntry, error) {
	var entries []SlotPlanEntry
	for rangeStr, nodeName := range plan {
		parts := strings.Split(rangeStr, "-")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid slot range format: %s", rangeStr)
		}
		start, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid slot start: %s", parts[0])
		}
		end, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid slot end: %s", parts[1])
		}
		if start < 0 || end >= 16384 || start > end {
			return nil, fmt.Errorf("invalid slot range: %d-%d", start, end)
		}
		entries = append(entries, SlotPlanEntry{
			Range:    SlotRange{Start: start, End: end},
			NodeName: nodeName,
		})
	}
	return entries, nil
}

func (r *AutoCacheReconciler) reconcileMigration(ctx context.Context, ac *cachev1alpha1.AutoCache) error {
	logger := log.FromContext(ctx)

	if len(ac.Spec.SlotPlan) == 0 {
		return nil
	}

	if ac.Status.Migration != nil && ac.Status.Migration.Phase == cachev1alpha1.MigrationPhaseInProgress {
		return r.checkMigrationProgress(ctx, ac)
	}

	planEntries, err := parseSlotPlan(ac.Spec.SlotPlan)
	if err != nil {
		logger.Error(err, "Failed to parse slot plan")
		return err
	}

	currentSlotMap, err := r.getCurrentSlotMap(ctx, ac)
	if err != nil {
		logger.Error(err, "Failed to get current slot map")
		return err
	}

	migrations := r.calculateMigrations(planEntries, currentSlotMap, ac)
	if len(migrations) == 0 {
		logger.Info("No migrations needed, slot plan matches current state")
		if ac.Status.Migration != nil && ac.Status.Migration.Phase != cachev1alpha1.MigrationPhaseCompleted {
			ac.Status.Migration = &cachev1alpha1.MigrationStatus{
				Phase:          cachev1alpha1.MigrationPhaseCompleted,
				CompletionTime: &metav1.Time{Time: time.Now()},
				Message:        "All slots are correctly distributed",
			}
			return r.Status().Update(ctx, ac)
		}
		return nil
	}

	logger.Info("Migrations needed", "count", len(migrations))
	return r.startMigration(ctx, ac, migrations[0])
}

type MigrationTask struct {
	Slot       int
	SourceNode string
	SourceAddr string
	TargetNode string
	TargetAddr string
}

func (r *AutoCacheReconciler) calculateMigrations(plan []SlotPlanEntry, current map[int]string, ac *cachev1alpha1.AutoCache) []MigrationTask {
	var tasks []MigrationTask

	nodeAddrs := make(map[string]string)
	for _, node := range ac.Status.Nodes {
		if node.PodIP != "" {
			nodeAddrs[node.Name] = fmt.Sprintf("%s:%d", node.PodIP, ac.Spec.Port)
		}
	}

	for _, entry := range plan {
		for slot := entry.Range.Start; slot <= entry.Range.End; slot++ {
			currentOwner, exists := current[slot]
			if !exists || currentOwner != entry.NodeName {
				if currentOwner == "" {
					continue
				}
				sourceAddr, sourceOK := nodeAddrs[currentOwner]
				targetAddr, targetOK := nodeAddrs[entry.NodeName]
				if sourceOK && targetOK {
					tasks = append(tasks, MigrationTask{
						Slot:       slot,
						SourceNode: currentOwner,
						SourceAddr: sourceAddr,
						TargetNode: entry.NodeName,
						TargetAddr: targetAddr,
					})
				}
			}
		}
	}

	return tasks
}

func (r *AutoCacheReconciler) getCurrentSlotMap(ctx context.Context, ac *cachev1alpha1.AutoCache) (map[int]string, error) {
	slotMap := make(map[int]string)

	pods := &corev1.PodList{}
	if err := r.List(ctx, pods,
		client.InNamespace(ac.Namespace),
		client.MatchingLabels(ac.GetSelectorLabels())); err != nil {
		return nil, err
	}

	for _, pod := range pods.Items {
		if !isPodReady(&pod) || pod.Status.PodIP == "" {
			continue
		}

		addr := fmt.Sprintf("%s:%d", pod.Status.PodIP, ac.Spec.Port)
		slots, err := r.getNodeSlots(ctx, addr)
		if err != nil {
			continue
		}

		for _, slot := range slots {
			slotMap[slot] = pod.Name
		}
	}

	return slotMap, nil
}

func (r *AutoCacheReconciler) getNodeSlots(ctx context.Context, addr string) ([]int, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := setConnDeadline(ctx, conn, 5*time.Second); err != nil {
		return nil, err
	}

	_, err = conn.Write([]byte("*2\r\n$7\r\nCLUSTER\r\n$5\r\nSLOTS\r\n"))
	if err != nil {
		return nil, err
	}

	reader := bufio.NewReader(conn)
	slots, err := parseNodeSlots(reader)
	if err != nil {
		return nil, fmt.Errorf("parse cluster slots: %w", err)
	}

	return slots, nil
}

func parseNodeSlots(reader *bufio.Reader) ([]int, error) {
	resp, err := parseRESP(reader)
	if err != nil {
		return nil, err
	}

	return parseClusterSlots(resp)
}

func parseClusterSlots(resp any) ([]int, error) {
	entries, ok := resp.([]any)
	if !ok {
		return nil, fmt.Errorf("cluster slots: expected array")
	}

	maxInt := int(^uint(0) >> 1)
	slots := make([]int, 0)
	for i, entry := range entries {
		rangeEntry, ok := entry.([]any)
		if !ok {
			return nil, fmt.Errorf("cluster slots[%d]: expected array", i)
		}
		if len(rangeEntry) < 2 {
			return nil, fmt.Errorf("cluster slots[%d]: expected start/end", i)
		}
		start, ok := rangeEntry[0].(int64)
		if !ok {
			return nil, fmt.Errorf("cluster slots[%d]: start not integer", i)
		}
		end, ok := rangeEntry[1].(int64)
		if !ok {
			return nil, fmt.Errorf("cluster slots[%d]: end not integer", i)
		}
		if start < 0 || end < 0 {
			return nil, fmt.Errorf("cluster slots[%d]: negative range", i)
		}
		if start > end {
			return nil, fmt.Errorf("cluster slots[%d]: start greater than end", i)
		}
		if start > int64(maxInt) || end > int64(maxInt) {
			return nil, fmt.Errorf("cluster slots[%d]: range overflows int", i)
		}
		for slot := int(start); slot <= int(end); slot++ {
			slots = append(slots, slot)
		}
	}

	return slots, nil
}

func setConnDeadline(ctx context.Context, conn net.Conn, timeout time.Duration) error {
	if ctx != nil {
		if deadline, ok := ctx.Deadline(); ok {
			return conn.SetDeadline(deadline)
		}
	}

	return conn.SetDeadline(time.Now().Add(timeout))
}

func (r *AutoCacheReconciler) startMigration(ctx context.Context, ac *cachev1alpha1.AutoCache, task MigrationTask) error {
	logger := log.FromContext(ctx)
	logger.Info("Starting migration", "slot", task.Slot, "from", task.SourceNode, "to", task.TargetNode)

	slot := int32(task.Slot)
	ac.Status.Migration = &cachev1alpha1.MigrationStatus{
		Phase:       cachev1alpha1.MigrationPhaseInProgress,
		CurrentSlot: &slot,
		SourceNode:  task.SourceNode,
		TargetNode:  task.TargetNode,
		StartTime:   &metav1.Time{Time: time.Now()},
		Message:     fmt.Sprintf("Migrating slot %d from %s to %s", task.Slot, task.SourceNode, task.TargetNode),
	}

	if err := r.Status().Update(ctx, ac); err != nil {
		return err
	}

	targetNodeID, err := r.getNodeID(ctx, task.TargetAddr)
	if err != nil {
		return r.failMigration(ctx, ac, fmt.Sprintf("Failed to get target node ID: %v", err))
	}

	sourceNodeID, err := r.getNodeID(ctx, task.SourceAddr)
	if err != nil {
		return r.failMigration(ctx, ac, fmt.Sprintf("Failed to get source node ID: %v", err))
	}

	if err := r.sendCommand(ctx, task.TargetAddr, fmt.Sprintf("CLUSTER SETSLOT %d IMPORTING %s", task.Slot, sourceNodeID)); err != nil {
		return r.failMigration(ctx, ac, fmt.Sprintf("Failed to set IMPORTING: %v", err))
	}

	if err := r.sendCommand(ctx, task.SourceAddr, fmt.Sprintf("CLUSTER SETSLOT %d MIGRATING %s", task.Slot, targetNodeID)); err != nil {
		return r.failMigration(ctx, ac, fmt.Sprintf("Failed to set MIGRATING: %v", err))
	}

	go r.migrateSlotKeys(context.Background(), ac.Namespace, ac.Name, task)

	return nil
}

func (r *AutoCacheReconciler) migrateSlotKeys(ctx context.Context, namespace, name string, task MigrationTask) {
	logger := log.FromContext(ctx).WithValues("namespace", namespace, "name", name)

	host, portStr, _ := net.SplitHostPort(task.TargetAddr)
	port, _ := strconv.Atoi(portStr)

	for {
		keys, err := r.getKeysInSlot(ctx, task.SourceAddr, task.Slot, 100)
		if err != nil {
			logger.Error(err, "Failed to get keys in slot")
			break
		}

		if len(keys) == 0 {
			break
		}

		for _, key := range keys {
			cmd := fmt.Sprintf("MIGRATE %s %d %s 0 5000 REPLACE", host, port, key)
			if err := r.sendCommand(ctx, task.SourceAddr, cmd); err != nil {
				logger.Error(err, "Failed to migrate key", "key", key)
			}
		}
	}
}

func (r *AutoCacheReconciler) getKeysInSlot(ctx context.Context, addr string, slot int, count int) ([]string, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := setConnDeadline(ctx, conn, 5*time.Second); err != nil {
		return nil, err
	}

	cmd := fmt.Sprintf("*4\r\n$7\r\nCLUSTER\r\n$13\r\nGETKEYSINSLOT\r\n$%d\r\n%d\r\n$%d\r\n%d\r\n",
		len(strconv.Itoa(slot)), slot, len(strconv.Itoa(count)), count)

	_, err = conn.Write([]byte(cmd))
	if err != nil {
		return nil, err
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}

	if strings.HasPrefix(line, "-") {
		return nil, fmt.Errorf("error: %s", strings.TrimSpace(line))
	}

	if !strings.HasPrefix(line, "*") {
		return nil, fmt.Errorf("unexpected response: %s", line)
	}

	countStr := strings.TrimPrefix(strings.TrimSpace(line), "*")
	keyCount, _ := strconv.Atoi(countStr)

	keys := make([]string, 0, keyCount)
	for range keyCount {
		line, err := reader.ReadString('\n')
		if err != nil {
			return keys, err
		}
		if strings.HasPrefix(line, "$") {
			keyLine, _ := reader.ReadString('\n')
			keys = append(keys, strings.TrimSpace(keyLine))
		}
	}

	return keys, nil
}

func (r *AutoCacheReconciler) checkMigrationProgress(ctx context.Context, ac *cachev1alpha1.AutoCache) error {
	if ac.Status.Migration == nil || ac.Status.Migration.CurrentSlot == nil {
		return nil
	}

	slot := int(*ac.Status.Migration.CurrentSlot)
	sourceAddr := r.getNodeAddr(ac, ac.Status.Migration.SourceNode)
	if sourceAddr == "" {
		return nil
	}

	keys, err := r.getKeysInSlot(ctx, sourceAddr, slot, 1)
	if err != nil {
		return err
	}

	if len(keys) == 0 {
		return r.completeMigration(ctx, ac)
	}

	return nil
}

func (r *AutoCacheReconciler) completeMigration(ctx context.Context, ac *cachev1alpha1.AutoCache) error {
	logger := log.FromContext(ctx)

	if ac.Status.Migration == nil || ac.Status.Migration.CurrentSlot == nil {
		return nil
	}

	slot := int(*ac.Status.Migration.CurrentSlot)
	targetAddr := r.getNodeAddr(ac, ac.Status.Migration.TargetNode)
	sourceAddr := r.getNodeAddr(ac, ac.Status.Migration.SourceNode)

	targetNodeID, err := r.getNodeID(ctx, targetAddr)
	if err != nil {
		return r.failMigration(ctx, ac, fmt.Sprintf("Failed to get target node ID: %v", err))
	}

	if err := r.sendCommand(ctx, targetAddr, fmt.Sprintf("CLUSTER SETSLOT %d NODE %s", slot, targetNodeID)); err != nil {
		return r.failMigration(ctx, ac, fmt.Sprintf("Failed to finalize target: %v", err))
	}

	if err := r.sendCommand(ctx, sourceAddr, fmt.Sprintf("CLUSTER SETSLOT %d NODE %s", slot, targetNodeID)); err != nil {
		logger.Info("Failed to finalize source (may be expected)", "error", err)
	}

	logger.Info("Migration completed", "slot", slot)

	ac.Status.Migration = &cachev1alpha1.MigrationStatus{
		Phase:          cachev1alpha1.MigrationPhaseCompleted,
		CompletionTime: &metav1.Time{Time: time.Now()},
		Message:        fmt.Sprintf("Slot %d migrated successfully", slot),
	}

	return r.Status().Update(ctx, ac)
}

func (r *AutoCacheReconciler) failMigration(ctx context.Context, ac *cachev1alpha1.AutoCache, message string) error {
	logger := log.FromContext(ctx)
	logger.Error(nil, "Migration failed", "message", message)

	ac.Status.Migration = &cachev1alpha1.MigrationStatus{
		Phase:          cachev1alpha1.MigrationPhaseFailed,
		CompletionTime: &metav1.Time{Time: time.Now()},
		Message:        message,
	}

	return r.Status().Update(ctx, ac)
}

func (r *AutoCacheReconciler) getNodeAddr(ac *cachev1alpha1.AutoCache, nodeName string) string {
	for _, node := range ac.Status.Nodes {
		if node.Name == nodeName && node.PodIP != "" {
			return fmt.Sprintf("%s:%d", node.PodIP, ac.Spec.Port)
		}
	}
	return ""
}

func (r *AutoCacheReconciler) getNodeID(ctx context.Context, addr string) (string, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	if err := setConnDeadline(ctx, conn, 5*time.Second); err != nil {
		return "", err
	}

	_, err = conn.Write([]byte("*2\r\n$7\r\nCLUSTER\r\n$4\r\nMYID\r\n"))
	if err != nil {
		return "", err
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	if strings.HasPrefix(line, "$") {
		idLine, _ := reader.ReadString('\n')
		return strings.TrimSpace(idLine), nil
	}

	if strings.HasPrefix(line, "+") {
		return strings.TrimPrefix(strings.TrimSpace(line), "+"), nil
	}

	return "", fmt.Errorf("unexpected response: %s", line)
}

func (r *AutoCacheReconciler) sendCommand(ctx context.Context, addr string, command string) error {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := setConnDeadline(ctx, conn, 5*time.Second); err != nil {
		return err
	}

	parts := strings.Fields(command)
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("*%d\r\n", len(parts)))
	for _, part := range parts {
		builder.WriteString(fmt.Sprintf("$%d\r\n%s\r\n", len(part), part))
	}
	respCmd := builder.String()

	_, err = conn.Write([]byte(respCmd))
	if err != nil {
		return err
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}

	if strings.HasPrefix(line, "-") {
		return fmt.Errorf("command error: %s", strings.TrimSpace(line))
	}

	return nil
}
