# Phase 3: K8s Operator 开发

## 目标

- 设计并实现 AutoCache CRD（自定义资源定义）
- 开发 Operator 控制器实现集群生命周期管理
- 支持声明式集群创建、扩缩容、滚动更新
- 实现 ConfigMap 和 Secret 自动管理
- 支持 Helm Chart 一键部署

## 时间：2026.03.10 - 2026.04.13（5 周）

---

## 架构设计

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         Kubernetes Cluster                               │
│                                                                          │
│  ┌────────────────────────────────────────────────────────────────┐    │
│  │                      AutoCache Operator                         │    │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐         │    │
│  │  │  Reconciler  │  │   Watchers   │  │   Webhooks   │         │    │
│  │  └──────────────┘  └──────────────┘  └──────────────┘         │    │
│  └────────────────────────────────────────────────────────────────┘    │
│                               │                                          │
│                               ▼                                          │
│  ┌────────────────────────────────────────────────────────────────┐    │
│  │                    Managed Resources                            │    │
│  │                                                                  │    │
│  │   ┌─────────────┐   ┌─────────────┐   ┌─────────────┐          │    │
│  │   │ StatefulSet │   │   Service   │   │  ConfigMap  │          │    │
│  │   │             │   │  (Headless) │   │             │          │    │
│  │   │ ┌─────────┐ │   └─────────────┘   └─────────────┘          │    │
│  │   │ │ Pod-0   │ │                                               │    │
│  │   │ │ Pod-1   │ │   ┌─────────────┐   ┌─────────────┐          │    │
│  │   │ │ Pod-2   │ │   │   Secret    │   │     PVC     │          │    │
│  │   │ └─────────┘ │   └─────────────┘   └─────────────┘          │    │
│  │   └─────────────┘                                               │    │
│  └────────────────────────────────────────────────────────────────┘    │
│                                                                          │
│  ┌────────────────────────────────────────────────────────────────┐    │
│  │                    AutoCache CRD                                │    │
│  │                                                                  │    │
│  │   apiVersion: cache.autocache.io/v1alpha1                      │    │
│  │   kind: AutoCache                                               │    │
│  │   metadata:                                                     │    │
│  │     name: my-cache                                              │    │
│  │   spec:                                                         │    │
│  │     replicas: 3                                                 │    │
│  │     image: autocache:latest                                     │    │
│  │     resources: ...                                              │    │
│  │                                                                  │    │
│  └────────────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Week 1: CRD 设计与类型定义

### 1.1 CRD 设计

#### api/v1alpha1/autocache_types.go
```go
package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AutoCacheSpec 定义 AutoCache 的期望状态
type AutoCacheSpec struct {
	// Replicas 集群节点数量
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	// +kubebuilder:default=3
	Replicas int32 `json:"replicas,omitempty"`

	// Image 容器镜像
	// +kubebuilder:default="autocache:latest"
	Image string `json:"image,omitempty"`

	// ImagePullPolicy 镜像拉取策略
	// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
	// +kubebuilder:default="IfNotPresent"
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// Resources 资源配置
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Port Redis 协议端口
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=6379
	Port int32 `json:"port,omitempty"`

	// ClusterPort 集群通信端口
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=16379
	ClusterPort int32 `json:"clusterPort,omitempty"`

	// Storage 存储配置
	Storage *StorageSpec `json:"storage,omitempty"`

	// Config 缓存配置
	Config *CacheConfig `json:"config,omitempty"`

	// Auth 认证配置
	Auth *AuthSpec `json:"auth,omitempty"`

	// Affinity Pod 亲和性配置
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Tolerations 容忍配置
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// NodeSelector 节点选择器
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// PodAnnotations Pod 注解
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`

	// ServiceAnnotations Service 注解
	ServiceAnnotations map[string]string `json:"serviceAnnotations,omitempty"`

	// TieredStorage 冷热分层配置
	TieredStorage *TieredStorageSpec `json:"tieredStorage,omitempty"`

	// Scheduling 调度增强配置
	Scheduling *SchedulingSpec `json:"scheduling,omitempty"`
}

// StorageSpec 存储配置
type StorageSpec struct {
	// Enabled 是否启用持久化
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// StorageClassName 存储类
	StorageClassName *string `json:"storageClassName,omitempty"`

	// Size 存储大小
	// +kubebuilder:default="10Gi"
	Size resource.Quantity `json:"size,omitempty"`

	// PersistenceMode 持久化模式
	// +kubebuilder:validation:Enum=RDB;AOF;Both
	// +kubebuilder:default="RDB"
	PersistenceMode string `json:"persistenceMode,omitempty"`
}

