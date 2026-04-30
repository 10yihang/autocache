package controllers

import (
	"strings"
	"testing"

	cachev1alpha1 "github.com/10yihang/autocache/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildContainer_ConfiguresSupportedServerEntrypoint(t *testing.T) {
	resources := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}
	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"},
		Spec: cachev1alpha1.AutoCacheSpec{
			Image:           "autocache:test",
			ImagePullPolicy: corev1.PullIfNotPresent,
			Resources:       resources,
			Port:            6380,
			ClusterPort:     16380,
		},
	}

	container := (&AutoCacheReconciler{}).buildContainer(ac)

	wantCommand := []string{
		"/autocache",
		"--config", "/etc/autocache/autocache.conf",
		"--data-dir", "/data",
		"--metrics-addr", ":9121",
		"--cluster-enabled",
		"--bind", "$(POD_IP)",
	}
	if strings.Join(container.Command, "\x00") != strings.Join(wantCommand, "\x00") {
		t.Fatalf("command = %#v, want %#v", container.Command, wantCommand)
	}
	if container.Image != "autocache:test" {
		t.Fatalf("image = %q, want autocache:test", container.Image)
	}
	if container.ImagePullPolicy != corev1.PullIfNotPresent {
		t.Fatalf("imagePullPolicy = %q, want %q", container.ImagePullPolicy, corev1.PullIfNotPresent)
	}
	if !resourcesEqual(container.Resources, resources) {
		t.Fatalf("resources = %#v, want %#v", container.Resources, resources)
	}

	requirePort(t, container.Ports, "redis", 6380)
	requirePort(t, container.Ports, "cluster", 16380)
	requirePort(t, container.Ports, "metrics", 9121)
	requireEnvFromField(t, container.Env, "POD_NAME", "metadata.name")
	requireEnvFromField(t, container.Env, "POD_IP", "status.podIP")
	requireEnvValue(t, container.Env, "CLUSTER_ANNOUNCE_PORT", "6380")
	requireEnvValue(t, container.Env, "CLUSTER_ANNOUNCE_BUS_PORT", "16380")
	requireMount(t, container.VolumeMounts, "config", "/etc/autocache")
	requireMount(t, container.VolumeMounts, "data", "/data")
}

func TestBuildStatefulSet_UsesEmptyDirWhenStorageDisabled(t *testing.T) {
	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"},
		Spec: cachev1alpha1.AutoCacheSpec{
			Replicas: 3,
			Image:    "autocache:test",
			Port:     6379,
			Storage:  &cachev1alpha1.StorageSpec{Enabled: false},
		},
	}

	sts := (&AutoCacheReconciler{}).buildStatefulSet(ac)

	if len(sts.Spec.VolumeClaimTemplates) != 0 {
		t.Fatalf("volumeClaimTemplates = %d, want 0", len(sts.Spec.VolumeClaimTemplates))
	}
	dataVolume := requireVolume(t, sts.Spec.Template.Spec.Volumes, "data")
	if dataVolume.EmptyDir == nil {
		t.Fatalf("data volume = %#v, want EmptyDir", dataVolume.VolumeSource)
	}
	requireMount(t, sts.Spec.Template.Spec.Containers[0].VolumeMounts, "data", "/data")
}

