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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AutoCacheSpec defines the desired state of AutoCache
type AutoCacheSpec struct {
	// Replicas is the number of cache nodes
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	// +kubebuilder:default=3
	Replicas int32 `json:"replicas,omitempty"`

	// Image is the container image for AutoCache
	// +kubebuilder:default="autocache:latest"
	Image string `json:"image,omitempty"`

	// ImagePullPolicy is the image pull policy
	// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
	// +kubebuilder:default="IfNotPresent"
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// Resources defines the resource requirements for the container
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Port is the Redis protocol port
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=6379
	Port int32 `json:"port,omitempty"`

	// ClusterPort is the cluster communication port
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=16379
	ClusterPort int32 `json:"clusterPort,omitempty"`

	// Storage defines the storage configuration
	// +optional
	Storage *StorageSpec `json:"storage,omitempty"`

	// Config defines cache configuration options
	// +optional
	Config *CacheConfig `json:"config,omitempty"`

	// Auth defines authentication configuration
	// +optional
	Auth *AuthSpec `json:"auth,omitempty"`

	// Affinity defines pod affinity rules
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Tolerations defines pod tolerations
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// NodeSelector defines node selection constraints
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// PodAnnotations defines annotations to add to pods
	// +optional
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`

	// ServiceAnnotations defines annotations to add to services
	// +optional
	ServiceAnnotations map[string]string `json:"serviceAnnotations,omitempty"`

	// TieredStorage defines tiered storage configuration
	// +optional
	TieredStorage *TieredStorageSpec `json:"tieredStorage,omitempty"`

	// SlotPlan defines the target slot distribution across nodes
	// Format: map of slot ranges to node names, e.g., {"0-8191": "autocache-0", "8192-16383": "autocache-1"}
	// +optional
	SlotPlan map[string]string `json:"slotPlan,omitempty"`
}

// StorageSpec defines storage configuration
type StorageSpec struct {
	// Enabled indicates whether persistent storage is enabled
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// StorageClassName is the storage class to use
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`

	// Size is the storage size
	// +kubebuilder:default="10Gi"
	Size resource.Quantity `json:"size,omitempty"`

	// PersistenceMode is the persistence mode
	// +kubebuilder:validation:Enum=RDB;AOF;Both
	// +kubebuilder:default="RDB"
	PersistenceMode string `json:"persistenceMode,omitempty"`
}

// CacheConfig defines cache configuration options
type CacheConfig struct {
	// MaxMemory is the maximum memory limit
	// +optional
	MaxMemory string `json:"maxMemory,omitempty"`

	// MaxMemoryPolicy is the eviction policy when maxmemory is reached
	// +kubebuilder:validation:Enum=noeviction;allkeys-lru;allkeys-lfu;volatile-lru;volatile-lfu;allkeys-random;volatile-ttl
	// +kubebuilder:default="noeviction"
	MaxMemoryPolicy string `json:"maxMemoryPolicy,omitempty"`

	// Databases is the number of databases
	// +kubebuilder:default=16
	Databases int32 `json:"databases,omitempty"`

	// Custom is a map of custom configuration options
	// +optional
	Custom map[string]string `json:"custom,omitempty"`
}

// AuthSpec defines authentication configuration
type AuthSpec struct {
	// Enabled indicates whether authentication is enabled
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// PasswordSecret is the name of the secret containing the password
	// +optional
	PasswordSecret string `json:"passwordSecret,omitempty"`

	// PasswordKey is the key in the secret containing the password
	// +kubebuilder:default="password"
	PasswordKey string `json:"passwordKey,omitempty"`
}

// TieredStorageSpec defines tiered storage configuration
type TieredStorageSpec struct {
	// Enabled indicates whether tiered storage is enabled
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// HotTier is the hot tier configuration
	// +optional
	HotTier *TierConfig `json:"hotTier,omitempty"`

	// WarmTier is the warm tier configuration
	// +optional
	WarmTier *TierConfig `json:"warmTier,omitempty"`

	// ColdTier is the cold tier configuration
	// +optional
	ColdTier *TierConfig `json:"coldTier,omitempty"`

	// MigrationPolicy defines the migration policy
	// +optional
	MigrationPolicy *MigrationPolicySpec `json:"migrationPolicy,omitempty"`
}

// TierConfig defines configuration for a storage tier
type TierConfig struct {
	// Type is the storage type
	// +kubebuilder:validation:Enum=Memory;SSD;S3
	Type string `json:"type"`

	// Capacity is the tier capacity
	// +optional
	Capacity resource.Quantity `json:"capacity,omitempty"`

	// ThresholdAccessCount is the access count threshold for tier migration
	// +optional
	ThresholdAccessCount int64 `json:"thresholdAccessCount,omitempty"`

	// ThresholdIdleTime is the idle time threshold for tier migration
	// +optional
	ThresholdIdleTime string `json:"thresholdIdleTime,omitempty"`
}

// MigrationPolicySpec defines the data migration policy
type MigrationPolicySpec struct {
	// ScanInterval is the interval between migration scans
	// +kubebuilder:default="1m"
	ScanInterval string `json:"scanInterval,omitempty"`

	// BatchSize is the number of keys to migrate per batch
	// +kubebuilder:default=100
	BatchSize int32 `json:"batchSize,omitempty"`

	// RateLimitMBps is the rate limit in MB/s
	// +optional
	RateLimitMBps int32 `json:"rateLimitMBps,omitempty"`
}