// CacheConfig 缓存配置
type CacheConfig struct {
	// MaxMemory 最大内存
	MaxMemory string `json:"maxMemory,omitempty"`

	// MaxMemoryPolicy 内存淘汰策略
	// +kubebuilder:validation:Enum=noeviction;allkeys-lru;allkeys-lfu;volatile-lru;volatile-lfu;allkeys-random;volatile-ttl
	// +kubebuilder:default="noeviction"
	MaxMemoryPolicy string `json:"maxMemoryPolicy,omitempty"`

	// Databases 数据库数量
	// +kubebuilder:default=16
	Databases int32 `json:"databases,omitempty"`

	// Custom 自定义配置
	Custom map[string]string `json:"custom,omitempty"`
}

// AuthSpec 认证配置
type AuthSpec struct {
	// Enabled 是否启用认证
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// PasswordSecret 密码 Secret 名称
	PasswordSecret string `json:"passwordSecret,omitempty"`

	// PasswordKey Secret 中的 key
	// +kubebuilder:default="password"
	PasswordKey string `json:"passwordKey,omitempty"`
}

// TieredStorageSpec 冷热分层配置
type TieredStorageSpec struct {
	// Enabled 是否启用冷热分层
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// HotTier 热数据层配置
	HotTier *TierConfig `json:"hotTier,omitempty"`

	// WarmTier 温数据层配置
	WarmTier *TierConfig `json:"warmTier,omitempty"`

	// ColdTier 冷数据层配置
	ColdTier *TierConfig `json:"coldTier,omitempty"`

	// MigrationPolicy 迁移策略
	MigrationPolicy *MigrationPolicySpec `json:"migrationPolicy,omitempty"`
}

// TierConfig 存储层配置
type TierConfig struct {
	// Type 存储类型
	// +kubebuilder:validation:Enum=Memory;SSD;S3
	Type string `json:"type"`

	// Capacity 容量
	Capacity resource.Quantity `json:"capacity,omitempty"`

	// ThresholdAccessCount 访问次数阈值
	ThresholdAccessCount int64 `json:"thresholdAccessCount,omitempty"`

	// ThresholdIdleTime 空闲时间阈值
	ThresholdIdleTime string `json:"thresholdIdleTime,omitempty"`
}

// MigrationPolicySpec 数据迁移策略
type MigrationPolicySpec struct {
	// ScanInterval 扫描间隔
	// +kubebuilder:default="1m"
	ScanInterval string `json:"scanInterval,omitempty"`

	// BatchSize 每批迁移的 key 数量
	// +kubebuilder:default=100
	BatchSize int32 `json:"batchSize,omitempty"`

	// RateLimitMBps 迁移速率限制 (MB/s)
	RateLimitMBps int32 `json:"rateLimitMBps,omitempty"`
}

// SchedulingSpec 调度增强配置
type SchedulingSpec struct {
	// TopologyAware 拓扑感知调度
	TopologyAware *TopologyAwareSpec `json:"topologyAware,omitempty"`

	// LoadAware 负载感知调度
	LoadAware *LoadAwareSpec `json:"loadAware,omitempty"`
}

// TopologyAwareSpec 拓扑感知配置
type TopologyAwareSpec struct {
	// Enabled 是否启用
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// TopologyKey 拓扑键
	// +kubebuilder:default="topology.kubernetes.io/zone"
	TopologyKey string `json:"topologyKey,omitempty"`

	// MaxSkew 最大偏差
	// +kubebuilder:default=1
	MaxSkew int32 `json:"maxSkew,omitempty"`
}

// LoadAwareSpec 负载感知配置
type LoadAwareSpec struct {
	// Enabled 是否启用
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// CPUThreshold CPU 阈值百分比
	// +kubebuilder:default=80
	CPUThreshold int32 `json:"cpuThreshold,omitempty"`

	// MemoryThreshold 内存阈值百分比
	// +kubebuilder:default=80
	MemoryThreshold int32 `json:"memoryThreshold,omitempty"`
}

