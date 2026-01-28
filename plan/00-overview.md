# AutoCache 项目总览

## 项目简介

AutoCache 是一个基于云原生架构的分布式缓存系统，采用 Golang 开发，兼容 Redis 协议（RESP），支持 Kubernetes 原生调度、智能冷热数据分层、Gossip 集群协议，旨在提供高性能、高可用、低成本的分布式 KV 缓存服务。

## 核心特性

| 特性 | 描述 |
|------|------|
| **Redis 协议兼容** | 完整支持 RESP2/RESP3 协议，可使用 redis-cli、redis-benchmark 直接连接 |
| **Gossip 集群** | 参考 Redis Cluster 的 Gossip 协议实现节点发现与状态同步 |
| **一致性哈希分片** | 16384 slots 分片，支持 MOVED/ASK 重定向 |
| **冷热数据分层** | 内存（热）- SSD（温）- 云存储（冷）三级存储架构 |
| **K8s Operator** | CRD 定义集群资源，自动化生命周期管理 |
| **调度增强** | 拓扑感知、负载感知的智能调度策略 |
| **可观测性** | Prometheus 指标 + Grafana 面板 + 告警 |

## 技术栈

| 类别 | 技术选型 |
|------|----------|
| 开发语言 | Go 1.21+ |
| Redis 协议 | tidwall/redcon |
| 集群通信 | 自研 Gossip + gRPC |
| 存储引擎 | 内存 Map + BadgerDB (SSD) |
| 容器编排 | Kubernetes 1.28+ |
| Operator | Kubebuilder / controller-runtime |
| 监控 | Prometheus + Grafana |
| 本地开发 | Kind / Docker |

## 项目结构