func TestBuildStatefulSet_UsesPVCWhenStorageEnabled(t *testing.T) {
	storageClassName := "fast-ssd"
	storageSize := resource.MustParse("20Gi")
	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"},
		Spec: cachev1alpha1.AutoCacheSpec{
			Replicas: 3,
			Image:    "autocache:test",
			Port:     6379,
			Storage: &cachev1alpha1.StorageSpec{
				Enabled:          true,
				StorageClassName: &storageClassName,
				Size:             storageSize,
			},
		},
	}

	sts := (&AutoCacheReconciler{}).buildStatefulSet(ac)

	if dataVolume := findVolume(sts.Spec.Template.Spec.Volumes, "data"); dataVolume != nil {
		t.Fatalf("unexpected data pod volume when PVC is enabled: %#v", dataVolume.VolumeSource)
	}
	if len(sts.Spec.VolumeClaimTemplates) != 1 {
		t.Fatalf("volumeClaimTemplates = %d, want 1", len(sts.Spec.VolumeClaimTemplates))
	}
	pvc := sts.Spec.VolumeClaimTemplates[0]
	if pvc.Name != "data" {
		t.Fatalf("pvc name = %q, want data", pvc.Name)
	}
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != storageClassName {
		t.Fatalf("storageClassName = %v, want %q", pvc.Spec.StorageClassName, storageClassName)
	}
	gotSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if gotSize.Cmp(storageSize) != 0 {
		t.Fatalf("storage size = %s, want %s", gotSize.String(), storageSize.String())
	}
	requireMount(t, sts.Spec.Template.Spec.Containers[0].VolumeMounts, "data", "/data")
}

func TestGenerateConfig_WritesSupportedServerConfig(t *testing.T) {
	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"},
		Spec: cachev1alpha1.AutoCacheSpec{
			Port:        6380,
			ClusterPort: 16380,
			Config: &cachev1alpha1.CacheConfig{
				MaxMemory:       "512mb",
				MaxMemoryPolicy: "allkeys-lru",
				Custom: map[string]string{
					"admin-enabled": "true",
					"admin-addr":    ":8080",
				},
			},
		},
	}

	config := (&AutoCacheReconciler{}).generateConfig(ac)
	for _, wantLine := range []string{
		"port 6380",
		"cluster-port 16380",
		"cluster-enabled yes",
		"cluster-config-file nodes.conf",
		"cluster-node-timeout 15000",
		"maxmemory 512mb",
		"maxmemory-policy allkeys-lru",
		"admin-enabled true",
		"admin-addr :8080",
	} {
		if !hasConfigLine(config, wantLine) {
			t.Fatalf("config missing line %q:\n%s", wantLine, config)
		}
	}
	if hasConfigPrefix(config, "databases ") {
		t.Fatalf("config unexpectedly writes unsupported databases setting:\n%s", config)
	}
}

func TestBuildStatefulSet_MergesPodAnnotations(t *testing.T) {
	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"},
		Spec: cachev1alpha1.AutoCacheSpec{
			Image: "autocache:test",
			PodAnnotations: map[string]string{
				"prometheus.io/port": "19121",
				"example.com/team":   "cache",
			},
		},
	}

	sts := (&AutoCacheReconciler{}).buildStatefulSet(ac)
	annotations := sts.Spec.Template.Annotations
	if annotations["prometheus.io/scrape"] != "true" {
		t.Fatalf("prometheus scrape annotation = %q, want true", annotations["prometheus.io/scrape"])
	}
	if annotations["prometheus.io/port"] != "19121" {
		t.Fatalf("prometheus port annotation = %q, want user override 19121", annotations["prometheus.io/port"])
	}
	if annotations["example.com/team"] != "cache" {
		t.Fatalf("custom pod annotation = %q, want cache", annotations["example.com/team"])
	}
}

func TestBuildServices_ApplyServiceAnnotationsOnlyToClientService(t *testing.T) {
	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"},
		Spec: cachev1alpha1.AutoCacheSpec{
			Port:        6380,
			ClusterPort: 16380,
			ServiceAnnotations: map[string]string{
				"service.beta.kubernetes.io/aws-load-balancer-type": "nlb",
			},
		},
	}
	r := &AutoCacheReconciler{}

	clientSvc := r.buildClientService(ac)
	headlessSvc := r.buildHeadlessService(ac)
	metricsSvc := r.buildMetricsService(ac)

	if clientSvc.Annotations["service.beta.kubernetes.io/aws-load-balancer-type"] != "nlb" {
		t.Fatalf("client service annotations = %#v, want service annotation", clientSvc.Annotations)
	}
	if len(headlessSvc.Annotations) != 0 {
		t.Fatalf("headless service annotations = %#v, want none", headlessSvc.Annotations)
	}
	if len(metricsSvc.Annotations) != 0 {
		t.Fatalf("metrics service annotations = %#v, want none", metricsSvc.Annotations)
	}
}