// AutoCacheStatus 定义 AutoCache 的观测状态
type AutoCacheStatus struct {
	// Phase 集群阶段
	// +kubebuilder:validation:Enum=Pending;Creating;Running;Updating;Scaling;Failed
	Phase ClusterPhase `json:"phase,omitempty"`

	// Replicas 当前副本数
	Replicas int32 `json:"replicas,omitempty"`

	// ReadyReplicas 就绪副本数
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// ClusterStatus 集群状态
	ClusterStatus string `json:"clusterStatus,omitempty"`

	// Nodes 节点信息
	Nodes []NodeStatus `json:"nodes,omitempty"`

	// Slots Slot 分配信息
	SlotsAssigned int32 `json:"slotsAssigned,omitempty"`

	// Conditions 状态条件
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastUpdateTime 最后更新时间
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`

	// ObservedGeneration 观测到的 generation
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// ClusterPhase 集群阶段
type ClusterPhase string

const (
	ClusterPhasePending  ClusterPhase = "Pending"
	ClusterPhaseCreating ClusterPhase = "Creating"
	ClusterPhaseRunning  ClusterPhase = "Running"
	ClusterPhaseUpdating ClusterPhase = "Updating"
	ClusterPhaseScaling  ClusterPhase = "Scaling"
	ClusterPhaseFailed   ClusterPhase = "Failed"
)

// NodeStatus 节点状态
type NodeStatus struct {
	// Name Pod 名称
	Name string `json:"name"`

	// PodIP Pod IP
	PodIP string `json:"podIP,omitempty"`

	// NodeID 集群节点 ID
	NodeID string `json:"nodeID,omitempty"`

	// Role 角色
	// +kubebuilder:validation:Enum=master;replica
	Role string `json:"role"`

	// Slots 分配的 Slots
	Slots string `json:"slots,omitempty"`

	// Ready 是否就绪
	Ready bool `json:"ready"`

	// Phase Pod 阶段
	Phase corev1.PodPhase `json:"phase,omitempty"`
}

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

// Condition Types
const (
	ConditionTypeReady       = "Ready"
	ConditionTypeProgressing = "Progressing"
	ConditionTypeDegraded    = "Degraded"
)

// IsReady 检查集群是否就绪
func (ac *AutoCache) IsReady() bool {
	return ac.Status.Phase == ClusterPhaseRunning &&
		ac.Status.ReadyReplicas == ac.Spec.Replicas
}

// GetLabels 获取标签
func (ac *AutoCache) GetLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "autocache",
		"app.kubernetes.io/instance":   ac.Name,
		"app.kubernetes.io/managed-by": "autocache-operator",
		"app.kubernetes.io/component":  "cache",
	}
}

// GetSelectorLabels 获取选择器标签
func (ac *AutoCache) GetSelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":     "autocache",
		"app.kubernetes.io/instance": ac.Name,
	}
}
```

### 1.2 生成 CRD 和 DeepCopy

```bash
# 安装 controller-gen
go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest

# 生成 DeepCopy 方法
controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

# 生成 CRD YAML
controller-gen crd:crdVersions=v1 paths="./api/..." output:crd:artifacts:config=config/crd/bases

# 生成 RBAC
controller-gen rbac:roleName=autocache-operator paths="./controllers/..." output:rbac:dir=config/rbac
```

### 1.3 CRD YAML 示例

#### config/crd/bases/cache.autocache.io_autocaches.yaml
```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: autocaches.cache.autocache.io
  annotations:
    controller-gen.kubebuilder.io/version: v0.14.0
spec:
  group: cache.autocache.io
  names:
    kind: AutoCache
    listKind: AutoCacheList
    plural: autocaches
    singular: autocache
    shortNames:
      - ac
  scope: Namespaced
  versions:
    - name: v1alpha1
      served: true
      storage: true
      subresources:
        status: {}
        scale:
          specReplicasPath: .spec.replicas
          statusReplicasPath: .status.replicas
      additionalPrinterColumns:
        - name: Replicas
          type: integer
          jsonPath: .spec.replicas
        - name: Ready
          type: integer
          jsonPath: .status.readyReplicas
        - name: Phase
          type: string
          jsonPath: .status.phase
        - name: Cluster
          type: string
          jsonPath: .status.clusterStatus
        - name: Age
          type: date
          jsonPath: .metadata.creationTimestamp
      schema:
        openAPIV3Schema:
          type: object
          properties:
            apiVersion:
              type: string
            kind:
              type: string
            metadata:
              type: object
            spec:
              type: object
              properties:
                replicas:
                  type: integer
                  minimum: 1
                  maximum: 1000
                  default: 3
                image:
                  type: string
                  default: "autocache:latest"
                port:
                  type: integer
                  minimum: 1
                  maximum: 65535
                  default: 6379
                resources:
                  type: object
                  x-kubernetes-preserve-unknown-fields: true
                storage:
                  type: object
                  properties:
                    enabled:
                      type: boolean
                      default: false
                    storageClassName:
                      type: string
                    size:
                      type: string
                      default: "10Gi"
                config:
                  type: object
                  properties:
                    maxMemory:
                      type: string
                    maxMemoryPolicy:
                      type: string
                      enum: [noeviction, allkeys-lru, allkeys-lfu, volatile-lru, volatile-lfu]
                      default: noeviction
                tieredStorage:
                  type: object
                  properties:
                    enabled:
                      type: boolean
                      default: false
            status:
              type: object
              properties:
                phase:
                  type: string
                  enum: [Pending, Creating, Running, Updating, Scaling, Failed]
                replicas:
                  type: integer
                readyReplicas:
                  type: integer
                clusterStatus:
                  type: string
                nodes:
                  type: array
                  items:
                    type: object
                    properties:
                      name:
                        type: string
                      podIP:
                        type: string
                      role:
                        type: string
                      ready:
                        type: boolean
                conditions:
                  type: array
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                      reason:
                        type: string
                      message:
                        type: string
                      lastTransitionTime:
                        type: string
                        format: date-time
```

---

## Week 2: Reconciler 核心逻辑

### 2.1 主控制器

#### controllers/autocache_controller.go
```go
package controllers

import (
	"context"
	"fmt"
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
	
	// 重新入队间隔
	requeueAfterSuccess = 30 * time.Second
	requeueAfterError   = 5 * time.Second
)

// AutoCacheReconciler reconciles a AutoCache object
type AutoCacheReconciler struct {
	client.Client
	Scheme *runtime.Scheme
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

// Reconcile 主调和循环
func (r *AutoCacheReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling AutoCache", "namespace", req.Namespace, "name", req.Name)

	// 获取 AutoCache 资源
	ac := &cachev1alpha1.AutoCache{}
	if err := r.Get(ctx, req.NamespacedName, ac); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("AutoCache not found, possibly deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get AutoCache")
		return ctrl.Result{}, err
	}

	// 处理删除
	if !ac.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, ac)
	}

	// 添加 Finalizer
	if !controllerutil.ContainsFinalizer(ac, finalizerName) {
		controllerutil.AddFinalizer(ac, finalizerName)
		if err := r.Update(ctx, ac); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 初始化状态
	if ac.Status.Phase == "" {
		ac.Status.Phase = cachev1alpha1.ClusterPhasePending
		if err := r.Status().Update(ctx, ac); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 调和 ConfigMap
	if err := r.reconcileConfigMap(ctx, ac); err != nil {
		logger.Error(err, "Failed to reconcile ConfigMap")
		return ctrl.Result{RequeueAfter: requeueAfterError}, err
	}

	// 调和 Headless Service
	if err := r.reconcileHeadlessService(ctx, ac); err != nil {
		logger.Error(err, "Failed to reconcile Headless Service")
		return ctrl.Result{RequeueAfter: requeueAfterError}, err
	}

	// 调和 Client Service
	if err := r.reconcileClientService(ctx, ac); err != nil {
		logger.Error(err, "Failed to reconcile Client Service")
		return ctrl.Result{RequeueAfter: requeueAfterError}, err
	}

	// 调和 StatefulSet
	if err := r.reconcileStatefulSet(ctx, ac); err != nil {
		logger.Error(err, "Failed to reconcile StatefulSet")
		return ctrl.Result{RequeueAfter: requeueAfterError}, err
	}

	// 更新状态
	if err := r.updateStatus(ctx, ac); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{RequeueAfter: requeueAfterError}, err
	}

	// 检查是否需要初始化集群
	if ac.Status.Phase == cachev1alpha1.ClusterPhaseRunning && ac.Status.SlotsAssigned == 0 {
		if err := r.initializeCluster(ctx, ac); err != nil {
			logger.Error(err, "Failed to initialize cluster")
			return ctrl.Result{RequeueAfter: requeueAfterError}, err
		}
	}

	logger.Info("Reconcile completed", "phase", ac.Status.Phase, "ready", ac.Status.ReadyReplicas)
	return ctrl.Result{RequeueAfter: requeueAfterSuccess}, nil
}

// handleDeletion 处理删除
func (r *AutoCacheReconciler) handleDeletion(ctx context.Context, ac *cachev1alpha1.AutoCache) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Handling deletion")

	// 执行清理逻辑
	// TODO: 清理外部资源

	// 移除 Finalizer
	controllerutil.RemoveFinalizer(ac, finalizerName)
	if err := r.Update(ctx, ac); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcileConfigMap 调和 ConfigMap
func (r *AutoCacheReconciler) reconcileConfigMap(ctx context.Context, ac *cachev1alpha1.AutoCache) error {
	cm := r.buildConfigMap(ac)
	
	// 设置 OwnerReference
	if err := controllerutil.SetControllerReference(ac, cm, r.Scheme); err != nil {
		return err
	}

	// 创建或更新
	existingCM := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, existingCM)
	if err != nil {
		if errors.IsNotFound(err) {
			return r.Create(ctx, cm)
		}
		return err
	}

	// 更新
	existingCM.Data = cm.Data
	return r.Update(ctx, existingCM)
}

// buildConfigMap 构建 ConfigMap
func (r *AutoCacheReconciler) buildConfigMap(ac *cachev1alpha1.AutoCache) *corev1.ConfigMap {
	config := ac.Spec.Config
	
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

// generateConfig 生成配置文件内容
func (r *AutoCacheReconciler) generateConfig(ac *cachev1alpha1.AutoCache) string {
	config := ac.Spec.Config
	
	content := fmt.Sprintf(`# AutoCache Configuration
port %d
cluster-port %d
cluster-enabled yes
cluster-config-file nodes.conf
cluster-node-timeout 15000
`, ac.Spec.Port, ac.Spec.ClusterPort)

	if config != nil {
		if config.MaxMemory != "" {
			content += fmt.Sprintf("maxmemory %s\n", config.MaxMemory)
		}
		if config.MaxMemoryPolicy != "" {
			content += fmt.Sprintf("maxmemory-policy %s\n", config.MaxMemoryPolicy)
		}
		for k, v := range config.Custom {
			content += fmt.Sprintf("%s %s\n", k, v)
		}
	}

	return content
}

// reconcileHeadlessService 调和 Headless Service
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

// buildHeadlessService 构建 Headless Service
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

// reconcileClientService 调和 Client Service
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

// buildClientService 构建 Client Service
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

// SetupWithManager 设置控制器
func (r *AutoCacheReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cachev1alpha1.AutoCache{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}
```

### 2.2 StatefulSet 管理

#### controllers/statefulset.go
```go
package controllers

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	cachev1alpha1 "github.com/10yihang/autocache/api/v1alpha1"
)

// reconcileStatefulSet 调和 StatefulSet
func (r *AutoCacheReconciler) reconcileStatefulSet(ctx context.Context, ac *cachev1alpha1.AutoCache) error {
	sts := r.buildStatefulSet(ac)
	
	if err := controllerutil.SetControllerReference(ac, sts, r.Scheme); err != nil {
		return err
	}

	existingSts := &appsv1.StatefulSet{}
	err := r.Get(ctx, types.NamespacedName{Name: sts.Name, Namespace: sts.Namespace}, existingSts)
	if err != nil {
		if errors.IsNotFound(err) {
			// 创建 StatefulSet
			if err := r.Create(ctx, sts); err != nil {
				return err
			}
			// 更新状态为 Creating
			ac.Status.Phase = cachev1alpha1.ClusterPhaseCreating
			return r.Status().Update(ctx, ac)
		}
		return err
	}

	// 检查是否需要更新
	needUpdate := false
	
	// 副本数变更
	if *existingSts.Spec.Replicas != ac.Spec.Replicas {
		existingSts.Spec.Replicas = &ac.Spec.Replicas
		needUpdate = true
		ac.Status.Phase = cachev1alpha1.ClusterPhaseScaling
	}
	
	// 镜像变更
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

// buildStatefulSet 构建 StatefulSet
func (r *AutoCacheReconciler) buildStatefulSet(ac *cachev1alpha1.AutoCache) *appsv1.StatefulSet {
	labels := ac.GetLabels()
	selectorLabels := ac.GetSelectorLabels()
	
	// 合并 Pod 注解
	podAnnotations := map[string]string{
		"prometheus.io/scrape": "true",
		"prometheus.io/port":   "9121",
	}
	for k, v := range ac.Spec.PodAnnotations {
		podAnnotations[k] = v
	}

	// 构建容器
	container := r.buildContainer(ac)
	
	// 构建 Init Container（用于初始化配置）
	initContainer := r.buildInitContainer(ac)
	
	// 构建卷
	volumes := r.buildVolumes(ac)
	
	// 构建 VolumeClaimTemplates
	var volumeClaimTemplates []corev1.PersistentVolumeClaim
	if ac.Spec.Storage != nil && ac.Spec.Storage.Enabled {
		volumeClaimTemplates = r.buildVolumeClaimTemplates(ac)
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ac.Name,
			Namespace: ac.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &ac.Spec.Replicas,
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
					ServiceAccountName: ac.Name,
					InitContainers:     []corev1.Container{initContainer},
					Containers:         []corev1.Container{container},
					Volumes:            volumes,
					Affinity:           r.buildAffinity(ac),
					Tolerations:        ac.Spec.Tolerations,
					NodeSelector:       ac.Spec.NodeSelector,
				},
			},
			VolumeClaimTemplates: volumeClaimTemplates,
		},
	}

	return sts
}

// buildContainer 构建主容器
func (r *AutoCacheReconciler) buildContainer(ac *cachev1alpha1.AutoCache) corev1.Container {
	// 环境变量
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
			Name:  "CLUSTER_ANNOUNCE_IP",
			Value: "$(POD_IP)",
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

	// 端口
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

	// 卷挂载
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

	// 健康检查
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
			Exec: &corev1.ExecAction{
				Command: []string{
					"sh", "-c",
					fmt.Sprintf("redis-cli -p %d ping | grep -q PONG", ac.Spec.Port),
				},
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
			"--config", "/etc/autocache/autocache.conf",
			"--cluster-announce-ip", "$(POD_IP)",
		},
	}
}

// buildInitContainer 构建 Init 容器
func (r *AutoCacheReconciler) buildInitContainer(ac *cachev1alpha1.AutoCache) corev1.Container {
	return corev1.Container{
		Name:  "init-config",
		Image: "busybox:1.35",
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

// buildVolumes 构建卷
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

// buildVolumeClaimTemplates 构建 PVC 模板
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
			Resources: corev1.ResourceRequirements{
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

// buildAffinity 构建亲和性配置
func (r *AutoCacheReconciler) buildAffinity(ac *cachev1alpha1.AutoCache) *corev1.Affinity {
	// 如果用户自定义了亲和性，使用用户的配置
	if ac.Spec.Affinity != nil {
		return ac.Spec.Affinity
	}

	// 默认：Pod 反亲和性，分散到不同节点
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

// updateStatus 更新状态
func (r *AutoCacheReconciler) updateStatus(ctx context.Context, ac *cachev1alpha1.AutoCache) error {
	// 获取 StatefulSet
	sts := &appsv1.StatefulSet{}
	err := r.Get(ctx, types.NamespacedName{Name: ac.Name, Namespace: ac.Namespace}, sts)
	if err != nil {
		return err
	}

	// 获取 Pods
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, 
		client.InNamespace(ac.Namespace),
		client.MatchingLabels(ac.GetSelectorLabels())); err != nil {
		return err
	}

	// 更新副本数
	ac.Status.Replicas = sts.Status.Replicas
	ac.Status.ReadyReplicas = sts.Status.ReadyReplicas

	// 更新节点信息
	ac.Status.Nodes = make([]cachev1alpha1.NodeStatus, 0, len(pods.Items))
	for _, pod := range pods.Items {
		node := cachev1alpha1.NodeStatus{
			Name:   pod.Name,
			PodIP:  pod.Status.PodIP,
			Ready:  isPodReady(&pod),
			Phase:  pod.Status.Phase,
			Role:   "master", // TODO: 从集群获取实际角色
		}
		ac.Status.Nodes = append(ac.Status.Nodes, node)
	}

	// 更新阶段
	if ac.Status.ReadyReplicas == ac.Spec.Replicas && ac.Status.Replicas == ac.Spec.Replicas {
		ac.Status.Phase = cachev1alpha1.ClusterPhaseRunning
		ac.Status.ClusterStatus = "ok"
	} else if ac.Status.Phase == cachev1alpha1.ClusterPhaseCreating && ac.Status.ReadyReplicas > 0 {
		// 部分就绪
	}

	// 更新时间
	now := metav1.Now()
	ac.Status.LastUpdateTime = &now
	ac.Status.ObservedGeneration = ac.Generation

	return r.Status().Update(ctx, ac)
}

// isPodReady 检查 Pod 是否就绪
func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// initializeCluster 初始化集群
func (r *AutoCacheReconciler) initializeCluster(ctx context.Context, ac *cachev1alpha1.AutoCache) error {
	// 获取所有 Ready 的 Pod
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
		return nil // 等待更多 Pod 就绪
	}

	// TODO: 调用第一个 Pod 执行集群初始化命令
	// 分配 slots 等

	return nil
}
```

---

## Week 3-4: 扩缩容与滚动更新

### 3.1 扩缩容处理

#### controllers/scaling.go
```go
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

// ScaleManager 扩缩容管理器
type ScaleManager struct {
	client client.Client
}

// NewScaleManager 创建扩缩容管理器
func NewScaleManager(c client.Client) *ScaleManager {
	return &ScaleManager{client: c}
}

// HandleScaleUp 处理扩容
func (s *ScaleManager) HandleScaleUp(ctx context.Context, ac *cachev1alpha1.AutoCache, newReplicas, oldReplicas int32) error {
	logger := log.FromContext(ctx)
	logger.Info("Handling scale up", "from", oldReplicas, "to", newReplicas)

	// 等待新 Pod 就绪
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

	// 让新节点加入集群
	if err := s.addNodesToCluster(ctx, ac, oldReplicas, newReplicas); err != nil {
		return err
	}

	// 重新分配 slots
	if err := s.rebalanceSlots(ctx, ac); err != nil {
		return err
	}

	return nil
}

// HandleScaleDown 处理缩容
func (s *ScaleManager) HandleScaleDown(ctx context.Context, ac *cachev1alpha1.AutoCache, newReplicas, oldReplicas int32) error {
	logger := log.FromContext(ctx)
	logger.Info("Handling scale down", "from", oldReplicas, "to", newReplicas)

	// 迁移待删除节点的数据
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

// addNodesToCluster 添加节点到集群
func (s *ScaleManager) addNodesToCluster(ctx context.Context, ac *cachev1alpha1.AutoCache, start, end int32) error {
	logger := log.FromContext(ctx)
	
	// 获取第一个节点作为种子
	firstPod := &corev1.Pod{}
	if err := s.client.Get(ctx, client.ObjectKey{
		Namespace: ac.Namespace,
		Name:      fmt.Sprintf("%s-0", ac.Name),
	}, firstPod); err != nil {
		return err
	}

	seedAddr := fmt.Sprintf("%s:%d", firstPod.Status.PodIP, ac.Spec.ClusterPort)

	// 让新节点 MEET 种子节点
	for i := start; i < end; i++ {
		podName := fmt.Sprintf("%s-%d", ac.Name, i)
		pod := &corev1.Pod{}
		if err := s.client.Get(ctx, client.ObjectKey{
			Namespace: ac.Namespace,
			Name:      podName,
		}, pod); err != nil {
			return err
		}

		// TODO: 执行 CLUSTER MEET 命令
		logger.Info("Adding node to cluster", "pod", podName, "seed", seedAddr)
	}

	return nil
}

// migrateDataFromNode 从节点迁移数据
func (s *ScaleManager) migrateDataFromNode(ctx context.Context, ac *cachev1alpha1.AutoCache, podName string) error {
	logger := log.FromContext(ctx)
	logger.Info("Migrating data from node", "pod", podName)
	
	// TODO: 执行数据迁移
	// 1. 获取节点的 slots
	// 2. 将 slots 迁移到其他节点
	
	return nil
}

// removeNodeFromCluster 从集群移除节点
func (s *ScaleManager) removeNodeFromCluster(ctx context.Context, ac *cachev1alpha1.AutoCache, podName string) error {
	logger := log.FromContext(ctx)
	logger.Info("Removing node from cluster", "pod", podName)
	
	// TODO: 执行 CLUSTER FORGET 命令
	
	return nil
}

// rebalanceSlots 重新均衡 slots
func (s *ScaleManager) rebalanceSlots(ctx context.Context, ac *cachev1alpha1.AutoCache) error {
	logger := log.FromContext(ctx)
	logger.Info("Rebalancing slots")
	
	// TODO: 计算每个节点应该持有的 slots 数量
	// 并执行迁移
	
	return nil
}
```

---

## Week 5: Helm Chart 与部署

### 5.1 Helm Chart 结构

```
deploy/helm/autocache/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── _helpers.tpl
│   ├── crd.yaml
│   ├── operator-deployment.yaml
│   ├── operator-rbac.yaml
│   ├── operator-service.yaml
│   └── autocache-sample.yaml
└── crds/
    └── cache.autocache.io_autocaches.yaml
```

### 5.2 Chart.yaml

```yaml
apiVersion: v2
name: autocache
description: A Helm chart for AutoCache - Cloud Native Distributed Cache System
type: application
version: 0.1.0
appVersion: "0.1.0"
keywords:
  - cache
  - redis
  - kubernetes
  - operator
maintainers:
  - name: Your Name
    email: your.email@example.com
```

### 5.3 values.yaml

```yaml
# Operator 配置
operator:
  image:
    repository: autocache-operator
    tag: latest
    pullPolicy: IfNotPresent
  
  replicas: 1
  
  resources:
    limits:
      cpu: 500m
      memory: 256Mi
    requests:
      cpu: 100m
      memory: 128Mi
  
  # 日志级别
  logLevel: info
  
  # Webhook 配置
  webhook:
    enabled: false
    port: 9443

# 默认 AutoCache 集群配置
autocache:
  # 是否创建示例集群
  createSample: false
  
  # 集群名称
  name: autocache-sample
  
  # 副本数
  replicas: 3
  
  # 镜像
  image:
    repository: autocache
    tag: latest
    pullPolicy: IfNotPresent
  
  # 资源配置
  resources:
    limits:
      cpu: 1000m
      memory: 1Gi
    requests:
      cpu: 100m
      memory: 256Mi
  
  # 端口
  port: 6379
  clusterPort: 16379
  
  # 存储
  storage:
    enabled: false
    storageClassName: ""
    size: 10Gi
  
  # 缓存配置
  config:
    maxMemory: "512mb"
    maxMemoryPolicy: "allkeys-lru"
  
  # 冷热分层
  tieredStorage:
    enabled: false
    hotTier:
      type: Memory
      capacity: 256Mi
    warmTier:
      type: SSD
      capacity: 1Gi

# ServiceAccount
serviceAccount:
  create: true
  name: ""
  annotations: {}

# RBAC
rbac:
  create: true

# Prometheus 监控
monitoring:
  enabled: true
  serviceMonitor:
    enabled: false
    interval: 30s
    labels: {}
```

### 5.4 Operator Deployment 模板

#### templates/operator-deployment.yaml
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "autocache.fullname" . }}-operator
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "autocache.labels" . | nindent 4 }}
    app.kubernetes.io/component: operator
