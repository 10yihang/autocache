# AutoCache 编码规范

本文档定义 AutoCache 项目的编码规范和最佳实践。

## 1. 项目结构规范

### 1.1 目录职责

| 目录 | 职责 | 可见性 |
|------|------|--------|
| `cmd/` | 程序入口，只负责启动 | - |
| `internal/` | 核心业务逻辑 | 仅项目内可见 |
| `pkg/` | 公共工具库 | 可被外部引用 |
| `api/` | K8s CRD 类型定义 | 可被外部引用 |
| `controllers/` | Operator 控制器 | - |
| `config/` | K8s 配置清单 | - |
| `deploy/` | 部署相关（Docker, Helm） | - |
| `test/` | 测试代码 | - |

### 1.2 文件命名

```
# Go 源文件
store.go          # 主文件
store_test.go     # 测试文件
store_bench_test.go # 基准测试

# 避免
myStore.go        # 不使用驼峰
my-store.go       # 不使用连字符
```

### 1.3 包命名

```go
// Good
package memory
package cluster
package tiered

// Bad
package memoryStore    // 不使用驼峰
package memory_store   // 不使用下划线
package util           // 太笼统
package common         // 太笼统
```

## 2. Go 代码规范

### 2.1 格式化

```bash
# 必须使用官方格式化工具
gofmt -w .
goimports -w .
```

### 2.2 命名规范

#### 变量命名
```go
// Good: 简短但有意义
var buf bytes.Buffer
var conn net.Conn
var ctx context.Context
var wg sync.WaitGroup
var mu sync.Mutex

// 循环变量
for i := 0; i < n; i++ {}
for _, v := range items {}
for k, v := range m {}

// Bad: 过长或无意义
var theBuffer bytes.Buffer
var a, b, c int
```

#### 常量命名
```go
// Good: 全大写 + 下划线
const MaxRetries = 3
const DefaultTimeout = 30 * time.Second
const (
    StatusPending = iota
    StatusRunning
    StatusDone
)

// Bad
const maxRetries = 3
const MAXRETRIES = 3
```

#### 函数命名
```go
// Good: 动词开头，驼峰命名
func GetUser(id string) *User {}
func SetValue(key, value string) error {}
func IsValid() bool {}          // 返回 bool 用 Is/Has/Can
func HasPermission() bool {}

// 私有函数
func handleRequest() {}
func parseConfig() {}
```

#### 接口命名
```go
// Good: 通常以 -er 结尾
type Reader interface {}
type Writer interface {}
type Closer interface {}
type Engine interface {}

// 单方法接口以方法名 + er
type Stringer interface {
    String() string
}
```

### 2.3 错误处理

#### 定义错误
```go
// 使用 errors.New 定义简单错误
var ErrNotFound = errors.New("key not found")
var ErrWrongType = errors.New("wrong type")

// 使用自定义类型定义复杂错误
type ClusterError struct {
    Type string
    Slot uint16
    Addr string
}

func (e *ClusterError) Error() string {
    return fmt.Sprintf("%s %d %s", e.Type, e.Slot, e.Addr)
}
```

#### 错误检查
```go
// Good: 立即检查错误
result, err := doSomething()
if err != nil {
    return fmt.Errorf("do something: %w", err)
}

// Good: 使用 errors.Is 和 errors.As
if errors.Is(err, ErrNotFound) {
    // handle not found
}

var clusterErr *ClusterError
if errors.As(err, &clusterErr) {
    // handle cluster error
}

// Bad: 忽略错误
result, _ := doSomething()
```

#### 错误包装
```go
// Good: 使用 %w 包装错误，保留错误链
if err != nil {
    return fmt.Errorf("parse config: %w", err)
}

// 添加上下文
if err != nil {
    return fmt.Errorf("parse config file %s: %w", path, err)
}
```

### 2.4 并发编程

#### Goroutine
```go
// Good: 明确生命周期
func (s *Server) Start() {
    s.wg.Add(1)
    go func() {
        defer s.wg.Done()
        s.acceptLoop()
    }()
}

func (s *Server) Stop() {
    close(s.stopCh)
    s.wg.Wait()
}

// Bad: 野生 goroutine
go doSomething()
```

#### Channel
```go
// Good: 明确 channel 方向
func worker(jobs <-chan Job, results chan<- Result) {}

// Good: 使用 select 处理超时
select {
case result := <-ch:
    // handle result
case <-time.After(5 * time.Second):
    return ErrTimeout
case <-ctx.Done():
    return ctx.Err()
}
```