func TestBuildContainer_ConfiguresTieredStorageArgs(t *testing.T) {
	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"},
		Spec: cachev1alpha1.AutoCacheSpec{
			Image: "autocache:test",
			TieredStorage: &cachev1alpha1.TieredStorageSpec{
				Enabled: true,
			},
		},
	}

	command := (&AutoCacheReconciler{}).buildContainer(ac).Command

	requireCommandFlag(t, command, "--tiered-enabled")
	requireCommandValue(t, command, "--badger-path", "/data/warm")
}

func TestBuildContainer_ConfiguresPersistenceOnlyForSupportedRDBMode(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		wantPersist bool
	}{
		{name: "empty mode defaults to supported RDB persistence", wantPersist: true},
		{name: "RDB mode enables supported write-behind persistence", mode: "RDB", wantPersist: true},
		{name: "AOF mode is unsupported and does not enable persistence", mode: "AOF"},
		{name: "Both mode is unsupported and does not enable persistence", mode: "Both"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac := &cachev1alpha1.AutoCache{
				ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"},
				Spec: cachev1alpha1.AutoCacheSpec{
					Image: "autocache:test",
					Storage: &cachev1alpha1.StorageSpec{
						Enabled:         true,
						PersistenceMode: tt.mode,
					},
					TieredStorage: &cachev1alpha1.TieredStorageSpec{
						Enabled: true,
					},
				},
			}

			command := (&AutoCacheReconciler{}).buildContainer(ac).Command
			if hasCommandFlag(command, "--persist-enabled") != tt.wantPersist {
				t.Fatalf("persist flag presence = %t, want %t in command %#v", hasCommandFlag(command, "--persist-enabled"), tt.wantPersist, command)
			}
		})
	}
}

func TestBuildContainer_DoesNotInjectAuthSecret(t *testing.T) {
	ac := &cachev1alpha1.AutoCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cache", Namespace: "default"},
		Spec: cachev1alpha1.AutoCacheSpec{
			Image: "autocache:test",
			Auth: &cachev1alpha1.AuthSpec{
				Enabled:        true,
				PasswordSecret: "autocache-password",
				PasswordKey:    "password",
			},
		},
	}

	container := (&AutoCacheReconciler{}).buildContainer(ac)

	for _, arg := range container.Command {
		rejectAuthToken(t, "command", arg)
	}
	for _, envVar := range container.Env {
		rejectAuthToken(t, "env name", envVar.Name)
		rejectAuthToken(t, "env value", envVar.Value)
		if envVar.ValueFrom != nil && envVar.ValueFrom.SecretKeyRef != nil {
			t.Fatalf("unexpected auth secret env injection: %#v", envVar)
		}
	}
}

func TestSyncSpecSupportCondition_MarksUnsupportedPersistenceModes(t *testing.T) {
	for _, mode := range []string{"AOF", "Both"} {
		t.Run(mode, func(t *testing.T) {
			ac := &cachev1alpha1.AutoCache{
				Spec: cachev1alpha1.AutoCacheSpec{
					Storage: &cachev1alpha1.StorageSpec{
						Enabled:         true,
						PersistenceMode: mode,
					},
					TieredStorage: &cachev1alpha1.TieredStorageSpec{
						Enabled: true,
					},
				},
			}

			syncSpecSupportCondition(ac)

			condition := meta.FindStatusCondition(ac.Status.Conditions, cachev1alpha1.ConditionTypeDegraded)
			if condition == nil {
				t.Fatalf("missing Degraded condition")
			}
			if condition.Status != metav1.ConditionTrue {
				t.Fatalf("Degraded status = %s, want True", condition.Status)
			}
			if condition.Reason != "UnsupportedPersistenceMode" {
				t.Fatalf("Degraded reason = %q, want UnsupportedPersistenceMode", condition.Reason)
			}
		})
	}
}

