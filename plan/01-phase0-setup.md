# Phase 0: 环境搭建与技术验证

## 目标

- 搭建完整的本地开发环境
- 验证核心技术栈可行性
- 完成项目初始化和基础框架搭建
- 跑通 Hello World Operator

## 时间：2025.12.22 - 2026.01.05（2 周）

---

## 任务清单

### 0.1 开发环境安装

#### Go 环境
```bash
# macOS
brew install go

# 验证
go version  # 应 >= 1.21

# 设置 Go 模块代理（国内加速）
go env -w GOPROXY=https://goproxy.cn,direct
```

#### Docker
```bash
# macOS
brew install --cask docker

# 启动 Docker Desktop
open /Applications/Docker.app

# 验证
docker version
```

#### Kind（Kubernetes in Docker）
```bash
# 安装 Kind
brew install kind

# 创建集群
kind create cluster --name autocache-dev

# 验证
kubectl cluster-info --context kind-autocache-dev
```

#### kubectl
```bash
brew install kubectl
kubectl version --client
```

#### Kubebuilder
```bash
# 安装 kubebuilder
brew install kubebuilder

# 验证
kubebuilder version
```

#### 其他工具
```bash
# 代码格式化
brew install golangci-lint

# Protocol Buffers（用于 gRPC）
brew install protobuf
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Redis CLI（用于测试）
brew install redis
```

### 0.2 项目初始化

#### 创建项目
```bash
mkdir -p ~/Code/autocache
cd ~/Code/autocache

# 初始化 Go 模块
go mod init github.com/10yihang/autocache

# 创建目录结构
mkdir -p cmd/{server,operator}
mkdir -p internal/{config,engine,protocol,cluster,persistence,metrics}
mkdir -p internal/engine/{memory,badger,tiered}
mkdir -p internal/protocol/commands
mkdir -p internal/cluster/{gossip,hash,migrate,replication}
mkdir -p internal/persistence/{rdb,aof}
mkdir -p pkg/{log,utils,errors}
mkdir -p api/v1alpha1
mkdir -p controllers/scheduler
mkdir -p config/{crd,rbac,manager,samples}
mkdir -p deploy/{docker,helm}
mkdir -p test/{unit,integration,e2e,benchmark}
mkdir -p docs/{design,api,user-guide}
mkdir -p scripts
```

#### Makefile
```makefile
# Makefile

.PHONY: all build test clean docker run

# 变量
BINARY_NAME=autocache
DOCKER_IMAGE=autocache:latest
GO=go
GOFLAGS=-ldflags="-s -w"

# 默认目标
all: build

# 构建
build:
	$(GO) build $(GOFLAGS) -o bin/$(BINARY_NAME) ./cmd/server

build-operator:
	$(GO) build $(GOFLAGS) -o bin/$(BINARY_NAME)-operator ./cmd/operator

# 运行
run:
	$(GO) run ./cmd/server

# 测试
test:
	$(GO) test -v -race -cover ./...

test-unit:
	$(GO) test -v -race -cover ./internal/...

test-integration:
	$(GO) test -v -tags=integration ./test/integration/...

test-benchmark:
	$(GO) test -bench=. -benchmem ./...

# 代码质量
lint:
	golangci-lint run ./...

fmt:
	$(GO) fmt ./...
	goimports -w .

vet:
	$(GO) vet ./...

# Docker
docker-build:
	docker build -t $(DOCKER_IMAGE) -f deploy/docker/Dockerfile .

docker-run:
	docker run -p 6379:6379 $(DOCKER_IMAGE)

# Kind
kind-create:
	kind create cluster --name autocache-dev --config scripts/kind-config.yaml

kind-delete:
	kind delete cluster --name autocache-dev

kind-load:
	kind load docker-image $(DOCKER_IMAGE) --name autocache-dev

# Operator
generate:
	controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."

manifests:
	controller-gen crd:trivialVersions=true rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

install-crd:
	kubectl apply -f config/crd/bases

# 清理
clean:
	rm -rf bin/
	$(GO) clean

# Redis benchmark 测试
redis-benchmark:
	redis-benchmark -h 127.0.0.1 -p 6379 -n 100000 -c 50 -t get,set

# 帮助
help:
	@echo "Available targets:"
	@echo "  build           - Build the server binary"
	@echo "  build-operator  - Build the operator binary"
	@echo "  run             - Run the server"
	@echo "  test            - Run all tests"
	@echo "  lint            - Run linter"
	@echo "  docker-build    - Build Docker image"
	@echo "  kind-create     - Create Kind cluster"
	@echo "  redis-benchmark - Run redis-benchmark"
```