// ClusterPhase represents the phase of the cluster
type ClusterPhase string

const (
	ClusterPhasePending  ClusterPhase = "Pending"
	ClusterPhaseCreating ClusterPhase = "Creating"
	ClusterPhaseRunning  ClusterPhase = "Running"
	ClusterPhaseUpdating ClusterPhase = "Updating"
	ClusterPhaseScaling  ClusterPhase = "Scaling"
	ClusterPhaseFailed   ClusterPhase = "Failed"
)

// AutoCacheStatus defines the observed state of AutoCache
type AutoCacheStatus struct {
	// Phase is the current phase of the cluster
	// +kubebuilder:validation:Enum=Pending;Creating;Running;Updating;Scaling;Failed
	Phase ClusterPhase `json:"phase,omitempty"`

	// Replicas is the current number of replicas
	Replicas int32 `json:"replicas,omitempty"`

	// ReadyReplicas is the number of ready replicas
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// ClusterStatus is the cluster status string
	ClusterStatus string `json:"clusterStatus,omitempty"`

	// Nodes contains information about each node
	// +optional
	Nodes []NodeStatus `json:"nodes,omitempty"`

	// SlotsAssigned is the number of slots assigned
	SlotsAssigned int32 `json:"slotsAssigned,omitempty"`

	// Conditions represent the latest available observations
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastUpdateTime is the last time the status was updated
	// +optional
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`

	// ObservedGeneration is the generation observed by the controller
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Migration contains the current migration status
	// +optional
	Migration *MigrationStatus `json:"migration,omitempty"`
}

// MigrationPhase represents the phase of slot migration
type MigrationPhase string

const (
	MigrationPhaseNone       MigrationPhase = ""
	MigrationPhasePending    MigrationPhase = "Pending"
	MigrationPhaseInProgress MigrationPhase = "InProgress"
	MigrationPhaseCompleted  MigrationPhase = "Completed"
	MigrationPhaseFailed     MigrationPhase = "Failed"
)

// MigrationStatus represents the status of slot migration
type MigrationStatus struct {
	// Phase is the current migration phase
	// +kubebuilder:validation:Enum="";Pending;InProgress;Completed;Failed
	Phase MigrationPhase `json:"phase,omitempty"`

	// CurrentSlot is the slot currently being migrated
	// +optional
	CurrentSlot *int32 `json:"currentSlot,omitempty"`

	// SourceNode is the source node of the current migration
	// +optional
	SourceNode string `json:"sourceNode,omitempty"`

	// TargetNode is the target node of the current migration
	// +optional
	TargetNode string `json:"targetNode,omitempty"`

	// Progress is the migration progress (0-100)
	// +optional
	Progress int32 `json:"progress,omitempty"`

	// KeysMigrated is the number of keys migrated
	// +optional
	KeysMigrated int64 `json:"keysMigrated,omitempty"`

	// KeysFailed is the number of keys that failed to migrate
	// +optional
	KeysFailed int64 `json:"keysFailed,omitempty"`

	// StartTime is when the migration started
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the migration completed
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Message contains additional information about the migration
	// +optional
	Message string `json:"message,omitempty"`
}

// NodeStatus represents the status of a single node
type NodeStatus struct {
	// Name is the pod name
	Name string `json:"name"`

	// PodIP is the pod IP address
	// +optional
	PodIP string `json:"podIP,omitempty"`

	// NodeID is the cluster node ID
	// +optional
	NodeID string `json:"nodeID,omitempty"`

	// Role is the node role (master or replica)
	// +kubebuilder:validation:Enum=master;replica
	Role string `json:"role"`

	// Slots is the slots assigned to this node
	// +optional
	Slots string `json:"slots,omitempty"`

	// Ready indicates whether the node is ready
	Ready bool `json:"ready"`

	// Phase is the pod phase
	// +optional
	Phase corev1.PodPhase `json:"phase,omitempty"`
}

// Condition types
const (
	ConditionTypeReady       = "Ready"
	ConditionTypeProgressing = "Progressing"
	ConditionTypeDegraded    = "Degraded"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:shortName=ac
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=`.spec.replicas`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.status.clusterStatus`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// AutoCache is the Schema for the autocaches API
type AutoCache struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AutoCacheSpec   `json:"spec,omitempty"`
	Status AutoCacheStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AutoCacheList contains a list of AutoCache
type AutoCacheList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AutoCache `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AutoCache{}, &AutoCacheList{})
}

// IsReady returns true if the cluster is ready
func (ac *AutoCache) IsReady() bool {
	return ac.Status.Phase == ClusterPhaseRunning &&
		ac.Status.ReadyReplicas == ac.Spec.Replicas
}

// GetLabels returns the standard labels for the cluster
func (ac *AutoCache) GetLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "autocache",
		"app.kubernetes.io/instance":   ac.Name,
		"app.kubernetes.io/managed-by": "autocache-operator",
		"app.kubernetes.io/component":  "cache",
	}
}

// GetSelectorLabels returns the selector labels for the cluster
func (ac *AutoCache) GetSelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":     "autocache",
		"app.kubernetes.io/instance": ac.Name,
	}
}