spec:
  replicas: {{ .Values.operator.replicas }}
  selector:
    matchLabels:
      {{- include "autocache.selectorLabels" . | nindent 6 }}
      app.kubernetes.io/component: operator
  template:
    metadata:
      labels:
        {{- include "autocache.labels" . | nindent 8 }}
        app.kubernetes.io/component: operator
    spec:
      serviceAccountName: {{ include "autocache.serviceAccountName" . }}
      containers:
        - name: operator
          image: "{{ .Values.operator.image.repository }}:{{ .Values.operator.image.tag }}"
          imagePullPolicy: {{ .Values.operator.image.pullPolicy }}
          args:
            - --leader-elect
            - --health-probe-bind-address=:8081
            - --metrics-bind-address=:8080
          ports:
            - name: metrics
              containerPort: 8080
              protocol: TCP
            - name: health
              containerPort: 8081
              protocol: TCP
          livenessProbe:
            httpGet:
              path: /healthz
              port: health
            initialDelaySeconds: 15
            periodSeconds: 20
          readinessProbe:
            httpGet:
              path: /readyz
              port: health
            initialDelaySeconds: 5
            periodSeconds: 10
          resources:
            {{- toYaml .Values.operator.resources | nindent 12 }}
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
      securityContext:
        runAsNonRoot: true