#### 初始化依赖
```bash
# 添加核心依赖
go get github.com/tidwall/redcon           # Redis 协议
go get github.com/dgraph-io/badger/v4      # SSD 存储
go get github.com/spf13/viper              # 配置管理
go get go.uber.org/zap                     # 日志
go get github.com/prometheus/client_golang # 监控

# Operator 相关
go get sigs.k8s.io/controller-runtime
go get k8s.io/api
go get k8s.io/apimachinery
go get k8s.io/client-go

# gRPC（节点间通信）
go get google.golang.org/grpc
go get google.golang.org/protobuf

# 整理依赖
go mod tidy
```

### 0.3 Hello World 服务器

#### cmd/server/main.go
```go
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/tidwall/redcon"
)

var addr = flag.String("addr", ":6379", "server address")

func main() {
	flag.Parse()

	// 简单的内存存储
	store := make(map[string]string)

	// 创建 Redis 协议服务器
	go func() {
		log.Printf("AutoCache server starting on %s", *addr)
		err := redcon.ListenAndServe(*addr,
			func(conn redcon.Conn, cmd redcon.Command) {
				switch string(cmd.Args[0]) {
				case "PING", "ping":
					conn.WriteString("PONG")
				case "SET", "set":
					if len(cmd.Args) < 3 {
						conn.WriteError("ERR wrong number of arguments for 'set' command")
						return
					}
					store[string(cmd.Args[1])] = string(cmd.Args[2])
					conn.WriteString("OK")
				case "GET", "get":
					if len(cmd.Args) < 2 {
						conn.WriteError("ERR wrong number of arguments for 'get' command")
						return
					}
					val, ok := store[string(cmd.Args[1])]
					if !ok {
						conn.WriteNull()
						return
					}
					conn.WriteBulkString(val)
				case "DEL", "del":
					if len(cmd.Args) < 2 {
						conn.WriteError("ERR wrong number of arguments for 'del' command")
						return
					}
					count := 0
					for i := 1; i < len(cmd.Args); i++ {
						if _, ok := store[string(cmd.Args[i])]; ok {
							delete(store, string(cmd.Args[i]))
							count++
						}
					}
					conn.WriteInt(count)
				case "QUIT", "quit":
					conn.WriteString("OK")
					conn.Close()
				case "COMMAND", "command":
					// redis-benchmark 需要这个命令
					conn.WriteArray(0)
				case "DBSIZE", "dbsize":
					conn.WriteInt(len(store))
				default:
					conn.WriteError("ERR unknown command '" + string(cmd.Args[0]) + "'")
				}
			},
			func(conn redcon.Conn) bool {
				// 接受连接
				log.Printf("Client connected: %s", conn.RemoteAddr())
				return true
			},
			func(conn redcon.Conn, err error) {
				// 连接关闭
				log.Printf("Client disconnected: %s", conn.RemoteAddr())
			},
		)
		if err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// 优雅关闭
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("Shutting down...")
}
```