func TestSyncSpecSupportCondition_MarksUnsupportedS3ColdTier(t *testing.T) {
	ac := &cachev1alpha1.AutoCache{
		Spec: cachev1alpha1.AutoCacheSpec{
			TieredStorage: &cachev1alpha1.TieredStorageSpec{
				Enabled: true,
				ColdTier: &cachev1alpha1.TierConfig{
					Type: "S3",
				},
			},
		},
	}

	syncSpecSupportCondition(ac)

	condition := meta.FindStatusCondition(ac.Status.Conditions, cachev1alpha1.ConditionTypeDegraded)
	if condition == nil {
		t.Fatalf("missing Degraded condition")
	}
	if condition.Status != metav1.ConditionTrue {
		t.Fatalf("Degraded status = %s, want True", condition.Status)
	}
	if condition.Reason != "UnsupportedColdTier" {
		t.Fatalf("Degraded reason = %q, want UnsupportedColdTier", condition.Reason)
	}
}

func TestSyncSpecSupportCondition_MarksUnsupportedAuth(t *testing.T) {
	ac := &cachev1alpha1.AutoCache{
		Spec: cachev1alpha1.AutoCacheSpec{
			Auth: &cachev1alpha1.AuthSpec{
				Enabled:        true,
				PasswordSecret: "autocache-password",
				PasswordKey:    "password",
			},
		},
	}

	syncSpecSupportCondition(ac)

	condition := meta.FindStatusCondition(ac.Status.Conditions, cachev1alpha1.ConditionTypeDegraded)
	if condition == nil {
		t.Fatalf("missing Degraded condition")
	}
	if condition.Status != metav1.ConditionTrue {
		t.Fatalf("Degraded status = %s, want True", condition.Status)
	}
	if condition.Reason != "UnsupportedAuth" {
		t.Fatalf("Degraded reason = %q, want UnsupportedAuth", condition.Reason)
	}
	if !strings.Contains(condition.Message, "Redis AUTH/requirepass") {
		t.Fatalf("Degraded message = %q, want Redis AUTH/requirepass boundary", condition.Message)
	}
}

func TestSyncSpecSupportCondition_MarksSupportedSpecNotDegraded(t *testing.T) {
	ac := &cachev1alpha1.AutoCache{
		Spec: cachev1alpha1.AutoCacheSpec{
			Storage: &cachev1alpha1.StorageSpec{
				Enabled:         true,
				PersistenceMode: "RDB",
			},
			TieredStorage: &cachev1alpha1.TieredStorageSpec{
				Enabled: true,
			},
			Auth: &cachev1alpha1.AuthSpec{
				Enabled: false,
			},
		},
	}

	syncSpecSupportCondition(ac)

	condition := meta.FindStatusCondition(ac.Status.Conditions, cachev1alpha1.ConditionTypeDegraded)
	if condition == nil {
		t.Fatalf("missing Degraded condition")
	}
	if condition.Status != metav1.ConditionFalse {
		t.Fatalf("Degraded status = %s, want False", condition.Status)
	}
	if condition.Reason != "SpecSupported" {
		t.Fatalf("Degraded reason = %q, want SpecSupported", condition.Reason)
	}
}

func resourcesEqual(got, want corev1.ResourceRequirements) bool {
	if len(got.Limits) != len(want.Limits) || len(got.Requests) != len(want.Requests) {
		return false
	}
	for name, wantQuantity := range want.Limits {
		gotQuantity, ok := got.Limits[name]
		if !ok || gotQuantity.Cmp(wantQuantity) != 0 {
			return false
		}
	}
	for name, wantQuantity := range want.Requests {
		gotQuantity, ok := got.Requests[name]
		if !ok || gotQuantity.Cmp(wantQuantity) != 0 {
			return false
		}
	}
	return true
}