```
autocache/
├── cmd/                          # 入口程序
│   ├── server/                   # 缓存服务器
│   │   └── main.go
│   └── operator/                 # K8s Operator
│       └── main.go
│
├── internal/                     # 内部包（不对外暴露）
│   ├── config/                   # 配置管理
│   │   ├── config.go             # 配置结构定义
│   │   └── loader.go             # 配置加载器
│   │
│   ├── engine/                   # 存储引擎
│   │   ├── engine.go             # Engine 接口定义
│   │   ├── memory/               # 内存存储实现
│   │   │   ├── store.go          # 核心存储结构
│   │   │   ├── dict.go           # 分片哈希表
│   │   │   ├── expiry.go         # 过期管理
│   │   │   └── eviction.go       # 淘汰策略 (LRU/LFU)
│   │   ├── badger/               # SSD 存储实现
│   │   │   ├── store.go
│   │   │   └── compaction.go
│   │   └── tiered/               # 冷热分层管理
│   │       ├── manager.go        # 分层管理器
│   │       ├── policy.go         # 分层策略
│   │       ├── migrator.go       # 数据迁移
│   │       └── stats.go          # 访问统计
│   │
│   ├── protocol/                 # Redis 协议层
│   │   ├── server.go             # TCP 服务器 (基于 redcon)
│   │   ├── handler.go            # 命令处理器
│   │   ├── commands/             # 命令实现
│   │   │   ├── string.go         # String 命令
│   │   │   ├── hash.go           # Hash 命令
│   │   │   ├── list.go           # List 命令
│   │   │   ├── set.go            # Set 命令
│   │   │   ├── zset.go           # ZSet 命令
│   │   │   ├── key.go            # Key 通用命令
│   │   │   ├── server.go         # 服务器命令 (INFO, PING)
│   │   │   └── cluster.go        # 集群命令 (CLUSTER, MOVED)
│   │   └── reply/                # 响应构建
│   │       └── reply.go
│   │
│   ├── cluster/                  # 分布式集群
│   │   ├── cluster.go            # 集群管理器
│   │   ├── node.go               # 节点定义
│   │   ├── slots.go              # Slot 分配管理
│   │   ├── gossip/               # Gossip 协议
│   │   │   ├── gossip.go         # Gossip 核心
│   │   │   ├── message.go        # 消息定义
│   │   │   ├── ping.go           # PING/PONG
│   │   │   └── failover.go       # 故障检测
│   │   ├── hash/                 # 一致性哈希
│   │   │   ├── hashring.go       # 哈希环
│   │   │   └── crc16.go          # CRC16 (Redis 兼容)
│   │   ├── migrate/              # 数据迁移
│   │   │   ├── migrator.go       # 迁移管理器
│   │   │   └── stream.go         # 流式迁移
│   │   └── replication/          # 主从复制
│   │       ├── master.go
│   │       └── replica.go
│   │
│   ├── persistence/              # 持久化
│   │   ├── rdb/                  # RDB 快照
│   │   │   ├── rdb.go
│   │   │   ├── encoder.go
│   │   │   └── decoder.go
│   │   └── aof/                  # AOF 日志
│   │       ├── aof.go
│   │       ├── writer.go
│   │       └── rewriter.go
│   │
│   └── metrics/                  # 监控指标
│       ├── metrics.go            # 指标定义
│       ├── collector.go          # 指标收集
│       └── exporter.go           # Prometheus 导出
│
├── pkg/                          # 公共包（可对外）
│   ├── log/                      # 日志
│   │   └── logger.go
│   ├── utils/                    # 工具函数
│   │   ├── bytes.go
│   │   ├── time.go
│   │   └── hash.go
│   └── errors/                   # 错误定义
│       └── errors.go
│
├── api/                          # K8s API 定义
│   └── v1alpha1/
│       ├── groupversion_info.go
│       ├── autocache_types.go    # CRD 类型
│       └── zz_generated.deepcopy.go
│
├── controllers/                  # Operator 控制器
│   ├── autocache_controller.go   # 主控制器
│   ├── statefulset.go            # StatefulSet 管理
│   ├── service.go                # Service 管理
│   ├── configmap.go              # ConfigMap 管理
│   └── scheduler/                # 调度增强
│       ├── plugin.go             # 调度插件
│       ├── topology.go           # 拓扑感知
│       └── loadaware.go          # 负载感知
│
├── config/                       # K8s 配置
│   ├── crd/                      # CRD 定义
│   │   └── bases/
│   ├── rbac/                     # RBAC 权限
│   ├── manager/                  # Operator 部署
│   └── samples/                  # 示例 CR
│
├── deploy/                       # 部署相关
│   ├── docker/
│   │   └── Dockerfile
│   └── helm/
│       └── autocache/
│           ├── Chart.yaml
│           ├── values.yaml
│           └── templates/
│
├── test/                         # 测试
│   ├── unit/                     # 单元测试
│   ├── integration/              # 集成测试
│   ├── e2e/                      # 端到端测试
│   └── benchmark/                # 性能测试
│       └── redis_benchmark.sh
│
├── docs/                         # 文档
│   ├── design/                   # 设计文档
│   ├── api/                      # API 文档
│   └── user-guide/               # 用户指南
│
├── scripts/                      # 脚本
│   ├── setup-kind.sh             # Kind 集群搭建
│   ├── build.sh                  # 构建脚本
│   └── test.sh                   # 测试脚本
│
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

## 里程碑规划

| 里程碑 | 时间节点 | 交付物 |
|--------|----------|--------|
| **M0** | 2026.01.05 | 开发环境就绪，Hello Operator 跑通 |
| **M1** | 2026.02.09 | 单节点 KV 引擎，redis-benchmark 可测试 |
| **M2** | 2026.03.09 | 3 节点 Gossip 集群，支持分片路由 |
| **M3** | 2026.04.13 | K8s Operator，一键部署集群 |
| **M4** | 2026.05.18 | 冷热分层 + 调度增强 |
| **M5** | 2026.06.12 | 监控完善，论文完成，答辩就绪 |

## 性能目标

| 指标 | 目标值 | 测试方法 |
|------|--------|----------|
| QPS (GET) | > 100,000 | redis-benchmark -t get -n 1000000 |
| QPS (SET) | > 80,000 | redis-benchmark -t set -n 1000000 |
| P99 延迟 | < 1ms | redis-benchmark --latency |
| 内存效率 | > 90% | 自研测试工具 |
| 冷热分层后存储成本 | 降低 30%+ | 对比测试 |

## 文档索引

| 文档 | 说明 |
|------|------|
| [01-phase0-setup.md](./01-phase0-setup.md) | Phase 0: 环境搭建 |
| [02-phase1-kv-engine.md](./02-phase1-kv-engine.md) | Phase 1: 单节点 KV 引擎 |
| [03-phase2-cluster.md](./03-phase2-cluster.md) | Phase 2: Gossip 分布式集群 |
| [04-phase3-operator.md](./04-phase3-operator.md) | Phase 3: K8s Operator |
| [05-phase4-optimization.md](./05-phase4-optimization.md) | Phase 4: 冷热分层 + 调度增强 |
| [06-phase5-monitoring.md](./06-phase5-monitoring.md) | Phase 5: 监控运维 |
| [coding-standards.md](./coding-standards.md) | 编码规范 |
| [architecture.md](./architecture.md) | 架构设计 |