```

### 5.5 部署命令

```bash
# 安装 CRD
kubectl apply -f config/crd/bases/

# 使用 Helm 安装 Operator
helm install autocache-operator deploy/helm/autocache \
  --namespace autocache-system \
  --create-namespace

# 创建 AutoCache 集群
cat <<EOF | kubectl apply -f -
apiVersion: cache.autocache.io/v1alpha1
kind: AutoCache
metadata:
  name: my-cache
  namespace: default
spec:
  replicas: 3
  image: autocache:latest
  port: 6379
  resources:
    limits:
      memory: 1Gi
      cpu: 1000m
    requests:
      memory: 256Mi
      cpu: 100m
EOF

# 查看集群状态
kubectl get autocache
kubectl describe autocache my-cache
```

---

## 测试验证

### 集成测试

```bash
# 创建 Kind 集群
kind create cluster --name autocache-test

# 加载镜像
kind load docker-image autocache:latest --name autocache-test
kind load docker-image autocache-operator:latest --name autocache-test

# 安装 CRD 和 Operator
make install
make deploy

# 创建测试集群
kubectl apply -f config/samples/cache_v1alpha1_autocache.yaml

# 等待就绪
kubectl wait --for=condition=Ready autocache/autocache-sample --timeout=300s

# 验证集群
kubectl get pods
kubectl logs -l app.kubernetes.io/name=autocache

# 测试连接
kubectl port-forward svc/autocache-sample 6379:6379 &
redis-cli -p 6379 PING
redis-cli -p 6379 CLUSTER INFO
```

---

## 交付物

- [x] AutoCache CRD 定义
- [x] Operator 控制器
- [x] StatefulSet 管理
- [x] Service 管理（Headless + Client）
- [x] ConfigMap 管理
- [x] 扩缩容逻辑
- [x] Helm Chart
- [x] RBAC 配置

## 验收标准

```bash
# 一键部署
helm install autocache deploy/helm/autocache

# 创建集群
kubectl apply -f - <<EOF
apiVersion: cache.autocache.io/v1alpha1
kind: AutoCache
metadata:
  name: test-cache
spec:
  replicas: 3
EOF

# 验证
kubectl get ac
# NAME         REPLICAS   READY   PHASE     CLUSTER   AGE
# test-cache   3          3       Running   ok        5m

# 扩容
kubectl scale ac test-cache --replicas=5

# 缩容
kubectl scale ac test-cache --replicas=3
```

## 下一步

完成 Phase 3 后，进入 [Phase 4: 冷热分层 + 调度增强](./05-phase4-optimization.md)