#### 锁
```go
// Good: 使用 defer 释放锁
func (s *Store) Get(key string) interface{} {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.data[key]
}

// Good: 缩小锁的范围
func (s *Store) Update(key string, fn func(interface{}) interface{}) {
    s.mu.Lock()
    value := fn(s.data[key])
    s.data[key] = value
    s.mu.Unlock()
    
    // 锁外执行通知
    s.notifyWatchers(key, value)
}
```

### 2.5 接口设计

```go
// Good: 小接口
type Engine interface {
    Get(ctx context.Context, key string) (string, error)
    Set(ctx context.Context, key string, value string, ttl time.Duration) error
    Del(ctx context.Context, keys ...string) (int64, error)
}

// Good: 接口由使用者定义
// store.go
type Store struct {}

func (s *Store) Get(key string) string {}
func (s *Store) Set(key, value string) {}

// handler.go - 只依赖需要的方法
type Getter interface {
    Get(key string) string
}

func HandleGet(g Getter, key string) {}
```

### 2.6 测试规范

#### 单元测试
```go
func TestStore_Get(t *testing.T) {
    // Arrange
    store := NewStore()
    store.Set("key", "value")
    
    // Act
    got, err := store.Get("key")
    
    // Assert
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if got != "value" {
        t.Errorf("Get() = %v, want %v", got, "value")
    }
}

// 表格驱动测试
func TestKeySlot(t *testing.T) {
    tests := []struct {
        name string
        key  string
        want uint16
    }{
        {"simple key", "foo", 12182},
        {"hash tag", "{user:123}.name", 11535},
        {"empty", "", 0},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := KeySlot(tt.key)
            if got != tt.want {
                t.Errorf("KeySlot(%q) = %v, want %v", tt.key, got, tt.want)
            }
        })
    }
}
```

#### 基准测试
```go
func BenchmarkStore_Get(b *testing.B) {
    store := NewStore()
    store.Set("key", "value")
    
    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            store.Get("key")
        }
    })
}
```

## 3. 注释规范

### 3.1 包注释
```go
// Package memory implements an in-memory key-value store with sharding.
// It supports concurrent access and various eviction policies.
package memory
```

### 3.2 类型和函数注释
```go
// Store is a thread-safe in-memory key-value store.
// It uses sharding to reduce lock contention.
type Store struct {
    shards []*shard
    // ...
}

// Get retrieves the value for the given key.
// It returns ErrNotFound if the key does not exist.
// It returns ErrExpired if the key has expired (and removes it).
func (s *Store) Get(ctx context.Context, key string) (string, error) {
    // ...
}
```

### 3.3 TODO 注释
```go
// TODO: implement LFU eviction policy
// TODO(username): fix race condition in cluster join
// FIXME: handle edge case when slot is migrating
```

## 4. 日志规范

### 4.1 日志级别

| 级别 | 用途 |
|------|------|
| Debug | 调试信息，生产环境关闭 |
| Info | 重要的业务事件 |
| Warn | 可恢复的错误或异常情况 |
| Error | 不可恢复的错误 |
| Fatal | 致命错误，程序退出 |

### 4.2 结构化日志

```go
// Good: 使用结构化日志
logger.Info("client connected",
    zap.String("remote_addr", conn.RemoteAddr().String()),
    zap.Int("connections", s.connCount),
)

logger.Error("failed to save snapshot",
    zap.String("path", path),
    zap.Error(err),
)

// Bad: 格式化字符串
log.Printf("client %s connected, total: %d", addr, count)
```

### 4.3 敏感信息

```go
// Good: 脱敏处理
logger.Info("auth attempt",
    zap.String("user", username),
    zap.String("password", "***"),
)

// Bad: 记录敏感信息
logger.Info("auth", zap.String("password", password))
```

## 5. 配置规范

### 5.1 配置结构

```go
// Config 应用配置
type Config struct {
    // Server 服务器配置
    Server ServerConfig `yaml:"server"`
    
    // Cluster 集群配置
    Cluster ClusterConfig `yaml:"cluster"`
    
    // Storage 存储配置
    Storage StorageConfig `yaml:"storage"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
    // Addr 监听地址
    Addr string `yaml:"addr" default:":6379"`
    
    // MaxConnections 最大连接数
    MaxConnections int `yaml:"max_connections" default:"10000"`
}
```

### 5.2 配置验证

```go
// Validate 验证配置
func (c *Config) Validate() error {
    if c.Server.Addr == "" {
        return errors.New("server.addr is required")
    }
    if c.Server.MaxConnections <= 0 {
        return errors.New("server.max_connections must be positive")
    }
    return nil
}
```

## 6. API 设计规范

### 6.1 Context 使用

```go
// Good: 第一个参数是 context
func (s *Store) Get(ctx context.Context, key string) (string, error) {}