func requirePort(t *testing.T, ports []corev1.ContainerPort, name string, port int32) {
	t.Helper()
	for _, containerPort := range ports {
		if containerPort.Name == name {
			if containerPort.ContainerPort != port {
				t.Fatalf("%s port = %d, want %d", name, containerPort.ContainerPort, port)
			}
			if containerPort.Protocol != corev1.ProtocolTCP {
				t.Fatalf("%s protocol = %s, want TCP", name, containerPort.Protocol)
			}
			return
		}
	}
	t.Fatalf("missing container port %q in %#v", name, ports)
}

func requireEnvFromField(t *testing.T, env []corev1.EnvVar, name, fieldPath string) {
	t.Helper()
	for _, envVar := range env {
		if envVar.Name != name {
			continue
		}
		if envVar.ValueFrom == nil || envVar.ValueFrom.FieldRef == nil {
			t.Fatalf("%s env = %#v, want fieldRef %s", name, envVar, fieldPath)
		}
		if envVar.ValueFrom.FieldRef.FieldPath != fieldPath {
			t.Fatalf("%s fieldPath = %q, want %q", name, envVar.ValueFrom.FieldRef.FieldPath, fieldPath)
		}
		return
	}
	t.Fatalf("missing env %q in %#v", name, env)
}

func requireEnvValue(t *testing.T, env []corev1.EnvVar, name, value string) {
	t.Helper()
	for _, envVar := range env {
		if envVar.Name == name {
			if envVar.Value != value {
				t.Fatalf("%s value = %q, want %q", name, envVar.Value, value)
			}
			return
		}
	}
	t.Fatalf("missing env %q in %#v", name, env)
}

func requireMount(t *testing.T, mounts []corev1.VolumeMount, name, mountPath string) {
	t.Helper()
	for _, mount := range mounts {
		if mount.Name == name {
			if mount.MountPath != mountPath {
				t.Fatalf("%s mountPath = %q, want %q", name, mount.MountPath, mountPath)
			}
			return
		}
	}
	t.Fatalf("missing volume mount %q in %#v", name, mounts)
}

func requireVolume(t *testing.T, volumes []corev1.Volume, name string) corev1.Volume {
	t.Helper()
	volume := findVolume(volumes, name)
	if volume == nil {
		t.Fatalf("missing volume %q in %#v", name, volumes)
	}
	return *volume
}

func findVolume(volumes []corev1.Volume, name string) *corev1.Volume {
	for i := range volumes {
		if volumes[i].Name == name {
			return &volumes[i]
		}
	}
	return nil
}

func hasConfigLine(config, wantLine string) bool {
	for _, line := range strings.Split(config, "\n") {
		if line == wantLine {
			return true
		}
	}
	return false
}

func hasConfigPrefix(config, prefix string) bool {
	for _, line := range strings.Split(config, "\n") {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func requireCommandFlag(t *testing.T, command []string, flag string) {
	t.Helper()
	if !hasCommandFlag(command, flag) {
		t.Fatalf("missing command flag %q in %#v", flag, command)
	}
}

func requireCommandValue(t *testing.T, command []string, flag, value string) {
	t.Helper()
	for i := 0; i < len(command)-1; i++ {
		if command[i] == flag {
			if command[i+1] != value {
				t.Fatalf("%s value = %q, want %q in %#v", flag, command[i+1], value, command)
			}
			return
		}
	}
	t.Fatalf("missing command flag %q with value %q in %#v", flag, value, command)
}

func hasCommandFlag(command []string, flag string) bool {
	for _, arg := range command {
		if arg == flag {
			return true
		}
	}
	return false
}

func rejectAuthToken(t *testing.T, location, value string) {
	t.Helper()
	lower := strings.ToLower(value)
	for _, token := range []string{"auth", "password", "secret", "requirepass"} {
		if strings.Contains(lower, token) {
			t.Fatalf("unexpected %s auth token %q", location, value)
		}
	}
}