#### 验证
```bash
# 终端 1：启动服务器
go run cmd/server/main.go

# 终端 2：使用 redis-cli 测试
redis-cli -p 6379
> PING
PONG
> SET hello world
OK
> GET hello
"world"

# 使用 redis-benchmark 测试
redis-benchmark -p 6379 -n 10000 -c 10 -t set,get
```

### 0.4 Hello World Operator

#### 初始化 Operator 项目
```bash
cd ~/Code/autocache

# 使用 kubebuilder 初始化（如果是全新项目）
# 注意：我们已经有项目结构了，这里手动创建

# 创建 API 类型文件
cat > api/v1alpha1/groupversion_info.go << 'EOF'
// Package v1alpha1 contains API Schema definitions for the cache v1alpha1 API group
// +kubebuilder:object:generate=true
// +groupName=cache.autocache.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is group version used to register these objects
	GroupVersion = schema.GroupVersion{Group: "cache.autocache.io", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
EOF
```

#### api/v1alpha1/autocache_types.go
```go
package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AutoCacheSpec defines the desired state of AutoCache
type AutoCacheSpec struct {
	// Replicas is the number of cache nodes
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	// +kubebuilder:default=3
	Replicas int32 `json:"replicas,omitempty"`

	// Image is the container image for cache nodes
	// +kubebuilder:default="autocache:latest"
	Image string `json:"image,omitempty"`

	// Resources defines the resource requirements
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Port is the port for Redis protocol
	// +kubebuilder:default=6379
	Port int32 `json:"port,omitempty"`
}

// AutoCacheStatus defines the observed state of AutoCache
type AutoCacheStatus struct {
	// Phase represents the current phase of the cluster
	// +kubebuilder:validation:Enum=Pending;Creating;Running;Failed
	Phase string `json:"phase,omitempty"`

	// ReadyReplicas is the number of ready nodes
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Nodes contains the list of cluster nodes
	Nodes []NodeStatus `json:"nodes,omitempty"`

	// Conditions represent the latest available observations
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// NodeStatus represents the status of a cache node
type NodeStatus struct {
	// Name is the node name
	Name string `json:"name"`

	// Address is the node address
	Address string `json:"address"`

	// Role is master or replica
	Role string `json:"role"`

	// Slots assigned to this node (for masters)
	Slots string `json:"slots,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=`.spec.replicas`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
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
```

### 0.5 Kind 集群配置

#### scripts/kind-config.yaml
```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: autocache-dev
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: 30000
        hostPort: 30000
        protocol: TCP
  - role: worker
  - role: worker
  - role: worker
```

```bash
# 创建多节点集群
kind create cluster --config scripts/kind-config.yaml

# 验证
kubectl get nodes
```

### 0.6 技术验证检查清单

| 验证项 | 命令 | 预期结果 |
|--------|------|----------|
| Go 版本 | `go version` | go1.21+ |
| Docker | `docker version` | 显示版本信息 |
| Kind | `kind version` | 显示版本 |
| kubectl | `kubectl version` | 显示客户端版本 |
| Kind 集群 | `kubectl get nodes` | 3+ 个 Ready 节点 |
| Hello Server | `redis-cli PING` | PONG |
| redis-benchmark | 见上文 | 正常输出 QPS |

---

## 交付物

- [x] 开发环境安装完成
- [x] 项目目录结构创建
- [x] go.mod 初始化，依赖安装
- [x] Makefile 配置
- [x] Hello World Redis 服务器（可通过 redis-cli/redis-benchmark 测试）
- [x] CRD 类型定义文件
- [x] Kind 多节点集群配置

## 注意事项

1. **redis-benchmark 兼容性**：必须实现 `COMMAND` 命令（返回空数组即可），否则 redis-benchmark 会报错
2. **Kind 资源**：3 节点集群至少需要 4GB 内存
3. **端口冲突**：确保 6379 端口未被占用

## 下一步

完成 Phase 0 后，进入 [Phase 1: 单节点 KV 引擎](./02-phase1-kv-engine.md)