// 使用 context 控制超时
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()

result, err := store.Get(ctx, key)
```

### 6.2 Options 模式

```go
// 简单配置使用 Config 结构体
func NewStore(cfg *Config) *Store {}

// 复杂配置使用 Options 模式
type Option func(*options)

func WithMaxMemory(size int64) Option {
    return func(o *options) {
        o.maxMemory = size
    }
}

func NewStore(opts ...Option) *Store {
    o := defaultOptions()
    for _, opt := range opts {
        opt(&o)
    }
    // ...
}
```

## 7. Git 规范

### 7.1 提交信息

```
<type>(<scope>): <subject>

<body>

<footer>
```

#### Type
- `feat`: 新功能
- `fix`: Bug 修复
- `docs`: 文档更新
- `style`: 代码格式（不影响功能）
- `refactor`: 重构
- `perf`: 性能优化
- `test`: 测试
- `chore`: 构建/工具

#### 示例
```
feat(engine): implement LRU eviction policy

Add support for allkeys-lru eviction policy.
When memory exceeds maxmemory limit, evict least recently used keys.

Closes #123
```

### 7.2 分支命名

```
main              # 主分支
develop           # 开发分支
feature/xxx       # 功能分支
fix/xxx           # 修复分支
release/v1.0.0    # 发布分支
```

## 8. 代码检查

### 8.1 Linter 配置

#### .golangci.yml
```yaml
run:
  timeout: 5m
  
linters:
  enable:
    - gofmt
    - goimports
    - govet
    - errcheck
    - staticcheck
    - gosimple
    - ineffassign
    - unused
    - misspell
    - gocritic
    - revive
    
linters-settings:
  errcheck:
    check-type-assertions: true
  govet:
    check-shadowing: true
  goimports:
    local-prefixes: github.com/10yihang/autocache
  revive:
    rules:
      - name: var-naming
      - name: package-comments
      - name: unexported-return
        
issues:
  exclude-rules:
    - path: _test\.go
      linters:
        - errcheck
```

### 8.2 Pre-commit Hook

```bash
#!/bin/sh
# .git/hooks/pre-commit

# 格式化
gofmt -l . | grep -v vendor/ && exit 1

# Lint
golangci-lint run ./...

# 测试
go test -short ./...
```

## 9. 性能规范

### 9.1 避免内存分配

```go
// Good: 预分配切片
result := make([]string, 0, len(keys))
for _, key := range keys {
    result = append(result, key)
}

// Good: 使用 sync.Pool
var bufPool = sync.Pool{
    New: func() interface{} {
        return new(bytes.Buffer)
    },
}

func process() {
    buf := bufPool.Get().(*bytes.Buffer)
    defer func() {
        buf.Reset()
        bufPool.Put(buf)
    }()
    // use buf
}

// Bad: 频繁分配
for i := 0; i < n; i++ {
    buf := new(bytes.Buffer)
    // ...
}
```

### 9.2 避免不必要的拷贝

```go
// Good: 使用指针
func (s *Store) Get(key string) (*Entry, bool) {}

// Good: 使用切片而非数组
func process(data []byte) {}

// Bad: 值传递大结构体
func process(entry Entry) {}
```

## 10. 安全规范

### 10.1 输入验证

```go
// Good: 验证所有外部输入
func (h *Handler) HandleSet(key, value string) error {
    if len(key) > MaxKeySize {
        return ErrKeyTooLong
    }
    if len(value) > MaxValueSize {
        return ErrValueTooLong
    }
    // ...
}
```

### 10.2 资源限制

```go
// Good: 限制连接数
type Server struct {
    maxConns   int
    connSem    chan struct{}
}

func (s *Server) Accept(conn net.Conn) error {
    select {
    case s.connSem <- struct{}{}:
        // accepted
    default:
        conn.Close()
        return ErrTooManyConnections
    }
    // ...
}
```

---

遵循这些规范将帮助保持代码质量和一致性。在代码审查中应检查这些规范的遵守情况。
